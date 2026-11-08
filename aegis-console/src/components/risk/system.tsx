"use client";

import { Fragment, useMemo, useState } from "react";
import { AlertTriangle, ChevronDown, Cpu, History, Inbox, Server } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { AuditEntry, RiskStatus } from "@/lib/api/risk";
import { formatCompact, formatDateTime, formatRelative } from "@/lib/datetime";
import { useRiskAudit, useRiskStatus } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { errorText, useSceneName } from "./decisions";

const COMPONENTS: Array<{ key: string; name: string; role: string }> = [
  { key: "risk-decision", name: "决策服务", role: "在线决策" },
  { key: "postgres", name: "PostgreSQL", role: "配置与名单" },
  { key: "redis", name: "Redis Cluster", role: "名单索引与滑窗" },
  { key: "clickhouse", name: "ClickHouse", role: "流水与分析" },
  { key: "etcd", name: "etcd", role: "发布与发现" },
  { key: "nats", name: "NATS JetStream", role: "事件总线" },
  { key: "risingwave", name: "RisingWave", role: "流式特征" },
  { key: "nebula", name: "NebulaGraph", role: "关系图" },
  { key: "milvus", name: "Milvus", role: "相似设备" },
];

const DEP_LABEL: Record<string, string> = { redis: "Redis", nebula: "NebulaGraph", milvus: "Milvus", nats: "NATS", etcd: "etcd" };

export function RiskSystem() {
  const status = useRiskStatus();

  return (
    <TabStack>
      {status.isError && !status.data ? (
        <Panel icon={AlertTriangle} tone="rose" title="读取失败">
          <p className="text-sm text-muted-foreground">{errorText(status.error)}</p>
        </Panel>
      ) : !status.data ? (
        <>
          <Skeleton className="h-[88px] rounded-2xl" />
          <div className="grid grid-cols-1 gap-3 @xl:grid-cols-2 @4xl:grid-cols-3">
            {Array.from({ length: 9 }).map((_, i) => (
              <Skeleton key={i} className="h-[88px] rounded-xl" />
            ))}
          </div>
          <Skeleton className="h-[240px] rounded-2xl" />
        </>
      ) : (
        <>
          <HealthSummary status={status.data} />
          <ComponentGrid components={status.data.components} />
          <DecisionPanel status={status.data} />
        </>
      )}
      <AuditPanel />
    </TabStack>
  );
}

/* ── 概况 ── */

function HealthSummary({ status }: { status: RiskStatus }) {
  const keys = new Set([...COMPONENTS.map((c) => c.key), ...Object.keys(status.components ?? {})]);
  const bad = [...keys].filter((k) => (status.components?.[k] ?? "未上报") !== "ok").length;
  const total = keys.size;
  const spool = status.spoolPending ?? 0;
  const loaded = status.decision?.scenes?.length ?? 0;
  const sceneErrors = (status.decision?.scenes ?? []).filter((s) => s.error).length;

  return (
    <div className="grid min-w-0 gap-3 @3xl:grid-cols-[minmax(0,1.4fr)_repeat(3,minmax(0,1fr))]">
      <div
        className={cn(
          "flex min-w-0 items-center gap-4 rounded-2xl border p-4 sm:px-5",
          bad ? "border-rose-500/40 bg-rose-500/5" : "border-emerald-500/40 bg-emerald-500/5"
        )}
      >
        <span className={cn("grid size-11 shrink-0 place-items-center rounded-xl", bad ? "bg-rose-500/15 text-rose-600 dark:text-rose-400" : "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400")}>
          <Server className="size-5" />
        </span>
        <div className="min-w-0">
          <div className={cn("text-lg font-semibold", bad ? "text-rose-700 dark:text-rose-300" : "text-emerald-700 dark:text-emerald-300")}>
            {bad ? `${bad} 项异常` : "全部正常"}
          </div>
          <div className="text-xs text-muted-foreground tabular-nums">
            {total - bad} / {total} 正常
          </div>
        </div>
      </div>
      <Figure label="决策实例" value={status.decision?.instance || "—"} mono />
      <Figure label="已加载场景" value={String(loaded)} tone={sceneErrors ? "danger" : undefined} sub={sceneErrors ? `${sceneErrors} 个加载失败` : undefined} />
      <Figure label="待发事件" value={spool.toLocaleString("zh-CN")} tone={spool > 0 ? "warn" : undefined} sub={spool > 0 ? "消息总线恢复后补发" : undefined} />
    </div>
  );
}

function Figure({ label, value, sub, tone, mono }: { label: string; value: string; sub?: string; tone?: "warn" | "danger"; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col justify-center gap-1 rounded-2xl border border-border bg-card p-4">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={cn(
          "truncate text-xl leading-7 font-semibold tabular-nums",
          mono && "font-mono text-sm leading-7",
          tone === "warn" && "text-amber-600 dark:text-amber-400",
          tone === "danger" && "text-rose-600 dark:text-rose-400"
        )}
        title={value}
      >
        {value}
      </span>
      {sub ? <span className="truncate text-[11px] text-muted-foreground">{sub}</span> : null}
    </div>
  );
}

/* ── 组件 ── */

function ComponentGrid({ components }: { components: Record<string, string> }) {
  const extra = Object.keys(components ?? {}).filter((k) => !COMPONENTS.some((c) => c.key === k));
  const items = [...COMPONENTS, ...extra.map((k) => ({ key: k, name: k, role: "" }))];
  return (
    <div className="grid min-w-0 grid-cols-1 gap-3 @xl:grid-cols-2 @4xl:grid-cols-3">
      {items.map((c) => {
        const raw = components?.[c.key];
        const ok = raw === "ok";
        const missing = raw === undefined;
        return (
          <div
            key={c.key}
            className={cn(
              "flex min-w-0 flex-col gap-2 rounded-xl border p-3.5",
              ok ? "border-border bg-card" : missing ? "border-dashed border-border bg-card" : "border-rose-500/40 bg-rose-500/5"
            )}
          >
            <div className="flex min-w-0 items-center gap-2">
              <span aria-hidden className={cn("size-2 shrink-0 rounded-full", ok ? "bg-emerald-500" : missing ? "bg-muted-foreground" : "bg-rose-500")} />
              <span className="min-w-0 flex-1 truncate text-sm font-medium">{c.name}</span>
              <Badge variant={ok ? "success" : missing ? "secondary" : "danger"} size="sm">
                {ok ? "正常" : missing ? "未上报" : "异常"}
              </Badge>
            </div>
            {ok || missing ? (
              <span className="truncate text-xs text-muted-foreground">{c.role}</span>
            ) : (
              <p className="line-clamp-2 font-mono text-[11px] break-all text-rose-600 dark:text-rose-400" title={raw}>
                {raw}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}

/* ── 决策实例 ── */

function DecisionPanel({ status }: { status: RiskStatus }) {
  const sceneName = useSceneName();
  const d = status.decision;
  const deps = Object.entries(d?.dependencies ?? {});
  const scenes = [...(d?.scenes ?? [])].sort((a, b) => a.scene.localeCompare(b.scene));

  return (
    <Panel
      icon={Cpu}
      tone="primary"
      title="已加载版本"
      aside={
        deps.length ? (
          <div className="flex flex-wrap items-center gap-1">
            {deps.map(([k, v]) => (
              <Badge key={k} variant={v === "ok" ? "success" : "danger"} size="sm" title={v === "ok" ? undefined : v}>
                {DEP_LABEL[k] ?? k}
              </Badge>
            ))}
          </div>
        ) : null
      }
      bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
    >
      {!d ? (
        <div className="border-t">
          <CardEmpty icon={Cpu} title="决策服务不可用" />
        </div>
      ) : scenes.length === 0 ? (
        <div className="border-t">
          <CardEmpty icon={Cpu} title="暂无已加载场景" />
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-5">场景</TableHead>
              <TableHead>版本</TableHead>
              <TableHead>候选</TableHead>
              <TableHead>流量</TableHead>
              <TableHead>加载</TableHead>
              <TableHead className="pr-5">状态</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {scenes.map((s) => (
              <TableRow key={s.scene}>
                <TableCell className="pl-5">
                  <div className="text-sm">{sceneName(s.scene)}</div>
                  <div className="font-mono text-[11px] text-muted-foreground">{s.scene}</div>
                </TableCell>
                <TableCell>
                  <div className="font-mono text-xs">{s.version || "—"}</div>
                  {s.checksum ? <div className="max-w-28 truncate font-mono text-[11px] text-muted-foreground" title={s.checksum}>{s.checksum.slice(0, 12)}</div> : null}
                </TableCell>
                <TableCell className="font-mono text-xs">{s.candidate || "—"}</TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {s.candidate && s.canary_percent ? <Badge variant="warning" size="sm">灰度 {s.canary_percent}%</Badge> : null}
                    {s.shadow ? <Badge variant="info" size="sm">影子</Badge> : null}
                    {!s.candidate ? <span className="text-xs text-muted-foreground">全量</span> : null}
                  </div>
                </TableCell>
                <TableCell className="text-xs whitespace-nowrap text-muted-foreground" title={s.loaded_at ? formatDateTime(s.loaded_at) : undefined}>
                  {s.loaded_at ? formatRelative(s.loaded_at) : "—"}
                </TableCell>
                <TableCell className="max-w-72 pr-5">
                  {s.error ? (
                    <span className="line-clamp-2 font-mono text-[11px] break-all text-rose-600 dark:text-rose-400" title={s.error}>
                      {s.error}
                    </span>
                  ) : (
                    <Badge variant="success" size="sm">正常</Badge>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  );
}

/* ── 审计 ── */

const AUDIT_LABEL: Record<string, string> = {
  "case.review": "复核结案", "cep.delete": "删除行为模式", "cep.save": "保存行为模式", dispose: "手工处置",
  "feature.delete": "删除特征", "feature.save": "保存特征", "list.add": "添加名单", "list.rebuild": "重建名单索引",
  "list.remove": "移除名单", "model.delete": "删除模型", "model.upload": "上传模型", "model.update": "更新模型",
  "rule.delete": "删除规则", "rule.save": "保存规则", "scene.delete": "删除场景", "scene.publish": "发布场景",
  "scene.save": "保存场景", "scene.promote": "全量发布", "scene.rollback": "回滚场景", "scene.withdraw": "撤回候选",
  "scene.traffic": "调整流量", "stream.sync": "同步流式特征", "template.apply": "安装规则模板",
};

const AUDIT_TONE = (action: string): "danger" | "warning" | "info" | "secondary" => {
  if (action.endsWith(".delete") || action === "list.remove") return "danger";
  if (action === "dispose" || action === "case.review" || action.startsWith("scene.publish") || action === "scene.rollback") return "warning";
  if (action.endsWith(".save") || action === "list.add") return "info";
  return "secondary";
};

function AuditPanel() {
  const audit = useRiskAudit(200);
  const [keyword, setKeyword] = useState("");
  const [open, setOpen] = useState<Set<number>>(new Set());
  const rows = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    const list = audit.data ?? [];
    if (!k) return list;
    return list.filter((e) => [e.operator, e.action, AUDIT_LABEL[e.action] ?? "", e.target].some((v) => v.toLowerCase().includes(k)));
  }, [audit.data, keyword]);

  const toggle = (id: number) =>
    setOpen((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  return (
    <Panel
      icon={History}
      tone="amber"
      title="审计日志"
      aside={<Input className="h-8 w-48" placeholder="操作人、动作、对象" value={keyword} onChange={(e) => setKeyword(e.target.value)} />}
      bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
    >
      {audit.isError && !audit.data ? (
        <div className="flex items-center gap-2 border-t px-5 py-8 text-sm text-rose-600 dark:text-rose-400">
          <AlertTriangle className="size-4" />
          {errorText(audit.error)}
        </div>
      ) : audit.isLoading ? (
        <div className="flex flex-col gap-2 border-t p-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-10 rounded-lg" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="border-t">
          <CardEmpty icon={Inbox} title="暂无审计记录" />
        </div>
      ) : (
        <div className="max-h-[640px] overflow-y-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8 pl-5" />
                <TableHead>时间</TableHead>
                <TableHead>操作人</TableHead>
                <TableHead>动作</TableHead>
                <TableHead className="pr-5">对象</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((e) => (
                <AuditRow key={e.id} entry={e} open={open.has(e.id)} onToggle={() => toggle(e.id)} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Panel>
  );
}

function AuditRow({ entry, open, onToggle }: { entry: AuditEntry; open: boolean; onToggle: () => void }) {
  const hasDetail = entry.detail !== null && entry.detail !== undefined && !(typeof entry.detail === "object" && Object.keys(entry.detail as object).length === 0);
  return (
    <Fragment>
      <TableRow
        className={cn(hasDetail && "cursor-pointer")}
        aria-expanded={hasDetail ? open : undefined}
        tabIndex={hasDetail ? 0 : undefined}
        onClick={hasDetail ? onToggle : undefined}
        onKeyDown={(ev) => {
          if (hasDetail && ev.key === "Enter") onToggle();
        }}
      >
        <TableCell className="pl-5">
          {hasDetail ? <ChevronDown className={cn("size-3.5 text-muted-foreground transition-transform", open && "rotate-180")} /> : null}
        </TableCell>
        <TableCell className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(entry.at)}>
          {formatCompact(entry.at)}
        </TableCell>
        <TableCell className="max-w-40 truncate text-sm">{entry.operator || "—"}</TableCell>
        <TableCell>
          <span className="inline-flex items-center gap-1.5">
            <Badge variant={AUDIT_TONE(entry.action)} size="sm">
              {AUDIT_LABEL[entry.action] ?? entry.action}
            </Badge>
            {AUDIT_LABEL[entry.action] ? <span className="hidden font-mono text-[11px] text-muted-foreground md:inline">{entry.action}</span> : null}
          </span>
        </TableCell>
        <TableCell className="max-w-72 truncate pr-5 font-mono text-xs" title={entry.target}>
          {entry.target || "—"}
        </TableCell>
      </TableRow>
      {open && hasDetail ? (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={5} className="bg-muted/30 px-5 py-3">
            <pre className="max-h-80 overflow-auto rounded-lg border border-border bg-background p-3 font-mono text-[11px] leading-5 whitespace-pre-wrap break-all">
              {JSON.stringify(entry.detail, null, 2)}
            </pre>
          </TableCell>
        </TableRow>
      ) : null}
    </Fragment>
  );
}
