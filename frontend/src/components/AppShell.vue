<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref, type Component } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NIcon, useMessage, useNotification, type MenuOption } from "naive-ui";
import { Events } from "@wailsio/runtime";
import {
  AddCircleOutline,
  BuildOutline,
  CopyOutline,
  DesktopOutline,
  DocumentTextOutline,
  GitNetworkOutline,
  GridOutline,
  ListOutline,
  ServerOutline,
  SettingsOutline,
  ShieldCheckmarkOutline,
} from "@vicons/ionicons5";
import { TaskFinished } from "../../bindings/events";
import { HyperVService, errorMessage } from "../api";
import type { Task } from "../api";
import { useAppStore } from "../stores/app";
import { useVMStore } from "../stores/vms";
import { useTaskStore } from "../stores/tasks";
import { useSetupStore } from "../stores/setup";
import {
  taskKindLabel,
  taskStatusLabel,
} from "../utils/format";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const notification = useNotification();

const app = useAppStore();
const vmStore = useVMStore();
const taskStore = useTaskStore();
const setup = useSetupStore();
const initializing = ref(true);

function renderIcon(icon: Component) {
  return () => h(NIcon, null, { default: () => h(icon) });
}

const menuOptions = computed<MenuOption[]>(() => [
  {
    label: "总览",
    key: "/",
    icon: renderIcon(GridOutline),
  },
  {
    label: "计算机",
    key: "/hosts",
    icon: renderIcon(ServerOutline),
  },
  { type: "divider", key: "divider-1" },
  {
    label: "虚拟机",
    key: "/vms",
    icon: renderIcon(DesktopOutline),
  },
  {
    label: "新建虚拟机",
    key: "/create",
    icon: renderIcon(AddCircleOutline),
  },
  {
    label: "克隆虚拟机",
    key: "/clone",
    icon: renderIcon(CopyOutline),
  },
  {
    label: () =>
      h("div", { class: "menu-label" }, [
        h("span", "任务队列"),
        taskStore.pendingCount > 0
          ? h("span", { class: "menu-badge" }, String(taskStore.pendingCount))
          : null,
      ]),
    key: "/tasks",
    icon: renderIcon(ListOutline),
  },
  { type: "divider", key: "divider-2" },
  {
    label: "虚拟交换机",
    key: "/network",
    icon: renderIcon(GitNetworkOutline),
  },
  {
    label: "环境检查",
    key: "/setup",
    icon: renderIcon(ShieldCheckmarkOutline),
  },
  {
    label: "主机与工具",
    key: "/host",
    icon: renderIcon(BuildOutline),
  },
  {
    label: "API 文档",
    key: "/api-docs",
    icon: renderIcon(DocumentTextOutline),
  },
  {
    label: "设置",
    key: "/settings",
    icon: renderIcon(SettingsOutline),
  },
]);

const activeKey = computed(() => route.path);

function onMenuSelect(key: string) {
  if (key !== route.path) {
    void router.push(key);
  }
}

function notifyTaskFinished(task: Task) {
  const title = `${taskKindLabel(task.kind)} · ${taskStatusLabel(task.status)}`;
  const content =
    task.status === "failed"
      ? task.error || "任务执行失败，请查看任务日志"
      : `${task.target || task.id} ${taskStatusLabel(task.status)}`;
  if (task.status === "failed") {
    notification.error({ title, content, duration: 6500, keepAliveOnHover: true });
  } else if (task.status === "canceled") {
    notification.warning({ title, content, duration: 4000 });
  } else {
    notification.success({ title, content, duration: 3500 });
  }
  void vmStore.fetch();
}

let offTaskFinished: (() => void) | undefined;
let offHostChanged: (() => void) | undefined;

onMounted(async () => {
  taskStore.bindEvents();
  offTaskFinished = Events.On(TaskFinished, (event: { data: Task }) => {
    if (event?.data) {
      notifyTaskFinished(event.data);
    }
  });
  offHostChanged = Events.On("host:changed", async () => {
    await app.init();
    await vmStore.fetch();
    await taskStore.fetch();
    await setup.check();
  });

  try {
    await app.init();
    await setup.check();
    if (!setup.ready && !setup.skipped && route.path !== "/setup") {
      await router.replace("/setup");
    }
  } finally {
    initializing.value = false;
  }

  void vmStore.fetch();
  void taskStore.fetch();
});

onUnmounted(() => {
  offTaskFinished?.();
  offHostChanged?.();
});

async function restartElevated() {
  try {
    await HyperVService.RestartElevated();
  } catch (err) {
    message.error(errorMessage(err));
  }
}
</script>

<template>
  <div class="shell">
    <aside class="sidebar">
      <div class="sidebar-brand">
        <div class="brand-mark">Hv</div>
        <div class="brand-text">
          <div class="brand-title">Hyper-V 管理助手</div>
          <div class="brand-subtitle">{{ app.hostName }}</div>
        </div>
      </div>

      <div class="sidebar-nav">
        <n-menu
          :options="menuOptions"
          :value="activeKey"
          :indent="16"
          :root-indent="12"
          @update:value="onMenuSelect"
        />
      </div>

      <div class="sidebar-footer">
        <div class="role-line">
          <span class="role-dot" :class="{ admin: app.isAdmin }"></span>
          {{ app.isAdmin ? "管理员权限" : "普通用户权限" }}
        </div>
        <div>
          v{{ app.appVersion || "0.1.0" }}
          <span v-if="app.wailsVersion"> · Wails {{ app.wailsVersion }}</span>
        </div>
      </div>
    </aside>

    <main class="main">
      <div class="host-bar">
        <span
          class="status-dot"
          :class="app.activeHost?.connected ? 'ok' : 'idle'"
        ></span>
        <span class="host-bar-caption">当前主机</span>
        <span class="host-bar-name">{{ app.activeHost?.name || "本机" }}</span>
        <span
          v-if="app.activeHost && !app.activeHost.isLocal"
          class="host-bar-address mono-text"
        >
          {{ app.activeHost.address }}
        </span>
        <span v-if="app.activeHost && !app.activeHost.connected" class="cell-muted">
          （未连接）
        </span>
        <span class="spacer"></span>
        <n-button size="tiny" quaternary @click="router.push('/hosts')">
          切换计算机
        </n-button>
      </div>

      <div v-if="app.loaded && !app.isAdmin" class="admin-banner">
        <span>
          当前进程不是以管理员身份运行。查看虚拟机可能受限，启动 / 克隆 / 新建 / 删除等操作会失败。
        </span>
        <span class="spacer"></span>
        <n-button size="tiny" type="warning" ghost @click="restartElevated">
          以管理员身份重启
        </n-button>
      </div>

      <div v-if="app.loading" class="app-loading-bar">
        <n-progress :percentage="100" :show-indicator="false" status="info" processing />
      </div>

      <div class="content">
        <div class="page">
          <n-alert
            v-if="app.error"
            type="error"
            class="inline-error"
            :title="'初始化失败'"
            closable
          >
            {{ app.error }}
          </n-alert>
          <router-view />
        </div>
      </div>
    </main>

    <div v-if="initializing" class="boot-screen">
      <div class="boot-mark">Hv</div>
      <div class="boot-title">正在检查运行环境</div>
      <div class="boot-desc">
        读取 Windows 版本、CPU 虚拟化、Hyper-V 组件与增强会话状态...
      </div>
      <n-spin size="small" />
    </div>
  </div>
</template>

<style scoped>
.boot-screen {
  position: fixed;
  inset: 0;
  z-index: 100;
  background: var(--bg);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.boot-mark {
  width: 46px;
  height: 46px;
  border-radius: 12px;
  background: #2f6fed;
  color: #fff;
  font-weight: 700;
  font-size: 19px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 8px;
  box-shadow: inset 0 -2px 0 rgba(0, 0, 0, 0.16);
}

.boot-title {
  font-size: 15px;
  font-weight: 600;
  color: #eef1f7;
}

.boot-desc {
  font-size: 12px;
  color: var(--muted);
  margin-bottom: 12px;
}

.app-loading-bar {
  height: 2px;
  overflow: hidden;
  margin-bottom: -2px;
}

.host-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 24px;
  border-bottom: 1px solid var(--border-soft);
  background: #0d1015;
  font-size: 12px;
  color: #aeb8c9;
}

.host-bar-caption {
  color: var(--muted);
}

.host-bar-name {
  color: #e8ecf4;
  font-weight: 500;
}

.host-bar-address {
  font-size: 11px;
  color: #6f7a8d;
}

.host-bar .spacer {
  flex: 1;
}

.app-loading-bar :deep(.n-progress) {
  height: 2px;
}

.app-loading-bar :deep(.n-progress .n-progress-graph-line-fill) {
  border-radius: 0;
}
</style>
