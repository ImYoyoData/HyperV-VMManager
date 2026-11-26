package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// API Key 权限等级。
const (
	PermissionRead  = "read"
	PermissionWrite = "write"
	PermissionAdmin = "admin"
)

// APIConfig 是旧版单 Token API 配置，仅用于配置迁移。
type APIConfig struct {
	Enabled       bool   `json:"enabled"`
	ListenAddress string `json:"listenAddress"`
	Port          int    `json:"port"`
	Token         string `json:"token"`
}

// WebConfig 是内置网页控制台 / HTTP API 的配置。
type WebConfig struct {
	Enabled       bool   `json:"enabled"`
	ListenAddress string `json:"listenAddress"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

// TunnelConfig 是内置 frp 客户端（内网穿透）的配置。
// frp 以 Go 库的形式编译进应用，不需要额外安装 frpc.exe。
type TunnelConfig struct {
	Enabled    bool            `json:"enabled"`
	ServerAddr string          `json:"serverAddr"`
	ServerPort int             `json:"serverPort"`
	Token      string          `json:"token"`
	Protocol   string          `json:"protocol"` // tcp | kcp | quic | websocket | wss
	ProxyName  string          `json:"proxyName"`
	RemotePort int             `json:"remotePort"` // 网页控制台在 frps 上的端口，0 表示随机
	Mappings   []TunnelMapping `json:"mappings,omitempty"`
}

// TunnelMapping 是一条额外的 TCP 端口映射，可指向虚拟机内的服务端口。
type TunnelMapping struct {
	ID         string `json:"id,omitempty"`
	Enabled    bool   `json:"enabled"`
	Name       string `json:"name"`
	LocalIP    string `json:"localIP"` // 目标 IP，默认 127.0.0.1
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"` // 0 表示由 frps 随机分配
}

// APIKey 是供自动化脚本使用的 API Key。
type APIKey struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Token      string   `json:"token"`
	Permission string   `json:"permission"` // read | write | admin
	HostIDs    []string `json:"hostIDs,omitempty"`
	CreatedAt  string   `json:"createdAt,omitempty"`
	LastUsedAt string   `json:"lastUsedAt,omitempty"`
}

// Settings 是应用级配置，对应原 TUI 脚本的 $defaults。
type Settings struct {
	StorageRoot            string       `json:"storageRoot"`
	LogDir                 string       `json:"logDir"`
	DefaultGeneration      int          `json:"defaultGeneration"`
	DefaultMemoryStartupMB int64        `json:"defaultMemoryStartupMB"`
	DefaultDynamicMemory   bool         `json:"defaultDynamicMemory"`
	DefaultMemoryMinimumMB int64        `json:"defaultMemoryMinimumMB"`
	DefaultMemoryMaximumMB int64        `json:"defaultMemoryMaximumMB"`
	DefaultCPUCount        int          `json:"defaultCPUCount"`
	DefaultBandwidthMbps   int          `json:"defaultBandwidthMbps"`
	AutoRefreshSeconds     int          `json:"autoRefreshSeconds"`
	Theme                  string       `json:"theme"`         // system | light | dark
	CloseBehavior          string       `json:"closeBehavior"` // ask | close | tray
	AutoStart              bool         `json:"autoStart"`
	Web                    WebConfig    `json:"web"`
	Tunnel                 TunnelConfig `json:"tunnel"`
	APIKeys                []APIKey     `json:"apiKeys"`
	API                    APIConfig    `json:"api,omitempty"` // 旧版字段，仅迁移使用
}

// Defaults 返回默认配置。网页控制台默认开启，账号 admin，密码随机生成。
func Defaults() Settings {
	return Settings{
		StorageRoot:            `D:\VMs`,
		DefaultGeneration:      2,
		DefaultMemoryStartupMB: 4096,
		DefaultDynamicMemory:   false,
		DefaultMemoryMinimumMB: 1024,
		DefaultMemoryMaximumMB: 8192,
		DefaultCPUCount:        4,
		DefaultBandwidthMbps:   100,
		AutoRefreshSeconds:     10,
		Theme:                  "system",
		CloseBehavior:          "ask",
		AutoStart:              false,
		Web: WebConfig{
			Enabled:       true,
			ListenAddress: "127.0.0.1",
			Port:          17890,
			Username:      "admin",
			Password:      newSecret(),
		},
		Tunnel: TunnelConfig{
			Enabled:    false,
			ServerPort: 7000,
			Protocol:   "tcp",
			ProxyName:  "hvvm-web",
			RemotePort: 17890,
		},
		APIKeys: []APIKey{defaultAPIKey()},
	}
}

func defaultAPIKey() APIKey {
	return APIKey{
		ID:         newKeyID(),
		Name:       "默认管理员 Key",
		Token:      newToken(),
		Permission: PermissionAdmin,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
}

func newToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func newSecret() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func newKeyID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return "key-" + hex.EncodeToString(buf)
}

// Store 负责设置的加载与保存。
type Store struct {
	mu       sync.RWMutex
	path     string
	dataDir  string
	settings Settings
}

// NewStore 打开（或创建）设置文件。
func NewStore() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(configDir, "HyperV-VMManager")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	store := &Store{
		path:     filepath.Join(dataDir, "settings.json"),
		dataDir:  dataDir,
		settings: Defaults(),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	// 日志与任务历史优先放到非系统盘；只有 C 盘时退回 AppData。
	if strings.TrimSpace(store.settings.LogDir) == "" {
		store.settings.LogDir = defaultLogDir(dataDir)
		if err := store.persist(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(store.settings.LogDir, 0o755); err != nil {
		store.settings.LogDir = filepath.Join(dataDir, "logs")
		if err := os.MkdirAll(store.settings.LogDir, 0o755); err != nil {
			return nil, err
		}
		if err := store.persist(); err != nil {
			return nil, err
		}
	}
	// 旧版本的任务历史原来放在 AppData，首次迁移到新的日志目录。
	legacyTasks := filepath.Join(dataDir, "tasks.json")
	newTasks := filepath.Join(store.settings.LogDir, "tasks.json")
	if _, err := os.Stat(newTasks); os.IsNotExist(err) {
		if data, readErr := os.ReadFile(legacyTasks); readErr == nil {
			_ = os.WriteFile(newTasks, data, 0o600)
		}
	}
	return store, nil
}

// defaultLogDir 选择第一个可写的非系统盘；没有其他盘时使用 AppData\logs。
func defaultLogDir(dataDir string) string {
	for _, letter := range []string{"D", "E", "F", "G", "H", "I", "J"} {
		root := letter + ":\\"
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		dir := filepath.Join(root, "HyperV-VMManager", "logs")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	dir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.persist()
		}
		return err
	}

	// 旧版字段检测，用于把单个 API Token 迁移成默认 Key、沿用端口。
	var legacy struct {
		API     *APIConfig `json:"api"`
		Web     *WebConfig `json:"web"`
		APIKeys *[]APIKey  `json:"apiKeys"`
	}
	_ = json.Unmarshal(data, &legacy)

	loaded := Defaults()
	if err := json.Unmarshal(data, &loaded); err != nil {
		_ = os.Rename(s.path, s.path+".corrupt")
		return s.persist()
	}

	if legacy.Web == nil && legacy.API != nil {
		loaded.Web.Enabled = true
		if strings.TrimSpace(legacy.API.ListenAddress) != "" {
			loaded.Web.ListenAddress = legacy.API.ListenAddress
		}
		if legacy.API.Port > 0 {
			loaded.Web.Port = legacy.API.Port
		}
		if legacy.APIKeys == nil && strings.TrimSpace(legacy.API.Token) != "" {
			key := defaultAPIKey()
			key.Name = "默认管理员 Key"
			key.Token = strings.TrimSpace(legacy.API.Token)
			loaded.APIKeys = []APIKey{key}
		}
	}
	loaded.API = APIConfig{}

	loaded = normalize(loaded)
	s.settings = loaded
	return s.persist()
}

// Get 返回当前设置的副本。
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.settings
	cp.APIKeys = append([]APIKey(nil), s.settings.APIKeys...)
	for i := range cp.APIKeys {
		cp.APIKeys[i].HostIDs = append([]string(nil), cp.APIKeys[i].HostIDs...)
	}
	return cp
}

// Save 规范化并保存设置。
// 网页端提交的脱敏设置不会携带密码和 API Key，这里会用当前值补齐，
// 避免普通设置保存操作把密钥清空。
func (s *Store) Save(value Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(value.Web.Password) == "" {
		value.Web.Password = s.settings.Web.Password
	}
	if strings.TrimSpace(value.LogDir) == "" {
		value.LogDir = s.settings.LogDir
	}
	if strings.TrimSpace(value.Tunnel.Token) == "" {
		value.Tunnel.Token = s.settings.Tunnel.Token
	}
	if value.APIKeys == nil {
		value.APIKeys = s.settings.APIKeys
	}
	if value.API.Token == "" {
		value.API.Token = s.settings.API.Token
	}

	value = normalize(value)
	s.settings = value
	if err := s.persistLocked(); err != nil {
		return Settings{}, err
	}
	return s.GetLockedCopy(), nil
}

// Update 在当前设置上做原子修改（用于 API Key 管理）。
func (s *Store) Update(mutate func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.settings
	mutate(&current)
	current = normalize(current)
	s.settings = current
	if err := s.persistLocked(); err != nil {
		return Settings{}, err
	}
	return s.GetLockedCopy(), nil
}

func (s *Store) GetLockedCopy() Settings {
	cp := s.settings
	cp.APIKeys = append([]APIKey(nil), s.settings.APIKeys...)
	for i := range cp.APIKeys {
		cp.APIKeys[i].HostIDs = append([]string(nil), cp.APIKeys[i].HostIDs...)
	}
	return cp
}

// DataDir 返回应用数据目录。
func (s *Store) DataDir() string { return s.dataDir }

// Path 返回设置文件路径。
func (s *Store) Path() string { return s.path }

// TasksPath 返回任务历史文件路径（优先放在日志目录/非系统盘）。
func (s *Store) TasksPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := s.settings.LogDir
	if strings.TrimSpace(dir) == "" {
		dir = s.dataDir
	}
	return filepath.Join(dir, "tasks.json")
}

// HostsPath 返回主机连接配置文件路径。
func (s *Store) HostsPath() string { return filepath.Join(s.dataDir, "hosts.json") }

func (s *Store) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func normalize(value Settings) Settings {
	value.StorageRoot = strings.TrimSpace(value.StorageRoot)
	value.LogDir = strings.TrimSpace(value.LogDir)
	if value.DefaultGeneration != 1 && value.DefaultGeneration != 2 {
		value.DefaultGeneration = 2
	}
	if value.DefaultMemoryStartupMB < 256 {
		value.DefaultMemoryStartupMB = 4096
	}
	if value.DefaultMemoryMinimumMB < 256 {
		value.DefaultMemoryMinimumMB = 1024
	}
	if value.DefaultMemoryMaximumMB < value.DefaultMemoryStartupMB {
		value.DefaultMemoryMaximumMB = 8192
	}
	if value.DefaultCPUCount < 1 || value.DefaultCPUCount > 128 {
		value.DefaultCPUCount = 4
	}
	if value.DefaultBandwidthMbps < 0 {
		value.DefaultBandwidthMbps = 0
	}
	if value.AutoRefreshSeconds < 2 || value.AutoRefreshSeconds > 300 {
		value.AutoRefreshSeconds = 10
	}
	value.Theme = strings.ToLower(strings.TrimSpace(value.Theme))
	switch value.Theme {
	case "system", "light", "dark":
	default:
		value.Theme = "system"
	}
	value.CloseBehavior = strings.ToLower(strings.TrimSpace(value.CloseBehavior))
	switch value.CloseBehavior {
	case "ask", "close", "tray":
	default:
		value.CloseBehavior = "ask"
	}

	value.Web.ListenAddress = strings.TrimSpace(value.Web.ListenAddress)
	if value.Web.ListenAddress == "" {
		value.Web.ListenAddress = "127.0.0.1"
	}
	if value.Web.Port < 1 || value.Web.Port > 65535 {
		value.Web.Port = 17890
	}
	value.Web.Username = strings.TrimSpace(value.Web.Username)
	if value.Web.Username == "" {
		value.Web.Username = "admin"
	}
	value.Web.Password = strings.TrimSpace(value.Web.Password)
	if value.Web.Password == "" {
		value.Web.Password = newSecret()
	}

	value.Tunnel.ServerAddr = strings.TrimSpace(value.Tunnel.ServerAddr)
	value.Tunnel.Token = strings.TrimSpace(value.Tunnel.Token)
	if value.Tunnel.ServerPort < 1 || value.Tunnel.ServerPort > 65535 {
		value.Tunnel.ServerPort = 7000
	}
	value.Tunnel.Protocol = strings.ToLower(strings.TrimSpace(value.Tunnel.Protocol))
	switch value.Tunnel.Protocol {
	case "tcp", "kcp", "quic", "websocket", "wss":
	default:
		value.Tunnel.Protocol = "tcp"
	}
	value.Tunnel.ProxyName = strings.TrimSpace(value.Tunnel.ProxyName)
	if value.Tunnel.ProxyName == "" {
		value.Tunnel.ProxyName = "hvvm-web"
	}
	if value.Tunnel.RemotePort < 0 || value.Tunnel.RemotePort > 65535 {
		value.Tunnel.RemotePort = 0
	}
	mappings := make([]TunnelMapping, 0, len(value.Tunnel.Mappings))
	seenMappings := map[string]bool{}
	for _, mapping := range value.Tunnel.Mappings {
		mapping.Name = strings.TrimSpace(mapping.Name)
		mapping.LocalIP = strings.TrimSpace(mapping.LocalIP)
		if mapping.LocalIP == "" {
			mapping.LocalIP = "127.0.0.1"
		}
		if mapping.LocalPort < 1 || mapping.LocalPort > 65535 {
			continue
		}
		if mapping.RemotePort < 0 || mapping.RemotePort > 65535 {
			continue
		}
		if mapping.Name == "" {
			mapping.Name = fmt.Sprintf("map-%d", len(mappings)+1)
		}
		if seenMappings[mapping.Name] {
			continue
		}
		if mapping.ID == "" {
			mapping.ID = fmt.Sprintf("map-%d-%d", time.Now().UnixNano(), len(mappings))
		}
		seenMappings[mapping.Name] = true
		mappings = append(mappings, mapping)
	}
	value.Tunnel.Mappings = mappings

	seen := map[string]bool{}
	keys := make([]APIKey, 0, len(value.APIKeys))
	for _, key := range value.APIKeys {
		key.ID = strings.TrimSpace(key.ID)
		if key.ID == "" {
			key.ID = newKeyID()
		}
		if seen[key.ID] {
			continue
		}
		seen[key.ID] = true
		key.Name = strings.TrimSpace(key.Name)
		if key.Name == "" {
			key.Name = "API Key"
		}
		key.Token = strings.TrimSpace(key.Token)
		if key.Token == "" {
			key.Token = newToken()
		}
		key.Permission = strings.ToLower(strings.TrimSpace(key.Permission))
		switch key.Permission {
		case PermissionRead, PermissionWrite, PermissionAdmin:
		default:
			key.Permission = PermissionRead
		}
		key.CreatedAt = strings.TrimSpace(key.CreatedAt)
		if key.CreatedAt == "" {
			key.CreatedAt = time.Now().Format(time.RFC3339)
		}
		key.HostIDs = normalizeHostIDs(key.HostIDs)
		keys = append(keys, key)
	}
	value.APIKeys = keys
	return value
}

func normalizeHostIDs(ids []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

// Masked 返回可安全暴露给网页前端的设置副本。
func Masked(value Settings) Settings {
	cp := value
	cp.Web.Password = ""
	cp.Tunnel.Token = ""
	cp.APIKeys = nil
	cp.API = APIConfig{}
	return cp
}
