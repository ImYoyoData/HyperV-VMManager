package tunnel

import (
	"strings"
	"testing"

	"hypervvmmanager/internal/settings"
)

func TestApplyDisabled(t *testing.T) {
	runner := New()
	status := runner.Apply(Config{Enabled: false})
	if status.Enabled || status.Running {
		t.Fatalf("禁用时不应启动 frpc: %+v", status)
	}
	if runner.Status().Enabled {
		t.Fatal("Status 应保持禁用状态")
	}
}

func TestValidate(t *testing.T) {
	valid := Config{
		Enabled:    true,
		WebEnabled: true,
		ServerAddr: "frp.example.com",
		ServerPort: 7000,
		Protocol:   "tcp",
		ProxyName:  "hvvm-web",
		RemotePort: 17890,
		LocalPort:  17890,
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("合法配置校验失败: %v", err)
	}

	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"缺少服务器", Config{ServerPort: 7000, LocalPort: 1}, "服务器地址"},
		{"端口越界", Config{ServerAddr: "a", ServerPort: 70000, LocalPort: 1}, "服务器端口"},
		{"协议不支持", Config{ServerAddr: "a", ServerPort: 7000, Protocol: "udp2", LocalPort: 1}, "传输协议"},
		{"代理名非法", Config{ServerAddr: "a", ServerPort: 7000, ProxyName: "a b", LocalPort: 1}, "代理名称"},
		{"远程端口越界", Config{ServerAddr: "a", ServerPort: 7000, RemotePort: 70000, LocalPort: 1}, "远程端口"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.cfg)
			if err == nil {
				t.Fatal("期望校验失败")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息 %q 缺少关键字 %q", err.Error(), tc.want)
			}
		})
	}
}

func TestConfigFromSettingsUsesWebPort(t *testing.T) {
	value := settings.Defaults()
	value.Tunnel.Enabled = true
	value.Web.Enabled = true
	value.Web.Port = 18888
	cfg := ConfigFromSettings(value)
	if cfg.LocalPort != 18888 {
		t.Fatalf("本地端口应跟随网页控制台端口，得到 %d", cfg.LocalPort)
	}
	if !cfg.Enabled {
		t.Fatal("网页控制台开启时穿透应启用")
	}
	value.Web.Enabled = false
	if ConfigFromSettings(value).Enabled {
		t.Fatal("网页控制台关闭且没有自定义映射时穿透不应启用")
	}
}

func TestConfigFromSettingsMappings(t *testing.T) {
	value := settings.Defaults()
	value.Web.Enabled = false
	value.Tunnel.Enabled = true
	value.Tunnel.ServerAddr = "frp.example.com"
	value.Tunnel.ServerPort = 7000
	value.Tunnel.Mappings = []settings.TunnelMapping{
		{
			Enabled:    true,
			Name:       "vm-rdp",
			LocalIP:    "172.17.136.94",
			LocalPort:  3389,
			RemotePort: 13389,
		},
	}
	cfg := ConfigFromSettings(value)
	if !cfg.Enabled {
		t.Fatal("存在自定义映射时应启用穿透")
	}
	if cfg.WebEnabled {
		t.Fatal("网页控制台关闭时不应包含网页映射")
	}
	if len(cfg.Mappings) != 1 || cfg.Mappings[0].LocalPort != 3389 {
		t.Fatalf("自定义映射未正确转换: %+v", cfg.Mappings)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("自定义映射校验失败: %v", err)
	}
}
