<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useDialog, useMessage } from "naive-ui";
import {
  AddCircleOutline,
  DesktopOutline,
  RefreshOutline,
  ServerOutline,
} from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import HelpTip from "../components/HelpTip.vue";
import { errorMessage } from "../api";
import type { HostItem, HostRequest } from "../api";
import { useHostsStore } from "../stores/hosts";
import { formatDateTime } from "../utils/format";

const router = useRouter();
const message = useMessage();
const dialog = useDialog();
const hosts = useHostsStore();

const showForm = ref(false);
const saving = ref(false);
const editing = ref<HostItem | null>(null);

const showConnect = ref(false);
const connectTarget = ref<HostItem | null>(null);
const connectPassword = ref("");
const connecting = ref(false);

const form = reactive({
  id: "",
  name: "",
  address: "",
  port: 5985,
  useSSL: false,
  username: "",
  authentication: "Default",
});

const authOptions = [
  { label: "Default（协商，推荐）", value: "Default" },
  { label: "Negotiate", value: "Negotiate" },
  { label: "CredSSP（双跳认证）", value: "CredSSP" },
];

const isEditing = computed(() => !!editing.value);
const formTitle = computed(() => (isEditing.value ? "编辑计算机" : "添加远程计算机"));

watch(
  () => form.useSSL,
  (useSSL) => {
    if (isEditing.value) return;
    form.port = useSSL ? 5986 : 5985;
  }
);

function resetForm() {
  editing.value = null;
  form.id = "";
  form.name = "";
  form.address = "";
  form.port = 5985;
  form.useSSL = false;
  form.username = "";
  form.authentication = "Default";
}

function openAdd() {
  resetForm();
  showForm.value = true;
}

function openEdit(item: HostItem) {
  resetForm();
  editing.value = item;
  form.id = item.id;
  form.name = item.name;
  form.address = item.address;
  form.port = item.port || (item.useSSL ? 5986 : 5985);
  form.useSSL = item.useSSL;
  form.username = item.username;
  form.authentication = item.authentication || "Default";
  showForm.value = true;
}

async function saveHost() {
  if (!form.address.trim()) {
    message.warning("请输入远程计算机名或 IP 地址");
    return;
  }
  if (!form.port || form.port < 1 || form.port > 65535) {
    message.warning("WinRM 端口必须在 1 到 65535 之间");
    return;
  }
  saving.value = true;
  try {
    const request: HostRequest = {
      id: form.id,
      name: form.name.trim() || form.address.trim(),
      address: form.address.trim(),
      port: Number(form.port),
      useSSL: form.useSSL,
      username: form.username.trim(),
      authentication: form.authentication,
    };
    if (isEditing.value) {
      await hosts.update(request);
      message.success("计算机配置已更新，请重新连接");
    } else {
      await hosts.add(request);
      message.success("远程计算机已添加，请点击“连接”验证 WinRM 与 Hyper-V 权限");
    }
    showForm.value = false;
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    saving.value = false;
  }
}

async function activate(item: HostItem) {
  if (item.active) return;
  if (item.isLocal) {
    try {
      await hosts.connect(item.id, "");
      message.success("已切换到本机");
      await afterSwitch();
    } catch (err) {
      message.error(errorMessage(err));
    }
    return;
  }
  if (item.connected) {
    try {
      await hosts.setActive(item.id);
      message.success(`已切换到 ${item.name}`);
      await afterSwitch();
    } catch (err) {
      message.error(errorMessage(err));
    }
    return;
  }
  openConnect(item);
}

function openConnect(item: HostItem) {
  connectTarget.value = item;
  connectPassword.value = "";
  showConnect.value = true;
}

async function doConnect() {
  const item = connectTarget.value;
  if (!item) return;
  if (item.username && !connectPassword.value) {
    message.warning("请输入远程计算机密码");
    return;
  }
  connecting.value = true;
  try {
    await hosts.connect(item.id, connectPassword.value);
    message.success(`已连接 ${item.name}`);
    showConnect.value = false;
    connectPassword.value = "";
    await afterSwitch();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    connecting.value = false;
  }
}

async function afterSwitch() {
  await hosts.fetch();
  await router.push("/vms");
}

function confirmRemove(item: HostItem) {
  dialog.warning({
    title: "删除计算机",
    content: `确定删除「${item.name}」的连接配置吗？不会影响远程服务器上的虚拟机。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await hosts.remove(item.id);
        message.success("计算机配置已删除");
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

function hostAddress(item: HostItem): string {
  if (item.isLocal) return "本机（当前运行管理助手的电脑）";
  const scheme = item.useSSL ? "https" : "http";
  return `${item.address}:${item.port}（${scheme}）`;
}

onMounted(() => {
  void hosts.fetch();
});
</script>

<template>
  <PageHeader
    title="计算机"
    subtitle="连接本机或远程 Hyper-V 服务器，后续所有虚拟机操作都针对当前主机"
  >
    <n-button size="small" :loading="hosts.loading" @click="hosts.fetch()">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      刷新
    </n-button>
    <n-button size="small" type="primary" @click="openAdd">
      <template #icon>
        <n-icon :component="AddCircleOutline" />
      </template>
      添加远程计算机
    </n-button>
  </PageHeader>

  <n-alert v-if="hosts.error" type="warning" class="inline-error" title="读取计算机列表失败">
    {{ hosts.error }}
  </n-alert>

  <div class="host-grid">
    <div
      v-for="item in hosts.items"
      :key="item.id"
      class="host-card"
      :class="{ active: item.active }"
    >
      <div class="host-card-head">
        <div class="host-icon">
          <n-icon :component="item.isLocal ? DesktopOutline : ServerOutline" :size="21" />
        </div>
        <div class="host-name-wrap">
          <div class="host-name">{{ item.name }}</div>
          <div class="host-address mono-text">{{ hostAddress(item) }}</div>
        </div>
        <div class="host-badges">
          <n-tag v-if="item.active" size="tiny" type="success" :bordered="false">当前主机</n-tag>
          <n-tag v-else-if="item.connected" size="tiny" type="info" :bordered="false">已连接</n-tag>
        </div>
      </div>

      <div class="host-meta">
        <div>
          <span class="host-meta-key">用户名</span>
          <span class="host-meta-value">{{ item.username || "当前 Windows 凭据" }}</span>
        </div>
        <div>
          <span class="host-meta-key">认证</span>
          <span class="host-meta-value">{{ item.authentication || "Default" }}</span>
        </div>
        <div>
          <span class="host-meta-key">最近连接</span>
          <span class="host-meta-value">{{ formatDateTime(item.lastConnectedAt) }}</span>
        </div>
      </div>

      <div class="host-actions">
        <n-button
          v-if="!item.active"
          size="small"
          type="primary"
          ghost
          @click="activate(item)"
        >
          {{ item.connected || item.isLocal ? "切换到此主机" : "连接" }}
        </n-button>
        <n-tag v-else size="small" type="success" :bordered="false">正在使用</n-tag>
        <n-button
          v-if="!item.isLocal"
          size="small"
          quaternary
          @click="openEdit(item)"
        >
          编辑
        </n-button>
        <n-button
          v-if="!item.isLocal"
          size="small"
          quaternary
          type="error"
          @click="confirmRemove(item)"
        >
          删除
        </n-button>
      </div>
    </div>
  </div>

  <div class="panel remote-help">
    <div class="card-title">连接远程 Hyper-V 主机的前提</div>
    <ul class="help-list">
      <li>远程主机已启用 WinRM（PowerShell Remoting），默认端口 5985（HTTP）或 5986（HTTPS）。</li>
      <li>使用的 Windows 账户在远程主机上属于管理员或 Hyper-V Administrators，具备 Hyper-V 管理权限。</li>
      <li>远程主机已启用 Hyper-V 功能，并且虚拟化在 BIOS/UEFI 中已开启。</li>
      <li>跨域或工作组环境可能需要在 WinRM 中配置 TrustedHosts，或使用 CredSSP / HTTPS。</li>
      <li>创建、克隆时填写的镜像、ISO、存储目录都是 <strong>远程主机上的绝对路径</strong>，远程模式下不能使用本机文件选择器。</li>
    </ul>
    <n-collapse style="margin-top: 10px">
      <n-collapse-item title="远程主机启用 WinRM 的 PowerShell 命令" name="winrm">
        <pre class="code-block"># 在远程 Hyper-V 主机上以管理员身份运行
Enable-PSRemoting -Force
Set-Item WSMan:\localhost\Client\TrustedHosts -Value "*" -Force
# 或仅允许管理端 IP：
# Set-Item WSMan:\localhost\Client\TrustedHosts -Value "192.168.1.10" -Force
Restart-Service WinRM</pre>
      </n-collapse-item>
      <n-collapse-item title="密码保存说明" name="password">
        <div class="help-text">
          远程密码只保存在当前应用进程内存中，用于建立 WinRM 会话，不会写入 hosts.json。
          关闭应用后需要重新输入；主机名、地址、用户名等配置会保留。
        </div>
      </n-collapse-item>
    </n-collapse>
  </div>

  <n-modal v-model:show="showForm" preset="card" :title="formTitle" style="width: 560px">
    <n-form label-placement="top" size="small">
      <div class="form-grid">
        <n-form-item label="显示名称">
          <n-input v-model:value="form.name" placeholder="例如 实验室 Hyper-V" />
        </n-form-item>
        <n-form-item>
          <template #label>
            计算机名 / IP
            <HelpTip>
              <p>填写远程 Hyper-V 主机的 DNS 名称或 IP 地址。</p>
              <p>管理端必须能够通过 WinRM 访问该地址，并且账户具备 Hyper-V 管理权限。</p>
            </HelpTip>
          </template>
          <n-input v-model:value="form.address" placeholder="例如 hyperv-lab 或 192.168.1.20" />
        </n-form-item>
        <n-form-item>
          <template #label>
            WinRM 端口
            <HelpTip>
              <p>HTTP 默认 5985，HTTPS 默认 5986。</p>
              <p>如果远程主机修改过 WinRM 监听端口，请填写实际端口。</p>
            </HelpTip>
          </template>
          <n-input-number v-model:value="form.port" :min="1" :max="65535" style="width: 100%" />
        </n-form-item>
        <n-form-item label="使用 HTTPS">
          <n-switch v-model:value="form.useSSL" />
        </n-form-item>
        <n-form-item label="用户名（可选）">
          <n-input v-model:value="form.username" placeholder="留空使用当前 Windows 凭据；例如 LAB\\Administrator" />
        </n-form-item>
        <n-form-item label="认证方式">
          <n-select v-model:value="form.authentication" :options="authOptions" />
        </n-form-item>
      </div>
      <n-alert type="info" :show-icon="true">
        保存配置不会立即连接，也不会保存密码。添加后点击“连接”验证 WinRM 和 Hyper-V 权限。
      </n-alert>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showForm = false">取消</n-button>
        <n-button size="small" type="primary" :loading="saving" @click="saveHost">保存</n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal
    v-model:show="showConnect"
    preset="card"
    :title="`连接 ${connectTarget?.name || ''}`"
    style="width: 460px"
  >
    <div v-if="connectTarget">
      <div class="connect-address mono-text">{{ hostAddress(connectTarget) }}</div>
      <n-form label-placement="top" size="small" style="margin-top: 12px">
        <n-form-item label="Windows 密码">
          <n-input
            v-model:value="connectPassword"
            type="password"
            show-password-on="click"
            :placeholder="connectTarget.username ? '输入远程主机账户密码' : '留空使用当前 Windows 凭据'"
            @keyup.enter="doConnect"
          />
        </n-form-item>
      </n-form>
      <n-alert type="warning" :show-icon="true">
        密码只保存在内存中。连接会通过 WinRM 执行 <span class="mono-text">Get-VMHost</span>
        验证权限，失败时请检查远程 WinRM、防火墙和 Hyper-V 管理权限。
      </n-alert>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showConnect = false">取消</n-button>
        <n-button size="small" type="primary" :loading="connecting" @click="doConnect">
          连接
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.host-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 16px;
}

.host-card {
  background: var(--panel);
  border: 1px solid var(--border-soft);
  border-radius: 10px;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.host-card.active {
  border-color: #2c5d9c;
  box-shadow: inset 0 0 0 1px rgba(79, 140, 255, 0.12);
}

.host-card-head {
  display: flex;
  align-items: flex-start;
  gap: 11px;
}

.host-icon {
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  border-radius: 9px;
  background: #1a2130;
  color: #8fb6ff;
  display: flex;
  align-items: center;
  justify-content: center;
}

.host-name-wrap {
  flex: 1;
  min-width: 0;
}

.host-name {
  font-size: 14px;
  font-weight: 600;
  color: #eef1f7;
}

.host-address {
  font-size: 11px;
  color: var(--muted);
  margin-top: 2px;
  word-break: break-all;
}

.host-badges {
  display: flex;
  gap: 6px;
  flex: 0 0 auto;
}

.host-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px 12px;
  font-size: 11.5px;
}

.host-meta-key {
  color: var(--muted);
  display: block;
  margin-bottom: 2px;
}

.host-meta-value {
  color: #c8d0de;
  word-break: break-all;
}

.host-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  border-top: 1px solid var(--border-soft);
  padding-top: 11px;
}

.remote-help {
  margin-top: 0;
}

.help-list {
  margin: 8px 0 0;
  padding-left: 20px;
  font-size: 12px;
  line-height: 1.8;
  color: #aeb8c9;
}

.help-list strong {
  color: #eef1f7;
}

.help-text {
  font-size: 12px;
  line-height: 1.75;
  color: #aeb8c9;
}

.code-block {
  margin: 0;
  padding: 12px;
  border-radius: 8px;
  background: #0b0d12;
  border: 1px solid var(--border);
  color: #b7c0d0;
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 11.5px;
  line-height: 1.7;
  overflow-x: auto;
  white-space: pre-wrap;
}

.connect-address {
  font-size: 12px;
  color: #aeb8c9;
  background: #10131a;
  border: 1px solid var(--border-soft);
  border-radius: 7px;
  padding: 8px 10px;
  word-break: break-all;
}

@media (max-width: 1100px) {
  .host-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .host-meta {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
