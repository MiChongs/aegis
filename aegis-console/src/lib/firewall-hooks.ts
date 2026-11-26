"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import {
  firewallApi,
  type FirewallEventQuery,
  type FirewallExclusionInput,
  type FirewallRuleInput,
  type FirewallSandboxInput,
  type FirewallSettingsPatch,
  type FirewallStatsQuery
} from "@/lib/api/firewall";

/**
 * 防火墙工作台的数据层（docs/firewall.md）。
 *
 * - 运行态每 5 秒、事件与统计每 15 秒轮询一次（总览的「实时」视图）。
 * - 规则、例外、设置的写接口成功后统一失效 `FIREWALL_KEY` 下的缓存：一次改动会同时影响
 *   运行态（规则数、例外数、热更新版本号）、规则目录（启用 / 移除标记）与指令文本。
 * - 后端在保存前用「改动之后」的整套配置试编译 WAF，失败返回 40090 / 40092 / 40093，
 *   message 里带着具体原因（哪条例外、哪个表达式），直接显示给管理员。
 */

export const FIREWALL_KEY = ["admin-firewall"] as const;

const keys = {
  runtime: (token: string | null) => [...FIREWALL_KEY, "runtime", token] as const,
  settings: (token: string | null) => [...FIREWALL_KEY, "settings", token] as const,
  events: (token: string | null, query: FirewallEventQuery) => [...FIREWALL_KEY, "events", token, query] as const,
  event: (token: string | null, id: number | null) => [...FIREWALL_KEY, "event", token, id] as const,
  stats: (token: string | null, query: FirewallStatsQuery) => [...FIREWALL_KEY, "stats", token, query] as const,
  rules: (token: string | null) => [...FIREWALL_KEY, "rules", token] as const,
  exclusions: (token: string | null) => [...FIREWALL_KEY, "exclusions", token] as const,
  catalog: (token: string | null) => [...FIREWALL_KEY, "catalog", token] as const,
  catalogRule: (token: string | null, id: number | null) => [...FIREWALL_KEY, "catalog-rule", token, id] as const,
  directives: (token: string | null) => [...FIREWALL_KEY, "directives", token] as const
};

export function firewallErrorMessage(error: unknown, fallback = "操作失败") {
  if (error instanceof ApiError) {
    return error.message || fallback;
  }
  return fallback;
}

/* ── 查询 ── */

export function useFirewallRuntimeQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.runtime(token),
    queryFn: () => firewallApi.runtime(token as string),
    enabled: Boolean(token && enabled),
    refetchInterval: 5_000,
    placeholderData: keepPreviousData
  });
}

export function useFirewallSettingsQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.settings(token),
    queryFn: () => firewallApi.settings(token as string),
    enabled: Boolean(token && enabled)
  });
}

export function useFirewallEventsQuery(query: FirewallEventQuery, options?: { live?: boolean; enabled?: boolean }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.events(token, query),
    queryFn: () => firewallApi.events(token as string, query),
    enabled: Boolean(token && (options?.enabled ?? true)),
    refetchInterval: options?.live ? 15_000 : false,
    placeholderData: keepPreviousData
  });
}

export function useFirewallEventQuery(id: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.event(token, id),
    queryFn: () => firewallApi.event(token as string, id as number),
    enabled: Boolean(token && id)
  });
}

export function useFirewallStatsQuery(query: FirewallStatsQuery, options?: { live?: boolean }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.stats(token, query),
    queryFn: () => firewallApi.stats(token as string, query),
    enabled: Boolean(token),
    refetchInterval: options?.live ? 15_000 : false,
    placeholderData: keepPreviousData
  });
}

export function useFirewallRulesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.rules(token),
    queryFn: () => firewallApi.rules(token as string),
    enabled: Boolean(token)
  });
}

export function useFirewallExclusionsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.exclusions(token),
    queryFn: () => firewallApi.exclusions(token as string),
    enabled: Boolean(token)
  });
}

export function useFirewallCatalogQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.catalog(token),
    queryFn: () => firewallApi.catalog(token as string),
    enabled: Boolean(token && enabled),
    staleTime: 60_000
  });
}

export function useFirewallCatalogRuleQuery(id: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.catalogRule(token, id),
    queryFn: () => firewallApi.catalogRule(token as string, id as number),
    enabled: Boolean(token && id)
  });
}

export function useFirewallDirectivesQuery(enabled: boolean) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.directives(token),
    queryFn: () => firewallApi.directives(token as string),
    enabled: Boolean(token && enabled)
  });
}

/* ── 写操作 ── */

function useFirewallMutation<V, R>(run: (token: string, vars: V) => Promise<R>, success?: string | ((result: R, vars: V) => string)) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => run(token as string, vars),
    onSuccess: async (result, vars) => {
      await queryClient.invalidateQueries({ queryKey: FIREWALL_KEY });
      // 旧面板（封禁、地域）用的缓存键不在 FIREWALL_KEY 下
      await queryClient.invalidateQueries({ queryKey: ["firewall-logs"] });
      if (success) {
        toast.success(typeof success === "function" ? success(result, vars) : success);
      }
    },
    onError: (error) => {
      toast.error(firewallErrorMessage(error));
    }
  });
}

export function useUpdateFirewallSettingsMutation() {
  return useFirewallMutation((token, patch: FirewallSettingsPatch) => firewallApi.updateSettings(token, patch), "已保存并在所有副本生效");
}

export function useCreateFirewallRuleMutation() {
  return useFirewallMutation((token, input: FirewallRuleInput) => firewallApi.createRule(token, input), (rule) => `已创建规则「${rule.name}」`);
}

export function useUpdateFirewallRuleMutation() {
  return useFirewallMutation(
    (token, vars: { id: number; input: FirewallRuleInput }) => firewallApi.updateRule(token, vars.id, vars.input),
    (rule) => `已保存规则「${rule.name}」`
  );
}

export function useToggleFirewallRuleMutation() {
  return useFirewallMutation(
    (token, vars: { id: number; enabled: boolean }) => firewallApi.toggleRule(token, vars.id, vars.enabled),
    (_, vars) => (vars.enabled ? "已启用" : "已停用")
  );
}

export function useDeleteFirewallRuleMutation() {
  return useFirewallMutation((token, id: number) => firewallApi.deleteRule(token, id), "已删除规则");
}

export function useReorderFirewallRulesMutation() {
  return useFirewallMutation((token, ids: number[]) => firewallApi.reorderRules(token, ids));
}

export function useValidateFirewallRuleMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (vars: { expression: string; action?: FirewallRuleInput["action"] }) =>
      firewallApi.validateRule(token as string, vars.expression, vars.action)
  });
}

export function useCreateFirewallExclusionMutation() {
  return useFirewallMutation(
    (token, input: FirewallExclusionInput) => firewallApi.createExclusion(token, input),
    (item) => `已创建例外「${item.name}」`
  );
}

export function useUpdateFirewallExclusionMutation() {
  return useFirewallMutation(
    (token, vars: { id: number; input: FirewallExclusionInput }) => firewallApi.updateExclusion(token, vars.id, vars.input),
    (item) => `已保存例外「${item.name}」`
  );
}

export function useToggleFirewallExclusionMutation() {
  return useFirewallMutation(
    (token, vars: { id: number; enabled: boolean }) => firewallApi.toggleExclusion(token, vars.id, vars.enabled),
    (_, vars) => (vars.enabled ? "已启用" : "已停用")
  );
}

export function useDeleteFirewallExclusionMutation() {
  return useFirewallMutation((token, id: number) => firewallApi.deleteExclusion(token, id), "已删除例外");
}

export function useCleanupFirewallEventsMutation() {
  return useFirewallMutation(
    (token, before: string) => firewallApi.cleanup(token, before),
    (result) => `已清理 ${result.deleted} 条事件`
  );
}

/** 测试台：不改任何状态，失败时由调用方展示错误（不弹全局提示） */
export function useFirewallSandboxMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: FirewallSandboxInput) => firewallApi.sandbox(token as string, input)
  });
}
