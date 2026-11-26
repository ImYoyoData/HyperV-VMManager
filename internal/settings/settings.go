package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// APIConfig 是内置 HTTP API 的配置。
type APIConfig struct {
	Enabled       bool   `json:"enabled"`
	ListenAddress string `json:"listenAddress"`
	Port          int    `json:"port"`
	Token         string `json:"token"`
}

// Settings 是应用级配置，对应原 TUI 脚本的 $defaults。
type Settings struct {
	StorageRoot            string    `json:"storageRoot"`
	DefaultGeneration      int       `json:"defaultGeneration"`
	DefaultMemoryStartupMB int64     `json:"defaultMemoryStartupMB"`
	DefaultDynamicMemory   bool      `json:"defaultDynamicMemory"`
	DefaultMemoryMinimumMB int64     `json:"defaultMemoryMinimumMB"`
	DefaultMemoryMaximumMB int64     `json:"defaultMemoryMaximumMB"`
	DefaultCPUCount        int       `json:"defaultCPUCount"`
	DefaultBandwidthMbps   int       `json:"defaultBandwidthMbps"`
	AutoRefreshSeconds     int       `json:"autoRefreshSeconds"`
	API                    APIConfig `json:"api"`
}

// Defaults 返回默认配置。
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
		API: APIConfig{
			Enabled:       false,
			ListenAddress: "127.0.0.1",
			Port:          17890,
			Token:         newToken(),
		},
	}
}

func newToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
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
	return store, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.persist()
		}
		return err
	}
	var loaded Settings
	// 先填充默认值，再覆盖，保证旧版本配置文件缺少的新字段有默认值。
	loaded = Defaults()
	if err := json.Unmarshal(data, &loaded); err != nil {
		_ = os.Rename(s.path, s.path+".corrupt")
		return s.persist()
	}
	loaded = normalize(loaded)
	s.settings = loaded
	if loaded.API.Token == "" {
		loaded.API.Token = newToken()
		s.settings = loaded
	}
	return s.persist()
}

// Get 返回当前设置的副本。
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.settings
	return cp
}

// Save 规范化并保存设置。
func (s *Store) Save(value Settings) (Settings, error) {
	value = normalize(value)
	if value.API.Token == "" {
		value.API.Token = newToken()
	}

	s.mu.Lock()
	s.settings = value
	err := s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return Settings{}, err
	}
	cp := value
	return cp, nil
}

// DataDir 返回应用数据目录。
func (s *Store) DataDir() string { return s.dataDir }

// Path 返回设置文件路径。
func (s *Store) Path() string { return s.path }

// TasksPath 返回任务历史文件路径。
func (s *Store) TasksPath() string { return filepath.Join(s.dataDir, "tasks.json") }

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
	value.API.ListenAddress = strings.TrimSpace(value.API.ListenAddress)
	if value.API.ListenAddress == "" {
		value.API.ListenAddress = "127.0.0.1"
	}
	if value.API.Port < 1 || value.API.Port > 65535 {
		value.API.Port = 17890
	}
	value.API.Token = strings.TrimSpace(value.API.Token)
	return value
}
