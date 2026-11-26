package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/hyperv"
	"hypervvmmanager/internal/tasks"
)

// Config 是 HTTP API 的运行时配置。
type Config struct {
	Enabled       bool
	ListenAddress string
	Port          int
	Token         string
}

// Status 描述 API 服务状态。
type Status struct {
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	Address string `json:"address"`
	Error   string `json:"error,omitempty"`
}

// Backend 是 GUI 与 HTTP API 共用的业务接口。
type Backend interface {
	ListVMs() ([]hyperv.VM, error)
	GetVM(name string) (*hyperv.VM, error)
	GetHostInfo() (*hyperv.HostInfo, error)
	ListSwitches() ([]hyperv.VirtualSwitch, error)
	SuggestCloneDirectory(sourceName string) (string, error)

	ListHosts() ([]hosts.Item, error)
	ConnectHost(id string, password string) (*hosts.Item, error)

	SubmitCreate(hostID string, req hyperv.CreateRequest) (*tasks.Task, error)
	SubmitClone(hostID string, req hyperv.CloneRequest) (*tasks.Task, error)
	SubmitEdit(name string, req hyperv.EditRequest) (*tasks.Task, error)
	SubmitDelete(name string, deleteFiles bool) (*tasks.Task, error)
	SubmitPower(name string, action string) (*tasks.Task, error)

	ListTasks(limit int) ([]tasks.Task, error)
	GetTask(id string) (*tasks.Task, error)
	CancelTask(id string) error
}

// Server 是内嵌的 HTTP API 服务，可在设置中热启停。
type Server struct {
	backend Backend

	mu     sync.Mutex
	server *http.Server
	cfg    Config
	status Status
}

func New(backend Backend) *Server {
	return &Server{
		backend: backend,
		status:  Status{Enabled: false},
	}
}

// Apply 应用新配置：先关闭旧服务，再按需启动。
func (s *Server) Apply(cfg Config) Status {
	s.Shutdown()

	s.mu.Lock()
	s.cfg = cfg
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
	if !s.status.Enabled || s.status.Running {
		s.status = Status{Enabled: s.status.Enabled, Address: s.status.Address, Error: s.status.Error}
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
		s.mu.Lock()
		token := s.cfg.Token
		s.mu.Unlock()
		if token != "" {
			header := r.Header.Get("Authorization")
			if header != "Bearer "+token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未授权，请在 Authorization 头中提供 Bearer Token"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().Format(time.RFC3339)})
	})

	mux.HandleFunc("GET /api/v1/host", func(w http.ResponseWriter, r *http.Request) {
		info, err := s.backend.GetHostInfo()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})

	mux.HandleFunc("GET /api/v1/hosts", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.backend.ListHosts()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	mux.HandleFunc("POST /api/v1/hosts/{id}/connect", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if r.ContentLength > 0 {
			if err := decodeJSON(w, r, &body); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		item, err := s.backend.ConnectHost(r.PathValue("id"), body.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	})

	mux.HandleFunc("GET /api/v1/vms", func(w http.ResponseWriter, r *http.Request) {
		vms, err := s.backend.ListVMs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": vms})
	})

	mux.HandleFunc("GET /api/v1/vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		vm, err := s.backend.GetVM(r.PathValue("name"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, vm)
	})

	mux.HandleFunc("GET /api/v1/vms/{name}/suggest-clone-path", func(w http.ResponseWriter, r *http.Request) {
		dir, err := s.backend.SuggestCloneDirectory(r.PathValue("name"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"directory": dir})
	})

	mux.HandleFunc("POST /api/v1/vms", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.HandleFunc("POST /api/v1/vms/{name}/clone", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.HandleFunc("PATCH /api/v1/vms/{name}", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.HandleFunc("DELETE /api/v1/vms/{name}", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.HandleFunc("POST /api/v1/vms/{name}/power", func(w http.ResponseWriter, r *http.Request) {
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
	})

	mux.HandleFunc("GET /api/v1/switches", func(w http.ResponseWriter, r *http.Request) {
		switches, err := s.backend.ListSwitches()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": switches})
	})

	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	mux.HandleFunc("GET /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		task, err := s.backend.GetTask(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, task)
	})

	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := s.backend.CancelTask(r.PathValue("id")); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"canceled": true})
	})

	return mux
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

// hostIDFromRequest 读取显式指定的目标主机：?hostId=... 或 X-HyperV-Host 头。
func hostIDFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.URL.Query().Get("hostId")); value != "" {
		return value
	}
	return strings.TrimSpace(r.Header.Get("X-HyperV-Host"))
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
