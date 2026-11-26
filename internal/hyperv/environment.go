package hyperv

import (
	"context"
	"fmt"
	"strings"
)

// 环境检查项状态。
const (
	CheckOK      = "ok"
	CheckWarn    = "warn"
	CheckError   = "error"
	CheckUnknown = "unknown"
)

// EnvironmentCheck 是单个环境检查结果。
type EnvironmentCheck struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// Action 由前端映射为按钮：elevate | open-features | open-services |
	// fix-enhanced-session | bios | upgrade-windows | none
	Action string `json:"action"`
}

// EnvironmentStatus 是运行 Hyper-V 管理前的环境检查结果。
type EnvironmentStatus struct {
	ComputerName                  string             `json:"computerName"`
	ProductName                   string             `json:"productName"`
	EditionID                     string             `json:"editionId"`
	DisplayVersion                string             `json:"displayVersion"`
	BuildNumber                   string             `json:"buildNumber"`
	InstallationType              string             `json:"installationType"`
	CPUName                       string             `json:"cpuName"`
	CPUCores                      int                `json:"cpuCores"`
	LogicalProcessors             int                `json:"logicalProcessors"`
	TotalMemoryMB                 int64              `json:"totalMemoryMB"`
	IsAdmin                       bool               `json:"isAdmin"`
	SupportedEdition              bool               `json:"supportedEdition"`
	HypervisorPresent             bool               `json:"hypervisorPresent"`
	VirtualizationFirmwareEnabled bool               `json:"virtualizationFirmwareEnabled"`
	SLATSupported                 bool               `json:"slatSupported"`
	HyperVFeatureState            string             `json:"hyperVFeatureState"`
	HyperVFeatureKnown            bool               `json:"hyperVFeatureKnown"`
	HyperVModuleAvailable         bool               `json:"hyperVModuleAvailable"`
	HyperVVersion                 string             `json:"hyperVVersion"`
	VMServiceStatus               string             `json:"vmServiceStatus"`
	TermServiceStatus             string             `json:"termServiceStatus"`
	TermServicePresent            bool               `json:"termServicePresent"`
	EnhancedSessionEnabled        bool               `json:"enhancedSessionEnabled"`
	EnhancedSessionKnown          bool               `json:"enhancedSessionKnown"`
	Overall                       string             `json:"overall"` // ready | needs-action | unsupported
	Ready                         bool               `json:"ready"`
	Checks                        []EnvironmentCheck `json:"checks"`
	AIHelperPrompt                string             `json:"aiHelperPrompt"`
}

type rawEnvironment struct {
	ComputerName                  string `json:"computerName"`
	ProductName                   string `json:"productName"`
	EditionID                     string `json:"editionId"`
	DisplayVersion                string `json:"displayVersion"`
	BuildNumber                   string `json:"buildNumber"`
	InstallationType              string `json:"installationType"`
	CPUName                       string `json:"cpuName"`
	CPUCores                      int    `json:"cpuCores"`
	LogicalProcessors             int    `json:"logicalProcessors"`
	TotalMemoryMB                 int64  `json:"totalMemoryMB"`
	IsAdmin                       bool   `json:"isAdmin"`
	HypervisorPresent             bool   `json:"hypervisorPresent"`
	VirtualizationFirmwareEnabled bool   `json:"virtualizationFirmwareEnabled"`
	SLATSupported                 bool   `json:"slatSupported"`
	HyperVFeatureState            string `json:"hyperVFeatureState"`
	HyperVFeatureKnown            bool   `json:"hyperVFeatureKnown"`
	HyperVModuleAvailable         bool   `json:"hyperVModuleAvailable"`
	HyperVVersion                 string `json:"hyperVVersion"`
	VMServiceStatus               string `json:"vmmsStatus"`
	TermServiceStatus             string `json:"termServiceStatus"`
	TermServicePresent            bool   `json:"termServicePresent"`
	EnhancedSessionEnabled        bool   `json:"enhancedSessionEnabled"`
	EnhancedSessionKnown          bool   `json:"enhancedSessionKnown"`
}

// environmentScript 读取 Windows 版本、CPU、Hyper-V 功能与增强会话状态。
const environmentScript = `
$reg = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' -ErrorAction Stop
$cpu = Get-CimInstance Win32_Processor -ErrorAction Stop | Select-Object -First 1
$cs = Get-CimInstance Win32_ComputerSystem -ErrorAction Stop
$featureState = 'Unknown'
$featureKnown = $false
try {
  $feature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V-All -ErrorAction Stop
  $featureState = "$($feature.State)"
  $featureKnown = $true
} catch {
  $featureKnown = $false
}
$module = Get-Module -ListAvailable Hyper-V | Sort-Object Version -Descending | Select-Object -First 1
$moduleAvailable = $null -ne $module
$vmms = Get-CimInstance Win32_Service -Filter "Name='vmms'" -ErrorAction SilentlyContinue | Select-Object -First 1
$term = Get-CimInstance Win32_Service -Filter "Name='TermService'" -ErrorAction SilentlyContinue | Select-Object -First 1
$vmmsStatus = if ($vmms) { "$($vmms.State)" } else { "Absent" }
$termStatus = if ($term) { "$($term.State)" } else { "Absent" }
$termPresent = $null -ne $term
$enhanced = $false
$enhancedKnown = $false
if ($moduleAvailable) {
  try {
    $enhanced = [bool](Get-VMHost -ErrorAction Stop).EnableEnhancedSessionMode
    $enhancedKnown = $true
  } catch { }
}
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
Write-HvResult ([ordered]@{
  computerName = "$env:COMPUTERNAME"
  productName = "$($reg.ProductName)"
  editionId = "$($reg.EditionID)"
  displayVersion = "$($reg.DisplayVersion)"
  buildNumber = "$($reg.CurrentBuild).$($reg.UBR)"
  installationType = "$($reg.InstallationType)"
  cpuName = "$($cpu.Name)"
  cpuCores = [int]$cpu.NumberOfCores
  logicalProcessors = [int]$cpu.NumberOfLogicalProcessors
  totalMemoryMB = [int64]($cs.TotalPhysicalMemory / 1MB)
  isAdmin = $isAdmin
  hypervisorPresent = [bool]$cs.HypervisorPresent
  virtualizationFirmwareEnabled = [bool]$cpu.VirtualizationFirmwareEnabled
  slatSupported = [bool]$cpu.SecondLevelAddressTranslationExtensions
  hyperVFeatureState = $featureState
  hyperVFeatureKnown = $featureKnown
  hyperVModuleAvailable = $moduleAvailable
  hyperVVersion = if ($module) { "$($module.Version)" } else { "" }
  vmmsStatus = $vmmsStatus
  termServiceStatus = $termStatus
  termServicePresent = $termPresent
  enhancedSessionEnabled = $enhanced
  enhancedSessionKnown = $enhancedKnown
})
`

// EnvironmentStatus 读取 Windows 版本、CPU 虚拟化、Hyper-V 功能与增强会话状态。
func (c *Client) EnvironmentStatus(ctx context.Context) (*EnvironmentStatus, error) {
	var raw rawEnvironment
	if err := c.run(ctx, environmentScript, nil, &raw); err != nil {
		return nil, err
	}
	status := evaluateEnvironment(raw)
	return &status, nil
}

func evaluateEnvironment(raw rawEnvironment) EnvironmentStatus {
	edition := strings.TrimSpace(raw.EditionID)
	supportedEdition := !isHomeEdition(edition)
	checks := make([]EnvironmentCheck, 0, 6)

	addCheck := func(id, title, status, detail, action string) {
		checks = append(checks, EnvironmentCheck{
			ID: id, Title: title, Status: status, Detail: detail, Action: action,
		})
	}

	// 1. 管理员权限
	if raw.IsAdmin {
		addCheck("admin", "管理员权限", CheckOK, "应用已以管理员身份运行。", "none")
	} else {
		addCheck("admin", "管理员权限", CheckError,
			"Hyper-V 的创建、修改、删除操作需要管理员权限。请以管理员身份重新启动应用。", "elevate")
	}

	// 2. Windows 版本
	editionDetail := fmt.Sprintf("%s（%s，版本 %s，Build %s）", raw.ProductName, edition, raw.DisplayVersion, raw.BuildNumber)
	if supportedEdition {
		addCheck("edition", "Windows 版本", CheckOK,
			editionDetail+"，支持客户端 Hyper-V。", "none")
	} else {
		addCheck("edition", "Windows 版本", CheckError,
			editionDetail+"。Windows 家庭版不包含 Hyper-V 管理功能，需要升级到专业版、专业工作站版、企业版或教育版。", "upgrade-windows")
	}

	// 3. CPU 虚拟化 / SLAT
	// 说明：Win32_Processor.SecondLevelAddressTranslationExtensions 在部分新 CPU、
	// 或 Hyper-V 已接管虚拟化时可能返回 false，不能单独作为“不支持”的依据。
	// 以 Hypervisor 是否已运行、固件虚拟化是否开启作为主要判断。
	virtualizationReady := raw.HypervisorPresent || raw.VirtualizationFirmwareEnabled
	if raw.HypervisorPresent {
		addCheck("virtualization", "CPU 虚拟化", CheckOK,
			fmt.Sprintf("CPU：%s。Hypervisor 已在运行，硬件虚拟化已启用，Hyper-V 可以正常使用。", raw.CPUName), "none")
	} else if raw.VirtualizationFirmwareEnabled {
		detail := fmt.Sprintf("CPU：%s。BIOS/UEFI 已开启硬件虚拟化，启用 Hyper-V 后会接管。", raw.CPUName)
		if !raw.SLATSupported {
			detail += fmt.Sprintf("（WMI 未报告 SLAT，但这通常是系统或驱动的读取限制，不影响 Hyper-V 启用。）")
		}
		addCheck("virtualization", "CPU 虚拟化", CheckOK, detail, "none")
	} else {
		detail := fmt.Sprintf("CPU：%s。BIOS/UEFI 中尚未开启硬件虚拟化（Intel VT-x / AMD-V）。需要重启进入 BIOS/UEFI，开启 Virtualization Technology / SVM Mode 等选项。", raw.CPUName)
		if !raw.SLATSupported {
			detail += " 同时系统未报告 SLAT 支持，请在开启虚拟化后用任务管理器确认“虚拟化：已启用”。"
		}
		addCheck("virtualization", "CPU 虚拟化", CheckError, detail, "bios")
	}

	// 4. Hyper-V 功能与模块
	switch {
	case !supportedEdition:
		addCheck("hyperv-feature", "Hyper-V 组件", CheckError,
			"当前 Windows 版本不支持客户端 Hyper-V，无法安装相关组件。", "upgrade-windows")
	case raw.HyperVFeatureKnown && strings.EqualFold(raw.HyperVFeatureState, "Enabled") && raw.HyperVModuleAvailable && raw.VMServiceStatus == "Running":
		addCheck("hyperv-feature", "Hyper-V 组件", CheckOK,
			fmt.Sprintf("Hyper-V 功能已启用，PowerShell 模块版本 %s，vmms 服务运行中。", raw.HyperVVersion), "none")
	case raw.HyperVFeatureKnown && strings.EqualFold(raw.HyperVFeatureState, "Enabled"):
		detail := "Hyper-V 功能已启用，但"
		if !raw.HyperVModuleAvailable {
			detail += " 未找到 Hyper-V PowerShell 模块"
		}
		if raw.VMServiceStatus != "Running" {
			detail += fmt.Sprintf(" vmms 服务状态为 %s", raw.VMServiceStatus)
		}
		detail += "。请重启电脑后重新检查；如果仍然失败，请打开 Windows 功能确认 Hyper-V 管理工具已安装。"
		addCheck("hyperv-feature", "Hyper-V 组件", CheckWarn, detail, "open-features")
	case raw.HyperVFeatureKnown && strings.EqualFold(raw.HyperVFeatureState, "Disabled"):
		addCheck("hyperv-feature", "Hyper-V 组件", CheckError,
			"Hyper-V 功能已安装但未启用。请在“启用或关闭 Windows 功能”中勾选 Hyper-V（管理工具与平台），确定后重启电脑。", "open-features")
	default:
		addCheck("hyperv-feature", "Hyper-V 组件", CheckUnknown,
			"无法读取 Hyper-V 功能状态（可能被系统策略或精简版系统移除）。请打开 Windows 功能查看是否存在 Hyper-V 选项。", "open-features")
	}

	// 5. 增强会话
	enhancedSupported := raw.InstallationType != "Server Core" && raw.TermServicePresent
	switch {
	case !supportEnough(raw):
		addCheck("enhanced-session", "增强会话", CheckUnknown,
			"Hyper-V 环境尚未就绪，暂时无法判断增强会话。", "none")
	case !enhancedSupported:
		addCheck("enhanced-session", "增强会话", CheckWarn,
			"当前系统缺少远程桌面服务（TermService）或属于 Server Core，增强会话不可用。基础 VMConnect 仍然可以使用。", "none")
	case raw.EnhancedSessionKnown && raw.EnhancedSessionEnabled:
		addCheck("enhanced-session", "增强会话", CheckOK,
			"增强会话已开启，VMConnect 可使用剪贴板、驱动器重定向和高分辨率。", "none")
	case raw.EnhancedSessionKnown && !raw.EnhancedSessionEnabled:
		addCheck("enhanced-session", "增强会话", CheckWarn,
			"增强会话当前关闭。开启后可以使用剪贴板、驱动器重定向等功能；部分系统需要远程桌面服务支持。", "fix-enhanced-session")
	default:
		addCheck("enhanced-session", "增强会话", CheckWarn,
			"无法读取增强会话状态。可以尝试点击“修复增强会话”自动配置主机策略与服务。", "fix-enhanced-session")
	}

	ready := true
	overall := "ready"
	for _, check := range checks {
		// 增强会话只是附加能力，不阻止使用 Hyper-V 主功能。
		if check.ID == "enhanced-session" {
			continue
		}
		if check.Status == CheckError || check.Status == CheckUnknown {
			ready = false
			if overall == "ready" {
				overall = "needs-action"
			}
		}
	}
	if !supportedEdition {
		overall = "unsupported"
		ready = false
	}
	// 虚拟化、Hyper-V 组件与 vmms 是运行虚拟机的硬性条件。
	// 不再以 SLAT 的 WMI 字段作为阻断条件。
	if !raw.IsAdmin || !virtualizationReady || !strings.EqualFold(raw.HyperVFeatureState, "Enabled") ||
		!raw.HyperVModuleAvailable || raw.VMServiceStatus != "Running" {
		ready = false
		if overall == "ready" {
			overall = "needs-action"
		}
	}

	status := EnvironmentStatus{
		ComputerName:                  raw.ComputerName,
		ProductName:                   raw.ProductName,
		EditionID:                     raw.EditionID,
		DisplayVersion:                raw.DisplayVersion,
		BuildNumber:                   raw.BuildNumber,
		InstallationType:              raw.InstallationType,
		CPUName:                       raw.CPUName,
		CPUCores:                      raw.CPUCores,
		LogicalProcessors:             raw.LogicalProcessors,
		TotalMemoryMB:                 raw.TotalMemoryMB,
		IsAdmin:                       raw.IsAdmin,
		SupportedEdition:              supportedEdition,
		HypervisorPresent:             raw.HypervisorPresent,
		VirtualizationFirmwareEnabled: raw.VirtualizationFirmwareEnabled,
		SLATSupported:                 raw.SLATSupported,
		HyperVFeatureState:            raw.HyperVFeatureState,
		HyperVFeatureKnown:            raw.HyperVFeatureKnown,
		HyperVModuleAvailable:         raw.HyperVModuleAvailable,
		HyperVVersion:                 raw.HyperVVersion,
		VMServiceStatus:               raw.VMServiceStatus,
		TermServiceStatus:             raw.TermServiceStatus,
		TermServicePresent:            raw.TermServicePresent,
		EnhancedSessionEnabled:        raw.EnhancedSessionEnabled,
		EnhancedSessionKnown:          raw.EnhancedSessionKnown,
		Overall:                       overall,
		Ready:                         ready,
		Checks:                        checks,
	}
	status.AIHelperPrompt = buildAIHelperPrompt(status)
	return status
}

func supportEnough(raw rawEnvironment) bool {
	return raw.SupportedEdition() &&
		strings.EqualFold(raw.HyperVFeatureState, "Enabled") && raw.HyperVModuleAvailable
}

func (raw rawEnvironment) SupportedEdition() bool {
	return !isHomeEdition(raw.EditionID)
}

func isHomeEdition(edition string) bool {
	switch strings.ToLower(strings.TrimSpace(edition)) {
	case "core", "coresinglelanguage", "corecountryspecific",
		"home", "homesinglelanguage", "homecountryspecific",
		"starter", "startern":
		return true
	default:
		return false
	}
}

func buildAIHelperPrompt(status EnvironmentStatus) string {
	var b strings.Builder
	b.WriteString("我在 Windows 上准备使用 Hyper-V 运行虚拟机，但环境检查没有完全通过。请根据下面的设备信息，给我一份一步一步的排查和配置教程，包含需要在 BIOS/UEFI 中修改的项目、Windows 功能启用步骤，以及如何验证是否成功。\n\n")
	b.WriteString("【设备信息】\n")
	fmt.Fprintf(&b, "- 计算机名：%s\n", status.ComputerName)
	fmt.Fprintf(&b, "- 系统：%s（%s，版本 %s，Build %s）\n", status.ProductName, status.EditionID, status.DisplayVersion, status.BuildNumber)
	fmt.Fprintf(&b, "- 系统安装类型：%s\n", status.InstallationType)
	fmt.Fprintf(&b, "- CPU：%s（%d 核 / %d 线程）\n", status.CPUName, status.CPUCores, status.LogicalProcessors)
	fmt.Fprintf(&b, "- 物理内存：%d MB\n", status.TotalMemoryMB)
	fmt.Fprintf(&b, "- BIOS/UEFI 硬件虚拟化：%v\n", status.VirtualizationFirmwareEnabled)
	fmt.Fprintf(&b, "- SLAT（二级地址转换）：%v\n", status.SLATSupported)
	fmt.Fprintf(&b, "- Hypervisor 已运行：%v\n", status.HypervisorPresent)
	fmt.Fprintf(&b, "- Hyper-V 功能状态：%s（可读取：%v）\n", status.HyperVFeatureState, status.HyperVFeatureKnown)
	fmt.Fprintf(&b, "- Hyper-V PowerShell 模块：%v（%s）\n", status.HyperVModuleAvailable, status.HyperVVersion)
	fmt.Fprintf(&b, "- vmms 服务：%s\n", status.VMServiceStatus)
	fmt.Fprintf(&b, "- TermService（增强会话依赖）：%s\n\n", status.TermServiceStatus)

	b.WriteString("【检查结果】\n")
	for _, check := range status.Checks {
		fmt.Fprintf(&b, "- [%s] %s：%s\n", check.Status, check.Title, check.Detail)
	}
	b.WriteString("\n【我的目标】\n在这台电脑上创建和运行 Hyper-V 虚拟机，并在需要时使用 VMConnect 增强会话。\n")
	b.WriteString("请按优先级给出操作步骤，说明每一步是否需要重启、如何判断成功，以及仍然失败时可以继续检查哪些地方。如果当前 Windows 版本不支持 Hyper-V，请说明可以升级到哪些版本。")
	return b.String()
}
