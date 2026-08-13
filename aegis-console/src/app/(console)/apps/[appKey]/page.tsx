"use client";

import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { AppGovernanceNotice } from "@/components/apps/app-governance-notice";
import { AppDeleteDialog } from "@/components/apps/app-delete-dialog";
import { AppDetailHeader } from "@/components/apps/app-detail-header";
import { AppSectionNav, AppSectionTitle } from "@/components/apps/app-section-nav";
import { AppAuditPanel } from "@/components/apps/app-audit-panel";
import { AppAuthProtocolPanel } from "@/components/apps/app-auth-protocol-panel";
import { AppAuthSessionPanel } from "@/components/apps/app-auth-session-panel";
import { AppCaptchaPanel } from "@/components/apps/app-captcha-panel";
import { AppInfoPanel } from "@/components/apps/app-info-panel";
import { AppLotteryPanel } from "@/components/apps/app-lottery-panel";
import { AppOAuthPanel } from "@/components/apps/app-oauth-panel";
import { AppPasswordPanel } from "@/components/apps/app-password-panel";
import { AppPaymentPanel } from "@/components/apps/app-payment-panel";
import { AppSignInRewardPanel } from "@/components/apps/app-signin-reward-panel";
import { AppStatsPanel } from "@/components/apps/app-stats-panel";
import { AppUserSettingsPanel } from "@/components/apps/app-user-settings-panel";
import { AppCardKeyPanel } from "@/components/apps/card-key/app-card-key-panel";
import { AppVipPanel } from "@/components/apps/vip/app-vip-panel";
import { EmailConfigPanel } from "@/components/configuration/email-config-panel";
import { useAdminAppQuery, useAdminAppsQuery, useDeleteAdminAppMutation } from "@/lib/admin-hooks";
import { useAppGovernanceQuery } from "@/lib/platform-governance-hooks";
import { useAppScopeStore } from "@/lib/app-scope-store";
import { appSectionHref, resolveAppSection } from "@/lib/app-sections";
import type { AppSummary } from "@/lib/api/types";

/**
 * 应用详情 —— **全部应用级配置**的归属地，作用域就是路径里的 appKey。
 *
 * 与旧版的差别不只是多了一层路由：URL 里带上应用之后，任何一条链接（分享给同事、
 * 记在工单里、从别的页面跳回来）都同时带着「哪个应用 + 哪个区块」两个信息。
 * 旧版 `/apps?tab=oauth` 只说了后者，打开时配的是谁全凭运气。
 *
 * 平台级配置一律在 /configuration；平台对应用的强制结论在 /platform，
 * 那些结论应用管理员改不动，只会以顶部横幅的形式出现在这里。
 */
function AppDetailInner() {
  const params = useParams<{ appKey: string }>();
  const appKey = decodeURIComponent(String(params?.appKey ?? ""));
  const searchParams = useSearchParams();
  const router = useRouter();
  const section = resolveAppSection(searchParams.get("tab"));

  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const detailQuery = useAdminAppQuery(appKey);
  const app = useMemo<AppSummary | null>(
    () => apps.find((item) => item.appKey === appKey) ?? (detailQuery.data as AppSummary | undefined) ?? null,
    [apps, appKey, detailQuery.data]
  );

  // 治理状态与顶部横幅共用同一个查询（同 queryKey，不会多打一次请求）
  const governanceQuery = useAppGovernanceQuery(appKey);

  // 记住最近打开的应用：侧边栏那批不带 appKey 的深链靠它落回这里
  const setLastAppKey = useAppScopeStore((s) => s.setLastAppKey);
  useEffect(() => {
    if (app?.appKey) setLastAppKey(app.appKey);
  }, [app?.appKey, setLastAppKey]);

  const [deleteTarget, setDeleteTarget] = useState<AppSummary | null>(null);
  const deleteMutation = useDeleteAdminAppMutation();

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return;
    try {
      await deleteMutation.mutateAsync(deleteTarget.appKey);
      toast.success(`应用 ${deleteTarget.name} 已删除`);
      setDeleteTarget(null);
      setLastAppKey(null);
      router.replace("/apps");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [deleteTarget, deleteMutation, router, setLastAppKey]);

  const selectSection = useCallback(
    (next: string) => router.replace(appSectionHref(appKey, next), { scroll: false }),
    [appKey, router]
  );

  if (!app) {
    if (appsQuery.isLoading || detailQuery.isLoading) return <LoadingState title="加载应用" />;
    return (
      <div className="page-stack">
        <EmptyState title="应用不存在" description={`没有找到 AppKey 为 ${appKey || "（空）"} 的应用，它可能已被删除。`} />
        <Button asChild size="sm" variant="outline" className="h-8 w-fit text-xs">
          <Link href="/apps">返回应用列表</Link>
        </Button>
      </div>
    );
  }

  return (
    <div className="page-stack">
      <AppDetailHeader
        app={app}
        apps={apps}
        section={section}
        governanceState={governanceQuery.data?.governance.state}
        onDelete={setDeleteTarget}
      />

      {/* 被平台治理时的横幅：应用管理员在这里才看得到「被怎么了、为什么、怎么申诉」，
          否则他只会遇到一连串不明所以的 403 */}
      <AppGovernanceNotice appKey={appKey} />

      <div className="grid gap-4 lg:grid-cols-[12.5rem_minmax(0,1fr)] lg:gap-6">
        <AppSectionNav section={section} onSelect={selectSection} />
        <div className="min-w-0 space-y-4">
          <AppSectionTitle section={section} />
          <AppSectionPanel section={section} app={app} />
        </div>
      </div>

      {deleteTarget && (
        <AppDeleteDialog
          open
          onOpenChange={(open) => !open && setDeleteTarget(null)}
          appName={deleteTarget.name}
          onConfirm={handleDelete}
          isPending={deleteMutation.isPending}
        />
      )}
    </div>
  );
}

/**
 * 只挂载当前区块。
 *
 * 十几个面板同时挂载会在进页面的一瞬间打出十几条请求（OAuth 渠道、抽奖奖池、
 * 密码策略模板……），其中绝大多数当次根本不会被看到。
 */
function AppSectionPanel({ section, app }: { section: string; app: AppSummary }) {
  switch (section) {
    case "stats":
      return <AppStatsPanel appKey={app.appKey} />;
    case "audit":
      return <AppAuditPanel appKey={app.appKey} />;
    case "policy":
      return <AppAuthSessionPanel appKey={app.appKey} />;
    case "password":
      return <AppPasswordPanel appKey={app.appKey} />;
    case "captcha":
      return <AppCaptchaPanel appKey={app.appKey} />;
    case "oauth":
      return <AppOAuthPanel appKey={app.appKey} />;
    case "auth-protocol":
      return <AppAuthProtocolPanel appKey={app.appKey} />;
    case "email":
      return <EmailConfigPanel appId={app.id} />;
    case "payment":
      return <AppPaymentPanel appId={app.id} appKey={app.appKey} />;
    case "settings":
      return <AppUserSettingsPanel appId={app.id} />;
    case "vip":
      return <AppVipPanel appKey={app.appKey} />;
    case "card-key":
      return <AppCardKeyPanel appKey={app.appKey} />;
    case "signin-reward":
      return <AppSignInRewardPanel appKey={app.appKey} />;
    case "lottery":
      return <AppLotteryPanel appKey={app.appKey} />;
    default:
      return <AppInfoPanel appKey={app.appKey} />;
  }
}

export default function AppDetailPage() {
  return (
    <Suspense fallback={<LoadingState title="加载应用" />}>
      <AppDetailInner />
    </Suspense>
  );
}
