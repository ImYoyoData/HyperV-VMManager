package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// 任务状态。
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

// 任务类型。
const (
	KindClone  = "clone"
	KindCreate = "create"
	KindEdit   = "edit"
	KindDelete = "delete"
	KindPower  = "power"
)

// TaskLog 是一条任务日志。
type TaskLog struct {
	Time    string `json:"time"`
	Level   string `json:"level"` // info | warn | error
	Message string `json:"message"`
}

// Task 是任务队列对外暴露的任务快照。
type Task struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	HostID     string         `json:"hostID,omitempty"`
	Target     string         `json:"target"`
	Status     string         `json:"status"`
	Progress   int            `json:"progress"`
	Step       string         `json:"step"`
	Result     map[string]any `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  string         `json:"createdAt"`
	StartedAt  string         `json:"startedAt,omitempty"`
	FinishedAt string         `json:"finishedAt,omitempty"`
	Logs       []TaskLog      `json:"logs"`
}

// CloneSnapshot 返回可安全跨 goroutine 使用的副本。
func (t *Task) CloneSnapshot() Task {
	cp := *t
	cp.Logs = append([]TaskLog(nil), t.Logs...)
	if t.Result != nil {
		cp.Result = make(map[string]any, len(t.Result))
		for k, v := range t.Result {
			cp.Result[k] = v
		}
	}
	return cp
}

func nowString() string {
	return time.Now().Format(time.RFC3339)
}

// KindLabel 返回任务类型的中文名称。
func KindLabel(kind string) string {
	switch kind {
	case KindClone:
		return "克隆虚拟机"
	case KindCreate:
		return "新建虚拟机"
	case KindEdit:
		return "编辑虚拟机"
	case KindDelete:
		return "删除虚拟机"
	case KindPower:
		return "电源操作"
	default:
		return kind
	}
}

// Handler 是任务执行函数。payload 为提交任务时的 JSON 参数。
type Handler func(ctx context.Context, reporter *Reporter, payload json.RawMessage) (map[string]any, error)

// Reporter 由队列注入，任务执行过程中用于上报进度与日志。
type Reporter struct {
	queue *Queue
	entry *entry
}

// Progress 更新任务进度（0-100）与当前步骤。
func (r *Reporter) Progress(percent int, step string) {
	r.queue.updateProgress(r.entry, percent, step)
}

// Log 追加一条 info 日志。
func (r *Reporter) Log(format string, args ...any) {
	r.queue.appendLog(r.entry, "info", fmt.Sprintf(format, args...))
}

// Warn 追加一条 warn 日志。
func (r *Reporter) Warn(format string, args ...any) {
	r.queue.appendLog(r.entry, "warn", fmt.Sprintf(format, args...))
}
