import { apiRequest, buildQuery } from "./client";
import type { ListEntry, RiskCase } from "./risk";

/**
 * 账号处置：一键解除风控、关联删除风险账号（主服务 /api/admin/system/risk-ops/*，见 internal/service/risk_ops.go）。
 * 主服务把封号、防火墙与风控平台的名单、复核单、关系图合成一份清单；解除与删除限超级管理员。
 */

const P = "/api/admin/system/risk-ops";

export interface OpsBan {
  id: number;
  appid: number;
  userId: number;
  banType: "temporary" | "permanent";
  banScope: "login" | "all";
  status: string;
  reason: string;
  evidence?: Record<string, unknown>;
  bannedByAdminName?: string;
  startAt: string;
  endAt?: string | null;
  createdAt: string;
}

export interface OpsUser {
  id: number;
  publicId: number;
  appid: number;
  appName?: string;
  account: string;
  nickname?: string;
  avatar?: string;
  enabled: boolean;
  createdAt: string;
  lastLoginAt?: string | null;
  ban?: OpsBan | null;
  banFromRisk: boolean;
}

export interface OpsIPBan {
  id: number;
  ip: string;
  reason: string;
  source: string;
  triggerRule: string;
  expiresAt?: string | null;
  createdAt: string;
  isp?: string;
  city?: string;
}

export type OpsEntityKind = "device" | "ip" | "account" | "phone" | "email" | "identity" | "payer" | "passkey";

export interface OpsEntity {
  kind: OpsEntityKind | string;
  value?: string;
  label: string;
  lastSeen?: number;
  shared?: boolean;
  users?: number;
  banned?: boolean;
  lists: ListEntry[];
  firewall?: OpsIPBan[];
}

export interface OpsRecent {
  blocked: number;
  reviewed: number;
  total: number;
  maxScore: number;
  lastAt: string;
  lastHit: string;
  lastReason: string;
  lastScene: string;
}

export interface OpsSubject {
  userId: number;
  user: OpsUser | null;
  graphBanned: boolean;
  graphStatus?: string;
  lists: ListEntry[];
  entities: OpsEntity[];
  cases: RiskCase[];
  recent?: OpsRecent | null;
}

export interface OpsReason {
  kind: string;
  label?: string;
  vid?: string;
  lastSeen?: number;
}

export interface OpsRelated {
  userId: number;
  name: string;
  banned: boolean;
  status?: string;
  strength: "strong" | "weak";
  via: number[];
  reasons: OpsReason[];
  lastSeen: number;
  user: OpsUser;
}

export interface OpsInspection {
  appId: number;
  days: number;
  subjects: OpsSubject[];
  related: OpsRelated[];
  errors: Record<string, string>;
}

export interface OpsUserResult { userId: number; name: string; ok: boolean; done?: string; error?: string }

export interface OpsReleaseInput {
  appId: number;
  userIds: number[];
  banIds: number[];
  ipBanIds: number[];
  entryIds: string[];
  closeCases: boolean;
  allowDays: number;
  note: string;
}

export interface OpsReleaseResult {
  users: OpsUserResult[];
  unbanned: number;
  ipUnbanned: number;
  listsRemoved: number;
  casesClosed: number;
  allowed: number;
  problems: string[];
}

export interface OpsPurgeInput {
  appId: number;
  userIds: number[];
  blockDevices: boolean;
  blockDays: number;
  note: string;
  confirm: string;
}

export interface OpsPurgeResult {
  users: OpsUserResult[];
  deleted: number;
  devicesBlocked: number;
  casesClosed: number;
  /** 关系图里删掉的节点：账号本身与只属于它们的登录名、手机号、设备、IP 等 */
  graphRemoved: number;
  problems: string[];
}

export interface OpsBannedPage { items: OpsUser[]; total: number; page: number }

/** 删除前要求输入的确认文字（与后端 RiskOpsConfirmText 一致） */
export const purgeConfirmText = (n: number) => `删除 ${n} 个账号`;

const post = <T,>(t: string, path: string, body: unknown) =>
  apiRequest<T>(`${P}${path}`, { method: "POST", token: t, body: JSON.stringify(body) });

export const inspectAccounts = (t: string, appId: number, userIds: number[], related: boolean) =>
  post<OpsInspection>(t, "/inspect", { appId, userIds, related });
export const releaseAccounts = (t: string, input: OpsReleaseInput) => post<OpsReleaseResult>(t, "/release", input);
export const purgeAccounts = (t: string, input: OpsPurgeInput) => post<OpsPurgeResult>(t, "/purge", input);
export const listRiskBanned = (t: string, q: { app?: number; q?: string; page?: number; limit?: number }) =>
  apiRequest<OpsBannedPage>(`${P}/banned${buildQuery(q)}`, { token: t });
