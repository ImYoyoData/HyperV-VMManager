<script setup lang="ts">
defineProps<{
  label: string;
  value: string | number;
  hint?: string;
}>();
</script>

<template>
  <div class="stat-card">
    <div class="stat-label">
      <slot name="icon" />
      <span>{{ label }}</span>
    </div>
    <div class="stat-value">{{ value }}</div>
    <div v-if="hint" class="stat-hint">{{ hint }}</div>
  </div>
</template>
