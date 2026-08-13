"use client";

import { useMemo, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Ban, Copy, Loader2, MonitorSmartphone, RotateCcw, Ticket, Unplug } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  CardKindBadge,
  CardStateBadge,
  formatCardDate,
  formatRemaining
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
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { CardKey } from "@/lib/api/card-key";
import {
  useCardKeyBatchesQuery,
  useCardKeyDevicesQuery,
  useCardKeysQuery,
  useDisableCardKeysMutation,
  useRestoreCardKeysMutation,
  useUnbindCardKeyDeviceMutation
} from "@/lib/card-key-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

/**
 * 单卡列表。
 *
 * 批量作废按**选中的 id 列表**下发，不做「按当前筛选条件批量」：管理员看到的
 * 列表与实际执行之间存在时间差（翻页期间有人兑换），按条件批量会误伤
 * 没被看过的卡，而作废是发出去之后收不回来的动作。
 */
export function CardKeyCodesPanel({
  appKey,
  batchId,
  onBatchIdChange
}: {
  appKey: string;
  batchId?: number;
  onBatchIdChange: (batchId?: number) => void;
}) {
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [confirmDisable, setConfirmDisable] = useState(false);
  const [reason, setReason] = useState("");
  const [deviceCard, setDeviceCard] = useState<CardKey | null>(null);

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const batchesQuery = useCardKeyBatchesQuery(appKey);
  const disableMutation = useDisableCardKeysMutation(appKey);
  const restoreMutation = useRestoreCardKeysMutation(appKey);

  const listQuery = useCardKeysQuery(appKey, {
    batchId,
    status: status === "all" ? undefined : status,
    keyword: debouncedKeyword || undefined,
    page,
    limit: 20
  });

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / 20));
  const batches = batchesQuery.data ?? [];

  // 任一筛选变化都回到第 1 页并清空选中：否则会停在新条件下不存在的页码上
  // （表现为「明明有数据却是空的」），而选中项会指向已经不在列表里的卡。
  const applyFilter = (apply: () => void) => {
    apply();
    setPage(1);
    setSelected(new Set());
  };

  const toggleOne = (id: number, checked: boolean) => {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    setSelected(next);
  };

  const pageIds = items.map((item) => item.id);
  const allSelected = pageIds.length > 0 && pageIds.every((id) => selected.has(id));
  const someSelected = pageIds.some((id) => selected.has(id));

  const toggleAll = (checked: boolean) => {
    const next = new Set(selected);
    for (const id of pageIds) {
      if (checked) next.add(id);
      else next.delete(id);
    }
    setSelected(next);
  };

  const doDisable = async () => {
    try {
      const result = await disableMutation.mutateAsync({ ids: [...selected], reason });
      toast.success(`已作废 ${result.affected} 张`);
      setSelected(new Set());
      setConfirmDisable(false);
      setReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "作废失败");
    }
  };

  const doRestore = async () => {
    try {
      const result = await restoreMutation.mutateAsync([...selected]);
      toast.success(`已恢复 ${result.affected} 张`);
      setSelected(new Set());
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "恢复失败");
    }
  };

  const copyCode = async (code: string) => {
    try {
      await navigator.clipboard.writeText(code);
      toast.success("卡密已复制");
    } catch {
      toast.error("复制失败，请手动选择");
    }
  };

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Ticket className="size-4" />}
        title="卡密"
        description="逐张查看状态、绑定关系与设备；作废后立刻失效，已核销的卡不受影响"
      >
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Input
            value={keyword}
            onChange={(event) => applyFilter(() => setKeyword(event.target.value))}
            placeholder="搜索卡面或备注"
            className="h-8 w-56"
          />
          <Select
            value={batchId ? String(batchId) : "all"}
            onValueChange={(value) =>
              applyFilter(() => onBatchIdChange(value === "all" ? undefined : Number(value)))
            }
          >
            <SelectTrigger className="h-8 w-48">
              <SelectValue placeholder="全部批次" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部批次</SelectItem>
              {batches.map((batch) => (
                <SelectItem key={batch.id} value={String(batch.id)}>
                  {batch.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={status} onValueChange={(value) => applyFilter(() => setStatus(value))}>
            <SelectTrigger className="h-8 w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="unused">未使用</SelectItem>
              <SelectItem value="active">使用中</SelectItem>
              <SelectItem value="used">已核销</SelectItem>
              <SelectItem value="disabled">已作废</SelectItem>
              <SelectItem value="expired">已过期</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {listQuery.isLoading ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
            {keyword || status !== "all" || batchId
              ? "当前筛选条件下没有卡密。"
              : "这个应用还没有生成过卡密。"}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10">
                  <Checkbox
                    checked={allSelected ? true : someSelected ? "indeterminate" : false}
                    aria-label="全选本页"
                    onCheckedChange={(checked) => toggleAll(Boolean(checked))}
                  />
                </TableHead>
                <TableHead>卡密</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>绑定账号</TableHead>
                <TableHead>授权剩余</TableHead>
                <TableHead>设备</TableHead>
                <TableHead className="text-right">生成时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((card) => (
                <TableRow key={card.id}>
                  <TableCell>
                    <Checkbox
                      checked={selected.has(card.id)}
                      aria-label={`选择 ${card.code}`}
                      onCheckedChange={(checked) => toggleOne(card.id, Boolean(checked))}
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1.5">
                      <code className="font-mono text-xs">{card.code}</code>
                      <button
                        type="button"
                        onClick={() => copyCode(card.code)}
                        className="text-muted-foreground transition-colors hover:text-foreground"
                        aria-label="复制卡密"
                      >
                        <Copy className="size-3" />
                      </button>
                      <CardKindBadge kind={card.kind} />
                    </div>
                    {card.disabledReason ? (
                      <p className="mt-0.5 text-xs text-muted-foreground">{card.disabledReason}</p>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    <CardStateBadge card={card} />
                  </TableCell>
                  <TableCell className="text-xs">{card.boundAccount || "—"}</TableCell>
                  <TableCell className="text-xs tabular-nums">
                    {card.kind === "login" ? formatRemaining(card.expiresAt) : "—"}
                  </TableCell>
                  <TableCell>
                    {card.kind === "login" ? (
                      <button
                        type="button"
                        onClick={() => setDeviceCard(card)}
                        className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
                      >
                        <MonitorSmartphone className="size-3" />
                        <span className="tabular-nums">
                          {card.deviceCount}/{card.maxDevices}
                        </span>
                      </button>
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                  </TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">
                    {formatCardDate(card.createdAt)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}

        {total > 0 ? (
          <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
            <span className="tabular-nums">
              共 {total} 张 · 第 {page}/{totalPages} 页
            </span>
            <div className="flex gap-1.5">
              <Button
                size="sm"
                variant="outline"
                disabled={page <= 1}
                onClick={() => {
                  setPage((value) => value - 1);
                  setSelected(new Set());
                }}
              >
                上一页
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= totalPages}
                onClick={() => {
                  setPage((value) => value + 1);
                  setSelected(new Set());
                }}
              >
                下一页
              </Button>
            </div>
          </div>
        ) : null}
      </SectionCard>

      {/* 批量操作条：选中数为 0 时整体卸载，与用户批量操作条同一形状 */}
      <AnimatePresence>
        {selected.size > 0 ? (
          <motion.div
            initial={{ y: 16, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            exit={{ y: 16, opacity: 0 }}
            transition={{ duration: 0.16, ease: "easeOut" }}
            className="sticky bottom-4 z-20 mx-auto flex w-fit max-w-full flex-wrap items-center gap-1.5 rounded-full border bg-popover/95 px-3 py-2 shadow-lg backdrop-blur"
          >
            <span className="px-1 text-xs tabular-nums text-muted-foreground">
              已选 <span className="font-semibold text-foreground">{selected.size}</span> 张
            </span>
            <Button size="sm" variant="ghost" onClick={() => setConfirmDisable(true)}>
              <Ban className="size-3.5" /> 作废
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={doRestore}
              disabled={restoreMutation.isPending}
            >
              <RotateCcw className="size-3.5" /> 恢复
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
              取消
            </Button>
          </motion.div>
        ) : null}
      </AnimatePresence>

      <AlertDialog open={confirmDisable} onOpenChange={setConfirmDisable}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>作废选中的 {selected.size} 张卡密？</AlertDialogTitle>
            <AlertDialogDescription>
              作废后这些卡立刻失效：兑换会被拒，授权卡也登不进去。
              <b>已核销的卡不受影响</b> —— 那笔权益已经发出去了，改它的状态只会让核销记录与卡状态自相矛盾。
              作废是可撤销的，用「恢复」放回未使用。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="px-1">
            <Input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="作废原因（可选，会显示在卡上）"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={doDisable} disabled={disableMutation.isPending}>
              {disableMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认作废
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <CardDevicesSheet
        appKey={appKey}
        card={deviceCard}
        onOpenChange={(open) => !open && setDeviceCard(null)}
      />
    </div>
  );
}

/**
 * 一张卡绑定的设备。
 *
 * 解绑是「用户换电脑了」唯一的出口 —— 设备标识会随重装系统变化，
 * 没有它，一张一机卡在用户重装之后就永久报废了。
 */
function CardDevicesSheet({
  appKey,
  card,
  onOpenChange
}: {
  appKey: string;
  card: CardKey | null;
  onOpenChange: (open: boolean) => void;
}) {
  const devicesQuery = useCardKeyDevicesQuery(appKey, card?.id);
  const unbindMutation = useUnbindCardKeyDeviceMutation(appKey);
  const devices = devicesQuery.data ?? [];

  const unbind = async (deviceId: string) => {
    if (!card) return;
    try {
      await unbindMutation.mutateAsync({ cardId: card.id, deviceId });
      toast.success("已解绑，名额已释放");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解绑失败");
    }
  };

  return (
    <Sheet open={Boolean(card)} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[92vw] max-w-md flex-col gap-0 p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>绑定设备</SheetTitle>
          <SheetDescription>
            <code className="font-mono text-xs">{card?.code}</code>
            {card ? ` · 已绑 ${card.deviceCount}/${card.maxDevices} 台` : null}
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-6 py-5">
          {devicesQuery.isLoading ? (
            <Skeleton className="h-16 w-full" />
          ) : devices.length === 0 ? (
            <p className="rounded-lg border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
              这张卡还没有在任何设备上使用过。
            </p>
          ) : (
            devices.map((device) => (
              <div
                key={device.id}
                className="flex items-start justify-between gap-3 rounded-lg border border-border p-3"
              >
                <div className="min-w-0 space-y-0.5">
                  <p className="truncate text-xs font-medium">{device.deviceName || "未命名设备"}</p>
                  <p className="truncate font-mono text-xs text-muted-foreground">{device.deviceId}</p>
                  <p className="text-xs text-muted-foreground">
                    最近 {formatCardDate(device.lastSeenAt)} · 共 {device.seenCount} 次
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => unbind(device.deviceId)}
                  disabled={unbindMutation.isPending}
                >
                  <Unplug className="size-3.5" /> 解绑
                </Button>
              </div>
            ))
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
