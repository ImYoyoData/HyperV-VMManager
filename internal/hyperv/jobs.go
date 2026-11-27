package hyperv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

func report(progress ProgressFunc, percent int, step string) {
	if progress != nil {
		progress(percent, step)
	}
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
if ($null -ne $opts.nestedVirtualization) {
  $enableNested = [bool]$opts.nestedVirtualization
  Set-VMProcessor -VM $vm -ExposeVirtualizationExtensions $enableNested -ErrorAction Stop
  if ($enableNested) {
    $adapters | Set-VMNetworkAdapter -MacAddressSpoofing On -ErrorAction Stop
  }
}
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
$base = "$($opts.exportDirectory)"
if ($base) {
  New-Item -ItemType Directory -Path $base -Force | Out-Null
} else {
  $base = Join-Path $env:TEMP 'HyperV-VMManager'
}
$exportDir = Join-Path $base ('clone-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $exportDir -Force | Out-Null
$srcDisks = @(Get-VMHardDiskDrive -VM $src | Where-Object { $_.Path })
$needBytes = 0
foreach ($d in $srcDisks) {
  if (Test-Path -LiteralPath $d.Path) { $needBytes += (Get-Item -LiteralPath $d.Path).Length }
}
if ($needBytes -lt 1GB) { $needBytes = 1GB }
Assert-HvFreeSpace $exportDir ($needBytes + 1GB) '克隆临时导出目录'
Assert-HvFreeSpace $targetDir ($needBytes + 1GB) '克隆目标目录'
Export-VM -VM $src -Path $exportDir -ErrorAction Stop
$vmcx = Get-ChildItem -Path $exportDir -Recurse -Filter *.vmcx | Select-Object -First 1
if (-not $vmcx) { throw '导出失败：未找到 .vmcx 文件' }
Write-HvResult ([ordered]@{ vmcx = $vmcx.FullName; exportDir = $exportDir })
`

const cloneDifferencingScript = `
$src = Get-VM -Name $opts.sourceName -ErrorAction Stop
if (Get-VM -Name $opts.newName -ErrorAction SilentlyContinue) { throw "已存在同名虚拟机 '$($opts.newName)'" }
$vmDir = Join-Path $opts.targetDirectory $opts.newName
if (Test-Path -LiteralPath $vmDir) { throw "目标目录已存在：$vmDir" }
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
$srcDisks = @(Get-VMHardDiskDrive -VM $src | Where-Object { $_.Path })
if ($srcDisks.Count -eq 0) { throw '源虚拟机没有虚拟硬盘，无法创建差异磁盘克隆' }
$vhdDir = Join-Path $vmDir 'Virtual Hard Disks'
New-Item -ItemType Directory -Path $vhdDir -Force | Out-Null
Assert-HvFreeSpace $vmDir 1GB '差异磁盘目录'
$diffPaths = @()
$index = 0
foreach ($d in $srcDisks) {
  if (-not (Test-Path -LiteralPath $d.Path)) { throw "源虚拟硬盘不存在：$($d.Path)" }
  $index++
  $ext = [IO.Path]::GetExtension($d.Path)
  $diffPath = Join-Path $vhdDir ("$($opts.newName)-disk$index$ext")
  if (Test-Path -LiteralPath $diffPath) { throw "差异磁盘已存在：$diffPath" }
  New-VHD -ParentPath $d.Path -Path $diffPath -Differencing -ErrorAction Stop | Out-Null
  $diffPaths += $diffPath
}
$params = @{
  Name = $opts.newName
  Generation = [int]$src.Generation
  MemoryStartupBytes = ([int64]$opts.memory.startupMB * 1MB)
  VHDPath = $diffPaths[0]
  Path = $vmDir
}
if ($opts.switchName) { $params['SwitchName'] = $opts.switchName }
$vm = New-VM @params -ErrorAction Stop
for ($i = 1; $i -lt $diffPaths.Count; $i++) {
  Add-VMHardDiskDrive -VM $vm -Path $diffPaths[$i] -ErrorAction Stop | Out-Null
}
$srcNet = @(Get-VMNetworkAdapter -VM $src)
$newNet = @(Get-VMNetworkAdapter -VM $vm)
if ($srcNet.Count -gt 0) {
  while ($newNet.Count -lt $srcNet.Count) {
    Add-VMNetworkAdapter -VM $vm -ErrorAction Stop | Out-Null
    $newNet = @(Get-VMNetworkAdapter -VM $vm)
  }
  if (-not $opts.switchName -and $srcNet[0].SwitchName) {
    $newNet | Connect-VMNetworkAdapter -SwitchName $srcNet[0].SwitchName -ErrorAction Stop
  }
}
Write-HvResult ([ordered]@{ name = "$($vm.Name)"; path = "$($vm.Path)" })
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

	if req.DiskMode == "" {
		req.DiskMode = "copy"
	}
	switch req.DiskMode {
	case "copy", "differencing":
	default:
		return errors.New("克隆磁盘方式必须是 copy 或 differencing")
	}
	req.ExportDirectory = strings.TrimSpace(req.ExportDirectory)
	if req.ExportDirectory != "" {
		if err := ValidateAbsoluteDirectory(req.ExportDirectory); err != nil {
			return fmt.Errorf("临时导出目录无效: %w", err)
		}
	}

	report(progress, 3, "校验参数")
	if req.DiskMode == "differencing" {
		report(progress, 10, "检查源虚拟机并创建差异磁盘")
		diffOpts := map[string]any{
			"sourceName":          req.SourceName,
			"newName":             req.NewName,
			"targetDirectory":     req.TargetDirectory,
			"stopSourceIfRunning": req.StopSourceIfRunning,
			"memory":              req.Memory,
			"cpuCount":            req.CPUCount,
			"switchName":          req.SwitchName,
			"bandwidthMbps":       req.BandwidthMbps,
		}
		var created struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := c.run(ctx, cloneDifferencingScript, diffOpts, &created); err != nil {
			return fmt.Errorf("创建差异磁盘克隆失败: %w", err)
		}
		report(progress, 88, "应用内存 / CPU / 网络 / 带宽")
		applyOpts := map[string]any{
			"name":                 req.NewName,
			"memory":               req.Memory,
			"cpuCount":             req.CPUCount,
			"switchName":           req.SwitchName,
			"bandwidthMbps":        req.BandwidthMbps,
			"resetMacAddress":      true,
			"nestedVirtualization": req.NestedVirtualization,
		}
		if err := c.run(ctx, applySettingsScript, applyOpts, nil); err != nil {
			return fmt.Errorf("新虚拟机已创建，但应用配置失败: %w", err)
		}
		report(progress, 100, "差异磁盘克隆完成")
		return nil
	}

	report(progress, 8, "检查源虚拟机状态并导出")
	exportOpts := map[string]any{
		"sourceName":          req.SourceName,
		"newName":             req.NewName,
		"targetDirectory":     req.TargetDirectory,
		"exportDirectory":     req.ExportDirectory,
		"stopSourceIfRunning": req.StopSourceIfRunning,
	}
	var exported struct {
		VMCX      string `json:"vmcx"`
		ExportDir string `json:"exportDir"`
	}
	if err := c.run(ctx, cloneExportScript, exportOpts, &exported); err != nil {
		return fmt.Errorf("导出源虚拟机失败: %w", err)
	}
	if exported.VMCX == "" {
		return errors.New("导出源虚拟机失败：未找到 .vmcx 文件")
	}
	// 临时导出目录在目标主机上，无论导入成功还是失败都要清理。
	if exported.ExportDir != "" {
		defer func(dir string) {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = c.run(cleanupCtx,
				`if ($opts.dir -and (Test-Path -LiteralPath $opts.dir)) { Remove-Item -LiteralPath $opts.dir -Recurse -Force -ErrorAction SilentlyContinue }`,
				map[string]any{"dir": dir}, nil)
		}(exported.ExportDir)
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
		"name":                 req.NewName,
		"memory":               req.Memory,
		"cpuCount":             req.CPUCount,
		"switchName":           req.SwitchName,
		"bandwidthMbps":        req.BandwidthMbps,
		"resetMacAddress":      true,
		"nestedVirtualization": req.NestedVirtualization,
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
$vhdDir = "$($opts.diskDirectory)"
if (-not $vhdDir) { $vhdDir = Join-Path $vmDir 'Virtual Hard Disks' }
New-Item -ItemType Directory -Path $vhdDir -Force | Out-Null
Assert-HvFreeSpace $vmDir 512MB '虚拟机配置目录'
$ext = '.vhdx'
if ("$($opts.diskMode)" -eq 'differencing' -and $opts.imagePath -and ([IO.Path]::GetExtension("$($opts.imagePath)") -eq '.vhd')) { $ext = '.vhd' }
$vhd = Join-Path $vhdDir ($opts.name + $ext)
switch ("$($opts.diskMode)") {
  'differencing' {
    if (-not $opts.imagePath) { throw '差异磁盘需要选择父盘镜像' }
    if (-not (Test-Path -LiteralPath $opts.imagePath)) { throw "父盘不存在：$($opts.imagePath)" }
    Assert-HvFreeSpace $vhd 1GB '差异磁盘'
    New-VHD -ParentPath $opts.imagePath -Path $vhd -Differencing -ErrorAction Stop | Out-Null
  }
  'copy' {
    if (-not $opts.imagePath) { throw '完整复制需要选择镜像' }
    if (-not (Test-Path -LiteralPath $opts.imagePath)) { throw "镜像不存在：$($opts.imagePath)" }
    $imageBytes = (Get-Item -LiteralPath $opts.imagePath -ErrorAction Stop).Length
    Assert-HvFreeSpace $vhd ($imageBytes + 1GB) '复制镜像'
    Copy-Item -LiteralPath $opts.imagePath -Destination $vhd -Force -ErrorAction Stop
  }
  default {
    if ([int]$opts.diskSizeGB -lt 1) { throw '空硬盘大小至少 1 GB' }
    if ("$($opts.diskType)" -eq 'fixed') {
      Assert-HvFreeSpace $vhd (([int64]$opts.diskSizeGB * 1GB) + 1GB) '固定大小虚拟硬盘'
      New-VHD -Path $vhd -SizeBytes ([int64]$opts.diskSizeGB * 1GB) -Fixed -ErrorAction Stop | Out-Null
    } else {
      Assert-HvFreeSpace $vhd 1GB '动态虚拟硬盘'
      New-VHD -Path $vhd -SizeBytes ([int64]$opts.diskSizeGB * 1GB) -Dynamic -ErrorAction Stop | Out-Null
    }
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
if ([bool]$opts.nestedVirtualization) {
  Set-VMProcessor -VM $vm -ExposeVirtualizationExtensions $true -ErrorAction Stop
  Get-VMNetworkAdapter -VMName $opts.name | Set-VMNetworkAdapter -MacAddressSpoofing On -ErrorAction Stop
}
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
	if req.DiskType == "" {
		req.DiskType = "dynamic"
	}
	switch req.DiskType {
	case "dynamic", "fixed":
	default:
		return errors.New("磁盘类型必须是 dynamic 或 fixed")
	}
	req.DiskDirectory = strings.TrimSpace(req.DiskDirectory)
	if req.DiskDirectory != "" {
		if err := ValidateAbsoluteDirectory(req.DiskDirectory); err != nil {
			return fmt.Errorf("虚拟硬盘目录无效: %w", err)
		}
	}

	report(progress, 5, "创建虚拟硬盘")
	diskOpts := map[string]any{
		"name":          req.Name,
		"directory":     req.Directory,
		"diskMode":      req.DiskMode,
		"diskType":      req.DiskType,
		"diskDirectory": req.DiskDirectory,
		"imagePath":     req.ImagePath,
		"diskSizeGB":    req.DiskSizeGB,
		"isoPath":       req.ISOPath,
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
		"name":                 req.Name,
		"generation":           req.Generation,
		"directory":            req.Directory,
		"memory":               req.Memory,
		"cpuCount":             req.CPUCount,
		"switchName":           req.SwitchName,
		"bandwidthMbps":        req.BandwidthMbps,
		"vhdPath":              prepared.VHDPath,
		"isoPath":              req.ISOPath,
		"disableCheckpoints":   req.DisableCheckpoints,
		"nestedVirtualization": req.NestedVirtualization,
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
$resizeDisks = @($opts.disks | Where-Object { $_ -and "$($_.path)" })
if ($resizeDisks.Count -gt 0) {
  if ($vm.State -ne 'Off') {
    if (-not [bool]$opts.stopVMForDiskResize) { throw '调整虚拟硬盘需要先关闭虚拟机' }
    Stop-VM -VM $vm -Force -ErrorAction Stop
    $deadline = (Get-Date).AddSeconds(180)
    while ((Get-VM -Name $vm.Name).State -ne 'Off') {
      if ((Get-Date) -gt $deadline) { throw '等待虚拟机关机超时（180 秒）' }
      Start-Sleep -Seconds 1
    }
    $vm = Get-VM -Name $vm.Name -ErrorAction Stop
  }
  $attachedPaths = @(Get-VMHardDiskDrive -VM $vm | ForEach-Object { "$($_.Path)" })
  foreach ($item in $resizeDisks) {
    $path = "$($item.path)"
    if (-not ($attachedPaths -contains $path)) { throw "硬盘不属于该虚拟机：$path" }
    if (-not (Test-Path -LiteralPath $path)) { throw "虚拟硬盘不存在：$path" }
    $vhd = Get-VHD -Path $path -ErrorAction Stop
    $newSize = [int64]$item.sizeBytes
    $current = [int64]$vhd.Size
    if ($newSize -le 0) { throw '新容量必须大于 0' }
    $safeMin = [Math]::Max([int64]$vhd.MinimumSize, [int64]$vhd.FileSize)
    if ($newSize -lt $safeMin) {
      throw ("新容量 {0:N1} GB 小于安全下限 {1:N1} GB（最低可设置 {2:N1} GB，当前文件占用 {3:N1} GB），缩小会导致数据丢失" -f ($newSize/1GB), ($safeMin/1GB), ($vhd.MinimumSize/1GB), ($vhd.FileSize/1GB))
    }
    if ($newSize -lt $current -and -not [bool]$item.allowShrink) {
      throw '缩小虚拟硬盘需要明确确认'
    }
    if ($newSize -lt $current -and (([IO.Path]::GetExtension($path) -ne '.vhdx') -or $vhd.VhdType -ne 'Dynamic')) {
      throw "只有动态 VHDX 支持缩小；当前类型是 $($vhd.VhdType)"
    }
    if ($newSize -eq $current) { continue }
    if ($newSize -gt $current -and $vhd.VhdType -eq 'Fixed') {
      Assert-HvFreeSpace $path (($newSize - $current) + 1GB) '扩展固定大小虚拟硬盘'
    }
    Resize-VHD -Path $path -SizeBytes $newSize -ErrorAction Stop
  }
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
if ($null -ne $opts.nestedVirtualization) {
  $enableNested = [bool]$opts.nestedVirtualization
  Set-VMProcessor -VM $vm -ExposeVirtualizationExtensions $enableNested -ErrorAction Stop
  if ($enableNested) {
    $adapters | Set-VMNetworkAdapter -MacAddressSpoofing On -ErrorAction Stop
  }
}
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
if ($null -ne $opts.notes) { Set-VM -VM $vm -Notes "$($opts.notes)" -ErrorAction Stop }
if ($null -ne $opts.automaticStartAction -and "$($opts.automaticStartAction)") {
  Set-VM -VM $vm -AutomaticStartAction "$($opts.automaticStartAction)" -ErrorAction Stop
}
if ($null -ne $opts.automaticStartDelay) { Set-VM -VM $vm -AutomaticStartDelay ([int]$opts.automaticStartDelay) -ErrorAction Stop }
if ($null -ne $opts.automaticStopAction -and "$($opts.automaticStopAction)") {
  Set-VM -VM $vm -AutomaticStopAction "$($opts.automaticStopAction)" -ErrorAction Stop
}
if ($null -ne $opts.automaticCheckpoints) {
  Set-VM -VM $vm -AutomaticCheckpointsEnabled ([bool]$opts.automaticCheckpoints) -ErrorAction Stop
}
if ($null -ne $opts.processorReserve) { Set-VMProcessor -VM $vm -Reserve ([int]$opts.processorReserve) -ErrorAction Stop }
if ($null -ne $opts.processorMaximum) { Set-VMProcessor -VM $vm -Maximum ([int]$opts.processorMaximum) -ErrorAction Stop }
if ($null -ne $opts.processorWeight) { Set-VMProcessor -VM $vm -RelativeWeight ([int]$opts.processorWeight) -ErrorAction Stop }
if ($null -ne $opts.processorMigration) {
  Set-VMProcessor -VM $vm -CompatibilityForMigrationEnabled ([bool]$opts.processorMigration) -ErrorAction Stop
}
if ($null -ne $opts.hwThreadsPerCore) { Set-VMProcessor -VM $vm -HwThreadCountPerCore ([int]$opts.hwThreadsPerCore) -ErrorAction Stop }
$currentVm = Get-VM -Name $vm.Name -ErrorAction Stop
if ($null -ne $opts.memoryBuffer) {
  if (-not $currentVm.DynamicMemoryEnabled) { throw '内存缓冲区仅适用于启用了动态内存的虚拟机' }
  Set-VMMemory -VM $vm -Buffer ([int]$opts.memoryBuffer) -ErrorAction Stop
}
if ($null -ne $opts.memoryPriority) { Set-VMMemory -VM $vm -Priority ([int]$opts.memoryPriority) -ErrorAction Stop }
if ($opts.networkAdapters) {
  $allAdapters = @(Get-VMNetworkAdapter -VMName $vm.Name -ErrorAction SilentlyContinue)
  foreach ($cfg in @($opts.networkAdapters)) {
    $adapter = $allAdapters | Where-Object { "$($_.Id)" -eq "$($cfg.id)" } | Select-Object -First 1
    if (-not $adapter -and "$($cfg.name)") {
      $adapter = $allAdapters | Where-Object { "$($_.Name)" -eq "$($cfg.name)" } | Select-Object -First 1
    }
    if (-not $adapter) { throw "找不到网络适配器: $($cfg.id)" }
    if ($null -ne $cfg.switchName -and "$($cfg.switchName)") {
      Connect-VMNetworkAdapter -VMNetworkAdapter $adapter -SwitchName "$($cfg.switchName)" -ErrorAction Stop
    }
    if ($null -ne $cfg.dynamicMacAddress) {
      if ([bool]$cfg.dynamicMacAddress) {
        Set-VMNetworkAdapter -VMNetworkAdapter $adapter -DynamicMacAddress:$true -ErrorAction Stop
      } else {
        $staticMac = "$($cfg.staticMacAddress)"
        if (-not $staticMac) { $staticMac = "$($adapter.MacAddress)" }
        Set-VMNetworkAdapter -VMNetworkAdapter $adapter -DynamicMacAddress:$false -StaticMacAddress $staticMac -ErrorAction Stop
      }
    } elseif ($null -ne $cfg.staticMacAddress -and "$($cfg.staticMacAddress)") {
      Set-VMNetworkAdapter -VMNetworkAdapter $adapter -DynamicMacAddress:$false -StaticMacAddress "$($cfg.staticMacAddress)" -ErrorAction Stop
    }
    $params = @{}
    if ($null -ne $cfg.macAddressSpoofing) { $params.MacAddressSpoofing = "$($cfg.macAddressSpoofing)" }
    if ($null -ne $cfg.dhcpGuard) { $params.DhcpGuard = "$($cfg.dhcpGuard)" }
    if ($null -ne $cfg.routerGuard) { $params.RouterGuard = "$($cfg.routerGuard)" }
    if ($null -ne $cfg.deviceNaming) { $params.DeviceNaming = "$($cfg.deviceNaming)" }
    if ($null -ne $cfg.vmqWeight) { $params.VmqWeight = [int]$cfg.vmqWeight }
    if ($null -ne $cfg.iovWeight) { $params.IovWeight = [int]$cfg.iovWeight }
    if ($null -ne $cfg.ipsecOffload) { $params.IPsecOffloadMaximumSecurityAssociation = [bool]$cfg.ipsecOffload }
    if ($null -ne $cfg.stormLimit) { $params.StormLimit = [int]$cfg.stormLimit }
    if ($null -ne $cfg.bandwidthMbps) { $params.MaximumBandwidth = [int64]$cfg.bandwidthMbps * 1000000 }
    if ($params.Count -gt 0) { Set-VMNetworkAdapter -VMNetworkAdapter $adapter @params -ErrorAction Stop }
    if ($null -ne $cfg.vlanMode -and "$($cfg.vlanMode)") {
      switch ("$($cfg.vlanMode)") {
        'Access' { Set-VMNetworkAdapterVlan -VMNetworkAdapter $adapter -Access -VlanId ([int]$cfg.vlanID) -ErrorAction Stop }
        'Untagged' { Set-VMNetworkAdapterVlan -VMNetworkAdapter $adapter -Untagged -ErrorAction Stop }
        default { throw "不支持的 VLAN 模式: $($cfg.vlanMode)" }
      }
    }
  }
}
if ($opts.security) {
  $sec = $opts.security
  if ($null -ne $sec.tpmEnabled) {
    if ($vm.Generation -ne 2) { throw '只有第 2 代虚拟机支持 TPM' }
    if ([bool]$sec.tpmEnabled) { Enable-VMTPM -VM $vm -ErrorAction Stop } else { Disable-VMTPM -VM $vm -ErrorAction Stop }
  }
  if ($null -ne $sec.encryptStateAndVmMigrationTraffic -or $null -ne $sec.virtualizationBasedSecurityOptOut) {
    $secParams = @{}
    if ($null -ne $sec.encryptStateAndVmMigrationTraffic) {
      $secParams.EncryptStateAndVmMigrationTraffic = [bool]$sec.encryptStateAndVmMigrationTraffic
    }
    if ($null -ne $sec.virtualizationBasedSecurityOptOut) {
      $secParams.VirtualizationBasedSecurityOptOut = [bool]$sec.virtualizationBasedSecurityOptOut
    }
    Set-VMSecurity -VM $vm @secParams -ErrorAction Stop
  }
  if ($vm.Generation -eq 2) {
    if ($null -ne $sec.secureBoot) {
      $secure = if ([bool]$sec.secureBoot) { 'On' } else { 'Off' }
      if ([bool]$sec.secureBoot -and "$($sec.secureBootTemplate)") {
        Set-VMFirmware -VM $vm -EnableSecureBoot $secure -SecureBootTemplate "$($sec.secureBootTemplate)" -ErrorAction Stop
      } else {
        Set-VMFirmware -VM $vm -EnableSecureBoot $secure -ErrorAction Stop
      }
    }
    if ($null -ne $sec.preferredNetworkBootProtocol -and "$($sec.preferredNetworkBootProtocol)") {
      Set-VMFirmware -VM $vm -PreferredNetworkBootProtocol "$($sec.preferredNetworkBootProtocol)" -ErrorAction Stop
    }
  }
}
if ($opts.integrationServices) {
  foreach ($svc in @($opts.integrationServices)) {
    if ([bool]$svc.enabled) {
      Enable-VMIntegrationService -VMName $vm.Name -Name "$($svc.name)" -ErrorAction Stop
    } else {
      Disable-VMIntegrationService -VMName $vm.Name -Name "$($svc.name)" -ErrorAction Stop
    }
  }
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
	if req.NewName == "" && req.Memory == nil && req.CPUCount == nil && req.SwitchName == nil && req.BandwidthMbps == nil && req.CheckpointsEnabled == nil && req.NestedVirtualization == nil && len(req.Disks) == 0 &&
		req.Notes == nil && req.AutomaticStartAction == nil && req.AutomaticStartDelay == nil && req.AutomaticStopAction == nil && req.AutomaticCheckpoints == nil &&
		req.ProcessorReserve == nil && req.ProcessorMaximum == nil && req.ProcessorWeight == nil && req.ProcessorMigration == nil && req.HwThreadsPerCore == nil &&
		req.MemoryBuffer == nil && req.MemoryPriority == nil && len(req.NetworkAdapters) == 0 && req.Security == nil && len(req.IntegrationServices) == 0 {
		return errors.New("没有需要修改的内容")
	}
	if err := validateEditAdvanced(&req); err != nil {
		return err
	}
	if len(req.Disks) > 0 {
		seen := map[string]bool{}
		for _, disk := range req.Disks {
			if strings.TrimSpace(disk.Path) == "" {
				return errors.New("硬盘路径不能为空")
			}
			if disk.SizeBytes <= 0 {
				return errors.New("硬盘容量必须大于 0")
			}
			if seen[disk.Path] {
				return errors.New("同一块硬盘不能重复调整")
			}
			seen[disk.Path] = true
		}
	}

	report(progress, 10, "应用虚拟机配置")
	opts := map[string]any{
		"name":                 name,
		"newName":              req.NewName,
		"memory":               req.Memory,
		"cpuCount":             req.CPUCount,
		"switchName":           req.SwitchName,
		"bandwidthMbps":        req.BandwidthMbps,
		"checkpointsEnabled":   req.CheckpointsEnabled,
		"nestedVirtualization": req.NestedVirtualization,
		"disks":                req.Disks,
		"stopVMForDiskResize":  req.StopVMForDiskResize,
		"notes":                req.Notes,
		"automaticStartAction": req.AutomaticStartAction,
		"automaticStartDelay":  req.AutomaticStartDelay,
		"automaticStopAction":  req.AutomaticStopAction,
		"automaticCheckpoints": req.AutomaticCheckpoints,
		"processorReserve":     req.ProcessorReserve,
		"processorMaximum":     req.ProcessorMaximum,
		"processorWeight":      req.ProcessorWeight,
		"processorMigration":   req.ProcessorMigration,
		"hwThreadsPerCore":     req.HwThreadsPerCore,
		"memoryBuffer":         req.MemoryBuffer,
		"memoryPriority":       req.MemoryPriority,
		"networkAdapters":      req.NetworkAdapters,
		"security":             req.Security,
		"integrationServices":  req.IntegrationServices,
	}
	if err := c.run(ctx, editVMScript, opts, nil); err != nil {
		return err
	}
	report(progress, 100, "编辑完成")
	return nil
}

func validateEditAdvanced(req *EditRequest) error {
	if req.AutomaticStartAction != nil {
		switch strings.ToLower(strings.TrimSpace(*req.AutomaticStartAction)) {
		case "nothing":
			*req.AutomaticStartAction = "Nothing"
		case "startifrunning", "startifpreviouslyrunning":
			*req.AutomaticStartAction = "StartIfRunning"
		case "start", "alwaysstart":
			*req.AutomaticStartAction = "Start"
		default:
			return errors.New("自动启动操作必须是 Nothing、StartIfRunning（上次运行则启动）或 Start（总是启动）")
		}
	}
	if req.AutomaticStopAction != nil {
		switch strings.ToLower(strings.TrimSpace(*req.AutomaticStopAction)) {
		case "turnoff":
			*req.AutomaticStopAction = "TurnOff"
		case "save":
			*req.AutomaticStopAction = "Save"
		case "shutdown":
			*req.AutomaticStopAction = "ShutDown"
		default:
			return errors.New("自动停止操作必须是 TurnOff、Save 或 ShutDown")
		}
	}
	if req.AutomaticStartDelay != nil && (*req.AutomaticStartDelay < 0 || *req.AutomaticStartDelay > 86400) {
		return errors.New("自动启动延迟必须在 0 到 86400 秒之间")
	}
	if req.Notes != nil && len([]rune(*req.Notes)) > 4096 {
		return errors.New("虚拟机备注不能超过 4096 个字符")
	}
	if err := validatePercent(req.ProcessorReserve, "处理器保留"); err != nil {
		return err
	}
	if err := validatePercent(req.ProcessorMaximum, "处理器上限"); err != nil {
		return err
	}
	if req.ProcessorWeight != nil && (*req.ProcessorWeight < 0 || *req.ProcessorWeight > 10000) {
		return errors.New("处理器相对权重必须在 0 到 10000 之间")
	}
	if req.HwThreadsPerCore != nil && (*req.HwThreadsPerCore < 0 || *req.HwThreadsPerCore > 2) {
		return errors.New("每核心硬件线程数只能是 0、1 或 2")
	}
	if req.MemoryBuffer != nil && (*req.MemoryBuffer < 0 || *req.MemoryBuffer > 200) {
		return errors.New("内存缓冲区必须在 0 到 200 之间")
	}
	if err := validatePercent(req.MemoryPriority, "内存优先级"); err != nil {
		return err
	}

	for i := range req.NetworkAdapters {
		cfg := &req.NetworkAdapters[i]
		cfg.ID = strings.TrimSpace(cfg.ID)
		cfg.Name = strings.TrimSpace(cfg.Name)
		if cfg.ID == "" && cfg.Name == "" {
			return errors.New("网络适配器 ID 或名称不能为空")
		}
		if cfg.BandwidthMbps != nil {
			if err := ValidateBandwidth(*cfg.BandwidthMbps); err != nil {
				return err
			}
		}
		if cfg.VmqWeight != nil && (*cfg.VmqWeight < 0 || *cfg.VmqWeight > 100) {
			return errors.New("VMQ 权重必须在 0 到 100 之间")
		}
		if cfg.IovWeight != nil && (*cfg.IovWeight < 0 || *cfg.IovWeight > 100) {
			return errors.New("SR-IOV 权重必须在 0 到 100 之间")
		}
		if cfg.StormLimit != nil && *cfg.StormLimit < 0 {
			return errors.New("广播风暴限制不能小于 0")
		}
		if cfg.DynamicMacAddress != nil && *cfg.DynamicMacAddress {
			cfg.StaticMacAddress = nil
		}
		if cfg.StaticMacAddress != nil {
			mac := strings.TrimSpace(*cfg.StaticMacAddress)
			if mac != "" {
				if _, err := net.ParseMAC(mac); err != nil {
					return fmt.Errorf("静态 MAC 地址无效: %s", mac)
				}
				cfg.StaticMacAddress = &mac
				dynamic := false
				cfg.DynamicMacAddress = &dynamic
			}
		}
		if err := canonicalOnOff(cfg.MacAddressSpoofing, "MAC 地址欺骗"); err != nil {
			return err
		}
		if err := canonicalOnOff(cfg.DhcpGuard, "DHCP 防护"); err != nil {
			return err
		}
		if err := canonicalOnOff(cfg.RouterGuard, "路由器防护"); err != nil {
			return err
		}
		if err := canonicalOnOff(cfg.DeviceNaming, "设备命名"); err != nil {
			return err
		}
		if cfg.VlanMode != nil {
			switch strings.ToLower(strings.TrimSpace(*cfg.VlanMode)) {
			case "access":
				*cfg.VlanMode = "Access"
				if cfg.VlanID == nil || *cfg.VlanID < 1 || *cfg.VlanID > 4095 {
					return errors.New("Access VLAN 需要 1 到 4095 的 VLAN ID")
				}
			case "untagged":
				*cfg.VlanMode = "Untagged"
				zero := 0
				cfg.VlanID = &zero
			default:
				return errors.New("VLAN 模式仅支持 Access 或 Untagged")
			}
		}
	}

	if req.Security != nil {
		if req.Security.SecureBootTemplate != nil {
			template := strings.TrimSpace(*req.Security.SecureBootTemplate)
			switch template {
			case "", "MicrosoftWindows", "MicrosoftUEFICertificateAuthority", "OpenSourceShieldedVM":
			default:
				return errors.New("安全启动模板必须是 MicrosoftWindows、MicrosoftUEFICertificateAuthority 或 OpenSourceShieldedVM")
			}
			req.Security.SecureBootTemplate = &template
		}
		if req.Security.PreferredNetworkBootProtocol != nil {
			protocol := strings.TrimSpace(*req.Security.PreferredNetworkBootProtocol)
			switch protocol {
			case "IPv4", "IPv6", "None":
			default:
				return errors.New("网络启动协议必须是 IPv4、IPv6 或 None")
			}
			req.Security.PreferredNetworkBootProtocol = &protocol
		}
	}

	for i := range req.IntegrationServices {
		req.IntegrationServices[i].Name = strings.TrimSpace(req.IntegrationServices[i].Name)
		if req.IntegrationServices[i].Name == "" {
			return errors.New("集成服务名称不能为空")
		}
	}
	return nil
}

func validatePercent(value *int, label string) error {
	if value != nil && (*value < 0 || *value > 100) {
		return fmt.Errorf("%s必须在 0 到 100 之间", label)
	}
	return nil
}

func canonicalOnOff(value *string, label string) error {
	if value == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(*value)) {
	case "on", "true":
		*value = "On"
	case "off", "false":
		*value = "Off"
	default:
		return fmt.Errorf("%s必须是 On 或 Off", label)
	}
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
