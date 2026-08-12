"use client";

import { useMemo, useState } from "react";
import { Gift, Loader2, RotateCcw, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { formatConversion, formatVipDate, isTrialActive } from "@/components/apps/vip/vip-shared";
import {
  useAdminVipPlansQuery,
  useAdminVipTrialClaimsQuery,
  useClaimAdminVipTrialMutation,
  useResetAdminVipTrialMutation
} from "@/lib/vip-hooks";
import type { VipTrialClaim } from "@/lib/api/vip";

/**
 * 试用台：汇总 + 领取记录 + 客服出口。
 *
 * 汇总里的**转化**是开试用的唯一理由。没有这一列，这张表只是一堆
 * 「谁在什么时候领了七天」的流水，回答不了「这个试用到底值不值得开」。
 */
export function VipTrialPanel({ appKey }: { appKey: string }) {
  const [page, setPage] = useState(1);
  const limit = 20;

  const claimsQuery = useAdminVipTrialClaimsQuery(appKey, { page, limit });
  const plansQuery = useAdminVipPlansQuery(appKey);
  const claimMutation = useClaimAdminVipTrialMutation(appKey);
  const resetMutation = useResetAdminVipTrialMutation(appKey);

  const [grantTarget, setGrantTarget] = useState<PickedUser | null>(null);
  const [pendingReset, setPendingReset] = useState<VipTrialClaim | null>(null);

  const data = claimsQuery.data;
  const items = useMemo(() => data?.items ?? [], [data?.items]);
  const summary = data?.summary ?? { total: 0, active: 0, converted: 0 };
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / limit));
  const activeTrialPlan = (plansQuery.data ?? []).find((plan) => plan.kind === "trial" && plan.isActive) ?? null;

  const claimForUser = async () => {
    if (!grantTarget) return;
    try {
      const result = await claimMutation.mutateAsync(grantTarget.id);
      toast.success(
        result.replayed
          ? `${grantTarget.account ?? grantTarget.id} 此前已在试用中，未重复发放`
          : `已为 ${grantTarget.account ?? grantTarget.id} 开通试用`
      );
      setGrantTarget(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "代领失败");
    }
  };

  const confirmReset = async () => {
    if (!pendingReset) return;
    try {
      await resetMutation.mutateAsync(pendingReset.userId);
      toast.success("试用资格已恢复");
      setPendingReset(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "恢复失败");
    }
  };

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Sparkles className="size-4" />}
        title="试用概览"
        description={
          activeTrialPlan
            ? `当前试用套餐：${activeTrialPlan.name}（${activeTrialPlan.durationDays} 天${activeTrialPlan.trialDeviceLimited ? "，限设备" : ""}）`
            : "当前没有启用中的试用套餐 —— 客户端的试用入口不存在"
        }
      >
        <div className="grid gap-3 sm:grid-cols-4">
          <StatTile label="累计领取" value={summary.total} hint="一人一次，这就是领过的人数" />
          <StatTile label="试用中" value={summary.active} hint="仍在试用期内" />
          <StatTile label="已转化" value={summary.converted} hint="领取之后发生过付费开通" />
          <StatTile
            label="转化率"
            value={formatConversion(summary.converted, summary.total)}
            hint={summary.total === 0 ? "还没有人领过" : "已转化 / 累计领取"}
          />
        </div>
      </SectionCard>

      <SectionCard
        icon={<Gift className="size-4" />}
        title="领取记录"
        description="一人一次由数据库唯一约束保证，这里是那份资格账本"
      >
        {/* 代领控件放在卡内而不是卡头：UserPicker 的触发器是 w-full，
            塞进 shrink-0 的 aside 会把旁边的按钮顶出卡片（SectionCard 是 overflow-hidden） */}
        <div className="mb-4 space-y-2 rounded-xl border border-border bg-muted/30 p-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="w-64 min-w-0">
              <UserPicker appKey={appKey} value={grantTarget} onChange={setGrantTarget} />
            </div>
            <Button
              size="sm"
              variant="outline"
              disabled={!grantTarget || claimMutation.isPending || !activeTrialPlan}
              onClick={claimForUser}
            >
              {claimMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              代领试用
            </Button>
            {!activeTrialPlan ? (
              <span className="text-[11px] text-muted-foreground">没有启用中的试用套餐，无法代领</span>
            ) : null}
          </div>
          <p className="text-[11px] leading-snug text-muted-foreground">
            代领走与用户自助<b>完全相同</b>的资格判定：已领过、已是会员都会被拒。
            要跳过资格直接送时长请用「会员查询」里的授予 —— 那会如实记成管理员授予，
            不会污染这里的转化率。
          </p>
        </div>

        {claimsQuery.isLoading ? (
          <div className="space-y-2">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            还没有人领过试用。
          </p>
        ) : (
          <>
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>用户</TableHead>
                    <TableHead>套餐</TableHead>
                    <TableHead>试用到期</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>设备 / IP</TableHead>
                    <TableHead>领取时间</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((claim) => {
                    const active = isTrialActive(claim.trialEndsAt);
                    return (
                      <TableRow key={claim.id}>
                        <TableCell className="text-xs">
                          <div className="font-medium">{claim.account || `#${claim.userId}`}</div>
                          <div className="text-[10px] text-muted-foreground">ID {claim.userId}</div>
                        </TableCell>
                        <TableCell className="text-xs">
                          <div>{claim.planName}</div>
                          <div className="text-[10px] text-muted-foreground">{claim.durationDays} 天</div>
                        </TableCell>
                        <TableCell className="text-xs">{formatVipDate(claim.trialEndsAt)}</TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                            <Badge variant={active ? "warning" : "secondary"} size="sm">
                              {active ? "试用中" : "已结束"}
                            </Badge>
                            {claim.converted ? (
                              <Badge variant="success" size="sm">
                                已转化
                              </Badge>
                            ) : null}
                            {claim.operator ? (
                              <Badge variant="info" size="sm">
                                代领
                              </Badge>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell className="max-w-40 text-[10px] text-muted-foreground">
                          <div className="truncate font-mono">{claim.deviceId || "—"}</div>
                          <div className="truncate font-mono">{claim.clientIp || "—"}</div>
                        </TableCell>
                        <TableCell className="text-xs">{formatVipDate(claim.createdAt)}</TableCell>
                        <TableCell className="text-right">
                          <Button
                            variant="ghost"
                            size="xs"
                            onClick={() => setPendingReset(claim)}
                            className="text-muted-foreground"
                          >
                            <RotateCcw className="size-3" /> 恢复资格
                          </Button>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>

            {totalPages > 1 ? (
              <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
                <span>
                  共 {total} 条 · 第 {page} / {totalPages} 页
                </span>
                <div className="flex gap-2">
                  <Button variant="outline" size="xs" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                    上一页
                  </Button>
                  <Button
                    variant="outline"
                    size="xs"
                    disabled={page >= totalPages}
                    onClick={() => setPage((p) => p + 1)}
                  >
                    下一页
                  </Button>
                </div>
              </div>
            ) : null}
          </>
        )}
      </SectionCard>

      <AlertDialog open={Boolean(pendingReset)} onOpenChange={(open) => !open && setPendingReset(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              恢复「{pendingReset?.account || `#${pendingReset?.userId}`}」的试用资格？
            </AlertDialogTitle>
            <AlertDialogDescription>
              只删这条资格记录，<b>不收回已经发放的会员时长</b> —— 那是两件事：资格是「还能不能领」，
              时长是「已经给出去的东西」。他仍在试用期内时也领不了第二次（会被判成「已是会员」），
              要等这段试用结束。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmReset} disabled={resetMutation.isPending}>
              {resetMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认恢复
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function StatTile({ label, value, hint }: { label: string; value: number | string; hint: string }) {
  return (
    <div className="rounded-xl border border-border bg-muted/30 p-3">
      <div className="text-[10px] font-medium tracking-wide text-muted-foreground uppercase">{label}</div>
      <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
      <div className="mt-0.5 text-[10px] leading-snug text-muted-foreground">{hint}</div>
    </div>
  );
}
