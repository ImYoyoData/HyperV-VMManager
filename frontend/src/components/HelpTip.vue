<script setup lang="ts">
import { HelpCircleOutline } from "@vicons/ionicons5";
</script>

<template>
  <n-popover trigger="hover" placement="top" :style="{ maxWidth: '360px' }">
    <template #trigger>
      <n-icon class="help-tip-icon" :component="HelpCircleOutline" />
    </template>
    <div class="help-tip-content">
      <slot />
    </div>
  </n-popover>
</template>

<style scoped>
.help-tip-icon {
  font-size: 14px;
  color: #7f889b;
  vertical-align: -2px;
  margin-left: 5px;
  cursor: help;
  transition: color 0.15s ease;
}

.help-tip-icon:hover {
  color: #9fb0cc;
}

.help-tip-content {
  font-size: 12px;
  line-height: 1.7;
  color: #cfd6e4;
}

.help-tip-content :deep(p) {
  margin: 0 0 6px;
}

.help-tip-content :deep(p:last-child) {
  margin-bottom: 0;
}

.help-tip-content :deep(ul) {
  margin: 0;
  padding-left: 16px;
}

.help-tip-content :deep(strong) {
  color: #eef1f7;
}
</style>
