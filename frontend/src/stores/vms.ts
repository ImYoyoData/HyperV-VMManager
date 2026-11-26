import { defineStore } from "pinia";
import { HyperVService, errorMessage } from "../api";
import type { VM } from "../api";

export const useVMStore = defineStore("vms", {
  state: () => ({
    items: [] as VM[],
    loading: false,
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
    async fetch() {
      if (this.loading) return;
      this.loading = true;
      this.error = "";
      try {
        const items = await HyperVService.ListVMs();
        this.items = items ?? [];
        this.lastUpdated = new Date().toISOString();
      } catch (err) {
        this.error = errorMessage(err);
      } finally {
        this.loading = false;
      }
    },
    clear() {
      this.items = [];
      this.error = "";
    },
  },
});
