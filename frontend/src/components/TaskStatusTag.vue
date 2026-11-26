<script setup lang="ts">
import { computed } from "vue";
import { taskStatusLabel, taskStatusType } from "../utils/format";

const props = defineProps<{ status: string }>();
const label = computed(() => taskStatusLabel(props.status));
const type = computed(() => taskStatusType(props.status));
</script>

<template>
  <n-tag :type="type" size="small" :bordered="false">
    {{ label }}
  </n-tag>
</template>
