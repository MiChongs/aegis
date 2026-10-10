"use client";

import type { ReactNode } from "react";
import { Activity, AppWindow, Cable, ShieldCheck, Users } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { formatTime } from "@/components/users/detail/user-detail-shared";
import type { OnlineStats } from "@/lib/api/types";
import { cn } from "@/lib/utils";

/**
 * 全站在线概览。数字来自 Redis presence 的实时快照，
 * 管理员两格来自管理员会话表，只对超管显示。
 */
export function OnlineOverview({
  stats,
  loading,
  admins
}: {
  stats?: OnlineStats;
  loading: boolean;
  /** 超管才有；undefined 表示不显示管理员两格 */
  admins?: { online: number; sessions: number; loading: boolean };
}) {
  const perUser = stats && stats.onlineUsers > 0 ? stats.onlineConnections / stats.onlineUsers : 0;
  const tiles: Array<{ key: string; icon: ReactNode; label: string; value?: number | string; hint?: string; loading: boolean }> = [
    {
      key: "users",
      icon: <Users className="size-3.5" />,
      label: "在线用户",
      value: stats?.onlineUsers,
      hint: "按应用分别计数",
      loading
    },
    {
      key: "connections",
      icon: <Cable className="size-3.5" />,
      label: "实时连接",
      value: stats?.onlineConnections,
      hint: perUser > 0 ? `人均 ${perUser.toFixed(1)} 条` : "暂无连接",
      loading
    },
    {
      key: "apps",
      icon: <AppWindow className="size-3.5" />,
      label: "活跃应用",
      value: stats?.onlineApps,
      hint: "存在在线用户的应用",
      loading
    }
  ];
  if (admins) {
    tiles.push({
      key: "admins",
      icon: <ShieldCheck className="size-3.5" />,
      label: "在线管理员",
      value: admins.online,
      hint: `${admins.sessions} 个有效会话`,
      loading: admins.loading
    });
  }

  return (
    <div className="overflow-hidden rounded-2xl border bg-card text-card-foreground">
      <div className={cn("grid grid-cols-2", admins ? "lg:grid-cols-4" : "lg:grid-cols-3")}>
        {tiles.map((tile, index) => (
          <div
            key={tile.key}
            className={cn(
              "min-w-0 px-4 py-3.5",
              index % 2 === 1 && "border-l",
              index >= 2 && "border-t lg:border-t-0",
              index >= 1 && "lg:border-l"
            )}
          >
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              {tile.icon}
              {tile.label}
            </div>
            {tile.loading ? (
              <Skeleton className="mt-1.5 h-7 w-14" />
            ) : (
              <div className="mt-1 text-2xl leading-8 font-semibold tracking-tight tabular-nums">
                {typeof tile.value === "number" ? tile.value.toLocaleString("zh-CN") : tile.value ?? "0"}
              </div>
            )}
            <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{tile.loading ? " " : tile.hint}</div>
          </div>
        ))}
      </div>
      <div className="flex items-center gap-1.5 border-t px-4 py-2 text-[11px] text-muted-foreground">
        <Activity className="size-3" />
        {stats?.refreshedAt ? `快照时间 ${formatTime(stats.refreshedAt)}` : loading ? "正在获取快照" : "暂无快照"}
      </div>
    </div>
  );
}
