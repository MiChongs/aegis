import { apiRequest, buildQuery } from "./client";

/**
 * 应用级第三方登录（OAuth2）渠道配置。
 *
 * - 每个 App 独立维护渠道列表；未做应用级配置时回落到平台级 .env（source=platform）；
 * - ClientSecret 只写不读：读接口只返回 clientSecretSet / clientSecretHint；
 * - 保存时 clientSecret 留空 = 保持原密钥不变，clearClientSecret=true = 显式清空。
 */

/** 协议适配器：决定 token 交换与用户信息解析的差异 */
export type OAuthKind = "generic" | "qq" | "wechat" | "weibo" | "github" | "microsoft";

/** token 端点凭据传递方式 */
export type TokenAuthStyle = "auto" | "params" | "basic";

/** 用户信息端点凭据传递方式 */
export type UserInfoAuthStyle = "header" | "query";

/** 配置来源：应用级 / 平台级兜底 */
export type OAuthSource = "app" | "platform";

export type AppOAuthProvider = {
  id: number;
  appId: number;
  provider: string;
  kind: OAuthKind;
  displayName: string;
  icon?: string;
  color?: string;
  enabled: boolean;
  clientId: string;
  clientSecretSet: boolean;
  clientSecretHint?: string;
  redirectUrl: string;
  authUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  scopes: string[];
  tokenAuthStyle: TokenAuthStyle;
  userInfoAuthStyle: UserInfoAuthStyle;
  profileMapping: Record<string, string>;
  extraAuthParams: Record<string, string>;
  allowLogin: boolean;
  allowRegister: boolean;
  allowBind: boolean;
  sortOrder: number;
  remark?: string;
  createdAt: string;
  updatedAt: string;
  /** 以下为服务端计算字段 */
  source: OAuthSource;
  bindings: number;
  ready: boolean;
  issues?: string[];
  warnings?: string[];
};

export type AppOAuthProviderPayload = {
  provider: string;
  kind?: OAuthKind;
  displayName?: string;
  icon?: string;
  color?: string;
  enabled?: boolean;
  clientId?: string;
  /** 留空表示保持原密钥不变 */
  clientSecret?: string;
  clearClientSecret?: boolean;
  redirectUrl?: string;
  authUrl?: string;
  tokenUrl?: string;
  userInfoUrl?: string;
  scopes?: string[];
  tokenAuthStyle?: TokenAuthStyle;
  userInfoAuthStyle?: UserInfoAuthStyle;
  profileMapping?: Record<string, string>;
  extraAuthParams?: Record<string, string>;
  allowLogin?: boolean;
  allowRegister?: boolean;
  allowBind?: boolean;
  sortOrder?: number;
  remark?: string;
};

export type OAuthTemplateField = {
  key: string;
  label: string;
  placeholder?: string;
  hint?: string;
};

export type OAuthTemplate = {
  key: string;
  kind: OAuthKind;
  name: string;
  icon: string;
  color: string;
  category: string;
  description: string;
  docsUrl?: string;
  consoleUrl?: string;
  authUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  scopes: string[];
  tokenAuthStyle?: TokenAuthStyle;
  userInfoAuthStyle?: UserInfoAuthStyle;
  /** 端点需人工填写（自定义 / OIDC） */
  requiresEndpoints: boolean;
  fields?: OAuthTemplateField[];
  notes?: string[];
};

export type OAuthEndpointProbe = {
  name: string;
  url: string;
  reachable: boolean;
  status?: number;
  latencyMs?: number;
  message?: string;
};

export type OAuthTestResult = {
  provider: string;
  ready: boolean;
  issues?: string[];
  warnings?: string[];
  authorizeUrl?: string;
  endpoints: OAuthEndpointProbe[];
  checkedAt: string;
};

export type OAuthBinding = {
  id: number;
  appId: number;
  userId: number;
  account?: string;
  provider: string;
  displayName?: string;
  icon?: string;
  providerUserId: string;
  unionId?: string;
  nickname?: string;
  avatar?: string;
  email?: string;
  createdAt: string;
  updatedAt: string;
};

export type OAuthBindingPage = {
  items: OAuthBinding[];
  total: number;
  page: number;
  pageSize: number;
};

export type AppOAuthProviderList = {
  items: AppOAuthProvider[];
  /** 回调地址前缀，拼上 provider 即为服务商后台要登记的地址 */
  callbackUrlPrefix: string;
};

export type OAuthTemplateList = {
  items: OAuthTemplate[];
  callbackUrlPrefix: string;
};

const appPath = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;

export function getOAuthTemplates(token: string) {
  return apiRequest<OAuthTemplateList>("/api/admin/oauth-providers/templates", { token });
}

export function listAppOAuthProviders(token: string, appKey: string) {
  return apiRequest<AppOAuthProviderList>(`${appPath(appKey)}/oauth-providers`, { token });
}

export function createAppOAuthProvider(token: string, appKey: string, payload: AppOAuthProviderPayload) {
  return apiRequest<AppOAuthProvider>(`${appPath(appKey)}/oauth-providers`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAppOAuthProvider(
  token: string,
  appKey: string,
  provider: string,
  payload: AppOAuthProviderPayload
) {
  return apiRequest<AppOAuthProvider>(
    `${appPath(appKey)}/oauth-providers/${encodeURIComponent(provider)}`,
    { method: "PUT", token, body: JSON.stringify(payload) }
  );
}

export function setAppOAuthProviderEnabled(
  token: string,
  appKey: string,
  provider: string,
  enabled: boolean
) {
  return apiRequest<AppOAuthProvider>(
    `${appPath(appKey)}/oauth-providers/${encodeURIComponent(provider)}/enabled`,
    { method: "PUT", token, body: JSON.stringify({ enabled }) }
  );
}

export function deleteAppOAuthProvider(token: string, appKey: string, provider: string) {
  return apiRequest<{ deleted: boolean }>(
    `${appPath(appKey)}/oauth-providers/${encodeURIComponent(provider)}`,
    { method: "DELETE", token }
  );
}

export function reorderAppOAuthProviders(token: string, appKey: string, providers: string[]) {
  return apiRequest<{ items: AppOAuthProvider[] }>(`${appPath(appKey)}/oauth-providers/reorder`, {
    method: "POST",
    token,
    body: JSON.stringify({ providers })
  });
}

export function testAppOAuthProvider(token: string, appKey: string, provider: string) {
  return apiRequest<OAuthTestResult>(
    `${appPath(appKey)}/oauth-providers/${encodeURIComponent(provider)}/test`,
    { method: "POST", token }
  );
}

export function listAppOAuthBindings(
  token: string,
  appKey: string,
  params?: { provider?: string; userId?: number; keyword?: string; page?: number; pageSize?: number }
) {
  return apiRequest<OAuthBindingPage>(
    `${appPath(appKey)}/oauth-bindings${buildQuery({ ...params })}`,
    { token }
  );
}

export function deleteAppOAuthBinding(
  token: string,
  appKey: string,
  payload: { userId: number; provider: string; force?: boolean }
) {
  return apiRequest<{ unbound: boolean }>(`${appPath(appKey)}/oauth-bindings`, {
    method: "DELETE",
    token,
    body: JSON.stringify(payload)
  });
}
