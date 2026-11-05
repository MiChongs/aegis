"use client";

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { AppGovernanceNotice } from "@/components/apps/app-governance-notice";
import { AppDeleteDialog } from "@/components/apps/app-delete-dialog";
import { AppDetailHero } from "@/components/apps/detail/app-detail-hero";
import { AppSectionHeading } from "@/components/apps/detail/app-section-heading";
import { AppSectionMobileBar } from "@/components/apps/detail/app-section-mobile";
import { AppSectionRail } from "@/components/apps/detail/app-section-rail";
import { AppAuditPanel } from "@/components/apps/app-audit-panel";
import { AppAuthProtocolPanel } from "@/components/apps/app-auth-protocol-panel";
import { AppAuthSessionPanel } from "@/components/apps/app-auth-session-panel";
import { AppCaptchaPanel } from "@/components/apps/app-captcha-panel";
import { AppInfoPanel } from "@/components/apps/app-info-panel";
import { AppLotteryPanel } from "@/components/apps/lottery/app-lottery-panel";
import { AppItemGrantPanel } from "@/components/apps/item-grant/app-item-grant-panel";
import { AppLevelsPanel } from "@/components/apps/levels/app-levels-panel";
import { AppRewardedAdPanel } from "@/components/apps/rewarded-ad/app-rewarded-ad-panel";
import { AppAdPolicyPanel } from "@/components/apps/ad-policy/app-ad-policy-panel";
import { AppCloudStoragePanel } from "@/components/apps/cloud-storage/app-cloud-storage-panel";
import { AppAIGatewayPanel } from "@/components/apps/ai-gateway/app-ai-gateway-panel";
import { AppOAuthPanel } from "@/components/apps/app-oauth-panel";
import { AppOAuth2ServerPanel } from "@/components/apps/app-oauth2-server-panel";
import { AppPasswordPanel } from "@/components/apps/app-password-panel";
import { AppPaymentPanel } from "@/components/apps/app-payment-panel";
import { AppSignInRewardPanel } from "@/components/apps/app-signin-reward-panel";
import { AppStatsPanel } from "@/components/apps/app-stats-panel";
import { AppUserSettingsPanel } from "@/components/apps/app-user-settings-panel";
import { AppCardKeyPanel } from "@/components/apps/card-key/app-card-key-panel";
import { AppPointsStorePanel } from "@/components/apps/points-store/app-points-store-panel";
import { AppInvitePanel } from "@/components/apps/invite/app-invite-panel";
import { AppPromoPanel } from "@/components/apps/promo/app-promo-panel";
import { AppDeviceLimitPanel } from "@/components/apps/device-limit/app-device-limit-panel";
import { AppRegisterIPPanel } from "@/components/apps/register-ip/app-register-ip-panel";
import { AppPasskeyPanel } from "@/components/apps/app-passkey-panel";
import { AppCrashReportsPanel } from "@/components/apps/crash-reports/app-crash-reports-panel";
import { AppListenTogetherPanel } from "@/components/apps/listen-together/app-listen-together-panel";
import { AppLeaderboardPanel } from "@/components/apps/leaderboard/app-leaderboard-panel";
import { AppAccountSetupPanel } from "@/components/apps/account-setup/app-account-setup-panel";
import { AppAccountDeletionPanel } from "@/components/apps/account-deletion/app-account-deletion-panel";
import { AppVipPanel } from "@/components/apps/vip/app-vip-panel";
import { EmailWorkspace } from "@/components/email/email-workspace";
import { AIChannelPanel } from "@/components/ai/ai-channel-panel";
import { useAdminAppQuery, useAdminAppsQuery, useDeleteAdminAppMutation } from "@/lib/admin-hooks";
import { useAppGovernanceQuery } from "@/lib/platform-governance-hooks";
import { useAppScopeStore } from "@/lib/app-scope-store";
import { useAppSectionNavStore } from "@/lib/app-section-nav-store";
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

  // 记下进过的区块，区块目录顶部的「最近」靠它
  const visitSection = useAppSectionNavStore((s) => s.visit);
  useEffect(() => {
    visitSection(section);
  }, [section, visitSection]);

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

  // 换区块时不跳回页顶（头部还在原处），但若已经滚过了内容区开头，就把新区块的开头拉回吸顶条下方，
  // 否则在长面板底部点了「下一个」，看到的是新面板的半截
  const contentRef = useRef<HTMLDivElement>(null);
  const selectSection = useCallback(
    (next: string) => {
      router.replace(appSectionHref(appKey, next), { scroll: false });
      const el = contentRef.current;
      if (!el) return;
      const margin = parseFloat(getComputedStyle(el).scrollMarginTop) || 0;
      if (el.getBoundingClientRect().top < margin) el.scrollIntoView({ block: "start" });
    },
    [appKey, router]
  );

  if (!app) {
    if (appsQuery.isLoading || detailQuery.isLoading) return <LoadingState title="加载应用" />;
    return (
      <div className="page-stack">
        <EmptyState title="应用不存在" />
        <Button asChild size="sm" variant="outline" className="h-8 w-fit text-xs">
          <Link href="/apps">返回应用列表</Link>
        </Button>
      </div>
    );
  }

  return (
    <div className="page-stack min-w-0 gap-4 lg:gap-5">
      <AppDetailHero
        app={app}
        apps={apps}
        section={section}
        governanceState={governanceQuery.data?.governance.state}
        onDelete={setDeleteTarget}
      />

      {/* 被平台治理时的横幅：应用管理员在这里才看得到「被怎么了、为什么、怎么申诉」，
          否则他只会遇到一连串不明所以的 403 */}
      <AppGovernanceNotice appKey={appKey} />

      {/* 1280 以下的吸顶区块条：必须是页面根的直接子元素，吸顶范围才是整页而不是某个格子。
          分界放在 xl 而不是 lg：1024 宽时控制台侧栏已占去三百多像素，再常驻一列区块导航，
          面板只剩四百来宽，却按 lg 断点排成双栏，验证码页右栏只剩 41px */}
      <AppSectionMobileBar section={section} onSelect={selectSection} />

      <div className="grid min-w-0 gap-4 xl:grid-cols-[14.5rem_minmax(0,1fr)] xl:gap-5 2xl:grid-cols-[15.5rem_minmax(0,1fr)] 2xl:gap-6">
        <AppSectionRail section={section} onSelect={selectSection} />
        {/* app-detail-content：把面板关在视口内（见 globals.css），宽表格与工具条在自己的格子里横滑 */}
        <div ref={contentRef} className="app-detail-content min-w-0 scroll-mt-40 space-y-4 xl:scroll-mt-20">
          <AppSectionHeading section={section} onSelect={selectSection} />
          <AppSectionPanel section={section} app={app} onDelete={() => setDeleteTarget(app)} />
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
function AppSectionPanel({ section, app, onDelete }: { section: string; app: AppSummary; onDelete: () => void }) {
  switch (section) {
    case "stats":
      return <AppStatsPanel appKey={app.appKey} />;
    case "audit":
      return <AppAuditPanel appKey={app.appKey} />;
    case "crash-reports":
      return <AppCrashReportsPanel appKey={app.appKey} />;
    case "listen-together":
      return <AppListenTogetherPanel appKey={app.appKey} />;
    case "policy":
      return <AppAuthSessionPanel appKey={app.appKey} />;
    case "device-limit":
      return <AppDeviceLimitPanel appKey={app.appKey} />;
    case "register-ip":
      return <AppRegisterIPPanel appKey={app.appKey} />;
    case "password":
      return <AppPasswordPanel appKey={app.appKey} />;
    case "passkey":
      return <AppPasskeyPanel appKey={app.appKey} />;
    case "captcha":
      return <AppCaptchaPanel appKey={app.appKey} />;
    case "oauth":
      return <AppOAuthPanel appKey={app.appKey} />;
    case "account-setup":
      return <AppAccountSetupPanel appKey={app.appKey} />;
    case "oauth2-server":
      return <AppOAuth2ServerPanel appKey={app.appKey} />;
    case "auth-protocol":
      return <AppAuthProtocolPanel appKey={app.appKey} />;
    case "email":
      return <EmailWorkspace scope={{ kind: "app", appId: app.id }} />;
    case "ai":
      return <AIChannelPanel scope={{ kind: "app", appKey: app.appKey }} />;
    case "ai-gateway":
      return <AppAIGatewayPanel appKey={app.appKey} />;
    case "payment":
      return <AppPaymentPanel appId={app.id} appKey={app.appKey} />;
    case "cloud-storage":
      return <AppCloudStoragePanel appKey={app.appKey} />;
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
    case "item-grant":
      return <AppItemGrantPanel appKey={app.appKey} />;
    case "levels":
      return <AppLevelsPanel appKey={app.appKey} />;
    case "rewarded-ad":
      return <AppRewardedAdPanel appKey={app.appKey} />;
    case "ad-policy":
      return <AppAdPolicyPanel appKey={app.appKey} />;
    case "points-store":
      return <AppPointsStorePanel appKey={app.appKey} />;
    case "invite":
      return <AppInvitePanel appKey={app.appKey} />;
    case "promo":
      return <AppPromoPanel appKey={app.appKey} />;
    case "leaderboard":
      return <AppLeaderboardPanel appKey={app.appKey} />;
    case "account-deletion":
      return <AppAccountDeletionPanel appKey={app.appKey} />;
    default:
      return <AppInfoPanel appKey={app.appKey} onDelete={onDelete} />;
  }
}

export default function AppDetailPage() {
  return (
    <Suspense fallback={<LoadingState title="加载应用" />}>
      <AppDetailInner />
    </Suspense>
  );
}
