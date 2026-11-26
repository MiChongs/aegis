"use client";

import { useEffect, useState } from "react";
import { CircleAlert, FlaskConical, History, ListChecks, Loader2, Pencil, Play, Plus, Send, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Field, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { TRANSPORT_META } from "@/components/firewall/firewall-kit";
import { ExclusionSentence, useExclusionHandoff } from "@/components/firewall/exclusions/exclusion-format";
import type { FirewallExclusionInput, FirewallSandboxInput, FirewallSandboxResult, FirewallTransport } from "@/lib/api/firewall";
import { formatClockSeconds } from "@/lib/datetime";
import { firewallErrorMessage, useFirewallSandboxMutation } from "@/lib/firewall-hooks";
import { useFirewallIntents } from "@/lib/firewall-store";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { CONTENT_TYPES, SANDBOX_METHODS, SANDBOX_PRESETS, contentTypeKeyOf, type ContentTypeKey } from "./sandbox-presets";
import { SandboxResultView } from "./sandbox-result";

type FirewallTabKey = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/**
 * 测试台（POST sandbox）：把一条请求交给与线上同一套的入口段 + WAF 跑一遍，不改任何状态。
 *
 * - 事件「在测试台复现」与例外编辑器「在测试台验证」会把请求放进 store，挂载时读出、effect 里清掉。
 * - 带草稿例外时可以随时关掉它再跑一次，对比「有没有这条例外」的差别；历史里每条都标着带没带。
 * - 失败不弹全局提示（hook 约定），错误就地显示在结果区。
 */

type HeaderRow = { id: number; key: string; value: string };

type Composer = {
  method: string;
  url: string;
  transport: FirewallTransport;
  ip: string;
  headers: HeaderRow[];
  body: string;
};

type Run = { id: number; at: number; input: FirewallSandboxInput; result: FirewallSandboxResult };

const HISTORY_LIMIT = 8;
const TRANSPORTS: FirewallTransport[] = ["plain", "sealed", "legacy"];

let rowSeq = 0;
const nextRowId = () => {
  rowSeq += 1;
  return rowSeq;
};

function rowsOf(headers?: Record<string, string>): HeaderRow[] {
  const rows = Object.entries(headers ?? {}).map(([key, value]) => ({ id: nextRowId(), key, value }));
  return rows.length ? rows : [{ id: nextRowId(), key: "", value: "" }];
}

function composerOf(input?: FirewallSandboxInput | null): Composer {
  return {
    method: input?.method || "POST",
    url: input?.url || "/api/v1/apps/demo_app/feedback",
    transport: input?.transport || "plain",
    ip: input?.ip || "",
    headers: rowsOf(input?.headers ?? (input ? {} : { "Content-Type": "application/json", "User-Agent": "Voyage/1.0 (Android 15)" })),
    body: input?.body ?? (input ? "" : '{\n  "content": "页面打不开，点击「我的」后闪退"\n}')
  };
}

function toInput(form: Composer): FirewallSandboxInput {
  const headers: Record<string, string> = {};
  for (const row of form.headers) {
    const key = row.key.trim();
    if (key) headers[key] = row.value;
  }
  return {
    method: form.method,
    url: form.url.trim() || "/",
    headers,
    body: form.body || undefined,
    ip: form.ip.trim() || undefined,
    transport: form.transport
  };
}

function headerValue(rows: HeaderRow[], name: string) {
  return rows.find((row) => row.key.trim().toLowerCase() === name.toLowerCase())?.value ?? "";
}

export function FirewallSandboxTab({ onNavigate }: { onNavigate: (tab: FirewallTabKey) => void }) {
  const sandbox = useFirewallSandboxMutation();
  const canWrite = usePermissionChecker()("system:firewall:policy");

  // 挂载时读出跳转草稿（只读不清，清在 effect 里）
  const [initial] = useState(() => ({
    draft: useFirewallIntents.getState().sandboxDraft,
    editId: useExclusionHandoff.getState().editId
  }));
  useEffect(() => {
    useFirewallIntents.getState().takeSandboxDraft();
    useExclusionHandoff.getState().takeEditId();
  }, []);

  const [form, setForm] = useState<Composer>(() => composerOf(initial.draft));
  const [draftExclusion, setDraftExclusion] = useState<FirewallExclusionInput | null>(initial.draft?.draftExclusion ?? null);
  const [withDraft, setWithDraft] = useState(true);
  const [history, setHistory] = useState<Run[]>([]);
  const [shown, setShown] = useState<Run | null>(null);
  const set = (patch: Partial<Composer>) => setForm((current) => ({ ...current, ...patch }));

  const contentType = headerValue(form.headers, "content-type");
  const typeKey = contentTypeKeyOf(contentType);

  const setHeader = (name: string, value: string) =>
    setForm((current) => {
      const index = current.headers.findIndex((row) => row.key.trim().toLowerCase() === name.toLowerCase());
      if (index >= 0) {
        const headers = current.headers.map((row, i) => (i === index ? { ...row, value } : row));
        return { ...current, headers };
      }
      const blank = current.headers.findIndex((row) => !row.key.trim() && !row.value);
      const headers =
        blank >= 0
          ? current.headers.map((row, i) => (i === blank ? { ...row, key: name, value } : row))
          : [...current.headers, { id: nextRowId(), key: name, value }];
      return { ...current, headers };
    });

  const switchType = (key: ContentTypeKey) => {
    const option = CONTENT_TYPES.find((item) => item.key === key)!;
    setHeader("Content-Type", option.value);
    if (!form.body.trim()) set({ body: option.template });
    if (form.method === "GET" || form.method === "HEAD") set({ method: "POST" });
  };

  const run = () => {
    const input: FirewallSandboxInput = { ...toInput(form), draftExclusion: draftExclusion && withDraft ? draftExclusion : undefined };
    sandbox.mutate(input, {
      onSuccess: (result) => {
        const entry: Run = { id: Date.now(), at: Date.now(), input, result };
        setHistory((current) => [entry, ...current].slice(0, HISTORY_LIMIT));
        setShown(entry);
      }
    });
  };

  const loadRun = (entry: Run) => {
    setForm(composerOf(entry.input));
    if (entry.input.draftExclusion) {
      setDraftExclusion(entry.input.draftExclusion);
      setWithDraft(true);
    } else if (draftExclusion) {
      setWithDraft(false);
    }
    setShown(entry);
  };

  const backToEditor = () => {
    if (!draftExclusion) return;
    useFirewallIntents.getState().setExclusionDraft(draftExclusion);
    useExclusionHandoff.getState().setEditId(initial.editId);
    onNavigate("exclusions");
  };

  const shownType = shown ? (Object.entries(shown.input.headers ?? {}).find(([key]) => key.toLowerCase() === "content-type")?.[1] ?? "") : "";

  return (
    <TabStack>
      {draftExclusion ? (
        <div className="flex min-w-0 flex-col gap-2.5 rounded-2xl border border-violet-500/30 bg-violet-500/5 px-4 py-3 sm:flex-row sm:items-center">
          <div className="flex min-w-0 flex-1 items-start gap-2.5">
            <ListChecks className="mt-0.5 size-4 shrink-0 text-violet-600 dark:text-violet-400" />
            <div className="min-w-0">
              <p className="text-sm font-medium">
                带草稿例外检测 <span className="font-normal text-muted-foreground">「{draftExclusion.name || "未命名"}」</span>
              </p>
              <ExclusionSentence input={draftExclusion} className="text-muted-foreground" />
            </div>
          </div>
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <label className="inline-flex items-center gap-2 text-xs">
              <Switch checked={withDraft} onCheckedChange={setWithDraft} aria-label="使用草稿例外" />
              {withDraft ? "使用草稿" : "不使用草稿"}
            </label>
            {canWrite ? (
              <Button size="sm" variant="outline" onClick={backToEditor}>
                <Pencil />
                回到例外编辑
              </Button>
            ) : null}
            <Button size="icon-sm" variant="ghost" aria-label="移除草稿例外" onClick={() => setDraftExclusion(null)}>
              <X />
            </Button>
          </div>
        </div>
      ) : null}

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <Panel
          icon={Send}
          tone="primary"
          title="请求"
          aside={
            <Button size="sm" onClick={run} disabled={sandbox.isPending}>
              {sandbox.isPending ? <Loader2 className="animate-spin" /> : <Play />}
              运行
            </Button>
          }
        >
          <div className="flex min-w-0 flex-col gap-4">
            <div className="flex min-w-0 flex-col gap-1.5">
              <span className="text-xs text-muted-foreground">样本</span>
              <div className="flex min-w-0 flex-wrap gap-1.5">
                {SANDBOX_PRESETS.map((preset) => (
                  <button
                    key={preset.key}
                    type="button"
                    onClick={() => setForm(composerOf({ ...preset.input, transport: form.transport, ip: form.ip }))}
                    className={cn(
                      "inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[11px] transition-colors",
                      preset.expect === "pass"
                        ? "border-emerald-500/30 text-emerald-700 hover:bg-emerald-500/10 dark:text-emerald-400"
                        : "border-border text-muted-foreground hover:border-foreground/30 hover:text-foreground"
                    )}
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            </div>

            <div className="flex min-w-0 gap-2">
              <Select value={form.method} onValueChange={(method) => set({ method })}>
                <SelectTrigger className="w-28 shrink-0 font-mono text-xs" aria-label="方法">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SANDBOX_METHODS.map((method) => (
                    <SelectItem key={method} value={method} className="font-mono text-xs">
                      {method}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                value={form.url}
                onChange={(event) => set({ url: event.target.value })}
                placeholder="/api/v1/apps/demo_app/feedback?page=1"
                className="min-w-0 font-mono text-xs"
                aria-label="路径与查询串"
              />
            </div>

            <div className="grid min-w-0 gap-4 @xl:grid-cols-[minmax(0,1fr)_minmax(0,200px)]">
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs text-muted-foreground">载荷</span>
                <Segmented<FirewallTransport>
                  value={form.transport}
                  options={TRANSPORTS.map((value) => ({ value, label: TRANSPORT_META[value].label }))}
                  onChange={(transport) => set({ transport })}
                  ariaLabel="载荷"
                />
                <span className="text-[11px] leading-4 text-muted-foreground">
                  测试台检查这里填写的明文；线上加密请求解密之后跑的是同一套 WAF，事件会标出载荷来源。
                </span>
              </div>
              <Field label="来源 IP">
                <Input value={form.ip} onChange={(event) => set({ ip: event.target.value })} placeholder="可选，如 203.0.113.10" className="font-mono text-xs" />
              </Field>
            </div>

            <div className="flex min-w-0 flex-col gap-1.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">请求头</span>
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={() => set({ headers: [...form.headers, { id: nextRowId(), key: "", value: "" }] })}
                  disabled={form.headers.length >= 30}
                >
                  <Plus />
                  添加
                </Button>
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                {form.headers.map((row) => (
                  <div key={row.id} className="flex min-w-0 items-center gap-1.5">
                    <Input
                      value={row.key}
                      onChange={(event) => set({ headers: form.headers.map((item) => (item.id === row.id ? { ...item, key: event.target.value } : item)) })}
                      placeholder="名称"
                      className="h-8 w-[38%] min-w-0 shrink-0 font-mono text-xs"
                      aria-label="请求头名称"
                    />
                    <Input
                      value={row.value}
                      onChange={(event) => set({ headers: form.headers.map((item) => (item.id === row.id ? { ...item, value: event.target.value } : item)) })}
                      placeholder="值"
                      className="h-8 min-w-0 flex-1 font-mono text-xs"
                      aria-label="请求头值"
                    />
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label="删除请求头"
                      onClick={() => {
                        const rest = form.headers.filter((item) => item.id !== row.id);
                        set({ headers: rest.length ? rest : [{ id: nextRowId(), key: "", value: "" }] });
                      }}
                    >
                      <X />
                    </Button>
                  </div>
                ))}
              </div>
            </div>

            <div className="flex min-w-0 flex-col gap-1.5">
              <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">请求体</span>
                <div className="flex flex-wrap gap-1" role="group" aria-label="Content-Type">
                  {CONTENT_TYPES.map((option) => (
                    <button
                      key={option.key}
                      type="button"
                      onClick={() => switchType(option.key)}
                      aria-pressed={typeKey === option.key}
                      className={cn(
                        "rounded-md border px-1.5 text-[11px] leading-5 transition-colors",
                        typeKey === option.key ? "border-foreground/40 bg-accent text-foreground" : "border-border text-muted-foreground hover:text-foreground"
                      )}
                    >
                      {option.label}
                    </button>
                  ))}
                </div>
              </div>
              <Textarea
                value={form.body}
                onChange={(event) => set({ body: event.target.value })}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                    event.preventDefault();
                    run();
                  }
                }}
                spellCheck={false}
                placeholder={form.method === "GET" ? "GET 请求通常没有请求体" : '{"content": "..."}'}
                className="max-h-[420px] min-h-36 font-mono text-xs leading-5 md:text-xs"
                aria-label="请求体"
              />
            </div>
          </div>
        </Panel>

        <div className="flex min-w-0 flex-col gap-4">
          <Panel icon={FlaskConical} tone="violet" title="结果">
            {sandbox.isError ? (
              <div className="mb-4 flex min-w-0 items-start gap-2 rounded-lg border border-rose-500/30 bg-rose-500/8 px-3 py-2 text-xs text-rose-700 dark:text-rose-300">
                <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
                <span className="min-w-0 break-all">{firewallErrorMessage(sandbox.error, "运行失败")}</span>
              </div>
            ) : null}
            {shown ? (
              <SandboxResultView result={shown.result} declaredType={shownType} />
            ) : (
              <div className="grid min-h-48 place-items-center rounded-xl border border-dashed border-border px-4 text-center text-xs text-muted-foreground">
                {sandbox.isPending ? "运行中" : "选一个样本或填写请求后运行"}
              </div>
            )}
          </Panel>

          {history.length ? (
            <Panel icon={History} title="最近运行">
              <ul className="flex min-w-0 flex-col gap-1">
                {history.map((entry) => (
                  <li key={entry.id}>
                    <button
                      type="button"
                      onClick={() => loadRun(entry)}
                      className={cn(
                        "flex w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent",
                        shown?.id === entry.id && "bg-accent"
                      )}
                    >
                      <span
                        className={cn(
                          "size-2 shrink-0 rounded-full",
                          entry.result.verdict === "block" ? "bg-rose-500" : entry.result.verdict === "observe" ? "bg-amber-500" : "bg-emerald-500"
                        )}
                      />
                      <span className="w-14 shrink-0 font-mono text-[11px]">{entry.input.method}</span>
                      <span className="min-w-0 flex-1 truncate font-mono text-[11px]">{entry.input.url}</span>
                      {entry.input.draftExclusion ? (
                        <span className="shrink-0 rounded bg-violet-500/10 px-1 text-[10px] text-violet-700 dark:text-violet-300">带例外</span>
                      ) : null}
                      <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
                        {entry.result.inboundScore} 分 · {formatClockSeconds(entry.at)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </Panel>
          ) : null}
        </div>
      </div>
    </TabStack>
  );
}
