package hyperv

import (
	"strings"
	"testing"
)

func baseRawEnvironment() rawEnvironment {
	return rawEnvironment{
		ComputerName:                  "TEST-PC",
		ProductName:                   "Windows 11 Pro",
		EditionID:                     "Professional",
		DisplayVersion:                "24H2",
		BuildNumber:                   "26100.2314",
		InstallationType:              "Client",
		CPUName:                       "Test CPU",
		CPUCores:                      8,
		LogicalProcessors:             16,
		TotalMemoryMB:                 32768,
		IsAdmin:                       true,
		HypervisorPresent:             true,
		VirtualizationFirmwareEnabled: true,
		SLATSupported:                 true,
		HyperVFeatureState:            "Enabled",
		HyperVFeatureKnown:            true,
		HyperVFunctional:              true,
		HyperVModuleAvailable:         true,
		HyperVVersion:                 "2.0.0.0",
		VMServiceStatus:               "Running",
		TermServiceStatus:             "Running",
		TermServicePresent:            true,
		EnhancedSessionEnabled:        true,
		EnhancedSessionKnown:          true,
	}
}

func TestEvaluateEnvironmentReady(t *testing.T) {
	status := evaluateEnvironment(baseRawEnvironment())
	if !status.Ready {
		t.Fatalf("完整环境应当 ready，实际 %s: %+v", status.Overall, status.Checks)
	}
	if status.Overall != "ready" {
		t.Fatalf("整体状态应为 ready，实际 %s", status.Overall)
	}
	if !strings.Contains(status.AIHelperPrompt, "TEST-PC") {
		t.Fatal("AI 提示词缺少设备信息")
	}
}

func TestEvaluateEnvironmentServerFeatureUnknownButFunctional(t *testing.T) {
	// Windows Server 2019 上 Hyper-V 是服务器角色，Get-WindowsOptionalFeature
	// 可能读不到状态；只要 Get-VMHost 成功，就应判定组件可用。
	raw := baseRawEnvironment()
	raw.ProductName = "Windows Server 2019 Standard"
	raw.EditionID = "ServerStandard"
	raw.InstallationType = "Server"
	raw.HyperVFeatureState = "Unknown"
	raw.HyperVFeatureKnown = false
	raw.HyperVFunctional = true

	status := evaluateEnvironment(raw)
	if !status.Ready {
		t.Fatalf("服务器角色且 Get-VMHost 成功时应 ready，实际 %s: %+v", status.Overall, status.Checks)
	}
	if status.Overall != "ready" {
		t.Fatalf("整体状态应为 ready，实际 %s", status.Overall)
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "hyperv-feature" && check.Status == CheckOK {
			found = true
		}
	}
	if !found {
		t.Fatal("Hyper-V 组件检查应通过")
	}
}

func TestEvaluateEnvironmentSlatUnknownButHypervisorRunning(t *testing.T) { // 用户机器上 Hyper-V 已运行，但 WMI 可能报告 SLAT=false。
	// 这种情况不能判定为不支持。
	raw := baseRawEnvironment()
	raw.SLATSupported = false
	raw.HypervisorPresent = true
	raw.VirtualizationFirmwareEnabled = false

	status := evaluateEnvironment(raw)
	if !status.Ready {
		t.Fatalf("Hypervisor 已运行但 SLAT WMI 为 false 时应 ready，实际 %s: %+v", status.Overall, status.Checks)
	}
	if status.Overall != "ready" {
		t.Fatalf("整体状态应为 ready，实际 %s", status.Overall)
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "virtualization" && check.Status == CheckOK {
			found = true
		}
	}
	if !found {
		t.Fatal("CPU 虚拟化检查应通过")
	}
}

func TestEvaluateEnvironmentSlatUnknownButFirmwareEnabled(t *testing.T) {
	raw := baseRawEnvironment()
	raw.SLATSupported = false
	raw.HypervisorPresent = false
	raw.VirtualizationFirmwareEnabled = true

	status := evaluateEnvironment(raw)
	if !status.Ready {
		t.Fatalf("固件虚拟化已开启时应 ready，实际 %s: %+v", status.Overall, status.Checks)
	}
}

func TestEvaluateEnvironmentHomeEdition(t *testing.T) {
	raw := baseRawEnvironment()
	raw.EditionID = "Core"
	raw.ProductName = "Windows 11 家庭版"
	raw.HyperVFeatureState = "Unknown"
	raw.HyperVFeatureKnown = false
	status := evaluateEnvironment(raw)
	if status.Overall != "unsupported" {
		t.Fatalf("家庭版应为 unsupported，实际 %s", status.Overall)
	}
	if status.SupportedEdition {
		t.Fatal("家庭版不应被判断为支持")
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "edition" && check.Action == "upgrade-windows" {
			found = true
		}
	}
	if !found {
		t.Fatal("家庭版检查项应提供升级建议")
	}
}

func TestEvaluateEnvironmentNoVirtualization(t *testing.T) {
	raw := baseRawEnvironment()
	raw.HypervisorPresent = false
	raw.VirtualizationFirmwareEnabled = false
	status := evaluateEnvironment(raw)
	if status.Ready {
		t.Fatal("未开启虚拟化时不应 ready")
	}
	if status.Overall != "needs-action" {
		t.Fatalf("应为 needs-action，实际 %s", status.Overall)
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "virtualization" && check.Action == "bios" {
			found = true
		}
	}
	if !found {
		t.Fatal("应提示 BIOS/UEFI 开启虚拟化")
	}
}

func TestEvaluateEnvironmentHyperVDisabled(t *testing.T) {
	raw := baseRawEnvironment()
	raw.HyperVFeatureState = "Disabled"
	raw.HyperVFunctional = false
	raw.HyperVModuleAvailable = false
	raw.VMServiceStatus = "Absent"
	status := evaluateEnvironment(raw)
	if status.Ready {
		t.Fatal("Hyper-V 功能未启用时不应 ready")
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "hyperv-feature" && check.Action == "open-features" {
			found = true
		}
	}
	if !found {
		t.Fatal("应提供打开 Windows 功能的操作")
	}
}

func TestEvaluateEnvironmentEnhancedSessionWarningOnly(t *testing.T) {
	raw := baseRawEnvironment()
	raw.EnhancedSessionEnabled = false
	status := evaluateEnvironment(raw)
	if !status.Ready {
		t.Fatalf("增强会话关闭不应阻止应用 ready，实际 %s", status.Overall)
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "enhanced-session" && check.Action == "fix-enhanced-session" {
			found = true
		}
	}
	if !found {
		t.Fatal("应提供修复增强会话的入口")
	}
}

func TestEvaluateEnvironmentClientGPUParavirtualOnly(t *testing.T) {
	raw := baseRawEnvironment()
	raw.GPUs = []GPUDevice{{Name: "Test GPU"}}
	raw.PartitionableGPUs = []PartitionableGPU{{
		Name:                 `\\?\PCI#VEN_10DE#GPUPARAV`,
		Paravirtual:          true,
		ValidPartitionCounts: []int32{32},
		PartitionCount:       32,
		TotalVRAMBytes:       8 << 30,
	}}

	status := evaluateEnvironment(raw)
	if status.GPUAssignmentSupported {
		t.Fatal("客户端 GPU-PV 不应被判断为官方 GPU-P")
	}
	if status.GPUAssignmentMode != "gpupv" {
		t.Fatalf("GPU 分配模式应为 gpupv，实际 %s", status.GPUAssignmentMode)
	}
	if !status.Ready {
		t.Fatalf("GPU 警告不应阻止 ready，实际 %s: %+v", status.Overall, status.Checks)
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "gpu" && check.Status == CheckWarn && check.Action == "gpu-help" {
			found = true
		}
	}
	if !found {
		t.Fatal("客户端应显示 GPU 显存分配警告和说明入口")
	}
}

func TestEvaluateEnvironmentServer2025GPUP(t *testing.T) {
	raw := baseRawEnvironment()
	raw.ProductName = "Windows Server 2025 Standard"
	raw.EditionID = "ServerStandard"
	raw.InstallationType = "Server"
	raw.BuildNumber = "26100.1000"
	raw.GPUs = []GPUDevice{{Name: "NVIDIA A2"}}
	raw.PartitionableGPUs = []PartitionableGPU{{
		Name:                 `\\?\PCI#VEN_10DE&DEV_25B6`,
		ValidPartitionCounts: []int32{2, 4, 8, 16},
		PartitionCount:       4,
		TotalVRAMBytes:       16 << 30,
		AvailableVRAMBytes:   16 << 30,
	}}

	status := evaluateEnvironment(raw)
	if !status.GPUAssignmentSupported {
		t.Fatal("Windows Server 2025 + 可分区 GPU 应支持官方 GPU-P")
	}
	if status.GPUAssignmentMode != "gpup" {
		t.Fatalf("GPU 分配模式应为 gpup，实际 %s", status.GPUAssignmentMode)
	}
	found := false
	for _, check := range status.Checks {
		if check.ID == "gpu" && check.Status == CheckOK {
			found = true
		}
	}
	if !found {
		t.Fatal("Server 2025 可分区 GPU 检查应通过")
	}
}

func TestEvaluateEnvironmentServer2025WithoutPartitionableGPU(t *testing.T) {
	raw := baseRawEnvironment()
	raw.ProductName = "Windows Server 2025 Standard"
	raw.EditionID = "ServerStandard"
	raw.InstallationType = "Server"
	raw.BuildNumber = "26100.1000"
	raw.GPUs = []GPUDevice{{Name: "Consumer GPU"}}

	status := evaluateEnvironment(raw)
	if status.GPUAssignmentSupported {
		t.Fatal("没有可分区 GPU 时不应支持官方 GPU-P")
	}
	if status.GPUAssignmentMode != "gpup-unsupported" {
		t.Fatalf("GPU 分配模式应为 gpup-unsupported，实际 %s", status.GPUAssignmentMode)
	}
	if !status.Ready {
		t.Fatalf("GPU 警告不应阻止 ready，实际 %s: %+v", status.Overall, status.Checks)
	}
}
