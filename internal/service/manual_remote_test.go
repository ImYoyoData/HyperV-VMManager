package service

import (
	"encoding/base64"
	"os"
	"testing"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
)

// TestManualRemoteHostFlow 完整走一遍“添加远程计算机 -> 连接 -> 列出虚拟机”的后端流程。
// 需要设置 HV_TEST_HOST / HV_TEST_USER / HV_TEST_PASS_B64，默认跳过。
func TestManualRemoteHostFlow(t *testing.T) {
	host := os.Getenv("HV_TEST_HOST")
	if host == "" {
		t.Skip("未设置 HV_TEST_HOST")
	}
	password, _ := base64.StdEncoding.DecodeString(os.Getenv("HV_TEST_PASS_B64"))

	// 把设置目录指向临时目录，避免污染真实的 %AppData%\HyperV-VMManager。
	t.Setenv("APPDATA", t.TempDir())

	store, err := settings.NewStore()
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	hostStore, err := hosts.NewStore(store.HostsPath())
	if err != nil {
		t.Fatalf("创建主机存储失败: %v", err)
	}
	queue, err := tasks.NewQueue(store.TasksPath())
	if err != nil {
		t.Fatalf("创建任务队列失败: %v", err)
	}
	svc := New(queue, store, hostStore)

	item, err := svc.AddHost(hosts.Request{
		Name:     "manual-test",
		Address:  host,
		Port:     5985,
		Username: os.Getenv("HV_TEST_USER"),
	})
	if err != nil {
		t.Fatalf("添加远程计算机失败: %v", err)
	}
	connected, err := svc.ConnectHost(item.ID, string(password), true)
	if err != nil {
		t.Fatalf("连接远程计算机失败: %v", err)
	}
	t.Logf("连接成功: id=%s name=%s connected=%v active=%v", connected.ID, connected.Name, connected.Connected, connected.Active)

	vms, err := svc.ListVMsForHost(item.ID)
	if err != nil {
		t.Fatalf("读取远程虚拟机失败: %v", err)
	}
	t.Logf("远程主机虚拟机数量: %d", len(vms))
	for i, vm := range vms {
		if i >= 5 {
			break
		}
		t.Logf("  - %s (%s)", vm.Name, vm.State)
	}
}
