<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import { CopyOutline, FolderOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import HelpTip from "../components/HelpTip.vue";
import VmStateTag from "../components/VmStateTag.vue";
import { HyperVService, errorMessage } from "../api";
import type { CloneRequest, VirtualSwitch } from "../api";
import { useAppStore } from "../stores/app";
import { useTaskStore } from "../stores/tasks";
import { useVMStore } from "../stores/vms";
import { formatMB, switchTypeLabel, vmStateLabel } from "../utils/format";

const router = useRouter();
const message = useMessage();
const app = useAppStore();
const vmStore = useVMStore();
const taskStore = useTaskStore();

const switches = ref<VirtualSwitch[]>([]);
const submitting = ref(false);
const loadingPath = ref(false);
const defaultsApplied = ref(false);

const form = reactive({
  sourceName: "",
  newName: "",
  targetDirectory: "",
  followSource: true,
  stopSourceIfRunning: true,
  startupMB: 4096,
  dynamic: false,
  minimumMB: 1024,
  maximumMB: 8192,
  cpuCount: 4,
  switchName: "",
  bandwidthMbps: 100,
});

const sourceVM = computed(() => vmStore.byName(form.sourceName));
const sourceIsRunning = computed(
  () => !!sourceVM.value && sourceVM.value.state !== "Off"
);

const vmOptions = computed(() =>
  vmStore.items.map((vm) => ({
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
    const directory = await HyperVService.SuggestCloneDirectory(name);
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
  const vm = vmStore.byName(name);
  if (!vm) return;
  form.newName = `${vm.name}-Clone`;
  form.startupMB = vm.memoryStartupMB;
  form.dynamic = vm.dynamicMemoryEnabled;
  form.minimumMB = vm.memoryMinimumMB || 1024;
  form.maximumMB = vm.memoryMaximumMB || Math.max(vm.memoryStartupMB, 8192);
  form.cpuCount = vm.processorCount;
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
  try {
    const directory = await HyperVService.PickDirectory("选择克隆虚拟机存放目录");
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

  submitting.value = true;
  try {
    const request: CloneRequest = {
      sourceName: form.sourceName,
      newName: form.newName.trim(),
      targetDirectory: form.targetDirectory.trim(),
      stopSourceIfRunning: form.stopSourceIfRunning,
      memory: {
        startupMB: Number(form.startupMB),
        dynamic: form.dynamic,
        minimumMB: Number(form.minimumMB),
        maximumMB: Number(form.maximumMB),
      },
      cpuCount: Number(form.cpuCount),
      switchName: form.switchName,
      bandwidthMbps: Number(form.bandwidthMbps),
    };
    const task = await HyperVService.SubmitClone(request);
    message.success(`克隆任务已提交${task ? `（${task.id}）` : ""}`);
    void taskStore.fetch();
    void router.push("/tasks");
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    submitting.value = false;
  }
}

async function loadSwitches() {
  try {
    switches.value = (await HyperVService.ListSwitches()) ?? [];
  } catch {
    switches.value = [];
  }
}

onMounted(() => {
  applyDefaults();
  void vmStore.fetch();
  void loadSwitches();
});
</script>

<template>
  <PageHeader title="克隆虚拟机" subtitle="整台复制源虚拟机，并覆盖名称、位置与硬件配置">
    <n-button size="small" @click="router.push('/vms')">返回虚拟机列表</n-button>
  </PageHeader>

  <n-alert v-if="vmStore.error" type="warning" class="inline-error" title="无法读取虚拟机列表">
    {{ vmStore.error }}
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
            <n-button size="small" :loading="loadingPath" @click="browseDirectory">
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
          克隆会重新生成虚拟机 ID 和动态 MAC 地址，避免与源虚拟机冲突。导出过程可能需要几分钟，
          取决于虚拟硬盘大小。
        </n-alert>
      </n-form>
    </div>

    <div class="summary-card">
      <div class="summary-title">克隆摘要</div>
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
  background: #10131a;
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
  color: #cfd6e4;
  margin: 16px 0 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border-soft);
}
</style>
