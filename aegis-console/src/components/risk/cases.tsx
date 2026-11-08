"use client";

import { useState } from "react";
import { toast } from "sonner";
import { AlertTriangle, CheckCircle2, ClipboardCheck, ExternalLink, Loader2, XCircle } from "lucide-react";

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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import type { DisposeInput, ListKind, RiskCase } from "@/lib/api/risk";
import { formatCompact, formatDateTime } from "@/lib/datetime";
import { useReviewCase, useRiskCases, useRiskScenes } from "@/lib/risk-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { DecisionDetailDialog, Fact, Subject, errorText, useSceneName } from "./decisions";
import { useAppName } from "./kit/app-scope";
import { ActionBadge, ScoreMeter } from "./kit/badges";
import { DURATION_PRESETS, KIND_LABEL } from "./kit/labels";

const PAGE_SIZE = 30;

const STATUS = [
  { value: "pending", label: "待复核" },
  { value: "closed", label: "已结案" },
] as const;

type Status = (typeof STATUS)[number]["value"];

const SOURCE_LABEL: Record<string, string> = { decision: "决策", cep: "行为模式", manual: "手工", review: "复核" };

export function RiskCases({ app }: { app?: number }) {
  const [status, setStatus] = useState<Status>("pending");
  const [scene, setScene] = useState("");
  const [user, setUser] = useState("");
  const [page, setPage] = useState(1);
  const [openCase, setOpenCase] = useState<RiskCase | null>(null);
  const scenes = useRiskScenes();
  const sceneName = useSceneName();
  const debouncedUser = useDebouncedValue(user);
  const userId = Number(debouncedUser.trim());

  const query = useRiskCases({
    status,
    app,
    scene: scene || undefined,
    user: Number.isFinite(userId) && userId > 0 ? userId : undefined,
    page,
    limit: PAGE_SIZE,
  });
  const rows = query.data?.list ?? [];
  const total = query.data?.total ?? 0;
  const closed = status === "closed";

  return (
    <TabStack>
      <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-2xl border border-border bg-card p-4 sm:px-5">
        <Segmented
          value={status}
          options={STATUS}
          onChange={(v) => {
            setStatus(v);
            setPage(1);
          }}
          ariaLabel="复核状态"
        />
        <Select
          value={scene || "all"}
          onValueChange={(v) => {
            setScene(v === "all" ? "" : v);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-40">
            <SelectValue placeholder="全部场景" />
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
        <Input
          className="h-8 w-36"
          inputMode="numeric"
          placeholder="用户 ID"
          value={user}
          onChange={(e) => {
            setUser(e.target.value);
            setPage(1);
          }}
        />
        {!closed && total > 0 ? (
          <span className="ml-auto text-xs text-muted-foreground">
            待处理 <span className="font-medium text-foreground tabular-nums">{total}</span>
          </span>
        ) : null}
      </div>

      <Panel icon={ClipboardCheck} tone="violet" title={closed ? "已结案" : "待复核"} bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0">
        {query.isError && !query.data ? (
          <div className="flex items-center gap-2 border-t px-5 py-8 text-sm text-rose-600 dark:text-rose-400">
            <AlertTriangle className="size-4" />
            {errorText(query.error)}
          </div>
        ) : query.isLoading ? (
          <div className="flex flex-col gap-2 border-t p-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-11 rounded-lg" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <div className="border-t">
            <CardEmpty icon={ClipboardCheck} title={closed ? "暂无结案记录" : "暂无待复核"} />
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-5">时间</TableHead>
                  <TableHead>场景</TableHead>
                  <TableHead>主体</TableHead>
                  <TableHead>IP</TableHead>
                  <TableHead>处置</TableHead>
                  <TableHead className="w-28">分数</TableHead>
                  <TableHead>原因</TableHead>
                  <TableHead>来源</TableHead>
                  {closed ? (
                    <>
                      <TableHead>结论</TableHead>
                      <TableHead className="pr-5">复核</TableHead>
                    </>
                  ) : (
                    <TableHead className="pr-5 text-right" />
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((c) => (
                  <TableRow
                    key={c.id}
                    tabIndex={0}
                    className="cursor-pointer"
                    onClick={() => setOpenCase(c)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") setOpenCase(c);
                    }}
                  >
                    <TableCell className="pl-5 text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(c.createdAt)}>
                      {formatCompact(c.createdAt)}
                    </TableCell>
                    <TableCell className="max-w-32 truncate text-sm">{sceneName(c.scene)}</TableCell>
                    <TableCell className="max-w-40">
                      <Subject userId={c.userId} account={c.account} />
                    </TableCell>
                    <TableCell className="font-mono text-xs whitespace-nowrap">{c.ip || "—"}</TableCell>
                    <TableCell>
                      <ActionBadge action={c.action} />
                    </TableCell>
                    <TableCell>
                      <ScoreMeter score={c.score} />
                    </TableCell>
                    <TableCell className="max-w-56 truncate text-xs text-muted-foreground" title={c.reasons?.join("\n")}>
                      {c.reasons?.[0] ?? "—"}
                      {(c.reasons?.length ?? 0) > 1 ? <span className="ml-1 tabular-nums">+{c.reasons.length - 1}</span> : null}
                    </TableCell>
                    <TableCell className="text-xs">{SOURCE_LABEL[c.source] ?? c.source}</TableCell>
                    {closed ? (
                      <>
                        <TableCell>
                          <VerdictBadge verdict={c.verdict} />
                        </TableCell>
                        <TableCell className="pr-5 text-xs whitespace-nowrap">
                          <div className="leading-tight">{c.reviewer || "—"}</div>
                          <div className="text-[11px] text-muted-foreground tabular-nums">{formatCompact(c.reviewedAt)}</div>
                        </TableCell>
                      </>
                    ) : (
                      <TableCell className="pr-5 text-right">
                        <Button size="xs" variant="outline">
                          复核
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} total={total} unit="单" onPage={setPage} />
          </>
        )}
      </Panel>

      <Dialog open={!!openCase} onOpenChange={(o) => !o && setOpenCase(null)}>
        <DialogContent className="max-h-[90vh] gap-4 overflow-y-auto sm:max-w-2xl">
          {openCase ? <CaseReview key={openCase.id} item={openCase} onClose={() => setOpenCase(null)} /> : null}
        </DialogContent>
      </Dialog>
    </TabStack>
  );
}

function VerdictBadge({ verdict }: { verdict: RiskCase["verdict"] }) {
  if (verdict === "approve") return <Badge variant="success" size="sm">放行</Badge>;
  if (verdict === "reject") return <Badge variant="danger" size="sm">确认风险</Badge>;
  return <Badge variant="secondary" size="sm">—</Badge>;
}

/* ── 复核 ── */

type Target = DisposeInput["targets"][number];

const TARGETS: Array<{ value: Target; label: string; blockOnly?: boolean }> = [
  { value: "device", label: "设备" },
  { value: "ip", label: "IP" },
  { value: "account", label: "登录名" },
  { value: "user", label: "用户" },
  { value: "ban_account", label: "封号", blockOnly: true },
  { value: "firewall", label: "封 IP", blockOnly: true },
];

function targetAvailable(c: RiskCase, t: Target) {
  switch (t) {
    case "device":
      return !!c.deviceId;
    case "ip":
    case "firewall":
      return !!c.ip;
    case "account":
      return !!c.account;
    case "user":
    case "ban_account":
      return c.userId > 0;
  }
}

const RESULT_LABEL: Record<string, string> = { device: "设备", ip: "IP", account: "登录名", user: "用户", ban_account: "封号", firewall: "封 IP" };
const RESULT_OK = new Set(["已下发", "已加入名单"]);

function CaseReview({ item, onClose }: { item: RiskCase; onClose: () => void }) {
  const appName = useAppName();
  const sceneName = useSceneName();
  const review = useReviewCase();
  const [decisionOpen, setDecisionOpen] = useState(false);
  const [verdict, setVerdict] = useState<"approve" | "reject">("reject");
  const [allow, setAllow] = useState(false);
  const [kind, setKind] = useState<Exclude<ListKind, "allow">>("block");
  const [targets, setTargets] = useState<Target[]>(() => (["device", "user"] as Target[]).filter((t) => targetAvailable(item, t)));
  const [duration, setDuration] = useState(String(7 * 86400));
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [result, setResult] = useState<Record<string, string> | null | undefined>(undefined);
  const pending = item.status === "pending" && result === undefined;

  const toggle = (t: Target, on: boolean) => setTargets((cur) => (on ? [...new Set([...cur, t])] : cur.filter((x) => x !== t)));
  const severe = verdict === "reject" && targets.some((t) => t === "ban_account" || t === "firewall");

  const submit = () => {
    review.mutate(
      {
        id: item.id,
        verdict,
        note: note.trim(),
        allow: verdict === "approve" ? allow : undefined,
        dispose:
          verdict === "reject" && targets.length > 0
            ? { kind, targets, durationSeconds: Number(duration), reason: note.trim() }
            : undefined,
      },
      {
        onSuccess: (res) => {
          toast.success("已结案");
          if (res.dispose && Object.keys(res.dispose).length > 0) setResult(res.dispose);
          else onClose();
        },
        onError: (e) => toast.error(errorText(e, "提交失败")),
      }
    );
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{pending ? "复核" : "复核结果"}</DialogTitle>
        <DialogDescription className="tabular-nums">
          复核单 #{item.id}
          {item.decisionId ? <span className="ml-2 font-mono text-xs">{item.decisionId}</span> : null}
        </DialogDescription>
      </DialogHeader>

      <div className="flex min-w-0 flex-col gap-4">
        <div className="flex min-w-0 flex-wrap items-center gap-3 rounded-xl border border-border bg-muted/30 p-3">
          <ActionBadge action={item.action} size="default" />
          <div className="flex min-w-40 flex-1 items-center gap-3">
            <span className="text-2xl leading-7 font-semibold tabular-nums">{item.score}</span>
            <ScoreMeter score={item.score} className="min-w-0 flex-1" />
          </div>
          {item.decisionId ? (
            <Button size="sm" variant="outline" onClick={() => setDecisionOpen(true)}>
              <ExternalLink />
              查看决策
            </Button>
          ) : null}
        </div>

        <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
          <Fact label="时间" value={formatDateTime(item.createdAt)} />
          <Fact label="场景" value={sceneName(item.scene)} />
          <Fact label="应用" value={appName(item.appId)} />
          <Fact label="用户" value={item.userId ? `#${item.userId}` : "—"} />
          <Fact label="登录名" value={item.account || "—"} />
          <Fact label="来源" value={SOURCE_LABEL[item.source] ?? item.source} />
          <Fact label="IP" value={item.ip || "—"} mono />
          <Fact label="设备 ID" value={item.deviceId || "—"} mono className="col-span-2" />
        </dl>

        {item.reasons?.length ? (
          <ul className="flex flex-col gap-1 rounded-lg border border-border px-3 py-2.5">
            {item.reasons.map((r, i) => (
              <li key={i} className="flex gap-2 text-sm">
                <span className="mt-2 size-1.5 shrink-0 rounded-full bg-amber-500" />
                {r}
              </li>
            ))}
          </ul>
        ) : null}

        {result !== undefined ? (
          <DisposeResult result={result} />
        ) : item.status === "closed" ? (
          <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 rounded-lg border border-border p-3 sm:grid-cols-3">
            <Fact label="结论" value={<VerdictBadge verdict={item.verdict} />} />
            <Fact label="复核人" value={item.reviewer || "—"} />
            <Fact label="复核时间" value={formatDateTime(item.reviewedAt)} />
            <Fact label="备注" value={item.note || "—"} className="col-span-2 sm:col-span-3" wrap />
          </dl>
        ) : (
          <div className="flex min-w-0 flex-col gap-4 border-t pt-4">
            <div className="grid grid-cols-2 gap-2">
              <VerdictOption active={verdict === "approve"} tone="approve" title="放行" onClick={() => setVerdict("approve")} />
              <VerdictOption active={verdict === "reject"} tone="reject" title="确认风险" onClick={() => setVerdict("reject")} />
            </div>

            {verdict === "approve" ? (
              <Label className="inline-flex items-center gap-2 text-sm font-normal">
                <Checkbox checked={allow} onCheckedChange={(v) => setAllow(v === true)} />
                设备与用户加白 30 天
              </Label>
            ) : (
              <div className="flex min-w-0 flex-col gap-3">
                <div className="flex min-w-0 flex-col gap-1.5">
                  <span className="text-xs text-muted-foreground">处置对象</span>
                  <div className="flex flex-wrap gap-2">
                    {TARGETS.map((t) => {
                      const disabled = !targetAvailable(item, t.value) || (t.blockOnly && kind !== "block");
                      const checked = targets.includes(t.value) && !disabled;
                      return (
                        <Label
                          key={t.value}
                          className={cn(
                            "inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-sm font-normal",
                            checked ? "border-primary/50 bg-primary/5" : "border-border",
                            disabled && "opacity-50",
                            t.blockOnly && checked && "border-rose-500/50 bg-rose-500/5"
                          )}
                        >
                          <Checkbox checked={checked} disabled={disabled} onCheckedChange={(v) => toggle(t.value, v === true)} />
                          {t.label}
                        </Label>
                      );
                    })}
                  </div>
                </div>
                <div className="grid min-w-0 gap-3 sm:grid-cols-[160px_minmax(0,1fr)]">
                  <div className="flex min-w-0 flex-col gap-1.5">
                    <span className="text-xs text-muted-foreground">名单</span>
                    <Select
                      value={kind}
                      onValueChange={(v) => {
                        const next = v as Exclude<ListKind, "allow">;
                        setKind(next);
                        if (next !== "block") setTargets((cur) => cur.filter((t) => t !== "ban_account" && t !== "firewall"));
                      }}
                    >
                      <SelectTrigger size="sm" className="h-8 w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="block">{KIND_LABEL.block}</SelectItem>
                        <SelectItem value="watch">{KIND_LABEL.watch}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="flex min-w-0 flex-col gap-1.5">
                    <span className="text-xs text-muted-foreground">时长</span>
                    <Segmented
                      value={duration}
                      options={DURATION_PRESETS.map((d) => ({ value: String(d.seconds), label: d.label }))}
                      onChange={setDuration}
                      ariaLabel="时长"
                    />
                  </div>
                </div>
              </div>
            )}

            <div className="flex min-w-0 flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">备注</span>
              <Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} placeholder="复核说明" />
            </div>
          </div>
        )}
      </div>

      <DialogFooter>
        {pending ? (
          <>
            <Button variant="ghost" onClick={onClose}>
              取消
            </Button>
            <Button
              variant={verdict === "reject" ? "destructive" : "default"}
              disabled={review.isPending}
              onClick={() => (severe ? setConfirm(true) : submit())}
            >
              {review.isPending ? <Loader2 className="animate-spin" /> : null}
              {verdict === "reject" ? "确认风险" : "放行"}
            </Button>
          </>
        ) : (
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
        )}
      </DialogFooter>

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认处置</AlertDialogTitle>
            <AlertDialogDescription>
              将{targets.includes("ban_account") ? "封禁该账号" : ""}
              {targets.includes("ban_account") && targets.includes("firewall") ? "并" : ""}
              {targets.includes("firewall") ? "在防火墙封禁该 IP" : ""}
              {duration === "0" ? "，永久生效。" : `，持续 ${DURATION_PRESETS.find((d) => String(d.seconds) === duration)?.label ?? ""}。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                setConfirm(false);
                submit();
              }}
            >
              确认
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <DecisionDetailDialog id={item.decisionId || null} open={decisionOpen} onOpenChange={setDecisionOpen} />
    </>
  );
}

function VerdictOption({ active, tone, title, onClick }: { active: boolean; tone: "approve" | "reject"; title: string; onClick: () => void }) {
  const Icon = tone === "approve" ? CheckCircle2 : XCircle;
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "flex h-12 items-center justify-center gap-2 rounded-xl border text-sm font-medium transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        !active && "border-border text-muted-foreground hover:bg-accent/50",
        active && tone === "approve" && "border-emerald-500/60 bg-emerald-500/8 text-emerald-700 dark:text-emerald-300",
        active && tone === "reject" && "border-rose-500/60 bg-rose-500/8 text-rose-700 dark:text-rose-300"
      )}
    >
      <Icon className="size-4" />
      {title}
    </button>
  );
}

export function DisposeResult({ result }: { result: Record<string, string> | null }) {
  const entries = Object.entries(result ?? {});
  if (entries.length === 0) return null;
  return (
    <section className="flex min-w-0 flex-col gap-2 rounded-lg border border-border p-3">
      <h4 className="text-xs font-medium text-muted-foreground">处置结果</h4>
      <ul className="flex flex-col gap-1.5">
        {entries.map(([k, v]) => {
          const ok = RESULT_OK.has(v);
          return (
            <li key={k} className="flex items-center gap-2 text-sm">
              {ok ? <CheckCircle2 className="size-4 text-emerald-500" /> : <XCircle className="size-4 text-rose-500" />}
              <span className="w-14 shrink-0">{RESULT_LABEL[k] ?? k}</span>
              <span className={cn("min-w-0 truncate", ok ? "text-muted-foreground" : "text-rose-600 dark:text-rose-400")}>{v}</span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
