# Hyper-V 虚拟机管理助手

Windows 上基于 **Wails v3 + Go + Vue 3 + Naive UI** 的 Hyper-V 可视化管理工具。
把常用的 Hyper-V 操作从 PowerShell 脚本搬进桌面界面，并提供统一的**串行任务队列**与
**HTTP API**，方便后续接入自动化、计费或业务系统。

## 功能

- **环境检查**：启动时检查管理员权限、Windows 版本、CPU 虚拟化（VT-x/AMD-V、SLAT）、
  Hyper-V 组件、vmms 服务与增强会话；未通过时给出教程、可复制的 AI 求助提示词与
  打开系统功能的按钮。
- **虚拟机管理**：列表、搜索、状态过滤、连接控制台（VMConnect）、电源操作
  （启动/关机/强制断电/保存/暂停/恢复/重启）。
- **计算机连接**：连接本机或远程 Hyper-V 服务器（WinRM），保存主机配置、切换当前主机；
  所有虚拟机操作与任务都绑定到提交时选择的主机。
- **新建虚拟机**：代数、存储位置、空硬盘/差异磁盘/完整复制、ISO、内存、动态内存、
  CPU、交换机、带宽、检查点，向导式配置并提交为任务；可选择目标计算机。
- **克隆虚拟机**：导出 → 导入 → 重命名 → 应用内存/CPU/网络/带宽，自动生成新 ID 和
  动态 MAC；源机运行时可按配置自动关机；可选择目标计算机并读取该主机上的源虚拟机。
- **编辑 / 删除**：内存、CPU、网络、带宽、重命名、检查点；删除时二次确认、可选删除文件目录。
- **任务队列**：所有写操作串行执行，实时显示进度、当前步骤与日志，可取消、清理已完成任务。
- **虚拟交换机**：查看类型/上行网卡，新建外部/内部/专用交换机，删除前检查连接。
- **主机工具**：增强会话开启/关闭/状态、Hyper-V 管理器、交换机管理器、服务、Windows 功能等。
- **HTTP API**：与应用共用同一任务队列，支持通过任务 ID 查询状态与进度，详见应用内
  “API 文档”页面或 [docs/architecture.md](docs/architecture.md)。

## 环境要求

- Windows 10/11 **专业版、专业工作站版、企业版、教育版**，或 Windows Server；
  家庭版不包含 Hyper-V 管理功能。
- CPU 支持硬件虚拟化（Intel VT-x / AMD-V）与 SLAT。
- BIOS/UEFI 已开启虚拟化。
- 已启用 Hyper-V 功能（含 Hyper-V 管理工具与平台）。
- **管理员权限**：应用清单要求以管理员身份运行（UAC）。开发时请在管理员终端中执行命令。

启动后如果环境不满足，会自动进入“环境检查”页面，按提示处理后点击“重新检查”。

## 开发

```powershell
# 需要管理员终端
wails3 dev
```

单独调试前端：

```powershell
cd frontend
npm install
npm run dev
```

类型检查与后端测试：

```powershell
cd frontend
npx vue-tsc --noEmit

cd ..
go test ./internal/...
```

> 注意：`wails3 dev` 启动的 exe 带有 `requireAdministrator` 清单，因此终端本身需要管理员权限；
> 否则 Windows 无法创建进程。

## 构建

```powershell
wails3 build                    # 当前架构，产物在 bin/hyperv-vmmanager.exe
wails3 task build ARCH=amd64    # x64
wails3 task build ARCH=arm64    # Windows on ARM
wails3 task build ARCH=386      # x86
```

打包安装程序（需要 NSIS）：

```powershell
wails3 package
```

## 数据目录

```
%AppData%\HyperV-VMManager\
├── settings.json   # 默认硬件参数、HTTP API 配置
├── hosts.json      # 本机 / 远程主机连接配置（不含密码）
└── tasks.json      # 最近 500 条任务历史
```

远程主机的密码不会写入磁盘，只保存在当前应用进程内存中；重启应用后需要重新输入密码。

任务历史只保留最近 500 条；应用异常退出时，正在执行的任务会在下次启动标记为失败，
排队中的任务标记为取消。

## 连接远程主机

在“计算机”页面添加远程 Hyper-V 服务器，填写计算机名/IP、WinRM 端口（HTTP 5985 /
HTTPS 5986）、用户名与认证方式，然后点击“连接”：

- 应用通过 PowerShell Remoting（WinRM）在远程主机上执行 Hyper-V 命令。
- 远程主机需要启用 WinRM，且使用的 Windows 账户具备管理员或 Hyper-V Administrators 权限。
- 新建/克隆页面可以选择目标计算机（必须是已连接的主机），任务会绑定该主机，不会切换当前管理主机。
- 远程模式下，创建/克隆/编辑时填写的存储目录、镜像、ISO 必须是 **远程主机上的绝对路径**，
  本机文件选择器会被禁用。
- 任务会记录提交时所在的主机；切换当前主机不会影响已经排队的任务。
- 密码只在内存中保存，不写入 `hosts.json`。

远程主机启用 WinRM（以管理员身份运行）：

```powershell
Enable-PSRemoting -Force
Restart-Service WinRM
```

跨域/工作组环境可能需要在 WinRM 中配置 TrustedHosts，或使用 CredSSP/HTTPS。应用不会
自动修改这些安全设置，连接失败时会在界面显示原始错误。

## HTTP API

在“设置 → HTTP API”中启用，默认监听 `127.0.0.1:17890`，使用 Bearer Token。

```bash
curl -X POST http://127.0.0.1:17890/api/v1/vms/win11/clone \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"newName":"win11-clone","targetDirectory":"D:\\VMs",
       "stopSourceIfRunning":true,
       "memory":{"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
       "cpuCount":4,"switchName":"","bandwidthMbps":100}'

curl -H "Authorization: Bearer <token>" \
  http://127.0.0.1:17890/api/v1/tasks/T20261126153001A1B2
```

提交类接口返回 `202 Accepted` 与任务对象；任务状态为
`queued / running / succeeded / failed / canceled`，`progress` 为 0-100。

完整参数说明见应用内 **API 文档** 页面。

## 目录结构

```
main.go                       Wails 应用装配与窗口
internal/hyperv/              Hyper-V 客户端、PowerShell 执行层、环境检测
internal/tasks/               单 worker 任务队列与持久化
internal/service/             Wails 绑定门面、参数校验、HTTP API 后端
internal/httpapi/             内嵌 HTTP API
internal/settings/            设置读写
frontend/src/pages/           总览、虚拟机、新建、克隆、任务、网络、主机、设置、环境检查、API 文档
docs/architecture.md          总体设计与 API
docs/roadmap.md               多主机、端口映射、网页访问、CI 设计
```

## 已知限制

- 远程连接依赖 WinRM；连接失败通常是 WinRM 未启用、凭据权限不足或防火墙/TrustedHosts 配置问题。
- 远程模式下不能浏览远程磁盘目录，镜像/ISO/存储路径需要手动输入。
- 远程密码不持久化，重启应用后需要重新连接。
- 克隆使用 `Export-VM` + `Import-VM`，需要足够的临时磁盘空间，耗时会随虚拟硬盘大小增加。
- 取消运行中的任务会终止 PowerShell 进程或远程会话，可能留下部分导出/导入文件，需要手动清理。
- 端口映射、网页访问尚未实现，仅完成设计。
- 请勿在家庭版 Windows 上使用；应用会提示升级到支持 Hyper-V 的版本。

## CI / 发布

- 推送到 `dev`：构建 Windows x64 / ARM64 / x86，不发布。
- 推送到 `main`：构建三个架构，并按 `build/config.yml` 的版本号创建 GitHub Release。
- 详见 `.github/workflows/build.yml`。

## 许可

见 [LICENSE](LICENSE)。
