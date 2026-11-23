"use client";

import { useCallback } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { motion } from "motion/react";
import { FileText, History, Radio, Workflow, type LucideIcon } from "lucide-react";
import { usePermissionChecker } from "@/lib/permissions";
import {
  useNotifyChannelsQuery,
  useNotifyDeliveryStatsQuery,
  useNotifySubscriptionsQuery,
  useNotifyTemplatesQuery
} from "@/lib/ticket-hooks";
import { cn } from "@/lib/utils";
import { ChannelsSection } from "./notify/notify-channels";
import { DeliveriesSection } from "./notify/notify-deliveries";
import { NotifyHealthStrip } from "./notify/notify-health";
import { SubscriptionsSection } from "./notify/notify-subscriptions";
import { TemplatesSection } from "./notify/notify-templates";

// 统一通知出口控制台。
//
// 这一屏就是「封装统一入口」的落点：业务侧只发事件 key，
// 管理员在这里决定「哪个事件 → 投到哪个渠道 → 用什么模板 → 什么条件下静默」。
// 渠道配置表单完全由后端 /catalog 的元数据驱动，新增一种 IM 时前端零改动。
//
// 顶部是近 7 天投递健康，下面左侧分节、右侧内容；分节记在地址栏 ?section=，
// 其余参数（tab 等）原样保留。

const SECTIONS = ["channels", "subscriptions", "templates", "deliveries"] as const;
type Section = (typeof SECTIONS)[number];

const SECTION_META: Record<Section, { label: string; hint: string; icon: LucideIcon }> = {
  channels: { label: "渠道", hint: "投递目标", icon: Radio },
  subscriptions: { label: "事件订阅", hint: "事件 → 渠道", icon: Workflow },
  templates: { label: "模板", hint: "消息格式", icon: FileText },
  deliveries: { label: "投递记录", hint: "每次发送结果", icon: History }
};

export function NotifyCenterPanel() {
  const can = usePermissionChecker();
  const canWrite = can("notify:channel:write");
  const canTest = can("notify:test");

  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const raw = searchParams.get("section");
  const section: Section = SECTIONS.includes(raw as Section) ? (raw as Section) : "channels";

  const go = useCallback(
    (next: Section) => {
      const params = new URLSearchParams(searchParams.toString());
      if (next === "channels") params.delete("section");
      else params.set("section", next);
      const query = params.toString();
      router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
    },
    [pathname, router, searchParams]
  );

  const channelsQuery = useNotifyChannelsQuery();
  const subsQuery = useNotifySubscriptionsQuery();
  const templatesQuery = useNotifyTemplatesQuery(0);
  const statsQuery = useNotifyDeliveryStatsQuery(7);

  const counts: Record<Section, { value?: number; alert?: boolean }> = {
    channels: { value: channelsQuery.data?.length, alert: channelsQuery.data?.some((item) => item.enabled && item.lastStatus && item.lastStatus !== "success") },
    subscriptions: { value: subsQuery.data?.length },
    templates: { value: templatesQuery.data?.length },
    deliveries: { value: statsQuery.data?.failed || undefined, alert: Boolean(statsQuery.data?.failed) }
  };

  return (
    <div className="space-y-4">
      <NotifyHealthStrip />

      <div className="grid gap-4 lg:grid-cols-[13rem_minmax(0,1fr)] lg:items-start">
        <nav
          aria-label="通知出口分节"
          className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1 lg:sticky lg:top-20 lg:mx-0 lg:flex-col lg:overflow-visible lg:p-0"
        >
          {SECTIONS.map((key) => {
            const meta = SECTION_META[key];
            const Icon = meta.icon;
            const active = key === section;
            const count = counts[key];
            return (
              <button
                key={key}
                type="button"
                onClick={() => go(key)}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "relative flex shrink-0 items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors lg:w-full",
                  active ? "text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                )}
              >
                {active ? (
                  <motion.span
                    layoutId="notify-section-active"
                    className="absolute inset-0 rounded-lg border bg-card shadow-sm"
                    transition={{ type: "spring", stiffness: 500, damping: 40 }}
                  />
                ) : null}
                <Icon className={cn("relative size-4 shrink-0", active && "text-primary")} />
                <span className="relative min-w-0 flex-1">
                  <span className="block text-sm font-medium leading-5">{meta.label}</span>
                  <span className="hidden text-[11px] leading-4 text-muted-foreground lg:block">{meta.hint}</span>
                </span>
                {count.value !== undefined ? (
                  <span
                    className={cn(
                      "relative min-w-5 rounded-full px-1.5 text-center text-[11px] leading-5 tabular-nums",
                      count.alert ? "bg-red-500/12 text-red-600 dark:text-red-400" : "bg-muted text-muted-foreground"
                    )}
                  >
                    {count.value}
                  </span>
                ) : null}
              </button>
            );
          })}
        </nav>

        <motion.div
          key={section}
          initial={{ opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
          className="min-w-0"
        >
          {section === "channels" ? <ChannelsSection canWrite={canWrite} canTest={canTest} /> : null}
          {section === "subscriptions" ? <SubscriptionsSection canWrite={canWrite} /> : null}
          {section === "templates" ? <TemplatesSection canWrite={canWrite} /> : null}
          {section === "deliveries" ? <DeliveriesSection canWrite={canWrite} /> : null}
        </motion.div>
      </div>
    </div>
  );
}
