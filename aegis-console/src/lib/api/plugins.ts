import { apiRequest, buildQuery } from "./client";

// ── 类型 ──

export type HookBinding = {
  hookName: string;
  phase: "before" | "after";
  priority: number;
};

export type Plugin = {
  id: number;
  name: string;
  displayName: string;
  description: string;
  type: "expr" | "wasm";
  status: "enabled" | "disabled" | "error";
  version: string;
  author: string;
  hooks: HookBinding[];
  config: Record<string, unknown>;
  exprScript?: string;
  wasmModuleUrl?: string;
  wasmHash?: string;
  priority: number;
  errorMessage?: string;
  createdBy?: number | null;
  createdAt: string;
  updatedAt: string;
};

export type PluginListResult = {
  items: Plugin[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type HookDefinition = {
  name: string;
  domain: string;
  phase: string;
  description: string;
};

export type PluginHookSummary = {
  pluginId: number;
  pluginName: string;
  displayName: string;
  phase: string;
  priority: number;
  type: string;
  status: string;
};

export type HookRegistryView = {
  hooks: HookDefinition[];
  bindings: Record<string, PluginHookSummary[]>;
};

export type HookExecution = {
  id: number;
  pluginId: number;
  pluginName: string;
  hookName: string;
  phase: string;
  durationMs: number;
  status: string;
  error?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  createdAt: string;
};

export type HookExecutionListResult = {
  items: HookExecution[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

// ── API 函数 ──

export function listPlugins(token: string, params?: { status?: string; type?: string; keyword?: string; page?: number; limit?: number }) {
  return apiRequest<PluginListResult>(`/api/admin/system/plugins${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}

export function createPlugin(token: string, data: {
  name: string; displayName: string; description?: string; type: string;
  hooks?: HookBinding[]; config?: Record<string, unknown>; exprScript?: string; priority?: number;
}) {
  return apiRequest<Plugin>("/api/admin/system/plugins", { method: "POST", token, body: JSON.stringify(data) });
}

export function getPlugin(token: string, id: number) {
  return apiRequest<Plugin>(`/api/admin/system/plugins/${id}`, { token });
}

export function updatePlugin(token: string, id: number, data: Record<string, unknown>) {
  return apiRequest<Plugin>(`/api/admin/system/plugins/${id}`, { method: "PUT", token, body: JSON.stringify(data) });
}

export function deletePlugin(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/plugins/${id}`, { method: "DELETE", token });
}

export function enablePlugin(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/plugins/${id}/enable`, { method: "POST", token });
}

export function disablePlugin(token: string, id: number) {
  return apiRequest<void>(`/api/admin/system/plugins/${id}/disable`, { method: "POST", token });
}

export function getHookRegistry(token: string) {
  return apiRequest<HookRegistryView>("/api/admin/system/plugins/registry", { token });
}

export function listHookExecutions(token: string, params?: { pluginId?: number; hookName?: string; status?: string; page?: number; limit?: number }) {
  return apiRequest<HookExecutionListResult>(`/api/admin/system/plugins/executions${buildQuery(params as Record<string, string | number | boolean | undefined>)}`, { token });
}
