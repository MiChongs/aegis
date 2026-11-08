"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as risk from "@/lib/api/risk";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 风控平台的 React Query hooks。失效关系集中在这里：
 * 配置类写操作失效对应配置；发布类写操作连带失效场景列表与发布记录；
 * 名单 / 处置 / 复核失效名单、复核单与画像。
 */

export const RISK_KEYS = {
  meta: ["risk", "meta"] as const,
  scenes: ["risk", "scenes"] as const,
  scene: (key: string) => ["risk", "scene", key] as const,
  bundle: (key: string) => ["risk", "bundle", key] as const,
  releases: (key: string) => ["risk", "releases", key] as const,
  features: ["risk", "features"] as const,
  rules: ["risk", "rules"] as const,
  templates: ["risk", "templates"] as const,
  models: ["risk", "models"] as const,
  lists: ["risk", "lists"] as const,
  patterns: ["risk", "patterns"] as const,
  dashboard: ["risk", "dashboard"] as const,
  decisions: ["risk", "decisions"] as const,
  decision: (id: string) => ["risk", "decision", id] as const,
  events: ["risk", "events"] as const,
  alerts: ["risk", "alerts"] as const,
  cases: ["risk", "cases"] as const,
  graph: ["risk", "graph"] as const,
  profile: ["risk", "profile"] as const,
  status: ["risk", "status"] as const,
  audit: ["risk", "audit"] as const,
};

function useToken() {
  const token = useAdminToken();
  return token as string;
}

function useInvalidate() {
  const qc = useQueryClient();
  return (...keys: ReadonlyArray<readonly unknown[]>) => {
    for (const key of keys) qc.invalidateQueries({ queryKey: key });
    qc.invalidateQueries({ queryKey: RISK_KEYS.audit });
  };
}

// ── 读 ──

export function useRiskMeta() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.meta, queryFn: () => risk.getRiskMeta(t), enabled: !!t, staleTime: 10 * 60_000 });
}

export function useRiskScenes() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.scenes, queryFn: () => risk.listScenes(t), enabled: !!t });
}

export function useRiskScene(key: string) {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.scene(key), queryFn: () => risk.getScene(t, key), enabled: !!t && !!key });
}

export function useRiskDraftBundle(key: string, enabled = true) {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.bundle(key), queryFn: () => risk.getDraftBundle(t, key), enabled: !!t && !!key && enabled });
}

export function useRiskReleases(key: string) {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.releases(key), queryFn: () => risk.listReleases(t, key), enabled: !!t && !!key });
}

export function useRiskRelease(id: number | null) {
  const t = useToken();
  return useQuery({ queryKey: ["risk", "release", id], queryFn: () => risk.getRelease(t, id as number), enabled: !!t && !!id });
}

export function useRiskFeatures() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.features, queryFn: () => risk.listFeatures(t), enabled: !!t });
}

export function useRiskFeatureSQL(key: string | null) {
  const t = useToken();
  return useQuery({ queryKey: ["risk", "feature-sql", key], queryFn: () => risk.getFeatureSQL(t, key as string), enabled: !!t && !!key, retry: false });
}

export function useRiskRules(scene?: string) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.rules, scene ?? ""], queryFn: () => risk.listRules(t, scene), enabled: !!t });
}

export function useRiskTemplates() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.templates, queryFn: () => risk.listTemplates(t), enabled: !!t, staleTime: 10 * 60_000 });
}

export function useRiskModels() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.models, queryFn: () => risk.listModels(t), enabled: !!t });
}

export function useRiskLists(q: Parameters<typeof risk.listEntries>[1]) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.lists, q], queryFn: () => risk.listEntries(t, q), enabled: !!t, placeholderData: keepPreviousData });
}

export function useRiskPatterns() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.patterns, queryFn: () => risk.listPatterns(t), enabled: !!t });
}

export function useRiskDashboard(q: Parameters<typeof risk.getDashboard>[1]) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.dashboard, q], queryFn: () => risk.getDashboard(t, q), enabled: !!t, refetchInterval: 60_000, placeholderData: keepPreviousData });
}

export function useRiskDecisions(q: Parameters<typeof risk.listDecisions>[1], enabled = true) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.decisions, q], queryFn: () => risk.listDecisions(t, q), enabled: !!t && enabled, placeholderData: keepPreviousData });
}

export function useRiskDecision(id: string | null) {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.decision(id ?? ""), queryFn: () => risk.getDecision(t, id as string), enabled: !!t && !!id, retry: 1 });
}

export function useRiskEvents(q: Parameters<typeof risk.listEvents>[1], enabled = true) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.events, q], queryFn: () => risk.listEvents(t, q), enabled: !!t && enabled });
}

export function useRiskAlerts(q: Parameters<typeof risk.listAlerts>[1]) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.alerts, q], queryFn: () => risk.listAlerts(t, q), enabled: !!t, placeholderData: keepPreviousData });
}

export function useRiskCases(q: Parameters<typeof risk.listCases>[1]) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.cases, q], queryFn: () => risk.listCases(t, q), enabled: !!t, placeholderData: keepPreviousData });
}

export function useRiskGraph(q: Parameters<typeof risk.getGraph>[1] | null) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.graph, q], queryFn: () => risk.getGraph(t, q as Parameters<typeof risk.getGraph>[1]), enabled: !!t && !!q, retry: false });
}

export function useRiskUserProfile(app: number | undefined, user: number | undefined, days = 30) {
  const t = useToken();
  return useQuery({
    queryKey: [...RISK_KEYS.profile, app, user, days],
    queryFn: () => risk.getUserProfile(t, app as number, user as number, days),
    enabled: !!t && !!app && !!user,
    retry: false,
  });
}

export function useRiskStatus() {
  const t = useToken();
  return useQuery({ queryKey: RISK_KEYS.status, queryFn: () => risk.getRiskStatus(t), enabled: !!t, refetchInterval: 30_000 });
}

export function useRiskAudit(limit = 100) {
  const t = useToken();
  return useQuery({ queryKey: [...RISK_KEYS.audit, limit], queryFn: () => risk.listAudit(t, limit), enabled: !!t });
}

// ── 写 ──

export function useSaveScene() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (a: { key: string; scene: Partial<risk.Scene> }) => risk.saveScene(t, a.key, a.scene),
    onSuccess: (_d, a) => inv(RISK_KEYS.scenes, RISK_KEYS.scene(a.key), RISK_KEYS.bundle(a.key)),
  });
}

export function useDeleteScene() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (key: string) => risk.deleteScene(t, key), onSuccess: () => inv(RISK_KEYS.scenes) });
}

export function useValidateScene() {
  const t = useToken();
  return useMutation({ mutationFn: (key: string) => risk.validateScene(t, key) });
}

export function useSimulateScene() {
  const t = useToken();
  return useMutation({ mutationFn: (a: { key: string; input: risk.SimulateInput }) => risk.simulateScene(t, a.key, a.input) });
}

function useReleaseMutation<A extends { key: string }, R>(fn: (t: string, a: A) => Promise<R>) {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (a: A) => fn(t, a),
    onSuccess: (_d, a) => inv(RISK_KEYS.scenes, RISK_KEYS.scene(a.key), RISK_KEYS.releases(a.key), RISK_KEYS.status),
  });
}

export const usePublishScene = () =>
  useReleaseMutation((t, a: { key: string; note: string; mode: "full" | "canary" | "shadow"; canaryPercent?: number }) =>
    risk.publishScene(t, a.key, { note: a.note, mode: a.mode, canaryPercent: a.canaryPercent }));
export const usePromoteScene = () => useReleaseMutation((t, a: { key: string }) => risk.promoteScene(t, a.key));
export const useRollbackScene = () => useReleaseMutation((t, a: { key: string; releaseId: number }) => risk.rollbackScene(t, a.key, a.releaseId));
export const useWithdrawScene = () => useReleaseMutation((t, a: { key: string }) => risk.withdrawScene(t, a.key));
export const useSceneTraffic = () =>
  useReleaseMutation((t, a: { key: string; canaryPercent?: number; shadow?: boolean; drop?: boolean }) =>
    risk.setSceneTraffic(t, a.key, { canaryPercent: a.canaryPercent, shadow: a.shadow, drop: a.drop }));

export function useSaveFeature() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (f: Partial<risk.Feature> & { key: string }) => risk.saveFeature(t, f.key, f),
    onSuccess: () => inv(RISK_KEYS.features, ["risk", "bundle"], ["risk", "feature-sql"]),
  });
}

export function useDeleteFeature() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (key: string) => risk.deleteFeature(t, key), onSuccess: () => inv(RISK_KEYS.features) });
}

export function useSyncStreams() {
  const t = useToken();
  return useMutation({ mutationFn: () => risk.syncStreams(t) });
}

export function useSaveRule() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (r: Partial<risk.Rule>) => risk.saveRule(t, r), onSuccess: () => inv(RISK_KEYS.rules, RISK_KEYS.scenes, ["risk", "bundle"]) });
}

export function useDeleteRule() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (id: number) => risk.deleteRule(t, id), onSuccess: () => inv(RISK_KEYS.rules, RISK_KEYS.scenes, ["risk", "bundle"]) });
}

export function useApplyTemplate() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (a: { template: string; scene: string; observe: boolean }) => risk.applyTemplate(t, a),
    onSuccess: () => inv(RISK_KEYS.rules, RISK_KEYS.scenes, ["risk", "bundle"]),
  });
}

export function useValidateExpr() {
  const t = useToken();
  return useMutation({ mutationFn: (expr: string) => risk.validateExpr(t, expr) });
}

export function useUploadModel() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (form: FormData) => risk.uploadModel(t, form), onSuccess: () => inv(RISK_KEYS.models) });
}

export function useUpdateModel() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (a: { key: string; name?: string; description?: string; inputs?: string[]; transform?: boolean }) => risk.updateModel(t, a.key, a),
    onSuccess: () => inv(RISK_KEYS.models),
  });
}

export function useDeleteModel() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (key: string) => risk.deleteModel(t, key), onSuccess: () => inv(RISK_KEYS.models) });
}

export function usePredictModel() {
  const t = useToken();
  return useMutation({ mutationFn: (a: { key: string; values: Record<string, number> }) => risk.predictModel(t, a.key, a.values) });
}

export function useAddListEntries() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (input: Parameters<typeof risk.addEntries>[1]) => risk.addEntries(t, input), onSuccess: () => inv(RISK_KEYS.lists, RISK_KEYS.profile) });
}

export function useRemoveListEntries() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (ids: string[]) => risk.removeEntries(t, ids), onSuccess: () => inv(RISK_KEYS.lists, RISK_KEYS.profile) });
}

export function useCheckLists() {
  const t = useToken();
  return useMutation({ mutationFn: (input: Parameters<typeof risk.checkLists>[1]) => risk.checkLists(t, input) });
}

export function useRebuildLists() {
  const t = useToken();
  return useMutation({ mutationFn: () => risk.rebuildLists(t) });
}

export function useSavePattern() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (p: Partial<risk.CepPattern> & { key: string }) => risk.savePattern(t, p.key, p), onSuccess: () => inv(RISK_KEYS.patterns) });
}

export function useDeletePattern() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (key: string) => risk.deletePattern(t, key), onSuccess: () => inv(RISK_KEYS.patterns) });
}

export function useReviewCase() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({
    mutationFn: (a: { id: number } & Parameters<typeof risk.reviewCase>[2]) => risk.reviewCase(t, a.id, a),
    onSuccess: () => inv(RISK_KEYS.cases, RISK_KEYS.lists, RISK_KEYS.profile, RISK_KEYS.dashboard),
  });
}

export function useDispose() {
  const t = useToken();
  const inv = useInvalidate();
  return useMutation({ mutationFn: (input: risk.DisposeInput) => risk.dispose(t, input), onSuccess: () => inv(RISK_KEYS.lists, RISK_KEYS.profile) });
}
