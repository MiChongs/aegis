import { apiRequest, buildQuery } from "./client";
import type { DeviceInfo } from "./device-info";
import type {
  AdminAppUserDetail,
  AdminAppUserItem,
  AdminAppUserProfile,
  AppCommerceSettings,
  AppDetail,
  AppLoginBaseline,
  AppSignInRecordItem,
  AppSignInStats,
  AutoSignAdminOverview,
  AutoSignPolicy,
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
  /** 全字段模糊；`#123` 形态按用户 ID 直达 */
  keyword?: string;
  account?: string;
  nickname?: string;
  email?: string;
  phone?: string;
  inviteCode?: string;
  registerIp?: string;
  markcode?: string;
  customId?: string;
  userId?: number;
  /** 逗号分隔的用户 ID，按 ID 批量取（最多 500 个），给出时忽略其他筛选 */
  ids?: string;
  enabled?: boolean;
  /** RFC3339 绝对时刻；控制台按浏览器时区把日期换成当天起止后再传 */
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

/** 设备目录里的一台设备（来自登录记录），与后端 user.AdminDeviceItem 对齐。 */
export type AdminDeviceItem = {
  deviceId: string;
  deviceInfo?: DeviceInfo;
  firstSeenAt: string;
  lastSeenAt: string;
  logins: number;
  users: number;
  lastUser?: { id: number; account?: string; nickname?: string };
  lastIp?: string;
};

export type AdminDeviceListParams = {
  keyword?: string;
  userId?: number;
  /** 逗号分隔的设备 ID */
  ids?: string;
  limit?: number;
};

export function getAdminAppDevices(token: string, appKey: string, params?: AdminDeviceListParams) {
  return apiRequest<{ items: AdminDeviceItem[] }>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/users/devices${buildQuery({ ...params })}`,
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

/**
 * 管理员编辑用户资料，按字段 PATCH：
 * 不带的字段不动，带空串 = 清空。birthday 传 YYYY-MM-DD，清空生日用 clearBirthday。
 * avatar 只收外部 http(s) 链接；上传图片文件走 uploadAdminAppUserAvatar。
 */
export type AdminAppUserProfilePatch = {
  nickname?: string;
  email?: string;
  phone?: string;
  avatar?: string;
  birthday?: string;
  clearBirthday?: boolean;
  bio?: string;
};

export function updateAdminAppUserProfile(
  token: string,
  appKey: string,
  userId: number | string,
  payload: AdminAppUserProfilePatch
) {
  return apiRequest<{ profile?: AdminAppUserProfile }>(
    `/api/admin/apps/${appKey}/users/${userId}/profile`,
    {
      method: "PUT",
      token,
      body: JSON.stringify(payload)
    }
  );
}

/** 管理员替用户上传头像（multipart），返回更新后的档案与上传结果。 */
export function uploadAdminAppUserAvatar(
  token: string,
  appKey: string,
  userId: number | string,
  file: File
) {
  const form = new FormData();
  form.append("file", file);
  return apiRequest<{ profile?: AdminAppUserProfile; upload?: { avatarUrl?: string } }>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/avatar`,
    { method: "POST", token, body: form }
  );
}

/** 管理员移除用户自定义头像，回到服务端默认头像。 */
export function removeAdminAppUserAvatar(token: string, appKey: string, userId: number | string) {
  return apiRequest<{ profile?: AdminAppUserProfile; avatar?: { url?: string } }>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/avatar`,
    { method: "DELETE", token }
  );
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
  /** GeoIP 经纬度。缺失表示没解析出来（内网地址恒缺失），不要当 0 用 */
  latitude?: number;
  longitude?: number;
  /** 内网 / 回环来源。活动地图据此归到「服务器地址」而不是伪造坐标 */
  isPrivate?: boolean;
  /** 设备展示名（同 deviceInfo.name） */
  device?: string;
  deviceInfo?: DeviceInfo;
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

/** 积分 / 经验调整的结果，与后端 points.IntegralAdjustResult / ExperienceAdjustResult 对齐。 */
export type PointsAdjustResult = {
  userId: number;
  appid: number;
  account: string;
  amount: number;
  beforeAmount: number;
  afterAmount: number;
  reason: string;
  operationType: "add" | "consume";
  transactionNo: string;
  createdAt: string;
  /** 同一请求编号已生效过：返回的是首次结果，余额没有再次变化 */
  replayed?: boolean;
  levelChanged?: boolean;
  oldLevel?: number;
  newLevel?: number;
  /** 扣减经验时的等级处理 */
  levelMode?: ExperienceLevelMode;
};

/**
 * 扣减经验后的等级处理：keep 保留现有等级只扣经验；recalc 按扣减后的经验重新判定，可以降级
 * （不低于锁定的保底，封顶照旧）。两种都不收回已发的等级奖励，再升回去也不重复发。
 */
export type ExperienceLevelMode = "keep" | "recalc";

export type PointsAdjustPayload = {
  userId: number;
  appid: number;
  /** 正数增加、负数扣减；单次绝对值不超过 100,000,000 */
  amount: number;
  reason?: string;
  /** 防重复提交：同一编号只生效一次 */
  requestId?: string;
  /** 只对扣减经验有意义 */
  levelMode?: ExperienceLevelMode;
};

// 后端字段是下划线写法（user_id / request_id）。这里显式转换，不把驼峰对象原样发出去 ——
// 原样发送 userId 曾让每次调整都报「缺少必要的请求参数」。
function pointsAdjustBody(payload: PointsAdjustPayload) {
  return JSON.stringify({
    user_id: payload.userId,
    appid: payload.appid,
    amount: payload.amount,
    reason: payload.reason,
    request_id: payload.requestId,
    level_mode: payload.levelMode
  });
}

export function adjustUserIntegral(token: string, payload: PointsAdjustPayload) {
  return apiRequest<PointsAdjustResult>("/api/app/points/adjust-integral", {
    method: "POST",
    token,
    body: pointsAdjustBody(payload)
  });
}

export function adjustUserExperience(token: string, payload: PointsAdjustPayload) {
  return apiRequest<PointsAdjustResult>("/api/app/points/adjust-experience", {
    method: "POST",
    token,
    body: pointsAdjustBody(payload)
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
  params?: { keyword?: string; category?: string; type?: string; level?: string; page?: number; limit?: number }
) {
  const query = buildQuery({
    keyword: params?.keyword,
    category: params?.category,
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

/**
 * 旧版后端把空的规则 / 里程碑列表序列化成 null（Go 的 nil 切片），而各签到组件都直接当数组用
 * （`policy.milestones.length`），一份没有里程碑的策略就能让整个页签崩掉。在取数这一层补成空数组。
 */
function withSignInPolicyLists<T extends { policy: SignInRewardPolicy }>(view: T): T {
  return {
    ...view,
    policy: { ...view.policy, rules: view.policy.rules ?? [], milestones: view.policy.milestones ?? [] }
  };
}

export function getAdminAppSignInReward(token: string, appKey: string) {
  return apiRequest<SignInRewardPolicyView>(`/api/admin/apps/${appKey}/signin-reward`, { token }).then(
    withSignInPolicyLists
  );
}

export function getAdminAppSignInStats(token: string, appKey: string, params?: { days?: number }) {
  const query = buildQuery({ days: params?.days });
  return apiRequest<AppSignInStats>(`/api/admin/apps/${appKey}/signin/stats${query}`, { token });
}

export function getAdminAppSignInRecords(
  token: string,
  appKey: string,
  params?: { keyword?: string; source?: string; bonusType?: string; dateFrom?: string; dateTo?: string; page?: number; limit?: number }
) {
  const query = buildQuery({
    keyword: params?.keyword,
    source: params?.source,
    bonusType: params?.bonusType,
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
  }).then(withSignInPolicyLists);
}

export function getAdminAppAutoSign(token: string, appKey: string) {
  return apiRequest<AutoSignAdminOverview>(`/api/admin/apps/${appKey}/auto-sign`, { token });
}

export function updateAdminAppAutoSign(
  token: string,
  appKey: string,
  payload: { policy: Omit<AutoSignPolicy, "isDefault"> }
) {
  return apiRequest<AutoSignAdminOverview>(`/api/admin/apps/${appKey}/auto-sign`, {
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
  }).then(withSignInPolicyLists);
}

export function resetAdminAppSignInReward(token: string, appKey: string) {
  return apiRequest<SignInRewardPolicyView>(`/api/admin/apps/${appKey}/signin-reward/reset`, {
    method: "POST",
    token,
    body: JSON.stringify({})
  }).then(withSignInPolicyLists);
}

export function getSignInRewardTemplates(token: string) {
  return apiRequest<SignInRewardTemplateCatalog>("/api/admin/apps/signin-reward/templates", { token });
}
