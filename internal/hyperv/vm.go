package hyperv

import (
	"context"
	"fmt"
	"strings"
)

// vmDTOScript 是列表中每个虚拟机对象的统一序列化逻辑，供列表与详情复用。
const vmDTOScript = `
function New-VmDto($vm) {
  $adapters = @()
  foreach ($a in @(Get-VMNetworkAdapter -VMName $vm.Name -ErrorAction SilentlyContinue)) {
    $adapters += [ordered]@{
      name = "$($a.Name)"
      switchName = "$($a.SwitchName)"
      macAddress = "$($a.MacAddress)"
      status = "$($a.Status)"
      ipAddresses = @($a.IPAddresses | ForEach-Object { "$_" })
    }
  }
  $disks = @()
  foreach ($d in @(Get-VMHardDiskDrive -VMName $vm.Name -ErrorAction SilentlyContinue)) {
    $disks += [ordered]@{
      path = "$($d.Path)"
      controllerType = "$($d.ControllerType)"
      controllerNumber = [int]$d.ControllerNumber
      controllerLocation = [int]$d.ControllerLocation
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
