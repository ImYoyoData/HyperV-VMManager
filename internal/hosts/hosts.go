package hosts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LocalID 是本机连接的固定 ID。
const LocalID = "local"

// Profile 是持久化的 Hyper-V 主机配置（不包含密码）。
type Profile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Address         string `json:"address,omitempty"` // 空表示本机
	Port            int    `json:"port"`
	UseSSL          bool   `json:"useSSL"`
	Username        string `json:"username,omitempty"`
	Authentication  string `json:"authentication,omitempty"`
	LastConnectedAt string `json:"lastConnectedAt,omitempty"`
}

// Item 是返回给前端 / API 的主机视图。
type Item struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Address         string `json:"address"`
	Port            int    `json:"port"`
	UseSSL          bool   `json:"useSSL"`
	Username        string `json:"username"`
	Authentication  string `json:"authentication"`
	IsLocal         bool   `json:"isLocal"`
	Connected       bool   `json:"connected"`
	Active          bool   `json:"active"`
	PasswordSaved   bool   `json:"passwordSaved"`
	LastConnectedAt string `json:"lastConnectedAt,omitempty"`
}

// Request 是新增 / 编辑 / 连接主机时的输入。
type Request struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	Address        string `json:"address,omitempty"`
	Port           int    `json:"port"`
	UseSSL         bool   `json:"useSSL"`
	Username       string `json:"username,omitempty"`
	Authentication string `json:"authentication,omitempty"`
	Password       string `json:"password,omitempty"` // 仅连接时使用，不落盘
}

// Store 管理主机配置（%AppData%\HyperV-VMManager\hosts.json）。
type Store struct {
	mu       sync.RWMutex
	path     string
	profiles []Profile
}

func NewStore(path string) (*Store, error) {
	store := &Store{path: path}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		s.profiles = []Profile{defaultLocal()}
		return s.persist()
	}

	var profiles []Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		_ = os.Rename(s.path, s.path+".corrupt")
		s.profiles = []Profile{defaultLocal()}
		return s.persist()
	}

	normalized := make([]Profile, 0, len(profiles)+1)
	hasLocal := false
	for _, profile := range profiles {
		profile = normalizeProfile(profile)
		if profile.ID == LocalID {
			if hasLocal {
				continue
			}
			hasLocal = true
			profile.Address = ""
			profile.Port = 0
			profile.UseSSL = false
			profile.Username = ""
			profile.Authentication = ""
			if profile.Name == "" {
				profile.Name = "本机"
			}
		}
		if profile.Name == "" {
			continue
		}
		normalized = append(normalized, profile)
	}
	if !hasLocal {
		normalized = append([]Profile{defaultLocal()}, normalized...)
	}
	s.profiles = normalized
	return s.persist()
}

func defaultLocal() Profile {
	return Profile{ID: LocalID, Name: "本机"}
}

func normalizeProfile(profile Profile) Profile {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Address = strings.TrimSpace(profile.Address)
	profile.Username = strings.TrimSpace(profile.Username)
	profile.Authentication = strings.TrimSpace(profile.Authentication)
	if profile.ID != LocalID {
		if profile.UseSSL {
			if profile.Port == 0 {
				profile.Port = 5986
			}
		} else if profile.Port == 0 {
			profile.Port = 5985
		}
	}
	return profile
}

// List 返回所有主机配置的副本。
func (s *Store) List() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Profile, len(s.profiles))
	copy(result, s.profiles)
	return result
}

// Get 按 ID 读取主机配置。
func (s *Store) Get(id string) (*Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, profile := range s.profiles {
		if profile.ID == id {
			cp := profile
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("主机不存在: %s", id)
}

// Add 新增远程主机配置。
func (s *Store) Add(request Request) (Profile, error) {
	profile, err := profileFromRequest(request)
	if err != nil {
		return Profile{}, err
	}
	profile.ID = newHostID()
	profile.LastConnectedAt = ""

	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = append(s.profiles, profile)
	if err := s.persistLocked(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// Update 修改主机配置；本机只允许改名称。
func (s *Store) Update(request Request) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := -1
	for i, profile := range s.profiles {
		if profile.ID == request.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return Profile{}, fmt.Errorf("主机不存在: %s", request.ID)
	}

	current := s.profiles[index]
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Profile{}, errors.New("请输入主机名称")
	}
	current.Name = name

	if current.ID != LocalID {
		profile, err := profileFromRequest(request)
		if err != nil {
			return Profile{}, err
		}
		profile.ID = current.ID
		profile.LastConnectedAt = current.LastConnectedAt
		current = profile
	}

	s.profiles[index] = current
	if err := s.persistLocked(); err != nil {
		return Profile{}, err
	}
	return current, nil
}

// Remove 删除远程主机；本机不可删除。
func (s *Store) Remove(id string) error {
	if id == LocalID {
		return errors.New("本机连接不能删除")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, profile := range s.profiles {
		if profile.ID == id {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			return s.persistLocked()
		}
	}
	return fmt.Errorf("主机不存在: %s", id)
}

// MarkConnected 记录最近一次成功连接时间。
func (s *Store) MarkConnected(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			s.profiles[i].LastConnectedAt = time.Now().Format(time.RFC3339)
			return s.persistLocked()
		}
	}
	return fmt.Errorf("主机不存在: %s", id)
}

func profileFromRequest(request Request) (Profile, error) {
	if request.ID == LocalID || strings.TrimSpace(request.Address) == "" {
		return Profile{}, errors.New("远程主机必须填写计算机名或 IP 地址")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = strings.TrimSpace(request.Address)
	}
	profile := normalizeProfile(Profile{
		ID:             request.ID,
		Name:           name,
		Address:        strings.TrimSpace(request.Address),
		Port:           request.Port,
		UseSSL:         request.UseSSL,
		Username:       strings.TrimSpace(request.Username),
		Authentication: strings.TrimSpace(request.Authentication),
	})
	if profile.Port < 1 || profile.Port > 65535 {
		return Profile{}, errors.New("WinRM 端口必须在 1 到 65535 之间")
	}
	if profile.Address == "" {
		return Profile{}, errors.New("请输入计算机名或 IP 地址")
	}
	return profile, nil
}

func newHostID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return "host-" + hex.EncodeToString(buf)
}

func (s *Store) persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
