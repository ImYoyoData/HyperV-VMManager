package hyperv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// natScriptHelpers 提供 IP 前缀判断，供 NAT 脚本复用。
const natScriptHelpers = `
function Test-HvIpInPrefix($ip, $prefix) {
  if ($prefix -notmatch '^(.+)/(\d+)$') { return $false }
  $network = $matches[1]
  $length = [int]$matches[2]
  try {
    $ipBytes = [System.Net.IPAddress]::Parse($ip).GetAddressBytes()
    $netBytes = [System.Net.IPAddress]::Parse($network).GetAddressBytes()
  } catch { return $false }
  if ($ipBytes.Length -ne 4 -or $netBytes.Length -ne 4) { return $false }
  $full = [int][math]::Floor($length / 8)
  $rem = $length % 8
  for ($i = 0; $i -lt $full; $i++) {
    if ($ipBytes[$i] -ne $netBytes[$i]) { return $false }
  }
  if ($rem -gt 0) {
    $mask = [byte]((0xFF -shl (8 - $rem)) -band 0xFF)
    if (($ipBytes[$full] -band $mask) -ne ($netBytes[$full] -band $mask)) { return $false }
  }
  return $true
}
`

const listNATScript = `
$lanIP = ""
try {
  $cfg = Get-NetIPConfiguration -ErrorAction SilentlyContinue | Where-Object {
    $_.IPv4DefaultGateway -ne $null -and $_.InterfaceAlias -notlike 'vEthernet*' -and $_.NetAdapter -and $_.NetAdapter.Status -eq 'Up'
  } | Select-Object -First 1
  if ($cfg) {
    $addr = @($cfg.IPv4Address) | Select-Object -First 1
    if ($addr) { $lanIP = "$($addr.IPAddress)" }
  }
} catch { }
$proxyRules = @()
foreach ($line in @(netsh interface portproxy show v4tov4 2>$null)) {
  if ($line -match '^\s*(\d{1,3}(?:\.\d{1,3}){3})\s+(\d+)\s+(\d{1,3}(?:\.\d{1,3}){3})\s+(\d+)\s*$') {
    $proxyRules += [pscustomobject]@{
      externalIPAddress = "$($matches[1])"
      externalPort = [int]$matches[2]
      internalIPAddress = "$($matches[3])"
      internalPort = [int]$matches[4]
    }
  }
}
$items = New-Object System.Collections.ArrayList
foreach ($nat in @(Get-NetNat -ErrorAction SilentlyContinue | Sort-Object Name)) {
  $prefix = "$($nat.InternalIPInterfaceAddressPrefix)"
  $hostIP = ""
  $switchName = ""
  foreach ($ip in @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue)) {
    if (-not (Test-HvIpInPrefix "$($ip.IPAddress)" $prefix)) { continue }
    $hostIP = "$($ip.IPAddress)"
    $alias = "$($ip.InterfaceAlias)"
    if ($alias -match '^vEthernet \((.+)\)$') { $switchName = $matches[1] }
    break
  }
  $mappings = @()
  foreach ($rule in @($proxyRules)) {
    if (-not (Test-HvIpInPrefix $rule.internalIPAddress $prefix)) { continue }
    $mappings += [ordered]@{
      protocol = "TCP"
      externalIPAddress = "$($rule.externalIPAddress)"
      externalPort = [int]$rule.externalPort
      internalIPAddress = "$($rule.internalIPAddress)"
      internalPort = [int]$rule.internalPort
    }
  }
  [void]$items.Add([ordered]@{
    name = "$($nat.Name)"
    prefix = $prefix
    hostIP = $hostIP
    externalIP = $lanIP
    switchName = $switchName
    active = [bool]$nat.Active
    staticMappings = @($mappings)
  })
}
Write-HvResult @($items)
`

const createNATScript = `
$name = "$($opts.name)"
$switchName = "$($opts.switchName)"
$hostIP = "$($opts.hostIP)"
$prefixLength = [int]$opts.prefixLength
$switch = Get-VMSwitch -Name $switchName -ErrorAction SilentlyContinue
if ($switch) {
  if ("$($switch.SwitchType)" -ne 'Internal') {
    throw "交换机 $switchName 已存在，但类型是 $($switch.SwitchType)，不是 Internal。请换一个内部交换机名称，避免修改外部交换机的网络配置。"
  }
} else {
  New-VMSwitch -Name $switchName -SwitchType Internal -ErrorAction Stop | Out-Null
  Start-Sleep -Milliseconds 1200
}
$adapter = Get-NetAdapter -Name "vEthernet ($switchName)" -ErrorAction SilentlyContinue
if (-not $adapter) {
  $adapter = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Name -like "vEthernet ($switchName)*" } | Select-Object -First 1
}
if (-not $adapter) { throw "找不到内部交换机的主机虚拟网卡 vEthernet ($switchName)" }
$hasIP = @(Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.IPAddress -eq $hostIP -and [int]$_.PrefixLength -eq $prefixLength })
if ($hasIP.Count -eq 0) {
  foreach ($old in @(Get-NetIPAddress -InterfaceIndex $adapter.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue)) {
    Remove-NetIPAddress -InterfaceIndex $adapter.ifIndex -IPAddress $old.IPAddress -PrefixLength $old.PrefixLength -Confirm:$false -ErrorAction SilentlyContinue
  }
  New-NetIPAddress -InterfaceIndex $adapter.ifIndex -IPAddress $hostIP -PrefixLength $prefixLength -ErrorAction Stop | Out-Null
}
$bytes = [System.Net.IPAddress]::Parse($hostIP).GetAddressBytes()
$full = [int][math]::Floor($prefixLength / 8)
$rem = $prefixLength % 8
if ($rem -gt 0) {
  $mask = [byte](0xFF -shl (8 - $rem))
  $bytes[$full] = [byte]($bytes[$full] -band $mask)
  for ($i = $full + 1; $i -lt 4; $i++) { $bytes[$i] = 0 }
} else {
  for ($i = $full; $i -lt 4; $i++) { $bytes[$i] = 0 }
}
$network = ([System.Net.IPAddress]::new([byte[]]$bytes)).ToString()
$prefix = "$network/$prefixLength"
if (Get-NetNat -Name $name -ErrorAction SilentlyContinue) {
  Set-NetNat -Name $name -InternalIPInterfaceAddressPrefix $prefix -Confirm:$false -ErrorAction Stop
} else {
  New-NetNat -Name $name -InternalIPInterfaceAddressPrefix $prefix -ErrorAction Stop | Out-Null
}
Write-HvResult ([ordered]@{ name = $name; prefix = $prefix; switchName = $switchName; hostIP = $hostIP })
`

const removeNATScript = `
$name = "$($opts.name)"
if (-not (Get-NetNat -Name $name -ErrorAction SilentlyContinue)) { throw "NAT 网络不存在: $name" }
Remove-NetNat -Name $name -Confirm:$false -ErrorAction Stop
Write-HvResult ([ordered]@{ name = $name })
`

const addNATMappingScript = `
$natName = "$($opts.natName)"
$externalPort = [int]$opts.externalPort
$internalIP = "$($opts.internalIPAddress)"
$internalPort = [int]$opts.internalPort
$nat = Get-NetNat -Name $natName -ErrorAction Stop
$prefix = "$($nat.InternalIPInterfaceAddressPrefix)"
if (-not (Test-HvIpInPrefix $internalIP $prefix)) { throw "内部 IP $internalIP 不在 NAT 前缀 $prefix 内" }
try { Start-Service iphlpsvc -ErrorAction SilentlyContinue } catch { }
& netsh interface portproxy delete v4tov4 "listenaddress=0.0.0.0" "listenport=$externalPort" 2>$null | Out-Null
$output = & netsh interface portproxy add v4tov4 "listenaddress=0.0.0.0" "listenport=$externalPort" "connectaddress=$internalIP" "connectport=$internalPort" 2>&1
if ($LASTEXITCODE -ne 0) { throw (($output | ForEach-Object { "$_" }) -join [Environment]::NewLine) }
Write-HvResult ([ordered]@{ natName = $natName; protocol = "TCP"; externalPort = $externalPort; internalIPAddress = $internalIP; internalPort = $internalPort })
`

const removeNATMappingScript = `
$externalPort = [int]$opts.externalPort
$output = & netsh interface portproxy delete v4tov4 "listenaddress=0.0.0.0" "listenport=$externalPort" 2>&1
if ($LASTEXITCODE -ne 0) { throw (($output | ForEach-Object { "$_" }) -join [Environment]::NewLine) }
Write-HvResult ([ordered]@{ natName = "$($opts.natName)"; protocol = "TCP"; externalPort = $externalPort })
`

// ListNATNetworks 返回主机上的 Windows NAT 实例、主机虚拟网卡地址和端口转发规则。
func (c *Client) ListNATNetworks(ctx context.Context) ([]NATNetwork, error) {
	var items []NATNetwork
	if err := c.run(ctx, natScriptHelpers+listNATScript, nil, &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []NATNetwork{}
	}
	return items, nil
}

// CreateNATNetwork 创建（或更新）内部交换机 + 主机 IP + Windows NAT。
func (c *Client) CreateNATNetwork(ctx context.Context, req CreateNATRequest) error {
	if err := validateCreateNATRequest(&req); err != nil {
		return err
	}
	return c.run(ctx, natScriptHelpers+createNATScript, req, nil)
}

// RemoveNATNetwork 删除 NAT 实例，保留内部交换机和主机 IP。
func (c *Client) RemoveNATNetwork(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("NAT 名称不能为空")
	}
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("NAT 名称无效: %w", err)
	}
	return c.run(ctx, natScriptHelpers+removeNATScript, map[string]any{"name": name}, nil)
}

// AddNATStaticMapping 新增或覆盖一条与 NAT 网络关联的 TCP 端口转发。
// 使用 Windows portproxy 实现，保证本机 frpc 也能通过 127.0.0.1 访问。
func (c *Client) AddNATStaticMapping(ctx context.Context, req NATStaticMappingRequest) error {
	if err := validateNATMappingRequest(&req); err != nil {
		return err
	}
	return c.run(ctx, natScriptHelpers+addNATMappingScript, req, nil)
}

// RemoveNATStaticMapping 删除一条 NAT 端口转发。
func (c *Client) RemoveNATStaticMapping(ctx context.Context, req RemoveNATStaticMappingRequest) error {
	req.NATName = strings.TrimSpace(req.NATName)
	req.Protocol = strings.ToUpper(strings.TrimSpace(req.Protocol))
	if req.NATName == "" {
		return errors.New("NAT 名称不能为空")
	}
	if req.Protocol != "TCP" {
		return errors.New("当前端口转发使用 Windows portproxy，只支持 TCP")
	}
	if req.ExternalPort < 1 || req.ExternalPort > 65535 {
		return errors.New("外部端口必须在 1 到 65535 之间")
	}
	return c.run(ctx, natScriptHelpers+removeNATMappingScript, req, nil)
}

func validateCreateNATRequest(req *CreateNATRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.SwitchName = strings.TrimSpace(req.SwitchName)
	req.HostIP = strings.TrimSpace(req.HostIP)
	if req.Name == "" {
		return errors.New("NAT 名称不能为空")
	}
	if err := ValidateName(req.Name); err != nil {
		return fmt.Errorf("NAT 名称无效: %w", err)
	}
	if req.SwitchName == "" {
		req.SwitchName = req.Name
	}
	if err := ValidateName(req.SwitchName); err != nil {
		return fmt.Errorf("内部交换机名称无效: %w", err)
	}
	ip := net.ParseIP(req.HostIP).To4()
	if ip == nil {
		return errors.New("主机 IP 必须是 IPv4 地址，例如 192.168.100.1")
	}
	if req.PrefixLength < 8 || req.PrefixLength > 30 {
		return errors.New("前缀长度必须在 8 到 30 之间")
	}
	mask := net.CIDRMask(req.PrefixLength, 32)
	network := ip.Mask(mask)
	if ip.Equal(network) {
		return errors.New("主机 IP 不能是网段地址，例如 192.168.100.0")
	}
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = network[i] | ^mask[i]
	}
	if ip.Equal(broadcast) {
		return errors.New("主机 IP 不能是广播地址，例如 192.168.100.255")
	}
	return nil
}

func validateNATMappingRequest(req *NATStaticMappingRequest) error {
	req.NATName = strings.TrimSpace(req.NATName)
	req.Protocol = strings.ToUpper(strings.TrimSpace(req.Protocol))
	req.InternalIPAddress = strings.TrimSpace(req.InternalIPAddress)
	if req.NATName == "" {
		return errors.New("NAT 名称不能为空")
	}
	if req.Protocol != "TCP" {
		return errors.New("当前端口转发使用 Windows portproxy，只支持 TCP")
	}
	if req.ExternalPort < 1 || req.ExternalPort > 65535 {
		return errors.New("外部端口必须在 1 到 65535 之间")
	}
	if req.InternalPort < 1 || req.InternalPort > 65535 {
		return errors.New("内部端口必须在 1 到 65535 之间")
	}
	if net.ParseIP(req.InternalIPAddress).To4() == nil {
		return errors.New("虚拟机内部 IP 必须是 IPv4 地址")
	}
	return nil
}
