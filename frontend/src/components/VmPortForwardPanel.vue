<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useDialog, useMessage } from "naive-ui";
import { HyperVService, errorMessage } from "../api";
import type { NATNetwork, NATStaticMapping, VM } from "../api";
import { useAppStore } from "../stores/app";

const props = defineProps<{
  vm: VM;
  natNetworks: NATNetwork[];
}>();

const emit = defineEmits<{
  (event: "changed"): void;
}>();

const router = useRouter();
const message = useMessage();
const dialog = useDialog();
const app = useAppStore();

const showAdd = ref(false);
const adding = ref(false);
const selectedNat = ref<string | null>(null);
const internalIP = ref("");
const externalPort = ref(3389);
const internalPort = ref(3389);

const vmIPs = computed(() => {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const adapter of props.vm.networks ?? []) {
    for (const raw of adapter.ipAddresses ?? []) {
      const ip = raw.trim();
      if (!ip || ip.includes(":") || seen.has(ip)) continue;
      seen.add(ip);
      result.push(ip);
    }
  }
  return result;
});

const ipOptions = computed(() =>
  vmIPs.value.map((ip) => ({ label: ip, value: ip }))
);

const natOptions = computed(() =>
  props.natNetworks.map((nat) => ({
    label: `${nat.name}（${nat.prefix || "未知网段"}）`,
    value: nat.name,
  }))
);

const rules = computed(() => {
  const ips = new Set(vmIPs.value);
  const result: { nat: NATNetwork; rule: NATStaticMapping }[] = [];
  for (const nat of props.natNetworks) {
    for (const rule of nat.staticMappings ?? []) {
      if (ips.has(rule.internalIPAddress)) {
        result.push({ nat, rule });
      }
    }
  }
  return result;
});

const isLocalHost = computed(() => !app.activeHost || app.activeHost.isLocal);

function openAdd() {
  selectedNat.value = natOptions.value[0]?.value ?? null;
  internalIP.value = vmIPs.value[0] ?? "";
  externalPort.value = 3389;
  internalPort.value = 3389;
  showAdd.value = true;
}

async function add() {
  if (!selectedNat.value) {
    message.warning("请选择 NAT 网络");
    return;
  }
  const ip = internalIP.value.trim();
  if (!/^(?:\d{1,3}\.){3}\d{1,3}$/.test(ip)) {
    message.warning("请输入虚拟机内部的 IPv4 地址");
    return;
  }
  if (externalPort.value < 1 || externalPort.value > 65535) {
    message.warning("宿主机端口必须在 1 到 65535 之间");
    return;
  }
  if (internalPort.value < 1 || internalPort.value > 65535) {
    message.warning("虚拟机端口必须在 1 到 65535 之间");
    return;
  }
  adding.value = true;
  try {
    await HyperVService.AddNATStaticMapping({
      natName: selectedNat.value,
      protocol: "TCP",
      externalPort: Number(externalPort.value),
      internalIPAddress: ip,
      internalPort: Number(internalPort.value),
    });
    message.success("端口转发已添加");
    showAdd.value = false;
    emit("changed");
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    adding.value = false;
  }
}

function remove(nat: NATNetwork, rule: NATStaticMapping) {
  dialog.warning({
    title: "删除端口转发",
    content: `确定删除宿主机 ${rule.externalPort} → ${rule.internalIPAddress}:${rule.internalPort} 吗？公网映射不会自动删除。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await HyperVService.RemoveNATStaticMapping({
          natName: nat.name,
          protocol: rule.protocol,
          externalPort: rule.externalPort,
        });
        message.success("端口转发已删除");
        emit("changed");
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

function mapToTunnel(nat: NATNetwork, rule: NATStaticMapping) {
  const targetIP = isLocalHost.value
    ? "127.0.0.1"
    : nat.externalIP || nat.hostIP || "127.0.0.1";
  dialog.warning({
    title: "把端口转发映射到公网",
    content:
      `将把「宿主机 ${rule.externalPort} → ${rule.internalIPAddress}:${rule.internalPort}」` +
      `添加到内网穿透，frp 本地目标为 ${targetIP}:${rule.externalPort}，远程端口由 frps 随机分配。` +
      `\n\n添加后请到“内网穿透”页面填写 frps 地址并启用。`,
    positiveText: "添加映射",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const saved = await HyperVService.AddTunnelMapping({
          name: `${props.vm.name}-${rule.externalPort}`,
          localIP: targetIP,
          localPort: rule.externalPort,
          remotePort: 0,
        });
        if (saved) {
          app.settings = saved;
        }
        message.success("已加入内网穿透映射，请到“内网穿透”页面填写 frps 并启用");
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

function openTunnelPage() {
  void router.push("/tunnel");
}
</script>

<template>
  <div class="port-forward">
    <div class="port-forward-head">
      <div class="form-hint">
        虚拟机 IPv4：
        <span v-if="vmIPs.length" class="mono-text">{{ vmIPs.join("、") }}</span>
        <span v-else>未获取到（请先在虚拟机内配置 NAT 网段的静态 IP，并确认集成服务正常）</span>
      </div>
      <n-space size="small">
        <n-button size="tiny" @click="openTunnelPage">打开内网穿透</n-button>
        <n-button
          size="tiny"
          type="primary"
          :disabled="natNetworks.length === 0"
          @click="openAdd"
        >
          添加端口转发
        </n-button>
      </n-space>
    </div>

    <n-alert v-if="natNetworks.length === 0" type="info" :show-icon="true">
      还没有 NAT 网络。请先到“虚拟交换机”页面创建“内部网络 NAT”，再把虚拟机网卡连接到该内部交换机，
      并在虚拟机里设置同网段静态 IP。
    </n-alert>

    <div v-else-if="rules.length === 0" class="empty-hint">
      这台虚拟机还没有端口转发。点击“添加端口转发”，把宿主机端口指向虚拟机的服务端口。
    </div>

    <div
      v-for="item in rules"
      :key="`${item.nat.name}-${item.rule.protocol}-${item.rule.externalPort}`"
      class="forward-row"
    >
      <div class="forward-main">
        <div class="forward-title">
          <span class="forward-proto">{{ item.rule.protocol }}</span>
          <span class="mono-text">宿主机 {{ item.rule.externalPort }}</span>
          <span class="forward-arrow">→</span>
          <span class="mono-text">{{ item.rule.internalIPAddress }}:{{ item.rule.internalPort }}</span>
        </div>
        <div class="forward-meta">
          NAT 网络：{{ item.nat.name }} · 宿主机 IP：{{ item.nat.hostIP || "未知" }}
        </div>
      </div>
      <n-space size="small">
        <n-button size="tiny" @click="mapToTunnel(item.nat, item.rule)">映射到公网</n-button>
        <n-button
          size="tiny"
          quaternary
          type="error"
          @click="remove(item.nat, item.rule)"
        >
          删除
        </n-button>
      </n-space>
    </div>

    <div class="forward-steps">
      步骤：1. 虚拟机使用 NAT 网段内的静态 IP，网关填 NAT 主机 IP（例如 192.168.100.1）；
      2. 在这里添加「宿主机端口 → 虚拟机 IP:服务端口」；
      3. 点“映射到公网”把这条转发加入内网穿透；4. 到“内网穿透”页面填写 frps 地址并启用。
    </div>

    <n-modal
      v-model:show="showAdd"
      preset="card"
      title="添加端口转发"
      style="width: 520px"
    >
      <n-form label-placement="top" size="small">
        <n-form-item label="NAT 网络">
          <n-select v-model:value="selectedNat" :options="natOptions" />
        </n-form-item>
        <n-form-item label="虚拟机内部 IP">
          <n-auto-complete
            v-model:value="internalIP"
            :options="ipOptions"
            placeholder="例如 192.168.100.2"
          />
        </n-form-item>
        <div class="forward-form-grid">
          <n-form-item label="宿主机外部端口">
            <n-input-number
              v-model:value="externalPort"
              :min="1"
              :max="65535"
              style="width: 100%"
            />
          </n-form-item>
          <n-form-item label="虚拟机内部端口">
            <n-input-number
              v-model:value="internalPort"
              :min="1"
              :max="65535"
              style="width: 100%"
            />
          </n-form-item>
        </div>
        <n-alert type="info" :show-icon="true">
          外部端口监听在宿主机所有网卡。虚拟机必须先有 NAT 网段的静态 IP，否则转发目标不存在。
          目前端口转发为 TCP。
        </n-alert>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button size="small" @click="showAdd = false">取消</n-button>
          <n-button size="small" type="primary" :loading="adding" @click="add">
            添加
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.port-forward-head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  justify-content: space-between;
  margin-bottom: 8px;
}

.forward-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-inset);
  margin-top: 8px;
}

.forward-main {
  flex: 1;
  min-width: 0;
}

.forward-title {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  font-size: 12px;
}

.forward-proto {
  display: inline-flex;
  align-items: center;
  height: 18px;
  padding: 0 6px;
  border-radius: 4px;
  background: var(--net-blue-bg);
  color: var(--net-blue-fg);
  font-size: 10px;
}

.forward-arrow {
  color: var(--text-3);
}

.forward-meta {
  margin-top: 3px;
  font-size: 11px;
  color: var(--text-3);
}

.forward-steps {
  margin-top: 10px;
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--text-3);
}

.forward-form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
</style>
