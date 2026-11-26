import { HyperVService as WailsHyperVService } from "../../bindings/hypervvmmanager/internal/service";
import type { Bootstrap } from "../../bindings/hypervvmmanager/internal/service/models";
import type { APIKeyRequest } from "../../bindings/hypervvmmanager/internal/service/models";
import type {
  VM,
  HostInfo,
  VirtualSwitch,
  HostNetworkAdapter,
  EnhancedSessionStatus,
  EnvironmentStatus,
  EnvironmentCheck,
  CreateRequest,
  CloneRequest,
  EditRequest,
  CreateSwitchRequest,
  MemoryOptions,
} from "../../bindings/hypervvmmanager/internal/hyperv/models";
import type { Task, TaskLog, Stats as TaskStats } from "../../bindings/hypervvmmanager/internal/tasks/models";
import type { Settings, APIKey } from "../../bindings/hypervvmmanager/internal/settings/models";
import type { Status as APIStatus } from "../../bindings/hypervvmmanager/internal/httpapi/models";
import type { Status as TunnelStatus } from "../../bindings/hypervvmmanager/internal/tunnel/models";
import type {
  Item as HostItem,
  Request as HostRequest,
} from "../../bindings/hypervvmmanager/internal/hosts/models";

export type {
  Bootstrap,
  VM,
  HostInfo,
  HostItem,
  HostRequest,
  VirtualSwitch,
  HostNetworkAdapter,
  EnhancedSessionStatus,
  EnvironmentStatus,
  EnvironmentCheck,
  Task,
  TaskLog,
  TaskStats,
  Settings,
  APIKey,
  APIKeyRequest,
  APIStatus,
  TunnelStatus,
  CreateRequest,
  CloneRequest,
  EditRequest,
  CreateSwitchRequest,
  MemoryOptions,
};

// Wails 桌面端使用 WebView2 / WKWebView / WebKitGTK；普通浏览器没有这些标志。
export const isWebMode =
  typeof window !== "undefined" &&
  !(window as unknown as { chrome?: { webview?: unknown } }).chrome?.webview &&
  !(window as unknown as { wails?: unknown }).wails;

interface WebRequestOptions {
  silent?: boolean;
}

async function webFetch<T>(
  path: string,
  init: RequestInit = {},
  options: WebRequestOptions = {}
): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`/api/v1${path}`, {
    credentials: "same-origin",
    ...init,
    headers,
  });
  const text = await response.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!response.ok) {
    if (response.status === 401 && !options.silent) {
      window.dispatchEvent(new CustomEvent("hvvm:unauthorized"));
    }
    let message = `请求失败（${response.status}）`;
    if (data && typeof data === "object" && "error" in data) {
      message = String((data as Record<string, unknown>).error);
    }
    throw new Error(message);
  }
  return data as T;
}

function postJSON<T>(path: string, body?: unknown): Promise<T> {
  return webFetch<T>(path, {
    method: "POST",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

function withHost(path: string, hostID?: string | null): string {
  if (!hostID) return path;
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}hostId=${encodeURIComponent(hostID)}`;
}

function segment(value: string): string {
  return encodeURIComponent(value);
}

async function listItems<T>(path: string): Promise<T[]> {
  const result = await webFetch<{ items: T[] | null }>(path);
  return result.items ?? [];
}

async function getWebBootstrap(): Promise<Bootstrap> {
  return webFetch<Bootstrap>("/bootstrap");
}

/** 网页登录：成功后服务端写入 HttpOnly Cookie。 */
export async function webLogin(username: string, password: string): Promise<void> {
  await webFetch(
    "/auth/login",
    {
      method: "POST",
      body: JSON.stringify({ username, password }),
    },
    { silent: true }
  );
}

/** 网页退出登录。 */
export async function webLogout(): Promise<void> {
  try {
    await webFetch("/auth/logout", { method: "POST" }, { silent: true });
  } catch {
    // 忽略退出失败
  }
}

/** 检查网页登录状态。 */
export async function webMe(): Promise<{ authenticated: boolean; username?: string }> {
  return webFetch("/auth/me", {}, { silent: true });
}

const webService = {
  GetBootstrap: () => getWebBootstrap(),
  GetHostInfo: () => webFetch<HostInfo>("/host"),
  GetEnvironmentStatus: () => webFetch<EnvironmentStatus>("/environment"),
  GetEnhancedSessionStatus: () => webFetch<EnhancedSessionStatus>("/enhanced-session"),
  SetEnhancedSession: (enabled: boolean) => postJSON("/enhanced-session", { enabled }),

  ListVMs: () => listItems<VM>("/vms"),
  ListVMsForHost: (hostID: string) => listItems<VM>(withHost("/vms", hostID)),
  GetVM: (name: string) => webFetch<VM>(`/vms/${segment(name)}`),
  ListSwitches: () => listItems<VirtualSwitch>("/switches"),
  ListSwitchesForHost: (hostID: string) => listItems<VirtualSwitch>(withHost("/switches", hostID)),
  ListHostNetworkAdapters: () => listItems<HostNetworkAdapter>("/host-network-adapters"),
  CreateSwitch: (req: CreateSwitchRequest) => postJSON("/switches", req),
  RemoveSwitch: (name: string) =>
    webFetch(`/switches/${segment(name)}`, { method: "DELETE" }),
  SuggestCloneDirectory: (sourceName: string) =>
    webFetch<{ directory: string }>(
      `/vms/${segment(sourceName)}/suggest-clone-path`
    ).then((result) => result.directory),
  SuggestCloneDirectoryForHost: (hostID: string, sourceName: string) =>
    webFetch<{ directory: string }>(
      withHost(`/vms/${segment(sourceName)}/suggest-clone-path`, hostID)
    ).then((result) => result.directory),

  ListHosts: () => listItems<HostItem>("/hosts"),
  GetActiveHost: () =>
    listItems<HostItem>("/hosts").then((items) => items.find((item) => item.active) ?? null),
  AddHost: (request: HostRequest) => postJSON<HostItem>("/hosts", request),
  UpdateHost: (request: HostRequest) =>
    webFetch<HostItem>(`/hosts/${segment(request.id ?? "")}`, {
      method: "PATCH",
      body: JSON.stringify(request),
    }),
  RemoveHost: (id: string) => webFetch(`/hosts/${segment(id)}`, { method: "DELETE" }),
  ConnectHost: (id: string, password: string, remember: boolean) =>
    postJSON<HostItem>(`/hosts/${segment(id)}/connect`, { password, remember }),
  ForgetHostCredential: (id: string) =>
    webFetch(`/hosts/${segment(id)}/credential`, { method: "DELETE" }),
  SetActiveHost: (id: string) => postJSON<HostItem>(`/hosts/${segment(id)}/connect`, {}),

  SubmitCreate: (hostID: string, req: CreateRequest) =>
    webFetch<Task>(withHost("/vms", hostID), {
      method: "POST",
      body: JSON.stringify(req),
    }),
  SubmitClone: (hostID: string, req: CloneRequest) =>
    webFetch<Task>(withHost(`/vms/${segment(req.sourceName)}/clone`, hostID), {
      method: "POST",
      body: JSON.stringify(req),
    }),
  SubmitEdit: (name: string, req: EditRequest) =>
    webFetch<Task>(`/vms/${segment(name)}`, {
      method: "PATCH",
      body: JSON.stringify(req),
    }),
  SubmitDelete: (name: string, deleteFiles: boolean) =>
    webFetch<Task>(`/vms/${segment(name)}?deleteFiles=${deleteFiles ? "true" : "false"}`, {
      method: "DELETE",
    }),
  SubmitPower: (name: string, action: string) =>
    postJSON<Task>(`/vms/${segment(name)}/power`, { action }),

  ListTasks: (limit: number) => listItems<Task>(`/tasks?limit=${limit}`),
  GetTask: (id: string) => webFetch<Task>(`/tasks/${segment(id)}`),
  CancelTask: (id: string) => postJSON(`/tasks/${segment(id)}/cancel`),
  ClearFinishedTasks: () =>
    postJSON<{ removed: number }>("/tasks/clear").then((result) => result.removed),
  GetTaskStats: () => webFetch<TaskStats>("/tasks/stats"),

  GetSettings: () => webFetch<Settings>("/settings"),
  SaveSettings: (value: Settings) =>
    webFetch<Settings>("/settings", {
      method: "PUT",
      body: JSON.stringify(value),
    }),

  // API Key 管理只允许在桌面端进行，网页端不暴露 Token。
  ListAPIKeys: () => Promise.resolve([] as APIKey[]),
  CreateAPIKey: (_request: APIKeyRequest) =>
    Promise.reject(new Error("请在桌面端“设置 → API 访问”中管理 API Key")),
  UpdateAPIKey: (_request: APIKeyRequest) =>
    Promise.reject(new Error("请在桌面端“设置 → API 访问”中管理 API Key")),
  DeleteAPIKey: (_id: string) =>
    Promise.reject(new Error("请在桌面端“设置 → API 访问”中管理 API Key")),
  RegenerateAPIKeyToken: (_id: string) =>
    Promise.reject(new Error("请在桌面端“设置 → API 访问”中管理 API Key")),

  IsAdmin: () => getWebBootstrap().then((value) => value.isAdmin),
  GetAPIStatus: () => getWebBootstrap().then((value) => value.api),

  // 内网穿透由安装在本机的桌面端管理，网页控制台只展示状态。
  GetTunnelStatus: () =>
    Promise.resolve({ enabled: false, running: false, connected: false } as TunnelStatus),
  RestartTunnel: () =>
    Promise.reject(new Error("请在桌面端“设置 → 内网穿透”中管理 frp 连接")),

  PickFile: () =>
    Promise.reject(new Error("网页控制台不能访问宿主机文件，请手动输入绝对路径")),
  PickFileIn: () =>
    Promise.reject(new Error("网页控制台不能访问宿主机文件，请手动输入绝对路径")),
  PickDirectory: () =>
    Promise.reject(new Error("网页控制台不能访问宿主机目录，请手动输入绝对路径")),
  PickDirectoryIn: () =>
    Promise.reject(new Error("网页控制台不能访问宿主机目录，请手动输入绝对路径")),
  OpenLogDir: () =>
    Promise.reject(new Error("网页控制台不能打开宿主机日志目录")),
  OpenTool: () => Promise.reject(new Error("网页控制台不能打开宿主机管理工具")),
  ConnectVM: () => Promise.reject(new Error("网页控制台不能启动 VMConnect，请在桌面端连接")),
  RestartElevated: () => Promise.reject(new Error("网页控制台不能重启桌面应用")),
};

// 桌面端使用 Wails 绑定；普通浏览器使用 HTTP + Cookie。
export const HyperVService = (
  isWebMode ? webService : WailsHyperVService
) as unknown as typeof WailsHyperVService;

export function errorMessage(err: unknown): string {
  if (typeof err === "string") return err;
  if (err instanceof Error) return err.message;
  if (err && typeof err === "object") {
    const record = err as Record<string, unknown>;
    const message = record.message ?? record.error;
    if (typeof message === "string" && message.trim() !== "") {
      return message;
    }
    try {
      return JSON.stringify(err);
    } catch {
      return String(err);
    }
  }
  return String(err);
}

export function defaultMemory(settings: Settings): MemoryOptions {
  return {
    startupMB: settings.defaultMemoryStartupMB,
    dynamic: settings.defaultDynamicMemory,
    minimumMB: settings.defaultMemoryMinimumMB,
    maximumMB: settings.defaultMemoryMaximumMB,
  };
}
