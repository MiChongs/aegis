"use client";

import { useState } from "react";
import { BarChart3, Calculator, Megaphone, ReceiptText, TicketPercent, Tickets } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { COUPON_DELETE, COUPON_ISSUE, COUPON_READ, COUPON_WRITE } from "@/lib/api/coupons";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { usePermissionsLoading } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { CouponOrdersPanel, type OrderFilter } from "./coupon-orders-panel";
import { CouponOverviewPanel } from "./coupon-overview-panel";
import { CouponQuotePanel } from "./coupon-quote-panel";
import { CouponTemplatesPanel } from "./coupon-templates-panel";
import { PromotionsPanel } from "./promotions-panel";
import { UserCouponsPanel } from "./user-coupons-panel";

/**
 * 优惠券与活动区块（docs/coupons.md）。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 发了多少券、用了多少、让利多少、带来多少实收；趋势、排行与发券来源 |
 * | 优惠券 | 每一种券的规则、库存与领券中心设置；上线、暂停、结束、发券 |
 * | 活动 | 限时折扣、满减与支付返券；时段、人群、名额与预算 |
 * | 用户的券 | 发到用户手里的每一张券，收回 |
 * | 订单优惠 | 每一笔用了优惠的订单：预占、结算、释放、退款冲正 |
 * | 试算 | 替某个用户把一笔订单过一遍定价引擎 |
 *
 * 读要 `app:coupon:read`，编辑 `app:coupon:write`，删除 `app:coupon:delete`，发券与收回 `app:coupon:issue`。
 * 视图状态不进 URL（`?tab=` 已被区块占用）；跨视图跳转（看某种券发给了谁、看某期活动的订单）带着筛选过去。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "templates", label: "优惠券", icon: TicketPercent },
  { key: "promotions", label: "活动", icon: Megaphone },
  { key: "coupons", label: "用户的券", icon: Tickets },
  { key: "orders", label: "订单优惠", icon: ReceiptText },
  { key: "quote", label: "试算", icon: Calculator }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppCouponsPanel({ appKey }: { appKey?: string | null }) {
  if (!appKey) return <NoAppSelected icon={<TicketPercent className="size-5" />} />;
  return <CouponWorkspace key={appKey} appKey={appKey} />;
}

function CouponWorkspace({ appKey }: { appKey: string }) {
  const can = useAppKeyPermissionChecker(appKey);
  const permissionsLoading = usePermissionsLoading();
  const [view, setView] = useState<ViewKey>("overview");
  const [couponTemplateId, setCouponTemplateId] = useState(0);
  const [orderFilter, setOrderFilter] = useState<OrderFilter>(null);

  if (permissionsLoading) return <LoadingState title="加载中" />;
  if (!can(COUPON_READ)) return <EmptyState title="缺少「优惠券与活动：查看」权限" />;
  const canWrite = can(COUPON_WRITE);
  const canDelete = can(COUPON_DELETE);
  const canIssue = can(COUPON_ISSUE);

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
              view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "overview" ? <CouponOverviewPanel appKey={appKey} /> : null}
      {view === "templates" ? (
        <CouponTemplatesPanel
          appKey={appKey}
          canWrite={canWrite}
          canDelete={canDelete}
          canIssue={canIssue}
          onOpenCoupons={(template) => {
            setCouponTemplateId(template.id);
            setView("coupons");
          }}
        />
      ) : null}
      {view === "promotions" ? (
        <PromotionsPanel
          appKey={appKey}
          canWrite={canWrite}
          canDelete={canDelete}
          onOpenOrders={(promotion) => {
            setOrderFilter({ kind: "promotion", id: promotion.id, name: promotion.name });
            setView("orders");
          }}
        />
      ) : null}
      {view === "coupons" ? (
        <UserCouponsPanel appKey={appKey} canIssue={canIssue} templateId={couponTemplateId} onTemplateChange={setCouponTemplateId} />
      ) : null}
      {view === "orders" ? <CouponOrdersPanel appKey={appKey} filter={orderFilter} onFilterChange={setOrderFilter} /> : null}
      {view === "quote" ? <CouponQuotePanel appKey={appKey} /> : null}
    </div>
  );
}
