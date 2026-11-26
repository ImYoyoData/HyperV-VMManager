import { defineStore } from "pinia";
import { HyperVService, errorMessage } from "../api";
import type { Bootstrap, HostInfo, HostItem, Settings, APIStatus } from "../api";

export const useAppStore = defineStore("app", {
  state: () => ({
    loaded: false,
    loading: false,
    error: "",
    isAdmin: false,
    dataDir: "",
    hostInfo: null as HostInfo | null,
    activeHost: null as HostItem | null,
    hostError: "",
    vmError: "",
    vmCount: 0,
    runningCount: 0,
    settings: null as Settings | null,
    api: null as APIStatus | null,
    appVersion: "",
    wailsVersion: "",
  }),
  getters: {
    hostName: (state) => state.hostInfo?.computerName || "本机",
    isRemote: (state) => !!state.activeHost && !state.activeHost.isLocal,
    hostLabel: (state) => state.activeHost?.name || "本机",
    apiBaseURL: (state) =>
      state.settings && state.settings.api
        ? `http://${state.settings.api.listenAddress}:${state.settings.api.port}`
        : "",
  },
  actions: {
    async init() {
      this.loading = true;
      this.error = "";
      try {
        const data = await HyperVService.GetBootstrap();
        if (!data) {
          throw new Error("未收到应用的启动数据");
        }
        this.applyBootstrap(data);
      } catch (err) {
        this.error = errorMessage(err);
      } finally {
        this.loaded = true;
        this.loading = false;
      }
    },
    applyBootstrap(data: Bootstrap) {
      this.isAdmin = data.isAdmin;
      this.dataDir = data.dataDir;
      this.hostInfo = data.hostInfo ?? null;
      this.activeHost = data.activeHost ?? null;
      this.hostError = data.hostError ?? "";
      this.vmError = data.vmError ?? "";
      this.vmCount = data.vmCount;
      this.runningCount = data.runningCount;
      this.settings = data.settings;
      this.api = data.api;
      this.appVersion = data.appVersion;
      this.wailsVersion = data.wailsVersion;
    },
    async refreshHostInfo() {
      try {
        const info = await HyperVService.GetHostInfo();
        if (info) {
          this.hostInfo = info;
          this.hostError = "";
        }
      } catch (err) {
        this.hostError = errorMessage(err);
      }
    },
    async refreshAdmin() {
      try {
        this.isAdmin = await HyperVService.IsAdmin();
      } catch {
        this.isAdmin = false;
      }
    },
    async refreshAPIStatus() {
      try {
        this.api = await HyperVService.GetAPIStatus();
      } catch {
        // 状态读取失败时保持旧值
      }
    },
    async saveSettings(value: Settings): Promise<Settings> {
      const saved = await HyperVService.SaveSettings(value);
      if (!saved) {
        throw new Error("保存设置失败：服务未返回数据");
      }
      this.settings = saved;
      await this.refreshAPIStatus();
      return saved;
    },
  },
});
