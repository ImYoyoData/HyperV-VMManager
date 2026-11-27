import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { TaskFinished, TaskUpdate } from "../../bindings/events";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type { Task } from "../api";

let webPollTimer: ReturnType<typeof setInterval> | undefined;
let offTaskUpdate: (() => void) | undefined;
let offTaskFinished: (() => void) | undefined;

export const useTaskStore = defineStore("tasks", {
  state: () => ({
    items: [] as Task[],
    loading: false,
    error: "",
    bound: false,
  }),
  getters: {
    pendingCount: (state) =>
      state.items.filter((task) => task.status === "queued" || task.status === "running").length,
    runningCount: (state) => state.items.filter((task) => task.status === "running").length,
    failedCount: (state) => state.items.filter((task) => task.status === "failed").length,
    finishedCount: (state) =>
      state.items.filter(
        (task) =>
          task.status === "succeeded" || task.status === "failed" || task.status === "canceled"
      ).length,
    recent: (state) => state.items.slice(0, 5),
  },
  actions: {
    async fetch() {
      if (this.loading) return;
      this.loading = true;
      this.error = "";
      try {
        const items = (await HyperVService.ListTasks(100)) ?? [];
        // 列表接口只返回最近 20 条日志；如果详情页已经拉取过完整日志，
        // 保留完整日志并把列表中的新日志合并进来，避免轮询把日志清空。
        const previous = new Map(this.items.map((item) => [item.id, item]));
        this.items = items.map((task) => {
          const existing = previous.get(task.id);
          const existingLogs = existing?.logs ?? [];
          const incomingLogs = task.logs ?? [];
          if (existingLogs.length <= incomingLogs.length) {
            return task;
          }
          const seen = new Set(existingLogs.map((log) => `${log.time}|${log.message}`));
          const merged = [
            ...existingLogs,
            ...incomingLogs.filter((log) => !seen.has(`${log.time}|${log.message}`)),
          ];
          return { ...task, logs: merged.slice(-300) };
        });
      } catch (err) {
        this.error = errorMessage(err);
      } finally {
        this.loading = false;
      }
    },
    upsert(task: Task) {
      const index = this.items.findIndex((item) => item.id === task.id);
      if (index >= 0) {
        this.items[index] = task;
      } else {
        this.items.unshift(task);
      }
    },
    bindEvents() {
      if (this.bound) return;
      this.bound = true;
      if (isWebMode) {
        // 浏览器没有 Wails 事件通道，用轮询刷新任务进度；页面不可见时暂停。
        webPollTimer = setInterval(() => {
          if (!document.hidden) {
            void this.fetch();
          }
        }, 3000);
        return;
      }
      const offUpdate = Events.On(TaskUpdate, (event: { data: Task }) => {
        if (event?.data) {
          this.upsert(event.data);
        }
      });
      const offFinished = Events.On(TaskFinished, (event: { data: Task }) => {
        if (event?.data) {
          this.upsert(event.data);
        }
      });
      offTaskUpdate = typeof offUpdate === "function" ? offUpdate : undefined;
      offTaskFinished = typeof offFinished === "function" ? offFinished : undefined;
    },
    stopEvents() {
      if (webPollTimer) {
        clearInterval(webPollTimer);
        webPollTimer = undefined;
      }
      offTaskUpdate?.();
      offTaskFinished?.();
      offTaskUpdate = undefined;
      offTaskFinished = undefined;
      this.bound = false;
    },
    async cancel(id: string) {
      await HyperVService.CancelTask(id);
    },
    async clearFinished() {
      const removed = await HyperVService.ClearFinishedTasks();
      this.items = this.items.filter(
        (task) =>
          task.status === "queued" || task.status === "running"
      );
      return removed;
    },
  },
});
