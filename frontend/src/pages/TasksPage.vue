<script setup lang="ts">
import { computed, h, nextTick, onMounted, ref, watch } from "vue";
import {
  NButton,
  NIcon,
  NProgress,
  NSpace,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { RefreshOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import StatCard from "../components/StatCard.vue";
import TaskKindTag from "../components/TaskKindTag.vue";
import TaskStatusTag from "../components/TaskStatusTag.vue";
import { errorMessage } from "../api";
import type { Task } from "../api";
import { useTaskStore } from "../stores/tasks";
import { useHostsStore } from "../stores/hosts";
import { formatDateTime, formatTimeOnly, isPendingStatus } from "../utils/format";

const message = useMessage();
const taskStore = useTaskStore();
const hosts = useHostsStore();

const selectedId = ref("");
const showDetail = ref(false);
const logBox = ref<HTMLElement | null>(null);
const canceling = ref("");

const selectedTask = computed(
  () => taskStore.items.find((task) => task.id === selectedId.value) ?? null
);

function hostName(hostID?: string | null): string {
  if (!hostID) return "本机";
  return hosts.items.find((item) => item.id === hostID)?.name ?? hostID;
}

const stats = computed(() => ({
  queued: taskStore.items.filter((task) => task.status === "queued").length,
  running: taskStore.items.filter((task) => task.status === "running").length,
  succeeded: taskStore.items.filter((task) => task.status === "succeeded").length,
  failed: taskStore.items.filter((task) => task.status === "failed").length,
}));

function progressStatus(task: Task): "default" | "success" | "error" | "info" | "warning" {
  switch (task.status) {
    case "running":
      return "info";
    case "succeeded":
      return "success";
    case "failed":
      return "error";
    case "canceled":
      return "warning";
    default:
      return "default";
  }
}

const columns: DataTableColumns<Task> = [
  {
    title: "任务 ID",
    key: "id",
    width: 186,
    render: (row) => h("span", { class: "mono-text" }, row.id),
  },
  {
    title: "类型",
    key: "kind",
    width: 78,
    render: (row) => h(TaskKindTag, { kind: row.kind }),
  },
  {
    title: "计算机",
    key: "hostID",
    width: 120,
    render: (row) => h("span", { class: "cell-muted" }, hostName(row.hostID)),
  },
  {
    title: "目标",
    key: "target",
    width: 150,
    render: (row) => h("span", { class: "cell-strong" }, row.target || "—"),
  },
  {
    title: "状态",
    key: "status",
    width: 86,
    render: (row) => h(TaskStatusTag, { status: row.status }),
  },
  {
    title: "进度",
    key: "progress",
    width: 168,
    render: (row) =>
      h(NProgress, {
        type: "line",
        percentage: row.progress,
        status: progressStatus(row),
        height: 6,
        showIndicator: true,
      }),
  },
  {
    title: "当前步骤",
    key: "step",
    width: 190,
    ellipsis: { tooltip: true },
    render: (row) => h("span", { class: "cell-muted" }, row.step || "—"),
  },
  {
    title: "创建时间",
    key: "createdAt",
    width: 156,
    render: (row) => h("span", { class: "cell-muted" }, formatDateTime(row.createdAt)),
  },
  {
    title: "操作",
    key: "actions",
    width: 140,
    align: "right",
    render: (row) =>
      h(
        NSpace,
        { justify: "end", size: 6, wrap: false },
        {
          default: () => [
            h(
              NButton,
              {
                size: "tiny",
                quaternary: true,
                onClick: () => openDetail(row.id),
              },
              { default: () => "日志" }
            ),
            isPendingStatus(row.status)
              ? h(
                  NButton,
                  {
                    size: "tiny",
                    tertiary: true,
                    type: "error",
                    loading: canceling.value === row.id,
                    onClick: () => cancelTask(row),
                  },
                  { default: () => "取消" }
                )
              : null,
          ],
        }
      ),
  },
];

function openDetail(id: string) {
  selectedId.value = id;
  showDetail.value = true;
  void nextTick(scrollLogs);
}

async function cancelTask(task: Task) {
  canceling.value = task.id;
  try {
    await taskStore.cancel(task.id);
    message.success(`已请求取消任务 ${task.id}`);
    await taskStore.fetch();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    canceling.value = "";
  }
}

async function clearFinished() {
  try {
    const removed = await taskStore.clearFinished();
    message.success(`已清理 ${removed} 个已结束任务`);
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function refresh() {
  await taskStore.fetch();
}

function scrollLogs() {
  if (logBox.value) {
    logBox.value.scrollTop = logBox.value.scrollHeight;
  }
}

watch(
  () => selectedTask.value?.logs?.length,
  () => {
    if (showDetail.value) {
      void nextTick(scrollLogs);
    }
  }
);

onMounted(() => {
  void taskStore.fetch();
  void hosts.fetch();
});
</script>

<template>
  <PageHeader title="任务队列" subtitle="所有写操作串行执行，可查询进度、步骤与日志">
    <n-button size="small" :loading="taskStore.loading" @click="refresh">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      刷新
    </n-button>
    <n-button size="small" @click="clearFinished">清空已结束</n-button>
  </PageHeader>

  <n-alert v-if="taskStore.error" type="error" class="inline-error" title="读取任务失败">
    {{ taskStore.error }}
  </n-alert>

  <div class="stat-grid">
    <StatCard label="排队中" :value="stats.queued" hint="等待执行的任务" />
    <StatCard label="执行中" :value="stats.running" hint="当前正在执行" />
    <StatCard label="已完成" :value="stats.succeeded" hint="最近 100 条任务" />
    <StatCard label="失败" :value="stats.failed" hint="可在日志中查看原因" />
  </div>

  <div class="panel table-panel">
    <n-data-table
      :columns="columns"
      :data="taskStore.items"
      :loading="taskStore.loading"
      :bordered="false"
      :single-line="false"
      size="small"
      :row-key="(row: Task) => row.id"
      :scroll-x="1180"
    >
      <template #empty>
        <div class="empty-hint">
          还没有任务。新建、克隆、编辑或删除虚拟机后会在这里显示。
        </div>
      </template>
    </n-data-table>
  </div>

  <n-drawer v-model:show="showDetail" :width="640" placement="right">
    <n-drawer-content v-if="selectedTask" closable>
      <template #header>
        <div class="drawer-title">
          <span>任务详情</span>
          <TaskKindTag :kind="selectedTask.kind" />
          <TaskStatusTag :status="selectedTask.status" />
        </div>
      </template>

      <n-descriptions :column="2" label-placement="top" size="small" bordered>
        <n-descriptions-item label="任务 ID" :span="2">
          <span class="mono-text">{{ selectedTask.id }}</span>
        </n-descriptions-item>
        <n-descriptions-item label="目标">
          {{ selectedTask.target || "—" }}
        </n-descriptions-item>
        <n-descriptions-item label="计算机">
          {{ hostName(selectedTask.hostID) }}
        </n-descriptions-item>
        <n-descriptions-item label="创建时间">
          {{ formatDateTime(selectedTask.createdAt) }}
        </n-descriptions-item>
        <n-descriptions-item label="开始时间">
          {{ formatDateTime(selectedTask.startedAt) }}
        </n-descriptions-item>
        <n-descriptions-item label="结束时间">
          {{ formatDateTime(selectedTask.finishedAt) }}
        </n-descriptions-item>
        <n-descriptions-item label="当前步骤" :span="2">
          {{ selectedTask.step || "—" }}
        </n-descriptions-item>
      </n-descriptions>

      <div class="detail-section">
        <div class="detail-section-title">进度</div>
        <n-progress
          type="line"
          :percentage="selectedTask.progress"
          :status="progressStatus(selectedTask)"
          :height="10"
        />
      </div>

      <n-alert
        v-if="selectedTask.error"
        type="error"
        title="任务错误"
        style="margin-top: 14px"
      >
        {{ selectedTask.error }}
      </n-alert>

      <div class="detail-section">
        <div class="detail-section-title">执行日志</div>
        <div ref="logBox" class="log-view">
          <div v-if="(selectedTask.logs ?? []).length === 0" class="cell-muted">
            暂无日志
          </div>
          <div
            v-for="(log, index) in selectedTask.logs ?? []"
            :key="index"
            class="log-line"
          >
            <span class="log-time">{{ formatTimeOnly(log.time) }}</span>
            <span :class="'log-' + log.level">{{ log.message }}</span>
          </div>
        </div>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button
            v-if="isPendingStatus(selectedTask.status)"
            size="small"
            type="error"
            ghost
            :loading="canceling === selectedTask.id"
            @click="cancelTask(selectedTask)"
          >
            取消任务
          </n-button>
          <n-button size="small" @click="showDetail = false">关闭</n-button>
        </n-space>
      </template>
    </n-drawer-content>
  </n-drawer>
</template>

<style scoped>
.table-panel {
  padding: 6px 8px;
}

.drawer-title {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 16px;
  font-weight: 600;
}
</style>
