<script setup lang="ts">
import { computed } from "vue";
import { vmStateLabel, vmStateType } from "../utils/format";

const props = defineProps<{ state: string }>();
const label = computed(() => vmStateLabel(props.state));
const type = computed(() => vmStateType(props.state));
</script>

<template>
  <n-tag :type="type" size="small" :bordered="false">
    {{ label }}
  </n-tag>
</template>
