"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import {
  nativePluginApi,
  trafficApi,
  type HistoryRange,
  type NativePlugin,
  type SimulateInput,
  type TrafficConfig,
  type TrafficRule,
  type TrafficSettings,
  type TrafficSnapshot
} from "@/lib/api/traffic";

/**
 * 流量管理（内置插件 traffic）的数据层。
 *
 * - 运行态（总览、单条规则统计）每 2 秒轮询一次，只在页面可见时轮询。
 * - 每个写接口都带 `expectedRevision`（取自配置缓存），返回完整的新配置并直接写回缓存，
 *   不再额外拉一次。
 * - 40990 = 修订号冲突（别的管理员先改了）：统一提示并重新拉配置，调用方不用各自处理。
 */

export const TRAFFIC_KEY = ["admin-traffic"] as const;
const NATIVE_KEY = ["admin-native-plugins"] as const;

const keys = {
  config: (token: string | null) => [...TRAFFIC_KEY, "config", token] as const,
  overview: (token: string | null, seconds: number) => [...TRAFFIC_KEY, "overview", token, seconds] as const,
  templates: (token: string | null) => [...TRAFFIC_KEY, "templates", token] as const,
  revisions: (token: string | null) => [...TRAFFIC_KEY, "revisions", token] as const,
  revision: (token: string | null, revision: number) => [...TRAFFIC_KEY, "revision", token, revision] as const,
  history: (token: string | null, range: HistoryRange, ruleId?: string) =>
    [...TRAFFIC_KEY, "history", token, range, ruleId ?? ""] as const,
  events: (token: string | null, limit: number, kind?: string) => [...TRAFFIC_KEY, "events", token, limit, kind ?? ""] as const,
  ruleStats: (token: string | null, ruleId: string, seconds: number) =>
    [...TRAFFIC_KEY, "rule-stats", token, ruleId, seconds] as const,
  validate: (token: string | null, json: string) => [...TRAFFIC_KEY, "validate", token, json] as const,
  native: (token: string | null) => [...NATIVE_KEY, token] as const
};

export const TRAFFIC_ERROR = {
  conflict: 40990,
  invalid: 40091,
  ruleNotFound: 40490,
  revisionNotFound: 40491
} as const;

export function trafficErrorCode(error: unknown) {
  return error instanceof ApiError ? error.code : undefined;
}

export function trafficErrorMessage(error: unknown, fallback = "操作失败") {
  if (error instanceof ApiError) {
    if (error.code === TRAFFIC_ERROR.ruleNotFound) return "规则不存在";
    if (error.code === TRAFFIC_ERROR.revisionNotFound) return "版本不存在";
    return error.message || fallback;
  }
  return fallback;
}

/* ── 页面可见性：隐藏时停掉轮询 ── */

function subscribeVisibility(callback: () => void) {
  document.addEventListener("visibilitychange", callback);
  return () => document.removeEventListener("visibilitychange", callback);
}

export function useDocumentVisible() {
  return useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState === "visible",
    () => true
  );
}

/* ── 规则速率环形缓冲：每次轮询到总览就记下各规则上一秒的到达数 ── */

const RING_SIZE = 60;
const EMPTY_RING: number[] = [];
const rings = new Map<string, number[]>();
const ringListeners = new Set<() => void>();
let lastRingStamp = "";

function pushRuleRates(snapshot: TrafficSnapshot) {
  // 同一个快照（缓存命中、并发请求）不重复入环
  if (!snapshot?.now || snapshot.now === lastRingStamp) return;
  lastRingStamp = snapshot.now;
  for (const rule of snapshot.rules ?? []) {
    const prev = rings.get(rule.id) ?? EMPTY_RING;
    const next = [...prev, rule.lastSecond?.arrivals ?? 0];
    rings.set(rule.id, next.length > RING_SIZE ? next.slice(next.length - RING_SIZE) : next);
  }
  ringListeners.forEach((listener) => listener());
}

function subscribeRings(listener: () => void) {
  ringListeners.add(listener);
  return () => ringListeners.delete(listener);
}

export function useRuleRateRing(ruleId: string) {
  return useSyncExternalStore(
    subscribeRings,
    () => rings.get(ruleId) ?? EMPTY_RING,
    () => EMPTY_RING
  );
}

/* ── 小工具 ── */

export function useDebouncedValue<T>(value: T, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(id);
  }, [value, delay]);
  return debounced;
}

/* ── 查询 ── */

const LIVE_INTERVAL = 2_000;

export function useTrafficOverviewQuery(seconds: number, enabled = true) {
  const token = useAdminToken();
  const visible = useDocumentVisible();
  return useQuery({
    queryKey: keys.overview(token, seconds),
    queryFn: async () => {
      const data = await trafficApi.overview(token as string, seconds);
      pushRuleRates(data.local);
      return data;
    },
    enabled: Boolean(token && enabled),
    refetchInterval: visible ? LIVE_INTERVAL : false,
    placeholderData: keepPreviousData,
    retry: 1
  });
}

export function useTrafficConfigQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.config(token),
    queryFn: () => trafficApi.config(token as string),
    enabled: Boolean(token && enabled)
  });
}

export function useTrafficTemplatesQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.templates(token),
    queryFn: () => trafficApi.templates(token as string),
    enabled: Boolean(token && enabled),
    staleTime: 5 * 60_000
  });
}

export function useTrafficRevisionsQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.revisions(token),
    queryFn: () => trafficApi.revisions(token as string, 100),
    enabled: Boolean(token && enabled)
  });
}

export function useTrafficRevisionQuery(revision: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.revision(token, revision ?? 0),
    queryFn: () => trafficApi.revision(token as string, revision as number),
    enabled: Boolean(token && revision),
    staleTime: Infinity
  });
}

export function useTrafficHistoryQuery(range: HistoryRange, ruleId?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: keys.history(token, range, ruleId),
    queryFn: () => trafficApi.history(token as string, range, ruleId || undefined),
    enabled: Boolean(token),
    placeholderData: keepPreviousData,
    refetchInterval: 60_000
  });
}

export function useTrafficEventsQuery(limit = 50, kind?: string) {
  const token = useAdminToken();
  const visible = useDocumentVisible();
  return useQuery({
    queryKey: keys.events(token, limit, kind),
    queryFn: () => trafficApi.events(token as string, limit, kind),
    enabled: Boolean(token),
    refetchInterval: visible ? 10_000 : false
  });
}

export function useTrafficRuleStatsQuery(ruleId: string | null, seconds = 60) {
  const token = useAdminToken();
  const visible = useDocumentVisible();
  return useQuery({
    queryKey: keys.ruleStats(token, ruleId ?? "", seconds),
    queryFn: () => trafficApi.ruleStats(token as string, ruleId as string, seconds),
    enabled: Boolean(token && ruleId),
    refetchInterval: visible ? LIVE_INTERVAL : false,
    placeholderData: keepPreviousData,
    retry: 1
  });
}

/** 防抖校验：草稿停止变化 400ms 后交给后端校验，返回字段级错误 */
export function useTrafficRuleValidation(rule: TrafficRule) {
  const token = useAdminToken();
  const json = useDebouncedValue(JSON.stringify(rule), 400);
  return useQuery({
    queryKey: keys.validate(token, json),
    queryFn: () => trafficApi.validateRule(token as string, JSON.parse(json) as TrafficRule),
    enabled: Boolean(token),
    placeholderData: keepPreviousData,
    staleTime: Infinity,
    retry: false
  });
}

/** 保存前同步校验一遍当前草稿（防抖还没赶上时） */
export function validateRuleNow(queryClient: QueryClient, token: string, rule: TrafficRule) {
  const json = JSON.stringify(rule);
  return queryClient.fetchQuery({
    queryKey: keys.validate(token, json),
    queryFn: () => trafficApi.validateRule(token, rule),
    staleTime: Infinity
  });
}

/* ── 写操作 ── */

function useConfigMutation<V>(
  run: (token: string, vars: V, expectedRevision: number | undefined) => Promise<TrafficConfig>,
  optimistic?: (config: TrafficConfig, vars: V) => TrafficConfig
) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const configKey = keys.config(token);

  return useMutation({
    mutationFn: (vars: V) => run(token as string, vars, queryClient.getQueryData<TrafficConfig>(configKey)?.revision),
    onMutate: async (vars: V) => {
      if (!optimistic) return { previous: undefined };
      await queryClient.cancelQueries({ queryKey: configKey });
      const previous = queryClient.getQueryData<TrafficConfig>(configKey);
      if (previous) queryClient.setQueryData(configKey, optimistic(previous, vars));
      return { previous };
    },
    onSuccess: (config) => {
      queryClient.setQueryData(configKey, config);
      void queryClient.invalidateQueries({ queryKey: [...TRAFFIC_KEY, "revisions"] });
      void queryClient.invalidateQueries({ queryKey: [...TRAFFIC_KEY, "overview"] });
    },
    onError: (error, _vars, context) => {
      if (context?.previous) queryClient.setQueryData(configKey, context.previous);
      if (trafficErrorCode(error) === TRAFFIC_ERROR.conflict) {
        toast.error("配置已被其他管理员修改，已刷新");
        void queryClient.invalidateQueries({ queryKey: [...TRAFFIC_KEY, "config"] });
        void queryClient.invalidateQueries({ queryKey: [...TRAFFIC_KEY, "revisions"] });
        return;
      }
      toast.error(trafficErrorMessage(error, "保存失败"));
    }
  });
}

export function useSaveTrafficSettingsMutation() {
  return useConfigMutation<TrafficSettings>((token, settings, expectedRevision) =>
    trafficApi.saveSettings(token, settings, { expectedRevision })
  );
}

export function useCreateTrafficRuleMutation() {
  return useConfigMutation<{ rule: TrafficRule; position?: "top" | "bottom" }>((token, { rule, position }, expectedRevision) =>
    trafficApi.createRule(token, rule, { expectedRevision, position })
  );
}

export function useUpdateTrafficRuleMutation() {
  return useConfigMutation<{ ruleId: string; rule: TrafficRule }>((token, { ruleId, rule }, expectedRevision) =>
    trafficApi.updateRule(token, ruleId, rule, { expectedRevision })
  );
}

export function useDeleteTrafficRuleMutation() {
  return useConfigMutation<string>(
    (token, ruleId, expectedRevision) => trafficApi.deleteRule(token, ruleId, { expectedRevision }),
    (config, ruleId) => ({ ...config, rules: config.rules.filter((rule) => rule.id !== ruleId) })
  );
}

export function useToggleTrafficRuleMutation() {
  return useConfigMutation<{ ruleId: string; enabled: boolean }>(
    (token, { ruleId, enabled }, expectedRevision) => trafficApi.toggleRule(token, ruleId, enabled, { expectedRevision }),
    (config, { ruleId, enabled }) => ({
      ...config,
      rules: config.rules.map((rule) => (rule.id === ruleId ? { ...rule, enabled } : rule))
    })
  );
}

export function useReorderTrafficRulesMutation() {
  return useConfigMutation<string[]>(
    (token, ids, expectedRevision) => trafficApi.reorderRules(token, ids, { expectedRevision }),
    (config, ids) => {
      const byId = new Map(config.rules.map((rule) => [rule.id, rule]));
      const ordered = ids.map((id) => byId.get(id)).filter((rule): rule is TrafficRule => Boolean(rule));
      return { ...config, rules: ordered };
    }
  );
}

export function useRollbackTrafficMutation() {
  return useConfigMutation<{ revision: number; note?: string }>((token, { revision, note }) =>
    trafficApi.rollback(token, revision, note)
  );
}

export function useResetTrafficStatsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => trafficApi.resetStats(token as string),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: [...TRAFFIC_KEY, "overview"] });
      void queryClient.invalidateQueries({ queryKey: [...TRAFFIC_KEY, "rule-stats"] });
    },
    onError: (error) => toast.error(trafficErrorMessage(error, "重置失败"))
  });
}

export function useTrafficSimulateMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: ({ input, draft }: { input: SimulateInput; draft?: TrafficRule }) =>
      trafficApi.simulate(token as string, input, draft),
    onError: (error) => toast.error(trafficErrorMessage(error, "模拟失败"))
  });
}

/* ── 内置插件 ── */

export function useNativePluginsQuery(enabled = true) {
  const token = useAdminToken();
  const visible = useDocumentVisible();
  return useQuery({
    queryKey: keys.native(token),
    queryFn: () => nativePluginApi.list(token as string),
    enabled: Boolean(token && enabled),
    // 启动中 / 停止中是过渡态，页面开着时跟上它的变化
    refetchInterval: (query) =>
      visible && query.state.data?.some((plugin) => plugin.state === "starting" || plugin.state === "stopping") ? 1_500 : false
  });
}

export type NativePluginAction = "enable" | "disable" | "restart";

const ACTION_DONE: Record<NativePluginAction, string> = {
  enable: "已启用",
  disable: "已停用",
  restart: "已重启"
};

export function useNativePluginActionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, action }: { id: string; action: NativePluginAction }) => nativePluginApi[action](token as string, id),
    onSuccess: (plugin: NativePlugin, { action }) => {
      queryClient.setQueryData<NativePlugin[]>(keys.native(token), (list) =>
        list?.map((item) => (item.id === plugin.id ? plugin : item))
      );
      void queryClient.invalidateQueries({ queryKey: NATIVE_KEY });
      void queryClient.invalidateQueries({ queryKey: TRAFFIC_KEY });
      toast.success(`${plugin.name || plugin.id}${ACTION_DONE[action]}`);
    },
    onError: (error) => toast.error(trafficErrorMessage(error, "操作失败"))
  });
}
