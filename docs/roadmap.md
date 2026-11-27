# 后续能力设计（Roadmap）

本文档记录尚未实现、但已经确定方向的能力。目标是让后续开发在不破坏现有任务队列与
API 语义的前提下逐步接入。

## 1. 多主机连接（已实现）

**已实现：**

- 新增“计算机”页面：本机 / 远程主机列表、添加、编辑、删除、连接与切换。
- `hosts.json` 持久化主机配置（不保存密码）；勾选“记住密码”时密码加密保存在
  Windows 凭据管理器，供 WinRM、VMConnect 与 Hyper-V 管理器复用。
- 远程执行通过 WinRM：`New-PSSession` + `Invoke-Command`，复用同一套 PowerShell 脚本协议。
- 任务对象与 payload 增加 `hostID`；任务执行时使用提交时绑定的主机，切换当前主机不影响已排队任务。
- 新建/克隆页可选择目标计算机（仅已连接主机），任务绑定该主机；克隆会读取目标主机上的虚拟机列表。
- API 增加 `GET /api/v1/hosts` 与 `POST /api/v1/hosts/{id}/connect`；新建/克隆接口支持
  `?hostId=` 或 `X-HyperV-Host` 指定目标计算机。
- 远程模式下禁用本机文件选择器，镜像/ISO/目录需要填写远程主机上的绝对路径。

**待完善：**

- 远程目录浏览（通过远端 `Get-ChildItem`）。
- 编辑/删除/电源接口显式支持 `hostId`（当前这些操作仍作用于当前主机）。

### 1.1 数据模型

```go
type HostProfile struct {
    ID              string `json:"id"`              // host-local / host-remote-<uuid>
    Name            string `json:"name"`            // 显示名称
    Address         string `json:"address"`         // 空表示本机；否则计算机名或 IP
    Port            int    `json:"port"`            // WinRM 端口，默认 5985/5986
    UseSSL          bool   `json:"useSSL"`
    Username        string `json:"username"`        // DOMAIN\user 或 .\user
    Authentication  string `json:"authentication"`  // Default | Negotiate | CredSSP
    EncryptedSecret string `json:"encryptedSecret"` // DPAPI 加密后的密码，绝不存明文
    LastConnectedAt string `json:"lastConnectedAt"`
    Tags            []string
}
```

- 主机列表保存在 `%AppData%\HyperV-VMManager\hosts.json`。
- 密码通过 Windows 凭据管理器（`cmdkey` / Credential Manager API）保存，目标名使用
  `TERMSRV/<主机>`，避免明文落盘，并可被 VMConnect 复用。
- 当前主机是应用级状态，切换主机时前端刷新 VM 列表、任务列表与增强会话状态。

### 1.2 执行层抽象

```go
type Executor interface {
    Run(ctx context.Context, script string, opts any, out any) error
    ConnectVM(vmName string) error
    // ...
}
```

- `LocalExecutor`：现有实现，通过本地 `powershell.exe` 执行脚本。
- `PSRemotingExecutor`：本地 PowerShell 建立 WinRM 会话，在远程主机上执行同一段脚本。

远程执行示意：

```powershell
$secure = ConvertTo-SecureString $env:HV_PASS -AsPlainText -Force
$cred   = New-Object System.Management.Automation.PSCredential($env:HV_USER, $secure)
$session = New-PSSession -ComputerName $env:HV_HOST -Credential $cred `
    -Port $env:HV_PORT -UseSSL:$env:HV_SSL -ErrorAction Stop
try {
    $script    = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:HV_SCRIPT))
    $optsBase64 = $env:HV_OPTS
    $result = Invoke-Command -Session $session -ScriptBlock {
        param($scriptText, $optsBase64)
        $env:HV_OPTS = $optsBase64
        $env:HV_REMOTE = '1'
        Invoke-Expression $scriptText
    } -ArgumentList $script, $optsBase64
    $result | Select-Object -Last 1
} finally {
    if ($session) { Remove-PSSession $session }
}
```

脚本协议需要支持远程模式：`Write-HvResult` / `Write-HvError` 在远程会话中使用
`Write-Output` 返回 Base64 JSON，本地默认仍输出到 stdout。其余脚本逻辑不变。

### 1.3 远程限制

- 文件选择器只能选择本机文件。远程主机模式下，镜像、ISO、存储目录需要手动输入
  远程主机上的绝对路径，或后续实现远程目录浏览（通过 `Get-ChildItem`）。
- 增强会话（VMConnect）可以在本机连接到远程 Hyper-V 主机：
  `vmconnect.exe <remote-host> <vm-name>`。主机策略配置、服务开关仍需要在远程主机上执行。
- 打开 `virtmgmt.msc` 等本机管理工具不受影响，但操作对象仍是本机。
- WinRM 需要在目标主机启用，并且当前用户具备 Hyper-V 管理权限：
  ```powershell
  Enable-PSRemoting -Force
  # 或仅允许特定主机：
  winrm quickconfig
  ```
- 跨域/工作组场景通常需要 `TrustedHosts` 或 CredSSP。应用不自动修改这些安全设置，
  而是在连接测试失败时给出明确提示。

### 1.4 API 与任务绑定

- 任务已增加 `hostID`，任务执行器根据 `hostID` 选择 `Executor`。
- 已实现：
  - `GET /api/v1/hosts`
  - `POST /api/v1/hosts`（通过 API 新增连接配置）
  - `POST /api/v1/hosts/{id}/connect`（连接并切换当前主机）
  - `GET /api/v1/hosts/{id}/capacity`
  - 新建/克隆接口显式支持 `hostId` 查询参数或 `X-HyperV-Host` 头。
- 待实现：
  - `POST /api/v1/hosts/{id}/test`
  - 编辑/删除/电源接口显式支持 `hostId`。

## 2. 端口映射（已实现）

当前实现：内部交换机 + 自建 Windows NAT（`New-NetNat`），端口转发使用
`netsh interface portproxy`（TCP），并在虚拟机详情、虚拟交换机页面和内网穿透页面提供管理入口。

### 2.1 方案对比

| 方案 | 适用场景 | 优点 | 限制 |
| --- | --- | --- | --- |
| `netsh interface portproxy` | 宿主机任意交换机类型，TCP 转发 | 不依赖 Hyper-V 网络类型；实现简单 | 仅 TCP；依赖 IP Helper（iphlpsvc）；需要管理员 |
| `New-NetNatStaticMapping` | 内部/专用交换机 + NAT | 支持 TCP/UDP；适合 NAT 网络 | 需要先有 NAT 实例；外部交换机不适用 |
| 路由器 / 云安全组 | 外部桥接，VM 有独立局域网 IP | 性能最好，宿主无负担 | 需要网络设备权限，应用无法统一管理 |
| 反代 / 隧道（frp、Cloudflare Tunnel） | 只暴露 HTTP/HTTPS | 不暴露 VM IP；可加 TLS | 需要额外服务，不适合任意 TCP/UDP |

推荐的产品形态：

- VM 详情 → 端口映射 → 选择方式：
  - 推荐 `netsh portproxy`（TCP）用于 RDP/Web 等常见场景；
  - 如果虚拟机接在 NAT 交换机下，提供 `NetNatStaticMapping`（TCP/UDP）。
- 只允许映射到该虚拟机网卡上实际存在的 IP，避免误转发。
- 创建前检查端口占用、管理员权限、IP Helper 服务状态。

### 2.2 数据模型

```go
type PortMapping struct {
    ID          string `json:"id"`
    VMName      string `json:"vmName"`
    Protocol    string `json:"protocol"`    // TCP | UDP
    ListenAddr  string `json:"listenAddr"`  // 0.0.0.0 / 指定宿主 IP
    ListenPort  int    `json:"listenPort"`
    TargetIP    string `json:"targetIP"`    // VM 的 IPv4
    TargetPort  int    `json:"targetPort"`
    Method      string `json:"method"`      // portproxy | netnat
    Enabled     bool   `json:"enabled"`
    CreatedAt   string `json:"createdAt"`
}
```

### 2.3 API 设计（预留）

```
GET    /api/v1/vms/{name}/port-mappings
POST   /api/v1/vms/{name}/port-mappings
DELETE /api/v1/vms/{name}/port-mappings/{id}
```

创建/删除应进入任务队列（`kind=portmapping`），避免与克隆、创建并发修改网络配置。

## 3. 网页访问 / 公网预览（已实现）

当前实现：内嵌 `internal/httpapi` 提供静态资源与 `/api/v1/*`，桌面端设置页可配置监听
地址、端口、账号密码；登录使用 HttpOnly Cookie，并限制登录失败频率。内置 frp 客户端
可以把网页控制台和 NAT 端口转发映射到公网 frps。

### 3.1 推荐架构

```
浏览器 ── HTTPS ──> 反向代理 / 隧道（可选）
                        │
                 内嵌 Web Server（桌面进程内）
                        │
             静态资源（frontend/dist） + /api/v1/*
                        │
                    tasks.Queue / HyperVService
```

- 新增 `internal/webserver`：
  - `http.FileServer` 提供 `frontend/dist`（embed FS）；
  - `/api/v1/*` 复用现有 `httpapi.Server`；
  - 认证：Token 登录换取 HttpOnly Cookie（比把 Token 放在 URL 安全），或 Basic Auth。
- 前端增加“运行模式适配层”：
  - Wails 环境（`window._wails` 存在）→ 使用生成的绑定；
  - 浏览器环境 → 使用 `fetch("/api/v1/...")`。
- 桌面应用设置：
  - “网页访问”开关、监听地址、端口；
  - 默认只监听 `127.0.0.1`，外部访问必须显式改为 `0.0.0.0` 并强提示；
  - 强制 Token，密码错误限速；
  - 可选的 HTTPS（证书路径）或建议放在 Caddy/Nginx 反代之后。
- 公网暴露：
  - 路由器端口转发、frp、Cloudflare Tunnel 等由用户选择；
  - 应用只负责监听、认证和审计，不自动修改防火墙/路由器。

### 3.2 安全要求（必须）

- 默认关闭网页访问；开启时要求确认风险。
- 必须有认证；不允许匿名访问 Hyper-V 管理接口。
- API Token 不出现在 URL、日志与前端构建产物中。
- 登录失败限速、记录来源 IP/时间/操作（审计日志）。
- 建议仅在可信网络或 VPN/隧道内使用；公网必须 HTTPS。

### 3.3 另一种形态：Wails Server Mode

Wails v3 支持 `-tags server` 构建无 GUI 的纯 HTTP 服务（`wails3 build:server`）。
可以在服务器上部署一个“无桌面”版本，复用同一套服务与队列；与桌面端内嵌 Web
Server 相比，它更适合长期运行，但需要单独部署与进程管理。

## 4. API 演进与自动化对接

- 当前 API 以本机为目标，适合脚本、计费系统、自动化平台在本机或受控内网调用。
- 多主机版本将在提交类接口中加入 `hostId`，并增加连接测试接口。
- 任务对象已经是稳定契约，新增字段保持向后兼容；任务 ID 可直接持久化到业务系统中。
- 建议业务侧实现：
  1. 提交任务 → 保存 `task.id` 与业务订单号；
  2. 轮询任务（或后续增加 Webhook / SSE 推送）；
  3. `succeeded` 后开通服务，`failed` 时读取 `error` 与 `logs`；
  4. 定期清理 `succeeded` / `canceled` 任务。
- 后续可增加：
  - 任务事件 Webhook（任务完成时 POST 到业务回调地址）；
  - 任务模板（保存一套克隆/新建参数，按模板重复创建）；
  - 快照管理、从快照恢复、批量电源操作。

## 5. 构建与发布（GitHub Actions）

- `main`：稳定分支。推送后自动构建 Windows x64 / ARM64 / x86 并创建 Release。
- `dev`：开发分支。推送后仅构建，不上传 Release。
- 版本号来源：`build/config.yml` 的 `info.version`；Release Tag 为 `v<version>`。
- 构建矩阵：
  - `amd64` → x64
  - `arm64` → ARM64
  - `386` → x86
- Windows Runner 需要：
  - Go（按 `go.mod` 版本）
  - Node.js + npm
  - Wails v3 CLI
  - WebView2 运行时（GitHub Windows Runner 自带）
- 发布产物：每个架构一个 exe；后续可增加 NSIS 安装包（需要 makensis）。
- 安装包与签名：
  - 未签名 exe 首次运行会触发 SmartScreen，可在 Release 说明中说明；
  - 生产环境建议配置代码签名证书（`wails3 tool sign`）。
