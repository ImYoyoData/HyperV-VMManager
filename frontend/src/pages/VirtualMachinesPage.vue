<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  NButton,
  NDropdown,
  NIcon,
  NSpace,
  useMessage,
  type DataTableColumns,
  type DropdownOption,
} from "naive-ui";
import {
  AddCircleOutline,
  CopyOutline,
  RefreshOutline,
  SearchOutline,
} from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import HelpTip from "../components/HelpTip.vue";
import VmStateTag from "../components/VmStateTag.vue";
import { HyperVService, errorMessage } from "../api";
import type { EditRequest, VM, VirtualSwitch } from "../api";
import { useAppStore } from "../stores/app";
import { useTaskStore } from "../stores/tasks";
import { useVMStore } from "../stores/vms";
import {
  formatDuration,
  formatMB,
  primaryIPv4,
} from "../utils/format";

const router = useRouter();
const message = useMessage();

const app = useAppStore();
const vmStore = useVMStore();
const taskStore = useTaskStore();

const search = ref("");
const stateFilter = ref<string | null>(null);
const autoRefresh = ref(true);
const switches = ref<VirtualSwitch[]>([]);

const stateOptions = [
  { label: "全部状态", value: "" },
  { label: "运行中", value: "Running" },
  { label: "已关机", value: "Off" },
  { label: "已保存", value: "Saved" },
  { label: "已暂停", value: "Paused" },
];

const filteredVMs = computed(() => {
  const keyword = search.value.trim().toLowerCase();
  return vmStore.items.filter((vm) => {
    if (keyword && !vm.name.toLowerCase().includes(keyword)) return false;
    if (stateFilter.value && vm.state !== stateFilter.value) return false;
    return true;
  });
});

const showDetail = ref(false);
const selectedName = ref("");
const selectedVM = computed(
  () => vmStore.items.find((vm) => vm.name === selectedName.value) ?? null
);

const showEdit = ref(false);
const showDelete = ref(false);
const editSource = ref<VM | null>(null);
const deleteTarget = ref<VM | null>(null);
const deleteConfirmName = ref("");
const deleteFiles = ref(false);

const editForm = reactive({
  name: "",
  startupMB: 4096,
  dynamic: false,
  minimumMB: 1024,
  maximumMB: 8192,
  cpuCount: 4,
  switchName: "",
  bandwidthMbps: 100,
  checkpointsEnabled: true,
});

const switchOptions = computed(() => [
  { label: "保持不变", value: "" },
  ...switches.value.map((item) => ({
    label: `${item.name}（${item.switchType === "External" ? "外部" : item.switchType === "Internal" ? "内部" : "专用"}）`,
    value: item.name,
  })),
]);

function vmIPv4(vm: VM): string {
  const addresses = (vm.networks ?? []).flatMap((adapter) => adapter.ipAddresses ?? []);
  return primaryIPv4(addresses);
}

function vmSwitch(vm: VM): string {
  const names = (vm.networks ?? [])
    .map((adapter) => adapter.switchName)
    .filter((name) => name);
  return names.length > 0 ? names[0] : "";
}

const columns: DataTableColumns<VM> = [
  {
    title: "名称",
    key: "name",
    width: 210,
    render: (row) =>
      h("div", null, [
        h("div", { class: "vm-name" }, row.name),
        h(
          "div",
          { class: "vm-sub" },
          `第 ${row.generation} 代 · ${row.vmId ? row.vmId.slice(0, 8) : "—"}`
        ),
      ]),
  },
  {
    title: "状态",
    key: "state",
    width: 92,
    render: (row) => h(VmStateTag, { state: row.state }),
  },
  {
    title: "CPU",
    key: "processorCount",
    width: 68,
    render: (row) => h("span", { class: "cell-strong" }, `${row.processorCount} 核`),
  },
  {
    title: "内存",
    key: "memoryStartupMB",
    width: 132,
    render: (row) =>
      h("div", null, [
        h("div", { class: "cell-strong" }, formatMB(row.memoryStartupMB)),
        row.dynamicMemoryEnabled
          ? h("div", { class: "vm-sub" }, `动态 ${formatMB(row.memoryMinimumMB)} - ${formatMB(row.memoryMaximumMB)}`)
          : null,
      ]),
  },
  {
    title: "网络",
    key: "networks",
    width: 176,
    render: (row) =>
      h("div", null, [
        h("div", { class: "cell-strong" }, vmSwitch(row) || "未连接"),
        h("div", { class: "vm-sub" }, vmIPv4(row) || "无 IP"),
      ]),
  },
  {
    title: "运行时长",
    key: "uptimeSeconds",
    width: 110,
    render: (row) => h("span", { class: "cell-muted" }, formatDuration(row.uptimeSeconds)),
  },
  {
    title: "操作",
    key: "actions",
    width: 176,
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
                onClick: () => openDetail(row),
              },
              { default: () => "详情" }
            ),
            h(
              NDropdown,
              {
                trigger: "click",
                placement: "bottom-end",
                options: actionOptions(row),
                onSelect: (key: string | number) => handleAction(String(key), row),
              },
              {
                default: () =>
                  h(
                    NButton,
                    { size: "tiny", tertiary: true },
                    { default: () => "操作" }
                  ),
              }
            ),
          ],
        }
      ),
  },
];

function actionOptions(vm: VM): DropdownOption[] {
  return [
    {
      label: "连接控制台",
      key: "connect",
    },
    {
      label: "编辑配置",
      key: "edit",
    },
    { type: "divider", key: "divider-1" },
    { label: "启动", key: "start", disabled: vm.state !== "Off" },
    {
      label: "关机",
      key: "stop",
      disabled: vm.state !== "Running" && vm.state !== "Saved" && vm.state !== "Paused",
    },
    { label: "强制断电", key: "turnoff", disabled: vm.state === "Off" },
    { type: "divider", key: "divider-2" },
    { label: "保存状态", key: "save", disabled: vm.state !== "Running" },
    { label: "暂停", key: "pause", disabled: vm.state !== "Running" },
    { label: "恢复", key: "resume", disabled: vm.state !== "Paused" },
    {
      label: "重启",
      key: "restart",
      disabled: vm.state !== "Running" && vm.state !== "Saved",
    },
    { type: "divider", key: "divider-3" },
    { label: "删除虚拟机", key: "delete" },
  ];
}

async function handleAction(key: string, vm: VM) {
  switch (key) {
    case "connect":
      await connectVM(vm);
      return;
    case "edit":
      openEdit(vm);
      return;
    case "delete":
      openDelete(vm);
      return;
    default:
      await submitPower(vm, key);
  }
}

async function connectVM(vm: VM) {
  try {
    await HyperVService.ConnectVM(vm.name);
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function submitPower(vm: VM, action: string) {
  try {
    const task = await HyperVService.SubmitPower(vm.name, action);
    message.success(`已提交电源任务${task ? `（${task.id}）` : ""}`);
    void taskStore.fetch();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

function openDetail(vm: VM) {
  selectedName.value = vm.name;
  showDetail.value = true;
}

function openEdit(vm: VM) {
  editSource.value = vm;
  editForm.name = vm.name;
  editForm.startupMB = vm.memoryStartupMB;
  editForm.dynamic = vm.dynamicMemoryEnabled;
  editForm.minimumMB = vm.memoryMinimumMB || 1024;
  editForm.maximumMB = vm.memoryMaximumMB || Math.max(vm.memoryStartupMB, 8192);
  editForm.cpuCount = vm.processorCount;
  editForm.switchName = "";
  editForm.bandwidthMbps = 100;
  editForm.checkpointsEnabled = vm.checkpointType !== "Disabled";
  showEdit.value = true;
}

async function saveEdit() {
  const vm = editSource.value;
  if (!vm) return;
  const request: EditRequest = {
    newName: editForm.name !== vm.name ? editForm.name : "",
    memory: {
      startupMB: Number(editForm.startupMB),
      dynamic: editForm.dynamic,
      minimumMB: Number(editForm.minimumMB),
      maximumMB: Number(editForm.maximumMB),
    },
    cpuCount: Number(editForm.cpuCount),
    switchName: editForm.switchName,
    bandwidthMbps: Number(editForm.bandwidthMbps),
    checkpointsEnabled: editForm.checkpointsEnabled,
  };
  try {
    const task = await HyperVService.SubmitEdit(vm.name, request);
    message.success(`已提交编辑任务${task ? `（${task.id}）` : ""}`);
    showEdit.value = false;
    void taskStore.fetch();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

function openDelete(vm: VM) {
  deleteTarget.value = vm;
  deleteConfirmName.value = "";
  deleteFiles.value = false;
  showDelete.value = true;
}

const deleteConfirmValid = computed(
  () => !!deleteTarget.value && deleteConfirmName.value === deleteTarget.value.name
);

async function confirmDelete() {
  const vm = deleteTarget.value;
  if (!vm || !deleteConfirmValid.value) return;
  try {
    const task = await HyperVService.SubmitDelete(vm.name, deleteFiles.value);
    message.success(`已提交删除任务${task ? `（${task.id}）` : ""}`);
    showDelete.value = false;
    showDetail.value = false;
    void taskStore.fetch();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function fetchSwitches() {
  try {
    const items = await HyperVService.ListSwitches();
    switches.value = items ?? [];
  } catch {
    switches.value = [];
  }
}

async function refresh() {
  await Promise.all([vmStore.fetch(), fetchSwitches()]);
}

let refreshTimer: ReturnType<typeof setInterval> | undefined;

function setupAutoRefresh() {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = undefined;
  if (!autoRefresh.value) return;
  const seconds = Math.max(app.settings?.autoRefreshSeconds ?? 10, 3);
  refreshTimer = setInterval(() => {
    if (!document.hidden) {
      void vmStore.fetch();
    }
  }, seconds * 1000);
}

watch(
  () => [autoRefresh.value, app.settings?.autoRefreshSeconds],
  () => setupAutoRefresh()
);

onMounted(() => {
  void vmStore.fetch();
  void fetchSwitches();
  setupAutoRefresh();
});

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer);
});

async function restartElevated() {
  try {
    await HyperVService.RestartElevated();
  } catch (err) {
    message.error(errorMessage(err));
  }
}
</script>

<template>
  <PageHeader title="虚拟机" subtitle="查看状态、连接控制台并管理电源与配置">
    <n-button size="small" @click="router.push('/clone')">
      <template #icon>
        <n-icon :component="CopyOutline" />
      </template>
      克隆
    </n-button>
    <n-button size="small" type="primary" @click="router.push('/create')">
      <template #icon>
        <n-icon :component="AddCircleOutline" />
      </template>
      新建虚拟机
    </n-button>
  </PageHeader>

  <n-alert
    v-if="vmStore.error"
    type="warning"
    class="inline-error"
    title="读取虚拟机列表失败"
  >
    <div class="list-error">
      <span>{{ vmStore.error }}</span>
      <n-button v-if="!app.isAdmin" size="tiny" type="warning" ghost @click="restartElevated">
        以管理员身份重启
      </n-button>
    </div>
  </n-alert>

  <div class="toolbar">
    <n-input v-model:value="search" placeholder="搜索虚拟机名称" clearable style="width: 220px">
      <template #prefix>
        <n-icon :component="SearchOutline" />
      </template>
    </n-input>
    <n-select
      v-model:value="stateFilter"
      :options="stateOptions"
      placeholder="全部状态"
      clearable
      style="width: 140px"
    />
    <span class="spacer"></span>
    <n-switch v-model:value="autoRefresh" size="small" />
    <span class="cell-muted">自动刷新</span>
    <n-button size="small" :loading="vmStore.loading" @click="refresh">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      刷新
    </n-button>
  </div>

  <div class="panel table-panel">
    <n-data-table
      :columns="columns"
      :data="filteredVMs"
      :loading="vmStore.loading"
      :bordered="false"
      :single-line="false"
      size="small"
      :row-key="(row: VM) => row.vmId || row.name"
      :scroll-x="1000"
    >
      <template #empty>
        <div class="empty-hint">
          {{ search || stateFilter ? "没有匹配的虚拟机" : "还没有虚拟机，创建一台试试。" }}
        </div>
      </template>
    </n-data-table>
  </div>

  <n-drawer v-model:show="showDetail" :width="560" placement="right">
    <n-drawer-content v-if="selectedVM" closable>
      <template #header>
        <div class="drawer-title">
          <span>{{ selectedVM.name }}</span>
          <VmStateTag :state="selectedVM.state" />
        </div>
      </template>

      <div class="detail-section">
        <div class="detail-section-title">基本信息</div>
        <n-descriptions :column="2" label-placement="top" size="small" bordered>
          <n-descriptions-item label="虚拟机 ID">
            <span class="mono-text">{{ selectedVM.vmId || "—" }}</span>
          </n-descriptions-item>
          <n-descriptions-item label="代数">
            第 {{ selectedVM.generation }} 代
          </n-descriptions-item>
          <n-descriptions-item label="版本">
            {{ selectedVM.version || "—" }}
          </n-descriptions-item>
          <n-descriptions-item label="运行时长">
            {{ formatDuration(selectedVM.uptimeSeconds) }}
          </n-descriptions-item>
          <n-descriptions-item label="检查点类型">
            {{ selectedVM.checkpointType || "—" }}
          </n-descriptions-item>
          <n-descriptions-item label="状态说明">
            {{ selectedVM.status || "—" }}
          </n-descriptions-item>
          <n-descriptions-item label="虚拟机目录" :span="2">
            <span class="mono-text">{{ selectedVM.path || "—" }}</span>
          </n-descriptions-item>
        </n-descriptions>
      </div>

      <div class="detail-section">
        <div class="detail-section-title">内存与处理器</div>
        <n-descriptions :column="2" label-placement="top" size="small" bordered>
          <n-descriptions-item label="启动内存">
            {{ formatMB(selectedVM.memoryStartupMB) }}
          </n-descriptions-item>
          <n-descriptions-item label="已分配内存">
            {{ formatMB(selectedVM.memoryAssignedMB) }}
          </n-descriptions-item>
          <n-descriptions-item label="动态内存">
            {{ selectedVM.dynamicMemoryEnabled ? "已启用" : "已关闭" }}
          </n-descriptions-item>
          <n-descriptions-item label="动态范围">
            {{ selectedVM.dynamicMemoryEnabled
              ? `${formatMB(selectedVM.memoryMinimumMB)} - ${formatMB(selectedVM.memoryMaximumMB)}`
              : "—" }}
          </n-descriptions-item>
          <n-descriptions-item label="CPU 核数">
            {{ selectedVM.processorCount }} 核
          </n-descriptions-item>
        </n-descriptions>
      </div>

      <div class="detail-section">
        <div class="detail-section-title">网络适配器</div>
        <div v-if="(selectedVM.networks ?? []).length === 0" class="empty-hint">
          没有网络适配器
        </div>
        <div
          v-for="adapter in selectedVM.networks ?? []"
          :key="adapter.name || adapter.macAddress"
          class="adapter-card"
        >
          <div class="adapter-title">
            <span>{{ adapter.name || "网络适配器" }}</span>
            <n-tag size="tiny" :bordered="false" :type="adapter.switchName ? 'info' : 'default'">
              {{ adapter.switchName || "未连接交换机" }}
            </n-tag>
          </div>
          <div class="adapter-meta">
            <span>MAC：{{ adapter.macAddress || "—" }}</span>
            <span>状态：{{ adapter.status || "—" }}</span>
            <span>IP：{{ primaryIPv4(adapter.ipAddresses) || "无" }}</span>
          </div>
        </div>
      </div>

      <div class="detail-section">
        <div class="detail-section-title">虚拟硬盘</div>
        <div v-if="(selectedVM.hardDrives ?? []).length === 0" class="empty-hint">
          没有虚拟硬盘
        </div>
        <div v-for="disk in selectedVM.hardDrives ?? []" :key="disk.path" class="adapter-card">
          <div class="adapter-title">
            <span class="mono-text">{{ disk.path || "—" }}</span>
          </div>
          <div class="adapter-meta">
            <span>{{ disk.controllerType }} 控制器 {{ disk.controllerNumber }}</span>
            <span>位置 {{ disk.controllerLocation }}</span>
          </div>
        </div>

        <div v-if="(selectedVM.dvdDrives ?? []).length > 0" class="detail-subtitle">
          虚拟光驱
        </div>
        <div v-for="dvd in selectedVM.dvdDrives ?? []" :key="'dvd-' + dvd.controllerNumber + '-' + dvd.controllerLocation" class="adapter-card">
          <div class="adapter-title">
            <span class="mono-text">{{ dvd.path || "未挂载 ISO" }}</span>
          </div>
          <div class="adapter-meta">
            <span>控制器 {{ dvd.controllerNumber }} · 位置 {{ dvd.controllerLocation }}</span>
          </div>
        </div>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button size="small" @click="connectVM(selectedVM)">连接控制台</n-button>
          <n-button size="small" type="primary" @click="openEdit(selectedVM)">编辑配置</n-button>
          <n-button size="small" type="error" ghost @click="openDelete(selectedVM)">删除</n-button>
        </n-space>
      </template>
    </n-drawer-content>
  </n-drawer>

  <n-modal v-model:show="showEdit" preset="card" title="编辑虚拟机配置" style="width: 560px">
    <div v-if="editSource" class="edit-form">
      <n-form label-placement="top" size="small">
        <div class="form-grid">
          <n-form-item label="虚拟机名称">
            <n-input v-model:value="editForm.name" />
          </n-form-item>
          <n-form-item label="CPU 核数">
            <n-input-number v-model:value="editForm.cpuCount" :min="1" :max="128" style="width: 100%" />
          </n-form-item>
          <n-form-item label="启动内存（MB）">
            <n-input-number v-model:value="editForm.startupMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item label="动态内存">
            <n-switch v-model:value="editForm.dynamic" />
          </n-form-item>
          <n-form-item v-if="editForm.dynamic" label="动态内存最小值（MB）">
            <n-input-number v-model:value="editForm.minimumMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item v-if="editForm.dynamic" label="动态内存最大值（MB）">
            <n-input-number v-model:value="editForm.maximumMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item>
            <template #label>
              网络交换机
              <HelpTip>
                <p><strong>保持不变</strong>：不修改当前网络连接。</p>
                <p><strong>外部（桥接）</strong>：连接物理网卡，可访问局域网/互联网。</p>
                <p><strong>内部</strong>：仅虚拟机与主机互通；<strong>专用</strong>：仅虚拟机之间互通。</p>
                <p>修改交换机会影响该虚拟机的所有网络适配器。</p>
              </HelpTip>
            </template>
            <n-select v-model:value="editForm.switchName" :options="switchOptions" />
          </n-form-item>
          <n-form-item label="带宽上限（Mbps，0 = 不限）">
            <n-input-number v-model:value="editForm.bandwidthMbps" :min="0" :max="100000" style="width: 100%" />
          </n-form-item>
          <n-form-item>
            <template #label>
              启用检查点
              <HelpTip>
                <p>检查点可保存虚拟机某一时刻的整机状态，便于测试后回滚。</p>
                <p>长期使用会累积差异磁盘，占用空间并可能影响性能；生产虚拟机通常建议关闭。</p>
              </HelpTip>
            </template>
            <n-switch v-model:value="editForm.checkpointsEnabled" />
          </n-form-item>
        </div>
      </n-form>
      <div class="form-hint">
        编辑任务会进入队列执行。交换机选择"保持不变"时不会修改现有网络连接。
      </div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showEdit = false">取消</n-button>
        <n-button size="small" type="primary" @click="saveEdit">提交任务</n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal v-model:show="showDelete" preset="card" title="删除虚拟机" style="width: 520px">
    <div v-if="deleteTarget">
      <n-alert type="error" :show-icon="true" style="margin-bottom: 14px">
        删除操作不可撤销。虚拟机正在运行时会先强制关机。
      </n-alert>
      <div class="delete-target-name">{{ deleteTarget.name }}</div>
      <div class="cell-muted" style="margin-bottom: 12px">
        虚拟机目录：<span class="mono-text">{{ deleteTarget.path || "未知" }}</span>
      </div>
      <n-form label-placement="top" size="small">
        <n-form-item :label="`请输入虚拟机名称以确认：${deleteTarget.name}`">
          <n-input v-model:value="deleteConfirmName" placeholder="输入名称" />
        </n-form-item>
        <n-checkbox v-model:checked="deleteFiles">
          同时删除虚拟机文件目录（不可恢复）
        </n-checkbox>
      </n-form>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showDelete = false">取消</n-button>
        <n-button size="small" type="error" :disabled="!deleteConfirmValid" @click="confirmDelete">
          确认删除
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.table-panel {
  padding: 6px 8px;
}

.list-error {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.drawer-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 16px;
  font-weight: 600;
}

.detail-subtitle {
  font-size: 12px;
  color: #cfd6e4;
  margin: 14px 0 8px;
}

.edit-form :deep(.n-form-item) {
  margin-bottom: 4px;
}

.delete-target-name {
  font-size: 16px;
  font-weight: 600;
  color: #f0f3f9;
  margin-bottom: 4px;
}
</style>
