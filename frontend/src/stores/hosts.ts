import { defineStore } from "pinia";
import { HyperVService, errorMessage } from "../api";
import type { HostItem, HostRequest } from "../api";

export const useHostsStore = defineStore("hosts", {
  state: () => ({
    items: [] as HostItem[],
    loading: false,
    error: "",
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
      await this.fetch();
    },
    async connect(id: string, password: string) {
      const item = await HyperVService.ConnectHost(id, password);
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
