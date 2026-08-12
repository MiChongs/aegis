"use client";

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { evictAvatarCache } from "@/components/ui/avatar";
import * as appOAuth from "@/lib/api/app-oauth";
import * as database from "@/lib/api/database";
import * as reports from "@/lib/api/reports";
import * as risk from "@/lib/api/risk";
import * as userMaster from "@/lib/api/user-master";
import * as storageRes from "@/lib/api/storage-resource";
import * as sessionMgmt from "@/lib/api/session-mgmt";
import {
  archiveSystemAnnouncement,
  createSystemAnnouncement,
  deleteSystemAnnouncement,
  getSystemAnnouncements,
  publishSystemAnnouncement,
  updateSystemAnnouncement,
} from "@/lib/api/system";
import {
  assignWorkflowTask,
  auditAdminSite,
  batchAuditAdminSites,
  batchInitializeUserSettings,
  batchReviewAdminRoleApplications,
  checkUserSettingsIntegrity,
  cleanupUserSettings,
  createAdminAccount,
  createAdminApp,
  createAdminEmailConfig,
  createAdminPaymentConfig,
  deleteAdminVipPlan,
  getAdminPaymentOrderDetail,
  getAdminPaymentOrders,
  getAdminVipPlans,
  getAdminVipTransactions,
  grantAdminVip,
  saveAdminVipPlan,
  createAdminVersion,
  createAdminVersionChannel,
  createAppStorageConfig,
  createGlobalStorageConfig,
  createWorkflow,
  createWorkflowFromTemplate,
  deleteAdminEmailConfig,
  deleteAdminPaymentConfig,
  deleteAdminSite,
  deleteAdminVersion,
  deleteAdminVersionChannel,
  deleteAppStorageConfig,
  deleteGlobalStorageConfig,
  deleteWorkflow,
  getAdminAppPasswordPolicy,
  getAdminAppSignInRecords,
  getAdminAppSignInStats,
  getAdminAppSignInReward,
  getAdminAppCommerceSettings,
  getAdminAppLoginBaseline,
  getAdminAppPolicy,
  getAdminEmailConfigs,
  getAdminEmailDeliveries,
  getAdminPaymentConfigs,
  getAdminPaymentMethods,
  getAdminPaymentRefundable,
  getAdminPaymentRefunds,
  getAdminOrderRefunds,
  createAdminPaymentRefund,
  syncAdminPaymentRefund,
  getAdminAccounts,
  getAdminApp,
  getAdminAppAuthSources,
  getAdminAppLoginAudits,
  getAdminAppNotifications,
  getAdminProfile,
  getAdminAppRegions,
  getAdminApps,
  getAdminAppSessionAudits,
  getAdminAppStats,
  getAdminAppTrend,
  getAdminAppUser,
  getAdminAppUsers,
  getAdminRoleApplicationStatistics,
  getAdminRoleApplications,
  getAdminSiteAuditStats,
  getAdminSiteAudits,
  getAdminSites,
  getAdminUserSettings,
  getAdminUserSettingsStats,
  getAdminVersionChannels,
  getAdminVersionStats,
  getAdminVersions,
  getAdminMe,
  getAdminRoles,
  getAdminRolePermissionTree,
  getRoleMatrix,
  getRoleGraph,
  getRoleImpactPreview,
  createCustomRole,
  updateCustomRole,
  deleteCustomRole,
  listAuditLogs,
  getAuditStats,
  listMessageTemplates,
  createMessageTemplate,
  updateMessageTemplate,
  deleteMessageTemplate,
  getAdminDashboard,
  getAppMonitor,
  getAppMonitorComponents,
  getAppOnlineStats,
  getAppOnlineUsers,
  getAppStorageConfigDetail,
  getAppStorageConfigs,
  getGlobalStorageConfigDetail,
  getGlobalStorageConfigs,
  getSystemOnlineStats,
  getSystemMonitor,
  getSystemMonitorApps,
  getSystemMonitorComponents,
  getSystemMonitorHistory,
  getAppMonitorHistory,
  getWorkflowDetail,
  getWorkflowEngineStatus,
  getWorkflowInstanceDetail,
  getWorkflowInstances,
  getWorkflowList,
  getWorkflowLogs,
  getWorkflowNodeTypes,
  getWorkflowStatistics,
  getWorkflowTaskDetail,
  getWorkflowTaskHistory,
  getWorkflowTasksTodo,
  getWorkflowTemplates,
  initializeUserSettings,
  getPasswordPolicyTemplates,
  getSignInRewardTemplates,
  pauseWorkflowInstance,
  previewVersionChannelMatch,
  publishAdminVersion,
  revokeAdminVersion,
  resetAdminAppLoginBaseline,
  resetAdminAppPasswordPolicy,
  resetAdminAppSignInReward,
  reviewAdminRoleApplication,
  resumeWorkflowInstance,
  saveWorkflowAsTemplate,
  startWorkflow,
  testAdminAppPasswordPolicy,
  testAdminAppSignInReward,
  testAdminEmailConfig,
  testAdminPaymentConfig,
  testAppStorageConfig,
  testGlobalStorageConfig,
  completeWorkflowTask,
  toggleAdminSitePin,
  updateAdminAccountAccess,
  updateAdminAccountStatus,
  updateAdminApp,
  deleteAdminApp,
  getAdminAppEncryption,
  updateAdminAppEncryption,
  updateAdminAppCommerceSettings,
  updateAdminAppPasswordPolicy,
  updateAdminAppSignInReward,
  updateAdminAppPolicy,
  updateAdminProfile,
  updateAdminAppUserStatus,
  updateAdminAppUserProfile,
  resetAdminAppUserPassword,
  revokeAdminAppUserSessions,
  getAdminUserSessions,
  revokeAdminUserSession,
  revokeAdminUserSessionsBatch,
  getAdminCaptchaConfig,
  updateAdminCaptchaConfig,
  generateCaptcha,
  getAdminCaptchaPublicConfig,
  testAdminCaptchaSMS,
  deleteAdminAppUser,
  adjustUserIntegral,
  adjustUserExperience,
  updateAdminEmailConfig,
  updateAdminPaymentConfig,
  updateAdminSite,
  updateAdminVersion,
  updateAdminVersionChannel,
  updateAppStorageConfig,
  updateGlobalStorageConfig,
  updateWorkflow,
  uploadAdminAvatar,
  validateWorkflowDefinition,
  cancelWorkflowInstance,
  initEpayPaymentConfig
} from "@/lib/api-client";
import {
  beginAdminPasskeyRegistration,
  beginAdminTOTPEnrollment,
  deleteAdminPasskey,
  disableAdminTOTP,
  enableAdminTOTP,
  finishAdminPasskeyRegistration,
  generateAdminRecoveryCodes,
  getAdminPasskeys,
  getAdminRecoveryCodes,
  getAdminSecurityStatus,
  regenerateAdminRecoveryCodes
} from "@/lib/api/security";
import {
  getAdminSystemSettings,
  updateAdminSystemSettings,
  getFirewallLogs,
  getFirewallLogDetail,
  getFirewallStats,
  deleteFirewallLogs,
  getIPBans,
  createIPBan,
  getIPBanModes,
  deleteIPBan,
  getGeoBans,
  upsertGeoBan,
  toggleGeoBan,
  deleteGeoBan
} from "@/lib/api/system";
import type { FirewallLogListParams, IPBanListParams } from "@/lib/api/system";
import type { AdminAppUserListParams } from "@/lib/api/apps";
import type { StorageBatchAction, StorageObjectQuery } from "@/lib/api/types";
import {
  getAdminLotteryActivities,
  getAdminLotteryActivity,
  createAdminLotteryActivity,
  updateAdminLotteryActivity,
  deleteAdminLotteryActivity,
  getAdminLotteryPrizes,
  createAdminLotteryPrize,
  updateAdminLotteryPrize,
  deleteAdminLotteryPrize,
  commitLotterySeed,
  revealLotterySeed,
  getAdminLotteryDraws,
  getAdminLotteryStats
} from "@/lib/api/lottery";
import {
  bulkDeletePlatformBanners,
  createPlatformBanner,
  deletePlatformBanner,
  getActivePlatformBanners,
  listPlatformBanners,
  updatePlatformBanner,
  uploadPlatformBannerImage,
  type PlatformBannerListParams
} from "@/lib/api/platform-banners";
import { useAuthStore } from "@/lib/auth-store";

async function invalidateWorkflowQueries(queryClient: ReturnType<typeof useQueryClient>) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["workflow-list"] }),
    queryClient.invalidateQueries({ queryKey: ["workflow-detail"] }),
    queryClient.invalidateQueries({ queryKey: ["workflow-statistics"] }),
    queryClient.invalidateQueries({ queryKey: ["workflow-instances"] }),
    queryClient.invalidateQueries({ queryKey: ["workflow-logs"] }),
    queryClient.invalidateQueries({ queryKey: ["workflow-templates"] }),
    queryClient.invalidateQueries({ queryKey: ["workflow-tasks"] })
  ]);
}

export function useAdminToken() {
  return useAuthStore((state) => state.accessToken);
}

export function useHydrated() {
  return useAuthStore((state) => state.hydrated);
}

export function useAdminSessionQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-session", token],
    queryFn: () => getAdminMe(token as string),
    enabled: Boolean(token),
    retry: false
  });
}

export function useAdminAppsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-apps", token],
    queryFn: () => getAdminApps(token as string),
    enabled: Boolean(token)
  });
}

export function useAdminAppQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app", token, appKey],
    queryFn: () => getAdminApp(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppStatsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-stats", token, appKey],
    queryFn: () => getAdminAppStats(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppTrendQuery(appKey?: string | null, days = 7) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-trend", token, appKey, days],
    queryFn: () => getAdminAppTrend(token as string, appKey as string, days),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppRegionsQuery(
  appKey?: string | null,
  params?: { type?: string; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-regions", token, appKey, params?.type, params?.limit],
    queryFn: () => getAdminAppRegions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppAuthSourcesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-auth-sources", token, appKey],
    queryFn: () => getAdminAppAuthSources(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppUsersQuery(appKey?: string | null, params?: AdminAppUserListParams) {
  const token = useAdminToken();
  return useQuery({
    // 参数已有十来项，逐个列进 key 早晚会漏一个（漏掉的那项换值时不会重新请求）。
    // 序列化整个对象最稳；params 是本轮渲染现构造的普通对象，JSON 顺序由字面量顺序决定，
    // 调用方只要不动态拼 key 就是稳定的。
    queryKey: ["admin-app-users", token, appKey, JSON.stringify(params ?? {})],
    queryFn: () => getAdminAppUsers(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    // 翻页 / 改排序时保留上一页数据，避免表格整块闪成骨架
    placeholderData: (previous) => previous
  });
}

export function useAdminAppUserQuery(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-user", token, appKey, userId],
    queryFn: () => getAdminAppUser(token as string, appKey as string, userId as string | number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useAdminAppLoginAuditsQuery(
  appKey?: string | null,
  params?: { keyword?: string; status?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-login-audits", token, appKey, params?.keyword, params?.status, params?.page, params?.limit],
    queryFn: () => getAdminAppLoginAudits(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppSessionAuditsQuery(
  appKey?: string | null,
  params?: { keyword?: string; eventType?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-session-audits", token, appKey, params?.keyword, params?.eventType, params?.page, params?.limit],
    queryFn: () => getAdminAppSessionAudits(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppNotificationsQuery(
  appKey?: string | null,
  params?: { keyword?: string; type?: string; level?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      "admin-app-notifications",
      token,
      appKey,
      params?.keyword,
      params?.type,
      params?.level,
      params?.page,
      params?.limit
    ],
    queryFn: () => getAdminAppNotifications(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAccountsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-accounts", token],
    queryFn: () => getAdminAccounts(token as string),
    enabled: Boolean(token)
  });
}

export function useAdminProfileQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-profile", token],
    queryFn: () => getAdminProfile(token as string),
    enabled: Boolean(token)
  });
}

export function useAdminSecurityStatusQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-security", token],
    queryFn: () => getAdminSecurityStatus(token as string),
    enabled: Boolean(token && enabled)
  });
}

export function useAdminRecoveryCodesQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-recovery-codes", token],
    queryFn: () => getAdminRecoveryCodes(token as string),
    enabled: Boolean(token && enabled)
  });
}

export function useAdminPasskeysQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-passkeys", token],
    queryFn: () => getAdminPasskeys(token as string),
    enabled: Boolean(token && enabled)
  });
}

export function useAdminRolesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-roles", token],
    queryFn: () => getAdminRoles(token as string),
    enabled: Boolean(token)
  });
}

export function useAdminRolePermissionTreeQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-role-permission-tree", token],
    queryFn: () => getAdminRolePermissionTree(token as string),
    enabled: Boolean(token)
  });
}

export function useRoleMatrixQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["role-matrix", token],
    queryFn: () => getRoleMatrix(token as string),
    enabled: Boolean(token)
  });
}

export function useRoleGraphQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["role-graph", token],
    queryFn: () => getRoleGraph(token as string),
    enabled: Boolean(token)
  });
}

export function useRoleImpactPreviewQuery(roleKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["role-impact", roleKey, token],
    queryFn: () => getRoleImpactPreview(token as string, roleKey as string),
    enabled: Boolean(token && roleKey)
  });
}

export function useCreateCustomRoleMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createCustomRole>[1]) => createCustomRole(token as string, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-roles"] });
      qc.invalidateQueries({ queryKey: ["admin-role-permission-tree"] });
      qc.invalidateQueries({ queryKey: ["role-matrix"] });
      qc.invalidateQueries({ queryKey: ["role-graph"] });
    }
  });
}

export function useUpdateCustomRoleMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ roleKey, ...payload }: { roleKey: string } & Parameters<typeof updateCustomRole>[2]) => updateCustomRole(token as string, roleKey, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-roles"] });
      qc.invalidateQueries({ queryKey: ["admin-role-permission-tree"] });
      qc.invalidateQueries({ queryKey: ["role-matrix"] });
      qc.invalidateQueries({ queryKey: ["role-graph"] });
    }
  });
}

export function useDeleteCustomRoleMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ roleKey, force }: { roleKey: string; force?: boolean }) => deleteCustomRole(token as string, roleKey, force),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-roles"] });
      qc.invalidateQueries({ queryKey: ["admin-role-permission-tree"] });
      qc.invalidateQueries({ queryKey: ["role-matrix"] });
      qc.invalidateQueries({ queryKey: ["role-graph"] });
    }
  });
}

// ── 审计日志 ──

export function useAuditLogsQuery(params: Parameters<typeof listAuditLogs>[1]) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["audit-logs", params, token], queryFn: () => listAuditLogs(token as string, params), enabled: Boolean(token) });
}

/**
 * 审计日志无限滚动版本——供虚拟化列表使用。
 * 以 page 为分页游标，从 1 开始；根据当前累计条数与 total 比较得到 hasMore。
 */
export function useAuditLogsInfiniteQuery(
  params: Omit<Parameters<typeof listAuditLogs>[1], "page">,
  limit = 50
) {
  const token = useAdminToken();
  return useInfiniteQuery({
    queryKey: ["audit-logs-infinite", params, limit, token],
    initialPageParam: 1,
    enabled: Boolean(token),
    queryFn: ({ pageParam }) =>
      listAuditLogs(token as string, { ...params, limit, page: pageParam as number }),
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((sum, p) => sum + p.items.length, 0);
      if (loaded >= lastPage.total) return undefined;
      return (lastPage.page ?? allPages.length) + 1;
    }
  });
}

export function useAuditStatsQuery() {
  const token = useAdminToken();
  return useQuery({ queryKey: ["audit-stats", token], queryFn: () => getAuditStats(token as string), enabled: Boolean(token) });
}

// ── 消息模板 ──

export function useMessageTemplatesQuery() {
  const token = useAdminToken();
  return useQuery({ queryKey: ["message-templates", token], queryFn: () => listMessageTemplates(token as string), enabled: Boolean(token) });
}

export function useCreateMessageTemplateMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createMessageTemplate>[1]) => createMessageTemplate(token as string, payload),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["message-templates"] }); }
  });
}

export function useUpdateMessageTemplateMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ code, ...payload }: { code: string } & Record<string, unknown>) => updateMessageTemplate(token as string, code, payload),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["message-templates"] }); }
  });
}

export function useDeleteMessageTemplateMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => deleteMessageTemplate(token as string, code),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["message-templates"] }); }
  });
}

// ── 会话管理 Hooks ──

export function useAllSessionsQuery(page = 1, limit = 20) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["all-sessions", page, limit, token], queryFn: () => sessionMgmt.listAllSessions(token as string, { page, limit }), enabled: Boolean(token) });
}
export function useAdminSessionsQuery(adminId?: number | null) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["admin-sessions", adminId, token], queryFn: () => sessionMgmt.listAdminSessions(token as string, adminId as number), enabled: Boolean(token && adminId) });
}
export function useOnlineAdminsQuery() {
  const token = useAdminToken();
  return useQuery({ queryKey: ["online-admins", token], queryFn: () => sessionMgmt.listOnlineAdmins(token as string), enabled: Boolean(token), refetchInterval: 15000 });
}
export function useRevokeSessionMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (sessionId: string) => sessionMgmt.revokeSession(token as string, sessionId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["all-sessions"] }); qc.invalidateQueries({ queryKey: ["admin-sessions"] }); qc.invalidateQueries({ queryKey: ["online-admins"] }); } });
}
export function useForceLogoutMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (adminId: number) => sessionMgmt.forceLogoutAdmin(token as string, adminId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["all-sessions"] }); qc.invalidateQueries({ queryKey: ["admin-sessions"] }); qc.invalidateQueries({ queryKey: ["online-admins"] }); } });
}
export function useTempPermissionsQuery(adminId?: number) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["temp-permissions", adminId, token], queryFn: () => sessionMgmt.listTempPermissions(token as string, adminId), enabled: Boolean(token) });
}
export function useGrantTempPermissionMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { adminId: number; permission: string; appId?: number | null; reason: string; expiresAt: string }) => sessionMgmt.grantTempPermission(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["temp-permissions"] }); } });
}
export function useRevokeTempPermissionMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => sessionMgmt.revokeTempPermission(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["temp-permissions"] }); } });
}
export function useDelegationsQuery(adminId?: number, role?: string) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["delegations", adminId, role, token], queryFn: () => sessionMgmt.listDelegations(token as string, { adminId, role }), enabled: Boolean(token) });
}
export function useCreateDelegationMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { delegatorId: number; delegateId: number; scope: string; scopeId?: number | null; reason: string; expiresAt: string }) => sessionMgmt.createDelegation(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["delegations"] }); } });
}
export function useRevokeDelegationMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => sessionMgmt.revokeDelegation(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["delegations"] }); } });
}

// ── 风控中心 Hooks ──
//
// 已迁至 lib/risk-hooks.ts（与工单 / 组织域同一约定：一个域一个 hooks 文件）。
// 风控的失效关系比其他域复杂得多 —— 一次复核会连带影响评估记录、待复核队列、
// 大盘、IP 库与设备库五张表，散在这里会漏。

// ── 报表分析 Hooks ──

function useReportQuery<T>(key: string, appKey: string | undefined, fn: (token: string, appKey: string, p: { start: string; end: string }) => Promise<T>, start?: string, end?: string) {
  const token = useAdminToken();
  return useQuery({ queryKey: [key, appKey, start, end, token], queryFn: () => fn(token as string, appKey as string, { start: start!, end: end! }), enabled: Boolean(token && appKey && start && end) });
}
export function useRegistrationReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-registration", appKey, reports.getRegistrationReport, start, end); }
export function useLoginReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-login", appKey, reports.getLoginReport, start, end); }
export function useRetentionReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-retention", appKey, reports.getRetentionReport, start, end); }
export function useActiveReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-active", appKey, reports.getActiveReport, start, end); }
export function useDeviceReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-device", appKey, reports.getDeviceReport, start, end); }
export function useRegionReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-region", appKey, reports.getRegionReport, start, end); }
export function useChannelReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-channel", appKey, reports.getChannelReport, start, end); }
export function usePaymentReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-payment", appKey, reports.getPaymentReport, start, end); }
export function useNotificationReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-notification", appKey, reports.getNotificationReport, start, end); }
export function useRiskReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-risk", appKey, reports.getRiskReport, start, end); }
export function useActivityReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-activity", appKey, reports.getActivityReport, start, end); }
export function useFunnelReportQuery(appKey?: string, start?: string, end?: string) { return useReportQuery("report-funnel", appKey, reports.getFunnelReport, start, end); }

// ── 用户主数据 Hooks ──

export function useGlobalIdentitiesQuery(params?: { keyword?: string; status?: string; lifecycleState?: string; riskLevel?: string; tagId?: number; page?: number; limit?: number }) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["global-identities", params, token], queryFn: () => userMaster.listIdentities(token as string, params), enabled: Boolean(token) });
}
export function useGlobalIdentityQuery(id?: number | null) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["global-identity", id, token], queryFn: () => userMaster.getIdentity(token as string, id as number), enabled: Boolean(token && id) });
}
export function useUpdateIdentityStatusMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { id: number; status: string }) => userMaster.updateIdentityStatus(token as string, args.id, args.status), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identities"] }); qc.invalidateQueries({ queryKey: ["global-identity"] }); } });
}
export function useUpdateIdentityRiskMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { id: number; riskScore: number; riskLevel: string }) => userMaster.updateIdentityRisk(token as string, args.id, { riskScore: args.riskScore, riskLevel: args.riskLevel }), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identities"] }); qc.invalidateQueries({ queryKey: ["global-identity"] }); } });
}
export function useUpdateIdentityLifecycleMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { id: number; state: string }) => userMaster.updateIdentityLifecycle(token as string, args.id, args.state), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identities"] }); } });
}
export function useSyncIdentitiesMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (appId: number) => userMaster.syncIdentities(token as string, appId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identities"] }); } });
}
export function useMergeIdentityMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { id: number; targetId: number }) => userMaster.mergeIdentity(token as string, args.id, args.targetId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identities"] }); } });
}
export function useDeactivateIdentityMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { id: number; reason?: string; coolingDays?: number }) => userMaster.deactivateIdentity(token as string, args.id, args.reason, args.coolingDays), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identities"] }); qc.invalidateQueries({ queryKey: ["deactivations"] }); } });
}
export function useUserMasterTagsQuery() {
  const token = useAdminToken();
  return useQuery({ queryKey: ["user-master-tags", token], queryFn: () => userMaster.listTags(token as string), enabled: Boolean(token) });
}
export function useCreateUserMasterTagMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { name: string; color: string; description?: string }) => userMaster.createTag(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-master-tags"] }); } });
}
export function useDeleteUserMasterTagMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => userMaster.deleteTag(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-master-tags"] }); } });
}
export function useAssignTagMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { identityId: number; tagId: number }) => userMaster.assignTag(token as string, args.identityId, args.tagId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identity"] }); } });
}
export function useRemoveTagMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { identityId: number; tagId: number }) => userMaster.removeTag(token as string, args.identityId, args.tagId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["global-identity"] }); } });
}
export function useUserSegmentsQuery() {
  const token = useAdminToken();
  return useQuery({ queryKey: ["user-segments", token], queryFn: () => userMaster.listSegments(token as string), enabled: Boolean(token) });
}
export function useCreateUserSegmentMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { name: string; description?: string; segmentType: string }) => userMaster.createSegment(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-segments"] }); } });
}
export function useDeleteUserSegmentMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => userMaster.deleteSegment(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-segments"] }); } });
}
export function useUserListEntriesQuery(listType: string, page = 1) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["user-lists", listType, page, token], queryFn: () => userMaster.listUserListEntries(token as string, listType, { page }), enabled: Boolean(token) });
}
export function useCreateUserListEntryMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { listType: string; identityId?: number; email?: string; phone?: string; ip?: string; reason: string }) => userMaster.createUserListEntry(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-lists"] }); } });
}
export function useDeleteUserListEntryMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => userMaster.deleteUserListEntry(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-lists"] }); } });
}
export function useUserAppealsQuery(status?: string, page = 1) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["user-appeals", status, page, token], queryFn: () => userMaster.listAppeals(token as string, { status, page }), enabled: Boolean(token) });
}
export function useReviewAppealMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { id: number; action: string; comment?: string }) => userMaster.reviewAppeal(token as string, args.id, { action: args.action, comment: args.comment }), onSuccess: () => { qc.invalidateQueries({ queryKey: ["user-appeals"] }); } });
}
export function useDeactivationsQuery() {
  const token = useAdminToken();
  return useQuery({ queryKey: ["deactivations", token], queryFn: () => userMaster.listDeactivations(token as string), enabled: Boolean(token) });
}
export function useCancelDeactivationMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => userMaster.cancelDeactivation(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["deactivations"] }); } });
}

// ── 存储资源 Hooks ──

/**
 * 一次操作会同时改变列表、回收站与用量三张表。失效关系集中在这里而不是散在
 * 组件的 onSuccess 里 —— 散着写必然漏掉其中几张（重构前删完文件用量还是旧数字）。
 */
const STORAGE_OBJECT_SCOPE_KEYS = [["storage-objects"], ["storage-trash"], ["storage-usage"]] as const;

function invalidateStorageObjectScope(qc: ReturnType<typeof useQueryClient>) {
  STORAGE_OBJECT_SCOPE_KEYS.forEach((queryKey) => qc.invalidateQueries({ queryKey: [...queryKey] }));
}

export function useStorageObjectsQuery(params?: StorageObjectQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["storage-objects", params, token],
    queryFn: () => storageRes.listStorageObjects(token as string, params),
    enabled: Boolean(token),
    // 翻页 / 切目录时保留上一页数据，避免列表整体闪成骨架屏
    placeholderData: (previous) => previous,
  });
}
export function useTrashObjectsQuery(configId?: number) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["storage-trash", configId, token], queryFn: () => storageRes.listTrashObjects(token as string, { configId }), enabled: Boolean(token) });
}
export function useSoftDeleteObjectMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => storageRes.softDeleteStorageObject(token as string, id), onSuccess: () => invalidateStorageObjectScope(qc) });
}
export function useRestoreObjectMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => storageRes.restoreStorageObject(token as string, id), onSuccess: () => invalidateStorageObjectScope(qc) });
}
export function usePermanentDeleteObjectMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => storageRes.permanentDeleteStorageObject(token as string, id), onSuccess: () => invalidateStorageObjectScope(qc) });
}
export function useBatchStorageObjectsMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { action: StorageBatchAction; ids: number[] }) =>
      storageRes.batchMutateStorageObjects(token as string, args.action, args.ids),
    onSuccess: () => invalidateStorageObjectScope(qc),
  });
}
export function useStorageObjectLinkMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (args: { objectId: number; download?: boolean; expiresIn?: number }) =>
      storageRes.createStorageObjectLink(token as string, args.objectId, args),
  });
}
export function useCleanupTrashMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (days?: number) => storageRes.cleanupTrash(token as string, days), onSuccess: () => invalidateStorageObjectScope(qc) });
}
export function useStorageRulesQuery(configId?: number, appId?: number) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["storage-rules", configId, appId, token], queryFn: () => storageRes.listStorageRules(token as string, { configId, appId }), enabled: Boolean(token) });
}
export function useCreateStorageRuleMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { configId?: number; appId?: number; name: string; ruleType: string; ruleData: Record<string, unknown> }) => storageRes.createStorageRule(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["storage-rules"] }); } });
}
export function useDeleteStorageRuleMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => storageRes.deleteStorageRule(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["storage-rules"] }); } });
}
export function useCDNConfigQuery(configId?: number | null) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["cdn-config", configId, token], queryFn: () => storageRes.getCDNConfig(token as string, configId as number), enabled: Boolean(token && configId) });
}
export function useUpsertCDNConfigMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (args: { configId: number; data: Record<string, unknown> }) => storageRes.upsertCDNConfig(token as string, args.configId, args.data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["cdn-config"] }); } });
}
export function useDeleteCDNConfigMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (configId: number) => storageRes.deleteCDNConfig(token as string, configId), onSuccess: () => { qc.invalidateQueries({ queryKey: ["cdn-config"] }); } });
}
export function useImageRulesQuery(configId?: number) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["image-rules", configId, token], queryFn: () => storageRes.listImageRules(token as string, configId), enabled: Boolean(token) });
}
export function useCreateImageRuleMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (data: { configId?: number; name: string; ruleType: string; ruleData: Record<string, unknown> }) => storageRes.createImageRule(token as string, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ["image-rules"] }); } });
}
export function useDeleteImageRuleMutation() {
  const token = useAdminToken(); const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => storageRes.deleteImageRule(token as string, id), onSuccess: () => { qc.invalidateQueries({ queryKey: ["image-rules"] }); } });
}
export function useStorageUsageQuery(configId?: number) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["storage-usage", configId, token], queryFn: () => storageRes.getStorageUsage(token as string, configId), enabled: Boolean(token) });
}
export function useStorageUsageHistoryQuery(configId?: number | null, days?: number) {
  const token = useAdminToken();
  return useQuery({ queryKey: ["storage-usage-history", configId, days, token], queryFn: () => storageRes.getStorageUsageHistory(token as string, configId as number, days), enabled: Boolean(token && configId) });
}

export function useAdminDashboardQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-dashboard", token],
    queryFn: () => getAdminDashboard(token as string),
    enabled: Boolean(token),
    refetchInterval: 60000,
  });
}

export function useSystemOnlineStatsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["system-online-stats", token],
    queryFn: () => getSystemOnlineStats(token as string),
    enabled: Boolean(token)
  });
}

export function useAdminSystemSettingsQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-system-settings", token],
    queryFn: () => getAdminSystemSettings(token as string),
    enabled: Boolean(token && enabled)
  });
}

// 监控接口在后端是超管专属；控制台内带 token 请求，公开状态页 token 为空按匿名发出
export function useSystemMonitorQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["system-monitor", token],
    queryFn: () => getSystemMonitor(token),
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useSystemMonitorAppsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["system-monitor-apps", token],
    queryFn: () => getSystemMonitorApps(token),
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useSystemMonitorComponentsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["system-monitor-components", token],
    queryFn: () => getSystemMonitorComponents(token),
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useAppMonitorQuery(appId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-monitor", appId, token],
    queryFn: () => getAppMonitor(appId as string | number, token),
    enabled: Boolean(appId),
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useAppMonitorComponentsQuery(appId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-monitor-components", appId, token],
    queryFn: () => getAppMonitorComponents(appId as string | number, token),
    enabled: Boolean(appId),
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useSystemMonitorHistoryQuery(keys: string[], range_: import("@/lib/api/monitor").MonitorHistoryRange = "hour") {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["system-monitor-history", keys.join(","), range_, token],
    queryFn: () => getSystemMonitorHistory(keys, range_, token),
    enabled: keys.length > 0,
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useAppMonitorHistoryQuery(appId?: number | string | null, keys: string[] = [], range_: import("@/lib/api/monitor").MonitorHistoryRange = "hour") {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-monitor-history", appId, keys.join(","), range_, token],
    queryFn: () => getAppMonitorHistory(appId as string | number, keys, range_, token),
    enabled: Boolean(appId) && keys.length > 0,
    refetchInterval: 15_000,
    staleTime: 10_000
  });
}

export function useAppOnlineStatsQuery(appId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-online-stats", token, appId],
    queryFn: () => getAppOnlineStats(token as string, appId as string | number),
    enabled: Boolean(token && appId)
  });
}

export function useAppOnlineUsersQuery(
  appId?: number | string | null,
  params?: { page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-online-users", token, appId, params?.page, params?.limit],
    queryFn: () => getAppOnlineUsers(token as string, appId as string | number, params),
    enabled: Boolean(token && appId)
  });
}

export function useCreateAdminAppMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminApp>[1]) =>
      createAdminApp(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-apps"] });
    }
  });
}

export function useUpdateAdminAppMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminApp>[2]) =>
      updateAdminApp(token as string, appKey as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-apps"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-stats"] })
      ]);
    }
  });
}

/**
 * 按行更新任意应用（appKey 随 payload 传入）。
 *
 * `useUpdateAdminAppMutation` 在创建时就绑死了一个 appKey，列表页要给每一行
 * 提供「停用 / 关闭注册」这类快捷开关时用不了它（Hook 不能在循环里调）。
 * 后端 `AdminAppUpsertRequest` 全字段都是指针，因此这里只发改动的那一项，
 * 不会把没传的字段覆盖成零值。
 */
export function useAdminAppPatchMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appKey, ...payload }: { appKey: string } & Parameters<typeof updateAdminApp>[2]) =>
      updateAdminApp(token as string, appKey, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-apps"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-stats"] }),
        // 列表页的用户数 / 今日新增来自治理总览，状态改了这里也要跟着刷新
        queryClient.invalidateQueries({ queryKey: ["platform-governance", "apps"] })
      ]);
    }
  });
}

export function useDeleteAdminAppMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (appKey: string) => deleteAdminApp(token as string, appKey),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-apps"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-stats"] })
      ]);
    }
  });
}

export function useAdminAppEncryptionQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-encryption", token, appKey],
    queryFn: () => getAdminAppEncryption(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useUpdateAdminAppEncryptionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAppEncryption>[2]) =>
      updateAdminAppEncryption(token as string, appKey as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-encryption"] });
    }
  });
}

export function useCreateAdminAccountMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminAccount>[1]) =>
      createAdminAccount(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-accounts"] });
    }
  });
}

export function useUpdateAdminProfileMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const patchOperator = useAuthStore((state) => state.patchOperator);
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminProfile>[1]) =>
      updateAdminProfile(token as string, payload),
    onSuccess: async (profile) => {
      patchOperator({
        displayName: profile.account?.displayName,
        avatar: profile.account?.avatar
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-profile"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-accounts"] })
      ]);
    }
  });
}

export function useUpdateAdminSystemSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminSystemSettings>[1]) =>
      updateAdminSystemSettings(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-system-settings"] });
    }
  });
}

export function useBeginAdminTOTPEnrollmentMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: () => beginAdminTOTPEnrollment(token as string)
  });
}

export function useEnableAdminTOTPMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof enableAdminTOTP>[1]) => enableAdminTOTP(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-security"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-recovery-codes"] })
      ]);
    }
  });
}

export function useDisableAdminTOTPMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof disableAdminTOTP>[1]) => disableAdminTOTP(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-security"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-recovery-codes"] })
      ]);
    }
  });
}

export function useGenerateAdminRecoveryCodesMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof generateAdminRecoveryCodes>[1]) =>
      generateAdminRecoveryCodes(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-security"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-recovery-codes"] })
      ]);
    }
  });
}

export function useRegenerateAdminRecoveryCodesMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof regenerateAdminRecoveryCodes>[1]) =>
      regenerateAdminRecoveryCodes(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-security"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-recovery-codes"] })
      ]);
    }
  });
}

export function useBeginAdminPasskeyRegistrationMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: () => beginAdminPasskeyRegistration(token as string)
  });
}

export function useFinishAdminPasskeyRegistrationMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof finishAdminPasskeyRegistration>[1]) =>
      finishAdminPasskeyRegistration(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-security"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-passkeys"] })
      ]);
    }
  });
}

export function useDeleteAdminPasskeyMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (credentialId: string) => deleteAdminPasskey(token as string, credentialId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-security"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-passkeys"] })
      ]);
    }
  });
}

export function useUploadAdminAvatarMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const patchOperator = useAuthStore((state) => state.patchOperator);
  return useMutation({
    mutationFn: (payload: Parameters<typeof uploadAdminAvatar>[1]) =>
      uploadAdminAvatar(token as string, payload),
    onSuccess: async (result) => {
      const oldAvatar = useAuthStore.getState().operator?.avatar;
      if (oldAvatar) evictAvatarCache(oldAvatar.split("?")[0]);
      patchOperator({
        displayName: result.profile?.account?.displayName,
        avatar: result.profile?.account?.avatar || result.upload?.avatar,
        avatarVersion: Date.now(),
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-profile"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-accounts"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-session"] })
      ]);
    }
  });
}

export function useUpdateAdminAccountStatusMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { adminId: number | string; status: "active" | "disabled" }) =>
      updateAdminAccountStatus(token as string, payload.adminId, { status: payload.status }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-accounts"] });
    }
  });
}

export function useUpdateAdminAccountAccessMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAccountAccess>[2] & { adminId: number | string }) =>
      updateAdminAccountAccess(token as string, payload.adminId, {
        isSuperAdmin: payload.isSuperAdmin,
        assignments: payload.assignments
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-accounts"] });
    }
  });
}

export function useAdminAppPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-policy", token, appKey],
    queryFn: () => getAdminAppPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useUpdateAdminAppPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAppPolicy>[2]) =>
      updateAdminAppPolicy(token as string, appKey as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-policy"] });
    }
  });
}

export function useAdminAppCommerceSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-commerce", token, appKey],
    queryFn: () => getAdminAppCommerceSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useUpdateAdminAppCommerceSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAppCommerceSettings>[2]) =>
      updateAdminAppCommerceSettings(token as string, appKey as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-commerce"] });
    }
  });
}

export function useAdminAppLoginBaselineQuery(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-login-baseline", token, appKey, userId],
    queryFn: () => getAdminAppLoginBaseline(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useResetAdminAppLoginBaselineMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userId: number | string) => resetAdminAppLoginBaseline(token as string, appKey as string, userId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-login-baseline"] });
    }
  });
}

export function useAdminAppPasswordPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-password-policy", token, appKey],
    queryFn: () => getAdminAppPasswordPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function usePasswordPolicyTemplatesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["password-policy-templates", token],
    queryFn: () => getPasswordPolicyTemplates(token as string),
    enabled: Boolean(token)
  });
}

export function useUpdateAdminAppPasswordPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAppPasswordPolicy>[2]) =>
      updateAdminAppPasswordPolicy(token as string, appKey as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-password-policy"] });
    }
  });
}

export function useTestAdminAppPasswordPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof testAdminAppPasswordPolicy>[2]) =>
      testAdminAppPasswordPolicy(token as string, appKey as string, payload)
  });
}

export function useResetAdminAppPasswordPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => resetAdminAppPasswordPolicy(token as string, appKey as string),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-password-policy"] });
    }
  });
}

export function useAdminAppSignInRewardQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-signin-reward", token, appKey],
    queryFn: () => getAdminAppSignInReward(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppSignInStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-signin-stats", token, appKey, days],
    queryFn: () => getAdminAppSignInStats(token as string, appKey as string, { days }),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminAppSignInRecordsQuery(
  appKey?: string | null,
  params?: { keyword?: string; source?: string; dateFrom?: string; dateTo?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-app-signin-records", token, appKey, params],
    queryFn: () => getAdminAppSignInRecords(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useSignInRewardTemplatesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["signin-reward-templates", token],
    queryFn: () => getSignInRewardTemplates(token as string),
    enabled: Boolean(token)
  });
}

export function useUpdateAdminAppSignInRewardMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAppSignInReward>[2]) =>
      updateAdminAppSignInReward(token as string, appKey as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-signin-reward"] });
    }
  });
}

export function useTestAdminAppSignInRewardMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof testAdminAppSignInReward>[2]) =>
      testAdminAppSignInReward(token as string, appKey as string, payload)
  });
}

export function useResetAdminAppSignInRewardMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => resetAdminAppSignInReward(token as string, appKey as string),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-app-signin-reward"] });
    }
  });
}

// ──────────────── 平台级 Banner（超级管理员专属） ────────────────

export function usePlatformBannersQuery(params: PlatformBannerListParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-banners", token, params],
    queryFn: () => listPlatformBanners(token as string, params),
    enabled: Boolean(token)
  });
}

/** Overview 轮播消费的 hook；60s staleTime 与后端缓存 TTL 对齐 */
export function useActivePlatformBannersQuery(enabled: boolean = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-banners", "active", token],
    queryFn: () => getActivePlatformBanners(token as string),
    enabled: Boolean(token) && enabled,
    // 5 分钟 staleTime：横幅改动不频繁，延长缓存让跨页回到总览时数据即刻可用
    staleTime: 5 * 60_000,
    // gcTime（TanStack Query v5 的 cacheTime 替代）：10 分钟保留非活跃缓存，
    // 卸载后再回来同样秒开，图片走 HTTP 缓存几乎无延迟
    gcTime: 10 * 60_000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchOnMount: false
  });
}

function invalidatePlatformBanners(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["platform-banners"] });
}

export function useCreatePlatformBannerMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createPlatformBanner>[1]) =>
      createPlatformBanner(token as string, payload),
    onSuccess: () => invalidatePlatformBanners(qc)
  });
}

export function useUpdatePlatformBannerMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { id: number | string; data: Parameters<typeof updatePlatformBanner>[2] }) =>
      updatePlatformBanner(token as string, args.id, args.data),
    onSuccess: () => invalidatePlatformBanners(qc)
  });
}

export function useDeletePlatformBannerMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number | string) => deletePlatformBanner(token as string, id),
    onSuccess: () => invalidatePlatformBanners(qc)
  });
}

export function useBulkDeletePlatformBannersMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids: number[]) => bulkDeletePlatformBanners(token as string, ids),
    onSuccess: () => invalidatePlatformBanners(qc)
  });
}

/** 平台 Banner 图片上传（交给 Dropzone 使用） */
export function useUploadPlatformBannerImageMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (args: { file: File; configName?: string }) =>
      uploadPlatformBannerImage(token as string, args.file, args.configName)
  });
}

export function useAdminUserSettingsStatsQuery(appId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-settings-stats", token, appId],
    queryFn: () => getAdminUserSettingsStats(token as string, appId as string | number),
    enabled: Boolean(token && appId)
  });
}

export function useAdminUserSettingsQuery(appId?: number | string | null, userId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-settings", token, appId, userId],
    queryFn: () => getAdminUserSettings(token as string, { appid: appId as string | number, userId: userId as string | number }),
    enabled: Boolean(token && appId && userId)
  });
}

export function useBatchInitializeUserSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof batchInitializeUserSettings>[1]) =>
      batchInitializeUserSettings(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-user-settings-stats"] });
    }
  });
}

export function useInitializeUserSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof initializeUserSettings>[1]) =>
      initializeUserSettings(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-user-settings-stats"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-user-settings"] })
      ]);
    }
  });
}

export function useCheckUserSettingsIntegrityMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof checkUserSettingsIntegrity>[1]) =>
      checkUserSettingsIntegrity(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-user-settings-stats"] });
    }
  });
}

export function useCleanupUserSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof cleanupUserSettings>[1]) =>
      cleanupUserSettings(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-user-settings-stats"] });
    }
  });
}

export function useAdminEmailConfigsQuery(appId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-email-configs", token, appId],
    queryFn: () => getAdminEmailConfigs(token as string, { appid: appId as number }),
    enabled: Boolean(token && appId)
  });
}

export function useCreateAdminEmailConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminEmailConfig>[1]) =>
      createAdminEmailConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-email-configs"] });
    }
  });
}

export function useUpdateAdminEmailConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminEmailConfig>[1]) =>
      updateAdminEmailConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-email-configs"] });
    }
  });
}

export function useDeleteAdminEmailConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof deleteAdminEmailConfig>[1]) =>
      deleteAdminEmailConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-email-configs"] });
    }
  });
}

export function useTestAdminEmailConfigMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof testAdminEmailConfig>[1]) =>
      testAdminEmailConfig(token as string, payload)
  });
}

/**
 * 邮件投递留痕。Zeabur 通道的状态由 webhook 异步推进，
 * 因此开着面板时定时刷新，避免看到的永远是刚发出时的 pending。
 */
export function useAdminEmailDeliveriesQuery(
  appId?: number | null,
  filters?: { configId?: number; status?: string; keyword?: string; page?: number; pageSize?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-email-deliveries", token, appId, filters],
    queryFn: () =>
      getAdminEmailDeliveries(token as string, {
        appid: appId as number,
        config_id: filters?.configId,
        status: filters?.status,
        keyword: filters?.keyword,
        page: filters?.page,
        pageSize: filters?.pageSize
      }),
    enabled: Boolean(token && appId),
    refetchInterval: 30_000
  });
}

/**
 * 支付渠道目录：全部可用渠道的自描述元数据（含配置字段 schema）。
 * 与应用无关且几乎不变，因此长缓存，避免每次打开配置面板重复请求。
 */
export function useAdminPaymentMethodsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-payment-methods", token],
    queryFn: () => getAdminPaymentMethods(token as string),
    enabled: Boolean(token),
    staleTime: 30 * 60 * 1000
  });
}

// ── 退款 ──

/** 订单可退款额度与渠道退款能力；仅在退款弹窗打开时请求 */
export function useAdminPaymentRefundableQuery(appId?: number | null, orderNo?: string | null, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-payment-refundable", token, appId, orderNo],
    queryFn: () => getAdminPaymentRefundable(token as string, { appid: appId as number, order_no: orderNo as string }),
    enabled: Boolean(token && appId && orderNo && enabled)
  });
}

export function useAdminOrderRefundsQuery(appId?: number | null, orderNo?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-order-refunds", token, appId, orderNo],
    queryFn: () => getAdminOrderRefunds(token as string, { appid: appId as number, order_no: orderNo as string }),
    enabled: Boolean(token && appId && orderNo)
  });
}

export function useAdminPaymentRefundsQuery(
  appId?: number | null,
  filters?: { status?: string; payment_method?: string; keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-payment-refunds", token, appId, filters],
    queryFn: () => getAdminPaymentRefunds(token as string, { appid: appId as number, ...filters }),
    enabled: Boolean(token && appId)
  });
}

/** 发起退款后，订单、退款单、可退额度三处缓存都会变，统一失效 */
export function useCreateAdminPaymentRefundMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminPaymentRefund>[1]) =>
      createAdminPaymentRefund(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-payment-refunds"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-order-refunds"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-payment-refundable"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-payment-orders"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-payment-order-detail"] })
      ]);
    }
  });
}

export function useSyncAdminPaymentRefundMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof syncAdminPaymentRefund>[1]) =>
      syncAdminPaymentRefund(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-payment-refunds"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-order-refunds"] })
      ]);
    }
  });
}

export function useAdminPaymentConfigsQuery(appId?: number | null, paymentMethod?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-payment-configs", token, appId, paymentMethod],
    queryFn: () =>
      getAdminPaymentConfigs(token as string, {
        appid: appId as number,
        payment_method: paymentMethod || undefined
      }),
    enabled: Boolean(token && appId)
  });
}

export function useAdminPaymentOrdersQuery(
  appId?: number | null,
  filters?: { status?: string; payment_method?: string; keyword?: string; user_id?: number; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-payment-orders", token, appId, filters],
    queryFn: () =>
      getAdminPaymentOrders(token as string, {
        appid: appId as number,
        ...filters
      }),
    enabled: Boolean(token && appId)
  });
}

export function useAdminPaymentOrderDetailQuery(appId?: number | null, orderNo?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-payment-order-detail", token, appId, orderNo],
    queryFn: () => getAdminPaymentOrderDetail(token as string, { appid: appId as number, order_no: orderNo as string }),
    enabled: Boolean(token && appId && orderNo)
  });
}

export function useAdminVipPlansQuery(appId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-vip-plans", token, appId],
    queryFn: () => getAdminVipPlans(token as string, appId as number),
    enabled: Boolean(token && appId)
  });
}

export function useSaveAdminVipPlanMutation(appId?: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof saveAdminVipPlan>[2]) =>
      saveAdminVipPlan(token as string, appId as number, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-vip-plans"] });
    }
  });
}

export function useDeleteAdminVipPlanMutation(appId?: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (planId: number) => deleteAdminVipPlan(token as string, appId as number, planId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-vip-plans"] });
    }
  });
}

export function useGrantAdminVipMutation(appId?: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof grantAdminVip>[2]) =>
      grantAdminVip(token as string, appId as number, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-vip-transactions"] });
    }
  });
}

export function useAdminVipTransactionsQuery(
  appId?: number | null,
  params?: { userId?: number; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-vip-transactions", token, appId, params],
    queryFn: () => getAdminVipTransactions(token as string, appId as number, params || {}),
    enabled: Boolean(token && appId)
  });
}

export function useCreateAdminPaymentConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminPaymentConfig>[1]) =>
      createAdminPaymentConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-payment-configs"] });
    }
  });
}

export function useUpdateAdminPaymentConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminPaymentConfig>[1]) =>
      updateAdminPaymentConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-payment-configs"] });
    }
  });
}

export function useDeleteAdminPaymentConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof deleteAdminPaymentConfig>[1]) =>
      deleteAdminPaymentConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-payment-configs"] });
    }
  });
}

export function useTestAdminPaymentConfigMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof testAdminPaymentConfig>[1]) =>
      testAdminPaymentConfig(token as string, payload)
  });
}

export function useInitEpayPaymentConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof initEpayPaymentConfig>[1]) =>
      initEpayPaymentConfig(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-payment-configs"] });
    }
  });
}

export function useAdminVersionsQuery(
  appKey?: string | null,
  params?: { page?: number; limit?: number; status?: string; platform?: string; channel_id?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-versions", token, appKey, params?.page, params?.limit, params?.status, params?.platform, params?.channel_id],
    queryFn: () => getAdminVersions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminVersionChannelsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-version-channels", token, appKey],
    queryFn: () => getAdminVersionChannels(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminVersionStatsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-version-stats", token, appKey],
    queryFn: () => getAdminVersionStats(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useCreateAdminVersionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; payload: Record<string, unknown> }) =>
      createAdminVersion(token as string, p.appKey, p.payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-versions"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function useUpdateAdminVersionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; versionId: number; payload: Record<string, unknown> }) =>
      updateAdminVersion(token as string, p.appKey, p.versionId, p.payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-versions"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function useDeleteAdminVersionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; versionId: number }) =>
      deleteAdminVersion(token as string, p.appKey, p.versionId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-versions"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function usePublishAdminVersionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; versionId: number }) =>
      publishAdminVersion(token as string, p.appKey, p.versionId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-versions"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function useRevokeAdminVersionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; versionId: number }) =>
      revokeAdminVersion(token as string, p.appKey, p.versionId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-versions"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function useCreateAdminVersionChannelMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; payload: Record<string, unknown> }) =>
      createAdminVersionChannel(token as string, p.appKey, p.payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-version-channels"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function useUpdateAdminVersionChannelMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; channelId: number; payload: Record<string, unknown> }) =>
      updateAdminVersionChannel(token as string, p.appKey, p.channelId, p.payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-version-channels"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function useDeleteAdminVersionChannelMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (p: { appKey: string; channelId: number }) =>
      deleteAdminVersionChannel(token as string, p.appKey, p.channelId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-version-channels"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-version-stats"] })
      ]);
    }
  });
}

export function usePreviewVersionChannelMatchMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof previewVersionChannelMatch>[1]) =>
      previewVersionChannelMatch(token as string, payload)
  });
}

export function useAdminSitesQuery(
  appId?: number | null,
  params?: { page?: number; limit?: number; keyword?: string; status?: string }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-sites", token, appId, params?.page, params?.limit, params?.keyword, params?.status],
    queryFn: () => getAdminSites(token as string, { appid: appId as number, ...params }),
    enabled: Boolean(token && appId)
  });
}

export function useAdminSiteAuditsQuery(
  appId?: number | null,
  params?: { page?: number; limit?: number; keyword?: string; status?: string }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-site-audits", token, appId, params?.page, params?.limit, params?.keyword, params?.status],
    queryFn: () => getAdminSiteAudits(token as string, { appid: appId as number, ...params }),
    enabled: Boolean(token && appId)
  });
}

export function useAdminSiteAuditStatsQuery(appId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-site-audit-stats", token, appId],
    queryFn: () => getAdminSiteAuditStats(token as string, { appid: appId as number }),
    enabled: Boolean(token && appId)
  });
}

export function useAuditAdminSiteMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof auditAdminSite>[1]) => auditAdminSite(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-site-audits"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-sites"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-site-audit-stats"] })
      ]);
    }
  });
}

export function useBatchAuditAdminSitesMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof batchAuditAdminSites>[1]) =>
      batchAuditAdminSites(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-site-audits"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-sites"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-site-audit-stats"] })
      ]);
    }
  });
}

export function useToggleAdminSitePinMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof toggleAdminSitePin>[1]) =>
      toggleAdminSitePin(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-sites"] });
    }
  });
}

export function useUpdateAdminSiteMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminSite>[1]) => updateAdminSite(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-sites"] });
    }
  });
}

export function useDeleteAdminSiteMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof deleteAdminSite>[1]) => deleteAdminSite(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-sites"] });
    }
  });
}

export function useAdminRoleApplicationsQuery(
  appId?: number | null,
  params?: { page?: number; limit?: number; status?: string; requestedRole?: string; priority?: string; keyword?: string }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      "admin-role-applications",
      token,
      appId,
      params?.page,
      params?.limit,
      params?.status,
      params?.requestedRole,
      params?.priority,
      params?.keyword
    ],
    queryFn: () => getAdminRoleApplications(token as string, { appid: appId as number, ...params }),
    enabled: Boolean(token && appId)
  });
}

export function useAdminRoleApplicationStatisticsQuery(appId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-role-application-statistics", token, appId],
    queryFn: () => getAdminRoleApplicationStatistics(token as string, { appid: appId as number }),
    enabled: Boolean(token && appId)
  });
}

export function useReviewAdminRoleApplicationMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof reviewAdminRoleApplication>[1]) =>
      reviewAdminRoleApplication(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-role-applications"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-role-application-statistics"] })
      ]);
    }
  });
}

export function useBatchReviewAdminRoleApplicationsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof batchReviewAdminRoleApplications>[1]) =>
      batchReviewAdminRoleApplications(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-role-applications"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-role-application-statistics"] })
      ]);
    }
  });
}

export function useGlobalStorageConfigsQuery(provider?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["storage-global", token, provider],
    queryFn: () => getGlobalStorageConfigs(token as string, provider),
    enabled: Boolean(token)
  });
}

export function useAppStorageConfigsQuery(appid?: number | null, provider?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["storage-app", token, appid, provider],
    queryFn: () => getAppStorageConfigs(token as string, appid as number, provider),
    enabled: Boolean(token && appid)
  });
}

export function useGlobalStorageConfigDetailQuery(configId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["storage-global-detail", token, configId],
    queryFn: () => getGlobalStorageConfigDetail(token as string, configId as number),
    enabled: Boolean(token && configId)
  });
}

export function useAppStorageConfigDetailQuery(appid?: number | null, configId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["storage-app-detail", token, appid, configId],
    queryFn: () => getAppStorageConfigDetail(token as string, appid as number, configId as number),
    enabled: Boolean(token && appid && configId)
  });
}

export function useCreateGlobalStorageConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createGlobalStorageConfig>[1]) =>
      createGlobalStorageConfig(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["storage-global"] }),
        queryClient.invalidateQueries({ queryKey: ["storage-global-detail"] })
      ]);
    }
  });
}

export function useUpdateGlobalStorageConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateGlobalStorageConfig>[1]) =>
      updateGlobalStorageConfig(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["storage-global"] }),
        queryClient.invalidateQueries({ queryKey: ["storage-global-detail"] })
      ]);
    }
  });
}

export function useDeleteGlobalStorageConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (configId: number) => deleteGlobalStorageConfig(token as string, configId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["storage-global"] }),
        queryClient.invalidateQueries({ queryKey: ["storage-global-detail"] })
      ]);
    }
  });
}

export function useTestGlobalStorageConfigMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (configId: number) => testGlobalStorageConfig(token as string, configId)
  });
}

export function useCreateAppStorageConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAppStorageConfig>[1]) =>
      createAppStorageConfig(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["storage-app"] }),
        queryClient.invalidateQueries({ queryKey: ["storage-app-detail"] })
      ]);
    }
  });
}

export function useUpdateAppStorageConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAppStorageConfig>[1]) =>
      updateAppStorageConfig(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["storage-app"] }),
        queryClient.invalidateQueries({ queryKey: ["storage-app-detail"] })
      ]);
    }
  });
}

export function useDeleteAppStorageConfigMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { appid: number; configId: number }) =>
      deleteAppStorageConfig(token as string, payload.appid, payload.configId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["storage-app"] }),
        queryClient.invalidateQueries({ queryKey: ["storage-app-detail"] })
      ]);
    }
  });
}

export function useTestAppStorageConfigMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: { appid: number; configId: number }) =>
      testAppStorageConfig(token as string, payload.appid, payload.configId)
  });
}

export function useUpdateAdminAppUserStatusMutation(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateAdminAppUserStatus>[3]) =>
      updateAdminAppUserStatus(token as string, appKey as string, userId as string | number, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-app-users"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-user"] }),
        queryClient.invalidateQueries({ queryKey: ["app-online-users"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-stats"] })
      ]);
    }
  });
}

// ── 用户管理操作 ──────────────────────────

export function useUpdateAdminAppUserProfileMutation(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { nickname?: string; email?: string }) =>
      updateAdminAppUserProfile(token as string, appKey as string, userId as string | number, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-app-users"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-user"] })
      ]);
    }
  });
}

export function useResetAdminAppUserPasswordMutation(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: { newPassword: string }) =>
      resetAdminAppUserPassword(token as string, appKey as string, userId as string | number, payload)
  });
}

export function useRevokeAdminAppUserSessionsMutation(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: () => revokeAdminAppUserSessions(token as string, appKey as string, userId as string | number)
  });
}

export function useAdminUserSessionsQuery(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-sessions", token, appKey, userId],
    queryFn: () => getAdminUserSessions(token as string, appKey as string, userId as string | number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useRevokeAdminUserSessionMutation(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (tokenHash: string) =>
      revokeAdminUserSession(token as string, appKey as string, userId as string | number, tokenHash),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-user-sessions"] });
    }
  });
}

export function useRevokeAdminUserSessionsBatchMutation(appKey?: string | null, userId?: number | string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (tokenHashes: string[]) =>
      revokeAdminUserSessionsBatch(token as string, appKey as string, userId as string | number, tokenHashes),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-user-sessions"] });
    }
  });
}

export function useDeleteAdminAppUserMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userId: number) => deleteAdminAppUser(token as string, appKey as string, userId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-app-users"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-user"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-stats"] })
      ]);
    }
  });
}

export function useAdjustUserIntegralMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { userId: number; appid: number; amount: number; reason?: string }) =>
      adjustUserIntegral(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-app-users"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-user"] })
      ]);
    }
  });
}

export function useAdjustUserExperienceMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { userId: number; appid: number; amount: number; reason?: string }) =>
      adjustUserExperience(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-app-users"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-app-user"] })
      ]);
    }
  });
}

export function useWorkflowStatisticsQuery(appid?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-statistics", token, appid],
    queryFn: () => getWorkflowStatistics(token as string, appid as number),
    enabled: Boolean(token && appid)
  });
}

export function useWorkflowListQuery(
  appid?: number | null,
  payload?: { status?: string; category?: string; keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-list", token, appid, payload?.status, payload?.category, payload?.keyword, payload?.page, payload?.limit],
    queryFn: () =>
      getWorkflowList(token as string, {
        appid: appid as number,
        status: payload?.status,
        category: payload?.category,
        keyword: payload?.keyword,
        page: payload?.page,
        limit: payload?.limit
      }),
    enabled: Boolean(token && appid)
  });
}

export function useWorkflowDetailQuery(appid?: number | null, workflowId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-detail", token, appid, workflowId],
    queryFn: () => getWorkflowDetail(token as string, { appid: appid as number, workflowId: workflowId as number }),
    enabled: Boolean(token && appid && workflowId)
  });
}

export function useWorkflowInstancesQuery(
  appid?: number | null,
  payload?: { workflowId?: number; status?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-instances", token, appid, payload?.workflowId, payload?.status, payload?.page, payload?.limit],
    queryFn: () =>
      getWorkflowInstances(token as string, {
        appid: appid as number,
        workflowId: payload?.workflowId,
        status: payload?.status,
        page: payload?.page,
        limit: payload?.limit
      }),
    enabled: Boolean(token && appid)
  });
}

export function useWorkflowInstanceDetailQuery(appid?: number | null, instanceId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-instance-detail", token, appid, instanceId],
    queryFn: () => getWorkflowInstanceDetail(token as string, { appid: appid as number, instance_id: instanceId as number }),
    enabled: Boolean(token && appid && instanceId)
  });
}

export function useWorkflowLogsQuery(
  appid?: number | null,
  payload?: { workflowId?: number; instanceId?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-logs", token, appid, payload?.workflowId, payload?.instanceId, payload?.limit],
    queryFn: () =>
      getWorkflowLogs(token as string, {
        appid: appid as number,
        workflowId: payload?.workflowId,
        instanceId: payload?.instanceId,
        limit: payload?.limit
      }),
    enabled: Boolean(token && appid)
  });
}

export function useWorkflowTemplatesQuery(
  appid?: number | null,
  payload?: { category?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-templates", token, appid, payload?.category, payload?.page, payload?.limit],
    queryFn: () =>
      getWorkflowTemplates(token as string, {
        appid: appid as number,
        category: payload?.category,
        page: payload?.page,
        limit: payload?.limit
      }),
    enabled: Boolean(token && appid)
  });
}

export function useWorkflowEngineStatusQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-engine-status", token],
    queryFn: () => getWorkflowEngineStatus(token as string),
    enabled: Boolean(token)
  });
}

export function useWorkflowTasksTodoQuery(
  appid?: number | null,
  payload?: { userId?: number; status?: string; priority?: number; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      "workflow-tasks",
      token,
      appid,
      payload?.userId,
      payload?.status,
      payload?.priority,
      payload?.page,
      payload?.limit
    ],
    queryFn: () =>
      getWorkflowTasksTodo(token as string, {
        appid: appid as number,
        user_id: payload?.userId,
        status: payload?.status,
        priority: payload?.priority,
        page: payload?.page,
        limit: payload?.limit
      }),
    enabled: Boolean(token && appid)
  });
}

export function useWorkflowTaskDetailQuery(appid?: number | null, taskId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-task-detail", token, appid, taskId],
    queryFn: () => getWorkflowTaskDetail(token as string, { appid: appid as number, task_id: taskId as number }),
    enabled: Boolean(token && appid && taskId)
  });
}

export function useWorkflowTaskHistoryQuery(appid?: number | null, taskId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-task-history", token, appid, taskId],
    queryFn: () => getWorkflowTaskHistory(token as string, { appid: appid as number, task_id: taskId as number }),
    enabled: Boolean(token && appid && taskId)
  });
}

export function useWorkflowNodeTypesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["workflow-node-types", token],
    queryFn: () => getWorkflowNodeTypes(token as string),
    enabled: Boolean(token)
  });
}

export function useCreateWorkflowMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createWorkflow>[1]) => createWorkflow(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useUpdateWorkflowMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof updateWorkflow>[1]) => updateWorkflow(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useDeleteWorkflowMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof deleteWorkflow>[1]) => deleteWorkflow(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useStartWorkflowMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof startWorkflow>[1]) => startWorkflow(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function usePauseWorkflowInstanceMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof pauseWorkflowInstance>[1]) =>
      pauseWorkflowInstance(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useResumeWorkflowInstanceMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof resumeWorkflowInstance>[1]) =>
      resumeWorkflowInstance(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useCancelWorkflowInstanceMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof cancelWorkflowInstance>[1]) =>
      cancelWorkflowInstance(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useCreateWorkflowFromTemplateMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createWorkflowFromTemplate>[1]) =>
      createWorkflowFromTemplate(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useSaveWorkflowAsTemplateMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof saveWorkflowAsTemplate>[1]) =>
      saveWorkflowAsTemplate(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useValidateWorkflowDefinitionMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof validateWorkflowDefinition>[1]) =>
      validateWorkflowDefinition(token as string, payload)
  });
}

export function useCompleteWorkflowTaskMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof completeWorkflowTask>[1]) =>
      completeWorkflowTask(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

export function useAssignWorkflowTaskMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof assignWorkflowTask>[1]) =>
      assignWorkflowTask(token as string, payload),
    onSuccess: async () => {
      await invalidateWorkflowQueries(queryClient);
    }
  });
}

// ── 防火墙安全日志 ──────────────────────────

export function useFirewallLogsQuery(params?: FirewallLogListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["firewall-logs", token, params],
    queryFn: () => getFirewallLogs(token as string, params),
    enabled: Boolean(token)
  });
}

export function useFirewallLogDetailQuery(logId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["firewall-log-detail", token, logId],
    queryFn: () => getFirewallLogDetail(token as string, logId as number),
    enabled: Boolean(token && logId)
  });
}

export function useFirewallStatsQuery(params?: Parameters<typeof getFirewallStats>[1]) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["firewall-stats", token, params],
    queryFn: () => getFirewallStats(token as string, params),
    enabled: Boolean(token)
  });
}

export function useDeleteFirewallLogsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (before: string) => deleteFirewallLogs(token as string, before),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["firewall-logs"] }),
        queryClient.invalidateQueries({ queryKey: ["firewall-stats"] })
      ]);
    }
  });
}

// ── IP 封禁管理 ──────────────────────────

export function useIPBansQuery(params?: IPBanListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["ip-bans", token, params],
    queryFn: () => getIPBans(token as string, params),
    enabled: Boolean(token)
  });
}

export function useCreateIPBanMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createIPBan>[1]) => createIPBan(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["ip-bans"] });
    }
  });
}

export function useDeleteIPBanMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (banId: number) => deleteIPBan(token as string, banId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["ip-bans"] });
    }
  });
}

export function useIPBanModesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["ip-ban-modes", token],
    queryFn: () => getIPBanModes(token as string),
    enabled: Boolean(token),
    staleTime: 5 * 60 * 1000 // 模式清单变化极少，缓存 5 分钟
  });
}

// ── 地域封禁 ──────────────────────────

export function useGeoBansQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["geo-bans", token],
    queryFn: () => getGeoBans(token as string),
    enabled: Boolean(token)
  });
}

export function useUpsertGeoBanMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof upsertGeoBan>[1]) => upsertGeoBan(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["geo-bans"] });
    }
  });
}

export function useToggleGeoBanMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => toggleGeoBan(token as string, id, enabled),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["geo-bans"] });
    }
  });
}

export function useDeleteGeoBanMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteGeoBan(token as string, id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["geo-bans"] });
    }
  });
}

// ── 验证码配置 ──

export function useAdminCaptchaConfigQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-captcha-config", token, appKey],
    queryFn: () => getAdminCaptchaConfig(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useUpdateAdminCaptchaConfigMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (config: import("@/lib/api/captcha").CaptchaAppConfig) =>
      updateAdminCaptchaConfig(token as string, appKey as string, config),
    onSuccess: async (data) => {
      queryClient.setQueryData(["admin-captcha-config", token, appKey], data);
      await queryClient.invalidateQueries({ queryKey: ["admin-captcha-config"] });
    }
  });
}

export function useTestAdminCaptchaSMSMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: import("@/lib/api/captcha").AdminTestSMSPayload) =>
      testAdminCaptchaSMS(token as string, appKey as string, payload)
  });
}

export function useGenerateCaptchaMutation() {
  return useMutation({
    mutationFn: (payload: { type: string; purpose: string; appid?: number }) =>
      generateCaptcha(payload)
  });
}

export function useAdminCaptchaPublicConfigQuery() {
  return useQuery({
    queryKey: ["admin-captcha-public-config"],
    queryFn: () => getAdminCaptchaPublicConfig(),
    staleTime: 60_000
  });
}

// ── 抽奖系统 ──────────────────────────

export function useAdminLotteryActivitiesQuery(
  appKey?: string | null,
  params?: { status?: string; keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-lottery-activities", token, appKey, params],
    queryFn: () => getAdminLotteryActivities(token!, appKey!, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminLotteryActivityQuery(appKey?: string | null, id?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-lottery-activity", token, appKey, id],
    queryFn: () => getAdminLotteryActivity(token!, appKey!, id!),
    enabled: Boolean(token && appKey && id)
  });
}

export function useCreateAdminLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminLotteryActivity>[2]) =>
      createAdminLotteryActivity(token!, appKey!, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-lottery-activities"] });
    }
  });
}

export function useUpdateAdminLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { id: number; data: Record<string, unknown> }) =>
      updateAdminLotteryActivity(token!, appKey!, payload.id, payload.data),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-lottery-activities"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-lottery-activity"] })
      ]);
    }
  });
}

export function useDeleteAdminLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteAdminLotteryActivity(token!, appKey!, id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-lottery-activities"] });
    }
  });
}

export function useAdminLotteryPrizesQuery(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-lottery-prizes", token, appKey, activityId],
    queryFn: () => getAdminLotteryPrizes(token!, appKey!, activityId!),
    enabled: Boolean(token && appKey && activityId)
  });
}

export function useCreateAdminLotteryPrizeMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAdminLotteryPrize>[3]) =>
      createAdminLotteryPrize(token!, appKey!, activityId!, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-lottery-prizes"] });
    }
  });
}

export function useUpdateAdminLotteryPrizeMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { prizeId: number; data: Record<string, unknown> }) =>
      updateAdminLotteryPrize(token!, appKey!, activityId!, payload.prizeId, payload.data),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-lottery-prizes"] });
    }
  });
}

export function useDeleteAdminLotteryPrizeMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (prizeId: number) => deleteAdminLotteryPrize(token!, appKey!, activityId!, prizeId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["admin-lottery-prizes"] });
    }
  });
}

export function useCommitLotterySeedMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => commitLotterySeed(token!, appKey!, id),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-lottery-activities"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-lottery-activity"] })
      ]);
    }
  });
}

export function useRevealLotterySeedMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => revealLotterySeed(token!, appKey!, id),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-lottery-activities"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-lottery-activity"] })
      ]);
    }
  });
}

export function useAdminLotteryDrawsQuery(
  appKey?: string | null,
  activityId?: number | null,
  params?: { page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-lottery-draws", token, appKey, activityId, params],
    queryFn: () => getAdminLotteryDraws(token!, appKey!, activityId!, params),
    enabled: Boolean(token && appKey && activityId)
  });
}

export function useAdminLotteryStatsQuery(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-lottery-stats", token, appKey, activityId],
    queryFn: () => getAdminLotteryStats(token!, appKey!, activityId!),
    enabled: Boolean(token && appKey && activityId)
  });
}

// ── 系统公告 ──

export function useSystemAnnouncementsQuery(params?: { status?: string; type?: string; level?: string; page?: number; limit?: number }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["system-announcements", token, params],
    queryFn: () => getSystemAnnouncements(token!, params),
    enabled: Boolean(token)
  });
}

export function useCreateAnnouncementMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: Record<string, unknown>) => createSystemAnnouncement(token!, payload),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["system-announcements"] }); }
  });
}

export function useUpdateAnnouncementMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: { id: number; payload: Record<string, unknown> }) => updateSystemAnnouncement(token!, p.id, p.payload),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["system-announcements"] }); }
  });
}

export function useDeleteAnnouncementMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteSystemAnnouncement(token!, id),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["system-announcements"] }); }
  });
}

export function usePublishAnnouncementMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => publishSystemAnnouncement(token!, id),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["system-announcements"] }); }
  });
}

export function useArchiveAnnouncementMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => archiveSystemAnnouncement(token!, id),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["system-announcements"] }); }
  });
}

// ── 应用级第三方登录（OAuth2）渠道 ──

export function useOAuthTemplatesQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["oauth-templates", token],
    queryFn: () => appOAuth.getOAuthTemplates(token as string),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });
}

export function useAppOAuthProvidersQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-oauth-providers", token, appKey],
    queryFn: () => appOAuth.listAppOAuthProviders(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 新建与更新共用：payload 无 id 概念，靠 provider slug 定位 */
export function useSaveAppOAuthProviderMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ mode, payload }: { mode: "create" | "update"; payload: appOAuth.AppOAuthProviderPayload }) =>
      mode === "create"
        ? appOAuth.createAppOAuthProvider(token as string, appKey as string, payload)
        : appOAuth.updateAppOAuthProvider(token as string, appKey as string, payload.provider, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["app-oauth-providers"] });
    }
  });
}

export function useToggleAppOAuthProviderMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ provider, enabled }: { provider: string; enabled: boolean }) =>
      appOAuth.setAppOAuthProviderEnabled(token as string, appKey as string, provider, enabled),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["app-oauth-providers"] });
    }
  });
}

export function useDeleteAppOAuthProviderMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (provider: string) =>
      appOAuth.deleteAppOAuthProvider(token as string, appKey as string, provider),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["app-oauth-providers"] });
    }
  });
}

export function useReorderAppOAuthProvidersMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (providers: string[]) =>
      appOAuth.reorderAppOAuthProviders(token as string, appKey as string, providers),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["app-oauth-providers"] });
    }
  });
}

export function useTestAppOAuthProviderMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (provider: string) =>
      appOAuth.testAppOAuthProvider(token as string, appKey as string, provider)
  });
}

export function useAppOAuthBindingsQuery(
  appKey?: string | null,
  params?: { provider?: string; userId?: number; keyword?: string; page?: number; pageSize?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["app-oauth-bindings", token, appKey, params],
    queryFn: () => appOAuth.listAppOAuthBindings(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useDeleteAppOAuthBindingMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { userId: number; provider: string; force?: boolean }) =>
      appOAuth.deleteAppOAuthBinding(token as string, appKey as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["app-oauth-bindings"] });
      await queryClient.invalidateQueries({ queryKey: ["app-oauth-providers"] });
    }
  });
}

// ── 数据库生命周期与泄漏监控（仅超级管理员） ──

/** 快照走服务端内存缓存，可安全地按 refetchInterval 轮询 */
export function useDatabaseSnapshotQuery(options?: { refetchInterval?: number; enabled?: boolean }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["database-snapshot", token],
    queryFn: () => database.getDatabaseSnapshot(token as string),
    enabled: Boolean(token) && options?.enabled !== false,
    refetchInterval: options?.refetchInterval
  });
}

export function useRefreshDatabaseMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => database.refreshDatabaseSnapshot(token as string),
    onSuccess: (data) => {
      queryClient.setQueryData(["database-snapshot", token], data);
    }
  });
}

export function useDatabaseHistoryQuery(range?: string, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["database-history", token, range],
    queryFn: () => database.getDatabaseHistory(token as string, range),
    enabled: Boolean(token) && enabled
  });
}

/** 会话列表会真正查库，默认不自动轮询 */
export function useDatabaseSessionsQuery(
  params?: { onlyProblematic?: boolean; limit?: number },
  enabled = true
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["database-sessions", token, params],
    queryFn: () => database.getDatabaseSessions(token as string, params),
    enabled: Boolean(token) && enabled
  });
}

export function useTerminateDatabaseSessionMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ pid, mode }: { pid: number; mode: "terminate" | "cancel" }) =>
      mode === "terminate"
        ? database.terminateDatabaseSession(token as string, pid)
        : database.cancelDatabaseSession(token as string, pid),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["database-sessions"] });
      await queryClient.invalidateQueries({ queryKey: ["database-snapshot"] });
    }
  });
}

export function useDatabaseMaintenanceQuery(limit?: number, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["database-maintenance", token, limit],
    queryFn: () => database.getDatabaseMaintenance(token as string, limit),
    enabled: Boolean(token) && enabled
  });
}

export function useWarmupDatabaseMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => database.warmupDatabasePool(token as string),
    onSuccess: (data) => {
      queryClient.setQueryData(["database-snapshot", token], data);
    }
  });
}
