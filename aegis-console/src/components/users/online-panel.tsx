"use client";

import { useState } from "react";
import { useIsFetching, useQueryClient } from "@tanstack/react-query";
import { AppWindow, RefreshCw, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  useAdminAppsQuery,
  useAdminSessionQuery,
  useOnlineAdminsQuery,
  useSystemOnlineStatsQuery
} from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { cn } from "@/lib/utils";
import { AdminSessionsView } from "./online/admin-sessions-view";
import { AppOnlineView } from "./online/app-online-view";
import { OnlineOverview } from "./online/online-overview";

type View = "apps" | "admins";

const REFRESH_MS = 10_000;
const ONLINE_QUERY_KEYS = ["system-online-stats", "app-online-stats", "app-online-users", "online-admins", "all-sessions", "admin-sessions"];

/**
 * 在线会话：上面是全站概览，下面按对象分两个视图。
 *
 *   应用用户 —— Redis presence 里的实时连接，按应用查看，可展开到每条连接、可强制下线。
 *   管理员   —— 控制台登录会话（仅超管），可撤销单个会话或让某位管理员整体下线。
 *
 * 自动刷新默认开启（10 秒），翻页与刷新之间保留上一屏数据，不闪骨架。
 */
export function OnlinePanel() {
  const operator = useAuthStore((s) => s.operator);
  const isSuperAdmin = operator?.isSuperAdmin ?? false;
  const selfId = operator?.id != null ? Number(operator.id) : null;

  const [view, setView] = useState<View>("apps");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const interval = autoRefresh ? REFRESH_MS : false;

  const queryClient = useQueryClient();
  const fetching = useIsFetching({ predicate: (q) => ONLINE_QUERY_KEYS.includes(String(q.queryKey[0])) }) > 0;

  const statsQuery = useSystemOnlineStatsQuery({ refetchInterval: interval });
  const appsQuery = useAdminAppsQuery();
  const onlineAdminsQuery = useOnlineAdminsQuery({ enabled: isSuperAdmin });
  const sessionQuery = useAdminSessionQuery();

  const onlineAdmins = onlineAdminsQuery.data ?? [];
  const activeView: View = isSuperAdmin ? view : "apps";

  function refreshAll() {
    void queryClient.invalidateQueries({
      predicate: (q) => ONLINE_QUERY_KEYS.includes(String(q.queryKey[0]))
    });
  }

  return (
    <div className="space-y-4">
      <OnlineOverview
        stats={statsQuery.data}
        loading={statsQuery.isLoading}
        admins={
          isSuperAdmin
            ? {
                online: onlineAdmins.length,
                sessions: onlineAdmins.reduce((sum, a) => sum + a.sessionCount, 0),
                loading: onlineAdminsQuery.isLoading
              }
            : undefined
        }
      />

      <div className="flex flex-wrap items-center justify-between gap-2">
        {isSuperAdmin ? (
          <ToggleGroup
            type="single"
            value={activeView}
            onValueChange={(v) => v && setView(v as View)}
            className="rounded-lg border bg-muted/40 p-0.5"
          >
            <ToggleGroupItem value="apps" className="h-8 gap-1.5 rounded-md px-3 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
              <AppWindow className="size-3.5" />
              应用用户
            </ToggleGroupItem>
            <ToggleGroupItem value="admins" className="h-8 gap-1.5 rounded-md px-3 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
              <ShieldCheck className="size-3.5" />
              管理员
              {onlineAdmins.length > 0 ? (
                <span className="rounded-full bg-emerald-500/15 px-1.5 text-[10px] leading-4 text-emerald-700 tabular-nums dark:text-emerald-300">
                  {onlineAdmins.length}
                </span>
              ) : null}
            </ToggleGroupItem>
          </ToggleGroup>
        ) : (
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <AppWindow className="size-4 text-muted-foreground" />
            应用在线用户
          </h2>
        )}

        <div className="flex items-center gap-2">
          <label className="flex cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs">
            <Switch checked={autoRefresh} onCheckedChange={setAutoRefresh} aria-label="自动刷新" />
            <span>自动刷新</span>
            <span className="hidden text-muted-foreground sm:inline">{autoRefresh ? "10 秒" : "已暂停"}</span>
          </label>
          <Button variant="outline" size="icon" className="size-8" aria-label="立即刷新" onClick={refreshAll}>
            <RefreshCw className={cn("size-3.5", fetching && "animate-spin")} />
          </Button>
        </div>
      </div>

      {activeView === "apps" ? (
        <AppOnlineView apps={appsQuery.data ?? []} appsLoading={appsQuery.isLoading} autoRefresh={interval} />
      ) : (
        <AdminSessionsView
          onlineAdmins={onlineAdmins}
          onlineLoading={onlineAdminsQuery.isLoading}
          currentSessionId={sessionQuery.data?.tokenId}
          selfId={selfId}
          autoRefresh={interval}
        />
      )}
    </div>
  );
}
