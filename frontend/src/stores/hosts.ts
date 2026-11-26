import { defineStore } from "pinia";
import { HyperVService, errorMessage } from "../api";
import type { HostItem, HostRequest } from "../api";

export const useHostsStore = defineStore("hosts", {
  state: () => ({
    items: [] as HostItem[],
    loading: false,
    error: "",
    // 仅保存在当前应用内存中的密码，用于切换主机后打开 VMConnect。
    // 不写入任何持久化存储，应用退出即清空。
    passwords: {} as Record<string, string>,
  }),
  getters: {
    active: (state) => state.items.find((item) => item.active) ?? null,
    remoteCount: (state) => state.items.filter((item) => !item.isLocal).length,
  },
  actions: {
    async fetch() {
      this.loading = true;
      this.error = "";
      try {
        const items = await HyperVService.ListHosts();
        this.items = items ?? [];
      } catch (err) {
        this.error = errorMessage(err);
      } finally {
        this.loading = false;
      }
    },
    async add(request: HostRequest) {
      const item = await HyperVService.AddHost(request);
      await this.fetch();
      return item;
    },
    async update(request: HostRequest) {
      const item = await HyperVService.UpdateHost(request);
      await this.fetch();
      return item;
    },
    async remove(id: string) {
      await HyperVService.RemoveHost(id);
      delete this.passwords[id];
      await this.fetch();
    },
    async connect(id: string, password: string, remember = true) {
      const item = await HyperVService.ConnectHost(id, password, remember);
      if (password) {
        this.passwords[id] = password;
      } else {
        delete this.passwords[id];
      }
      await this.fetch();
      return item;
    },
    async setActive(id: string) {
      const item = await HyperVService.SetActiveHost(id);
      await this.fetch();
      return item;
    },
  },
});
