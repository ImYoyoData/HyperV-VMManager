<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import { FolderOutline, AddCircleOutline, ServerOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import HelpTip from "../components/HelpTip.vue";
import HostConnectModal from "../components/HostConnectModal.vue";
import { HyperVService, errorMessage } from "../api";
import type { CreateRequest, HostItem, VirtualSwitch } from "../api";
import { useAppStore } from "../stores/app";
import { useTaskStore } from "../stores/tasks";
import { useHostsStore } from "../stores/hosts";
import { formatMB, switchTypeLabel } from "../utils/format";

const router = useRouter();
const message = useMessage();
const app = useAppStore();
const taskStore = useTaskStore();
const hosts = useHostsStore();

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

const step = ref(0);
const submitting = ref(false);
const switches = ref<VirtualSwitch[]>([]);
const defaultsApplied = ref(false);
const showHostConnect = ref(false);
const connectTarget = ref<HostItem | null>(null);
const pendingSubmit = ref(false);

const form = reactive({
  name: "",
  generation: 2,
  directory: "",

  diskMode: "new" as "new" | "differencing" | "copy",
  imagePath: "",
  diskSizeGB: 60,
  isoPath: "",

  startupMB: 4096,
  dynamic: false,
  minimumMB: 1024,
  maximumMB: 8192,
  cpuCount: 4,

  switchName: "",
  bandwidthMbps: 100,
  disableCheckpoints: true,
});

function applyDefaults() {
  const settings = app.settings;
  if (!settings || defaultsApplied.value) return;
  defaultsApplied.value = true;
  form.generation = settings.defaultGeneration || 2;
  form.directory = settings.storageRoot || "";
  form.startupMB = settings.defaultMemoryStartupMB || 4096;
  form.dynamic = settings.defaultDynamicMemory;
  form.minimumMB = settings.defaultMemoryMinimumMB || 1024;
  form.maximumMB = settings.defaultMemoryMaximumMB || 8192;
  form.cpuCount = settings.defaultCPUCount || 4;
  form.bandwidthMbps = settings.defaultBandwidthMbps || 100;
  if (!form.name) {
    form.name = `NewVM-${new Date().toISOString().slice(5, 10).replace("-", "")}`;
  }
}

watch(
  () => app.settings,
  () => applyDefaults(),
  { immediate: true }
);

const switchOptions = computed(() => [
  { label: "不连接", value: "" },
  ...switches.value.map((item) => ({
    label: `${item.name}（${switchTypeLabel(item.switchType)}）`,
    value: item.name,
  })),
]);

const diskModeLabel = computed(() => {
  switch (form.diskMode) {
    case "differencing":
      return "差异磁盘";
    case "copy":
      return "完整复制";
    default:
      return "新建空硬盘";
  }
});

const finalDirectory = computed(() => {
  if (!form.directory) return "—";
  return `${form.directory.replace(/[\\/]+$/, "")}\\${form.name || "虚拟机名称"}`;
});

function validateStep(index: number): string {
  if (index === 0) {
    const name = form.name.trim();
    if (!name) return "请输入虚拟机名称";
    if (name.length > 100) return "虚拟机名称不能超过 100 个字符";
    if (/[\\/:*?"<>|]/.test(name)) return `名称不能包含 \\ / : * ? " < > | 这些字符`;
    if (!form.directory.trim()) return "请选择虚拟机存储目录";
    if (form.generation !== 1 && form.generation !== 2) return "请选择虚拟机代数";
  }
  if (index === 1) {
    if (form.diskMode === "new") {
      if (!form.diskSizeGB || form.diskSizeGB < 1) return "硬盘大小至少 1 GB";
    } else if (!form.imagePath.trim()) {
      return form.diskMode === "differencing"
        ? "请选择差异磁盘的父盘镜像"
        : "请选择要复制的系统镜像";
    }
  }
  if (index === 2) {
    if (!form.startupMB || form.startupMB < 256) return "启动内存不能小于 256 MB";
    if (form.dynamic) {
      if (!form.minimumMB || form.minimumMB < 256) return "动态内存最小值不能小于 256 MB";
      if (!form.maximumMB || form.maximumMB < 256) return "动态内存最大值不能小于 256 MB";
      if (form.minimumMB > form.startupMB) return "动态内存最小值不能大于启动内存";
      if (form.startupMB > form.maximumMB) return "启动内存不能大于动态内存最大值";
    }
    if (!form.cpuCount || form.cpuCount < 1 || form.cpuCount > 128) return "CPU 核数必须在 1 到 128 之间";
  }
  if (index === 3) {
    if (form.bandwidthMbps < 0) return "带宽不能为负数";
  }
  return "";
}

function next() {
  const error = validateStep(step.value);
  if (error) {
    message.warning(error);
    return;
  }
  if (step.value < 3) step.value += 1;
}

function previous() {
  if (step.value > 0) step.value -= 1;
}

async function browseDirectory() {
  if (isRemote.value) {
    message.warning("目标主机是远程计算机，请手动输入远程主机上的绝对路径");
    return;
  }
  try {
    const dir = await HyperVService.PickDirectoryIn(
      "选择虚拟机存储目录",
      form.directory || app.settings?.storageRoot || ""
    );
    if (dir) form.directory = dir;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function browseImage() {
  if (isRemote.value) {
    message.warning("目标主机是远程计算机，请手动输入远程主机上的镜像路径");
    return;
  }
  try {
    const file = await HyperVService.PickFileIn(
      "选择系统镜像",
      "虚拟硬盘",
      "*.vhdx;*.vhd",
      form.directory || app.settings?.storageRoot || ""
    );
    if (file) form.imagePath = file;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function browseISO() {
  if (isRemote.value) {
    message.warning("目标主机是远程计算机，请手动输入远程主机上的 ISO 路径");
    return;
  }
  try {
    const file = await HyperVService.PickFileIn(
      "选择安装 ISO",
      "镜像文件",
      "*.iso",
      form.directory || app.settings?.storageRoot || ""
    );
    if (file) form.isoPath = file;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function submit() {
  for (let index = 0; index <= 3; index += 1) {
    const error = validateStep(index);
    if (error) {
      step.value = index;
      message.warning(error);
      return;
    }
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
    const request: CreateRequest = {
      name: form.name.trim(),
      generation: form.generation,
      directory: form.directory.trim(),
      memory: {
        startupMB: Number(form.startupMB),
        dynamic: form.dynamic,
        minimumMB: Number(form.minimumMB),
        maximumMB: Number(form.maximumMB),
      },
      cpuCount: Number(form.cpuCount),
      switchName: form.switchName,
      bandwidthMbps: Number(form.bandwidthMbps),
      diskMode: form.diskMode,
      imagePath: form.imagePath.trim(),
      diskSizeGB: Number(form.diskSizeGB),
      isoPath: form.isoPath.trim(),
      disableCheckpoints: form.disableCheckpoints,
    };
    const task = await HyperVService.SubmitCreate(targetHostID.value, request);
    message.success(`新建任务已提交到 ${host.name}${task ? `（${task.id}）` : ""}`);
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

async function onTargetHostChange(value: string) {
  targetHostID.value = value;
  form.switchName = "";
  const host = hosts.items.find((item) => item.id === value);
  await ensureHostConnected(host);
  await loadSwitches();
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
      // 保存的密码失效时，继续弹出密码框让用户重新输入。
    }
  }
  connectTarget.value = host;
  showHostConnect.value = true;
  return false;
}

async function onHostConnected(host: HostItem) {
  await hosts.fetch();
  await loadSwitches();
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
  await loadSwitches();
});
</script>

<template>
  <PageHeader title="新建虚拟机" subtitle="从镜像或空硬盘创建，硬件配置可在提交前调整">
    <n-button size="small" @click="router.push('/vms')">返回虚拟机列表</n-button>
  </PageHeader>

  <n-alert v-if="isRemote" type="warning" class="inline-error" title="目标计算机是远程主机">
    新建任务会在远程主机「{{ targetHost?.name }}」上执行。存储目录、镜像和 ISO
    必须填写该远程主机上的绝对路径，本机文件选择器已禁用。
  </n-alert>

  <n-steps :current="step" size="small" style="margin-bottom: 18px">
    <n-step title="基本信息" description="名称、代数与位置" />
    <n-step title="磁盘与镜像" description="硬盘来源与安装介质" />
    <n-step title="硬件配置" description="内存与处理器" />
    <n-step title="网络与确认" description="交换机、带宽与检查点" />
  </n-steps>

  <div class="wizard-layout">
    <div class="panel step-panel">
      <template v-if="step === 0">
        <n-form label-placement="top" size="small">
          <div class="form-grid">
            <n-form-item label="虚拟机名称">
              <n-input v-model:value="form.name" placeholder="例如 Win11-Test" />
            </n-form-item>
            <n-form-item>
              <template #label>
                虚拟机代数
                <HelpTip>
                  <p><strong>第 2 代（UEFI）</strong>：使用 UEFI 启动、GPT 分区，支持 Secure Boot，适合 Windows 8/10/11 和现代 Linux。新系统优先选择。</p>
                  <p><strong>第 1 代（BIOS）</strong>：使用传统 BIOS 启动、MBR 分区，适合 Windows 7 等老系统，或需要从旧机器迁移。</p>
                  <p>代数创建后不能直接修改，选错需要重新创建虚拟机。</p>
                </HelpTip>
              </template>
              <n-radio-group v-model:value="form.generation">
                <n-radio-button :value="2">第 2 代（UEFI，推荐）</n-radio-button>
                <n-radio-button :value="1">第 1 代（BIOS）</n-radio-button>
              </n-radio-group>
            </n-form-item>
          </div>
          <n-form-item>
            <template #label>
              目标计算机
              <HelpTip>
                <p>新建任务会提交到所选计算机上执行，不会切换当前管理主机。</p>
                <p>只有已经连接的主机可以被选择；未连接的主机请先到“计算机”页面连接。</p>
              </HelpTip>
            </template>
            <n-select
              v-model:value="targetHostID"
              :options="hostOptions"
              placeholder="选择目标计算机"
              @update:value="onTargetHostChange"
            />
          </n-form-item>
          <n-form-item label="存储根目录">
            <div class="input-with-button">
              <n-input v-model:value="form.directory" placeholder="例如 D:\VMs" />
              <n-button size="small" :disabled="isRemote" @click="browseDirectory">
                <template #icon>
                  <n-icon :component="FolderOutline" />
                </template>
                浏览
              </n-button>
            </div>
          </n-form-item>
          <div class="form-hint">
            虚拟机最终目录：<span class="mono-text">{{ finalDirectory }}</span>
          </div>
        </n-form>
      </template>

      <template v-else-if="step === 1">
        <n-form label-placement="top" size="small">
          <n-form-item>
            <template #label>
              磁盘方式
              <HelpTip>
                <p><strong>新建空硬盘</strong>：创建一块动态扩展的空白 VHDX，配合下方 ISO 全新安装系统。</p>
                <p><strong>差异磁盘</strong>：以现有镜像为父盘，只记录后续改动。占空间小、创建快，但父盘必须保持不变。</p>
                <p><strong>完整复制</strong>：生成独立副本，占用空间约等于镜像大小，适合需要独立修改的场景。</p>
              </HelpTip>
            </template>
            <n-radio-group v-model:value="form.diskMode">
              <n-radio-button value="new">新建空硬盘</n-radio-button>
              <n-radio-button value="differencing">差异磁盘（推荐）</n-radio-button>
              <n-radio-button value="copy">完整复制</n-radio-button>
            </n-radio-group>
          </n-form-item>

          <n-form-item v-if="form.diskMode === 'new'" label="空硬盘大小（GB）">
            <n-input-number v-model:value="form.diskSizeGB" :min="1" :max="65536" style="width: 220px" />
          </n-form-item>

          <n-form-item v-else :label="form.diskMode === 'differencing' ? '父盘镜像（.vhdx / .vhd）' : '系统镜像（.vhdx / .vhd）'">
            <div class="input-with-button">
              <n-input v-model:value="form.imagePath" placeholder="选择已有的虚拟硬盘文件" />
              <n-button size="small" :disabled="isRemote" @click="browseImage">
                <template #icon>
                  <n-icon :component="ServerOutline" />
                </template>
                浏览
              </n-button>
            </div>
          </n-form-item>

          <n-form-item>
            <template #label>
              安装 ISO（可选）
              <HelpTip>
                <p>选择系统安装镜像（.iso）。创建任务会自动挂载到虚拟光驱，并把光驱设为第一启动项。</p>
                <p>安装完成后可以回到虚拟机编辑面板卸载 ISO，或调整启动顺序从硬盘启动。</p>
              </HelpTip>
            </template>
            <div class="input-with-button">
              <n-input v-model:value="form.isoPath" placeholder="选择系统安装 ISO，留空则不挂载" />
              <n-button size="small" :disabled="isRemote" @click="browseISO">
                <template #icon>
                  <n-icon :component="AddCircleOutline" />
                </template>
                浏览
              </n-button>
            </div>
          </n-form-item>

          <n-alert type="info" :show-icon="true">
            <div class="hint-lines">
              <div>差异磁盘基于父盘建立，占用空间小、创建速度快，但父盘必须保持不变。</div>
              <div>完整复制会生成独立副本，占用空间等于镜像大小，适合需要独立修改的场景。</div>
              <div v-if="form.isoPath">创建后会自动挂载 ISO，并将光驱设为第一启动项。</div>
            </div>
          </n-alert>
        </n-form>
      </template>

      <template v-else-if="step === 2">
        <n-form label-placement="top" size="small">
          <div class="form-grid">
            <n-form-item>
              <template #label>
                启动内存（MB）
                <HelpTip>
                  <p>虚拟机启动时分配的内存。未开启动态内存时，虚拟机将固定使用这个值。</p>
                  <p>如果开启动态内存，启动内存应位于最小值和最大值之间，Hyper-V 会根据负载自动调整。</p>
                </HelpTip>
              </template>
              <n-input-number v-model:value="form.startupMB" :min="256" :step="512" style="width: 100%" />
            </n-form-item>
            <n-form-item label="CPU 核数">
              <n-input-number v-model:value="form.cpuCount" :min="1" :max="128" style="width: 100%" />
            </n-form-item>
            <n-form-item>
              <template #label>
                动态内存
                <HelpTip>
                  <p>关闭：固定使用启动内存，行为最可预测，适合游戏或对内存敏感的负载。</p>
                  <p>开启：在最小值与最大值之间按负载动态调整。来宾系统需要安装 Hyper-V 集成服务；Windows 通常自带。适合多台虚拟机共用主机内存。</p>
                </HelpTip>
              </template>
              <n-switch v-model:value="form.dynamic" />
            </n-form-item>
          </div>
          <div v-if="form.dynamic" class="form-grid">
            <n-form-item label="动态内存最小值（MB）">
              <n-input-number v-model:value="form.minimumMB" :min="256" :step="512" style="width: 100%" />
            </n-form-item>
            <n-form-item label="动态内存最大值（MB）">
              <n-input-number v-model:value="form.maximumMB" :min="256" :step="512" style="width: 100%" />
            </n-form-item>
          </div>
          <div class="form-hint">
            当前配置：{{ formatMB(form.startupMB) }} 启动内存，{{ form.cpuCount }} 个虚拟处理器。
          </div>
        </n-form>
      </template>

      <template v-else>
        <n-form label-placement="top" size="small">
          <div class="form-grid">
            <n-form-item>
              <template #label>
                虚拟交换机
                <HelpTip>
                  <p><strong>外部（桥接）</strong>：连接物理网卡，虚拟机和主机都能访问局域网与互联网。</p>
                  <p><strong>内部</strong>：仅虚拟机与主机互通，适合在主机上做代理或隔离测试。</p>
                  <p><strong>专用</strong>：仅虚拟机之间互通，主机不参与。</p>
                  <p>创建时也可以留空，之后再在虚拟机编辑面板中连接。</p>
                </HelpTip>
              </template>
              <n-select v-model:value="form.switchName" :options="switchOptions" />
            </n-form-item>
            <n-form-item label="带宽上限（Mbps，0 = 不限）">
              <n-input-number v-model:value="form.bandwidthMbps" :min="0" :max="100000" style="width: 100%" />
            </n-form-item>
            <n-form-item>
              <template #label>
                禁用检查点
                <HelpTip>
                  <p>检查点用于保存虚拟机某一时刻的完整状态，之后可以回滚。</p>
                  <p>禁用检查点可以减少磁盘占用、避免快照链过长导致性能下降；需要时可以在编辑面板重新开启。</p>
                </HelpTip>
              </template>
              <n-switch v-model:value="form.disableCheckpoints" />
            </n-form-item>
          </div>
          <n-alert type="info" :show-icon="true">
            网络交换机可以为空，创建后再到编辑面板连接。检查点默认禁用，可在编辑中重新开启。
          </n-alert>
        </n-form>
      </template>

      <div class="steps-actions">
        <n-button size="small" :disabled="step === 0" @click="previous">上一步</n-button>
        <n-space>
          <n-button v-if="step < 3" size="small" type="primary" @click="next">下一步</n-button>
          <n-button v-else size="small" type="primary" :loading="submitting" @click="submit">
            提交新建任务
          </n-button>
        </n-space>
      </div>
    </div>

    <div class="summary-card">
      <div class="summary-title">配置摘要</div>
      <div class="summary-row">
        <span class="key">目标计算机</span>
        <span class="value">{{ targetHost?.name || "—" }}<template v-if="isRemote">（远程）</template></span>
      </div>
      <div class="summary-row">
        <span class="key">名称</span>
        <span class="value">{{ form.name || "—" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">代数</span>
        <span class="value">第 {{ form.generation }} 代</span>
      </div>
      <div class="summary-row">
        <span class="key">目录</span>
        <span class="value mono-text">{{ finalDirectory }}</span>
      </div>
      <div class="summary-row">
        <span class="key">磁盘</span>
        <span class="value">
          {{ diskModeLabel }}
          <template v-if="form.diskMode === 'new'"> · {{ form.diskSizeGB }} GB</template>
        </span>
      </div>
      <div v-if="form.imagePath" class="summary-row">
        <span class="key">镜像</span>
        <span class="value mono-text">{{ form.imagePath }}</span>
      </div>
      <div v-if="form.isoPath" class="summary-row">
        <span class="key">安装 ISO</span>
        <span class="value mono-text">{{ form.isoPath }}</span>
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
        <span class="value">{{ form.switchName || "不连接" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">带宽</span>
        <span class="value">{{ form.bandwidthMbps > 0 ? `${form.bandwidthMbps} Mbps` : "不限" }}</span>
      </div>
      <div class="summary-row">
        <span class="key">检查点</span>
        <span class="value">{{ form.disableCheckpoints ? "禁用" : "启用" }}</span>
      </div>

      <n-button
        type="primary"
        block
        size="small"
        style="margin-top: 14px"
        :loading="submitting"
        @click="submit"
      >
        提交新建任务
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

.hint-lines {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.6;
}
</style>
