import type { CaptchaDynamicConfig } from "./captcha";

export type AdminAssignment = {
  roleKey?: string;
  appid?: number | null;
  appName?: string;
};

export type AdminSession = {
  adminId?: number;
  account?: string;
  displayName?: string;
  tokenId?: string;
  issuedAt?: string;
  expiresAt?: string;
  isSuperAdmin?: boolean;
  fallbackToken?: boolean;
  assignments?: AdminAssignment[];
};

export type AdminLoginResult = {
  accessToken?: string;
  expiresAt?: string;
  tokenType?: string;
  admin?: {
    id?: number;
    account?: string;
    displayName?: string;
    email?: string;
    avatar?: string;
    status?: string;
    authSource?: string;
    isSuperAdmin?: boolean;
  };
  assignments?: AdminAssignment[];
  requiresSecondFactor?: boolean;
  challenge?: {
    challengeId: string;
    methods: string[];
    expiresAt: string;
  };
};

export type FirewallSettings = {
  enabled: boolean;
  globalRate: string;
  authRate: string;
  adminRate: string;
  corazaEnabled: boolean;
  corazaParanoia: number;
  requestBodyLimit: number;
  requestBodyMemory: number;
  allowedCIDRs: string[];
  blockedCIDRs: string[];
  blockedUserAgents: string[];
  blockedPathPrefix: string[];
  maxPathLength: number;
  maxQueryLength: number;
  source: string;
  reloadVersion: number;
  reloadedAt?: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
};

export type SecurityModuleStatus = {
  key: string;
  name: string;
  enabled: boolean;
  ready: boolean;
  hotReload: boolean;
  message?: string;
};

export type TOTPStatus = {
  enabled: boolean;
  method?: string;
  issuer?: string;
  accountName?: string;
  enabledAt?: string | null;
  lastVerifiedAt?: string | null;
  pendingSetup?: boolean;
};

export type RecoveryCodeItem = {
  id: number;
  adminId?: number;
  codeHint: string;
  usedAt?: string | null;
  createdAt?: string;
  updatedAt?: string;
};

export type RecoveryCodeSummary = {
  enabled: boolean;
  total: number;
  remaining: number;
  generatedAt?: string | null;
  items?: RecoveryCodeItem[];
};

export type RecoveryCodeIssueResult = {
  total: number;
  remaining: number;
  generatedAt: string;
  codes: string[];
  items: RecoveryCodeItem[];
};

export type PasskeyItem = {
  id: number;
  credentialId: string;
  credentialName?: string;
  signCount: number;
  lastUsedAt?: string | null;
  createdAt?: string;
  updatedAt?: string;
};

export type PasskeySummary = {
  enabled: boolean;
  count: number;
  items?: PasskeyItem[];
};

export type TOTPEnrollment = {
  enrollmentId: string;
  secret: string;
  secretMasked: string;
  provisioningUri: string;
  issuer: string;
  accountName: string;
  expiresAt: string;
};

export type PasskeyRegistrationSession = {
  challengeId: string;
  appid?: number;
  userId?: number;
  expiresAt: string;
};

export type PasskeyRegistrationOptionsResult = {
  session: PasskeyRegistrationSession;
  options: Record<string, unknown>;
};

export type AdminSecurityStatus = {
  hasPassword: boolean;
  twoFactorEnabled: boolean;
  twoFactorMethod?: string;
  passkeyEnabled: boolean;
  lastLoginAt?: string | null;
  twoFactor: TOTPStatus;
  recoveryCodes: RecoveryCodeSummary;
  passkeys: PasskeySummary;
  modules?: SecurityModuleStatus[];
};

export type SystemSecurityModules = {
  totpEnabled: boolean;
  recoveryCodesEnabled: boolean;
  passkeyEnabled: boolean;
};

export type SystemSecurityTOTPSettings = {
  enabled: boolean;
  issuer: string;
  enrollmentTTLSeconds: number;
  skew: number;
  digits: number;
};

export type SystemSecurityRecoveryCodeSettings = {
  enabled: boolean;
  count: number;
  length: number;
};

export type SystemSecurityPasskeySettings = {
  enabled: boolean;
  rpDisplayName: string;
  rpId: string;
  rpOrigins: string[];
  rpTopOrigins: string[];
  challengeTTLSeconds: number;
  userVerification: string;
};

export type SystemSecuritySettings = {
  masterKeyConfigured: boolean;
  challengeTTLSeconds: number;
  modules: SystemSecurityModules;
  totp: SystemSecurityTOTPSettings;
  recoveryCodes: SystemSecurityRecoveryCodeSettings;
  passkey: SystemSecurityPasskeySettings;
  runtimeModules?: SecurityModuleStatus[];
  source: string;
  reloadVersion: number;
  reloadedAt?: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
};

export type AdminCaptchaSettings = {
  enabled: boolean;
  type: string;
  requireForLogin: boolean;
  requireForRegister: boolean;
  audioLang: string;
  /** 管理控制台登录页的动态验证码外观（与各应用那份互不影响） */
  dynamic?: CaptchaDynamicConfig;
};

export type LDAPSettings = {
  enabled: boolean;
  server: string;
  port: number;
  useTLS: boolean;
  useStartTLS: boolean;
  skipTLSVerify: boolean;
  bindDN: string;
  hasBindPassword: boolean;
  baseDN: string;
  userFilter: string;
  userAttribute: string;
  groupBaseDN?: string;
  groupFilter?: string;
  groupAttribute?: string;
  adminGroupDN?: string;
  attrMapping: { account: string; displayName: string; email: string; phone: string };
  connectionTimeoutSeconds: number;
  searchTimeoutSeconds: number;
  fallbackToLocal: boolean;
  source: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
};

export type LDAPTestResult = {
  connected: boolean;
  bindSuccess: boolean;
  searchOK: boolean;
  userFound?: boolean;
  userDN?: string;
  error?: string;
  latencyMs: number;
};

export type SystemSettings = {
  firewall: FirewallSettings;
  security: SystemSecuritySettings;
  adminCaptcha?: AdminCaptchaSettings;
  ldap?: LDAPSettings;
  oidc?: OIDCSettings;
  saml?: SAMLSettings;
  branding?: BrandingSettings;
};

export type SAMLAttrMapping = {
  account: string;
  displayName: string;
  email: string;
  phone: string;
  groups: string;
};

export type SAMLSettings = {
  enabled: boolean;
  idpMetadataURL: string;
  /** 出网只给布尔位：IdP 元数据 XML 可能很大，且编辑态不回显 */
  hasIdpMetadataXML: boolean;
  entityID: string;
  metadataURL: string;
  acsURL: string;
  spCertificate: string;
  /** SP 私钥 AES-GCM 落库，出网只给布尔位 */
  hasSpPrivateKey: boolean;
  nameIDFormat: string;
  signAuthnRequests: boolean;
  forceAuthn: boolean;
  allowIdpInitiated: boolean;
  allowedDomains?: string[];
  adminGroupAttribute?: string;
  adminGroupValue?: string;
  attrMapping: SAMLAttrMapping;
  fallbackToLocal: boolean;
  frontendCallbackURL: string;
  source: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
};

export type SAMLTestResult = {
  metadataOK: boolean;
  entityID?: string;
  ssoRedirectURL?: string;
  ssoPostURL?: string;
  singleLogoutURL?: string;
  certificateCount: number;
  supportedNameIdFormats?: string[];
  error?: string;
  latencyMs: number;
};

export type OIDCSettings = {
  enabled: boolean;
  issuerURL: string;
  clientID: string;
  hasClientSecret: boolean;
  redirectURL: string;
  scopes: string[];
  allowedDomains: string[];
  adminGroupClaim: string;
  adminGroupValue: string;
  attrMapping: { account: string; displayName: string; email: string; phone: string };
  fallbackToLocal: boolean;
  frontendCallbackURL: string;
  source: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
};

export type OIDCTestResult = {
  discoveryOK: boolean;
  issuer?: string;
  authEndpoint?: string;
  tokenEndpoint?: string;
  userInfoEndpoint?: string;
  jwksEndpoint?: string;
  supportedScopes?: string[];
  error?: string;
  latencyMs: number;
};

export type AppSummary = {
  id: number;
  name: string;
  appKey: string;
  status: boolean;
  registerStatus: boolean;
  loginStatus: boolean;
  disabledReason?: string;
  disabledRegisterReason?: string;
  disabledLoginReason?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type AppDetail = AppSummary & {
  settings?: Record<string, unknown>;
  policy?: Record<string, unknown>;
  passwordPolicy?: Record<string, unknown>;
  [key: string]: unknown;
};

/**
 * 应用级认证策略。每一项都有后端执行点 —— 只落库不生效的开关不该出现在这里。
 *
 * `registerCaptcha` / `registerCaptchaTimeOut` 已移除：注册是否要求验证码
 * 由应用验证码配置的 `requireForRegister` 单独决定（`/apps?tab=captcha`）。
 */
export type AppPolicy = {
  /** 登录必须显式携带设备标识，且与该用户已绑定设备一致 */
  loginCheckDevice?: boolean;
  /** 登录属地（国家 + 省/州）与上次成功登录不一致时拦截 */
  loginCheckUser?: boolean;
  /** 登录 IP 与上次成功登录不在同一网段（IPv4 /24、IPv6 /48）时拦截 */
  loginCheckIp?: boolean;
  /** 设备换绑冷却秒数，0 = 不限制。键名沿用旧系统字段 */
  loginCheckDeviceTimeOut?: number;
  multiDeviceLogin?: boolean;
  multiDeviceLimit?: number;
  /** 同一 IP 不允许重复注册 */
  registerCheckIp?: boolean;
  [key: string]: unknown;
};

/**
 * 应用级交易设置。
 *
 * 四个字段**必须整体提交**：后端的 PUT 是全量覆盖，
 * 只发一个字段会把其余三个静默重置成零值。
 */
export type AppCommerceSettings = {
  /** 每单位金额兑换的积分数（integral_purchase 订单用） */
  integralPerCurrency: number;
  /** 支付成功后自动把凭证寄到下单用户绑定的邮箱 */
  receiptEmailOnPaid?: boolean;
  /** 自动寄送时的凭证语言（BCP 47）；留空则按用户设置协商 */
  receiptLocale?: string;
  /** 钱包记账币种（ISO 4217），钱包流水凭证上的金额单位 */
  walletCurrency?: string;
};

/** 某用户的登录绑定基线：开启设备/IP/属地强绑定后的判定依据 */
export type AppLoginBaseline = {
  appid: number;
  userId: number;
  bound: boolean;
  baseline?: {
    deviceId?: string;
    ip?: string;
    region?: string;
    deviceBoundAt?: string;
    updatedAt?: string;
  } | null;
};

export type PasswordPolicy = {
  name?: string;
  description?: string;
  minLength?: number;
  maxLength?: number;
  requireUppercase?: boolean;
  requireLowercase?: boolean;
  requireNumbers?: boolean;
  requireSpecialChars?: boolean;
  minScore?: number;
  maxAge?: number;
  preventReuse?: number;
  isDefault?: boolean;
};

/** 密码合规看板数据（后端按 minScore 现算，不是缓存值） */
export type PasswordPolicyStats = {
  totalUsers: number;
  passwordUsers: number;
  compliantUsers: number;
  /** 达标率百分比（整数） */
  complianceRate: number;
  needChangeUsers: number;
  needChangeRate: number;
};

export type PasswordPolicyView = {
  appid?: number;
  appName?: string;
  policy: PasswordPolicy;
  stats?: PasswordPolicyStats;
};

export type PasswordPolicyTemplateCatalog = {
  templates: Record<string, PasswordPolicy>;
  usage?: Record<string, string>;
};

/** 一次可猜测模式的命中明细。后端刻意不回传命中的原文片段，只给位置区间。 */
export type PasswordPatternMatch = {
  /** zxcvbn 匹配器名：dictionary / spatial / repeat / sequence / date / regex */
  kind: string;
  /** 面向人的中文名 */
  label: string;
  /** 字典来源（泄露口令榜 / 中文语境弱口令表 / 账号相关信息 …） */
  source?: string;
  /** 命中区间，1 起算的闭区间，按字符不按字节 */
  start: number;
  end: number;
  guesses?: number;
};

export type PasswordStrengthDetails = {
  /** 字符数（rune），不是字节数 */
  length: number;
  hasLowercase: boolean;
  hasUppercase: boolean;
  hasNumbers: boolean;
  hasSpecialChars: boolean;
  /** 命中模式的中文名去重列表（tag 墙用）；带位置的明细见 patterns */
  hasCommonPatterns: string[];
  patterns?: PasswordPatternMatch[];
  /** 猜测熵 log2(guesses)，单位 bit。注意不是旧版的每字符香农熵 */
  entropy: number;
  /** 字节数。bcrypt 的 72 字节上限按字节算，中文口令会远早于字符数上限触顶 */
  byteLength: number;
};

export type PasswordRecommendation = { type?: string; priority?: string; message?: string };

export type PasswordStrengthAnalysis = {
  /** 0~100，由 zxcvbn 的猜测次数映射而来 */
  score: number;
  level: string;
  feedback: string[];
  details: PasswordStrengthDetails;
  recommendations: PasswordRecommendation[];
  /** zxcvbn 原生 0~4 档，与 score 同源不同刻度 */
  zxcvbnScore?: number;
  /** log10(猜测次数)，评分的真正依据 */
  guessesLog10?: number;
  /** 离线慢哈希（bcrypt 量级）场景下的可读破解时长 */
  crackTime?: string;
};

export type PasswordPolicyTestResult = {
  /** 后端只回显掩码，绝不回传明文 */
  password?: string;
  policy?: PasswordPolicy;
  strengthAnalysis?: PasswordStrengthAnalysis;
  policyCheck?: {
    isValid?: boolean;
    violations?: string[];
    analysis?: PasswordStrengthAnalysis;
    policy?: PasswordPolicy;
  };
  result?: {
    isValid?: boolean;
    score?: number;
    level?: string;
    violations?: string[];
    recommendations?: PasswordRecommendation[];
  };
};

export type SignInRewardRule = {
  key: string;
  name: string;
  description?: string;
  enabled: boolean;
  priority: number;
  group?: string;
  expression: string;
  bonusType?: string;
  bonusDescription?: string;
  integralMultiplierDelta: number;
  integralBonus: number;
  experienceMultiplierDelta: number;
  experienceBonus: number;
};

export type SignInRewardMilestone = {
  consecutiveDays: number;
  integralBonus: number;
  experienceBonus: number;
  bonusType?: string;
  description?: string;
};

export type SignInRewardPolicy = {
  name: string;
  description?: string;
  enabled: boolean;
  timezone: string;
  baseIntegral: number;
  baseExperience: number;
  firstSignInExperienceBonus: number;
  consecutiveExperienceStep: number;
  consecutiveExperienceStepCap: number;
  applyLevelExperienceMultiplier: boolean;
  maxIntegralReward: number;
  maxExperienceReward: number;
  rules: SignInRewardRule[];
  milestones: SignInRewardMilestone[];
  isDefault: boolean;
};

export type SignInRewardPolicyView = {
  appid: number;
  appName: string;
  policy: SignInRewardPolicy;
};

export type SignInRewardAppliedRule = {
  key: string;
  name: string;
  group?: string;
  bonusType?: string;
  description?: string;
  expression?: string;
  integralMultiplierDelta: number;
  integralBonus: number;
  experienceMultiplierDelta: number;
  experienceBonus: number;
  consecutiveDays?: number;
};

export type SignInRewardResolved = {
  baseIntegral: number;
  integralReward: number;
  experienceReward: number;
  rewardMultiplier: number;
  bonusType?: string;
  bonusDescription?: string;
};

export type SignInRewardPreviewInput = {
  occurredAt?: string;
  consecutiveDays: number;
  totalSignIns: number;
  userExperience: number;
};

export type SignInRewardPreview = {
  appid: number;
  appName: string;
  occurredAt: string;
  timezone: string;
  policy: SignInRewardPolicy;
  reward: SignInRewardResolved;
  appliedRules: SignInRewardAppliedRule[];
  environment: Record<string, unknown>;
};

export type SignInRewardTemplateCatalog = {
  templates: Record<string, SignInRewardPolicy>;
  usage?: Record<string, string>;
};

export type AppSignInTrendPoint = {
  date: string;
  count: number;
};

export type AppSignInSourceStat = {
  source: string;
  count: number;
};

export type AppSignInStats = {
  appid: number;
  days: number;
  todaySignCount: number;
  totalSignRecords: number;
  uniqueSignedUsers: number;
  totalIntegralReward: number;
  totalExperienceReward: number;
  avgConsecutiveDays: number;
  maxConsecutiveDays: number;
  trend: AppSignInTrendPoint[];
  sources: AppSignInSourceStat[];
};

export type AppSignInRecordItem = {
  id: number;
  userId: number;
  appid: number;
  account: string;
  nickname?: string;
  avatar?: string;
  email?: string;
  phone?: string;
  signDate: string;
  signedAt: string;
  integralReward: number;
  experienceReward: number;
  consecutiveDays: number;
  rewardMultiplier: number;
  bonusType?: string;
  bonusDescription?: string;
  signInSource?: string;
  deviceInfo?: string;
  ipAddress?: string;
  location?: string;
  createdAt: string;
};

export type AppStats = {
  appid: number;
  totalUsers: number;
  enabledUsers: number;
  disabledUsers: number;
  bannerCount: number;
  noticeCount: number;
  oauthBindCount: number;
  newUsersToday: number;
  newUsersLast7Days: number;
  newUsersLast30Days: number;
  loginSuccessToday: number;
  loginFailureToday: number;
};

export type MonitorStatus = "available" | "degraded" | "unavailable";

export type MonitorComponent = {
  key: string;
  name: string;
  status: MonitorStatus;
  severity: "info" | "warning" | "critical";
  available: boolean;
  summary: string;
  detail: string;
  checkedAt: string;
  dependsOn?: string[];
  meta?: Record<string, unknown>;
};

export type MonitorCounts = {
  total: number;
  available: number;
  degraded: number;
  unavailable: number;
};

export type MonitorRuntime = {
  appName: string;
  environment: string;
  port: number;
  checkedAt: string;
  startedAt: string;
  uptimeSeconds: number;
  timezone: string;
};

export type MonitorEndpoint = {
  key: string;
  name: string;
  method: string;
  path: string;
  scope: string;
  protected: boolean;
  status: MonitorStatus;
  summary: string;
  dependsOn?: string[];
  description?: string;
};

export type MonitorAppBrief = {
  id: number;
  name: string;
  status: MonitorStatus;
  score: number;
  availabilityRate: number;
  summary: string;
  checkedAt: string;
  counts: MonitorCounts;
};

export type MonitorOverview = {
  status: MonitorStatus;
  score: number;
  availabilityRate: number;
  summary: string;
  checkedAt: string;
  runtime: MonitorRuntime;
  counts: MonitorCounts;
  highlights?: string[];
  endpoints?: MonitorEndpoint[];
  applications?: MonitorAppBrief[];
  infrastructure: MonitorComponent[];
  modules: MonitorComponent[];
  components: MonitorComponent[];
};

export type AppMonitorOverview = {
  status: MonitorStatus;
  score: number;
  availabilityRate: number;
  summary: string;
  checkedAt: string;
  runtime: MonitorRuntime;
  counts: MonitorCounts;
  app: {
    id: number;
    name: string;
    status: boolean;
    registerStatus: boolean;
    loginStatus: boolean;
    disabledReason?: string;
    disabledRegisterReason?: string;
    disabledLoginReason?: string;
  };
  highlights?: string[];
  metrics?: Record<string, unknown>;
  infrastructure: MonitorComponent[];
  entrypoints?: MonitorComponent[];
  modules: MonitorComponent[];
  components: MonitorComponent[];
};

/** Banner 展示位：客户端据此决定这条 Banner 画在哪儿。 */
export type BannerSlot = "hero" | "popup" | "splash" | "notice" | "card";

export type BannerItem = {
  id: number;
  /** 落库的持久形态：外链 URL 或 `storage://{configId}/{objectKey}` */
  header?: string;
  /** 服务端解析出的可直接展示地址；`<img src>` 用这个，不要用 header */
  headerDisplayUrl?: string;
  title?: string;
  content?: string;
  url?: string;
  type?: BannerSlot | string;
  position?: number;
  status?: boolean;
  startTime?: string | null;
  endTime?: string | null;
  viewCount?: number;
  clickCount?: number;
  createdAt?: string;
  updatedAt?: string;
};

export type NoticeType = "notice" | "activity" | "maintenance" | "update" | "security";
export type NoticeLevel = "normal" | "important" | "critical";
export type NoticeStatus = "draft" | "published" | "archived";

export type NoticeItem = {
  id: number;
  title?: string;
  /** 富文本 HTML，服务端已净化 */
  content?: string;
  /** 服务端提取的纯文本摘要，列表与推送用它 */
  summary?: string;
  type?: NoticeType | string;
  level?: NoticeLevel | string;
  status?: NoticeStatus | string;
  pinned?: boolean;
  startTime?: string | null;
  endTime?: string | null;
  publishedAt?: string | null;
  viewCount?: number;
  createdBy?: number | null;
  createdAt?: string;
  updatedAt?: string;
};

export type NoticeListResponse = {
  items: NoticeItem[];
  total: number;
  page: number;
  limit: number;
};

/** 内容中心总览：Banner 与公告的计数一次取齐。 */
export type ContentOverview = {
  bannerTotal: number;
  bannerLive: number;
  bannerScheduled: number;
  bannerExpired: number;
  bannerDisabled: number;
  bannerViews: number;
  bannerClicks: number;
  noticeTotal: number;
  noticePublished: number;
  noticeDraft: number;
  noticeArchived: number;
  noticePinned: number;
  noticeViews: number;
  lastPublishedAt?: string | null;
};

export type ContentImageUploadResult = {
  /** 落库用的持久引用 */
  reference: string;
  /** 当场预览用的带票据地址 */
  url: string;
};

// 平台级 Banner —— 超级管理员专属的全局横幅（后端 /api/admin/system/banners/*）
export type PlatformBannerType = "info" | "notice" | "maintenance" | "release" | "security";

export type PlatformBannerItem = {
  id: number;
  title: string;
  description?: string;
  /** 持久化的规范形态：`storage://{configId}/{objectKey}` 或外链 URL */
  imageUrl: string;
  /** 读取时解析得到的展示 URL（带 ticket 的代理地址或原外链）；前端 <img> src 用这个 */
  imageDisplayUrl?: string;
  clickUrl?: string;
  type: PlatformBannerType;
  position: number;
  status: boolean;
  startTime?: string | null;
  endTime?: string | null;
  createdBy?: number | null;
  viewCount?: number;
  clickCount?: number;
  createdAt?: string;
  updatedAt?: string;
};

export type PlatformBannerMutation = Partial<
  Omit<
    PlatformBannerItem,
    "id" | "createdAt" | "updatedAt" | "viewCount" | "clickCount" | "createdBy"
  >
>;

export type PlatformBannerListResponse = {
  items: PlatformBannerItem[];
  total: number;
  page: number;
  limit: number;
};

/** 单个设置分类的覆盖率 */
export type SettingCoverageStat = {
  usersWithSettings: number;
  usersWithoutSettings: number;
  /** 覆盖率百分比 */
  coverage: number;
};

export type UserSettingsStats = {
  appid?: number;
  totalUsers: number;
  /** 分类 → 覆盖率 */
  settingsStats: Record<string, SettingCoverageStat>;
  recentSettings: Array<{ userId: number; category: string; createdAt: string; version: number }>;
  categories: string[];
  summary: { totalCategories: number; avgCoverage: number };
};

export type SettingsInitializeResult = {
  appid: number;
  userIds: number[];
  categories: string[];
  processedUsers: number;
  initializedCategories: number;
  skippedExisting: number;
};

export type SettingsIntegrityResult = {
  appid: number;
  totalIssues: number;
  issues: Array<{ userId: number; category: string; missingKeys: string[]; settingId: number }>;
  repairs: Array<{ userId: number; category: string; repairedKeys: string[] }>;
  autoRepair: boolean;
};

export type SettingsCleanupResult = {
  appid: number;
  foundInvalid: number;
  cleaned: number;
  dryRun: boolean;
  invalidSettings: Array<{ id: number; userId: number; category: string; isActive: boolean }>;
};

/** 邮件服务商配置字段的声明式描述（后端 Describe() 自述，驱动动态表单）。 */
export type EmailConfigField = {
  key: string;
  label: string;
  type: "text" | "secret" | "number" | "switch" | "select" | "textarea" | "email" | "url" | "kv";
  group?: string;
  required?: boolean;
  /** 密钥字段：加密落库、出网抹除、提交留空即不修改。 */
  secret?: boolean;
  placeholder?: string;
  help?: string;
  default?: unknown;
  options?: { value: string; label: string; help?: string }[];
  advanced?: boolean;
};

/** 一条通道的能力自述。attachments 决定凭证是走附件还是走下载链接。 */
export type EmailProviderCapabilities = {
  attachments: boolean;
  webhook: boolean;
  tags: boolean;
  tracking: boolean;
};

/**
 * 邮件服务商元数据。由后端各发送器的 Describe() 自述，
 * 驱动「服务商卡片 + 动态配置表单 + 回调地址提示」三处 UI ——
 * 因此后端新增一家服务商时，这里零改动即自动出现。
 */
export type EmailProviderMeta = {
  provider: string;
  name: string;
  description?: string;
  category?: string;
  categoryName?: string;
  icon?: string;
  brandColor?: string;
  docUrl?: string;
  capabilities: EmailProviderCapabilities;
  fields?: EmailConfigField[];
  /** 回执回调路径模板，{scope} 由前端替换成应用 id 或 platform。 */
  webhookPath?: string;
  webhookNote?: string;
  notes?: string[];
};

export type EmailProviderCatalog = {
  providers: EmailProviderMeta[];
  groups: Record<string, string>;
  categories: Record<string, string>;
};

export type EmailConfig = {
  id: number;
  /** 0 表示平台级配置。 */
  appid?: number;
  name?: string;
  provider?: string;
  enabled?: boolean;
  isDefault?: boolean;
  /** 仅平台级有意义：允许应用在自己没有配置时回落到这条通道。 */
  shared?: boolean;
  description?: string;
  /** 非密钥字段，键由服务商目录声明。 */
  settings?: Record<string, string>;
  /** 密钥「配没配」的布尔位 —— 值本身永不回传。 */
  secretSet?: Record<string, boolean>;
  createdAt?: string;
  updatedAt?: string;
};

/** 「这个作用域现在实际用哪条通道发信」。inherited 表示回落到了平台共享通道。 */
export type EmailChannelResolution = {
  configId: number;
  configName: string;
  provider: string;
  scope: "app" | "platform";
  inherited: boolean;
  attachments: boolean;
};

export type EmailDeliveryStats = {
  total: number;
  sent: number;
  delivered: number;
  failed: number;
  bounced: number;
  pending: number;
  last24h: number;
};

export type EmailDelivery = {
  id: number;
  appid: number;
  configId?: number;
  configName?: string;
  provider: string;
  providerMessageId?: string;
  messageId?: string;
  toAddress: string;
  fromAddress?: string;
  subject?: string;
  purpose?: string;
  status: string;
  errorMessage?: string;
  openCount?: number;
  clickCount?: number;
  lastEvent?: string;
  sentAt?: string;
  deliveredAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type EmailDeliveryPage = {
  items: EmailDelivery[];
  total: number;
  page: number;
  pageSize: number;
};

export type PaymentConfig = {
  id: number;
  appid?: number;
  payment_method?: string;
  config_name?: string;
  config_data?: Record<string, unknown>;
  enabled?: boolean;
  is_default?: boolean;
  description?: string;
  createdAt?: string;
  updatedAt?: string;
  [key: string]: unknown;
};

/** 支付渠道配置项声明（后端 Provider.Describe() 下发，驱动动态表单渲染） */
export type PaymentConfigField = {
  key: string;
  label: string;
  type: "text" | "secret" | "number" | "switch" | "select" | "textarea" | "tags" | "url";
  group?: "credential" | "gateway" | "limit" | "advanced";
  required?: boolean;
  placeholder?: string;
  help?: string;
  default?: unknown;
  options?: Array<{ value: string; label: string }>;
  advanced?: boolean;
};

/** 渠道能力矩阵（前端以能力标签可视化） */
export type PaymentProviderCapabilities = {
  redirect: boolean;
  qrcode: boolean;
  webhook: boolean;
  webhookSignature: boolean;
  remoteQuery: boolean;
  sandbox: boolean;
  subMerchant: boolean;
};

/** 支付渠道完整描述：渠道市场卡片 + 动态配置表单的唯一数据源 */
export type PaymentProviderMeta = {
  method: string;
  name: string;
  description?: string;
  category?: string;
  categoryName?: string;
  /** Simple Icons slug */
  icon?: string;
  brandColor?: string;
  docUrl?: string;
  callbackPath?: string;
  callbackNote?: string;
  regions?: string[];
  currencies?: string[];
  capabilities: PaymentProviderCapabilities;
  payTypes?: Array<{ value: string; label: string; description?: string }>;
  fields?: PaymentConfigField[];
  supportedTypes?: string[];
};

/** 退款单 */
export type PaymentRefund = {
  id: number;
  appid: number;
  order_id: number;
  order_no: string;
  refund_no: string;
  provider_refund_no?: string;
  user_id?: number | null;
  payment_method: string;
  amount: string;
  reason?: string;
  /** pending / processing / success / failed / closed */
  status: string;
  /** none / done / skipped / failed —— 已发放权益的回收结果 */
  reversal_status: string;
  reversal_message?: string;
  operator?: string;
  client_ip?: string;
  error_message?: string;
  raw_response?: Record<string, unknown>;
  refunded_at?: string | null;
  createdAt?: string;
  updatedAt?: string;
};

export type PaymentRefundList = {
  items: PaymentRefund[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

/** 订单可退款额度与渠道退款能力 */
export type PaymentRefundable = {
  order_no: string;
  payment_method: string;
  amount: string;
  refunded_amount: string;
  refundable: string;
  refund_status: string;
  supported: boolean;
  partial_allowed: boolean;
  reason?: string;
};

export type AdminPaymentOrder = {
  id: number;
  appid: number;
  user_id?: number | null;
  config_id?: number;
  order_no: string;
  provider_order_no?: string;
  subject: string;
  body?: string;
  amount: string;
  payment_method: string;
  provider_type?: string;
  status: string;
  notify_status?: string;
  client_ip?: string;
  metadata?: Record<string, unknown>;
  refunded_amount?: string;
  /** none / partial / full */
  refund_status?: string;
  paid_at?: string | null;
  expire_at?: string | null;
  createdAt?: string;
  updatedAt?: string;
  [key: string]: unknown;
};

export type AdminPaymentOrderList = {
  items: AdminPaymentOrder[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type AdminPaymentOrderDetail = {
  order: AdminPaymentOrder;
  fulfillment_status?: string;
  fulfilled_at?: string | null;
  callback_logs?: Array<{
    id: number;
    payment_method: string;
    callback_method: string;
    client_ip?: string;
    callback_data?: Record<string, unknown>;
    verification_status: string;
    message?: string;
    created_at: string;
  }>;
};

export type VipPlan = {
  id: number;
  appid: number;
  name: string;
  /** paid（付费）/ trial（试用） */
  kind?: string;
  trialDeviceLimited?: boolean;
  /** 套餐包含的权益标识（引用会员功能目录 tag） */
  features?: string[];
  durationDays: number;
  price: string;
  originalPrice?: string | null;
  bonusIntegral: number;
  description?: string;
  isActive: boolean;
  sortOrder: number;
  createdAt?: string;
  updatedAt?: string;
};

export type VipTransaction = {
  id: number;
  transactionNo: string;
  userId: number;
  appid: number;
  planId?: number | null;
  planName: string;
  features?: string[];
  durationDays: number;
  payChannel: string;
  payAmount: string;
  relatedOrderNo?: string;
  bonusIntegral: number;
  expireBefore?: string | null;
  expireAfter: string;
  operator?: string;
  createdAt: string;
};

/** 会员功能目录条目 */
export type VipFeature = {
  id: number;
  appid: number;
  tag: string;
  name: string;
  description?: string;
  isActive: boolean;
  sortOrder: number;
  createdAt?: string;
  updatedAt?: string;
};

export type VipTrialState = {
  active: boolean;
  claimedAt: string;
  endsAt: string;
  durationDays: number;
  planId?: number | null;
  planName: string;
  remainingSeconds: number;
};

export type VipTrialOffer = {
  available: boolean;
  reason: string;
  message: string;
  planId?: number;
  planName?: string;
  durationDays?: number;
  bonusIntegral?: number;
  deviceLimited?: boolean;
  description?: string;
};

/** 会员权益判定结果，与用户端 /vip/status 同源 */
export type VipEntitlement = {
  isVip: boolean;
  isTrial: boolean;
  /** none / unknown / trial / wallet / payment_order / admin_grant */
  source: string;
  planName?: string;
  expireAt?: string | null;
  remainingSeconds: number;
  remainingDays: number;
  features: string[];
  trial?: VipTrialState | null;
  trialOffer: VipTrialOffer;
};

export type VersionItem = {
  id: number;
  appid: number;
  channel_id?: number | null;
  channel_name?: string;
  version?: string;
  version_code?: number;
  description?: string;
  release_notes?: string;
  download_url?: string;
  file_size?: number;
  file_hash?: string;
  force_update?: boolean;
  update_type?: string;
  platform?: string;
  min_os_version?: string;
  status?: string;
  download_count?: number;
  metadata?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
};

export type VersionListResult = {
  items: VersionItem[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type ChannelRule = {
  field: string;   // platform / os_version / user_id / region / tag ...
  op: string;      // eq / neq / in / not_in / gt / lt / gte / lte / regex / contains
  value: unknown;  // 字符串 / 数字 / 数组
};

export type VersionChannel = {
  id: number;
  appid: number;
  name?: string;
  code?: string;
  description?: string;
  is_default?: boolean;
  status?: boolean;
  priority?: number;
  color?: string;
  level?: string;
  rollout_pct?: number;
  platforms?: string[];
  min_version_code?: number;
  max_version_code?: number;
  rules?: ChannelRule[];
  targetAudience?: Record<string, unknown>;
  userCount?: number;
  createdAt?: string;
  updatedAt?: string;
};

export type VersionStats = {
  appid: number;
  totalVersions?: number;
  publishedCount?: number;
  channelCount?: number;
  platformCounts?: Record<string, number>;
};

export type SystemAnnouncement = {
  id: number;
  adminId: number;
  adminName?: string;
  type: string;
  title: string;
  content?: string;
  level: string;
  pinned: boolean;
  status: string;
  publishedAt?: string | null;
  expiresAt?: string | null;
  metadata?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
};

export type SystemAnnouncementListResult = {
  items: SystemAnnouncement[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type SiteItem = {
  id: number;
  appid: number;
  userId?: number;
  account?: string;
  nickname?: string;
  avatar?: string;
  header?: string;
  name?: string;
  url?: string;
  type?: string;
  description?: string;
  category?: string;
  status?: string;
  audit_status?: string;
  audit_reason?: string;
  is_pinned?: boolean;
  view_count?: number;
  like_count?: number;
  extra?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
};

export type SiteListResult = {
  list: SiteItem[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
  hasNextPage?: boolean;
  hasPrevPage?: boolean;
};

export type SiteAuditStats = {
  appid: number;
  total?: number;
  byStatus?: Record<string, number>;
  pending?: number;
  approved?: number;
  rejected?: number;
};

export type RoleApplicationItem = {
  id: number;
  appid: number;
  userId?: number;
  account?: string;
  nickname?: string;
  avatar?: string;
  requestedRole?: string;
  currentRole?: string;
  reason?: string;
  status?: string;
  priority?: string;
  validDays?: number;
  reviewReason?: string;
  reviewedBy?: number | null;
  reviewedByName?: string;
  reviewedAt?: string | null;
  cancelledAt?: string | null;
  deviceInfo?: Record<string, unknown>;
  extra?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
};

export type RoleApplicationListResult = {
  items: RoleApplicationItem[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type RoleApplicationStatistics = {
  appid: number;
  total?: number;
  pending?: number;
  approved?: number;
  rejected?: number;
  cancelled?: number;
  byRole?: Record<string, number>;
  byPriority?: Record<string, number>;
};

export type UserTrend = {
  appid: number;
  days: number;
  totalNew: number;
  series: Array<{ date: string; count: number }>;
};

export type RegionStatItem = {
  region?: string;
  code?: string;
  label?: string;
  name?: string;
  parent?: string;
  parentPath?: string;
  count?: number;
  value?: number;
  percentage?: number;
  [key: string]: unknown;
};

export type RegionStatsResult = {
  appid?: number;
  type?: string;
  items?: RegionStatItem[];
  total?: number;
  [key: string]: unknown;
};

export type AuthSourceStatItem = {
  source?: string;
  label?: string;
  name?: string;
  count?: number;
  value?: number;
  percentage?: number;
  [key: string]: unknown;
};

export type AuthSourceStatsResult = {
  appid?: number;
  items?: AuthSourceStatItem[];
  total?: number;
  [key: string]: unknown;
};

export type AdminContactInfo = {
  platform: string;  // wechat / qq / telegram / discord / twitter / github / phone / email / other
  value: string;
  label?: string;
};

export type AdminAccount = {
  id: number;
  account: string;
  displayName: string;
  email?: string;
  avatar?: string;
  phone?: string;
  birthday?: string | null;
  bio?: string;
  contacts?: AdminContactInfo[];
  status?: string;
  authSource?: string;
  isSuperAdmin?: boolean;
  lastLoginAt?: string | null;
  createdAt?: string;
  updatedAt?: string;
};

export type AdminProfile = {
  account: AdminAccount;
  assignments?: AdminAssignment[];
  /**
   * 第三方认证源健康探测结果；仅当当前登录会话为超级管理员时由后端附加。
   *   - source: 该管理员的 authSource（ldap / oidc / saml）
   *   - available: 认证源是否可用（enabled 且探测成功）
   *   - reason: 不可用时的中文原因（如 "LDAP 服务器无法连接：dial tcp ..."）
   *   - checkedAt: 探测时间（用于展示"缓存 30s 内"的时效性）
   * 本地账号（password）不会返回此字段；非超管会话也不会返回。
   */
  loginAvailability?: AdminLoginAvailability;
};

export type AdminLoginAvailability = {
  source: "ldap" | "oidc" | "saml" | string;
  available: boolean;
  reason?: string;
  checkedAt?: string;
};

// ── RBAC 可视化编辑器 ──

export type RoleMatrixRow = {
  permissionCode: string;
  permissionName: string;
  groupKey: string;
  groupName: string;
  grants: Record<string, boolean>;
};

export type RoleMatrix = {
  roles: RoleDefinition[];
  groups: PermissionGroup[];
  rows: RoleMatrixRow[];
};

export type RoleGraphNode = {
  key: string;
  name: string;
  level: number;
  scope: string;
  isCustom: boolean;
  permCount: number;
};

export type RoleGraphEdge = {
  source: string;
  target: string;
  relation: "includes" | "inherits";
};

export type RoleGraph = {
  nodes: RoleGraphNode[];
  edges: RoleGraphEdge[];
};

export type ImpactAdmin = {
  adminId: number;
  account: string;
  displayName: string;
};

export type ImpactPreview = {
  affectedAdmins: ImpactAdmin[];
  totalAffected: number;
};

export type CustomRole = {
  id: number;
  roleKey: string;
  name: string;
  description?: string;
  level: number;
  scope: string;
  baseRole?: string;
  permissions: string[];
  isCustom: boolean;
  createdBy?: number | null;
  createdAt?: string;
  updatedAt?: string;
};

// ── 审计日志 ──

export type AuditLog = {
  id: number;

  // 身份
  adminId: number;
  adminName: string;
  adminRole?: string;
  sessionId?: string;

  // 分类
  action: string;
  category?: string;
  severity?: string;

  // 目标
  resource: string;
  resourceId: string;

  // 可读文本
  summary?: string;
  detail: string;

  // 请求追踪
  requestId?: string;
  traceId?: string;
  method?: string;
  path?: string;
  route?: string;
  statusCode?: number;
  latencyMs?: number;

  // 流量
  requestSize?: number;
  responseSize?: number;
  responseSnippet?: string;

  // 网络
  ip: string;
  country?: string;
  region?: string;
  city?: string;
  isp?: string;
  userAgent: string;

  // 结果
  status: string; // success / failed / denied / blocked
  errorCode?: string;
  errorMessage?: string;

  // 上下文
  changes?: Record<string, unknown>;
  createdAt: string;
};

export type AuditPage = {
  items: AuditLog[];
  total: number;
  page: number;
  limit: number;
};

export type AuditStatItem = { key: string; label: string; count: number };

export type AuditStats = {
  todayCount: number;
  weekCount: number;
  failedToday?: number;
  criticalToday?: number;
  avgLatencyMs?: number;
  topAdmins: AuditStatItem[];
  topActions: AuditStatItem[];
  topCategories?: AuditStatItem[];
  severityBuckets?: AuditStatItem[];
};

// ── 消息模板 ──

export type TemplateVariable = {
  key: string;
  name: string;
  description?: string;
  example?: string;
};

export type MessageTemplate = {
  id: number;
  code: string;
  name: string;
  description?: string;
  channel: "email" | "sms" | "notification";
  subject: string;
  bodyHtml: string;
  bodyText: string;
  variables: TemplateVariable[];
  isBuiltin: boolean;
  enabled: boolean;
  createdBy?: number | null;
  createdAt?: string;
  updatedAt?: string;
};

export type RenderResult = {
  subject: string;
  bodyHtml: string;
  bodyText: string;
};

// ── 品牌配置 ──

export type BrandingConfig = {
  platformName: string;
  consoleName: string;
  logoURL: string;
  logoDarkURL: string;
  faviconURL: string;
  primaryColor: string;
  primaryColorDark: string;
  accentColor: string;
  loginBgURL: string;
  loginBgColor: string;
  footerText: string;
  customCSS: string;
};

export type BrandingSettings = BrandingConfig & {
  source: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
};

// ── 组织架构 ──
//
// 所有组织域实体的 id 都是后端下发的 UUID 字符串（对外稳定标识），
// 只有 adminId / appId 仍是数字 —— 它们属于管理员与应用两个既有主键空间。

export type OrgContact = { name: string; email: string; phone: string };
export type OrgQuota = { memberLimit: number; deptLimit: number; appLimit: number };
export type OrgStats = { memberCount: number; deptCount: number; appCount: number; childCount: number };

export type Organization = {
  id: string;
  parentId?: string;
  parentName?: string;
  depth: number;
  name: string;
  code: string;
  kind: string;
  description: string;
  logoURL: string;
  status: string;
  ownerId?: number | null;
  ownerName?: string;
  ownerAccount?: string;
  contact: OrgContact;
  industry: string;
  region: string;
  quota: OrgQuota;
  expiresAt?: string | null;
  settings?: Record<string, unknown>;
  stats: OrgStats;
  createdBy?: number | null;
  createdAt: string;
  updatedAt: string;
  viewerRole?: string;
};

export type OrganizationNode = Organization & { children: OrganizationNode[] };

/** 调用者在某组织内的有效权限，按钮显隐一律读它 —— 与服务端判定同源 */
export type OrgAccess = {
  orgId: string;
  adminId: number;
  isSuperAdmin: boolean;
  isPlatformAdmin: boolean;
  orgRole: string;
  customRoles: string[];
  permissions: string[];
};

export type Department = {
  id: string;
  orgId: string;
  parentId?: string;
  depth: number;
  name: string;
  code: string;
  kind: string;
  description: string;
  sortOrder: number;
  leaderId?: number | null;
  leaderName?: string;
  status: string;
  memberLimit: number;
  settings?: Record<string, unknown>;
  memberCount: number;
  totalMemberCount: number;
  childCount: number;
  createdAt: string;
  updatedAt: string;
};

export type DepartmentNode = Department & { children: DepartmentNode[] };

export type DeptRef = { id: string; name: string; fullName?: string; isLeader?: boolean };

export type OrgMember = {
  id: string;
  orgId: string;
  adminId: number;
  account: string;
  displayName: string;
  email: string;
  avatar: string;
  orgRole: string;
  primaryDept?: DeptRef;
  departments: DeptRef[];
  employeeNo: string;
  title: string;
  status: string;
  joinedAt: string;
  leftAt?: string | null;
  invitedBy?: number | null;
  isSuperAdmin?: boolean;
};

export type DepartmentMember = {
  adminId: number;
  account: string;
  displayName: string;
  avatar: string;
  orgRole: string;
  isLeader: boolean;
  joinedAt: string;
  positionId?: string;
  positionName?: string;
  jobTitle?: string;
  reportingTo?: number | null;
  reportingName?: string;
  delegateTo?: number | null;
  delegateName?: string;
  delegateExpiresAt?: string | null;
};

export type ReportingNode = {
  adminId: number;
  account: string;
  displayName: string;
  avatar: string;
  jobTitle: string;
  depth: number;
};

export type Position = {
  id: string;
  orgId: string;
  name: string;
  code: string;
  level: number;
  description: string;
  memberCount: number;
  createdAt: string;
};

export type OrgRole = {
  id: string;
  orgId: string;
  roleKey: string;
  name: string;
  description: string;
  permissions: string[];
  isBuiltin: boolean;
  memberCount: number;
  createdAt: string;
  updatedAt: string;
};

export type OrgRoleGrant = {
  roleId: string;
  roleName: string;
  adminId: number;
  account: string;
  displayName: string;
  scopeDept?: DeptRef;
  grantedBy?: number | null;
  grantedAt: string;
};

export type OrgInvitation = {
  id: string;
  orgId: string;
  orgName: string;
  deptId?: string;
  deptName?: string;
  inviterId: number;
  inviterName: string;
  inviteeId: number;
  inviteeName: string;
  orgRole: string;
  isLeader: boolean;
  status: string;
  message: string;
  respondedAt?: string | null;
  expiresAt: string;
  createdAt: string;
};

export type OrgActivityLog = {
  orgId: string;
  actorId?: number | null;
  actorName?: string;
  action: string;
  targetType?: string;
  targetId?: string;
  summary: string;
  detail?: Record<string, unknown>;
  createdAt: string;
};

export type OrgAppBinding = {
  orgId: string;
  appId: number;
  appName: string;
  appKey?: string;
  owned: boolean;
  createdAt: string;
};

export type OrgOverviewStats = {
  memberTotal: number;
  memberActive: number;
  memberSuspended: number;
  deptTotal: number;
  deptMaxDepth: number;
  positionTotal: number;
  appTotal: number;
  pendingInvites: number;
  unassignedMembers: number;
  childOrgs: number;
};

export type OrgOverview = {
  organization: Organization;
  stats: OrgOverviewStats;
  roleBreakdown: Record<string, number>;
  topDepartments: DeptRef[];
  recentActivity: OrgActivityLog[];
};

export type OrgPage<T> = {
  items: T[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

// ── 组织审批与权限模板 ──

export type ApprovalStep = {
  approverType: string;
  approverId: number;
  approverRole?: string;
  name?: string;
  order: number;
};

export type ApprovalChain = {
  id: string;
  orgId: string;
  name: string;
  triggerType: string;
  steps: ApprovalStep[];
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
};

export type ApproverRef = { adminId: number; account: string; displayName: string };

export type StepResult = {
  step: number;
  approverId: number;
  approverName?: string;
  action: string;
  comment: string;
  at: string;
};

export type ApprovalInstance = {
  id: string;
  chainId: string;
  chainName?: string;
  orgId: string;
  triggerType: string;
  requesterId: number;
  requesterName?: string;
  subjectData: Record<string, unknown>;
  currentStep: number;
  totalSteps: number;
  status: string;
  stepsResult: StepResult[];
  pendingApprovers?: ApproverRef[];
  createdAt: string;
  updatedAt: string;
};

export type OrgPermissionTemplate = {
  id: string;
  orgId: string;
  name: string;
  description: string;
  permissions: string[];
  isDefault: boolean;
  createdAt: string;
};

export type CollaborationGroup = {
  id: string;
  orgId: string;
  name: string;
  description: string;
  departments: DeptRef[];
  permissions: string[];
  memberCount: number;
  createdAt: string;
};

// ── 组织元数据（驱动前端下拉与权限勾选树） ──

export type OrgEnumOption = { value: string; label: string };
export type OrgPermissionItem = { code: string; name: string };
export type OrgPermissionGroup = { key: string; name: string; permissions: OrgPermissionItem[] };
export type OrgBuiltinRole = { key: string; name: string; description: string; level: number };

export type OrgMetadata = {
  builtinRoles: OrgBuiltinRole[];
  permissionCatalog: OrgPermissionGroup[];
  rolePermissions: Record<string, string[]>;
  orgKinds: OrgEnumOption[];
  deptKinds: OrgEnumOption[];
  orgStatuses: OrgEnumOption[];
  memberStatuses: OrgEnumOption[];
  approvalTriggers: OrgEnumOption[];
  approverTypes: OrgEnumOption[];
  deleteStrategies: OrgEnumOption[];
};

export type OrgImportIssue = {
  rowNo: number;
  field: string;
  value: string;
  message: string;
  fatal: boolean;
};

export type OrgImportResult = {
  dryRun: boolean;
  totalRows: number;
  deptCreated: number;
  memberAdded: number;
  memberUpdated: number;
  skipped: number;
  issues: OrgImportIssue[];
};

// ── 会话管理 ──

export type AdminSessionRecord = {
  id: string;
  adminId: number;
  ip: string;
  userAgent: string;
  device: string;
  issuedAt: string;
  expiresAt: string;
  lastActiveAt: string;
  isRevoked: boolean;
  revokedBy?: number | null;
  revokedAt?: string | null;
  adminAccount?: string;
  adminName?: string;
};

export type TempPermission = {
  id: number;
  adminId: number;
  permission: string;
  appId?: number | null;
  grantedBy: number;
  reason: string;
  expiresAt: string;
  isRevoked: boolean;
  createdAt: string;
  adminAccount?: string;
  granterName?: string;
};

export type AdminDelegation = {
  id: number;
  delegatorId: number;
  delegateId: number;
  scope: string;
  scopeId?: number | null;
  grantedBy: number;
  reason: string;
  expiresAt: string;
  isRevoked: boolean;
  createdAt: string;
  delegatorName?: string;
  delegateName?: string;
};

export type OnlineAdmin = {
  adminId: number;
  account: string;
  displayName: string;
  sessionCount: number;
  lastActiveAt: string;
};

// ── 存储资源 ──

export type StorageObject = {
  id: number;
  configId: number;
  appId?: number | null;
  objectKey: string;
  fileName: string;
  contentType: string;
  size: number;
  etag: string;
  uploadedBy?: number | null;
  uploaderType: string;
  status: string;
  metadata: Record<string, unknown>;
  createdAt: string;
  deletedAt?: string | null;
};

/** 由 object_key 的斜杠分段现算出的「目录」—— 存储服务本身没有目录实体 */
export type StorageObjectFolder = {
  name: string;
  /** 完整路径，可直接作为下一次查询的 folder */
  path: string;
  fileCount: number;
  totalSize: number;
};

/**
 * 当前筛选下的汇总。刻意**不含** status 条件：这一栏要同时说出
 * 「活跃多少 / 回收站多少」，带上 status 会让其中一档恒为 0。
 */
export type StorageObjectSummary = {
  totalFiles: number;
  totalSize: number;
  activeFiles: number;
  activeSize: number;
  deletedFiles: number;
  deletedSize: number;
};

export type StorageObjectListResult = {
  items: StorageObject[];
  folders: StorageObjectFolder[];
  total: number;
  page: number;
  limit: number;
  summary: StorageObjectSummary;
};

export type StorageObjectSort = "createdAt" | "deletedAt" | "size" | "fileName" | "objectKey";

export type StorageObjectQuery = {
  configId?: number;
  appId?: number;
  prefix?: string;
  folder?: string;
  folderView?: boolean;
  keyword?: string;
  contentType?: string;
  status?: string;
  uploaderType?: string;
  uploadedBy?: number;
  minSize?: number;
  maxSize?: number;
  createdFrom?: string;
  createdTo?: string;
  sort?: StorageObjectSort;
  order?: "asc" | "desc";
  page?: number;
  limit?: number;
};

export type StorageBatchAction = "delete" | "restore" | "purge";

export type StorageBatchResult = {
  action: StorageBatchAction;
  requested: number;
  affected: number;
  /** 请求了但状态不满足前置条件的条数（例如恢复一个本来就活跃的对象） */
  skipped: number;
};

export type StorageObjectAccessLink = {
  objectId: number;
  configId: number;
  provider: string;
  objectKey: string;
  url: string;
  download: boolean;
  contentType: string;
  expiresAt: string;
};

export type StorageRule = {
  id: number;
  configId?: number | null;
  appId?: number | null;
  name: string;
  ruleType: string;
  ruleData: Record<string, unknown>;
  isActive: boolean;
  createdAt: string;
};

export type StorageCDNConfig = {
  id: number;
  configId: number;
  cdnDomain: string;
  cdnProtocol: string;
  cacheMaxAge: number;
  refererWhitelist: string[];
  refererBlacklist: string[];
  ipWhitelist: string[];
  signUrlEnabled: boolean;
  signUrlSecret?: string;
  signUrlTtl: number;
  createdAt: string;
  updatedAt: string;
};

export type StorageImageRule = {
  id: number;
  configId?: number | null;
  name: string;
  ruleType: string;
  ruleData: Record<string, unknown>;
  isActive: boolean;
  createdAt: string;
};

export type StorageUsageSnapshot = {
  id: number;
  configId: number;
  appId?: number | null;
  totalFiles: number;
  totalSize: number;
  activeFiles: number;
  deletedFiles: number;
  snapshotAt: string;
};

export type StorageUsageStats = {
  configId: number;
  totalFiles: number;
  totalSize: number;
  activeFiles: number;
  deletedFiles: number;
  topTypes: Array<{ contentType: string; count: number; size: number }>;
};

// ── 用户主数据 ──

export type GlobalIdentity = {
  id: number;
  email?: string;
  phone?: string;
  displayName: string;
  status: string;
  riskScore: number;
  riskLevel: string;
  lifecycleState: string;
  lifecycleChangedAt: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string | null;
  tags?: UserMasterTag[];
  mappings?: IdentityUserMapping[];
};

export type IdentityUserMapping = {
  id: number;
  identityId: number;
  appId: number;
  userId: number;
  createdAt: string;
  appName?: string;
  account?: string;
  nickname?: string;
};

export type UserMasterTag = {
  id: number;
  name: string;
  color: string;
  description: string;
  createdBy?: number | null;
  createdAt: string;
};

export type UserMasterSegment = {
  id: number;
  name: string;
  description: string;
  segmentType: string;
  rules: Record<string, unknown>;
  memberCount: number;
  createdBy?: number | null;
  createdAt: string;
  updatedAt: string;
};

export type UserListEntry = {
  id: number;
  listType: string;
  identityId?: number | null;
  email?: string;
  phone?: string;
  ip?: string;
  reason: string;
  expiresAt?: string | null;
  createdBy?: number | null;
  createdAt: string;
};

export type UserAppeal = {
  id: number;
  identityId: number;
  appealType: string;
  reason: string;
  evidence: string;
  status: string;
  reviewerId?: number | null;
  reviewComment: string;
  reviewedAt?: string | null;
  createdAt: string;
  identityName?: string;
};

export type DeactivationRequest = {
  id: number;
  identityId: number;
  reason: string;
  coolingDays: number;
  scheduledAt: string;
  status: string;
  createdAt: string;
  identityName?: string;
};

// ── 风控中心 ──
//
// 枚举与参数 schema **不在这里硬编码**：它们由后端 `/risk/metadata` 下发
// （见 RiskMetadata）。后端新增一种条件类型时前端零改动即自动出现，
// 在前端另抄一份枚举会让那次新增变成一次静默的漏项。

/** 后端分页信封。所有管理端列表接口统一返回 `{ list, total }`。 */
export type RiskListEnvelope<T> = { list: T[] | null; total: number };

export type RiskCatalogEntry = {
  value: string;
  label: string;
  description?: string;
  /** 前端着色提示：neutral / info / warning / danger / success */
  tone?: string;
};

export type RiskLevelCatalog = RiskCatalogEntry & { minScore: number; maxScore?: number | null };

export type RiskFieldSchema = {
  key: string;
  label: string;
  /** number / text / textarea / select / list / bool / time */
  type: string;
  required?: boolean;
  default?: unknown;
  placeholder?: string;
  help?: string;
  min?: number | null;
  max?: number | null;
  options?: RiskCatalogEntry[];
};

export type RiskConditionCatalog = {
  value: string;
  label: string;
  description: string;
  group: string;
  fields: RiskFieldSchema[] | null;
  /** 依赖外部 IP 情报源；未配置时该条件永不命中，UI 要显式提示 */
  requiresProvider?: boolean;
  requiresRedis?: boolean;
};

export type RiskVariableCatalog = {
  name: string; type: string; group: string; description: string; example?: string;
};

export type RiskFunctionCatalog = {
  name: string; signature: string; description: string; example?: string;
};

export type RiskExprSample = { title: string; expression: string; note?: string };

export type RiskMetadata = {
  scenes: RiskCatalogEntry[];
  levels: RiskLevelCatalog[];
  actions: RiskCatalogEntry[];
  conditionTypes: RiskConditionCatalog[];
  variables: RiskVariableCatalog[];
  functions: RiskFunctionCatalog[];
  deviceTags: RiskCatalogEntry[];
  ipTags: RiskCatalogEntry[];
  samples: RiskExprSample[];
};

export type RiskExprValidation = { valid: boolean; error?: string; message?: string };

export type RiskRule = {
  id: number; name: string; description: string; scene: string;
  conditionType: string; conditionData: Record<string, unknown>;
  score: number; isActive: boolean; priority: number;
  hitCount: number; lastHitAt?: string | null;
  createdBy?: number | null; updatedBy?: number | null;
  createdAt: string; updatedAt: string;
};

export type RiskRuleInput = {
  name: string; description?: string; scene: string;
  conditionType: string; conditionData: Record<string, unknown>;
  score: number; priority: number; isActive?: boolean;
};

export type RiskMatchedRule = {
  ruleId: number; ruleName: string; conditionType?: string; score: number; reason?: string;
};

export type RiskRuleEvaluation = {
  ruleId: number; ruleName: string; conditionType: string;
  score: number; priority: number; hit: boolean; reason: string; error?: string;
};

export type RiskAssessment = {
  id: number; scene: string; appId?: number | null; userId?: number | null;
  identityId?: number | null; account: string; ip: string; deviceId: string;
  userAgent: string; country: string;
  totalScore: number; riskLevel: string;
  matchedRules: RiskMatchedRule[] | null;
  evalContext?: Record<string, unknown> | null;
  latencyMs: number;
  action: string; actionDetail: string;
  reviewed: boolean; reviewerId?: number | null; reviewerName?: string;
  reviewResult?: string; reviewComment: string;
  reviewedAt?: string | null; createdAt: string;
};

export type RiskEntitySummary = {
  totalAssessments: number; blocked: number;
  avgScore: number; maxScore: number;
  distinctAccounts: number; distinctDevices: number; distinctIps: number;
  firstSeenAt?: string | null; lastSeenAt?: string | null;
};

export type RiskAssessmentDetail = {
  assessment: RiskAssessment;
  device?: RiskDeviceFingerprint | null;
  ipRecord?: IPRiskRecord | null;
  rules: RiskRuleEvaluation[] | null;
  sameIp: RiskAssessment[] | null;
  sameDevice: RiskAssessment[] | null;
  sameAccount: RiskAssessment[] | null;
  ipSummary: RiskEntitySummary;
  deviceSummary: RiskEntitySummary;
};

export type RiskDeviceFingerprint = {
  id: number; deviceId: string; userId?: number | null; appId?: number | null;
  fingerprint: Record<string, unknown>; riskTag: string;
  lastIp: string; userAgent: string; note: string;
  firstSeenAt: string; lastSeenAt: string; seenCount: number;
};

export type RiskDeviceDetail = {
  device: RiskDeviceFingerprint;
  summary: RiskEntitySummary;
  recent: RiskAssessment[] | null;
  ips: string[] | null;
  accounts: string[] | null;
};

export type IPRiskRecord = {
  id: number; ip: string; riskTag: string; riskScore: number;
  country: string; region: string; isp: string; asn: string;
  source: string; note: string;
  isProxy: boolean; isVpn: boolean; isTor: boolean; isDatacenter: boolean;
  totalRequests: number; totalBlocks: number;
  firstSeenAt: string; lastSeenAt: string;
};

export type IPRiskDetail = {
  record: IPRiskRecord;
  summary: RiskEntitySummary;
  recent: RiskAssessment[] | null;
  devices: RiskDeviceFingerprint[] | null;
  accounts: string[] | null;
};

export type RiskAction = {
  id: number; scene: string; minScore: number; maxScore?: number | null;
  action: string; banDuration: number; description: string;
  isActive: boolean; createdAt: string;
};

export type RiskActionInput = {
  scene: string; minScore: number; maxScore?: number | null;
  action: string; banDuration: number; description?: string;
};

export type RiskDashboardRange = { start: string; end: string; bucket: string };

export type RiskSummary = {
  totalAssessments: number; totalBlocked: number; totalChallenged: number;
  totalReviews: number; pendingReviews: number; totalPassed: number;
  highRiskCount: number; blockRate: number; avgScore: number; maxScore: number;
  avgLatencyMs: number; distinctIps: number; distinctDevices: number; distinctAccounts: number;
  prevTotalAssessments: number; prevTotalBlocked: number;
  prevBlockRate: number; prevAvgScore: number;
};

export type RiskSeriesPoint = {
  time: string; total: number;
  normal: number; low: number; medium: number; high: number; critical: number;
  pass: number; captcha: number; review: number; block: number; ban: number;
  avgScore: number; blockRate: number;
};

export type RiskScoreBucket = { min: number; max: number; level: string; count: number };
export type RiskSceneStat = { scene: string; count: number; blocked: number; avgScore: number };
export type RiskLevelStat = { level: string; count: number };
export type RiskActionStat = { action: string; count: number };
export type RiskCountryStat = { country: string; count: number; blocked: number };

export type RiskRuleHitStat = {
  ruleId: number; ruleName: string; scene: string; conditionType: string;
  score: number; isActive: boolean; hits: number; blocked: number;
  scoreSum: number; lastHitAt?: string | null;
};

export type RiskIPHitStat = {
  ip: string; country: string; count: number; blocked: number;
  maxScore: number; avgScore: number; riskTag: string;
};

export type RiskDeviceHitStat = {
  deviceId: string; count: number; blocked: number;
  maxScore: number; avgScore: number; riskTag: string; accounts: number;
};

/** 引擎运行态。「大盘全是 0」有两种原因：真没风险，或根本没规则在跑。 */
export type RiskEngineStatus = {
  totalRules: number; activeRules: number;
  totalActions: number; activeActions: number;
  scenesCovered: string[] | null; scenesUncovered: string[] | null;
  ipProvider: string; ipProviderReady: boolean;
  rateLimitOn: boolean; cacheTtlSeconds: number;
};

export type RiskDashboard = {
  range: RiskDashboardRange;
  summary: RiskSummary;
  series: RiskSeriesPoint[] | null;
  sceneDistribution: RiskSceneStat[] | null;
  levelDistribution: RiskLevelStat[] | null;
  actionDistribution: RiskActionStat[] | null;
  scoreHistogram: RiskScoreBucket[] | null;
  topRules: RiskRuleHitStat[] | null;
  topIps: RiskIPHitStat[] | null;
  topDevices: RiskDeviceHitStat[] | null;
  topCountries: RiskCountryStat[] | null;
  engine: RiskEngineStatus;
};

export type RiskEvalResult = {
  assessmentId?: number;
  totalScore: number; riskLevel: string;
  matchedRules: RiskMatchedRule[] | null;
  action: string; actionDetail: string;
  latencyMs?: number;
  evalContext?: Record<string, unknown> | null;
  /** 含未命中的**全部**参评规则；只显示命中项会让人猜"另外几条为什么没中" */
  evaluatedRules?: RiskRuleEvaluation[] | null;
};

export type RiskRuleDetail = {
  rule: RiskRule;
  stats: RiskRuleHitStat;
  recentHits: RiskAssessment[] | null;
  series: Array<{ time: string; count: number }> | null;
  explanation: string;
};

export type RiskAssessmentQuery = {
  scene?: string; riskLevel?: string; action?: string;
  ip?: string; deviceId?: string; account?: string; keyword?: string;
  ruleId?: number; reviewed?: string;
  minScore?: number; maxScore?: number;
  start?: string; end?: string;
  page?: number; pageSize?: number;
};

export type RiskEntityQuery = {
  keyword?: string; tag?: string; onlyRisk?: string;
  page?: number; pageSize?: number;
};

export type RiskSimulatePayload = {
  scene: string; ip?: string; deviceId?: string; userAgent?: string; account?: string;
  appId?: number; ruleIds?: number[];
  draft?: { name?: string; scene?: string; conditionType: string; conditionData: Record<string, unknown>; score?: number };
  overrides?: Record<string, unknown>;
};

export type AdminDashboard = {
  pendingInvitations: number;
  pendingRoleApps: number;
  recentAuditLogs: Array<{
    action: string;
    detail: string;
    ip: string;
    createdAt: string;
  }>;
  alerts: Array<{
    type: string;
    title: string;
    detail: string;
    time: string;
  }>;
  stats: {
    totalApps: number;
    totalUsers: number;
    todayLogins: number;
    activeSessions: number;
  };
};

export type AvatarUploadResult = {
  avatar?: string;
  reference?: string;
  storage?: Record<string, unknown>;
};

export type AdminAvatarUploadResponse = {
  profile: AdminProfile;
  upload?: AvatarUploadResult;
};

export type RoleDefinition = {
  key: string;
  name: string;
  description?: string;
  level?: number;
  scope?: string;
  permissions?: string[];
  isCustom?: boolean;
  baseRole?: string;
  createdBy?: number | null;
};

export type PermissionItem = {
  code: string;
  name: string;
  description?: string; // "granted" 表示已授权
};

export type PermissionGroup = {
  key: string;
  name: string;
  permissions: PermissionItem[];
};

export type RoleWithPermissions = RoleDefinition & {
  permissionGroups: PermissionGroup[];
};

export type StorageConfig = {
  id: number;
  scope: string;
  appid?: number | null;
  provider: string;
  config_name: string;
  access_mode: string;
  enabled: boolean;
  is_default: boolean;
  proxy_download: boolean;
  base_url?: string;
  root_path?: string;
  description?: string;
  config_data?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};

export type StorageConfigDetail = StorageConfig & {
  [key: string]: unknown;
};

export type WorkflowStatistics = {
  appid: number;
  totalWorkflows: number;
  activeWorkflows: number;
  totalInstances: number;
  runningInstances: number;
  completedInstances: number;
  failedInstances: number;
  pendingTasks: number;
  statusCounts?: Record<string, number>;
};

export type WorkflowItem = {
  id: number;
  appid: number;
  name: string;
  description?: string;
  category?: string;
  status: string;
  version: number;
  createdAt?: string;
  updatedAt?: string;
  [key: string]: unknown;
};

export type WorkflowNode = {
  id: string;
  type: string;
  name: string;
  config?: Record<string, unknown>;
};

export type WorkflowEdge = {
  id?: string;
  source: string;
  target: string;
  condition?: string;
};

export type WorkflowDefinition = {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
};

export type WorkflowListResult = {
  items: WorkflowItem[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type WorkflowDetail = WorkflowItem & {
  definition?: WorkflowDefinition | Record<string, unknown>;
  triggerConfig?: Record<string, unknown>;
  uiConfig?: Record<string, unknown>;
  permissions?: unknown[];
};

export type WorkflowInstance = {
  id: number;
  workflowId?: number;
  workflowName?: string;
  instanceName?: string;
  status?: string;
  currentNode?: string;
  currentStep?: string;
  initiatorId?: number;
  initiatorAccount?: string;
  priority?: string | number;
  startedAt?: string;
  completedAt?: string;
  createdAt?: string;
  updatedAt?: string;
  [key: string]: unknown;
};

export type WorkflowInstancesResult = {
  items: WorkflowInstance[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type WorkflowLogEntry = {
  id?: number;
  level?: string;
  message?: string;
  nodeId?: string;
  nodeName?: string;
  createdAt?: string;
  timestamp?: string;
  [key: string]: unknown;
};

export type WorkflowTask = {
  id: number;
  workflow_id?: number;
  instance_id?: number;
  appid?: number;
  node_id?: string;
  name?: string;
  type?: string;
  status?: string;
  priority?: number;
  assigned_to?: number | null;
  input_data?: Record<string, unknown>;
  output_data?: Record<string, unknown>;
  form_schema?: Record<string, unknown>;
  comment?: string;
  due_at?: string | null;
  completed_at?: string | null;
  createdAt?: string;
  updatedAt?: string;
  [key: string]: unknown;
};

export type WorkflowTaskListResult = {
  items: WorkflowTask[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type WorkflowNodeType = {
  type: string;
  label: string;
  [key: string]: unknown;
};

export type WorkflowTemplate = {
  id: number;
  name?: string;
  description?: string;
  category?: string;
  isPublic?: boolean;
  createdAt?: string;
  updatedAt?: string;
  [key: string]: unknown;
};

export type WorkflowTemplatesResult = {
  items: WorkflowTemplate[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type PagedResult<T> = {
  items: T[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type LoginAuditItem = {
  id: number;
  userId?: number | null;
  appid?: number;
  account?: string;
  nickname?: string;
  loginType?: string;
  provider?: string;
  tokenJTI?: string;
  loginIP?: string;
  deviceID?: string;
  userAgent?: string;
  status?: string;
  metadata?: Record<string, unknown>;
  createdAt?: string;
  [key: string]: unknown;
};

export type SessionAuditItem = {
  id: number;
  userId?: number | null;
  appid?: number;
  account?: string;
  nickname?: string;
  tokenJTI?: string;
  eventType?: string;
  metadata?: Record<string, unknown>;
  createdAt?: string;
  [key: string]: unknown;
};

export type NotificationItem = {
  id: number;
  appid?: number;
  userId?: number | null;
  account?: string;
  nickname?: string;
  type?: string;
  title?: string;
  content?: string;
  level?: string;
  status?: string;
  readAt?: string | null;
  createdAt?: string;
  updatedAt?: string;
  metadata?: Record<string, unknown>;
  [key: string]: unknown;
};

export type AdminAppUserItem = {
  id: number;
  appid?: number;
  account?: string;
  nickname?: string;
  avatar?: string;
  email?: string;
  phone?: string;
  enabled?: boolean;
  integral?: number;
  experience?: number;
  registerIP?: string;
  registerTime?: string;
  registerProvince?: string;
  registerCity?: string;
  registerIsp?: string;
  vipExpireAt?: string | null;
  disabledReason?: string;
  disabledEndTime?: string | null;
  markcode?: string;
  createdAt?: string;
  updatedAt?: string;
  extra?: Record<string, unknown>;
  [key: string]: unknown;
};

export type AdminAppUserProfile = {
  userId: number;
  nickname?: string;
  avatar?: string;
  email?: string;
  phone?: string;
  birthday?: string | null;
  bio?: string;
  role?: string;
  markcode?: string;
  customId?: string;
  customIdCount?: number;
  inviteCode?: string;
  parentInviteAccount?: string;
  registerIp?: string;
  registerIsp?: string;
  registerProvince?: string;
  registerCity?: string;
  registerTime?: string | null;
  disabledReason?: string;
  contacts?: Array<{
    platform: string;
    value: string;
    label?: string;
  }>;
  extra?: Record<string, unknown>;
  updatedAt?: string;
};

export type AdminUserSettingsRecord = {
  userId: number;
  category: string;
  settings: Record<string, unknown>;
  version: number;
  isActive: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type AdminUserSettingsRecordInfo = {
  version: number;
  createdAt?: string;
  updatedAt?: string;
  isActive: boolean;
};

export type AdminUserSettingsView = {
  user?: {
    id: number;
    account?: string;
    nickname?: string;
    avatar?: string;
    email?: string;
  };
  settings?: Record<string, AdminUserSettingsRecord>;
  recordInfo?: Record<string, AdminUserSettingsRecordInfo>;
  categories?: string[];
  configuredCategories?: string[];
  missingCategories?: string[];
};

export type AdminAppUserSecurity = {
  hasPassword: boolean;
  twoFactorEnabled: boolean;
  twoFactorMethod?: string;
  passkeyEnabled: boolean;
  passwordStrengthScore?: number;
  passwordChangeRequired?: boolean;
  passwordChangedAt?: string | null;
  passwordExpiresAt?: string | null;
  oauth2Bindings?: number;
  oauth2Providers?: string[];
  twoFactor: TOTPStatus;
  recoveryCodes: RecoveryCodeSummary;
  passkeys: PasskeySummary;
  modules?: SecurityModuleStatus[];
};

export type AdminAppUserDetail = AdminAppUserItem & {
  profile?: AdminAppUserProfile;
  settings?: AdminUserSettingsView;
  security?: AdminAppUserSecurity;
};

// ── 账号封禁（app_user_bans）──────────────────────────
//
// 与 `users.enabled` / `disabledReason` 是两件事：后者是账号上的一个开关位，
// 前者是一条有起止、有操作人、有证据、可撤销的**处置记录**。
// 控制台上「限制用户」写的是开关位，「新建封禁」写的是这张表。

/** temporary=到期自动解除；permanent=永久，只能人工撤销 */
export type AccountBanType = "temporary" | "permanent";
/** login=只挡登录；all=挡登录 + 已签发会话 */
export type AccountBanScope = "login" | "all";
export type AccountBanStatus = "active" | "expired" | "revoked";

export type AdminUserBan = {
  id: number;
  appid: number;
  userId: number;
  banType: AccountBanType | string;
  banScope: AccountBanScope | string;
  status: AccountBanStatus | string;
  reason: string;
  evidence?: Record<string, unknown>;
  bannedByAdminId?: number | null;
  bannedByAdminName?: string;
  revokedByAdminId?: number | null;
  revokedByAdminName?: string;
  revokeReason?: string;
  startAt: string;
  endAt?: string | null;
  revokedAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type AdminUserBanListResult = {
  items?: AdminUserBan[] | null;
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type AdminUserBanCreateInput = {
  banType: AccountBanType;
  banScope: AccountBanScope;
  reason: string;
  evidence?: Record<string, unknown>;
  startAt?: string;
  endAt?: string | null;
};

// ── 用户钱包 ──────────────────────────
//
// 金额一律是**字符串**（后端 shopspring/decimal 序列化结果）。
// 转成 number 做展示以外的任何事都会丢精度，禁止在前端做加减。

export type UserWallet = {
  userId: number;
  appid: number;
  balance: string;
  frozen: string;
  totalRecharged: string;
  totalConsumed: string;
  createdAt?: string;
  updatedAt?: string;
};

export type WalletTransactionType =
  | "recharge"
  | "consume"
  | "refund"
  | "admin_adjust"
  | "vip_purchase"
  | "order_pay";

export type UserWalletTransaction = {
  id: number;
  transactionNo: string;
  userId: number;
  appid: number;
  type: WalletTransactionType | string;
  amount: string;
  balanceBefore: string;
  balanceAfter: string;
  relatedOrderNo?: string;
  title: string;
  remark?: string;
  operator?: string;
  clientIp?: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
};

export type UserWalletTransactionList = {
  items?: UserWalletTransaction[] | null;
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type WalletAdjustResult = {
  transaction: UserWalletTransaction;
  wallet: UserWallet;
  replayed?: boolean;
};

// ── 单用户审计（管理端）──────────────────────────
//
// 与应用级 `LoginAuditItem` 的区别：这一份按 user_id 精确过滤，
// 因此响应里不再重复 account / nickname。

export type UserLoginAuditRecord = {
  id: number;
  appid: number;
  loginType: string;
  provider?: string;
  tokenJti?: string;
  loginIp?: string;
  deviceId?: string;
  userAgent?: string;
  status: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
  // 以下位置字段由后端按 loginIp 实时解析（不落库：GeoIP 库会更新）
  country?: string;
  countryCode?: string;
  region?: string;
  city?: string;
  isp?: string;
  location?: string;
  latitude?: number;
  longitude?: number;
  isPrivate?: boolean;
};

export type UserLoginAuditList = {
  items?: UserLoginAuditRecord[] | null;
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type UserSessionAuditRecord = {
  id: number;
  appid: number;
  tokenJti?: string;
  eventType: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
};

export type UserSessionAuditList = {
  items?: UserSessionAuditRecord[] | null;
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type OnlineStats = Record<string, unknown>;
/** 一条实时连接的明细，与后端 realtime.PresenceConnection 对齐。 */
export type OnlinePresenceConnection = {
  connectionId: string;
  appid: number;
  userId: number;
  tokenId?: string;
  deviceId?: string;
  ip?: string;
  userAgent?: string;
  connectedAt: string;
  lastSeenAt: string;
  serverId?: string;
};

/**
 * 在线用户，与后端 realtime.AppOnlineUser 对齐。
 *
 * 这里曾经是 `Record<string, unknown>`，于是读一个后端根本不返回的字段
 * TypeScript 也不会报 —— 在线用户表的「用户」「IP」两列长期为空就是这么来的。
 * 保持具名类型，字段对不上时 typecheck 会直接拦下来。
 */
export type OnlineUserItem = {
  appid: number;
  userId: number;
  account?: string;
  nickname?: string;
  ip?: string;
  connections: number;
  connectedAt?: string;
  lastSeenAt: string;
  sampleConnection?: OnlinePresenceConnection;
  connectionSamples?: OnlinePresenceConnection[];
};

// ── 防火墙安全日志 ──────────────────────────

export type FirewallLogEntry = {
  id: number;
  requestId: string;
  ip: string;
  method: string;
  path: string;
  queryString: string;
  userAgent: string;
  headers?: Record<string, string>;
  reason: string;
  httpStatus: number;
  responseCode: number;
  wafRuleId?: number | null;
  wafAction?: string;
  wafData?: string;
  country: string;
  countryCode: string;
  region: string;
  city: string;
  isp: string;
  asn: string;
  timezone: string;
  latitude?: number | null;
  longitude?: number | null;
  severity: string;
  blockedAt: string;
};

export type FirewallLogPage = {
  items: FirewallLogEntry[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
};

export type FirewallRankedItem = {
  key: string;
  count: number;
};

export type FirewallTimeSeriesPoint = {
  time: string;
  count: number;
  critical: number;
  high: number;
  medium: number;
  low: number;
};

export type FirewallStats = {
  totalBlocked: number;
  topIPs: FirewallRankedItem[];
  topCountries: FirewallRankedItem[];
  topRules: FirewallRankedItem[];
  topPaths: FirewallRankedItem[];
  topReasons: FirewallRankedItem[];
  severityCounts: FirewallRankedItem[];
  timeSeries: FirewallTimeSeriesPoint[];
};

// ── IP 封禁 ──────────────────────────

export type IPBanMode =
  | "forbidden"
  | "silent_drop"
  | "connection_reset"
  | "tarpit"
  | "stealth_404"
  | "stealth_503"
  | "teapot"
  | "rate_choke"
  | "redirect"
  | "honeypot"
  | "fake_empty"
  | "random_error"
  | "slow_response"
  | "random_delay"
  | "gone"
  | "bandwidth_choke"
  | "zip_bomb"
  | "chunked_infinite"
  | "infinite_redirect"
  | "mirror_request"
  | "fake_login"
  | "random_garbage"
  | "cursed_headers"
  | "json_bomb"
  | "cookie_bomb"
  | "reverse_slowloris"
  | "";

export type GeoBanScope = "country" | "region" | "city" | "asn" | "isp";

export type GeoBanEntry = {
  id: number;
  scopeType: GeoBanScope;
  scopeValue: string;
  mode: IPBanMode;
  reason: string;
  enabled: boolean;
  createdBy?: number | null;
  createdAt: string;
  updatedAt: string;
  expiresAt?: string | null;
  matchCount: number;
  lastMatchAt?: string | null;
};

export type IPBanEntry = {
  id: number;
  ip: string;
  reason: string;
  source: string;
  mode: IPBanMode;
  triggerRule: string;
  severity: string;
  duration: number;
  expiresAt?: string | null;
  status: string;
  revokedBy?: number | null;
  revokedAt?: string | null;
  country: string;
  countryCode: string;
  region: string;
  city: string;
  isp: string;
  triggerCount: number;
  createdAt: string;
  updatedAt: string;
};

export type IPBanModeOption = {
  value: IPBanMode;
  label: string;
  description: string;
};

export type IPBanModesResponse = {
  default: IPBanMode;
  modes: IPBanModeOption[];
};

export type IPBanPage = {
  items: IPBanEntry[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
};

// ── 交易中心 ──

/** 钱包流水（管理端行：流水本体 + 账号信息） */
export type AdminWalletTransaction = {
  id: number;
  transactionNo: string;
  userId: number;
  appid: number;
  /** recharge / consume / refund / admin_adjust / vip_purchase / order_pay */
  type: string;
  /** 正数入账、负数出账 */
  amount: string;
  balanceBefore: string;
  balanceAfter: string;
  relatedOrderNo?: string;
  title: string;
  remark?: string;
  operator?: string;
  clientIp?: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
  account?: string;
  nickname?: string;
};

export type AdminWalletTransactionList = {
  items: AdminWalletTransaction[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

/** 按流水类型的聚合 */
export type WalletTypeStat = {
  type: string;
  count: number;
  amount: string;
};

/**
 * 钱包资金面板。
 *
 * 入账与出账分开：净额为零既可能是没有交易，也可能是充了一万又花了一万。
 * balance 是时点值（平台此刻的待兑付负债），不随时间窗变化。
 */
export type WalletStats = {
  totalIn: string;
  totalOut: string;
  net: string;
  count: number;
  userCount: number;
  balance: string;
  byType: WalletTypeStat[];
};

/** 订单口径的资金面板 */
export type OrderGroupStat = {
  key: string;
  count: number;
  amount: string;
};

export type OrderStats = {
  totalOrders: number;
  paidOrders: number;
  paidAmount: string;
  pendingOrders: number;
  pendingAmount: string;
  refundCount: number;
  refundedAmount: string;
  /** 实收净额 = 已支付 - 已成功退款 */
  netAmount: string;
  payerCount: number;
  byStatus: OrderGroupStat[];
  byMethod: OrderGroupStat[];
};

/** 一种凭证语言 */
export type PaymentReceiptLocale = {
  tag: string;
  name: string;
  nativeName: string;
  direction: string;
  script: string;
  default: boolean;
  /** 该语言需要中日韩字体 */
  needsFont: boolean;
  /** 当前环境能否真正输出该语言 */
  available: boolean;
};

/** 凭证能力自述：这台机器能不能出中文凭证 */
export type PaymentReceiptCapability = {
  locales: PaymentReceiptLocale[];
  defaultLocale: string;
  supportsCJK: boolean;
  fontStatus: string;
  fontNotes?: string[];
};

export type PaymentReceiptEmailResult = {
  to: string;
  locale: string;
  documentType: string;
  /** false 表示渠道不支持附件，只发了下载链接 */
  attached: boolean;
  downloadUrl?: string;
  linkExpiresAt: string;
  messageId?: string;
  localeFallback?: boolean;
};

/** 一个时间桶上的资金往来 */
export type CommerceTrendPoint = {
  /** 桶起始时刻（UTC, RFC3339） */
  bucket: string;
  /** 直接可展示的短标签（日 `03-21` / 月 `2026-03`），由服务端按粒度生成 */
  label: string;
  paidAmount: string;
  paidOrders: number;
  refundedAmount: string;
  /** 实收净额 = 已支付 - 已退款 */
  netAmount: string;
  walletIn: string;
  walletOut: string;
};

/** 交易趋势。粒度由服务端按跨度自动决定，前端不指定也不推断。 */
export type CommerceTrend = {
  /** day / week / month */
  bucket: string;
  points: CommerceTrendPoint[];
};

/** 交易概览首屏：订单 + 钱包 + 趋势 + 凭证能力一次取齐 */
export type CommerceOverview = {
  orders: OrderStats;
  wallet: WalletStats;
  trend: CommerceTrend;
  receipt: PaymentReceiptCapability;
};
