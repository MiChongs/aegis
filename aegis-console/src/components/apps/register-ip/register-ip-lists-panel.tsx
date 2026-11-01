"use client";

import { useMemo, useState } from "react";
import { Pencil, Plus, Search, ShieldBan, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  formatCount,
  IPText,
  LIST_KINDS,
  LIST_META,
  ListKindBadge,
  Pager
} from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api-client";
import type { RegIPListEntry, RegIPListKind } from "@/lib/api/register-ip";
import { formatDateTime, formatRelative, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useAddRegIPListsMutation, useDeleteRegIPListsMutation, useRegIPListsQuery, useUpdateRegIPListMutation } from "@/lib/register-ip-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

const LIMIT = 20;
const EXPIRY_PRESETS = [
  { label: "永久", hours: 0 },
  { label: "1 小时", hours: 1 },
  { label: "1 天", hours: 24 },
  { label: "7 天", hours: 24 * 7 },
  { label: "30 天", hours: 24 * 30 }
];

/**
 * 名单。拒绝优先于放行、放行优先于放宽；同类里取最具体的网段。
 * 搜索框里填一个地址时列出「包含它」的网段，而不是文本模糊匹配。
 */
export function RegIPListsPanel({ appKey, onOpenIP }: { appKey: string; onOpenIP: (ip: string) => void }) {
  const [kind, setKind] = useState<RegIPListKind | "all">("all");
  const [keyword, setKeyword] = useState("");
  const [expired, setExpired] = useState(false);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number[]>([]);
  const [editing, setEditing] = useState<RegIPListEntry | "new" | null>(null);
  // 判断「已过期」的参照时刻：渲染期间不能调 Date.now()，挂载时取一次，数据刷新时跟着更新
  const [now, setNow] = useState(() => Date.now());
  const [seenData, setSeenData] = useState<unknown>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const params = useMemo(
    () => ({ kind: kind === "all" ? undefined : kind, keyword: debounced || undefined, expired: expired || undefined, page, limit: LIMIT }),
    [kind, debounced, expired, page]
  );
  const query = useRegIPListsQuery(appKey, params);
  const remove = useDeleteRegIPListsMutation(appKey);
  const items = query.data?.items ?? [];
  if (query.data !== seenData) {
    setSeenData(query.data);
    setNow(query.dataUpdatedAt || now);
  }
  const allSelected = items.length > 0 && items.every((item) => selected.includes(item.id));

  async function removeSelected(ids: number[]) {
    try {
      const result = await remove.mutateAsync(ids);
      toast.success(`已删除 ${result.deleted} 项`);
      setSelected([]);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  return (
    <div className="space-y-3">
      <div className="grid gap-2.5 sm:grid-cols-3">
        {LIST_KINDS.map((item) => {
          const meta = LIST_META[item];
          const active = kind === item;
          return (
            <button
              key={item}
              type="button"
              onClick={() => {
                setKind(active ? "all" : item);
                setPage(1);
              }}
              className={cn(
                "flex items-start gap-3 rounded-xl border border-border bg-card px-4 py-3 text-left transition-colors hover:bg-muted/50",
                active && "ring-2 ring-inset ring-primary/40"
              )}
            >
              <span className={cn("grid size-8 shrink-0 place-items-center rounded-lg ring-1 ring-inset", meta.tint)}>
                <meta.icon className="size-4" />
              </span>
              <span className="min-w-0">
                <span className="block text-sm font-medium">{meta.label}名单</span>
                <span className="block text-[11px] text-muted-foreground">{meta.description}</span>
              </span>
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-56 flex-1 sm:max-w-72">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
            placeholder="地址（查包含它的网段）或备注"
            className="h-8 pl-8 text-xs"
          />
        </div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Switch
            checked={expired}
            onCheckedChange={(value) => {
              setExpired(value);
              setPage(1);
            }}
          />
          含已过期
        </label>
        <div className="ml-auto flex gap-2">
          {selected.length ? (
            <Button size="sm" variant="outline" className="text-rose-600" disabled={remove.isPending} onClick={() => removeSelected(selected)}>
              <Trash2 className="size-3.5" /> 删除 {selected.length} 项
            </Button>
          ) : null}
          <Button size="sm" onClick={() => setEditing("new")}>
            <Plus className="size-3.5" /> 添加
          </Button>
        </div>
      </div>

      <div className="rounded-xl border border-border bg-card">
        {query.isLoading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-14 text-center">
            <ShieldBan className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">名单是空的</span>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8">
                  <Checkbox
                    checked={allSelected}
                    onCheckedChange={(value) => setSelected(value ? items.map((item) => item.id) : [])}
                    aria-label="全选"
                  />
                </TableHead>
                <TableHead>地址 / 网段</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>备注</TableHead>
                <TableHead>命中</TableHead>
                <TableHead>到期</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((entry) => {
                const expiredAt = entry.expiresAt ? new Date(entry.expiresAt).getTime() : 0;
                const isExpired = expiredAt > 0 && expiredAt < now;
                return (
                  <TableRow key={entry.id} className={cn(isExpired && "opacity-60")}>
                    <TableCell>
                      <Checkbox
                        checked={selected.includes(entry.id)}
                        onCheckedChange={(value) => setSelected((prev) => (value ? [...prev, entry.id] : prev.filter((id) => id !== entry.id)))}
                        aria-label="选择"
                      />
                    </TableCell>
                    <TableCell>
                      <IPText value={entry.cidr} onClick={() => onOpenIP(entry.cidr.split("/")[0])} />
                      <div className="text-[10px] text-muted-foreground">
                        {entry.createdBy || "系统"} · {formatRelative(entry.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <ListKindBadge kind={entry.kind} factor={entry.factor} />
                    </TableCell>
                    <TableCell className="max-w-56 truncate text-xs text-muted-foreground">{entry.note || "—"}</TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {formatCount(entry.hits)}
                      {entry.lastHitAt ? <div className="text-[10px] text-muted-foreground">{formatRelative(entry.lastHitAt)}</div> : null}
                    </TableCell>
                    <TableCell className="text-xs">
                      {entry.expiresAt ? (
                        isExpired ? (
                          <Badge variant="outline" size="sm">已过期</Badge>
                        ) : (
                          formatDateTime(entry.expiresAt)
                        )
                      ) : (
                        <span className="text-muted-foreground">永久</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-0.5">
                        <Button size="icon-sm" variant="ghost" className="size-7" onClick={() => setEditing(entry)} aria-label="编辑">
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button size="icon-sm" variant="ghost" className="size-7 text-rose-600" onClick={() => removeSelected([entry.id])} aria-label="删除">
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>
      <Pager total={query.data?.total ?? 0} page={page} limit={LIMIT} onPage={setPage} />

      {editing ? (
        <ListEntryDialog
          appKey={appKey}
          entry={editing === "new" ? null : editing}
          defaultKind={kind === "all" ? "deny" : kind}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function ListEntryDialog({
  appKey,
  entry,
  defaultKind,
  onClose
}: {
  appKey: string;
  entry: RegIPListEntry | null;
  defaultKind: RegIPListKind;
  onClose: () => void;
}) {
  const add = useAddRegIPListsMutation(appKey);
  const update = useUpdateRegIPListMutation(appKey);
  const [values, setValues] = useState("");
  const [kind, setKind] = useState<RegIPListKind>(entry?.kind ?? defaultKind);
  const [factor, setFactor] = useState(String(entry?.factor && entry.factor > 1 ? entry.factor : 5));
  const [note, setNote] = useState(entry?.note ?? "");
  const [expiresAt, setExpiresAt] = useState(entry?.expiresAt ? toLocalInputValue(entry.expiresAt) : "");
  const pending = add.isPending || update.isPending;
  const count = values.split(/[\s,，;]+/).filter(Boolean).length;

  async function submit() {
    const payload = {
      kind,
      factor: kind === "elevated" ? Number(factor) || 0 : undefined,
      note: note.trim(),
      expiresAt: expiresAt ? fromLocalInputValue(expiresAt) : null
    };
    try {
      if (entry) {
        await update.mutateAsync({ id: entry.id, input: payload });
        toast.success("已保存");
      } else {
        const result = await add.mutateAsync({ ...payload, values: [values] });
        toast.success(`已添加 ${result.items.length} 项`);
      }
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{entry ? "编辑名单项" : "添加到名单"}</DialogTitle>
          <DialogDescription>
            {entry ? <span className="font-mono">{entry.cidr}</span> : "单个地址或网段（CIDR），一次可以粘贴多行，最多 500 项；已存在的会被覆盖"}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          {!entry ? (
            <div className="space-y-1.5">
              <Textarea
                rows={5}
                value={values}
                onChange={(event) => setValues(event.target.value)}
                placeholder={"1.2.3.4\n10.20.0.0/16\n2408:8207::/48"}
                className="font-mono text-xs"
              />
              <div className="text-right text-[10px] text-muted-foreground">{count} 项</div>
            </div>
          ) : null}
          <div className="space-y-1.5">
            <Label className="text-xs">类型</Label>
            <div className="grid grid-cols-3 gap-2">
              {LIST_KINDS.map((item) => {
                const meta = LIST_META[item];
                return (
                  <button
                    key={item}
                    type="button"
                    onClick={() => setKind(item)}
                    className={cn(
                      "flex items-center gap-2 rounded-xl px-3 py-2 text-xs transition-colors",
                      kind === item ? cn("ring-1 ring-inset", meta.tint) : "bg-muted hover:bg-muted-foreground/10"
                    )}
                  >
                    <meta.icon className="size-3.5" />
                    {meta.label}
                  </button>
                );
              })}
            </div>
            <p className="text-[11px] text-muted-foreground">{LIST_META[kind].description}</p>
          </div>
          {kind === "elevated" ? (
            <div className="flex items-center gap-2">
              <Label className="text-xs">配额倍数</Label>
              <Input type="number" min={1.1} max={1000} step={0.5} value={factor} onChange={(event) => setFactor(event.target.value)} className="h-8 w-24 text-xs" />
              <span className="text-[11px] text-muted-foreground">例如 5：每天 10 个变成 50 个</span>
            </div>
          ) : null}
          <div className="space-y-1.5">
            <Label className="text-xs">到期</Label>
            <ToggleGroup
              type="single"
              size="sm"
              variant="outline"
              value=""
              onValueChange={(value) => {
                if (!value) return;
                const hours = Number(value);
                setExpiresAt(hours ? toLocalInputValue(new Date(Date.now() + hours * 3600_000).toISOString()) : "");
              }}
            >
              {EXPIRY_PRESETS.map((preset) => (
                <ToggleGroupItem key={preset.label} value={String(preset.hours)} className="h-7 px-2.5 text-xs">
                  {preset.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            <Input type="datetime-local" value={expiresAt} onChange={(event) => setExpiresAt(event.target.value)} className="h-8 text-xs" />
            {!expiresAt ? <p className="text-[11px] text-muted-foreground">不填为永久</p> : null}
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">备注</Label>
            <Input value={note} maxLength={200} onChange={(event) => setNote(event.target.value)} placeholder="例如：某高校出口、批量注册工作室" className="text-xs" />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending || (!entry && count === 0)} onClick={submit}>
            {pending ? "保存中..." : entry ? "保存" : `添加 ${count} 项`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
