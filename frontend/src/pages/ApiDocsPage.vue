<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import { CopyOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import { useAppStore } from "../stores/app";

const router = useRouter();
const message = useMessage();
const app = useAppStore();

interface ApiParam {
  name: string;
  type: string;
  required?: string;
  description: string;
}

interface ApiEndpoint {
  method: "GET" | "POST" | "PATCH" | "DELETE";
  path: string;
  title: string;
  description: string;
  params?: ApiParam[];
  body?: ApiParam[];
  response?: string;
  example?: string;
}

interface ApiGroup {
  title: string;
  description: string;
  endpoints: ApiEndpoint[];
}

const groups: ApiGroup[] = [
  {
    title: "基础信息",
    description: "鉴权、主机与网络信息查询。所有请求都需要 Bearer Token。",
    endpoints: [
      {
        method: "GET",
        path: "/api/v1/health",
        title: "健康检查",
        description: "用于检测 API 服务是否在线。不返回业务数据。",
        response: `{"status":"ok","time":"2026-11-26T15:30:00+08:00"}`,
      },
      {
        method: "GET",
        path: "/api/v1/host",
        title: "主机信息",
        description:
          "返回 Hyper-V 主机名、操作系统、Hyper-V 版本、CPU、内存、默认虚拟机目录与增强会话状态。",
        response: `{"computerName":"YOYO","osName":"Windows 11 Pro 10.0.26100","hyperVVersion":"2.0.0.0","logicalProcessors":16,"totalMemoryMB":32768,"enhancedSessionEnabled":true,...}`,
      },
      {
        method: "GET",
        path: "/api/v1/switches",
        title: "虚拟交换机列表",
        description: "返回所有虚拟交换机及类型、上行网卡、管理 OS 状态。",
        response: `{"items":[{"name":"Default Switch","switchType":"Internal","allowManagementOS":true,...}]}`,
      },
    ],
  },
  {
    title: "虚拟机查询与电源",
    description: "查询虚拟机状态，或提交电源任务。电源操作是异步任务。",
    endpoints: [
      {
        method: "GET",
        path: "/api/v1/vms",
        title: "虚拟机列表",
        description:
          "返回所有虚拟机，包含状态、代数、CPU、内存、运行时长、网络适配器、磁盘与 DVD 信息。",
        response: `{"items":[{"name":"win11","state":"Running","generation":2,"processorCount":4,"memoryStartupMB":4096,"networks":[...]}]}`,
      },
      {
        method: "GET",
        path: "/api/v1/vms/{name}",
        title: "虚拟机详情",
        description: "按名称查询单台虚拟机。名称需要 URL 编码。",
        params: [{ name: "name", type: "string（路径）", required: "是", description: "虚拟机名称" }],
      },
      {
        method: "GET",
        path: "/api/v1/vms/{name}/suggest-clone-path",
        title: "建议克隆目录",
        description: "返回源虚拟机所在目录，用于克隆向导的“跟随源机”选项。",
        params: [{ name: "name", type: "string（路径）", required: "是", description: "源虚拟机名称" }],
        response: `{"directory":"D:\\\\VMs"}`,
      },
      {
        method: "POST",
        path: "/api/v1/vms/{name}/power",
        title: "电源操作",
        description: "提交启动、关机、强制断电、保存、暂停、恢复或重启任务。",
        params: [
          { name: "name", type: "string（路径）", required: "是", description: "虚拟机名称" },
        ],
        body: [
          {
            name: "action",
            type: "string",
            required: "是",
            description:
              "start 启动 | stop 正常关机 | turnoff 强制断电 | save 保存状态 | pause 暂停 | resume 恢复 | restart 重启",
          },
        ],
        response: `202 Accepted，返回 queued 状态的任务对象`,
        example: `curl -X POST http://127.0.0.1:17890/api/v1/vms/win11/power \\
  -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{"action":"start"}'`,
      },
    ],
  },
  {
    title: "异步任务：新建 / 克隆 / 编辑 / 删除",
    description:
      "所有写操作进入同一串行队列，返回 202 与任务对象。用任务 ID 轮询进度。",
    endpoints: [
      {
        method: "POST",
        path: "/api/v1/vms",
        title: "新建虚拟机",
        description:
          "从空硬盘、差异磁盘或完整复制创建虚拟机。任务会先创建虚拟硬盘，再创建虚拟机并应用硬件配置。",
        body: [
          { name: "name", type: "string", required: "是", description: "新虚拟机名称，不能包含 \\ / : * ? \" < > |" },
          { name: "generation", type: "number", required: "是", description: "代数：1（BIOS）或 2（UEFI）" },
          { name: "directory", type: "string", required: "是", description: "存储根目录，例如 D:\\VMs（绝对路径）" },
          {
            name: "memory",
            type: "object",
            required: "是",
            description:
              "内存配置：startupMB 启动内存、dynamic 是否动态内存、minimumMB 最小值、maximumMB 最大值（单位 MB）",
          },
          { name: "cpuCount", type: "number", required: "是", description: "虚拟处理器数量，1-128" },
          { name: "switchName", type: "string", required: "否", description: "虚拟交换机名称；空字符串表示不连接" },
          { name: "bandwidthMbps", type: "number", required: "否", description: "网卡带宽上限（Mbps），0 表示不限，默认 100" },
          {
            name: "diskMode",
            type: "string",
            required: "是",
            description: "new 新建空硬盘 | differencing 差异磁盘 | copy 完整复制",
          },
          { name: "imagePath", type: "string", required: "条件", description: "diskMode 为 differencing / copy 时必填，虚拟硬盘路径（.vhdx/.vhd）" },
          { name: "diskSizeGB", type: "number", required: "条件", description: "diskMode 为 new 时必填，空硬盘大小（GB）" },
          { name: "isoPath", type: "string", required: "否", description: "安装 ISO 路径；填写后会自动挂载并设置光驱优先启动" },
          { name: "disableCheckpoints", type: "boolean", required: "否", description: "是否禁用检查点，默认 true" },
        ],
        response: `202 Accepted`,
        example: `curl -X POST http://127.0.0.1:17890/api/v1/vms \\
  -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "win11-test",
    "generation": 2,
    "directory": "D:\\\\VMs",
    "memory": {"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
    "cpuCount": 4,
    "switchName": "Default Switch",
    "bandwidthMbps": 100,
    "diskMode": "differencing",
    "imagePath": "D:\\\\Images\\\\win11.vhdx",
    "diskSizeGB": 60,
    "isoPath": "",
    "disableCheckpoints": true
  }'`,
      },
      {
        method: "POST",
        path: "/api/v1/vms/{name}/clone",
        title: "克隆虚拟机",
        description:
          "整台复制源虚拟机：导出 → 导入 → 重命名 → 应用内存/CPU/网络/带宽，并重新生成动态 MAC 地址。源机运行时会先关机（可关闭该行为）。",
        params: [{ name: "name", type: "string（路径）", required: "是", description: "源虚拟机名称" }],
        body: [
          { name: "newName", type: "string", required: "是", description: "新虚拟机名称" },
          { name: "targetDirectory", type: "string", required: "是", description: "存放根目录，最终目录为 targetDirectory\\newName" },
          { name: "stopSourceIfRunning", type: "boolean", required: "否", description: "源机运行中时是否自动关机；默认 false，未开启时会拒绝执行" },
          { name: "memory", type: "object", required: "是", description: "新机内存配置，字段同新建" },
          { name: "cpuCount", type: "number", required: "是", description: "新机 CPU 核数" },
          { name: "switchName", type: "string", required: "否", description: "新交换机名称；空字符串表示保持源机网络" },
          { name: "bandwidthMbps", type: "number", required: "否", description: "新机带宽上限，0 表示不限" },
        ],
        example: `curl -X POST http://127.0.0.1:17890/api/v1/vms/win11/clone \\
  -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{"newName":"win11-clone","targetDirectory":"D:\\\\VMs",
       "stopSourceIfRunning":true,
       "memory":{"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
       "cpuCount":4,"switchName":"","bandwidthMbps":100}'`,
      },
      {
        method: "PATCH",
        path: "/api/v1/vms/{name}",
        title: "编辑虚拟机",
        description:
          "修改指定字段，未提供（或为 null）的字段保持原值。重命名会改变后续接口使用的名称。",
        params: [{ name: "name", type: "string（路径）", required: "是", description: "当前虚拟机名称" }],
        body: [
          { name: "newName", type: "string", required: "否", description: "新名称" },
          { name: "memory", type: "object | null", required: "否", description: "新的内存配置" },
          { name: "cpuCount", type: "number | null", required: "否", description: "新的 CPU 核数" },
          { name: "switchName", type: "string | null", required: "否", description: "新的虚拟交换机名称" },
          { name: "bandwidthMbps", type: "number | null", required: "否", description: "新的带宽上限（Mbps）" },
          { name: "checkpointsEnabled", type: "boolean | null", required: "否", description: "是否启用检查点" },
        ],
        example: `curl -X PATCH http://127.0.0.1:17890/api/v1/vms/win11 \\
  -H "Authorization: Bearer <token>" \\
  -H "Content-Type: application/json" \\
  -d '{"cpuCount":8,"bandwidthMbps":200}'`,
      },
      {
        method: "DELETE",
        path: "/api/v1/vms/{name}",
        title: "删除虚拟机",
        description:
          "删除虚拟机配置。默认只删除虚拟机，不删除磁盘文件；需同时删除文件时设置 deleteFiles=true。运行中的虚拟机会先强制关机。",
        params: [
          { name: "name", type: "string（路径）", required: "是", description: "虚拟机名称" },
          { name: "deleteFiles", type: "boolean（查询参数）", required: "否", description: "true 时同时删除虚拟机目录，默认 false" },
        ],
        example: `curl -X DELETE "http://127.0.0.1:17890/api/v1/vms/win11?deleteFiles=false" \\
  -H "Authorization: Bearer <token>"`,
      },
    ],
  },
  {
    title: "任务队列",
    description:
      "提交类接口返回的任务都有唯一 ID。轮询 GET /tasks/{id}，读取 status / progress / step / logs 判断进度。",
    endpoints: [
      {
        method: "GET",
        path: "/api/v1/tasks",
        title: "任务列表",
        description: "返回最近的任务，新的在前。可以用 limit 限制数量（默认 100，最大 200）。",
        params: [{ name: "limit", type: "number（查询参数）", required: "否", description: "返回数量，默认 100" }],
        response: `{"items":[{"id":"T20261126153001A1B2","kind":"clone","status":"running","progress":55,...}]}`,
      },
      {
        method: "GET",
        path: "/api/v1/tasks/{id}",
        title: "任务详情",
        description:
          "按任务 ID 查询状态、进度、当前步骤与日志。status 取值：queued / running / succeeded / failed / canceled。",
        params: [{ name: "id", type: "string（路径）", required: "是", description: "任务 ID" }],
        response: `{"id":"T20261126153001A1B2","status":"running","progress":55,"step":"导入新虚拟机并重命名","logs":[{"time":"...","level":"info","message":"..."}]}`,
      },
      {
        method: "POST",
        path: "/api/v1/tasks/{id}/cancel",
        title: "取消任务",
        description:
          "排队中的任务会立即取消；运行中的任务会请求终止当前 PowerShell 操作，已产生的部分文件可能需要手动清理。",
        params: [{ name: "id", type: "string（路径）", required: "是", description: "任务 ID" }],
      },
    ],
  },
];

const baseURL = computed(() => app.apiBaseURL || "http://127.0.0.1:17890");
const token = computed(() => app.settings?.api.token || "<token>");

const taskFields: ApiParam[] = [
  { name: "id", type: "string", description: "任务 ID，用于后续查询" },
  { name: "kind", type: "string", description: "clone | create | edit | delete | power" },
  { name: "target", type: "string", description: "目标虚拟机名称" },
  { name: "status", type: "string", description: "queued | running | succeeded | failed | canceled" },
  { name: "progress", type: "number", description: "进度百分比 0-100" },
  { name: "step", type: "string", description: "当前步骤描述" },
  { name: "error", type: "string", description: "失败原因（仅失败时）" },
  { name: "logs", type: "array", description: "执行日志，包含 time / level / message" },
];

async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    message.success("已复制");
  } catch {
    message.warning("无法访问剪贴板，请手动复制");
  }
}
</script>

<template>
  <PageHeader
    title="API 文档"
    subtitle="供自动化脚本、业务系统对接的完整 HTTP API 说明"
  >
    <n-tag
      size="small"
      :type="app.api?.running ? 'success' : 'default'"
      :bordered="false"
    >
      {{ app.api?.running ? "API 运行中" : "API 未运行" }}
    </n-tag>
    <n-button size="small" @click="copyText(baseURL)">
      <template #icon>
        <n-icon :component="CopyOutline" />
      </template>
      复制 Base URL
    </n-button>
  </PageHeader>

  <div class="api-overview panel">
    <div class="overview-grid">
      <div>
        <div class="info-item-label">Base URL</div>
        <div class="info-item-value mono-text">{{ baseURL }}</div>
      </div>
      <div>
        <div class="info-item-label">认证</div>
        <div class="info-item-value">
          <span class="mono-text">Authorization: Bearer &lt;token&gt;</span>
        </div>
      </div>
      <div>
        <div class="info-item-label">Content-Type</div>
        <div class="info-item-value mono-text">application/json</div>
      </div>
      <div>
        <div class="info-item-label">任务模式</div>
        <div class="info-item-value">提交返回 202 + 任务 ID，轮询任务获取进度</div>
      </div>
    </div>
    <div class="overview-actions">
      <n-button size="tiny" tertiary @click="router.push('/settings')">前往设置开启 API</n-button>
      <span class="form-hint">
        默认只监听 127.0.0.1:17890。需要公网访问时请配合反向代理、HTTPS 与强 Token。
      </span>
    </div>
  </div>

  <div class="panel api-summary">
    <div class="card-title">任务对象字段</div>
    <div class="api-table-wrap">
      <table class="api-table">
        <thead>
          <tr><th>字段</th><th>类型</th><th>说明</th></tr>
        </thead>
        <tbody>
          <tr v-for="field in taskFields" :key="field.name">
            <td class="mono-text">{{ field.name }}</td>
            <td class="cell-muted">{{ field.type }}</td>
            <td>{{ field.description }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <n-alert type="info" :show-icon="true" style="margin-top: 12px">
      所有写操作共用一个串行队列：同一时间只执行一个克隆/新建/删除，避免导出、导入和磁盘复制相互干扰。
      提交任务时如果同一目标已有未完成的同类任务，会返回 400。
    </n-alert>
    <n-alert type="warning" :show-icon="true" style="margin-top: 10px">
      当前版本 API 只操作本机 Hyper-V。多主机连接（远程 Hyper-V 服务器）将在后续版本加入
      hostId 或 /hosts 接口；届时提交任务时需携带目标计算机标识。
    </n-alert>
  </div>

  <div v-for="group in groups" :key="group.title" class="api-group">
    <div class="api-group-heading">
      <div class="api-group-title">{{ group.title }}</div>
      <div class="api-group-desc">{{ group.description }}</div>
    </div>

    <n-collapse>
      <n-collapse-item
        v-for="endpoint in group.endpoints"
        :key="endpoint.method + endpoint.path"
        :name="endpoint.method + endpoint.path"
      >
        <template #header>
          <div class="endpoint-header">
            <span class="method" :class="'method-' + endpoint.method.toLowerCase()">
              {{ endpoint.method }}
            </span>
            <span class="endpoint-path mono-text">{{ endpoint.path }}</span>
            <span class="endpoint-title">{{ endpoint.title }}</span>
          </div>
        </template>

        <div class="endpoint-description">{{ endpoint.description }}</div>

        <div v-if="endpoint.params && endpoint.params.length > 0" class="api-section">
          <div class="api-section-title">路径 / 查询参数</div>
          <div class="api-table-wrap">
            <table class="api-table">
              <thead>
                <tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="param in endpoint.params" :key="param.name">
                  <td class="mono-text">{{ param.name }}</td>
                  <td class="cell-muted">{{ param.type }}</td>
                  <td class="cell-muted">{{ param.required || "否" }}</td>
                  <td>{{ param.description }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-if="endpoint.body && endpoint.body.length > 0" class="api-section">
          <div class="api-section-title">请求体</div>
          <div class="api-table-wrap">
            <table class="api-table">
              <thead>
                <tr><th>字段</th><th>类型</th><th>必填</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="param in endpoint.body" :key="param.name">
                  <td class="mono-text">{{ param.name }}</td>
                  <td class="cell-muted">{{ param.type }}</td>
                  <td class="cell-muted">{{ param.required || "否" }}</td>
                  <td>{{ param.description }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-if="endpoint.response" class="api-section">
          <div class="api-section-title">响应</div>
          <pre class="code-block">{{ endpoint.response }}</pre>
        </div>

        <div v-if="endpoint.example" class="api-section">
          <div class="api-section-title">示例</div>
          <pre class="code-block">{{ endpoint.example.replace("<token>", token) }}</pre>
        </div>
      </n-collapse-item>
    </n-collapse>
  </div>

  <div class="panel">
    <div class="card-title">完整示例：克隆并轮询进度</div>
    <pre class="code-block"># 1. 提交克隆任务
curl -X POST "{{ baseURL }}/api/v1/vms/win11/clone" \
  -H "Authorization: Bearer {{ token }}" \
  -H "Content-Type: application/json" \
  -d '{"newName":"win11-clone","targetDirectory":"D:\\VMs",
       "stopSourceIfRunning":true,
       "memory":{"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
       "cpuCount":4,"switchName":"","bandwidthMbps":100}'

# 2. 返回 202，记录任务 ID
# {"id":"T20261126153001A1B2","status":"queued","progress":0,...}

# 3. 轮询任务状态
curl -H "Authorization: Bearer {{ token }}" \
  "{{ baseURL }}/api/v1/tasks/T20261126153001A1B2"

# 4. 当 status 为 succeeded / failed / canceled 时结束轮询
# progress: 0-100；step: 当前步骤；logs: 详细日志</pre>

    <div class="card-title" style="margin-top: 16px">PowerShell 调用示例</div>
    <pre class="code-block">$headers = @{
  Authorization = "Bearer {{ token }}"
  "Content-Type" = "application/json"
}

$body = @{
  newName = "win11-clone"
  targetDirectory = "D:\VMs"
  stopSourceIfRunning = $true
  memory = @{ startupMB = 4096; dynamic = $false; minimumMB = 1024; maximumMB = 8192 }
  cpuCount = 4
  switchName = ""
  bandwidthMbps = 100
} | ConvertTo-Json -Depth 6

$task = Invoke-RestMethod -Method Post -Uri "{{ baseURL }}/api/v1/vms/win11/clone" -Headers $headers -Body $body
do {
  Start-Sleep -Seconds 2
  $task = Invoke-RestMethod -Uri "{{ baseURL }}/api/v1/tasks/$($task.id)" -Headers $headers
  Write-Host "$($task.status) $($task.progress)% $($task.step)"
} while ($task.status -in @("queued", "running"))</pre>
  </div>
</template>

<style scoped>
.api-overview {
  margin-bottom: 14px;
}

.overview-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px 22px;
}

.overview-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border-soft);
}

.api-summary {
  margin-bottom: 20px;
}

.api-group {
  margin-bottom: 22px;
}

.api-group-heading {
  margin-bottom: 8px;
}

.api-group-title {
  font-size: 14px;
  font-weight: 600;
  color: #eef1f7;
}

.api-group-desc {
  font-size: 12px;
  color: var(--muted);
  margin-top: 3px;
  line-height: 1.6;
}

.endpoint-header {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.method {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 7px;
  border-radius: 5px;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.4px;
  font-family: "Cascadia Code", "Consolas", monospace;
}

.method-get {
  background: #15283f;
  color: #7db3ff;
}

.method-post {
  background: #17342a;
  color: #7fd9ad;
}

.method-patch {
  background: #392d14;
  color: #e6bc6f;
}

.method-delete {
  background: #3a1c1f;
  color: #f08c8c;
}

.endpoint-path {
  flex: 0 1 auto;
  color: #d7deea;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.endpoint-title {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: 11.5px;
}

.endpoint-description {
  font-size: 12.5px;
  color: #a6b0c2;
  line-height: 1.75;
  margin-bottom: 12px;
}

.api-section {
  margin-top: 12px;
}

.api-section-title {
  font-size: 11.5px;
  font-weight: 600;
  color: #cfd6e4;
  margin-bottom: 6px;
}

.api-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
}

.api-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.api-table th {
  text-align: left;
  padding: 8px 10px;
  background: #181c26;
  color: #aeb8c9;
  font-weight: 500;
  white-space: nowrap;
  border-bottom: 1px solid var(--border);
}

.api-table td {
  padding: 8px 10px;
  border-bottom: 1px solid #1c202a;
  color: #c3cbd9;
  line-height: 1.6;
  vertical-align: top;
}

.api-table tr:last-child td {
  border-bottom: none;
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
  .overview-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
