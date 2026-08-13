"use client";

import { useMemo, useState } from "react";
import { ReceiptText } from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  formatCardDate,
  redemptionSourceLabel
} from "@/components/apps/card-key/card-key-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { useCardKeyBatchesQuery, useCardKeyRedemptionsQuery } from "@/lib/card-key-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

/**
 * 核销记录。
 *
 * 展示的是 `results`（**实际发出去了什么**）而不是 `rewards`（卡上配的是什么）。
 * 两者在绝大多数时候一致，但排障要的恰恰是不一致的那些情况 ——
 * 「用户说没到账」只有实际结果能回答。
 */
export function CardKeyRedemptionsPanel({ appKey }: { appKey: string }) {
  const [keyword, setKeyword] = useState("");
  const [batchId, setBatchId] = useState<number | undefined>(undefined);
  const [page, setPage] = useState(1);

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const batchesQuery = useCardKeyBatchesQuery(appKey);
  const listQuery = useCardKeyRedemptionsQuery(appKey, {
    batchId,
    keyword: debouncedKeyword || undefined,
    page,
    limit: 20
  });

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / 20));
  const batches = batchesQuery.data ?? [];

  return (
    <SectionCard
      icon={<ReceiptText className="size-4" />}
      title="核销记录"
      description="谁在什么时候用了哪张卡，以及实际发放的结果"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索卡面或账号"
          className="h-8 w-56"
        />
        <Select
          value={batchId ? String(batchId) : "all"}
          onValueChange={(value) => {
            setBatchId(value === "all" ? undefined : Number(value));
            setPage(1);
          }}
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
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {keyword || batchId ? "当前筛选条件下没有核销记录。" : "还没有人使用过卡密。"}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>卡密</TableHead>
              <TableHead>账号</TableHead>
              <TableHead>实际发放</TableHead>
              <TableHead>来源</TableHead>
              <TableHead className="text-right">时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell>
                  <code className="font-mono text-xs">{item.code}</code>
                  {item.batchName ? (
                    <p className="mt-0.5 text-xs text-muted-foreground">{item.batchName}</p>
                  ) : null}
                </TableCell>
                <TableCell className="text-xs">{item.account || `#${item.userId}`}</TableCell>
                <TableCell>
                  {item.results.length === 0 ? (
                    <span className="text-xs text-muted-foreground">无权益（仅授权）</span>
                  ) : (
                    <div className="flex flex-wrap gap-1">
                      {item.results.map((result) => (
                        <Badge
                          key={result.type}
                          variant="outline"
                          size="sm"
                          className="font-normal"
                          title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
                        >
                          {result.detail || result.label}
                        </Badge>
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {redemptionSourceLabel(item.source)}
                  {item.clientIp ? <span className="ml-1 font-mono">{item.clientIp}</span> : null}
                </TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {formatCardDate(item.createdAt)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {total > 0 ? (
        <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
          <span className="tabular-nums">
            共 {total} 条 · 第 {page}/{totalPages} 页
          </span>
          <div className="flex gap-1.5">
            <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((v) => v - 1)}>
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={page >= totalPages}
              onClick={() => setPage((v) => v + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      ) : null}
    </SectionCard>
  );
}
