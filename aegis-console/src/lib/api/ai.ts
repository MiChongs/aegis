import { apiRequest, buildQuery } from "./client";

/**
 * AI 通道 / 技能 / MCP / Agent 会话 API。两个作用域，一份能力：
 *
 * | 作用域 | 归属页面 | 服务的调用 |
 * |---|---|---|
 * | 平台级（appid=0） | `/configuration?tab=ai` | 平台自身的 AI 需求；可显式共享给应用兜底 |
 * | 应用级 | `/apps/{appKey}?tab=ai` | 该应用的 Agent 助手、`aegis.ai` 脚本调用、兼容网关 |
 *
 * 两边走的是**同一批后端服务方法**，只差一个 appid；与邮件通道不同的是
 * 应用级也用 RESTful 路由（这是新命名空间，没有历史包袱要背）。
 */

// ── 类型 ──

export type AIProviderCapabilities = {
  streaming?: boolean;
  toolCalls?: boolean;
  vision?: boolean;
  jsonMode?: boolean;
  reasoning?: boolean;
};

export type AIFieldOption = { value: string; label: string; help?: string };

export type AIConfigField = {
  key: string;
  label: string;
  type: string;
  group?: string;
  required?: boolean;
  secret?: boolean;
  placeholder?: string;
  help?: string;
  default?: string | number | boolean | null;
  options?: AIFieldOption[];
  advanced?: boolean;
};

export type AIProviderMeta = {
  provider: string;
  name: string;
  description?: string;
  category?: string;
  categoryName?: string;
  /** openai / anthropic —— 该供应商走哪种线上协议。 */
  protocol: string;
  icon?: string;
  brandColor?: string;
  docUrl?: string;
  defaultBaseUrl?: string;
  capabilities: AIProviderCapabilities;
  fields?: AIConfigField[];
  suggestedModels?: string[];
  notes?: string[];
};

export type AIProviderCatalog = {
  providers: AIProviderMeta[];
  categories: Record<string, string>;
};

export type AIConfig = {
  id: number;
  appid: number;
  name: string;
  provider: string;
  enabled: boolean;
  isDefault: boolean;
  shared: boolean;
  priority: number;
  description?: string;
  settings: Record<string, string>;
  secretSet: Record<string, boolean>;
  createdAt: string;
  updatedAt: string;
};

/** 通道链路里的一档：这次调用会先试谁、为什么。 */
export type AIResolution = {
  configId: number;
  configName: string;
  provider: string;
  protocol: string;
  scope: "app" | "platform";
  inherited: boolean;
  model: string;
  models?: string[];
};

export type AITestResult = {
  ok: boolean;
  elapsedMs: number;
  model?: string;
  reply?: string;
  error?: string;
  status?: number;
  usage?: { inputTokens: number; outputTokens: number; totalTokens: number };
};

export type AISkill = {
  id: number;
  appid: number;
  key: string;
  name: string;
  description: string;
  content: string;
  enabled: boolean;
  builtin: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type AIMCPServer = {
  id: number;
  appid: number;
  name: string;
  url: string;
  enabled: boolean;
  description?: string;
  headersSet: boolean;
  createdAt: string;
  updatedAt: string;
};

export type AIMCPTestResult = {
  ok: boolean;
  elapsedMs: number;
  tools?: string[];
  count?: number;
  error?: string;
};

export type AIConversation = {
  id: number;
  appId: number;
  adminId: number;
  scene: string;
  ref: string;
  title: string;
  providerConfigId?: number;
  model?: string;
  inputTokens: number;
  outputTokens: number;
  compactions: number;
  createdAt: string;
  updatedAt: string;
};

export type AIAgentMessage = {
  id: number;
  conversationId: number;
  role: string;
  /** 界面分片（AI SDK UIMessage parts），回放时原样交给聊天组件。 */
  parts: unknown[];
  usage?: { inputTokens: number; outputTokens: number; totalTokens: number };
  createdAt: string;
};

// ── 写入负载 ──

export type AIConfigPayload = {
  name?: string;
  provider?: string;
  enabled?: boolean;
  isDefault?: boolean;
  /** 仅平台级生效：允许没有自有配置的应用回落到这条通道。 */
  shared?: boolean;
  priority?: number;
  description?: string;
  settings?: Record<string, string>;
  /** 密钥明文。留空的键不会发出去（留空即不修改）。 */
  secrets?: Record<string, string>;
  clearSecrets?: string[];
  /** 换供应商时置 true：整体替换而不是逐键合并。 */
  replaceSettings?: boolean;
};

export type AISkillPayload = {
  key?: string;
  name?: string;
  description?: string;
  content?: string;
  enabled?: boolean;
};

export type AIMCPServerPayload = {
  name?: string;
  url?: string;
  enabled?: boolean;
  description?: string;
  /** 鉴权请求头（整体加密存放）；不传即不修改。 */
  headers?: Record<string, string>;
  clearHeaders?: boolean;
};

// ── 作用域路由 ──
//
// 平台级挂在 /api/admin/system/ai/*，应用级挂在 /api/admin/apps/{appKey}/ai/*。
// 用一个 base 函数把差异收在一处，其余函数不再分家。

export type AIScope = { kind: "platform" } | { kind: "app"; appKey: string };

export const PLATFORM_AI_SCOPE: AIScope = { kind: "platform" };

function aiBase(scope: AIScope) {
  return scope.kind === "platform"
    ? "/api/admin/system/ai"
    : `/api/admin/apps/${encodeURIComponent(scope.appKey)}/ai`;
}

// ── 供应商目录 ──

export function getAIProviderCatalog(token: string, scope: AIScope = PLATFORM_AI_SCOPE) {
  return apiRequest<AIProviderCatalog>(`${aiBase(scope)}/providers`, { token });
}

// ── 通道配置 ──

export function getAIConfigs(token: string, scope: AIScope) {
  return apiRequest<AIConfig[]>(`${aiBase(scope)}/configs`, { token });
}

export function createAIConfig(token: string, scope: AIScope, payload: AIConfigPayload) {
  return apiRequest<AIConfig>(`${aiBase(scope)}/configs`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAIConfig(token: string, scope: AIScope, configId: number, payload: AIConfigPayload) {
  return apiRequest<AIConfig>(`${aiBase(scope)}/configs/${configId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAIConfig(token: string, scope: AIScope, configId: number) {
  return apiRequest<null>(`${aiBase(scope)}/configs/${configId}`, { method: "DELETE", token });
}

/** 连通性测试：真实调用一次（会产生 token 计费），返回耗时与回复摘要。 */
export function testAIConfig(token: string, scope: AIScope, configId: number, model?: string) {
  return apiRequest<AITestResult>(`${aiBase(scope)}/configs/${configId}/test`, {
    method: "POST",
    token,
    body: JSON.stringify({ model: model ?? "" })
  });
}

/** 当前作用域的通道链路（含继承的平台共享通道），按尝试顺序排列。 */
export function getAIChannel(token: string, scope: AIScope) {
  return apiRequest<AIResolution[]>(`${aiBase(scope)}/channel`, { token });
}

// ── 技能 ──

export function getAISkills(token: string, scope: AIScope) {
  return apiRequest<AISkill[]>(`${aiBase(scope)}/skills`, { token });
}

export function createAISkill(token: string, scope: AIScope, payload: AISkillPayload) {
  return apiRequest<AISkill>(`${aiBase(scope)}/skills`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAISkill(token: string, scope: AIScope, skillId: number, payload: AISkillPayload) {
  return apiRequest<AISkill>(`${aiBase(scope)}/skills/${skillId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAISkill(token: string, scope: AIScope, skillId: number) {
  return apiRequest<null>(`${aiBase(scope)}/skills/${skillId}`, { method: "DELETE", token });
}

// ── MCP 服务器 ──

export function getAIMCPServers(token: string, scope: AIScope) {
  return apiRequest<AIMCPServer[]>(`${aiBase(scope)}/mcp-servers`, { token });
}

export function createAIMCPServer(token: string, scope: AIScope, payload: AIMCPServerPayload) {
  return apiRequest<AIMCPServer>(`${aiBase(scope)}/mcp-servers`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAIMCPServer(token: string, scope: AIScope, serverId: number, payload: AIMCPServerPayload) {
  return apiRequest<AIMCPServer>(`${aiBase(scope)}/mcp-servers/${serverId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAIMCPServer(token: string, scope: AIScope, serverId: number) {
  return apiRequest<null>(`${aiBase(scope)}/mcp-servers/${serverId}`, { method: "DELETE", token });
}

export function testAIMCPServer(token: string, scope: AIScope, serverId: number) {
  return apiRequest<AIMCPTestResult>(`${aiBase(scope)}/mcp-servers/${serverId}/test`, {
    method: "POST",
    token
  });
}

// ── Agent 会话（仅应用作用域） ──

export function getAIConversations(
  token: string,
  appKey: string,
  params: { scene?: string; ref?: string; limit?: number } = {}
) {
  return apiRequest<AIConversation[]>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/ai/conversations${buildQuery(params)}`,
    { token }
  );
}

export function getAIConversationDetail(token: string, appKey: string, conversationId: number) {
  return apiRequest<{ conversation: AIConversation; messages: AIAgentMessage[] }>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/ai/conversations/${conversationId}`,
    { token }
  );
}

export function deleteAIConversation(token: string, appKey: string, conversationId: number) {
  return apiRequest<null>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/ai/conversations/${conversationId}`,
    { method: "DELETE", token }
  );
}

/** Agent 流式对话端点（SSE，AI SDK UI Message Stream）——由聊天 transport 直接 fetch。 */
export function aiAgentStreamPath(appKey: string) {
  return `/api/admin/apps/${encodeURIComponent(appKey)}/ai/agent/stream`;
}
