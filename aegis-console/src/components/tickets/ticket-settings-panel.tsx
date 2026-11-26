"use client";

import { useCallback } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { motion } from "motion/react";
import { Coins, FolderTree, Info, MessageSquareText, Timer, Users, type LucideIcon } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import {
  useTicketCategoriesQuery,
  useTicketGroupsQuery,
  useTicketQuickRepliesQuery,
  useTicketSLAPoliciesQuery
} from "@/lib/ticket-hooks";
import { useFeedbackRewardPolicyQuery } from "@/lib/feedback-reward-hooks";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { CategorySection } from "./settings/category-section";
import { GroupSection } from "./settings/group-section";
import { QuickReplySection } from "./settings/quick-reply-section";
import { RewardSection } from "./settings/reward-section";
import { AppMark, ScopePicker, useSettingsScope } from "./settings/settings-scope";
import { SLASection } from "./settings/sla-section";

// 工单配置：分类 / 处理组 / SLA 策略 / 快捷回复 / 反馈奖励（只对应用范围有内容）。
//
// 顶部选配置范围（平台级 appid 0 或某个应用，放在 ?scope=），左侧分节导航（?section=），右侧是该分节的列表。
// 平台级配置对所有应用生效，后端只允许超级管理员修改；应用范围的列表里也会带出平台级条目，以「平台级」标记区分。
// 能不能改以后端为准：这里只在平台级范围给一行提示，不在前端自行禁用按钮。

const SECTIONS = ["categories", "groups", "sla", "quick-replies", "reward"] as const;
type Section = (typeof SECTIONS)[number];

const SECTION_META: Record<Section, { label: string; icon: LucideIcon }> = {
  categories: { label: "分类", icon: FolderTree },
  groups: { label: "处理组", icon: Users },
  sla: { label: "SLA 策略", icon: Timer },
  "quick-replies": { label: "快捷回复", icon: MessageSquareText },
  reward: { label: "反馈奖励", icon: Coins }
};

export function TicketSettingsPanel() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const scope = useSettingsScope();
  const isSuperAdmin = useAuthStore((state) => Boolean(state.operator?.isSuperAdmin));

  const rawSection = searchParams.get("section");
  const section: Section = SECTIONS.includes(rawSection as Section) ? (rawSection as Section) : "categories";
  const setSection = useCallback(
    (next: Section) => {
      const params = new URLSearchParams(searchParams.toString());
      if (next === "categories") params.delete("section");
      else params.set("section", next);
      router.replace(`${pathname}?${params.toString()}`, { scroll: false });
    },
    [pathname, router, searchParams]
  );

  const appId = scope.appId;
  const ready = scope.resolved;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-card px-4 py-3">
        <div className="flex min-w-0 items-center gap-2.5">
          {scope.current ? (
            <AppMark name={scope.current.name} className="size-8 rounded-lg text-xs" />
          ) : (
            <span className="flex size-8 items-center justify-center rounded-lg bg-muted text-xs font-semibold text-muted-foreground">
              平台
            </span>
          )}
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">配置范围</p>
            <p className="truncate text-sm font-medium text-foreground">
              {appId === 0 ? "平台级 · 对所有应用生效" : scope.current?.name ?? `应用 ${appId}`}
              {scope.current?.owned ? <span className="ml-1.5 text-xs font-normal text-primary">我创建的</span> : null}
            </p>
          </div>
        </div>
        <div className="ml-auto">
          <ScopePicker
            appId={appId}
            onChange={scope.setAppId}
            owned={scope.owned}
            others={scope.others}
            loading={scope.loading}
          />
        </div>
      </div>

      {appId === 0 && !isSuperAdmin ? (
        <div className="flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-foreground">
          <Info className="size-3.5 shrink-0 text-amber-600" />
          平台级配置只有超级管理员可以修改，你可以查看但保存会被拒绝。
        </div>
      ) : null}

      <div className="grid gap-4 md:grid-cols-[200px_minmax(0,1fr)]">
        <nav aria-label="配置分节" className="flex gap-1 overflow-x-auto md:flex-col md:overflow-visible">
          {SECTIONS.map((key) => (
            <SectionTab
              key={key}
              section={key}
              appId={appId}
              ready={ready}
              active={section === key}
              onSelect={() => setSection(key)}
            />
          ))}
        </nav>

        <div className="min-w-0">
          {!ready ? (
            <div className="space-y-3">
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-40 w-full rounded-xl" />
            </div>
          ) : (
            <motion.div
              key={`${section}-${appId}`}
              initial={{ opacity: 0, y: 4 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.16 }}
            >
              {section === "categories" ? <CategorySection appId={appId} /> : null}
              {section === "groups" ? <GroupSection appId={appId} /> : null}
              {section === "sla" ? <SLASection appId={appId} /> : null}
              {section === "quick-replies" ? <QuickReplySection appId={appId} /> : null}
              {section === "reward" ? <RewardSection appId={appId} /> : null}
            </motion.div>
          )}
        </div>
      </div>
    </div>
  );
}

function SectionTab({
  section,
  appId,
  ready,
  active,
  onSelect
}: {
  section: Section;
  appId: number;
  ready: boolean;
  active: boolean;
  onSelect: () => void;
}) {
  const { label, icon: Icon } = SECTION_META[section];
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={active ? "page" : undefined}
      className={cn(
        "relative flex shrink-0 items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors",
        active ? "text-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"
      )}
    >
      {active ? (
        <motion.span
          layoutId="ticket-settings-section"
          className="absolute inset-0 rounded-lg bg-accent"
          transition={{ type: "spring", stiffness: 500, damping: 40 }}
        />
      ) : null}
      <Icon className="relative size-4" />
      <span className="relative flex-1 text-left">{label}</span>
      <span className="relative min-w-5 text-right text-xs tabular-nums text-muted-foreground">
        {ready ? <SectionCount section={section} appId={appId} /> : null}
      </span>
    </button>
  );
}

/** 各分节的条目数：查询与分节内部共用缓存，不会多打请求 */
function SectionCount({ section, appId }: { section: Section; appId: number }) {
  if (section === "categories") return <CategoryCount appId={appId} />;
  if (section === "groups") return <GroupCount appId={appId} />;
  if (section === "sla") return <SLACount appId={appId} />;
  if (section === "reward") return <RewardState appId={appId} />;
  return <ReplyCount appId={appId} />;
}

/** 反馈奖励不是条目数，显示开没开 */
function RewardState({ appId }: { appId: number }) {
  const enabled = useFeedbackRewardPolicyQuery(appId).data?.enabled;
  if (appId === 0 || enabled === undefined) return null;
  return <>{enabled ? "开" : "关"}</>;
}

function CategoryCount({ appId }: { appId: number }) {
  return <>{useTicketCategoriesQuery(appId).data?.length ?? ""}</>;
}

function GroupCount({ appId }: { appId: number }) {
  return <>{useTicketGroupsQuery(appId).data?.length ?? ""}</>;
}

function SLACount({ appId }: { appId: number }) {
  return <>{useTicketSLAPoliciesQuery(appId).data?.length ?? ""}</>;
}

function ReplyCount({ appId }: { appId: number }) {
  return <>{useTicketQuickRepliesQuery(appId).data?.length ?? ""}</>;
}
