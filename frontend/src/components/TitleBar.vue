<script setup lang="ts">
import { Window } from "@wailsio/runtime";
import {
  CloseOutline,
  RemoveOutline,
  SquareOutline,
} from "@vicons/ionicons5";

const win = Window;

async function minimise() {
  try {
    await win.Minimise();
  } catch {
    // ignore
  }
}

async function toggleMaximise() {
  try {
    await win.ToggleMaximise();
  } catch {
    // ignore
  }
}

async function close() {
  try {
    await win.Close();
  } catch {
    // ignore
  }
}
</script>

<template>
  <div class="titlebar">
    <div class="titlebar-brand">
      <img src="/logo.svg" alt="" class="titlebar-logo" />
      <span>Hyper-V VM Assistant</span>
    </div>
    <div class="titlebar-actions">
      <button class="titlebar-btn" title="最小化" @click="minimise">
        <n-icon :component="RemoveOutline" :size="15" />
      </button>
      <button class="titlebar-btn" title="最大化 / 还原" @click="toggleMaximise">
        <n-icon :component="SquareOutline" :size="13" />
      </button>
      <button class="titlebar-btn close" title="关闭" @click="close">
        <n-icon :component="CloseOutline" :size="16" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.titlebar {
  height: 36px;
  flex: 0 0 36px;
  display: flex;
  align-items: center;
  background: var(--bg-sidebar);
  border-bottom: 1px solid var(--border-soft);
  --wails-draggable: drag;
  user-select: none;
}

.titlebar-brand {
  display: flex;
  align-items: center;
  gap: 9px;
  padding-left: 12px;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-soft);
  min-width: 0;
}

.titlebar-logo {
  width: 20px;
  height: 20px;
  border-radius: 5px;
}

.titlebar-actions {
  margin-left: auto;
  display: flex;
  height: 100%;
  --wails-draggable: no-drag;
}

.titlebar-btn {
  width: 46px;
  height: 100%;
  border: none;
  background: transparent;
  color: var(--text-soft);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}

.titlebar-btn:hover {
  background: var(--panel-hover);
  color: var(--text-strong);
}

.titlebar-btn.close:hover {
  background: #d64545;
  color: #fff;
}
</style>
