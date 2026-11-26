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
import ConnectHelpModal from "../components/ConnectHelpModal.vue";
import { HyperVService, errorMessage } from "../api";
import type { EditRequest, HostItem, VM, VirtualSwitch } from "../api";
import { useAppStore } from "../stores/app";
import { useHostsStore } from "../stores/hosts";
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
const hosts = useHostsStore();

const search = ref("");
const stateFilter = ref<string | null>(null);
const autoRefresh = ref(true);
const switches = ref<VirtualSwitch[]>([]);

const hostSelectValue = ref("");
const showHostConnect = ref(false);
const hostConnectTarget = ref<HostItem | null>(null);
const hostPassword = ref("");
const hostRemember = ref(true);
const hostConnecting = ref(false);
const hostConnectError = ref("");
const showHostHelp = ref(false);

const hostOptions = computed(() =>
  hosts.items.map((item) => ({
    label: `${item.name}${item.isLocal ? "（本机）" : "（" + item.address + "）"}${
      item.connected || item.isLocal ? "" : " · 未连接"
    }`,
    value: item.id,
  }))
);

const activeIsRemote = computed(() => !!app.activeHost && !app.activeHost.isLocal);

watch(
  () => app.activeHost?.id,
  (id) => {
    if (id) hostSelectValue.value = id;
  },
  { immediate: true }
);

async function afterHostSwitch() {
  await app.init();
  await fetchSwitches();
  await vmStore.fetch();
  void taskStore.fetch();
}

async function onHostChange(id: string) {
  const host = hosts.items.find((item) => item.id === id);
  if (!host || host.active) return;
  hostConnectError.value = "";
  if (host.isLocal) {
    try {
      await hosts.connect(host.id, "");
      await afterHostSwitch();
    } catch (err) {
      message.error(errorMessage(err));
      hostSelectValue.value = app.activeHost?.id ?? "";
    }
    return;
  }
  if (host.connected) {
    try {
      await hosts.setActive(host.id);
      await afterHostSwitch();
    } catch (err) {
      message.error(errorMessage(err));
      hostSelectValue.value = app.activeHost?.id ?? "";
    }
    return;
  }
  if (host.passwordSaved) {
    hostConnecting.value = true;
    try {
      await hosts.connect(host.id, "", true);
      await afterHostSwitch();
    } catch (err) {
      hostConnectTarget.value = host;
      hostConnectError.value = errorMessage(err);
      showHostHelp.value = true;
      hostSelectValue.value = app.activeHost?.id ?? "";
    } finally {
      hostConnecting.value = false;
    }
    return;
  }
  hostConnectTarget.value = host;
  hostPassword.value = "";
  hostRemember.value = true;
  showHostConnect.value = true;
}

function cancelHostConnect() {
  showHostConnect.value = false;
  hostSelectValue.value = app.activeHost?.id ?? "";
}

async function doHostConnect() {
  const host = hostConnectTarget.value;
  if (!host) return;
  if (host.username && !hostPassword.value) {
    message.warning("请输入远程计算机密码");
    return;
  }
  hostConnecting.value = true;
  hostConnectError.value = "";
  try {
    await hosts.connect(host.id, hostPassword.value, hostRemember.value);
    showHostConnect.value = false;
    await afterHostSwitch();
  } catch (err) {
    hostConnectError.value = errorMessage(err);
    showHostHelp.value = true;
    hostSelectValue.value = app.activeHost?.id ?? "";
  } finally {
    hostConnecting.value = false;
  }
}

// ---------- VMConnect 凭据 ----------

const showConsole = ref(false);
const consoleTarget = ref<VM | null>(null);
const consoleUser = ref("");
const consolePassword = ref("");
const consoleSaveCred = ref(true);
const consoleLoading = ref(false);

function defaultConsoleUser(): string {
  const host = app.activeHost;
  if (!host) return "";
  if (host.username && !host.username.includes("@")) return host.username;
  if (app.hostInfo?.computerName && app.hostInfo?.userName) {
    return `${app.hostInfo.computerName}\\${app.hostInfo.userName}`;
  }
  return host.username || "";
}

async function openConsole(vm: VM) {
  const host = app.activeHost;
  if (!host || host.isLocal) {
    try {
      await HyperVService.ConnectVM(vm.name);
    } catch (err) {
      message.error(errorMessage(err));
    }
    return;
  }
  consoleTarget.value = vm;
  consoleUser.value = defaultConsoleUser();
  consolePassword.value = hosts.passwords[host.id] ?? "";
  // Windows 凭据管理器里已经有这台主机的密码时，直接打开即可，
  // 不需要再要求输入密码；需要更新时用户可以勾选保存并输入新密码。
  consoleSaveCred.value = !host.passwordSaved && !hosts.passwords[host.id];
  showConsole.value = true;
}

async function submitConsole() {
  const host = app.activeHost;
  const vm = consoleTarget.value;
  if (!host || !vm) return;
  if (!consoleUser.value.trim()) {
    message.warning("请输入远程主机用户名");
    return;
  }
  if (consoleSaveCred.value && !consolePassword.value) {
    message.warning("保存凭据需要填写密码；也可以取消勾选后直接打开控制台");
    return;
  }
  consoleLoading.value = true;
  try {
    if (consoleSaveCred.value) {
      await HyperVService.SaveHostCredential(
        host.address,
        consoleUser.value.trim(),
        consolePassword.value
      );
      hosts.passwords[host.id] = consolePassword.value;
    }
    await HyperVService.ConnectVM(vm.name);
    showConsole.value = false;
    message.success("已打开 VMConnect 窗口");
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    consoleLoading.value = false;
  }
}

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
      await openConsole(vm);
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
  void hosts.fetch();
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
    <span class="cell-muted host-switch-label">计算机</span>
    <n-select
      :value="hostSelectValue"
      :options="hostOptions"
      :loading="hosts.loading"
      style="width: 240px"
      @update:value="onHostChange"
    />
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
          <n-button size="small" @click="openConsole(selectedVM)">连接控制台</n-button>
          <n-button size="small" type="primary" @click="openEdit(selectedVM)">编辑配置</n-button>
          <n-button size="small" type="error" ghost @click="openDelete(selectedVM)">删除</n-button>
        </n-space>
      </template>
    </n-drawer-content>
  </n-drawer>

  <n-modal
    v-model:show="showHostConnect"
    preset="card"
    :title="`连接 ${hostConnectTarget?.name || ''}`"
    style="width: 470px"
  >
    <div v-if="hostConnectTarget">
      <div class="connect-address mono-text">
        {{ hostConnectTarget.address }}:{{ hostConnectTarget.port }}
      </div>
      <n-form label-placement="top" size="small" style="margin-top: 12px">
        <n-form-item label="Windows 密码">
          <n-input
            v-model:value="hostPassword"
            type="password"
            show-password-on="click"
            :placeholder="hostConnectTarget.username ? '输入远程主机账户密码' : '留空使用当前 Windows 凭据'"
            @keyup.enter="doHostConnect"
          />
        </n-form-item>
        <n-checkbox v-model:checked="hostRemember">
          记住密码（保存到 Windows 凭据管理器，下次直接连接）
        </n-checkbox>
      </n-form>
      <n-alert v-if="hostConnectError" type="error" :show-icon="true" style="margin-top: 12px">
        <div class="connect-error">{{ hostConnectError }}</div>
        <n-button
          size="tiny"
          type="warning"
          ghost
          style="margin-top: 8px"
          @click="showHostHelp = true"
        >
          查看解决方案 / 一键修复
        </n-button>
      </n-alert>
      <n-alert v-else type="info" :show-icon="true" style="margin-top: 12px">
        密码加密保存在 Windows 凭据管理器，VMConnect 与 Hyper-V 管理器也能复用；
        下次可以直接连接，不需要再次输入。
      </n-alert>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="cancelHostConnect">取消</n-button>
        <n-button size="small" type="primary" :loading="hostConnecting" @click="doHostConnect">
          连接
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal v-model:show="showConsole" preset="card" title="打开虚拟机控制台" style="width: 560px">
    <n-alert type="info" :show-icon="true" style="margin-bottom: 12px">
      VMConnect 使用 Windows 身份验证。若远程主机使用 Microsoft 账户，请改用
      「计算机名\本地用户名」格式（例如 DESKTOP-1649RDS\83844），否则会出现
      0x8009030e「指定的登录会话不存在」错误。
    </n-alert>
    <n-form label-placement="top" size="small">
      <n-form-item label="远程主机用户名">
        <n-input v-model:value="consoleUser" placeholder="计算机名\用户名" />
      </n-form-item>
      <n-form-item label="密码">
        <n-input
          v-model:value="consolePassword"
          type="password"
          show-password-on="click"
          placeholder="远程主机账户密码"
        />
      </n-form-item>
      <n-checkbox v-model:checked="consoleSaveCred">
        保存到 Windows 凭据管理器（推荐，修复 VMConnect 认证失败）
      </n-checkbox>
    </n-form>
    <div class="form-hint" style="margin-top: 8px">
      保存后会写入当前 Windows 用户的凭据管理器，仅用于连接这台远程主机；
      取消勾选则只打开本次控制台。
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showConsole = false">取消</n-button>
        <n-button size="small" type="primary" :loading="consoleLoading" @click="submitConsole">
          打开控制台
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <ConnectHelpModal
    v-model:show="showHostHelp"
    :host="hostConnectTarget"
    :error="hostConnectError"
  />

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
  color: var(--text-3);
  margin: 14px 0 8px;
}

.edit-form :deep(.n-form-item) {
  margin-bottom: 4px;
}

.delete-target-name {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-strong);
  margin-bottom: 4px;
}

.host-switch-label {
  flex: 0 0 auto;
}

.connect-address {
  font-size: 12px;
  color: var(--text-3);
  background: var(--input-bg);
  border: 1px solid var(--border-soft);
  border-radius: 7px;
  padding: 8px 10px;
  word-break: break-all;
}

.connect-error {
  font-size: 11.5px;
  line-height: 1.7;
  max-height: 150px;
  overflow-y: auto;
  word-break: break-word;
}
</style>
