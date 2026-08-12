"use client";

import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { VipFeature, VipSource, VipTrialReason } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

/**
 * 会员域的**展示口径**：来源、渠道、试用判据、时长格式化。
 *
 * 集中在一处的理由与交易中心的 `commerce-format.tsx` 相同：同一个 `payment_order`
 * 在套餐页显示「在线支付」、在记录页显示「支付订单」的话，看的人无从判断
 * 这是不是两件事。枚举文案与后端 `internal/domain/vip` 的常量一一对应。
 */

/** 会员来源 → 中文名 + 徽标语义色 */
const SOURCE_META: Record<VipSource, { label: string; variant: "success" | "info" | "warning" | "secondary" }> = {
  none: { label: "非会员", variant: "secondary" },
  trial: { label: "试用", variant: "warning" },
  wallet: { label: "余额购买", variant: "success" },
  payment_order: { label: "在线支付", variant: "success" },
  admin_grant: { label: "管理员授予", variant: "info" },
  // 老系统迁移进来的用户：到期时间是直接写进 users 的，账本里没有对应流水。
  // 谎报成某个具体渠道比说"不知道"更糟 —— 那会让对账的人去找一笔不存在的钱。
  unknown: { label: "来源未知", variant: "secondary" }
};

export function vipSourceLabel(source?: VipSource) {
  return SOURCE_META[source ?? "none"]?.label ?? source ?? "—";
}

export function VipSourceBadge({ source }: { source?: VipSource }) {
  const meta = SOURCE_META[source ?? "none"] ?? SOURCE_META.none;
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 试用资格判据 → 说明 + 该怎么办 */
export const TRIAL_REASON_META: Record<VipTrialReason, { label: string; hint: string }> = {
  eligible: { label: "可领取", hint: "当前满足全部资格条件" },
  not_configured: { label: "未开放", hint: "该应用没有启用中的试用套餐，客户端应当整个隐藏入口" },
  already_claimed: { label: "已领过", hint: "试用一人一次；确需再给一次走「恢复资格」" },
  member_active: { label: "已是会员", hint: "会员期内领试用只是把到期时间再往后推，那不是试用" },
  device_claimed: { label: "设备已用", hint: "该套餐开了设备维度去重，这台设备已经有人领过" },
  device_required: { label: "缺设备标识", hint: "开了设备去重，但请求没带设备标识 —— 放行等于这个开关不存在" }
};

/** 会员状态徽标：是不是会员 / 是不是试用，一眼分清 */
export function VipStatusBadge({ isVip, isTrial }: { isVip: boolean; isTrial: boolean }) {
  if (!isVip) {
    return (
      <Badge variant="secondary" size="sm">
        非会员
      </Badge>
    );
  }
  return (
    <Badge variant={isTrial ? "warning" : "success"} size="sm">
      {isTrial ? "试用会员" : "正式会员"}
    </Badge>
  );
}

/**
 * 功能标识徽标。
 *
 * 悬浮显示目录里的展示名 —— 标识本身（`ai.chat`）是给机器看的，
 * 而看这张表的人多半只记得「AI 对话」。目录里查不到的标识标成悬空引用：
 * 那意味着套餐上留着一个删掉的功能，新开通的用户不会拿到它。
 */
export function FeatureTag({
  tag,
  catalog,
  className
}: {
  tag: string;
  catalog?: VipFeature[];
  className?: string;
}) {
  const feature = catalog?.find((item) => item.tag === tag);
  const dangling = Boolean(catalog) && !feature;
  const disabled = Boolean(feature && !feature.isActive);

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            "inline-flex max-w-full items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[10px] leading-4",
            dangling
              ? "border-destructive/40 bg-destructive/10 text-destructive"
              : disabled
                ? "border-border bg-muted text-muted-foreground line-through"
                : "border-border bg-muted/60 text-foreground",
            className
          )}
        >
          <span className="truncate">{tag}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent side="top">
        {dangling ? (
          <>
            <p className="font-medium">未登记的功能标识</p>
            <p className="text-[10px] text-background/70">目录里没有它，新开通的用户不会拿到这项权益</p>
          </>
        ) : (
          <>
            <p className="font-medium">{feature?.name || tag}</p>
            {feature?.description ? (
              <p className="max-w-56 text-[10px] text-background/70">{feature.description}</p>
            ) : null}
            {disabled ? <p className="text-[10px] text-background/70">已停用：校验一律判不通过</p> : null}
          </>
        )}
      </TooltipContent>
    </Tooltip>
  );
}

/** 一组功能标识；为空时说清「只是会员」而不是留一片空白 */
export function FeatureTagList({
  tags,
  catalog,
  emptyHint = "无细分权益"
}: {
  tags?: string[];
  catalog?: VipFeature[];
  emptyHint?: string;
}) {
  if (!tags?.length) {
    return <span className="text-[11px] text-muted-foreground">{emptyHint}</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {tags.map((tag) => (
        <FeatureTag key={tag} tag={tag} catalog={catalog} />
      ))}
    </div>
  );
}

/** 金额展示：后端给的是字符串，这里只做格式化，不做任何算术 */
export function formatVipPrice(price?: string) {
  const text = (price ?? "").trim();
  if (!text) return "—";
  return Number(text) === 0 ? "免费" : `¥ ${text}`;
}

export function formatVipDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

/**
 * 剩余时长。
 *
 * 不足一天时给到小时 —— 「还剩 0 天」既像已经过期又像马上过期，
 * 而这两种情况客服要说的话完全不同。
 */
export function formatRemaining(seconds?: number) {
  const value = Math.max(0, Math.floor(seconds ?? 0));
  if (value <= 0) return "已到期";
  const days = Math.floor(value / 86400);
  if (days >= 1) return `${days} 天`;
  const hours = Math.floor(value / 3600);
  if (hours >= 1) return `${hours} 小时`;
  return `${Math.max(1, Math.floor(value / 60))} 分钟`;
}

/**
 * 这段试用是否仍在有效期内。
 *
 * 写成独立函数而不是在组件里直接比 `Date.now()`：后者是渲染期的不纯调用
 * （`react-hooks/purity` 会拒绝）。这里的时间只用于**展示派生**，
 * 下一次重渲染刷新即可，不参与任何写操作的判定 —— 真正的判定在服务端。
 */
export function isTrialActive(endsAt?: string) {
  if (!endsAt) return false;
  const end = new Date(endsAt).getTime();
  return Number.isFinite(end) && end > Date.now();
}

/** 试用转化率。分母为 0 时显示「—」而不是 0% —— 没人领过与领了没人转化是两回事 */
export function formatConversion(converted: number, total: number) {
  if (!total) return "—";
  return `${((converted / total) * 100).toFixed(total >= 100 ? 1 : 0)}%`;
}
