"use client";

import { Loader2, Sparkles } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { VipFeature, VipPerk, VipSource } from "@/lib/api/vip";
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
  card_key: { label: "卡密核销", variant: "info" },
  ad_reward: { label: "看广告领取", variant: "info" },
  points_store: { label: "积分兑换", variant: "info" },
  invite: { label: "邀请奖励", variant: "info" },
  leaderboard: { label: "排行榜奖励", variant: "info" },
  lottery: { label: "抽奖", variant: "info" },
  item_grant: { label: "物品发放", variant: "info" },
  // 只出现在开通记录里：扣减天数是一条负时长的账，取消永久会员是一条 lifetime 留痕，
  // 都不是一段会员期。具体收回了什么看时长列（formatVipTerm）
  admin_revoke: { label: "收回", variant: "warning" },
  // 只出现在开通记录里：老系统的永久会员由迁移补记，判定时归为 unknown
  legacy_import: { label: "旧系统迁移", variant: "secondary" },
  // 开通永久时折掉存量的说明行（负时长），以及取消永久会员后还回的会员时长（正时长，是一段会员期）
  lifetime_convert: { label: "永久会员折算", variant: "warning" },
  // 老系统迁移进来的用户：到期时间是直接写进 users 的，账本里没有对应流水。
  // 谎报成某个具体渠道比说"不知道"更糟 —— 那会让对账的人去找一笔不存在的钱。
  unknown: { label: "来源未知", variant: "secondary" }
};

/** 接受 string：交易中心与用户详情走的是宽松类型层（`lib/api/types.ts`），渠道是裸字符串 */
export function vipSourceLabel(source?: VipSource | string) {
  return SOURCE_META[(source ?? "none") as VipSource]?.label ?? source ?? "—";
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
          </>
        ) : (
          <>
            <p className="font-medium">{feature?.name || tag}</p>
            {feature?.description ? (
              <p className="max-w-56 text-[10px] text-background/70">{feature.description}</p>
            ) : null}
            {disabled ? <p className="text-[10px] text-background/70">已停用</p> : null}
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

/**
 * 权益标签：自动生成的（AI 计费）与手填的补充标签用两种色调，一眼分得清哪些会随改价变。
 * 有说明（detail）时悬浮显示。标签只用于展示，与功能标识徽标（等宽字体的机器标识）刻意长得不一样。
 */
export function PerkTag({ perk, className }: { perk: VipPerk; className?: string }) {
  const auto = perk.source === "ai";
  const chip = (
    <span
      className={cn(
        "inline-flex max-w-full items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] leading-4",
        auto
          ? "border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-800/70 dark:bg-violet-950/60 dark:text-violet-300"
          : "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-800/70 dark:bg-sky-950/60 dark:text-sky-300",
        perk.detail ? "cursor-help" : null,
        className
      )}
    >
      {auto ? <Sparkles className="size-3 shrink-0" /> : null}
      <span className="truncate">{perk.label}</span>
    </span>
  );
  if (!perk.detail) return chip;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{chip}</TooltipTrigger>
      <TooltipContent side="top">
        <p className="font-medium">{perk.label}</p>
        <p className="max-w-56 text-[10px] text-background/70">{perk.detail}</p>
      </TooltipContent>
    </Tooltip>
  );
}

/** 一组权益标签（自动的在前、手填的在后）；为空时按 emptyHint 显示，emptyHint 为 null 时什么都不画 */
export function PerkTagList({
  perks,
  emptyHint = "暂无权益标签"
}: {
  perks?: Array<VipPerk | null | undefined> | null;
  emptyHint?: string | null;
}) {
  const list = (perks ?? []).filter((perk): perk is VipPerk => Boolean(perk?.label));
  if (list.length === 0) {
    return emptyHint ? <span className="text-[11px] text-muted-foreground">{emptyHint}</span> : null;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {list.map((perk) => (
        <PerkTag key={`${perk.source}:${perk.key}:${perk.label}`} perk={perk} />
      ))}
    </div>
  );
}

/** 功能标识多选（档位编辑、新建套餐时顺带建档位共用）。停用的标识照样可选，只是淡一些。 */
export function FeatureChecklist({
  catalog,
  value,
  onChange
}: {
  catalog: VipFeature[];
  value: string[];
  onChange: (next: string[]) => void;
}) {
  if (catalog.length === 0) {
    return (
      <p className="rounded-xl border border-dashed border-border px-3 py-4 text-center text-[11px] text-muted-foreground">
        暂无功能标识
      </p>
    );
  }
  const toggle = (tag: string) =>
    onChange(value.includes(tag) ? value.filter((item) => item !== tag) : [...value, tag]);
  return (
    <div className="space-y-1.5 rounded-xl border border-border p-2">
      {catalog.map((feature) => (
        <label
          key={feature.tag}
          className={cn(
            "flex cursor-pointer items-start gap-2.5 rounded-lg px-2 py-1.5 transition-colors hover:bg-muted/60",
            !feature.isActive && "opacity-60"
          )}
        >
          <Checkbox checked={value.includes(feature.tag)} onCheckedChange={() => toggle(feature.tag)} className="mt-0.5" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-xs font-medium">{feature.name}</span>
              <code className="rounded bg-muted px-1 text-[10px] text-muted-foreground">{feature.tag}</code>
              {!feature.isActive ? <span className="text-[10px] text-muted-foreground">已停用</span> : null}
            </div>
            {feature.description ? (
              <p className="truncate text-[10px] text-muted-foreground">{feature.description}</p>
            ) : null}
          </div>
        </label>
      ))}
    </div>
  );
}

/**
 * 套餐时长。永久套餐的 durationDays 恒为 0，写成「0 天」会被读成「白送的空套餐」。
 * 结构化入参：会员区块与用户详情 / 交易中心用的是两套类型层，字段同名。
 */
export function vipPlanTerm(plan: { lifetime?: boolean; durationDays: number }) {
  return plan.lifetime ? "永久" : `${plan.durationDays} 天`;
}

type VipTermRecord = {
  lifetime?: boolean;
  durationDays: number;
  payChannel: string;
  conversion?: { kind: string; points: number } | null;
};

/** 这条记录是不是一次永久开通（取消永久会员的留痕同样带 lifetime，要排除） */
export function isLifetimeGrant(txn: VipTermRecord) {
  return Boolean(txn.lifetime) && txn.payChannel !== "admin_revoke";
}

/**
 * 一条开通记录给会员时长带来的变化：永久 / 取消永久 / +N 天 / -N 天。
 * 扣减天数的记录 durationDays 本身就是负数。
 */
export function formatVipTerm(txn: VipTermRecord) {
  // 已拥有的发放被折成积分：没有记会员段，时长列说折了多少
  if (txn.conversion?.kind === "owned") return `折算 ${txn.conversion.points} 积分`;
  if (txn.lifetime) return txn.payChannel === "admin_revoke" ? "取消永久" : "永久";
  return txn.durationDays < 0 ? `-${Math.abs(txn.durationDays)} 天` : `+${txn.durationDays} 天`;
}

/**
 * 开通记录的「发到」。永久开通不动限时那条线，没有 expireAfter；
 * 取消永久会员的留痕也可能没有（用户没有限时会员时），那时只能是「—」而不是「永久」。
 */
export function formatVipExpireAfter(
  txn: VipTermRecord & { expireAfter?: string | null },
  format: (value: string) => string = formatVipDate
) {
  if (txn.expireAfter) return format(txn.expireAfter);
  return isLifetimeGrant(txn) ? "永久" : "—";
}

/** 待确认的收回动作：取消永久会员 / 扣减限时天数 */
export type VipRevokeAction = { lifetime: true } | { lifetime: false; days: number };

/**
 * 收回会员的确认弹窗（会员区块与用户详情共用，后果的说法只有一份）。
 *
 * 动作与开关由调用方分开存：关闭动画期间弹窗仍要显示刚才那个动作的文案，
 * 用「动作为空即关闭」的话，取消永久会员的弹窗会在淡出时闪成「扣减 0 天」。
 */
export function VipRevokeDialog({
  open,
  action,
  pending,
  onOpenChange,
  onConfirm
}: {
  open: boolean;
  action: VipRevokeAction;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {action.lifetime ? "取消永久会员？" : `扣减 ${action.days} 天会员？`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {action.lifetime
              ? "全部永久开通将作废，限时会员不受影响，费用不退还。"
              : `限时会员到期时间提前 ${action.days} 天，费用不退还。`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            确认
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
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
