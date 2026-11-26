package hyperv

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Client 封装对 Hyper-V PowerShell 模块的调用。
// 每个方法都在独立的 powershell.exe 进程中运行，不依赖全局会话状态。
type Client struct{}

func NewClient() *Client { return &Client{} }

const createNoWindow = 0x08000000

// scriptPreamble 统一脚本的输入输出协议：
//   - 参数：环境变量 HV_OPTS，值为 base64(UTF-8 JSON)
//   - 结果：stdout，值为 base64(UTF-8 JSON)
//   - 错误：stdout，前缀 HVFAIL: + base64(UTF-8 错误消息)
//
// 使用 base64 是为了绕开 Windows PowerShell 5.1 的控制台代码页问题，
// 保证中文名称、中文错误消息都不会乱码。
const scriptPreamble = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$WarningPreference = 'SilentlyContinue'
$opts = $null
if ($env:HV_OPTS) {
  $opts = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:HV_OPTS)) | ConvertFrom-Json
}
function Write-HvResult($obj) {
  $json = ConvertTo-Json -InputObject $obj -Depth 12 -Compress
  [Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json)))
}
function Write-HvError($msg) {
  $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("$msg"))
  [Console]::Out.Write("HVFAIL:$b64")
}
try {
`

const scriptFooter = `
} catch {
  Write-HvError $_.Exception.Message
  exit 1
}
`

// run 执行一段 PowerShell 脚本并解析结果。
// opts 会以 Base64 JSON 的形式传给脚本内的 $opts；out 为 nil 时不解析返回值。
func (c *Client) run(ctx context.Context, body string, opts any, out any) error {
	envValue := ""
	if opts != nil {
		payload, err := json.Marshal(opts)
		if err != nil {
			return fmt.Errorf("无法序列化脚本参数: %w", err)
		}
		envValue = base64.StdEncoding.EncodeToString(payload)
	}

	tmp, err := os.CreateTemp("", "hyperv-vmmanager-*.ps1")
	if err != nil {
		return fmt.Errorf("无法创建临时脚本: %w", err)
	}
	defer os.Remove(tmp.Name())

	// UTF-8 BOM 是 Windows PowerShell 5.1 正确识别 UTF-8 脚本的前提。
	script := "\uFEFF" + scriptPreamble + body + scriptFooter
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return fmt.Errorf("无法写入临时脚本: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("无法关闭临时脚本: %w", err)
	}

	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
	cmd.Env = append(os.Environ(), "HV_OPTS="+envValue)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	raw := strings.TrimSpace(stdout.String())

	if strings.HasPrefix(raw, "HVFAIL:") {
		message := strings.TrimPrefix(raw, "HVFAIL:")
		if decoded, decErr := base64.StdEncoding.DecodeString(message); decErr == nil {
			message = string(decoded)
		}
		if strings.TrimSpace(message) == "" {
			message = "Hyper-V 操作失败"
		}
		return errors.New(strings.TrimSpace(message))
	}

	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = runErr.Error()
		}
		return errors.New(detail)
	}

	if raw == "" || out == nil {
		return nil
	}

	payload, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("无法解析 PowerShell 输出: %w", err)
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("无法解析 PowerShell 返回的数据: %w", err)
	}
	return nil
}

// IsAdmin 判断当前进程是否具有管理员权限。
func IsAdmin() bool {
	cmd := exec.Command("net", "session")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Run() == nil
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Start()
}

// OpenTool 打开系统中的管理工具。
func (c *Client) OpenTool(tool string) error {
	var target string
	switch tool {
	case "hyperv-manager":
		target = "virtmgmt.msc"
	case "switch-manager":
		target = "virtmgmt.msc"
	case "hyperv-settings":
		target = "virtmgmt.msc"
	case "services":
		target = "services.msc"
	case "features":
		target = "optionalfeatures"
	case "taskmgr":
		target = "taskmgr"
	default:
		return fmt.Errorf("未知的管理工具: %s", tool)
	}
	return startDetached("cmd", "/c", "start", "", target)
}

// ConnectVM 使用 VMConnect 打开虚拟机控制台。
func (c *Client) ConnectVM(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("虚拟机名称不能为空")
	}
	return startDetached("vmconnect.exe", "localhost", name)
}
