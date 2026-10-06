"use client";

import { useState } from "react";
import Link from "next/link";
import { ChevronRight, Snowflake, Users } from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { QuotaMeter, formatDateTime } from "@/components/apps/cloud-storage/cloud-storage-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { CloudUserParams } from "@/lib/api/cloud-storage";
import { useCloudStorageUsersQuery } from "@/lib/cloud-storage-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const PAGE_SIZE = 20;

const SORTS: Array<{ value: NonNullable<CloudUserParams["sort"]>; label: string }> = [
  { value: "usage", label: "按用量" },
  { value: "items", label: "按条目数" },
  { value: "recent", label: "按最近写入" }
];

/**
 * 用过云存储的用户。
 *
 * 只列在云存储里留下过账目的人（写过、或被管理员单独设置过），不是应用的全部用户 ——
 * 后者在「用户」页面。点一行进入该用户详情的「云存储」页签，条目、修订、配额与冻结都在那里。
 */
export function CloudStorageUsersPanel({ appKey }: { appKey: string }) {
  const [keyword, setKeyword] = useState("");
  const [sort, setSort] = useState<NonNullable<CloudUserParams["sort"]>>("usage");
  const [frozen, setFrozen] = useState<"all" | "true" | "false">("all");
  const [page, setPage] = useState(1);
  const debouncedKeyword = useDebouncedValue(keyword, 300);

  const listQuery = useCloudStorageUsersQuery(appKey, {
    keyword: debouncedKeyword || undefined,
    sort,
    frozen: frozen === "all" ? undefined : frozen,
    page,
    limit: PAGE_SIZE
  });

  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const filtered = Boolean(keyword || frozen !== "all");

  return (
    <SectionCard icon={<Users className="size-4" />} title="用户">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索账号、昵称或用户 ID"
          className="h-8 w-60"
        />
        <Select
          value={sort}
          onValueChange={(value) => {
            setSort(value as NonNullable<CloudUserParams["sort"]>);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SORTS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={frozen}
          onValueChange={(value) => {
            setFrozen(value as "all" | "true" | "false");
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="false">正常</SelectItem>
            <SelectItem value="true">已冻结</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配用户" : "暂无用户使用云存储"}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>用户</TableHead>
              <TableHead className="w-56">用量</TableHead>
              <TableHead className="text-right">条目 / 回收站</TableHead>
              <TableHead className="text-right">修订</TableHead>
              <TableHead className="text-right">最近写入</TableHead>
              <TableHead className="w-10" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((user) => {
              const href = `/app-users/${encodeURIComponent(appKey)}/${user.userId}?tab=cloud`;
              return (
                <TableRow key={user.userId} className="group">
                  <TableCell className="text-xs">
                    <Link href={href} className="flex items-center gap-1.5 font-medium hover:underline">
                      <span className="truncate">{user.nickname || user.account || `#${user.userId}`}</span>
                      {user.frozen ? (
                        <Badge variant="info" size="sm" className="gap-1 font-normal">
                          <Snowflake className="size-3" />
                          已冻结
                        </Badge>
                      ) : null}
                      {user.quotaOverride ? (
                        <Badge variant="outline" size="sm" className="font-normal">
                          自定义配额
                        </Badge>
                      ) : null}
                    </Link>
                    <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">
                      #{user.userId}
                      {user.account && user.nickname ? ` · ${user.account}` : ""}
                    </p>
                  </TableCell>
                  <TableCell>
                    <QuotaMeter used={user.usedBytes} quota={user.quotaBytes} />
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {user.itemCount} / {user.trashCount}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{user.revisionCount}</TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">
                    {formatDateTime(user.lastWriteAt)}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button asChild size="icon-xs" variant="ghost" aria-label="管理">
                      <Link href={href}>
                        <ChevronRight />
                      </Link>
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}

      {total > 0 ? (
        <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
          <span className="tabular-nums">
            共 {total} 人 · 第 {page}/{totalPages} 页
          </span>
          <div className="flex gap-1.5">
            <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={page >= totalPages}
              onClick={() => setPage((value) => value + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      ) : null}
    </SectionCard>
  );
}
