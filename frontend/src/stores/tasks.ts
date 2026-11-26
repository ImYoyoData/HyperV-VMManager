import { defineStore } from "pinia";
import { Events } from "@wailsio/runtime";
import { TaskFinished, TaskUpdate } from "../../bindings/events";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type { Task } from "../api";

let webPollTimer: ReturnType<typeof setInterval> | undefined;

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
        const items = await HyperVService.ListTasks(100);
        this.items = items ?? [];
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
        // 浏览器没有 Wails 事件通道，用轮询刷新任务进度。
        webPollTimer = setInterval(() => {
          void this.fetch();
        }, 3000);
        return;
      }
      Events.On(TaskUpdate, (event: { data: Task }) => {
        if (event?.data) {
          this.upsert(event.data);
        }
      });
      Events.On(TaskFinished, (event: { data: Task }) => {
        if (event?.data) {
          this.upsert(event.data);
        }
      });
    },
    stopEvents() {
      if (webPollTimer) {
        clearInterval(webPollTimer);
        webPollTimer = undefined;
      }
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
