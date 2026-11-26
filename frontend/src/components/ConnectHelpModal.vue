<script setup lang="ts">
import { computed, ref } from "vue";
import { useDialog, useMessage } from "naive-ui";
import { CopyOutline, ShieldCheckmarkOutline } from "@vicons/ionicons5";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type { HostItem } from "../api";

const props = defineProps<{
  show: boolean;
  host?: HostItem | null;
  error?: string;
}>();

const emit = defineEmits<{
  (event: "update:show", value: boolean): void;
}>();

const message = useMessage();
const dialog = useDialog();

const fixing = ref(false);
const fixResult = ref("");
const fixError = ref("");

interface CommandItem {
  key: string;
  title: string;
  target: "本机" | "远程主机";
  description: string;
  command: string;
}

const address = computed(() => {
  const value = props.host?.address?.trim();
  return value || "<远程主机名或IP>";
});

const commands = computed<CommandItem[]>(() => [
  {
    key: "enable",
    title: "启用 WinRM 与防火墙规则",
    target: "远程主机",
    description:
      "在远程 Hyper-V 主机上以管理员身份打开 PowerShell 执行。WinRM 默认使用 5985（HTTP）或 5986（HTTPS）。",
    command: `Enable-PSRemoting -Force
Enable-NetFirewallRule -DisplayGroup "Windows 远程管理"`,
  },
  {
    key: "trusted",
    title: "把远程主机加入 TrustedHosts",
    target: "本机",
    description:
      "在工作组环境或用 IP 地址连接时，Kerberos 无法验证目标名称。在本机（运行管理助手的电脑）以管理员身份执行。",
    command: `Set-Item WSMan:\\localhost\\Client\\TrustedHosts -Value "${address.value}" -Force
Restart-Service WinRM`,
  },
  {
    key: "credssp-server",
    title: "启用 CredSSP 服务端",
    target: "远程主机",
    description:
      "需要双跳认证（例如在远程主机上再访问其他服务器）或默认认证失败时使用。在远程主机上以管理员身份执行。",
    command: `Enable-WSManCredSSP -Role Server -Force`,
  },
  {
    key: "credssp-client",
    title: "启用 CredSSP 客户端委派",
    target: "本机",
    description:
      "在本机以管理员身份执行，然后把计算机配置里的“认证方式”改为 CredSSP 再连接。",
    command: `Enable-WSManCredSSP -Role Client -DelegateComputer "${address.value}" -Force`,
  },
  {
    key: "https",
    title: "改用 HTTPS（5986）",
    target: "远程主机",
    description:
      "如果 HTTP 5985 被策略禁用，可在远程主机配置 WinRM HTTPS 监听器，然后在计算机配置中勾选“使用 HTTPS”并填写 5986。本工具会跳过自签名证书校验。",
    command: `winrm quickconfig -transport:https`,
  },
]);

async function copyCommand(command: string) {
  try {
    await navigator.clipboard.writeText(command);
    message.success("命令已复制，请到对应机器上以管理员身份执行");
  } catch {
    message.warning("无法访问剪贴板，请手动选中命令复制");
  }
}

function confirmFix() {
  fixError.value = "";
  fixResult.value = "";
  dialog.warning({
    title: "修复本机 WinRM 配置",
    content: `将启动 WinRM 客户端服务，并把「${address.value}」加入 TrustedHosts。\n\n该操作会修改本机系统配置，需要管理员权限，可能会弹出 UAC 授权窗口。`,
    positiveText: "开始修复",
    negativeText: "取消",
    onPositiveClick: async () => {
      fixing.value = true;
      try {
        const result = await HyperVService.FixRemoteWinRM(address.value);
        fixResult.value = result || "修复完成";
        message.success("本机 WinRM 配置已修复，请关闭此窗口后重新点击连接");
      } catch (err) {
        fixError.value = errorMessage(err);
        message.error("自动修复失败，请按下方命令手动处理");
      } finally {
        fixing.value = false;
      }
    },
  });
}

function close() {
  emit("update:show", false);
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    title="远程连接排查与解决方案"
    style="width: 720px"
    @update:show="emit('update:show', $event)"
  >
    <n-alert v-if="error" type="error" :show-icon="true" style="margin-bottom: 14px" title="连接错误">
      <div class="help-error mono-text">{{ error }}</div>
    </n-alert>

    <n-alert type="info" :show-icon="true" style="margin-bottom: 16px">
      远程连接失败通常由 WinRM 未启用、TrustedHosts 未配置、防火墙拦截或 CredSSP 未委派引起。
      下面每段命令都可以直接复制，标注了需要在“本机”还是“远程主机”上执行。
    </n-alert>

    <div v-if="!isWebMode && host?.address" class="fix-box">
      <div class="fix-text">
        <div class="fix-title">一键修复本机 WinRM 客户端</div>
        <div class="fix-desc">
          自动启动 WinRM 服务，并把「{{ host.address }}」加入 TrustedHosts，
          适合“工作组 + IP 地址”导致的 Kerberos / TrustedHosts 错误。需要管理员权限，会弹出 UAC。
        </div>
      </div>
      <n-button
        size="small"
        type="primary"
        :loading="fixing"
        @click="confirmFix"
      >
        <template #icon>
          <n-icon :component="ShieldCheckmarkOutline" />
        </template>
        一键修复本机
      </n-button>
    </div>

    <n-alert v-if="fixResult" type="success" :show-icon="true" style="margin-bottom: 12px">
      <div class="fix-result">{{ fixResult }}</div>
    </n-alert>
    <n-alert v-if="fixError" type="error" :show-icon="true" style="margin-bottom: 12px">
      <div class="fix-result">{{ fixError }}</div>
    </n-alert>

    <div class="command-list">
      <div v-for="item in commands" :key="item.key" class="command-card">
        <div class="command-head">
          <div class="command-title">{{ item.title }}</div>
          <n-tag size="tiny" :type="item.target === '本机' ? 'info' : 'warning'" :bordered="false">
            {{ item.target }}
          </n-tag>
          <div class="spacer"></div>
          <n-button size="tiny" @click="copyCommand(item.command)">
            <template #icon>
              <n-icon :component="CopyOutline" />
            </template>
            复制命令
          </n-button>
        </div>
        <div class="command-desc">{{ item.description }}</div>
        <pre class="command-code">{{ item.command }}</pre>
      </div>
    </div>

    <div class="help-section">
      <div class="help-section-title">常见错误对照</div>
      <ul class="help-list">
        <li>
          <strong>Kerberos 目标名称不正确</strong>：用 IP 连接工作组机器时最常见。配置 TrustedHosts，
          或改用主机名 / 域账号，也可以改用 HTTPS。
        </li>
        <li>
          <strong>未启用 CredSSP 委派</strong>：在远程主机执行 CredSSP 服务端命令，在本机执行客户端委派命令，
          再把认证方式改为 CredSSP。
        </li>
        <li>
          <strong>拒绝访问</strong>：确认用户名密码正确，账户属于远程主机的 Administrators 或
          Hyper-V Administrators 组。
        </li>
        <li>
          <strong>无法连接到远程服务器</strong>：检查地址、端口 5985/5986、WinRM 服务和防火墙规则。
        </li>
      </ul>
    </div>

    <div class="help-section">
      <div class="help-section-title">CredSSP 组策略（域环境）</div>
      <div class="help-text">
        如果 CredSSP 仍提示未委派，请在本机打开 <span class="mono-text">gpedit.msc</span>，进入
        <span class="mono-text">计算机配置 → 管理模板 → 系统 → 凭据分配 → 允许分配新的凭据</span>，
        启用并加入 <span class="mono-text">WSMAN/{{ address }}</span>（必要时再加
        <span class="mono-text">WSMAN/*</span>）。
      </div>
    </div>

    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="close">关闭</n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.help-error {
  font-size: 11.5px;
  line-height: 1.7;
  word-break: break-word;
  max-height: 140px;
  overflow-y: auto;
}

.command-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 380px;
  overflow-y: auto;
  padding-right: 4px;
}

.fix-box {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 14px;
  margin-bottom: 14px;
  border: 1px solid var(--border-strong);
  border-radius: 9px;
  background: var(--icon-bg);
}

.fix-text {
  flex: 1;
  min-width: 0;
}

.fix-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-1);
}

.fix-desc {
  margin-top: 3px;
  font-size: 11.5px;
  line-height: 1.65;
  color: var(--text-3);
}

.fix-result {
  font-size: 11.5px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
}

.command-card {
  border: 1px solid var(--border-soft);
  border-radius: 9px;
  background: var(--surface-inset);
  padding: 11px 13px;
}

.command-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.command-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-1);
}

.command-head .spacer {
  flex: 1;
}

.command-desc {
  margin-top: 4px;
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--muted);
}

.command-code {
  margin: 8px 0 0;
  padding: 9px 11px;
  border-radius: 7px;
  background: var(--code-bg);
  border: 1px solid var(--border);
  color: var(--text-3);
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 11.5px;
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-all;
}

.help-section {
  margin-top: 16px;
}

.help-section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-1);
  margin-bottom: 6px;
}

.help-list {
  margin: 0;
  padding-left: 18px;
  font-size: 11.5px;
  line-height: 1.75;
  color: var(--text-3);
}

.help-list strong {
  color: var(--text-1);
}

.help-text {
  font-size: 11.5px;
  line-height: 1.75;
  color: var(--text-3);
}
</style>
