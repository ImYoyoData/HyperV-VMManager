import { defineStore } from "pinia";

export type ThemeMode = "system" | "light" | "dark";

const STORAGE_KEY = "hvvm.theme";
const media =
  typeof window !== "undefined"
    ? window.matchMedia("(prefers-color-scheme: dark)")
    : null;

let mediaListenerAdded = false;

function readMode(): ThemeMode {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === "light" || value === "dark" || value === "system") {
      return value;
    }
  } catch {
    // 忽略存储不可用
  }
  return "system";
}

export const useThemeStore = defineStore("theme", {
  state: () => ({
    mode: readMode() as ThemeMode,
    systemDark: media?.matches ?? true,
  }),
  getters: {
    isDark: (state) => (state.mode === "dark" ? true : state.mode === "light" ? false : state.systemDark),
  },
  actions: {
    init() {
      this.apply();
      if (media && !mediaListenerAdded) {
        mediaListenerAdded = true;
        media.addEventListener("change", (event) => {
          this.systemDark = event.matches;
          if (this.mode === "system") {
            this.apply();
          }
        });
      }
    },
    setMode(mode: ThemeMode) {
      this.mode = mode;
      try {
        localStorage.setItem(STORAGE_KEY, mode);
      } catch {
        // 忽略存储不可用
      }
      this.apply();
    },
    apply() {
      if (typeof document === "undefined") return;
      document.documentElement.dataset.theme = this.isDark ? "dark" : "light";
      document.documentElement.style.colorScheme = this.isDark ? "dark" : "light";
    },
  },
});
