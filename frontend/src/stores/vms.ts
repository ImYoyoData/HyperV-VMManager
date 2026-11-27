import { defineStore } from "pinia";
import { HyperVService, errorMessage } from "../api";
import type { VM } from "../api";

interface FetchOptions {
  /** 后台刷新：不触发表格 loading 遮罩，不阻塞用户操作。 */
  silent?: boolean;
  /** 跳过短时间内的去重，强制重新读取。 */
  force?: boolean;
}

// 同一时间只允许一个列表请求。切换主机、任务完成和定时刷新可能同时触发，
// 合并成一次调用可以避免重复启动 PowerShell，明显降低 CPU 与内存占用。
let inFlightFetch: Promise<void> | null = null;
let lastFetchAt = 0;
// 切换主机时递增，用来丢弃旧主机仍在途的请求结果。
let fetchGeneration = 0;
// 静默刷新在这个时间窗口内复用上一次结果，避免同一秒内的重复请求。
const SILENT_DEDUPE_MS = 1500;
// 普通刷新也做一个很短的合并窗口，避免首屏多处同时触发。
const FETCH_DEDUPE_MS = 800;

const inFlightDetails = new Map<string, Promise<VM | null>>();

export const useVMStore = defineStore("vms", {
  state: () => ({
    items: [] as VM[],
    // 已单独拉取过完整详情的虚拟机（含磁盘、DVD、嵌套虚拟化）。
    details: {} as Record<string, VM>,
    loading: false,
    refreshing: false,
    error: "",
    lastUpdated: "",
  }),
  getters: {
    runningCount: (state) => state.items.filter((vm) => vm.state === "Running").length,
    totalCount: (state) => state.items.length,
    byName: (state) => (name: string) => state.items.find((vm) => vm.name === name),
    names: (state) => state.items.map((vm) => vm.name),
  },
  actions: {
    async fetch(options: FetchOptions = {}) {
      if (inFlightFetch) {
        if (options.force) {
          // 强制刷新时等待当前请求结束，再发起一次新请求，
          // 避免任务刚结束时拿到任务开始前的旧列表。
          await inFlightFetch;
          return this.fetch({ ...options, force: true });
        }
        return inFlightFetch;
      }
      if (!options.force) {
        const window = options.silent ? SILENT_DEDUPE_MS : FETCH_DEDUPE_MS;
        if (Date.now() - lastFetchAt < window) return;
      }

      const generation = fetchGeneration;
      if (options.silent) {
        this.refreshing = true;
      } else {
        this.loading = true;
      }
      this.error = "";

      const run = (async () => {
        try {
          const items = await HyperVService.ListVMSummaries();
          if (generation !== fetchGeneration) return;
          this.applySummaries(items ?? []);
          lastFetchAt = Date.now();
          this.lastUpdated = new Date().toISOString();
        } catch (err) {
          if (generation === fetchGeneration) {
            this.error = errorMessage(err);
          }
        } finally {
          if (generation === fetchGeneration) {
            this.loading = false;
            this.refreshing = false;
          }
        }
      })();

      inFlightFetch = run;
      try {
        await run;
      } finally {
        if (inFlightFetch === run) {
          inFlightFetch = null;
        }
      }
    },
    applySummaries(items: VM[]) {
      const names = new Set(items.map((vm) => vm.name));
      // 删除已经不存在的虚拟机的详情缓存。
      for (const name of Object.keys(this.details)) {
        if (!names.has(name)) delete this.details[name];
      }
      this.items = items.map((summary) => {
        const detail = this.details[summary.name];
        if (!detail) return summary;
        // 列表字段用最新摘要，磁盘等重字段沿用已拉取的详情。
        return {
          ...summary,
          hardDrives: detail.hardDrives,
          dvdDrives: detail.dvdDrives,
          nestedVirtualization: detail.nestedVirtualization,
        };
      });
    },
    async fetchDetail(name: string): Promise<VM | null> {
      const existing = inFlightDetails.get(name);
      if (existing) return existing;

      const generation = fetchGeneration;
      const run = (async () => {
        try {
          const detail = await HyperVService.GetVM(name);
          if (!detail || generation !== fetchGeneration) return null;
          this.details[name] = detail;
          const index = this.items.findIndex((vm) => vm.name === name);
          if (index >= 0) {
            this.items[index] = {
              ...this.items[index],
              ...detail,
            };
          }
          return detail;
        } catch (err) {
          if (generation === fetchGeneration) {
            this.error = errorMessage(err);
          }
          return null;
        } finally {
          inFlightDetails.delete(name);
        }
      })();

      inFlightDetails.set(name, run);
      return run;
    },
    clear() {
      fetchGeneration++;
      this.items = [];
      this.details = {};
      this.error = "";
      lastFetchAt = 0;
      inFlightFetch = null;
      inFlightDetails.clear();
    },
  },
});
