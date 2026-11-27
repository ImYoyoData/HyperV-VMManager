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
import ConnectHelpModal from "../components/ConnectHelpModal.vue";
import { errorMessage, isWebMode, HyperVService } from "../api";
import type { HostItem, HostRequest } from "../api";
import { useHostsStore } from "../stores/hosts";
import { useAppStore } from "../stores/app";
import { useVMStore } from "../stores/vms";
import { useTaskStore } from "../stores/tasks";
import { useSetupStore } from "../stores/setup";
import { formatDateTime } from "../utils/format";

const router = useRouter();
const message = useMessage();
const dialog = useDialog();
const hosts = useHostsStore();
const app = useAppStore();
const vmStore = useVMStore();
const taskStore = useTaskStore();
const setup = useSetupStore();

const showForm = ref(false);
const saving = ref(false);
const editing = ref<HostItem | null>(null);
const formType = ref<"local" | "remote">("remote");

const showConnect = ref(false);
const connectTarget = ref<HostItem | null>(null);
const connectPassword = ref("");
const rememberPassword = ref(true);
const connecting = ref(false);
const connectError = ref("");
const showConnectHelp = ref(false);

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
const formTitle = computed(() => (isEditing.value ? "编辑计算机" : "添加计算机"));
const hasLocal = computed(() => hosts.items.some((item) => item.isLocal));

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

async function openAdd() {
  if (hosts.items.length === 0) {
    await hosts.fetch();
  }
  resetForm();
  // 本机已经存在时默认并只能选择远程；本机不存在时才默认本地。
  formType.value = hasLocal.value ? "remote" : "local";
  showForm.value = true;
}

function openEdit(item: HostItem) {
  resetForm();
  formType.value = "remote";
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
      message.success("计算机配置已更新");
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
  if (item.passwordSaved) {
    // 已经记住密码：直接连接，不再弹出密码框。
    try {
      await hosts.connect(item.id, "");
      message.success(`已连接 ${item.name}`);
      await afterSwitch();
    } catch (err) {
      // 保存的密码可能已经失效，直接打开密码框让用户重新输入，
      // 同时保留错误信息和修复入口。
      openConnect(item);
      connectError.value = errorMessage(err);
    }
    return;
  }
  openConnect(item);
}

// 本地计算机固定只有一个：已经存在时直接切换，不会重复添加。
async function addLocal() {
  const local = hosts.items.find((item) => item.isLocal);
  if (!local) {
    message.warning("未找到本地计算机配置，请重新打开应用");
    return;
  }
  if (local.active) {
    message.info("本地计算机只能存在一个，当前已经连接到本机");
    return;
  }
  try {
    await hosts.connect(local.id, "");
    message.success("本地计算机已存在，已切换到本机");
    await afterSwitch();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

function openConnect(item: HostItem) {
  connectTarget.value = item;
  connectPassword.value = "";
  rememberPassword.value = true;
  connectError.value = "";
  showConnect.value = true;
}

async function saveLocal() {
  if (hasLocal.value) {
    message.info("本机已添加且只能存在一个，请选择“远程计算机”");
    formType.value = "remote";
    return;
  }
  await addLocal();
  showForm.value = false;
}

async function doConnect() {
  const item = connectTarget.value;
  if (!item) return;
  if (item.username && !connectPassword.value) {
    message.warning("请输入远程计算机密码");
    return;
  }
  connecting.value = true;
  connectError.value = "";
  try {
    await hosts.connect(item.id, connectPassword.value, rememberPassword.value);
    message.success(`已连接 ${item.name}`);
    showConnect.value = false;
    connectPassword.value = "";
    await afterSwitch();
  } catch (err) {
    connectError.value = errorMessage(err);
    // 连接失败时直接给出可复制的修复命令，而不是只弹一条报错。
    showConnectHelp.value = true;
  } finally {
    connecting.value = false;
  }
}

async function afterSwitch() {
  await hosts.fetch();
  if (isWebMode) {
    vmStore.clear();
    await Promise.all([
      app.init(),
      vmStore.fetch({ force: true }),
      taskStore.fetch(),
      setup.check({ force: true }),
    ]);
  }
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

function forgetPassword(item: HostItem) {
  dialog.warning({
    title: "忘记已保存的密码",
    content: `将删除 Windows 凭据管理器中「${item.address || item.name}」的密码。删除后需要重新输入密码才能连接。`,
    positiveText: "删除密码",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await HyperVService.ForgetHostCredential(item.id);
        await hosts.fetch();
        message.success("已删除保存的密码");
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
      添加计算机
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
          <n-tag v-if="item.passwordSaved" size="tiny" :bordered="false">已记住密码</n-tag>
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
        <n-button
          v-if="item.passwordSaved"
          size="small"
          quaternary
          @click="forgetPassword(item)"
        >
          忘记密码
        </n-button>
      </div>
    </div>
  </div>

  <div class="panel remote-help">
    <div class="card-title">连接远程 Hyper-V 主机的前提</div>
    <ul class="help-list">
      <li>本地计算机固定为运行管理助手的这台电脑，只能存在一个；远程计算机可以添加多个。</li>
      <li>远程主机已启用 WinRM（PowerShell Remoting），默认端口 5985（HTTP）或 5986（HTTPS）。</li>
      <li>使用的 Windows 账户在远程主机上属于管理员或 Hyper-V Administrators，具备 Hyper-V 管理权限。</li>
      <li>远程主机已启用 Hyper-V 功能，并且虚拟化在 BIOS/UEFI 中已开启。</li>
      <li>跨域或工作组环境可能需要在 WinRM 中配置 TrustedHosts，或使用 CredSSP / HTTPS。</li>
      <li>创建、克隆时填写的镜像、ISO、存储目录都是 <strong>远程主机上的绝对路径</strong>，远程模式下不能使用本机文件选择器。</li>
    </ul>
    <n-collapse style="margin-top: 10px">
      <n-collapse-item title="远程主机启用 WinRM（在远程主机上执行）" name="winrm">
        <pre class="code-block"># 在远程 Hyper-V 主机上以管理员身份运行
Enable-PSRemoting -Force
Enable-NetFirewallRule -DisplayGroup "Windows 远程管理"</pre>
      </n-collapse-item>
      <n-collapse-item title="工作组 / IP 连接（在本机执行）" name="trusted">
        <pre class="code-block"># 在工作组环境用 IP 连接时，必须让本机信任目标主机
Set-Item WSMan:\localhost\Client\TrustedHosts -Value "192.168.0.16" -Force
Restart-Service WinRM

# 需要 CredSSP 双跳认证时：
Enable-WSManCredSSP -Role Client -DelegateComputer "192.168.0.16" -Force
# 并在远程主机执行：Enable-WSManCredSSP -Role Server -Force</pre>
        <div class="help-text" style="margin-top: 8px">
          连接失败时可以直接点击弹窗里的“一键修复本机”，自动完成 TrustedHosts 配置；
          其余命令也可以直接复制到对应机器的管理员 PowerShell 中执行。
        </div>
      </n-collapse-item>
      <n-collapse-item title="密码保存说明" name="password">
        <div class="help-text">
          勾选“记住密码”时，密码会加密保存到 Windows 凭据管理器，应用重启后可以直接连接，
          VMConnect 和 Hyper-V 管理器也能复用；不勾选时密码只保存在当前应用进程内存中。
          任何情况下密码都不会写入 hosts.json。要删除已保存的密码，请点击计算机卡片上的“忘记密码”。
        </div>
      </n-collapse-item>
    </n-collapse>
  </div>

  <n-modal v-model:show="showForm" preset="card" :title="formTitle" style="width: 580px">
    <n-form label-placement="top" size="small">
      <n-form-item v-if="!isEditing" label="添加类型">
        <div class="type-picker">
          <n-radio-group v-model:value="formType">
            <n-radio-button value="local" :disabled="hasLocal">本机</n-radio-button>
            <n-radio-button value="remote">远程计算机</n-radio-button>
          </n-radio-group>
          <div class="form-hint" style="margin-top: 6px">
            <template v-if="hasLocal">
              本机已经添加且只能存在一个；要切换本机请直接在列表中点击“切换到此主机”。
              现在请添加远程计算机。
            </template>
            <template v-else>
              默认使用本机。本机配置只会存在一个，也可以改为添加远程计算机。
            </template>
          </div>
        </div>
      </n-form-item>

      <template v-if="formType === 'local' && !isEditing">
        <n-alert type="info" :show-icon="true">
          将使用正在运行管理助手的这台电脑作为当前主机。本机配置在应用启动时自动创建，不会重复添加。
        </n-alert>
      </template>

      <template v-else>
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
          保存配置不会立即连接，也不会保存密码。添加后点击“连接”验证 WinRM 和 Hyper-V 权限；
          连接失败时会自动给出可复制的修复命令。
        </n-alert>
      </template>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showForm = false">取消</n-button>
        <n-button
          v-if="!isEditing && formType === 'local'"
          size="small"
          type="primary"
          :disabled="hasLocal"
          @click="saveLocal"
        >
          使用本机
        </n-button>
        <n-button v-else size="small" type="primary" :loading="saving" @click="saveHost">
          {{ isEditing ? "保存" : "添加" }}
        </n-button>
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
        <n-checkbox v-model:checked="rememberPassword">
          记住密码（加密保存到 Windows 凭据管理器，下次直接连接）
        </n-checkbox>
      </n-form>
      <n-alert v-if="connectError" type="error" :show-icon="true">
        <div class="connect-error">{{ connectError }}</div>
        <n-button
          size="tiny"
          type="warning"
          ghost
          style="margin-top: 8px"
          @click="showConnectHelp = true"
        >
          查看解决方案 / 一键修复
        </n-button>
      </n-alert>
      <n-alert v-else type="warning" :show-icon="true">
        勾选“记住密码”会把密码加密保存到 Windows 凭据管理器；不勾选时密码仅在本次运行期间保存在内存中。
        连接会通过 WinRM 执行 <span class="mono-text">Get-VMHost</span>
        验证权限；如果失败，会自动打开解决方案并给出可复制的修复命令。
      </n-alert>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button v-if="connectTarget" size="small" quaternary @click="showConnectHelp = true">
          连接遇到问题？
        </n-button>
        <n-button size="small" @click="showConnect = false">取消</n-button>
        <n-button size="small" type="primary" :loading="connecting" @click="doConnect">
          连接
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <ConnectHelpModal
    v-model:show="showConnectHelp"
    :host="connectTarget"
    :error="connectError"
  />
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
  border-color: var(--border-strong);
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
  background: var(--icon-bg);
  color: var(--icon-fg);
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
  color: var(--text-strong);
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
  color: var(--text-2);
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
  color: var(--text-3);
}

.help-list strong {
  color: var(--text-strong);
}

.help-text {
  font-size: 12px;
  line-height: 1.75;
  color: var(--text-3);
}

.code-block {
  margin: 0;
  padding: 12px;
  border-radius: 8px;
  background: var(--code-bg);
  border: 1px solid var(--border);
  color: var(--text-3);
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 11.5px;
  line-height: 1.7;
  overflow-x: auto;
  white-space: pre-wrap;
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

.type-picker {
  width: 100%;
}

.connect-error {
  font-size: 11.5px;
  line-height: 1.7;
  max-height: 150px;
  overflow-y: auto;
  word-break: break-word;
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
