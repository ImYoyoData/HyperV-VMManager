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
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Target 描述当前要操作的 Hyper-V 主机。ComputerName 为空表示本机。
type Target struct {
	ID             string
	Name           string
	ComputerName   string
	Port           int
	UseSSL         bool
	Username       string
	Password       string
	Authentication string
}

func LocalTarget() Target {
	return Target{ID: "local", Name: "本机"}
}

func (t Target) IsLocal() bool {
	return strings.TrimSpace(t.ComputerName) == ""
}

// Client 封装对 Hyper-V PowerShell 模块的调用。
// 本机直接运行 powershell.exe；远程通过 WinRM (Invoke-Command) 执行同一段脚本。
type Client struct {
	target Target
}

func NewClient() *Client {
	return &Client{target: LocalTarget()}
}

func NewClientForTarget(target Target) *Client {
	return &Client{target: target}
}

func (c *Client) Target() Target { return c.target }

const createNoWindow = 0x08000000

// scriptPreamble 统一脚本的输入输出协议：
//   - 参数：环境变量 HV_OPTS，值为 base64(UTF-8 JSON)
//   - 结果：stdout，值为 base64(UTF-8 JSON)
//   - 错误：stdout，前缀 HVFAIL: + base64(UTF-8 错误消息)
//
// 使用 base64 是为了绕开 Windows PowerShell 5.1 的控制台代码页问题，
// 保证中文名称、中文错误消息都不会乱码。
// 远程模式（HV_REMOTE=1）下使用 Write-Output，让 Invoke-Command 把结果带回本地。
const scriptPreamble = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$WarningPreference = 'SilentlyContinue'
$opts = $null
if ($env:HV_OPTS) {
  $opts = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:HV_OPTS)) | ConvertFrom-Json
}
function Write-HvResult($obj) {
  $json = ConvertTo-Json -InputObject $obj -Depth 12 -Compress
  $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
  if ($env:HV_REMOTE -eq '1') { Write-Output $b64 } else { [Console]::Out.Write($b64) }
}
function Write-HvError($msg) {
  $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("$msg"))
  $line = "HVFAIL:$b64"
  if ($env:HV_REMOTE -eq '1') { Write-Output $line } else { [Console]::Out.Write($line) }
}
try {
`

const scriptFooter = `
} catch {
  Write-HvError $_.Exception.Message
  return
}
`

func buildScript(body string, withBOM bool) string {
	script := scriptPreamble + body + scriptFooter
	if withBOM {
		return "\uFEFF" + script
	}
	return script
}

// remoteWrapperScript 在本地执行：建立 WinRM 会话，把目标脚本送到远程主机执行。
// 错误统一以 HVFAIL:base64(UTF-8) 写到 stdout，避免中文 Windows 的 GBK 控制台
// 代码页把中文错误消息变成乱码。
const remoteWrapperScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$sessionParams = @{ ComputerName = $env:HV_HOST; ErrorAction = 'Stop' }
if ($env:HV_PORT) { $sessionParams.Port = [int]$env:HV_PORT }
if ($env:HV_SSL -eq '1') {
  $sessionParams.UseSSL = $true
  $sessionParams.SessionOption = New-PSSessionOption -SkipCACheck -SkipCNCheck
}
if ($env:HV_AUTH) { $sessionParams.Authentication = $env:HV_AUTH }
if ($env:HV_USER) {
  $password = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:HV_PASS))
  $secure = ConvertTo-SecureString $password -AsPlainText -Force
  $sessionParams.Credential = New-Object System.Management.Automation.PSCredential($env:HV_USER, $secure)
}
$session = $null
try {
  $session = New-PSSession @sessionParams
  $scriptText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:HV_SCRIPT))
  $output = Invoke-Command -Session $session -ScriptBlock {
    param($scriptText, $opts)
    $env:HV_OPTS = $opts
    $env:HV_REMOTE = '1'
    Invoke-Expression $scriptText
  } -ArgumentList $scriptText, $env:HV_OPTS
  $last = @($output) | Select-Object -Last 1
  if ($null -ne $last) { [Console]::Out.Write([string]$last) }
} catch {
  $b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("$($_.Exception.Message)"))
  [Console]::Out.Write("HVFAIL:$b64")
  exit 1
} finally {
  if ($session) { Remove-PSSession $session -ErrorAction SilentlyContinue }
}
`

func encodeOptions(opts any) (string, error) {
	if opts == nil {
		return "", nil
	}
	payload, err := json.Marshal(opts)
	if err != nil {
		return "", fmt.Errorf("无法序列化脚本参数: %w", err)
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}

func parseOutput(raw string, out any) error {
	raw = strings.TrimSpace(raw)
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

// decodeConsoleText 把 Windows 控制台输出转换为 UTF-8。
// 中文 Windows 的控制台默认使用 GBK(936) 代码页，若直接当作 UTF-8
// 展示，中文错误消息会变成乱码。
func decodeConsoleText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if utf8.ValidString(raw) {
		return raw
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().String(raw); err == nil {
		return strings.TrimSpace(decoded)
	}
	return raw
}

// failureDetail 从 stdout / stderr 中提取可读的失败原因。
// 连接阶段（New-PSSession）的错误会以 HVFAIL:base64(UTF-8) 写到 stdout；
// 其他 PowerShell 运行时错误则以本地控制台编码写到 stderr。
func failureDetail(stdout, stderr string) string {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "HVFAIL:") {
			continue
		}
		message := strings.TrimPrefix(line, "HVFAIL:")
		if decoded, err := base64.StdEncoding.DecodeString(message); err == nil {
			if text := strings.TrimSpace(string(decoded)); text != "" {
				return text
			}
		}
	}
	return decodeConsoleText(stderr)
}

// run 执行脚本并解析结果。远程目标会通过 WinRM 在目标主机上执行。
func (c *Client) run(ctx context.Context, body string, opts any, out any) error {
	if c.target.IsLocal() {
		return c.runLocal(ctx, body, opts, out)
	}
	return c.runRemote(ctx, body, opts, out)
}

func (c *Client) runLocal(ctx context.Context, body string, opts any, out any) error {
	envValue, err := encodeOptions(opts)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "hyperv-vmmanager-*.ps1")
	if err != nil {
		return fmt.Errorf("无法创建临时脚本: %w", err)
	}
	defer os.Remove(tmp.Name())

	// UTF-8 BOM 是 Windows PowerShell 5.1 正确识别 UTF-8 脚本的前提。
	if _, err := tmp.WriteString(buildScript(body, true)); err != nil {
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
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if detail := failureDetail(stdout.String(), stderr.String()); detail != "" {
			return errors.New(detail)
		}
		return fmt.Errorf("PowerShell 执行失败: %w", runErr)
	}
	return parseOutput(stdout.String(), out)
}

func (c *Client) runRemote(ctx context.Context, body string, opts any, out any) error {
	optsValue, err := encodeOptions(opts)
	if err != nil {
		return err
	}
	script := buildScript(body, false)

	tmp, err := os.CreateTemp("", "hyperv-vmmanager-remote-*.ps1")
	if err != nil {
		return fmt.Errorf("无法创建临时脚本: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString("\uFEFF" + remoteWrapperScript); err != nil {
		tmp.Close()
		return fmt.Errorf("无法写入临时脚本: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("无法关闭临时脚本: %w", err)
	}

	port := c.target.Port
	if port <= 0 {
		if c.target.UseSSL {
			port = 5986
		} else {
			port = 5985
		}
	}

	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
	cmd.Env = append(os.Environ(),
		"HV_SCRIPT="+base64.StdEncoding.EncodeToString([]byte(script)),
		"HV_OPTS="+optsValue,
		"HV_HOST="+c.target.ComputerName,
		"HV_PORT="+strconv.Itoa(port),
		"HV_SSL="+boolFlag(c.target.UseSSL),
		"HV_USER="+c.target.Username,
		"HV_PASS="+base64.StdEncoding.EncodeToString([]byte(c.target.Password)),
		"HV_AUTH="+c.target.Authentication,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if detail := failureDetail(stdout.String(), stderr.String()); detail != "" {
			return errors.New(detail)
		}
		return fmt.Errorf("WinRM 连接失败: %w", runErr)
	}
	return parseOutput(stdout.String(), out)
}

func boolFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// TestConnection 验证远程 WinRM 连接与 Hyper-V 权限，然后返回主机信息。
func (c *Client) TestConnection(ctx context.Context) (*HostInfo, error) {
	body := `
$vh = Get-VMHost -ErrorAction Stop
Write-HvResult ([ordered]@{ name = "$env:COMPUTERNAME"; version = "$($vh.Version)" })
`
	if err := c.run(ctx, body, nil, nil); err != nil {
		return nil, err
	}
	return c.HostInfo(ctx)
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

// OpenTool 打开系统中的管理工具（始终作用于本机）。
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

// ConnectVM 使用 VMConnect 打开虚拟机控制台；远程主机使用 host name 形式。
func (c *Client) ConnectVM(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("虚拟机名称不能为空")
	}
	server := "localhost"
	if !c.target.IsLocal() {
		server = c.target.ComputerName
	}
	return startDetached("vmconnect.exe", server, name)
}
