import { defineStore } from "pinia";
import { HyperVService, errorMessage } from "../api";
import type { EnvironmentStatus } from "../api";

const SKIP_KEY = "hvvm.setup.skipped";

// 启动与主机切换可能同时触发环境检查，合并成一次避免重复启动 PowerShell。
let inFlightCheck: Promise<void> | null = null;

function readSkipped(): boolean {
  try {
    return sessionStorage.getItem(SKIP_KEY) === "1";
  } catch {
    return false;
  }
}

export const useSetupStore = defineStore("setup", {
  state: () => ({
    status: null as EnvironmentStatus | null,
    loading: false,
    error: "",
    checked: false,
    skipped: readSkipped(),
    fixing: false,
    fixingError: "",
  }),
  getters: {
    checks: (state) => state.status?.checks ?? [],
    ready: (state) => !!state.status?.ready,
    needsAction: (state) => state.status?.overall === "needs-action",
    unsupported: (state) => state.status?.overall === "unsupported",
  },
  actions: {
    async check(options: { force?: boolean } = {}) {
      if (inFlightCheck) {
        if (options.force) {
          await inFlightCheck;
          return this.check({ force: true });
        }
        return inFlightCheck;
      }
      this.loading = true;
      this.error = "";

      const run = (async () => {
        try {
          const status = await HyperVService.GetEnvironmentStatus();
          if (!status) {
            throw new Error("服务未返回环境检测结果");
          }
          this.status = status;
          this.checked = true;
        } catch (err) {
          this.error = errorMessage(err);
        } finally {
          this.loading = false;
        }
      })();

      inFlightCheck = run;
      try {
        await run;
      } finally {
        if (inFlightCheck === run) {
          inFlightCheck = null;
        }
      }
    },
    skip() {
      this.skipped = true;
      try {
        sessionStorage.setItem(SKIP_KEY, "1");
      } catch {
        // 忽略无法写入 sessionStorage 的情况
      }
    },
    resetSkip() {
      this.skipped = false;
      try {
        sessionStorage.removeItem(SKIP_KEY);
      } catch {
        // 忽略
      }
    },
    async fixEnhancedSession() {
      this.fixing = true;
      this.fixingError = "";
      try {
        await HyperVService.SetEnhancedSession(true);
        await this.check({ force: true });
      } catch (err) {
        this.fixingError = errorMessage(err);
        throw err;
      } finally {
        this.fixing = false;
      }
    },
  },
});
