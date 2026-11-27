<script setup lang="ts">
import { h, onMounted, reactive, ref } from "vue";
import {
  NButton,
  NIcon,
  NSpace,
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { AddCircleOutline, BuildOutline, RefreshOutline } from "@vicons/ionicons5";
import PageHeader from "../components/PageHeader.vue";
import { HyperVService, errorMessage, isWebMode } from "../api";
import type {
  HostNetworkAdapter,
  NATNetwork,
  NATStaticMapping,
  VirtualSwitch,
} from "../api";
import { switchTypeLabel, switchTypeTag } from "../utils/format";

const message = useMessage();
const dialog = useDialog();

const items = ref<VirtualSwitch[]>([]);
const adapters = ref<HostNetworkAdapter[]>([]);
const loading = ref(false);
const loadError = ref("");

const showCreate = ref(false);
const creating = ref(false);
const form = reactive({
  name: "",
  switchType: "External",
  netAdapterName: "",
  allowManagementOS: true,
});

const columns: DataTableColumns<VirtualSwitch> = [
  {
    title: "名称",
    key: "name",
    width: 200,
    fixed: "left",
    render: (row) => h("span", { class: "vm-name" }, row.name),
  },
  {
    title: "类型",
    key: "switchType",
    width: 130,
    render: (row) =>
      h(
        "span",
        { class: "tag-wrapper" },
        h(
          "span",
          {
            class: "custom-tag",
            "data-type": row.switchType,
          },
          switchTypeLabel(row.switchType)
        )
      ),
  },
  {
    title: "上行网卡",
    key: "netAdapterInterfaceDescription",
    width: 240,
    ellipsis: { tooltip: true },
    render: (row) => h("span", { class: "cell-muted" }, row.netAdapterInterfaceDescription || "—"),
  },
  {
    title: "主机共享",
    key: "allowManagementOS",
    width: 100,
    render: (row) => h("span", { class: "cell-muted" }, row.allowManagementOS ? "是" : "否"),
  },
  {
    title: "状态",
    key: "status",
    width: 90,
    render: (row) => h("span", { class: "cell-muted" }, row.status || "—"),
  },
  {
    title: "操作",
    key: "actions",
    width: 120,
    align: "right",
    fixed: "right",
    render: (row) =>
      h(
        NSpace,
        { justify: "end", size: 6, wrap: false },
        {
          default: () => [
            h(
              NButton,
              {
                size: "tiny",
                tertiary: true,
                type: "error",
                onClick: () => confirmRemove(row),
              },
              { default: () => "删除" }
            ),
          ],
        }
      ),
  },
];

async function loadSwitches() {
  loading.value = true;
  loadError.value = "";
  try {
    items.value = (await HyperVService.ListSwitches()) ?? [];
  } catch (err) {
    loadError.value = errorMessage(err);
  } finally {
    loading.value = false;
  }
}

async function loadAdapters() {
  try {
    adapters.value = (await HyperVService.ListHostNetworkAdapters()) ?? [];
  } catch {
    adapters.value = [];
  }
}

function openCreate() {
  form.name = "";
  form.switchType = "External";
  form.netAdapterName = adapters.value.length > 0 ? adapters.value[0].name : "";
  form.allowManagementOS = true;
  showCreate.value = true;
}

async function createSwitch() {
  if (!form.name.trim()) {
    message.warning("请输入交换机名称");
    return;
  }
  if (/[\\/:*?"<>|]/.test(form.name.trim())) {
    message.warning(`名称不能包含 \\ / : * ? " < > | 这些字符`);
    return;
  }
  if (form.switchType === "External" && !form.netAdapterName) {
    message.warning("创建外部交换机需要选择物理网卡");
    return;
  }
  creating.value = true;
  try {
    await HyperVService.CreateSwitch({
      name: form.name.trim(),
      switchType: form.switchType,
      netAdapterName: form.netAdapterName,
      allowManagementOS: form.allowManagementOS,
    });
    message.success("虚拟交换机创建成功");
    showCreate.value = false;
    await loadSwitches();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    creating.value = false;
  }
}

function confirmRemove(item: VirtualSwitch) {
  dialog.warning({
    title: "删除虚拟交换机",
    content: `确定删除交换机「${item.name}」吗？仍有虚拟机网卡连接时无法删除。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await HyperVService.RemoveSwitch(item.name);
        message.success("交换机已删除");
        await loadSwitches();
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

// ---------- Internal + 自建 NAT ----------

const natNetworks = ref<NATNetwork[]>([]);
const natLoading = ref(false);
const natError = ref("");
const vmIPOptions = ref<{ label: string; value: string }[]>([]);

const showCreateNAT = ref(false);
const creatingNAT = ref(false);
const editingNAT = ref(false);
const natForm = reactive({
  name: "NAT-Internal",
  switchName: "NAT-Internal",
  hostIP: "192.168.100.1",
  prefixLength: 24,
});

const showForward = ref(false);
const forwardingNAT = ref<NATNetwork | null>(null);
const addingForward = ref(false);
const forwardForm = reactive({
  protocol: "TCP",
  externalPort: 3389,
  internalIPAddress: "",
  internalPort: 3389,
});

async function loadNAT() {
  if (isWebMode) return;
  natLoading.value = true;
  natError.value = "";
  try {
    natNetworks.value = (await HyperVService.ListNATNetworks()) ?? [];
  } catch (err) {
    natError.value = errorMessage(err);
    natNetworks.value = [];
  } finally {
    natLoading.value = false;
  }
}

async function loadVMIPs() {
  try {
    const vms = (await HyperVService.ListVMSummaries()) ?? [];
    const options: { label: string; value: string }[] = [];
    const seen = new Set<string>();
    for (const vm of vms) {
      for (const adapter of vm.networks ?? []) {
        for (const ip of adapter.ipAddresses ?? []) {
          if (!ip || ip.includes(":") || seen.has(ip)) continue;
          seen.add(ip);
          options.push({ label: `${vm.name} · ${ip}`, value: ip });
        }
      }
    }
    vmIPOptions.value = options;
  } catch {
    vmIPOptions.value = [];
  }
}

function openCreateNAT() {
  editingNAT.value = false;
  natForm.name = "NAT-Internal";
  natForm.switchName = "NAT-Internal";
  natForm.hostIP = "192.168.100.1";
  natForm.prefixLength = 24;
  showCreateNAT.value = true;
}

function openEditNAT(nat: NATNetwork) {
  editingNAT.value = true;
  natForm.name = nat.name;
  natForm.switchName = nat.switchName || nat.name;
  natForm.hostIP = nat.hostIP || "192.168.100.1";
  natForm.prefixLength = Number(nat.prefix.split("/")[1] || 24);
  showCreateNAT.value = true;
}

async function createNAT() {
  if (!natForm.name.trim()) {
    message.warning("请输入 NAT 名称");
    return;
  }
  if (!natForm.switchName.trim()) {
    message.warning("请输入内部交换机名称");
    return;
  }
  if (!/^(?:\d{1,3}\.){3}\d{1,3}$/.test(natForm.hostIP.trim())) {
    message.warning("主机 IP 格式不正确");
    return;
  }
  if (natForm.prefixLength < 8 || natForm.prefixLength > 30) {
    message.warning("前缀长度必须在 8 到 30 之间");
    return;
  }
  creatingNAT.value = true;
  try {
    await HyperVService.CreateNATNetwork({
      name: natForm.name.trim(),
      switchName: natForm.switchName.trim(),
      hostIP: natForm.hostIP.trim(),
      prefixLength: Number(natForm.prefixLength),
    });
    message.success(editingNAT.value ? "NAT 网络已更新" : "NAT 网络创建成功");
    showCreateNAT.value = false;
    await loadSwitches();
    await loadNAT();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    creatingNAT.value = false;
  }
}

function confirmRemoveNAT(nat: NATNetwork) {
  dialog.warning({
    title: "删除 NAT 网络",
    content: `确定删除 NAT「${nat.name}」吗？只删除 NAT 实例，不会删除内部交换机和主机 IP。`,
    positiveText: "删除 NAT",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await HyperVService.RemoveNATNetwork(nat.name);
        message.success("NAT 已删除");
        await loadNAT();
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

function openForward(nat: NATNetwork) {
  forwardingNAT.value = nat;
  const parts = (nat.prefix.split("/")[0] || "").split(".");
  forwardForm.protocol = "TCP";
  forwardForm.externalPort = 3389;
  forwardForm.internalPort = 3389;
  forwardForm.internalIPAddress =
    parts.length === 4 ? `${parts[0]}.${parts[1]}.${parts[2]}.2` : "";
  showForward.value = true;
  void loadVMIPs();
}

async function addForward() {
  const nat = forwardingNAT.value;
  if (!nat) return;
  if (!/^(?:\d{1,3}\.){3}\d{1,3}$/.test(forwardForm.internalIPAddress.trim())) {
    message.warning("请输入虚拟机内部 IPv4 地址");
    return;
  }
  if (forwardForm.externalPort < 1 || forwardForm.internalPort < 1) {
    message.warning("端口必须在 1 到 65535 之间");
    return;
  }
  addingForward.value = true;
  try {
    await HyperVService.AddNATStaticMapping({
      natName: nat.name,
      protocol: forwardForm.protocol,
      externalPort: Number(forwardForm.externalPort),
      internalIPAddress: forwardForm.internalIPAddress.trim(),
      internalPort: Number(forwardForm.internalPort),
    });
    message.success("端口转发已添加");
    showForward.value = false;
    await loadNAT();
  } catch (err) {
    message.error(errorMessage(err));
  } finally {
    addingForward.value = false;
  }
}

function removeForward(nat: NATNetwork, mapping: NATStaticMapping) {
  dialog.warning({
    title: "删除端口转发",
    content: `确定删除 ${mapping.protocol} ${mapping.externalPort} → ${mapping.internalIPAddress}:${mapping.internalPort} 吗？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await HyperVService.RemoveNATStaticMapping({
          natName: nat.name,
          protocol: mapping.protocol,
          externalPort: mapping.externalPort,
        });
        message.success("端口转发已删除");
        await loadNAT();
      } catch (err) {
        message.error(errorMessage(err));
      }
    },
  });
}

async function openSwitchManager() {
  try {
    await HyperVService.OpenTool("switch-manager");
  } catch (err) {
    message.error(errorMessage(err));
  }
}

onMounted(() => {
  void loadSwitches();
  void loadAdapters();
  void loadNAT();
});
</script>

<template>
  <PageHeader title="虚拟交换机" subtitle="管理 Hyper-V 网络交换机与外部桥接">
    <n-button size="small" @click="openSwitchManager">
      <template #icon>
        <n-icon :component="BuildOutline" />
      </template>
      交换机管理器
    </n-button>
    <n-button size="small" :loading="loading" @click="loadSwitches">
      <template #icon>
        <n-icon :component="RefreshOutline" />
      </template>
      刷新
    </n-button>
    <n-button size="small" type="primary" @click="openCreate">
      <template #icon>
        <n-icon :component="AddCircleOutline" />
      </template>
      新建交换机
    </n-button>
  </PageHeader>

  <n-alert v-if="loadError" type="warning" class="inline-error" title="无法读取虚拟交换机">
    {{ loadError }}
  </n-alert>

  <div class="network-legend">
    <div class="legend-item">
      <div class="legend-title">外部（桥接）</div>
      <div class="legend-desc">绑定物理网卡，虚拟机和主机可以访问局域网与互联网。</div>
    </div>
    <div class="legend-item">
      <div class="legend-title">内部</div>
      <div class="legend-desc">虚拟机只能与主机通信，适合主机代理上网或隔离测试。</div>
    </div>
    <div class="legend-item">
      <div class="legend-title">专用</div>
      <div class="legend-desc">仅虚拟机之间互通，主机不参与，适合搭建隔离实验环境。</div>
    </div>
  </div>

  <div class="panel table-panel">
    <n-data-table
      :columns="columns"
      :data="items"
      :loading="loading"
      :bordered="false"
      :single-line="false"
      size="small"
      :row-key="(row: VirtualSwitch) => row.id || row.name"
      :scroll-x="900"
    >
      <template #empty>
        <div class="empty-hint">
          还没有虚拟交换机。新建一台外部交换机后，虚拟机才能访问外部网络。
        </div>
      </template>
    </n-data-table>
  </div>

  <div v-if="!isWebMode" class="panel nat-panel">
    <div class="panel-header">
      <div>
        <div class="card-title">内部网络 NAT（端口转发）</div>
        <div class="card-subtitle">
          内部交换机 + 自建 Windows NAT。虚拟机使用静态 IP 隔离在内部网段，再通过端口转发暴露服务。
        </div>
      </div>
      <div class="spacer"></div>
      <n-button size="small" :loading="natLoading" @click="loadNAT">
        <template #icon>
          <n-icon :component="RefreshOutline" />
        </template>
        刷新 NAT
      </n-button>
      <n-button size="small" type="primary" @click="openCreateNAT">
        <template #icon>
          <n-icon :component="AddCircleOutline" />
        </template>
        新建 NAT 网络
      </n-button>
    </div>

    <n-alert v-if="natError" type="warning" class="inline-error" title="无法读取 NAT 网络">
      {{ natError }}
    </n-alert>

    <div class="form-hint nat-tip">
      Windows NAT 负责虚拟机出站上网；端口转发使用 Windows portproxy（TCP），
      外部端口监听在宿主机所有网卡。虚拟机需要手动设置同网段静态 IP、网关为下面的“主机 IP”，
      DNS 可填写外部 DNS（如 8.8.8.8）。添加端口转发后，可直接在“设置 → 内网穿透”里选择并映射到公网。
    </div>

    <n-alert v-if="editingNAT" type="warning" :show-icon="true" style="margin-bottom: 10px">
      修改主机 IP 或网段会让虚拟机原来的静态 IP、网关失效；请同步修改虚拟机内的网络配置。
    </n-alert>

    <div v-if="natNetworks.length === 0" class="empty-hint">
      还没有 NAT 网络。新建后会自动创建内部交换机并配置主机 IP。
    </div>

    <div v-for="nat in natNetworks" :key="nat.name" class="nat-card">
      <div class="nat-card-head">
        <div class="nat-card-title">
          <span class="vm-name">{{ nat.name }}</span>
          <n-tag size="tiny" :type="nat.active ? 'success' : 'default'" :bordered="false">
            {{ nat.active ? "已启用" : "未启用" }}
          </n-tag>
        </div>
        <div class="nat-card-actions">
          <n-button size="tiny" @click="openEditNAT(nat)">编辑 NAT</n-button>
          <n-button size="tiny" @click="openForward(nat)">添加端口转发</n-button>
          <n-button size="tiny" tertiary type="error" @click="confirmRemoveNAT(nat)">
            删除 NAT
          </n-button>
        </div>
      </div>
      <div class="nat-meta">
        <span>网段 <span class="mono-text">{{ nat.prefix || "未知" }}</span></span>
        <span>主机 IP <span class="mono-text">{{ nat.hostIP || "未找到" }}</span></span>
        <span>对外 IP <span class="mono-text">{{ nat.externalIP || "未找到" }}</span></span>
        <span>交换机 <span class="mono-text">{{ nat.switchName || "—" }}</span></span>
      </div>

      <div v-if="nat.staticMappings?.length" class="nat-mappings">
        <div
          v-for="mapping in nat.staticMappings"
          :key="`${mapping.protocol}-${mapping.externalPort}`"
          class="nat-mapping"
        >
          <span class="nat-mapping-label">
            {{ mapping.protocol }} <span class="mono-text">{{ mapping.externalPort }}</span>
          </span>
          <span class="nat-mapping-arrow">→</span>
          <span class="mono-text">{{ mapping.internalIPAddress }}:{{ mapping.internalPort }}</span>
          <div class="spacer"></div>
          <n-button size="tiny" quaternary type="error" @click="removeForward(nat, mapping)">
            删除
          </n-button>
        </div>
      </div>
      <div v-else class="form-hint">还没有端口转发规则。</div>
    </div>
  </div>

  <n-modal
    v-model:show="showCreateNAT"
    preset="card"
    :title="editingNAT ? '编辑内部 NAT 网络' : '新建内部 NAT 网络'"
    style="width: 560px"
  >
    <n-form label-placement="top" size="small">
      <div class="form-grid">
        <n-form-item label="NAT 名称">
          <n-input
            v-model:value="natForm.name"
            placeholder="NAT-Internal"
            :disabled="editingNAT"
          />
        </n-form-item>
        <n-form-item label="内部交换机名称">
          <n-input v-model:value="natForm.switchName" placeholder="NAT-Internal" />
        </n-form-item>
        <n-form-item label="主机 IP">
          <n-input v-model:value="natForm.hostIP" placeholder="192.168.100.1" />
        </n-form-item>
        <n-form-item label="前缀长度">
          <n-input-number
            v-model:value="natForm.prefixLength"
            :min="8"
            :max="30"
            style="width: 100%"
          />
        </n-form-item>
      </div>
      <n-alert type="info" :show-icon="true">
        如果交换机不存在，会自动创建 Internal 交换机，并把主机 IP 配置到 vEthernet 网卡。
        建议使用 192.168.x.0/24 等不会与现有网络冲突的网段。
      </n-alert>
      <n-alert type="warning" :show-icon="true" style="margin-top: 10px">
        Windows NAT 不提供 DHCP。创建后请给虚拟机配置静态 IP（例如 192.168.100.2）、
        子网掩码和网关 {{ natForm.hostIP || "主机 IP" }}，DNS 可用 8.8.8.8。
      </n-alert>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showCreateNAT = false">取消</n-button>
        <n-button size="small" type="primary" :loading="creatingNAT" @click="createNAT">
          {{ editingNAT ? "保存" : "创建" }}
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal
    v-model:show="showForward"
    preset="card"
    :title="`添加端口转发${forwardingNAT ? ' · ' + forwardingNAT.name : ''}`"
    style="width: 520px"
  >
    <n-form label-placement="top" size="small">
      <div class="form-grid">
        <n-form-item label="协议">
          <n-tag size="small" :bordered="false" type="info">
            TCP（Windows portproxy）
          </n-tag>
        </n-form-item>
        <n-form-item label="宿主机外部端口">
          <n-input-number
            v-model:value="forwardForm.externalPort"
            :min="1"
            :max="65535"
            style="width: 100%"
          />
        </n-form-item>
        <n-form-item label="虚拟机内部 IP">
          <n-auto-complete
            v-model:value="forwardForm.internalIPAddress"
            :options="vmIPOptions"
            placeholder="例如 192.168.100.2"
          />
        </n-form-item>
        <n-form-item label="虚拟机内部端口">
          <n-input-number
            v-model:value="forwardForm.internalPort"
            :min="1"
            :max="65535"
            style="width: 100%"
          />
        </n-form-item>
      </div>
      <n-alert type="info" :show-icon="true">
        外部端口监听在宿主机所有网卡（0.0.0.0）。添加后可在“设置 → 内网穿透”里选择这条转发，
        把宿主机 {{ forwardingNAT?.externalIP || "局域网 IP" }}:{{ forwardForm.externalPort }}
        映射到公网。
      </n-alert>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showForward = false">取消</n-button>
        <n-button size="small" type="primary" :loading="addingForward" @click="addForward">
          添加
        </n-button>
      </n-space>
    </template>
  </n-modal>

  <n-modal v-model:show="showCreate" preset="card" title="新建虚拟交换机" style="width: 520px">
    <n-form label-placement="top" size="small">
      <n-form-item label="交换机名称">
        <n-input v-model:value="form.name" placeholder="例如 External-Bridge" />
      </n-form-item>
      <n-form-item label="类型">
        <n-radio-group v-model:value="form.switchType">
          <n-radio-button value="External">外部（桥接）</n-radio-button>
          <n-radio-button value="Internal">内部</n-radio-button>
          <n-radio-button value="Private">专用</n-radio-button>
        </n-radio-group>
      </n-form-item>
      <n-form-item v-if="form.switchType === 'External'" label="物理网卡">
        <n-select
          v-model:value="form.netAdapterName"
          :options="adapters.map((item) => ({ label: `${item.name}（${item.description}）`, value: item.name }))"
          placeholder="选择用于桥接的物理网卡"
          filterable
        />
      </n-form-item>
      <n-form-item v-if="form.switchType === 'External'" label="主机共享">
        <n-switch v-model:value="form.allowManagementOS" />
        <span class="form-hint" style="margin-left: 10px">
          允许主机通过该交换机继续联网
        </span>
      </n-form-item>
      <n-alert type="warning" :show-icon="true">
        创建外部交换机时，Windows 会重新配置物理网卡，可能导致主机网络短暂中断。
      </n-alert>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button size="small" @click="showCreate = false">取消</n-button>
        <n-button size="small" type="primary" :loading="creating" @click="createSwitch">
          创建
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.table-panel {
  padding: 6px 8px;
}

.tag-wrapper {
  display: inline-flex;
}

.custom-tag {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: 6px;
  font-size: 11px;
  background: var(--net-neutral-bg);
  color: var(--net-neutral-fg);
}

.custom-tag[data-type="External"] {
  background: var(--net-blue-bg);
  color: var(--net-blue-fg);
}

.custom-tag[data-type="Internal"] {
  background: var(--net-cyan-bg);
  color: var(--net-cyan-fg);
}

.custom-tag[data-type="Private"] {
  background: var(--net-purple-bg);
  color: var(--net-purple-fg);
}

.nat-panel {
  margin-top: 16px;
}

.nat-tip {
  margin: 4px 0 12px;
  line-height: 1.7;
}

.nat-card {
  border: 1px solid var(--border-soft);
  border-radius: 9px;
  padding: 14px 16px;
  margin-top: 10px;
  background: var(--panel);
}

.nat-card-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.nat-card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
}

.nat-card-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.nat-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 18px;
  margin-top: 8px;
  font-size: 11.5px;
  color: var(--text-3);
}

.nat-mappings {
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px solid var(--border-soft);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.nat-mapping {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-2);
}

.nat-mapping-label {
  min-width: 80px;
}

.nat-mapping-arrow {
  color: var(--text-3);
}
</style>
