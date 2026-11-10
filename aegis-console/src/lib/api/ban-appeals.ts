/**
 * 封禁申诉与全站封禁的管理端 API（契约见 docs/account-suspension.md 第 7 节）。
 *
 *   应用级   /api/admin/apps/{appkey}/ban-appeals      列表、统计、详情、通过、驳回、重新复审、设置
 *   跨应用   /api/admin/system/ban-appeals             全部应用的申诉（超级管理员），条目带 appid、appName
 *   全站封禁 /api/admin/system/platform-bans           列表、详情（主体打码、连带账号）、解除（超级管理员）
 *
 * 列表响应的 `items` 可能是 null（Go 的 nil slice），调用方一律 `?? []`。
 */
import { apiRequest, buildQuery } from "./client";
import type { AccountBanLevel, AccountBanReasonCode, AccountBanSource } from "./types";

// ── 申诉 ──────────────────────────

export type AppealStatus = "pending" | "reviewing" | "approved" | "rejected" | "withdrawn";
/**
 * 队列筛选。open 与 suggested 是控制台的视图，不是存储的状态：
 *   open       进行中（pending / reviewing）且还没有模型结论
 *   suggested  进行中且模型已给出结论，等人工拍板
 */
export type AppealQueue = "open" | "suggested" | "approved" | "rejected" | "withdrawn";
export type AppealVerdict = "approve" | "reject" | "escalate";
/** 风控证据强度：strong 证据确凿，weak 像误判 */
export type EvidenceStrength = "strong" | "medium" | "weak";
export type AppealAutoReview = "off" | "suggest" | "auto";

/** 决策模型（风控复评 + 大模型）的结论 */
export interface AppealModelResult {
  verdict: AppealVerdict | string;
  /** 0–1 */
  confidence: number;
  /** 给审核员看的摘要 */
  summary?: string;
  /** 给用户的回复（用户语言） */
  suggestedReply?: string;
  /** 用了哪个模型 */
  model?: string;
  reviewedAt?: string | null;
  /** 模型调用失败或被跳过时的说明 */
  error?: string;
}

export interface AppealBan {
  id: number;
  level: AccountBanLevel | string;
  banType: "temporary" | "permanent" | string;
  banScope: "login" | "all" | string;
  status: string;
  reasonCode?: AccountBanReasonCode | string;
  /** 内部原因 */
  reason?: string;
  /** 对用户显示的说明 */
  publicReason?: string;
  source?: AccountBanSource | string;
  platformBanId?: number | null;
  appealable?: boolean;
  bannedByAdminName?: string;
  startAt: string;
  endAt?: string | null;
}

export interface AppealUser {
  id: number;
  publicId?: number;
  account?: string;
  nickname?: string;
  avatar?: string;
  /** 打码后的邮箱 / 手机号 */
  email?: string;
  phone?: string;
  enabled?: boolean;
  createdAt?: string;
  lastLoginAt?: string | null;
}

export interface BanAppeal {
  id: number;
  appid: number;
  /** 跨应用列表才有 */
  appName?: string;
  banId: number;
  userId: number;
  status: AppealStatus | string;
  content: string;
  contactEmail?: string;
  /** 用户语言（en / zh-CN / zh-TW / ja / ko） */
  locale?: string;
  submittedAt: string;
  decidedAt?: string | null;
  /** model | human | "" */
  decidedBy?: string;
  decidedByAdminName?: string;
  /** 给用户的回复 */
  reply?: string;
  /** 内部备注，只在控制台显示 */
  note?: string;
  model?: AppealModelResult | null;
  /** 列表里为了拼一句话带上的摘要 */
  user?: AppealUser | null;
  ban?: AppealBan | null;
}

export interface AppealEvidence {
  level: EvidenceStrength | string;
  /** 近 30 天最高风险分 */
  maxScore?: number;
  /** 近 30 天被拦截次数 */
  blockedCount?: number;
  /** 命中的规则（名称） */
  rules?: string[];
  /** 所在名单的说明 */
  lists?: string[];
  /** 关联账号数 */
  relatedAccounts?: number;
  /** 归纳出的事实，一句一条 */
  facts?: string[];
}

export interface BanAppealDetail {
  appeal: BanAppeal;
  ban: AppealBan | null;
  user: AppealUser | null;
  evidence?: AppealEvidence | null;
  /** 同一条封禁的其它申诉（不含本条），新的在前 */
  history?: BanAppeal[] | null;
}

export interface BanAppealList {
  items: BanAppeal[] | null;
  total: number;
  page: number;
  pageSize: number;
}

export interface BanAppealStats {
  open: number;
  suggested: number;
  approved: number;
  rejected: number;
  withdrawn: number;
  total?: number;
  /** 模型自动通过 / 自动驳回的数量（已通过 / 已驳回的子集） */
  autoApproved?: number;
  autoRejected?: number;
}

export interface BanAppealSettings {
  enabled: boolean;
  autoReview: AppealAutoReview;
  approveThreshold: number;
  rejectThreshold: number;
  autoApproveManual: boolean;
  allowDays: number;
  maxAppealsPerBan: number;
  cooldownHours: number;
  minLength: number;
  maxLength: number;
  appealUrl: string;
  aiConfigId: number;
}

export interface BanAppealQuery {
  status?: AppealQueue;
  q?: string;
  page?: number;
  pageSize?: number;
  /** 只在跨应用接口上有意义 */
  appid?: number;
}

export interface AppealDecisionInput {
  reply: string;
  note: string;
}

const appBase = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/ban-appeals`;
const SYS_APPEALS = "/api/admin/system/ban-appeals";

export function listBanAppeals(token: string, appKey: string, q: BanAppealQuery) {
  const query = buildQuery({ status: q.status, q: q.q, page: q.page, pageSize: q.pageSize });
  return apiRequest<BanAppealList>(`${appBase(appKey)}${query}`, { token });
}

export function getBanAppealStats(token: string, appKey: string) {
  return apiRequest<BanAppealStats>(`${appBase(appKey)}/stats`, { token });
}

export function getBanAppeal(token: string, appKey: string, id: number) {
  return apiRequest<BanAppealDetail>(`${appBase(appKey)}/${id}`, { token });
}

export function approveBanAppeal(token: string, appKey: string, id: number, input: AppealDecisionInput) {
  return apiRequest<BanAppealDetail>(`${appBase(appKey)}/${id}/approve`, { method: "POST", token, body: JSON.stringify(input) });
}

export function rejectBanAppeal(token: string, appKey: string, id: number, input: AppealDecisionInput) {
  return apiRequest<BanAppealDetail>(`${appBase(appKey)}/${id}/reject`, { method: "POST", token, body: JSON.stringify(input) });
}

/** 重新跑一遍模型复审（异步，返回最新状态） */
export function rerunBanAppealReview(token: string, appKey: string, id: number) {
  return apiRequest<BanAppealDetail>(`${appBase(appKey)}/${id}/review`, { method: "POST", token, body: "{}" });
}

export function getBanAppealSettings(token: string, appKey: string) {
  return apiRequest<BanAppealSettings>(`${appBase(appKey)}/settings`, { token });
}

export function updateBanAppealSettings(token: string, appKey: string, settings: BanAppealSettings) {
  return apiRequest<BanAppealSettings>(`${appBase(appKey)}/settings`, { method: "PUT", token, body: JSON.stringify(settings) });
}

/** 全部应用的申诉（超级管理员） */
export function listAllBanAppeals(token: string, q: BanAppealQuery) {
  const query = buildQuery({ status: q.status, q: q.q, page: q.page, pageSize: q.pageSize, appid: q.appid });
  return apiRequest<BanAppealList>(`${SYS_APPEALS}${query}`, { token });
}

export function getAllBanAppealStats(token: string) {
  return apiRequest<BanAppealStats>(`${SYS_APPEALS}/stats`, { token });
}

// ── 全站封禁 ──────────────────────────

export type PlatformBanStatus = "active" | "revoked" | "expired";
export type PlatformSubjectKind = "email" | "phone" | "identity" | "device";

export interface PlatformBanSubject {
  kind: PlatformSubjectKind | string;
  /** 打码值，例如 a***@b.com、138****0000 */
  masked: string;
  /** 第三方身份的提供方（qq / google / wechat …） */
  provider?: string;
  createdAt?: string;
}

export interface PlatformBanAccount {
  /** 连带的 user_account_bans.id */
  banId: number;
  appid: number;
  appName?: string;
  userId: number;
  account?: string;
  nickname?: string;
  status: string;
}

export interface PlatformBan {
  id: number;
  status: PlatformBanStatus | string;
  banType: "temporary" | "permanent" | string;
  endAt?: string | null;
  reasonCode?: AccountBanReasonCode | string;
  reason?: string;
  publicReason?: string;
  source?: AccountBanSource | string;
  /** 从哪个应用的哪个账号发起 */
  originAppId?: number;
  originAppName?: string;
  originUserId?: number;
  originAccount?: string;
  createdByAdminName?: string;
  createdAt: string;
  revokedAt?: string | null;
  revokedByAdminName?: string;
  revokeReason?: string;
  subjectCount: number;
  accountCount: number;
  /** 列表里预览的几条主体 */
  subjects?: PlatformBanSubject[] | null;
}

export interface PlatformBanDetail {
  ban: PlatformBan;
  subjects: PlatformBanSubject[] | null;
  accounts: PlatformBanAccount[] | null;
}

export interface PlatformBanList {
  items: PlatformBan[] | null;
  total: number;
  page: number;
  pageSize: number;
}

const SYS_PLATFORM = "/api/admin/system/platform-bans";

export function listPlatformBans(token: string, q: { status?: string; q?: string; page?: number; pageSize?: number }) {
  const query = buildQuery({ status: q.status, q: q.q, page: q.page, pageSize: q.pageSize });
  return apiRequest<PlatformBanList>(`${SYS_PLATFORM}${query}`, { token });
}

export function getPlatformBan(token: string, id: number) {
  return apiRequest<PlatformBanDetail>(`${SYS_PLATFORM}/${id}`, { token });
}

export function revokePlatformBan(token: string, id: number, reason: string) {
  return apiRequest<PlatformBanDetail>(`${SYS_PLATFORM}/${id}/revoke`, { method: "POST", token, body: JSON.stringify({ reason }) });
}
