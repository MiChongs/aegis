"use client";

import { useState } from "react";
import { ChevronLeft, ChevronRight, Crown, RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useAdminVipTransactionsQuery } from "@/lib/admin-hooks";
import { formatMoney, formatTime } from "./commerce-format";

const channelLabels: Record<string, string> = {
  wallet: "余额直购",
  payment_order: "支付订单",
  admin_grant: "管理员授予"
};

/**
 * 会员开通记录。
 *
 * 与订单页并列而不是合并：余额直购走的是 `wallet` 渠道，**不产生支付订单**，
 * 在订单页一条都看不到。只看订单会以为会员卖得很差。
 */
export function VipTransactionsPanel({ appId }: { appId?: number | null }) {
  const [page, setPage] = useState(1);
  const query = useAdminVipTransactionsQuery(appId, { page, limit: 20 });

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / 20));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-end gap-2">
        <span className="text-xs tabular-nums text-muted-foreground">共 {total} 条</span>
        <Button size="sm" variant="ghost" className="h-7 gap-1 text-xs" onClick={() => void query.refetch()}>
          <RefreshCw className="size-3" />
          刷新
        </Button>
      </div>

      {query.isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-10 w-full rounded-lg" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无会员开通记录</div>
      ) : (
        <div className="overflow-x-auto rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>流水号</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>套餐</TableHead>
                <TableHead className="text-right">时长</TableHead>
                <TableHead className="text-right">金额</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>到期时间</TableHead>
                <TableHead>开通时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="font-mono text-xs">{item.transactionNo}</TableCell>
                  <TableCell className="text-xs tabular-nums text-muted-foreground">UID-{item.userId}</TableCell>
                  <TableCell className="max-w-40 truncate text-xs">
                    <span className="flex items-center gap-1.5">
                      <Crown className="size-3 text-amber-500" />
                      {item.planName}
                    </span>
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{item.durationDays} 天</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{formatMoney(item.payAmount)}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="text-[10px]">
                      {channelLabels[item.payChannel] ?? item.payChannel}
                    </Badge>
                    {item.relatedOrderNo ? (
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{item.relatedOrderNo}</span>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(item.expireAfter)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(item.createdAt)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-end gap-2">
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={page <= 1}
            onClick={() => setPage((value) => value - 1)}
          >
            <ChevronLeft className="size-3" />
            上一页
          </Button>
          <span className="text-xs tabular-nums text-muted-foreground">
            {page} / {totalPages}
          </span>
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={page >= totalPages}
            onClick={() => setPage((value) => value + 1)}
          >
            下一页
            <ChevronRight className="size-3" />
          </Button>
        </div>
      )}
    </div>
  );
}
