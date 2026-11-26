<script setup lang="ts">
import { reactive, ref, watch } from "vue";
import { useMessage } from "naive-ui";
import { FolderOutline, RefreshOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import { HyperVService, errorMessage } from "../api";
import type { Settings } from "../api";
import { useAppStore } from "../stores/app";

const app = useAppStore();
const message = useMessage();

const saving = ref(false);
const regenerating = ref(false);

const form = reactive<Settings>({
  storageRoot: "",
  defaultGeneration: 2,
  defaultMemoryStartupMB: 4096,
  defaultDynamicMemory: false,
  defaultMemoryMinimumMB: 1024,
  defaultMemoryMaximumMB: 8192,
  defaultCPUCount: 4,
  defaultBandwidthMbps: 100,
  autoRefreshSeconds: 10,
  api: {
    enabled: false,
    listenAddress: "127.0.0.1",
    port: 17890,
    token: "",
  },
});

function copySettings(value: Settings): Settings {
  return {
    ...value,
    api: { ...value.api },
  };
}

watch(
  () => app.settings,
  (settings) => {
    if (settings) {
      Object.assign(form, copySettings(settings));
    }
  },
  { immediate: true, deep: true }
);

async function save() {
  saving.value = true;
  try {
    const saved = await app.saveSettings(copySettings(form));
    message.success("设置已保存");
    if (saved.api.enabled) {
      message.info(`HTTP API 已监听 ${saved.api.listenAddress}:${saved.api.port}`, {
        duration: 5000,
      });
    }
    await app.refreshAPIStatus();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    saving.value = false;
  }
}

async function browseStorageRoot() {
  try {
    const directory = await HyperVService.PickDirectory("选择默认虚拟机存储目录");
    if (directory) {
      form.storageRoot = directory;
    }
  } catch (err) {
    message.error(errorMessage(err));
  }
}

async function copyToken() {
  try {
    await navigator.clipboard.writeText(form.api.token);
    message.success("Token 已复制到剪贴板");
  } catch {
    message.warning("无法访问剪贴板，请手动选择复制");
  }
}

function regenerateToken() {
  regenerating.value = true;
  try {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    form.api.token = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
    message.info("已生成新 Token，点击保存后生效");
  } finally {
    regenerating.value = false;
  }
}

const endpoints = [
  { method: "GET", path: "/api/v1/health" },
  { method: "GET", path: "/api/v1/vms" },
  { method: "POST", path: "/api/v1/vms" },
  { method: "POST", path: "/api/v1/vms/{name}/clone" },
  { method: "PATCH", path: "/api/v1/vms/{name}" },
  { method: "DELETE", path: "/api/v1/vms/{name}?deleteFiles=true" },
  { method: "POST", path: "/api/v1/vms/{name}/power" },
  { method: "GET", path: "/api/v1/tasks" },
  { method: "GET", path: "/api/v1/tasks/{id}" },
  { method: "POST", path: "/api/v1/tasks/{id}/cancel" },
];
</script>

<template>
  <PageHeader title="设置" subtitle="默认硬件参数、存储目录与 HTTP API">
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
            <n-button size="small" @click="browseStorageRoot">
              <template #icon>
                <n-icon :component="FolderOutline" />
              </template>
              浏览
            </n-button>
          </div>
        </n-form-item>

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
        （设置文件与任务历史保存在这里）
      </div>
    </div>

    <div class="panel">
      <div class="panel-header">
        <div>
          <div class="card-title">HTTP API</div>
          <div class="card-subtitle">供脚本或第三方程序提交任务、查询进度</div>
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

      <n-form label-placement="top" size="small">
        <div class="form-grid">
          <n-form-item label="启用 API 服务">
            <n-switch v-model:value="form.api.enabled" />
          </n-form-item>
          <n-form-item label="监听地址">
            <n-input v-model:value="form.api.listenAddress" placeholder="127.0.0.1" />
          </n-form-item>
          <n-form-item label="端口">
            <n-input-number v-model:value="form.api.port" :min="1" :max="65535" style="width: 100%" />
          </n-form-item>
        </div>

        <n-form-item label="Bearer Token">
          <div class="input-with-button">
            <n-input v-model:value="form.api.token" placeholder="请求时放在 Authorization: Bearer <token>" />
            <n-button size="small" @click="copyToken">复制</n-button>
            <n-button size="small" :loading="regenerating" @click="regenerateToken">重新生成</n-button>
          </div>
        </n-form-item>
      </n-form>

      <n-alert v-if="app.api?.error" type="error" style="margin-bottom: 12px">
        {{ app.api.error }}
      </n-alert>
      <n-alert type="info" :show-icon="true">
        默认只监听 127.0.0.1，外部程序需要与本机在同一台机器上。若改为 0.0.0.0
        暴露到局域网，请务必使用强 Token，并注意防火墙与 Hyper-V 权限风险。
      </n-alert>

      <div v-if="app.api?.running" class="api-base">
        API 基地址：<span class="mono-text">http://{{ form.api.listenAddress }}:{{ form.api.port }}</span>
      </div>
      <div class="form-hint" v-else>
        保存并启用后，API 会自动启动；修改端口或 Token 会重启监听。
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="panel-header">
      <div>
        <div class="card-title">API 端点</div>
        <div class="card-subtitle">提交类接口返回 202 与任务对象，使用任务 ID 查询进度</div>
      </div>
    </div>
    <div class="endpoint-list">
      <div v-for="endpoint in endpoints" :key="endpoint.method + endpoint.path" class="api-endpoint">
        <span class="method">{{ endpoint.method }}</span>
        <span class="path">{{ endpoint.path }}</span>
      </div>
    </div>

    <n-collapse style="margin-top: 12px">
      <n-collapse-item title="curl 示例：克隆并查询任务" name="curl">
        <pre class="code-block">curl -X POST http://127.0.0.1:17890/api/v1/vms/win11/clone \
  -H "Authorization: Bearer &lt;token&gt;" \
  -H "Content-Type: application/json" \
  -d '{"newName":"win11-clone","targetDirectory":"D:\\VMs","stopSourceIfRunning":true,
       "memory":{"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
       "cpuCount":4,"switchName":"","bandwidthMbps":100}'

# 返回 202 与任务对象，例如：
# {"id":"T20261126153001A1B2","kind":"clone","status":"queued",...}

curl -H "Authorization: Bearer &lt;token&gt;" \
  http://127.0.0.1:17890/api/v1/tasks/T20261126153001A1B2</pre>
      </n-collapse-item>
    </n-collapse>
  </div>

  <div class="settings-actions">
    <n-button type="primary" :loading="saving" @click="save">保存设置</n-button>
    <n-button :loading="saving" @click="app.init()">重新读取</n-button>
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

.api-base {
  margin-top: 12px;
  font-size: 12px;
  color: #b8c1d2;
}

.endpoint-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 12px;
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

@media (max-width: 1200px) {
  .endpoint-list {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
