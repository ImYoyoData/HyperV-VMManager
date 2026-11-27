package hyperv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"hypervvmmanager/internal/credentials"
)

// FixClientWinRM 在本机（WinRM 客户端）启动 WinRM 服务，并把远程主机加入
// TrustedHosts。工作组环境通过 IP 使用 Negotiate/NTLM 认证时必须这样做。
// 如果当前进程不是管理员，会通过 UAC 提权执行。
func FixClientWinRM(ctx context.Context, host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("远程主机地址不能为空")
	}
	if strings.ContainsAny(host, " '\"\r\n;&|`$") {
		return "", errors.New("远程主机地址包含非法字符")
	}

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
try {
  Start-Service WinRM -ErrorAction Stop
  $current = ''
  try { $current = (Get-Item 'WSMan:\localhost\Client\TrustedHosts' -ErrorAction Stop).Value } catch { $current = '' }
  $items = @($current -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
  $target = '%s'
  if (-not ($items -contains '*') -and -not ($items -icontains $target)) { $items += $target }
  Set-Item 'WSMan:\localhost\Client\TrustedHosts' -Value ($items -join ',') -Force
  $value = (Get-Item 'WSMan:\localhost\Client\TrustedHosts').Value
  if ($value) { 'OK:' + $value } else { 'OK:' }
} catch {
  'ERR:' + $_.Exception.Message
}
`, host)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if IsAdmin() {
		out, err := runPowerShellScript(ctx, script)
		if err != nil {
			return "", err
		}
		return parseFixResult(out)
	}

	return runElevatedFix(ctx, script)
}

func runPowerShellScript(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if text := decodeConsoleText(string(out)); text != "" {
			return "", errors.New(text)
		}
		return "", fmt.Errorf("执行 WinRM 配置命令失败: %w", err)
	}
	return decodeConsoleText(string(out)), nil
}

func runElevatedFix(ctx context.Context, script string) (string, error) {
	dir, err := os.MkdirTemp("", "hvvm-winrm-*")
	if err != nil {
		return "", fmt.Errorf("无法创建临时目录: %w", err)
	}
	defer os.RemoveAll(dir)

	scriptPath := filepath.Join(dir, "fix-winrm.ps1")
	resultPath := filepath.Join(dir, "result.txt")
	// 将脚本包进 here-string，再执行并把结果写入文件，便于提权进程结束后读取。
	wrapped := "\uFEFF$ErrorActionPreference = 'Stop'\n" +
		"$resultPath = '" + escapePowerShellPath(resultPath) + "'\n" +
		"$script = @'\n" + script + "\n'@\n" +
		"try { $result = (& ([ScriptBlock]::Create($script))) -join \"`n\" } catch { $result = 'ERR:' + $_.Exception.Message }\n" +
		"$result | Out-File -LiteralPath $resultPath -Encoding UTF8\n"

	if err := os.WriteFile(scriptPath, []byte(wrapped), 0o600); err != nil {
		return "", fmt.Errorf("无法写入临时脚本: %w", err)
	}

	launcher := fmt.Sprintf(
		"$p = Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList @('-NoLogo','-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File','%s'); exit $p.ExitCode",
		escapePowerShellPath(scriptPath),
	)
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", launcher)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", errors.New("等待管理员授权超时，请重试并在 UAC 弹窗中点击“是”")
		}
		if text := decodeConsoleText(string(out)); text != "" {
			return "", errors.New(text)
		}
		return "", fmt.Errorf("请求管理员权限失败: %w", err)
	}

	data, err := os.ReadFile(resultPath)
	if err != nil {
		return "", errors.New("管理员进程没有返回结果，可能已取消 UAC 授权")
	}
	return parseFixResult(decodeConsoleText(string(data)))
}

func parseFixResult(out string) (string, error) {
	out = strings.TrimSpace(strings.TrimPrefix(out, "\uFEFF"))
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ERR:") {
			return "", errors.New(strings.TrimSpace(strings.TrimPrefix(line, "ERR:")))
		}
		if strings.HasPrefix(line, "OK:") {
			hosts := strings.TrimSpace(strings.TrimPrefix(line, "OK:"))
			if hosts == "" {
				hosts = "（空）"
			}
			return "已启动 WinRM 客户端服务，并把远程主机加入 TrustedHosts。\n当前 TrustedHosts：" + hosts, nil
		}
	}
	if out == "" {
		return "", errors.New("WinRM 配置命令没有返回结果")
	}
	return out, nil
}

func escapePowerShellPath(path string) string {
	return strings.ReplaceAll(path, "'", "''")
}

// SaveHostCredential 把远程主机的凭据写入 Windows 凭据管理器，
// 供 VMConnect、远程桌面等系统工具复用。
// 凭据目标使用 TERMSRV/<主机>，这是 Windows 远程连接读取凭据时使用的名称。
// 远程主机使用 Microsoft 账户时，VMConnect 的 Negotiate 不接受邮箱格式，
// 应使用「计算机名\本地用户名」或「MicrosoftAccount\邮箱」形式。
func SaveHostCredential(host, username, password string) error {
	host = strings.TrimSpace(host)
	username = strings.TrimSpace(username)
	if host == "" {
		return errors.New("远程主机地址不能为空")
	}
	if username == "" {
		return errors.New("用户名不能为空")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("远程账户密码为空。Windows 默认禁止空密码本地账户进行远程登录（包括 VMConnect），请先在远程主机上为该账户设置密码")
	}
	if strings.ContainsAny(host+username, "\r\n") {
		return errors.New("主机地址或用户名包含非法字符")
	}
	return credentials.Save(host, username, password)
}
