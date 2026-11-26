package hosts

import (
	"path/filepath"
	"testing"
)

func TestStoreStartsWithLocal(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	profiles := store.List()
	if len(profiles) != 1 {
		t.Fatalf("默认应只有本机主机，实际 %d", len(profiles))
	}
	if profiles[0].ID != LocalID || profiles[0].Address != "" {
		t.Fatalf("本机配置不正确: %+v", profiles[0])
	}
}

func TestStoreCRUD(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}

	profile, err := store.Add(Request{
		Name:     "实验室服务器",
		Address:  "192.168.1.20",
		Username: `LAB\Administrator`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID == "" || profile.ID == LocalID {
		t.Fatalf("远程主机 ID 不正确: %q", profile.ID)
	}
	if profile.Port != 5985 {
		t.Fatalf("默认 WinRM 端口应为 5985，实际 %d", profile.Port)
	}

	updated, err := store.Update(Request{
		ID:       profile.ID,
		Name:     "实验室服务器 2",
		Address:  "hyperv-lab.local",
		Port:     5986,
		UseSSL:   true,
		Username: `LAB\Administrator`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "实验室服务器 2" || updated.Port != 5986 || !updated.UseSSL {
		t.Fatalf("更新结果不正确: %+v", updated)
	}

	if err := store.MarkConnected(profile.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastConnectedAt == "" {
		t.Fatal("应记录最近连接时间")
	}

	reloaded, err := NewStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 2 {
		t.Fatalf("重新加载后应有 2 台主机，实际 %d", len(reloaded.List()))
	}

	if err := store.Remove(LocalID); err == nil {
		t.Fatal("本机连接不应允许删除")
	}
	if err := store.Remove(profile.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 1 {
		t.Fatal("删除后应只剩本机")
	}
}

func TestStoreRejectsInvalidAddress(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Request{Name: "空地址", Address: "  "}); err == nil {
		t.Fatal("空地址应被拒绝")
	}
}
