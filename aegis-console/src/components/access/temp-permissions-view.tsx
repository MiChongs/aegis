"use client";

import { useMemo, useState } from "react";
import { Hourglass, Plus, Search, Timer, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useTempPermissionsQuery } from "@/lib/admin-hooks";
import {
  useAccessAdminsQuery,
  usePermissionCatalogQuery,
  useRevokeTempPermissionMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { tierOf } from "@/lib/api/access";
import { formatDateTimeMinute } from "@/lib/datetime";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { AdminIdentity, CodeText, Pane } from "./access-kit";
import { AdminMultiPicker, useAppOptions } from "./pickers";
import { TempPermissionGrantForm } from "./temp-permission-form";
import { buildTreeIndex, permissionLabel } from "./tree-model";

function remaining(expiresAt: string) {
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (ms <= 0) return { label: "已到期", urgent: true };
  const hours = ms / 3600_000;
  if (hours < 1)
    return {
      label: `${Math.max(1, Math.round(ms / 60_000))} 分钟`,
      urgent: true,
    };
  if (hours < 24)
    return { label: `${Math.round(hours)} 小时`, urgent: hours < 4 };
  return { label: `${Math.round(hours / 24)} 天`, urgent: false };
}

/** 临时权限：全部未过期的临时授权，按剩余时间排序；可批量审视与撤销。 */
export function TempPermissionsView() {
  const can = usePermissionChecker();
  const tempQuery = useTempPermissionsQuery();
  const catalogQuery = usePermissionCatalogQuery();
  const adminsQuery = useAccessAdminsQuery(can("system:admin:read"));
  const revoke = useRevokeTempPermissionMutation();
  const apps = useAppOptions();
  const index = useMemo(
    () => buildTreeIndex(catalogQuery.data),
    [catalogQuery.data],
  );
  const [keyword, setKeyword] = useState("");
  const [granting, setGranting] = useState(false);
  const [target, setTarget] = useState<number[]>([]);

  const items = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    return (tempQuery.data ?? [])
      .filter((item) => !item.isRevoked)
      .filter(
        (item) =>
          !k ||
          (item.adminAccount ?? "").toLowerCase().includes(k) ||
          item.permission.includes(k) ||
          permissionLabel(index, item.permission).includes(k),
      )
      .sort((a, b) => a.expiresAt.localeCompare(b.expiresAt));
  }, [tempQuery.data, keyword, index]);

  const doRevoke = async (id: number) => {
    try {
      await revoke.mutateAsync(id);
      toast.success("临时权限已撤销");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "撤销失败");
    }
  };

  const admins = adminsQuery.data ?? [];
  const byId = new Map(admins.map((entry) => [entry.account.id, entry]));

  return (
    <Pane
      title={
        <span className="flex items-center gap-2">
          临时权限
          <span className="text-xs font-normal text-muted-foreground tabular-nums">
            {items.length} 条
          </span>
        </span>
      }
      icon={Timer}
      aside={
        <>
          <div className="relative hidden sm:block">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索"
              className="h-8 w-48 pl-8 text-xs"
            />
          </div>
          {can("system:temp_perm:grant") && can("system:admin:read") && (
            <Button
              size="sm"
              className="h-8 gap-1.5"
              onClick={() => {
                setTarget([]);
                setGranting(true);
              }}
            >
              <Plus className="size-3.5" />
              授予
            </Button>
          )}
        </>
      }
    >
      <div className="divide-y divide-border/60">
        {tempQuery.isLoading && (
          <div className="p-4">
            <Skeleton className="h-12" />
          </div>
        )}
        {!tempQuery.isLoading && items.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-14 text-sm text-muted-foreground">
            <Hourglass className="size-6 opacity-50" />
            暂无临时权限
          </div>
        )}
        {items.map((item) => {
          const left = remaining(item.expiresAt);
          const entry = byId.get(item.adminId);
          return (
            <div
              key={item.id}
              className="grid items-center gap-3 px-4 py-2.5 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)_8rem_6rem_2rem]"
            >
              <AdminIdentity
                name={entry?.account.displayName ?? item.adminAccount}
                account={item.adminAccount}
                avatar={entry?.account.avatar}
                tier={entry ? tierOf(entry.account) : undefined}
                size="sm"
              />
              <div className="min-w-0">
                <div className="truncate text-sm">
                  {permissionLabel(index, item.permission)}
                </div>
                <div className="flex flex-wrap gap-x-3 text-[11px] text-muted-foreground">
                  <CodeText>{item.permission}</CodeText>
                  <span>
                    {item.appId
                      ? (apps.find((a) => a.id === item.appId)?.name ??
                        `应用 #${item.appId}`)
                      : "不限应用"}
                  </span>
                  {item.reason && (
                    <span className="truncate">{item.reason}</span>
                  )}
                  {item.granterName && <span>由 {item.granterName} 授予</span>}
                </div>
              </div>
              <span className="text-xs text-muted-foreground tabular-nums">
                至 {formatDateTimeMinute(item.expiresAt)}
              </span>
              <span
                className={cn(
                  "inline-flex h-6 w-fit items-center rounded-full px-2 text-[11px] font-medium tabular-nums",
                  left.urgent
                    ? "bg-amber-500/12 text-amber-700 dark:text-amber-300"
                    : "bg-muted text-muted-foreground",
                )}
              >
                剩余 {left.label}
              </span>
              {can("system:temp_perm:revoke") ? (
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground hover:text-rose-600"
                  aria-label="撤销"
                  onClick={() => void doRevoke(item.id)}
                >
                  <Trash2 className="size-4" />
                </Button>
              ) : (
                <span />
              )}
            </div>
          );
        })}
      </div>

      <Dialog open={granting} onOpenChange={setGranting}>
        <DialogContent className="gap-4 sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>授予临时权限</DialogTitle>
          </DialogHeader>
          <div className="space-y-1.5">
            <Label>管理员</Label>
            <AdminMultiPicker
              admins={admins}
              value={target}
              onChange={(ids) => setTarget(ids.slice(-1))}
              disabledReason={(entry) =>
                tierOf(entry.account) !== "admin"
                  ? "无需授权"
                  : !entry.capabilities.canAccess
                    ? "无权调整"
                    : undefined
              }
            />
          </div>
          {target[0] && catalogQuery.data && (
            <TempPermissionGrantForm
              adminId={target[0]}
              index={index}
              onGranted={() => setGranting(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </Pane>
  );
}
