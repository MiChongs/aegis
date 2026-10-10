"use client";

import { Button } from "@/components/ui/button";

/** 列表底部的分页条，与激励广告记录表同一形态。 */
export function AdPolicyPager({
  total,
  page,
  totalPages,
  onPage
}: {
  total: number;
  page: number;
  totalPages: number;
  onPage: (page: number) => void;
}) {
  if (total <= 0) return null;
  return (
    <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
      <span className="tabular-nums">
        共 {total} 条 · 第 {page}/{totalPages} 页
      </span>
      <div className="flex gap-1.5">
        <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          上一页
        </Button>
        <Button size="sm" variant="outline" disabled={page >= totalPages} onClick={() => onPage(page + 1)}>
          下一页
        </Button>
      </div>
    </div>
  );
}
