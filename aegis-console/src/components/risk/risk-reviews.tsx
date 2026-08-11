"use client";

import { useState } from "react";
import { CheckCircle2, Clock, Eye, XCircle } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { usePendingReviewsQuery, useReviewAssessmentMutation } from "@/lib/risk-hooks";
import { AssessmentDetailSheet, Pagination } from "./risk-assessments";
import {
  ActionBadge, LevelBadge, SceneBadge, ellipsisMiddle, fmtDateTime, fmtRelative,
} from "./risk-shared";

/**
 * 人工复核队列。
 *
 * 与重构前的差别在于「能不能判」：以前一行只有场景、分数、IP 和两个按钮，
 * 复核人拿不到任何判断依据。现在每行给出命中判据摘要与关联维度，
 * 需要深挖时直接开详情抽屉（与评估记录页共用同一个组件）。
 */

const PAGE_SIZE = 20;

export function RiskReviewsPanel() {
  const [page, setPage] = useState(1);
  const [detailId, setDetailId] = useState<number | null>(null);
  const query = usePendingReviewsQuery(page, PAGE_SIZE);
  const reviewMut = useReviewAssessmentMutation();

  const items = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const decide = async (id: number, result: "approved" | "rejected") => {
    try {
      await reviewMut.mutateAsync({ id, result });
      toast.success(result === "approved" ? "已通过" : "已拒绝并封禁相关 IP 与设备");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "复核失败");
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Clock className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">待复核队列</h3>
        <Badge variant={total > 0 ? "warning" : "outline"} size="sm">{total}</Badge>
        <span className="text-xs text-muted-foreground">
          处置动作为人工复核且尚未裁决的记录
        </span>
      </div>

      {query.isLoading && items.length === 0 ? (
        <div className="space-y-2">{Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="h-20 animate-pulse rounded-xl bg-muted/50" />
        ))}</div>
      ) : items.length === 0 ? (
        <EmptyState title="队列已清空"
          description="把某段分数的处置动作设为人工复核后，命中的请求会进入这里" />
      ) : (
        <>
          <div className="space-y-2">
            {items.map((item) => (
              <Card key={item.id}>
                <CardContent className="space-y-2 p-3">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <SceneBadge scene={item.scene} />
                    <LevelBadge level={item.riskLevel} />
                    <ActionBadge action={item.action} />
                    <Badge variant="outline" size="sm" className="font-mono">{item.totalScore} 分</Badge>
                    <span className="text-xs text-muted-foreground">{fmtRelative(item.createdAt)}</span>
                    <span className="ml-auto text-[10px] text-muted-foreground">{fmtDateTime(item.createdAt)}</span>
                  </div>

                  <div className="grid gap-x-4 gap-y-0.5 text-[11px] sm:grid-cols-3">
                    <Field label="账号" value={item.account || "--"} />
                    <Field label="IP" value={`${item.ip || "--"}${item.country ? ` ${item.country}` : ""}`} />
                    <Field label="设备" value={item.deviceId ? ellipsisMiddle(item.deviceId, 14, 6) : "未上报"} />
                  </div>

                  {(item.matchedRules ?? []).length > 0 && (
                    <div className="space-y-0.5 rounded-lg border bg-muted/30 px-2.5 py-1.5">
                      {(item.matchedRules ?? []).map((rule) => (
                        <div key={rule.ruleId} className="flex items-start gap-2 text-[11px]">
                          <span className="shrink-0 font-medium">{rule.ruleName}</span>
                          <span className="min-w-0 flex-1 truncate text-muted-foreground">{rule.reason || "—"}</span>
                          <span className="shrink-0 font-mono text-amber-700 dark:text-amber-400">+{rule.score}</span>
                        </div>
                      ))}
                    </div>
                  )}

                  <div className="flex items-center gap-1.5">
                    <Button size="sm" className="h-7 text-xs" disabled={reviewMut.isPending}
                      onClick={() => decide(item.id, "approved")}>
                      <CheckCircle2 className="size-3.5" />通过
                    </Button>
                    <Button size="sm" variant="outline" className="h-7 text-xs" disabled={reviewMut.isPending}
                      onClick={() => decide(item.id, "rejected")}>
                      <XCircle className="size-3.5" />拒绝并封禁
                    </Button>
                    <Button size="sm" variant="ghost" className="ml-auto h-7 text-xs"
                      onClick={() => setDetailId(item.id)}>
                      <Eye className="size-3.5" />查看完整上下文
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
        </>
      )}

      {detailId && <AssessmentDetailSheet id={detailId} onClose={() => setDetailId(null)} />}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate font-mono">{value}</span>
    </div>
  );
}
