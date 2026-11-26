package hyperv

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunnerProtocol 验证 PowerShell 输入输出协议（中文与 JSON 编码）。
func TestRunnerProtocol(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe 不可用")
	}
	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := `
Write-HvResult ([ordered]@{ message = '测试输出'; number = 42; list = @('a','b') })
`
	var result struct {
		Message string   `json:"message"`
		Number  int      `json:"number"`
		List    []string `json:"list"`
	}
	if err := client.run(ctx, body, nil, &result); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.Message != "测试输出" {
		t.Fatalf("中文输出乱码: %q", result.Message)
	}
	if result.Number != 42 || len(result.List) != 2 {
		t.Fatalf("JSON 解析结果不正确: %+v", result)
	}
}

// TestRunnerError 验证脚本错误能被解码成中文错误消息。
func TestRunnerError(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe 不可用")
	}
	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := `throw '参数校验失败：测试错误'`
	err := client.run(ctx, body, nil, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if !strings.Contains(err.Error(), "测试错误") {
		t.Fatalf("错误消息不正确: %v", err)
	}
}

// TestScriptSyntax 使用 PowerShell 解析器检查所有内置脚本的语法。
func TestScriptSyntax(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe 不可用")
	}

	scripts := map[string]string{
		"listVMs":       vmDTOScript + "\nGet-VM | Out-Null\n",
		"power":         powerScript,
		"applySettings": applySettingsScript,
		"cloneExport":   cloneExportScript,
		"cloneImport":   cloneImportScript,
		"createDisk":    createDiskScript,
		"createVM":      createVMScript,
		"editVM":        editVMScript,
		"deleteVM":      deleteVMScript,
		"environment":   environmentScript,
	}

	for name, body := range scripts {
		body := body
		t.Run(name, func(t *testing.T) {
			full := "\uFEFF" + scriptPreamble + body + scriptFooter
			dir := t.TempDir()
			path := filepath.Join(dir, name+".ps1")
			if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
				t.Fatal(err)
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(path))
			command := `$path=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:PS_SCRIPT_PATH))` +
				`; $tokens=$null; $errors=$null` +
				`; [void][System.Management.Automation.Language.Parser]::ParseFile($path,[ref]$tokens,[ref]$errors)` +
				`; if ($errors.Count -gt 0) { Write-Output ($errors | ForEach-Object { $_.Message }); exit 1 }`
			cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
			cmd.Env = append(os.Environ(), "PS_SCRIPT_PATH="+encoded)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("脚本语法错误 %s: %v\n%s", name, err, output)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"Win11", "测试虚拟机", "VM_01-test", "a b"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("名称 %q 应该合法: %v", name, err)
		}
	}
	invalid := []string{"", "a/b", `a\b`, "a:b", "CON", "lpt1", "name.", "name "}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("名称 %q 应该被拒绝", name)
		}
	}
}

func TestValidateMemory(t *testing.T) {
	valid := MemoryOptions{StartupMB: 4096, Dynamic: true, MinimumMB: 1024, MaximumMB: 8192}
	if err := ValidateMemory(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []MemoryOptions{
		{StartupMB: 100},
		{StartupMB: 4096, Dynamic: true, MinimumMB: 8192, MaximumMB: 8192},
		{StartupMB: 4096, Dynamic: true, MinimumMB: 1024, MaximumMB: 2048},
		{StartupMB: 2000000},
	}
	for _, memory := range invalid {
		if err := ValidateMemory(memory); err == nil {
			t.Errorf("内存配置 %+v 应该被拒绝", memory)
		}
	}
}

func TestDeletePathGuard(t *testing.T) {
	protected := []string{
		`C:\Windows`,
		`C:\Windows\System32`,
		`C:\Users\test\VM`,
		`C:\ProgramData\VM`,
		`C:\`,
		`D:\`,
	}
	for _, path := range protected {
		if err := validateDeletePath(path); err == nil {
			t.Errorf("路径 %q 应该被拒绝删除", path)
		}
	}
	allowed := []string{`D:\VMs\Test`, `E:\Hyper-V\Win11`}
	for _, path := range allowed {
		if err := validateDeletePath(path); err != nil {
			t.Errorf("路径 %q 应该允许删除: %v", path, err)
		}
	}
}
