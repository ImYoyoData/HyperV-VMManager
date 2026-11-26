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

	"hypervvmmanager/internal/httpapi"
	"hypervvmmanager/internal/hyperv"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
)

const (
	appVersion   = "0.1.0"
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
	AppVersion   string            `json:"appVersion"`
	WailsVersion string            `json:"wailsVersion"`
}

// HyperVService 是 Wails 绑定门面，同时实现 httpapi.Backend。
type HyperVService struct {
	client   *hyperv.Client
	queue    *tasks.Queue
	settings *settings.Store
	api      *httpapi.Server
}

// New 组装服务并注册任务执行器。
func New(client *hyperv.Client, queue *tasks.Queue, store *settings.Store) *HyperVService {
	s := &HyperVService{client: client, queue: queue, settings: store}
	s.registerHandlers()
	return s
}

// AttachAPI 注入 HTTP API 服务（打破循环依赖）。
func (s *HyperVService) AttachAPI(api *httpapi.Server) {
	s.api = api
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
		var req hyperv.CloneRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		rep.Log("克隆源虚拟机 %s -> %s", req.SourceName, req.NewName)
		if err := s.client.Clone(ctx, req, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.NewName}, nil
	})

	s.queue.Register(tasks.KindCreate, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req hyperv.CreateRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		rep.Log("新建虚拟机 %s（第 %d 代）", req.Name, req.Generation)
		if err := s.client.Create(ctx, req, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name}, nil
	})

	s.queue.Register(tasks.KindEdit, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req editPayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		rep.Log("编辑虚拟机 %s", req.Name)
		if err := s.client.Update(ctx, req.Name, req.Request, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name}, nil
	})

	s.queue.Register(tasks.KindDelete, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req deletePayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		rep.Log("删除虚拟机 %s", req.Name)
		if err := s.client.Delete(ctx, req.Name, req.DeleteFiles, rep.Progress); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "deletedFiles": req.DeleteFiles}, nil
	})

	s.queue.Register(tasks.KindPower, func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		var req powerPayload
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("任务参数无效: %w", err)
		}
		action := hyperv.PowerAction(req.Action)
		rep.Log("%s虚拟机 %s", hyperv.PowerActionLabel(action), req.Name)
		if err := s.client.Power(ctx, req.Name, action); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "action": req.Action}, nil
	})
}

type editPayload struct {
	Name    string             `json:"name"`
	Request hyperv.EditRequest `json:"request"`
}

type deletePayload struct {
	Name        string `json:"name"`
	DeleteFiles bool   `json:"deleteFiles"`
}

type powerPayload struct {
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

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		info, err := s.client.HostInfo(ctx)
		if err != nil {
			bootstrap.HostError = err.Error()
			return
		}
		bootstrap.HostInfo = info
	}()
	go func() {
		defer wg.Done()
		vms, err := s.client.ListVMs(ctx)
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
	return s.client.HostInfo(ctx)
}

// GetEnvironmentStatus 检查 Windows 版本、虚拟化、Hyper-V 组件与增强会话。
func (s *HyperVService) GetEnvironmentStatus() (*hyperv.EnvironmentStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return s.client.EnvironmentStatus(ctx)
}

// ListVMs 返回虚拟机列表。
func (s *HyperVService) ListVMs() ([]hyperv.VM, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.client.ListVMs(ctx)
}

// GetVM 返回单个虚拟机详情。
func (s *HyperVService) GetVM(name string) (*hyperv.VM, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.client.GetVM(ctx, name)
}

// SuggestCloneDirectory 返回源虚拟机所在目录，供"跟随源机"使用。
func (s *HyperVService) SuggestCloneDirectory(sourceName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	vm, err := s.client.GetVM(ctx, sourceName)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(vm.Path) == "" {
		return "", errors.New("无法获取源虚拟机路径")
	}
	return filepath.Dir(vm.Path), nil
}

// SubmitClone 提交克隆任务。
func (s *HyperVService) SubmitClone(req hyperv.CloneRequest) (*tasks.Task, error) {
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
	if err := s.ensureNoActiveTask(tasks.KindClone, req.NewName); err != nil {
		return nil, err
	}
	return s.queue.Submit(tasks.KindClone, req.NewName, req)
}

// SubmitCreate 提交新建任务。
func (s *HyperVService) SubmitCreate(req hyperv.CreateRequest) (*tasks.Task, error) {
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
	if err := s.ensureNoActiveTask(tasks.KindCreate, req.Name); err != nil {
		return nil, err
	}
	return s.queue.Submit(tasks.KindCreate, req.Name, req)
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
	if err := s.ensureNoActiveTask(tasks.KindEdit, name); err != nil {
		return nil, err
	}
	return s.queue.Submit(tasks.KindEdit, name, editPayload{Name: name, Request: req})
}

// SubmitDelete 提交删除任务。
func (s *HyperVService) SubmitDelete(name string, deleteFiles bool) (*tasks.Task, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("虚拟机名称不能为空")
	}
	if err := s.ensureNoActiveTask(tasks.KindDelete, name); err != nil {
		return nil, err
	}
	return s.queue.Submit(tasks.KindDelete, name, deletePayload{Name: name, DeleteFiles: deleteFiles})
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
	if err := s.ensureNoActiveTask(tasks.KindPower, name); err != nil {
		return nil, err
	}
	return s.queue.Submit(tasks.KindPower, name, powerPayload{Name: name, Action: action})
}

func (s *HyperVService) ensureNoActiveTask(kind, target string) error {
	for _, task := range s.queue.List(200) {
		if task.Kind == kind && task.Target == target &&
			(task.Status == tasks.StatusQueued || task.Status == tasks.StatusRunning) {
			return fmt.Errorf("该目标已有未完成的同类任务（%s），请等待完成后再提交", task.ID)
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
	return s.client.ListSwitches(ctx)
}

// ListHostNetworkAdapters 返回可用于外部交换机的物理网卡。
func (s *HyperVService) ListHostNetworkAdapters() ([]hyperv.HostNetworkAdapter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.client.ListHostNetworkAdapters(ctx)
}

// CreateSwitch 创建虚拟交换机。
func (s *HyperVService) CreateSwitch(req hyperv.CreateSwitchRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return s.client.CreateSwitch(ctx, req)
}

// RemoveSwitch 删除虚拟交换机。
func (s *HyperVService) RemoveSwitch(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.client.RemoveSwitch(ctx, name)
}

// GetEnhancedSessionStatus 读取增强会话状态。
func (s *HyperVService) GetEnhancedSessionStatus() (*hyperv.EnhancedSessionStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.client.EnhancedSessionStatus(ctx)
}

// SetEnhancedSession 开启或关闭增强会话。
func (s *HyperVService) SetEnhancedSession(enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return s.client.SetEnhancedSession(ctx, enabled)
}

// OpenTool 打开系统管理工具。
func (s *HyperVService) OpenTool(tool string) error {
	return s.client.OpenTool(tool)
}

// ConnectVM 打开 VMConnect 控制台。
func (s *HyperVService) ConnectVM(name string) error {
	return s.client.ConnectVM(name)
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
