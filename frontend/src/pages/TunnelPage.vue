<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useMessage } from "naive-ui";
import { SaveOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type { NATNetwork, Settings, TunnelStatus } from "../api";
import { useAppStore } from "../stores/app";

interface TunnelMappingForm {
  id?: string;
  enabled: boolean;
  name: string;
  localIP: string;
  localPort: number;
  remotePort: number;
}

const app = useAppStore();
const message = useMessage();

const form = reactive({
  enabled: false,
  serverAddr: "",
  serverPort: 7000,
  token: "",
  protocol: "tcp",
  proxyName: "hvvm-web",
  remotePort: 17890,
  mappings: [] as TunnelMappingForm[],
});

const webPort = ref(17890);
const saving = ref(false);
const tunnelStatus = ref<TunnelStatus | null>(null);
const tunnelLoading = ref(false);
let tunnelTimer: ReturnType<typeof setInterval> | undefined;

const natNetworks = ref<NATNetwork[]>([]);
const natLoading = ref(false);
const selectedNATMapping = ref<string | null>(null);

const tunnelProtocolOptions = [
  { label: "TCP（默认，最稳定）", value: "tcp" },
  { label: "WebSocket（可穿过 HTTP 代理）", value: "websocket" },
  { label: "WSS（WebSocket + TLS）", value: "wss" },
  { label: "KCP（UDP 加速）", value: "kcp" },
  { label: "QUIC（UDP + TLS）", value: "quic" },
];

function applySettings(settings: Settings | null) {
  if (!settings) return;
  webPort.value = settings.web.port;
  form.enabled = settings.tunnel.enabled;
  form.serverAddr = settings.tunnel.serverAddr;
  form.serverPort = settings.tunnel.serverPort;
  form.token = settings.tunnel.token;
  form.protocol = settings.tunnel.protocol;
  form.proxyName = settings.tunnel.proxyName;
  form.remotePort = settings.tunnel.remotePort;
  form.mappings = (settings.tunnel.mappings ?? []).map((mapping) => ({ ...mapping }));
}

watch(() => app.settings, applySettings, { immediate: true, deep: true });

async function save() {
  if (!app.settings) {
    message.warning("设置尚未加载");
    return;
  }
  saving.value = true;
  try {
    await app.saveSettings({
      ...app.settings,
      tunnel: {
        ...app.settings.tunnel,
        enabled: form.enabled,
        serverAddr: form.serverAddr.trim(),
        serverPort: Number(form.serverPort),
        token: form.token,
        protocol: form.protocol,
        proxyName: form.proxyName.trim(),
        remotePort: Number(form.remotePort),
        mappings: form.mappings.map((mapping) => ({ ...mapping })),
      },
    });
    message.success("内网穿透设置已保存");
    if (form.enabled) {
      window.setTimeout(() => void loadTunnelStatus(), 1200);
    }
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    saving.value = false;
  }
}

async function loadTunnelStatus() {
  if (isWebMode) return;
  try {
    tunnelStatus.value = await HyperVService.GetTunnelStatus();
  } catch {
    // 状态查询失败不打断页面
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

function addMapping() {
  form.mappings.push({
    enabled: true,
    name: `map-${form.mappings.length + 1}`,
    localIP: "",
    localPort: 3389,
    remotePort: 0,
  });
}

function removeMapping(index: number) {
  form.mappings.splice(index, 1);
}

function addMappingFromNAT() {
  const option = natMappingOptions.value.find(
    (item) => item.value === selectedNATMapping.value
  );
  if (!option) return;
  form.mappings.push({
    enabled: true,
    name: option.name,
    localIP: option.hostIP,
    localPort: option.externalPort,
    remotePort: 0,
  });
  selectedNATMapping.value = null;
  message.success("已添加 NAT 转发映射，保存设置后生效");
}

const tunnelPublicAddress = computed(() => {
  const status = tunnelStatus.value;
  if (!status) return "";
  if (status.remoteAddr) return status.remoteAddr;
  if (status.serverAddr && form.remotePort > 0) {
    return `${status.serverAddr.split(":")[0]}:${form.remotePort}`;
  }
  return "";
});

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

onMounted(() => {
  void loadTunnelStatus();
  void loadNATNetworks();
  tunnelTimer = setInterval(() => void loadTunnelStatus(), 5000);
});

onUnmounted(() => {
  if (tunnelTimer) clearInterval(tunnelTimer);
});
</script>

<template>
  <PageHeader title="内网穿透" subtitle="把虚拟机的服务通过 frp 映射到公网">
    <n-button
      v-if="!isWebMode"
      size="small"
      :loading="tunnelLoading"
      :disabled="!form.enabled"
      @click="restartTunnel"
    >
      重新连接
    </n-button>
    <n-button v-if="!isWebMode" size="small" :loading="natLoading" @click="loadNATNetworks">
      刷新 NAT
    </n-button>
    <n-button v-if="!isWebMode" size="small" type="primary" :loading="saving" @click="save">
      <template #icon>
        <n-icon :component="SaveOutline" />
      </template>
      保存设置
    </n-button>
  </PageHeader>

  <div class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">先看这里：从端口转发到公网的流程</div>
        <div class="card-subtitle">
          内网穿透只负责把本机能访问的端口映射到公网，不能直接猜到虚拟机的 IP。
        </div>
      </div>
      <div class="spacer"></div>
      <n-tag size="small" :type="tunnelStatusType" :bordered="false">
        {{ tunnelStatusText }}
      </n-tag>
    </div>
    <div class="flow-steps">
      <div class="flow-step">
        <span class="flow-index">1</span>
        <div>
          <div class="flow-title">准备内部 NAT 网络</div>
          <div class="flow-desc">
            到“虚拟交换机”页面创建“内部网络 NAT”，把虚拟机网卡接到这个内部交换机，
            并在虚拟机系统里设置同网段的静态 IP（例如 192.168.100.2，网关 192.168.100.1）。
          </div>
        </div>
      </div>
      <div class="flow-step">
        <span class="flow-index">2</span>
        <div>
          <div class="flow-title">在虚拟机详情里添加端口转发</div>
          <div class="flow-desc">
            打开虚拟机的“详情”或“编辑配置”，在“端口转发与内网穿透”里添加
            「宿主机端口 → 虚拟机 IP:服务端口」。这里会直接显示虚拟机的 IPv4，不需要自己猜。
          </div>
        </div>
      </div>
      <div class="flow-step">
        <span class="flow-index">3</span>
        <div>
          <div class="flow-title">在内网穿透里选择并保存</div>
          <div class="flow-desc">
            在下面选择刚才的端口转发，或在虚拟机详情直接点“映射到公网”。
            填写 frps 地址和 Token，保存并启用，即可从公网访问。
          </div>
        </div>
      </div>
    </div>
    <n-alert v-if="isWebMode" type="warning" :show-icon="true">
      内网穿透的配置和 frpc 客户端运行在桌面端。请在本机桌面应用中使用此页面。
    </n-alert>
  </div>

  <div v-if="!isWebMode" class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">frp 服务器</div>
        <div class="card-subtitle">frpc 已编译进应用，不需要安装额外程序</div>
      </div>
    </div>

    <n-form label-placement="top" size="small">
      <div class="form-grid">
        <n-form-item label="启用内网穿透">
          <n-switch v-model:value="form.enabled" />
        </n-form-item>
        <n-form-item label="传输协议">
          <n-select v-model:value="form.protocol" :options="tunnelProtocolOptions" />
        </n-form-item>
        <n-form-item label="frps 服务器地址">
          <n-input v-model:value="form.serverAddr" placeholder="例如 frp.example.com" />
        </n-form-item>
        <n-form-item label="frps 端口">
          <n-input-number v-model:value="form.serverPort" :min="1" :max="65535" style="width: 100%" />
        </n-form-item>
        <n-form-item label="认证 Token">
          <n-input
            v-model:value="form.token"
            type="password"
            show-password-on="click"
            placeholder="与 frps 的 auth.token 保持一致"
          />
        </n-form-item>
        <n-form-item label="网页控制台远程端口">
          <n-input-number v-model:value="form.remotePort" :min="0" :max="65535" style="width: 100%" />
        </n-form-item>
        <n-form-item label="代理名称">
          <n-input v-model:value="form.proxyName" placeholder="hvvm-web" />
        </n-form-item>
      </div>
    </n-form>

    <div class="tunnel-section">
      <div class="tunnel-section-head">
        <div>
          <div class="tunnel-section-title">从 NAT 端口转发添加</div>
          <div class="form-hint">
            选择虚拟机详情里创建的端口转发，自动填好 frp 的本地目标。
          </div>
        </div>
        <n-button size="small" :loading="natLoading" @click="loadNATNetworks">刷新列表</n-button>
      </div>
      <div class="mapping-row">
        <n-select
          v-model:value="selectedNATMapping"
          :options="natMappingOptions"
          placeholder="选择 NAT 端口转发"
          clearable
          style="flex: 1"
        />
        <n-button size="small" type="primary" :disabled="!selectedNATMapping" @click="addMappingFromNAT">
          添加为 frp 映射
        </n-button>
      </div>
      <div v-if="natMappingOptions.length === 0" class="form-hint">
        没有可用的 NAT 端口转发。先到虚拟机详情给具体虚拟机添加端口转发。
      </div>
      <n-alert v-if="app.activeHost && !app.activeHost.isLocal" type="warning" :show-icon="true">
        当前管理的是远程主机，内置 frpc 在本机运行。目标地址必须是本机可达的远程宿主机 IP；
        如果无法直连，请把 frpc 部署到远程宿主机，或改用可路由地址。
      </n-alert>
    </div>

    <div class="tunnel-section">
      <div class="tunnel-section-head">
        <div>
          <div class="tunnel-section-title">自定义端口映射</div>
          <div class="form-hint">
            每条映射是「frp 本地目标 → frps 远程端口」。本地目标通常来自上面的 NAT 端口转发。
          </div>
        </div>
        <n-button size="small" dashed @click="addMapping">添加映射</n-button>
      </div>
      <div v-if="form.mappings.length === 0" class="empty-hint">
        还没有端口映射
      </div>
      <div v-for="(mapping, index) in form.mappings" :key="mapping.id || index" class="mapping-row">
        <n-switch v-model:value="mapping.enabled" size="small" />
        <n-input v-model:value="mapping.name" placeholder="名称" style="width: 110px" />
        <n-input v-model:value="mapping.localIP" placeholder="目标 IP" style="flex: 1" />
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
    </div>
  </div>

  <div v-if="!isWebMode" class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">运行状态</div>
        <div class="card-subtitle">保存后 frpc 会自动重启；状态每 5 秒刷新</div>
      </div>
    </div>
    <n-alert v-if="tunnelStatus?.error" type="error" :show-icon="true" style="margin-bottom: 12px">
      {{ tunnelStatus.error }}
    </n-alert>
    <div class="tunnel-status">
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">本地网页控制台</span>
        <span class="mono-text">http://127.0.0.1:{{ webPort }}</span>
      </div>
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">公网地址</span>
        <span class="mono-text">{{ tunnelPublicAddress || "连接成功后由 frps 分配" }}</span>
      </div>
      <div class="tunnel-status-row">
        <span class="tunnel-status-key">运行状态</span>
        <span>{{ tunnelStatusText }}<template v-if="tunnelStatus?.startedAt"> · 启动于 {{ tunnelStatus.startedAt }}</template></span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.flow-steps {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 12px;
  margin-top: 12px;
}

.flow-step {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-inset);
}

.flow-index {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex: 0 0 22px;
  border-radius: 999px;
  background: var(--net-blue-bg);
  color: var(--net-blue-fg);
  font-size: 11px;
  font-weight: 600;
}

.flow-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-strong);
}

.flow-desc {
  margin-top: 3px;
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--text-3);
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0 14px;
}

.tunnel-section {
  margin-top: 18px;
}

.tunnel-section-head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  justify-content: space-between;
  margin-bottom: 10px;
}

.tunnel-section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-strong);
}

.mapping-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 0;
}

.mapping-arrow {
  color: var(--text-3);
}

.tunnel-status {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tunnel-status-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  font-size: 12px;
  color: var(--text-2);
}

.tunnel-status-key {
  flex: 0 0 120px;
  color: var(--text-3);
}
</style>
