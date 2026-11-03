"use client";

import { useState } from "react";
import { Ban, ChevronLeft, ChevronRight, Loader2, Plus, Search, Trash2 } from "lucide-react";
import { toast } from "sonner";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { EmailSuppression } from "@/lib/api/email-policy";
import { formatDateTimeMinute, fromLocalInputValue, parseInstant } from "@/lib/datetime";
import {
  useCreateEmailSuppressionMutation,
  useDeleteEmailSuppressionMutation,
  useEmailSuppressionsQuery,
  type EmailScope
} from "@/lib/email-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { SUPPRESSION_REASON, SUPPRESSION_SOURCE, formatNumber } from "./email-shared";

const PAGE_SIZE = 30;

/**
 * 抑制名单：不再往这些地址发信。
 *
 * 硬退信与投诉由投递回执自动加入（发信策略里可关），也可以手动添加（例如用户要求不再接收）。
 * 被抑制的地址收不到任何邮件，包括验证码 —— 用户换了邮箱或修好了邮箱之后，在这里移出即可。
 */
export function EmailSuppressionsPanel({ scope, canWrite }: { scope: EmailScope; canWrite: boolean }) {
  const [reason, setReason] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<EmailSuppression | null>(null);
  const debounced = useDebouncedValue(keyword, 300);
  const query = useEmailSuppressionsQuery(scope, {
    reason: reason === "all" ? undefined : reason,
    keyword: debounced.trim() || undefined,
    page,
    pageSize: PAGE_SIZE
  });
  const removeMutation = useDeleteEmailSuppressionMutation(scope);
  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const [now] = useState(() => Date.now());

  const remove = async () => {
    if (!removing) return;
    try {
      await removeMutation.mutateAsync(removing.id);
      toast.success("已移出抑制名单");
      setRemoving(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-64">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-sm"
            placeholder="邮箱地址或备注"
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
          />
        </div>
        <Select
          value={reason}
          onValueChange={(value) => {
            setReason(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部原因</SelectItem>
            {Object.entries(SUPPRESSION_REASON).map(([value, meta]) => (
              <SelectItem key={value} value={value}>
                {meta.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs text-muted-foreground">共 {formatNumber(total)} 个地址</span>
          {canWrite ? (
            <Button size="sm" className="h-8" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" /> 添加地址
            </Button>
          ) : null}
        </div>
      </div>

      <div className="overflow-hidden rounded-2xl border bg-card">
        {query.isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 5 }).map((_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-16 text-center">
            <Ban className="size-6 text-muted-foreground/60" />
            <p className="text-sm text-muted-foreground">抑制名单为空</p>
            <p className="text-xs text-muted-foreground">硬退信与投诉的地址会由投递回执自动加入</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">地址</th>
                  <th className="px-3 py-2.5 text-left font-medium">原因</th>
                  <th className="px-3 py-2.5 text-left font-medium">备注</th>
                  <th className="px-3 py-2.5 text-left font-medium">有效期</th>
                  <th className="px-3 py-2.5 text-left font-medium">加入时间</th>
                  {canWrite ? <th className="w-12 px-3 py-2.5" /> : null}
                </tr>
              </thead>
              <tbody>
                {items.map((item) => {
                  const reasonMeta = SUPPRESSION_REASON[item.reason] ?? { label: item.reason, tone: "outline" as const };
                  const expiresAt = parseInstant(item.expiresAt);
                  const expired = expiresAt ? expiresAt.getTime() <= now : false;
                  return (
                    <tr key={item.id} className={cn("border-b last:border-b-0", expired && "opacity-60")}>
                      <td className="px-4 py-2.5 font-mono text-xs">{item.address}</td>
                      <td className="whitespace-nowrap px-3 py-2.5">
                        <Badge variant={reasonMeta.tone} size="sm">
                          {reasonMeta.label}
                        </Badge>
                        <span className="ml-1.5 text-[11px] text-muted-foreground">
                          {SUPPRESSION_SOURCE[item.source] ?? item.source}
                          {item.createdBy ? `，${item.createdBy}` : ""}
                        </span>
                      </td>
                      <td className="max-w-[18rem] truncate px-3 py-2.5 text-xs text-muted-foreground" title={item.note}>
                        {item.note || "—"}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs">
                        {expiresAt ? (
                          expired ? (
                            <span className="text-muted-foreground">已到期</span>
                          ) : (
                            `至 ${formatDateTimeMinute(item.expiresAt)}`
                          )
                        ) : (
                          <span className="text-muted-foreground">长期</span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs tabular-nums text-muted-foreground">
                        {formatDateTimeMinute(item.createdAt)}
                      </td>
                      {canWrite ? (
                        <td className="px-3 py-2.5 text-right">
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7 text-muted-foreground hover:text-destructive"
                            aria-label="移出"
                            onClick={() => setRemoving(item)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </td>
                      ) : null}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {totalPages > 1 ? (
        <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
          <Button size="icon" variant="outline" className="size-7" aria-label="上一页" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            <ChevronLeft className="size-3.5" />
          </Button>
          <span className="tabular-nums">
            {page} / {totalPages}
          </span>
          <Button
            size="icon"
            variant="outline"
            className="size-7"
            aria-label="下一页"
            disabled={page >= totalPages}
            onClick={() => setPage(page + 1)}
          >
            <ChevronRight className="size-3.5" />
          </Button>
        </div>
      ) : null}

      <AddSuppressionDialog scope={scope} open={adding} onOpenChange={setAdding} />

      <AlertDialog open={Boolean(removing)} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移出 {removing?.address}</AlertDialogTitle>
            <AlertDialogDescription>
              移出后会恢复向这个地址发信。如果它是因硬退信加入的，地址仍不可用时再次发信还会被退回。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                event.preventDefault();
                void remove();
              }}
              disabled={removeMutation.isPending}
            >
              移出
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function AddSuppressionDialog({
  scope,
  open,
  onOpenChange
}: {
  scope: EmailScope;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const mutation = useCreateEmailSuppressionMutation(scope);
  const [address, setAddress] = useState("");
  const [reason, setReason] = useState("manual");
  const [note, setNote] = useState("");
  const [expires, setExpires] = useState("");

  const close = () => {
    onOpenChange(false);
    setAddress("");
    setReason("manual");
    setNote("");
    setExpires("");
  };

  const submit = async () => {
    try {
      await mutation.mutateAsync({
        address: address.trim(),
        reason,
        note: note.trim(),
        expires_at: expires ? fromLocalInputValue(expires) : null
      });
      toast.success("已加入抑制名单");
      close();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "添加失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? onOpenChange(true) : close())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>添加到抑制名单</DialogTitle>
          <DialogDescription>加入后不再向这个地址发送任何邮件，包括验证码。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">
              邮箱地址<span className="text-destructive"> *</span>
            </Label>
            <Input type="email" value={address} placeholder="user@example.com" onChange={(event) => setAddress(event.target.value)} />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">原因</Label>
              <Select value={reason} onValueChange={setReason}>
                <SelectTrigger className="w-full text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(SUPPRESSION_REASON).map(([value, meta]) => (
                    <SelectItem key={value} value={value}>
                      {meta.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">到期时间</Label>
              <Input type="datetime-local" value={expires} onChange={(event) => setExpires(event.target.value)} className="text-sm" />
            </div>
          </div>
          <div className="space-y-1.5">
            <div className="flex items-baseline justify-between">
              <Label className="text-[11px] font-medium">备注</Label>
              <span className="text-[10px] tabular-nums text-muted-foreground">{note.length}/200</span>
            </div>
            <Textarea value={note} maxLength={200} rows={2} placeholder="例如：用户要求不再接收" onChange={(event) => setNote(event.target.value)} />
          </div>
          <p className="text-[11px] text-muted-foreground">到期时间留空表示长期有效。</p>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={close}>
              取消
            </Button>
            <Button type="submit" disabled={!address.trim() || mutation.isPending}>
              {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              添加
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
