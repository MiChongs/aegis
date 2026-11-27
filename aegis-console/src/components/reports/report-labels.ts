/**
 * 报表里各维度键的展示名与数值格式。后端只给原始键（password、alipay、high …），
 * 展示名统一在这里翻译，找不到的原样显示。
 */

export const OTHER_KEY = "__other__";

const COMMON: Record<string, string> = {
  [OTHER_KEY]: "其他",
  unknown: "未知",
  "": "未知"
};

export const LOGIN_TYPE: Record<string, string> = {
  password: "密码登录",
  register: "注册",
  refresh: "令牌续期",
  sms: "短信验证码",
  email: "邮箱验证码",
  email_code: "邮箱验证码",
  oauth: "第三方登录",
  oauth2: "第三方登录",
  native_oauth: "第三方登录",
  qrcode: "扫码登录",
  qr: "扫码登录",
  scan: "扫码登录",
  cardkey: "卡密登录",
  passkey: "通行密钥",
  webauthn: "通行密钥",
  guest: "游客登录",
  device: "设备登录"
};

export const PROVIDER: Record<string, string> = {
  password: "账号密码",
  sms: "短信",
  email: "邮箱",
  cardkey: "卡密",
  admin: "后台创建或导入",
  qq: "QQ",
  wechat: "微信",
  weixin: "微信",
  github: "GitHub",
  google: "Google",
  apple: "Apple",
  microsoft: "Microsoft",
  gitee: "Gitee",
  dingtalk: "钉钉",
  feishu: "飞书",
  alipay: "支付宝",
  weibo: "微博",
  douyin: "抖音",
  passkey: "通行密钥"
};

export const FAILURE_REASON: Record<string, string> = {
  invalid_password: "密码错误",
  invalid_code: "验证码错误",
  user_disabled: "账号已停用",
  user_banned: "账号被封禁",
  rate_limited: "尝试过于频繁",
  risk_blocked: "风控拦截",
  captcha_failed: "人机验证未通过",
  user_not_found: "账号不存在"
};

export const PAYMENT_METHOD: Record<string, string> = {
  alipay: "支付宝",
  wxpay: "微信支付",
  wechat: "微信支付",
  qqpay: "QQ 钱包",
  unionpay: "云闪付",
  stripe: "Stripe",
  paypal: "PayPal",
  wallet: "钱包余额",
  usdt: "USDT",
  afdian: "爱发电",
  douyinpay: "抖音支付",
  epay: "易支付",
  bank: "网银"
};

export const ORDER_STATUS: Record<string, string> = {
  paid: "已支付",
  pending: "待支付",
  expired: "已过期",
  failed: "失败",
  cancelled: "已取消",
  closed: "已关闭",
  refunded: "已退款"
};

export const PLATFORM: Record<string, string> = {
  android: "Android",
  ios: "iOS",
  harmonyos: "HarmonyOS",
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
  web: "网页"
};

export const RISK_LEVEL: Record<string, string> = {
  normal: "正常",
  low: "低",
  medium: "中",
  high: "高",
  critical: "严重"
};

export const RISK_ACTION: Record<string, string> = {
  pass: "放行",
  captcha: "人机验证",
  review: "人工复核",
  block: "拦截",
  ban: "封禁"
};

export const RISK_SCENE: Record<string, string> = {
  login: "登录",
  register: "注册",
  api: "接口调用",
  payment: "支付",
  invite: "邀请",
  lottery: "抽奖",
  signin: "签到",
  sms: "短信"
};

export const SEVERITY: Record<string, string> = {
  info: "提示",
  low: "低",
  medium: "中",
  high: "高",
  critical: "严重"
};

export const PRIZE_KIND: Record<string, string> = {
  none: "谢谢参与",
  points: "积分",
  integral: "积分",
  experience: "经验",
  vip_plan: "会员套餐",
  vip_days: "会员天数",
  feature: "功能解锁",
  lottery_draws: "抽奖次数",
  manual: "人工发放",
  number_change: "改号卡",
  number_ticket: "靓号券",
  number_draw: "摇号次数",
  number_assign: "指定号码"
};

export const DRAW_SOURCE: Record<string, string> = {
  free: "免费次数",
  bonus: "奖励次数",
  points: "积分抽奖"
};

export const ACTIVITY_STATUS: Record<string, string> = {
  draft: "草稿",
  active: "进行中",
  paused: "已暂停",
  ended: "已结束"
};

export const AD_STATUS: Record<string, string> = {
  pending: "待校验",
  granted: "已发放",
  rejected: "已拒绝",
  shown: "已展示",
  clicked: "已点击",
  failed: "加载失败",
  skipped: "已跳过"
};

export const NOTIFICATION_LEVEL: Record<string, string> = {
  info: "普通",
  success: "成功",
  warning: "提醒",
  error: "重要",
  critical: "紧急"
};

export const NOTIFICATION_TYPE: Record<string, string> = {
  system: "系统",
  announcement: "公告",
  activity: "活动",
  security: "安全",
  payment: "支付",
  signin: "签到",
  invite: "邀请",
  vip: "会员",
  ticket: "工单",
  lottery: "抽奖",
  reward: "奖励"
};

export const POINT_CATEGORY: Record<string, string> = {
  signin: "签到",
  sign_in: "签到",
  lottery: "抽奖",
  invite: "邀请",
  cardkey: "卡密",
  card_key: "卡密",
  rewarded_ad: "激励广告",
  ad: "广告",
  points_store: "积分商城",
  store: "积分商城",
  admin: "后台调整",
  admin_adjust: "后台调整",
  ai_gateway: "AI 中转",
  task: "任务",
  vip: "会员"
};

export const SIGN_SOURCE: Record<string, string> = {
  app: "应用内",
  web: "网页",
  auto: "自动签到",
  api: "接口",
  admin: "后台补签"
};

export const INVITE_SOURCE: Record<string, string> = {
  register: "注册时填写",
  link: "邀请链接",
  code: "邀请码",
  qrcode: "二维码",
  admin: "后台绑定"
};

export const ACTIVE_SIGNAL: Record<string, string> = {
  login: "登录",
  session: "会话活动",
  signin: "签到"
};

export const WEEKDAY = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];

/** 一个维度键的展示名 */
export function labelOf(dict: Record<string, string> | undefined, key: string, fallback?: string) {
  if (key in COMMON) return COMMON[key];
  return dict?.[key] ?? dict?.[key.toLowerCase()] ?? fallback ?? key;
}

const integer = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 });
const decimal = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2 });
const money = new Intl.NumberFormat("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

export type ValueFormat = "int" | "decimal" | "percent" | "money" | "minutes" | "hours" | "days" | "ms";

export function formatValue(value: number | null | undefined, format: ValueFormat = "int") {
  if (value === null || value === undefined || Number.isNaN(value)) return "—";
  switch (format) {
    case "percent":
      return `${(value * 100).toFixed(Math.abs(value) < 0.1 && value !== 0 ? 2 : 1)}%`;
    case "money":
      return `¥${money.format(value)}`;
    case "decimal":
      return decimal.format(value);
    case "minutes":
      return formatMinutes(value);
    case "hours":
      return value < 48 ? `${decimal.format(value)} 小时` : `${decimal.format(value / 24)} 天`;
    case "days":
      return `${decimal.format(value)} 天`;
    case "ms":
      return `${decimal.format(value)} ms`;
    default:
      return integer.format(value);
  }
}

/** 紧凑写法：1.2万、3.4亿 */
export function formatCompact(value: number, format: ValueFormat = "int") {
  if (format === "percent") return formatValue(value, "percent");
  const abs = Math.abs(value);
  const prefix = format === "money" ? "¥" : "";
  if (abs >= 1e8) return `${prefix}${decimal.format(value / 1e8)}亿`;
  if (abs >= 1e4) return `${prefix}${decimal.format(value / 1e4)}万`;
  return format === "money" ? `¥${decimal.format(value)}` : decimal.format(value);
}

function formatMinutes(value: number) {
  if (value < 1) return `${Math.round(value * 60)} 秒`;
  if (value < 60) return `${decimal.format(value)} 分钟`;
  if (value < 60 * 48) return `${decimal.format(value / 60)} 小时`;
  return `${decimal.format(value / 1440)} 天`;
}

/** 桶标签的短写：日 10-21、周 10-20 起、月 2026-10 */
export function bucketLabel(bucket: string, granularity: string) {
  if (granularity === "month") return bucket;
  return bucket.length >= 10 ? bucket.slice(5) : bucket;
}
