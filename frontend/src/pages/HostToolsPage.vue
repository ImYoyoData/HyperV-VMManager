<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useMessage } from "naive-ui";
import {
  ConstructOutline,
  ExtensionPuzzleOutline,
  GitNetworkOutline,
  RefreshOutline,
  ServerOutline,
  SettingsOutline,
  SpeedometerOutline,
} from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import { HyperVService, errorMessage } from "../api";
import type { EnhancedSessionStatus } from "../api";
import { useAppStore } from "../stores/app";
import { formatMB } from "../utils/format";

const app = useAppStore();
const message = useMessage();

const session = ref<EnhancedSessionStatus | null>(null);
const sessionLoading = ref(false);
const sessionError = ref("");
const toggling = ref(false);

const host = computed(() => app.hostInfo);

const tools = [
  {
    key: "hyperv-manager",
    label: "Hyper-V 管理器",
    hint: "virtmgmt.msc",
    icon: ServerOutline,
  },
  {
    key: "switch-manager",
    label: "虚拟交换机管理器",
    hint: "网络与桥接配置",
    icon: GitNetworkOutline,
  },
  {
    key: "hyperv-settings",
    label: "Hyper-V 主机设置",
    hint: "默认路径与策略",
    icon: SettingsOutline,
  },
  {
    key: "services",
    label: "Windows 服务",
    hint: "services.msc",
    icon: ConstructOutline,
  },
  {
    key: "features",
    label: "Windows 功能",
    hint: "启用或关闭 Hyper-V",
    icon: ExtensionPuzzleOutline,
  },
  {
    key: "taskmgr",
    label: "任务管理器",
    hint: "性能与进程",
    icon: SpeedometerOutline,
  },
];

async function loadSession() {
  sessionLoading.value = true;
  sessionError.value = "";
  try {
    session.value = await HyperVService.GetEnhancedSessionStatus();
  } catch (err) {
    sessionError.value = errorMessage(err);
  } finally {
    sessionLoading.value = false;
  }
}

async function toggleSession(enabled: boolean) {
  toggling.value = true;
  try {
    await HyperVService.SetEnhancedSession(enabled);
    message.success(enabled ? "增强会话已开启" : "增强会话已关闭");
    await loadSession();
    await app.refreshHostInfo();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    toggling.value = false;
  }
}

async function openTool(key: string) {
  try {
    await HyperVService.OpenTool(key);
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function restartElevated() {
  try {
    await HyperVService.RestartElevated();
  } catch (err) {
    message.error(errorMessage(err));
  }
}

onMounted(() => {
  void app.refreshHostInfo();
  void loadSession();
});
</script>

<template>
  <PageHeader title="主机与工具" subtitle="增强会话、系统信息与常用管理工具">
    <n-button size="small" :loading="sessionLoading" @click="loadSession">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      刷新状态
    </n-button>
    <n-button size="small" @click="app.refreshHostInfo()">刷新主机信息</n-button>
  </PageHeader>

  <n-alert v-if="!app.isAdmin" type="warning" class="inline-error" title="需要管理员权限">
    <div class="admin-alert">
      <span>增强会话开关与部分管理工具需要管理员权限。</span>
      <n-button size="tiny" type="warning" ghost @click="restartElevated">
        以管理员身份重启
      </n-button>
    </div>
  </n-alert>

  <div class="dashboard-grid">
    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">系统与 Hyper-V</div>
          <div class="card-subtitle">主机信息读取自 Hyper-V PowerShell 模块</div>
        </div>
      </div>

      <div v-if="host" class="info-list">
        <div>
          <div class="info-item-label">计算机名</div>
          <div class="info-item-value">{{ host.computerName }}</div>
        </div>
        <div>
          <div class="info-item-label">系统类型</div>
          <div class="info-item-value">{{ host.installationType || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">操作系统</div>
          <div class="info-item-value">{{ host.osName || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">Hyper-V 版本</div>
          <div class="info-item-value">{{ host.hyperVVersion || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">逻辑处理器</div>
          <div class="info-item-value">{{ host.logicalProcessors || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">物理内存</div>
          <div class="info-item-value">{{ formatMB(host.totalMemoryMB) }}</div>
        </div>
        <div>
          <div class="info-item-label">vmms 服务</div>
          <div class="info-item-value">
            <span class="status-dot" :class="host.vmServiceStatus === 'Running' ? 'ok' : 'bad'"></span>
            {{ host.vmServiceStatus || "未知" }}
          </div>
        </div>
        <div>
          <div class="info-item-label">TermService 服务</div>
          <div class="info-item-value">
            <span class="status-dot" :class="host.termServiceStatus === 'Running' ? 'ok' : 'idle'"></span>
            {{ host.termServiceStatus || "未知" }}
          </div>
        </div>
        <div>
          <div class="info-item-label">虚拟机默认目录</div>
          <div class="info-item-value mono-text">{{ host.virtualMachinePath || "—" }}</div>
        </div>
        <div>
          <div class="info-item-label">虚拟硬盘默认目录</div>
          <div class="info-item-value mono-text">{{ host.virtualHardDiskPath || "—" }}</div>
        </div>
      </div>
      <div v-else class="empty-hint">
        {{ app.hostError || "正在读取主机信息..." }}
      </div>
    </div>

    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">增强会话</div>
          <div class="card-subtitle">VMConnect 的剪贴板、驱动器重定向与更高分辨率</div>
        </div>
      </div>

      <n-alert v-if="sessionError" type="warning" class="inline-error">
        {{ sessionError }}
      </n-alert>

      <n-spin :show="sessionLoading">
        <div v-if="session">
          <n-descriptions :column="2" label-placement="top" size="small" bordered>
            <n-descriptions-item label="系统类型">
              {{ session.installationType || "—" }}
            </n-descriptions-item>
            <n-descriptions-item label="支持状态">
              {{ session.supported ? "支持" : "不支持（Server Core）" }}
            </n-descriptions-item>
            <n-descriptions-item label="主机策略">
              <n-tag size="small" :type="session.hostPolicyEnabled ? 'success' : 'default'" :bordered="false">
                {{ session.hostPolicyEnabled ? "已开启" : "已关闭" }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="注册表值">
              {{ session.registryValue }}
            </n-descriptions-item>
            <n-descriptions-item label="vmms 服务">
              {{ session.vmServiceStatus || "未知" }}
            </n-descriptions-item>
            <n-descriptions-item label="TermService 服务">
              {{ session.termServiceStatus || "未知" }}
            </n-descriptions-item>
          </n-descriptions>

          <div class="settings-actions">
            <n-button
              size="small"
              type="primary"
              :disabled="!session.supported"
              :loading="toggling"
              @click="toggleSession(true)"
            >
              开启增强会话
            </n-button>
            <n-button
              size="small"
              :disabled="!session.supported"
              :loading="toggling"
              @click="toggleSession(false)"
            >
              关闭增强会话
            </n-button>
          </div>
          <div class="form-hint">
            开启后需要关闭并重新打开 VMConnect 窗口才会生效。
          </div>
        </div>
        <div v-else-if="!sessionLoading" class="empty-hint">
          无法读取增强会话状态
        </div>
      </n-spin>
    </div>
  </div>

  <div class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">管理工具</div>
        <div class="card-subtitle">调用 Windows 原生管理器与系统工具</div>
      </div>
    </div>
    <div class="tool-grid">
      <button
        v-for="tool in tools"
        :key="tool.key"
        class="tool-tile"
        @click="openTool(tool.key)"
      >
        <span class="tool-icon"><n-icon :component="tool.icon" /></span>
        <span>
          <div class="tool-label">{{ tool.label }}</div>
          <div class="tool-hint">{{ tool.hint }}</div>
        </span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.admin-alert {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
</style>
