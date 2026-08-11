"use client";

/**
 * 用户详情页的共享原语与**信号推导**。
 *
 * 设计要点（改这个文件前请先读）：
 *
 * 1. **字段不再一格一张卡。** 旧版每个字段都是 `rounded-2xl border px-4 py-3`，
 *    四十个字段就是四十张视觉权重完全相同的卡片，"账号"和"自定义 ID 次数"
 *    长得一模一样。这里换成 `<Facts>` 密集行表：标签定宽、值左对齐、行间发丝线。
 *    需要强调的东西靠 `<StatTile>` / 徽标 / 信号带，而不是靠给每个字段加边框。
 *
 * 2. **信号是推导出来的，不是让人自己扫的。** `deriveUserSignals()` 把
 *    「有没有生效封禁 / 账号是不是被限制 / 有没有可用的登录方式 / 密码过没过期 /
 *    恢复码还剩几个」这些散落在三层嵌套结构里的事实合成一条警示带。
 *    管理员打开页面第一眼看到的应该是结论，不是原始字段。
 *
 * 3. **金额一律当字符串处理。** 后端是 shopspring/decimal，转 number 会丢精度。
 *    这里只做展示格式化（补零、正负号），绝不做算术。
 */

import * as React from "react";
import { Ban, CircleAlert, Fingerprint, KeyRound, ShieldAlert, TriangleAlert } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import type {
  AdminAppUserDetail,
  AdminUserBan,
  UserWallet
} from "@/lib/api/types";

export const EMPTY = "—";

// ── 格式化 ──────────────────────────

/**
 * 解析时间串。
 *
 * 除了 null / 空串 / 非法值，还要把 Go 的**零值时间**（`0001-01-01T00:00:00Z`）
 * 当成"没有"。后端的 `time.Time` 字段没有 omitempty 时会如实序列化零值，
 * 直接格式化会在界面上显示"0001/01/01 00:00" —— 那不是数据，那是没数据。
 */
function parseTime(value?: string | null) {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return date.getUTCFullYear() <= 1 ? null : date;
}

export function isZeroTime(value?: string | null) {
  return Boolean(value) && parseTime(value) === null;
}

export function formatTime(value?: string | null) {
  const date = parseTime(value);
  if (!date) return EMPTY;
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

export function formatShortTime(value?: string | null) {
  const date = parseTime(value);
  if (!date) return EMPTY;
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

export function formatDate(value?: string | null) {
  const date = parseTime(value);
  if (!date) return EMPTY;
  return date.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

const UNITS: Array<[number, string]> = [
  [60_000, "分钟"],
  [3_600_000, "小时"],
  [86_400_000, "天"]
];

/** "3 天前" / "5 分钟后"。时间点本身仍由 title 属性给出，这里只做快速感知。 */
export function relativeTime(value?: string | null) {
  const date = parseTime(value);
  if (!date) return EMPTY;
  const diff = date.getTime() - Date.now();
  const abs = Math.abs(diff);
  if (abs < 60_000) return diff >= 0 ? "即将" : "刚刚";
  let text = "";
  if (abs < UNITS[1][0]) text = `${Math.round(abs / UNITS[0][0])} 分钟`;
  else if (abs < UNITS[2][0]) text = `${Math.round(abs / UNITS[1][0])} 小时`;
  else if (abs < 86_400_000 * 365) text = `${Math.round(abs / UNITS[2][0])} 天`;
  else text = `${(abs / (86_400_000 * 365)).toFixed(1)} 年`;
  return diff >= 0 ? `${text}后` : `${text}前`;
}

export function isPast(value?: string | null) {
  const date = parseTime(value);
  return date !== null && date.getTime() < Date.now();
}

export function textValue(value: unknown, fallback = EMPTY) {
  return typeof value === "string" && value.trim() ? value.trim() : fallback;
}

export function inputValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

export function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

export function numberText(value: unknown) {
  return typeof value === "number" && Number.isFinite(value)
    ? value.toLocaleString("zh-CN")
    : EMPTY;
}

export function boolText(value: boolean | undefined, trueText = "是", falseText = "否") {
  return value ? trueText : falseText;
}

/** 金额展示：字符串进、字符串出，永不经过 Number。 */
export function formatMoney(value?: string | null, fallback = "0.00") {
  const raw = typeof value === "string" ? value.trim() : "";
  if (!raw) return fallback;
  const negative = raw.startsWith("-");
  const digits = negative ? raw.slice(1) : raw.replace(/^\+/, "");
  const [intPart = "0", decPart = ""] = digits.split(".");
  const grouped = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const decimals = (decPart + "00").slice(0, 2);
  return `${negative ? "-" : ""}${grouped}.${decimals}`;
}

export function isNegativeAmount(value?: string | null) {
  return typeof value === "string" && value.trim().startsWith("-");
}

export function joinText(parts: Array<string | null | undefined>, fallback = EMPTY) {
  const filtered = parts.map((item) => textValue(item, "")).filter(Boolean);
  return filtered.length ? filtered.join(" · ") : fallback;
}

export function userInitials(nickname?: string | null, account?: string | null) {
  return String(nickname || account || "U")
    .trim()
    .slice(0, 2)
    .toUpperCase();
}

// ── 布局原语 ──────────────────────────

export function Panel({
  title,
  description,
  icon,
  action,
  children,
  className,
  bodyClassName
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  icon?: React.ReactNode;
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <Card className={cn("gap-0 overflow-hidden py-0", className)}>
      <CardHeader className="flex flex-row items-start justify-between gap-4 border-b px-5 py-4">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
            {icon ? <span className="text-muted-foreground">{icon}</span> : null}
            {title}
          </div>
          {description ? (
            <p className="text-xs leading-5 text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {action ? <div className="flex shrink-0 items-center gap-2">{action}</div> : null}
      </CardHeader>
      <CardContent className={cn("px-5 py-4", bodyClassName)}>{children}</CardContent>
    </Card>
  );
}

/**
 * 密集字段表。
 *
 * 分隔线画在每个 `<Fact>` 自己身上（而不是靠容器的 `divide-y`）——
 * 两列布局下 `divide-y` 的相邻兄弟选择器会把线画到错误的位置。
 * 单列时容器再把最后一行的线去掉，避免卡片底部多一道。
 */
export function Facts({
  children,
  columns = 1,
  className
}: {
  children: React.ReactNode;
  columns?: 1 | 2;
  className?: string;
}) {
  return (
    <dl
      className={cn(
        "grid grid-cols-1",
        columns === 2
          ? "sm:grid-cols-2 sm:gap-x-8"
          : "[&>*:last-child]:border-b-0",
        className
      )}
    >
      {children}
    </dl>
  );
}

export function Fact({
  label,
  value,
  icon,
  mono,
  hint,
  tone
}: {
  label: React.ReactNode;
  value?: React.ReactNode;
  icon?: React.ReactNode;
  mono?: boolean;
  hint?: React.ReactNode;
  tone?: "default" | "muted" | "danger" | "warning" | "success";
}) {
  const empty = value === null || value === undefined || value === "" || value === EMPTY;
  return (
    <div className="flex items-start justify-between gap-6 border-b border-border/70 py-2.5">
      <dt className="flex shrink-0 items-center gap-1.5 pt-px text-xs text-muted-foreground">
        {icon}
        {label}
      </dt>
      <dd className="min-w-0 text-right">
        <div
          className={cn(
            "text-sm break-all",
            mono && "font-mono text-[13px]",
            empty && "text-muted-foreground",
            tone === "muted" && "text-muted-foreground",
            tone === "danger" && "text-red-600 dark:text-red-400",
            tone === "warning" && "text-amber-600 dark:text-amber-400",
            tone === "success" && "text-emerald-600 dark:text-emerald-400"
          )}
        >
          {empty ? EMPTY : value}
        </div>
        {hint ? <div className="mt-0.5 text-[11px] text-muted-foreground">{hint}</div> : null}
      </dd>
    </div>
  );
}

export function StatTile({
  label,
  value,
  hint,
  icon,
  tone = "default"
}: {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  icon?: React.ReactNode;
  tone?: "default" | "danger" | "warning" | "success" | "info";
}) {
  return (
    <div
      className={cn(
        "rounded-2xl border px-4 py-3",
        tone === "danger" && "border-red-200 bg-red-50/60 dark:border-red-900/60 dark:bg-red-950/30",
        tone === "warning" &&
          "border-amber-200 bg-amber-50/60 dark:border-amber-900/60 dark:bg-amber-950/30",
        tone === "success" &&
          "border-emerald-200 bg-emerald-50/60 dark:border-emerald-900/60 dark:bg-emerald-950/30",
        tone === "info" && "border-sky-200 bg-sky-50/60 dark:border-sky-900/60 dark:bg-sky-950/30"
      )}
    >
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {icon}
        {label}
      </div>
      <div className="mt-1.5 truncate text-xl font-semibold tabular-nums text-foreground">
        {value}
      </div>
      {hint ? <div className="mt-1 truncate text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

export function EmptyRow({ text = "暂无记录" }: { text?: string }) {
  return <div className="py-10 text-center text-sm text-muted-foreground">{text}</div>;
}

/** 递归展开任意 JSON 值（附加字段、审计 metadata、封禁证据）。 */
export function ValueTree({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (value === null || value === undefined) {
    return <span className="text-sm text-muted-foreground">{EMPTY}</span>;
  }
  if (typeof value === "boolean") {
    return (
      <Badge variant={value ? "success" : "outline"} size="sm">
        {String(value)}
      </Badge>
    );
  }
  if (typeof value === "number") {
    return <span className="text-sm tabular-nums">{value}</span>;
  }
  if (typeof value === "string") {
    return <span className="text-sm break-all">{value.trim() || EMPTY}</span>;
  }
  if (Array.isArray(value)) {
    if (!value.length) return <span className="text-sm text-muted-foreground">{EMPTY}</span>;
    return (
      <div className="space-y-1.5">
        {value.map((item, index) => (
          <div key={index} className="rounded-lg bg-muted/40 px-2.5 py-1.5">
            <ValueTree value={item} depth={depth + 1} />
          </div>
        ))}
      </div>
    );
  }
  const entries = Object.entries(value as Record<string, unknown>);
  if (!entries.length) return <span className="text-sm text-muted-foreground">{EMPTY}</span>;
  return (
    <div className={cn("space-y-1", depth > 0 && "border-l pl-3")}>
      {entries.map(([key, entry]) => (
        <div key={key} className="flex items-start justify-between gap-4">
          <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{key}</span>
          <div className="min-w-0 text-right">
            <ValueTree value={entry} depth={depth + 1} />
          </div>
        </div>
      ))}
    </div>
  );
}

// ── 领域徽标 ──────────────────────────

export const BAN_TYPE_LABEL: Record<string, string> = {
  temporary: "临时",
  permanent: "永久"
};

export const BAN_SCOPE_LABEL: Record<string, string> = {
  login: "仅登录",
  all: "全部访问"
};

export const BAN_STATUS_LABEL: Record<string, string> = {
  active: "生效中",
  expired: "已到期",
  revoked: "已撤销"
};

export function BanStatusBadge({ status }: { status?: string }) {
  const variant =
    status === "active" ? "danger" : status === "revoked" ? "outline" : "secondary";
  return (
    <Badge variant={variant} size="sm">
      {BAN_STATUS_LABEL[status ?? ""] ?? status ?? EMPTY}
    </Badge>
  );
}

export const WALLET_TXN_LABEL: Record<string, string> = {
  recharge: "充值",
  consume: "消费",
  refund: "退款",
  admin_adjust: "管理员调整",
  vip_purchase: "购买会员",
  order_pay: "订单支付"
};

export const LOGIN_STATUS_LABEL: Record<string, string> = {
  success: "成功",
  failed: "失败",
  blocked: "被拦截",
  challenge: "需二次验证"
};

export function LoginStatusBadge({ status }: { status?: string }) {
  const ok = status === "success";
  return (
    <Badge variant={ok ? "success" : status === "blocked" ? "danger" : "warning"} size="sm">
      {LOGIN_STATUS_LABEL[status ?? ""] ?? status ?? EMPTY}
    </Badge>
  );
}

export const SESSION_EVENT_LABEL: Record<string, string> = {
  issued: "签发",
  refreshed: "刷新",
  revoked: "撤销",
  revoked_by_admin: "管理员撤销",
  revoked_all_by_user: "用户全部下线",
  expired: "过期",
  replaced: "被顶替",
  logout: "登出"
};

/** UA 粗解析。只用于列表里的一句话摘要，完整串仍在 tooltip / title 里。 */
export function describeUserAgent(ua?: string | null) {
  if (!ua) return { label: "未知客户端", mobile: false };
  const mobile = /mobile|android|iphone|ipad/i.test(ua);
  const browser = ua.match(/(Chrome|Firefox|Safari|Edge|Opera|Brave|Arc|okhttp|Dalvik)[/ ]?[\d.]*/i);
  const os = ua.match(/(Windows NT|Mac OS X|Linux|Android|iOS|iPhone OS)\s?[\d._]*/i);
  const label = [os?.[0]?.replace(/_/g, "."), browser?.[0]].filter(Boolean).join(" · ");
  return { label: label || (mobile ? "移动客户端" : "桌面客户端"), mobile };
}

// ── 信号推导 ──────────────────────────

export type SignalTone = "danger" | "warning";

export type UserSignal = {
  id: string;
  tone: SignalTone;
  icon: React.ReactNode;
  title: string;
  detail: string;
  /** 点进去能处理这件事的页签 */
  tab?: string;
  tabLabel?: string;
};

/** 密码强度门槛，与后端默认策略 `passwordPolicy.minScore` 的默认值一致。 */
const WEAK_PASSWORD_SCORE = 40;

/**
 * 把用户详情里散落的事实合成一组「现在有什么问题」。
 *
 * 收录标准有两条，缺一不可：
 *
 *   **可行动** —— 说得出是什么、为什么、去哪处理。
 *     纯陈述性的状态（「已开启二次验证」）不进这里，那是概览页的事。
 *
 *   **罕见** —— 只在异常时出现。
 *     一条对大多数用户都会亮的信号（「会员已过期」「设置分类未初始化」）
 *     会把整条警示带训练成背景噪音，那时真正的封禁提示也一起被跳过了。
 *     这类信息放它本来该在的页签里（会员在概览与资产、设置缺失在资料页的徽标）。
 *
 * 因此这里只有 danger 与 warning 两档，没有 info —— 这条带子的含义就是「有问题」。
 */
export function deriveUserSignals(
  user: AdminAppUserDetail | undefined,
  activeBan: AdminUserBan | null | undefined
): UserSignal[] {
  if (!user) return [];
  const signals: UserSignal[] = [];
  const security = user.security;

  if (activeBan) {
    const permanent = activeBan.banType === "permanent";
    signals.push({
      id: "ban-active",
      tone: "danger",
      icon: <Ban className="size-4" />,
      title: `账号处于封禁中（${BAN_SCOPE_LABEL[activeBan.banScope] ?? activeBan.banScope}）`,
      detail: [
        textValue(activeBan.reason, "未填写封禁原因"),
        permanent ? "永久封禁，需人工撤销" : `到期时间 ${formatTime(activeBan.endAt)}`,
        activeBan.bannedByAdminName ? `操作人 ${activeBan.bannedByAdminName}` : ""
      ]
        .filter(Boolean)
        .join(" · "),
      tab: "governance",
      tabLabel: "去处置"
    });
  }

  if (user.enabled === false) {
    signals.push({
      id: "disabled",
      tone: "danger",
      icon: <ShieldAlert className="size-4" />,
      title: "账号已被限制",
      detail: [
        textValue(user.disabledReason, "未填写限制原因"),
        user.disabledEndTime ? `解除时间 ${formatTime(user.disabledEndTime)}` : "未设置解除时间"
      ].join(" · "),
      tab: "governance",
      tabLabel: "去处置"
    });
  }

  // 三种登录凭据全无 = 这个账号谁也登不进去，包括本人。
  const bindings = numberValue(security?.oauth2Bindings);
  if (security && !security.hasPassword && !security.passkeyEnabled && bindings === 0) {
    signals.push({
      id: "no-credential",
      tone: "danger",
      icon: <KeyRound className="size-4" />,
      title: "该账号没有任何可用的登录方式",
      detail: "既未设置密码，也没有 Passkey 与第三方绑定。需要管理员重置密码后才能登录。",
      tab: "security",
      tabLabel: "去安全"
    });
  }

  if (security?.passwordExpiresAt && isPast(security.passwordExpiresAt)) {
    signals.push({
      id: "password-expired",
      tone: "warning",
      icon: <TriangleAlert className="size-4" />,
      title: "密码已过期",
      detail: `过期于 ${formatTime(security.passwordExpiresAt)}，下次登录会被要求改密。`,
      tab: "security",
      tabLabel: "去安全"
    });
  } else if (security?.passwordChangeRequired) {
    signals.push({
      id: "password-change-required",
      tone: "warning",
      icon: <TriangleAlert className="size-4" />,
      title: "已标记为必须修改密码",
      detail: "用户下次登录时会被强制进入改密流程。",
      tab: "security",
      tabLabel: "去安全"
    });
  }

  const score = security?.passwordStrengthScore;
  if (security?.hasPassword && typeof score === "number" && score > 0 && score < WEAK_PASSWORD_SCORE) {
    signals.push({
      id: "weak-password",
      tone: "warning",
      icon: <CircleAlert className="size-4" />,
      title: `密码强度偏低（${score} 分）`,
      detail: `低于默认策略门槛 ${WEAK_PASSWORD_SCORE} 分。分值是设置密码时算的，策略调严不会追溯已有密码。`,
      tab: "security",
      tabLabel: "去安全"
    });
  }

  if (security?.twoFactor?.pendingSetup) {
    signals.push({
      id: "2fa-pending",
      tone: "warning",
      icon: <Fingerprint className="size-4" />,
      title: "二次验证配置未完成",
      detail: "已生成密钥但从未验证成功，这个状态下 2FA 并未真正生效。",
      tab: "security",
      tabLabel: "去安全"
    });
  }

  const codes = security?.recoveryCodes;
  if (codes?.enabled && typeof codes.remaining === "number" && codes.remaining <= 2) {
    signals.push({
      id: "recovery-low",
      tone: "warning",
      icon: <CircleAlert className="size-4" />,
      title: `恢复码仅剩 ${codes.remaining} 个`,
      detail: "用尽后若同时丢失二次验证设备，账号将无法自助找回。",
      tab: "security",
      tabLabel: "去安全"
    });
  }

  return signals;
}

/**
 * 钱包数据是否可用。
 *
 * 注意后端**从不返回 404**：没有 `user_wallets` 行时它拼一个零值钱包回来
 * （刻意不为只读请求建行）。所以这里判的是"这次请求拿到了数据"，
 * 不是"这个用户开过户"—— 后者从这个接口分辨不出来，界面上就不要假装能分辨。
 * 唯一的旁证是 `createdAt` 为 Go 零值时间，`isZeroTime()` 能识别。
 */
export function hasWallet(wallet: UserWallet | undefined | null) {
  return Boolean(wallet && typeof wallet.balance === "string");
}
