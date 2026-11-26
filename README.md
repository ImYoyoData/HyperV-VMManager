# Hyper-V VM Assistant

为 **云电脑 / AI Agent 云端运行场景** 打造的 Windows Hyper-V 可视化管理工具。

当 AI Agent 运行在云端 Windows 主机上时，经常需要批量创建、克隆、启动、停止和回收虚拟机。
本项目把原本分散在 PowerShell 脚本里的 Hyper-V 操作，做成一套带任务队列、HTTP API 和网页控制台的
桌面应用：既适合人工在宿主机上操作，也适合业务系统、计费系统和自动化 Agent 通过 API 调用。

> Wails v3 + Go + Vue 3 + Naive UI
> 仓库：<https://github.com/ImYoyoData/HyperV-VMManager>

## 为什么做这个

- 云电脑场景下，虚拟机创建/克隆是**耗时操作**，需要异步执行、排队、查询进度，而不是阻塞脚本。
- AI Agent 和业务系统需要一套**稳定的 HTTP API**，用任务 ID 查询是否完成、失败原因和日志。
- 宿主机可能不止一台，需要同时管理本机和多台远程 Hyper-V 服务器。
- 运维人员需要 GUI，自动化脚本需要 API，两者应该共用同一个任务队列和同一套状态。
- 网页控制台让没有安装桌面端的场景也能通过浏览器管理；配合内网穿透可用于远程运维。

## 主要功能

### 环境检查与权限

- 应用以管理员权限运行（Windows 清单要求 `requireAdministrator`）。
- 启动时检查 Windows 版本、CPU 虚拟化、SLAT、Hyper-V 组件、vmms 服务与增强会话。
- 家庭版等不支持 Hyper-V 的系统会提示升级到专业版 / 专业工作站版 / 企业版 / 教育版。
- 虚拟化未开启、组件未安装时给出图文教程、打开 Windows 功能按钮，以及可复制的 AI 求助提示词。

### 计算机管理

- 只有一个“添加计算机”入口，弹窗内选择“本机 / 远程计算机”；本地计算机固定唯一，已存在时自动禁用本机选项。
- 远程主机通过 WinRM 执行 Hyper-V 命令，支持 HTTP/HTTPS、端口、用户名、Default/Negotiate/CredSSP。
- 主机配置保存在 `hosts.json`；密码可以勾选“记住密码”，加密保存到 Windows 凭据管理器，
  下次直接连接，也能被 VMConnect / Hyper-V 管理器复用。可以随时“忘记密码”。
- 连接失败会自动弹出排查窗口，提供 TrustedHosts、CredSSP、HTTPS 的可复制命令，并支持一键修复本机 WinRM 设置。
- 当前主机用于虚拟机列表、编辑、删除和电源操作；新建/克隆可以单独选择目标计算机，虚拟机列表页也可以下拉切换计算机。

### 虚拟机管理

- 列表、搜索、状态过滤、详情、VMConnect 连接。
- 电源操作：启动、关机、强制断电、保存、暂停、恢复、重启。
- 新建向导：代数、目录、空硬盘/差异磁盘/完整复制、ISO、内存、动态内存、CPU、交换机、带宽、检查点。
- 克隆向导：导出 → 导入 → 重命名 → 应用配置，自动生成新 ID 和动态 MAC；可选择目标计算机并读取该主机的虚拟机。
- 编辑：内存、CPU、网络、带宽、重命名、检查点。
- 删除：输入名称二次确认，可选删除虚拟机文件目录。

### 任务队列

所有写操作（克隆、新建、编辑、删除、电源）进入**单 worker 串行队列**：

- 任务状态：`queued` / `running` / `succeeded` / `failed` / `canceled`。
- 进度（0-100）、当前步骤、详细日志。
- 支持取消、清理已完成任务。
- 任务绑定提交时的 `hostID`，切换当前主机会影响新任务，但不会改变已排队任务。
- 历史持久化到 `tasks.json`（最近 500 条）；应用异常退出后，中断任务会标记为失败。

### 网页控制台

- 内置 HTTP 服务同时托管前端页面和 API，**打包进同一个二进制**，默认开启。
- 浏览器访问 `http://127.0.0.1:17890`，使用账号密码登录（HttpOnly Cookie 会话）。
- 设置页可以修改监听地址、端口、用户名、密码；默认用户名为 `admin`，密码随机生成并显示在桌面端设置页。
- 默认只监听本机；如需公网访问，请配合 HTTPS、反向代理或内网穿透，不要直接暴露明文端口。
- 网页端的 API Key 管理、文件选择、VMConnect 等桌面专属功能会明确提示不可用。

### API Key 与自动化

- 支持创建多个 API Key，每个 Key 有独立权限和计算机范围。
- 权限：
  - `read`：查询虚拟机、任务、主机。
  - `write`：额外允许创建、克隆、编辑、删除、电源操作。
  - `admin`：额外允许管理计算机与设置。
- 计算机范围：全部计算机，或指定主机；受限 Key 只能操作允许的主机。
- Key 配置通过桌面端“设置 → API 访问”的弹窗管理；网页端不展示 Key。
- 提交类接口返回 `202 Accepted` 与任务对象，用任务 ID 轮询状态与进度。

### 外观与窗口

- 无边框窗口 + 自定义标题栏，支持拖动、最小化、最大化、关闭。
- 主题跟随系统，也可手动切换浅色 / 暗色；设置立即预览并持久化。
- 第一次关闭窗口时询问“直接关闭”还是“最小化到系统托盘”，选择后记住；设置中可随时修改。
- 系统托盘支持显示主窗口和退出。

### 关于与更新

- 设置页底部“关于”展示应用 logo、版本、Wails 版本、作者和仓库地址。
- “检查更新”会查询 GitHub 最新 Release 并提示是否有新版本。
- 仓库：<https://github.com/ImYoyoData/HyperV-VMManager>

## 环境要求

- Windows 10/11 **专业版、专业工作站版、企业版、教育版**，或 Windows Server；家庭版不包含客户端 Hyper-V。
- CPU 支持硬件虚拟化（Intel VT-x / AMD-V）与 SLAT，BIOS/UEFI 已开启虚拟化。
- 已启用 Hyper-V 功能（Hyper-V 管理工具 + Hyper-V 平台）。
- 管理员权限。

## 开发

```powershell
# 需要管理员终端（应用清单要求管理员权限）
wails3 dev
```

单独调试前端：

```powershell
cd frontend
npm install
npm run dev
```

测试与类型检查：

```powershell
go test ./internal/...
cd frontend
npx vue-tsc --noEmit
```

## 构建

```powershell
wails3 build                    # 当前架构
wails3 task build ARCH=amd64    # x64
wails3 task build ARCH=arm64    # ARM64
wails3 task build ARCH=386      # x86
```

安装包（需要 NSIS）：

```powershell
wails3 package
```

## 数据目录

```
%AppData%\HyperV-VMManager\
├── settings.json   # 默认硬件参数、主题、关闭行为、网页控制台、内网穿透、API Key
└── hosts.json      # 本机 / 远程主机连接配置（不含密码）

D:\HyperV-VMManager\logs\          # 默认日志目录，优先使用 D/E/F 等非系统盘
├── tasks.json      # 最近 500 条任务历史
└── app-YYYY-MM-DD.log
```

- 只有 C 盘时自动回退到 `%AppData%\HyperV-VMManager\logs`。
- 日志与任务历史目录可以在“设置 → 日志与任务队列目录”中修改。
- 远程密码保存在 Windows 凭据管理器（加密），不写入配置文件；可在计算机卡片上“忘记密码”。

## HTTP API 摘要

在桌面端“设置 → 网页控制台”中查看监听地址与端口；API Key 在“设置 → API 访问”中创建。

```
GET    /api/v1/health                  健康检查（公开）
GET    /api/v1/skill.md                AI 技能包（公开，Markdown）
POST   /api/v1/auth/login              网页登录
GET    /api/v1/hosts                   计算机列表
GET    /api/v1/hosts/{id}              计算机详情
GET    /api/v1/hosts/{id}/capacity     CPU / 内存 / 磁盘容量
POST   /api/v1/hosts/{id}/connect      连接并切换计算机
DELETE /api/v1/hosts/{id}/credential   删除已保存的密码
GET    /api/v1/host                    当前主机信息
GET    /api/v1/switches                虚拟交换机列表
GET    /api/v1/tunnel                  内置 frpc 状态
GET    /api/v1/vms                     虚拟机列表
POST   /api/v1/vms                     新建虚拟机（可 ?hostId=）
POST   /api/v1/vms/{name}/clone         克隆虚拟机（可 ?hostId=）
PATCH  /api/v1/vms/{name}              编辑虚拟机
DELETE /api/v1/vms/{name}?deleteFiles=true
POST   /api/v1/vms/{name}/power        电源操作
GET    /api/v1/tasks                   任务列表
GET    /api/v1/tasks/{id}              任务详情（状态 / 进度 / 日志）
GET    /api/v1/tasks/stats             任务统计
POST   /api/v1/tasks/{id}/cancel       取消任务
```

应用内“API 文档”页面还提供 **一键复制 AI 提示词** 与 **下载技能包**，也可以直接访问
`/api/v1/skill.md` 把接口说明交给其他 AI。

示例：

```bash
curl -X POST "http://127.0.0.1:17890/api/v1/vms/win11/clone" \
  -H "Authorization: Bearer <key>" \
  -H "Content-Type: application/json" \
  -d '{"newName":"win11-clone","targetDirectory":"D:\\VMs",
       "stopSourceIfRunning":true,
       "memory":{"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
       "cpuCount":4,"switchName":"","bandwidthMbps":100}'

curl -H "Authorization: Bearer <key>" \
  "http://127.0.0.1:17890/api/v1/tasks/T20261126153001A1B2"
```

完整参数说明见应用内“API 文档”页面。

## 内网穿透

内置 **frp 官方客户端库**（fatedier/frp，Apache-2.0），frpc 直接编译进应用，不需要额外下载 `frpc.exe`。

- 在“设置 → 内网穿透（frp）”中填写 frps 地址、端口、Token、传输协议。
- 默认映射网页控制台：`127.0.0.1:<网页端口>` → `frps:<远程端口>`。
- 可以添加多条 **自定义端口映射**，目标 IP/端口可以指向虚拟机内的服务，例如
  `172.17.136.94:3389`、`172.17.136.94:22`、`172.17.136.94:80`。
- 公网侧仍然需要一台运行 `frps` 的服务器（自建或使用 frp 服务商）；应用只内置客户端。
- frpc 运行在**当前电脑**上，因此目标 IP 必须是当前电脑能直接访问的地址。
  如果虚拟机在远程 Hyper-V 主机的 NAT / 内部网络中，需要先在远程主机上做端口转发，
  或把 frpc 部署到那台远程主机。

虚拟机端口映射的两种典型情况：

1. **虚拟机使用外部交换机或可路由 IP**：直接把目标 IP 填成虚拟机 IP，frpc 可以直连。
2. **虚拟机在 NAT / Default Switch / 内部交换机后面**：当前电脑通常无法直接访问其 IP。
   可以在 Hyper-V 主机上执行 `netsh interface portproxy`（或 `Add-NetNatStaticMapping`）把
   主机端口转发到虚拟机 IP:端口，然后让本应用映射到该主机地址:端口。

## 目录结构

```
main.go                       Wails 装配、无边框窗口、系统托盘、关闭行为
internal/hyperv/              Hyper-V 客户端、PowerShell/远程 WinRM 执行层、环境检测
internal/tasks/               单 worker 任务队列与持久化
internal/hosts/               主机配置存储
internal/service/             Wails 绑定门面、任务提交、API Key、设置
internal/httpapi/             内置网页控制台 / HTTP API / 登录会话
internal/settings/            设置读写与迁移
frontend/src/pages/           总览、计算机、虚拟机、新建、克隆、任务、网络、主机、设置、环境、API 文档
docs/architecture.md          总体设计与 API
docs/roadmap.md               多主机、端口映射、网页访问、内网穿透设计
```

## 已知限制

- 远程连接依赖 WinRM；失败通常是 WinRM 未启用、凭据权限不足或防火墙/TrustedHosts 配置问题。
- 远程模式下不能浏览远程磁盘目录，镜像/ISO/存储路径需要手动输入。
- 远程密码不持久化，重启应用后需要重新连接。
- 克隆使用 `Export-VM` + `Import-VM`，需要足够的临时磁盘空间，耗时会随虚拟硬盘大小增加。
- 取消运行中的任务会终止 PowerShell 进程或远程会话，可能留下部分导出/导入文件。
- 端口映射、内置 frp 穿透尚未实现，仅完成设计。
- 请勿在家庭版 Windows 上使用；应用会提示升级到支持 Hyper-V 的版本。

## CI / 发布

- 推送到 `dev`：构建 Windows x64 / ARM64 / x86，不发布。
- 推送到 `main`：构建三个架构，并按 `build/config.yml` 版本号创建 GitHub Release。
- 详见 `.github/workflows/build.yml`。

## 许可

见 [LICENSE](LICENSE)。
