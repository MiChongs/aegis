import type { FeatureKind, GraphNodeKind, ListKind, ListType, RiskAction, RiskEntity, RiskLevel } from "@/lib/api/risk";

/** 风控平台的展示文案（一处定义，全部页面共用） */

export const ACTION_LABEL: Record<RiskAction, string> = {
  pass: "放行", review: "人工复核", captcha: "图形验证码", block: "拦截", ban: "封禁",
};

export const ACTION_TONE: Record<RiskAction, "success" | "info" | "warning" | "danger"> = {
  pass: "success", review: "info", captcha: "warning", block: "danger", ban: "danger",
};

/** 图表配色：处置从轻到重 */
export const ACTION_COLOR: Record<RiskAction, string> = {
  pass: "var(--chart-2)", review: "var(--chart-1)", captcha: "var(--chart-4)", block: "var(--chart-5)", ban: "var(--destructive)",
};

export const ACTION_ORDER: RiskAction[] = ["pass", "review", "captcha", "block", "ban"];

export const LEVEL_LABEL: Record<RiskLevel, string> = {
  none: "无风险", low: "低", medium: "中", high: "高", critical: "严重",
};

export const LEVEL_TONE: Record<RiskLevel, "secondary" | "info" | "warning" | "danger"> = {
  none: "secondary", low: "info", medium: "warning", high: "danger", critical: "danger",
};

export const ENTITY_LABEL: Record<RiskEntity, string> = {
  user: "用户", account: "账号", device: "设备", ip: "IP", ip_subnet: "网段", phone: "手机号", email_domain: "邮箱域名", app: "应用",
};

export const LIST_TYPE_LABEL: Record<ListType, string> = {
  ip: "IP", cidr: "网段", device: "设备", account: "账号", user: "用户 ID", email_domain: "邮箱域名",
  phone_prefix: "手机号段", asn: "ASN", country: "国家", ua_keyword: "UA 关键词",
};

export const LIST_TYPE_HINT: Record<ListType, string> = {
  ip: "203.0.113.7", cidr: "203.0.113.0/24", device: "设备 ID", account: "登录名、邮箱或手机号", user: "数字用户 ID",
  email_domain: "mailinator.com（含子域）", phone_prefix: "1700000（前缀匹配）", asn: "AS4134 或 4134", country: "两位 ISO 代码，如 US",
  ua_keyword: "包含即命中，如 python-requests",
};

export const KIND_LABEL: Record<ListKind, string> = { block: "黑名单", allow: "白名单", watch: "观察名单" };
export const KIND_TONE: Record<ListKind, "danger" | "success" | "warning"> = { block: "danger", allow: "success", watch: "warning" };

export const FEATURE_KIND_LABEL: Record<FeatureKind, string> = {
  velocity: "实时滑窗", stream: "流式聚合", graph: "关系图", vector: "相似设备", derived: "派生",
};

export const FEATURE_KIND_HINT: Record<FeatureKind, string> = {
  velocity: "决策服务在 Redis Cluster 里维护的滑窗计数，本次请求算在内",
  stream: "RisingWave 从事件流聚合，适合长窗口与业务结果（失败、支付、退款）",
  graph: "NebulaGraph 关系查询：同设备 / 同 IP 账号、被封邻居、邀请树",
  vector: "Milvus 设备指纹相似检索：改机与模拟器批量",
  derived: "用表达式组合请求字段与其它特征",
};

export const DIM_LABEL: Record<string, string> = {
  network: "网络", device: "设备", account: "账号", behavior: "行为", geo: "地理", client: "客户端",
  list: "名单", graph: "关系", model: "模型", custom: "自定义",
};

export const GRAPH_TEMPLATE_LABEL: Record<string, string> = {
  device_accounts: "同设备的其他账号数", ip_accounts: "同 IP 的其他账号数", phone_accounts: "同手机号的其他账号数",
  banned_neighbors: "两跳内被封账号数", invite_children: "直接邀请人数", invite_tree: "三层邀请树规模",
  device_users: "设备上出现过的账号数", ip_users: "IP 上出现过的账号数",
};

export const NODE_KIND_LABEL: Record<GraphNodeKind, string> = {
  user: "用户", account: "登录名", device: "设备", ip: "IP", subnet: "网段", phone: "手机号", email: "邮箱",
  identity: "第三方账号", payer: "付款账号", passkey: "通行密钥", client: "授权应用",
};

export const EDGE_LABEL: Record<string, string> = {
  uses_device: "使用设备", uses_ip: "使用 IP", in_subnet: "属于网段", has_phone: "绑定手机", has_email: "绑定邮箱",
  account_of: "登录名", invited: "邀请", targets: "交互对象",
  registered_from: "注册 IP", registered_on: "注册设备", bound_identity: "绑定第三方账号", paid_with: "付款账号",
  has_passkey: "通行密钥", authorized: "授权应用",
};

export const STAGE_LABEL: Record<string, string> = {
  fetch: "名单与特征", derived: "派生特征", models: "模型", rules: "规则", flow: "决策流", policy: "策略",
};

export const SOURCE_LABEL: Record<string, string> = {
  lists: "名单", velocity: "滑窗计数", stream: "流式特征", graph: "关系图", vector: "相似设备",
};

export const DECIDED_BY_LABEL: Record<string, string> = {
  list: "名单", rule: "规则", flow: "决策流", policy: "策略", default: "默认", no_release: "场景未发布", scope: "不在场景应用范围",
};

export const EVENT_LABEL: Record<string, string> = {
  decision: "风控决策", "login.success": "登录成功", "login.fail": "登录失败", "register.success": "注册成功",
  "password.reset": "重置密码", "payment.paid": "支付成功", "payment.refund": "退款", "invite.bind": "邀请绑定",
  signin: "签到", "lottery.draw": "抽奖", "cardkey.redeem": "卡密兑换", "points.redeem": "积分兑换", "ad.reward": "激励广告发奖",
  "profile.update": "资料变更", "account.ban": "账号封禁", "account.unban": "账号解封", "verify_code.sent": "验证码发送",
  "cep.alert": "行为模式告警", "login.locked": "登录防爆破锁定", "geo.anomaly": "登录地理异常",
};

export const SEVERITY_LABEL: Record<string, string> = { low: "低", medium: "中", high: "高", critical: "严重" };

/** 秒 → 「10 分钟」「1 天」 */
export function formatWindow(seconds: number): string {
  if (!seconds) return "永久";
  const units: Array<[number, string]> = [[86400, "天"], [3600, "小时"], [60, "分钟"], [1, "秒"]];
  for (const [n, label] of units) {
    if (seconds % n === 0) return `${seconds / n} ${label}`;
  }
  return `${seconds} 秒`;
}

/** 微秒 → 「0.42 ms」 */
export function formatMicros(us: number): string {
  if (us >= 1000_000) return `${(us / 1000_000).toFixed(2)} s`;
  if (us >= 1000) return `${(us / 1000).toFixed(us >= 10_000 ? 0 : 1)} ms`;
  return `${Math.round(us)} µs`;
}

export const WINDOW_PRESETS: Array<{ label: string; seconds: number }> = [
  { label: "1 分钟", seconds: 60 }, { label: "10 分钟", seconds: 600 }, { label: "1 小时", seconds: 3600 },
  { label: "6 小时", seconds: 21600 }, { label: "1 天", seconds: 86400 }, { label: "7 天", seconds: 604800 }, { label: "30 天", seconds: 2592000 },
];

export const DURATION_PRESETS: Array<{ label: string; seconds: number }> = [
  { label: "1 小时", seconds: 3600 }, { label: "1 天", seconds: 86400 }, { label: "7 天", seconds: 604800 },
  { label: "30 天", seconds: 2592000 }, { label: "永久", seconds: 0 },
];
