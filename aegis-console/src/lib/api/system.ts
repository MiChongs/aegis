import { apiRequest, buildQuery } from "./client";
import type { CaptchaDynamicConfig } from "./captcha";
import type { AdminLoginResult, BrandingConfig, FirewallLogEntry, FirewallLogPage, FirewallStats, GeoBanEntry, GeoBanScope, IPBanEntry, IPBanMode, IPBanModesResponse, IPBanPage, LDAPTestResult, OIDCTestResult, SAMLTestResult, SystemSettings } from "./types";

export function getAdminSystemSettings(token: string) {
  return apiRequest<SystemSettings>("/api/admin/system/settings", { token });
}

export function updateAdminSystemSettings(
  token: string,
  payload: {
    firewall?: {
      enabled?: boolean;
      globalRate?: string;
      authRate?: string;
      adminRate?: string;
      corazaEnabled?: boolean;
      corazaParanoia?: number;
      requestBodyLimit?: number;
      requestBodyMemory?: number;
      allowedCIDRs?: string[];
      blockedCIDRs?: string[];
      blockedUserAgents?: string[];
      blockedPathPrefix?: string[];
      maxPathLength?: number;
      maxQueryLength?: number;
      defaultBanMode?: string;
      tarpitDelayMs?: number;
    };
    security?: {
      challengeTTLSeconds?: number;
      modules?: {
        totpEnabled?: boolean;
        recoveryCodesEnabled?: boolean;
        passkeyEnabled?: boolean;
      };
      totp?: {
        enabled?: boolean;
        issuer?: string;
        enrollmentTTLSeconds?: number;
        skew?: number;
        digits?: number;
      };
      recoveryCodes?: {
        enabled?: boolean;
        count?: number;
        length?: number;
      };
      passkey?: {
        enabled?: boolean;
        rpDisplayName?: string;
        rpId?: string;
        rpOrigins?: string[];
        rpTopOrigins?: string[];
        challengeTTLSeconds?: number;
        userVerification?: string;
      };
    };
    adminCaptcha?: {
      enabled?: boolean;
      type?: string;
      requireForLogin?: boolean;
      requireForRegister?: boolean;
      audioLang?: string;
      dynamic?: CaptchaDynamicConfig;
    };
    ldap?: {
      enabled?: boolean;
      server?: string;
      port?: number;
      useTLS?: boolean;
      useStartTLS?: boolean;
      skipTLSVerify?: boolean;
      bindDN?: string;
      bindPassword?: string;
      baseDN?: string;
      userFilter?: string;
      userAttribute?: string;
      groupBaseDN?: string;
      groupFilter?: string;
      groupAttribute?: string;
      adminGroupDN?: string;
      attrMapping?: { account?: string; displayName?: string; email?: string; phone?: string };
      connectionTimeoutSeconds?: number;
      searchTimeoutSeconds?: number;
      fallbackToLocal?: boolean;
    };
    oidc?: {
      enabled?: boolean;
      issuerURL?: string;
      clientID?: string;
      clientSecret?: string;
      redirectURL?: string;
      scopes?: string[];
      allowedDomains?: string[];
      adminGroupClaim?: string;
      adminGroupValue?: string;
      attrMapping?: { account?: string; displayName?: string; email?: string; phone?: string };
      fallbackToLocal?: boolean;
      frontendCallbackURL?: string;
    };
    saml?: {
      enabled?: boolean;
      idpMetadataURL?: string;
      idpMetadataXML?: string;
      entityID?: string;
      metadataURL?: string;
      acsURL?: string;
      spCertificate?: string;
      spPrivateKey?: string;
      nameIDFormat?: string;
      signAuthnRequests?: boolean;
      forceAuthn?: boolean;
      allowIdpInitiated?: boolean;
      allowedDomains?: string[];
      adminGroupAttribute?: string;
      adminGroupValue?: string;
      attrMapping?: { account?: string; displayName?: string; email?: string; phone?: string; groups?: string };
      fallbackToLocal?: boolean;
      frontendCallbackURL?: string;
    };
    branding?: {
      platformName?: string;
      consoleName?: string;
      logoURL?: string;
      logoDarkURL?: string;
      faviconURL?: string;
      primaryColor?: string;
      primaryColorDark?: string;
      accentColor?: string;
      loginBgURL?: string;
      loginBgColor?: string;
      footerText?: string;
      customCSS?: string;
    };
  }
) {
  return apiRequest<SystemSettings>("/api/admin/system/settings", {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 防火墙安全日志 ──────────────────────────

export type FirewallLogListParams = {
  page?: number;
  pageSize?: number;
  startTime?: string;
  endTime?: string;
  ip?: string;
  country?: string;
  reason?: string;
  wafRuleId?: number;
  pathPattern?: string;
  severity?: string;
  sortBy?: string;
  sortOrder?: string;
};

export function getFirewallLogs(token: string, params?: FirewallLogListParams) {
  return apiRequest<FirewallLogPage>(
    `/api/admin/system/firewall/logs${buildQuery(params ?? {})}`,
    { token }
  );
}

export function getFirewallLogDetail(token: string, logId: number) {
  return apiRequest<FirewallLogEntry>(`/api/admin/system/firewall/logs/${logId}`, { token });
}

export function getFirewallStats(
  token: string,
  params?: { startTime?: string; endTime?: string; granularity?: string }
) {
  return apiRequest<FirewallStats>(
    `/api/admin/system/firewall/stats${buildQuery(params ?? {})}`,
    { token }
  );
}

export function deleteFirewallLogs(token: string, before: string) {
  return apiRequest<{ deleted: number; before: string }>("/api/admin/system/firewall/logs", {
    method: "DELETE",
    token,
    body: JSON.stringify({ before })
  });
}

// ── IP 封禁管理 ──────────────────────────

export type IPBanListParams = {
  page?: number;
  pageSize?: number;
  ip?: string;
  status?: string;
  source?: string;
};

export function getIPBans(token: string, params?: IPBanListParams) {
  return apiRequest<IPBanPage>(`/api/admin/system/firewall/bans${buildQuery(params ?? {})}`, { token });
}

export function createIPBan(token: string, payload: { ip: string; reason?: string; duration?: number; mode?: IPBanMode; delayMs?: number }) {
  return apiRequest<IPBanEntry>("/api/admin/system/firewall/bans", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getIPBanModes(token: string) {
  return apiRequest<IPBanModesResponse>("/api/admin/system/firewall/bans/modes", { token });
}

export function deleteIPBan(token: string, banId: number) {
  return apiRequest<{ banId: number; revokedAt: string }>(`/api/admin/system/firewall/bans/${banId}`, {
    method: "DELETE",
    token
  });
}

// ── 地域封禁 ──────────────────────────

export function getGeoBans(token: string) {
  return apiRequest<GeoBanEntry[]>("/api/admin/system/firewall/geo-bans", { token });
}

export function upsertGeoBan(
  token: string,
  payload: { scopeType: GeoBanScope; scopeValue: string; mode?: IPBanMode | ""; reason?: string; enabled?: boolean; expiresAt?: string }
) {
  return apiRequest<GeoBanEntry>("/api/admin/system/firewall/geo-bans", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function toggleGeoBan(token: string, id: number, enabled: boolean) {
  return apiRequest<{ id: number; enabled: boolean }>(`/api/admin/system/firewall/geo-bans/${id}`, {
    method: "PATCH",
    token,
    body: JSON.stringify({ enabled })
  });
}

export function deleteGeoBan(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/system/firewall/geo-bans/${id}`, {
    method: "DELETE",
    token
  });
}

// ── 崩溃日志管理 ──────────────────────────

export type CrashLogFile = {
  name: string;
  size: number;
  modTime: string;
};

export function getCrashLogs(token: string) {
  return apiRequest<CrashLogFile[]>("/api/admin/system/crashlogs", { token });
}

export function getCrashLogContent(token: string, filename: string) {
  return fetch(`${process.env.NEXT_PUBLIC_API_BASE_URL || ""}/api/admin/system/crashlogs/${filename}`, {
    headers: { Authorization: `Bearer ${token}` }
  }).then(r => {
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    return r.text();
  });
}

export function deleteCrashLog(token: string, filename: string) {
  return apiRequest<null>(`/api/admin/system/crashlogs/${filename}`, {
    method: "DELETE",
    token
  });
}

// ── 系统运行时信息 ──────────────────────────

export type SystemRuntimeInfo = {
  appName: string;
  environment: string;
  port: number;
  checkedAt: string;
  startedAt: string;
  uptimeSeconds: number;
  timezone: string;
  goVersion: string;
  goOS: string;
  goArch: string;
  goroutines: number;
  cgoCalls: number;
  memAlloc: number;
  memTotalAlloc: number;
  memSys: number;
  numGC: number;
  lastGCTime: number;
  hostname: string;
  os: string;
  platform: string;
  platformVersion: string;
  kernelArch: string;
  kernelVersion: string;
  cpuModel: string;
  cpuCores: number;
  cpuThreads: number;
  cpuUsage: number;
  memTotal: number;
  memUsed: number;
  memFree: number;
  memUsedPercent: number;
  diskTotal: number;
  diskUsed: number;
  diskFree: number;
  diskUsedPercent: number;
  pid: number;
  processMemory: number;
};

export function getSystemRuntimeInfo(token: string) {
  return apiRequest<SystemRuntimeInfo>("/api/admin/system/runtime", { token });
}

// ── 内存管理 ──────────────────────────

export type MemoryMetrics = {
  timestamp: string;
  heapAlloc: number;
  heapSys: number;
  heapInuse: number;
  heapIdle: number;
  heapObjects: number;
  stackInuse: number;
  gcSys: number;
  totalAlloc: number;
  sys: number;
  numGC: number;
  lastGCNano: number;
  pauseTotalNs: number;
  goroutines: number;
  processRSS: number;
};

export type GCTunerSnapshot = {
  gogc: number;
  memoryLimitMB: number;
  memoryLimitRaw: number;
  tuneCount: number;
  lastTuneAt?: string;
};

export type PoolStats = {
  name: string;
  gets: number;
  puts: number;
  hits: number;
  misses: number;
  hitRate: number;
};

export type CacheStats = {
  maxEntries: number;
  size: number;
  hits: number;
  misses: number;
  evictions: number;
  expired: number;
  hitRate: number;
};

export type MemoryAlert = {
  time: string;
  level: string;
  message: string;
  value: number;
};

export type LeakIndicator = {
  name: string;
  trending: string;
  samples: number;
  consecutiveUp: number;
  latestValue: number;
  deltaPercent: number;
  suspectedLeak: boolean;
  alertMessage?: string;
  lastCheckedAt: string;
};

export type LeakReport = {
  checkedAt: string;
  suspicious: boolean;
  indicators: LeakIndicator[];
  summary: string;
};

export type MemorySnapshot = {
  timestamp: string;
  gcTuner?: GCTunerSnapshot;
  metrics?: MemoryMetrics;
  pools: PoolStats[];
  cache: CacheStats;
  leakReport?: LeakReport;
  alerts: MemoryAlert[];
  config: {
    gcAutoTune: boolean;
    memoryLimitMB: number;
    monitorInterval: string;
    gcTuneInterval: string;
    leakDetection: boolean;
    leakWindow: number;
    cacheMaxEntries: number;
    cacheTTL: string;
  };
};

export function getMemorySnapshot(token: string) {
  return apiRequest<MemorySnapshot>("/api/admin/system/memory/snapshot", { token });
}

export function forceGC(token: string) {
  return apiRequest<MemorySnapshot>("/api/admin/system/memory/gc", { method: "POST", token });
}

export function setGOGC(token: string, value: number) {
  return apiRequest<{ old: number; new: number }>("/api/admin/system/memory/gogc", {
    method: "PUT",
    token,
    body: JSON.stringify({ value }),
  });
}

export function getMemoryHistory(token: string, range_?: string) {
  return apiRequest<MemoryMetrics[]>(
    `/api/admin/system/memory/history${range_ ? `?range=${range_}` : ""}`,
    { token }
  );
}

export function getMemoryPoolStats(token: string) {
  return apiRequest<PoolStats[]>("/api/admin/system/memory/pools", { token });
}

export function getMemoryCacheStats(token: string) {
  return apiRequest<CacheStats>("/api/admin/system/memory/cache", { token });
}

export function flushMemoryCache(token: string) {
  return apiRequest<CacheStats>("/api/admin/system/memory/cache", { method: "DELETE", token });
}

export function getMemoryLeakReport(token: string) {
  return apiRequest<LeakReport>("/api/admin/system/memory/leak", { token });
}

// ── 系统公告 ──────────────────────────

import type { SystemAnnouncement, SystemAnnouncementListResult } from "./types";

export function getSystemAnnouncements(
  token: string,
  params?: { status?: string; type?: string; level?: string; page?: number; limit?: number }
) {
  return apiRequest<SystemAnnouncementListResult>(
    `/api/admin/system/announcements${buildQuery(params ?? {})}`,
    { token }
  );
}

export function getSystemAnnouncementDetail(token: string, id: number) {
  return apiRequest<SystemAnnouncement>(`/api/admin/system/announcements/${id}`, { token });
}

export function createSystemAnnouncement(token: string, payload: Record<string, unknown>) {
  return apiRequest<SystemAnnouncement>("/api/admin/system/announcements", {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

export function updateSystemAnnouncement(token: string, id: number, payload: Record<string, unknown>) {
  return apiRequest<SystemAnnouncement>(`/api/admin/system/announcements/${id}`, {
    method: "PUT", token, body: JSON.stringify(payload),
  });
}

export function deleteSystemAnnouncement(token: string, id: number) {
  return apiRequest<null>(`/api/admin/system/announcements/${id}`, { method: "DELETE", token });
}

export function publishSystemAnnouncement(token: string, id: number) {
  return apiRequest<SystemAnnouncement>(`/api/admin/system/announcements/${id}/publish`, {
    method: "POST", token,
  });
}

export function archiveSystemAnnouncement(token: string, id: number) {
  return apiRequest<SystemAnnouncement>(`/api/admin/system/announcements/${id}/archive`, {
    method: "POST", token,
  });
}

export function getActiveAnnouncements(token?: string) {
  return apiRequest<SystemAnnouncement[]>("/api/system/announcements/active", { token: token || undefined });
}

// ── LDAP 连接测试 ──────────────────────────

// ── LDAP ──────────────────────────

export function getLDAPPublicConfig() {
  return apiRequest<{ enabled: boolean }>("/api/admin/auth/ldap/config");
}

// ── OIDC ──────────────────────────

export function getOIDCPublicConfig() {
  return apiRequest<{ enabled: boolean }>("/api/admin/auth/oidc/config");
}

export function getOIDCAuthURL() {
  return apiRequest<{ url: string; state: string }>("/api/admin/auth/oidc/authorize");
}

export function exchangeOIDCTicket(ticket: string) {
  return apiRequest<AdminLoginResult>("/api/admin/auth/oidc/exchange", {
    method: "POST",
    body: JSON.stringify({ ticket }),
  });
}

export function testOIDCDiscovery(token: string, payload: { issuerURL: string }) {
  return apiRequest<OIDCTestResult>("/api/admin/system/oidc/test", {
    method: "POST", token, body: JSON.stringify(payload),
  });
}

// ── LDAP 连接测试 ──────────────────────────

export function testLDAPConnection(
  token: string,
  payload: {
    server: string;
    port?: number;
    useTLS?: boolean;
    useStartTLS?: boolean;
    skipTLSVerify?: boolean;
    bindDN: string;
    bindPassword: string;
    baseDN: string;
    userFilter?: string;
    testAccount?: string;
    connectionTimeoutSeconds?: number;
  }
) {
  return apiRequest<LDAPTestResult>("/api/admin/system/ldap/test", {
    method: "POST",
    token,
    body: JSON.stringify(payload),
  });
}

// ── SAML ──────────────────────────

export function getSAMLPublicConfig() {
  return apiRequest<{ enabled: boolean }>("/api/admin/auth/saml/config");
}

export function getSAMLAuthURL() {
  return apiRequest<{ url: string; state: string }>("/api/admin/auth/saml/authorize");
}

export function exchangeSAMLTicket(ticket: string) {
  return apiRequest<AdminLoginResult>("/api/admin/auth/saml/exchange", {
    method: "POST",
    body: JSON.stringify({ ticket }),
  });
}

/** 解析 IdP 元数据（URL 或直接粘贴的 XML），返回 SSO 端点与证书数量 */
export function testSAMLMetadata(token: string, payload: { idpMetadataURL?: string; idpMetadataXML?: string }) {
  return apiRequest<SAMLTestResult>("/api/admin/system/saml/test", {
    method: "POST",
    token,
    body: JSON.stringify(payload),
  });
}

// ── 品牌配置 ──────────────────────────

export function getPublicBranding() {
  return apiRequest<BrandingConfig>("/api/public/branding");
}
