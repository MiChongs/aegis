"use client";

import { useMemo, useState } from "react";
import { Download, Layers, Loader2, Plus, Ticket, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { CardKeyBatchEditor } from "@/components/apps/card-key/card-key-batch-editor";
import {
  CardKindBadge,
  RewardSummary,
  formatCardDate,
  validityLabel
} from "@/components/apps/card-key/card-key-shared";
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
import { ApiError } from "@/lib/api/client";
import type { CardKeyBatch } from "@/lib/api/card-key";
import {
  useCardKeyBatchesQuery,
  useCardKeyCatalogQuery,
  useDeleteCardKeyBatchMutation,
  useExportCardKeyBatchMutation,
  useSetCardKeyBatchStatusMutation
} from "@/lib/card-key-hooks";

/**
 * 批次列表。
 *
 * 每一行回答三件事：这批是什么卡、发什么、用掉多少。核销进度随列表一起下发
 * （后端 `stats`），不另开一个接口 —— 分两次请求会出现「列表已刷新、进度还是
 * 上一次」的画面，而运营正是照着那个数字决定要不要补发。
 */
export function CardKeyBatchesPanel({
  appKey,
  onOpenCodes
}: {
  appKey: string;
  onOpenCodes: (batchId: number) => void;
}) {
  const batchesQuery = useCardKeyBatchesQuery(appKey);
  const catalogQuery = useCardKeyCatalogQuery(appKey);
  const statusMutation = useSetCardKeyBatchStatusMutation(appKey);
  const deleteMutation = useDeleteCardKeyBatchMutation(appKey);
  const exportMutation = useExportCardKeyBatchMutation(appKey);

  const [editorOpen, setEditorOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<CardKeyBatch | null>(null);

  const batches = useMemo(() => batchesQuery.data ?? [], [batchesQuery.data]);
  const catalog = catalogQuery.data?.rewards ?? [];

  const toggleStatus = async (batch: CardKeyBatch, enabled: boolean) => {
    try {
      await statusMutation.mutateAsync({ batchId: batch.id, enabled });
      toast.success(enabled ? `已启用「${batch.name}」` : `已停用「${batch.name}」`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const handleExport = async (batch: CardKeyBatch) => {
    try {
      await exportMutation.mutateAsync(batch.id);
      toast.success("导出已开始下载");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "导出失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMutation.mutateAsync(pendingDelete.id);
      toast.success(`已删除「${pendingDelete.name}」`);
      setPendingDelete(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  if (batchesQuery.isLoading) {
    return (
      <div className="space-y-3">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-28 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Layers className="size-4" />}
        title="卡密批次"
        description="卡密是批量生成的，一批共用同一份权益、有效期与卡面格式"
        aside={
          <Button size="sm" onClick={() => setEditorOpen(true)}>
            <Plus className="size-3.5" /> 生成卡密
          </Button>
        }
      >
        {batches.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            还没有生成过卡密。生成后可导出 CSV 发给渠道，用户在客户端调
            <code className="mx-1 font-mono">/card-keys/redeem</code>兑换，
            授权卡则直接用它登录。
          </p>
        ) : (
          <div className="space-y-3">
            {batches.map((batch) => {
              const stats = batch.stats;
              const consumed = (stats?.used ?? 0) + (stats?.active ?? 0);
              return (
                <div key={batch.id} className="rounded-xl border border-border p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-semibold">{batch.name}</span>
                        <CardKindBadge kind={batch.kind} />
                        {batch.status === "disabled" ? (
                          <Badge variant="danger" size="sm">
                            已停用
                          </Badge>
                        ) : null}
                      </div>
                      <RewardSummary rewards={batch.rewards} catalog={catalog} />
                      <p className="text-xs text-muted-foreground">
                        {validityLabel(batch.validityMode, batch.validityDays, batch.validUntil)}
                        {batch.kind === "login" ? ` · 可绑 ${batch.maxDevices} 台设备` : null}
                        {batch.codePrefix ? ` · 前缀 ${batch.codePrefix}` : null}
                        {` · ${formatCardDate(batch.createdAt)}`}
                        {batch.createdBy ? ` · ${batch.createdBy}` : null}
                      </p>
                      {batch.remark ? (
                        <p className="text-xs text-muted-foreground">{batch.remark}</p>
                      ) : null}
                    </div>

                    <div className="flex shrink-0 items-center gap-1.5">
                      <Switch
                        checked={batch.status === "active"}
                        onCheckedChange={(next) => toggleStatus(batch, next)}
                        aria-label="启用批次"
                      />
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => handleExport(batch)}
                        disabled={exportMutation.isPending}
                      >
                        <Download className="size-3.5" /> 导出
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => onOpenCodes(batch.id)}>
                        <Ticket className="size-3.5" /> 查看卡
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setPendingDelete(batch)}
                        aria-label="删除批次"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  </div>

                  {/* 核销进度：分母是这一批的总数，分子是「已经落到用户头上」的那些
                      （已核销 + 使用中）。未使用的那部分才是还能发出去的库存。 */}
                  <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span className="tabular-nums">
                      共 <span className="font-medium text-foreground">{stats?.total ?? batch.total}</span> 张
                    </span>
                    <span className="tabular-nums">未使用 {stats?.unused ?? 0}</span>
                    <span className="tabular-nums">已用 {consumed}</span>
                    {stats?.disabled ? <span className="tabular-nums">已作废 {stats.disabled}</span> : null}
                    {stats?.expired ? <span className="tabular-nums">已过期 {stats.expired}</span> : null}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>

      <CardKeyBatchEditor appKey={appKey} open={editorOpen} onOpenChange={setEditorOpen} />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除批次「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              这一批的 <b>{pendingDelete?.stats?.total ?? pendingDelete?.total ?? 0}</b> 张卡与它们的核销记录会
              <b>一并删除</b>，已经发出去的卡当场失效。
              {(pendingDelete?.stats?.unused ?? 0) > 0 ? (
                <>
                  {" "}其中还有 <b>{pendingDelete?.stats?.unused}</b> 张没被使用。
                </>
              ) : null}
              {" "}只是想停止发放的话，用上面的开关停用批次即可 —— 那不会动任何已有的卡。
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
