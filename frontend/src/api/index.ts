import { HyperVService } from "../../bindings/hypervvmmanager/internal/service";
import type { Bootstrap } from "../../bindings/hypervvmmanager/internal/service/models";
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
import type { Settings } from "../../bindings/hypervvmmanager/internal/settings/models";
import type { Status as APIStatus } from "../../bindings/hypervvmmanager/internal/httpapi/models";
import type {
  Item as HostItem,
  Request as HostRequest,
} from "../../bindings/hypervvmmanager/internal/hosts/models";

export { HyperVService };
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
  APIStatus,
  CreateRequest,
  CloneRequest,
  EditRequest,
  CreateSwitchRequest,
  MemoryOptions,
};

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
