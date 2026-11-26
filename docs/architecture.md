# Hyper-V 虚拟机管理助手 · 设计说明

> Wails v3 + Go + Vue 3 + Naive UI 的 Windows 桌面应用，把原有三个 PowerShell 脚本的
> 功能拆分为可视化模块，并为后续的 HTTP API 调用预留统一的任务队列。

## 1. 原始脚本功能映射

| 原始脚本 | 功能 | 新位置 |
| --- | --- | --- |
| `Hyper-V先运行我申请权限.bat` | UAC 提权、放开 .ps1 执行策略、启动菜单 | 应用启动时检测管理员权限，提供"以管理员身份重启"；脚本由应用内部调用，不再依赖 ExecutionPolicy |
| `Hyper-V管理菜单.ps1` 1~4、6、8、9 | 打开 Hyper-V 管理器 / 交换机管理器 / 主机设置 / 服务 / Windows 功能 / 任务管理器 | 主机与工具页（快捷工具卡片） |
| `Hyper-V管理菜单.ps1` 5 | VMConnect 连接虚拟机 | 虚拟机列表 / 详情中的"连接"按钮 |
| `Hyper-V管理菜单.ps1` 7 | 查看所有虚拟机状态 | 虚拟机页、总览页 |
| `Hyper-V管理菜单.ps1` A/B/C | 增强会话开启 / 关闭 / 状态 | 主机与工具页（增强会话卡片） |
| `-TUI.ps1` 克隆 | 导出 → 导入 → 重命名 → 应用内存/CPU/网络/带宽 | 克隆虚拟机向导，拆分为多个任务步骤并显示进度 |
| `-TUI.ps1` 新建 | 磁盘（差异/复制/空盘）+ New-VM + ISO + 硬件配置 | 新建虚拟机向导 |
| `-TUI.ps1` 编辑 | 内存 / CPU / 交换机 / 带宽 / 重命名 | 虚拟机详情 → 编辑面板 |
| `-TUI.ps1` 删除 | 二次确认 + 输入名称 + 可选删目录 | 删除确认弹窗（需输入名称、可选删除文件） |
| `-TUI.ps1` 默认配置 | 目录、内存、动态内存、CPU、带宽、代数 | 设置页 |

## 2. 架构分层

```
┌──────────────────────────────┐
│  Vue 3 + Naive UI (前端)      │
│  Pinia + Vue Router           │
└──────────────┬───────────────┘
               │ Wails Bindings / Events
┌──────────────▼───────────────┐
│  internal/service             │  GUI 门面：绑定方法、参数校验、事件推送
│  - 提交任务 / 查询 VM / 设置  │
└──────┬──────────────┬────────┘
       │              │
┌──────▼──────┐ ┌─────▼───────────────┐
│ tasks.Queue │ │ internal/httpapi     │
│ 单 worker   │ │ HTTP API（可开关）    │
│ 持久化/进度 │ │ 与 GUI 共用队列       │
└──────┬──────┘ └─────────────────────┘
       │ Handler
┌──────▼────────────────────────┐
│  internal/hyperv               │
│  PowerShell 执行层 + 业务脚本  │
│  - VM 列表/电源/克隆/新建/删除 │
│  - 主机信息/交换机/增强会话    │
└───────────────────────────────┘
```

设计原则：

- **所有写操作都进队列**：克隆、新建、编辑、删除、电源操作统一排队，串行执行，避免多个
  Export-VM / New-VM 相互干扰。
- **任务状态与界面解耦**：任务有稳定的 ID、状态、进度、日志，GUI 和 HTTP API 查询的是
  同一份数据。
- **PowerShell 仅作为 Hyper-V 的调用方式**：脚本从临时 UTF-8(BOM) 文件加载，参数通过
  环境变量以 Base64 JSON 传递，输出以 Base64 JSON 回传，避免中文乱码和引号注入。
- **权限失败可诊断**：任务日志会保留 PowerShell 的原始错误，前端按任务状态展示。

## 3. 任务队列

### 3.1 数据模型

```go
type Task struct {
    ID           string     // T20261126153001A1B2
    Kind         string     // clone | create | edit | delete | power
    Target       string     // 虚拟机名称
    Status       string     // queued | running | succeeded | failed | canceled
    Progress     int        // 0-100
    Step         string     // 当前步骤描述
    Payload      json.RawMessage
    Result       map[string]any
    Error        string
    CreatedAt    time.Time
    StartedAt    *time.Time
    FinishedAt   *time.Time
    Logs         []TaskLog
}
```

### 3.2 状态机

```
queued ──开始执行──> running ──成功──> succeeded
   │                    │
   ├──取消──> canceled  ├──失败──> failed
   └──取消──> canceled  └──取消──> canceled
```

- 队列为单 worker，FIFO 执行。
- 取消运行中的任务会终止 PowerShell 进程，Hyper-V 自身可能留下半成品文件，会在任务日志中
  说明。
- 每次状态 / 进度变化都会通过 Wails 事件 `task:update` 推送；完成时额外推送 `task:finished`。
- 任务与设置持久化到 `%AppData%\HyperV-VMManager\`。应用异常退出后，正在执行的任务在下次
  启动时标记为失败，排队中的任务标记为已取消。

### 3.3 进度拆分

| 任务 | 步骤 | 进度 |
| --- | --- | --- |
| 克隆 | 校验参数 / 源机停机 | 0-12% |
|  | Export-VM 导出 | 12-55% |
|  | Import-VM 导入 + 重命名 | 55-88% |
|  | 应用内存/CPU/网络/带宽 | 88-98% |
|  | 清理临时文件 | 100% |
| 新建 | 创建硬盘（差异/复制/空盘） | 5-45% |
|  | New-VM + ISO + 硬件配置 | 45-98% |
| 删除 | 关机 | 5-20% |
|  | Remove-VM | 20-60% |
|  | 清理文件目录 | 60-100% |

## 4. HTTP API（后续集成用）

默认关闭，在设置页开启；默认监听 `127.0.0.1:17890`，使用 Bearer Token。

```
GET    /api/v1/health                  健康检查
GET    /api/v1/hosts                   计算机列表（本机 / 远程）
POST   /api/v1/hosts/{id}/connect      连接并切换当前计算机
GET    /api/v1/host                    当前主机信息
GET    /api/v1/vms                     虚拟机列表
GET    /api/v1/vms/{name}              虚拟机详情
POST   /api/v1/vms                     新建虚拟机（异步任务；?hostId= 指定目标计算机）
POST   /api/v1/vms/{name}/clone        克隆虚拟机（异步任务；?hostId= 指定目标计算机）
PATCH  /api/v1/vms/{name}              编辑虚拟机（异步任务）
DELETE /api/v1/vms/{name}?deleteFiles=true  删除虚拟机（异步任务）
POST   /api/v1/vms/{name}/power        电源操作（异步任务）
GET    /api/v1/vms/{name}/suggest-clone-path
GET    /api/v1/switches                虚拟交换机列表
GET    /api/v1/tasks                   任务列表
GET    /api/v1/tasks/{id}              任务详情（状态 / 进度 / 日志）
POST   /api/v1/tasks/{id}/cancel       取消任务
```

提交类接口返回 `202 Accepted` 与任务对象，客户端使用返回的 `id` 轮询任务：

```bash
curl -X POST http://127.0.0.1:17890/api/v1/vms/win11/clone \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"newName":"win11-clone","targetDirectory":"D:\\VMs","stopSourceIfRunning":true,
       "memory":{"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
       "cpuCount":4,"switchName":"","bandwidthMbps":100}'

curl -H "Authorization: Bearer <token>" http://127.0.0.1:17890/api/v1/tasks/T20261126153001A1B2
```

## 5. 界面分区

- **总览**：主机信息、VM 统计、增强会话状态、最近任务。
- **虚拟机**：列表、搜索/过滤、电源操作、连接、详情抽屉、编辑、删除。
- **新建虚拟机**：分步向导（基本信息 → 磁盘与镜像 → 硬件 → 网络 → 确认）。
- **克隆虚拟机**：源机 → 名称/位置 → 硬件覆盖 → 网络 → 确认。
- **任务队列**：任务列表、进度、步骤、日志详情、取消、清理。
- **虚拟交换机**：列表、新建、删除、打开交换机管理器。
- **主机与工具**：增强会话开关/状态、快捷管理工具、系统信息。
- **设置**：默认硬件参数、默认目录、HTTP API 开关与 Token。

## 6. 后续扩展点

- 任务队列可直接作为"API 驱动"的执行核心，增加新任务类型只需注册一个 Handler。
- Hyper-V 客户端按功能拆分，后续可增加快照、模板、导入/导出、远程主机（改为 WMI 或
  PowerShell Remoting）。
- HTTP API 与 GUI 共用服务层，新增接口不会改变队列语义。
