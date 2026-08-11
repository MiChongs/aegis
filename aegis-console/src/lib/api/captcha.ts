import { apiRequest } from "./client";

// ── 类型 ──

export type CaptchaAppConfig = {
  imageEnabled: boolean;
  mathEnabled: boolean;
  digitEnabled: boolean;
  dynamicEnabled: boolean;
  audioEnabled: boolean;
  chiralEnabled: boolean;
  smsEnabled: boolean;
  defaultType: string;
  /** 登录场景是否要求验证码（默认 true，与旧行为对齐） */
  requireForLogin: boolean;
  /** 注册场景是否要求验证码（默认 true） */
  requireForRegister: boolean;
  sms: CaptchaSMSConfig;
  antiFlood: {
    requireCaptcha: boolean;
    ipHourlyLimit: number;
    ipDailyLimit: number;
    phoneDailyLimit: number;
    globalPhoneDailyLimit: number;
    sendIntervalSeconds: number;
  };
};

export type CaptchaSMSTemplateConfig = {
  purpose: string;
  name: string;
  enabled: boolean;
  signName: string;
  templateId: string;
  codeParamKey: string;
};

export type CaptchaSMSConfig = {
  provider: string;
  accessKey: string;
  secretKey: string;
  region: string;
  signName: string;
  templateId: string;
  codeParamKey: string;
  sdkAppId: string;
  templates: CaptchaSMSTemplateConfig[];
};

export type CaptchaGenerateResult = {
  captchaId: string;
  imageData?: string;      // Base64 图片（PNG/GIF）
  audioData?: string;      // Base64 音频（WAV）
  mimeType?: string;       // image/png / image/gif / audio/wav
  clickRequired?: boolean; // 是否需要点击坐标验证
  imageWidth?: number;     // 图片宽度
  imageHeight?: number;    // 图片高度
  hint?: string;           // 提示文字
  chiralCount?: string;    // 手性碳数量（加密）
  expiresAt: number;
};

// ── 公开配置 API（登录/注册前调用） ──

export type AdminCaptchaPublicConfig = {
  enabled: boolean;
  type: string;
  requireForLogin: boolean;
  requireForRegister: boolean;
};

export function getAdminCaptchaPublicConfig() {
  return apiRequest<AdminCaptchaPublicConfig>("/api/admin/captcha/config");
}

// ── 管理员 API ──

export function getAdminCaptchaConfig(token: string, appKey: string) {
  return apiRequest<CaptchaAppConfig>(`/api/admin/apps/${appKey}/captcha-config`, { token });
}

export function updateAdminCaptchaConfig(token: string, appKey: string, config: CaptchaAppConfig) {
  return apiRequest<CaptchaAppConfig>(`/api/admin/apps/${appKey}/captcha-config`, {
    method: "PUT",
    token,
    body: JSON.stringify(config)
  });
}

export type AdminTestSMSPayload = {
  phone: string;
  purpose?: string;
};

export type AdminTestSMSResult = {
  purpose: string;
  templateId: string;
  signName: string;
  result: Record<string, unknown>;
};

export function testAdminCaptchaSMS(token: string, appKey: string, payload: AdminTestSMSPayload) {
  return apiRequest<AdminTestSMSResult>(`/api/admin/apps/${appKey}/captcha-config/test-sms`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 管理员验证码 API（aegis-console 是平台/应用管理端，统一走 /api/admin/captcha/*） ──

export function generateCaptcha(payload: { type: string; purpose: string; appid?: number }) {
  return apiRequest<CaptchaGenerateResult>("/api/admin/captcha/generate", {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export function verifyCaptchaClick(payload: { captchaId: string; clicks: Array<{ x: number; y: number }> }) {
  return apiRequest<{ valid: boolean }>("/api/admin/captcha/verify-click", {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export function verifyCaptcha(payload: { captchaId: string; answer: string }) {
  return apiRequest<{ valid: boolean }>("/api/admin/captcha/verify", {
    method: "POST",
    body: JSON.stringify(payload)
  });
}
