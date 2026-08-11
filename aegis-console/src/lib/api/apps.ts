import { apiRequest, buildQuery } from "./client";
import type {
  AdminAppUserDetail,
  AdminAppUserItem,
  AppCommerceSettings,
  AppDetail,
  AppLoginBaseline,
  AppSignInRecordItem,
  AppSignInStats,
  AppPolicy,
  AppStats,
  AppSummary,
  AuthSourceStatsResult,
  LoginAuditItem,
  NotificationItem,
  PasswordPolicy,
  PasswordPolicyTemplateCatalog,
  PasswordPolicyTestResult,
  PasswordPolicyView,
  PagedResult,
  SignInRewardPolicy,
  SignInRewardPolicyView,
  SignInRewardPreview,
  SignInRewardPreviewInput,
  SignInRewardTemplateCatalog,
  RegionStatsResult,
  SessionAuditItem,
  UserTrend
} from "./types";

export function getAdminApps(token: string) {
  return apiRequest<AppSummary[]>("/api/admin/apps", { token });
}

export function getAdminApp(token: string, appKey: string) {
  return apiRequest<AppDetail>(`/api/admin/apps/${appKey}`, { token });
}

export function getAdminAppStats(token: string, appKey: string) {
  return apiRequest<AppStats>(`/api/admin/apps/${appKey}/stats`, { token });
}

export function getAdminAppTrend(token: string, appKey: string, days = 7) {
  return apiRequest<UserTrend>(`/api/admin/apps/${appKey}/stats/user-trend?days=${days}`, { token });
}

export function getAdminAppRegions(
  token: string,
  appKey: string,
  params?: { type?: string; limit?: number }
) {
  const query = buildQuery({
    type: params?.type,
    limit: params?.limit
  });
  return apiRequest<RegionStatsResult>(`/api/admin/apps/${appKey}/stats/regions${query}`, { token });
}

export function getAdminAppAuthSources(token: string, appKey: string) {
  return apiRequest<AuthSourceStatsResult>(`/api/admin/apps/${appKey}/stats/auth-sources`, { token });
}

/**
 * 应用用户列表查询。
 *
 * 这些字段与后端 `AdminUserListQuery` 一一对应，**不要只透传 keyword** ——
 * 精确字段（account / email / registerIp / inviteCode）走等值/前缀匹配，
 * 与 keyword 的全字段模糊搜是两种完全不同的查法：
 * 「按注册 IP 找同源小号」用 keyword 会把 UA 里含该串的行也捞进来。
 */
export type AdminAppUserListParams = {
  keyword?: string;
  account?: string;
  nickname?: string;
  email?: string;
  phone?: string;
  inviteCode?: string;
  registerIp?: string;
  userId?: number;
  enabled?: boolean;
  /** YYYY-MM-DD 或 RFC3339；只给日期时后端把 createdTo 补到当天 23:59:59.999 */
  createdFrom?: string;
  createdTo?: string;
  /** createdAt(默认) / updatedAt / id / account / integral / experience / vipExpireAt / registerTime / nickname / email */
  sort?: string;
  order?: "asc" | "desc";
  page?: number;
  limit?: number;
};

export function getAdminAppUsers(token: string, appKey: string, params?: AdminAppUserListParams) {
  return apiRequest<PagedResult<AdminAppUserItem>>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/users${buildQuery({ ...params })}`,
    { token }
  );
}

export function getAdminAppUser(token: string, appKey: string, userId: number | string) {
  return apiRequest<AdminAppUserDetail>(`/api/admin/apps/${appKey}/users/${userId}`, { token });
}

export function updateAdminAppUserStatus(
  token: string,
  appKey: string,
  userId: number | string,
  payload: {
    enabled?: boolean;
    disabledReason?: string;
    clearDisabledEndTime?: boolean;
    disabledEndTime?: string;
  }
) {
  return apiRequest<AdminAppUserDetail>(`/api/admin/apps/${appKey}/users/${userId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminAppUserProfile(
  token: string,
  appKey: string,
  userId: number | string,
  payload: { nickname?: string; email?: string }
) {
  return apiRequest<void>(`/api/admin/apps/${appKey}/users/${userId}/profile`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function resetAdminAppUserPassword(
  token: string,
  appKey: string,
  userId: number | string,
  payload: { newPassword: string }
) {
  return apiRequest<void>(`/api/admin/apps/${appKey}/users/${userId}/reset-password`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function revokeAdminAppUserSessions(
  token: string,
  appKey: string,
  userId: number | string
) {
  return apiRequest<void>(`/api/admin/apps/${appKey}/users/${userId}/revoke-sessions`, {
    method: "POST",
    token
  });
}

export type SessionDetailView = {
  tokenHash: string;
  tokenId: string;
  account: string;
  deviceId?: string;
  ip: string;
  userAgent: string;
  provider?: string;
  issuedAt: string;
  expiresAt: string;
  country?: string;
  countryCode?: string;
  region?: string;
  city?: string;
  isp?: string;
  location?: string;
};

export function getAdminUserSessions(token: string, appKey: string, userId: number | string) {
  return apiRequest<{ items: SessionDetailView[]; total: number }>(
    `/api/admin/apps/${appKey}/users/${userId}/sessions`,
    { token }
  );
}

export function revokeAdminUserSession(token: string, appKey: string, userId: number | string, tokenHash: string) {
  return apiRequest<{ revoked: number }>(
    `/api/admin/apps/${appKey}/users/${userId}/sessions/${encodeURIComponent(tokenHash)}`,
    { method: "DELETE", token }
  );
}

export function revokeAdminUserSessionsBatch(token: string, appKey: string, userId: number | string, tokenHashes: string[]) {
  return apiRequest<{ revoked: number }>(
    `/api/admin/apps/${appKey}/users/${userId}/sessions/revoke-batch`,
    { method: "POST", token, body: JSON.stringify({ tokenHashes }) }
  );
}

export function deleteAdminAppUser(token: string, appKey: string, userId: number | string) {
  return apiRequest<void>(`/api/admin/apps/${appKey}/users/${userId}`, {
    method: "DELETE",
    token
  });
}

export function adjustUserIntegral(
  token: string,
  payload: { userId: number; appid: number; amount: number; reason?: string }
) {
  return apiRequest<Record<string, unknown>>("/api/app/points/adjust-integral", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function adjustUserExperience(
  token: string,
  payload: { userId: number; appid: number; amount: number; reason?: string }
) {
  return apiRequest<Record<string, unknown>>("/api/app/points/adjust-experience", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminAppLoginAudits(
  token: string,
  appKey: string,
  params?: { keyword?: string; status?: string; page?: number; limit?: number }
) {
  const query = buildQuery({
    keyword: params?.keyword,
    status: params?.status,
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<LoginAuditItem>>(`/api/admin/apps/${appKey}/audits/login${query}`, { token });
}

export function getAdminAppSessionAudits(
  token: string,
  appKey: string,
  params?: { keyword?: string; eventType?: string; page?: number; limit?: number }
) {
  const query = buildQuery({
    keyword: params?.keyword,
    eventType: params?.eventType,
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<SessionAuditItem>>(`/api/admin/apps/${appKey}/audits/sessions${query}`, { token });
}

export function getAdminAppNotifications(
  token: string,
  appKey: string,
  params?: { keyword?: string; type?: string; level?: string; page?: number; limit?: number }
) {
  const query = buildQuery({
    keyword: params?.keyword,
    type: params?.type,
    level: params?.level,
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<NotificationItem>>(`/api/admin/apps/${appKey}/notifications${query}`, { token });
}

export function createAdminApp(
  token: string,
  payload: {
    name: string;
    status?: boolean;
    registerStatus?: boolean;
    loginStatus?: boolean;
    disabledReason?: string;
    disabledRegisterReason?: string;
    disabledLoginReason?: string;
    settings?: Record<string, unknown>;
  }
) {
  return apiRequest<AppSummary>("/api/admin/apps", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAdminApp(
  token: string,
  appKey: string,
  payload: {
    name?: string;
    status?: boolean;
    disabledReason?: string;
    registerStatus?: boolean;
    disabledRegisterReason?: string;
    loginStatus?: boolean;
    disabledLoginReason?: string;
    settings?: Record<string, unknown>;
  }
) {
  return apiRequest<AppSummary>(`/api/admin/apps/${appKey}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminApp(token: string, appKey: string) {
  return apiRequest<void>(`/api/admin/apps/${appKey}`, { method: "DELETE", token });
}

// 传输加密配置
export type TransportEncryptionView = {
  enabled: boolean;
  strict: boolean;
  responseEncryption: boolean;
  hasSecret: boolean;
  secretHint?: string;
  allowedAlgorithms: string[];
  supportedAlgorithms: string[];
  hasRSAKey: boolean;
  rsaPublicKey?: string;
  hasECDHKey: boolean;
  ecdhPublicKey?: string;
};

export type TransportEncryptionUpdate = {
  enabled?: boolean;
  strict?: boolean;
  responseEncryption?: boolean;
  secret?: string;
  allowedAlgorithms?: string[];
  generateRSAKey?: boolean;
  generateECDHKey?: boolean;
};

export function getAdminAppEncryption(token: string, appKey: string) {
  return apiRequest<TransportEncryptionView>(`/api/admin/apps/${appKey}/encryption`, { token });
}

export function updateAdminAppEncryption(token: string, appKey: string, payload: TransportEncryptionUpdate) {
  return apiRequest<TransportEncryptionView>(`/api/admin/apps/${appKey}/encryption`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminAppPolicy(token: string, appKey: string) {
  return apiRequest<AppPolicy>(`/api/admin/apps/${appKey}/policy`, { token });
}

export function updateAdminAppPolicy(token: string, appKey: string, payload: AppPolicy) {
  return apiRequest<AppPolicy>(`/api/admin/apps/${appKey}/policy`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 登录绑定基线 ──
// 开启 loginCheckDevice / loginCheckIp / loginCheckUser 后，判定依据是
// 用户上一次被放行的登录指纹。重置是唯一的解绑出口。

export function getAdminAppLoginBaseline(token: string, appKey: string, userId: number | string) {
  return apiRequest<AppLoginBaseline>(`/api/admin/apps/${appKey}/login-baseline/${userId}`, { token });
}

export function resetAdminAppLoginBaseline(token: string, appKey: string, userId: number | string) {
  return apiRequest<AppLoginBaseline>(`/api/admin/apps/${appKey}/login-baseline/${userId}`, {
    method: "DELETE",
    token
  });
}

// ── 交易设置 ──

export function getAdminAppCommerceSettings(token: string, appKey: string) {
  return apiRequest<AppCommerceSettings>(`/api/admin/apps/${appKey}/commerce`, { token });
}

export function updateAdminAppCommerceSettings(token: string, appKey: string, payload: AppCommerceSettings) {
  return apiRequest<AppCommerceSettings>(`/api/admin/apps/${appKey}/commerce`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminAppPasswordPolicy(token: string, appKey: string) {
  return apiRequest<PasswordPolicyView>(`/api/admin/apps/${appKey}/password-policy`, { token });
}

export function updateAdminAppPasswordPolicy(
  token: string,
  appKey: string,
  payload: { policy: PasswordPolicy }
) {
  return apiRequest<PasswordPolicyView>(`/api/admin/apps/${appKey}/password-policy`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function testAdminAppPasswordPolicy(
  token: string,
  appKey: string,
  payload: { password: string }
) {
  return apiRequest<PasswordPolicyTestResult>(`/api/admin/apps/${appKey}/password-policy/test`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function resetAdminAppPasswordPolicy(token: string, appKey: string) {
  return apiRequest<PasswordPolicyView>(`/api/admin/apps/${appKey}/password-policy/reset`, {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}

export function getPasswordPolicyTemplates(token: string) {
  return apiRequest<PasswordPolicyTemplateCatalog>("/api/admin/apps/password-policy/templates", { token });
}

export function getAdminAppSignInReward(token: string, appKey: string) {
  return apiRequest<SignInRewardPolicyView>(`/api/admin/apps/${appKey}/signin-reward`, { token });
}

export function getAdminAppSignInStats(token: string, appKey: string, params?: { days?: number }) {
  const query = buildQuery({ days: params?.days });
  return apiRequest<AppSignInStats>(`/api/admin/apps/${appKey}/signin/stats${query}`, { token });
}

export function getAdminAppSignInRecords(
  token: string,
  appKey: string,
  params?: { keyword?: string; source?: string; dateFrom?: string; dateTo?: string; page?: number; limit?: number }
) {
  const query = buildQuery({
    keyword: params?.keyword,
    source: params?.source,
    dateFrom: params?.dateFrom,
    dateTo: params?.dateTo,
    page: params?.page,
    limit: params?.limit
  });
  return apiRequest<PagedResult<AppSignInRecordItem>>(`/api/admin/apps/${appKey}/signin/records${query}`, { token });
}

export function updateAdminAppSignInReward(
  token: string,
  appKey: string,
  payload: { policy: SignInRewardPolicy }
) {
  return apiRequest<SignInRewardPolicyView>(`/api/admin/apps/${appKey}/signin-reward`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function testAdminAppSignInReward(
  token: string,
  appKey: string,
  payload: SignInRewardPreviewInput
) {
  return apiRequest<SignInRewardPreview>(`/api/admin/apps/${appKey}/signin-reward/test`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function resetAdminAppSignInReward(token: string, appKey: string) {
  return apiRequest<SignInRewardPolicyView>(`/api/admin/apps/${appKey}/signin-reward/reset`, {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}

export function getSignInRewardTemplates(token: string) {
  return apiRequest<SignInRewardTemplateCatalog>("/api/admin/apps/signin-reward/templates", { token });
}
