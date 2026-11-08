"use client";

import { useState } from "react";
import { toast } from "sonner";
import { AlertTriangle, DatabaseZap, ListChecks, Loader2, Plus, ScanSearch, Trash2 } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import type { ListHit, ListKind, ListType } from "@/lib/api/risk";
import { formatCompact, formatDateTime, formatRelative, parseInstant } from "@/lib/datetime";
import { useAddListEntries, useCheckLists, useRebuildLists, useRemoveListEntries, useRiskLists, useRiskScenes } from "@/lib/risk-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { ListHitTable, errorText, useSceneName } from "./decisions";
import { AppScopeSelect, useAppName } from "./kit/app-scope";
import { KindBadge } from "./kit/badges";
import { DURATION_PRESETS, KIND_LABEL, LIST_TYPE_HINT, LIST_TYPE_LABEL } from "./kit/labels";

const PAGE_SIZE = 50;
const LIST_TYPES = Object.keys(LIST_TYPE_LABEL) as ListType[];
const KINDS: ListKind[] = ["block", "allow", "watch"];
const SOURCE_LABEL: Record<string, string> = { manual: "手工", review: "复核", decision: "决策", cep: "行为模式" };

export function RiskLists({ app }: { app?: number }) {
  const [type, setType] = useState("");
  const [kind, setKind] = useState("");
  const [value, setValue] = useState("");
  const [source, setSource] = useState("");
  const [active, setActive] = useState(true);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [removing, setRemoving] = useState<string[] | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [checkOpen, setCheckOpen] = useState(false);
  const [rebuildOpen, setRebuildOpen] = useState(false);
  const appName = useAppName();
  const sceneName = useSceneName();
  const remove = useRemoveListEntries();
  const rebuild = useRebuildLists();
  const debouncedValue = useDebouncedValue(value);

  const query = useRiskLists({
    type: type || undefined,
    kind: kind || undefined,
    value: debouncedValue.trim() || undefined,
    app,
    source: source || undefined,
    active,
    page,
    limit: PAGE_SIZE,
  });
  const rows = query.data?.list ?? [];
  const total = query.data?.total ?? 0;

  const reset = () => {
    setPage(1);
    setSelected(new Set());
  };
  const allOnPage = rows.length > 0 && rows.every((r) => selected.has(r.id));
  const someOnPage = rows.some((r) => selected.has(r.id));

  const doRemove = (ids: string[]) =>
    remove.mutate(ids, {
      onSuccess: (res) => {
        toast.success(`已移除 ${res.removed} 条`);
        setSelected((cur) => new Set([...cur].filter((id) => !ids.includes(id))));
      },
      onError: (e) => toast.error(errorText(e, "移除失败")),
    });

  return (
    <TabStack>
      <div className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-card p-4 sm:px-5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <FilterSelect
            value={type}
            onChange={(v) => {
              setType(v);
              reset();
            }}
            all="全部类型"
            options={LIST_TYPES.map((t) => ({ value: t, label: LIST_TYPE_LABEL[t] }))}
          />
          <FilterSelect
            value={kind}
            onChange={(v) => {
              setKind(v);
              reset();
            }}
            all="全部种类"
            options={KINDS.map((k) => ({ value: k, label: KIND_LABEL[k] }))}
            width="w-32"
          />
          <FilterSelect
            value={source}
            onChange={(v) => {
              setSource(v);
              reset();
            }}
            all="全部来源"
            options={Object.entries(SOURCE_LABEL).map(([v, l]) => ({ value: v, label: l }))}
            width="w-32"
          />
          <Input
            className="h-8 w-48 font-mono text-xs"
            placeholder="取值前缀"
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              reset();
            }}
          />
          <Label className="inline-flex items-center gap-2 text-xs font-normal text-muted-foreground">
            <Switch
              checked={active}
              onCheckedChange={(v) => {
                setActive(v);
                reset();
              }}
            />
            仅有效
          </Label>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setCheckOpen(true)}>
              <ScanSearch />
              检测
            </Button>
            <Button size="sm" variant="outline" disabled={rebuild.isPending} onClick={() => setRebuildOpen(true)}>
              {rebuild.isPending ? <Loader2 className="animate-spin" /> : <DatabaseZap />}
              重建索引
            </Button>
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus />
              添加
            </Button>
          </div>
        </div>
      </div>

      <Panel
        icon={ListChecks}
        tone="emerald"
        title="名单"
        aside={
          selected.size > 0 ? (
            <>
              <span className="text-xs text-muted-foreground">
                已选 <span className="font-medium text-foreground tabular-nums">{selected.size}</span>
              </span>
              <Button size="sm" variant="ghost" className="h-8" onClick={() => setSelected(new Set())}>
                取消选择
              </Button>
              <Button size="sm" variant="destructive" className="h-8" disabled={remove.isPending} onClick={() => setRemoving([...selected])}>
                <Trash2 />
                移除
              </Button>
            </>
          ) : null
        }
        bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
      >
        {query.isError && !query.data ? (
          <div className="flex items-center gap-2 border-t px-5 py-8 text-sm text-rose-600 dark:text-rose-400">
            <AlertTriangle className="size-4" />
            {errorText(query.error)}
          </div>
        ) : query.isLoading ? (
          <div className="flex flex-col gap-2 border-t p-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <div className="border-t">
            <CardEmpty icon={ListChecks} title="暂无名单" />
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10 pl-5">
                    <Checkbox
                      aria-label="全选本页"
                      checked={allOnPage ? true : someOnPage ? "indeterminate" : false}
                      onCheckedChange={(v) =>
                        setSelected((cur) => {
                          const next = new Set(cur);
                          for (const r of rows) {
                            if (v === true) next.add(r.id);
                            else next.delete(r.id);
                          }
                          return next;
                        })
                      }
                    />
                  </TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>取值</TableHead>
                  <TableHead>种类</TableHead>
                  <TableHead>范围</TableHead>
                  <TableHead>原因</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead>到期</TableHead>
                  <TableHead>添加</TableHead>
                  <TableHead className="w-10 pr-5" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((e) => (
                  <TableRow key={e.id} data-state={selected.has(e.id) ? "selected" : undefined}>
                    <TableCell className="pl-5">
                      <Checkbox
                        aria-label="选择"
                        checked={selected.has(e.id)}
                        onCheckedChange={(v) =>
                          setSelected((cur) => {
                            const next = new Set(cur);
                            if (v === true) next.add(e.id);
                            else next.delete(e.id);
                            return next;
                          })
                        }
                      />
                    </TableCell>
                    <TableCell className="text-xs whitespace-nowrap">{LIST_TYPE_LABEL[e.type] ?? e.type}</TableCell>
                    <TableCell className="max-w-56 truncate font-mono text-xs" title={e.value}>
                      {e.value}
                    </TableCell>
                    <TableCell>
                      <KindBadge kind={e.kind} />
                    </TableCell>
                    <TableCell className="max-w-44 text-xs">
                      <div className="truncate">{e.app ? appName(e.app) : "全部应用"}</div>
                      <div className="truncate text-[11px] text-muted-foreground">
                        {e.scenes?.length ? e.scenes.map(sceneName).join("、") : "全部场景"}
                      </div>
                    </TableCell>
                    <TableCell className="max-w-48 truncate text-xs text-muted-foreground" title={e.reason}>
                      {e.reason || "—"}
                    </TableCell>
                    <TableCell className="text-xs whitespace-nowrap">
                      {SOURCE_LABEL[e.source ?? ""] ?? e.source ?? "—"}
                      {e.by ? <div className="max-w-24 truncate text-[11px] text-muted-foreground">{e.by}</div> : null}
                    </TableCell>
                    <TableCell className="text-xs whitespace-nowrap">
                      <Expiry exp={e.exp} />
                    </TableCell>
                    <TableCell className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(e.at)}>
                      {formatCompact(e.at)}
                    </TableCell>
                    <TableCell className="pr-5">
                      <Button size="icon-xs" variant="ghost" aria-label="移除" onClick={() => setRemoving([e.id])}>
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <ListPagination
              page={page}
              totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))}
              total={total}
              unit="条"
              onPage={(p) => {
                setPage(p);
              }}
            />
          </>
        )}
      </Panel>

      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除名单</AlertDialogTitle>
            <AlertDialogDescription>将移除 {removing?.length ?? 0} 条名单，立即对决策生效。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (removing) doRemove(removing);
                setRemoving(null);
              }}
            >
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={rebuildOpen} onOpenChange={setRebuildOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重建索引</AlertDialogTitle>
            <AlertDialogDescription>将按数据库中的有效名单重写 Redis 索引。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                rebuild.mutate(undefined, {
                  onSuccess: (res) => toast.success(`已重建 ${res.entries} 条`),
                  onError: (e) => toast.error(errorText(e, "重建失败")),
                })
              }
            >
              重建
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-h-[90vh] gap-4 overflow-y-auto sm:max-w-xl">
          <AddListForm defaultApp={app} onDone={() => setAddOpen(false)} />
        </DialogContent>
      </Dialog>

      <Dialog open={checkOpen} onOpenChange={setCheckOpen}>
        <DialogContent className="max-h-[90vh] gap-4 overflow-y-auto sm:max-w-2xl">
          <CheckListsForm defaultApp={app} />
        </DialogContent>
      </Dialog>
    </TabStack>
  );
}

function FilterSelect({
  value,
  onChange,
  all,
  options,
  width = "w-36",
}: {
  value: string;
  onChange: (v: string) => void;
  all: string;
  options: Array<{ value: string; label: string }>;
  width?: string;
}) {
  return (
    <Select value={value || "all"} onValueChange={(v) => onChange(v === "all" ? "" : v)}>
      <SelectTrigger size="sm" className={cn("h-8", width)}>
        <SelectValue placeholder={all} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">{all}</SelectItem>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function Expiry({ exp }: { exp?: string | null }) {
  const at = parseInstant(exp);
  if (!at) return <span className="text-muted-foreground">永久</span>;
  if (isPast(at)) return <Badge variant="secondary" size="sm">已过期</Badge>;
  return <span title={formatDateTime(at)}>{formatRelative(at)}</span>;
}

function isPast(at: Date) {
  return at.getTime() <= Date.now();
}

/* ── 添加 ── */

function AddListForm({ defaultApp, onDone }: { defaultApp?: number; onDone: () => void }) {
  const scenes = useRiskScenes();
  const add = useAddListEntries();
  const [type, setType] = useState<ListType>("ip");
  const [kind, setKind] = useState<ListKind>("block");
  const [text, setText] = useState("");
  const [sceneKeys, setSceneKeys] = useState<string[]>([]);
  const [appId, setAppId] = useState<number | undefined>(defaultApp);
  const [reason, setReason] = useState("");
  const [duration, setDuration] = useState("0");
  const [invalid, setInvalid] = useState<string[]>([]);

  const values = text
    .split(/\r?\n/)
    .map((v) => v.trim())
    .filter(Boolean);

  const submit = () =>
    add.mutate(
      {
        listType: type,
        kind,
        values,
        appId,
        scenes: sceneKeys.length ? sceneKeys : undefined,
        reason: reason.trim() || undefined,
        durationSeconds: Number(duration),
      },
      {
        onSuccess: (res) => {
          toast.success(`已添加 ${res.added} 条`);
          const bad = res.invalid ?? [];
          if (bad.length) {
            setInvalid(bad);
            setText(bad.join("\n"));
          } else {
            onDone();
          }
        },
        onError: (e) => toast.error(errorText(e, "添加失败")),
      }
    );

  return (
    <>
      <DialogHeader>
        <DialogTitle>添加名单</DialogTitle>
      </DialogHeader>
      <div className="flex min-w-0 flex-col gap-4">
        <div className="grid min-w-0 gap-3 sm:grid-cols-2">
          <FormField label="类型">
            <Select value={type} onValueChange={(v) => setType(v as ListType)}>
              <SelectTrigger size="sm" className="h-9 w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LIST_TYPES.map((t) => (
                  <SelectItem key={t} value={t}>
                    {LIST_TYPE_LABEL[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>
          <FormField label="种类">
            <Segmented value={kind} options={KINDS.map((k) => ({ value: k, label: KIND_LABEL[k] }))} onChange={setKind} ariaLabel="种类" />
          </FormField>
        </div>
        <FormField label={`取值 ${values.length ? values.length : ""}`.trim()}>
          <Textarea
            rows={6}
            className="font-mono text-xs"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              if (invalid.length) setInvalid([]);
            }}
            placeholder={`每行一个，如 ${LIST_TYPE_HINT[type]}`}
          />
        </FormField>
        {invalid.length > 0 ? (
          <div className="flex flex-col gap-1.5 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2.5">
            <span className="inline-flex items-center gap-1.5 text-xs font-medium text-amber-700 dark:text-amber-300">
              <AlertTriangle className="size-3.5" />
              {invalid.length} 个取值不合法，未添加
            </span>
            <div className="flex flex-wrap gap-1">
              {invalid.slice(0, 50).map((v, i) => (
                <span key={`${v}-${i}`} className="rounded bg-amber-500/10 px-1.5 py-0.5 font-mono text-[11px] text-amber-800 dark:text-amber-200">
                  {v}
                </span>
              ))}
              {invalid.length > 50 ? <span className="text-[11px] text-muted-foreground">等 {invalid.length} 个</span> : null}
            </div>
          </div>
        ) : null}
        <FormField label="场景">
          <div className="flex flex-wrap gap-1.5">
            <ChipToggle active={sceneKeys.length === 0} onClick={() => setSceneKeys([])}>
              全部场景
            </ChipToggle>
            {(scenes.data ?? []).map((s) => (
              <ChipToggle
                key={s.key}
                active={sceneKeys.includes(s.key)}
                onClick={() => setSceneKeys((cur) => (cur.includes(s.key) ? cur.filter((k) => k !== s.key) : [...cur, s.key]))}
              >
                {s.name}
              </ChipToggle>
            ))}
          </div>
        </FormField>
        <div className="grid min-w-0 gap-3 sm:grid-cols-2">
          <FormField label="应用">
            <AppScopeSelect value={appId} onChange={setAppId} className="h-9 w-full" />
          </FormField>
          <FormField label="原因">
            <Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="选填" />
          </FormField>
        </div>
        <FormField label="时长">
          <Segmented
            value={duration}
            options={DURATION_PRESETS.map((d) => ({ value: String(d.seconds), label: d.label }))}
            onChange={setDuration}
            ariaLabel="时长"
          />
        </FormField>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onDone}>
          {invalid.length ? "完成" : "取消"}
        </Button>
        <Button disabled={values.length === 0 || add.isPending} onClick={submit}>
          {add.isPending ? <Loader2 className="animate-spin" /> : null}
          添加 {values.length > 0 ? values.length : ""}
        </Button>
      </DialogFooter>
    </>
  );
}

function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      {children}
    </div>
  );
}

function ChipToggle({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center rounded-md border px-2.5 text-xs transition-colors",
        active ? "border-primary/50 bg-primary/10 text-foreground" : "border-border text-muted-foreground hover:bg-accent/50"
      )}
    >
      {children}
    </button>
  );
}

/* ── 检测 ── */

const CHECK_FIELDS: Array<{ type: ListType; label: string; mono?: boolean }> = [
  { type: "ip", label: "IP", mono: true },
  { type: "device", label: "设备 ID", mono: true },
  { type: "account", label: "登录名" },
  { type: "user", label: "用户 ID" },
  { type: "phone_prefix", label: "手机号", mono: true },
  { type: "email_domain", label: "邮箱或域名" },
  { type: "asn", label: "ASN", mono: true },
  { type: "country", label: "国家", mono: true },
  { type: "ua_keyword", label: "User-Agent" },
];

function CheckListsForm({ defaultApp }: { defaultApp?: number }) {
  const scenes = useRiskScenes();
  const check = useCheckLists();
  const [appId, setAppId] = useState<number | undefined>(defaultApp);
  const [scene, setScene] = useState("");
  const [values, setValues] = useState<Partial<Record<ListType, string>>>({});
  const [hits, setHits] = useState<ListHit[] | null>(null);

  const filled = Object.fromEntries(
    Object.entries(values)
      .map(([k, v]) => [k, (v ?? "").trim()])
      .filter(([, v]) => v)
  ) as Partial<Record<ListType, string>>;
  const any = Object.keys(filled).length > 0;

  const run = () =>
    check.mutate(
      { appId, scene: scene || undefined, values: filled },
      { onSuccess: setHits, onError: (e) => toast.error(errorText(e, "检测失败")) }
    );

  return (
    <>
      <DialogHeader>
        <DialogTitle>名单检测</DialogTitle>
      </DialogHeader>
      <div className="flex min-w-0 flex-col gap-4">
        <div className="grid min-w-0 gap-3 sm:grid-cols-2">
          <FormField label="应用">
            <AppScopeSelect value={appId} onChange={setAppId} className="h-9 w-full" />
          </FormField>
          <FormField label="场景">
            <Select value={scene || "all"} onValueChange={(v) => setScene(v === "all" ? "" : v)}>
              <SelectTrigger size="sm" className="h-9 w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部场景</SelectItem>
                {(scenes.data ?? []).map((s) => (
                  <SelectItem key={s.key} value={s.key}>
                    {s.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>
        </div>
        <div className="grid min-w-0 gap-3 sm:grid-cols-3">
          {CHECK_FIELDS.map((f) => (
            <FormField key={f.type} label={f.label}>
              <Input
                className={cn("h-8", f.mono && "font-mono text-xs")}
                value={values[f.type] ?? ""}
                placeholder={LIST_TYPE_HINT[f.type]}
                onChange={(e) => setValues((cur) => ({ ...cur, [f.type]: e.target.value }))}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && any) run();
                }}
              />
            </FormField>
          ))}
        </div>
        {hits !== null ? (
          hits.length ? (
            <div className="flex flex-col gap-2">
              <span className="text-xs font-medium">命中 {hits.length} 条</span>
              <ListHitTable hits={hits} />
            </div>
          ) : (
            <p className="rounded-lg bg-muted/40 px-3 py-6 text-center text-sm text-muted-foreground">未命中</p>
          )
        ) : null}
      </div>
      <DialogFooter>
        <Button disabled={!any || check.isPending} onClick={run}>
          {check.isPending ? <Loader2 className="animate-spin" /> : <ScanSearch />}
          检测
        </Button>
      </DialogFooter>
    </>
  );
}

