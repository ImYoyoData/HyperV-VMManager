<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import { CopyOutline, FolderOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import HelpTip from "../components/HelpTip.vue";
import VmStateTag from "../components/VmStateTag.vue";
import HostConnectModal from "../components/HostConnectModal.vue";
import { HyperVService, errorMessage } from "../api";
import type { CloneRequest, HostCapacity, HostItem, VM, VirtualSwitch } from "../api";
import { useAppStore } from "../stores/app";
import { useHostsStore } from "../stores/hosts";
import { useTaskStore } from "../stores/tasks";
import { formatMB, switchTypeLabel, vmStateLabel } from "../utils/format";

const router = useRouter();
const message = useMessage();
const app = useAppStore();
const hosts = useHostsStore();
const taskStore = useTaskStore();

const switches = ref<VirtualSwitch[]>([]);
const sourceVMs = ref<VM[]>([]);
const submitting = ref(false);
const loadingPath = ref(false);
const defaultsApplied = ref(false);
const showHostConnect = ref(false);
const connectTarget = ref<HostItem | null>(null);
const pendingSubmit = ref(false);
const targetCapacity = ref<HostCapacity | null>(null);
const capacityLoading = ref(false);

const targetHostID = ref("");
const targetHost = computed(
  () => hosts.items.find((item) => item.id === targetHostID.value) ?? app.activeHost
);
const isRemote = computed(() => !!targetHost.value && !targetHost.value.isLocal);
const hostOptions = computed(() =>
  hosts.items.map((item) => ({
    label: `${item.name}${item.isLocal ? "（本机）" : "（" + item.address + "）"}${item.connected ? "" : " · 未连接"}`,
    value: item.id,
  }))
);

const form = reactive({
  sourceName: "",
  newName: "",
  targetDirectory: "",
  diskMode: "copy" as "copy" | "differencing",
  exportDirectory: "",
  followSource: true,
  stopSourceIfRunning: true,
  startupMB: 4096,
  dynamic: false,
  minimumMB: 1024,
  maximumMB: 8192,
  cpuCount: 4,
  switchName: "",
  bandwidthMbps: 100,
  nestedVirtualization: false,
});

const sourceVM = computed(
  () => sourceVMs.value.find((vm) => vm.name === form.sourceName) ?? null
);
const sourceIsRunning = computed(
  () => !!sourceVM.value && sourceVM.value.state !== "Off"
);

const vmOptions = computed(() =>
  sourceVMs.value.map((vm) => ({
    label: `${vm.name}（${vmStateLabel(vm.state)}）`,
    value: vm.name,
  }))
);

const switchOptions = computed(() => [
  { label: "保持源虚拟机网络不变", value: "" },
  ...switches.value.map((item) => ({
    label: `${item.name}（${switchTypeLabel(item.switchType)}）`,
    value: item.name,
  })),
]);

const finalDirectory = computed(() => {
  if (!form.targetDirectory) return "—";
  return `${form.targetDirectory.replace(/[\\/]+$/, "")}\\${form.newName || "新名称"}`;
});

const driveOptions = computed(() =>
  (targetCapacity.value?.disks ?? []).map((disk) => ({
    label: `${disk.drive} 可用 ${formatGB(disk.freeBytes)} / 共 ${formatGB(disk.totalBytes)}`,
    value: disk.drive,
  }))
);

const selectedDrive = computed(() => {
  const match = /^[A-Za-z]:/.exec(form.targetDirectory);
  return match ? match[0].toUpperCase() : "";
});

const targetDisk = computed(() => {
  const drive = selectedDrive.value;
  return targetCapacity.value?.disks?.find((disk) => disk.drive.toUpperCase() === drive) ?? null;
});

function formatGB(bytes: number): string {
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
}

const cpuAvailable = computed(() => Math.max(0, targetCapacity.value?.availableCPU ?? 0));
const memoryAvailableMB = computed(() => Math.max(0, targetCapacity.value?.availableMemoryMB ?? 0));
const memoryRequestedMB = computed(() =>
  form.dynamic ? Number(form.maximumMB) : Number(form.startupMB)
);

const suggestedCPU = computed(() => {
  if (cpuAvailable.value <= 0) return 2;
  return Math.max(1, Math.min(8, Math.floor(cpuAvailable.value / 2)));
});

const suggestedMemoryMB = computed(() => {
  if (memoryAvailableMB.value <= 0) return 4096;
  return Math.max(2048, Math.min(8192, Math.floor(memoryAvailableMB.value / 2 / 1024) * 1024));
});

const sourceDiskBytes = computed(() =>
  (sourceVM.value?.hardDrives ?? []).reduce(
    (sum, disk) => sum + (disk.fileSizeBytes || 0),
    0
  )
);

const freeGB = computed(() => (targetDisk.value?.freeBytes ?? 0) / 1024 ** 3);

function resourceLevel(requested: number, available: number): "ok" | "warn" | "bad" {
  if (available <= 0) return "bad";
  if (requested > available) return "bad";
  if (requested > available * 0.8) return "warn";
  return "ok";
}

const cpuLevel = computed(() => resourceLevel(Number(form.cpuCount), cpuAvailable.value));
const memoryLevel = computed(() => resourceLevel(memoryRequestedMB.value, memoryAvailableMB.value));
const diskLevel = computed(() => {
  if (form.diskMode === "differencing") return "ok";
  return resourceLevel(sourceDiskBytes.value / 1024 ** 3, freeGB.value);
});

function onDriveChange(drive: string) {
  const rest = form.targetDirectory.replace(/^[A-Za-z]:[\\/]?/, "");
  form.targetDirectory = `${drive}\\${rest || "VMs"}`;
}

async function loadCapacity(hostID: string) {
  if (!hostID) {
    targetCapacity.value = null;
    return;
  }
  capacityLoading.value = true;
  try {
    targetCapacity.value = await HyperVService.GetHostCapacity(hostID);
  } catch {
    targetCapacity.value = null;
  } finally {
    capacityLoading.value = false;
  }
}

async function browseExportDirectory() {
  if (isRemote.value) {
    message.warning("目标主机是远程计算机，请手动输入远程主机上的绝对路径");
    return;
  }
  try {
    const directory = await HyperVService.PickDirectoryIn(
      "选择克隆临时导出目录",
      form.exportDirectory || form.targetDirectory || ""
    );
    if (directory) form.exportDirectory = directory;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

function applyDefaults() {
  const settings = app.settings;
  if (!settings || defaultsApplied.value) return;
  defaultsApplied.value = true;
  form.startupMB = settings.defaultMemoryStartupMB || 4096;
  form.dynamic = settings.defaultDynamicMemory;
  form.minimumMB = settings.defaultMemoryMinimumMB || 1024;
  form.maximumMB = settings.defaultMemoryMaximumMB || 8192;
  form.cpuCount = settings.defaultCPUCount || 4;
  form.bandwidthMbps = settings.defaultBandwidthMbps || 100;
}

watch(
  () => app.settings,
  () => applyDefaults(),
  { immediate: true }
);

async function refreshSourceDirectory(name: string) {
  if (!name) return;
  loadingPath.value = true;
  try {
    const directory = await HyperVService.SuggestCloneDirectoryForHost(
      targetHostID.value,
      name
    );
    if (directory) {
      form.targetDirectory = directory;
    }
  } catch (err) {
    message.warning(`无法自动获取源虚拟机目录：${errorMessage(err)}`);
  } finally {
    loadingPath.value = false;
  }
}

async function onSourceChange(name: string) {
  const vm = sourceVMs.value.find((item) => item.name === name);
  if (!vm) return;
  form.newName = `${vm.name}-Clone`;
  form.startupMB = vm.memoryStartupMB;
  form.dynamic = vm.dynamicMemoryEnabled;
  form.minimumMB = vm.memoryMinimumMB || 1024;
  form.maximumMB = vm.memoryMaximumMB || Math.max(vm.memoryStartupMB, 8192);
  form.cpuCount = vm.processorCount;
  form.nestedVirtualization = vm.nestedVirtualization;
  form.switchName = "";
  if (form.followSource) {
    await refreshSourceDirectory(name);
  }
}

watch(
  () => form.followSource,
  (follow) => {
    if (follow && form.sourceName) {
      void refreshSourceDirectory(form.sourceName);
    }
  }
);

async function browseDirectory() {
  if (isRemote.value) {
    message.warning("目标主机是远程计算机，请手动输入远程主机上的目标目录");
    return;
  }
  try {
    const directory = await HyperVService.PickDirectoryIn(
      "选择克隆虚拟机存放目录",
      form.targetDirectory || app.settings?.storageRoot || ""
    );
    if (directory) form.targetDirectory = directory;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

function validate(): string {
  if (!form.sourceName) return "请选择源虚拟机";
  if (!form.newName.trim()) return "请输入新虚拟机名称";
  if (/[\\/:*?"<>|]/.test(form.newName.trim())) {
    return `名称不能包含 \\ / : * ? " < > | 这些字符`;
  }
  if (form.newName.trim() === form.sourceName) return "新名称不能与源虚拟机名称相同";
  if (!form.targetDirectory.trim()) return "请选择目标目录";
  if (!form.startupMB || form.startupMB < 256) return "启动内存不能小于 256 MB";
  if (form.dynamic) {
    if (form.minimumMB > form.startupMB) return "动态内存最小值不能大于启动内存";
    if (form.startupMB > form.maximumMB) return "启动内存不能大于动态内存最大值";
  }
  if (!form.cpuCount || form.cpuCount < 1 || form.cpuCount > 128) return "CPU 核数必须在 1 到 128 之间";
  if (form.bandwidthMbps < 0) return "带宽不能为负数";
  if (sourceIsRunning.value && !form.stopSourceIfRunning) {
    return "源虚拟机正在运行，请勾选“允许自动关机”或先手动关机";
  }
  return "";
}

async function submit() {
  const error = validate();
  if (error) {
    message.warning(error);
    return;
  }
  if (!sourceVM.value) {
    message.warning("请选择源虚拟机");
    return;
  }
  const host = hosts.items.find((item) => item.id === targetHostID.value);
  if (!host) {
    message.warning("请选择目标计算机");
    return;
  }
  if (!host.connected) {
    pendingSubmit.value = true;
    await ensureHostConnected(host);
    return;
  }

  submitting.value = true;
  try {
    const request: CloneRequest = {
      sourceName: form.sourceName,
      newName: form.newName.trim(),
      targetDirectory: form.targetDirectory.trim(),
      stopSourceIfRunning: form.stopSourceIfRunning,
      diskMode: form.diskMode,
      exportDirectory: form.exportDirectory.trim(),
      memory: {
        startupMB: Number(form.startupMB),
        dynamic: form.dynamic,
        minimumMB: Number(form.minimumMB),
        maximumMB: Number(form.maximumMB),
      },
      cpuCount: Number(form.cpuCount),
      switchName: form.switchName,
      bandwidthMbps: Number(form.bandwidthMbps),
      nestedVirtualization: form.nestedVirtualization,
    };
    const task = await HyperVService.SubmitClone(targetHostID.value, request);
    message.success(`克隆任务已提交到 ${host.name}${task ? `（${task.id}）` : ""}`);
    void taskStore.fetch();
    void router.push("/tasks");
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    submitting.value = false;
  }
}

async function loadSwitches() {
  const hostID = targetHostID.value;
  if (!hostID) {
    switches.value = [];
    return;
  }
  try {
    switches.value = (await HyperVService.ListSwitchesForHost(hostID)) ?? [];
  } catch {
    switches.value = [];
  }
}

async function loadSourceVMs() {
  const hostID = targetHostID.value;
  if (!hostID) {
    sourceVMs.value = [];
    return;
  }
  try {
    sourceVMs.value = (await HyperVService.ListVMsForHost(hostID)) ?? [];
  } catch (err) {
    sourceVMs.value = [];
    message.warning(`读取目标计算机的虚拟机失败：${errorMessage(err)}`);
  }
}

async function onTargetHostChange(value: string) {
  targetHostID.value = value;
  form.sourceName = "";
  form.newName = "";
  form.targetDirectory = "";
  switches.value = [];
  const host = hosts.items.find((item) => item.id === value);
  await ensureHostConnected(host);
  await Promise.all([loadSwitches(), loadSourceVMs(), loadCapacity(value)]);
}

async function ensureHostConnected(host: HostItem | undefined): Promise<boolean> {
  if (!host) return false;
  if (host.isLocal || host.connected) return true;
  if (host.passwordSaved) {
    try {
      await hosts.connect(host.id, "");
      message.success(`已连接 ${host.name}`);
      await hosts.fetch();
      return true;
    } catch {
      // 落回密码输入
    }
  }
  connectTarget.value = host;
  showHostConnect.value = true;
  return false;
}

async function onHostConnected(host: HostItem) {
  await hosts.fetch();
  await Promise.all([loadSwitches(), loadSourceVMs()]);
  if (pendingSubmit.value && host.id === targetHostID.value) {
    pendingSubmit.value = false;
    await submit();
  }
}

onMounted(async () => {
  applyDefaults();
  await hosts.fetch();
  if (!targetHostID.value) {
    targetHostID.value =
      app.activeHost?.id || hosts.items.find((item) => item.isLocal)?.id || hosts.items[0]?.id || "";
  }
  await Promise.all([loadSwitches(), loadSourceVMs(), loadCapacity(targetHostID.value)]);
});
</script>

<template>
  <PageHeader title="克隆虚拟机" subtitle="整台复制源虚拟机，并覆盖名称、位置与硬件配置">
    <n-button size="small" @click="router.push('/vms')">返回虚拟机列表</n-button>
  </PageHeader>

  <n-alert v-if="isRemote" type="warning" class="inline-error" title="目标计算机是远程主机">
    克隆任务会在远程主机「{{ targetHost?.name }}」上执行。目标目录必须是该远程主机上的绝对路径，
    且需要足够的磁盘空间用于导出和导入。
  </n-alert>

  <div class="wizard-layout">
    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">克隆参数</div>
          <div class="card-subtitle">源虚拟机在克隆期间会被导出，运行中的虚拟机将先关机</div>
        </div>
      </div>

      <n-form label-placement="top" size="small">
        <n-form-item>
          <template #label>
            目标计算机
            <HelpTip>
              <p>克隆任务会提交到所选计算机上执行，不会切换当前管理主机。</p>
              <p>源虚拟机列表来自目标计算机；切换目标计算机会重新读取该主机上的虚拟机。</p>
            </HelpTip>
          </template>
          <n-select
            v-model:value="targetHostID"
            :options="hostOptions"
            placeholder="选择目标计算机"
            @update:value="onTargetHostChange"
          />
        </n-form-item>

        <div class="form-grid">
          <n-form-item label="源虚拟机">
            <n-select
              v-model:value="form.sourceName"
              :options="vmOptions"
              filterable
              placeholder="选择要克隆的虚拟机"
              @update:value="onSourceChange"
            />
          </n-form-item>
          <n-form-item label="新虚拟机名称">
            <n-input v-model:value="form.newName" placeholder="例如 Win11-Clone" />
          </n-form-item>
        </div>

        <div v-if="sourceVM" class="source-summary">
          <VmStateTag :state="sourceVM.state" />
          <span class="cell-muted">
            {{ formatMB(sourceVM.memoryStartupMB) }} · {{ sourceVM.processorCount }} 核 ·
            第 {{ sourceVM.generation }} 代
          </span>
          <span class="cell-muted mono-text">{{ sourceVM.path }}</span>
        </div>

        <n-alert
          v-if="sourceIsRunning"
          type="warning"
          :show-icon="true"
          style="margin-bottom: 14px"
          title="源虚拟机正在运行"
        >
          <div class="running-alert">
            <n-checkbox v-model:checked="form.stopSourceIfRunning">
              允许自动关闭源虚拟机后再克隆
            </n-checkbox>
            <span class="cell-muted">若取消勾选，请先手动关机，否则任务会失败。</span>
          </div>
        </n-alert>

        <n-form-item>
          <template #label>
            存放位置
            <HelpTip>
              <p><strong>跟随源虚拟机所在目录</strong>：在源机目录的上一级创建以新名称命名的目录。</p>
              <p>克隆会重新复制虚拟硬盘，确保目标磁盘有足够空间（至少与源盘已用空间相当）。</p>
            </HelpTip>
          </template>
          <div class="input-with-button">
            <n-input v-model:value="form.targetDirectory" placeholder="例如 D:\VMs" />
            <n-button size="small" :loading="loadingPath" :disabled="isRemote" @click="browseDirectory">
              <template #icon>
                <n-icon :component="FolderOutline" />
              </template>
              浏览
            </n-button>
          </div>
        </n-form-item>
        <div class="path-line">
          <n-checkbox v-model:checked="form.followSource">跟随源虚拟机所在目录</n-checkbox>
          <span class="form-hint" v-if="loadingPath">正在读取源目录...</span>
        </div>
        <div class="form-hint" style="margin-bottom: 12px">
          新虚拟机最终目录：<span class="mono-text">{{ finalDirectory }}</span>
        </div>

        <n-form-item>
          <template #label>
            目标磁盘
            <HelpTip>
              <p>选择新虚拟机文件存放在哪个磁盘。列表显示目标计算机上的固定磁盘与可用空间。</p>
              <p>完整复制大约需要源盘已用空间等量的空闲空间；空间不足时可以选择差异磁盘。</p>
            </HelpTip>
          </template>
          <n-select
            :value="selectedDrive"
            :options="driveOptions"
            :loading="capacityLoading"
            placeholder="选择目标磁盘"
            @update:value="onDriveChange"
          />
        </n-form-item>
        <div v-if="targetDisk" class="form-hint" style="margin: -4px 0 10px">
          {{ targetDisk.drive }} 可用 {{ formatGB(targetDisk.freeBytes) }} / 共
          {{ formatGB(targetDisk.totalBytes) }}
        </div>

        <n-form-item>
          <template #label>
            克隆磁盘方式
            <HelpTip>
              <p><strong>完整复制</strong>：导出再导入，生成完全独立的虚拟硬盘；需要源盘已用空间等量的临时空间和目标空间。</p>
              <p><strong>差异磁盘</strong>：新虚拟机使用以源盘为父盘的差异磁盘，占用空间小、创建快；但源盘不能删除或修改，否则新虚拟机会损坏。</p>
            </HelpTip>
          </template>
          <n-radio-group v-model:value="form.diskMode">
            <n-radio-button value="copy">完整复制（独立磁盘）</n-radio-button>
            <n-radio-button value="differencing">差异磁盘（省空间）</n-radio-button>
          </n-radio-group>
        </n-form-item>
        <n-form-item v-if="form.diskMode === 'copy'">
          <template #label>
            临时导出目录（可选）
            <HelpTip>
              <p>完整复制会先把源虚拟机导出到临时目录。留空时使用目标主机的系统临时目录。</p>
              <p>如果系统盘空间不足，请改成目标磁盘上的目录，例如 D:\VMs\clone-temp。</p>
            </HelpTip>
          </template>
          <div class="input-with-button">
            <n-input v-model:value="form.exportDirectory" placeholder="留空使用目标主机临时目录" />
            <n-button size="small" :disabled="isRemote" @click="browseExportDirectory">
              <template #icon>
                <n-icon :component="FolderOutline" />
              </template>
              浏览
            </n-button>
          </div>
        </n-form-item>
        <n-alert
          v-if="form.diskMode === 'differencing'"
          type="warning"
          :show-icon="true"
          style="margin-bottom: 12px"
        >
          差异磁盘不会复制源盘，新虚拟机会依赖源虚拟机的硬盘文件。源盘不能删除、移动或修改，
          源虚拟机需要保持关机直到差异盘创建完成。
        </n-alert>

        <div class="resource-grid">
          <div class="resource-hint" :class="'res-' + diskLevel">
            <div class="resource-label">磁盘</div>
            <div>
              目标 {{ targetDisk?.drive || "—" }} 可用
              {{ formatGB(targetDisk?.freeBytes ?? 0) }} / 总
              {{ formatGB(targetDisk?.totalBytes ?? 0) }}
            </div>
            <div v-if="form.diskMode === 'copy'">
              源盘文件占用约 {{ formatGB(sourceDiskBytes) }}
            </div>
            <div v-else>差异磁盘占用空间较小</div>
          </div>
          <div class="resource-hint" :class="'res-' + memoryLevel">
            <div class="resource-label">内存</div>
            <div>
              可用 {{ formatMB(memoryAvailableMB) }} / 总
              {{ formatMB(targetCapacity?.totalMemoryMB ?? 0) }}
            </div>
            <div>当前 {{ formatMB(memoryRequestedMB) }} · 建议 {{ formatMB(suggestedMemoryMB) }}</div>
            <n-button
              size="tiny"
              text
              type="primary"
              @click="form.startupMB = suggestedMemoryMB; form.maximumMB = Math.max(form.maximumMB, suggestedMemoryMB)"
            >
              使用建议
            </n-button>
          </div>
          <div class="resource-hint" :class="'res-' + cpuLevel">
            <div class="resource-label">CPU</div>
            <div>可用 {{ cpuAvailable }} / 总 {{ targetCapacity?.logicalProcessors ?? 0 }} 核</div>
            <div>当前 {{ form.cpuCount }} 核 · 建议 {{ suggestedCPU }} 核</div>
            <n-button size="tiny" text type="primary" @click="form.cpuCount = suggestedCPU">
              使用建议
            </n-button>
          </div>
        </div>

        <div class="detail-section-title">硬件覆盖</div>
        <div class="form-grid">
          <n-form-item>
            <template #label>
              启动内存（MB）
              <HelpTip>
                <p>新虚拟机的启动内存。克隆会覆盖源机的内存设置。</p>
                <p>开启动态内存后，Hyper-V 会在最小值与最大值之间按负载调整，适合多台虚拟机共用主机内存。</p>
              </HelpTip>
            </template>
            <n-input-number v-model:value="form.startupMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item label="CPU 核数">
            <n-input-number v-model:value="form.cpuCount" :min="1" :max="128" style="width: 100%" />
          </n-form-item>
          <n-form-item label="动态内存">
            <n-switch v-model:value="form.dynamic" />
          </n-form-item>
          <n-form-item>
            <template #label>
              嵌套虚拟化
              <HelpTip>
                <p>允许在该虚拟机内继续运行 Hyper-V、WSL2、Windows Sandbox、Docker 等需要虚拟化扩展的系统。</p>
                <p>需要宿主机 CPU 支持；开启后会自动为虚拟网卡启用 MAC 地址欺骗。</p>
                <p>克隆时会覆盖源机的嵌套虚拟化设置；建议使用固定内存。</p>
              </HelpTip>
            </template>
            <n-switch v-model:value="form.nestedVirtualization" />
          </n-form-item>
          <n-form-item>
            <template #label>
              网络交换机
              <HelpTip>
                <p><strong>保持源虚拟机网络不变</strong>：新机沿用源机的交换机连接。</p>
                <p><strong>指定交换机</strong>：把所有网络适配器连接到新的虚拟交换机。</p>
                <p>外部交换机用于桥接物理网络，内部和专用交换机用于主机内隔离通信。</p>
              </HelpTip>
            </template>
            <n-select v-model:value="form.switchName" :options="switchOptions" />
          </n-form-item>
          <n-form-item v-if="form.dynamic" label="动态内存最小值（MB）">
            <n-input-number v-model:value="form.minimumMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item v-if="form.dynamic" label="动态内存最大值（MB）">
            <n-input-number v-model:value="form.maximumMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item label="带宽上限（Mbps，0 = 不限）">
            <n-input-number v-model:value="form.bandwidthMbps" :min="0" :max="100000" style="width: 100%" />
          </n-form-item>
        </div>

        <n-alert type="info" :show-icon="true">
          <template v-if="form.diskMode === 'differencing'">
            差异磁盘克隆会重新生成虚拟机 ID 和动态 MAC 地址，创建速度快、占用空间小；
            请勿修改、移动或删除源虚拟机的硬盘文件。
          </template>
          <template v-else>
            完整复制会重新生成虚拟机 ID 和动态 MAC 地址，避免与源虚拟机冲突。导出过程可能需要几分钟，
            取决于虚拟硬盘大小。
          </template>
        </n-alert>
      </n-form>
    </div>

    <div class="summary-card">
      <div class="summary-title">克隆摘要</div>
      <div class="summary-row">
        <span class="key">目标计算机</span>
        <span class="value">{{ targetHost?.name || "—" }}<template v-if="isRemote">（远程）</template></span>
      </div>
      <div class="summary-row">
        <span class="key">源虚拟机</span>
        <span class="value">{{ form.sourceName || "—" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">新名称</span>
        <span class="value">{{ form.newName || "—" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">目标目录</span>
        <span class="value mono-text">{{ finalDirectory }}</span>
      </div>
      <div class="summary-row">
        <span class="key">磁盘方式</span>
        <span class="value">{{ form.diskMode === "differencing" ? "差异磁盘（省空间）" : "完整复制" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">源机状态</span>
        <span class="value">{{ sourceVM ? vmStateLabel(sourceVM.state) : "—" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">内存</span>
        <span class="value">
          {{ formatMB(form.startupMB) }}
          <template v-if="form.dynamic">
            （动态 {{ formatMB(form.minimumMB) }} - {{ formatMB(form.maximumMB) }}）
          </template>
        </span>
      </div>
      <div class="summary-row">
        <span class="key">CPU</span>
        <span class="value">{{ form.cpuCount }} 核</span>
      </div>
      <div class="summary-row">
        <span class="key">网络</span>
        <span class="value">{{ form.switchName || "保持源机网络" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">带宽</span>
        <span class="value">{{ form.bandwidthMbps > 0 ? `${form.bandwidthMbps} Mbps` : "不限" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">嵌套虚拟化</span>
        <span class="value">{{ form.nestedVirtualization ? "开启" : "关闭" }}</span>
      </div>

      <n-button
        type="primary"
        block
        size="small"
        style="margin-top: 14px"
        :loading="submitting"
        @click="submit"
      >
        <template #icon>
          <n-icon :component="CopyOutline" />
        </template>
        提交克隆任务
      </n-button>
      <div class="form-hint" style="text-align: center; margin-top: 8px">
        任务会进入队列，可在任务队列查看进度
      </div>
    </div>
  </div>
  <HostConnectModal
    v-model:show="showHostConnect"
    :host="connectTarget"
    @connected="onHostConnected"
  />
</template>

<style scoped>
.input-with-button {
  display: flex;
  gap: 8px;
  width: 100%;
}

.input-with-button :deep(.n-input) {
  flex: 1;
}

.source-summary {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 8px 10px;
  margin-bottom: 14px;
  border-radius: 8px;
  background: var(--input-bg);
  border: 1px solid var(--border-soft);
}

.running-alert {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.path-line {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 4px;
}

.detail-section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-3);
  margin: 16px 0 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border-soft);
}

.resource-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 4px 0 12px;
}

.resource-hint {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 9px 11px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-inset);
  font-size: 11.5px;
  line-height: 1.7;
}

.resource-label {
  font-weight: 600;
  color: var(--text-1);
}

.res-ok {
  border-color: var(--ok-border);
  background: var(--ok-surface);
}

.res-warn {
  border-color: var(--amber-border);
  background: var(--amber-surface);
}

.res-bad {
  border-color: var(--err-border);
  background: var(--err-surface);
}</style>
