"use client";

import { useEffect, useMemo, useState } from "react";
import type { DataTableColumnDef } from "./data-table";
import { Wifi } from "lucide-react";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { LoadingState } from "@/components/ui/data-state";
import { useAdminAppsQuery, useAppOnlineUsersQuery } from "@/lib/admin-hooks";
import { DataTable } from "./data-table";

type OnlineRow = { account: string; nickname: string; ip: string; connectedAt: string };

function textVal(v: unknown, fb = "—") { return typeof v === "string" && v.trim() ? v : fb; }

function fmtTime(v: unknown) {
  if (typeof v !== "string") return "—";
  const d = new Date(v);
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

const columns: DataTableColumnDef<OnlineRow>[] = [
  { accessorKey: "account", header: "用户", cell: ({ row }) => <span className="font-medium">{row.original.nickname || row.original.account}</span> },
  { accessorKey: "ip", header: "IP 地址", cell: ({ getValue }) => <span className="font-mono text-xs text-muted-foreground">{getValue() as string}</span> },
  { accessorKey: "connectedAt", header: "连接时间", cell: ({ getValue }) => <span className="text-xs tabular-nums text-muted-foreground">{fmtTime(getValue())}</span> }
];

export function OnlinePanel() {
  const appsQuery = useAdminAppsQuery();
  const apps = appsQuery.data || [];
  const [selectedAppId, setSelectedAppId] = useState<number | null>(null);

  useEffect(() => {
    if (apps.length > 0 && !selectedAppId) setSelectedAppId(apps[0].id);
  }, [apps, selectedAppId]);

  const onlineQuery = useAppOnlineUsersQuery(selectedAppId, { page: 1, limit: 100 });
  const rawItems = onlineQuery.data?.items || [];

  const rows: OnlineRow[] = useMemo(() => rawItems.map((item) => ({
    account: textVal(item.account, item.userId ? `#${item.userId}` : "—"),
    nickname: textVal(item.nickname, ""),
    // IP 顶层没有时回落到样本连接：两处是同一个值，后端把它提到顶层是为了省掉这层挖掘。
    ip: textVal(item.ip, textVal(item.sampleConnection?.ip)),
    connectedAt: textVal(item.connectedAt, textVal(item.lastSeenAt))
  })), [rawItems]);

  if (appsQuery.isLoading) return <LoadingState title="加载中" />;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        {apps.length > 0 && (
          <Select value={selectedAppId ? String(selectedAppId) : ""} onValueChange={(v) => setSelectedAppId(Number(v))}>
            <SelectTrigger className="h-8 w-44 text-xs"><SelectValue placeholder="选择应用" /></SelectTrigger>
            <SelectContent>
              {apps.map((app) => <SelectItem key={app.id} value={String(app.id)}>{app.name}</SelectItem>)}
            </SelectContent>
          </Select>
        )}
        <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Wifi className="size-3" />
          {rows.length} 个在线
        </span>
      </div>
      <DataTable columns={columns} data={rows} emptyText={selectedAppId ? "暂无在线用户" : "请先选择应用"} />
    </div>
  );
}
