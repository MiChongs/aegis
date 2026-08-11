import {
  BarChart3,
  CreditCard,
  Dices,
  Fingerprint,
  Gift,
  Info,
  KeyRound,
  Mail,
  PlugZap,
  ScrollText,
  ShieldCheck,
  SlidersHorizontal,
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
      { key: "audit", title: "审计日志", summary: "登录、会话与通知记录", icon: ScrollText }
    ]
  },
  {
    key: "auth",
    title: "认证与安全",
    sections: [
      // tab 键沿用 policy：已分享出去的深链不该因为改叫法而失效
      { key: "policy", title: "认证与会话", summary: "登录校验与多端策略", icon: Fingerprint },
      { key: "password", title: "密码与凭证", summary: "强度规则与过期策略", icon: KeyRound },
      { key: "captcha", title: "验证码", summary: "人机校验渠道与场景", icon: ShieldCheck },
      { key: "oauth", title: "第三方登录", summary: "OAuth 渠道与自动建号", icon: KeyRound },
      // 同理沿用 auth-protocol
      { key: "auth-protocol", title: "接入", summary: "安全等级、密钥与自检", icon: PlugZap }
    ]
  },
  {
    key: "service",
    title: "服务能力",
    sections: [
      { key: "email", title: "邮件", summary: "发信出口与模板", icon: Mail },
      { key: "payment", title: "支付", summary: "渠道市场与交易参数", icon: CreditCard },
      { key: "settings", title: "用户设置", summary: "资料字段与默认值", icon: SlidersHorizontal }
    ]
  },
  {
    key: "growth",
    title: "用户运营",
    sections: [
      { key: "signin-reward", title: "签到奖励", summary: "连签规则与奖励发放", icon: Gift },
      { key: "lottery", title: "抽奖", summary: "奖池、概率与限次", icon: Dices }
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
