"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createAIConfig,
  createAIMCPServer,
  createAISkill,
  deleteAIConfig,
  deleteAIConversation,
  deleteAIMCPServer,
  deleteAISkill,
  getAIChannel,
  getAIConfigs,
  getAIConversationDetail,
  getAIConversations,
  getAIMCPServers,
  getAIProviderCatalog,
  getAISkills,
  testAIConfig,
  testAIMCPServer,
  updateAIConfig,
  updateAIMCPServer,
  updateAISkill,
  type AIConfig,
  type AIConfigPayload,
  type AIConversation,
  type AIMCPServer,
  type AIMCPServerPayload,
  type AIProviderCatalog,
  type AIResolution,
  type AIScope,
  type AISkill,
  type AISkillPayload
} from "@/lib/api/ai";
import { useAdminToken } from "@/lib/admin-hooks";

export { PLATFORM_AI_SCOPE, type AIScope } from "@/lib/api/ai";

/**
 * AI 通道的 React Query hooks。
 *
 * 作用域用 `AIScope` 表达（与邮件的 EmailScope 同一套教训）：
 * 平台级不是「没有 appKey」，而是一个显式的档位。
 */

function scopeKey(scope: AIScope) {
  return scope.kind === "platform" ? "platform" : `app-${scope.appKey}`;
}

function isReady(scope: AIScope) {
  return scope.kind === "platform" || Boolean(scope.appKey);
}

// ── 供应商目录 ──

/** 静态自述，缓存整场会话 —— 只随后端发版变化。 */
export function useAIProviderCatalogQuery(scope: AIScope) {
  const token = useAdminToken();
  return useQuery<AIProviderCatalog>({
    // 目录内容与作用域无关，但平台级路由只有全局管理员可达，
    // 应用管理员必须从应用级路由取 —— 缓存键也要分开，否则会互相污染权限结论。
    queryKey: ["ai", "providers", scopeKey(scope)],
    queryFn: () => getAIProviderCatalog(token as string, scope),
    enabled: Boolean(token) && isReady(scope),
    staleTime: Infinity
  });
}

// ── 通道配置 ──

export function useAIConfigsQuery(scope: AIScope) {
  const token = useAdminToken();
  return useQuery<AIConfig[]>({
    queryKey: ["ai", "configs", scopeKey(scope)],
    queryFn: () => getAIConfigs(token as string, scope),
    enabled: Boolean(token) && isReady(scope)
  });
}

/**
 * 通道链路。空数组是最正常的初始状态（一条通道都没配），
 * 后端也按这个约定返回 200 + []，不需要 404 特判。
 */
export function useAIChannelQuery(scope: AIScope) {
  const token = useAdminToken();
  return useQuery<AIResolution[]>({
    queryKey: ["ai", "channel", scopeKey(scope)],
    queryFn: () => getAIChannel(token as string, scope),
    enabled: Boolean(token) && isReady(scope),
    retry: false
  });
}

/** 一次保存会同时影响配置列表与链路结论；应用侧的继承结论也一并刷掉。 */
function useAIInvalidator(scope: AIScope) {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["ai", "configs", scopeKey(scope)] });
    void queryClient.invalidateQueries({ queryKey: ["ai", "channel"] });
  };
}

export function useSaveAIConfigMutation(scope: AIScope) {
  const token = useAdminToken();
  const invalidate = useAIInvalidator(scope);
  return useMutation({
    mutationFn: ({ configId, payload }: { configId?: number; payload: AIConfigPayload }) =>
      configId
        ? updateAIConfig(token as string, scope, configId, payload)
        : createAIConfig(token as string, scope, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAIConfigMutation(scope: AIScope) {
  const token = useAdminToken();
  const invalidate = useAIInvalidator(scope);
  return useMutation({
    mutationFn: (configId: number) => deleteAIConfig(token as string, scope, configId),
    onSuccess: invalidate
  });
}

export function useTestAIConfigMutation(scope: AIScope) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: ({ configId, model }: { configId: number; model?: string }) =>
      testAIConfig(token as string, scope, configId, model)
  });
}

// ── 技能 ──

export function useAISkillsQuery(scope: AIScope) {
  const token = useAdminToken();
  return useQuery<AISkill[]>({
    queryKey: ["ai", "skills", scopeKey(scope)],
    queryFn: () => getAISkills(token as string, scope),
    enabled: Boolean(token) && isReady(scope)
  });
}

export function useSaveAISkillMutation(scope: AIScope) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ skillId, payload }: { skillId?: number; payload: AISkillPayload }) =>
      skillId
        ? updateAISkill(token as string, scope, skillId, payload)
        : createAISkill(token as string, scope, payload),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["ai", "skills", scopeKey(scope)] })
  });
}

export function useDeleteAISkillMutation(scope: AIScope) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skillId: number) => deleteAISkill(token as string, scope, skillId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["ai", "skills", scopeKey(scope)] })
  });
}

// ── MCP 服务器 ──

export function useAIMCPServersQuery(scope: AIScope) {
  const token = useAdminToken();
  return useQuery<AIMCPServer[]>({
    queryKey: ["ai", "mcp", scopeKey(scope)],
    queryFn: () => getAIMCPServers(token as string, scope),
    enabled: Boolean(token) && isReady(scope)
  });
}

export function useSaveAIMCPServerMutation(scope: AIScope) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ serverId, payload }: { serverId?: number; payload: AIMCPServerPayload }) =>
      serverId
        ? updateAIMCPServer(token as string, scope, serverId, payload)
        : createAIMCPServer(token as string, scope, payload),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["ai", "mcp", scopeKey(scope)] })
  });
}

export function useDeleteAIMCPServerMutation(scope: AIScope) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (serverId: number) => deleteAIMCPServer(token as string, scope, serverId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["ai", "mcp", scopeKey(scope)] })
  });
}

export function useTestAIMCPServerMutation(scope: AIScope) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (serverId: number) => testAIMCPServer(token as string, scope, serverId)
  });
}

// ── Agent 会话（应用作用域） ──

export function useAIConversationsQuery(appKey: string, scene: string, ref?: string) {
  const token = useAdminToken();
  return useQuery<AIConversation[]>({
    queryKey: ["ai", "conversations", appKey, scene, ref ?? ""],
    queryFn: () => getAIConversations(token as string, appKey, { scene, ref }),
    enabled: Boolean(token) && Boolean(appKey)
  });
}

export function useAIConversationDetailQuery(appKey: string, conversationId: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["ai", "conversation", appKey, conversationId],
    queryFn: () => getAIConversationDetail(token as string, appKey, conversationId),
    enabled: Boolean(token) && Boolean(appKey) && conversationId > 0
  });
}

export function useDeleteAIConversationMutation(appKey: string) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (conversationId: number) => deleteAIConversation(token as string, appKey, conversationId),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["ai", "conversations", appKey] })
  });
}
