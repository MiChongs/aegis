"use client";

import { useState } from "react";
import { Crown, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { VipPlan } from "@/lib/api-client";
import {
  useAdminVipPlansQuery,
  useDeleteAdminVipPlanMutation,
  useGrantAdminVipMutation,
  useSaveAdminVipPlanMutation
} from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";

type PlanDraft = {
  id?: number;
  name: string;
  durationDays: string;
  price: string;
  originalPrice: string;
  bonusIntegral: string;
  description: string;
  isActive: boolean;
  sortOrder: string;
};

const emptyDraft: PlanDraft = {
  name: "",
  durationDays: "30",
  price: "",
  originalPrice: "",
  bonusIntegral: "0",
  description: "",
  isActive: true,
  sortOrder: "0"
};

export function VipPlansPanel({ appId }: { appId?: number | null }) {
  const plansQuery = useAdminVipPlansQuery(appId);
  const saveMutation = useSaveAdminVipPlanMutation(appId);
  const deleteMutation = useDeleteAdminVipPlanMutation(appId);
  const grantMutation = useGrantAdminVipMutation(appId);

  const [editorOpen, setEditorOpen] = useState(false);
  const [draft, setDraft] = useState<PlanDraft>(emptyDraft);
  const [grantUserId, setGrantUserId] = useState("");
  const [grantDays, setGrantDays] = useState("30");
  const [grantReason, setGrantReason] = useState("");

  const plans = plansQuery.data || [];

  function openCreate() {
    setDraft(emptyDraft);
    setEditorOpen(true);
  }

  function openEdit(plan: VipPlan) {
    setDraft({
      id: plan.id,
      name: plan.name,
      durationDays: String(plan.durationDays),
      price: plan.price,
      originalPrice: plan.originalPrice ?? "",
      bonusIntegral: String(plan.bonusIntegral),
      description: plan.description ?? "",
      isActive: plan.isActive,
      sortOrder: String(plan.sortOrder)
    });
    setEditorOpen(true);
  }

  async function handleSave() {
    if (!draft.name.trim()) {
      toast.error("套餐名称不能为空");
      return;
    }
    const days = Number(draft.durationDays);
    if (!Number.isInteger(days) || days <= 0) {
      toast.error("套餐时长必须为正整数（天）");
      return;
    }
    if (draft.price.trim() === "" || Number.isNaN(Number(draft.price)) || Number(draft.price) < 0) {
      toast.error("套餐价格无效");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        id: draft.id,
        name: draft.name.trim(),
        durationDays: days,
        price: Number(draft.price).toFixed(2),
        originalPrice: draft.originalPrice.trim() ? Number(draft.originalPrice).toFixed(2) : undefined,
        bonusIntegral: Number(draft.bonusIntegral) || 0,
        description: draft.description.trim() || undefined,
        isActive: draft.isActive,
        sortOrder: Number(draft.sortOrder) || 0
      });
      toast.success(draft.id ? "套餐已更新" : "套餐已创建");
      setEditorOpen(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleDelete(plan: VipPlan) {
    try {
      await deleteMutation.mutateAsync(plan.id);
      toast.success(`已删除「${plan.name}」`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  }

  async function handleGrant() {
    const userId = Number(grantUserId);
    const days = Number(grantDays);
    if (!Number.isInteger(userId) || userId <= 0) {
      toast.error("请输入有效的用户 ID");
      return;
    }
    if (!Number.isInteger(days) || days <= 0) {
      toast.error("授予天数必须为正整数");
      return;
    }
    try {
      const txn = await grantMutation.mutateAsync({ userId, days, reason: grantReason.trim() || undefined });
      toast.success(`已授予用户 ${userId} 共 ${days} 天 VIP，到期：${new Date(txn.expireAfter).toLocaleString("zh-CN", { hour12: false })}`);
      setGrantUserId("");
      setGrantReason("");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "授予失败");
    }
  }

  if (plansQuery.isLoading) {
    return <div className="space-y-3">{Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-10 w-full rounded-lg" />)}</div>;
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <span className="text-xs tabular-nums text-muted-foreground">{plans.length} 个套餐（用户购买时以服务端价格为准，下单即锁价）</span>
        <div className="flex gap-1.5">
          <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => void plansQuery.refetch()}>
            <RefreshCw className="size-3" />刷新
          </Button>
          <Button size="sm" className="h-7 gap-1 text-xs" onClick={openCreate}>
            <Plus className="size-3" />新建套餐
          </Button>
        </div>
      </div>

      {plans.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">
          暂无套餐 —— 创建后用户即可在应用内通过余额或在线支付购买 VIP
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>套餐</TableHead>
                <TableHead>时长</TableHead>
                <TableHead className="text-right">价格</TableHead>
                <TableHead className="text-right">赠送积分</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-12" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {plans.map((plan) => (
                <TableRow key={plan.id} className="cursor-pointer" onClick={() => openEdit(plan)}>
                  <TableCell className="text-xs font-medium">
                    <span className="flex items-center gap-1.5">
                      <Crown className="size-3 text-amber-500" />
                      {plan.name}
                    </span>
                  </TableCell>
                  <TableCell className="text-xs tabular-nums text-muted-foreground">{plan.durationDays} 天</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {plan.price}
                    {plan.originalPrice ? <span className="ml-1.5 text-muted-foreground line-through">{plan.originalPrice}</span> : null}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums text-muted-foreground">{plan.bonusIntegral || "—"}</TableCell>
                  <TableCell>
                    {plan.isActive
                      ? <Badge className="bg-emerald-500/15 text-emerald-600 hover:bg-emerald-500/15">在售</Badge>
                      : <Badge variant="secondary">已下架</Badge>}
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <Button size="icon" variant="ghost" className="size-7 text-muted-foreground hover:text-destructive" onClick={() => void handleDelete(plan)}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="rounded-xl border px-4 py-3">
        <p className="mb-2.5 text-xs font-medium">手动授予 VIP（人工补偿 / 活动发放）</p>
        <div className="flex flex-wrap items-end gap-2">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">用户 ID</Label>
            <Input className="h-8 w-32 text-xs" placeholder="10001" value={grantUserId} onChange={(e) => setGrantUserId(e.target.value)} />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">天数</Label>
            <Input className="h-8 w-24 text-xs" type="number" value={grantDays} onChange={(e) => setGrantDays(e.target.value)} />
          </div>
          <div className="min-w-44 flex-1 space-y-1">
            <Label className="text-xs text-muted-foreground">原因（计入授予记录）</Label>
            <Input className="h-8 text-xs" placeholder="例如：活动补偿" value={grantReason} onChange={(e) => setGrantReason(e.target.value)} />
          </div>
          <Button size="sm" className="h-8 text-xs" disabled={grantMutation.isPending} onClick={() => void handleGrant()}>授予</Button>
        </div>
      </div>

      <Sheet open={editorOpen} onOpenChange={setEditorOpen}>
        <SheetContent side="right" className="w-[92vw] max-w-md overflow-y-auto">
          <SheetHeader>
            <SheetTitle className="text-sm">{draft.id ? "编辑套餐" : "新建套餐"}</SheetTitle>
          </SheetHeader>
          <div className="space-y-3 pt-4">
            <div className="space-y-1">
              <Label className="text-xs">套餐名称 <span className="text-destructive">*</span></Label>
              <Input className="h-8 text-sm" placeholder="月度会员" value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label className="text-xs">时长（天） <span className="text-destructive">*</span></Label>
                <Input className="h-8 text-sm" type="number" placeholder="30" value={draft.durationDays} onChange={(e) => setDraft({ ...draft, durationDays: e.target.value })} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">价格 <span className="text-destructive">*</span></Label>
                <Input className="h-8 text-sm" placeholder="19.90" value={draft.price} onChange={(e) => setDraft({ ...draft, price: e.target.value })} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">划线原价（可选）</Label>
                <Input className="h-8 text-sm" placeholder="29.90" value={draft.originalPrice} onChange={(e) => setDraft({ ...draft, originalPrice: e.target.value })} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">赠送积分</Label>
                <Input className="h-8 text-sm" type="number" placeholder="0" value={draft.bonusIntegral} onChange={(e) => setDraft({ ...draft, bonusIntegral: e.target.value })} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">排序（越小越靠前）</Label>
                <Input className="h-8 text-sm" type="number" value={draft.sortOrder} onChange={(e) => setDraft({ ...draft, sortOrder: e.target.value })} />
              </div>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">套餐说明</Label>
              <Textarea className="text-xs" rows={3} placeholder="展示给用户的套餐权益说明" value={draft.description} onChange={(e) => setDraft({ ...draft, description: e.target.value })} />
            </div>
            <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5">
              <Checkbox checked={draft.isActive} onCheckedChange={(v) => setDraft({ ...draft, isActive: v === true })} />
              <span className="text-xs">上架在售（下架后用户不可购买，已购不受影响）</span>
            </label>
            <div className="flex justify-end gap-2 pt-2">
              <Button size="sm" variant="outline" className="text-xs" onClick={() => setEditorOpen(false)}>取消</Button>
              <Button size="sm" className="text-xs" disabled={saveMutation.isPending} onClick={() => void handleSave()}>
                {saveMutation.isPending ? "保存中…" : "保存"}
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}
