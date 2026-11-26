"use client";

import { useState } from "react";
import { CheckCircle2, CircleDashed, History, Loader2, RotateCcw, Rocket, XCircle } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { StateBlock, errorMessage } from "@/components/users/detail/kit";
import type { CosmeticItem, CosmeticRevision, CosmeticSlot, Design } from "@/lib/api/cosmetics";
import { useCosmeticRevisionsQuery, useRestoreCosmeticRevisionMutation } from "@/lib/cosmetic-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { SlotDesign } from "../cosmetic-preview";
import { isPlateSlot } from "../cosmetic-shared";

type StepState = "idle" | "running" | "done" | "skipped" | "failed";

export type PublishStep = { key: "save" | "preview" | "publish"; label: string; state: StepState; detail?: string };

/**
 * 发布：填说明 → 先保存草稿（有未保存的修改时）→ 用离屏 canvas 把当前帧渲染成 PNG 上传为预览图 → 发布。
 * 预览图失败（素材没开跨域读取）不拦发布，如实标出来。
 */
export function PublishDialog({
  open,
  onOpenChange,
  item,
  slot,
  design,
  time,
  onPublish
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  item: CosmeticItem;
  slot: CosmeticSlot;
  design: Design;
  time: number;
  onPublish: (note: string, onStep: (steps: PublishStep[]) => void) => Promise<boolean>;
}) {
  const [note, setNote] = useState("");
  const [steps, setSteps] = useState<PublishStep[] | null>(null);
  const running = Boolean(steps?.some((step) => step.state === "running"));

  const submit = async () => {
    const ok = await onPublish(note.trim(), setSteps);
    if (ok) {
      onOpenChange(false);
      setNote("");
      setSteps(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !running && onOpenChange(next)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>发布第 {item.revision + 1} 版</DialogTitle>
          <DialogDescription>发布后客户端按新版本绘制，旧版本保留在版本历史里</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex h-36 items-center justify-center rounded-lg border bg-muted/40">
            <SlotDesign
              slot={slot}
              design={design}
              width={slot === "profile_card" || isPlateSlot(slot) ? 240 : 128}
              height={slot === "profile_card" ? 135 : isPlateSlot(slot) ? 96 : 128}
              time={time}
              nameText={item.name}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">发布说明</Label>
            <Textarea value={note} maxLength={128} rows={2} onChange={(event) => setNote(event.target.value)} placeholder="选填，例如：调整外环转速" disabled={running} />
          </div>
          {steps ? (
            <ol className="flex flex-col gap-1.5 rounded-lg border p-3">
              {steps.map((step) => (
                <li key={step.key} className="flex items-center gap-2 text-sm">
                  {step.state === "running" ? (
                    <Loader2 className="size-4 animate-spin text-muted-foreground" />
                  ) : step.state === "done" ? (
                    <CheckCircle2 className="size-4 text-emerald-500" />
                  ) : step.state === "failed" ? (
                    <XCircle className="size-4 text-red-500" />
                  ) : (
                    <CircleDashed className="size-4 text-muted-foreground" />
                  )}
                  <span className={cn(step.state === "skipped" && "text-muted-foreground")}>{step.label}</span>
                  {step.detail ? <span className="ml-auto truncate text-xs text-muted-foreground">{step.detail}</span> : null}
                </li>
              ))}
            </ol>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={running}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={running}>
            {running ? <Loader2 className="animate-spin" /> : <Rocket />}
            发布
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 版本历史：渲染每个版本；「放回草稿」只改草稿，不直接发布 */
export function RevisionsSheet({
  open,
  onOpenChange,
  appKey,
  item,
  slot,
  dirty,
  canRestore,
  assetUrl,
  onRestored
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  item: CosmeticItem;
  slot: CosmeticSlot;
  dirty: boolean;
  canRestore: boolean;
  assetUrl?: (id: number) => string | undefined;
  onRestored: (item: CosmeticItem) => void;
}) {
  const query = useCosmeticRevisionsQuery(appKey, item.id, open);
  const restore = useRestoreCosmeticRevisionMutation(appKey);
  const [confirm, setConfirm] = useState<CosmeticRevision | null>(null);
  const revisions = [...(query.data ?? [])].sort((a, b) => b.revision - a.revision);

  const doRestore = async (revision: CosmeticRevision) => {
    try {
      const restored = await restore.mutateAsync({ itemId: item.id, revision: revision.revision });
      toast.success(`已把第 ${revision.revision} 版放回草稿`);
      setConfirm(null);
      onRestored(restored);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col gap-0 p-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle className="flex items-center gap-2">
            <History className="size-4" />
            版本历史
          </SheetTitle>
          <SheetDescription>保留最近 50 个版本</SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          {query.isLoading ? (
            <div className="flex flex-col gap-2">
              {[0, 1, 2].map((index) => (
                <div key={index} className="h-24 animate-pulse rounded-lg bg-muted" />
              ))}
            </div>
          ) : query.isError ? (
            <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
          ) : revisions.length === 0 ? (
            <StateBlock compact icon={History} title="还没有发布过" />
          ) : (
            <ul className="flex flex-col gap-2">
              {revisions.map((revision) => (
                <li key={revision.id} className="flex gap-3 rounded-lg border p-2.5">
                  <div className="flex size-20 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted/50">
                    {revision.design ? (
                      <SlotDesign
                        slot={slot}
                        design={revision.design}
                        width={slot === "name_style" ? 80 : 76}
                        height={slot === "profile_card" ? 43 : 76}
                        fontSize={13}
                        nameText={item.name}
                        assetUrl={assetUrl}
                      />
                    ) : null}
                  </div>
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-semibold">第 {revision.revision} 版</span>
                      {revision.revision === item.revision ? (
                        <Badge variant="success" size="sm">
                          当前
                        </Badge>
                      ) : null}
                    </div>
                    <span className="truncate text-xs text-muted-foreground">{revision.note || "无说明"}</span>
                    <span className="text-[11px] text-muted-foreground">
                      {formatDateTimeMinute(revision.createdAt)}
                      {revision.publishedBy ? `，${revision.publishedBy}` : ""}
                    </span>
                    {canRestore ? (
                      <Button
                        size="xs"
                        variant="outline"
                        className="mt-1 w-fit"
                        disabled={restore.isPending}
                        onClick={() => (dirty ? setConfirm(revision) : void doRestore(revision))}
                      >
                        <RotateCcw />
                        放回草稿
                      </Button>
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
        <AlertDialog open={Boolean(confirm)} onOpenChange={(next) => !next && setConfirm(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>放回第 {confirm?.revision} 版？</AlertDialogTitle>
              <AlertDialogDescription>草稿里还有没保存的修改，放回后会被这个版本替换。</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <Button onClick={() => confirm && void doRestore(confirm)} disabled={restore.isPending}>
                放回草稿
              </Button>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
  );
}
