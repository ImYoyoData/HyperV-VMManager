<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import {
  AddCircleOutline,
  BuildOutline,
  CopyOutline,
  DesktopOutline,
  GridOutline,
  LayersOutline,
  ListOutline,
  PlayCircleOutline,
  RefreshOutline,
  ServerOutline,
} from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import StatCard from "../components/StatCard.vue";
import TaskKindTag from "../components/TaskKindTag.vue";
import TaskStatusTag from "../components/TaskStatusTag.vue";
import VmStateTag from "../components/VmStateTag.vue";
import { HyperVService, errorMessage } from "../api";
import { useAppStore } from "../stores/app";
import { useTaskStore } from "../stores/tasks";
import { useVMStore } from "../stores/vms";
import { formatDuration, formatMB } from "../utils/format";

const router = useRouter();
const message = useMessage();

const app = useAppStore();
const vmStore = useVMStore();
const taskStore = useTaskStore();

const refreshing = ref(false);

const host = computed(() => app.hostInfo);
const assignedMemory = computed(() =>
  vmStore.items.reduce((sum, vm) => sum + (vm.memoryAssignedMB || 0), 0)
);

const stateSummary = computed(() => {
  const counts = new Map<string, number>();
  for (const vm of vmStore.items) {
    counts.set(vm.state, (counts.get(vm.state) ?? 0) + 1);
  }
  return [
    { state: "Running", count: counts.get("Running") ?? 0 },
    { state: "Off", count: counts.get("Off") ?? 0 },
    { state: "Saved", count: counts.get("Saved") ?? 0 },
    { state: "Paused", count: counts.get("Paused") ?? 0 },
  ];
});

async function refreshAll() {
  refreshing.value = true;
  await Promise.all([app.refreshHostInfo(), vmStore.fetch({ force: true }), taskStore.fetch()]);
  refreshing.value = false;
}

async function openTool(tool: string) {
  try {
    await HyperVService.OpenTool(tool);
  } catch (err) {
    message.error(errorMessage(err));
  }
}
</script>

<template>
  <PageHeader title="总览" subtitle="主机状态、虚拟机概况与最近任务">
    <n-button size="small" :loading="refreshing" @click="refreshAll">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      刷新
    </n-button>
  </PageHeader>

  <n-alert
    v-if="app.hostError"
    type="warning"
    class="inline-error"
    title="无法完整读取 Hyper-V 主机信息"
  >
    {{ app.hostError }}
  </n-alert>

  <div class="stat-grid">
    <StatCard label="虚拟机总数" :value="vmStore.totalCount" :hint="`${vmStore.runningCount} 台运行中`">
      <template #icon>
        <n-icon :component="DesktopOutline" />
      </template>
    </StatCard>
    <StatCard
      label="运行中"
      :value="vmStore.runningCount"
      :hint="vmStore.totalCount > 0 ? `占比 ${Math.round((vmStore.runningCount / vmStore.totalCount) * 100)}%` : '暂无虚拟机'"
    >
      <template #icon>
        <n-icon :component="PlayCircleOutline" />
      </template>
    </StatCard>
    <StatCard
      label="已分配内存"
      :value="formatMB(assignedMemory)"
      hint="所有运行中虚拟机的当前内存"
    >
      <template #icon>
        <n-icon :component="LayersOutline" />
      </template>
    </StatCard>
    <StatCard
      label="待处理任务"
      :value="taskStore.pendingCount"
      :hint="taskStore.failedCount > 0 ? `${taskStore.failedCount} 个任务失败` : '队列空闲'"
    >
      <template #icon>
        <n-icon :component="ListOutline" />
      </template>
    </StatCard>
  </div>

  <div class="dashboard-grid">
    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">主机信息</div>
          <div class="card-subtitle">Hyper-V 服务与系统概况</div>
        </div>
        <div class="spacer"></div>
        <n-tag
          v-if="host"
          size="small"
          :type="host.enhancedSessionEnabled ? 'success' : 'default'"
          :bordered="false"
        >
          增强会话{{ host.enhancedSessionEnabled ? "已开启" : "已关闭" }}
        </n-tag>
      </div>

      <div v-if="host" class="info-list">
        <div>
          <div class="info-item-label">计算机名</div>
          <div class="info-item-value">{{ host.computerName }}</div>
        </div>
        <div>
          <div class="info-item-label">Hyper-V 版本</div>
          <div class="info-item-value">{{ host.hyperVVersion || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">操作系统</div>
          <div class="info-item-value">{{ host.osName || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">处理器 / 物理内存</div>
          <div class="info-item-value">
            {{ host.logicalProcessors || "—" }} 逻辑处理器 · {{ formatMB(host.totalMemoryMB) }}
          </div>
        </div>
        <div>
          <div class="info-item-label">虚拟机默认目录</div>
          <div class="info-item-value mono-text">{{ host.virtualMachinePath || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">虚拟硬盘默认目录</div>
          <div class="info-item-value mono-text">{{ host.virtualHardDiskPath || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">vmms 服务</div>
          <div class="info-item-value">
            <span class="status-dot" :class="host.vmServiceStatus === 'Running' ? 'ok' : 'bad'"></span>
            {{ host.vmServiceStatus || "未知" }}
          </div>
        </div>
        <div>
          <div class="info-item-label">TermService 服务</div>
          <div class="info-item-value">
            <span class="status-dot" :class="host.termServiceStatus === 'Running' ? 'ok' : 'idle'"></span>
            {{ host.termServiceStatus || "未知" }}
          </div>
        </div>
      </div>
      <div v-else class="empty-hint">
        {{ app.loading ? "正在读取主机信息..." : "尚未获取主机信息" }}
      </div>
    </div>

    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">快捷操作</div>
          <div class="card-subtitle">常用入口与系统工具</div>
        </div>
      </div>
      <div class="quick-grid">
        <button class="quick-btn" @click="router.push('/create')">
          <span class="quick-icon"><n-icon :component="AddCircleOutline" /></span>
          新建虚拟机
        </button>
        <button class="quick-btn" @click="router.push('/clone')">
          <span class="quick-icon"><n-icon :component="CopyOutline" /></span>
          克隆虚拟机
        </button>
        <button class="quick-btn" @click="router.push('/tasks')">
          <span class="quick-icon"><n-icon :component="ListOutline" /></span>
          任务队列
        </button>
        <button class="quick-btn" @click="router.push('/vms')">
          <span class="quick-icon"><n-icon :component="GridOutline" /></span>
          虚拟机列表
        </button>
        <button class="quick-btn" @click="openTool('hyperv-manager')">
          <span class="quick-icon"><n-icon :component="ServerOutline" /></span>
          Hyper-V 管理器
        </button>
        <button class="quick-btn" @click="openTool('switch-manager')">
          <span class="quick-icon"><n-icon :component="BuildOutline" /></span>
          虚拟交换机管理器
        </button>
      </div>
    </div>
  </div>

  <div class="dashboard-grid">
    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">最近任务</div>
          <div class="card-subtitle">克隆、新建、删除等操作的执行情况</div>
        </div>
        <div class="spacer"></div>
        <n-button size="tiny" quaternary @click="router.push('/tasks')">查看全部</n-button>
      </div>

      <div v-if="taskStore.recent.length === 0" class="empty-hint">
        暂无任务，创建或克隆虚拟机后会在这里显示进度。
      </div>
      <div v-else>
        <div v-for="task in taskStore.recent" :key="task.id" class="recent-row">
          <div class="recent-main">
            <TaskKindTag :kind="task.kind" />
            <span class="recent-target">{{ task.target || "—" }}</span>
            <span class="cell-muted recent-step">{{ task.step }}</span>
          </div>
          <div class="recent-side">
            <n-progress
              class="recent-progress"
              type="line"
              :percentage="task.progress"
              :show-indicator="false"
              :status="task.status === 'failed' ? 'error' : task.status === 'running' ? 'info' : 'default'"
            />
            <TaskStatusTag :status="task.status" />
          </div>
        </div>
      </div>
    </div>

    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">虚拟机状态分布</div>
          <div class="card-subtitle">共 {{ vmStore.totalCount }} 台虚拟机</div>
        </div>
      </div>

      <div v-for="entry in stateSummary" :key="entry.state" class="state-row">
        <div class="state-label">
          <VmStateTag :state="entry.state" />
        </div>
        <div class="state-bar">
          <div
            class="state-bar-fill"
            :style="{
              width:
                vmStore.totalCount > 0
                  ? `${Math.max((entry.count / vmStore.totalCount) * 100, entry.count > 0 ? 4 : 0)}%`
                  : '0%',
            }"
          ></div>
        </div>
        <div class="state-count">{{ entry.count }}</div>
      </div>

      <div v-if="vmStore.error" class="empty-hint">
        {{ vmStore.error }}
      </div>
      <div v-else-if="vmStore.totalCount === 0 && vmStore.loading" class="empty-hint">
        正在读取虚拟机列表...
      </div>
      <div v-else-if="vmStore.totalCount === 0" class="empty-hint">
        还没有虚拟机，去新建一台吧。
      </div>
    </div>
  </div>
</template>

<style scoped>
.recent-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 9px 0;
  border-bottom: 1px dashed var(--border-soft);
}

.recent-row:last-child {
  border-bottom: none;
}

.recent-main {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
  flex: 1;
}

.recent-target {
  font-weight: 500;
  color: var(--text-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.recent-step {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.recent-side {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
}

.recent-progress {
  width: 110px;
}

.state-row {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr) 36px;
  align-items: center;
  gap: 12px;
  padding: 7px 0;
}

.state-bar {
  height: 5px;
  border-radius: 3px;
  background: var(--bar-track);
  overflow: hidden;
}

.state-bar-fill {
  height: 100%;
  background: var(--bar-fill);
  border-radius: 3px;
  transition: width 0.3s ease;
}

.state-count {
  text-align: right;
  font-variant-numeric: tabular-nums;
  color: var(--text-3);
  font-size: 12px;
}
</style>
