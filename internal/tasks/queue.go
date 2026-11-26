package tasks

import (
	"context"
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

const (
	maxTasks = 500
	maxLogs  = 300
)

type entry struct {
	task            *Task
	payload         json.RawMessage
	created         time.Time
	cancel          context.CancelFunc
	cancelRequested bool
}

// Queue 是单 worker 的串行任务队列。
// Hyper-V 的克隆、新建、删除等操作不适合并发执行，因此统一排队。
type Queue struct {
	mu        sync.Mutex
	entries   []*entry
	byID      map[string]*entry
	handlers  map[string]Handler
	running   *entry
	wake      chan struct{}
	storePath string
	emit      func(Task)
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	started   bool
}

// NewQueue 创建队列并加载持久化历史。
func NewQueue(storePath string) (*Queue, error) {
	q := &Queue{
		byID:      map[string]*entry{},
		handlers:  map[string]Handler{},
		wake:      make(chan struct{}, 1),
		storePath: storePath,
	}
	if err := q.load(); err != nil {
		return nil, err
	}
	return q, nil
}

// Register 注册任务类型的执行器。
func (q *Queue) Register(kind string, handler Handler) {
	q.mu.Lock()
	q.handlers[kind] = handler
	q.mu.Unlock()
}

// SetEmitter 设置任务更新回调。回调在锁外执行。
func (q *Queue) SetEmitter(fn func(Task)) {
	q.mu.Lock()
	q.emit = fn
	q.mu.Unlock()
}

// Start 启动 worker。
func (q *Queue) Start(parent context.Context) {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	q.ctx, q.cancel = context.WithCancel(parent)
	q.wg.Add(1)
	q.mu.Unlock()

	go q.worker()
	q.signal()
}

// Stop 停止队列，并终止正在执行的任务。
func (q *Queue) Stop() {
	q.mu.Lock()
	if !q.started {
		q.mu.Unlock()
		return
	}
	q.started = false
	cancel := q.cancel
	q.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	q.wg.Wait()
}

// Submit 提交任务，返回排队中的任务快照。
func (q *Queue) Submit(kind, target string, payload any) (*Task, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("无法序列化任务参数: %w", err)
	}

	q.mu.Lock()
	if _, ok := q.handlers[kind]; !ok {
		q.mu.Unlock()
		return nil, fmt.Errorf("未知的任务类型: %s", kind)
	}

	now := time.Now()
	task := &Task{
		ID:        q.newIDLocked(now),
		Kind:      kind,
		Target:    target,
		Status:    StatusQueued,
		Step:      "排队等待执行",
		CreatedAt: now.Format(time.RFC3339),
		Logs: []TaskLog{{
			Time:    now.Format(time.RFC3339),
			Level:   "info",
			Message: "任务已加入队列",
		}},
	}
	e := &entry{task: task, payload: raw, created: now}
	q.entries = append(q.entries, e)
	q.byID[task.ID] = e
	q.trimLocked()
	q.saveLocked()
	snapshot := task.CloneSnapshot()
	q.mu.Unlock()

	q.emitSafe(snapshot)
	q.signal()
	return &snapshot, nil
}

// Get 按 ID 查询任务。
func (q *Queue) Get(id string) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.byID[id]
	if e == nil {
		return nil, fmt.Errorf("任务不存在: %s", id)
	}
	snapshot := e.task.CloneSnapshot()
	return &snapshot, nil
}

// List 返回最近的 limit 条任务，最新的在前。
func (q *Queue) List(limit int) []Task {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]Task, 0, min(limit, len(q.entries)))
	for i := len(q.entries) - 1; i >= 0 && len(result) < limit; i-- {
		result = append(result, q.entries[i].task.CloneSnapshot())
	}
	return result
}

// Cancel 取消任务。排队中的任务立即取消，运行中的任务请求终止。
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	e := q.byID[id]
	if e == nil {
		q.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}

	switch e.task.Status {
	case StatusQueued:
		e.task.Status = StatusCanceled
		e.task.Step = "已取消（排队中）"
		e.task.FinishedAt = nowString()
		q.appendLogLocked(e.task, "warn", "任务在排队阶段被取消")
		q.saveLocked()
		snapshot := e.task.CloneSnapshot()
		q.mu.Unlock()
		q.emitSafe(snapshot)
		return nil
	case StatusRunning:
		e.cancelRequested = true
		if e.cancel != nil {
			e.cancel()
		}
		q.appendLogLocked(e.task, "warn", "已请求取消任务，正在终止当前操作...")
		q.saveLocked()
		snapshot := e.task.CloneSnapshot()
		q.mu.Unlock()
		q.emitSafe(snapshot)
		return nil
	default:
		q.mu.Unlock()
		return fmt.Errorf("任务已结束，无法取消")
	}
}

// ClearFinished 删除所有已结束的任务，返回删除数量。
func (q *Queue) ClearFinished() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := make([]*entry, 0, len(q.entries))
	removed := 0
	for _, e := range q.entries {
		if e.task.Status == StatusQueued || e.task.Status == StatusRunning {
			kept = append(kept, e)
			continue
		}
		delete(q.byID, e.task.ID)
		removed++
	}
	q.entries = kept
	q.saveLocked()
	return removed
}

// Stats 汇总任务状态数量。
type Stats struct {
	Total     int `json:"total"`
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Canceled  int `json:"canceled"`
}

func (q *Queue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	stats := Stats{Total: len(q.entries)}
	for _, e := range q.entries {
		switch e.task.Status {
		case StatusQueued:
			stats.Queued++
		case StatusRunning:
			stats.Running++
		case StatusSucceeded:
			stats.Succeeded++
		case StatusFailed:
			stats.Failed++
		case StatusCanceled:
			stats.Canceled++
		}
	}
	return stats
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		e := q.next()
		if e != nil {
			q.run(e)
			continue
		}
		select {
		case <-q.wake:
			continue
		case <-q.ctx.Done():
			return
		}
	}
}

func (q *Queue) next() *entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.entries {
		if e.task.Status == StatusQueued {
			return e
		}
	}
	return nil
}

func (q *Queue) run(e *entry) {
	q.mu.Lock()
	q.running = e
	e.task.Status = StatusRunning
	e.task.Progress = 1
	e.task.Step = "开始执行"
	e.task.StartedAt = nowString()
	ctx, cancel := context.WithCancel(q.ctx)
	e.cancel = cancel
	q.appendLogLocked(e.task, "info", "开始执行任务")
	q.saveLocked()
	handler := q.handlers[e.task.Kind]
	snapshot := e.task.CloneSnapshot()
	q.mu.Unlock()
	q.emitSafe(snapshot)

	var (
		result map[string]any
		runErr error
	)
	if handler == nil {
		runErr = fmt.Errorf("任务类型未注册: %s", e.task.Kind)
	} else {
		func() {
			defer func() {
				if r := recover(); r != nil {
					runErr = fmt.Errorf("任务执行发生异常: %v", r)
				}
			}()
			result, runErr = handler(ctx, &Reporter{queue: q, entry: e}, e.payload)
		}()
	}

	q.mu.Lock()
	e.cancel = nil
	// 先判断取消状态，再释放 ctx。否则 ctx.Err() 会永远是 Canceled。
	canceled := runErr != nil && (e.cancelRequested || errors.Is(runErr, context.Canceled))
	cancel()
	switch {
	case canceled:
		e.task.Status = StatusCanceled
		e.task.Step = "已取消"
		e.task.Error = ""
		q.appendLogLocked(e.task, "warn", "任务已取消")
	case runErr != nil:
		e.task.Status = StatusFailed
		e.task.Step = "执行失败"
		e.task.Error = runErr.Error()
		q.appendLogLocked(e.task, "error", runErr.Error())
	default:
		e.task.Status = StatusSucceeded
		e.task.Progress = 100
		e.task.Step = "执行完成"
		e.task.Result = result
		q.appendLogLocked(e.task, "info", "任务执行成功")
	}
	e.task.FinishedAt = nowString()
	q.running = nil
	q.saveLocked()
	snapshot = e.task.CloneSnapshot()
	q.mu.Unlock()

	q.emitSafe(snapshot)
}

func (q *Queue) updateProgress(e *entry, percent int, step string) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	q.mu.Lock()
	if e.task.Status != StatusRunning {
		q.mu.Unlock()
		return
	}
	changed := false
	if percent > e.task.Progress {
		e.task.Progress = percent
		changed = true
	}
	if step != "" && step != e.task.Step {
		e.task.Step = step
		q.appendLogLocked(e.task, "info", step)
		changed = true
	}
	if !changed {
		q.mu.Unlock()
		return
	}
	q.saveLocked()
	snapshot := e.task.CloneSnapshot()
	q.mu.Unlock()

	q.emitSafe(snapshot)
}

func (q *Queue) appendLog(e *entry, level, message string) {
	q.mu.Lock()
	if e.task.Status != StatusRunning {
		q.mu.Unlock()
		return
	}
	q.appendLogLocked(e.task, level, message)
	q.saveLocked()
	snapshot := e.task.CloneSnapshot()
	q.mu.Unlock()

	q.emitSafe(snapshot)
}

func (q *Queue) appendLogLocked(task *Task, level, message string) {
	task.Logs = append(task.Logs, TaskLog{
		Time:    nowString(),
		Level:   level,
		Message: message,
	})
	if len(task.Logs) > maxLogs {
		task.Logs = task.Logs[len(task.Logs)-maxLogs:]
	}
}

func (q *Queue) newIDLocked(t time.Time) string {
	for {
		buf := make([]byte, 3)
		_, _ = rand.Read(buf)
		id := "T" + t.Format("20060102150405") + strings.ToUpper(hex.EncodeToString(buf))
		if q.byID[id] == nil {
			return id
		}
	}
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) emitSafe(task Task) {
	q.mu.Lock()
	fn := q.emit
	q.mu.Unlock()
	if fn == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	fn(task)
}

func (q *Queue) trimLocked() {
	for len(q.entries) > maxTasks {
		removed := false
		for i, e := range q.entries {
			if e.task.Status == StatusRunning || e.task.Status == StatusQueued {
				continue
			}
			q.entries = append(q.entries[:i], q.entries[i+1:]...)
			delete(q.byID, e.task.ID)
			removed = true
			break
		}
		if !removed {
			return
		}
	}
}

type persistedQueue struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

func (q *Queue) load() error {
	data, err := os.ReadFile(q.storePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var persisted persistedQueue
	if err := json.Unmarshal(data, &persisted); err != nil {
		// 文件损坏时备份并忽略，避免应用无法启动。
		_ = os.Rename(q.storePath, q.storePath+".corrupt")
		return nil
	}

	for i := range persisted.Tasks {
		task := persisted.Tasks[i]
		if task.Logs == nil {
			task.Logs = []TaskLog{}
		}
		switch task.Status {
		case StatusRunning:
			task.Status = StatusFailed
			task.Error = "应用退出导致任务中断"
			task.Step = "任务中断"
			task.FinishedAt = nowString()
			task.Logs = append(task.Logs, TaskLog{Time: nowString(), Level: "error", Message: "应用退出导致任务中断"})
		case StatusQueued:
			task.Status = StatusCanceled
			task.Step = "应用重启，排队任务已取消"
			task.FinishedAt = nowString()
			task.Logs = append(task.Logs, TaskLog{Time: nowString(), Level: "warn", Message: "应用重启，排队任务已取消"})
		}
		e := &entry{task: &task}
		q.entries = append(q.entries, e)
		q.byID[task.ID] = e
	}
	q.saveLocked()
	return nil
}

func (q *Queue) saveLocked() {
	if q.storePath == "" {
		return
	}
	tasks := make([]Task, 0, len(q.entries))
	for _, e := range q.entries {
		tasks = append(tasks, e.task.CloneSnapshot())
	}
	data, err := json.MarshalIndent(persistedQueue{Version: 1, Tasks: tasks}, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(q.storePath), 0o755); err != nil {
		return
	}
	tmp := q.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, q.storePath)
}
