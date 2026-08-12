"use client";

import { useMemo, useState } from "react";
import { Crown, Gift, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
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
import { Switch } from "@/components/ui/switch";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { VipPlanEditor } from "@/components/apps/vip/vip-plan-editor";
import { FeatureTagList, formatVipPrice } from "@/components/apps/vip/vip-shared";
import {
  useAdminVipFeaturesQuery,
  useAdminVipPlansQuery,
  useDeleteAdminVipPlanMutation,
  useSaveAdminVipPlanMutation
} from "@/lib/vip-hooks";
import type { VipPlan } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

/**
 * 套餐管理。
 *
 * 付费与试用**分区展示**而不是混在一张表里：它们的入口完全不同
 * （一个被购买、一个被领取），混着列会让人以为试用也能卖，
 * 而那正是后端两道闸门拦下的事。
 */
export function VipPlansPanel({ appKey }: { appKey: string }) {
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const saveMutation = useSaveAdminVipPlanMutation(appKey);
  const deleteMutation = useDeleteAdminVipPlanMutation(appKey);

  const [editing, setEditing] = useState<{ open: boolean; plan: VipPlan | null }>({ open: false, plan: null });
  const [pendingDelete, setPendingDelete] = useState<VipPlan | null>(null);

  const plans = useMemo(() => plansQuery.data ?? [], [plansQuery.data]);
  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);
  const paidPlans = plans.filter((plan) => plan.kind !== "trial");
  const trialPlans = plans.filter((plan) => plan.kind === "trial");
  const activeTrial = trialPlans.find((plan) => plan.isActive) ?? null;

  const toggleActive = async (plan: VipPlan, isActive: boolean) => {
    try {
      await saveMutation.mutateAsync({ id: plan.id, isActive });
      toast.success(isActive ? `已启用「${plan.name}」` : `已停用「${plan.name}」`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.id);
      toast.success("套餐已删除");
      setPendingDelete(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  const renderPlan = (plan: VipPlan) => (
    <div
      key={plan.id}
      className={cn(
        "flex flex-col gap-3 rounded-xl border border-border p-4 transition-colors sm:flex-row sm:items-start sm:justify-between",
        !plan.isActive && "bg-muted/40"
      )}
    >
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold">{plan.name}</span>
          {plan.kind === "trial" ? (
            <Badge variant="warning" size="sm">
              试用
            </Badge>
          ) : null}
          {!plan.isActive ? (
            <Badge variant="secondary" size="sm">
              已停用
            </Badge>
          ) : null}
          {plan.kind === "trial" && plan.trialDeviceLimited ? (
            <Badge variant="info" size="sm">
              限设备
            </Badge>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span>
            <span className="font-medium text-foreground">{plan.durationDays}</span> 天
          </span>
          <span>
            <span className="font-medium text-foreground">{formatVipPrice(plan.price)}</span>
            {plan.originalPrice ? <span className="ml-1 line-through">¥ {plan.originalPrice}</span> : null}
          </span>
          {plan.bonusIntegral > 0 ? <span>赠 {plan.bonusIntegral} 积分</span> : null}
          <span>排序 {plan.sortOrder}</span>
        </div>

        {plan.description ? (
          <p className="text-[11px] leading-snug text-muted-foreground">{plan.description}</p>
        ) : null}

        <FeatureTagList tags={plan.features} catalog={features} emptyHint="不含细分权益（只是会员）" />
      </div>

      <div className="flex shrink-0 items-center gap-1">
        <Switch
          checked={plan.isActive}
          onCheckedChange={(value) => toggleActive(plan, value)}
          disabled={saveMutation.isPending}
        />
        <Button variant="ghost" size="icon-sm" onClick={() => setEditing({ open: true, plan })}>
          <Pencil className="size-3.5" />
          <span className="sr-only">编辑</span>
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground hover:text-destructive"
          onClick={() => setPendingDelete(plan)}
        >
          <Trash2 className="size-3.5" />
          <span className="sr-only">删除</span>
        </Button>
      </div>
    </div>
  );

  if (plansQuery.isLoading) {
    return (
      <div className="space-y-3">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-24 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Crown className="size-4" />}
        title="付费套餐"
        description="出现在客户端的购买列表里，可用余额或在线支付购买"
        aside={
          <Button size="sm" onClick={() => setEditing({ open: true, plan: null })}>
            <Plus className="size-3.5" /> 新建套餐
          </Button>
        }
      >
        {paidPlans.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            还没有付费套餐。客户端调 <code className="font-mono">/vip/plans</code> 会拿到空列表。
          </p>
        ) : (
          <div className="space-y-3">{paidPlans.map(renderPlan)}</div>
        )}
      </SectionCard>

      <SectionCard
        icon={<Gift className="size-4" />}
        title="试用套餐"
        description="一人一次，由「领取」入口发放；每个应用至多一个启用中的试用"
      >
        {trialPlans.length === 0 ? (
          <div className="space-y-2 rounded-xl border border-dashed border-border px-4 py-6 text-center">
            <p className="text-xs text-muted-foreground">
              还没有试用套餐。客户端的 <code className="font-mono">trialOffer.reason</code> 会是
              <code className="ml-1 font-mono">not_configured</code>，试用入口应当整个隐藏。
            </p>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setEditing({ open: true, plan: null })}
            >
              <Plus className="size-3.5" /> 新建试用套餐
            </Button>
          </div>
        ) : (
          <div className="space-y-3">{trialPlans.map(renderPlan)}</div>
        )}
        {trialPlans.length > 1 && !activeTrial ? (
          <p className="mt-3 text-[11px] text-muted-foreground">
            当前没有启用中的试用套餐，试用入口对客户端不存在。
          </p>
        ) : null}
      </SectionCard>

      <VipPlanEditor
        appKey={appKey}
        open={editing.open}
        onOpenChange={(open) => setEditing((prev) => ({ ...prev, open }))}
        plan={editing.plan}
        features={features}
        hasOtherActiveTrial={Boolean(activeTrial && activeTrial.id !== editing.plan?.id)}
      />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除套餐「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              删除的是<b>售卖入口</b>，不影响已经开通的会员 —— 他们的时长与功能权益来自开通时的账本快照，
              不会因为套餐消失而失效。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
