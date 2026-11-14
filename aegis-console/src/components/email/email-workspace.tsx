"use client";

import { useState } from "react";
import { Ban, Inbox, LayoutDashboard, LayoutTemplate, Mail, Server, ShieldCheck } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { useEmailProviderCatalogQuery, type EmailScope } from "@/lib/email-hooks";
import { useAppPermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { EmailChannelsPanel } from "./email-channels-panel";
import { EmailDeliveryTable } from "./email-delivery-table";
import { EmailOverviewPanel, type EmailOverviewTarget } from "./email-overview-panel";
import { EmailPolicyPanel } from "./email-policy-panel";
import { EmailSuppressionsPanel } from "./email-suppressions-panel";
import { EmailTemplatesPanel } from "./email-templates-panel";
import { useEmailPolicyDraft } from "./use-email-policy-draft";

/**
 * 邮件区块。平台级（`/configuration?tab=email`）与应用级（`/apps/{appKey}?tab=email`）共用，只差一个 scope。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 现在从哪条通道发信、有没有要处理的、发了多少、去哪儿了 |
 * | 通道 | 能从哪些出口发信，各自的凭据、发件人与回执地址 |
 * | 模板 | 品牌与每类信的文案，所见即收件人所见 |
 * | 发信策略 | 验证码规则、频次上限、退信抑制、重置链接白名单 |
 * | 投递记录 | 每封信的状态与回执时间线 |
 * | 抑制名单 | 不再投递的地址 |
 *
 * 模板与发信策略共用一份草稿（同一个 Policy），切换视图时未保存的修改不会丢。
 * 视图状态不进 URL：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: LayoutDashboard },
  { key: "channels", label: "通道", icon: Server },
  { key: "templates", label: "模板", icon: LayoutTemplate },
  { key: "policy", label: "发信策略", icon: ShieldCheck },
  { key: "deliveries", label: "投递记录", icon: Inbox },
  { key: "suppressions", label: "抑制名单", icon: Ban }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function EmailWorkspace({ scope }: { scope: EmailScope }) {
  if (scope.kind === "app" && scope.appId <= 0) {
    return <NoAppSelected icon={<Mail className="size-5" />} />;
  }
  return <Workspace scope={scope} />;
}

function Workspace({ scope }: { scope: EmailScope }) {
  const [view, setView] = useState<ViewKey>("overview");
  // 从概览跳到投递记录时带过去的筛选；每次跳转换一个 key，让列表按新筛选重新初始化
  const [deliveryFilter, setDeliveryFilter] = useState<{ key: number; status?: string; purpose?: string }>({ key: 0 });
  // 应用级按该应用判定，平台级不带应用（任一作用域持有即可）
  const can = useAppPermissionChecker(scope.kind === "app" ? scope.appId : null);
  const canWrite = can("email:write");
  const catalogQuery = useEmailProviderCatalogQuery();
  const providers = catalogQuery.data?.providers ?? [];
  const draft = useEmailPolicyDraft(scope);

  const navigate = (target: EmailOverviewTarget) => {
    if (target.view === "deliveries") {
      setDeliveryFilter((prev) => ({ key: prev.key + 1, status: target.status, purpose: target.purpose }));
    }
    setView(target.view);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => {
          const unsaved = draft.dirty && (item.key === "templates" || item.key === "policy");
          return (
            <button
              key={item.key}
              type="button"
              onClick={() => {
                if (item.key === "deliveries" && view !== "deliveries") {
                  setDeliveryFilter((prev) => ({ key: prev.key + 1 }));
                }
                setView(item.key);
              }}
              className={cn(
                "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                view === item.key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <item.icon className="size-3.5" />
              {item.label}
              {unsaved ? <span className="size-1.5 rounded-full bg-amber-500" aria-label="未保存" /> : null}
            </button>
          );
        })}
      </div>

      {view === "overview" ? <EmailOverviewPanel scope={scope} providers={providers} onNavigate={navigate} /> : null}
      {view === "channels" ? <EmailChannelsPanel scope={scope} providers={providers} canWrite={canWrite} /> : null}
      {view === "templates" ? <EmailTemplatesPanel scope={scope} draft={draft} canWrite={canWrite} /> : null}
      {view === "policy" ? <EmailPolicyPanel draft={draft} canWrite={canWrite} /> : null}
      {view === "deliveries" ? (
        <EmailDeliveryTable
          key={deliveryFilter.key}
          scope={scope}
          providers={providers}
          initialStatus={deliveryFilter.status}
          initialPurpose={deliveryFilter.purpose}
        />
      ) : null}
      {view === "suppressions" ? <EmailSuppressionsPanel scope={scope} canWrite={canWrite} /> : null}
    </div>
  );
}
