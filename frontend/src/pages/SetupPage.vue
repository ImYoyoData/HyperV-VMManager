<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useDialog, useMessage } from "naive-ui";
import {
  ArrowForwardOutline,
  CheckmarkCircleOutline,
  CloseCircleOutline,
  CopyOutline,
  HelpCircleOutline,
  RefreshOutline,
  ShieldCheckmarkOutline,
  WarningOutline,
} from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import { HyperVService, errorMessage } from "../api";
import type { EnvironmentCheck } from "../api";
import { useSetupStore } from "../stores/setup";
import { formatMB } from "../utils/format";

const router = useRouter();
const message = useMessage();
const dialog = useDialog();
const setup = useSetupStore();

const showBiosHelp = ref(false);
const showUpgradeHelp = ref(false);

const status = computed(() => setup.status);

const overallTitle = computed(() => {
  if (!status.value) return "正在检查运行环境";
  switch (status.value.overall) {
    case "ready":
      return "环境已就绪";
    case "unsupported":
      return "当前系统不支持 Hyper-V";
    default:
      return "还需要完成一些配置";
  }
});

const overallClass = computed(() => {
  if (!status.value) return "neutral";
  switch (status.value.overall) {
    case "ready":
      return "success";
    case "unsupported":
      return "danger";
    default:
      return "warning";
  }
});

const overallDescription = computed(() => {
  if (!status.value) {
    return "正在读取 Windows 版本、CPU 虚拟化、Hyper-V 组件与增强会话状态，请稍候。";
  }
  switch (status.value.overall) {
    case "ready":
      return "所有必要检查均已通过，可以开始创建、克隆和管理虚拟机。";
    case "unsupported":
      return "当前 Windows 版本或硬件不满足 Hyper-V 的运行条件。请查看下方说明，必要时升级系统或更换硬件。";
    default:
      return "按下方检查项逐项处理后，点击“重新检查”。部分操作需要重启电脑才能生效。";
  }
});

const featureTutorial = computed(
  () =>
    status.value?.checks?.some(
      (check) => check.id === "hyperv-feature" && check.status !== "ok"
    ) ?? false
);

const biosTutorial = computed(
  () =>
    status.value?.checks?.some(
      (check) => check.id === "virtualization" && check.action === "bios"
    ) ?? false
);

const upgradeTutorial = computed(
  () =>
    status.value?.checks?.some(
      (check) => check.action === "upgrade-windows"
    ) ?? false
);

function checkType(check: EnvironmentCheck): "success" | "warning" | "error" | "default" {
  switch (check.status) {
    case "ok":
      return "success";
    case "warn":
      return "warning";
    case "error":
      return "error";
    default:
      return "default";
  }
}

function checkIcon(check: EnvironmentCheck) {
  switch (check.status) {
    case "ok":
      return CheckmarkCircleOutline;
    case "warn":
      return WarningOutline;
    case "error":
      return CloseCircleOutline;
    default:
      return HelpCircleOutline;
  }
}

function checkStatusLabel(check: EnvironmentCheck): string {
  switch (check.status) {
    case "ok":
      return "通过";
    case "warn":
      return "注意";
    case "error":
      return "需要处理";
    default:
      return "未确定";
  }
}

function actionLabel(action: string): string {
  switch (action) {
    case "elevate":
      return "以管理员身份重启";
    case "open-features":
      return "打开 Windows 功能";
    case "open-services":
      return "打开服务管理器";
    case "fix-enhanced-session":
      return "修复增强会话";
    case "bios":
      return "查看 BIOS 设置教程";
    case "upgrade-windows":
      return "查看版本升级建议";
    default:
      return "";
  }
}

async function runAction(check: EnvironmentCheck) {
  switch (check.action) {
    case "elevate":
      try {
        await HyperVService.RestartElevated();
      } catch (err) {
        message.error(errorMessage(err));
      }
      return;
    case "open-features":
      try {
        await HyperVService.OpenTool("features");
      } catch (err) {
        message.error(errorMessage(err));
      }
      return;
    case "open-services":
      try {
        await HyperVService.OpenTool("services");
      } catch (err) {
        message.error(errorMessage(err));
      }
      return;
    case "fix-enhanced-session":
      dialog.warning({
        title: "修复增强会话",
        content:
          "将开启 Hyper-V 主机增强会话策略，并确保 vmms 与 TermService 服务已启动。此操作需要管理员权限。",
        positiveText: "开始修复",
        negativeText: "取消",
        onPositiveClick: async () => {
          try {
            await setup.fixEnhancedSession();
            message.success("增强会话配置已更新，请重启 VMConnect 后验证");
          } catch (err) {
            message.error(errorMessage(err));
          }
        },
      });
      return;
    case "bios":
      showBiosHelp.value = true;
      return;
    case "upgrade-windows":
      showUpgradeHelp.value = true;
      return;
    default:
      return;
  }
}

async function checkAgain() {
  await setup.check();
  if (setup.ready) {
    message.success("环境检查通过");
  }
}

async function enterApp() {
  setup.skip();
  await router.push("/vms");
}

async function copyPrompt() {
  const prompt = status.value?.aiHelperPrompt;
  if (!prompt) return;
  try {
    await navigator.clipboard.writeText(prompt);
    message.success("提示词已复制，可以发给豆包、元宝等 AI 助手");
  } catch {
    message.warning("无法访问剪贴板，请手动选择复制");
  }
}

async function openFeatures() {
  try {
    await HyperVService.OpenTool("features");
  } catch (err) {
    message.error(errorMessage(err));
  }
}

onMounted(() => {
  if (!setup.checked) {
    void setup.check();
  }
});
</script>

<template>
  <PageHeader
    title="环境检查"
    subtitle="运行 Hyper-V 管理前，确认系统版本、虚拟化、组件与增强会话"
  >
    <n-button size="small" :loading="setup.loading" @click="checkAgain">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      重新检查
    </n-button>
    <n-button
      v-if="setup.checked && !setup.ready"
      size="small"
      @click="enterApp"
    >
      仍然进入
    </n-button>
    <n-button
      v-if="setup.ready"
      size="small"
      type="primary"
      @click="router.push('/vms')"
    >
      <template #icon>
        <n-icon :component="ArrowForwardOutline" />
      </template>
      进入管理
    </n-button>
  </PageHeader>

  <n-alert v-if="setup.error" type="error" class="inline-error" title="环境检查失败">
    {{ setup.error }}
  </n-alert>

  <div class="setup-hero" :class="overallClass">
    <div class="setup-hero-icon">
      <n-icon
        :component="status?.overall === 'ready' ? ShieldCheckmarkOutline : status?.overall === 'unsupported' ? CloseCircleOutline : WarningOutline"
        :size="26"
      />
    </div>
    <div class="setup-hero-text">
      <div class="setup-hero-title">{{ overallTitle }}</div>
      <div class="setup-hero-desc">{{ overallDescription }}</div>
    </div>
    <n-spin v-if="setup.loading" size="small" />
  </div>

  <div v-if="status" class="device-card">
    <div class="info-list">
      <div>
        <div class="info-item-label">设备</div>
        <div class="info-item-value">{{ status.computerName }}</div>
      </div>
      <div>
        <div class="info-item-label">系统</div>
        <div class="info-item-value">
          {{ status.productName }} · {{ status.editionId }} · {{ status.displayVersion }}
          <span class="cell-muted">Build {{ status.buildNumber }}</span>
        </div>
      </div>
      <div>
        <div class="info-item-label">CPU</div>
        <div class="info-item-value">{{ status.cpuName }}</div>
      </div>
      <div>
        <div class="info-item-label">内存</div>
        <div class="info-item-value">
          {{ formatMB(status.totalMemoryMB) }} · {{ status.cpuCores }} 核 / {{ status.logicalProcessors }} 线程
        </div>
      </div>
    </div>
  </div>

  <div class="check-list">
    <div
      v-for="check in setup.checks"
      :key="check.id"
      class="check-item"
      :class="'status-' + check.status"
    >
      <div class="check-icon" :class="'icon-' + check.status">
        <n-icon :component="checkIcon(check)" :size="19" />
      </div>
      <div class="check-body">
        <div class="check-heading">
          <span class="check-title">{{ check.title }}</span>
          <n-tag size="tiny" :type="checkType(check)" :bordered="false">
            {{ checkStatusLabel(check) }}
          </n-tag>
        </div>
        <div class="check-detail">{{ check.detail }}</div>
      </div>
      <div class="check-actions">
        <n-button
          v-if="check.action !== 'none' && actionLabel(check.action)"
          size="tiny"
          tertiary
          @click="runAction(check)"
        >
          {{ actionLabel(check.action) }}
        </n-button>
      </div>
    </div>
  </div>

  <div v-if="featureTutorial" class="panel tutorial-panel">
    <div class="card-title">如何启用 Hyper-V 组件</div>
    <ol class="tutorial-steps">
      <li>点击下方按钮打开“启用或关闭 Windows 功能”。</li>
      <li>在列表中找到 <strong>Hyper-V</strong>，勾选它（包含“Hyper-V 管理工具”和“Hyper-V 平台”）。</li>
      <li>点击“确定”，等待 Windows 安装组件。</li>
      <li>安装完成后按提示重启电脑。</li>
      <li>重启后回到本页面，点击“重新检查”。</li>
    </ol>
    <n-button size="small" type="primary" @click="openFeatures">
      打开“启用或关闭 Windows 功能”
    </n-button>
  </div>

  <n-modal
    v-model:show="showBiosHelp"
    preset="card"
    title="在 BIOS/UEFI 中开启硬件虚拟化"
    style="width: 640px"
  >
    <ol class="tutorial-steps">
      <li>重启电脑，在品牌 Logo 出现时按 <strong>Del</strong>、<strong>F2</strong>、<strong>F10</strong> 或 <strong>Esc</strong> 进入 BIOS/UEFI（不同品牌按键不同）。</li>
      <li>找到 <strong>Advanced</strong>、<strong>CPU Configuration</strong>、<strong>Security</strong> 或 <strong>Virtualization</strong> 菜单。</li>
      <li>将以下项目设为 Enabled：<strong>Intel Virtualization Technology（VT-x）</strong>、<strong>Intel VT-d</strong>，AMD 平台选择 <strong>AMD-V / SVM Mode</strong>。</li>
      <li>按 <strong>F10</strong> 保存并退出，电脑会自动重启。</li>
      <li>进入 Windows 后，打开任务管理器 → 性能 → CPU，确认“虚拟化：已启用”。</li>
    </ol>
    <n-alert type="info" :show-icon="true">
      如果 Hyper-V 已经在运行，BIOS 中的虚拟化选项可能显示为已开启，此时不需要再修改。
    </n-alert>
  </n-modal>

  <n-modal
    v-model:show="showUpgradeHelp"
    preset="card"
    title="Windows 版本与 Hyper-V"
    style="width: 620px"
  >
    <p class="tutorial-paragraph">
      客户端 Hyper-V 管理功能只在以下版本中提供：Windows 专业版、专业工作站版、企业版、教育版，
      以及对应的 Windows Server 版本。Windows 家庭版不包含 Hyper-V 管理器。
    </p>
    <ul class="tutorial-steps">
      <li>打开 设置 → 系统 → 激活，查看当前版本与升级选项。</li>
      <li>可以通过 Microsoft Store 或产品密钥升级到专业版/专业工作站版。</li>
      <li>企业环境请联系 IT 管理员获取合适的版本镜像。</li>
    </ul>
    <n-alert type="warning" :show-icon="true">
      升级系统版本前请备份重要数据，并确认设备满足目标版本的要求。
    </n-alert>
  </n-modal>

  <div v-if="status && !setup.ready" class="panel ai-panel">
    <div class="panel-header">
      <div>
        <div class="card-title">需要 AI 帮你排查？</div>
        <div class="card-subtitle">
          复制下面这段完整提示词，发给豆包、元宝等 AI 助手，让它根据这台设备的信息给出图文教程。
        </div>
      </div>
      <div class="spacer"></div>
      <n-button size="small" @click="copyPrompt">
        <template #icon>
          <n-icon :component="CopyOutline" />
        </template>
        复制提示词
      </n-button>
    </div>
    <n-input
      type="textarea"
      readonly
      :value="status.aiHelperPrompt"
      :autosize="{ minRows: 9, maxRows: 20 }"
      class="prompt-box"
    />
  </div>
</template>

<style scoped>
.setup-hero {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 18px 20px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: #12151c;
  margin-bottom: 14px;
}

.setup-hero.success {
  border-color: #1f4a37;
  background: #101a16;
}

.setup-hero.warning {
  border-color: #4a3a1b;
  background: #19160f;
}

.setup-hero.danger {
  border-color: #4d2525;
  background: #1b1113;
}

.setup-hero-icon {
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #1a1f2b;
  color: #aab4c6;
}

.setup-hero.success .setup-hero-icon {
  background: #123526;
  color: #49c78b;
}

.setup-hero.warning .setup-hero-icon {
  background: #33280f;
  color: #e0a95e;
}

.setup-hero.danger .setup-hero-icon {
  background: #3a181a;
  color: #ef6464;
}

.setup-hero-text {
  flex: 1;
  min-width: 0;
}

.setup-hero-title {
  font-size: 16px;
  font-weight: 600;
  color: #f1f4fa;
}

.setup-hero-desc {
  font-size: 12px;
  color: #97a1b4;
  margin-top: 3px;
  line-height: 1.6;
}

.device-card {
  background: #11141b;
  border: 1px solid var(--border-soft);
  border-radius: 10px;
  padding: 16px 18px;
  margin-bottom: 14px;
}

.check-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}

.check-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 13px 16px;
  border-radius: 9px;
  border: 1px solid var(--border-soft);
  background: var(--panel);
}

.check-item.status-error {
  border-color: #3d2427;
}

.check-icon {
  margin-top: 1px;
  color: #7f889b;
}

.check-icon.icon-ok {
  color: #49c78b;
}

.check-icon.icon-warn {
  color: #e0a95e;
}

.check-icon.icon-error {
  color: #ef6464;
}

.check-body {
  flex: 1;
  min-width: 0;
}

.check-heading {
  display: flex;
  align-items: center;
  gap: 8px;
}

.check-title {
  font-size: 13px;
  font-weight: 500;
  color: #e8ecf4;
}

.check-detail {
  font-size: 12px;
  color: #929cb0;
  margin-top: 4px;
  line-height: 1.65;
  word-break: break-word;
}

.check-actions {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
}

.tutorial-panel {
  margin-bottom: 16px;
}

.tutorial-steps {
  margin: 10px 0 16px;
  padding-left: 20px;
  font-size: 12.5px;
  line-height: 1.85;
  color: #b5bfd0;
}

.tutorial-steps strong {
  color: #eef1f7;
}

.tutorial-paragraph {
  font-size: 12.5px;
  line-height: 1.8;
  color: #b5bfd0;
  margin: 0 0 10px;
}

.ai-panel {
  margin-top: 16px;
}

.prompt-box :deep(.n-input__textarea-el) {
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 11.5px;
  line-height: 1.7;
}
</style>
