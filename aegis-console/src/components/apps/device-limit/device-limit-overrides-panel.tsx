"use client";

import { useState } from "react";
import { MonitorSmartphone, Pencil, Plus, Search, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { DeviceLimitOverrideDialog } from "@/components/apps/device-limit/device-limit-override-dialog";
import {
  CLASS_META,
  ClassIcon,
  DEVICE_CLASSES,
  DeviceLimitPager,
  limitText
} from "@/components/apps/device-limit/device-limit-shared";
import type { PickedUser } from "@/components/commerce/user-picker";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/lib/api-client";
import type { DeviceLimitOverride } from "@/lib/api/device-limit";
import { formatDateTime, formatRelative, parseInstant } from "@/lib/datetime";
import { useDeleteDeviceLimitOverrideMutation, useDeviceLimitOverridesQuery } from "@/lib/device-limit-hooks";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const LIMIT = 20;

/** 例外名单：豁免或另配上限的用户，优先于会员档位与默认上限。 */
export function DeviceLimitOverridesPanel({
  appKey,
  onOpenUser
}: {
  appKey: string;
  onOpenUser: (user: PickedUser) => void;
}) {
  const can = useAppKeyPermissionChecker(appKey);
  const canWrite = can("app:user:device:manage");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [editing, setEditing] = useState<{ user: PickedUser | null; override: DeviceLimitOverride | null } | null>(null);
  const [deleting, setDeleting] = useState<DeviceLimitOverride | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const query = useDeviceLimitOverridesQuery(appKey, { keyword: debounced || undefined, page, limit: LIMIT });
  const deleteMutation = useDeleteDeviceLimitOverrideMutation(appKey);
  const items = query.data?.items ?? [];
  // 判断「已到期」的基准时刻，挂载时取一次，渲染期不读时钟
  const [now] = useState(() => Date.now());

  async function handleDelete() {
    if (!deleting) return;
    try {
      await deleteMutation.mutateAsync(deleting.userId);
      toast.success("已删除");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    } finally {
      setDeleting(null);
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-72">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
            placeholder="账号、用户 ID 或备注"
            className="h-9 pl-8 text-xs"
          />
        </div>
        {canWrite ? (
          <Button size="sm" className="ml-auto" onClick={() => setEditing({ user: null, override: null })}>
            <Plus className="size-3.5" /> 添加例外
          </Button>
        ) : null}
      </div>

      <div className="rounded-xl border border-border bg-card">
        {query.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-14 text-center">
            <ShieldCheck className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">暂无例外</span>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>用户</TableHead>
                <TableHead>上限</TableHead>
                <TableHead>到期</TableHead>
                <TableHead>备注</TableHead>
                <TableHead>修改</TableHead>
                <TableHead className="w-28" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => {
                const expires = parseInstant(item.expiresAt);
                const expired = Boolean(expires && expires.getTime() <= now);
                const classes = DEVICE_CLASSES.filter((cls) => (item.classLimits?.[cls] ?? 0) > 0);
                const user: PickedUser = { id: item.userId, account: item.account };
                return (
                  <TableRow key={item.userId} className={cn(expired && "opacity-60")}>
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => onOpenUser(user)}
                        className="text-left text-xs font-medium hover:underline"
                      >
                        {item.account || `UID-${item.userId}`}
                      </button>
                      <div className="font-mono text-[10px] text-muted-foreground">UID-{item.userId}</div>
                    </TableCell>
                    <TableCell>
                      {item.exempt ? (
                        <Badge variant="success" size="sm" className="gap-1">
                          <ShieldCheck className="size-3" /> 豁免
                        </Badge>
                      ) : (
                        <div className="flex flex-wrap items-center gap-1">
                          <Badge variant="warning" size="sm">
                            {limitText(item.maxDevices)}
                          </Badge>
                          {classes.map((cls) => (
                            <span
                              key={cls}
                              className={cn("inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] ring-1 ring-inset", CLASS_META[cls].tint)}
                            >
                              <ClassIcon cls={cls} className="size-2.5" />
                              {item.classLimits?.[cls]}
                            </span>
                          ))}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="text-xs">
                      {expires ? (
                        expired ? (
                          <Badge variant="secondary" size="sm">已到期</Badge>
                        ) : (
                          <span title={formatDateTime(expires)}>{formatDateTime(expires)}</span>
                        )
                      ) : (
                        <span className="text-muted-foreground">长期</span>
                      )}
                    </TableCell>
                    <TableCell className="max-w-48 truncate text-xs text-muted-foreground" title={item.note}>
                      {item.note || "—"}
                    </TableCell>
                    <TableCell className="text-[11px] text-muted-foreground">
                      <div>{item.updatedBy || "—"}</div>
                      <div title={formatDateTime(item.updatedAt)}>{formatRelative(item.updatedAt)}</div>
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-0.5">
                        <Button size="icon-sm" variant="ghost" className="size-7" aria-label="查看设备" onClick={() => onOpenUser(user)}>
                          <MonitorSmartphone className="size-3.5" />
                        </Button>
                        {canWrite ? (
                          <>
                            <Button
                              size="icon-sm"
                              variant="ghost"
                              className="size-7"
                              aria-label="编辑"
                              onClick={() => setEditing({ user, override: item })}
                            >
                              <Pencil className="size-3.5" />
                            </Button>
                            <Button
                              size="icon-sm"
                              variant="ghost"
                              className="size-7 text-destructive"
                              aria-label="删除"
                              onClick={() => setDeleting(item)}
                            >
                              <Trash2 className="size-3.5" />
                            </Button>
                          </>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>
      <DeviceLimitPager total={query.data?.total ?? 0} page={page} limit={LIMIT} onPage={setPage} />

      <DeviceLimitOverrideDialog
        appKey={appKey}
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        user={editing?.user ?? null}
        override={editing?.override}
      />

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 {deleting?.account || `UID-${deleting?.userId}`} 的例外？</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
