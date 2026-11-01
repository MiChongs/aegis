"use client";

import { useMemo, useState } from "react";
import { Network, Pencil, Plus, Search, ShieldBan, Smartphone, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  DeviceText,
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
import type { RegIPListInput, RegIPListKind } from "@/lib/api/register-ip";
import { formatDateTime, formatRelative, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import {
  useAddRegDeviceListsMutation,
  useAddRegIPListsMutation,
  useDeleteRegDeviceListsMutation,
  useDeleteRegIPListsMutation,
  useRegDeviceListsQuery,
  useRegIPListsQuery,
  useUpdateRegDeviceListMutation,
  useUpdateRegIPListMutation
} from "@/lib/register-ip-hooks";
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

type Target = "ip" | "device";

/** 地址名单与设备名单统一成一种行，表格与弹窗共用 */
type Row = {
  id: number;
  value: string;
  kind: RegIPListKind;
  factor: number;
  note: string;
  expiresAt: string | null;
  hits: number;
  lastHitAt: string | null;
  createdBy: string;
  createdAt: string;
};

type UpdateInput = { kind: RegIPListKind; factor?: number; note?: string; expiresAt?: string | null };

/**
 * 名单：地址名单与设备名单。判定时两边合并 —— 任一边拒绝即拒绝，否则任一边放行即放行，否则取放宽倍数大的。
 * 地址名单里同类取最具体的网段；搜索框填一个地址时列出「包含它」的网段。设备名单按标识精确匹配（不分大小写）。
 */
export function RegIPListsPanel({
  appKey,
  onOpenIP,
  onOpenDevice
}: {
  appKey: string;
  onOpenIP: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  const [target, setTarget] = useState<Target>("ip");
  const [kind, setKind] = useState<RegIPListKind | "all">("all");
  const [keyword, setKeyword] = useState("");
  const [expired, setExpired] = useState(false);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number[]>([]);
  const [editing, setEditing] = useState<Row | "new" | null>(null);
  const debounced = useDebouncedValue(keyword.trim(), 300);
  const params = useMemo(
    () => ({ kind: kind === "all" ? undefined : kind, keyword: debounced || undefined, expired: expired || undefined, page, limit: LIMIT }),
    [kind, debounced, expired, page]
  );
  const ipQuery = useRegIPListsQuery(appKey, params, target === "ip");
  const deviceQuery = useRegDeviceListsQuery(appKey, params, target === "device");
  const removeIP = useDeleteRegIPListsMutation(appKey);
  const removeDevice = useDeleteRegDeviceListsMutation(appKey);
  const addIP = useAddRegIPListsMutation(appKey);
  const addDevice = useAddRegDeviceListsMutation(appKey);
  const updateIP = useUpdateRegIPListMutation(appKey);
  const updateDevice = useUpdateRegDeviceListMutation(appKey);

  const query = target === "ip" ? ipQuery : deviceQuery;
  const rows: Row[] = useMemo(
    () =>
      target === "ip"
        ? (ipQuery.data?.items ?? []).map((item) => ({ ...item, value: item.cidr }))
        : (deviceQuery.data?.items ?? []).map((item) => ({ ...item, value: item.deviceId })),
    [target, ipQuery.data, deviceQuery.data]
  );
  // 判断「已过期」的参照时刻：渲染期间不能调 Date.now()，数据刷新时跟着更新
  const [now, setNow] = useState(() => Date.now());
  const [seenData, setSeenData] = useState<unknown>(null);
  if (query.data !== seenData) {
    setSeenData(query.data);
    setNow(query.dataUpdatedAt || now);
  }
  const allSelected = rows.length > 0 && rows.every((row) => selected.includes(row.id));
  const resetPage = () => {
    setPage(1);
    setSelected([]);
  };

  async function removeRows(ids: number[]) {
    try {
      const result = target === "ip" ? await removeIP.mutateAsync(ids) : await removeDevice.mutateAsync(ids);
      toast.success(`已删除 ${result.deleted} 项`);
      setSelected([]);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }
  const save = async (input: RegIPListInput | { id: number; input: UpdateInput }) => {
    if ("id" in input) {
      return target === "ip" ? updateIP.mutateAsync(input) : updateDevice.mutateAsync(input);
    }
    const result = target === "ip" ? await addIP.mutateAsync(input) : await addDevice.mutateAsync(input);
    return result.items.length;
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <div className="inline-flex rounded-xl bg-muted p-1">
          {(
            [
              ["ip", "地址名单", Network],
              ["device", "设备名单", Smartphone]
            ] as const
          ).map(([value, label, Icon]) => (
            <button
              key={value}
              type="button"
              onClick={() => {
                setTarget(value);
                setKeyword("");
                resetPage();
              }}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                target === value ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <Icon className="size-3.5" />
              {label}
            </button>
          ))}
        </div>
        <span className="text-[11px] text-muted-foreground">两边合并判定：任一边拒绝即拒绝</span>
      </div>

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
                resetPage();
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
                <span className="block text-sm font-medium">
                  {meta.label}
                  {target === "ip" ? "地址" : "设备"}
                </span>
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
              resetPage();
            }}
            placeholder={target === "ip" ? "地址（查包含它的网段）或备注" : "设备标识或备注"}
            className="h-8 pl-8 text-xs"
          />
        </div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Switch
            checked={expired}
            onCheckedChange={(value) => {
              setExpired(value);
              resetPage();
            }}
          />
          含已过期
        </label>
        <div className="ml-auto flex gap-2">
          {selected.length ? (
            <Button size="sm" variant="outline" className="text-rose-600" onClick={() => removeRows(selected)}>
              <Trash2 className="size-3.5" /> 删除 {selected.length} 项
            </Button>
          ) : null}
          <Button size="sm" onClick={() => setEditing("new")}>
            <Plus className="size-3.5" /> 添加{target === "ip" ? "地址" : "设备"}
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
        ) : rows.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-14 text-center">
            <ShieldBan className="size-6 text-muted-foreground/50" />
            <span className="text-sm text-muted-foreground">{target === "ip" ? "地址名单是空的" : "设备名单是空的"}</span>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8">
                  <Checkbox checked={allSelected} onCheckedChange={(value) => setSelected(value ? rows.map((row) => row.id) : [])} aria-label="全选" />
                </TableHead>
                <TableHead>{target === "ip" ? "地址 / 网段" : "设备标识"}</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>备注</TableHead>
                <TableHead>命中</TableHead>
                <TableHead>到期</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => {
                const expiredAt = row.expiresAt ? new Date(row.expiresAt).getTime() : 0;
                const isExpired = expiredAt > 0 && expiredAt < now;
                return (
                  <TableRow key={row.id} className={cn(isExpired && "opacity-60")}>
                    <TableCell>
                      <Checkbox
                        checked={selected.includes(row.id)}
                        onCheckedChange={(value) => setSelected((prev) => (value ? [...prev, row.id] : prev.filter((id) => id !== row.id)))}
                        aria-label="选择"
                      />
                    </TableCell>
                    <TableCell className="max-w-72">
                      {target === "ip" ? (
                        <IPText value={row.value} onClick={() => onOpenIP(row.value.split("/")[0])} />
                      ) : (
                        <DeviceText value={row.value} onClick={() => onOpenDevice(row.value)} />
                      )}
                      <div className="text-[10px] text-muted-foreground">
                        {row.createdBy || "系统"} · {formatRelative(row.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <ListKindBadge kind={row.kind} factor={row.factor} />
                    </TableCell>
                    <TableCell className="max-w-56 truncate text-xs text-muted-foreground">{row.note || "—"}</TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {formatCount(row.hits)}
                      {row.lastHitAt ? <div className="text-[10px] text-muted-foreground">{formatRelative(row.lastHitAt)}</div> : null}
                    </TableCell>
                    <TableCell className="text-xs">
                      {row.expiresAt ? (
                        isExpired ? (
                          <Badge variant="outline" size="sm">已过期</Badge>
                        ) : (
                          formatDateTime(row.expiresAt)
                        )
                      ) : (
                        <span className="text-muted-foreground">永久</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-0.5">
                        <Button size="icon-sm" variant="ghost" className="size-7" onClick={() => setEditing(row)} aria-label="编辑">
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button size="icon-sm" variant="ghost" className="size-7 text-rose-600" onClick={() => removeRows([row.id])} aria-label="删除">
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
          target={target}
          row={editing === "new" ? null : editing}
          defaultKind={kind === "all" ? "deny" : kind}
          onSave={save}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function ListEntryDialog({
  target,
  row,
  defaultKind,
  onSave,
  onClose
}: {
  target: Target;
  row: Row | null;
  defaultKind: RegIPListKind;
  onSave: (input: RegIPListInput | { id: number; input: UpdateInput }) => Promise<unknown>;
  onClose: () => void;
}) {
  const [values, setValues] = useState("");
  const [kind, setKind] = useState<RegIPListKind>(row?.kind ?? defaultKind);
  const [factor, setFactor] = useState(String(row?.factor && row.factor > 1 ? row.factor : 5));
  const [note, setNote] = useState(row?.note ?? "");
  const [expiresAt, setExpiresAt] = useState(row?.expiresAt ? toLocalInputValue(row.expiresAt) : "");
  const [pending, setPending] = useState(false);
  const count = values.split(/[\s,，;]+/).filter(Boolean).length;
  const noun = target === "ip" ? "地址" : "设备";

  async function submit() {
    const payload = {
      kind,
      factor: kind === "elevated" ? Number(factor) || 0 : undefined,
      note: note.trim(),
      expiresAt: expiresAt ? fromLocalInputValue(expiresAt) : null
    };
    setPending(true);
    try {
      if (row) {
        await onSave({ id: row.id, input: payload });
        toast.success("已保存");
      } else {
        const added = await onSave({ ...payload, values: [values] });
        toast.success(`已添加 ${added} 项`);
      }
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{row ? `编辑${noun}名单项` : `添加到${noun}名单`}</DialogTitle>
          <DialogDescription>
            {row ? (
              <span className="break-all font-mono">{row.value}</span>
            ) : target === "ip" ? (
              "单个地址或网段（CIDR），一次可以粘贴多行，最多 500 项；已存在的会被覆盖"
            ) : (
              "设备标识（X-Device-Id），一次可以粘贴多行，最多 500 项；不分大小写；无效的标识只能加入拒绝名单"
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          {!row ? (
            <div className="space-y-1.5">
              <Textarea
                rows={5}
                value={values}
                onChange={(event) => setValues(event.target.value)}
                placeholder={target === "ip" ? "1.2.3.4\n10.20.0.0/16\n2408:8207::/48" : "6f9619ff-8b86-d011-b42d-00c04fc964ff\na1b2c3d4e5f60718"}
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
            <p className="text-[11px] text-muted-foreground">
              {target === "device" && kind === "elevated" ? "配额乘以倍数，适合共享的测试机、门店收银机" : LIST_META[kind].description}
            </p>
          </div>
          {kind === "elevated" ? (
            <div className="flex items-center gap-2">
              <Label className="text-xs">配额倍数</Label>
              <Input type="number" min={1.1} max={1000} step={0.5} value={factor} onChange={(event) => setFactor(event.target.value)} className="h-8 w-24 text-xs" />
              <span className="text-[11px] text-muted-foreground">例如 5：每天 3 个变成 15 个</span>
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
            <Input
              value={note}
              maxLength={200}
              onChange={(event) => setNote(event.target.value)}
              placeholder={target === "ip" ? "例如：某高校出口、批量注册工作室" : "例如：内部测试机、确认的群控设备"}
              className="text-xs"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={pending || (!row && count === 0)} onClick={submit}>
            {pending ? "保存中..." : row ? "保存" : `添加 ${count} 项`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
