import {
  BarChart3,
  Bug,
  Cloud,
  CreditCard,
  Crown,
  Dices,
  Fingerprint,
  Gift,
  Headphones,
  Info,
  KeyRound,
  Mail,
  MonitorSmartphone,
  Milestone,
  MonitorPlay,
  PackagePlus,
  Network,
  PlugZap,
  Route,
  ScanFace,
  ScrollText,
  ShieldCheck,
  ShieldHalf,
  ShoppingBag,
  SlidersHorizontal,
  Sparkles,
  Ticket,
  Trophy,
  UserCog,
  UserPlus,
  UserX,
  type LucideIcon
} from "lucide-react";

/**
 * 应用配置区块的**唯一目录**。
 *
 * 这份表同时驱动三处，缺少任何一处的同步都会让入口与内容对不上：
 *
 * | 消费方 | 用途 |
 * |---|---|
 * | `/apps/[appKey]` 详情页左侧子导航 | 分组渲染与高亮 |
 * | `lib/navigation.ts` 侧边栏三级子项 | 深链 `/apps?tab=xxx` |
 * | `/apps` 列表页卡片快捷入口 | 直达某个区块 |
 *
 * `key` 就是 URL 上的 `?tab=` 值，**不可改名** —— 已经分享出去的深链不该因为
 * 显示名调整而失效。`policy` 与 `auth-protocol` 两个键即为此保留旧名。
 */

export type AppSection = {
  /** URL `?tab=` 值，同时是区块唯一标识 */
  key: string;
  title: string;
  /** 一句话说明：子导航第二行与列表页 tooltip 共用，避免同一区块两套措辞 */
  summary: string;
  icon: LucideIcon;
};

export type AppSectionGroup = {
  key: string;
  title: string;
  sections: AppSection[];
};

export const appSectionGroups: AppSectionGroup[] = [
  {
    key: "overview",
    title: "概览",
    sections: [
      { key: "info", title: "基本信息", summary: "标识、开关与关联入口", icon: Info },
      { key: "stats", title: "统计分析", summary: "用户规模与增长趋势", icon: BarChart3 },
      { key: "audit", title: "审计日志", summary: "登录、会话与通知记录", icon: ScrollText },
      { key: "crash-reports", title: "崩溃报告", summary: "客户端上报的崩溃与排查记录", icon: Bug }
    ]
  },
  {
    key: "auth",
    title: "认证与安全",
    sections: [
      // tab 键沿用 policy：已分享出去的深链不该因为改叫法而失效
      { key: "policy", title: "认证与会话", summary: "登录绑定与注册准入", icon: Fingerprint },
      { key: "device-limit", title: "登录设备", summary: "设备数上限、超限处置与在线设备", icon: MonitorSmartphone },
      { key: "password", title: "密码与凭证", summary: "强度规则与过期策略", icon: KeyRound },
      { key: "passkey", title: "通行密钥", summary: "RP ID、网页来源与安卓应用", icon: ScanFace },
      { key: "captcha", title: "验证码", summary: "人机校验渠道与场景", icon: ShieldCheck },
      { key: "oauth", title: "第三方登录", summary: "OAuth 渠道与自动建号", icon: KeyRound },
      { key: "account-setup", title: "账号初始化", summary: "快速登录账号补设用户名与密码", icon: UserCog },
      { key: "oauth2-server", title: "OAuth2 授权服务", summary: "对外登录、客户端与令牌", icon: Network },
      // 同理沿用 auth-protocol
      { key: "auth-protocol", title: "接入", summary: "安全等级、密钥与自检", icon: PlugZap }
    ]
  },
  {
    key: "service",
    title: "服务能力",
    sections: [
      { key: "email", title: "邮件", summary: "发信出口与模板", icon: Mail },
      { key: "ai", title: "AI 服务", summary: "模型通道、技能与 MCP", icon: Sparkles },
      { key: "ai-gateway", title: "AI 中转", summary: "用户按量调用、模型价格与密钥", icon: Route },
      { key: "payment", title: "支付", summary: "收款渠道、订单与交易设置", icon: CreditCard },
      { key: "cloud-storage", title: "云存储", summary: "用户数据云端备份与配额", icon: Cloud },
      { key: "listen-together", title: "一起听", summary: "多人同步收听：服务状态、房间与成员", icon: Headphones },
      { key: "settings", title: "用户设置", summary: "资料字段与默认值", icon: SlidersHorizontal }
    ]
  },
  {
    key: "growth",
    title: "用户运营",
    sections: [
      { key: "vip", title: "会员", summary: "套餐、试用与功能权益", icon: Crown },
      { key: "card-key", title: "卡密", summary: "授权卡与兑换卡", icon: Ticket },
      { key: "signin-reward", title: "签到奖励", summary: "连签规则与奖励发放", icon: Gift },
      { key: "points-store", title: "积分商城", summary: "积分兑换会员、功能与福利", icon: ShoppingBag },
      { key: "levels", title: "等级体系", summary: "画布配置等级、晋级条件、达成奖励与权益", icon: Milestone },
      { key: "invite", title: "邀请", summary: "邀请码、双方奖励与邀请记录", icon: UserPlus },
      { key: "leaderboard", title: "排行榜", summary: "积分榜单、周期结算与名次奖励", icon: Trophy },
      { key: "account-deletion", title: "账号注销", summary: "注销方式、审核与重新注册限制", icon: UserX },
      { key: "lottery", title: "抽奖", summary: "九宫格与转盘、抽奖次数与保底", icon: Dices },
      { key: "item-grant", title: "物品发放", summary: "画布规则：手动、定时与事件触发发放", icon: PackagePlus },
      { key: "rewarded-ad", title: "激励广告", summary: "看广告领奖励、回调验签与限次", icon: MonitorPlay },
      { key: "ad-policy", title: "广告服务", summary: "同意要求、基础服务与开屏广告", icon: ShieldHalf }
    ]
  }
];

/** 摊平后的全部区块，供校验与查找 */
export const appSections: AppSection[] = appSectionGroups.flatMap((group) => group.sections);

/** 无 `?tab=` 时落到的区块 */
export const DEFAULT_APP_SECTION = appSections[0].key;

const SECTION_MAP = new Map(appSections.map((section) => [section.key, section]));

export function findAppSection(key?: string | null): AppSection | undefined {
  return key ? SECTION_MAP.get(key) : undefined;
}

/** 把任意输入收敛成合法区块键（非法值一律回落到默认区块） */
export function resolveAppSection(key?: string | null): string {
  return key && SECTION_MAP.has(key) ? key : DEFAULT_APP_SECTION;
}

/** 应用详情页某区块的链接 */
export function appSectionHref(appKey: string, section?: string | null): string {
  const resolved = resolveAppSection(section);
  return resolved === DEFAULT_APP_SECTION
    ? `/apps/${encodeURIComponent(appKey)}`
    : `/apps/${encodeURIComponent(appKey)}?tab=${resolved}`;
}
