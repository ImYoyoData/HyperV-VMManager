<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  NButton,
  NDropdown,
  NIcon,
  NSpace,
  useDialog,
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
import VmPortForwardPanel from "../components/VmPortForwardPanel.vue";
import ConnectHelpModal from "../components/ConnectHelpModal.vue";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type {
  EditRequest,
  HostItem,
  NATNetwork,
  NATStaticMapping,
  VM,
  VirtualSwitch,
} from "../api";
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
const dialog = useDialog();

const app = useAppStore();
const vmStore = useVMStore();
const taskStore = useTaskStore();
const hosts = useHostsStore();

const search = ref("");
const stateFilter = ref<string | null>(null);
const autoRefresh = ref(true);
const switches = ref<VirtualSwitch[]>([]);
const natNetworks = ref<NATNetwork[]>([]);

const hostSelectValue = ref("");
const showHostConnect = ref(false);
const hostConnectTarget = ref<HostItem | null>(null);
const hostPassword = ref("");
const hostRemember = ref(true);
const hostConnecting = ref(false);
const hostConnectError = ref("");
const showHostHelp = ref(false);
const hostSwitching = ref(false);

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
  // 不同主机的虚拟机可能同名，先清空旧主机的列表与详情缓存。
  vmStore.clear();
  // 主机信息、交换机、NAT 与虚拟机列表互不依赖，并行加载可以明显缩短切换等待。
  await Promise.all([
    app.init(),
    fetchSwitches(),
    loadNAT(),
    // 页面已有整页切换遮罩，这里用静默刷新避免表格再叠一层 loading。
    vmStore.fetch({ silent: true, force: true }),
  ]);
  void taskStore.fetch();
}

async function withHostSwitching<T>(action: () => Promise<T>): Promise<T> {
  hostSwitching.value = true;
  try {
    return await action();
  } finally {
    hostSwitching.value = false;
  }
}

async function onHostChange(id: string) {
  const host = hosts.items.find((item) => item.id === id);
  if (!host || host.active) return;
  hostConnectError.value = "";
  if (host.isLocal) {
    try {
      await withHostSwitching(async () => {
        await hosts.connect(host.id, "");
        await afterHostSwitch();
      });
    } catch (err) {
      message.error(errorMessage(err));
      hostSelectValue.value = app.activeHost?.id ?? "";
    }
    return;
  }
  if (host.connected) {
    try {
      await withHostSwitching(async () => {
        await hosts.setActive(host.id);
        await afterHostSwitch();
      });
    } catch (err) {
      message.error(errorMessage(err));
      hostSelectValue.value = app.activeHost?.id ?? "";
    }
    return;
  }
  if (host.passwordSaved) {
    hostConnecting.value = true;
    try {
      await withHostSwitching(async () => {
        await hosts.connect(host.id, "", true);
        await afterHostSwitch();
      });
    } catch (err) {
      // 保存的凭据可能已经失效，打开密码框让用户重新输入。
      hostConnectTarget.value = host;
      hostPassword.value = "";
      hostRemember.value = true;
      hostConnectError.value = errorMessage(err);
      showHostConnect.value = true;
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
    await withHostSwitching(async () => {
      await hosts.connect(host.id, hostPassword.value, hostRemember.value);
      showHostConnect.value = false;
      await afterHostSwitch();
    });
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
const consoleUsersRefreshing = ref(false);
const settingPassword = ref(false);

const consoleUsers = computed(() =>
  (app.hostInfo?.localUsers ?? []).filter((user) => user.enabled)
);

const consoleComputerName = computed(() => app.hostInfo?.computerName ?? "");

const consoleUserOptions = computed(() =>
  consoleUsers.value.map((user) => ({
    label: user.fullName ? `${user.name}（${user.fullName}）` : user.name,
    value: consoleComputerName.value
      ? `${consoleComputerName.value}\\${user.name}`
      : user.name,
  }))
);

const selectedConsoleUser = computed(() => {
  const value = consoleUser.value.trim();
  const name = value.includes("\\") ? value.split("\\").pop() ?? "" : value;
  return (
    consoleUsers.value.find(
      (user) => user.name.toLowerCase() === name.toLowerCase()
    ) ?? null
  );
});

const consoleUserHasNoPassword = computed(
  () =>
    !!selectedConsoleUser.value &&
    !selectedConsoleUser.value.passwordRequired &&
    !consolePassword.value &&
    (consoleSaveCred.value || !app.activeHost?.passwordSaved)
);

const consoleUserIsLocal = computed(() => {
  const source = (selectedConsoleUser.value?.principalSource ?? "").toLowerCase();
  return source === "" || source === "local";
});

function defaultConsoleUser(): string {
  const host = app.activeHost;
  if (!host) return "";
  const configured = (host.username ?? "").trim();
  if (configured && !configured.includes("@")) {
    if (!configured.includes("\\") && consoleComputerName.value) {
      return `${consoleComputerName.value}\\${configured}`;
    }
    return configured;
  }

  const computer = consoleComputerName.value;
  const sessionUser = app.hostInfo?.userName ?? "";
  // 只有会话账户确实存在于远程本地账户列表时才使用，避免把本机账户名
  // 或 WinRM 连接账户误当成远程控制台账户。
  if (
    computer &&
    sessionUser &&
    consoleUsers.value.some(
      (user) => user.name.toLowerCase() === sessionUser.toLowerCase()
    )
  ) {
    return `${computer}\\${sessionUser}`;
  }
  if (computer && consoleUsers.value.length === 1) {
    return `${computer}\\${consoleUsers.value[0].name}`;
  }
  return configured;
}

function normalizeConsoleUser(): string {
  let value = consoleUser.value.trim();
  if (!value) return "";
  if (
    !value.includes("\\") &&
    !value.includes("@") &&
    consoleComputerName.value
  ) {
    value = `${consoleComputerName.value}\\${value}`;
  }
  return value;
}

async function refreshConsoleUsers() {
  consoleUsersRefreshing.value = true;
  try {
    await app.refreshHostInfo();
    if (!consoleUser.value.trim()) {
      consoleUser.value = defaultConsoleUser();
    }
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    consoleUsersRefreshing.value = false;
  }
}

async function setRemotePassword() {
  const host = app.activeHost;
  if (!host || host.isLocal) return;
  if (!consolePassword.value) {
    message.warning("请先在密码框输入新密码");
    return;
  }
  const username = normalizeConsoleUser();
  if (!username) {
    message.warning("请先选择或输入远程主机账户");
    return;
  }
  if (!consoleUserIsLocal.value) {
    message.warning(
      "这不是远程主机上的本地账户（可能是 Microsoft 账户），请使用该账户原本的密码，不要在这里修改"
    );
    return;
  }
  const bare = username.includes("\\") ? username.split("\\").pop() ?? "" : username;
  if (!bare) {
    message.warning("无法识别远程本地账户名");
    return;
  }
  const configured = (host.username ?? "").trim();
  const configuredUser = configured.includes("\\")
    ? configured.split("\\").pop() ?? ""
    : configured;

  dialog.warning({
    title: "设置远程本地账户密码",
    content: `将把远程主机本地账户「${bare}」的密码修改为密码框中的内容。如果应用当前就是用这个账户连接远程主机，修改后会自动尝试用新密码重连。确定继续吗？`,
    positiveText: "设置密码",
    negativeText: "取消",
    onPositiveClick: async () => {
      settingPassword.value = true;
      try {
        await HyperVService.SetLocalUserPassword({
          userName: bare,
          password: consolePassword.value,
        });
        message.success("远程本地账户密码已更新");
        if (configuredUser && configuredUser.toLowerCase() === bare.toLowerCase()) {
          try {
            await hosts.connect(host.id, consolePassword.value, true);
          } catch (err) {
            message.warning(
              `密码已修改，但自动重连失败：${errorMessage(err)}。请回到“计算机”页面用新密码重新连接`
            );
          }
        }
        await app.refreshHostInfo();
      } catch (err) {
        message.error(errorMessage(err));
      } finally {
        settingPassword.value = false;
      }
    },
  });
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
  showConsole.value = true;
  const initialUser = defaultConsoleUser();
  consoleUser.value = initialUser;
  consolePassword.value = hosts.passwords[host.id] ?? "";
  // Windows 凭据管理器里已经有这台主机的密码时，直接打开即可，
  // 不需要再要求输入密码；需要更新时用户可以勾选保存并输入新密码。
  consoleSaveCred.value = !host.passwordSaved && !hosts.passwords[host.id];

  // 切换主机后 app.hostInfo 可能是本机缓存，打开控制台时强制刷新一次，
  // 否则会把本机账户误当成远程主机账户。
  consoleUsersRefreshing.value = true;
  try {
    await app.refreshHostInfo();
    if (consoleUser.value === initialUser) {
      consoleUser.value = defaultConsoleUser();
    }
  } catch {
    // 读取失败时保留当前缓存，用户可以手动输入或再点“刷新账户”。
  } finally {
    consoleUsersRefreshing.value = false;
  }
}

async function submitConsole() {
  const host = app.activeHost;
  const vm = consoleTarget.value;
  if (!host || !vm) return;
  const username = normalizeConsoleUser();
  if (!username) {
    message.warning("请输入远程主机用户名");
    return;
  }
  if (consoleSaveCred.value && !consolePassword.value) {
    message.warning("保存凭据需要填写非空密码");
    return;
  }
  if (!consoleSaveCred.value && !consolePassword.value && !app.activeHost?.passwordSaved) {
    message.warning("远程 VMConnect 需要远程主机账户密码；请填写密码后再打开");
    return;
  }
  consoleLoading.value = true;
  try {
    if (consoleSaveCred.value) {
      await HyperVService.SaveHostCredential(
        host.address,
        username,
        consolePassword.value
      );
      hosts.passwords[host.id] = consolePassword.value;
    }
    consoleUser.value = username;
    await HyperVService.ConnectVMWithCredential(
      vm.name,
      username,
      consolePassword.value
    );
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

// 虚拟机较多时只渲染当前页，降低表格布局与 DOM 开销；用户可切换每页数量。
const tablePagination = {
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [10, 20, 30, 50, 100],
};

// 队列中正在执行或等待执行的目标名称，用于在列表里标记“任务中”。
const pendingNames = computed(() => {
  const names = new Set<string>();
  for (const task of taskStore.items) {
    if (task.status === "queued" || task.status === "running") {
      names.add(task.target);
    }
  }
  return names;
});

const showDetail = ref(false);
const selectedName = ref("");
const selectedVM = computed(
  () => vmStore.items.find((vm) => vm.name === selectedName.value) ?? null
);

watch(selectedVM, (vm) => {
  if (!vm && showDetail.value) {
    showDetail.value = false;
  }
});

const showEdit = ref(false);
const showDelete = ref(false);
const editSource = ref<VM | null>(null);
const editLoading = ref(false);
const savingEdit = ref(false);
const editBaseline = ref("");
let editRequestID = 0;
const deleteTarget = ref<VM | null>(null);
const deleteConfirmName = ref("");
const deleteFiles = ref(false);

interface NetworkAdapterForm {
  id: string;
  name: string;
  switchName: string;
  dynamicMacAddress: boolean;
  staticMacAddress: string;
  macAddressSpoofing: string;
  dhcpGuard: string;
  routerGuard: string;
  vlanMode: string;
  vlanID: number;
  deviceNaming: string;
  vmqWeight: number;
  iovWeight: number;
  ipsecOffload: boolean;
  stormLimit: number;
  bandwidthMbps: number;
  status: string;
  ipAddresses: string[];
}

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
  nestedVirtualization: false,
  notes: "",
  automaticStartAction: "Nothing",
  automaticStartDelay: 0,
  automaticStopAction: "ShutDown",
  automaticCheckpoints: true,
  processorReserve: 0,
  processorMaximum: 100,
  processorWeight: 100,
  processorMigration: false,
  hwThreadsPerCore: 0,
  memoryBuffer: 20,
  memoryPriority: 50,
  secureBoot: false,
  secureBootTemplate: "MicrosoftWindows",
  networkBootProtocol: "IPv4",
  tpmEnabled: false,
  encryptTraffic: false,
  vbsOptOut: false,
  networkAdapters: [] as NetworkAdapterForm[],
  integrationServices: [] as { name: string; displayName: string; enabled: boolean }[],
});
const editDiskSizes = ref<number[]>([]);
const editOriginalAdapters = ref<NetworkAdapterForm[]>([]);
const editOriginal = ref<{
  tpmEnabled: boolean;
  secureBoot: boolean;
  secureBootTemplate: string;
  networkBootProtocol: string;
  encryptTraffic: boolean;
  vbsOptOut: boolean;
} | null>(null);

interface DiskChange {
  disk: DiskInfo;
  sizeGB: number;
  sizeBytes: number;
  shrink: boolean;
}

type DiskInfo = NonNullable<VM["hardDrives"]>[number];

const switchOptions = computed(() => [
  { label: "保持不变", value: "" },
  ...switches.value.map((item) => ({
    label: `${item.name}（${item.switchType === "External" ? "外部" : item.switchType === "Internal" ? "内部" : "专用"}）`,
    value: item.name,
  })),
]);

const onOffOptions = [
  { label: "关闭", value: "Off" },
  { label: "开启", value: "On" },
];

const automaticStartOptions = [
  { label: "不自动启动", value: "Nothing" },
  { label: "上次运行则启动", value: "StartIfRunning" },
  { label: "总是自动启动", value: "Start" },
];

const automaticStopOptions = [
  { label: "直接关闭", value: "TurnOff" },
  { label: "保存状态", value: "Save" },
  { label: "正常关机", value: "ShutDown" },
];

const secureBootTemplateOptions = [
  { label: "Microsoft Windows", value: "MicrosoftWindows" },
  { label: "Microsoft UEFI 证书颁发机构", value: "MicrosoftUEFICertificateAuthority" },
  { label: "开源受防护 VM", value: "OpenSourceShieldedVM" },
];

const networkBootProtocolOptions = [
  { label: "IPv4", value: "IPv4" },
  { label: "IPv6", value: "IPv6" },
  { label: "无", value: "None" },
];

const vlanModeOptions = [
  { label: "未标记（Untagged）", value: "Untagged" },
  { label: "Access 单 VLAN", value: "Access" },
];

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
    fixed: "left",
    render: (row) =>
      h("div", null, [
        h("div", { class: "vm-name" }, [
          row.name,
          pendingNames.value.has(row.name)
            ? h("span", { class: "vm-pending" }, "任务中")
            : null,
        ]),
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
    title: "端口转发",
    key: "nat",
    width: 160,
    render: (row) => {
      if (isWebMode) {
        return h("span", { class: "cell-muted" }, "—");
      }
      const rules = vmForwardRules(row);
      if (rules.length === 0) {
        return h("span", { class: "cell-muted" }, "未配置");
      }
      const lines = rules.slice(0, 2).map((item) =>
        h(
          "div",
          { class: "vm-sub" },
          `宿主 ${item.rule.externalPort} → ${item.rule.internalPort}`
        )
      );
      if (rules.length > 2) {
        lines.push(h("div", { class: "vm-sub" }, `还有 ${rules.length - 2} 条`));
      }
      return h("div", null, lines);
    },
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
    fixed: "right",
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

async function openDetail(vm: VM) {
  selectedName.value = vm.name;
  showDetail.value = true;
  const detail = await vmStore.fetchDetail(vm.name);
  if (!detail && vmStore.error) {
    message.error(`读取虚拟机详情失败：${vmStore.error}`);
  }
}

function formatGB(bytes: number): string {
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
}

function formatGBOrDash(bytes: number): string {
  return bytes > 0 ? formatGB(bytes) : "—";
}

function minSafeBytes(disk: DiskInfo): number {
  return Math.max(disk.minimumSizeBytes || 0, disk.fileSizeBytes || 0);
}

function suggestedDiskGB(disk: DiskInfo): number {
  const current = Math.ceil((disk.virtualSizeBytes || 0) / 1024 ** 3);
  const safe = Math.ceil(minSafeBytes(disk) / 1024 ** 3);
  return Math.max(current, safe + 10, Math.ceil(current * 1.25));
}

function diskRowWarning(disk: DiskInfo, index: number): string {
  const sizeGB = Number(editDiskSizes.value[index] || 0);
  if (sizeGB < 1) return "容量至少 1 GB";
  const newBytes = sizeGB * 1024 ** 3;
  const safe = minSafeBytes(disk);
  if (newBytes < safe) {
    return `低于安全下限 ${formatGB(safe)}，缩小可能丢失数据`;
  }
  if (disk.vhdType === "Differencing" && newBytes !== disk.virtualSizeBytes) {
    return "差异磁盘不能单独调整容量";
  }
  if (disk.vhdType === "Fixed" && newBytes < (disk.virtualSizeBytes || 0)) {
    return "固定大小硬盘不支持缩小";
  }
  if (newBytes < (disk.virtualSizeBytes || 0)) {
    return "缩小操作可能导致来宾分区数据丢失";
  }
  return "";
}

async function openEdit(vm: VM) {
  // 先用列表摘要撑起弹窗，再异步补全磁盘、安全与网络详情，避免“点了没反应”。
  const requestID = ++editRequestID;
  editSource.value = vm;
  editLoading.value = true;
  showEdit.value = true;
  try {
    const detail = await vmStore.fetchDetail(vm.name);
    if (requestID !== editRequestID) return;
    if (detail) {
      editSource.value = detail;
    } else if (vmStore.error) {
      message.warning(`读取完整配置失败，已使用列表摘要：${vmStore.error}`);
    }
    fillEditForm(editSource.value);
  } finally {
    if (requestID === editRequestID) {
      editLoading.value = false;
    }
  }
}

function fillEditForm(source: VM) {
  editForm.name = source.name;
  editForm.startupMB = source.memoryStartupMB;
  editForm.dynamic = source.dynamicMemoryEnabled;
  editForm.minimumMB = source.memoryMinimumMB || 1024;
  editForm.maximumMB = source.memoryMaximumMB || Math.max(source.memoryStartupMB, 8192);
  editForm.cpuCount = source.processorCount;
  editForm.switchName = "";
  editForm.bandwidthMbps = 100;
  editForm.checkpointsEnabled = source.checkpointType !== "Disabled";
  editForm.nestedVirtualization = source.nestedVirtualization;
  editForm.notes = source.notes ?? "";
  editForm.automaticStartAction = source.automaticStartAction || "Nothing";
  editForm.automaticStartDelay = source.automaticStartDelay ?? 0;
  editForm.automaticStopAction = source.automaticStopAction || "ShutDown";
  editForm.automaticCheckpoints = source.automaticCheckpoints ?? true;
  editForm.processorReserve = source.processorReserve ?? 0;
  editForm.processorMaximum = source.processorMaximum ?? 100;
  editForm.processorWeight = source.processorWeight ?? 100;
  editForm.processorMigration = source.processorMigration ?? false;
  editForm.hwThreadsPerCore = source.hwThreadsPerCore ?? 0;
  editForm.memoryBuffer = source.memoryBuffer ?? 0;
  editForm.memoryPriority = source.memoryPriority ?? 50;
  editForm.secureBoot = source.secureBoot === "On";
  editForm.secureBootTemplate = source.secureBootTemplate || "MicrosoftWindows";
  editForm.networkBootProtocol = source.networkBootProtocol || "IPv4";
  editForm.tpmEnabled = source.tpmEnabled ?? false;
  editForm.encryptTraffic = source.encryptTraffic ?? false;
  editForm.vbsOptOut = source.vbsOptOut ?? false;
  editForm.networkAdapters = (source.networks ?? []).map((adapter) => ({
    id: adapter.id || adapter.name,
    name: adapter.name,
    switchName: adapter.switchName || "",
    dynamicMacAddress: adapter.dynamicMacAddress,
    staticMacAddress: adapter.macAddress || "",
    macAddressSpoofing: adapter.macAddressSpoofing || "Off",
    dhcpGuard: adapter.dhcpGuard || "Off",
    routerGuard: adapter.routerGuard || "Off",
    vlanMode: adapter.vlanMode === "Access" ? "Access" : "Untagged",
    vlanID: adapter.vlanID || 1,
    deviceNaming: adapter.deviceNaming || "Off",
    vmqWeight: adapter.vmqWeight ?? 0,
    iovWeight: adapter.iovWeight ?? 0,
    ipsecOffload: adapter.ipsecOffload ?? false,
    stormLimit: adapter.stormLimit ?? 0,
    bandwidthMbps: adapter.maximumBandwidthBps
      ? Math.round(adapter.maximumBandwidthBps / 1_000_000)
      : 0,
    status: adapter.status,
    ipAddresses: adapter.ipAddresses ?? [],
  }));
  editForm.integrationServices = (source.integrationServices ?? []).map((service) => ({
    name: service.name,
    displayName: service.displayName,
    enabled: service.enabled,
  }));
  editOriginalAdapters.value = editForm.networkAdapters.map((adapter) => ({
    ...adapter,
  }));
  editOriginal.value = {
    tpmEnabled: editForm.tpmEnabled,
    secureBoot: editForm.secureBoot,
    secureBootTemplate: editForm.secureBootTemplate,
    networkBootProtocol: editForm.networkBootProtocol,
    encryptTraffic: editForm.encryptTraffic,
    vbsOptOut: editForm.vbsOptOut,
  };
  editDiskSizes.value = (source.hardDrives ?? []).map((disk) =>
    Math.max(1, Math.round((disk.virtualSizeBytes || 0) / 1024 ** 3))
  );
  editBaseline.value = editFormSignature();
}

function editFormSignature(): string {
  return JSON.stringify({
    form: editForm,
    disks: editDiskSizes.value,
    security: editOriginal.value,
  });
}

function isEditDirty(): boolean {
  return !!editBaseline.value && editBaseline.value !== editFormSignature();
}

function closeEdit(force = false) {
  if (savingEdit.value) return;
  if (!force && !editLoading.value && isEditDirty()) {
    dialog.warning({
      title: "放弃未保存的修改？",
      content: "当前编辑的配置还没有提交，关闭后这些修改会丢失。",
      positiveText: "放弃修改",
      negativeText: "继续编辑",
      onPositiveClick: () => {
        showEdit.value = false;
      },
    });
    return;
  }
  showEdit.value = false;
}

function onEditShowChange(value: boolean) {
  if (value) {
    showEdit.value = true;
    return;
  }
  closeEdit();
}

function resetEditForm() {
  if (editSource.value) {
    fillEditForm(editSource.value);
  }
}

function editRunningWarning(): string {
  const state = editSource.value?.state ?? "";
  if (state === "Off" || !state) return "";
  return "虚拟机正在运行：CPU 核数、嵌套虚拟化、TPM、安全启动/固件设置以及加密选项需要关机后才能修改，已暂时禁用；内存、备注、自动启动、网络适配器和集成服务可以继续在线修改。";
}

async function saveEdit() {
  const vm = editSource.value;
  if (!vm || editLoading.value || savingEdit.value) return;

  const name = editForm.name.trim();
  if (!name) {
    message.warning("虚拟机名称不能为空");
    return;
  }
  if (name.length > 100 || /[\\/:*?"<>|]/.test(name)) {
    message.warning('名称不能包含 \\ / : * ? " < > | 这些字符，且不能超过 100 个字符');
    return;
  }
  const startupMB = Number(editForm.startupMB);
  const minimumMB = Number(editForm.minimumMB);
  const maximumMB = Number(editForm.maximumMB);
  if (!startupMB || startupMB < 256) {
    message.warning("启动内存不能小于 256 MB");
    return;
  }
  if (editForm.dynamic) {
    if (!minimumMB || minimumMB < 256 || !maximumMB || maximumMB < 256) {
      message.warning("动态内存最小值和最大值都不能小于 256 MB");
      return;
    }
    if (minimumMB > startupMB) {
      message.warning("动态内存最小值不能大于启动内存");
      return;
    }
    if (startupMB > maximumMB) {
      message.warning("启动内存不能大于动态内存最大值");
      return;
    }
  }
  const cpuCount = Number(editForm.cpuCount);
  if (!cpuCount || cpuCount < 1 || cpuCount > 128) {
    message.warning("CPU 核数必须在 1 到 128 之间");
    return;
  }

  const changes: DiskChange[] = (vm.hardDrives ?? []).flatMap((disk, index) => {
    const sizeGB = Number(editDiskSizes.value[index] || 0);
    if (!sizeGB || sizeGB < 1) return [];
    const sizeBytes = Math.round(sizeGB * 1024 ** 3);
    if (sizeBytes === disk.virtualSizeBytes) return [];
    return [{
      disk,
      sizeGB,
      sizeBytes,
      shrink: sizeBytes < (disk.virtualSizeBytes || 0),
    }];
  });

  const unsafe = changes.find((item) => item.sizeBytes < minSafeBytes(item.disk));
  if (unsafe) {
    message.error(
      `${unsafe.disk.path} 新容量 ${unsafe.sizeGB} GB 低于安全下限 ${formatGB(minSafeBytes(unsafe.disk))}，不允许缩小`
    );
    return;
  }

  const invalidType = changes.find((item) =>
    item.disk.vhdType === "Differencing"
      ? item.sizeBytes !== item.disk.virtualSizeBytes
      : item.disk.vhdType === "Fixed" && item.shrink
  );
  if (invalidType) {
    message.error(
      invalidType.disk.vhdType === "Differencing"
        ? "差异磁盘不能单独调整容量"
        : "固定大小硬盘不支持缩小"
    );
    return;
  }

  if (changes.some((item) => item.shrink)) {
    const detail = changes
      .filter((item) => item.shrink)
      .map(
        (item) =>
          `${item.disk.path}\n  ${formatGB(item.disk.virtualSizeBytes)} → ${item.sizeGB} GB（安全下限 ${formatGB(minSafeBytes(item.disk))}）`
      )
      .join("\n\n");
    dialog.warning({
      title: "缩小虚拟硬盘可能导致数据丢失",
      content: `${detail}\n\n缩小前请确认来宾系统内已经腾出足够空间。任务会先关闭虚拟机；Hyper-V 只调整虚拟容量，不会自动缩带来宾分区。`,
      positiveText: "确认缩小并提交",
      negativeText: "取消",
      onPositiveClick: () => submitEdit(vm, changes),
    });
    return;
  }

  await submitEdit(vm, changes);
}

async function submitEdit(vm: VM, changes: DiskChange[]) {
  const networkAdapters: NonNullable<EditRequest["networkAdapters"]> = [];
  editForm.networkAdapters.forEach((adapter, index) => {
    const original = editOriginalAdapters.value[index];
    if (!original) return;
    const update: NonNullable<EditRequest["networkAdapters"]>[number] = {
      id: adapter.id,
      name: adapter.name,
    };
    let changed = false;
    if (adapter.switchName && adapter.switchName !== original.switchName) {
      update.switchName = adapter.switchName;
      changed = true;
    }
    if (adapter.dynamicMacAddress !== original.dynamicMacAddress) {
      update.dynamicMacAddress = adapter.dynamicMacAddress;
      changed = true;
    }
    if (
      !adapter.dynamicMacAddress &&
      adapter.staticMacAddress &&
      adapter.staticMacAddress !== original.staticMacAddress
    ) {
      update.staticMacAddress = adapter.staticMacAddress;
      changed = true;
    }
    if (adapter.macAddressSpoofing !== original.macAddressSpoofing) {
      update.macAddressSpoofing = adapter.macAddressSpoofing;
      changed = true;
    }
    if (adapter.dhcpGuard !== original.dhcpGuard) {
      update.dhcpGuard = adapter.dhcpGuard;
      changed = true;
    }
    if (adapter.routerGuard !== original.routerGuard) {
      update.routerGuard = adapter.routerGuard;
      changed = true;
    }
    if (adapter.deviceNaming !== original.deviceNaming) {
      update.deviceNaming = adapter.deviceNaming;
      changed = true;
    }
    if (adapter.vmqWeight !== original.vmqWeight) {
      update.vmqWeight = Number(adapter.vmqWeight);
      changed = true;
    }
    if (adapter.iovWeight !== original.iovWeight) {
      update.iovWeight = Number(adapter.iovWeight);
      changed = true;
    }
    if (adapter.ipsecOffload !== original.ipsecOffload) {
      update.ipsecOffload = adapter.ipsecOffload;
      changed = true;
    }
    if (adapter.stormLimit !== original.stormLimit) {
      update.stormLimit = Number(adapter.stormLimit);
      changed = true;
    }
    if (adapter.bandwidthMbps !== original.bandwidthMbps) {
      update.bandwidthMbps = Number(adapter.bandwidthMbps);
      changed = true;
    }
    if (
      adapter.vlanMode !== original.vlanMode ||
      (adapter.vlanMode === "Access" && adapter.vlanID !== original.vlanID)
    ) {
      update.vlanMode = adapter.vlanMode;
      if (adapter.vlanMode === "Access") {
        update.vlanID = Number(adapter.vlanID);
      }
      changed = true;
    }
    if (changed) networkAdapters.push(update);
  });

  const security: NonNullable<EditRequest["security"]> = {};
  const originalSecurity = editOriginal.value;
  if (originalSecurity) {
    if (editForm.tpmEnabled !== originalSecurity.tpmEnabled) {
      security.tpmEnabled = editForm.tpmEnabled;
    }
    if (editForm.secureBoot !== originalSecurity.secureBoot) {
      security.secureBoot = editForm.secureBoot;
    }
    if (
      editForm.secureBoot &&
      editForm.secureBootTemplate !== originalSecurity.secureBootTemplate
    ) {
      security.secureBootTemplate = editForm.secureBootTemplate;
    }
    if (editForm.networkBootProtocol !== originalSecurity.networkBootProtocol) {
      security.preferredNetworkBootProtocol = editForm.networkBootProtocol;
    }
    if (editForm.encryptTraffic !== originalSecurity.encryptTraffic) {
      security.encryptStateAndVmMigrationTraffic = editForm.encryptTraffic;
    }
    if (editForm.vbsOptOut !== originalSecurity.vbsOptOut) {
      security.virtualizationBasedSecurityOptOut = editForm.vbsOptOut;
    }
  }
  const securityPayload =
    Object.keys(security).length > 0 ? security : undefined;

  const integrationServices = editForm.integrationServices
    .filter((service) => {
      const original = (vm.integrationServices ?? []).find(
        (item) => item.name === service.name
      );
      return !!original && original.enabled !== service.enabled;
    })
    .map((service) => ({ name: service.name, enabled: service.enabled }));

  const request: EditRequest = {};
  let changed = false;

  const name = editForm.name.trim();
  if (name && name !== vm.name) {
    request.newName = name;
    changed = true;
  }

  const sourceMinimum = vm.memoryMinimumMB || 1024;
  const sourceMaximum = vm.memoryMaximumMB || Math.max(vm.memoryStartupMB, 8192);
  if (
    Number(editForm.startupMB) !== vm.memoryStartupMB ||
    editForm.dynamic !== vm.dynamicMemoryEnabled ||
    (editForm.dynamic &&
      (Number(editForm.minimumMB) !== sourceMinimum ||
        Number(editForm.maximumMB) !== sourceMaximum))
  ) {
    request.memory = {
      startupMB: Number(editForm.startupMB),
      dynamic: editForm.dynamic,
      minimumMB: Number(editForm.minimumMB),
      maximumMB: Number(editForm.maximumMB),
    };
    changed = true;
  }

  if (Number(editForm.cpuCount) !== vm.processorCount) {
    request.cpuCount = Number(editForm.cpuCount);
    changed = true;
  }
  if (editForm.checkpointsEnabled !== (vm.checkpointType !== "Disabled")) {
    request.checkpointsEnabled = editForm.checkpointsEnabled;
    changed = true;
  }
  if (editForm.nestedVirtualization !== vm.nestedVirtualization) {
    request.nestedVirtualization = editForm.nestedVirtualization;
    changed = true;
  }
  if (editForm.notes !== (vm.notes ?? "")) {
    request.notes = editForm.notes;
    changed = true;
  }
  if (editForm.automaticStartAction !== (vm.automaticStartAction || "Nothing")) {
    request.automaticStartAction = editForm.automaticStartAction;
    changed = true;
  }
  if (Number(editForm.automaticStartDelay) !== (vm.automaticStartDelay ?? 0)) {
    request.automaticStartDelay = Number(editForm.automaticStartDelay);
    changed = true;
  }
  if (editForm.automaticStopAction !== (vm.automaticStopAction || "ShutDown")) {
    request.automaticStopAction = editForm.automaticStopAction;
    changed = true;
  }
  if (editForm.automaticCheckpoints !== (vm.automaticCheckpoints ?? true)) {
    request.automaticCheckpoints = editForm.automaticCheckpoints;
    changed = true;
  }
  if (Number(editForm.processorReserve) !== (vm.processorReserve ?? 0)) {
    request.processorReserve = Number(editForm.processorReserve);
    changed = true;
  }
  if (Number(editForm.processorMaximum) !== (vm.processorMaximum ?? 100)) {
    request.processorMaximum = Number(editForm.processorMaximum);
    changed = true;
  }
  if (Number(editForm.processorWeight) !== (vm.processorWeight ?? 100)) {
    request.processorWeight = Number(editForm.processorWeight);
    changed = true;
  }
  if (editForm.processorMigration !== (vm.processorMigration ?? false)) {
    request.processorMigration = editForm.processorMigration;
    changed = true;
  }
  if (Number(editForm.hwThreadsPerCore) !== (vm.hwThreadsPerCore ?? 0)) {
    request.hwThreadsPerCore = Number(editForm.hwThreadsPerCore);
    changed = true;
  }
  if (editForm.dynamic && Number(editForm.memoryBuffer) !== (vm.memoryBuffer ?? 0)) {
    request.memoryBuffer = Number(editForm.memoryBuffer);
    changed = true;
  }
  if (Number(editForm.memoryPriority) !== (vm.memoryPriority ?? 50)) {
    request.memoryPriority = Number(editForm.memoryPriority);
    changed = true;
  }
  if (networkAdapters.length > 0) {
    request.networkAdapters = networkAdapters;
    changed = true;
  }
  if (securityPayload) {
    request.security = securityPayload;
    changed = true;
  }
  if (integrationServices.length > 0) {
    request.integrationServices = integrationServices;
    changed = true;
  }
  if (changes.length > 0) {
    request.disks = changes.map((item) => ({
      path: item.disk.path,
      sizeBytes: item.sizeBytes,
      allowShrink: true,
    }));
    request.stopVMForDiskResize = true;
    changed = true;
  }

  if (!changed) {
    message.info("没有检测到配置变更");
    return;
  }

  savingEdit.value = true;
  try {
    const task = await HyperVService.SubmitEdit(vm.name, request);
    message.success(`已提交编辑任务${task ? `（${task.id}）` : ""}`);
    editBaseline.value = editFormSignature();
    showEdit.value = false;
    if (request.newName) {
      // 改名任务完成后旧名称会消失，先关闭详情抽屉避免停留在旧数据。
      showDetail.value = false;
    }
    void taskStore.fetch();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    savingEdit.value = false;
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

async function loadNAT() {
  if (isWebMode) return;
  try {
    natNetworks.value = (await HyperVService.ListNATNetworks()) ?? [];
  } catch {
    natNetworks.value = [];
  }
}

function vmForwardRules(
  vm: VM
): { nat: NATNetwork; rule: NATStaticMapping }[] {
  const seen = new Set<string>();
  const result: { nat: NATNetwork; rule: NATStaticMapping }[] = [];
  for (const adapter of vm.networks ?? []) {
    for (const ip of adapter.ipAddresses ?? []) {
      if (!ip || ip.includes(":")) continue;
      for (const item of forwardRulesByIP.value.get(ip) ?? []) {
        const key = `${item.nat.name}|${item.rule.protocol}|${item.rule.externalPort}`;
        if (seen.has(key)) continue;
        seen.add(key);
        result.push(item);
      }
    }
  }
  return result;
}

// 按虚拟机 IP 建立端口转发的索引，避免表格每一行都遍历全部 NAT 规则。
const forwardRulesByIP = computed(() => {
  const index = new Map<string, { nat: NATNetwork; rule: NATStaticMapping }[]>();
  for (const nat of natNetworks.value) {
    for (const rule of nat.staticMappings ?? []) {
      const ip = (rule.internalIPAddress || "").trim();
      if (!ip) continue;
      const list = index.get(ip);
      if (list) {
        list.push({ nat, rule });
      } else {
        index.set(ip, [{ nat, rule }]);
      }
    }
  }
  return index;
});

async function refresh() {
  // 虚拟机列表是主要等待项，交换机列表在后台更新即可。
  await vmStore.fetch({ force: true });
  void fetchSwitches();
}

let refreshTimer: ReturnType<typeof setInterval> | undefined;

function setupAutoRefresh() {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = undefined;
  if (!autoRefresh.value) return;
  const seconds = Math.max(app.settings?.autoRefreshSeconds ?? 10, 3);
  refreshTimer = setInterval(() => {
    if (!document.hidden && !hostSwitching.value && !hostConnecting.value) {
      // 自动刷新走轻量摘要接口，静默更新，不显示表格 loading 遮罩。
      void vmStore.fetch({ silent: true });
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
  void loadNAT();
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

  <div v-if="hostSwitching" class="page-loading-mask">
    <n-spin size="large" />
    <div class="page-loading-text">正在切换计算机，请稍候…</div>
  </div>

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
      :disabled="hostSwitching"
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
    <span v-if="vmStore.refreshing" class="cell-muted">后台更新中…</span>
    <n-switch v-model:value="autoRefresh" size="small" />
    <span class="cell-muted">自动刷新</span>
    <n-button size="small" :loading="vmStore.loading" :disabled="hostSwitching" @click="refresh">
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
      :scroll-x="1130"
      :pagination="tablePagination"
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
          <n-descriptions-item label="嵌套虚拟化">
            {{ selectedVM.nestedVirtualization ? "已开启" : "已关闭" }}
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

      <div v-if="!isWebMode" class="detail-section">
        <div class="detail-section-title">端口转发与内网穿透</div>
        <VmPortForwardPanel
          :vm="selectedVM"
          :nat-networks="natNetworks"
          @changed="loadNAT"
        />
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
            <span>虚拟容量 {{ formatGBOrDash(disk.virtualSizeBytes) }}</span>
            <span>文件占用 {{ formatGBOrDash(disk.fileSizeBytes) }}</span>
            <span>{{ disk.vhdType || "—" }}</span>
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

  <n-modal v-model:show="showConsole" preset="card" title="打开虚拟机控制台" style="width: 620px">
    <n-alert type="info" :show-icon="true" style="margin-bottom: 12px">
      VMConnect 这里填写的是远程 Hyper-V 主机的 Windows 账户，不是虚拟机里的账户。
      本地账户和 Microsoft 账户都优先使用列表中的「计算机名\账户名」；如果系统明确要求
      Microsoft 账户格式，再用「MicrosoftAccount\邮箱」。空密码账户默认不能远程登录，
      请先在远程主机上设置密码。
    </n-alert>
    <n-alert
      v-if="app.activeHost?.passwordSaved && !consoleSaveCred"
      type="info"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      这台主机已有保存的凭据，VMConnect 会优先使用已保存的账户。要改用其他账户，
      请填写新密码并勾选下方“保存到 Windows 凭据管理器”。
    </n-alert>
    <n-form label-placement="top" size="small">
      <n-form-item label="远程主机用户名">
        <div class="console-user-row">
          <n-auto-complete
            v-model:value="consoleUser"
            :options="consoleUserOptions"
            placeholder="计算机名\用户名"
            clearable
          />
          <n-button size="small" :loading="consoleUsersRefreshing" @click="refreshConsoleUsers">
            刷新账户
          </n-button>
        </div>
        <div class="form-hint" style="margin-top: 4px">
          账户列表来自当前活动主机。如果显示的不是远程那台电脑的账户，请先在“计算机”页面切换到该主机，再点“刷新账户”。
        </div>
      </n-form-item>
      <n-form-item label="密码">
        <div class="console-password-row">
          <n-input
            v-model:value="consolePassword"
            type="password"
            show-password-on="click"
            placeholder="远程主机账户密码（不是 PIN）"
            autocomplete="off"
          />
          <n-button
            v-if="consoleUserIsLocal"
            size="small"
            :loading="settingPassword"
            @click="setRemotePassword"
          >
            设为远程密码
          </n-button>
        </div>
      </n-form-item>
      <n-checkbox v-model:checked="consoleSaveCred">
        保存到 Windows 凭据管理器（推荐，修复 VMConnect 认证失败）
      </n-checkbox>
    </n-form>
    <n-alert
      v-if="consoleUserHasNoPassword"
      type="warning"
      :show-icon="true"
      style="margin-top: 10px"
    >
      该账户可能没有设置密码。Windows 默认禁止空密码本地账户从远程登录（包括
      VMConnect），会出现 0x8009030e。可以在密码框输入新密码后点旁边的“设为远程密码”，
      通过当前连接直接设置；也可以在远程主机上以管理员身份运行：
      <pre class="console-command">net user &lt;用户名&gt; "新的强密码"
net localgroup "Hyper-V Administrators" &lt;用户名&gt; /add</pre>
    </n-alert>
    <n-alert
      v-else-if="!consolePassword && !app.activeHost?.passwordSaved"
      type="warning"
      :show-icon="true"
      style="margin-top: 10px"
    >
      远程控制台需要远程主机账户密码用于网络身份验证。VMConnect 不使用 PIN；
      如果远程主机使用 Microsoft 账户，请填写 Microsoft 账户密码。
    </n-alert>
    <div class="form-hint" style="margin-top: 8px">
      保存后会写入 Windows 凭据管理器。打开远程控制台时，应用会用这里填写的账户启动
      VMConnect；VMConnect 内部的 WinRM 查询使用这份凭据，而不是当前 Windows 账户。
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showConsole = false">取消</n-button>
        <n-button size="small" type="primary" :loading="consoleLoading" :disabled="consoleUsersRefreshing" @click="submitConsole">
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

  <n-modal
    :show="showEdit"
    preset="card"
    title="编辑虚拟机配置"
    style="width: 820px"
    @update:show="onEditShowChange"
  >
    <div v-if="editSource" class="edit-form">
      <div v-if="editLoading" class="edit-loading-mask">
        <n-spin size="small" />
        <span>正在读取虚拟机完整配置…</span>
      </div>
      <n-alert type="info" :show-icon="true" style="margin-bottom: 10px">
        只会提交你实际改动过的字段。安全、固件和 CPU 等设置通常需要虚拟机关机后才能修改。
      </n-alert>
      <n-alert
        v-if="!editLoading && editRunningWarning()"
        type="warning"
        :show-icon="true"
        style="margin-bottom: 10px"
      >
        {{ editRunningWarning() }}
      </n-alert>

      <n-tabs type="line" animated>
        <n-tab-pane name="general" tab="常规">
          <n-form label-placement="top" size="small">
            <div class="form-grid">
              <n-form-item label="虚拟机名称">
                <n-input v-model:value="editForm.name" />
              </n-form-item>
              <n-form-item label="虚拟化代数">
                <n-input :value="`第 ${editSource.generation} 代`" disabled />
              </n-form-item>
              <n-form-item label="备注" class="span-2">
                <n-input
                  v-model:value="editForm.notes"
                  type="textarea"
                  :autosize="{ minRows: 2, maxRows: 5 }"
                />
              </n-form-item>
              <n-form-item label="自动启动操作">
                <n-select
                  v-model:value="editForm.automaticStartAction"
                  :options="automaticStartOptions"
                />
              </n-form-item>
              <n-form-item label="自动启动延迟（秒）">
                <n-input-number
                  v-model:value="editForm.automaticStartDelay"
                  :min="0"
                  :max="86400"
                  style="width: 100%"
                />
              </n-form-item>
              <n-form-item label="自动停止操作">
                <n-select
                  v-model:value="editForm.automaticStopAction"
                  :options="automaticStopOptions"
                />
              </n-form-item>
              <n-form-item label="自动检查点">
                <n-switch
                  v-model:value="editForm.automaticCheckpoints"
                  :disabled="!editForm.checkpointsEnabled"
                />
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
        </n-tab-pane>

        <n-tab-pane name="processor" tab="处理器">
          <n-form label-placement="top" size="small">
            <div class="form-grid">
              <n-form-item label="CPU 核数">
                <n-input-number
                  v-model:value="editForm.cpuCount"
                  :min="1"
                  :max="128"
                  :disabled="editSource.state !== 'Off'"
                  style="width: 100%"
                />
              </n-form-item>
              <n-form-item>
                <template #label>
                  嵌套虚拟化
                  <HelpTip>
                    <p>允许在该虚拟机内运行 Hyper-V、WSL2、Windows Sandbox、Docker 等需要虚拟化扩展的系统。</p>
                    <p>需要宿主机 CPU 支持；开启后会自动为虚拟网卡启用 MAC 地址欺骗。</p>
                    <p>修改后需要重新启动虚拟机才会在来宾系统里生效。</p>
                  </HelpTip>
                </template>
                <n-switch
                  v-model:value="editForm.nestedVirtualization"
                  :disabled="editSource.state !== 'Off'"
                />
              </n-form-item>
              <n-form-item label="处理器保留（%）">
                <n-input-number v-model:value="editForm.processorReserve" :min="0" :max="100" style="width: 100%" />
              </n-form-item>
              <n-form-item label="处理器上限（%）">
                <n-input-number v-model:value="editForm.processorMaximum" :min="1" :max="100" style="width: 100%" />
              </n-form-item>
              <n-form-item label="相对权重">
                <n-input-number v-model:value="editForm.processorWeight" :min="0" :max="10000" style="width: 100%" />
              </n-form-item>
              <n-form-item label="迁移兼容模式">
                <n-switch v-model:value="editForm.processorMigration" />
              </n-form-item>
              <n-form-item label="每核心硬件线程数">
                <n-input-number
                  v-model:value="editForm.hwThreadsPerCore"
                  :min="0"
                  :max="2"
                  :disabled="editSource.state !== 'Off'"
                  style="width: 100%"
                />
              </n-form-item>
            </div>
          </n-form>
        </n-tab-pane>

        <n-tab-pane name="memory" tab="内存">
          <n-form label-placement="top" size="small">
            <div class="form-grid">
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
              <n-form-item label="内存缓冲区（%）">
                <n-input-number
                  v-model:value="editForm.memoryBuffer"
                  :min="0"
                  :max="200"
                  :disabled="!editForm.dynamic"
                  style="width: 100%"
                />
              </n-form-item>
              <n-form-item label="内存优先级">
                <n-input-number v-model:value="editForm.memoryPriority" :min="0" :max="100" style="width: 100%" />
              </n-form-item>
            </div>
          </n-form>
        </n-tab-pane>

        <n-tab-pane name="network" tab="网络">
          <div v-if="editForm.networkAdapters.length === 0" class="empty-hint">
            这台虚拟机没有网络适配器。
          </div>
          <div
            v-for="(adapter, index) in editForm.networkAdapters"
            :key="adapter.id || index"
            class="adapter-edit-card"
          >
            <div class="adapter-edit-head">
              <div class="adapter-edit-title">
                <span class="vm-name">{{ adapter.name || "网络适配器" }}</span>
                <span class="cell-muted">
                  {{ adapter.ipAddresses.join("、") || "无 IP" }}
                </span>
              </div>
              <n-tag size="tiny" :bordered="false">{{ adapter.status || "—" }}</n-tag>
            </div>
            <n-form label-placement="top" size="small">
              <div class="form-grid">
                <n-form-item label="虚拟交换机">
                  <n-select
                    v-model:value="adapter.switchName"
                    :options="switchOptions.filter((item) => item.value !== '')"
                    placeholder="选择交换机"
                  />
                </n-form-item>
                <n-form-item label="VLAN 模式">
                  <n-select v-model:value="adapter.vlanMode" :options="vlanModeOptions" />
                </n-form-item>
                <n-form-item v-if="adapter.vlanMode === 'Access'" label="VLAN ID">
                  <n-input-number v-model:value="adapter.vlanID" :min="1" :max="4095" style="width: 100%" />
                </n-form-item>
                <n-form-item label="带宽上限（Mbps，0=不限）">
                  <n-input-number v-model:value="adapter.bandwidthMbps" :min="0" :max="100000" style="width: 100%" />
                </n-form-item>
                <n-form-item label="静态 MAC 地址">
                  <n-input v-model:value="adapter.staticMacAddress" :disabled="adapter.dynamicMacAddress" />
                </n-form-item>
                <n-form-item label="动态 MAC">
                  <n-switch v-model:value="adapter.dynamicMacAddress" />
                </n-form-item>
                <n-form-item label="MAC 地址欺骗">
                  <n-select v-model:value="adapter.macAddressSpoofing" :options="onOffOptions" />
                </n-form-item>
                <n-form-item label="DHCP 防护">
                  <n-select v-model:value="adapter.dhcpGuard" :options="onOffOptions" />
                </n-form-item>
                <n-form-item label="路由器防护">
                  <n-select v-model:value="adapter.routerGuard" :options="onOffOptions" />
                </n-form-item>
                <n-form-item label="设备命名">
                  <n-select v-model:value="adapter.deviceNaming" :options="onOffOptions" />
                </n-form-item>
                <n-form-item label="VMQ 权重">
                  <n-input-number v-model:value="adapter.vmqWeight" :min="0" :max="100" style="width: 100%" />
                </n-form-item>
                <n-form-item label="SR-IOV 权重">
                  <n-input-number v-model:value="adapter.iovWeight" :min="0" :max="100" style="width: 100%" />
                </n-form-item>
                <n-form-item label="广播风暴限制">
                  <n-input-number v-model:value="adapter.stormLimit" :min="0" style="width: 100%" />
                </n-form-item>
                <n-form-item label="IPsec 卸载">
                  <n-switch v-model:value="adapter.ipsecOffload" />
                </n-form-item>
              </div>
            </n-form>
          </div>
          <div class="form-hint">
            VLAN、VMQ、SR-IOV、IPsec 需要交换机和物理网卡支持；运行中的虚拟机可能拒绝修改。
            修改交换机会影响该适配器的网络连接。
          </div>
        </n-tab-pane>

        <n-tab-pane name="security" tab="安全与固件">
          <n-alert v-if="editSource.generation !== 2" type="info" :show-icon="true" style="margin-bottom: 10px">
            安全启动和 TPM 仅第 2 代虚拟机支持。当前是第 {{ editSource.generation }} 代。
          </n-alert>
          <n-form label-placement="top" size="small">
            <div class="form-grid">
              <n-form-item label="安全启动">
                <n-switch
                  v-model:value="editForm.secureBoot"
                  :disabled="editSource.generation !== 2 || editSource.state !== 'Off'"
                />
              </n-form-item>
              <n-form-item label="安全启动模板">
                <n-select
                  v-model:value="editForm.secureBootTemplate"
                  :options="secureBootTemplateOptions"
                  :disabled="editSource.generation !== 2 || editSource.state !== 'Off' || !editForm.secureBoot"
                />
              </n-form-item>
              <n-form-item label="网络启动协议">
                <n-select
                  v-model:value="editForm.networkBootProtocol"
                  :options="networkBootProtocolOptions"
                  :disabled="editSource.generation !== 2 || editSource.state !== 'Off'"
                />
              </n-form-item>
              <n-form-item label="启用 TPM">
                <n-switch
                  v-model:value="editForm.tpmEnabled"
                  :disabled="editSource.generation !== 2 || editSource.state !== 'Off'"
                />
              </n-form-item>
              <n-form-item label="加密状态与迁移流量">
                <n-switch
                  v-model:value="editForm.encryptTraffic"
                  :disabled="editSource.state !== 'Off'"
                />
              </n-form-item>
              <n-form-item label="关闭基于虚拟化的安全（VBS）">
                <n-switch
                  v-model:value="editForm.vbsOptOut"
                  :disabled="editSource.state !== 'Off'"
                />
              </n-form-item>
            </div>
          </n-form>
          <div class="form-hint">
            加密、TPM 和安全启动通常需要虚拟机关机；启用 TPM 后建议同时启用安全启动。
            修改失败时请先关闭虚拟机再提交。
          </div>
        </n-tab-pane>

        <n-tab-pane name="services" tab="集成服务">
          <div v-if="editForm.integrationServices.length === 0" class="empty-hint">
            没有读取到集成服务信息。
          </div>
          <div
            v-for="service in editForm.integrationServices"
            :key="service.name"
            class="service-row"
          >
            <div>
              <div class="service-name">{{ service.displayName || service.name }}</div>
              <div class="cell-muted mono-text">{{ service.name }}</div>
            </div>
            <n-switch v-model:value="service.enabled" />
          </div>
          <div class="form-hint">
            集成服务控制虚拟机与宿主机之间的心跳、时间同步、关机、数据交换、VSS 备份等能力。
            关闭“数据交换”后，宿主机可能无法读取虚拟机内的 IP 地址。
          </div>
        </n-tab-pane>

        <n-tab-pane name="forward" tab="端口转发">
          <VmPortForwardPanel
            v-if="!isWebMode"
            :vm="editSource"
            :nat-networks="natNetworks"
            @changed="loadNAT"
          />
          <n-alert v-else type="info" :show-icon="true">
            端口转发和 NAT 管理需要在桌面端进行。
          </n-alert>
        </n-tab-pane>

        <n-tab-pane name="storage" tab="存储">
          <div v-if="(editSource.hardDrives ?? []).length > 0" class="disk-edit-section">
            <div class="detail-section-title">虚拟硬盘（可调整容量）</div>
            <div
              v-for="(disk, index) in editSource.hardDrives ?? []"
              :key="disk.path"
              class="disk-edit-row"
            >
              <div class="disk-edit-info">
                <div class="mono-text">{{ disk.path }}</div>
                <div class="adapter-meta">
                  <span>类型：{{ disk.vhdType || "—" }}</span>
                  <span>虚拟容量：{{ formatGBOrDash(disk.virtualSizeBytes) }}</span>
                  <span>文件占用：{{ formatGBOrDash(disk.fileSizeBytes) }}</span>
                  <span>最低安全：{{ formatGBOrDash(minSafeBytes(disk)) }}</span>
                </div>
              </div>
              <div class="disk-edit-actions">
                <n-input-number
                  v-model:value="editDiskSizes[index]"
                  :min="1"
                  :max="65536"
                  :disabled="!disk.virtualSizeBytes || disk.vhdType === 'Differencing'"
                  style="width: 150px"
                >
                  <template #suffix>GB</template>
                </n-input-number>
                <n-button
                  v-if="disk.vhdType !== 'Differencing'"
                  size="tiny"
                  @click="editDiskSizes[index] = suggestedDiskGB(disk)"
                >
                  建议 {{ suggestedDiskGB(disk) }} GB
                </n-button>
              </div>
              <div v-if="diskRowWarning(disk, index)" class="disk-warning">
                {{ diskRowWarning(disk, index) }}
              </div>
            </div>
            <n-alert type="warning" :show-icon="true">
              扩展后需要在来宾系统里用“磁盘管理”扩展分区；缩小只调整虚拟容量，不会自动缩带来宾分区。
              调整硬盘会先关闭虚拟机；缩小可能丢失数据，请确认来宾系统内已经腾出足够空间。
            </n-alert>
          </div>
          <div v-else class="empty-hint">没有虚拟硬盘。</div>
        </n-tab-pane>
      </n-tabs>

      <div class="form-hint">
        编辑任务会进入队列执行。部分设置需要虚拟机关机；网络适配器只提交改动过的字段。
      </div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" :disabled="editLoading || savingEdit" @click="resetEditForm">
          还原修改
        </n-button>
        <n-button size="small" :disabled="savingEdit" @click="closeEdit()">取消</n-button>
        <n-button
          size="small"
          type="primary"
          :loading="savingEdit"
          :disabled="editLoading || savingEdit"
          @click="saveEdit"
        >
          提交任务
        </n-button>
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

.edit-form {
  position: relative;
}

.edit-loading-mask {
  position: absolute;
  inset: -6px;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border-radius: 8px;
  background: var(--bg);
  opacity: 0.92;
  font-size: 12.5px;
  color: var(--text-2);
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

.disk-edit-section {
  margin-top: 16px;
}

.disk-edit-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-inset);
  margin-bottom: 8px;
}

.disk-edit-info {
  flex: 1;
  min-width: 200px;
}

.disk-edit-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.disk-warning {
  flex-basis: 100%;
  color: var(--warning);
  font-size: 11.5px;
  line-height: 1.6;
}

.console-user-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.console-user-row :deep(.n-auto-complete) {
  flex: 1;
  min-width: 0;
}

.console-command {
  margin: 8px 0 0;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--surface-inset);
  border: 1px solid var(--border-soft);
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 11px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
}

.console-password-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.console-password-row :deep(.n-input) {
  flex: 1;
  min-width: 0;
}

.page-loading-mask {
  position: fixed;
  inset: 0;
  z-index: 1500;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  background: rgba(8, 10, 14, 0.45);
  backdrop-filter: blur(2px);
}

.page-loading-text {
  font-size: 13px;
  color: var(--text-1);
}

.span-2 {
  grid-column: 1 / -1;
}

.adapter-edit-card {
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 12px;
  background: var(--surface-inset);
}

.adapter-edit-head {
  display: flex;
  align-items: center;
  gap: 10px;
  justify-content: space-between;
  margin-bottom: 8px;
}

.adapter-edit-title {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
}

.service-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-inset);
  margin-bottom: 8px;
}

.service-name {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-strong);
}
</style>
