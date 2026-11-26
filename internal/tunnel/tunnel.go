// Package tunnel 内置 frp 客户端（frpc），用于把本机网页控制台通过
// 公网 frps 服务器暴露出去。frp 是 Apache-2.0 授权的纯 Go 项目，
// 因此直接作为库编译进应用，不需要额外的 frpc.exe。
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	frpclient "github.com/fatedier/frp/client"
	frpproxy "github.com/fatedier/frp/client/proxy"
	"github.com/fatedier/frp/pkg/config/source"
	"github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/policy/security"
	"hypervvmmanager/internal/settings"
)

const defaultProxyName = "hvvm-web"

var proxyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Config 是启动内置 frpc 所需要的参数。
type Config struct {
	Enabled    bool
	WebEnabled bool
	ServerAddr string
	ServerPort int
	Token      string
	Protocol   string
	ProxyName  string
	RemotePort int
	LocalPort  int
	Mappings   []Mapping
}

// Mapping 是一条额外的 TCP 端口映射。
type Mapping struct {
	Enabled    bool
	Name       string
	LocalIP    string
	LocalPort  int
	RemotePort int
}

// Status 是内网穿透的运行时状态，供桌面端与设置页展示。
type Status struct {
	Enabled      bool   `json:"enabled"`
	Running      bool   `json:"running"`
	Connected    bool   `json:"connected"`
	Error        string `json:"error,omitempty"`
	ServerAddr   string `json:"serverAddr,omitempty"`
	RemoteAddr   string `json:"remoteAddr,omitempty"`
	ProxyName    string `json:"proxyName,omitempty"`
	LocalAddress string `json:"localAddress,omitempty"`
	StartedAt    string `json:"startedAt,omitempty"`
}

// Runner 管理单个内置 frpc 客户端的生命周期；配置变更时整体重启。
type Runner struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation int
	status     Status
}

// New 创建 Runner。
func New() *Runner {
	return &Runner{}
}

// ConfigFromSettings 将持久化配置转换为运行参数。
func ConfigFromSettings(value settings.Settings) Config {
	cfg := Config{
		Enabled:    value.Tunnel.Enabled && (value.Web.Enabled || len(value.Tunnel.Mappings) > 0),
		WebEnabled: value.Web.Enabled,
		ServerAddr: value.Tunnel.ServerAddr,
		ServerPort: value.Tunnel.ServerPort,
		Token:      value.Tunnel.Token,
		Protocol:   value.Tunnel.Protocol,
		ProxyName:  value.Tunnel.ProxyName,
		RemotePort: value.Tunnel.RemotePort,
		LocalPort:  value.Web.Port,
	}
	for _, mapping := range value.Tunnel.Mappings {
		cfg.Mappings = append(cfg.Mappings, Mapping{
			Enabled:    mapping.Enabled,
			Name:       mapping.Name,
			LocalIP:    mapping.LocalIP,
			LocalPort:  mapping.LocalPort,
			RemotePort: mapping.RemotePort,
		})
	}
	return cfg
}

// Validate 校验内网穿透配置。
func Validate(cfg Config) error {
	if strings.TrimSpace(cfg.ServerAddr) == "" {
		return errors.New("请填写 frps 服务器地址")
	}
	if cfg.ServerPort < 1 || cfg.ServerPort > 65535 {
		return errors.New("frps 服务器端口必须在 1 到 65535 之间")
	}
	switch cfg.Protocol {
	case "", "tcp", "kcp", "quic", "websocket", "wss":
	default:
		return errors.New("frp 传输协议必须是 tcp、kcp、quic、websocket 或 wss")
	}
	proxyName := strings.TrimSpace(cfg.ProxyName)
	if proxyName == "" {
		proxyName = defaultProxyName
	}
	if !proxyNamePattern.MatchString(proxyName) {
		return errors.New("代理名称只能包含字母、数字、点、下划线和短横线，长度不超过 64")
	}
	if cfg.RemotePort < 0 || cfg.RemotePort > 65535 {
		return errors.New("远程端口必须在 0 到 65535 之间，0 表示由 frps 随机分配")
	}
	enabledMappings := 0
	for _, mapping := range cfg.Mappings {
		if !mapping.Enabled {
			continue
		}
		enabledMappings++
		name := strings.TrimSpace(mapping.Name)
		if name != "" && !proxyNamePattern.MatchString(name) {
			return errors.New("端口映射名称只能包含字母、数字、点、下划线和短横线，长度不超过 64")
		}
		if mapping.LocalPort < 1 || mapping.LocalPort > 65535 {
			return errors.New("端口映射的目标端口必须在 1 到 65535 之间")
		}
		if mapping.RemotePort < 0 || mapping.RemotePort > 65535 {
			return errors.New("端口映射的远程端口必须在 0 到 65535 之间")
		}
	}
	if cfg.WebEnabled {
		if cfg.LocalPort < 1 || cfg.LocalPort > 65535 {
			return errors.New("本地网页控制台端口无效")
		}
	} else if enabledMappings == 0 {
		return errors.New("启用内网穿透时，至少需要网页控制台或一条自定义端口映射")
	}
	return nil
}

// Apply 按新配置重启内网穿透。返回应用后的即时状态；
// 真正的连接结果会异步更新，可通过 Status 查询。
func (r *Runner) Apply(cfg Config) Status {
	r.stop()

	cfg.ServerAddr = strings.TrimSpace(cfg.ServerAddr)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Protocol = strings.ToLower(strings.TrimSpace(cfg.Protocol))
	cfg.ProxyName = strings.TrimSpace(cfg.ProxyName)
	if cfg.ProxyName == "" {
		cfg.ProxyName = defaultProxyName
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "tcp"
	}
	if cfg.ServerPort == 0 {
		cfg.ServerPort = 7000
	}

	if !cfg.Enabled {
		status := Status{Enabled: false}
		r.setStatus(status)
		return status
	}

	if err := Validate(cfg); err != nil {
		status := Status{Enabled: true, Error: err.Error()}
		r.setStatus(status)
		return status
	}

	initial := Status{
		Enabled:    true,
		Running:    true,
		ServerAddr: net.JoinHostPort(cfg.ServerAddr, strconv.Itoa(cfg.ServerPort)),
		ProxyName:  cfg.ProxyName,
		StartedAt:  time.Now().Format(time.RFC3339),
	}
	if cfg.WebEnabled {
		initial.LocalAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.LocalPort))
		if cfg.RemotePort > 0 {
			initial.RemoteAddr = net.JoinHostPort(cfg.ServerAddr, strconv.Itoa(cfg.RemotePort))
		}
	}

	svc, err := newService(cfg)
	if err != nil {
		initial.Running = false
		initial.Error = err.Error()
		r.setStatus(initial)
		return initial
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.generation++
	generation := r.generation
	r.cancel = cancel
	r.status = initial
	r.mu.Unlock()

	go r.run(ctx, cancel, generation, svc, cfg.ProxyName)
	watchName := cfg.ProxyName
	if !cfg.WebEnabled {
		for _, mapping := range cfg.Mappings {
			if mapping.Enabled {
				watchName = cfg.ProxyName + "-" + mapping.Name
				break
			}
		}
	}
	go r.watch(ctx, generation, svc, watchName)
	return initial
}

// Status 返回当前状态快照。
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Stop 停止内网穿透。
func (r *Runner) Stop() {
	r.stop()
	r.setStatus(Status{})
}

func (r *Runner) stop() {
	r.mu.Lock()
	cancel := r.cancel
	enabled := r.status.Enabled
	r.cancel = nil
	r.generation++
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.setStatus(Status{Enabled: enabled})
}

func (r *Runner) setStatus(status Status) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

func (r *Runner) run(ctx context.Context, cancel context.CancelFunc, generation int, svc *frpclient.Service, proxyName string) {
	err := svc.Run(ctx)
	cancel()

	r.mu.Lock()
	if r.generation != generation {
		r.mu.Unlock()
		return
	}
	r.cancel = nil
	r.status.Running = false
	r.status.Connected = false
	if err != nil && ctx.Err() == nil {
		r.status.Error = err.Error()
	} else if err == nil && ctx.Err() == nil {
		r.status.Error = "frpc 已退出"
	}
	r.mu.Unlock()
}

func (r *Runner) watch(ctx context.Context, generation int, svc *frpclient.Service, proxyName string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			working, ok := svc.StatusExporter().GetProxyStatus(proxyName)
			if !ok {
				continue
			}
			r.mu.Lock()
			if r.generation == generation {
				r.status.Connected = working.Phase == frpproxy.ProxyPhaseRunning
				if working.RemoteAddr != "" {
					r.status.RemoteAddr = working.RemoteAddr
				}
				if working.Phase == frpproxy.ProxyPhaseStartErr || working.Phase == frpproxy.ProxyPhaseCheckFailed {
					if working.Err != "" {
						r.status.Error = working.Err
					}
				}
			}
			r.mu.Unlock()
		}
	}
}

func newService(cfg Config) (*frpclient.Service, error) {
	loginFailExit := true
	common := &v1.ClientCommonConfig{
		ServerAddr:    cfg.ServerAddr,
		ServerPort:    cfg.ServerPort,
		LoginFailExit: &loginFailExit,
		Auth: v1.AuthClientConfig{
			Method: v1.AuthMethodToken,
			Token:  cfg.Token,
		},
		Transport: v1.ClientTransportConfig{
			Protocol: cfg.Protocol,
		},
		Log: v1.LogConfig{
			To:                "console",
			Level:             "warn",
			DisablePrintColor: true,
		},
	}

	configSource := source.NewConfigSource()
	proxies := make([]v1.ProxyConfigurer, 0, 1+len(cfg.Mappings))
	if cfg.WebEnabled {
		proxies = append(proxies, &v1.TCPProxyConfig{
			ProxyBaseConfig: v1.ProxyBaseConfig{
				Name: cfg.ProxyName,
				Type: "tcp",
				ProxyBackend: v1.ProxyBackend{
					LocalIP:   "127.0.0.1",
					LocalPort: cfg.LocalPort,
				},
			},
			RemotePort: cfg.RemotePort,
		})
	}
	for _, mapping := range cfg.Mappings {
		if !mapping.Enabled {
			continue
		}
		name := strings.TrimSpace(mapping.Name)
		if name == "" {
			name = "map"
		}
		proxyName := cfg.ProxyName + "-" + name
		if len(proxyName) > 64 {
			proxyName = proxyName[:64]
		}
		localIP := strings.TrimSpace(mapping.LocalIP)
		if localIP == "" {
			localIP = "127.0.0.1"
		}
		proxies = append(proxies, &v1.TCPProxyConfig{
			ProxyBaseConfig: v1.ProxyBaseConfig{
				Name: proxyName,
				Type: "tcp",
				ProxyBackend: v1.ProxyBackend{
					LocalIP:   localIP,
					LocalPort: mapping.LocalPort,
				},
			},
			RemotePort: mapping.RemotePort,
		})
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("没有启用的 frp 端口映射")
	}
	if err := configSource.ReplaceAll(proxies, nil); err != nil {
		return nil, fmt.Errorf("生成 frp 代理配置失败: %w", err)
	}

	return frpclient.NewService(frpclient.ServiceOptions{
		Common:                 common,
		ConfigSourceAggregator: source.NewAggregator(configSource),
		UnsafeFeatures:         security.NewUnsafeFeatures(nil),
	})
}
