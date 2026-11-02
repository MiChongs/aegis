"use client";

import { useState } from "react";
import { BarChart3, ClipboardCheck, Megaphone } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { PromoCampaignsPanel } from "@/components/apps/promo/promo-campaigns-panel";
import { formatCount } from "@/components/apps/promo/promo-shared";
import { PromoStatsPanel } from "@/components/apps/promo/promo-stats-panel";
import { PromoSubmissionsPanel } from "@/components/apps/promo/promo-submissions-panel";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { PromoSubmissionStatus } from "@/lib/api/promo";
import { usePromoSubmissionsQuery } from "@/lib/promo-hooks";

/**
 * 推广活动区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 活动 | 办了哪几期、什么时候、上没上线、开了哪些任务、参与与审核的大致数字；新建、编辑、上下线、删除 |
 * | 提交审核 | 用户提交的文章链接：打开原文核对，单条或批量通过、驳回（驳回写原因） |
 * | 活动统计 | 一期活动的参与、邀请、档位领取、分平台审核、按日走势与邀请排行 |
 *
 * 视图状态不进 URL，理由同积分商城：`?tab=` 已经被应用区块占用。从活动列表跳到审核或统计时带上活动。
 * 规则见 docs/promo.md。
 */
const VIEWS = [
  { key: "campaigns", label: "活动", icon: Megaphone },
  { key: "submissions", label: "提交审核", icon: ClipboardCheck },
  { key: "stats", label: "活动统计", icon: BarChart3 }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppPromoPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("campaigns");
  // 跨视图带过去的筛选条件
  const [submissionCampaign, setSubmissionCampaign] = useState<number | undefined>(undefined);
  const [submissionStatus, setSubmissionStatus] = useState<PromoSubmissionStatus | undefined>("pending");
  const [statsCampaign, setStatsCampaign] = useState<number | undefined>(undefined);

  // 全部活动的待审核数，挂在页签上
  const pendingQuery = usePromoSubmissionsQuery(appKey, { status: "pending", page: 1, limit: 1 });
  const pending = pendingQuery.data?.total ?? 0;

  if (!appKey) {
    return <NoAppSelected icon={<Megaphone className="size-5" />} />;
  }

  const openSubmissions = (campaignId?: number, status?: PromoSubmissionStatus) => {
    setSubmissionCampaign(campaignId);
    setSubmissionStatus(status);
    setView("submissions");
  };

  const openStats = (campaignId?: number) => {
    setStatsCampaign(campaignId);
    setView("stats");
  };

  return (
    <div className="flex flex-col gap-4">
      <Tabs value={view} onValueChange={(value) => setView(value as ViewKey)} className="gap-4">
        <div className="-mx-1 overflow-x-auto px-1">
          <TabsList className="h-10">
            {VIEWS.map((item) => (
              <TabsTrigger key={item.key} value={item.key} className="gap-1.5 px-3">
                <item.icon />
                {item.label}
                {item.key === "submissions" && pending > 0 ? (
                  <Badge variant="warning" size="sm" className="tabular-nums">
                    {formatCount(pending)}
                  </Badge>
                ) : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>

      {view === "campaigns" ? (
        <PromoCampaignsPanel appKey={appKey} onOpenSubmissions={openSubmissions} onOpenStats={openStats} />
      ) : null}
      {view === "submissions" ? (
        <PromoSubmissionsPanel
          appKey={appKey}
          campaignId={submissionCampaign}
          status={submissionStatus}
          onCampaignIdChange={setSubmissionCampaign}
          onStatusChange={setSubmissionStatus}
        />
      ) : null}
      {view === "stats" ? (
        <PromoStatsPanel
          appKey={appKey}
          campaignId={statsCampaign}
          onCampaignIdChange={setStatsCampaign}
          onOpenSubmissions={openSubmissions}
        />
      ) : null}
    </div>
  );
}
