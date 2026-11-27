package hyperv

import (
	"context"
	"fmt"
	"strings"
)

// vmDTOScript 是列表中每个虚拟机对象的统一序列化逻辑，供列表与详情复用。
const vmDTOScript = `
function New-VmDto($vm) {
  $proc = Get-VMProcessor -VMName $vm.Name -ErrorAction SilentlyContinue | Select-Object -First 1
  $mem = Get-VMMemory -VMName $vm.Name -ErrorAction SilentlyContinue | Select-Object -First 1
  $firmware = $null
  try { $firmware = Get-VMFirmware -VMName $vm.Name -ErrorAction Stop } catch { }
  $security = $null
  try { $security = Get-VMSecurity -VMName $vm.Name -ErrorAction Stop } catch { }
  $integration = @()
  foreach ($svc in @(Get-VMIntegrationService -VMName $vm.Name -ErrorAction SilentlyContinue | Sort-Object Name)) {
    $integration += [ordered]@{
      name = "$($svc.Name)"
      displayName = "$($svc.DisplayName)"
      enabled = [bool]$svc.Enabled
    }
  }
  $adapters = @()
  foreach ($a in @(Get-VMNetworkAdapter -VMName $vm.Name -ErrorAction SilentlyContinue)) {
    $vlan = Get-VMNetworkAdapterVlan -VMNetworkAdapter $a -ErrorAction SilentlyContinue | Select-Object -First 1
    $adapters += [ordered]@{
      id = "$($a.Id)"
      name = "$($a.Name)"
      switchName = "$($a.SwitchName)"
      macAddress = "$($a.MacAddress)"
      dynamicMacAddress = [bool]$a.DynamicMacAddress
      macAddressSpoofing = "$($a.MacAddressSpoofing)"
      dhcpGuard = "$($a.DhcpGuard)"
      routerGuard = "$($a.RouterGuard)"
      vlanMode = "$($vlan.OperationMode)"
      vlanID = if ($vlan -and $vlan.AccessVlanId) { [int]$vlan.AccessVlanId } else { 0 }
      deviceNaming = "$($a.DeviceNaming)"
      vmqWeight = [int]$a.VmqWeight
      iovWeight = [int]$a.IovWeight
      ipsecOffload = [bool]$a.IPsecOffloadMaximumSecurityAssociation
      stormLimit = [int]$a.StormLimit
      maximumBandwidthBps = [int64]$a.MaximumBandwidth
      status = "$($a.Status)"
      ipAddresses = @($a.IPAddresses | ForEach-Object { "$_" })
    }
  }
  $disks = @()
  foreach ($d in @(Get-VMHardDiskDrive -VMName $vm.Name -ErrorAction SilentlyContinue)) {
    $vhd = Get-VHD -Path $d.Path -ErrorAction SilentlyContinue
    $disks += [ordered]@{
      path = "$($d.Path)"
      controllerType = "$($d.ControllerType)"
      controllerNumber = [int]$d.ControllerNumber
      controllerLocation = [int]$d.ControllerLocation
      virtualSizeBytes = if ($vhd) { [int64]$vhd.Size } else { 0 }
      fileSizeBytes = if ($vhd) { [int64]$vhd.FileSize } else { 0 }
      minimumSizeBytes = if ($vhd) { [int64]$vhd.MinimumSize } else { 0 }
      vhdType = if ($vhd) { "$($vhd.VhdType)" } else { "" }
      parentPath = if ($vhd) { "$($vhd.ParentPath)" } else { "" }
    }
  }
  $dvds = @()
  foreach ($d in @(Get-VMDvdDrive -VMName $vm.Name -ErrorAction SilentlyContinue)) {
    $dvds += [ordered]@{
      path = "$($d.Path)"
      controllerNumber = [int]$d.ControllerNumber
      controllerLocation = [int]$d.ControllerLocation
    }
  }
  [ordered]@{
    name = "$($vm.Name)"
    vmId = "$($vm.VMId)"
    state = "$($vm.State)"
    generation = [int]$vm.Generation
    processorCount = [int]$vm.ProcessorCount
    nestedVirtualization = [bool]($proc.ExposeVirtualizationExtensions)
    notes = "$($vm.Notes)"
    automaticStartAction = "$($vm.AutomaticStartAction)"
    automaticStartDelay = [int]$vm.AutomaticStartDelay
    automaticStopAction = "$($vm.AutomaticStopAction)"
    automaticCheckpoints = [bool]$vm.AutomaticCheckpointsEnabled
    processorReserve = [int]$proc.Reserve
    processorMaximum = [int]$proc.Maximum
    processorWeight = [int]$proc.RelativeWeight
    processorMigration = [bool]$proc.CompatibilityForMigrationEnabled
    hwThreadsPerCore = [int]$proc.HwThreadCountPerCore
    memoryBuffer = [int]$mem.Buffer
    memoryPriority = [int]$mem.Priority
    secureBoot = if ($firmware) { "$($firmware.SecureBoot)" } else { "" }
    secureBootTemplate = if ($firmware) { "$($firmware.SecureBootTemplate)" } else { "" }
    networkBootProtocol = if ($firmware) { "$($firmware.PreferredNetworkBootProtocol)" } else { "" }
    tpmEnabled = if ($security) { [bool]$security.TpmEnabled } else { $false }
    shielded = if ($security) { [bool]$security.Shielded } else { $false }
    encryptTraffic = if ($security) { [bool]$security.EncryptStateAndVmMigrationTraffic } else { $false }
    vbsOptOut = if ($security) { [bool]$security.VirtualizationBasedSecurityOptOut } else { $false }
    integrationServices = @($integration)
    memoryStartupMB = [int64]($vm.MemoryStartup / 1MB)
    memoryAssignedMB = [int64]($vm.MemoryAssigned / 1MB)
    dynamicMemoryEnabled = [bool]$vm.DynamicMemoryEnabled
    memoryMinimumMB = [int64]($vm.MemoryMinimum / 1MB)
    memoryMaximumMB = [int64]($vm.MemoryMaximum / 1MB)
    uptimeSeconds = [int64]$vm.Uptime.TotalSeconds
    path = "$($vm.Path)"
    version = "$($vm.Version)"
    checkpointType = "$($vm.CheckpointType)"
    status = "$($vm.Status)"
    networks = $adapters
    hardDrives = $disks
    dvdDrives = $dvds
  }
}
`

// vmSummaryScript 是列表中使用的轻量查询：只调用一次 Get-VM 和一次
// Get-VMNetworkAdapter -All，不逐台查询磁盘、DVD、处理器等重字段。
// 磁盘与嵌套虚拟化详情在打开详情/编辑时由 GetVM 再补全。
const vmSummaryScript = `
$networkByVm = @{}
foreach ($adapter in @(Get-VMNetworkAdapter -All -ErrorAction SilentlyContinue)) {
  $vmName = "$($adapter.VMName)"
  if (-not $vmName) { continue }
  if (-not $networkByVm.ContainsKey($vmName)) {
    $networkByVm[$vmName] = New-Object System.Collections.ArrayList
  }
  [void]$networkByVm[$vmName].Add([ordered]@{
    name = "$($adapter.Name)"
    switchName = "$($adapter.SwitchName)"
    macAddress = "$($adapter.MacAddress)"
    status = "$($adapter.Status)"
    ipAddresses = @($adapter.IPAddresses | ForEach-Object { "$_" })
  })
}
$items = New-Object System.Collections.ArrayList
foreach ($vm in @(Get-VM | Sort-Object Name)) {
  $networks = @()
  if ($networkByVm.ContainsKey($vm.Name)) { $networks = @($networkByVm[$vm.Name]) }
  [void]$items.Add([ordered]@{
    name = "$($vm.Name)"
    vmId = "$($vm.VMId)"
    state = "$($vm.State)"
    generation = [int]$vm.Generation
    processorCount = [int]$vm.ProcessorCount
    nestedVirtualization = $false
    memoryStartupMB = [int64]($vm.MemoryStartup / 1MB)
    memoryAssignedMB = [int64]($vm.MemoryAssigned / 1MB)
    dynamicMemoryEnabled = [bool]$vm.DynamicMemoryEnabled
    memoryMinimumMB = [int64]($vm.MemoryMinimum / 1MB)
    memoryMaximumMB = [int64]($vm.MemoryMaximum / 1MB)
    uptimeSeconds = [int64]$vm.Uptime.TotalSeconds
    path = "$($vm.Path)"
    version = "$($vm.Version)"
    checkpointType = "$($vm.CheckpointType)"
    status = "$($vm.Status)"
    networks = $networks
    hardDrives = @()
    dvdDrives = @()
  })
}
Write-HvResult @($items)
`

// ListVMSummaries 返回轻量虚拟机列表，供虚拟机页面自动刷新使用。
func (c *Client) ListVMSummaries(ctx context.Context) ([]VM, error) {
	var vms []VM
	if err := c.run(ctx, vmSummaryScript, nil, &vms); err != nil {
		return nil, err
	}
	if vms == nil {
		vms = []VM{}
	}
	return vms, nil
}

// ListVMs 返回主机上所有虚拟机及其网络/磁盘信息。
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	body := vmDTOScript + `
$items = New-Object System.Collections.ArrayList
foreach ($vm in @(Get-VM | Sort-Object Name)) {
  [void]$items.Add((New-VmDto $vm))
}
Write-HvResult @($items)
`
	var vms []VM
	if err := c.run(ctx, body, nil, &vms); err != nil {
		return nil, err
	}
	if vms == nil {
		vms = []VM{}
	}
	return vms, nil
}

// GetVM 返回单个虚拟机的详细信息。
func (c *Client) GetVM(ctx context.Context, name string) (*VM, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("虚拟机名称不能为空")
	}
	body := vmDTOScript + `
$vm = Get-VM -Name $opts.name -ErrorAction Stop
Write-HvResult (New-VmDto $vm)
`
	var vm VM
	if err := c.run(ctx, body, map[string]any{"name": name}, &vm); err != nil {
		return nil, err
	}
	return &vm, nil
}

const powerScript = `
$vm = Get-VM -Name $opts.name -ErrorAction Stop
switch ("$($opts.action)") {
  'start'   { Start-VM -VM $vm -ErrorAction Stop }
  'stop'    { Stop-VM -VM $vm -Force -ErrorAction Stop }
  'turnoff' { Stop-VM -VM $vm -Force -TurnOff -ErrorAction Stop }
  'save'    { Save-VM -VM $vm -ErrorAction Stop }
  'pause'   { Suspend-VM -VM $vm -ErrorAction Stop }
  'resume'  { Resume-VM -VM $vm -ErrorAction Stop }
  'restart' { Restart-VM -VM $vm -ErrorAction Stop }
  default   { throw "未知的电源操作: $($opts.action)" }
}
Write-HvResult ([ordered]@{ name = "$($vm.Name)"; action = "$($opts.action)" })
`

// Power 执行电源操作。action 取值见 PowerAction 常量。
func (c *Client) Power(ctx context.Context, name string, action PowerAction) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("虚拟机名称不能为空")
	}
	return c.run(ctx, powerScript, map[string]any{"name": name, "action": string(action)}, nil)
}

// PowerActionLabel 返回电源操作的中文描述，用于任务日志。
func PowerActionLabel(action PowerAction) string {
	switch action {
	case PowerStart:
		return "启动"
	case PowerStop:
		return "关机"
	case PowerTurnOff:
		return "强制断电"
	case PowerSave:
		return "保存状态"
	case PowerPause:
		return "暂停"
	case PowerResume:
		return "恢复"
	case PowerRestart:
		return "重启"
	default:
		return string(action)
	}
}

// DiskSpaces 返回主机上的固定磁盘容量。
func (c *Client) DiskSpaces(ctx context.Context) ([]DiskSpace, error) {
	body := `
$items = New-Object System.Collections.ArrayList
foreach ($d in @(Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" -ErrorAction SilentlyContinue | Sort-Object DeviceID)) {
  [void]$items.Add([ordered]@{
    drive = "$($d.DeviceID)"
    totalBytes = [int64]$d.Size
    freeBytes = [int64]$d.FreeSpace
  })
}
Write-HvResult @($items)
`
	var disks []DiskSpace
	if err := c.run(ctx, body, nil, &disks); err != nil {
		return nil, err
	}
	if disks == nil {
		disks = []DiskSpace{}
	}
	return disks, nil
}
