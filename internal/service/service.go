package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/httpapi"
	"hypervvmmanager/internal/hyperv"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
)

const (
	appVersion   = "0.1.1"
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
	defer s.mu.RUnlock()
	client := s.clients[hostID]
	if client == nil {
		return nil, fmt.Errorf("主机尚未连接或已断开: %s", hostID)
	}
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
	profile, err := s.hostStore.Update(request)
	if err != nil {
		return nil, err
	}
	// 配置变化后，旧的连接缓存不再可信。
	s.mu.Lock()
	delete(s.clients, profile.ID)
	if s.activeHostID == profile.ID {
		s.activeHostID = hosts.LocalID
	}
	s.mu.Unlock()
	item := s.hostItem(profile)
	return &item, nil
}

// RemoveHost 删除远程主机配置并断开缓存。
func (s *HyperVService) RemoveHost(id string) error {
	if err := s.hostStore.Remove(id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.clients, id)
	if s.activeHostID == id {
		s.activeHostID = hosts.LocalID
	}
	s.mu.Unlock()
	s.emitHostChanged()
	return nil
}

// ConnectHost 连接远程主机并设为当前主机；本机直接切换。
func (s *HyperVService) ConnectHost(id string, password string) (*hosts.Item, error) {
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
		if strings.TrimSpace(profile.Username) != "" {
			return nil, errors.New("请输入远程主机密码")
		}
	}

	target := hyperv.Target{
		ID:             profile.ID,
		Name:           profile.Name,
		ComputerName:   profile.Address,
		Port:           profile.Port,
		UseSSL:         profile.UseSSL,
		Username:       profile.Username,
		Password:       password,
		Authentication: profile.Authentication,
	}
	client := hyperv.NewClientForTarget(target)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := client.TestConnection(ctx); err != nil {
		return nil, fmt.Errorf("连接远程主机 %s 失败: %w", profile.Address, err)
	}

	s.mu.Lock()
	s.clients[id] = client
	s.activeHostID = id
	s.mu.Unlock()
	s.hostStore.MarkConnected(id)
	profile.LastConnectedAt = time.Now().Format(time.RFC3339)

	item := s.hostItem(*profile)
	s.emitHostChanged()
	return &item, nil
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
			if task.Status != tasks.StatusQueued && task.Status != tasks.StatusRunning {
				app.Event.Emit("task:finished", task)
			}
		})
	}
	s.queue.Start(ctx)

	cfg := s.settings.Get().API
	if s.api != nil {
		s.api.Apply(httpapi.Config{
			Enabled:       cfg.Enabled,
			ListenAddress: cfg.ListenAddress,
			Port:          cfg.Port,
			Token:         cfg.Token,
		})
	}
	return nil
}

// ServiceShutdown 由 Wails 在应用关闭时调用。
func (s *HyperVService) ServiceShutdown() error {
	s.queue.Stop()
	if s.api != nil {
		s.api.Shutdown()
	}
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
		vms, err := s.currentClient().ListVMs(ctx)
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
	switch req.DiskMode {
	case "", "new":
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
	if req.NewName == "" && req.Memory == nil && req.CPUCount == nil && req.SwitchName == nil && req.BandwidthMbps == nil && req.CheckpointsEnabled == nil {
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
	saved, err := s.settings.Save(value)
	if err != nil {
		return nil, err
	}
	if s.api != nil {
		s.api.Apply(httpapi.Config{
			Enabled:       saved.API.Enabled,
			ListenAddress: saved.API.ListenAddress,
			Port:          saved.API.Port,
			Token:         saved.API.Token,
		})
	}
	return &saved, nil
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
	return dialog.PromptForSingleSelection()
}

// PickDirectory 打开目录选择器，返回绝对路径；取消时返回空字符串。
func (s *HyperVService) PickDirectory(title string) (string, error) {
	if s.isRemote() {
		return "", errors.New("当前连接的是远程主机，请在输入框中手动填写远程主机上的绝对路径")
	}
	app := application.Get()
	if app == nil {
		return "", errors.New("应用尚未启动")
	}
	return app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		PromptForSingleSelection()
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
