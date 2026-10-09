"use client";

import { useMemo, useState } from "react";
import { CalendarClock, Loader2, Search, ShieldCheck, ShieldMinus, Ticket } from "lucide-react";
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
  VipRevokeDialog,
  VipSourceBadge,
  VipStatusBadge,
  formatRemaining,
  formatVipDate,
  formatVipExpireAfter,
  formatVipPrice,
  formatVipTerm,
  type VipRevokeAction
} from "@/components/apps/vip/vip-shared";
import {
  useAdminVipEntitlementQuery,
  useAdminVipFeaturesQuery,
  useAdminVipTransactionsQuery,
  useGrantAdminVipMutation,
  useRevokeAdminVipMutation
} from "@/lib/vip-hooks";
import { cn } from "@/lib/utils";

/** 授予天数的快捷档位 */
const GRANT_PRESETS = [7, 30, 90, 365];

/**
 * 会员查询与授予。
 *
 * 展示的是与用户手机上、与接入方服务端**同一份**判定结论
 * （后端的 `ResolveEntitlement` 是唯一入口）—— 客服每一通电话都要先回答
 * 「你到底是不是会员」，三处说法不一致时这个问题就没法结束。
 *
 * 永久与限时是并列的两条线：扣减天数只动限时那条线，取消永久会员只动永久那条线，
 * 所以两个收回动作按这个人实际有哪条线分别出现，而不是一律摆出来等后端报错。
 */
export function VipMemberPanel({ appKey }: { appKey: string }) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [grantDays, setGrantDays] = useState("30");
  const [grantLifetime, setGrantLifetime] = useState(false);
  const [grantReason, setGrantReason] = useState("");
  const [revokeDays, setRevokeDays] = useState("");
  const [revokeReason, setRevokeReason] = useState("");
  const [revokeDialog, setRevokeDialog] = useState<{ open: boolean; action: VipRevokeAction }>({
    open: false,
    action: { lifetime: true }
  });

  const entitlementQuery = useAdminVipEntitlementQuery(appKey, user?.id);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const transactionsQuery = useAdminVipTransactionsQuery(appKey, { userId: user?.id, page: 1, limit: 20 });
  const grantMutation = useGrantAdminVipMutation(appKey);
  const revokeMutation = useRevokeAdminVipMutation(appKey);

  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);
  const entitlement = entitlementQuery.data;
  const transactions = useMemo(() => transactionsQuery.data?.items ?? [], [transactionsQuery.data?.items]);

  // 限时那条线是否还在期内：永久会员看 timedExpireAt，其余看是不是会员
  const hasTimed = entitlement
    ? entitlement.isLifetime
      ? Boolean(entitlement.timedExpireAt)
      : entitlement.isVip
    : false;
  const canRevoke = Boolean(entitlement?.isLifetime) || hasTimed;

  const grant = async () => {
    if (!user) return;
    const days = Number(grantDays);
    if (!grantLifetime && (!Number.isFinite(days) || days <= 0)) {
      toast.error("授予天数必须大于 0");
      return;
    }
    const reason = grantReason.trim() || undefined;
    try {
      await grantMutation.mutateAsync(
        grantLifetime ? { userId: user.id, lifetime: true, reason } : { userId: user.id, days, reason }
      );
      toast.success(
        grantLifetime ? `已为 ${user.account ?? user.id} 授予永久会员` : `已为 ${user.account ?? user.id} 授予 ${days} 天`
      );
      setGrantReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "授予失败");
    }
  };

  const requestDeduct = () => {
    const days = Number(revokeDays);
    if (!Number.isInteger(days) || days <= 0) {
      toast.error("扣减天数必须为正整数");
      return;
    }
    setRevokeDialog({ open: true, action: { lifetime: false, days } });
  };

  const confirmRevoke = async () => {
    if (!user) return;
    const action = revokeDialog.action;
    const reason = revokeReason.trim() || undefined;
    try {
      await revokeMutation.mutateAsync(
        action.lifetime
          ? { userId: user.id, lifetime: true, reason }
          : { userId: user.id, days: action.days, reason }
      );
      toast.success(action.lifetime ? "已取消永久会员" : `已扣减 ${action.days} 天`);
      setRevokeDialog((prev) => ({ ...prev, open: false }));
      setRevokeDays("");
      setRevokeReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const offerMeta = entitlement ? TRIAL_REASON_META[entitlement.trialOffer.reason] : null;

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Search className="size-4" />}
        title="会员查询"
        // UserPicker 的触发器是 w-full，必须给它一个定宽容器才不会撑破卡头
        aside={
          <div className="w-64 min-w-0">
            <UserPicker appKey={appKey} value={user} onChange={setUser} />
          </div>
        }
      >
        {!user ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            请选择用户
          </p>
        ) : entitlementQuery.isLoading ? (
          <Skeleton className="h-32 w-full rounded-xl" />
        ) : entitlement ? (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <VipStatusBadge
                isVip={entitlement.isVip}
                isTrial={entitlement.isTrial}
                isLifetime={entitlement.isLifetime}
              />
              <VipSourceBadge source={entitlement.source} />
              {entitlement.planName ? (
                <span className="text-xs text-muted-foreground">{entitlement.planName}</span>
              ) : null}
            </div>

            {/* 永久会员没有到期时间与剩余时长；另买的限时会员只贡献功能，到期时间单独列出 */}
            <div
              className={cn(
                "grid gap-3",
                entitlement.isLifetime && entitlement.timedExpireAt ? "sm:grid-cols-2 xl:grid-cols-4" : "sm:grid-cols-3"
              )}
            >
              {entitlement.isLifetime ? (
                <>
                  <Fact label="到期时间" value="永久" />
                  <Fact label="开通时间" value={formatVipDate(entitlement.lifetimeSince)} />
                  {entitlement.timedExpireAt ? (
                    <Fact label="限时到期" value={formatVipDate(entitlement.timedExpireAt)} />
                  ) : null}
                </>
              ) : (
                <>
                  <Fact label="到期时间" value={formatVipDate(entitlement.expireAt)} />
                  <Fact
                    label="剩余"
                    value={entitlement.isVip ? formatRemaining(entitlement.remainingSeconds) : "—"}
                    hint={entitlement.isVip ? `${entitlement.remainingDays} 天` : "非会员"}
                  />
                </>
              )}
              <Fact
                label="功能权益"
                value={
                  <FeatureTagList
                    tags={entitlement.features}
                    catalog={features}
                    emptyHint={entitlement.isVip ? "无细分权益" : "—"}
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
                <span className="text-[11px] text-muted-foreground">试用资格：</span>
                <Badge variant={entitlement.trialOffer.available ? "success" : "secondary"} size="sm">
                  {offerMeta?.label ?? entitlement.trialOffer.reason}
                </Badge>
              </div>
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">暂无会员状态</p>
        )}
      </SectionCard>

      {user ? (
        <SectionCard
          icon={<ShieldCheck className="size-4" />}
          title="授予会员"
        >
          <div className="flex flex-wrap items-end gap-3">
            {grantLifetime ? null : (
              <div className="space-y-1.5">
                <Label className="text-xs">天数</Label>
                <Input
                  inputMode="numeric"
                  value={grantDays}
                  onChange={(event) => setGrantDays(event.target.value)}
                  className="w-28"
                />
              </div>
            )}
            <div className="min-w-52 flex-1 space-y-1.5">
              <Label className="text-xs">理由</Label>
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
            {GRANT_PRESETS.map((days) => (
              <Button
                key={days}
                variant={!grantLifetime && grantDays === String(days) ? "secondary" : "outline"}
                size="xs"
                onClick={() => {
                  setGrantLifetime(false);
                  setGrantDays(String(days));
                }}
              >
                {days} 天
              </Button>
            ))}
            <Button variant={grantLifetime ? "secondary" : "outline"} size="xs" onClick={() => setGrantLifetime(true)}>
              永久
            </Button>
          </div>
        </SectionCard>
      ) : null}

      {user && canRevoke ? (
        <SectionCard
          icon={<ShieldMinus className="size-4" />}
          title="收回会员"
        >
          <div className="flex flex-wrap items-end gap-3">
            {hasTimed ? (
              <div className="space-y-1.5">
                <Label className="text-xs">天数</Label>
                <Input
                  inputMode="numeric"
                  value={revokeDays}
                  onChange={(event) => setRevokeDays(event.target.value)}
                  placeholder="7"
                  className="w-28"
                />
              </div>
            ) : null}
            <div className="min-w-52 flex-1 space-y-1.5">
              <Label className="text-xs">理由</Label>
              <Input
                value={revokeReason}
                onChange={(event) => setRevokeReason(event.target.value)}
                placeholder="如：误发放 / 违规处置"
              />
            </div>
            {hasTimed ? (
              <Button size="sm" variant="outline" onClick={requestDeduct} disabled={revokeMutation.isPending}>
                扣减天数
              </Button>
            ) : null}
            {entitlement?.isLifetime ? (
              <Button
                size="sm"
                variant="outline"
                className="text-destructive hover:text-destructive"
                onClick={() => setRevokeDialog({ open: true, action: { lifetime: true } })}
                disabled={revokeMutation.isPending}
              >
                取消永久会员
              </Button>
            ) : null}
          </div>
        </SectionCard>
      ) : null}

      {user ? (
        <SectionCard
          icon={<CalendarClock className="size-4" />}
          title="开通记录"
        >
          {transactionsQuery.isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : transactions.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
              暂无开通记录
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
                    <TableHead>开通时功能</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {transactions.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className="text-xs">{formatVipDate(item.createdAt)}</TableCell>
                      <TableCell className="text-xs">{item.planName}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1">
                          <VipSourceBadge source={item.payChannel} />
                          {item.revokedAt ? (
                            <Badge variant="danger" size="sm">
                              已作废
                            </Badge>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">{formatVipPrice(item.payAmount)}</TableCell>
                      <TableCell className="text-xs tabular-nums">{formatVipTerm(item)}</TableCell>
                      <TableCell className="text-xs">{formatVipExpireAfter(item)}</TableCell>
                      <TableCell>
                        <FeatureTagList tags={item.features} catalog={features} emptyHint="—" />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </SectionCard>
      ) : null}

      <VipRevokeDialog
        open={revokeDialog.open}
        action={revokeDialog.action}
        pending={revokeMutation.isPending}
        onOpenChange={(open) => setRevokeDialog((prev) => ({ ...prev, open }))}
        onConfirm={confirmRevoke}
      />
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
