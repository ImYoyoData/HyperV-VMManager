package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/httpapi"
	"hypervvmmanager/internal/hyperv"
	"hypervvmmanager/internal/tasks"
)

type fakeBackend struct {
	lastClone       hyperv.CloneRequest
	lastDeleteFiles bool
}

func (f *fakeBackend) ListVMs() ([]hyperv.VM, error) {
	return []hyperv.VM{{Name: "vm1", State: "Running"}}, nil
}

func (f *fakeBackend) GetVM(name string) (*hyperv.VM, error) {
	return &hyperv.VM{Name: name, State: "Off"}, nil
}

func (f *fakeBackend) GetHostInfo() (*hyperv.HostInfo, error) {
	return &hyperv.HostInfo{ComputerName: "TEST"}, nil
}

func (f *fakeBackend) ListHosts() ([]hosts.Item, error) {
	return []hosts.Item{{ID: "local", Name: "本机", IsLocal: true, Connected: true, Active: true}}, nil
}

func (f *fakeBackend) ConnectHost(id string, password string) (*hosts.Item, error) {
	return &hosts.Item{ID: id, Connected: true, Active: true}, nil
}

func (f *fakeBackend) ListSwitches() ([]hyperv.VirtualSwitch, error) {
	return []hyperv.VirtualSwitch{{Name: "Default Switch", SwitchType: "Internal"}}, nil
}

func (f *fakeBackend) SuggestCloneDirectory(sourceName string) (string, error) {
	return `D:\VMs`, nil
}

func (f *fakeBackend) SubmitCreate(req hyperv.CreateRequest) (*tasks.Task, error) {
	return &tasks.Task{ID: "T-CREATE", Kind: "create", Target: req.Name, Status: "queued"}, nil
}

func (f *fakeBackend) SubmitClone(req hyperv.CloneRequest) (*tasks.Task, error) {
	f.lastClone = req
	return &tasks.Task{ID: "T-CLONE", Kind: "clone", Target: req.NewName, Status: "queued"}, nil
}

func (f *fakeBackend) SubmitEdit(name string, req hyperv.EditRequest) (*tasks.Task, error) {
	return &tasks.Task{ID: "T-EDIT", Kind: "edit", Target: name, Status: "queued"}, nil
}

func (f *fakeBackend) SubmitDelete(name string, deleteFiles bool) (*tasks.Task, error) {
	f.lastDeleteFiles = deleteFiles
	return &tasks.Task{ID: "T-DELETE", Kind: "delete", Target: name, Status: "queued"}, nil
}

func (f *fakeBackend) SubmitPower(name string, action string) (*tasks.Task, error) {
	switch action {
	case "start", "stop", "turnoff", "save", "pause", "resume", "restart":
	default:
		return nil, errors.New("未知的电源操作")
	}
	return &tasks.Task{ID: "T-POWER", Kind: "power", Target: name, Status: "queued"}, nil
}

func (f *fakeBackend) ListTasks(limit int) ([]tasks.Task, error) {
	return []tasks.Task{{ID: "T1", Status: "running", Progress: 42}}, nil
}

func (f *fakeBackend) GetTask(id string) (*tasks.Task, error) {
	if id == "missing" {
		return nil, errors.New("任务不存在")
	}
	return &tasks.Task{ID: id, Status: "running", Progress: 42}, nil
}

func (f *fakeBackend) CancelTask(id string) error {
	return nil
}

func startServer(t *testing.T) (*fakeBackend, string, func()) {
	t.Helper()
	backend := &fakeBackend{}
	server := httpapi.New(backend)
	status := server.Apply(httpapi.Config{
		Enabled:       true,
		ListenAddress: "127.0.0.1",
		Port:          0,
		Token:         "secret-token",
	})
	if !status.Running {
		t.Fatalf("API 启动失败: %s", status.Error)
	}
	return backend, "http://" + status.Address, server.Shutdown
}

func doRequest(t *testing.T, method, url, token string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	return response, data
}

func TestHTTPAPIAuth(t *testing.T) {
	_, base, shutdown := startServer(t)
	defer shutdown()

	response, _ := doRequest(t, http.MethodGet, base+"/api/v1/health", "", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未带 Token 应返回 401，实际 %d", response.StatusCode)
	}

	response, body := doRequest(t, http.MethodGet, base+"/api/v1/health", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("健康检查应返回 200，实际 %d：%s", response.StatusCode, body)
	}
}

func TestHTTPAPIHosts(t *testing.T) {
	_, base, shutdown := startServer(t)
	defer shutdown()

	response, body := doRequest(t, http.MethodGet, base+"/api/v1/hosts", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("主机列表应返回 200，实际 %d：%s", response.StatusCode, body)
	}
	if !strings.Contains(string(body), `"local"`) {
		t.Fatalf("主机列表内容不正确: %s", body)
	}

	response, body = doRequest(t, http.MethodPost, base+"/api/v1/hosts/host-1/connect", "secret-token", map[string]string{"password": "secret"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("连接主机应返回 200，实际 %d：%s", response.StatusCode, body)
	}
	if !strings.Contains(string(body), `"active":true`) {
		t.Fatalf("连接结果不正确: %s", body)
	}
}

func TestHTTPAPIResources(t *testing.T) {
	_, base, shutdown := startServer(t)
	defer shutdown()

	response, body := doRequest(t, http.MethodGet, base+"/api/v1/vms", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("虚拟机列表应返回 200，实际 %d：%s", response.StatusCode, body)
	}
	var list struct {
		Items []hyperv.VM `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("虚拟机列表内容不正确: %v %s", err, body)
	}

	response, _ = doRequest(t, http.MethodGet, base+"/api/v1/vms/vm1", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("虚拟机详情应返回 200，实际 %d", response.StatusCode)
	}

	response, _ = doRequest(t, http.MethodGet, base+"/api/v1/vms/vm1/suggest-clone-path", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("建议目录接口应返回 200，实际 %d", response.StatusCode)
	}
}

func TestHTTPAPISubmitClone(t *testing.T) {
	backend, base, shutdown := startServer(t)
	defer shutdown()

	request := map[string]any{
		"newName":             "clone-1",
		"targetDirectory":     `D:\VMs`,
		"stopSourceIfRunning": true,
		"memory": map[string]any{
			"startupMB": 4096,
			"dynamic":   false,
			"minimumMB": 1024,
			"maximumMB": 8192,
		},
		"cpuCount":      4,
		"switchName":    "",
		"bandwidthMbps": 100,
	}

	response, body := doRequest(t, http.MethodPost, base+"/api/v1/vms/win11/clone", "secret-token", request)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("克隆提交应返回 202，实际 %d：%s", response.StatusCode, body)
	}
	if location := response.Header.Get("Location"); location != "/api/v1/tasks/T-CLONE" {
		t.Fatalf("Location 头不正确: %s", location)
	}
	if backend.lastClone.SourceName != "win11" {
		t.Fatalf("路径中的源虚拟机名称未传给后端: %+v", backend.lastClone)
	}
	if backend.lastClone.NewName != "clone-1" {
		t.Fatalf("请求体解析不正确: %+v", backend.lastClone)
	}
}

func TestHTTPAPIDeleteAndPower(t *testing.T) {
	backend, base, shutdown := startServer(t)
	defer shutdown()

	response, body := doRequest(t, http.MethodDelete, base+"/api/v1/vms/vm1?deleteFiles=true", "secret-token", nil)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("删除提交应返回 202，实际 %d：%s", response.StatusCode, body)
	}
	if !backend.lastDeleteFiles {
		t.Fatal("deleteFiles 查询参数未传递")
	}

	response, _ = doRequest(t, http.MethodPost, base+"/api/v1/vms/vm1/power", "secret-token", map[string]string{"action": "start"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("电源提交应返回 202，实际 %d", response.StatusCode)
	}

	response, _ = doRequest(t, http.MethodPost, base+"/api/v1/vms/vm1/power", "secret-token", map[string]string{"action": "invalid"})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法电源操作应返回 400，实际 %d", response.StatusCode)
	}
}

func TestHTTPAPITasks(t *testing.T) {
	_, base, shutdown := startServer(t)
	defer shutdown()

	response, body := doRequest(t, http.MethodGet, base+"/api/v1/tasks", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("任务列表应返回 200，实际 %d：%s", response.StatusCode, body)
	}
	if !strings.Contains(string(body), `"progress":42`) {
		t.Fatalf("任务列表内容不正确: %s", body)
	}

	response, body = doRequest(t, http.MethodGet, base+"/api/v1/tasks/T1", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("任务详情应返回 200，实际 %d：%s", response.StatusCode, body)
	}
	var task tasks.Task
	if err := json.Unmarshal(body, &task); err != nil || task.Progress != 42 {
		t.Fatalf("任务详情解析不正确: %v %s", err, body)
	}

	response, _ = doRequest(t, http.MethodGet, base+"/api/v1/tasks/missing", "secret-token", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在的任务应返回 404，实际 %d", response.StatusCode)
	}

	response, _ = doRequest(t, http.MethodPost, base+"/api/v1/tasks/T1/cancel", "secret-token", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("取消任务应返回 200，实际 %d", response.StatusCode)
	}
}

func TestHTTPAPIInvalidBody(t *testing.T) {
	_, base, shutdown := startServer(t)
	defer shutdown()

	request, _ := http.NewRequest(http.MethodPost, base+"/api/v1/vms", strings.NewReader("{not json"))
	request.Header.Set("Authorization", "Bearer secret-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应返回 400，实际 %d", response.StatusCode)
	}
}
