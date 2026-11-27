<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useDialog, useMessage } from "naive-ui";
import {
  AddCircleOutline,
  CopyOutline,
  FolderOutline,
  GlobeOutline,
  RefreshOutline,
  TrashOutline,
} from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import HelpTip from "../components/HelpTip.vue";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type {
  APIKey,
  APIKeyRequest,
  HostItem,
  NATNetwork,
  Settings,
  TunnelStatus,
} from "../api";
import { useAppStore } from "../stores/app";
import { useHostsStore } from "../stores/hosts";
import { useThemeStore, type ThemeMode } from "../stores/theme";

const app = useAppStore();
const hosts = useHostsStore();
const themes = useThemeStore();
const message = useMessage();
const dialog = useDialog();

const repository = "https://github.com/ImYoyoData/HyperV-VMManager";
const author = "Yoyo";

const saving = ref(false);
const loadingKeys = ref(false);
const keys = ref<APIKey[]>([]);
const revealedTokens = ref<Set<string>>(new Set());

const showKeyModal = ref(false);
const savingKey = ref(false);
const editingKey = ref<APIKey | null>(null);
const keyForm = reactive({
  name: "",
  permission: "read",
  scope: "all" as "all" | "selected",
  hostIDs: [] as string[],
});

const permissionOptions = [
  { label: "只读（查询虚拟机、任务、主机）", value: "read" },
  { label: "读写（含创建、克隆、编辑、删除、电源）", value: "write" },
  { label: "管理员（含主机与设置管理）", value: "admin" },
];

const tunnelProtocolOptions = [
  { label: "TCP（默认，最稳定）", value: "tcp" },
  { label: "WebSocket（可穿过 HTTP 代理）", value: "websocket" },
  { label: "WSS（WebSocket + TLS）", value: "wss" },
  { label: "KCP（UDP 加速）", value: "kcp" },
  { label: "QUIC（UDP + TLS）", value: "quic" },
];

const hostOptions = computed(() =>
  hosts.items.map((item: HostItem) => ({
    label: item.isLocal ? `${item.name}（本机）` : `${item.name}（${item.address}）`,
    value: item.id,
  }))
);

const form = reactive({
  storageRoot: "",
  logDir: "",
  defaultGeneration: 2,
  defaultMemoryStartupMB: 4096,
  defaultDynamicMemory: false,
  defaultMemoryMinimumMB: 1024,
  defaultMemoryMaximumMB: 8192,
  defaultCPUCount: 4,
  defaultBandwidthMbps: 100,
  autoRefreshSeconds: 10,
  theme: "system",
  closeBehavior: "ask",
  autoStart: false,
  web: {
    enabled: true,
    listenAddress: "127.0.0.1",
    port: 17890,
    username: "admin",
    password: "",
  },
  tunnel: {
    enabled: false,
    serverAddr: "",
    serverPort: 7000,
    token: "",
    protocol: "tcp",
    proxyName: "hvvm-web",
    remotePort: 17890,
    mappings: [] as TunnelMappingForm[],
  },
});

const tunnelStatus = ref<TunnelStatus | null>(null);
const tunnelLoading = ref(false);
let tunnelTimer: ReturnType<typeof setInterval> | undefined;

const natNetworks = ref<NATNetwork[]>([]);
const natLoading = ref(false);
const selectedNATMapping = ref<string | null>(null);

interface TunnelMappingForm {
  id?: string;
  enabled: boolean;
  name: string;
  localIP: string;
  localPort: number;
  remotePort: number;
}

const latestVersion = ref("");
const checkingUpdate = ref(false);

function copySettings(value: Settings): Settings {
  return {
    ...value,
    web: { ...value.web },
    tunnel: {
      ...value.tunnel,
      mappings: (value.tunnel.mappings ?? []).map((mapping) => ({ ...mapping })),
    },
    apiKeys: null,
  };
}

watch(
  () => app.settings,
  (settings) => {
    if (!settings) return;
    Object.assign(form, copySettings(settings));
    form.web = { ...settings.web };
    form.tunnel = {
      ...settings.tunnel,
      mappings: (settings.tunnel.mappings ?? []).map((mapping) => ({ ...mapping })),
    };
  },
  { immediate: true, deep: true }
);

async function save() {
  saving.value = true;
  try {
    const value: Settings = {
      storageRoot: form.storageRoot,
      logDir: form.logDir,
      defaultGeneration: form.defaultGeneration,
      defaultMemoryStartupMB: form.defaultMemoryStartupMB,
      defaultDynamicMemory: form.defaultDynamicMemory,
      defaultMemoryMinimumMB: form.defaultMemoryMinimumMB,
      defaultMemoryMaximumMB: form.defaultMemoryMaximumMB,
      defaultCPUCount: form.defaultCPUCount,
      defaultBandwidthMbps: form.defaultBandwidthMbps,
      autoRefreshSeconds: form.autoRefreshSeconds,
      theme: form.theme,
      closeBehavior: form.closeBehavior,
      autoStart: form.autoStart,
      web: { ...form.web },
      tunnel: {
        ...form.tunnel,
        mappings: form.tunnel.mappings.map((mapping) => ({ ...mapping })),
      },
      apiKeys: null,
    };
    await app.saveSettings(value);
    themes.setMode(form.theme as ThemeMode);
    message.success("设置已保存");
    if (form.web.enabled) {
      message.info(`网页控制台地址：http://${form.web.listenAddress}:${form.web.port}`, {
        duration: 5000,
      });
    }
    if (!isWebMode && form.tunnel.enabled) {
      window.setTimeout(() => void loadTunnelStatus(), 1200);
    }
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    saving.value = false;
  }
}

async function browseStorageRoot() {
  try {
    const directory = await HyperVService.PickDirectoryIn(
      "选择默认虚拟机存储目录",
      form.storageRoot
    );
    if (directory) form.storageRoot = directory;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function browseLogDir() {
  try {
    const directory = await HyperVService.PickDirectoryIn(
      "选择日志与任务历史目录",
      form.logDir
    );
    if (directory) form.logDir = directory;
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function openLogDir() {
  try {
    await HyperVService.OpenLogDir();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function copyText(value: string, label = "内容") {
  try {
    await navigator.clipboard.writeText(value);
    message.success(`${label}已复制`);
  } catch {
    message.warning("无法访问剪贴板，请手动选择复制");
  }
}

function regenerateWebPassword() {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  form.web.password = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  message.info("已生成新密码，保存后生效");
}

async function loadTunnelStatus() {
  if (isWebMode) return;
  try {
    tunnelStatus.value = await HyperVService.GetTunnelStatus();
  } catch {
    // 状态查询失败不打断设置页
  }
}

async function restartTunnel() {
  tunnelLoading.value = true;
  try {
    const status = await HyperVService.RestartTunnel();
    tunnelStatus.value = status;
    if (status.error) {
      message.error(status.error);
    } else {
      message.success("已重新连接 frps，状态稍后刷新");
    }
    window.setTimeout(() => void loadTunnelStatus(), 1500);
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    tunnelLoading.value = false;
  }
}

const tunnelPublicAddress = computed(() => {
  const status = tunnelStatus.value;
  if (!status) return "";
  if (status.remoteAddr) return status.remoteAddr;
  if (status.serverAddr && form.tunnel.remotePort > 0) {
    return `${status.serverAddr.split(":")[0]}:${form.tunnel.remotePort}`;
  }
  return "";
});

function addMapping() {
  form.tunnel.mappings.push({
    enabled: true,
    name: `map-${form.tunnel.mappings.length + 1}`,
    localIP: "",
    localPort: 3389,
    remotePort: 0,
  });
}

function removeMapping(index: number) {
  form.tunnel.mappings.splice(index, 1);
}

const natMappingOptions = computed(() => {
  const options: {
    label: string;
    value: string;
    hostIP: string;
    externalPort: number;
    name: string;
  }[] = [];
  const isLocalHost = !app.activeHost || app.activeHost.isLocal;
  for (const nat of natNetworks.value) {
    // 本机运行时 frpc 用 127.0.0.1 访问 portproxy 即可；
    // 远程主机时使用对方报告的局域网 IP，仍要求本机能访问到它。
    const targetHost = isLocalHost
      ? "127.0.0.1"
      : nat.externalIP || nat.hostIP || "127.0.0.1";
    for (const mapping of nat.staticMappings ?? []) {
      options.push({
        label: `${nat.name} · ${mapping.protocol} ${mapping.externalPort} → ${mapping.internalIPAddress}:${mapping.internalPort}`,
        value: `${nat.name}|${mapping.protocol}|${mapping.externalPort}`,
        hostIP: targetHost,
        externalPort: mapping.externalPort,
        name: `${nat.name}-${mapping.externalPort}-${mapping.protocol.toLowerCase()}`,
      });
    }
  }
  return options;
});

async function loadNATNetworks() {
  if (isWebMode) return;
  natLoading.value = true;
  try {
    natNetworks.value = (await HyperVService.ListNATNetworks()) ?? [];
  } catch {
    natNetworks.value = [];
  } finally {
    natLoading.value = false;
  }
}

function addMappingFromNAT() {
  const option = natMappingOptions.value.find(
    (item) => item.value === selectedNATMapping.value
  );
  if (!option) return;
  form.tunnel.mappings.push({
    enabled: true,
    name: option.name,
    localIP: option.hostIP,
    localPort: option.externalPort,
    remotePort: 0,
  });
  selectedNATMapping.value = null;
  message.success("已添加 NAT 转发映射，保存设置后生效");
}

const tunnelStatusText = computed(() => {
  const status = tunnelStatus.value;
  if (!status || !status.enabled) return "未启用";
  if (status.error) return "连接异常";
  if (status.connected) return "已连接";
  if (status.running) return "连接中";
  return "未运行";
});

const tunnelStatusType = computed<"default" | "success" | "warning" | "error">(() => {
  const status = tunnelStatus.value;
  if (!status || !status.enabled) return "default";
  if (status.error) return "error";
  if (status.connected) return "success";
  return "warning";
});

function changeTheme(mode: ThemeMode) {
  form.theme = mode;
  themes.setMode(mode);
}

async function loadKeys() {
  if (isWebMode) return;
  loadingKeys.value = true;
  try {
    keys.value = (await HyperVService.ListAPIKeys()) ?? [];
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loadingKeys.value = false;
  }
}

function openCreateKey() {
  editingKey.value = null;
  keyForm.name = "";
  keyForm.permission = "read";
  keyForm.scope = "all";
  keyForm.hostIDs = [];
  showKeyModal.value = true;
}

function openEditKey(key: APIKey) {
  editingKey.value = key;
  keyForm.name = key.name;
  keyForm.permission = key.permission;
  keyForm.scope = key.hostIDs && key.hostIDs.length > 0 ? "selected" : "all";
  keyForm.hostIDs = [...(key.hostIDs ?? [])];
  showKeyModal.value = true;
}

async function saveKey() {
  if (!keyForm.name.trim()) {
    message.warning("请输入 Key 名称");
    return;
  }
  if (keyForm.scope === "selected" && keyForm.hostIDs.length === 0) {
    message.warning("请选择允许管理的计算机");
    return;
  }
  savingKey.value = true;
  try {
    const request: APIKeyRequest = {
      id: editingKey.value?.id,
      name: keyForm.name.trim(),
      permission: keyForm.permission,
      hostIDs: keyForm.scope === "all" ? [] : keyForm.hostIDs,
    };
    if (editingKey.value) {
      await HyperVService.UpdateAPIKey(request);
      message.success("API Key 已更新");
    } else {
      const created = await HyperVService.CreateAPIKey(request);
      if (created?.token) {
        await copyText(created.token, "新 Key 的 Token");
      }
      message.success("API Key 已创建");
    }
    showKeyModal.value = false;
    await loadKeys();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    savingKey.value = false;
  }
}

function confirmDeleteKey(key: APIKey) {
  dialog.warning({
    title: "删除 API Key",
    content: `确定删除「${key.name}」吗？使用该 Key 的自动化脚本会立即失效。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await HyperVService.DeleteAPIKey(key.id);
        message.success("API Key 已删除");
        await loadKeys();
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

function confirmRegenerateToken(key: APIKey) {
  dialog.warning({
    title: "重新生成 Token",
    content: `重新生成「${key.name}」的 Token 后，旧 Token 会立即失效。`,
    positiveText: "重新生成",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const updated = await HyperVService.RegenerateAPIKeyToken(key.id);
        if (updated?.token) {
          await copyText(updated.token, "新 Token");
        }
        message.success("Token 已重新生成");
        await loadKeys();
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

function toggleToken(id: string) {
  const next = new Set(revealedTokens.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  revealedTokens.value = next;
}

function maskedToken(token: string): string {
  if (token.length <= 8) return "••••••••";
  return `${token.slice(0, 4)}••••••••${token.slice(-4)}`;
}

function keyScopeLabel(key: APIKey): string {
  if (!key.hostIDs || key.hostIDs.length === 0) return "全部计算机";
  return key.hostIDs
    .map((id) => hosts.items.find((item) => item.id === id)?.name ?? id)
    .join("、");
}

function permissionLabel(permission: string): string {
  switch (permission) {
    case "admin":
      return "管理员";
    case "write":
      return "读写";
    default:
      return "只读";
  }
}

function permissionType(permission: string): "default" | "info" | "warning" | "success" | "error" {
  switch (permission) {
    case "admin":
      return "error";
    case "write":
      return "warning";
    default:
      return "default";
  }
}

async function checkUpdate() {
  checkingUpdate.value = true;
  try {
    const response = await fetch(
      "https://api.github.com/repos/ImYoyoData/HyperV-VMManager/releases/latest",
      { headers: { Accept: "application/vnd.github+json" } }
    );
    if (!response.ok) {
      throw new Error(`GitHub 返回 ${response.status}`);
    }
    const data = (await response.json()) as { tag_name?: string; html_url?: string };
    const tag = data.tag_name ?? "";
    latestVersion.value = tag;
    const normalize = (value: string) => value.replace(/^v/i, "").trim();
    if (tag && normalize(tag) !== normalize(app.appVersion)) {
      message.info(`发现新版本 ${tag}`);
    } else {
      message.success("当前已是最新版本");
    }
  } catch (err) {
    message.error(`检查更新失败：${errorMessage(err)}`);
  } finally {
    checkingUpdate.value = false;
  }
}

function openRepository() {
  window.open(repository, "_blank");
}

onMounted(() => {
  void hosts.fetch();
  void loadKeys();
  void loadTunnelStatus();
  void loadNATNetworks();
  tunnelTimer = setInterval(() => void loadTunnelStatus(), 5000);
});

onUnmounted(() => {
  if (tunnelTimer) clearInterval(tunnelTimer);
});
</script>

<template>
  <PageHeader title="设置" subtitle="默认硬件参数、网页控制台、API Key 与关于">
    <n-button size="small" type="primary" :loading="saving" @click="save">保存设置</n-button>
  </PageHeader>

  <div class="dashboard-grid">
    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">默认虚拟机配置</div>
          <div class="card-subtitle">新建与克隆向导的初始值，对应原脚本的 $defaults</div>
        </div>
      </div>

      <n-form label-placement="top" size="small">
        <n-form-item label="默认存储根目录">
          <div class="input-with-button">
            <n-input v-model:value="form.storageRoot" placeholder="例如 D:\VMs" />
            <n-button size="small" :disabled="isWebMode" @click="browseStorageRoot">
              <template #icon>
                <n-icon :component="FolderOutline" />
              </template>
              浏览
            </n-button>
          </div>
        </n-form-item>

        <n-form-item label="日志与任务队列目录">
          <div class="input-with-button">
            <n-input v-model:value="form.logDir" placeholder="例如 D:\HyperV-VMManager\logs" />
            <n-button size="small" :disabled="isWebMode" @click="browseLogDir">
              <template #icon>
                <n-icon :component="FolderOutline" />
              </template>
              浏览
            </n-button>
            <n-button size="small" :disabled="isWebMode" @click="openLogDir">打开</n-button>
          </div>
        </n-form-item>
        <div class="form-hint" style="margin: -4px 0 10px">
          应用配置与主机列表保存在系统用户目录（%AppData%\HyperV-VMManager）。
          日志和任务历史默认优先写入 D/E/F 等非系统盘；只有 C 盘时自动放回 AppData。
        </div>

        <div class="form-grid">
          <n-form-item label="默认代数">
            <n-radio-group v-model:value="form.defaultGeneration">
              <n-radio-button :value="2">第 2 代</n-radio-button>
              <n-radio-button :value="1">第 1 代</n-radio-button>
            </n-radio-group>
          </n-form-item>
          <n-form-item label="默认 CPU 核数">
            <n-input-number v-model:value="form.defaultCPUCount" :min="1" :max="128" style="width: 100%" />
          </n-form-item>
          <n-form-item label="默认启动内存（MB）">
            <n-input-number v-model:value="form.defaultMemoryStartupMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item label="默认动态内存">
            <n-switch v-model:value="form.defaultDynamicMemory" />
          </n-form-item>
          <n-form-item v-if="form.defaultDynamicMemory" label="动态内存最小值（MB）">
            <n-input-number v-model:value="form.defaultMemoryMinimumMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item v-if="form.defaultDynamicMemory" label="动态内存最大值（MB）">
            <n-input-number v-model:value="form.defaultMemoryMaximumMB" :min="256" :step="512" style="width: 100%" />
          </n-form-item>
          <n-form-item label="默认带宽上限（Mbps，0 = 不限）">
            <n-input-number v-model:value="form.defaultBandwidthMbps" :min="0" :max="100000" style="width: 100%" />
          </n-form-item>
          <n-form-item label="虚拟机列表自动刷新（秒）">
            <n-input-number v-model:value="form.autoRefreshSeconds" :min="2" :max="300" style="width: 100%" />
          </n-form-item>
        </div>
      </n-form>

      <div class="form-hint">
        应用数据目录：<span class="mono-text">{{ app.dataDir || "—" }}</span>
      </div>
    </div>

    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">外观与关闭行为</div>
          <div class="card-subtitle">主题默认跟随系统；关闭行为可在首次关闭时选择后记住</div>
        </div>
      </div>

      <n-form label-placement="top" size="small">
        <n-form-item label="主题">
          <n-radio-group :value="form.theme" @update:value="changeTheme">
            <n-radio-button value="system">跟随系统</n-radio-button>
            <n-radio-button value="light">浅色</n-radio-button>
            <n-radio-button value="dark">暗色</n-radio-button>
          </n-radio-group>
        </n-form-item>
        <n-form-item>
          <template #label>
            关闭窗口时
            <HelpTip>
              <p><strong>每次询问</strong>：第一次关闭时弹窗选择，选择后会记住。</p>
              <p><strong>最小化到托盘</strong>：关闭按钮只隐藏窗口，应用继续在托盘运行。</p>
              <p><strong>直接关闭</strong>：关闭窗口即退出应用。</p>
            </HelpTip>
          </template>
          <n-select
            v-model:value="form.closeBehavior"
            :options="[
              { label: '每次询问（首次关闭时选择）', value: 'ask' },
              { label: '最小化到系统托盘', value: 'tray' },
              { label: '直接关闭并退出', value: 'close' },
            ]"
          />
        </n-form-item>
        <n-form-item>
          <template #label>
            开机自启动
            <HelpTip>
              <p>开启后会把当前程序路径写入当前用户的 Windows 启动项。</p>
              <p>移动程序文件后需要重新保存一次设置，或关闭再开启此选项。</p>
            </HelpTip>
          </template>
          <n-switch v-model:value="form.autoStart" />
        </n-form-item>

        <n-divider style="margin: 6px 0 16px" />

        <div class="panel-header" style="margin-bottom: 10px">
          <div>
            <div class="card-title">网页控制台</div>
            <div class="card-subtitle">浏览器访问地址与登录账号，默认开启，仅监听本机</div>
          </div>
          <div class="spacer"></div>
          <n-tag
            size="small"
            :type="app.api?.running ? 'success' : app.api?.enabled ? 'warning' : 'default'"
            :bordered="false"
          >
            {{ app.api?.running ? "运行中" : app.api?.enabled ? "未运行" : "已关闭" }}
          </n-tag>
        </div>

        <div class="form-grid">
          <n-form-item label="启用网页控制台">
            <n-switch v-model:value="form.web.enabled" />
          </n-form-item>
          <n-form-item label="监听地址">
            <n-input v-model:value="form.web.listenAddress" placeholder="127.0.0.1" />
          </n-form-item>
          <n-form-item label="端口">
            <n-input-number v-model:value="form.web.port" :min="1" :max="65535" style="width: 100%" />
          </n-form-item>
          <n-form-item label="登录用户名">
            <n-input v-model:value="form.web.username" placeholder="admin" />
          </n-form-item>
        </div>
        <n-form-item label="登录密码">
          <div class="input-with-button">
            <n-input
              v-model:value="form.web.password"
              type="password"
              show-password-on="click"
              placeholder="留空表示保持当前密码不变"
            />
            <n-button size="small" @click="copyText(form.web.password, '网页控制台密码')">
              复制
            </n-button>
            <n-button size="small" @click="regenerateWebPassword">随机生成</n-button>
          </div>
        </n-form-item>

        <n-alert type="info" :show-icon="true">
          默认只监听 127.0.0.1。若改为 0.0.0.0 暴露到局域网，请务必使用强密码；公网访问请配合
          HTTPS、反向代理或内网穿透，不要直接暴露明文端口。
        </n-alert>
        <div v-if="app.api?.error" class="form-hint" style="color: var(--danger)">
          {{ app.api.error }}
        </div>
        <div v-if="app.api?.running" class="form-hint">
          地址：<span class="mono-text">http://{{ form.web.listenAddress }}:{{ form.web.port }}</span>
        </div>
      </n-form>
    </div>
  </div>

  <div v-if="!isWebMode" class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">内网穿透（frp）</div>
        <div class="card-subtitle">
          把本机网页控制台映射到公网 frps 服务器；frpc 已编译进应用，不需要额外下载或安装
        </div>
      </div>
      <div class="spacer"></div>
      <n-tag size="small" :type="tunnelStatusType" :bordered="false">
        {{ tunnelStatusText }}
      </n-tag>
      <n-button size="small" @click="$router.push('/tunnel')">
        独立页面
      </n-button>
      <n-button
        size="small"
        :loading="tunnelLoading"
        :disabled="!form.tunnel.enabled"
        @click="restartTunnel"
      >
        重新连接
      </n-button>
    </div>

    <n-form label-placement="top" size="small">
      <div class="form-grid">
        <n-form-item label="启用内网穿透">
          <n-switch v-model:value="form.tunnel.enabled" />
        </n-form-item>
        <n-form-item label="传输协议">
          <n-select v-model:value="form.tunnel.protocol" :options="tunnelProtocolOptions" />
        </n-form-item>
        <n-form-item>
          <template #label>
            frps 服务器地址
            <HelpTip>
              <p>公网服务器上运行的 frps 地址，例如 <code>frp.example.com</code> 或 <code>1.2.3.4</code>。</p>
              <p>frps 与 frpc 使用同一个认证 Token；请确保服务器防火墙放行 frps 的 bindPort（默认 7000）和远程端口。</p>
              <p>frp 是 Apache-2.0 开源项目，此处使用的是官方 Go 客户端库，已随应用一起编译，不需要 frpc.exe。</p>
            </HelpTip>
          </template>
          <n-input v-model:value="form.tunnel.serverAddr" placeholder="例如 frp.example.com" />
        </n-form-item>
        <n-form-item label="frps 端口">
          <n-input-number
            v-model:value="form.tunnel.serverPort"
            :min="1"
            :max="65535"
            style="width: 100%"
          />
        </n-form-item>
        <n-form-item label="认证 Token">
          <n-input
            v-model:value="form.tunnel.token"
            type="password"
            show-password-on="click"
            placeholder="与 frps 的 auth.token 保持一致"
          />
        </n-form-item>
        <n-form-item>
          <template #label>
            远程端口
            <HelpTip>
              <p>frps 服务器上对外暴露的端口，访问 <span class="mono-text">http://服务器地址:远程端口</span> 即可打开网页控制台。</p>
              <p>填 0 表示由 frps 随机分配；这要求服务端配置了 allowPorts。固定端口更便于记住和配置防火墙。</p>
            </HelpTip>
          </template>
          <n-input-number
            v-model:value="form.tunnel.remotePort"
            :min="0"
            :max="65535"
            style="width: 100%"
          />
        </n-form-item>
        <n-form-item label="代理名称">
          <n-input v-model:value="form.tunnel.proxyName" placeholder="hvvm-web" />
        </n-form-item>
      </div>
    </n-form>

    <div class="tunnel-section">
      <div class="tunnel-section-head">
        <div>
          <div class="tunnel-section-title">从 NAT 端口转发添加</div>
          <div class="form-hint">
            先在“虚拟交换机”页面创建 Internal + NAT 和端口转发，再在这里一键生成 frp 映射。
          </div>
        </div>
        <n-button size="small" :loading="natLoading" @click="loadNATNetworks">
          刷新 NAT
        </n-button>
      </div>
      <div class="mapping-row">
        <n-select
          v-model:value="selectedNATMapping"
          :options="natMappingOptions"
          placeholder="选择 NAT 端口转发"
          clearable
          style="flex: 1"
        />
        <n-button
          size="small"
          type="primary"
          :disabled="!selectedNATMapping"
          @click="addMappingFromNAT"
        >
          添加为 frp 映射
        </n-button>
      </div>
      <div v-if="natMappingOptions.length === 0" class="form-hint">
        没有可用的 NAT 端口转发，或者当前连接的主机还没有配置 NAT。
      </div>
      <n-alert
        v-if="app.activeHost && !app.activeHost.isLocal"
        type="warning"
        :show-icon="true"
        style="margin-top: 10px"
      >
        当前管理的是远程主机。内置 frpc 在本机运行，目标地址必须是本机可达的远程宿主机 IP；
        如果无法直连，请把 frpc 部署到远程宿主机，或使用可路由的远程地址。
      </n-alert>
    </div>

    <div class="tunnel-section">
      <div class="tunnel-section-head">
        <div>
          <div class="tunnel-section-title">自定义端口映射</div>
          <div class="form-hint">
            可把虚拟机内的服务端口（如 3389 / 22 / 80）映射到 frps。
            目标 IP 必须是运行本应用的电脑能够直接访问的地址。
          </div>
        </div>
        <n-button size="small" dashed @click="addMapping">添加映射</n-button>
      </div>
      <div v-if="form.tunnel.mappings.length === 0" class="empty-hint">
        还没有自定义端口映射
      </div>
      <div
        v-for="(mapping, index) in form.tunnel.mappings"
        :key="mapping.id || index"
        class="mapping-row"
      >
        <n-switch v-model:value="mapping.enabled" size="small" />
        <n-input v-model:value="mapping.name" placeholder="名称" style="width: 110px" />
        <n-input v-model:value="mapping.localIP" placeholder="目标 IP（虚拟机 IP）" style="flex: 1" />
        <n-input-number
          v-model:value="mapping.localPort"
          :min="1"
          :max="65535"
          placeholder="目标端口"
          style="width: 120px"
        />
        <span class="mapping-arrow">→</span>
        <n-input-number
          v-model:value="mapping.remotePort"
          :min="0"
          :max="65535"
          placeholder="远程端口"
          style="width: 120px"
        />
        <n-button size="tiny" quaternary type="error" @click="removeMapping(index)">删除</n-button>
      </div>
      <n-alert type="warning" :show-icon="true" style="margin-top: 10px">
        如果虚拟机在远程 Hyper-V 主机的 NAT / 内部网络中，本机无法直接访问它的 IP；
        需要先在远程主机上做端口转发，或把 frpc 部署到远程主机。
        本应用内置的 frpc 只能连接当前电脑可访问的地址。
      </n-alert>
    </div>

    <n-alert v-if="tunnelStatus?.error" type="error" :show-icon="true" style="margin-bottom: 12px">
      {{ tunnelStatus.error }}
    </n-alert>

    <div class="tunnel-status">
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">本地服务</span>
        <span class="mono-text">http://127.0.0.1:{{ form.web.port }}</span>
      </div>
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">公网地址</span>
        <span class="mono-text">{{ tunnelPublicAddress || "连接成功后由 frps 分配" }}</span>
      </div>
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">运行状态</span>
        <span>{{ tunnelStatusText }}<template v-if="tunnelStatus?.startedAt"> · 启动于 {{ tunnelStatus.startedAt }}</template></span>
      </div>
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">说明</span>
        <span class="tunnel-status-note">
          公网侧需要一台运行 frps 的服务器（自建或使用 frp 服务商）。本应用只内置 frpc 客户端，
          不额外下载任何二进制文件；frps 官方发布页在
          <a href="https://github.com/fatedier/frp/releases" target="_blank" rel="noreferrer">GitHub</a>。
        </span>
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">API 访问</div>
        <div class="card-subtitle">
          供自动化脚本使用。每个 Key 可以单独设置权限和允许管理的计算机。
        </div>
      </div>
      <div class="spacer"></div>
      <n-button v-if="!isWebMode" size="small" @click="loadKeys" :loading="loadingKeys">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新
      </n-button>
      <n-button v-if="!isWebMode" size="small" type="primary" @click="openCreateKey">
        <template #icon>
          <n-icon :component="AddCircleOutline" />
        </template>
        新建 API Key
      </n-button>
    </div>

    <n-alert v-if="isWebMode" type="info" :show-icon="true">
      网页控制台不展示 API Key。请回到桌面端“设置 → API 访问”中创建和管理 Key。
    </n-alert>

    <div v-else class="key-list">
      <div v-if="keys.length === 0 && !loadingKeys" class="empty-hint">
        还没有 API Key。
      </div>
      <div v-for="key in keys" :key="key.id" class="key-row">
        <div class="key-main">
          <div class="key-name">{{ key.name }}</div>
          <div class="key-meta">
            <n-tag size="tiny" :type="permissionType(key.permission)" :bordered="false">
              {{ permissionLabel(key.permission) }}
            </n-tag>
            <span class="cell-muted">范围：{{ keyScopeLabel(key) }}</span>
            <span class="cell-muted" v-if="key.createdAt">创建：{{ key.createdAt }}</span>
          </div>
        </div>
        <div class="key-token">
          <span class="mono-text">
            {{ revealedTokens.has(key.id) ? key.token : maskedToken(key.token) }}
          </span>
          <n-button size="tiny" quaternary @click="toggleToken(key.id)">
            {{ revealedTokens.has(key.id) ? "隐藏" : "显示" }}
          </n-button>
          <n-button size="tiny" quaternary @click="copyText(key.token, 'Token')">
            <template #icon>
              <n-icon :component="CopyOutline" />
            </template>
          </n-button>
        </div>
        <div class="key-actions">
          <n-button size="tiny" quaternary @click="openEditKey(key)">编辑</n-button>
          <n-button size="tiny" quaternary @click="confirmRegenerateToken(key)">重新生成</n-button>
          <n-button size="tiny" quaternary type="error" @click="confirmDeleteKey(key)">
            <template #icon>
              <n-icon :component="TrashOutline" />
            </template>
          </n-button>
        </div>
      </div>
    </div>

    <div class="form-hint" style="margin-top: 10px">
      权限：只读 = GET 查询；读写 = 还可创建/克隆/编辑/删除/电源；管理员 = 还可管理计算机和设置。
      未指定计算机范围的 Key 可以访问全部计算机。
    </div>
  </div>

  <div class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">关于</div>
        <div class="card-subtitle">Hyper-V VM Assistant</div>
      </div>
    </div>
    <div class="about-layout">
      <img src="/logo.svg" alt="Hyper-V VM Assistant" class="about-logo" />
      <div class="about-body">
        <div class="about-title">Hyper-V VM Assistant</div>
        <div class="about-desc">
          为云电脑、AI Agent 云端运行场景打造的 Hyper-V 可视化管理工具。
          支持本机与远程 Hyper-V、虚拟机创建/克隆/编辑/删除、任务队列、网页控制台与自动化 API。
        </div>
        <div class="about-meta">
          <span>版本：v{{ app.appVersion || "0.1.1" }}</span>
          <span>Wails {{ app.wailsVersion || "3.1.0" }}</span>
          <span>作者：{{ author }}</span>
          <span>
            仓库：
            <a :href="repository" target="_blank" rel="noreferrer">{{ repository }}</a>
          </span>
        </div>
        <div class="settings-actions">
          <n-button size="small" @click="openRepository">打开 GitHub 仓库</n-button>
          <n-button size="small" :loading="checkingUpdate" @click="checkUpdate">
            检查更新
          </n-button>
          <span v-if="latestVersion" class="cell-muted">最新 Release：{{ latestVersion }}</span>
        </div>
      </div>
    </div>
  </div>

  <n-modal v-model:show="showKeyModal" preset="card" :title="editingKey ? '编辑 API Key' : '新建 API Key'" style="width: 560px">
    <n-form label-placement="top" size="small">
      <n-form-item label="名称">
        <n-input v-model:value="keyForm.name" placeholder="例如 计费系统 / 运维脚本" />
      </n-form-item>
      <n-form-item label="权限">
        <n-select v-model:value="keyForm.permission" :options="permissionOptions" />
      </n-form-item>
      <n-form-item>
        <template #label>
          允许管理的计算机
          <HelpTip>
            <p><strong>全部计算机</strong>：Key 可以访问任何已连接的主机。</p>
            <p><strong>指定计算机</strong>：只能查询/操作选中的主机；创建/克隆时也必须指定该主机。</p>
          </HelpTip>
        </template>
        <n-radio-group v-model:value="keyForm.scope">
          <n-radio-button value="all">全部计算机</n-radio-button>
          <n-radio-button value="selected">指定计算机</n-radio-button>
        </n-radio-group>
      </n-form-item>
      <n-form-item v-if="keyForm.scope === 'selected'" label="选择计算机">
        <n-select
          v-model:value="keyForm.hostIDs"
          :options="hostOptions"
          multiple
          filterable
          placeholder="选择允许管理的计算机"
        />
      </n-form-item>
      <n-alert type="warning" :show-icon="true">
        Token 只在创建或重新生成时完整显示一次，请立即复制保存。列表中的 Token 默认为掩码显示。
      </n-alert>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showKeyModal = false">取消</n-button>
        <n-button size="small" type="primary" :loading="savingKey" @click="saveKey">
          {{ editingKey ? "保存" : "创建" }}
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <div class="settings-actions">
    <n-button type="primary" :loading="saving" @click="save">保存设置</n-button>
    <span class="form-hint">主题会立即预览；网页控制台和关闭行为保存后生效。</span>
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

.key-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.key-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 14px;
  border: 1px solid var(--border-soft);
  border-radius: 9px;
  background: var(--panel-strong);
  flex-wrap: wrap;
}

.key-main {
  flex: 1;
  min-width: 220px;
}

.key-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-strong);
}

.key-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 4px;
  flex-wrap: wrap;
  font-size: 11.5px;
}

.key-token {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11.5px;
  color: var(--text-soft);
  word-break: break-all;
}

.key-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.tunnel-status {
  border: 1px solid var(--border-soft);
  border-radius: 9px;
  background: var(--panel-strong);
  padding: 10px 14px;
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.tunnel-section {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border-soft);
}

.tunnel-section-head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 10px;
}

.tunnel-section-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-1);
}

.mapping-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}

.mapping-arrow {
  color: var(--muted);
}

.tunnel-status-row {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--text-soft);
  line-height: 1.6;
}

.tunnel-status-key {
  flex: 0 0 64px;
  color: var(--muted);
}

.tunnel-status-note a {
  color: var(--primary);
}

.about-layout {
  display: flex;
  gap: 22px;
  align-items: flex-start;
}

.about-logo {
  width: 96px;
  height: 96px;
  flex: 0 0 96px;
  border-radius: 18px;
}

.about-body {
  flex: 1;
  min-width: 0;
}

.about-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-strong);
}

.about-desc {
  margin-top: 6px;
  max-width: 720px;
  font-size: 12.5px;
  line-height: 1.8;
  color: var(--text-soft);
}

.about-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 20px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--muted);
}

.about-meta a {
  color: var(--primary);
}

@media (max-width: 900px) {
  .about-layout {
    flex-direction: column;
  }
}
</style>
