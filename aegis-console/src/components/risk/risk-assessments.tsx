"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle, CheckCircle2, ChevronLeft, ChevronRight, Eye, Fingerprint,
  Globe2, RefreshCw, Repeat, Search, Trash2, XCircle,
} from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  usePurgeAssessmentsMutation, useReplayAssessmentMutation, useReviewAssessmentMutation,
  useRiskAssessmentQuery, useRiskAssessmentsQuery,
} from "@/lib/risk-hooks";
import type {
  RiskAssessment, RiskAssessmentQuery, RiskEntitySummary, RiskEvalResult, RiskRuleEvaluation,
} from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  ActionBadge, DetailRow, LevelBadge, SceneBadge, ellipsisMiddle, fmtDateTime,
  fmtNumber, fmtRelative, useRiskCatalog,
} from "./risk-shared";

/**
 * 评估记录。
 *
 * 重构前这个页签**永远是空的** —— 前端把后端的 `{ list, total }` 当成
 * `{ items }` 读。修好之后真正的问题才浮现：只按场景 / 等级两项过滤、
 * 不分页、点进去没有详情，一条「命中 3 条规则、判 block」的记录
 * 无法解释成因，复核人只能凭猜。
 *
 * 现在：全维度筛选 + 分页 + 详情抽屉（判据逐条 + 环境快照 + 同 IP/设备/账号的近期行为 + 重放）。
 */

const PAGE_SIZE = 25;

export function RiskAssessmentsPanel({ presetIP, presetDevice, onClearPreset }: {
  presetIP?: string | null;
  presetDevice?: string | null;
  onClearPreset?: () => void;
}) {
  const { scenes, levels, actions } = useRiskCatalog();
  const [filters, setFilters] = useState({
    scene: "", riskLevel: "", action: "", reviewed: "", keyword: "",
    minScore: "", start: "", end: "",
  });
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<number | null>(null);
  const [purgeOpen, setPurgeOpen] = useState(false);
  const [purgeDays, setPurgeDays] = useState("30");

  const query = useMemo<RiskAssessmentQuery>(() => ({
    scene: filters.scene || undefined,
    riskLevel: filters.riskLevel || undefined,
    action: filters.action || undefined,
    reviewed: filters.reviewed || undefined,
    keyword: filters.keyword.trim() || undefined,
    minScore: filters.minScore ? Number(filters.minScore) : undefined,
    start: filters.start ? new Date(filters.start).toISOString() : undefined,
    end: filters.end ? new Date(filters.end).toISOString() : undefined,
    // 从大盘 Top 榜点进来时带上实体过滤
    ip: presetIP || undefined,
    deviceId: presetDevice || undefined,
    page,
    pageSize: PAGE_SIZE,
  }), [filters, page, presetIP, presetDevice]);

  const listQuery = useRiskAssessmentsQuery(query);
  const purgeMut = usePurgeAssessmentsMutation();
  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const patch = (next: Partial<typeof filters>) => {
    setFilters((prev) => ({ ...prev, ...next }));
    setPage(1); // 换筛选条件必须回第一页，否则会停在一个空页上
  };

  return (
    <div className="space-y-3">
      {(presetIP || presetDevice) && (
        <div className="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-xs">
          <Search className="size-3.5 text-primary" />
          <span>
            已按{presetIP ? " IP " : "设备 "}
            <code className="font-mono">{presetIP ?? ellipsisMiddle(presetDevice ?? "", 16, 8)}</code>
            {" "}过滤
          </span>
          <Button variant="ghost" size="sm" className="ml-auto h-6 px-2 text-xs" onClick={onClearPreset}>
            清除
          </Button>
        </div>
      )}

      <Card>
        <CardContent className="flex flex-wrap items-end gap-2 p-3">
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">搜索</Label>
            <Input value={filters.keyword} onChange={(e) => patch({ keyword: e.target.value })}
              placeholder="IP、设备号或账号" className="h-8 w-52 text-xs" />
          </div>
          <FilterSelect label="场景" value={filters.scene} onChange={(v) => patch({ scene: v })}
            options={scenes.map((s) => ({ value: s.value, label: s.label }))} />
          <FilterSelect label="等级" value={filters.riskLevel} onChange={(v) => patch({ riskLevel: v })}
            options={levels.map((l) => ({ value: l.value, label: l.label }))} />
          <FilterSelect label="动作" value={filters.action} onChange={(v) => patch({ action: v })}
            options={actions.map((a) => ({ value: a.value, label: a.label }))} />
          <FilterSelect label="复核" value={filters.reviewed} onChange={(v) => patch({ reviewed: v })}
            options={[{ value: "false", label: "未复核" }, { value: "true", label: "已复核" }]} />
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">最低分</Label>
            <Input type="number" value={filters.minScore} onChange={(e) => patch({ minScore: e.target.value })}
              placeholder="0" className="h-8 w-20 font-mono text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">起始时间</Label>
            <Input type="datetime-local" value={filters.start} onChange={(e) => patch({ start: e.target.value })}
              className="h-8 w-44 text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[10px] text-muted-foreground">结束时间</Label>
            <Input type="datetime-local" value={filters.end} onChange={(e) => patch({ end: e.target.value })}
              className="h-8 w-44 text-xs" />
          </div>
          <div className="ml-auto flex items-center gap-1">
            <Button variant="ghost" size="sm" className="h-8" onClick={() => listQuery.refetch()}>
              <RefreshCw className={cn("size-3.5", listQuery.isFetching && "animate-spin")} />
            </Button>
            <Button variant="ghost" size="sm" className="h-8 text-destructive" onClick={() => setPurgeOpen(true)}>
              <Trash2 className="size-3.5" />清理
            </Button>
          </div>
        </CardContent>
      </Card>

      {listQuery.isLoading && items.length === 0 ? (
        <div className="space-y-1.5">{Array.from({ length: 8 }, (_, i) => (
          <div key={i} className="h-11 animate-pulse rounded-lg bg-muted/50" />
        ))}</div>
      ) : items.length === 0 ? (
        <EmptyState title="没有匹配的评估记录"
          description="登录、注册等场景发生时才会产生记录，也可能是筛选条件过窄" />
      ) : (
        <>
          <div className="overflow-x-auto rounded-xl border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-36">时间</TableHead>
                  <TableHead className="w-20">场景</TableHead>
                  <TableHead className="w-16 text-right">分数</TableHead>
                  <TableHead className="w-20">等级</TableHead>
                  <TableHead className="w-20">动作</TableHead>
                  <TableHead className="w-36">IP</TableHead>
                  <TableHead>账号与设备</TableHead>
                  <TableHead className="w-40">命中规则</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.id} className="cursor-pointer text-xs" onClick={() => setDetailId(item.id)}>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {fmtDateTime(item.createdAt)}
                    </TableCell>
                    <TableCell><SceneBadge scene={item.scene} /></TableCell>
                    <TableCell className="text-right font-mono font-semibold tabular-nums">{item.totalScore}</TableCell>
                    <TableCell><LevelBadge level={item.riskLevel} /></TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <ActionBadge action={item.action} />
                        {item.action === "review" && (
                          <Badge variant={item.reviewed ? "success" : "warning"} size="sm">
                            {item.reviewed ? "已核" : "待核"}
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="font-mono">
                      {item.ip || "--"}
                      {item.country && <span className="ml-1 text-muted-foreground">{item.country}</span>}
                    </TableCell>
                    <TableCell className="max-w-0">
                      <div className="truncate">{item.account || "--"}</div>
                      <div className="truncate font-mono text-[10px] text-muted-foreground">
                        {item.deviceId ? ellipsisMiddle(item.deviceId, 12, 6) : "无设备标识"}
                      </div>
                    </TableCell>
                    <TableCell>
                      <MatchedRulesCell rules={item.matchedRules} />
                    </TableCell>
                    <TableCell><ChevronRight className="size-3.5 text-muted-foreground" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {detailId && <AssessmentDetailSheet id={detailId} onClose={() => setDetailId(null)} />}

      <AlertDialog open={purgeOpen} onOpenChange={setPurgeOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清理历史评估记录</AlertDialogTitle>
            <AlertDialogDescription>
              这张表随登录量持续增长，需要定期清理。删除后无法恢复，
              规则命中计数与 IP、设备档案不受影响。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-1">
            <Label className="text-xs">保留最近多少天</Label>
            <Input type="number" min={1} value={purgeDays} onChange={(e) => setPurgeDays(e.target.value)}
              className="h-8 w-32 font-mono text-xs" />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={async () => {
              const days = Number(purgeDays);
              if (!Number.isFinite(days) || days < 1) { toast.error("请填写有效天数"); return; }
              try {
                const result = await purgeMut.mutateAsync(days);
                toast.success(`已清理 ${fmtNumber(result.removed)} 条记录`);
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "清理失败");
              }
            }}>确认清理</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function FilterSelect({ label, value, onChange, options }: {
  label: string; value: string; onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <div className="space-y-1">
      <Label className="text-[10px] text-muted-foreground">{label}</Label>
      <Select value={value || "all"} onValueChange={(v) => onChange(v === "all" ? "" : v)}>
        <SelectTrigger className="h-8 w-24 text-xs"><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectItem value="all">全部</SelectItem>
          {options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
        </SelectContent>
      </Select>
    </div>
  );
}

function MatchedRulesCell({ rules }: { rules: RiskAssessment["matchedRules"] }) {
  const list = rules ?? [];
  if (list.length === 0) return <span className="text-[10px] text-muted-foreground">未命中</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {list.slice(0, 2).map((rule) => (
        <Badge key={rule.ruleId} variant="outline" size="sm" title={rule.reason}>
          {rule.ruleName} +{rule.score}
        </Badge>
      ))}
      {list.length > 2 && <Badge variant="outline" size="sm">+{list.length - 2}</Badge>}
    </div>
  );
}

export function Pagination({ page, totalPages, total, onChange }: {
  page: number; totalPages: number; total: number; onChange: (page: number) => void;
}) {
  return (
    <div className="flex items-center justify-between px-1 text-xs text-muted-foreground">
      <span className="tabular-nums">共 {fmtNumber(total)} 条，第 {page} / {totalPages} 页</span>
      <div className="flex items-center gap-1">
        <Button variant="outline" size="sm" className="h-7 px-2" disabled={page <= 1}
          onClick={() => onChange(page - 1)}>
          <ChevronLeft className="size-3.5" />
        </Button>
        <Button variant="outline" size="sm" className="h-7 px-2" disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}>
          <ChevronRight className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}

/* ══════════════════════════════════════════════════════════
   评估详情
   ══════════════════════════════════════════════════════════ */

export function AssessmentDetailSheet({ id, onClose }: { id: number; onClose: () => void }) {
  const query = useRiskAssessmentQuery(id);
  const replayMut = useReplayAssessmentMutation();
  const reviewMut = useReviewAssessmentMutation();
  const [replay, setReplay] = useState<RiskEvalResult | null>(null);
  const [comment, setComment] = useState("");

  const detail = query.data;
  const assessment = detail?.assessment;

  const runReplay = async () => {
    try {
      const result = await replayMut.mutateAsync(id);
      setReplay(result);
      toast.success("已用当前规则集重跑");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重放失败");
    }
  };

  const submitReview = async (result: "approved" | "rejected") => {
    try {
      await reviewMut.mutateAsync({ id, result, comment: comment.trim() || undefined });
      toast.success(result === "approved" ? "已通过" : "已拒绝并封禁相关 IP 与设备");
      onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "复核失败");
    }
  };

  const needsReview = assessment?.action === "review" && !assessment.reviewed;

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-3xl">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="flex items-center gap-2">
            <Eye className="size-4" />评估记录 #{id}
          </SheetTitle>
          <SheetDescription>
            判定当时的全部事实，以及同 IP、设备、账号的近期行为
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {query.isLoading || !detail || !assessment ? (
            <div className="space-y-3 p-5">
              {Array.from({ length: 6 }, (_, i) => <div key={i} className="h-14 animate-pulse rounded-lg bg-muted/50" />)}
            </div>
          ) : (
            <div className="space-y-5 px-5 py-4">
              <section className="grid grid-cols-4 gap-2">
                <VerdictTile label="总分" value={String(assessment.totalScore)} />
                <VerdictTile label="等级" node={<LevelBadge level={assessment.riskLevel} />} />
                <VerdictTile label="动作" node={<ActionBadge action={assessment.action} />} />
                <VerdictTile label="判定耗时" value={`${assessment.latencyMs} ms`} />
              </section>

              {assessment.actionDetail && (
                <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                  {assessment.actionDetail}
                </p>
              )}

              <section className="space-y-2">
                <SectionTitle>命中判据</SectionTitle>
                <RuleEvaluationList rules={detail.rules} />
              </section>

              <section className="space-y-2">
                <SectionTitle>请求事实</SectionTitle>
                <Card><CardContent className="p-3">
                  <DetailRow label="发生时间">{fmtDateTime(assessment.createdAt)}（{fmtRelative(assessment.createdAt)}）</DetailRow>
                  <DetailRow label="场景"><SceneBadge scene={assessment.scene} /></DetailRow>
                  <DetailRow label="账号" mono>{assessment.account || "--"}</DetailRow>
                  <DetailRow label="IP" mono>
                    {assessment.ip || "--"}
                    {assessment.country && <span className="ml-2 text-muted-foreground">{assessment.country}</span>}
                  </DetailRow>
                  <DetailRow label="设备标识" mono>{assessment.deviceId || "未上报"}</DetailRow>
                  <DetailRow label="User-Agent" mono>{assessment.userAgent || "未上报"}</DetailRow>
                  {assessment.appId != null && <DetailRow label="应用 ID" mono>{assessment.appId}</DetailRow>}
                  {assessment.userId != null && <DetailRow label="用户 ID" mono>{assessment.userId}</DetailRow>}
                </CardContent></Card>
              </section>

              <section className="space-y-2">
                <SectionTitle>环境快照</SectionTitle>
                <p className="text-[10px] text-muted-foreground">
                  评估时读到的全部变量，不受后续规则修改影响
                </p>
                <EvalContextGrid context={assessment.evalContext} />
              </section>

              <section className="space-y-2">
                <div className="flex items-center justify-between">
                  <SectionTitle>用当前规则集重放</SectionTitle>
                  <Button variant="outline" size="sm" className="h-7 text-xs" disabled={replayMut.isPending}
                    onClick={runReplay}>
                    <Repeat className={cn("size-3.5", replayMut.isPending && "animate-spin")} />重放
                  </Button>
                </div>
                <p className="text-[10px] text-muted-foreground">
                  用当前规则集重新计算，查看调整规则后的结果
                </p>
                {replay && (
                  <Card><CardContent className="space-y-2 p-3">
                    <div className="flex items-center gap-2 text-xs">
                      <span className="text-muted-foreground">当时：</span>
                      <Badge variant="outline" size="sm" className="font-mono">{assessment.totalScore} 分</Badge>
                      <ActionBadge action={assessment.action} />
                      <span className="text-muted-foreground">→ 现在：</span>
                      <Badge variant="outline" size="sm" className="font-mono">{replay.totalScore} 分</Badge>
                      <ActionBadge action={replay.action} />
                      {replay.totalScore !== assessment.totalScore && (
                        <Badge variant={replay.totalScore > assessment.totalScore ? "danger" : "success"} size="sm">
                          {replay.totalScore > assessment.totalScore ? "+" : ""}
                          {replay.totalScore - assessment.totalScore}
                        </Badge>
                      )}
                    </div>
                    <RuleEvaluationList rules={replay.evaluatedRules ?? null} showMisses />
                  </CardContent></Card>
                )}
              </section>

              <section className="grid gap-3 sm:grid-cols-2">
                <EntitySummaryCard title="该 IP 的画像" icon={Globe2} summary={detail.ipSummary} subject={assessment.ip} />
                <EntitySummaryCard title="该设备的画像" icon={Fingerprint} summary={detail.deviceSummary} subject={assessment.deviceId} />
              </section>

              <RelatedList title="同 IP 的近期评估" items={detail.sameIp} currentId={id} />
              <RelatedList title="同设备的近期评估" items={detail.sameDevice} currentId={id} />
              <RelatedList title="同账号的近期评估" items={detail.sameAccount} currentId={id} />

              <section className="space-y-2">
                <SectionTitle>复核</SectionTitle>
                {assessment.reviewed ? (
                  <Card><CardContent className="p-3">
                    <DetailRow label="结论">
                      <Badge variant={assessment.reviewResult === "approved" ? "success" : "danger"} size="sm">
                        {assessment.reviewResult === "approved" ? "通过" : "拒绝"}
                      </Badge>
                    </DetailRow>
                    <DetailRow label="复核人">{assessment.reviewerName || `#${assessment.reviewerId ?? "--"}`}</DetailRow>
                    <DetailRow label="时间">{fmtDateTime(assessment.reviewedAt)}</DetailRow>
                    {assessment.reviewComment && <DetailRow label="备注">{assessment.reviewComment}</DetailRow>}
                  </CardContent></Card>
                ) : needsReview ? (
                  <div className="space-y-2">
                    <Textarea rows={2} className="text-xs" value={comment} placeholder="复核备注，可留空"
                      onChange={(e) => setComment(e.target.value)} />
                    <div className="flex items-center gap-2">
                      <Button size="sm" disabled={reviewMut.isPending} onClick={() => submitReview("approved")}>
                        <CheckCircle2 className="size-3.5" />通过
                      </Button>
                      <Button size="sm" variant="outline" disabled={reviewMut.isPending} onClick={() => submitReview("rejected")}>
                        <XCircle className="size-3.5" />拒绝并封禁
                      </Button>
                    </div>
                    <p className="text-[10px] text-muted-foreground">
                      拒绝会把该 IP 与设备标记为封禁，标记来源记为人工，不会被情报刷新覆盖
                    </p>
                  </div>
                ) : (
                  <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                    处置动作不是人工复核，无需处理
                  </p>
                )}
              </section>
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <h4 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">{children}</h4>;
}

function VerdictTile({ label, value, node }: { label: string; value?: string; node?: React.ReactNode }) {
  return (
    <div className="rounded-lg border p-2.5 text-center">
      <div className="text-[10px] text-muted-foreground">{label}</div>
      <div className="mt-1 flex items-center justify-center">
        {node ?? <span className="font-mono text-lg font-bold tabular-nums">{value}</span>}
      </div>
    </div>
  );
}

/**
 * 规则轨迹列表。
 *
 * showMisses 打开时连未命中的规则一起列出 —— 只显示命中项等于让排查的人
 * 猜「另外几条为什么没中」，那是调规则时最耗时的一步。
 */
function RuleEvaluationList({ rules, showMisses }: { rules: RiskRuleEvaluation[] | null; showMisses?: boolean }) {
  const { conditionLabel } = useRiskCatalog();
  const list = (rules ?? []).filter((rule) => showMisses || rule.hit);
  if (list.length === 0) {
    return (
      <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
        未命中任何规则，总分 0
      </p>
    );
  }
  return (
    <div className="space-y-1">
      {list.map((rule) => (
        <div key={`${rule.ruleId}-${rule.ruleName}`}
          className={cn("flex items-start gap-2 rounded-lg border px-2.5 py-1.5 text-xs",
            rule.hit ? "border-amber-300/70 bg-amber-50/50 dark:border-amber-900/50 dark:bg-amber-950/20" : "opacity-60")}>
          {rule.hit
            ? <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
            : <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="font-medium">{rule.ruleName}</span>
              {rule.conditionType && <Badge variant="outline" size="sm">{conditionLabel(rule.conditionType)}</Badge>}
            </div>
            {rule.reason && <p className="mt-0.5 text-[11px] text-muted-foreground">{rule.reason}</p>}
            {rule.error && (
              <p className="mt-0.5 font-mono text-[10px] text-rose-600 dark:text-rose-400">评估出错：{rule.error}</p>
            )}
          </div>
          <span className={cn("shrink-0 font-mono font-semibold tabular-nums",
            rule.hit ? "text-amber-700 dark:text-amber-400" : "text-muted-foreground")}>
            {rule.hit ? `+${rule.score}` : `(${rule.score})`}
          </span>
        </div>
      ))}
    </div>
  );
}

/** 环境快照按后端目录的分组呈现，一坨扁平 JSON 是读不动的。 */
function EvalContextGrid({ context }: { context?: Record<string, unknown> | null }) {
  const { metadata } = useRiskCatalog();
  const entries = context ?? {};

  const grouped = useMemo(() => {
    const byName = new Map((metadata?.variables ?? []).map((v) => [v.name, v]));
    const map = new Map<string, Array<{ name: string; label: string; value: string }>>();
    for (const [name, raw] of Object.entries(entries)) {
      if (name === "extra") continue;
      const meta = byName.get(name);
      const group = meta?.group ?? "其他";
      const list = map.get(group) ?? [];
      list.push({ name, label: meta?.description ?? name, value: formatContextValue(raw) });
      map.set(group, list);
    }
    return [...map.entries()];
  }, [entries, metadata]);

  if (grouped.length === 0) {
    return (
      <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
        这条记录没有环境快照
      </p>
    );
  }

  const extra = entries.extra as Record<string, unknown> | undefined;

  return (
    <div className="space-y-2">
      {grouped.map(([group, items]) => (
        <div key={group} className="rounded-lg border">
          <div className="border-b bg-muted/30 px-2.5 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            {group}
          </div>
          <div className="grid gap-x-4 gap-y-0.5 p-2.5 sm:grid-cols-2">
            {items.map((item) => (
              <div key={item.name} className="flex items-baseline gap-2 text-[11px]" title={item.label}>
                <code className="shrink-0 font-mono text-muted-foreground">{item.name}</code>
                <span className="min-w-0 flex-1 truncate text-right font-mono tabular-nums">{item.value}</span>
              </div>
            ))}
          </div>
        </div>
      ))}
      {extra && Object.keys(extra).length > 0 && (
        <div className="rounded-lg border">
          <div className="border-b bg-muted/30 px-2.5 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            调用方透传
          </div>
          <pre className="overflow-x-auto p-2.5 font-mono text-[10px]">{JSON.stringify(extra, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

function formatContextValue(value: unknown): string {
  if (value === null || value === undefined) return "--";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (typeof value === "number") return Number.isInteger(value) ? String(value) : value.toFixed(2);
  if (typeof value === "string") return value === "" ? "--" : value;
  return JSON.stringify(value);
}

function EntitySummaryCard({ title, icon: Icon, summary, subject }: {
  title: string; icon: typeof Globe2; summary: RiskEntitySummary; subject: string;
}) {
  if (!subject) {
    return (
      <Card><CardContent className="p-3">
        <h5 className="flex items-center gap-1.5 text-xs font-medium"><Icon className="size-3.5" />{title}</h5>
        <p className="mt-2 text-[11px] text-muted-foreground">本次请求未携带该维度。</p>
      </CardContent></Card>
    );
  }
  return (
    <Card><CardContent className="space-y-1.5 p-3">
      <h5 className="flex items-center gap-1.5 text-xs font-medium"><Icon className="size-3.5" />{title}</h5>
      <div className="grid grid-cols-2 gap-x-3 gap-y-0.5 text-[11px]">
        <SummaryLine label="历史评估" value={fmtNumber(summary.totalAssessments)} />
        <SummaryLine label="其中拦截" value={fmtNumber(summary.blocked)} />
        <SummaryLine label="平均分" value={summary.avgScore.toFixed(1)} />
        <SummaryLine label="峰值分" value={String(summary.maxScore)} />
        <SummaryLine label="关联账号" value={fmtNumber(summary.distinctAccounts)} />
        <SummaryLine label="关联设备" value={fmtNumber(summary.distinctDevices)} />
      </div>
      <Separator />
      <p className="text-[10px] text-muted-foreground">
        首次 {fmtRelative(summary.firstSeenAt)}，最近 {fmtRelative(summary.lastSeenAt)}
      </p>
    </CardContent></Card>
  );
}

function SummaryLine({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono tabular-nums">{value}</span>
    </div>
  );
}

function RelatedList({ title, items, currentId }: {
  title: string; items: RiskAssessment[] | null; currentId: number;
}) {
  const list = (items ?? []).filter((item) => item.id !== currentId);
  if (list.length === 0) return null;
  return (
    <section className="space-y-2">
      <SectionTitle>{title}</SectionTitle>
      <div className="space-y-1">
        {list.map((item) => (
          <div key={item.id} className="flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[11px]">
            <SceneBadge scene={item.scene} />
            <LevelBadge level={item.riskLevel} />
            <ActionBadge action={item.action} />
            <span className="truncate font-mono text-muted-foreground">{item.account || item.ip || "--"}</span>
            <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
              {item.totalScore} 分，{fmtRelative(item.createdAt)}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
