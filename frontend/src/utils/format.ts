export function formatMB(mb?: number | null): string {
  if (!mb || mb <= 0) return "—";
  if (mb >= 1024) {
    const gb = mb / 1024;
    return `${Number.isInteger(gb) ? gb : gb.toFixed(1)} GB`;
  }
  return `${Math.round(mb)} MB`;
}

export function formatDuration(seconds?: number | null): string {
  if (!seconds || seconds <= 0) return "—";
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  return `${minutes} 分钟`;
}

export function formatDateTime(value?: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours()
  )}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export function formatTimeOnly(value?: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export type TagType = "default" | "error" | "primary" | "info" | "success" | "warning";

export function vmStateLabel(state: string): string {
  const normalized = (state || "").toLowerCase();
  switch (normalized) {
    case "running":
      return "运行中";
    case "off":
      return "已关机";
    case "saved":
      return "已保存";
    case "paused":
      return "已暂停";
    case "starting":
      return "正在启动";
    case "stopping":
      return "正在停止";
    case "saving":
      return "正在保存";
    case "pausing":
      return "正在暂停";
    case "resuming":
      return "正在恢复";
    default:
      return state || "未知";
  }
}

export function vmStateType(state: string): TagType {
  switch ((state || "").toLowerCase()) {
    case "running":
      return "success";
    case "paused":
      return "info";
    case "saved":
      return "warning";
    case "starting":
    case "stopping":
    case "saving":
    case "pausing":
    case "resuming":
      return "warning";
    default:
      return "default";
  }
}

export function vmStateColor(state: string): string {
  switch ((state || "").toLowerCase()) {
    case "running":
      return "#49c78b";
    case "paused":
      return "#5aa9ff";
    case "saved":
      return "#e0a95e";
    case "off":
      return "#7e879c";
    default:
      return "#e0a95e";
  }
}

export function switchTypeLabel(value: string): string {
  switch ((value || "").toLowerCase()) {
    case "external":
      return "外部（桥接）";
    case "internal":
      return "内部";
    case "private":
      return "专用";
    default:
      return value || "未知";
  }
}

export function switchTypeTag(value: string): TagType {
  switch ((value || "").toLowerCase()) {
    case "external":
      return "primary";
    case "internal":
      return "info";
    case "private":
      return "default";
    default:
      return "default";
  }
}

export function taskStatusLabel(status: string): string {
  switch (status) {
    case "queued":
      return "排队中";
    case "running":
      return "执行中";
    case "succeeded":
      return "已完成";
    case "failed":
      return "失败";
    case "canceled":
      return "已取消";
    default:
      return status || "未知";
  }
}

export function taskStatusType(status: string): TagType {
  switch (status) {
    case "queued":
      return "default";
    case "running":
      return "info";
    case "succeeded":
      return "success";
    case "failed":
      return "error";
    case "canceled":
      return "warning";
    default:
      return "default";
  }
}

export function taskKindLabel(kind: string): string {
  switch (kind) {
    case "clone":
      return "克隆";
    case "create":
      return "新建";
    case "edit":
      return "编辑";
    case "delete":
      return "删除";
    case "power":
      return "电源";
    default:
      return kind || "未知";
  }
}

export function taskKindTag(kind: string): TagType {
  switch (kind) {
    case "clone":
      return "primary";
    case "create":
      return "success";
    case "edit":
      return "info";
    case "delete":
      return "error";
    case "power":
      return "warning";
    default:
      return "default";
  }
}

export function isPendingStatus(status: string): boolean {
  return status === "queued" || status === "running";
}

export function primaryIPv4(addresses?: string[] | null): string {
  if (!addresses || addresses.length === 0) return "";
  const ipv4 = addresses.filter((address) => /^\d{1,3}(\.\d{1,3}){3}$/.test(address));
  return ipv4.length > 0 ? ipv4[0] : "";
}
