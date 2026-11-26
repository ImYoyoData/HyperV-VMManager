package hyperv

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateName 校验虚拟机 / 交换机名称，避免非法字符与 Windows 保留名。
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("名称不能为空")
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return errors.New("名称不能以空格或点结尾")
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 100 {
		return errors.New("名称过长（最多 100 个字符）")
	}
	if strings.ContainsAny(name, `\/:*?"<>|`) {
		return errors.New(`名称不能包含 \ / : * ? " < > | 这些字符`)
	}
	base := strings.ToUpper(strings.TrimSpace(name))
	if idx := strings.Index(base, "."); idx >= 0 {
		base = base[:idx]
	}
	if reservedNames[base] {
		return fmt.Errorf("%s 是 Windows 保留名称", name)
	}
	return nil
}

// ValidateAbsoluteDirectory 校验目标目录必须是绝对路径，且不能是磁盘根目录。
func ValidateAbsoluteDirectory(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("目录不能为空")
	}
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("目录必须是绝对路径：%s", dir)
	}
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	rest = strings.Trim(rest, `\/`)
	if rest == "" {
		return errors.New("不能直接使用磁盘根目录，请选择子目录")
	}
	return nil
}

// ValidateMemory 校验内存参数。
func ValidateMemory(m MemoryOptions) error {
	if m.StartupMB < 256 {
		return errors.New("启动内存不能小于 256 MB")
	}
	if m.StartupMB > 1048576 {
		return errors.New("启动内存不能超过 1048576 MB（1 TB）")
	}
	if m.Dynamic {
		if m.MinimumMB < 256 {
			return errors.New("动态内存最小值不能小于 256 MB")
		}
		if m.MaximumMB < 256 {
			return errors.New("动态内存最大值不能小于 256 MB")
		}
		if m.MinimumMB > m.StartupMB {
			return errors.New("动态内存最小值不能大于启动内存")
		}
		if m.StartupMB > m.MaximumMB {
			return errors.New("启动内存不能大于动态内存最大值")
		}
	}
	return nil
}

// ValidateCPU 校验 CPU 核数。
func ValidateCPU(count int) error {
	if count < 1 || count > 128 {
		return errors.New("CPU 核数必须在 1 到 128 之间")
	}
	return nil
}

// ValidateBandwidth 校验带宽上限，0 表示不限。
func ValidateBandwidth(mbps int) error {
	if mbps < 0 || mbps > 100000 {
		return errors.New("带宽必须在 0 到 100000 Mbps 之间（0 表示不限）")
	}
	return nil
}

// validateDeletePath 保护系统目录 / 盘符根目录，避免误删。
func validateDeletePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("虚拟机路径为空，无法删除文件")
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("虚拟机路径不是绝对路径，拒绝删除：%s", clean)
	}
	volume := filepath.VolumeName(clean)
	rest := strings.Trim(strings.TrimPrefix(clean, volume), `\/`)
	if rest == "" {
		return errors.New("拒绝删除磁盘根目录")
	}
	lower := strings.ToLower(clean)
	protected := []string{
		`c:\windows`, `c:\program files`, `c:\program files (x86)`, `c:\programdata`, `c:\users`,
	}
	for _, root := range protected {
		if lower == root || strings.HasPrefix(lower, root+`\`) {
			return fmt.Errorf("拒绝删除受保护的系统目录：%s", clean)
		}
	}
	return nil
}
