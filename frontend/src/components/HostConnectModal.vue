<script setup lang="ts">
import { ref, watch } from "vue";
import { useMessage } from "naive-ui";
import { useHostsStore } from "../stores/hosts";
import { errorMessage } from "../api";
import type { HostItem } from "../api";
import ConnectHelpModal from "./ConnectHelpModal.vue";

const props = defineProps<{
  show: boolean;
  host?: HostItem | null;
}>();

const emit = defineEmits<{
  (event: "update:show", value: boolean): void;
  (event: "connected", host: HostItem): void;
}>();

const hosts = useHostsStore();
const message = useMessage();

const password = ref("");
const remember = ref(true);
const loading = ref(false);
const error = ref("");
const showHelp = ref(false);

watch(
  () => props.show,
  (show) => {
    if (show) {
      password.value = "";
      remember.value = true;
      error.value = "";
    }
  }
);

async function submit() {
  const host = props.host;
  if (!host) return;
  if (host.username && !password.value) {
    message.warning("请输入远程主机密码");
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    await hosts.connect(host.id, password.value, remember.value);
    message.success(`已连接 ${host.name}`);
    emit("connected", host);
    emit("update:show", false);
  } catch (err) {
    error.value = errorMessage(err);
    showHelp.value = true;
  } finally {
    loading.value = false;
  }
}

function close() {
  emit("update:show", false);
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="`连接 ${host?.name || ''}`"
    style="width: 470px"
    @update:show="emit('update:show', $event)"
  >
    <div v-if="host">
      <div class="connect-address mono-text">{{ host.address }}:{{ host.port }}</div>
      <n-form label-placement="top" size="small" style="margin-top: 12px">
        <n-form-item label="Windows 密码">
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            :placeholder="host.username ? '输入远程主机账户密码' : '留空使用当前 Windows 凭据'"
            @keyup.enter="submit"
          />
        </n-form-item>
        <n-checkbox v-model:checked="remember">
          记住密码（保存到 Windows 凭据管理器，下次直接连接）
        </n-checkbox>
      </n-form>
      <n-alert v-if="error" type="error" :show-icon="true" style="margin-top: 12px">
        <div class="connect-error">{{ error }}</div>
        <n-button
          size="tiny"
          type="warning"
          ghost
          style="margin-top: 8px"
          @click="showHelp = true"
        >
          查看解决方案 / 一键修复
        </n-button>
      </n-alert>
      <n-alert v-else type="info" :show-icon="true" style="margin-top: 12px">
        密码加密保存在 Windows 凭据管理器，VMConnect 与 Hyper-V 管理器也能复用；
        下次可以直接连接。
      </n-alert>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="close">取消</n-button>
        <n-button size="small" type="primary" :loading="loading" @click="submit">
          连接
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <ConnectHelpModal v-model:show="showHelp" :host="host" :error="error" />
</template>

<style scoped>
.connect-address {
  font-size: 12px;
  color: var(--text-3);
  background: var(--input-bg);
  border: 1px solid var(--border-soft);
  border-radius: 7px;
  padding: 8px 10px;
  word-break: break-all;
}

.connect-error {
  font-size: 11.5px;
  line-height: 1.7;
  max-height: 150px;
  overflow-y: auto;
  word-break: break-word;
}
</style>
