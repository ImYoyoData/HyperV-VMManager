package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/hyperv"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
	"hypervvmmanager/internal/tunnel"
)

const sessionCookieName = "hvvm_session"

// Config 是网页控制台与 HTTP API 的运行时配置。
type Config struct {
	Enabled       bool
	ListenAddress string
	Port          int
	Username      string
	Password      string
	APIKeys       []settings.APIKey
}

// Status 描述 API 服务状态。
type Status struct {
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Address string `json:"address"`
	Error   string `json:"error,omitempty"`
}

// Backend 是 GUI、网页控制台与 HTTP API 共用的业务接口。
type Backend interface {
	ListVMs() ([]hyperv.VM, error)
	ListVMsForHost(hostID string) ([]hyperv.VM, error)
	GetVM(name string) (*hyperv.VM, error)
	GetHostInfo() (*hyperv.HostInfo, error)
	ListSwitches() ([]hyperv.VirtualSwitch, error)
	ListSwitchesForHost(hostID string) ([]hyperv.VirtualSwitch, error)
	ListHostNetworkAdapters() ([]hyperv.HostNetworkAdapter, error)
	CreateSwitch(req hyperv.CreateSwitchRequest) error
	RemoveSwitch(name string) error
	SuggestCloneDirectory(sourceName string) (string, error)
	SuggestCloneDirectoryForHost(hostID, sourceName string) (string, error)

	ListHosts() ([]hosts.Item, error)
	GetActiveHost() (*hosts.Item, error)
	AddHost(request hosts.Request) (*hosts.Item, error)
	UpdateHost(request hosts.Request) (*hosts.Item, error)
	RemoveHost(id string) error
	ConnectHost(id string, password string, remember bool) (*hosts.Item, error)
	ForgetHostCredential(id string) error
	SetActiveHost(id string) (*hosts.Item, error)

	GetEnvironmentStatus() (*hyperv.EnvironmentStatus, error)
	GetEnhancedSessionStatus() (*hyperv.EnhancedSessionStatus, error)
	SetEnhancedSession(enabled bool) error
	GetHostCapacity(hostID string) (*hyperv.HostCapacity, error)
	GetTunnelStatus() tunnel.Status

	SubmitCreate(hostID string, req hyperv.CreateRequest) (*tasks.Task, error)
	SubmitClone(hostID string, req hyperv.CloneRequest) (*tasks.Task, error)
	SubmitEdit(name string, req hyperv.EditRequest) (*tasks.Task, error)
	SubmitDelete(name string, deleteFiles bool) (*tasks.Task, error)
	SubmitPower(name string, action string) (*tasks.Task, error)

	ListTasks(limit int) ([]tasks.Task, error)
	GetTask(id string) (*tasks.Task, error)
	CancelTask(id string) error
	ClearFinishedTasks() int
	GetTaskStats() tasks.Stats

	GetSettings() (*settings.Settings, error)
	SaveSettings(value settings.Settings) (*settings.Settings, error)
	GetDataDirectory() string
	GetVersions() (string, string)
	IsAdmin() bool
}

type session struct {
	Username string
	Expires  time.Time
}

type authContextKey struct{}

type authInfo struct {
	IsWeb    bool
	Username string
	Key      *settings.APIKey
}

// Server 是内嵌的网页控制台 / HTTP API 服务，可在设置中热启停。
type Server struct {
	backend Backend
	assets  fs.FS

	mu       sync.Mutex
	server   *http.Server
	cfg      Config
	status   Status
	sessions map[string]session
}

func New(backend Backend, assets fs.FS) *Server {
	return &Server{
		backend:  backend,
		assets:   assets,
		status:   Status{Enabled: false},
		sessions: map[string]session{},
	}
}

// Apply 应用新配置：先关闭旧服务，再按需启动。
func (s *Server) Apply(cfg Config) Status {
	s.Shutdown()

	s.mu.Lock()
	s.cfg = cfg
	s.sessions = map[string]session{}
	s.mu.Unlock()

	if !cfg.Enabled {
		status := Status{Enabled: false}
		s.mu.Lock()
		s.status = status
		s.mu.Unlock()
		return status
	}

	addr := net.JoinHostPort(cfg.ListenAddress, strconv.Itoa(cfg.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		status := Status{Enabled: true, Address: addr, Error: fmt.Sprintf("无法监听 %s: %v", addr, err)}
		s.mu.Lock()
		s.status = status
		s.mu.Unlock()
		return status
	}
	actualAddr := listener.Addr().String()

	srv := &http.Server{
		Handler:           s.withAuth(s.routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      150 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	s.mu.Lock()
	s.server = srv
	s.status = Status{Enabled: true, Running: true, Address: actualAddr}
	s.mu.Unlock()

	go func() {
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.status.Running = false
			s.status.Error = err.Error()
			s.mu.Unlock()
		}
	}()

	return Status{Enabled: true, Running: true, Address: actualAddr}
}

// Shutdown 停止 HTTP 服务。
func (s *Server) Shutdown() {
	s.mu.Lock()
	srv := s.server
	s.server = nil
	if s.status.Enabled {
		s.status.Running = false
	}
	s.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// Status 返回当前状态。
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// 静态资源、登录页与 AI 技能包需要匿名可访问。
		if !strings.HasPrefix(path, "/api/") || path == "/api/v1/health" || path == "/api/v1/auth/login" || path == "/api/v1/skill.md" {
			next.ServeHTTP(w, r)
			return
		}

		info := s.authenticate(r)
		if info == nil {
			writeError(w, http.StatusUnauthorized, errors.New("未登录或 Token 无效"))
			return
		}
		if err := s.authorizeRequest(info, r); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}

		ctx := context.WithValue(r.Context(), authContextKey{}, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authenticate(r *http.Request) *authInfo {
	s.mu.Lock()
	sessions := map[string]session{}
	for token, sess := range s.sessions {
		sessions[token] = sess
	}
	cfg := s.cfg
	s.mu.Unlock()

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if sess, ok := sessions[cookie.Value]; ok && time.Now().Before(sess.Expires) {
			return &authInfo{IsWeb: true, Username: sess.Username}
		}
	}

	token := bearerToken(r)
	if token == "" {
		return nil
	}
	var matched *settings.APIKey
	for i := range cfg.APIKeys {
		key := cfg.APIKeys[i]
		if subtle.ConstantTimeCompare([]byte(token), []byte(key.Token)) == 1 {
			cp := key
			matched = &cp
		}
	}
	if matched == nil {
		return nil
	}
	return &authInfo{Key: matched}
}

func permissionRank(permission string) int {
	switch permission {
	case settings.PermissionAdmin:
		return 3
	case settings.PermissionWrite:
		return 2
	case settings.PermissionRead:
		return 1
	default:
		return 0
	}
}

func (s *Server) authorizeRequest(info *authInfo, r *http.Request) error {
	if info.IsWeb {
		return nil
	}
	key := info.Key
	if key == nil {
		return errors.New("未授权")
	}

	needed := settings.PermissionRead
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		needed = settings.PermissionWrite
	}
	if isAdminPath(r.URL.Path, r.Method) {
		needed = settings.PermissionAdmin
	}
	if permissionRank(key.Permission) < permissionRank(needed) {
		return errors.New("当前 API Key 没有执行该操作的权限")
	}

	if len(key.HostIDs) == 0 {
		return nil
	}

	hostID := hostIDFromRequest(r)
	if hostID != "" {
		if !hostAllowed(key, hostID) {
			return errors.New("当前 API Key 没有该计算机的管理权限")
		}
		return nil
	}

	// 修改主机配置只允许管理员，且 HostID 范围在 handler 中校验。
	if strings.HasPrefix(r.URL.Path, "/api/v1/hosts") {
		return nil
	}

	// 其余虚拟机相关请求作用于当前主机，必须确保当前主机在允许范围内。
	if isHostScopedPath(r.URL.Path) {
		active, err := s.backend.GetActiveHost()
		if err != nil {
			return err
		}
		if active != nil && !hostAllowed(key, active.ID) {
			return errors.New("当前计算机不在该 API Key 的允许范围内，请先切换到允许的计算机")
		}
	}
	return nil
}

func isAdminPath(path string, method string) bool {
	if strings.HasPrefix(path, "/api/v1/settings") {
		return true
	}
	if path == "/api/v1/hosts" && method != http.MethodGet {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/hosts/") &&
		method != http.MethodGet && !strings.HasSuffix(path, "/connect") {
		return true
	}
	return false
}

func isHostScopedPath(path string) bool {
	prefixes := []string{
		"/api/v1/vms",
		"/api/v1/switches",
		"/api/v1/host",
		"/api/v1/host-network-adapters",
		"/api/v1/environment",
		"/api/v1/enhanced-session",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func hostAllowed(key *settings.APIKey, hostID string) bool {
	if key == nil || len(key.HostIDs) == 0 {
		return true
	}
	for _, id := range key.HostIDs {
		if id == hostID {
			return true
		}
	}
	return false
}

func authFrom(r *http.Request) *authInfo {
	if info, ok := r.Context().Value(authContextKey{}).(*authInfo); ok {
		return info
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// ---------- 认证 ----------
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", s.handleMe)

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().Format(time.RFC3339)})
	})

	// ---------- 聚合数据 ----------
	mux.HandleFunc("GET /api/v1/bootstrap", s.handleBootstrap)

	// ---------- 设置 ----------
	mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.handleSaveSettings)

	// ---------- 主机 ----------
	mux.HandleFunc("GET /api/v1/host", func(w http.ResponseWriter, r *http.Request) {
		info, err := s.backend.GetHostInfo()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("GET /api/v1/hosts", s.handleListHosts)
	mux.HandleFunc("POST /api/v1/hosts", s.handleAddHost)
	mux.HandleFunc("GET /api/v1/hosts/{id}", s.handleGetHost)
	mux.HandleFunc("GET /api/v1/hosts/{id}/capacity", s.handleHostCapacity)
	mux.HandleFunc("PATCH /api/v1/hosts/{id}", s.handleUpdateHost)
	mux.HandleFunc("DELETE /api/v1/hosts/{id}", s.handleRemoveHost)
	mux.HandleFunc("POST /api/v1/hosts/{id}/connect", s.handleConnectHost)
	mux.HandleFunc("DELETE /api/v1/hosts/{id}/credential", s.handleForgetHostCredential)

	// ---------- 内网穿透 / AI 接入 ----------
	mux.HandleFunc("GET /api/v1/tunnel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.backend.GetTunnelStatus())
	})
	mux.HandleFunc("GET /api/v1/skill.md", s.handleSkill)

	// ---------- 虚拟机 ----------
	mux.HandleFunc("GET /api/v1/vms", s.handleListVMs)
	mux.HandleFunc("GET /api/v1/vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		vm, err := s.backend.GetVM(r.PathValue("name"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	})
	mux.HandleFunc("GET /api/v1/vms/{name}/suggest-clone-path", s.handleSuggestClonePath)
	mux.HandleFunc("POST /api/v1/vms", s.handleCreateVM)
	mux.HandleFunc("POST /api/v1/vms/{name}/clone", s.handleCloneVM)
	mux.HandleFunc("PATCH /api/v1/vms/{name}", s.handleEditVM)
	mux.HandleFunc("DELETE /api/v1/vms/{name}", s.handleDeleteVM)
	mux.HandleFunc("POST /api/v1/vms/{name}/power", s.handlePower)

	// ---------- 网络 ----------
	mux.HandleFunc("GET /api/v1/switches", s.handleListSwitches)
	mux.HandleFunc("POST /api/v1/switches", s.handleCreateSwitch)
	mux.HandleFunc("DELETE /api/v1/switches/{name}", s.handleRemoveSwitch)
	mux.HandleFunc("GET /api/v1/host-network-adapters", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.backend.ListHostNetworkAdapters()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	// ---------- 环境 / 增强会话 ----------
	mux.HandleFunc("GET /api/v1/environment", func(w http.ResponseWriter, r *http.Request) {
		status, err := s.backend.GetEnvironmentStatus()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("GET /api/v1/enhanced-session", func(w http.ResponseWriter, r *http.Request) {
		status, err := s.backend.GetEnhancedSessionStatus()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /api/v1/enhanced-session", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.backend.SetEnhancedSession(body.Enabled); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": body.Enabled})
	})

	// ---------- 任务 ----------
	mux.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("GET /api/v1/tasks/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.backend.GetTaskStats())
	})
	mux.HandleFunc("POST /api/v1/tasks/clear", func(w http.ResponseWriter, r *http.Request) {
		removed := s.backend.ClearFinishedTasks()
		writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
	})
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleCancelTask)

	// ---------- 静态资源 / 网页控制台 ----------
	if s.assets != nil {
		fileServer := http.FileServer(http.FS(s.assets))
		mux.Handle("GET /", fileServer)
		mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, errors.New("接口不存在"))
		})
	}

	return mux
}

// ---------- 认证处理 ----------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	usernameOK := subtle.ConstantTimeCompare([]byte(body.Username), []byte(cfg.Username)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(body.Password), []byte(cfg.Password)) == 1
	if !usernameOK || !passwordOK {
		writeError(w, http.StatusUnauthorized, errors.New("用户名或密码错误"))
		return
	}

	token := randomHex(16)
	expires := time.Now().Add(12 * time.Hour)
	s.mu.Lock()
	s.sessions[token] = session{Username: cfg.Username, Expires: expires}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		Expires:  expires,
		MaxAge:   12 * 60 * 60,
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": cfg.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	info := authFrom(r)
	if info == nil {
		writeError(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	if info.IsWeb {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"kind":          "web",
			"username":      info.Username,
			"permission":    settings.PermissionAdmin,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"kind":          "key",
		"name":          info.Key.Name,
		"permission":    info.Key.Permission,
	})
}

// ---------- 聚合数据 ----------

type bootstrapResponse struct {
	Settings     settings.Settings `json:"settings"`
	HostInfo     *hyperv.HostInfo  `json:"hostInfo,omitempty"`
	HostError    string            `json:"hostError,omitempty"`
	VMCount      int               `json:"vmCount"`
	RunningCount int               `json:"runningCount"`
	IsAdmin      bool              `json:"isAdmin"`
	DataDir      string            `json:"dataDir"`
	API          Status            `json:"api"`
	TaskStats    tasks.Stats       `json:"taskStats"`
	ActiveHost   *hosts.Item       `json:"activeHost,omitempty"`
	AppVersion   string            `json:"appVersion"`
	WailsVersion string            `json:"wailsVersion"`
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	current, err := s.backend.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	appVersion, wailsVersion := s.backend.GetVersions()
	response := bootstrapResponse{
		Settings:     settings.Masked(*current),
		IsAdmin:      s.backend.IsAdmin(),
		DataDir:      s.backend.GetDataDirectory(),
		API:          s.Status(),
		TaskStats:    s.backend.GetTaskStats(),
		AppVersion:   appVersion,
		WailsVersion: wailsVersion,
	}
	if info, err := s.backend.GetHostInfo(); err == nil {
		response.HostInfo = info
	} else {
		response.HostError = err.Error()
	}
	if active, err := s.backend.GetActiveHost(); err == nil {
		response.ActiveHost = active
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	current, err := s.backend.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings.Masked(*current))
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var value settings.Settings
	if err := decodeJSON(w, r, &value); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	saved, err := s.backend.SaveSettings(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, settings.Masked(*saved))
}

// ---------- 主机 ----------

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	items, err := s.backend.ListHosts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	info := authFrom(r)
	if info != nil && info.Key != nil && len(info.Key.HostIDs) > 0 {
		filtered := make([]hosts.Item, 0, len(items))
		for _, item := range items {
			if hostAllowed(info.Key, item.ID) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.allowHostID(w, r, id) {
		return
	}
	items, err := s.backend.ListHosts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, item := range items {
		if item.ID == id {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeError(w, http.StatusNotFound, errors.New("计算机不存在"))
}

func (s *Server) handleHostCapacity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.allowHostID(w, r, id) {
		return
	}
	capacity, err := s.backend.GetHostCapacity(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, capacity)
}

func (s *Server) allowHostID(w http.ResponseWriter, r *http.Request, hostID string) bool {
	info := authFrom(r)
	if info != nil && info.Key != nil && !hostAllowed(info.Key, hostID) {
		writeError(w, http.StatusForbidden, errors.New("当前 API Key 没有该计算机的管理权限"))
		return false
	}
	return true
}

func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + r.Host
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="hyperv-vm-api-skill.md"`)
	_, _ = fmt.Fprintf(w, skillTemplate, base)
}

const skillTemplate = `# Hyper-V 虚拟机管理助手 API 技能包

Base URL: %s
认证头: Authorization: Bearer <API_KEY>
Content-Type: application/json

## 规则

- 新建、克隆、编辑、删除、电源操作都是异步任务，返回 202 和 task.id；用 GET /api/v1/tasks/{id} 轮询，status 为 queued/running/succeeded/failed/canceled。
- 默认操作“当前主机”；可以通过 ?hostId=<id> 查询参数或 X-HyperV-Host 请求头指定目标计算机。
- API Key 可能只允许管理部分计算机；403 表示没有该计算机权限。
- 远程计算机上的目录、镜像、ISO 必须传该远程计算机上的绝对路径。
- 电源 action：start | stop | turnoff | save | pause | resume | restart。
- 删除虚拟机默认只删配置；需要同时删文件时传 deleteFiles=true。

## 常用接口

### 查询
- GET /api/v1/health
- GET /api/v1/hosts
- GET /api/v1/hosts/{id}
- GET /api/v1/hosts/{id}/capacity
- GET /api/v1/host
- GET /api/v1/vms?hostId=<id>
- GET /api/v1/vms/{name}
- GET /api/v1/vms/{name}/suggest-clone-path
- GET /api/v1/switches
- GET /api/v1/tasks?limit=100
- GET /api/v1/tasks/{id}
- GET /api/v1/tunnel

### 连接计算机
POST /api/v1/hosts/{id}/connect
{"password":"<password>","remember":true}

remember=true 时密码写入 Windows 凭据管理器，后续可以直接连接，
VMConnect / Hyper-V 管理器也可以复用。

### 新建虚拟机
POST /api/v1/vms?hostId=<hostId>
{
  "name": "win11-test",
  "generation": 2,
  "directory": "D:\\VMs",
  "memory": {"startupMB":4096,"dynamic":false,"minimumMB":1024,"maximumMB":8192},
  "cpuCount": 4,
  "switchName": "Default Switch",
  "bandwidthMbps": 100,
  "diskMode": "differencing",
  "imagePath": "D:\\Images\\win11.vhdx",
  "diskSizeGB": 60,
  "isoPath": "D:\\Images\\win11.iso",
  "disableCheckpoints": true
}

### 克隆 / 编辑 / 删除 / 电源
- POST /api/v1/vms/{name}/clone?hostId=<hostId>
- PATCH /api/v1/vms/{name}
- DELETE /api/v1/vms/{name}?deleteFiles=false
- POST /api/v1/vms/{name}/power  {"action":"start"}

### 任务
- POST /api/v1/tasks/{id}/cancel
- POST /api/v1/tasks/clear

## 推荐工作流

1. GET /api/v1/hosts 找到目标计算机，记录 id。
2. 如果未连接，POST /api/v1/hosts/{id}/connect。
3. GET /api/v1/vms?hostId=<id> 确认名称不冲突，并读取虚拟机网络与磁盘信息。
4. POST /api/v1/vms?hostId=<id> 提交新建/克隆任务。
5. 轮询 GET /api/v1/tasks/{id}，直到 succeeded 或 failed；失败时读取 error 与 logs。
`

func (s *Server) handleAddHost(w http.ResponseWriter, r *http.Request) {
	var request hosts.Request
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.backend.AddHost(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	var request hosts.Request
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request.ID = r.PathValue("id")
	item, err := s.backend.UpdateHost(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleRemoveHost(w http.ResponseWriter, r *http.Request) {
	if err := s.backend.RemoveHost(r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConnectHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if info := authFrom(r); info != nil && info.Key != nil && len(info.Key.HostIDs) > 0 {
		if !hostAllowed(info.Key, id) {
			writeError(w, http.StatusForbidden, errors.New("当前 API Key 没有该计算机的管理权限"))
			return
		}
	}
	var body struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	item, err := s.backend.ConnectHost(id, body.Password, body.Remember)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleForgetHostCredential(w http.ResponseWriter, r *http.Request) {
	if err := s.backend.ForgetHostCredential(r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 虚拟机 ----------

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request) {
	hostID := hostIDFromRequest(r)
	var (
		vms []hyperv.VM
		err error
	)
	if hostID != "" {
		vms, err = s.backend.ListVMsForHost(hostID)
	} else {
		vms, err = s.backend.ListVMs()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": vms})
}

func (s *Server) handleSuggestClonePath(w http.ResponseWriter, r *http.Request) {
	hostID := hostIDFromRequest(r)
	var (
		dir string
		err error
	)
	if hostID != "" {
		dir, err = s.backend.SuggestCloneDirectoryForHost(hostID, r.PathValue("name"))
	} else {
		dir, err = s.backend.SuggestCloneDirectory(r.PathValue("name"))
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"directory": dir})
}

func (s *Server) handleCreateVM(w http.ResponseWriter, r *http.Request) {
	var req hyperv.CreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.backend.SubmitCreate(hostIDFromRequest(r), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeAccepted(w, task)
}

func (s *Server) handleCloneVM(w http.ResponseWriter, r *http.Request) {
	var req hyperv.CloneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.SourceName = r.PathValue("name")
	task, err := s.backend.SubmitClone(hostIDFromRequest(r), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeAccepted(w, task)
}

func (s *Server) handleEditVM(w http.ResponseWriter, r *http.Request) {
	var req hyperv.EditRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.backend.SubmitEdit(r.PathValue("name"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeAccepted(w, task)
}

func (s *Server) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	deleteFiles := false
	if value := r.URL.Query().Get("deleteFiles"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("deleteFiles 必须是 true 或 false"))
			return
		}
		deleteFiles = parsed
	}
	task, err := s.backend.SubmitDelete(r.PathValue("name"), deleteFiles)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeAccepted(w, task)
}

func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.backend.SubmitPower(r.PathValue("name"), body.Action)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeAccepted(w, task)
}

// ---------- 网络 ----------

func (s *Server) handleListSwitches(w http.ResponseWriter, r *http.Request) {
	hostID := hostIDFromRequest(r)
	var (
		switches []hyperv.VirtualSwitch
		err      error
	)
	if hostID != "" {
		switches, err = s.backend.ListSwitchesForHost(hostID)
	} else {
		switches, err = s.backend.ListSwitches()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": switches})
}

func (s *Server) handleCreateSwitch(w http.ResponseWriter, r *http.Request) {
	var req hyperv.CreateSwitchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.backend.CreateSwitch(req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name})
}

func (s *Server) handleRemoveSwitch(w http.ResponseWriter, r *http.Request) {
	if err := s.backend.RemoveSwitch(r.PathValue("name")); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 任务 ----------

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, err := s.backend.ListTasks(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	info := authFrom(r)
	if info != nil && info.Key != nil && len(info.Key.HostIDs) > 0 {
		filtered := make([]tasks.Task, 0, len(items))
		for _, task := range items {
			if task.HostID == "" || hostAllowed(info.Key, task.HostID) {
				filtered = append(filtered, task)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) taskAllowed(r *http.Request, task *tasks.Task) bool {
	info := authFrom(r)
	if info == nil || info.Key == nil || len(info.Key.HostIDs) == 0 {
		return true
	}
	return task.HostID == "" || hostAllowed(info.Key, task.HostID)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.backend.GetTask(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !s.taskAllowed(r, task) {
		writeError(w, http.StatusForbidden, errors.New("当前 API Key 没有该任务的访问权限"))
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.backend.GetTask(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !s.taskAllowed(r, task) {
		writeError(w, http.StatusForbidden, errors.New("当前 API Key 没有该任务的访问权限"))
		return
	}
	if err := s.backend.CancelTask(task.ID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"canceled": true})
}

// ---------- 通用 ----------

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

// hostIDFromRequest 读取显式指定的目标主机：?hostId=... 或 X-HyperV-Host 头。
func hostIDFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.URL.Query().Get("hostId")); value != "" {
		return value
	}
	return strings.TrimSpace(r.Header.Get("X-HyperV-Host"))
}

func randomHex(size int) string {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求体不是合法的 JSON: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "未知错误"
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeAccepted(w http.ResponseWriter, task *tasks.Task) {
	if task == nil {
		writeError(w, http.StatusInternalServerError, errors.New("任务创建失败"))
		return
	}
	w.Header().Set("Location", "/api/v1/tasks/"+task.ID)
	writeJSON(w, http.StatusAccepted, task)
}
