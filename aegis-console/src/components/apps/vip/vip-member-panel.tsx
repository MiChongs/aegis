"use client";

import { useMemo, useState } from "react";
import { CalendarClock, Loader2, Search, ShieldCheck, Ticket } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import {
  FeatureTagList,
  TRIAL_REASON_META,
  VipSourceBadge,
  VipStatusBadge,
  formatRemaining,
  formatVipDate,
  formatVipPrice
} from "@/components/apps/vip/vip-shared";
import {
  useAdminVipEntitlementQuery,
  useAdminVipFeaturesQuery,
  useAdminVipTransactionsQuery,
  useGrantAdminVipMutation
} from "@/lib/vip-hooks";

/**
 * 会员查询与授予。
 *
 * 展示的是与用户手机上、与接入方服务端**同一份**判定结论
 * （后端的 `ResolveEntitlement` 是唯一入口）—— 客服每一通电话都要先回答
 * 「你到底是不是会员」，三处说法不一致时这个问题就没法结束。
 */
export function VipMemberPanel({ appKey }: { appKey: string }) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [grantDays, setGrantDays] = useState("30");
  const [grantReason, setGrantReason] = useState("");

  const entitlementQuery = useAdminVipEntitlementQuery(appKey, user?.id);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const transactionsQuery = useAdminVipTransactionsQuery(appKey, { userId: user?.id, page: 1, limit: 20 });
  const grantMutation = useGrantAdminVipMutation(appKey);

  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);
  const entitlement = entitlementQuery.data;
  const transactions = useMemo(() => transactionsQuery.data?.items ?? [], [transactionsQuery.data?.items]);

  const grant = async () => {
    if (!user) return;
    const days = Number(grantDays);
    if (!Number.isFinite(days) || days <= 0) {
      toast.error("授予天数必须大于 0");
      return;
    }
    try {
      await grantMutation.mutateAsync({ userId: user.id, days, reason: grantReason.trim() || undefined });
      toast.success(`已为 ${user.account ?? user.id} 授予 ${days} 天`);
      setGrantReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "授予失败");
    }
  };

  const offerMeta = entitlement ? TRIAL_REASON_META[entitlement.trialOffer.reason] : null;

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Search className="size-4" />}
        title="会员查询"
        description="与用户端 /vip/status、接入方服务端校验读的是同一份判定结论"
        // UserPicker 的触发器是 w-full，必须给它一个定宽容器才不会撑破卡头
        aside={
          <div className="w-64 min-w-0">
            <UserPicker appKey={appKey} value={user} onChange={setUser} />
          </div>
        }
      >
        {!user ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            选一个用户，看他现在是不是会员、凭什么是、还剩多久、有哪些功能权益。
          </p>
        ) : entitlementQuery.isLoading ? (
          <Skeleton className="h-32 w-full rounded-xl" />
        ) : entitlement ? (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <VipStatusBadge isVip={entitlement.isVip} isTrial={entitlement.isTrial} />
              <VipSourceBadge source={entitlement.source} />
              {entitlement.planName ? (
                <span className="text-xs text-muted-foreground">{entitlement.planName}</span>
              ) : null}
            </div>

            <div className="grid gap-3 sm:grid-cols-3">
              <Fact label="到期时间" value={formatVipDate(entitlement.expireAt)} />
              <Fact
                label="剩余"
                value={entitlement.isVip ? formatRemaining(entitlement.remainingSeconds) : "—"}
                hint={
                  entitlement.isVip
                    ? `按天口径 ${entitlement.remainingDays} 天`
                    : "不是会员"
                }
              />
              <Fact
                label="功能权益"
                value={
                  <FeatureTagList
                    tags={entitlement.features}
                    catalog={features}
                    emptyHint={entitlement.isVip ? "只是会员，无细分权益" : "—"}
                  />
                }
              />
            </div>

            {/* 试用历史：领过就一直保留，客户端据此把"免费试用"换成"续费" */}
            <div className="rounded-xl border border-border bg-muted/30 p-3">
              <div className="flex items-center gap-2">
                <Ticket className="size-3.5 text-muted-foreground" />
                <span className="text-xs font-medium">试用</span>
                {entitlement.trial ? (
                  <Badge variant={entitlement.trial.active ? "warning" : "secondary"} size="sm">
                    {entitlement.trial.active ? "试用中" : "已用过"}
                  </Badge>
                ) : (
                  <Badge variant="secondary" size="sm">
                    从未领取
                  </Badge>
                )}
              </div>

              {entitlement.trial ? (
                <div className="mt-2 grid gap-2 text-[11px] text-muted-foreground sm:grid-cols-3">
                  <span>套餐：{entitlement.trial.planName}（{entitlement.trial.durationDays} 天）</span>
                  <span>领取于：{formatVipDate(entitlement.trial.claimedAt)}</span>
                  <span>
                    {entitlement.trial.active
                      ? `还剩 ${formatRemaining(entitlement.trial.remainingSeconds)}`
                      : `结束于 ${formatVipDate(entitlement.trial.endsAt)}`}
                  </span>
                </div>
              ) : null}

              <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2">
                <span className="text-[11px] text-muted-foreground">现在能不能领：</span>
                <Badge variant={entitlement.trialOffer.available ? "success" : "secondary"} size="sm">
                  {offerMeta?.label ?? entitlement.trialOffer.reason}
                </Badge>
                <span className="text-[10px] text-muted-foreground">{offerMeta?.hint}</span>
              </div>
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">查不到该用户的会员状态。</p>
        )}
      </SectionCard>

      {user ? (
        <SectionCard
          icon={<ShieldCheck className="size-4" />}
          title="授予会员"
          description="不动钱包、不走套餐，直接延长到期时间；如实记成「管理员授予」"
        >
          <div className="flex flex-wrap items-end gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs">天数</Label>
              <Input
                inputMode="numeric"
                value={grantDays}
                onChange={(event) => setGrantDays(event.target.value)}
                className="w-28"
              />
            </div>
            <div className="min-w-52 flex-1 space-y-1.5">
              <Label className="text-xs">理由（会写进账本）</Label>
              <Input
                value={grantReason}
                onChange={(event) => setGrantReason(event.target.value)}
                placeholder="如：客诉补偿 / 活动奖励"
              />
            </div>
            <Button size="sm" onClick={grant} disabled={grantMutation.isPending}>
              {grantMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              授予
            </Button>
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {[7, 30, 90, 365].map((days) => (
              <Button key={days} variant="outline" size="xs" onClick={() => setGrantDays(String(days))}>
                {days} 天
              </Button>
            ))}
          </div>
          <p className="mt-3 text-[11px] leading-snug text-muted-foreground">
            续期是<b>顺延</b>的：还在会员期内时从原到期时间往后加，不会把已有时长吃掉。
            授予不带任何功能标识 —— 要给细分权益请让用户走套餐，或先把功能勾进一个套餐再授予。
          </p>
        </SectionCard>
      ) : null}

      {user ? (
        <SectionCard
          icon={<CalendarClock className="size-4" />}
          title="开通记录"
          description="这个用户的每一段会员从哪来、发到什么时候"
        >
          {transactionsQuery.isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : transactions.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
              这个用户还没有任何开通记录。
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>套餐</TableHead>
                    <TableHead>来源</TableHead>
                    <TableHead>金额</TableHead>
                    <TableHead>时长</TableHead>
                    <TableHead>发到</TableHead>
                    <TableHead>功能</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {transactions.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className="text-xs">{formatVipDate(item.createdAt)}</TableCell>
                      <TableCell className="text-xs">{item.planName}</TableCell>
                      <TableCell>
                        <VipSourceBadge source={item.payChannel} />
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">{formatVipPrice(item.payAmount)}</TableCell>
                      <TableCell className="text-xs">{item.durationDays} 天</TableCell>
                      <TableCell className="text-xs">{formatVipDate(item.expireAfter)}</TableCell>
                      <TableCell>
                        <FeatureTagList tags={item.features} catalog={features} emptyHint="—" />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
          <p className="mt-3 text-[11px] leading-snug text-muted-foreground">
            功能一列是<b>开通那一刻的快照</b>：之后改套餐配置不会改写它，
            所以这里可能与套餐现在的配置不同 —— 那是对的。
          </p>
        </SectionCard>
      ) : null}
    </div>
  );
}

function Fact({ label, value, hint }: { label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="rounded-xl border border-border bg-muted/30 p-3">
      <div className="text-[10px] font-medium tracking-wide text-muted-foreground uppercase">{label}</div>
      <div className="mt-1 text-xs font-medium">{value}</div>
      {hint ? <div className="mt-0.5 text-[10px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}
