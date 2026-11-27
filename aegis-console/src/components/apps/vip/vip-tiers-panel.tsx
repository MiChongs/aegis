"use client";

import { useMemo, useState } from "react";
import { Layers, Loader2, Pencil, Plus, Trash2, Users } from "lucide-react";
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
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { VipTierEditor } from "@/components/apps/vip/vip-tier-editor";
import { FeatureTagList, PerkTagList } from "@/components/apps/vip/vip-shared";
import { useAdminVipFeaturesQuery, useAdminVipTiersQuery, useDeleteAdminVipTierMutation } from "@/lib/vip-hooks";
import type { VipTier } from "@/lib/api/vip";

/**
 * 会员档位。
 *
 * 权益只属于档位：每段会员期记的是档位，按档位**当前**的功能算。套餐只是档位的售卖规格，
 * 删套餐、重新上架、调价都不碰已开通的会员。删档位时档位下还有套餐或会员，必须选一个迁往的档位 ——
 * 这是会员权益唯一会整体改变归属的地方，所以单独放一个视图，而不是藏在套餐编辑里。
 */
export function VipTiersPanel({ appKey }: { appKey: string }) {
  const tiersQuery = useAdminVipTiersQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const deleteMutation = useDeleteAdminVipTierMutation(appKey);

  const [editing, setEditing] = useState<{ open: boolean; tier: VipTier | null }>({ open: false, tier: null });
  const [pendingDelete, setPendingDelete] = useState<VipTier | null>(null);
  const [moveTo, setMoveTo] = useState<string>("");

  const tiers = useMemo(() => tiersQuery.data ?? [], [tiersQuery.data]);
  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);

  const deleteNeedsTarget = Boolean(pendingDelete && (pendingDelete.members > 0 || pendingDelete.plans.length > 0));
  const targets = tiers.filter((tier) => tier.id !== pendingDelete?.id);

  const openDelete = (tier: VipTier) => {
    setMoveTo("");
    setPendingDelete(tier);
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    if (deleteNeedsTarget && !moveTo) {
      toast.error("请选择迁往的档位");
      return;
    }
    try {
      await deleteMutation.mutateAsync({ tierId: pendingDelete.id, moveTo: moveTo ? Number(moveTo) : undefined });
      toast.success(deleteNeedsTarget ? "档位已删除，套餐与会员已迁移" : "档位已删除");
      setPendingDelete(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  if (tiersQuery.isLoading) {
    return (
      <div className="space-y-3">
        {[0, 1].map((index) => (
          <Skeleton key={index} className="h-24 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Layers className="size-4" />}
        title="会员档位"
        description="会员的权益由档位决定，套餐只是档位的售卖规格"
        aside={
          <Button size="sm" onClick={() => setEditing({ open: true, tier: null })}>
            <Plus className="size-3.5" /> 新建档位
          </Button>
        }
      >
        {tiers.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            暂无档位，新建套餐时会自动创建
          </p>
        ) : (
          <div className="space-y-3">
            {tiers.map((tier) => (
              <div
                key={tier.id}
                className="flex flex-col gap-3 rounded-xl border border-border p-4 sm:flex-row sm:items-start sm:justify-between"
              >
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-semibold">{tier.name}</span>
                    {tier.defaultLifetime ? (
                      <Badge variant="success" size="sm">
                        永久会员默认档位
                      </Badge>
                    ) : null}
                    {tier.plans.length === 0 ? (
                      <Badge variant="secondary" size="sm">
                        无在售套餐
                      </Badge>
                    ) : null}
                  </div>

                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span className="inline-flex items-center gap-1">
                      <Users className="size-3.5" />
                      <span className="font-medium tabular-nums text-foreground">{tier.members}</span> 位在期会员
                      {tier.lifetimeMembers > 0 ? (
                        <span className="tabular-nums">（永久 {tier.lifetimeMembers}）</span>
                      ) : null}
                    </span>
                    {tier.plans.length > 0 ? (
                      <span className="min-w-0 truncate">
                        套餐：{tier.plans.map((plan) => plan.name + (plan.isActive ? "" : "（停用）")).join("、")}
                      </span>
                    ) : null}
                    <span>排序 {tier.sortOrder}</span>
                  </div>

                  {tier.description ? (
                    <p className="text-[11px] leading-snug text-muted-foreground">{tier.description}</p>
                  ) : null}

                  <FeatureTagList tags={tier.features} catalog={features} emptyHint="仅会员身份，无细分权益" />

                  {(tier.autoPerks?.length ?? 0) + (tier.perks?.length ?? 0) > 0 ? (
                    <div className="space-y-1 border-t border-dashed border-border pt-2">
                      <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[10px] text-muted-foreground">
                        <span>权益标签</span>
                        <span className="inline-flex items-center gap-1">
                          <span className="size-1.5 rounded-full bg-violet-500" />
                          自动生成
                        </span>
                        <span className="inline-flex items-center gap-1">
                          <span className="size-1.5 rounded-full bg-sky-500" />
                          补充
                        </span>
                      </div>
                      <PerkTagList perks={[...(tier.autoPerks ?? []), ...(tier.perks ?? [])]} emptyHint={null} />
                    </div>
                  ) : null}
                </div>

                <div className="flex shrink-0 items-center gap-1">
                  <Button variant="ghost" size="icon-sm" onClick={() => setEditing({ open: true, tier })}>
                    <Pencil className="size-3.5" />
                    <span className="sr-only">编辑</span>
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => openDelete(tier)}
                  >
                    <Trash2 className="size-3.5" />
                    <span className="sr-only">删除</span>
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </SectionCard>

      <VipTierEditor
        appKey={appKey}
        open={editing.open}
        onOpenChange={(open) => setEditing((prev) => ({ ...prev, open }))}
        tier={editing.tier}
        features={features}
      />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除档位「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteNeedsTarget
                ? `档位下还有 ${pendingDelete?.plans.length ?? 0} 个套餐、${pendingDelete?.members ?? 0} 位在期会员，将一并迁往所选档位，会员的权益随之改为目标档位的功能。`
                : "档位下没有套餐与在期会员。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteNeedsTarget ? (
            <div className="space-y-1.5">
              <Label className="text-xs">迁往</Label>
              {targets.length === 0 ? (
                <p className="text-[11px] text-destructive">没有其他档位可迁，请先新建一个档位</p>
              ) : (
                <Select value={moveTo} onValueChange={setMoveTo}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择档位" />
                  </SelectTrigger>
                  <SelectContent>
                    {targets.map((tier) => (
                      <SelectItem key={tier.id} value={String(tier.id)}>
                        {tier.name}（{tier.features.length} 项功能）
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void confirmDelete();
              }}
              disabled={deleteMutation.isPending || (deleteNeedsTarget && !moveTo)}
            >
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {deleteNeedsTarget ? "迁移并删除" : "确认删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
