package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hypervvmmanager/internal/applog"
	"hypervvmmanager/internal/credentials"
	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/httpapi"
	"hypervvmmanager/internal/hyperv"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
	"hypervvmmanager/internal/tunnel"
)

const (
	appVersion   = "0.3.0"
	wailsVersion = "3.1.0"
)

// Bootstrap 是前端启动时一次性获取的聚合数据。
type Bootstrap struct {
	Settings     settings.Settings `json:"settings"`
	HostInfo     *hyperv.HostInfo  `json:"hostInfo,omitempty"`
	HostError    string            `json:"hostError,omitempty"`
	VMError      string            `json:"vmError,omitempty"`
	VMCount      int               `json:"vmCount"`
	RunningCount int               `json:"runningCount"`
	IsAdmin      bool              `json:"isAdmin"`
	DataDir      string            `json:"dataDir"`
	API          httpapi.Status    `json:"api"`
	Tunnel       tunnel.Status     `json:"tunnel"`
	TaskStats    tasks.Stats       `json:"taskStats"`
	ActiveHost   *hosts.Item       `json:"activeHost,omitempty"`
	AppVersion   string            `json:"appVersion"`
	WailsVersion string            `json:"wailsVersion"`
}

// HyperVService 是 Wails 绑定门面，同时实现 httpapi.Backend。
type HyperVService struct {
	queue     *tasks.Queue
	settings  *settings.Store
	hostStore *hosts.Store
	api       *httpapi.Server
	tunnel    *tunnel.Runner

	mu           sync.RWMutex
	clients      map[string]*hyperv.Client
	activeHostID string
}

// New 组装服务并注册任务执行器。
func New(queue *tasks.Queue, store *settings.Store, hostStore *hosts.Store) *HyperVService {
	local := hyperv.NewClientForTarget(hyperv.LocalTarget())
	s := &HyperVService{
		queue:        queue,
		settings:     store,
		hostStore:    hostStore,
		tunnel:       tunnel.New(),
		clients:      map[string]*hyperv.Client{hosts.LocalID: local},
		activeHostID: hosts.LocalID,
	}
	s.registerHandlers()
	return s
}

// AttachAPI 注入 HTTP API 服务（打破循环依赖）。
func (s *HyperVService) AttachAPI(api *httpapi.Server) {
	s.api = api
}

func (s *HyperVService) currentClient() *hyperv.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if client := s.clients[s.activeHostID]; client != nil {
		return client
	}
	return s.clients[hosts.LocalID]
}

func (s *HyperVService) clientForHost(hostID string) (*hyperv.Client, error) {
	if strings.TrimSpace(hostID) == "" {
		hostID = hosts.LocalID
	}
	s.mu.RLock()
	client := s.clients[hostID]
	s.mu.RUnlock()
	if client != nil {
		return client, nil
	}

	// 连接缓存不存在时，尝试用 Windows 凭据管理器里保存的密码自动连接。
	// 这样新建、克隆、编辑等操作在应用重启后仍然可以直接使用。
	profile, err := s.hostStore.Get(hostID)
	if err != nil {
		return nil, err
	}
	if profile.Address == "" {
		return nil, fmt.Errorf("主机尚未连接: %s", hostID)
	}
	username := profile.Username
	password := ""
	if storedUser, storedPass, ok := credentials.Load(profile.Address); ok {
		username = storedUser
		password = storedPass
	} else if strings.TrimSpace(username) != "" {
		return nil, fmt.Errorf("远程主机 %s 尚未连接，请先在“计算机”页面输入密码连接", profile.Address)
	}

	autoClient := hyperv.NewClientForTarget(hyperv.Target{
		ID:             profile.ID,
		Name:           profile.Name,
		ComputerName:   profile.Address,
		Port:           profile.Port,
		UseSSL:         profile.UseSSL,
		Username:       username,
		Password:       password,
		Authentication: profile.Authentication,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := autoClient.TestConnection(ctx); err != nil {
		return nil, fmt.Errorf("自动连接远程主机 %s 失败: %w", profile.Address, err)
	}

	s.mu.Lock()
	if s.clients[hostID] == nil {
		s.clients[hostID] = autoClient
	}
	client = s.clients[hostID]
	s.mu.Unlock()
	return client, nil
}

func (s *HyperVService) activeHostIDValue() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeHostID == "" {
		return hosts.LocalID
	}
	return s.activeHostID
}

func (s *HyperVService) isRemote() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client := s.clients[s.activeHostID]
	return client != nil && !client.Target().IsLocal()
}

func (s *HyperVService) hostItem(profile hosts.Profile) hosts.Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hosts.Item{
		ID:              profile.ID,
		Name:            profile.Name,
		Address:         profile.Address,
		Port:            profile.Port,
		UseSSL:          profile.UseSSL,
		Username:        profile.Username,
		Authentication:  profile.Authentication,
		IsLocal:         profile.Address == "",
		Connected:       s.clients[profile.ID] != nil,
		Active:          s.activeHostID == profile.ID,
		PasswordSaved:   profile.Address != "" && credentials.Saved(profile.Address),
		LastConnectedAt: profile.LastConnectedAt,
	}
}

// ListHosts 返回主机配置与连接状态。
func (s *HyperVService) ListHosts() ([]hosts.Item, error) {
	profiles := s.hostStore.List()
	items := make([]hosts.Item, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, s.hostItem(profile))
	}
	return items, nil
}

// GetActiveHost 返回当前主机。
func (s *HyperVService) GetActiveHost() (*hosts.Item, error) {
	id := s.activeHostIDValue()
	profile, err := s.hostStore.Get(id)
	if err != nil {
		profile, err = s.hostStore.Get(hosts.LocalID)
		if err != nil {
			return nil, err
		}
	}
	item := s.hostItem(*profile)
	return &item, nil
}

// AddHost 新增远程主机配置（不连接）。
func (s *HyperVService) AddHost(request hosts.Request) (*hosts.Item, error) {
	profile, err := s.hostStore.Add(request)
	if err != nil {
		return nil, err
	}
	item := s.hostItem(profile)
	return &item, nil
}

// UpdateHost 修改远程主机配置。
func (s *HyperVService) UpdateHost(request hosts.Request) (*hosts.Item, error) {
	oldProfile, _ := s.hostStore.Get(request.ID)
	profile, err := s.hostStore.Update(request)
	if err != nil {
		return nil, err
	}
	// 地址变化后旧地址保存的凭据不再适用。
	if oldProfile != nil && oldProfile.Address != "" &&
		!strings.EqualFold(oldProfile.Address, profile.Address) {
		_ = credentials.Delete(oldProfile.Address)
	}
	// 只有连接参数变化时才丢弃已建立的会话；仅修改显示名称不应
	// 让当前主机断开并切回本机。
	wasActive := s.activeHostIDValue() == profile.ID
	if oldProfile == nil || !sameConnectionProfile(*oldProfile, profile) {
		s.mu.Lock()
		delete(s.clients, profile.ID)
		if s.activeHostID == profile.ID {
			s.activeHostID = hosts.LocalID
		}
		s.mu.Unlock()
	}
	item := s.hostItem(profile)
	// 非当前主机的配置变化不需要让所有页面重新加载。
	if wasActive {
		s.emitHostChanged()
	}
	return &item, nil
}

func sameConnectionProfile(a, b hosts.Profile) bool {
	return strings.EqualFold(strings.TrimSpace(a.Address), strings.TrimSpace(b.Address)) &&
		a.Port == b.Port &&
		a.UseSSL == b.UseSSL &&
		strings.EqualFold(strings.TrimSpace(a.Username), strings.TrimSpace(b.Username)) &&
		normalizeAuth(a.Authentication) == normalizeAuth(b.Authentication)
}

func normalizeAuth(value string) string {
	auth := strings.ToLower(strings.TrimSpace(value))
	if auth == "" {
		return "default"
	}
	return auth
}

// RemoveHost 删除远程主机配置并断开缓存。
func (s *HyperVService) RemoveHost(id string) error {
	profile, _ := s.hostStore.Get(id)
	if err := s.hostStore.Remove(id); err != nil {
		return err
	}
	if profile != nil && profile.Address != "" {
		_ = credentials.Delete(profile.Address)
	}
	wasActive := s.activeHostIDValue() == id
	s.mu.Lock()
	delete(s.clients, id)
	if s.activeHostID == id {
		s.activeHostID = hosts.LocalID
	}
	s.mu.Unlock()
	if wasActive {
		s.emitHostChanged()
	}
	return nil
}

// ConnectHost 连接远程主机并设为当前主机；本机直接切换。
// password 为空时会优先使用 Windows 凭据管理器里已保存的密码；
// remember 为 true 时会把本次密码保存到凭据管理器，供下次直接连接和
// VMConnect / Hyper-V 管理器复用。
func (s *HyperVService) ConnectHost(id string, password string, remember bool) (*hosts.Item, error) {
	profile, err := s.hostStore.Get(id)
	if err != nil {
		return nil, err
	}

	if profile.Address == "" {
		s.mu.Lock()
		if s.clients[hosts.LocalID] == nil {
			s.clients[hosts.LocalID] = hyperv.NewClientForTarget(hyperv.LocalTarget())
		}
		s.activeHostID = hosts.LocalID
		s.mu.Unlock()
		item := s.hostItem(*profile)
		s.emitHostChanged()
		return &item, nil
	}

	// 已经连接过且没有提供新密码时，直接切换。
	if password == "" {
		s.mu.RLock()
		existing := s.clients[id]
		s.mu.RUnlock()
		if existing != nil {
			s.mu.Lock()
			s.activeHostID = id
			s.mu.Unlock()
			item := s.hostItem(*profile)
			s.emitHostChanged()
			return &item, nil
		}
	}

	providedPassword := strings.TrimSpace(password) != ""
	username := profile.Username
	if !providedPassword {
		if storedUser, storedPass, ok := credentials.Load(profile.Address); ok {
			username = storedUser
			password = storedPass
		}
	}
	if password == "" && strings.TrimSpace(username) != "" {
		return nil, errors.New("请输入远程主机密码，或在连接时勾选“记住密码”")
	}

	target := hyperv.Target{
		ID:             profile.ID,
		Name:           profile.Name,
		ComputerName:   profile.Address,
		Port:           profile.Port,
		UseSSL:         profile.UseSSL,
		Username:       username,
		Password:       password,
		Authentication: profile.Authentication,
	}
	client := hyperv.NewClientForTarget(target)

	applog.Info("连接主机 %s (%s)，用户 %s", profile.Name, profile.Address, username)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	info, err := client.TestConnection(ctx)
	if err != nil {
		applog.Error("连接主机 %s 失败: %v", profile.Address, err)
		return nil, fmt.Errorf("连接远程主机 %s 失败: %w", profile.Address, err)
	}

	s.mu.Lock()
	s.clients[id] = client
	s.activeHostID = id
	s.mu.Unlock()
	s.hostStore.MarkConnected(id)
	profile.LastConnectedAt = time.Now().Format(time.RFC3339)
	applog.Info("已连接主机 %s (%s)", profile.Name, profile.Address)

	switch {
	case remember && password != "" && strings.TrimSpace(username) != "":
		credUser := username
		// 使用远程主机的本地账户格式，VMConnect / Hyper-V 管理器才能直接用。
		if info != nil && info.ComputerName != "" && info.UserName != "" {
			credUser = info.ComputerName + `\` + info.UserName
		}
		_ = credentials.Save(profile.Address, credUser, password)
	case !remember && providedPassword:
		_ = credentials.Delete(profile.Address)
	}

	item := s.hostItem(*profile)
	s.emitHostChanged()
	return &item, nil
}

// ForgetHostCredential 删除远程主机在 Windows 凭据管理器中保存的密码。
func (s *HyperVService) ForgetHostCredential(id string) error {
	profile, err := s.hostStore.Get(id)
	if err != nil {
		return err
	}
	if profile.Address == "" {
		return nil
	}
	return credentials.Delete(profile.Address)
}

// FixRemoteWinRM 在本机修复 WinRM 客户端配置：启动 WinRM 服务并把远程主机
// 加入 TrustedHosts。非管理员运行时会弹出 UAC 请求提权。
func (s *HyperVService) FixRemoteWinRM(host string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return hyperv.FixClientWinRM(ctx, host)
}

// SaveHostCredential 把远程主机凭据写入 Windows 凭据管理器，供 VMConnect 与
// Hyper-V 管理器复用；密码不会写入应用设置文件。
func (s *HyperVService) SaveHostCredential(host, username, password string) error {
	return hyperv.SaveHostCredential(host, username, password)
}

// SetActiveHost 切换到已连接的主机。
func (s *HyperVService) SetActiveHost(id string) (*hosts.Item, error) {
	profile, err := s.hostStore.Get(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.clients[id] == nil {
		s.mu.Unlock()
		return nil, errors.New("该主机尚未连接，请先连接")
	}
	s.activeHostID = id
	s.mu.Unlock()
	item := s.hostItem(*profile)
	s.emitHostChanged()
	return &item, nil
}

func (s *HyperVService) emitHostChanged() {
	app := application.Get()
	if app == nil {
		return
	}
	if item, err := s.GetActiveHost(); err == nil {
		app.Event.Emit("host:changed", item)
	}
}

// ServiceStartup 由 Wails 在应用启动时调用。
func (s *HyperVService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if app := application.Get(); app != nil {
		s.queue.SetEmitter(func(task tasks.Task) {
			app.Event.Emit("task:update", task)
			// 只在任务开始、结束或失败时写应用日志，进度刷新不重复刷盘。
			if task.Status != tasks.StatusRunning || task.Progress <= 1 || strings.TrimSpace(task.Error) != "" {
				applog.Info("任务 %s [%s] 主机=%s 目标=%s 状态=%s 进度=%d%% 步骤=%s",
					task.ID, task.Kind, task.HostID, task.Target, task.Status, task.Progress, task.Step)
			}
			if strings.TrimSpace(task.Error) != "" {
				applog.Error("任务 %s 失败: %s", task.ID, task.Error)
			}
			if task.Status != tasks.StatusQueued && task.Status != tasks.StatusRunning {
				app.Event.Emit("task:finished", task)
			}
		})
	}
	s.queue.Start(ctx)

	cfg := s.settings.Get()
	_ = applog.Init(cfg.LogDir)
	applog.Info("应用启动，版本 %s，日志目录 %s", appVersion, cfg.LogDir)
	if s.api != nil {
		s.api.Apply(httpapi.Config{
			Enabled:       cfg.Web.Enabled,
			ListenAddress: cfg.Web.ListenAddress,
			Port:          cfg.Web.Port,
			Username:      cfg.Web.Username,
			Password:      cfg.Web.Password,
			APIKeys:       cfg.APIKeys,
		})
	}
	s.tunnel.Apply(tunnel.ConfigFromSettings(cfg))
	return nil
}

// GetDataDirectory 返回应用数据目录。
func (s *HyperVService) GetDataDirectory() string {
	return s.settings.DataDir()
}

// OpenLogDir 在资源管理器中打开日志目录。
func (s *HyperVService) OpenLogDir() error {
	dir := s.settings.Get().LogDir
	if strings.TrimSpace(dir) == "" {
		return errors.New("日志目录未配置")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("日志目录无法创建: %w", err)
	}
	cmd := exec.Command("explorer.exe", dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}

// GetVersions 返回应用与 Wails 版本。
func (s *HyperVService) GetVersions() (string, string) {
	return appVersion, wailsVersion
}

// ServiceShutdown 由 Wails 在应用关闭时调用。
func (s *HyperVService) ServiceShutdown() error {
	applog.Info("应用退出")
	s.queue.Stop()
	s.tunnel.Stop()
	if s.api != nil {
		s.api.Shutdown()
	}
	applog.Close()
	return nil
}

func (s *HyperVService) registerHandlers() {
	s.queue.Register(tasks.KindClone, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var task cloneTaskPayload
		if err := json.Unmarshal(payload, &task); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		client, err := s.clientForHost(task.HostID)
		if err != nil {
			return nil, err
		}
		req := task.Request
		rep.Log("克隆源虚拟机 %s -> %s", req.SourceName, req.NewName)
		if err := client.Clone(ctx, req, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.NewName, "host": task.HostID}, nil
	})

	s.queue.Register(tasks.KindCreate, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var task createTaskPayload
		if err := json.Unmarshal(payload, &task); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		client, err := s.clientForHost(task.HostID)
		if err != nil {
			return nil, err
		}
		req := task.Request
		rep.Log("新建虚拟机 %s（第 %d 代）", req.Name, req.Generation)
		if err := client.Create(ctx, req, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "host": task.HostID}, nil
	})

	s.queue.Register(tasks.KindEdit, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req editPayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		client, err := s.clientForHost(req.HostID)
		if err != nil {
			return nil, err
		}
		rep.Log("编辑虚拟机 %s", req.Name)
		if err := client.Update(ctx, req.Name, req.Request, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "host": req.HostID}, nil
	})

	s.queue.Register(tasks.KindDelete, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req deletePayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		client, err := s.clientForHost(req.HostID)
		if err != nil {
			return nil, err
		}
		rep.Log("删除虚拟机 %s", req.Name)
		if err := client.Delete(ctx, req.Name, req.DeleteFiles, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "deletedFiles": req.DeleteFiles, "host": req.HostID}, nil
	})

	s.queue.Register(tasks.KindPower, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req powerPayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		client, err := s.clientForHost(req.HostID)
		if err != nil {
			return nil, err
		}
		action := hyperv.PowerAction(req.Action)
		rep.Log("%s虚拟机 %s", hyperv.PowerActionLabel(action), req.Name)
		if err := client.Power(ctx, req.Name, action); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "action": req.Action, "host": req.HostID}, nil
	})
}

type cloneTaskPayload struct {
	HostID  string              `json:"hostID"`
	Request hyperv.CloneRequest `json:"request"`
}

type createTaskPayload struct {
	HostID  string               `json:"hostID"`
	Request hyperv.CreateRequest `json:"request"`
}

type editPayload struct {
	HostID  string             `json:"hostID"`
	Name    string             `json:"name"`
	Request hyperv.EditRequest `json:"request"`
}

type deletePayload struct {
	HostID      string `json:"hostID"`
	Name        string `json:"name"`
	DeleteFiles bool   `json:"deleteFiles"`
}

type powerPayload struct {
	HostID string `json:"hostID"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

// GetBootstrap 返回首屏聚合数据。
func (s *HyperVService) GetBootstrap() (*Bootstrap, error) {
	current := s.settings.Get()
	bootstrap := &Bootstrap{
		Settings:     current,
		IsAdmin:      hyperv.IsAdmin(),
		DataDir:      s.settings.DataDir(),
		Tunnel:       s.tunnel.Status(),
		TaskStats:    s.queue.Stats(),
		AppVersion:   appVersion,
		WailsVersion: wailsVersion,
	}
	if s.api != nil {
		bootstrap.API = s.api.Status()
	}
	if active, err := s.GetActiveHost(); err == nil {
		bootstrap.ActiveHost = active
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		info, err := s.currentClient().HostInfo(ctx)
		if err != nil {
			bootstrap.HostError = err.Error()
			return
		}
		bootstrap.HostInfo = info
	}()
	go func() {
		defer wg.Done()
		vms, err := s.currentClient().ListVMSummaries(ctx)
		if err != nil {
			bootstrap.VMError = err.Error()
			return
		}
		bootstrap.VMCount = len(vms)
		for _, vm := range vms {
			if vm.State == "Running" {
				bootstrap.RunningCount++
			}
		}
	}()
	wg.Wait()
	return bootstrap, nil
}

// GetHostInfo 读取主机信息。
func (s *HyperVService) GetHostInfo() (*hyperv.HostInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.currentClient().HostInfo(ctx)
}

// SetLocalUserPassword 通过当前 WinRM 连接修改远程主机本地账户密码。
func (s *HyperVService) SetLocalUserPassword(req hyperv.SetLocalUserPasswordRequest) error {
	client := s.currentClient()
	if client.Target().IsLocal() {
		return errors.New("只支持修改远程主机的本地账户密码，本机账户请使用 Windows 设置或 net user")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return client.SetLocalUserPassword(ctx, req)
}

// GetEnvironmentStatus 检查 Windows 版本、虚拟化、Hyper-V 组件与增强会话。
func (s *HyperVService) GetEnvironmentStatus() (*hyperv.EnvironmentStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return s.currentClient().EnvironmentStatus(ctx)
}

// ListVMs 返回虚拟机列表。
func (s *HyperVService) ListVMs() ([]hyperv.VM, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.currentClient().ListVMs(ctx)
}

// ListVMSummaries 返回轻量虚拟机列表，供自动刷新和仪表盘统计使用。
func (s *HyperVService) ListVMSummaries() ([]hyperv.VM, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.currentClient().ListVMSummaries(ctx)
}

// ListVMsForHost 返回指定主机的虚拟机列表（用于新建/克隆时选择目标计算机）。
func (s *HyperVService) ListVMsForHost(hostID string) ([]hyperv.VM, error) {
	client, err := s.clientForHost(hostID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return client.ListVMs(ctx)
}

// GetHostCapacity 汇总指定主机的 CPU / 内存 / 磁盘分配与剩余情况。
func (s *HyperVService) GetHostCapacity(hostID string) (*hyperv.HostCapacity, error) {
	client, err := s.clientForHost(hostID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 主机信息、虚拟机列表和磁盘容量互不依赖，并行读取可把等待时间
	// 从三次 PowerShell 启动之和缩短到最慢的一次。
	var (
		wg       sync.WaitGroup
		info     *hyperv.HostInfo
		vms      []hyperv.VM
		disks    []hyperv.DiskSpace
		infoErr  error
		vmsError error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		info, infoErr = client.HostInfo(ctx)
	}()
	go func() {
		defer wg.Done()
		vms, vmsError = client.ListVMSummaries(ctx)
	}()
	go func() {
		defer wg.Done()
		disks, _ = client.DiskSpaces(ctx)
	}()
	wg.Wait()

	if infoErr != nil {
		return nil, infoErr
	}
	if vmsError != nil {
		return nil, vmsError
	}
	capacity := &hyperv.HostCapacity{
		HostID:            hostID,
		Name:              info.ComputerName,
		IsLocal:           client.Target().IsLocal(),
		LogicalProcessors: info.LogicalProcessors,
		TotalMemoryMB:     info.TotalMemoryMB,
	}
	for _, vm := range vms {
		capacity.AllocatedCPU += vm.ProcessorCount
		capacity.AllocatedMemoryMB += vm.MemoryStartupMB
	}
	capacity.AvailableCPU = capacity.LogicalProcessors - capacity.AllocatedCPU
	capacity.ReservedMemoryMB = capacity.TotalMemoryMB / 5
	if capacity.ReservedMemoryMB < 2048 {
		capacity.ReservedMemoryMB = 2048
	}
	if capacity.ReservedMemoryMB > capacity.TotalMemoryMB {
		capacity.ReservedMemoryMB = capacity.TotalMemoryMB / 2
	}
	capacity.AvailableMemoryMB = capacity.TotalMemoryMB - capacity.ReservedMemoryMB - capacity.AllocatedMemoryMB
	if disks == nil {
		disks = []hyperv.DiskSpace{}
	}
	capacity.Disks = disks
	return capacity, nil
}

// ListSwitchesForHost 返回指定主机的虚拟交换机。
func (s *HyperVService) ListSwitchesForHost(hostID string) ([]hyperv.VirtualSwitch, error) {
	client, err := s.clientForHost(hostID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return client.ListSwitches(ctx)
}

// SuggestCloneDirectoryForHost 返回指定主机上源虚拟机的所在目录。
func (s *HyperVService) SuggestCloneDirectoryForHost(hostID string, sourceName string) (string, error) {
	client, err := s.clientForHost(hostID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	vm, err := client.GetVM(ctx, sourceName)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(vm.Path) == "" {
		return "", errors.New("无法获取源虚拟机路径")
	}
	return filepath.Dir(vm.Path), nil
}

// GetVM 返回单个虚拟机详情。
func (s *HyperVService) GetVM(name string) (*hyperv.VM, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.currentClient().GetVM(ctx, name)
}

// SuggestCloneDirectory 返回源虚拟机所在目录，供"跟随源机"使用。
func (s *HyperVService) SuggestCloneDirectory(sourceName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	vm, err := s.currentClient().GetVM(ctx, sourceName)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(vm.Path) == "" {
		return "", errors.New("无法获取源虚拟机路径")
	}
	return filepath.Dir(vm.Path), nil
}

// SubmitClone 提交克隆任务；hostID 为空时使用当前主机。
func (s *HyperVService) SubmitClone(hostID string, req hyperv.CloneRequest) (*tasks.Task, error) {
	req.SourceName = strings.TrimSpace(req.SourceName)
	req.NewName = strings.TrimSpace(req.NewName)
	req.TargetDirectory = strings.TrimSpace(req.TargetDirectory)
	if req.SourceName == "" {
		return nil, errors.New("请选择源虚拟机")
	}
	if err := hyperv.ValidateName(req.NewName); err != nil {
		return nil, fmt.Errorf("新名称无效: %w", err)
	}
	if strings.EqualFold(req.SourceName, req.NewName) {
		return nil, errors.New("新名称不能与源虚拟机名称相同")
	}
	if err := hyperv.ValidateAbsoluteDirectory(req.TargetDirectory); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateMemory(req.Memory); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateCPU(req.CPUCount); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateBandwidth(req.BandwidthMbps); err != nil {
		return nil, err
	}
	if req.DiskMode == "" {
		req.DiskMode = "copy"
	}
	switch req.DiskMode {
	case "copy", "differencing":
	default:
		return nil, errors.New("克隆磁盘方式必须是 copy 或 differencing")
	}
	req.ExportDirectory = strings.TrimSpace(req.ExportDirectory)
	if req.ExportDirectory != "" {
		if err := hyperv.ValidateAbsoluteDirectory(req.ExportDirectory); err != nil {
			return nil, fmt.Errorf("临时导出目录无效: %w", err)
		}
	}
	resolvedHost, err := s.resolveHost(hostID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNoActiveTask(tasks.KindClone, resolvedHost, req.NewName); err != nil {
		return nil, err
	}
	return s.queue.SubmitForHost(resolvedHost, tasks.KindClone, req.NewName, cloneTaskPayload{HostID: resolvedHost, Request: req})
}

// SubmitCreate 提交新建任务；hostID 为空时使用当前主机。
func (s *HyperVService) SubmitCreate(hostID string, req hyperv.CreateRequest) (*tasks.Task, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Directory = strings.TrimSpace(req.Directory)
	req.ImagePath = strings.TrimSpace(req.ImagePath)
	req.ISOPath = strings.TrimSpace(req.ISOPath)
	if err := hyperv.ValidateName(req.Name); err != nil {
		return nil, fmt.Errorf("虚拟机名称无效: %w", err)
	}
	if err := hyperv.ValidateAbsoluteDirectory(req.Directory); err != nil {
		return nil, err
	}
	if req.Generation != 1 && req.Generation != 2 {
		return nil, errors.New("虚拟机代数必须是 1 或 2")
	}
	if err := hyperv.ValidateMemory(req.Memory); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateCPU(req.CPUCount); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateBandwidth(req.BandwidthMbps); err != nil {
		return nil, err
	}
	if req.DiskMode == "" {
		req.DiskMode = "new"
	}
	switch req.DiskMode {
	case "new":
		if req.DiskSizeGB < 1 {
			return nil, errors.New("空硬盘大小至少 1 GB")
		}
	case "differencing", "copy":
		if strings.TrimSpace(req.ImagePath) == "" {
			return nil, errors.New("请选择系统镜像")
		}
	default:
		return nil, errors.New("未知的磁盘方式")
	}
	req.DiskType = strings.TrimSpace(req.DiskType)
	if req.DiskType == "" {
		req.DiskType = "dynamic"
	}
	switch req.DiskType {
	case "dynamic", "fixed":
	default:
		return nil, errors.New("磁盘类型必须是 dynamic 或 fixed")
	}
	req.DiskDirectory = strings.TrimSpace(req.DiskDirectory)
	if req.DiskDirectory != "" {
		if err := hyperv.ValidateAbsoluteDirectory(req.DiskDirectory); err != nil {
			return nil, fmt.Errorf("虚拟硬盘目录无效: %w", err)
		}
	}
	resolvedHost, err := s.resolveHost(hostID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNoActiveTask(tasks.KindCreate, resolvedHost, req.Name); err != nil {
		return nil, err
	}
	return s.queue.SubmitForHost(resolvedHost, tasks.KindCreate, req.Name, createTaskPayload{HostID: resolvedHost, Request: req})
}

// SubmitEdit 提交编辑任务。
func (s *HyperVService) SubmitEdit(name string, req hyperv.EditRequest) (*tasks.Task, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("虚拟机名称不能为空")
	}
	req.NewName = strings.TrimSpace(req.NewName)
	if req.NewName != "" && !strings.EqualFold(req.NewName, name) {
		if err := hyperv.ValidateName(req.NewName); err != nil {
			return nil, fmt.Errorf("新名称无效: %w", err)
		}
	}
	if req.Memory != nil {
		if err := hyperv.ValidateMemory(*req.Memory); err != nil {
			return nil, err
		}
	}
	if req.CPUCount != nil {
		if err := hyperv.ValidateCPU(*req.CPUCount); err != nil {
			return nil, err
		}
	}
	if req.BandwidthMbps != nil {
		if err := hyperv.ValidateBandwidth(*req.BandwidthMbps); err != nil {
			return nil, err
		}
	}
	if len(req.Disks) > 0 {
		seen := map[string]bool{}
		for _, disk := range req.Disks {
			if strings.TrimSpace(disk.Path) == "" || disk.SizeBytes <= 0 {
				return nil, errors.New("硬盘路径和容量不能为空")
			}
			if seen[disk.Path] {
				return nil, errors.New("同一块硬盘不能重复调整")
			}
			seen[disk.Path] = true
		}
	}
	if req.NewName == "" && req.Memory == nil && req.CPUCount == nil && req.SwitchName == nil && req.BandwidthMbps == nil && req.CheckpointsEnabled == nil && req.NestedVirtualization == nil && len(req.Disks) == 0 &&
		req.Notes == nil && req.AutomaticStartAction == nil && req.AutomaticStartDelay == nil && req.AutomaticStopAction == nil && req.AutomaticCheckpoints == nil &&
		req.ProcessorReserve == nil && req.ProcessorMaximum == nil && req.ProcessorWeight == nil && req.ProcessorMigration == nil && req.HwThreadsPerCore == nil &&
		req.MemoryBuffer == nil && req.MemoryPriority == nil && len(req.NetworkAdapters) == 0 && req.Security == nil && len(req.IntegrationServices) == 0 {
		return nil, errors.New("没有需要修改的内容")
	}
	hostID := s.activeHostIDValue()
	if err := s.ensureNoActiveTask(tasks.KindEdit, hostID, name); err != nil {
		return nil, err
	}
	return s.queue.SubmitForHost(hostID, tasks.KindEdit, name, editPayload{HostID: hostID, Name: name, Request: req})
}

// SubmitDelete 提交删除任务。
func (s *HyperVService) SubmitDelete(name string, deleteFiles bool) (*tasks.Task, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("虚拟机名称不能为空")
	}
	hostID := s.activeHostIDValue()
	if err := s.ensureNoActiveTask(tasks.KindDelete, hostID, name); err != nil {
		return nil, err
	}
	return s.queue.SubmitForHost(hostID, tasks.KindDelete, name, deletePayload{HostID: hostID, Name: name, DeleteFiles: deleteFiles})
}

// SubmitPower 提交电源任务。
func (s *HyperVService) SubmitPower(name string, action string) (*tasks.Task, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("虚拟机名称不能为空")
	}
	switch hyperv.PowerAction(action) {
	case hyperv.PowerStart, hyperv.PowerStop, hyperv.PowerTurnOff, hyperv.PowerSave, hyperv.PowerPause, hyperv.PowerResume, hyperv.PowerRestart:
	default:
		return nil, fmt.Errorf("未知的电源操作: %s", action)
	}
	hostID := s.activeHostIDValue()
	if err := s.ensureNoActiveTask(tasks.KindPower, hostID, name); err != nil {
		return nil, err
	}
	return s.queue.SubmitForHost(hostID, tasks.KindPower, name, powerPayload{HostID: hostID, Name: name, Action: action})
}

func (s *HyperVService) resolveHost(hostID string) (string, error) {
	if strings.TrimSpace(hostID) == "" {
		hostID = s.activeHostIDValue()
	}
	if _, err := s.clientForHost(hostID); err != nil {
		return "", err
	}
	return hostID, nil
}

func (s *HyperVService) ensureNoActiveTask(kind string, hostID string, target string) error {
	if strings.TrimSpace(hostID) == "" {
		hostID = hosts.LocalID
	}
	for _, task := range s.queue.List(200) {
		taskHost := task.HostID
		if taskHost == "" {
			taskHost = hosts.LocalID
		}
		if task.Kind == kind && task.Target == target && taskHost == hostID &&
			(task.Status == tasks.StatusQueued || task.Status == tasks.StatusRunning) {
			return fmt.Errorf("该目标在该主机上已有未完成的同类任务（%s），请等待完成后再提交", task.ID)
		}
	}
	return nil
}

// ListTasks 返回任务列表。
func (s *HyperVService) ListTasks(limit int) ([]tasks.Task, error) {
	return s.queue.List(limit), nil
}

// GetTask 按 ID 查询任务。
func (s *HyperVService) GetTask(id string) (*tasks.Task, error) {
	return s.queue.Get(id)
}

// CancelTask 取消任务。
func (s *HyperVService) CancelTask(id string) error {
	return s.queue.Cancel(id)
}

// ClearFinishedTasks 清理已完成任务。
func (s *HyperVService) ClearFinishedTasks() int {
	return s.queue.ClearFinished()
}

// GetTaskStats 返回任务统计。
func (s *HyperVService) GetTaskStats() tasks.Stats {
	return s.queue.Stats()
}

// ListSwitches 返回虚拟交换机列表。
func (s *HyperVService) ListSwitches() ([]hyperv.VirtualSwitch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.currentClient().ListSwitches(ctx)
}

// ListHostNetworkAdapters 返回可用于外部交换机的物理网卡。
func (s *HyperVService) ListHostNetworkAdapters() ([]hyperv.HostNetworkAdapter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.currentClient().ListHostNetworkAdapters(ctx)
}

// CreateSwitch 创建虚拟交换机。
func (s *HyperVService) CreateSwitch(req hyperv.CreateSwitchRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.currentClient().CreateSwitch(ctx, req)
}

// RemoveSwitch 删除虚拟交换机。
func (s *HyperVService) RemoveSwitch(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.currentClient().RemoveSwitch(ctx, name)
}

// ListNATNetworks 返回主机上的 Windows NAT 实例和端口转发规则。
func (s *HyperVService) ListNATNetworks() ([]hyperv.NATNetwork, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.currentClient().ListNATNetworks(ctx)
}

// CreateNATNetwork 创建内部交换机 + 自建 NAT。
func (s *HyperVService) CreateNATNetwork(req hyperv.CreateNATRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	return s.currentClient().CreateNATNetwork(ctx, req)
}

// RemoveNATNetwork 删除 NAT 实例，保留内部交换机和主机 IP。
func (s *HyperVService) RemoveNATNetwork(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.currentClient().RemoveNATNetwork(ctx, name)
}

// AddNATStaticMapping 新增 NAT 端口转发。
func (s *HyperVService) AddNATStaticMapping(req hyperv.NATStaticMappingRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.currentClient().AddNATStaticMapping(ctx, req)
}

// RemoveNATStaticMapping 删除 NAT 端口转发。
func (s *HyperVService) RemoveNATStaticMapping(req hyperv.RemoveNATStaticMappingRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.currentClient().RemoveNATStaticMapping(ctx, req)
}

// GetEnhancedSessionStatus 读取增强会话状态。
func (s *HyperVService) GetEnhancedSessionStatus() (*hyperv.EnhancedSessionStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.currentClient().EnhancedSessionStatus(ctx)
}

// SetEnhancedSession 开启或关闭增强会话。
func (s *HyperVService) SetEnhancedSession(enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return s.currentClient().SetEnhancedSession(ctx, enabled)
}

// OpenTool 打开系统管理工具。
func (s *HyperVService) OpenTool(tool string) error {
	return s.currentClient().OpenTool(tool)
}

// ConnectVM 打开 VMConnect 控制台。
func (s *HyperVService) ConnectVM(name string) error {
	return s.currentClient().ConnectVM(name)
}

// ConnectVMWithCredential 使用远程主机账户启动 VMConnect。
//
// VMConnect 的 WinRM 查询使用独立进程的网络凭据，不会复用应用自己的
// WinRM 会话；因此远程控制台必须用 LOGON_NETCREDENTIALS_ONLY 启动，
// 让 VMConnect 内部使用这里填写的远程账户，而不是当前 Windows 登录账户。
func (s *HyperVService) ConnectVMWithCredential(name, username, password string) error {
	client := s.currentClient()
	if client.Target().IsLocal() {
		return client.ConnectVM(name)
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		if storedUser, storedPass, ok := credentials.Load(client.Target().ComputerName); ok {
			username = storedUser
			password = storedPass
		}
	}
	if username == "" || password == "" {
		return errors.New("远程 VMConnect 需要远程宿主机的账户和密码；空密码账户默认不能远程登录，请先设置密码")
	}
	return client.ConnectVMWithCredential(name, username, password)
}

// IsAdmin 返回当前是否具备管理员权限。
func (s *HyperVService) IsAdmin() bool {
	return hyperv.IsAdmin()
}

// GetSettings 返回当前设置。
func (s *HyperVService) GetSettings() (*settings.Settings, error) {
	current := s.settings.Get()
	return &current, nil
}

// SaveSettings 保存设置，并按需重启 HTTP API。
func (s *HyperVService) SaveSettings(value settings.Settings) (*settings.Settings, error) {
	if strings.TrimSpace(value.StorageRoot) == "" {
		return nil, errors.New("默认存储目录不能为空")
	}
	if strings.TrimSpace(value.LogDir) == "" {
		return nil, errors.New("日志目录不能为空")
	}
	if err := os.MkdirAll(value.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("日志目录无法创建: %w", err)
	}
	if err := hyperv.ValidateAbsoluteDirectory(value.StorageRoot); err != nil {
		return nil, fmt.Errorf("默认存储目录无效: %w", err)
	}
	memory := hyperv.MemoryOptions{
		StartupMB: value.DefaultMemoryStartupMB,
		Dynamic:   value.DefaultDynamicMemory,
		MinimumMB: value.DefaultMemoryMinimumMB,
		MaximumMB: value.DefaultMemoryMaximumMB,
	}
	if err := hyperv.ValidateMemory(memory); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateCPU(value.DefaultCPUCount); err != nil {
		return nil, err
	}
	if err := hyperv.ValidateBandwidth(value.DefaultBandwidthMbps); err != nil {
		return nil, err
	}
	if value.Web.Port < 1 || value.Web.Port > 65535 {
		return nil, errors.New("网页控制台端口必须在 1 到 65535 之间")
	}
	if strings.TrimSpace(value.Web.Username) == "" {
		return nil, errors.New("网页控制台用户名不能为空")
	}
	if value.Web.Password != "" && len(value.Web.Password) < 6 {
		return nil, errors.New("网页控制台密码至少 6 位；留空表示保持不变")
	}
	if value.Tunnel.Enabled {
		if err := tunnel.Validate(tunnel.ConfigFromSettings(value)); err != nil {
			return nil, err
		}
	}
	previous := s.settings.Get()
	saved, err := s.settings.Save(value)
	if err != nil {
		return nil, err
	}
	_ = applog.Init(saved.LogDir)
	applog.Info("设置已保存，日志目录 %s", saved.LogDir)
	// 只有网页控制台配置真正变化时才重启 HTTP 服务；否则网页端保存
	// 主题等普通设置会因为会话被清空而被迫重新登录。
	if !webEndpointEqual(previous, saved) {
		s.applyWebConfig(saved)
	} else if !webAuthEqual(previous, saved) {
		s.applyWebAuth(saved)
	}
	if !tunnelConfigEqual(previous, saved) {
		s.applyTunnel(saved)
	}
	s.applyAutoStart(saved.AutoStart)
	return &saved, nil
}

func webEndpointEqual(a, b settings.Settings) bool {
	return a.Web.Enabled == b.Web.Enabled &&
		a.Web.ListenAddress == b.Web.ListenAddress &&
		a.Web.Port == b.Web.Port
}

func webAuthEqual(a, b settings.Settings) bool {
	return a.Web.Username == b.Web.Username &&
		a.Web.Password == b.Web.Password &&
		reflect.DeepEqual(a.APIKeys, b.APIKeys)
}

func tunnelConfigEqual(a, b settings.Settings) bool {
	return reflect.DeepEqual(a.Tunnel, b.Tunnel) &&
		a.Web.Enabled == b.Web.Enabled &&
		a.Web.Port == b.Web.Port
}

func (s *HyperVService) applyAutoStart(enabled bool) {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	const name = "HyperV-VMManager"
	if enabled {
		_ = exec.Command("reg", "add", key, "/v", name, "/t", "REG_SZ", "/d", executable, "/f").Run()
		return
	}
	_ = exec.Command("reg", "delete", key, "/v", name, "/f").Run()
}

// APIKeyRequest 是新增/编辑 API Key 的输入。
type APIKeyRequest struct {
	ID         string   `json:"id,omitempty"`
	Name       string   `json:"name"`
	Permission string   `json:"permission"`
	HostIDs    []string `json:"hostIDs,omitempty"`
}

// TunnelMappingRequest 是虚拟机详情里“映射到公网”的输入。
type TunnelMappingRequest struct {
	Name       string `json:"name"`
	LocalIP    string `json:"localIP"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
}

// ListAPIKeys 返回所有 API Key（包含 Token，仅桌面端使用）。
func (s *HyperVService) ListAPIKeys() ([]settings.APIKey, error) {
	current := s.settings.Get()
	return current.APIKeys, nil
}

// CreateAPIKey 新建 API Key。
func (s *HyperVService) CreateAPIKey(request APIKeyRequest) (*settings.APIKey, error) {
	if err := s.validateAPIKeyRequest(request); err != nil {
		return nil, err
	}
	key := settings.APIKey{
		ID:         "key-" + randomHex(4),
		Name:       strings.TrimSpace(request.Name),
		Token:      randomHex(16),
		Permission: request.Permission,
		HostIDs:    request.HostIDs,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	saved, err := s.settings.Update(func(current *settings.Settings) {
		current.APIKeys = append(current.APIKeys, key)
	})
	if err != nil {
		return nil, err
	}
	s.applyWebAuth(saved)
	return &key, nil
}

// UpdateAPIKey 修改名称、权限和计算机范围。
func (s *HyperVService) UpdateAPIKey(request APIKeyRequest) (*settings.APIKey, error) {
	if strings.TrimSpace(request.ID) == "" {
		return nil, errors.New("API Key ID 不能为空")
	}
	if err := s.validateAPIKeyRequest(request); err != nil {
		return nil, err
	}
	var updated settings.APIKey
	found := false
	saved, err := s.settings.Update(func(current *settings.Settings) {
		for i := range current.APIKeys {
			if current.APIKeys[i].ID != request.ID {
				continue
			}
			current.APIKeys[i].Name = strings.TrimSpace(request.Name)
			current.APIKeys[i].Permission = request.Permission
			current.APIKeys[i].HostIDs = request.HostIDs
			updated = current.APIKeys[i]
			found = true
			return
		}
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("API Key 不存在")
	}
	s.applyWebAuth(saved)
	return &updated, nil
}

// DeleteAPIKey 删除 API Key。
func (s *HyperVService) DeleteAPIKey(id string) error {
	found := false
	saved, err := s.settings.Update(func(current *settings.Settings) {
		keys := make([]settings.APIKey, 0, len(current.APIKeys))
		for _, key := range current.APIKeys {
			if key.ID == id {
				found = true
				continue
			}
			keys = append(keys, key)
		}
		current.APIKeys = keys
	})
	if err != nil {
		return err
	}
	if !found {
		return errors.New("API Key 不存在")
	}
	s.applyWebAuth(saved)
	return nil
}

// RegenerateAPIKeyToken 重新生成 API Key 的 Token。
func (s *HyperVService) RegenerateAPIKeyToken(id string) (*settings.APIKey, error) {
	var updated settings.APIKey
	found := false
	saved, err := s.settings.Update(func(current *settings.Settings) {
		for i := range current.APIKeys {
			if current.APIKeys[i].ID != id {
				continue
			}
			current.APIKeys[i].Token = randomHex(16)
			updated = current.APIKeys[i]
			found = true
			return
		}
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("API Key 不存在")
	}
	s.applyWebAuth(saved)
	return &updated, nil
}

func (s *HyperVService) validateAPIKeyRequest(request APIKeyRequest) error {
	if strings.TrimSpace(request.Name) == "" {
		return errors.New("请输入 API Key 名称")
	}
	switch request.Permission {
	case settings.PermissionRead, settings.PermissionWrite, settings.PermissionAdmin:
	default:
		return errors.New("权限必须是 read、write 或 admin")
	}
	for _, id := range request.HostIDs {
		if _, err := s.hostStore.Get(id); err != nil {
			return fmt.Errorf("API Key 允许的计算机不存在: %s", id)
		}
	}
	return nil
}

func (s *HyperVService) applyWebConfig(value settings.Settings) {
	if s.api == nil {
		return
	}
	cfg := httpapi.Config{
		Enabled:       value.Web.Enabled,
		ListenAddress: value.Web.ListenAddress,
		Port:          value.Web.Port,
		Username:      value.Web.Username,
		Password:      value.Web.Password,
		APIKeys:       value.APIKeys,
	}
	// 延迟重启，避免当前 HTTP 请求被 Shutdown 截断。
	go func() {
		time.Sleep(250 * time.Millisecond)
		s.api.Apply(cfg)
	}()
}

// applyWebAuth 只更新登录凭据与 API Key，不重启监听，
// 因此不会让网页控制台已有的登录会话失效。
func (s *HyperVService) applyWebAuth(value settings.Settings) {
	if s.api == nil {
		return
	}
	s.api.UpdateAuth(httpapi.Config{
		Enabled:       value.Web.Enabled,
		ListenAddress: value.Web.ListenAddress,
		Port:          value.Web.Port,
		Username:      value.Web.Username,
		Password:      value.Web.Password,
		APIKeys:       value.APIKeys,
	})
}

// applyTunnel 按新设置重启内置 frp 客户端。
func (s *HyperVService) applyTunnel(value settings.Settings) {
	go func() {
		// 与网页控制台重启错开，保证本地端口先就绪。
		time.Sleep(500 * time.Millisecond)
		s.tunnel.Apply(tunnel.ConfigFromSettings(value))
	}()
}

// GetTunnelStatus 返回内网穿透运行状态。
func (s *HyperVService) GetTunnelStatus() tunnel.Status {
	return s.tunnel.Status()
}

// RestartTunnel 按当前设置重新连接 frps，返回即时状态。
func (s *HyperVService) RestartTunnel() tunnel.Status {
	current := s.settings.Get()
	return s.tunnel.Apply(tunnel.ConfigFromSettings(current))
}

// AddTunnelMapping 把一条 NAT 端口转发追加到内网穿透映射并保存。
// 添加后仍需要在“内网穿透”页面填写 frps 并启用。
func (s *HyperVService) AddTunnelMapping(req TunnelMappingRequest) (*settings.Settings, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.LocalIP = strings.TrimSpace(req.LocalIP)
	if req.LocalIP == "" {
		req.LocalIP = "127.0.0.1"
	}
	if req.LocalPort < 1 || req.LocalPort > 65535 {
		return nil, errors.New("本地端口必须在 1 到 65535 之间")
	}
	if req.RemotePort < 0 || req.RemotePort > 65535 {
		return nil, errors.New("远程端口必须在 0 到 65535 之间")
	}
	if req.Name == "" {
		req.Name = fmt.Sprintf("map-%d", req.LocalPort)
	}
	saved, err := s.settings.Update(func(current *settings.Settings) {
		current.Tunnel.Mappings = append(current.Tunnel.Mappings, settings.TunnelMapping{
			ID:         "map-" + randomHex(4),
			Enabled:    true,
			Name:       req.Name,
			LocalIP:    req.LocalIP,
			LocalPort:  req.LocalPort,
			RemotePort: req.RemotePort,
		})
	})
	if err != nil {
		return nil, err
	}
	s.applyTunnel(saved)
	return &saved, nil
}

func randomHex(size int) string {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// GetAPIStatus 返回 HTTP API 状态。
func (s *HyperVService) GetAPIStatus() httpapi.Status {
	if s.api == nil {
		return httpapi.Status{}
	}
	return s.api.Status()
}

// PickFile 打开文件选择器，返回绝对路径；取消时返回空字符串。
func (s *HyperVService) PickFile(title string, filterName string, filterPattern string) (string, error) {
	return s.PickFileIn(title, filterName, filterPattern, "")
}

// PickFileIn 与 PickFile 相同，但会从指定目录开始浏览。
func (s *HyperVService) PickFileIn(title string, filterName string, filterPattern string, directory string) (string, error) {
	if s.isRemote() {
		return "", errors.New("当前连接的是远程主机，请在输入框中手动填写远程主机上的绝对路径")
	}
	app := application.Get()
	if app == nil {
		return "", errors.New("应用尚未启动")
	}
	dialog := app.Dialog.OpenFile().SetTitle(title).CanChooseFiles(true).CanChooseDirectories(false)
	if filterName != "" && filterPattern != "" {
		dialog.AddFilter(filterName, filterPattern)
	}
	if strings.TrimSpace(directory) != "" {
		dialog.SetDirectory(directory)
	}
	return dialog.PromptForSingleSelection()
}

// PickDirectory 打开目录选择器，返回绝对路径；取消时返回空字符串。
func (s *HyperVService) PickDirectory(title string) (string, error) {
	return s.PickDirectoryIn(title, "")
}

// PickDirectoryIn 与 PickDirectory 相同，但会从指定目录开始浏览。
func (s *HyperVService) PickDirectoryIn(title string, directory string) (string, error) {
	if s.isRemote() {
		return "", errors.New("当前连接的是远程主机，请在输入框中手动填写远程主机上的绝对路径")
	}
	app := application.Get()
	if app == nil {
		return "", errors.New("应用尚未启动")
	}
	dialog := app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true)
	if strings.TrimSpace(directory) != "" {
		dialog.SetDirectory(directory)
	}
	return dialog.PromptForSingleSelection()
}

// RestartElevated 以管理员身份重新启动应用。
func (s *HyperVService) RestartElevated() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取程序路径: %w", err)
	}
	escaped := strings.ReplaceAll(executable, "'", "''")
	command := fmt.Sprintf("Start-Process -FilePath '%s' -Verb RunAs", escaped)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-WindowStyle", "Hidden", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("提权启动失败: %w", err)
	}
	if app := application.Get(); app != nil {
		go func() {
			time.Sleep(700 * time.Millisecond)
			app.Quit()
		}()
	}
	return nil
}
