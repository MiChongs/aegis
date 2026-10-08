import { apiRequest, buildQuery } from "./client";

/**
 * 对外 OAuth2 / OIDC 授权服务器（Ory Hydra）。
 *
 * 与 `app-oauth.ts`（本应用去接 QQ、微信等第三方登录）方向相反：这里管理的是
 * 「别的网站与应用使用本应用的账号登录」时用的客户端。客户端存放在 Hydra，
 * 后端按应用绑定（owner=aegis-app:<id>），控制台只看得到本应用的客户端。
 * 契约见 docs/oauth2-provider.md。
 */

export type OAuth2ScopeView = {
  scope: string;
  title: string;
  description: string;
  required: boolean;
};

export type OAuth2ServerOverview = {
  enabled: boolean;
  /** Hydra 管理端可达 */
  ready: boolean;
  readyError?: string;
  issuer?: string;
  endpoints?: Record<string, string>;
  scopes: OAuth2ScopeView[];
  grantTypes: string[];
  clientCount: number;
  subjectClaim: string;
};

export type OAuth2TokenAuthMethod = "client_secret_basic" | "client_secret_post" | "none";
export type OAuth2SubjectType = "public" | "pairwise";
export type OAuth2AccessTokenStrategy = "" | "opaque" | "jwt";

export type OAuth2Client = {
  clientId: string;
  name: string;
  /** 只在创建与重新生成时出现一次 */
  clientSecret?: string;
  public: boolean;
  redirectUris: string[];
  postLogoutRedirectUris: string[];
  grantTypes: string[];
  scopes: string[];
  audience: string[];
  tokenEndpointAuthMethod: OAuth2TokenAuthMethod;
  logoUri?: string;
  clientUri?: string;
  policyUri?: string;
  tosUri?: string;
  allowedCorsOrigins: string[];
  skipConsent: boolean;
  skipLogoutConsent: boolean;
  subjectType: OAuth2SubjectType;
  accessTokenStrategy?: OAuth2AccessTokenStrategy;
  frontchannelLogoutUri?: string;
  backchannelLogoutUri?: string;
  createdAt: string;
  updatedAt: string;
};

export type OAuth2ClientPayload = {
  name: string;
  redirectUris: string[];
  postLogoutRedirectUris: string[];
  grantTypes: string[];
  scopes: string[];
  audience: string[];
  tokenEndpointAuthMethod: OAuth2TokenAuthMethod;
  logoUri: string;
  clientUri: string;
  policyUri: string;
  tosUri: string;
  allowedCorsOrigins: string[];
  skipConsent: boolean;
  skipLogoutConsent: boolean;
  subjectType: OAuth2SubjectType;
  accessTokenStrategy: OAuth2AccessTokenStrategy;
  frontchannelLogoutUri: string;
  backchannelLogoutUri: string;
};

export type OAuth2Grant = {
  clientId: string;
  clientName: string;
  logoUri?: string;
  clientUri?: string;
  scopes: string[];
  grantedAt: string;
};

const appPath = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;

export function getOAuth2Overview(token: string, appKey: string) {
  return apiRequest<OAuth2ServerOverview>(`${appPath(appKey)}/oauth2/overview`, { token });
}

export function listOAuth2Clients(token: string, appKey: string) {
  return apiRequest<{ items: OAuth2Client[] }>(`${appPath(appKey)}/oauth2/clients`, { token });
}

export function createOAuth2Client(token: string, appKey: string, payload: OAuth2ClientPayload) {
  return apiRequest<OAuth2Client>(`${appPath(appKey)}/oauth2/clients`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateOAuth2Client(token: string, appKey: string, clientId: string, payload: OAuth2ClientPayload) {
  return apiRequest<OAuth2Client>(`${appPath(appKey)}/oauth2/clients/${encodeURIComponent(clientId)}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function rotateOAuth2ClientSecret(token: string, appKey: string, clientId: string) {
  return apiRequest<OAuth2Client>(
    `${appPath(appKey)}/oauth2/clients/${encodeURIComponent(clientId)}/rotate-secret`,
    { method: "POST", token }
  );
}

export function deleteOAuth2Client(token: string, appKey: string, clientId: string) {
  return apiRequest<null>(`${appPath(appKey)}/oauth2/clients/${encodeURIComponent(clientId)}`, {
    method: "DELETE",
    token
  });
}

export function listUserOAuth2Grants(token: string, appKey: string, userId: number) {
  return apiRequest<{ items: OAuth2Grant[] }>(`${appPath(appKey)}/users/${userId}/oauth2-grants`, { token });
}

/** clientId 为空时撤销该用户的全部授权，并让其在授权服务器上的登录态失效 */
export function revokeUserOAuth2Grants(token: string, appKey: string, userId: number, clientId?: string) {
  return apiRequest<null>(
    `${appPath(appKey)}/users/${userId}/oauth2-grants${buildQuery({ clientId: clientId || undefined })}`,
    { method: "DELETE", token }
  );
}
