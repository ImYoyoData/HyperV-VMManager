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
import { HyperVService, errorMessage } from "../api";
import type { HostNetworkAdapter, VirtualSwitch } from "../api";
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
    >
      <template #empty>
        <div class="empty-hint">
          还没有虚拟交换机。新建一台外部交换机后，虚拟机才能访问外部网络。
        </div>
      </template>
    </n-data-table>
  </div>

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
  background: #1c2130;
  color: #aeb8c9;
}

.custom-tag[data-type="External"] {
  background: #172a4a;
  color: #8fb6ff;
}

.custom-tag[data-type="Internal"] {
  background: #173039;
  color: #74c7cf;
}

.custom-tag[data-type="Private"] {
  background: #262230;
  color: #b7a8d8;
}
</style>
