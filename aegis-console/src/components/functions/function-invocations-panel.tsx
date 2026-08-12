"use client";

import { useState } from "react";
import { ChevronLeft, ChevronRight, Play, Loader2 } from "lucide-react";
import { toast } from "sonner";
import type { AppFunction, AppFunctionInvocation, AppFunctionResult } from "@/lib/api/app-functions";
import { invokeAppFunction } from "@/lib/api/app-functions";
import { useAdminToken } from "@/lib/admin-hooks";
import { useFunctionInvalidator, useFunctionInvocationsQuery } from "@/lib/function-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { EffectList, errorMessage, formatDuration, formatTime } from "./function-shared";

const STATUS_VARIANT: Record<string, "success" | "danger" | "warning"> = {
  success: "success",
  error: "danger",
  running: "warning"
};

/**
 * 调用审计 + 真实调用入口。
 *
 * 筛选是必需的而不是锦上添花：排障时看的从来不是「最近 20 条」，
 * 而是「失败的那几条」，一个每分钟几百次调用的函数靠时间倒序永远翻不到它们。
 */
export function FunctionInvocationsPanel({
  appKey,
  selected
}: {
  appKey: string;
  selected: AppFunction;
}) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);

  const [status, setStatus] = useState("all");
  const [callerType, setCallerType] = useState("all");
  const [eventId, setEventId] = useState("");
  const [page, setPage] = useState(1);
  const debouncedEventId = useDebouncedValue(eventId, 300);

  const invocationsQuery = useFunctionInvocationsQuery(appKey, selected.name, {
    status: status === "all" ? undefined : status,
    callerType: callerType === "all" ? undefined : callerType,
    eventId: debouncedEventId.trim() || undefined,
    page,
    limit: 20
  });

  const [detail, setDetail] = useState<AppFunctionInvocation | null>(null);
  const [invokeInput, setInvokeInput] = useState('{\n  "action": "ping"\n}');
  const [invoking, setInvoking] = useState(false);
  const [invokeResult, setInvokeResult] = useState<AppFunctionResult | string | null>(null);

  const data = invocationsQuery.data;
  const list = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / (data?.limit || 20)));

  // 任一筛选变化都回到第 1 页，否则会停在新条件下不存在的页码上，
  // 表现为「明明有数据却是空的」
  function changeFilter(apply: () => void) {
    apply();
    setPage(1);
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Play className="size-4" />
            真实调用
          </CardTitle>
          <CardDescription>
            以管理员身份走**完整**执行链：真实副作用会发生，并写入下方的调用审计。
            只想验证逻辑请用「脚本」页的试跑。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <Textarea
            className="min-h-28 font-mono text-xs"
            value={invokeInput}
            onChange={(event) => setInvokeInput(event.target.value)}
          />
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              disabled={invoking}
              onClick={async () => {
                if (!token) return;
                let input: unknown;
                try {
                  input = JSON.parse(invokeInput);
                } catch {
                  toast.error("输入不是合法 JSON");
                  return;
                }
                setInvoking(true);
                try {
                  const result = await invokeAppFunction(token, appKey, selected.name, { input });
                  setInvokeResult(result);
                  await invalidate(selected.name);
                  toast.success("调用成功");
                } catch (error) {
                  setInvokeResult(errorMessage(error));
                  toast.error(errorMessage(error));
                } finally {
                  setInvoking(false);
                }
              }}
            >
              {invoking ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
              执行
            </Button>
            {selected.status !== "active" || !selected.activeVersion ? (
              <span className="text-xs text-muted-foreground">
                函数尚未激活，调用会返回 40990
              </span>
            ) : null}
          </div>
          {invokeResult ? (
            <pre className="max-h-60 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-[11px]">
              {typeof invokeResult === "string"
                ? invokeResult
                : JSON.stringify(invokeResult, null, 2)}
            </pre>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>调用审计</CardTitle>
          <CardDescription>
            相同 eventId 重复提交会直接返回既有成功结果，不会二次执行副作用。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label className="text-xs">状态</Label>
              <Select value={status} onValueChange={(value) => changeFilter(() => setStatus(value))}>
                <SelectTrigger className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部</SelectItem>
                  <SelectItem value="success">成功</SelectItem>
                  <SelectItem value="error">失败</SelectItem>
                  <SelectItem value="running">执行中</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">调用方</Label>
              <Select
                value={callerType}
                onValueChange={(value) => changeFilter(() => setCallerType(value))}
              >
                <SelectTrigger className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部</SelectItem>
                  <SelectItem value="user">应用用户</SelectItem>
                  <SelectItem value="app">服务端密钥</SelectItem>
                  <SelectItem value="admin">管理员</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">eventId</Label>
              <Input
                className="h-9 font-mono text-xs"
                value={eventId}
                onChange={(event) => changeFilter(() => setEventId(event.target.value))}
                placeholder="精确匹配"
              />
            </div>
          </div>

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>eventId</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>调用方</TableHead>
                <TableHead>耗时</TableHead>
                <TableHead>时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((item) => (
                <TableRow
                  key={item.id}
                  className="cursor-pointer"
                  onClick={() => setDetail(item)}
                >
                  <TableCell className="max-w-52 truncate font-mono text-xs">
                    {item.eventId}
                  </TableCell>
                  <TableCell>
                    <Badge variant={STATUS_VARIANT[item.status] ?? "outline"} size="sm">
                      {item.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs">
                    {item.callerType}
                    {item.callerId ? (
                      <span className="ml-1 text-muted-foreground">#{item.callerId}</span>
                    ) : null}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {formatDuration(item.durationMs)}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatTime(item.createdAt)}
                  </TableCell>
                </TableRow>
              ))}
              {!list.length ? (
                <TableRow>
                  <TableCell colSpan={5} className="py-10 text-center text-sm text-muted-foreground">
                    {invocationsQuery.isLoading ? "加载中…" : "没有符合条件的调用记录"}
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

      <Sheet open={Boolean(detail)} onOpenChange={(open) => !open && setDetail(null)}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle className="font-mono text-sm">{detail?.eventId}</SheetTitle>
            <SheetDescription>
              {detail ? `${detail.callerType} · ${formatTime(detail.createdAt)}` : null}
            </SheetDescription>
          </SheetHeader>
          {detail ? (
            <div className="space-y-4 px-4 pb-6">
              <div className="grid grid-cols-2 gap-2 text-xs">
                <Field label="状态" value={detail.status} />
                <Field label="耗时" value={formatDuration(detail.durationMs)} />
                <Field label="请求摘要" value={detail.requestSha256.slice(0, 16)} mono />
                <Field label="响应摘要" value={(detail.responseSha256 || "—").slice(0, 16)} mono />
              </div>
              {detail.errorMessage ? (
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">错误</p>
                  <pre className="overflow-auto rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 font-mono text-[11px]">
                    {detail.errorMessage}
                  </pre>
                </div>
              ) : null}
              {detail.result?.output !== undefined ? (
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">返回值</p>
                  <pre className="max-h-64 overflow-auto rounded-lg border bg-muted/40 p-2.5 font-mono text-[11px]">
                    {JSON.stringify(detail.result.output, null, 2)}
                  </pre>
                </div>
              ) : null}
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">
                  副作用（记录的是实际发生了什么，不是脚本的一面之词）
                </p>
                <EffectList effects={detail.result?.effects ?? []} />
              </div>
            </div>
          ) : null}
        </SheetContent>
      </Sheet>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-lg border p-2">
      <p className="text-[11px] text-muted-foreground">{label}</p>
      <p className={mono ? "mt-0.5 font-mono text-[11px]" : "mt-0.5 text-xs"}>{value}</p>
    </div>
  );
}
