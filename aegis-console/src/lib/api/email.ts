import { apiRequest, buildQuery } from "./client";
import type {
  EmailChannelResolution,
  EmailConfig,
  EmailDeliveryPage,
  EmailDeliveryStats,
  EmailProviderCatalog
} from "./types";

/**
 * 邮件通道 API。两个作用域，一份能力：
 *
 * | 作用域 | 归属页面 | 服务的信 |
 * |---|---|---|
 * | 平台级（appid=0） | `/configuration?tab=email` | 管理员通知、平台告警；可显式共享给应用兜底 |
 * | 应用级 | `/apps/{appKey}?tab=email` | 该应用用户的验证码 / 重置 / 欢迎信 / 凭证 |
 *
 * 两边走的是**同一批后端服务方法**，只差一个 appid，因此这里也只有一层薄封装：
 * 平台级用 RESTful 路由，应用级沿用 `/api/admin/app/email-config/*` 的动词式旧命名空间
 * （那条命名空间已经发出去了，改路径等于让存量集成失效）。
 */

// ── 服务商目录（两个作用域共用） ──

/**
 * 拉取全部邮件服务商的自述（含配置字段 schema）。
 *
 * 服务商卡片与配置表单完全由该结果驱动 —— 后端新增一家服务商时，
 * 控制台不需要改任何一行代码。与支付渠道的 `getAdminPaymentMethods` 同一套做法。
 */
export function getEmailProviderCatalog(token: string) {
  return apiRequest<EmailProviderCatalog>("/api/admin/system/email/providers", { token });
}

// ── 平台级 ──

export function getPlatformEmailConfigs(token: string) {
  return apiRequest<EmailConfig[]>("/api/admin/system/email/configs", { token });
}

export function getPlatformEmailConfig(token: string, configId: number) {
  return apiRequest<EmailConfig>(`/api/admin/system/email/configs/${configId}`, { token });
}

export type PlatformEmailConfigPayload = {
  config_name?: string;
  provider?: string;
  enabled?: boolean;
  is_default?: boolean;
  shared?: boolean;
  description?: string;
  settings?: Record<string, string>;
  /** 密钥明文。空值会被后端忽略（留空即不修改），因此调用方不必自己过滤。 */
  secrets?: Record<string, string>;
  clear_secrets?: string[];
  /** 换服务商时置 true：整体替换而不是逐键合并，避免上一家的字段残留。 */
  replace_settings?: boolean;
};

export function createPlatformEmailConfig(token: string, payload: PlatformEmailConfigPayload) {
  return apiRequest<EmailConfig>("/api/admin/system/email/configs", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updatePlatformEmailConfig(
  token: string,
  configId: number,
  payload: PlatformEmailConfigPayload
) {
  return apiRequest<EmailConfig>(`/api/admin/system/email/configs/${configId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deletePlatformEmailConfig(token: string, configId: number) {
  return apiRequest<null>(`/api/admin/system/email/configs/${configId}`, {
    method: "DELETE",
    token
  });
}

export function testPlatformEmailConfig(token: string, configId: number, testEmail: string) {
  return apiRequest<Record<string, unknown>>(`/api/admin/system/email/configs/${configId}/test`, {
    method: "POST",
    token,
    body: JSON.stringify({ test_email: testEmail })
  });
}

export function getPlatformEmailDeliveries(
  token: string,
  params: {
    configId?: number;
    status?: string;
    provider?: string;
    purpose?: string;
    keyword?: string;
    page?: number;
    pageSize?: number;
  } = {}
) {
  return apiRequest<EmailDeliveryPage>(
    `/api/admin/system/email/deliveries${buildQuery(params)}`,
    { token }
  );
}

export function getPlatformEmailStats(token: string) {
  return apiRequest<EmailDeliveryStats>("/api/admin/system/email/stats", { token });
}

export function getPlatformEmailChannel(token: string) {
  return apiRequest<EmailChannelResolution>("/api/admin/system/email/channel", { token });
}

// ── 应用级 ──

export function getAppEmailConfigs(token: string, appid: number) {
  return apiRequest<EmailConfig[]>("/api/admin/app/email-config/list", {
    method: "POST",
    token,
    body: JSON.stringify({ appid })
  });
}

export type AppEmailConfigPayload = PlatformEmailConfigPayload & {
  appid: number;
  config_id?: number;
};

export function createAppEmailConfig(token: string, payload: AppEmailConfigPayload) {
  return apiRequest<EmailConfig>("/api/admin/app/email-config/create", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAppEmailConfig(token: string, payload: AppEmailConfigPayload) {
  return apiRequest<EmailConfig>("/api/admin/app/email-config/update", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAppEmailConfig(token: string, payload: { appid: number; config_id: number }) {
  return apiRequest<null>("/api/admin/app/email-config/delete", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function testAppEmailConfig(
  token: string,
  payload: { appid: number; config_id: number; test_email: string }
) {
  return apiRequest<Record<string, unknown>>("/api/admin/app/email-config/test", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAppEmailDeliveries(
  token: string,
  payload: {
    appid: number;
    config_id?: number;
    status?: string;
    provider?: string;
    purpose?: string;
    keyword?: string;
    page?: number;
    pageSize?: number;
  }
) {
  return apiRequest<EmailDeliveryPage>("/api/admin/app/email-config/deliveries", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAppEmailStats(token: string, appid: number) {
  return apiRequest<EmailDeliveryStats>("/api/admin/app/email-config/stats", {
    method: "POST",
    token,
    body: JSON.stringify({ appid })
  });
}

/**
 * 当前生效的通道。
 *
 * 应用没有自己的配置、正在借用平台共享通道时 `inherited` 为 true —— 界面必须说出这件事，
 * 否则管理员会对着一个空的邮件配置页纳闷验证码是怎么发出去的。
 * 后端在「一条通道都没有」时返回 404，因此调用方要把它当成正常状态处理，而不是错误。
 */
export function getAppEmailChannel(token: string, appid: number) {
  return apiRequest<EmailChannelResolution>("/api/admin/app/email-config/channel", {
    method: "POST",
    token,
    body: JSON.stringify({ appid })
  });
}
