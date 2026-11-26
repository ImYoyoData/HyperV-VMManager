<script setup lang="ts">
defineProps<{
  title: string;
  subtitle?: string;
}>();
</script>

<template>
  <div class="page-header">
    <div class="page-header-text">
      <h1 class="page-title">{{ title }}</h1>
      <div v-if="subtitle" class="page-subtitle">{{ subtitle }}</div>
    </div>
    <div class="page-actions">
      <slot />
    </div>
  </div>
</template>
