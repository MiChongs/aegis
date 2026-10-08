"use client";

import { useState } from "react";
import { KeyRound, Loader2, LogOut, Network } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { OAuth2Grant } from "@/lib/api/oauth2-server";
import {
  useOAuth2OverviewQuery,
  useRevokeUserOAuth2GrantsMutation,
  useUserOAuth2GrantsQuery
} from "@/lib/oauth2-server-hooks";
import { Panel, formatTime } from "./user-detail-shared";

/**
 * 用户详情 › 安全 › 已授权的第三方应用。
 *
 * 列出该用户经 OAuth2 授权服务（Hydra）授权过的客户端。撤销某一个会吊销它持有的令牌；
 * 「全部撤销」另外让用户在授权服务上的登录态失效，下次授权必须重新登录。
 * 授权服务未启用时整块不出现。
 */
export function UserOAuth2GrantsPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const overviewQuery = useOAuth2OverviewQuery(appKey);
  const usable = Boolean(overviewQuery.data?.enabled && overviewQuery.data.ready);
  const grantsQuery = useUserOAuth2GrantsQuery(appKey, userId, usable);
  const revoke = useRevokeUserOAuth2GrantsMutation(appKey, userId);
  const [pending, setPending] = useState<OAuth2Grant | "all" | null>(null);

  if (!usable) return null;
  const grants = grantsQuery.data?.items ?? [];

  async function handleRevoke() {
    if (!pending) return;
    try {
      await revoke.mutateAsync(pending === "all" ? undefined : pending.clientId);
      toast.success(pending === "all" ? "已撤销全部授权" : `已撤销对「${pending.clientName}」的授权`);
      setPending(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "撤销失败");
    }
  }

  return (
    <>
      <Panel
        title="已授权的第三方应用"
        icon={<Network className="size-4" />}
        description="该用户经 OAuth2 授权服务登录过的客户端"
        action={
          grants.length > 0 ? (
            <Button size="sm" variant="outline" className="h-8 gap-1 text-xs" onClick={() => setPending("all")}>
              <LogOut className="size-3" />
              全部撤销
            </Button>
          ) : null
        }
      >
        {grantsQuery.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-14 w-full rounded-xl" />
            <Skeleton className="h-14 w-full rounded-xl" />
          </div>
        ) : grants.length === 0 ? (
          <div className="py-6 text-center text-sm text-muted-foreground">暂无授权记录</div>
        ) : (
          <div className="space-y-2">
            {grants.map((grant) => (
              <div
                key={grant.clientId}
                className="flex flex-col gap-3 rounded-xl border px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between"
              >
                <div className="flex min-w-0 items-center gap-3">
                  {grant.logoUri ? (
                    // eslint-disable-next-line @next/next/no-img-element -- 接入方自填的任意外链，不走 next/image 的域名白名单
                    <img src={grant.logoUri} alt="" className="size-9 shrink-0 rounded-lg object-cover ring-1 ring-border" />
                  ) : (
                    <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                      <KeyRound className="size-4" />
                    </div>
                  )}
                  <div className="min-w-0 space-y-1">
                    <div className="truncate text-sm font-medium">{grant.clientName || grant.clientId}</div>
                    <div className="flex flex-wrap items-center gap-1">
                      {grant.scopes.map((scope) => (
                        <Badge key={scope} variant="outline" size="sm" className="font-mono">
                          {scope}
                        </Badge>
                      ))}
                      <span className="text-[11px] text-muted-foreground">授权于 {formatTime(grant.grantedAt)}</span>
                    </div>
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-8 shrink-0 self-end text-xs text-destructive hover:text-destructive sm:self-auto"
                  onClick={() => setPending(grant)}
                >
                  撤销
                </Button>
              </div>
            ))}
          </div>
        )}
      </Panel>

      <AlertDialog open={Boolean(pending)} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pending === "all" ? "撤销全部授权？" : `撤销对「${pending?.clientName}」的授权？`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pending === "all"
                ? "全部客户端持有的令牌立即失效，该用户在授权服务上的登录状态一并清除。"
                : "该客户端持有的访问令牌与刷新令牌立即失效，用户下次使用时需要重新授权。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleRevoke} disabled={revoke.isPending}>
              {revoke.isPending ? <Loader2 className="size-3 animate-spin" /> : null}
              确认撤销
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
