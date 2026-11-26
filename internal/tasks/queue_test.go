package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hypervvmmanager/internal/tasks"
)

func waitStatus(t *testing.T, queue *tasks.Queue, id string, status string, timeout time.Duration) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := queue.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return *task
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, _ := queue.Get(id)
	t.Fatalf("任务 %s 未在 %s 内进入状态 %s，当前状态 %s，错误 %s", id, timeout, status, task.Status, task.Error)
	return tasks.Task{}
}

func TestQueueRunsTaskAndPersists(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tasks.json")
	queue, err := tasks.NewQueue(storePath)
	if err != nil {
		t.Fatal(err)
	}

	queue.Register("demo", func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		rep.Progress(30, "第一步")
		rep.Log("收到参数：%s", string(payload))
		time.Sleep(30 * time.Millisecond)
		rep.Progress(80, "第二步")
		return map[string]any{"ok": true}, nil
	})

	queue.Start(context.Background())
	defer queue.Stop()

	task, err := queue.Submit("demo", "target-1", map[string]string{"name": "测试"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusQueued {
		t.Fatalf("提交后状态应为 queued，实际 %s", task.Status)
	}

	finished := waitStatus(t, queue, task.ID, tasks.StatusSucceeded, 5*time.Second)
	if finished.Progress != 100 {
		t.Fatalf("成功任务进度应为 100，实际 %d", finished.Progress)
	}
	if len(finished.Logs) < 3 {
		t.Fatalf("日志数量不足: %+v", finished.Logs)
	}
	if finished.Result["ok"] != true {
		t.Fatalf("任务结果不正确: %+v", finished.Result)
	}

	// 重新打开队列，验证历史持久化与状态恢复。
	queue.Stop()
	reloaded, err := tasks.NewQueue(storePath)
	if err != nil {
		t.Fatal(err)
	}
	items := reloaded.List(10)
	if len(items) != 1 {
		t.Fatalf("持久化任务数量应为 1，实际 %d", len(items))
	}
	if items[0].Status != tasks.StatusSucceeded {
		t.Fatalf("重启后任务状态应保持 succeeded，实际 %s", items[0].Status)
	}
}

func TestQueueRunsSerially(t *testing.T) {
	queue, err := tasks.NewQueue(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}

	order := make(chan string, 4)
	queue.Register("serial", func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		order <- strings.Trim(string(payload), "\"")
		time.Sleep(30 * time.Millisecond)
		return nil, nil
	})
	queue.Start(context.Background())
	defer queue.Stop()

	first, _ := queue.Submit("serial", "a", "a")
	second, _ := queue.Submit("serial", "b", "b")
	waitStatus(t, queue, first.ID, tasks.StatusSucceeded, 5*time.Second)
	waitStatus(t, queue, second.ID, tasks.StatusSucceeded, 5*time.Second)

	close(order)
	var received []string
	for value := range order {
		received = append(received, value)
	}
	if len(received) != 2 || received[0] != "a" || received[1] != "b" {
		t.Fatalf("任务未按 FIFO 串行执行: %v", received)
	}
}

func TestQueueCancelRunningTask(t *testing.T) {
	queue, err := tasks.NewQueue(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	queue.Register("slow", func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	queue.Start(context.Background())
	defer queue.Stop()

	task, _ := queue.Submit("slow", "slow-target", map[string]string{})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("任务未开始")
	}

	if err := queue.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	canceled := waitStatus(t, queue, task.ID, tasks.StatusCanceled, 5*time.Second)
	if canceled.Error != "" {
		t.Fatalf("取消的任务错误信息应为空，实际 %q", canceled.Error)
	}
}

func TestQueueCancelQueuedTask(t *testing.T) {
	queue, err := tasks.NewQueue(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}

	block := make(chan struct{})
	queue.Register("block", func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		<-block
		return nil, nil
	})
	queue.Start(context.Background())
	defer queue.Stop()

	first, _ := queue.Submit("block", "first", map[string]string{})
	second, _ := queue.Submit("block", "second", map[string]string{})

	waitStatus(t, queue, first.ID, tasks.StatusRunning, 3*time.Second)
	if err := queue.Cancel(second.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, queue, second.ID, tasks.StatusCanceled, 3*time.Second)
	close(block)
	waitStatus(t, queue, first.ID, tasks.StatusSucceeded, 3*time.Second)
}

func TestQueueFailedTask(t *testing.T) {
	queue, err := tasks.NewQueue(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	queue.Register("fail", func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		return nil, errors.New("模拟失败")
	})
	queue.Start(context.Background())
	defer queue.Stop()

	task, _ := queue.Submit("fail", "target", map[string]string{})
	failed := waitStatus(t, queue, task.ID, tasks.StatusFailed, 3*time.Second)
	if failed.Error != "模拟失败" {
		t.Fatalf("失败原因不正确: %q", failed.Error)
	}
}

func TestQueueClearFinished(t *testing.T) {
	queue, err := tasks.NewQueue(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	queue.Register("ok", func(ctx context.Context, rep *tasks.Reporter, payload json.RawMessage) (map[string]any, error) {
		return nil, nil
	})
	queue.Start(context.Background())
	defer queue.Stop()

	task, _ := queue.Submit("ok", "target", map[string]string{})
	waitStatus(t, queue, task.ID, tasks.StatusSucceeded, 3*time.Second)
	removed := queue.ClearFinished()
	if removed != 1 {
		t.Fatalf("清理数量应为 1，实际 %d", removed)
	}
	if len(queue.List(10)) != 0 {
		t.Fatal("清理后任务列表应为空")
	}
}
