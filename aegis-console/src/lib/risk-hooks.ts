"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as risk from "@/lib/api/risk";
import { useAdminToken } from "@/lib/admin-hooks";
import type {
  RiskAssessmentQuery, RiskEntityQuery, RiskSimulatePayload, RiskRuleInput, RiskActionInput,
} from "@/lib/api/types";

/**
 * 风控中心的 React Query hooks。
 *
 * 从 admin-hooks.ts 拆出来，与工单 / 组织域同一约定：一个域一个 hooks 文件。
 * 失效关系集中在这里定义 —— 「复核完之后大盘没刷新」这类问题的根源
 * 就是失效关系散落在各个组件里。
 */

/** 任何写操作之后都要连带失效的查询键。 */
const RISK_KEYS = {
  metadata: ["risk-metadata"] as const,
  dashboard: ["risk-dashboard"] as const,
  rules: ["risk-rules"] as const,
  rule: ["risk-rule"] as const,
  assessments: ["risk-assessments"] as const,
  assessment: ["risk-assessment"] as const,
  reviews: ["risk-reviews"] as const,
  devices: ["risk-devices"] as const,
  device: ["risk-device"] as const,
  ips: ["risk-ips"] as const,
  ip: ["risk-ip"] as const,
  actions: ["risk-actions"] as const,
};

function useInvalidate() {
  const qc = useQueryClient();
  return (...keys: readonly (readonly string[])[]) => {
    keys.forEach((key) => qc.invalidateQueries({ queryKey: key }));
  };
}

// ── 自描述目录 ──

/**
 * 目录几乎不变，缓存 30 分钟。它驱动整页的枚举与条件参数表单，
 * 每次切页签都重拉一次纯属浪费。
 */
export function useRiskMetadataQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.metadata, token],
    queryFn: () => risk.getRiskMetadata(token as string),
    enabled: Boolean(token),
    staleTime: 30 * 60 * 1000,
  });
}

export function useValidateExpressionMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (expression: string) => risk.validateRiskExpression(token as string, expression) });
}

// ── 大盘 ──

export function useRiskDashboardQuery(start?: string, end?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.dashboard, start, end, token],
    queryFn: () => risk.getRiskDashboard(token as string, start as string, end as string),
    enabled: Boolean(token && start && end),
  });
}

// ── 规则 ──

export function useRiskRulesQuery(scene?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.rules, scene ?? "", token],
    queryFn: () => risk.listRiskRules(token as string, scene),
    enabled: Boolean(token),
  });
}

export function useRiskRuleQuery(id?: number, start?: string, end?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.rule, id, start, end, token],
    queryFn: () => risk.getRiskRule(token as string, id as number, start, end),
    enabled: Boolean(token && id),
  });
}

export function useCreateRiskRuleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskRuleInput) => risk.createRiskRule(token as string, data),
    onSuccess: () => invalidate(RISK_KEYS.rules, RISK_KEYS.dashboard),
  });
}

export function useUpdateRiskRuleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; data: Partial<RiskRuleInput> }) =>
      risk.updateRiskRule(token as string, args.id, args.data),
    onSuccess: () => invalidate(RISK_KEYS.rules, RISK_KEYS.rule, RISK_KEYS.dashboard),
  });
}

export function useDeleteRiskRuleMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => risk.deleteRiskRule(token as string, id),
    onSuccess: () => invalidate(RISK_KEYS.rules, RISK_KEYS.dashboard),
  });
}

// ── 模拟 / 重放 ──

export function useSimulateRiskMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (data: RiskSimulatePayload) => risk.simulateRisk(token as string, data) });
}

export function useSimulateRuleMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (args: { ruleId: number; data: RiskSimulatePayload }) =>
      risk.simulateRule(token as string, args.ruleId, args.data),
  });
}

export function useReplayAssessmentMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (id: number) => risk.replayAssessment(token as string, id) });
}

// ── 评估记录 ──

export function useRiskAssessmentsQuery(params?: RiskAssessmentQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.assessments, params, token],
    queryFn: () => risk.listAssessments(token as string, params),
    enabled: Boolean(token),
    // 排查中的运维会反复刷新这张表；保留上一页数据避免翻页时整屏闪空。
    placeholderData: (previous) => previous,
  });
}

export function useRiskAssessmentQuery(id?: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.assessment, id, token],
    queryFn: () => risk.getAssessment(token as string, id as number),
    enabled: Boolean(token && id),
  });
}

export function usePurgeAssessmentsMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (days: number) => risk.purgeAssessments(token as string, days),
    onSuccess: () => invalidate(RISK_KEYS.assessments, RISK_KEYS.reviews, RISK_KEYS.dashboard),
  });
}

// ── 复核 ──

export function usePendingReviewsQuery(page = 1, pageSize = 20) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.reviews, page, pageSize, token],
    queryFn: () => risk.listPendingReviews(token as string, { page, pageSize }),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useReviewAssessmentMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; result: string; comment?: string }) =>
      risk.reviewAssessment(token as string, args.id, { result: args.result, comment: args.comment }),
    // 复核拒绝会连带把 IP / 设备标成封禁，四张表一起失效
    onSuccess: () => invalidate(
      RISK_KEYS.reviews, RISK_KEYS.assessments, RISK_KEYS.assessment,
      RISK_KEYS.dashboard, RISK_KEYS.ips, RISK_KEYS.devices,
    ),
  });
}

// ── 设备指纹 ──

export function useRiskDevicesQuery(params?: RiskEntityQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.devices, params, token],
    queryFn: () => risk.listDevices(token as string, params),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useRiskDeviceQuery(deviceId?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.device, deviceId, token],
    queryFn: () => risk.getDeviceDetail(token as string, deviceId as string),
    enabled: Boolean(token && deviceId),
  });
}

export function useUpdateDeviceTagMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; tag: string; note?: string }) =>
      risk.updateDeviceTag(token as string, args.id, args.tag, args.note),
    onSuccess: () => invalidate(RISK_KEYS.devices, RISK_KEYS.device),
  });
}

// ── IP 风险库 ──

export function useRiskIPsQuery(params?: RiskEntityQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.ips, params, token],
    queryFn: () => risk.listIPs(token as string, params),
    enabled: Boolean(token),
    placeholderData: (previous) => previous,
  });
}

export function useRiskIPQuery(ip?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.ip, ip, token],
    queryFn: () => risk.getIPDetail(token as string, ip as string),
    enabled: Boolean(token && ip),
  });
}

export function useUpdateIPTagMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; tag: string; note?: string }) =>
      risk.updateIPTag(token as string, args.id, args.tag, args.note),
    onSuccess: () => invalidate(RISK_KEYS.ips, RISK_KEYS.ip),
  });
}

export function useRefreshIPReputationMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (ip: string) => risk.refreshIPReputation(token as string, ip),
    onSuccess: () => invalidate(RISK_KEYS.ips, RISK_KEYS.ip),
  });
}

// ── 处置策略 ──

export function useRiskActionsQuery(scene?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...RISK_KEYS.actions, scene ?? "", token],
    queryFn: () => risk.listRiskActions(token as string, scene),
    enabled: Boolean(token),
  });
}

export function useCreateRiskActionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (data: RiskActionInput) => risk.createRiskAction(token as string, data),
    onSuccess: () => invalidate(RISK_KEYS.actions),
  });
}

export function useUpdateRiskActionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (args: { id: number; data: Partial<RiskActionInput> & { isActive?: boolean } }) =>
      risk.updateRiskAction(token as string, args.id, args.data),
    onSuccess: () => invalidate(RISK_KEYS.actions),
  });
}

export function useDeleteRiskActionMutation() {
  const token = useAdminToken();
  const invalidate = useInvalidate();
  return useMutation({
    mutationFn: (id: number) => risk.deleteRiskAction(token as string, id),
    onSuccess: () => invalidate(RISK_KEYS.actions),
  });
}
