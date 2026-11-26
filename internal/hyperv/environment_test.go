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
