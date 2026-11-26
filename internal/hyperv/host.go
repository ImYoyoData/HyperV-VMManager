package hyperv

import (
	"context"
	"fmt"
	"strings"
)

// HostInfo 读取 Hyper-V 主机、操作系统与增强会话状态。
// 非管理员运行时仍然返回可读取的部分，Hyper-V 相关字段留空。
func (c *Client) HostInfo(ctx context.Context) (*HostInfo, error) {
	body := `
$vh = $null
try { $vh = Get-VMHost -ErrorAction Stop } catch { }
$os = Get-CimInstance Win32_OperatingSystem -ErrorAction Stop
$cs = Get-CimInstance Win32_ComputerSystem -ErrorAction Stop
$module = Get-Module -ListAvailable Hyper-V | Sort-Object Version -Descending | Select-Object -First 1
$installationType = "$((Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' -ErrorAction SilentlyContinue).InstallationType)"
$vmms = Get-Service vmms -ErrorAction SilentlyContinue
$term = Get-Service TermService -ErrorAction SilentlyContinue
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
$enhanced = $false
if ($vh) { $enhanced = [bool]$vh.EnableEnhancedSessionMode }
Write-HvResult ([ordered]@{
  computerName = "$env:COMPUTERNAME"
  osName = "$($os.Caption) $($os.Version)"
  hyperVVersion = "$($module.Version)"
  logicalProcessors = [int]$cs.NumberOfLogicalProcessors
  totalMemoryMB = [int64]($cs.TotalPhysicalMemory / 1MB)
  virtualMachinePath = if ($vh) { "$($vh.VirtualMachinePath)" } else { "" }
  virtualHardDiskPath = if ($vh) { "$($vh.VirtualHardDiskPath)" } else { "" }
  installationType = $installationType
  enhancedSessionSupported = ($installationType -ne 'Server Core')
  enhancedSessionEnabled = $enhanced
  vmServiceStatus = "$($vmms.Status)"
  termServiceStatus = "$($term.Status)"
  isAdmin = $isAdmin
  moduleAvailable = ($null -ne $module)
})
`
	var info HostInfo
	if err := c.run(ctx, body, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// EnhancedSessionStatus 返回增强会话的详细状态。
func (c *Client) EnhancedSessionStatus(ctx context.Context) (*EnhancedSessionStatus, error) {
	body := `
$installationType = "$((Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' -ErrorAction SilentlyContinue).InstallationType)"
$reg = 0
$regValue = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Virtualization' -Name EnableEnhancedSessionMode -ErrorAction SilentlyContinue).EnableEnhancedSessionMode
if ($null -ne $regValue) { $reg = [int]$regValue }
$policy = $false
try { $policy = [bool](Get-VMHost -ErrorAction Stop).EnableEnhancedSessionMode } catch { }
$vmms = Get-Service vmms -ErrorAction SilentlyContinue
$term = Get-Service TermService -ErrorAction SilentlyContinue
Write-HvResult ([ordered]@{
  installationType = $installationType
  supported = ($installationType -ne 'Server Core')
  hostPolicyEnabled = $policy
  registryValue = $reg
  vmServiceStatus = "$($vmms.Status)"
  termServiceStatus = "$($term.Status)"
})
`
	var status EnhancedSessionStatus
	if err := c.run(ctx, body, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// SetEnhancedSession 开启或关闭增强会话主机策略。
func (c *Client) SetEnhancedSession(ctx context.Context, enabled bool) error {
	body := `
$installationType = "$((Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion' -ErrorAction SilentlyContinue).InstallationType)"
if ($installationType -eq 'Server Core') { throw '当前系统是 Server Core，原生不支持增强会话' }
$regPath = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Virtualization'
if (-not (Test-Path $regPath)) { New-Item -Path $regPath -Force | Out-Null }
if ([bool]$opts.enabled) {
  Set-VMHost -EnableEnhancedSessionMode $true -ErrorAction Stop
  Set-ItemProperty -Path $regPath -Name EnableEnhancedSessionMode -Value 1 -Type DWord -ErrorAction Stop
  Set-Service vmms -StartupType Automatic -ErrorAction SilentlyContinue
  Start-Service vmms -ErrorAction SilentlyContinue
  Set-Service TermService -StartupType Automatic -ErrorAction SilentlyContinue
  Start-Service TermService -ErrorAction SilentlyContinue
} else {
  Set-VMHost -EnableEnhancedSessionMode $false -ErrorAction Stop
  Set-ItemProperty -Path $regPath -Name EnableEnhancedSessionMode -Value 0 -Type DWord -ErrorAction Stop
}
Write-HvResult ([ordered]@{ enabled = [bool]$opts.enabled })
`
	return c.run(ctx, body, map[string]any{"enabled": enabled}, nil)
}

// ListSwitches 返回所有虚拟交换机。
func (c *Client) ListSwitches(ctx context.Context) ([]VirtualSwitch, error) {
	body := `
$items = New-Object System.Collections.ArrayList
foreach ($s in @(Get-VMSwitch | Sort-Object Name)) {
  [void]$items.Add([ordered]@{
    name = "$($s.Name)"
    id = "$($s.Id)"
    switchType = "$($s.SwitchType)"
    netAdapterInterfaceDescription = "$($s.NetAdapterInterfaceDescription)"
    allowManagementOS = [bool]$s.AllowManagementOS
    status = "$($s.Status)"
  })
}
Write-HvResult @($items)
`
	var switches []VirtualSwitch
	if err := c.run(ctx, body, nil, &switches); err != nil {
		return nil, err
	}
	if switches == nil {
		switches = []VirtualSwitch{}
	}
	return switches, nil
}

// ListHostNetworkAdapters 返回可用于外部交换机的物理网卡。
func (c *Client) ListHostNetworkAdapters(ctx context.Context) ([]HostNetworkAdapter, error) {
	body := `
$items = New-Object System.Collections.ArrayList
foreach ($a in @(Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Sort-Object Name)) {
  [void]$items.Add([ordered]@{
    name = "$($a.Name)"
    description = "$($a.InterfaceDescription)"
    macAddress = "$($a.MacAddress)"
    linkSpeed = "$($a.LinkSpeed)"
    status = "$($a.Status)"
  })
}
Write-HvResult @($items)
`
	var adapters []HostNetworkAdapter
	if err := c.run(ctx, body, nil, &adapters); err != nil {
		return nil, err
	}
	if adapters == nil {
		adapters = []HostNetworkAdapter{}
	}
	return adapters, nil
}

// CreateSwitch 创建虚拟交换机。
func (c *Client) CreateSwitch(ctx context.Context, req CreateSwitchRequest) error {
	if err := ValidateName(req.Name); err != nil {
		return fmt.Errorf("交换机名称无效: %w", err)
	}
	body := `
if (Get-VMSwitch -Name $opts.name -ErrorAction SilentlyContinue) { throw "已存在同名虚拟交换机 '$($opts.name)'" }
switch ("$($opts.switchType)") {
  'External' {
    if (-not $opts.netAdapterName) { throw '创建外部交换机需要选择物理网卡' }
    New-VMSwitch -Name $opts.name -NetAdapterName $opts.netAdapterName -AllowManagementOS ([bool]$opts.allowManagementOS) -ErrorAction Stop | Out-Null
  }
  'Internal' { New-VMSwitch -Name $opts.name -SwitchType Internal -ErrorAction Stop | Out-Null }
  'Private'  { New-VMSwitch -Name $opts.name -SwitchType Private -ErrorAction Stop | Out-Null }
  default    { throw "未知的交换机类型: $($opts.switchType)" }
}
Write-HvResult ([ordered]@{ name = "$($opts.name)" })
`
	return c.run(ctx, body, req, nil)
}

// RemoveSwitch 删除虚拟交换机；仍有虚拟机网卡连接时拒绝删除。
func (c *Client) RemoveSwitch(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("交换机名称不能为空")
	}
	body := `
$sw = Get-VMSwitch -Name $opts.name -ErrorAction Stop
$connected = @(Get-VMNetworkAdapter -All -ErrorAction SilentlyContinue | Where-Object { $_.SwitchName -eq $opts.name })
if ($connected.Count -gt 0) {
  throw "交换机 '$($opts.name)' 仍有 $($connected.Count) 个虚拟机网卡连接，请先断开连接"
}
Remove-VMSwitch -Name $opts.name -Force -ErrorAction Stop
Write-HvResult ([ordered]@{ name = "$($opts.name)" })
`
	return c.run(ctx, body, map[string]any{"name": name}, nil)
}
