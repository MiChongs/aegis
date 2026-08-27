"use client";

import { useState } from "react";
import { ChevronLeft, ChevronRight, Database, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useDeleteFunctionKvMutation, useFunctionKvQuery } from "@/lib/function-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { errorMessage, formatTime } from "./function-shared";

/**
 * KV 浏览器 —— 脚本的「服务端独占状态」长什么样。
 *
 * 排障时最常问的一句是「这个用户的配额计数现在是多少」。没有这个视图，
 * 唯一的回答方式是临时写一个脚本去读它 —— 而那本身就是一次真实的副作用，
 * 还会把计数再加一。
 *
 * KV 挂在应用下而不是某个函数下：多个函数共用同一个命名空间是常态
 * （发号的写、校验的读）。
 */
export function FunctionKvPanel({ appKey }: { appKey?: string | null }) {
  const [scope, setScope] = useState("all");
  const [scopeId, setScopeId] = useState("");
  const [prefix, setPrefix] = useState("");
  const [page, setPage] = useState(1);
  const debouncedPrefix = useDebouncedValue(prefix, 300);
  const debouncedScopeId = useDebouncedValue(scopeId, 300);

  const kvQuery = useFunctionKvQuery(appKey, {
    scope: scope === "all" ? undefined : scope,
    scopeId: debouncedScopeId ? Number(debouncedScopeId) : undefined,
    prefix: debouncedPrefix.trim() || undefined,
    page,
    limit: 20
  });
  const deleteMutation = useDeleteFunctionKvMutation(appKey);

  if (!appKey) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          请先选择应用。
        </CardContent>
      </Card>
    );
  }

  const data = kvQuery.data;
  const list = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / (data?.limit || 20)));

  function changeFilter(apply: () => void) {
    apply();
    setPage(1);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Database className="size-4" />
          键值存储
        </CardTitle>
        <CardDescription>
          脚本通过 <code className="font-mono">aegis.kv</code>（应用级）与{" "}
          <code className="font-mono">aegis.kv.user</code>（按调用者隔离）读写的服务端状态，
          客户端不可读取。<code className="font-mono">__aegis:</code>{" "}
          为平台保留前缀，脚本不可读写。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="space-y-1.5">
            <Label className="text-xs">作用域</Label>
            <Select value={scope} onValueChange={(value) => changeFilter(() => setScope(value))}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="app">应用级（共享）</SelectItem>
                <SelectItem value="user">用户级（按调用者隔离）</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">用户 ID</Label>
            <Input
              className="h-9"
              value={scopeId}
              onChange={(event) =>
                changeFilter(() => setScopeId(event.target.value.replace(/\D/g, "")))
              }
              placeholder="仅对用户级有效"
              inputMode="numeric"
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">键前缀</Label>
            <Input
              className="h-9 font-mono text-xs"
              value={prefix}
              onChange={(event) => changeFilter(() => setPrefix(event.target.value))}
              placeholder="quota:"
            />
          </div>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>键</TableHead>
              <TableHead>作用域</TableHead>
              <TableHead>值</TableHead>
              <TableHead>过期</TableHead>
              <TableHead>更新时间</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.map((entry) => {
              const expired = entry.expiresAt ? new Date(entry.expiresAt) <= new Date() : false;
              return (
                <TableRow key={`${entry.scope}-${entry.scopeId}-${entry.key}`}>
                  <TableCell className="max-w-52 truncate font-mono text-xs">{entry.key}</TableCell>
                  <TableCell className="text-xs">
                    {entry.scope === "user" ? (
                      <span>
                        user <span className="text-muted-foreground">#{entry.scopeId}</span>
                      </span>
                    ) : (
                      "app"
                    )}
                  </TableCell>
                  <TableCell className="max-w-64 truncate font-mono text-[11px] text-muted-foreground">
                    {JSON.stringify(entry.value)}
                    {entry.truncated ? "…" : ""}
                  </TableCell>
                  <TableCell className="text-xs">
                    {entry.expiresAt ? (
                      <span className="flex items-center gap-1.5">
                        {formatTime(entry.expiresAt)}
                        {/* 过期条目判定上等于不存在，但看不见它们就解释不了
                            「为什么 KV 表里有几十万行」 */}
                        {expired ? (
                          <Badge variant="outline" size="sm">
                            已过期
                          </Badge>
                        ) : null}
                      </span>
                    ) : (
                      <span className="text-muted-foreground">永不过期</span>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatTime(entry.updatedAt)}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`删除键 ${entry.key}`}
                      onClick={async () => {
                        try {
                          await deleteMutation.mutateAsync({
                            scope: entry.scope,
                            scopeId: entry.scopeId,
                            key: entry.key
                          });
                          toast.success("键已删除");
                        } catch (error) {
                          toast.error(errorMessage(error));
                        }
                      }}
                    >
                      <Trash2 className="size-4 text-destructive" />
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
            {!list.length ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                  {kvQuery.isLoading ? "加载中…" : "没有符合条件的键"}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>

        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>共 {total} 条</span>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              disabled={page <= 1}
              onClick={() => setPage((value) => Math.max(1, value - 1))}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <span>
              {page} / {totalPages}
            </span>
            <Button
              size="sm"
              variant="ghost"
              disabled={page >= totalPages}
              onClick={() => setPage((value) => value + 1)}
            >
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
