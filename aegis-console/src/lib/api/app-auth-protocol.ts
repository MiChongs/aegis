import { apiRequest } from "./client";

/**
 * 应用接入协议（Aegis App Protocol v1）。
 *
 * - `/api/v1/apps/{appKey}/config` 是公开接口，接入方无需任何凭据即可读取；
 * - 其余读写接口属于管理端，需要管理员 Token 与 App 作用域授权。
 *
 * 安全等级 standard / signed / sealed 只改变请求的包装方式，
 * 路径与 JSON 结构三档完全一致。详见 docs/app-integration.md。
 */

export const SECURITY_LEVELS = ["standard", "signed", "sealed"] as const;
export type SecurityLevel = (typeof SECURITY_LEVELS)[number];

export type RegistrationField = {
  name: string;
  type: string;
  required: boolean;
  mutable: boolean;
  label?: string;
  placeholder?: string;
};

export type AuthProtocolPolicy = {
  appId: number;
  protocolVersion: string;
  identifiers: string[];
  loginMethods: string[];
  registerMethods: string[];
  registrationSchema: RegistrationField[];
  requireCaptcha: boolean;
  autoLoginAfterRegister: boolean;
  securityLevel: SecurityLevel;
  allowLegacy: boolean;
  signingSecretSet: boolean;
  signingSecretHint?: string;
  signingSecretRotatedAt?: string;
  createdAt: string;
  updatedAt: string;
};

/** 管理端可写字段；密钥有独立的轮换入口，不随策略表单提交 */
export type AuthProtocolPatch = Pick<
  AuthProtocolPolicy,
  | "identifiers"
  | "loginMethods"
  | "registerMethods"
  | "registrationSchema"
  | "requireCaptcha"
  | "autoLoginAfterRegister"
  | "securityLevel"
  | "allowLegacy"
>;

export type TransportKey = {
  keyId: string;
  algorithm: string;
  status: string;
  notBefore: string;
  notAfter: string;
  createdAt: string;
  revokedAt?: string;
};

/** /config 下发的公钥比管理端多一个 publicKey，且不含创建/撤销时间 */
export type PublicTransportKey = Omit<TransportKey, "createdAt" | "revokedAt"> & {
  publicKey: string;
};

export type OAuthProviderBrief = {
  provider: string;
  displayName: string;
  icon?: string;
  color?: string;
  allowLogin: boolean;
  allowBind: boolean;
  sortOrder: number;
};

/**
 * 各入口到底要不要图形验证码 —— 服务端算好的结论。
 *
 * 服务端有三处开关共同决定这件事（接入协议策略、应用验证码配置的分场景开关、
 * 平台级的短信前置验证码），分属三个管理入口。接入方只看 /config，
 * 所以这里给的是「调这个接口要不要先取验证码」的直接答案，不是配置本身。
 */
export type CaptchaRequirement = {
  login: boolean;
  register: boolean;
  sms: boolean;
};

export type AppIntegrationConfig = {
  protocolVersion: string;
  app: { key: string; name: string; status: boolean };
  auth: {
    identifiers: string[];
    loginMethods: string[];
    registerMethods: string[];
    registrationSchema: RegistrationField[];
    captcha: CaptchaRequirement;
    autoLoginAfterRegister: boolean;
    registerEnabled: boolean;
    loginEnabled: boolean;
    oauthProviders: OAuthProviderBrief[];
  };
  security: {
    level: SecurityLevel;
    appKeyHeader: string;
    signature?: {
      scheme: string;
      header: string;
      timestampHeader: string;
      nonceHeader: string;
      canonical: string;
      maxClockSkewSeconds: number;
    };
    transport?: {
      protocol: string;
      algorithms: string[];
      activeKeyId: string;
      publicKeys: PublicTransportKey[];
      maxClockSkewSeconds: number;
      replayWindowSeconds: number;
      hkdfSalt: string;
    };
  };
  endpoints: Record<string, string>;
};

export type SelfTestStep = {
  key: string;
  title: string;
  ok: boolean;
  skipped?: boolean;
  durationMs: number;
  detail?: string;
  hint?: string;
};

export type SelfTestResult = {
  ok: boolean;
  securityLevel: SecurityLevel;
  baseUrl: string;
  steps: SelfTestStep[];
  startedAt: string;
  durationMs: number;
};

export type RotatedSecret = {
  appSecret: string;
  hint: string;
  rotatedAt: string;
  warning: string;
};

const appPath = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}`;

/** 公开接口：无需 Token，开发者门户与客户端 SDK 均直接调用 */
export function getAppIntegrationConfig(appKey: string) {
  return apiRequest<AppIntegrationConfig>(
    `/api/v1/apps/${encodeURIComponent(appKey)}/config`
  );
}

export function getAuthProtocol(token: string, appKey: string) {
  return apiRequest<AuthProtocolPolicy>(`${appPath(appKey)}/auth-protocol`, { token });
}

export function updateAuthProtocol(
  token: string,
  appKey: string,
  patch: Partial<AuthProtocolPatch>
) {
  return apiRequest<AuthProtocolPolicy>(`${appPath(appKey)}/auth-protocol`, {
    method: "PUT",
    token,
    body: JSON.stringify(patch)
  });
}

/** 轮换应用密钥。明文只在这次响应里出现一次，调用方必须立即呈现给管理员。 */
export function rotateSigningSecret(token: string, appKey: string) {
  return apiRequest<RotatedSecret>(`${appPath(appKey)}/auth-protocol/secret/rotate`, {
    method: "POST",
    token
  });
}

/** 服务端按当前安全等级实跑一遍接入链路 */
export function runIntegrationSelfTest(token: string, appKey: string, baseUrl: string) {
  return apiRequest<SelfTestResult>(`${appPath(appKey)}/auth-protocol/selftest`, {
    method: "POST",
    token,
    body: JSON.stringify({ baseUrl })
  });
}

/**
 * 列出传输密钥。
 *
 * 后端与其余 admin 列表接口一致，用 `{ items: [...] }` 信封返回；
 * 这里拆包成数组，让调用方拿到的就是可直接 map 的列表。
 */
export async function listTransportKeys(token: string, appKey: string): Promise<TransportKey[]> {
  const result = await apiRequest<{ items: TransportKey[] }>(
    `${appPath(appKey)}/auth-protocol/transport/keys`,
    { token }
  );
  return result?.items ?? [];
}

export function rotateTransportKey(token: string, appKey: string) {
  return apiRequest<TransportKey>(`${appPath(appKey)}/auth-protocol/transport/rotate`, {
    method: "POST",
    token
  });
}

export function revokeTransportKey(token: string, appKey: string, keyId: string) {
  return apiRequest<{ revoked: boolean }>(
    `${appPath(appKey)}/auth-protocol/transport/keys/${encodeURIComponent(keyId)}`,
    { method: "DELETE", token }
  );
}
