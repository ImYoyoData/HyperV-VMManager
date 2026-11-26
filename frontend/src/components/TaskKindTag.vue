<script setup lang="ts">
import { computed } from "vue";
import { taskKindLabel, taskKindTag } from "../utils/format";

const props = defineProps<{ kind: string }>();
const label = computed(() => taskKindLabel(props.kind));
const type = computed(() => taskKindTag(props.kind));
</script>

<template>
  <n-tag :type="type" size="small" :bordered="false">
    {{ label }}
  </n-tag>
</template>
