// Package applog 写入应用运行日志，按天分文件。
// 日志目录由设置决定，默认优先使用非系统盘。
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu      sync.Mutex
	dir     string
	file    *os.File
	fileDay string
)

// Init 切换日志目录并关闭旧文件。
func Init(logDir string) error {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
		fileDay = ""
	}
	dir = logDir
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = ""
		return err
	}
	return nil
}

// Dir 返回当前日志目录。
func Dir() string {
	mu.Lock()
	defer mu.Unlock()
	return dir
}

// Info 写入一条信息日志。
func Info(format string, args ...any) { write("INFO", format, args...) }

// Warn 写入一条警告日志。
func Warn(format string, args ...any) { write("WARN", format, args...) }

// Error 写入一条错误日志。
func Error(format string, args ...any) { write("ERROR", format, args...) }

func write(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return
	}
	now := time.Now()
	day := now.Format("2006-01-02")
	if file == nil || fileDay != day {
		if file != nil {
			_ = file.Close()
		}
		path := filepath.Join(dir, "app-"+day+".log")
		opened, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		file = opened
		fileDay = day
	}
	_, _ = fmt.Fprintf(file, "%s [%s] %s\n", now.Format(time.RFC3339), level, fmt.Sprintf(format, args...))
}

// Close 关闭当前日志文件。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
	}
}
