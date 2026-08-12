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
  /** 动态验证码外观（应用级动态配置，控制台可调、即时生效） */
  dynamic: CaptchaDynamicConfig;
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

/**
 * 动态验证码外观。
 *
 * 服务端会把越界值夹回区间（区间与默认值都由渲染引擎裁决），
 * 因此这里不做第二套校验 —— 两处各夹一遍就会出现互相矛盾的解释。
 */
export type CaptchaDynamicConfig = {
  /** 字符数 3-8 */
  length: number;
  /** 画布宽度 80-640 */
  width: number;
  /** 画布高度 40-240 */
  height: number;
  /** 帧数 4-40 */
  frames: number;
  /** 帧间隔（毫秒）20-1000 */
  frameDelayMs: number;
  /** 字符集：alnum / alpha / digit */
  mode: string;
  /** 干扰强度 0-100 */
  noise: number;
  /** 运动幅度 0-100 */
  wobble: number;
};

/** 样张：不落库、无法用于校验，纯粹给管理员看效果 */
export type CaptchaDynamicPreview = {
  imageData: string;
  mimeType: string;
  /** 样张答案。它不是凭据（验不了），而管理员要判断的正是「认不认得出来」 */
  answer: string;
  width: number;
  height: number;
  frames: number;
  frameDelayMs: number;
  durationMs: number;
  byteSize: number;
  /** 夹取之后**真正生效**的参数：填了 60 帧只出 13 帧时界面要说得出为什么 */
  applied: CaptchaDynamicConfig;
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

/**
 * 动态验证码样张。两条路由指向同一个后端 handler，区别只在鉴权作用域：
 * 应用面板按应用授权、平台面板按平台授权。参数全在请求体里，不读也不写配置。
 */
export function previewAppDynamicCaptcha(token: string, appKey: string, config: CaptchaDynamicConfig) {
  return apiRequest<CaptchaDynamicPreview>(`/api/admin/apps/${appKey}/captcha-config/preview`, {
    method: "POST",
    token,
    body: JSON.stringify(config)
  });
}

export function previewAdminDynamicCaptcha(token: string, config: CaptchaDynamicConfig) {
  return apiRequest<CaptchaDynamicPreview>("/api/admin/system/captcha/preview", {
    method: "POST",
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
