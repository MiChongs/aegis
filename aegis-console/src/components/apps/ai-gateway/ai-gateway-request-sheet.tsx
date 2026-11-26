"use client";

import { useState } from "react";
import { Loader2, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { CopyButton } from "@/components/apps/auth-protocol/auth-protocol-shared";
import { FactorChain, WindowHitLine } from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import {
  AttemptStatusBadge,
  ENDPOINT_LABEL,
  RequestStatusBadge,
  SOURCE_LABEL,
  describeCharge,
  describeUsage,
  formatMicro,
  formatBytes,
  formatDuration,
  formatPoints,
  formatTokens
} from "@/components/apps/ai-gateway/ai-gateway-shared";
import { Button } from "@/components/ui/button";
import { JsonViewer } from "@/components/ui/json-viewer";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useAIGatewayRequestQuery, useRefundAIGatewayRequestMutation } from "@/lib/ai-gateway-hooks";
import { ApiError } from "@/lib/api/client";
import type { AIGatewayAttempt, AIGatewayHold, AIGatewayRequest, AIGatewaySourceKey } from "@/lib/api/ai-gateway";
import { formatDateTime, parseInstant } from "@/lib/datetime";
import { cn } from "@/lib/utils";

const REFUND_REASONS = ["上游返回内容异常", "用户投诉，人工核实", "重复调用"];

/**
 * 调用详情抽屉：一次调用从暂扣到结算的全部记录。
 *
 * 进行中的调用由实时事件更新缓存（尝试列表、已用时、已生成内容），结束后重取一次拿到追踪详情。
 */
export function AIGatewayRequestSheet({
  appKey,
  record: initial,
  now,
  onClose
}: {
  appKey: string;
  record: AIGatewayRequest | null;
  now: number;
  onClose: () => void;
}) {
  const query = useAIGatewayRequestQuery(appKey, initial?.requestNo);
  const record = query.data ?? initial;

  return (
    <Sheet open={Boolean(initial)} onOpenChange={(open) => (open ? undefined : onClose())}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="shrink-0 border-b px-5 py-4 sm:px-6">
          <SheetTitle className="flex flex-wrap items-center gap-2 pr-8 font-mono text-sm">
            {record?.requestNo}
            {record ? <CopyButton value={record.requestNo} /> : null}
            {record ? <RequestStatusBadge status={record.status} /> : null}
          </SheetTitle>
        </SheetHeader>
        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-4 sm:px-6">
          {record ? <RequestDetail appKey={appKey} record={record} now={now} loading={query.isLoading} /> : null}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function RequestDetail({
  appKey,
  record,
  now,
  loading
}: {
  appKey: string;
  record: AIGatewayRequest;
  now: number;
  loading: boolean;
}) {
  const running = record.status === "reserved";
  const created = parseInstant(record.createdAt)?.getTime() ?? now;
  const elapsed = running ? Math.max(now - created, 0) : record.latencyMs;
  const firstToken = running ? record.live?.firstTokenMs : record.firstTokenMs;
  const attempts = record.trace?.attempts ?? [];

  return (
    <>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <Metric label={running ? "已用时" : "总耗时"} value={formatDuration(elapsed)} />
        <Metric label="首字" value={formatDuration(firstToken)} />
        <Metric
          label="上游尝试"
          value={record.attempts || attempts.length || "—"}
          hint={(record.attempts || attempts.length) > 1 ? "发生过回退" : undefined}
        />
        <Metric
          label={running ? "暂扣" : "实扣"}
          value={`${formatPoints(running ? record.reserved : record.charged)} 额度`}
          hint={running && record.live?.outputTokens ? `已生成约 ${formatTokens(record.live.outputTokens)} token` : undefined}
        />
      </div>

      {record.errorMessage ? (
        <div className="rounded-xl border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs">
          <span className="text-destructive">{record.errorMessage}</span>
          {record.errorCode ? <span className="ml-2 font-mono text-muted-foreground">{record.errorCode}</span> : null}
        </div>
      ) : null}

      <Section title="上游链路">
        {loading && attempts.length === 0 ? (
          <Skeleton className="h-20 w-full rounded-xl" />
        ) : attempts.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {running ? "等待上游响应" : "这次调用没有追踪详情（追踪上线之前的调用，或调用在到达上游前就结束了）"}
          </p>
        ) : (
          <AttemptWaterfall attempts={attempts} now={now} />
        )}
      </Section>

      <Section title="计费">
        <div className="divide-y divide-border">
          <DetailRow label="用量">
            {describeUsage(record)}
            {record.usageEstimated ? "（估算）" : ""}
            {record.clampMaxTokens ? ` · 输出上限压到 ${record.clampMaxTokens}` : ""}
          </DetailRow>
          <DetailRow label="倍率">
            {record.billing?.pricing ? (
              <div className="space-y-1.5">
                <FactorChain breakdown={record.billing.pricing} size="sm" />
                <div className="text-[11px]">
                  <WindowHitLine breakdown={record.billing.pricing} />
                </div>
              </div>
            ) : (
              record.factor
            )}
          </DetailRow>
          {record.billing?.profile ? (
            <DetailRow label="身份">
              {record.billing.profile.isVip ? record.billing.profile.tierName || "会员" : "非会员"}
              <span className="text-muted-foreground"> · Lv.{record.billing.profile.level}</span>
            </DetailRow>
          ) : null}
          <DetailRow label="暂扣">
            {formatPoints(record.reserved)} 额度
            {running && (record.holds ?? []).length > 0 ? <ChargeBreakdown items={record.holds} /> : null}
          </DetailRow>
          <DetailRow label="价格">{formatPoints(record.cost)} 额度</DetailRow>
          <DetailRow label="实扣">
            {formatPoints(record.charged)} 额度
            {Number(record.shortfall) > 0 ? (
              <span className="text-amber-600 dark:text-amber-400"> · 未收 {formatPoints(record.shortfall)} 额度</span>
            ) : null}
          </DetailRow>
          {(record.charges ?? []).length > 0 ? (
            <DetailRow label="扣费明细">
              <ChargeBreakdown items={record.charges} />
            </DetailRow>
          ) : null}
          {record.integralTxnNo || record.walletTxnNo ? (
            <DetailRow label="流水号">
              <span className="font-mono">{[record.integralTxnNo, record.walletTxnNo].filter(Boolean).join(" / ")}</span>
            </DetailRow>
          ) : null}
          {record.status === "refunded" ? (
            <DetailRow label="退款">
              {formatDateTime(record.refundedAt)}
              {record.refundedBy ? ` · ${record.refundedBy}` : ""}
              {record.refundReason ? ` · ${record.refundReason}` : ""}
            </DetailRow>
          ) : null}
        </div>
      </Section>

      <Section title="调用">
        <div className="divide-y divide-border">
          <DetailRow label="用户">{record.account || `#${record.userId}`}</DetailRow>
          <DetailRow label="凭据">{record.authKind === "key" ? `密钥 ${record.keyHint ?? ""}` : "登录令牌"}</DetailRow>
          <DetailRow label="接口">
            {ENDPOINT_LABEL[record.endpoint] ?? record.endpoint}
            {record.stream ? " · 流式" : ""}
          </DetailRow>
          <DetailRow label="模型">
            <span className="font-mono">{record.model}</span>
            {record.upstreamModel && record.upstreamModel !== record.model ? (
              <span className="text-muted-foreground"> → {record.upstreamModel}</span>
            ) : null}
          </DetailRow>
          <DetailRow label="通道">
            {record.configName || record.provider || "—"}
            {record.configName && record.provider ? <span className="text-muted-foreground"> · {record.provider}</span> : null}
          </DetailRow>
          {record.upstreamRequestId ? (
            <DetailRow label="上游编号">
              <span className="font-mono">{record.upstreamRequestId}</span>
            </DetailRow>
          ) : null}
          {record.finishReason ? <DetailRow label="结束原因">{record.finishReason}</DetailRow> : null}
          <DetailRow label="大小">
            请求 {formatBytes(record.requestBytes)} · 上游响应 {formatBytes(record.responseBytes)}
          </DetailRow>
          <DetailRow label="时间">
            {formatDateTime(record.createdAt)}
            {record.settledAt ? ` → ${formatDateTime(record.settledAt)}` : ""}
          </DetailRow>
          <DetailRow label="来源">
            {record.clientIp || "—"}
            {record.userAgent ? <span className="text-muted-foreground"> · {record.userAgent}</span> : null}
          </DetailRow>
        </div>
      </Section>

      {record.trace ? (
        <>
          <ContentSection title="请求内容" content={record.trace.request} />
          <ContentSection title="响应内容" content={record.trace.response} />
        </>
      ) : null}

      <RefundBox appKey={appKey} record={record} />
    </>
  );
}

const SOURCE_BAR: Partial<Record<AIGatewaySourceKey, string>> = {
  free_daily: "bg-sky-500",
  vip_daily: "bg-amber-500",
  vip_monthly: "bg-orange-500",
  pack: "bg-violet-500",
  credit: "bg-emerald-500",
  integral: "bg-stone-400",
  wallet: "bg-rose-500"
};

/**
 * 暂扣或实扣在各来源上的拆分：一条按占比分色的条 + 逐项明细；额度包来源再列出各批次各扣了多少
 * （holds[].lots / charges[].lots，微点）。
 */
function ChargeBreakdown({ items }: { items: Array<AIGatewayHold & { txnNo?: string }> }) {
  const total = items.reduce((sum, item) => sum + item.micro, 0) || 1;
  return (
    <div className="mt-1 space-y-1.5">
      <div className="flex h-1.5 overflow-hidden rounded-full bg-muted">
        {items.map((item, index) => (
          <span
            key={`${item.source}-${index}`}
            className={SOURCE_BAR[item.source] ?? "bg-muted-foreground"}
            style={{ width: `${(item.micro / total) * 100}%` }}
          />
        ))}
      </div>
      {items.map((item, index) => (
        <div key={`${item.source}-${index}`} className="space-y-0.5">
          <div className="flex items-center gap-1.5">
            <span className={cn("size-2 shrink-0 rounded-sm", SOURCE_BAR[item.source] ?? "bg-muted-foreground")} />
            <span>{describeCharge(item)}</span>
            {item.period ? <span className="font-mono text-[10px] text-muted-foreground">{item.period}</span> : null}
          </div>
          {item.source === "pack" && (item.lots?.length ?? 0) > 0 ? (
            <div className="ml-3.5 space-y-0.5 border-l border-dashed pl-2 text-[11px] text-muted-foreground">
              {item.lots?.map((lot) => (
                <div key={lot.lotId} className="flex items-center gap-2">
                  <span className="font-mono">批次 #{lot.lotId}</span>
                  <span className="font-mono tabular-nums text-foreground">{formatMicro(lot.micro)} 额度</span>
                  <span className="h-1 w-16 overflow-hidden rounded-full bg-muted">
                    <span className="block h-full bg-violet-500/70" style={{ width: `${(lot.micro / Math.max(item.micro, 1)) * 100}%` }} />
                  </span>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ))}
      {items.length > 1 ? (
        <div className="text-[10px] text-muted-foreground">
          {items.map((item) => `${SOURCE_LABEL[item.source] ?? item.source} ${Math.round((item.micro / total) * 100)}%`).join(" · ")}
        </div>
      ) : null}
    </div>
  );
}

function Metric({ label, value, hint }: { label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="rounded-xl border border-border px-3 py-2">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-sm font-semibold tabular-nums">{value}</div>
      {hint ? <div className="text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <h4 className="text-xs font-medium text-muted-foreground">{title}</h4>
      {children}
    </section>
  );
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-2 py-1.5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

/**
 * 上游尝试的瀑布图：每次尝试一行，横条按调用开始以来的时间定位；
 * 细线是收到响应头的时刻，圆点是第一个内容增量到达的时刻。
 */
function AttemptWaterfall({ attempts, now }: { attempts: AIGatewayAttempt[]; now: number }) {
  const spans = attempts.map((attempt) => {
    const start = parseInstant(attempt.startedAt)?.getTime() ?? now;
    const duration = attempt.status === "running" ? Math.max(now - start, attempt.latencyMs) : attempt.latencyMs;
    return { attempt, start, duration: Math.max(duration, 1) };
  });
  const origin = Math.min(...spans.map((span) => span.start));
  const total = Math.max(...spans.map((span) => span.start - origin + span.duration), 1);
  const percent = (value: number) => `${Math.min(Math.max((value / total) * 100, 0), 100)}%`;

  return (
    <div className="space-y-2">
      {spans.map(({ attempt, start, duration }) => {
        const offset = start - origin;
        const tone =
          attempt.status === "succeeded"
            ? "bg-emerald-500/70"
            : attempt.status === "failed"
              ? "bg-red-500/70"
              : attempt.status === "canceled"
                ? "bg-amber-500/70"
                : "bg-sky-500/70 animate-pulse";
        const rateLimit = Object.entries(attempt.rateLimit ?? {});
        return (
          <div key={attempt.seq} className="space-y-1.5 rounded-xl border border-border px-3 py-2.5">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
              <span className="font-mono text-muted-foreground">#{attempt.seq}</span>
              <span className="font-medium">{attempt.configName || attempt.provider || "通道"}</span>
              {attempt.provider ? <span className="text-muted-foreground">{attempt.provider}</span> : null}
              {attempt.inherited ? <span className="text-muted-foreground">平台共享</span> : null}
              {attempt.model ? <span className="font-mono text-muted-foreground">{attempt.model}</span> : null}
              <span className="ml-auto flex items-center gap-1.5">
                {attempt.httpStatus ? (
                  <span className={cn("font-mono", attempt.httpStatus >= 400 ? "text-destructive" : "text-muted-foreground")}>
                    HTTP {attempt.httpStatus}
                  </span>
                ) : null}
                <AttemptStatusBadge status={attempt.status} />
              </span>
            </div>
            <div className="relative h-2 rounded-full bg-muted">
              <div
                className={cn("absolute inset-y-0 rounded-full", tone)}
                style={{ left: percent(offset), width: percent(duration) }}
              />
              {attempt.headersMs ? (
                <div
                  className="absolute -inset-y-0.5 w-px bg-foreground/60"
                  style={{ left: percent(offset + attempt.headersMs) }}
                  title={`响应头 ${formatDuration(attempt.headersMs)}`}
                />
              ) : null}
              {attempt.firstTokenMs ? (
                <div
                  className="absolute top-1/2 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-background bg-foreground"
                  style={{ left: percent(offset + attempt.firstTokenMs) }}
                  title={`首字 ${formatDuration(attempt.firstTokenMs)}`}
                />
              ) : null}
            </div>
            <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground tabular-nums">
              <span>耗时 {formatDuration(duration)}</span>
              {attempt.headersMs ? <span>响应头 {formatDuration(attempt.headersMs)}</span> : null}
              {attempt.firstTokenMs ? <span>首字 {formatDuration(attempt.firstTokenMs)}</span> : null}
              {attempt.requestBytes ? <span>上行 {formatBytes(attempt.requestBytes)}</span> : null}
              {attempt.responseBytes ? <span>下行 {formatBytes(attempt.responseBytes)}</span> : null}
              {attempt.status === "failed" ? <span>{attempt.retryable ? "可重试，已换下一条" : "不可重试"}</span> : null}
            </div>
            {attempt.error ? <p className="break-words text-xs text-destructive">{attempt.error}</p> : null}
            {attempt.url || attempt.upstreamRequestId ? (
              <div className="space-y-0.5 text-[11px]">
                {attempt.url ? (
                  <div className="break-all font-mono text-muted-foreground">
                    {attempt.method ?? "POST"} {attempt.url}
                  </div>
                ) : null}
                {attempt.upstreamRequestId ? (
                  <div className="flex items-center gap-1">
                    <span className="text-muted-foreground">上游编号</span>
                    <span className="break-all font-mono">{attempt.upstreamRequestId}</span>
                    <CopyButton value={attempt.upstreamRequestId} className="size-6" />
                  </div>
                ) : null}
              </div>
            ) : null}
            {rateLimit.length > 0 ? (
              <div className="flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-[11px] text-muted-foreground">
                {rateLimit.map(([key, value]) => (
                  <span key={key}>
                    {key.replace(/^(x-ratelimit-|anthropic-ratelimit-)/, "")}={value}
                  </span>
                ))}
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function ContentSection({ title, content }: { title: string; content: Record<string, unknown> }) {
  const text = typeof content.text === "string" ? content.text : null;
  const reasoning = typeof content.reasoning === "string" ? content.reasoning : null;
  if (Object.keys(content).length === 0) {
    return (
      <Section title={title}>
        <p className="text-xs text-muted-foreground">无</p>
      </Section>
    );
  }
  return (
    <Section title={title}>
      {reasoning ? (
        <pre className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded-xl bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          {reasoning}
        </pre>
      ) : null}
      {text ? (
        <pre className="max-h-60 overflow-y-auto whitespace-pre-wrap break-words rounded-xl bg-muted/50 px-3 py-2 text-xs">
          {text}
        </pre>
      ) : null}
      <JsonViewer value={JSON.stringify(content, null, 2)} height={220} allowFullscreen />
    </Section>
  );
}

function RefundBox({ appKey, record }: { appKey: string; record: AIGatewayRequest }) {
  const refundMutation = useRefundAIGatewayRequestMutation(appKey);
  const [refunding, setRefunding] = useState(false);
  const [reason, setReason] = useState("");
  const refundable = record.status === "succeeded" || record.status === "aborted";
  if (!refundable) return null;

  const refund = async () => {
    try {
      await refundMutation.mutateAsync({ requestNo: record.requestNo, reason: reason.trim() });
      toast.success("已退款");
      setRefunding(false);
      setReason("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "退款失败");
    }
  };

  if (!refunding) {
    return (
      <div className="flex justify-end">
        <Button size="sm" variant="outline" onClick={() => setRefunding(true)}>
          <RotateCcw className="size-3.5" />
          整笔退款
        </Button>
      </div>
    );
  }
  return (
    <div className="space-y-2 rounded-xl border border-border p-3">
      <div className="flex flex-wrap gap-1.5">
        {REFUND_REASONS.map((item) => (
          <Button key={item} size="xs" variant={reason === item ? "secondary" : "outline"} onClick={() => setReason(item)}>
            {item}
          </Button>
        ))}
      </div>
      <Textarea value={reason} maxLength={200} rows={2} placeholder="退款原因" className="text-xs"
        onChange={(event) => setReason(event.target.value)} />
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={() => setRefunding(false)}>
          取消
        </Button>
        <Button size="sm" variant="destructive" onClick={() => void refund()} disabled={refundMutation.isPending}>
          {refundMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
          退回 {formatPoints(record.charged)} 额度
        </Button>
      </div>
    </div>
  );
}
