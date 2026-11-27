package hyperv

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEmbeddedScriptsParse 用 PowerShell 解析器检查内嵌脚本的语法，
// 避免修改脚本后只有运行时才暴露语法错误。
func TestEmbeddedScriptsParse(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows 需要检查 PowerShell 语法")
	}
	scripts := map[string]string{
		"create-disk":  buildScript(createDiskScript, true),
		"create-vm":    buildScript(createVMScript, true),
		"clone-export": buildScript(cloneExportScript, true),
		"clone-diff":   buildScript(cloneDifferencingScript, true),
		"clone-import": buildScript(cloneImportScript, true),
		"edit":         buildScript(editVMScript, true),
		"environment":  buildScript(environmentScript, true),
		"nat-list":     buildScript(natScriptHelpers+listNATScript, true),
		"nat-create":   buildScript(natScriptHelpers+createNATScript, true),
		"nat-remove":   buildScript(natScriptHelpers+removeNATScript, true),
		"nat-add":      buildScript(natScriptHelpers+addNATMappingScript, true),
		"nat-del":      buildScript(natScriptHelpers+removeNATMappingScript, true),
	}
	for name, script := range scripts {
		name, script := name, script
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".ps1")
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatalf("写入脚本失败: %v", err)
			}
			command := `$null = [ScriptBlock]::Create((Get-Content -Raw -LiteralPath '` + path + `'))`
			cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("PowerShell 语法错误: %v\n%s", err, out)
			}
		})
	}
}
