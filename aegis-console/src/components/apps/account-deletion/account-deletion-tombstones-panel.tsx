"use client";

import { useMemo, useState } from "react";
import { AtSign, Loader2, Lock, LockOpen, Smartphone, UserRound } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { DeletionPager, TOMBSTONE_KIND_LABEL } from "@/components/apps/account-deletion/account-deletion-shared";
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
import { AutoSkeleton } from "@/components/ui/auto-skeleton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { DeletionTombstone, DeletionTombstoneKind } from "@/lib/api/account-deletion";
import { useDeletionTombstonesQuery, useReleaseDeletionTombstoneMutation } from "@/lib/account-deletion-hooks";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;

const KIND_ICON: Record<DeletionTombstoneKind, typeof UserRound> = {
  account: UserRound,
  email: AtSign,
  phone: Smartphone
};

const PLACEHOLDER_ROWS: DeletionTombstone[] = Array.from({ length: 5 }, (_, index) => ({
  id: -1 - index,
  appId: 0,
  kind: "email",
  hint: "ab***@example.com",
  expiresAt: "2026-01-01T00:00:00Z",
  createdAt: "2026-01-01T00:00:00Z",
  account: "placeholder"
}));

/**
 * 重新注册限制：注销时写下的账号名、邮箱、手机号摘要，期满前三种注册都会被拦下（40918）。
 * 只存摘要与打码提示，搜索匹配的是提示与对应申请的账号名。解除后可立即用该标识重新注册。
 */
export function DeletionTombstonesPanel({ appKey }: { appKey: string }) {
  const can = useAppKeyPermissionChecker(appKey);
  const canWrite = can("app:user:deletion:release");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [releasing, setReleasing] = useState<DeletionTombstone | null>(null);

  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const listQuery = useDeletionTombstonesQuery(appKey, {
    keyword: debouncedKeyword || undefined,
    page,
    limit: LIMIT
  });
  const releaseMutation = useReleaseDeletionTombstoneMutation(appKey);

  const loading = listQuery.isLoading;
  const items = useMemo(() => (loading ? PLACEHOLDER_ROWS : listQuery.data?.items ?? []), [loading, listQuery.data]);
  const total = listQuery.data?.total ?? 0;

  const release = async () => {
    if (!releasing) return;
    try {
      await releaseMutation.mutateAsync(releasing.id);
      toast.success("已解除限制");
      setReleasing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解除失败");
    }
  };

  return (
    <SectionCard icon={<Lock className="size-4" />} title="重新注册限制">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索标识或账号"
          className="h-8 w-full sm:w-60"
        />
        {!loading ? (
          <span className="text-xs text-muted-foreground tabular-nums">生效中 {total.toLocaleString("zh-CN")} 条</span>
        ) : null}
      </div>

      {!loading && items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-xs text-muted-foreground">
          {keyword ? "暂无匹配的限制" : "暂无重新注册限制"}
        </p>
      ) : (
        <AutoSkeleton loading={loading}>
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>标识</TableHead>
                  <TableHead className="hidden sm:table-cell">对应账号</TableHead>
                  <TableHead className="text-right">到期</TableHead>
                  {canWrite ? <TableHead className="w-0" /> : null}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => {
                  const Icon = KIND_ICON[item.kind] ?? UserRound;
                  return (
                    <TableRow key={item.id}>
                      <TableCell>
                        <div className="flex items-center gap-2.5">
                          <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                            <Icon className="size-3.5" />
                          </span>
                          <div className="min-w-0">
                            <div className="truncate font-mono text-xs font-medium">{item.hint || "—"}</div>
                            <div className="text-[11px] text-muted-foreground">
                              {TOMBSTONE_KIND_LABEL[item.kind] ?? item.kind}
                              <span className="sm:hidden">{item.account ? `，${item.account}` : ""}</span>
                            </div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="hidden sm:table-cell">
                        <span className="font-mono text-xs">{item.account || "—"}</span>
                      </TableCell>
                      <TableCell className="text-right text-xs whitespace-nowrap">
                        <div className="tabular-nums" title={formatDateTimeMinute(item.expiresAt)}>
                          {formatRelative(item.expiresAt)}
                        </div>
                        <div className="mt-0.5 text-[11px] text-muted-foreground tabular-nums">
                          {formatDateTimeMinute(item.expiresAt)}
                        </div>
                      </TableCell>
                      {canWrite ? (
                        <TableCell className="text-right">
                          <Button size="sm" variant="ghost" className="h-7" onClick={() => setReleasing(item)}>
                            <LockOpen className="size-3.5" /> 解除
                          </Button>
                        </TableCell>
                      ) : null}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </AutoSkeleton>
      )}

      {!loading ? <DeletionPager total={total} page={page} limit={LIMIT} onPage={setPage} /> : null}

      <AlertDialog open={Boolean(releasing)} onOpenChange={(open) => !open && setReleasing(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              解除{releasing ? TOMBSTONE_KIND_LABEL[releasing.kind] ?? "" : ""} {releasing?.hint} 的限制？
            </AlertDialogTitle>
            <AlertDialogDescription>解除后可立即用该标识重新注册。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>返回</AlertDialogCancel>
            <AlertDialogAction onClick={() => void release()} disabled={releaseMutation.isPending}>
              {releaseMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认解除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}
