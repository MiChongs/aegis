"use client";

import { useState } from "react";
import { Layers, ReceiptText, Ticket } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { CardKeyBatchesPanel } from "@/components/apps/card-key/card-key-batches-panel";
import { CardKeyCodesPanel } from "@/components/apps/card-key/card-key-codes-panel";
import { CardKeyRedemptionsPanel } from "@/components/apps/card-key/card-key-redemptions-panel";
import { cn } from "@/lib/utils";

/**
 * 卡密区块。
 *
 * 三个视图回答三个不同的问题：
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 批次 | 发了几批、每批发什么、用掉多少 |
 * | 卡密 | 某一张卡现在是什么状态、绑在谁名下、绑了几台设备 |
 * | 核销记录 | 谁在什么时候用了哪张卡、实际发出去了什么 |
 *
 * 视图状态**不进 URL**：`?tab=` 已经被应用区块占用，再塞一层会让
 * 「分享出去的链接指向哪」变成两个参数的组合问题（与会员区块同一条约束）。
 *
 * 「卡密登录」的开关不在这里 —— 认证方式归 `?tab=auth-protocol`（接入）的
 * loginMethods 管，在这里再放一个就是同一件事的第二个配置入口，
 * 接入方无从判断哪个生效。
 */
const VIEWS = [
  { key: "batches", label: "批次", icon: Layers },
  { key: "codes", label: "卡密", icon: Ticket },
  { key: "redemptions", label: "核销记录", icon: ReceiptText }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppCardKeyPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("batches");
  // 从批次跳到「这一批的卡」时带过去的筛选条件。
  const [batchFilter, setBatchFilter] = useState<number | undefined>(undefined);

  if (!appKey) {
    return <NoAppSelected icon={<Ticket className="size-5" />} />;
  }

  const openBatchCodes = (batchId: number) => {
    setBatchFilter(batchId);
    setView("codes");
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "batches" ? (
        <CardKeyBatchesPanel appKey={appKey} onOpenCodes={openBatchCodes} />
      ) : null}
      {view === "codes" ? (
        <CardKeyCodesPanel appKey={appKey} batchId={batchFilter} onBatchIdChange={setBatchFilter} />
      ) : null}
      {view === "redemptions" ? <CardKeyRedemptionsPanel appKey={appKey} /> : null}
    </div>
  );
}
