package hyperv

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"
)

// TestManualRemote 用于人工验证远程 WinRM 连接，默认跳过。
func TestManualRemote(t *testing.T) {
	host := os.Getenv("HV_TEST_HOST")
	if host == "" {
		t.Skip("未设置 HV_TEST_HOST")
	}
	password := os.Getenv("HV_TEST_PASS")
	if encoded := os.Getenv("HV_TEST_PASS_B64"); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil {
			password = string(decoded)
		}
	}
	auth := os.Getenv("HV_TEST_AUTH")
	if auth == "" {
		auth = "Default"
	}
	client := NewClientForTarget(Target{
		ID:             "manual",
		Name:           host,
		ComputerName:   host,
		Port:           5985,
		Username:       os.Getenv("HV_TEST_USER"),
		Password:       password,
		Authentication: auth,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	info, err := client.TestConnection(ctx)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	t.Logf("连接成功: %+v", info)
}
