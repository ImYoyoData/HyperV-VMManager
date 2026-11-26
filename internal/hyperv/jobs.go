package hyperv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func report(progress ProgressFunc, percent int, step string) {
	if progress != nil {
		progress(percent, step)
	}
}

func randomSuffix() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

const applySettingsScript = `
$vm = Get-VM -Name $opts.name -ErrorAction Stop
$mem = $opts.memory
if ([bool]$mem.dynamic) {
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $true -StartupBytes ([int64]$mem.startupMB * 1MB) -MinimumBytes ([int64]$mem.minimumMB * 1MB) -MaximumBytes ([int64]$mem.maximumMB * 1MB) -ErrorAction Stop
} else {
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes ([int64]$mem.startupMB * 1MB) -ErrorAction Stop
}
Set-VMProcessor -VM $vm -Count ([int]$opts.cpuCount) -ErrorAction Stop
$adapters = @(Get-VMNetworkAdapter -VMName $opts.name)
if ($opts.switchName) {
  if ($adapters.Count -eq 0) { throw '虚拟机没有网络适配器，无法设置虚拟交换机' }
  $adapters | Connect-VMNetworkAdapter -SwitchName $opts.switchName -ErrorAction Stop
}
if ([bool]$opts.resetMacAddress) {
  $adapters | Set-VMNetworkAdapter -DynamicMacAddress -ErrorAction SilentlyContinue
}
foreach ($a in $adapters) {
  Set-VMNetworkAdapter -VMNetworkAdapter $a -MaximumBandwidth ([int64]$opts.bandwidthMbps * 1000000) -ErrorAction Stop
}
Write-HvResult ([ordered]@{ name = "$($vm.Name)"; applied = $true })
`

const cloneExportScript = `
$src = Get-VM -Name $opts.sourceName -ErrorAction Stop
if (Get-VM -Name $opts.newName -ErrorAction SilentlyContinue) { throw "已存在同名虚拟机 '$($opts.newName)'" }
$targetDir = Join-Path $opts.targetDirectory $opts.newName
if (Test-Path -LiteralPath $targetDir) { throw "目标目录已存在：$targetDir" }
if ($src.State -ne 'Off') {
  if (-not [bool]$opts.stopSourceIfRunning) {
    throw "源虚拟机 '$($opts.sourceName)' 正在运行（状态=$($src.State)），请先关机，或允许自动关机"
  }
  Stop-VM -VM $src -Force -ErrorAction Stop
  $deadline = (Get-Date).AddSeconds(180)
  while ((Get-VM -Name $opts.sourceName).State -ne 'Off') {
    if ((Get-Date) -gt $deadline) { throw '等待源虚拟机关机超时（180 秒）' }
    Start-Sleep -Seconds 1
  }
}
$exportDir = $opts.exportDirectory
if (Test-Path -LiteralPath $exportDir) { Remove-Item -LiteralPath $exportDir -Recurse -Force -ErrorAction SilentlyContinue }
New-Item -ItemType Directory -Path $exportDir -Force | Out-Null
Export-VM -VM $src -Path $exportDir -ErrorAction Stop
$vmcx = Get-ChildItem -Path $exportDir -Recurse -Filter *.vmcx | Select-Object -First 1
if (-not $vmcx) { throw '导出失败：未找到 .vmcx 文件' }
Write-HvResult ([ordered]@{ vmcx = $vmcx.FullName })
`

const cloneImportScript = `
$targetDir = Join-Path $opts.targetDirectory $opts.newName
$vhdDir = Join-Path $targetDir 'Virtual Hard Disks'
New-Item -ItemType Directory -Path $vhdDir -Force | Out-Null
$vm = Import-VM -Path $opts.vmcx -Copy -GenerateNewId -VirtualMachinePath $targetDir -VhdDestinationPath $vhdDir -ErrorAction Stop
Rename-VM -VM $vm -NewName $opts.newName -ErrorAction Stop
$vm = Get-VM -Name $opts.newName -ErrorAction Stop
Write-HvResult ([ordered]@{ name = "$($vm.Name)"; path = "$($vm.Path)" })
`

// Clone 通过导出再导入的方式克隆虚拟机。
// 进度拆分为：校验 -> 导出 -> 导入 -> 应用配置 -> 完成。
func (c *Client) Clone(ctx context.Context, req CloneRequest, progress ProgressFunc) error {
	req.SourceName = strings.TrimSpace(req.SourceName)
	req.NewName = strings.TrimSpace(req.NewName)
	req.TargetDirectory = strings.TrimSpace(req.TargetDirectory)
	if req.SourceName == "" {
		return errors.New("源虚拟机不能为空")
	}
	if err := ValidateName(req.NewName); err != nil {
		return fmt.Errorf("新名称无效: %w", err)
	}
	if strings.EqualFold(req.SourceName, req.NewName) {
		return errors.New("新名称不能与源虚拟机名称相同")
	}
	if err := ValidateAbsoluteDirectory(req.TargetDirectory); err != nil {
		return err
	}
	if err := ValidateMemory(req.Memory); err != nil {
		return err
	}
	if err := ValidateCPU(req.CPUCount); err != nil {
		return err
	}
	if err := ValidateBandwidth(req.BandwidthMbps); err != nil {
		return err
	}

	report(progress, 3, "校验参数")
	exportDir := filepath.Join(os.TempDir(), "HyperV-VMManager", "clone-"+randomSuffix())
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return fmt.Errorf("无法创建临时导出目录: %w", err)
	}
	defer os.RemoveAll(exportDir)

	report(progress, 8, "检查源虚拟机状态并导出")
	exportOpts := map[string]any{
		"sourceName":          req.SourceName,
		"newName":             req.NewName,
		"targetDirectory":     req.TargetDirectory,
		"exportDirectory":     exportDir,
		"stopSourceIfRunning": req.StopSourceIfRunning,
	}
	var exported struct {
		VMCX string `json:"vmcx"`
	}
	if err := c.run(ctx, cloneExportScript, exportOpts, &exported); err != nil {
		return fmt.Errorf("导出源虚拟机失败: %w", err)
	}
	if exported.VMCX == "" {
		return errors.New("导出源虚拟机失败：未找到 .vmcx 文件")
	}

	report(progress, 55, "导入新虚拟机并重命名")
	importOpts := map[string]any{
		"newName":         req.NewName,
		"targetDirectory": req.TargetDirectory,
		"vmcx":            exported.VMCX,
	}
	var imported struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := c.run(ctx, cloneImportScript, importOpts, &imported); err != nil {
		return fmt.Errorf("导入新虚拟机失败: %w", err)
	}

	report(progress, 88, "应用内存 / CPU / 网络 / 带宽")
	applyOpts := map[string]any{
		"name":            req.NewName,
		"memory":          req.Memory,
		"cpuCount":        req.CPUCount,
		"switchName":      req.SwitchName,
		"bandwidthMbps":   req.BandwidthMbps,
		"resetMacAddress": true,
	}
	if err := c.run(ctx, applySettingsScript, applyOpts, nil); err != nil {
		return fmt.Errorf("新虚拟机已创建，但应用配置失败: %w", err)
	}

	report(progress, 100, "克隆完成")
	return nil
}

const createDiskScript = `
$vmDir = Join-Path $opts.directory $opts.name
if (Get-VM -Name $opts.name -ErrorAction SilentlyContinue) { throw "已存在同名虚拟机 '$($opts.name)'" }
if (Test-Path -LiteralPath $vmDir) { throw "目标目录已存在：$vmDir" }
$vhdDir = Join-Path $vmDir 'Virtual Hard Disks'
New-Item -ItemType Directory -Path $vhdDir -Force | Out-Null
$ext = '.vhdx'
if ("$($opts.diskMode)" -eq 'differencing' -and $opts.imagePath -and ([IO.Path]::GetExtension("$($opts.imagePath)") -eq '.vhd')) { $ext = '.vhd' }
$vhd = Join-Path $vhdDir ($opts.name + $ext)
switch ("$($opts.diskMode)") {
  'differencing' {
    if (-not $opts.imagePath) { throw '差异磁盘需要选择父盘镜像' }
    if (-not (Test-Path -LiteralPath $opts.imagePath)) { throw "父盘不存在：$($opts.imagePath)" }
    New-VHD -ParentPath $opts.imagePath -Path $vhd -Differencing -ErrorAction Stop | Out-Null
  }
  'copy' {
    if (-not $opts.imagePath) { throw '完整复制需要选择镜像' }
    if (-not (Test-Path -LiteralPath $opts.imagePath)) { throw "镜像不存在：$($opts.imagePath)" }
    Copy-Item -LiteralPath $opts.imagePath -Destination $vhd -Force -ErrorAction Stop
  }
  default {
    if ([int]$opts.diskSizeGB -lt 1) { throw '空硬盘大小至少 1 GB' }
    New-VHD -Path $vhd -SizeBytes ([int64]$opts.diskSizeGB * 1GB) -Dynamic -ErrorAction Stop | Out-Null
  }
}
if ($opts.isoPath -and -not (Test-Path -LiteralPath $opts.isoPath)) { throw "ISO 不存在：$($opts.isoPath)" }
Write-HvResult ([ordered]@{ vhdPath = $vhd; vmDir = $vmDir })
`

const createVMScript = `
$vmDir = Join-Path $opts.directory $opts.name
$params = @{
  Name = $opts.name
  Generation = [int]$opts.generation
  MemoryStartupBytes = ([int64]$opts.memory.startupMB * 1MB)
  VHDPath = $opts.vhdPath
  Path = $vmDir
}
if ($opts.switchName) { $params['SwitchName'] = $opts.switchName }
$vm = New-VM @params -ErrorAction Stop
if ([bool]$opts.memory.dynamic) {
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $true -StartupBytes ([int64]$opts.memory.startupMB * 1MB) -MinimumBytes ([int64]$opts.memory.minimumMB * 1MB) -MaximumBytes ([int64]$opts.memory.maximumMB * 1MB) -ErrorAction Stop
} else {
  Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes ([int64]$opts.memory.startupMB * 1MB) -ErrorAction Stop
}
Set-VMProcessor -VM $vm -Count ([int]$opts.cpuCount) -ErrorAction Stop
if ($opts.isoPath) {
  $dvd = Add-VMDvdDrive -VM $vm -Path $opts.isoPath -ErrorAction Stop
  if ([int]$opts.generation -eq 2) {
    Set-VMFirmware -VM $vm -FirstBootDevice $dvd -ErrorAction Stop
  } else {
    Set-VMBios -VM $vm -StartupOrder @('CD','IDE','LegacyNetworkAdapter','Floppy') -ErrorAction Stop
  }
}
if ([bool]$opts.disableCheckpoints) {
  Set-VM -VM $vm -CheckpointType Disabled -ErrorAction Stop
}
$adapters = @(Get-VMNetworkAdapter -VMName $opts.name)
foreach ($a in $adapters) {
  Set-VMNetworkAdapter -VMNetworkAdapter $a -MaximumBandwidth ([int64]$opts.bandwidthMbps * 1000000) -ErrorAction Stop
}
Write-HvResult ([ordered]@{ name = "$($vm.Name)"; path = "$($vm.Path)"; vhdPath = "$($opts.vhdPath)" })
`

// Create 新建虚拟机：先创建磁盘，再创建虚拟机并应用硬件配置。
func (c *Client) Create(ctx context.Context, req CreateRequest, progress ProgressFunc) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Directory = strings.TrimSpace(req.Directory)
	req.ImagePath = strings.TrimSpace(req.ImagePath)
	req.ISOPath = strings.TrimSpace(req.ISOPath)
	if err := ValidateName(req.Name); err != nil {
		return fmt.Errorf("虚拟机名称无效: %w", err)
	}
	if err := ValidateAbsoluteDirectory(req.Directory); err != nil {
		return err
	}
	if req.Generation != 1 && req.Generation != 2 {
		return errors.New("虚拟机代数必须是 1 或 2")
	}
	if err := ValidateMemory(req.Memory); err != nil {
		return err
	}
	if err := ValidateCPU(req.CPUCount); err != nil {
		return err
	}
	if err := ValidateBandwidth(req.BandwidthMbps); err != nil {
		return err
	}
	if req.DiskMode == "" {
		req.DiskMode = "new"
	}
	switch req.DiskMode {
	case "differencing", "copy":
		if strings.TrimSpace(req.ImagePath) == "" {
			return errors.New("请选择系统镜像")
		}
	case "new":
		if req.DiskSizeGB < 1 {
			return errors.New("空硬盘大小至少 1 GB")
		}
	default:
		return errors.New("未知的磁盘方式")
	}

	report(progress, 5, "创建虚拟硬盘")
	diskOpts := map[string]any{
		"name":       req.Name,
		"directory":  req.Directory,
		"diskMode":   req.DiskMode,
		"imagePath":  req.ImagePath,
		"diskSizeGB": req.DiskSizeGB,
		"isoPath":    req.ISOPath,
	}
	var prepared struct {
		VHDPath string `json:"vhdPath"`
		VMDir   string `json:"vmDir"`
	}
	if err := c.run(ctx, createDiskScript, diskOpts, &prepared); err != nil {
		return fmt.Errorf("创建虚拟硬盘失败: %w", err)
	}

	report(progress, 45, "创建虚拟机并应用硬件配置")
	createOpts := map[string]any{
		"name":               req.Name,
		"generation":         req.Generation,
		"directory":          req.Directory,
		"memory":             req.Memory,
		"cpuCount":           req.CPUCount,
		"switchName":         req.SwitchName,
		"bandwidthMbps":      req.BandwidthMbps,
		"vhdPath":            prepared.VHDPath,
		"isoPath":            req.ISOPath,
		"disableCheckpoints": req.DisableCheckpoints,
	}
	if err := c.run(ctx, createVMScript, createOpts, nil); err != nil {
		return fmt.Errorf("创建虚拟机失败: %w", err)
	}

	report(progress, 100, "新建完成")
	return nil
}

const editVMScript = `
$vm = Get-VM -Name $opts.name -ErrorAction Stop
if ($opts.newName -and $opts.newName -ne $opts.name) {
  if (Get-VM -Name $opts.newName -ErrorAction SilentlyContinue) { throw "已存在同名虚拟机 '$($opts.newName)'" }
  Rename-VM -VM $vm -NewName $opts.newName -ErrorAction Stop
  $vm = Get-VM -Name $opts.newName -ErrorAction Stop
}
if ($opts.memory) {
  $mem = $opts.memory
  if ([bool]$mem.dynamic) {
    Set-VMMemory -VM $vm -DynamicMemoryEnabled $true -StartupBytes ([int64]$mem.startupMB * 1MB) -MinimumBytes ([int64]$mem.minimumMB * 1MB) -MaximumBytes ([int64]$mem.maximumMB * 1MB) -ErrorAction Stop
  } else {
    Set-VMMemory -VM $vm -DynamicMemoryEnabled $false -StartupBytes ([int64]$mem.startupMB * 1MB) -ErrorAction Stop
  }
}
if ($null -ne $opts.cpuCount) { Set-VMProcessor -VM $vm -Count ([int]$opts.cpuCount) -ErrorAction Stop }
$adapters = @(Get-VMNetworkAdapter -VMName $vm.Name)
if ($opts.switchName) {
  if ($adapters.Count -eq 0) { throw '虚拟机没有网络适配器，无法设置虚拟交换机' }
  $adapters | Connect-VMNetworkAdapter -SwitchName $opts.switchName -ErrorAction Stop
}
if ($null -ne $opts.bandwidthMbps) {
  foreach ($a in $adapters) {
    Set-VMNetworkAdapter -VMNetworkAdapter $a -MaximumBandwidth ([int64]$opts.bandwidthMbps * 1000000) -ErrorAction Stop
  }
}
if ($null -ne $opts.checkpointsEnabled) {
  $checkpointType = 'Disabled'
  if ([bool]$opts.checkpointsEnabled) { $checkpointType = 'Standard' }
  Set-VM -VM $vm -CheckpointType $checkpointType -ErrorAction Stop
}
Write-HvResult ([ordered]@{ name = "$($vm.Name)" })
`

// Update 修改虚拟机配置。只修改 EditRequest 中非 nil 的字段。
func (c *Client) Update(ctx context.Context, name string, req EditRequest, progress ProgressFunc) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("虚拟机名称不能为空")
	}
	if req.NewName != "" && !strings.EqualFold(req.NewName, name) {
		if err := ValidateName(req.NewName); err != nil {
			return fmt.Errorf("新名称无效: %w", err)
		}
	}
	if req.Memory != nil {
		if err := ValidateMemory(*req.Memory); err != nil {
			return err
		}
	}
	if req.CPUCount != nil {
		if err := ValidateCPU(*req.CPUCount); err != nil {
			return err
		}
	}
	if req.BandwidthMbps != nil {
		if err := ValidateBandwidth(*req.BandwidthMbps); err != nil {
			return err
		}
	}
	if req.NewName == "" && req.Memory == nil && req.CPUCount == nil && req.SwitchName == nil && req.BandwidthMbps == nil && req.CheckpointsEnabled == nil {
		return errors.New("没有需要修改的内容")
	}

	report(progress, 10, "应用虚拟机配置")
	opts := map[string]any{
		"name":               name,
		"newName":            req.NewName,
		"memory":             req.Memory,
		"cpuCount":           req.CPUCount,
		"switchName":         req.SwitchName,
		"bandwidthMbps":      req.BandwidthMbps,
		"checkpointsEnabled": req.CheckpointsEnabled,
	}
	if err := c.run(ctx, editVMScript, opts, nil); err != nil {
		return err
	}
	report(progress, 100, "编辑完成")
	return nil
}

const deleteVMScript = `
$vm = Get-VM -Name $opts.name -ErrorAction Stop
if ($vm.State -ne 'Off') {
  Stop-VM -VM $vm -Force -ErrorAction Stop
  $deadline = (Get-Date).AddSeconds(180)
  while ((Get-VM -Name $opts.name).State -ne 'Off') {
    if ((Get-Date) -gt $deadline) { throw '等待虚拟机关机超时（180 秒）' }
    Start-Sleep -Seconds 1
  }
}
$currentPath = "$($vm.Path)"
Remove-VM -VM $vm -Force -ErrorAction Stop
if ([bool]$opts.deleteFiles) {
  if ($currentPath -ne $opts.vmPath) {
    throw "虚拟机路径已变化，已删除虚拟机配置但拒绝删除文件（期望：$($opts.vmPath)，当前：$currentPath）"
  }
  $targets = @($currentPath, (Join-Path (Split-Path $currentPath -Parent) $opts.name)) | Where-Object { $_ } | Select-Object -Unique
  foreach ($t in $targets) {
    if ((Test-Path -LiteralPath $t) -and ((Split-Path -Parent $t) -ne '')) {
      Remove-Item -LiteralPath $t -Recurse -Force -ErrorAction SilentlyContinue
    }
  }
}
Write-HvResult ([ordered]@{ name = "$($opts.name)"; deletedFiles = [bool]$opts.deleteFiles })
`

// Delete 删除虚拟机；deleteFiles 为 true 时同时删除虚拟机目录。
func (c *Client) Delete(ctx context.Context, name string, deleteFiles bool, progress ProgressFunc) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("虚拟机名称不能为空")
	}

	vm, err := c.GetVM(ctx, name)
	if err != nil {
		return err
	}
	if deleteFiles {
		if err := validateDeletePath(vm.Path); err != nil {
			return err
		}
	}

	report(progress, 10, "停止虚拟机（如正在运行）")
	opts := map[string]any{
		"name":        name,
		"vmPath":      vm.Path,
		"deleteFiles": deleteFiles,
	}
	if err := c.run(ctx, deleteVMScript, opts, nil); err != nil {
		return err
	}
	report(progress, 100, "删除完成")
	return nil
}
