import { apiRequest, buildQuery } from "./client";
import type {
  RiskRule, RiskRuleDetail, RiskAssessment, RiskAssessmentDetail, RiskDeviceFingerprint,
  RiskDeviceDetail, IPRiskRecord, IPRiskDetail, RiskAction, RiskDashboard, RiskEvalResult,
  RiskMetadata, RiskExprValidation, RiskAssessmentQuery, RiskEntityQuery, RiskSimulatePayload,
  RiskRuleInput, RiskActionInput, RiskListEnvelope,
} from "./types";

const P = "/api/admin/system/risk";

/**
 * 后端分页一律返回 `{ list, total }`（与平台其余管理端接口一致）。
 * 这一层负责把它转成前端惯用的 `{ items, total }` —— 重构前前端直接把响应
 * 当成 `{ items }` 用，于是评估记录 / 待复核 / 设备 / IP 四个页签
 * 无论后端有多少数据都永远显示空列表。
 */
async function paged<T>(promise: Promise<RiskListEnvelope<T>>): Promise<{ items: T[]; total: number }> {
  const data = await promise;
  return { items: data?.list ?? [], total: data?.total ?? 0 };
}

type QueryParams = Record<string, string | number | boolean | undefined | null>;

// ── 自描述目录 ──
export const getRiskMetadata = (token: string) =>
  apiRequest<RiskMetadata>(`${P}/metadata`, { token });

export const validateRiskExpression = (token: string, expression: string) =>
  apiRequest<RiskExprValidation>(`${P}/expression/validate`, {
    method: "POST", token, body: JSON.stringify({ expression }),
  });

// ── 大盘 ──
export const getRiskDashboard = (token: string, start: string, end: string) =>
  apiRequest<RiskDashboard>(`${P}/dashboard${buildQuery({ start, end })}`, { token });

// ── 规则 ──
export const listRiskRules = (token: string, scene?: string) =>
  apiRequest<RiskRule[] | null>(`${P}/rules${buildQuery({ scene })}`, { token }).then((r) => r ?? []);

export const getRiskRule = (token: string, id: number, start?: string, end?: string) =>
  apiRequest<RiskRuleDetail>(`${P}/rules/${id}${buildQuery({ start, end })}`, { token });

export const createRiskRule = (token: string, data: RiskRuleInput) =>
  apiRequest<RiskRule>(`${P}/rules`, { method: "POST", token, body: JSON.stringify(data) });

export const updateRiskRule = (token: string, id: number, data: Partial<RiskRuleInput>) =>
  apiRequest<void>(`${P}/rules/${id}`, { method: "PUT", token, body: JSON.stringify(data) });

export const deleteRiskRule = (token: string, id: number) =>
  apiRequest<void>(`${P}/rules/${id}`, { method: "DELETE", token });

// ── 模拟 / 评估 ──
export const simulateRisk = (token: string, data: RiskSimulatePayload) =>
  apiRequest<RiskEvalResult>(`${P}/simulate`, { method: "POST", token, body: JSON.stringify(data) });

export const simulateRule = (token: string, id: number, data: RiskSimulatePayload) =>
  apiRequest<RiskEvalResult>(`${P}/rules/${id}/simulate`, { method: "POST", token, body: JSON.stringify(data) });

export const replayAssessment = (token: string, id: number) =>
  apiRequest<RiskEvalResult>(`${P}/assessments/${id}/replay`, { method: "POST", token });

// ── 评估记录 ──
export const listAssessments = (token: string, params?: RiskAssessmentQuery) =>
  paged(apiRequest<RiskListEnvelope<RiskAssessment>>(`${P}/assessments${buildQuery(params as QueryParams)}`, { token }));

export const getAssessment = (token: string, id: number) =>
  apiRequest<RiskAssessmentDetail>(`${P}/assessments/${id}`, { token });

export const purgeAssessments = (token: string, days: number) =>
  apiRequest<{ removed: number; before: string }>(`${P}/assessments`, {
    method: "DELETE", token, body: JSON.stringify({ days }),
  });

// ── 复核 ──
export const listPendingReviews = (token: string, params?: { page?: number; pageSize?: number }) =>
  paged(apiRequest<RiskListEnvelope<RiskAssessment>>(`${P}/reviews/pending${buildQuery(params as QueryParams)}`, { token }));

export const reviewAssessment = (token: string, id: number, data: { result: string; comment?: string }) =>
  apiRequest<void>(`${P}/assessments/${id}/review`, { method: "POST", token, body: JSON.stringify(data) });

// ── 设备指纹 ──
export const listDevices = (token: string, params?: RiskEntityQuery) =>
  paged(apiRequest<RiskListEnvelope<RiskDeviceFingerprint>>(`${P}/devices${buildQuery(params as QueryParams)}`, { token }));

export const getDeviceDetail = (token: string, deviceId: string) =>
  apiRequest<RiskDeviceDetail>(`${P}/devices/${encodeURIComponent(deviceId)}`, { token });

// 后端 DTO 绑定的字段名是 `tag`（不是 `riskTag`）；写错会被 binding:"required" 挡下来
// 并返回 400 —— 重构前前端发的正是 `riskTag`，于是"更新标记"这个动作从来没成功过。
export const updateDeviceTag = (token: string, id: number, tag: string, note?: string) =>
  apiRequest<void>(`${P}/devices/${id}/tag`, { method: "PUT", token, body: JSON.stringify({ tag, note }) });

// ── IP 风险库 ──
export const listIPs = (token: string, params?: RiskEntityQuery) =>
  paged(apiRequest<RiskListEnvelope<IPRiskRecord>>(`${P}/ips${buildQuery(params as QueryParams)}`, { token }));

export const getIPDetail = (token: string, ip: string) =>
  apiRequest<IPRiskDetail>(`${P}/ips/${encodeURIComponent(ip)}`, { token });

export const updateIPTag = (token: string, id: number, tag: string, note?: string) =>
  apiRequest<void>(`${P}/ips/${id}/tag`, { method: "PUT", token, body: JSON.stringify({ tag, note }) });

export const refreshIPReputation = (token: string, ip: string) =>
  apiRequest<IPRiskRecord>(`${P}/ips/${encodeURIComponent(ip)}/refresh`, { method: "POST", token });

// ── 处置策略 ──
export const listRiskActions = (token: string, scene?: string) =>
  apiRequest<RiskAction[] | null>(`${P}/actions${buildQuery({ scene })}`, { token }).then((r) => r ?? []);

export const createRiskAction = (token: string, data: RiskActionInput) =>
  apiRequest<RiskAction>(`${P}/actions`, { method: "POST", token, body: JSON.stringify(data) });

export const updateRiskAction = (token: string, id: number, data: Partial<RiskActionInput> & { isActive?: boolean }) =>
  apiRequest<void>(`${P}/actions/${id}`, { method: "PUT", token, body: JSON.stringify(data) });

export const deleteRiskAction = (token: string, id: number) =>
  apiRequest<void>(`${P}/actions/${id}`, { method: "DELETE", token });
