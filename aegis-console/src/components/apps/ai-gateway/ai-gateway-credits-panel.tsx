"use client";

import { useState } from "react";
import {
  Ban,
  CalendarClock,
  Coins,
  Crown,
  Gift,
  Hourglass,
  Infinity as InfinityIcon,
  Layers,
  Loader2,
  Minus,
  Package,
  Plus,
  ReceiptText,
  UserRound,
  Wallet
} from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  BUCKET_LABEL,
  CREDIT_KIND_LABEL,
  FactorChain,
  LOT_ORIGIN_LABEL,
  LotStatusBadge,
  WindowHitLine,
  discountLabel,
  formatRate,
  formatYuan,
  useNow
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { SOURCE_LABEL, formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { StatTile } from "@/components/content/content-shared";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  useAdjustAIGatewayCreditMutation,
  useAIGatewayLotsQuery,
  useAIGatewayUserAccountQuery,
  useAIGatewayUserCreditsQuery,
  useGrantAIGatewayLotMutation,
  useRevokeAIGatewayLotMutation
} from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayCreditLot, AIGatewayOverview } from "@/lib/api/ai-gateway";
import { formatDate, formatDateTime, formatDateTimeMinute, parseInstant } from "@/lib/datetime";
import { cn } from "@/lib/utils";

const AMOUNTS = ["100", "500", "1000", "5000"];
const REASONS = ["客服补偿", "活动奖励", "调用异常补偿", "误发收回"];
const VALIDITY = [7, 30, 90, 365];
const LOT_LIMIT = 10;
const DAY_MS = 86_400_000;

/**
 * 某个用户的 AI 额度（docs/ai-billing.md）：现在能用多少（与用户自己看到的总览同一份）、
 * 额度包批次（剩余 / 暂扣 / 到期，可作废）、发放限期额度与调整永久额度、额度流水。
 *
 * 调整用「方向 + 恒为正数的数量」，不用负数表示扣减（交易中心「别让管理员当输入机」那一节）。
 * 每次提交带一个请求编号，网络重试不会加两次。
 */
export function AIGatewayCreditsPanel({
  appKey,
  onOpenRequests
}: {
  appKey: string;
  onOpenRequests: (filter: { userId?: number }) => void;
}) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [page, setPage] = useState(1);
  const [lotScope, setLotScope] = useState<"active" | "all">("active");
  const [lotPage, setLotPage] = useState(1);
  const now = useNow(60_000);

  const accountQuery = useAIGatewayUserAccountQuery(appKey, user?.id);
  const creditsQuery = useAIGatewayUserCreditsQuery(appKey, user?.id ?? null, page);
  const lotsQuery = useAIGatewayLotsQuery(
    appKey,
    { userId: user?.id, status: lotScope === "active" ? "active" : undefined, page: lotPage, limit: LOT_LIMIT },
    Boolean(user)
  );
  const account = accountQuery.data;
  const entries = creditsQuery.data?.items ?? [];
  const totalPages = Math.max(1, Math.ceil((creditsQuery.data?.total ?? 0) / 20));
  const lots = lotsQuery.data?.items ?? [];
  const lotPages = Math.max(1, Math.ceil((lotsQuery.data?.total ?? 0) / LOT_LIMIT));

  const pickUser = (value: PickedUser | null) => {
    setUser(value);
    setPage(1);
    setLotPage(1);
  };

  return (
    <div className="space-y-4">
      <SectionCard icon={<UserRound className="size-4" />} title="用户">
        <div className="flex flex-wrap items-center gap-2">
          <div className="w-72">
            <UserPicker appKey={appKey} value={user} onChange={pickUser} />
          </div>
          {user ? (
            <Button size="sm" variant="outline" onClick={() => onOpenRequests({ userId: user.id })}>
              <ReceiptText className="size-3.5" />
              调用记录
            </Button>
          ) : null}
        </div>
      </SectionCard>

      {!user ? null : accountQuery.isLoading ? (
        <Skeleton className="h-40 w-full rounded-2xl" />
      ) : account ? (
        <>
          <AccountSummary account={account} now={now} />

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
            <SectionCard
              icon={<Layers className="size-4" />}
              title="额度批次"
              description="先到期的先用；永久批次排在限期批次之后"
              aside={
                <ToggleGroup
                  type="single"
                  size="sm"
                  value={lotScope}
                  onValueChange={(value) => {
                    if (!value) return;
                    setLotScope(value as "active" | "all");
                    setLotPage(1);
                  }}
                >
                  <ToggleGroupItem value="active" className="h-7 px-2.5 text-xs">可用</ToggleGroupItem>
                  <ToggleGroupItem value="all" className="h-7 px-2.5 text-xs">全部</ToggleGroupItem>
                </ToggleGroup>
              }
            >
              {lotsQuery.isLoading ? (
                <Skeleton className="h-32 w-full rounded-xl" />
              ) : lots.length === 0 ? (
                <p className="py-8 text-center text-xs text-muted-foreground">
                  {lotScope === "active" ? "没有可用的额度批次" : "还没有额度批次"}
                </p>
              ) : (
                <div className="space-y-2">
                  {lots.map((lot) => (
                    <LotRow key={lot.id} appKey={appKey} lot={lot} now={now} />
                  ))}
                </div>
              )}
              {lotPages > 1 ? (
                <Pager page={lotPage} pages={lotPages} onChange={setLotPage} />
              ) : null}
            </SectionCard>

            <GrantCard appKey={appKey} userId={user.id} />
          </div>

          <SectionCard icon={<ReceiptText className="size-4" />} title="额度流水" description="永久额度与额度包的每一笔变动">
            {creditsQuery.isLoading ? (
              <Skeleton className="h-40 w-full rounded-xl" />
            ) : entries.length === 0 ? (
              <p className="py-8 text-center text-xs text-muted-foreground">暂无流水</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>类型</TableHead>
                    <TableHead>账户</TableHead>
                    <TableHead className="text-right">变动</TableHead>
                    <TableHead className="text-right">该账户余额</TableHead>
                    <TableHead className="text-right">合计</TableHead>
                    <TableHead>说明</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {entries.map((entry) => {
                    const amount = Number(entry.amount);
                    return (
                      <TableRow key={entry.id}>
                        <TableCell className="whitespace-nowrap text-xs tabular-nums">{formatDateTime(entry.createdAt)}</TableCell>
                        <TableCell className="text-xs">
                          <KindBadge kind={entry.kind} />
                        </TableCell>
                        <TableCell className="whitespace-nowrap text-xs">
                          {entry.bucket ? BUCKET_LABEL[entry.bucket] ?? entry.bucket : "永久额度"}
                          {entry.lotId ? <span className="ml-1 font-mono text-[10px] text-muted-foreground">#{entry.lotId}</span> : null}
                        </TableCell>
                        <TableCell className={cn("text-right font-mono text-xs tabular-nums", amount > 0 ? "text-emerald-600 dark:text-emerald-400" : amount < 0 ? "text-rose-600 dark:text-rose-400" : "")}>
                          {amount > 0 ? "+" : ""}
                          {formatPoints(entry.amount)}
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs tabular-nums">{formatPoints(entry.balanceAfter)}</TableCell>
                        <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                          {entry.totalAfter ? formatPoints(entry.totalAfter) : "—"}
                        </TableCell>
                        <TableCell className="max-w-64 truncate text-xs text-muted-foreground">
                          {entry.remark}
                          {entry.operator ? ` · ${entry.operator}` : ""}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )}
            {totalPages > 1 ? <Pager page={page} pages={totalPages} onChange={setPage} /> : null}
          </SectionCard>
        </>
      ) : null}
    </div>
  );
}

const KIND_TONE: Record<string, string> = {
  grant: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  welcome: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  purchase: "bg-violet-500/10 text-violet-700 dark:text-violet-300",
  refund: "bg-sky-500/10 text-sky-700 dark:text-sky-300",
  consume: "bg-muted text-foreground",
  adjust: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  expire: "bg-stone-500/10 text-stone-600 dark:text-stone-300",
  revoke: "bg-rose-500/10 text-rose-700 dark:text-rose-300",
  order_refund: "bg-rose-500/10 text-rose-700 dark:text-rose-300"
};

function KindBadge({ kind }: { kind: string }) {
  return (
    <span className={cn("inline-flex rounded-md px-1.5 py-px text-[11px]", KIND_TONE[kind] ?? "bg-muted")}>
      {CREDIT_KIND_LABEL[kind as keyof typeof CREDIT_KIND_LABEL] ?? kind}
    </span>
  );
}

function Pager({ page, pages, onChange }: { page: number; pages: number; onChange: (page: number) => void }) {
  return (
    <div className="mt-3 flex items-center justify-end gap-2 text-xs">
      <Button size="xs" variant="outline" disabled={page <= 1} onClick={() => onChange(page - 1)}>上一页</Button>
      <span className="tabular-nums text-muted-foreground">{page} / {pages}</span>
      <Button size="xs" variant="outline" disabled={page >= pages} onClick={() => onChange(page + 1)}>下一页</Button>
    </div>
  );
}

function AccountSummary({ account, now }: { account: AIGatewayOverview; now: number }) {
  const pack = Number(account.pack ?? 0);
  const packFrozen = Number(account.packFrozen ?? 0);
  const credit = Number(account.credit ?? 0);
  const creditFrozen = Number(account.creditFrozen ?? 0);
  const balance = Number(account.balance ?? pack + credit);
  const barTotal = pack + packFrozen + credit + creditFrozen || 1;
  const expiring = account.expiring;
  const expiringIn = expiring ? Math.ceil(((parseInstant(expiring.at)?.getTime() ?? now) - now) / DAY_MS) : null;
  const member = account.member;

  return (
    <>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          label="现在可用"
          icon={Coins}
          tone="positive"
          value={formatPoints(account.available)}
          hint={`含周期额度与余额折算 · 倍率 ${account.factor}`}
        />
        <StatTile
          label="额度包"
          icon={Package}
          value={formatPoints(account.pack ?? "0")}
          hint={
            expiring && Number(expiring.amount) > 0
              ? `${formatPoints(expiring.amount)} 将在 ${expiringIn !== null && expiringIn <= 1 ? "1 天内" : `${expiringIn} 天后`}到期`
              : packFrozen > 0
                ? `暂扣中 ${formatPoints(packFrozen)}`
                : "没有 7 天内到期的"
          }
        />
        <StatTile
          label="永久额度"
          icon={InfinityIcon}
          value={formatPoints(account.credit)}
          hint={creditFrozen > 0 ? `暂扣中 ${formatPoints(creditFrozen)}` : "广告、活动、调用退款"}
        />
        <StatTile
          label="今日已用"
          icon={Hourglass}
          value={formatPoints(account.todaySpent)}
          hint={`累计 ${formatPoints(account.totalSpent)} · ${account.totalRequests} 次`}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-3 rounded-2xl border border-border bg-card p-4">
          <div className="flex items-baseline justify-between gap-2">
            <span className="text-xs text-muted-foreground">AI 额度合计（不含暂扣）</span>
            <span className="font-mono text-xl font-semibold tabular-nums">{formatPoints(balance)}</span>
          </div>
          <div className="flex h-2.5 overflow-hidden rounded-full bg-muted">
            <span className="bg-violet-500" style={{ width: `${(pack / barTotal) * 100}%` }} title={`额度包 ${formatPoints(pack)}`} />
            <span className="bg-violet-500/40" style={{ width: `${(packFrozen / barTotal) * 100}%` }} title={`额度包暂扣 ${formatPoints(packFrozen)}`} />
            <span className="bg-emerald-500" style={{ width: `${(credit / barTotal) * 100}%` }} title={`永久额度 ${formatPoints(credit)}`} />
            <span className="bg-emerald-500/40" style={{ width: `${(creditFrozen / barTotal) * 100}%` }} title={`永久额度暂扣 ${formatPoints(creditFrozen)}`} />
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
            <Legend className="bg-violet-500" label="额度包" />
            <Legend className="bg-emerald-500" label="永久额度" />
            <Legend className="bg-muted-foreground/30" label="浅色为暂扣中" />
          </div>
          <div className="space-y-2 border-t border-dashed pt-3">
            {account.quotas.map((quota) => {
              const limit = Number(quota.limit);
              const used = Number(quota.used) + Number(quota.reserved);
              return (
                <div key={quota.source} className={cn("grid grid-cols-[5.5rem_minmax(0,1fr)_auto] items-center gap-2 text-[11px]", !quota.eligible && "opacity-50")}>
                  <span className="text-muted-foreground">{SOURCE_LABEL[quota.source] ?? quota.label}</span>
                  <span className="h-1.5 overflow-hidden rounded-full bg-muted">
                    <span className="block h-full rounded-full bg-sky-500" style={{ width: `${limit > 0 ? Math.min(100, ((limit - used) / limit) * 100) : 0}%` }} />
                  </span>
                  <span className="font-mono tabular-nums">
                    {formatPoints(quota.remaining)} / {formatPoints(quota.limit)}
                  </span>
                </div>
              );
            })}
            <div className="flex items-center justify-between text-[11px]">
              <span className="flex items-center gap-1.5 text-muted-foreground">
                <Wallet className="size-3" />
                钱包余额（直付 {account.walletRate} 元 / 额度）
              </span>
              <span className="font-mono tabular-nums">{formatYuan(account.wallet)}</span>
            </div>
          </div>
        </div>

        <div className="space-y-3 rounded-2xl border border-border bg-card p-4 text-xs">
          <div className="flex flex-wrap items-center gap-2">
            {account.profile.isVip ? (
              <Badge variant="warning" size="sm" className="gap-1">
                <Crown className="size-3" />
                {account.profile.tierName || member?.name || "会员"}
              </Badge>
            ) : (
              <Badge variant="outline" size="sm">非会员</Badge>
            )}
            <span className="text-muted-foreground">Lv.{account.profile.level}</span>
            {!account.eligible ? <span className="text-amber-600 dark:text-amber-400">仅会员可用 AI</span> : null}
            {account.welcome ? (
              <span className="ml-auto flex items-center gap-1 text-[11px] text-muted-foreground">
                <Gift className="size-3" />
                {account.welcome.claimedAt
                  ? `新人礼已领 ${formatDate(account.welcome.claimedAt)}`
                  : account.welcome.claimable
                    ? "新人礼可领"
                    : account.welcome.enabled
                      ? "新人礼未领"
                      : "新人礼未开放"}
              </span>
            ) : null}
          </div>
          {account.pricing ? (
            <div className="space-y-1.5">
              <div className="text-[11px] text-muted-foreground">此刻的倍率</div>
              <FactorChain breakdown={account.pricing} size="sm" />
              <div className="text-[11px]">
                <WindowHitLine breakdown={account.pricing} />
              </div>
            </div>
          ) : null}
          {member && member.source !== "none" && member.source !== "" ? (
            <div className="grid grid-cols-3 gap-2 border-t border-dashed pt-3">
              <MiniStat label="调用价" value={discountLabel(member.multiplier)} />
              <MiniStat label="额度包" value={`${discountLabel(member.packDiscount)}${Number(member.packBonusRate) > 0 ? ` +${formatRate(member.packBonusRate)}` : ""}`} />
              <MiniStat label="限速" value={`${account.rpm} 次/分 · ${account.maxConcurrent}`} />
              <MiniStat label="会员每日" value={formatPoints(member.daily)} />
              <MiniStat label="会员每月" value={formatPoints(member.monthly)} />
              <MiniStat label="优惠来源" value={member.source === "tier" ? "档位配置" : "通用会员"} />
            </div>
          ) : (
            <p className="border-t border-dashed pt-3 text-[11px] text-muted-foreground">
              无会员优惠 · 限速 {account.rpm} 次/分、{account.maxConcurrent} 并发
            </p>
          )}
        </div>
      </div>
    </>
  );
}

function Legend({ className, label }: { className: string; label: string }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className={cn("size-2 rounded-sm", className)} />
      {label}
    </span>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-muted/50 px-2 py-1.5">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="truncate font-medium">{value}</div>
    </div>
  );
}

function LotRow({ appKey, lot, now }: { appKey: string; lot: AIGatewayCreditLot; now: number }) {
  const revokeMutation = useRevokeAIGatewayLotMutation(appKey);
  const [confirming, setConfirming] = useState(false);
  const [reason, setReason] = useState("");
  const amount = Number(lot.amount) || 0;
  const remaining = Number(lot.remaining) || 0;
  const frozen = Number(lot.frozen) || 0;
  const used = Math.max(0, Number(lot.used) || amount - remaining - frozen);
  const scale = amount || 1;
  const expiresAt = parseInstant(lot.expiresAt)?.getTime() ?? null;
  const daysLeft = expiresAt !== null ? Math.ceil((expiresAt - now) / DAY_MS) : null;
  const revocable = lot.status === "active" && remaining > 0;

  const revoke = async () => {
    try {
      await revokeMutation.mutateAsync({ lotId: lot.id, reason: reason.trim() });
      toast.success(`批次 #${lot.id} 已作废`);
      setConfirming(false);
      setReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "作废失败");
    }
  };

  return (
    <div className={cn("rounded-xl border px-3 py-2.5", lot.status !== "active" && "bg-muted/30")}>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="font-mono text-[10px] text-muted-foreground">#{lot.id}</span>
        <span className="font-medium">{lot.title || LOT_ORIGIN_LABEL[lot.origin] || lot.origin}</span>
        <Badge variant="outline" size="sm">{LOT_ORIGIN_LABEL[lot.origin] ?? lot.origin}</Badge>
        <LotStatusBadge status={lot.status} />
        {lot.orderNo ? <span className="font-mono text-[10px] text-muted-foreground">{lot.orderNo}</span> : null}
        <span className="ml-auto flex items-center gap-1 text-[11px]">
          <CalendarClock className="size-3 text-muted-foreground" />
          {expiresAt === null ? (
            <span className="text-muted-foreground">永久</span>
          ) : (
            <span className={cn(daysLeft !== null && daysLeft <= 7 && lot.status === "active" ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
              {formatDateTimeMinute(lot.expiresAt)}
              {lot.status === "active" && daysLeft !== null ? (daysLeft > 0 ? `（${daysLeft} 天后）` : "（已到期）") : ""}
            </span>
          )}
        </span>
      </div>
      <div className="mt-2 flex h-2 overflow-hidden rounded-full bg-muted">
        <span className="bg-muted-foreground/40" style={{ width: `${(used / scale) * 100}%` }} title={`已用 ${formatPoints(used)}`} />
        <span className="bg-amber-500" style={{ width: `${(frozen / scale) * 100}%` }} title={`暂扣中 ${formatPoints(frozen)}`} />
        <span className={lot.status === "active" ? "bg-violet-500" : "bg-violet-500/30"} style={{ width: `${(remaining / scale) * 100}%` }} title={`剩余 ${formatPoints(remaining)}`} />
      </div>
      <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground tabular-nums">
        <span>
          剩余 <span className="font-mono font-medium text-foreground">{formatPoints(lot.remaining)}</span> / {formatPoints(lot.amount)}
        </span>
        {frozen > 0 ? <span className="text-amber-600 dark:text-amber-400">暂扣 {formatPoints(lot.frozen)}</span> : null}
        <span>已用 {formatPoints(used)}</span>
        <span>发放于 {formatDateTimeMinute(lot.createdAt)}</span>
        {revocable ? (
          <Button size="xs" variant="ghost" className="ml-auto h-6 text-destructive hover:text-destructive" onClick={() => setConfirming(true)}>
            <Ban className="size-3" />
            作废
          </Button>
        ) : null}
      </div>

      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>作废批次 #{lot.id}？</AlertDialogTitle>
            <AlertDialogDescription>
              剩余的 {formatPoints(lot.remaining)} 额度会清零（记一条「作废」流水），不能恢复。
              {frozen > 0 ? `暂扣中的 ${formatPoints(lot.frozen)} 等调用结算后再清。` : ""}
              {lot.origin === "purchase" ? "这是购买的额度包：要退钱请到额度商店的订单里按剩余比例退款，作废不退钱。" : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Input value={reason} maxLength={200} placeholder="原因（选填）" className="h-8 text-xs" onChange={(event) => setReason(event.target.value)} />
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              disabled={revokeMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void revoke();
              }}
            >
              {revokeMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              作废
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 发放限期额度（建一个 admin 批次）与调整永久额度，两件事放一张卡里切换。 */
function GrantCard({ appKey, userId }: { appKey: string; userId: number }) {
  const [mode, setMode] = useState<"lot" | "credit">("lot");
  const [direction, setDirection] = useState<"add" | "sub">("add");
  const [amount, setAmount] = useState("");
  const [days, setDays] = useState("30");
  const [title, setTitle] = useState("");
  const [requestId, setRequestId] = useState(() => crypto.randomUUID());
  const grantMutation = useGrantAIGatewayLotMutation(appKey);
  const adjustMutation = useAdjustAIGatewayCreditMutation(appKey);
  const pending = grantMutation.isPending || adjustMutation.isPending;
  const validDays = Math.max(0, Math.floor(Number(days) || 0));
  const now = useNow(60_000);

  const reset = () => {
    setAmount("");
    setRequestId(crypto.randomUUID());
  };

  const submit = async () => {
    if (!/^\d+(\.\d{1,6})?$/.test(amount.trim()) || Number(amount) <= 0) {
      toast.error("数量需为正数，最多 6 位小数");
      return;
    }
    try {
      if (mode === "lot") {
        if (!/^\d+$/.test(days.trim())) {
          toast.error("有效天数需为非负整数（0 为永久）");
          return;
        }
        const result = await grantMutation.mutateAsync({
          userId,
          payload: { amount: amount.trim(), validityDays: validDays, title: title.trim() || "管理员发放", requestId }
        });
        toast.success(result.replayed ? "这笔发放已经生效过" : `已发放批次 #${result.lot.id}`);
      } else {
        const result = await adjustMutation.mutateAsync({
          userId,
          payload: { amount: direction === "sub" ? `-${amount.trim()}` : amount.trim(), reason: title.trim(), requestId }
        });
        toast.success(result.replayed ? "这笔调整已经生效过" : `已调整，永久额度 ${formatPoints(result.balance)}`);
      }
      reset();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "提交失败");
    }
  };

  return (
    <SectionCard
      icon={<Plus className="size-4" />}
      title={mode === "lot" ? "发放限期额度" : "调整永久额度"}
      description={mode === "lot" ? "建一个额度批次，到期清零" : "永久额度不过期"}
      aside={
        <ToggleGroup type="single" size="sm" value={mode} onValueChange={(value) => value && setMode(value as "lot" | "credit")}>
          <ToggleGroupItem value="lot" className="h-7 px-2.5 text-xs">限期</ToggleGroupItem>
          <ToggleGroupItem value="credit" className="h-7 px-2.5 text-xs">永久</ToggleGroupItem>
        </ToggleGroup>
      }
    >
      <div className="space-y-3">
        {mode === "credit" ? (
          <ToggleGroup type="single" size="sm" value={direction} onValueChange={(value) => value && setDirection(value as "add" | "sub")}>
            <ToggleGroupItem value="add" className="h-8 gap-1 px-3 text-xs"><Plus className="size-3" />增加</ToggleGroupItem>
            <ToggleGroupItem value="sub" className="h-8 gap-1 px-3 text-xs"><Minus className="size-3" />扣减</ToggleGroupItem>
          </ToggleGroup>
        ) : null}
        <div className="flex flex-wrap items-center gap-1.5">
          <Input value={amount} inputMode="decimal" placeholder="额度" className="h-8 w-32 font-mono text-xs" onChange={(event) => setAmount(event.target.value)} />
          {AMOUNTS.map((item) => (
            <Button key={item} size="xs" variant={amount === item ? "secondary" : "outline"} onClick={() => setAmount(item)}>
              {Number(item).toLocaleString("zh-CN")}
            </Button>
          ))}
        </div>
        {mode === "lot" ? (
          <div className="flex flex-wrap items-center gap-1.5">
            <Input value={days} inputMode="numeric" className="h-8 w-20 font-mono text-xs" onChange={(event) => setDays(event.target.value)} />
            <span className="text-xs text-muted-foreground">天有效</span>
            {VALIDITY.map((item) => (
              <Button key={item} size="xs" variant={days === String(item) ? "secondary" : "outline"} onClick={() => setDays(String(item))}>
                {item} 天
              </Button>
            ))}
            <Button size="xs" variant={days === "0" ? "secondary" : "outline"} onClick={() => setDays("0")}>永久</Button>
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-1.5">
          <Input value={title} maxLength={mode === "lot" ? 64 : 200} placeholder={mode === "lot" ? "批次名称（用户可见）" : "原因"} className="h-8 w-56 text-xs"
            onChange={(event) => setTitle(event.target.value)} />
          {REASONS.filter((item) => mode === "credit" || item !== "误发收回").map((item) => (
            <Button key={item} size="xs" variant={title === item ? "secondary" : "outline"} onClick={() => setTitle(item)}>
              {item}
            </Button>
          ))}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-muted/50 px-3 py-2 text-[11px]">
          <span className="text-muted-foreground">
            {Number(amount) > 0
              ? mode === "lot"
                ? `发放 ${formatPoints(amount)} 额度，${validDays === 0 ? "永久有效（按额度包来源扣，排在限期批次之后）" : `${formatDate(now + validDays * DAY_MS)} 前后到期`}`
                : `${direction === "add" ? "增加" : "扣减"} ${formatPoints(amount)} 永久额度`
              : "填写额度"}
          </span>
          <Button size="sm" onClick={() => void submit()} disabled={pending || !amount} variant={mode === "credit" && direction === "sub" ? "destructive" : "default"}>
            {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {mode === "lot" ? "发放" : direction === "add" ? "增加" : "扣减"}
          </Button>
        </div>
      </div>
    </SectionCard>
  );
}
