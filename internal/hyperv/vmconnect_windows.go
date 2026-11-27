//go:build windows

package hyperv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	logonNetCredentialsOnly  = 0x00000002
	createUnicodeEnvironment = 0x00000400
	errorLogonFailure        = 1326
	errorAccountRestriction  = 1327
	errorAccountDisabled     = 1331
	errorLogonTypeNotGranted = 1385
	errorPasswordExpired     = 1330
	errorInvalidLogonHours   = 1328
	errorInvalidWorkstation  = 1329
)

var (
	advapi32DLL                 = syscall.NewLazyDLL("advapi32.dll")
	procCreateProcessWithLogonW = advapi32DLL.NewProc("CreateProcessWithLogonW")
)

type processInformation struct {
	Process   syscall.Handle
	Thread    syscall.Handle
	ProcessID uint32
	ThreadID  uint32
}

// ConnectVMWithCredential 使用指定的远程主机账户启动 VMConnect。
//
// VMConnect 是独立进程，它内部的 WinRM 查询不会复用本应用已经建立的
// WinRM 会话，而是使用进程自己的网络凭据。因此必须用
// LOGON_NETCREDENTIALS_ONLY（等价于 runas /netonly）启动，让 VMConnect
// 的 WinRM 使用远程主机账户，而不是当前 Windows 登录账户。
func (c *Client) ConnectVMWithCredential(name, username, password string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("虚拟机名称不能为空")
	}
	if c.target.IsLocal() {
		return c.ConnectVM(name)
	}
	return startVMConnectWithCredentials(c.target.ComputerName, name, username, password)
}

func startVMConnectWithCredentials(server, vmName, username, password string) error {
	server = strings.TrimSpace(server)
	username = strings.TrimSpace(username)
	if server == "" {
		return errors.New("远程主机地址不能为空")
	}
	if username == "" {
		return errors.New("请输入远程主机的 Windows 用户名")
	}
	if password == "" {
		return errors.New("远程 VMConnect 需要密码用于网络身份验证；空密码账户默认不能远程登录")
	}

	domain, user := splitLogonUsername(username)
	if domain == "" && !strings.Contains(user, "@") {
		return errors.New("用户名需要带远程计算机名，例如 DESKTOP-1234\\001；Microsoft 账户请用 MicrosoftAccount\\邮箱")
	}

	exePath := vmconnectPath()
	if exePath == "" {
		return errors.New("找不到 vmconnect.exe，请确认 Hyper-V 管理工具已安装")
	}

	// VMConnect 的命令行参数是“主机 虚拟机名”。名称可能包含空格，统一加引号。
	commandLine := fmt.Sprintf(`"%s" "%s" "%s"`, exePath, server, vmName)
	applicationName, _ := syscall.UTF16PtrFromString(exePath)
	commandLinePtr, _ := syscall.UTF16PtrFromString(commandLine)
	userPtr, _ := syscall.UTF16PtrFromString(user)
	passwordPtr, _ := syscall.UTF16PtrFromString(password)
	var domainPtr *uint16
	if domain != "" {
		domainPtr, _ = syscall.UTF16PtrFromString(domain)
	}
	desktopPtr, _ := syscall.UTF16PtrFromString(`winsta0\default`)

	var startup syscall.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Desktop = desktopPtr
	var process processInformation

	result, _, callErr := procCreateProcessWithLogonW.Call(
		uintptr(unsafe.Pointer(userPtr)),
		uintptr(unsafe.Pointer(domainPtr)),
		uintptr(unsafe.Pointer(passwordPtr)),
		uintptr(logonNetCredentialsOnly),
		uintptr(unsafe.Pointer(applicationName)),
		uintptr(unsafe.Pointer(commandLinePtr)),
		uintptr(createUnicodeEnvironment),
		0,
		0,
		uintptr(unsafe.Pointer(&startup)),
		uintptr(unsafe.Pointer(&process)),
	)
	// 确保 UTF-16 缓冲区在系统调用期间一直有效。
	runtime.KeepAlive(applicationName)
	runtime.KeepAlive(commandLinePtr)
	runtime.KeepAlive(userPtr)
	runtime.KeepAlive(passwordPtr)
	runtime.KeepAlive(domainPtr)
	runtime.KeepAlive(desktopPtr)

	if result == 0 {
		return formatVMConnectLogonError(callErr)
	}
	_ = syscall.CloseHandle(process.Thread)
	_ = syscall.CloseHandle(process.Process)
	return nil
}

func splitLogonUsername(value string) (domain, user string) {
	if index := strings.Index(value, `\`); index >= 0 {
		return strings.TrimSpace(value[:index]), strings.TrimSpace(value[index+1:])
	}
	return "", strings.TrimSpace(value)
}

func vmconnectPath() string {
	if root := strings.TrimSpace(os.Getenv("SystemRoot")); root != "" {
		path := filepath.Join(root, "System32", "vmconnect.exe")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if path, err := exec.LookPath("vmconnect.exe"); err == nil {
		return path
	}
	return ""
}

func formatVMConnectLogonError(err error) error {
	errno, ok := err.(syscall.Errno)
	if !ok {
		return fmt.Errorf("以远程主机账户启动 VMConnect 失败: %w", err)
	}
	switch errno {
	case errorLogonFailure:
		return errors.New("远程主机用户名或密码错误（1326）。请确认填写的是远程 Hyper-V 宿主机账户，不是虚拟机里的账户")
	case errorAccountRestriction:
		return errors.New("远程主机账户被限制远程登录（1327）。常见原因是账户没有密码，Windows 默认禁止空密码账户进行网络登录")
	case errorPasswordExpired:
		return errors.New("远程主机账户密码已过期（1330）")
	case errorAccountDisabled:
		return errors.New("远程主机账户已被禁用（1331）")
	case errorInvalidLogonHours:
		return errors.New("远程主机账户不允许在当前时间登录（1328）")
	case errorInvalidWorkstation:
		return errors.New("远程主机账户不允许从这台电脑登录（1329）")
	case errorLogonTypeNotGranted:
		return errors.New("远程主机账户没有网络登录权限（1385）")
	case 1058, 1060, 1062:
		return errors.New("Windows 的 Secondary Logon (seclogon) 服务未运行，无法以其他账户启动 VMConnect。请以管理员身份运行 sc start seclogon 后重试")
	case 5:
		return errors.New("启动 VMConnect 被拒绝（5）。请尝试以管理员身份运行本应用")
	default:
		return fmt.Errorf("以远程主机账户启动 VMConnect 失败（错误 %d）：%w", uint32(errno), errno)
	}
}
