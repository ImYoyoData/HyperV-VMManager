<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useMessage } from "naive-ui";
import { LockClosedOutline, PersonOutline } from "@vicons/ionicons5";
import { errorMessage, isWebMode, webLogin, webMe } from "../api";

const router = useRouter();
const message = useMessage();

const form = reactive({
  username: "admin",
  password: "",
});
const loading = ref(false);

async function submit() {
  if (isWebMode === false) {
    await router.replace("/");
    return;
  }
  if (!form.username.trim() || !form.password) {
    message.warning("请输入用户名和密码");
    return;
  }
  loading.value = true;
  try {
    await webLogin(form.username.trim(), form.password);
    await router.replace("/");
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  try {
    await webMe();
    await router.replace("/");
  } catch {
    // 未登录，保留登录页
  }
});
</script>

<template>
  <div class="login-page">
    <div class="login-panel">
      <div class="login-brand">
        <img src="/logo.svg" class="login-mark" alt="" />
        <div>
          <div class="login-title">Hyper-V 管理助手</div>
          <div class="login-subtitle">网页控制台</div>
        </div>
      </div>

      <n-form label-placement="top" size="large" @submit.prevent="submit">
        <n-form-item label="用户名">
          <n-input v-model:value="form.username" placeholder="用户名">
            <template #prefix>
              <n-icon :component="PersonOutline" />
            </template>
          </n-input>
        </n-form-item>
        <n-form-item label="密码">
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            placeholder="密码"
            @keyup.enter="submit"
          >
            <template #prefix>
              <n-icon :component="LockClosedOutline" />
            </template>
          </n-input>
        </n-form-item>
      </n-form>

      <n-button
        type="primary"
        block
        size="large"
        :loading="loading"
        style="margin-top: 6px"
        @click="submit"
      >
        登录
      </n-button>

      <div class="login-hint">
        默认用户名 <span class="mono-text">admin</span>，密码可在桌面端
        “设置 → 网页控制台”中查看或修改。
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  background: var(--bg);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.login-panel {
  width: 380px;
  max-width: 100%;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 30px 28px 24px;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.25);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}

.login-mark {
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  border-radius: 11px;
  background: #2f6fed;
  color: #fff;
  font-weight: 700;
  font-size: 17px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.login-title {
  font-size: 17px;
  font-weight: 600;
  color: var(--text-strong);
}

.login-subtitle {
  font-size: 12px;
  color: var(--muted);
  margin-top: 2px;
}

.login-hint {
  margin-top: 18px;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--muted);
  text-align: center;
}
</style>
