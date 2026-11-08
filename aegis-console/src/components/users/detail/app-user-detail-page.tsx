"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Activity,
  AppWindow,
  ArrowLeft,
  ArrowUpRight,
  Ban,
  ChevronRight,
  Cloud,
  Coins,
  Copy,
  Crown,
  Gavel,
  Layers,
  Link2,
  ListChecks,
  MoreHorizontal,
  PencilLine,
  Receipt,
  Scale,
  ShieldAlert,
  ShieldCheck,
  ShieldHalf,
  Sparkles,
  Trash2,
  UserRound,
  Wallet
} from "lucide-react";
import { toast } from "sonner";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import { useAdminAppUserQuery, useAdminAppsQuery, useDeleteAdminAppUserMutation } from "@/lib/admin-hooks";
import { useAdminUserActiveBanQuery, useAdminUserWalletQuery } from "@/lib/app-user-hooks";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { UserDeleteDialog } from "../user-delete-dialog";
import { CopyButton, StateBlock, ToneIcon, copyToClipboard, formatInt, type Tone } from "./kit";
import { UserActivityTab } from "./user-activity-tab";
import { UserRiskTab } from "./user-risk-tab";
import { UserAdPolicyTab } from "./user-ad-policy-tab";
import { UserAssetsTab } from "./user-assets-tab";
import { UserCloudTab } from "./user-cloud-tab";
import {
  BAN_SCOPE_LABEL,
  deriveUserSignals,
  formatMoney,
  hasWallet,
  isPast,
  joinText,
  textValue,
  userInitials,
  type UserSignal
} from "./user-detail-shared";
import { UserGovernanceTab } from "./user-governance-tab";
import { UserGrowthTab } from "./user-growth-tab";
import { UserLedgerTab } from "./user-ledger-tab";
import { UserOverviewTab } from "./user-overview-tab";
import { UserProfileTab } from "./user-profile-tab";
import { UserRecordsTab } from "./user-records-tab";
import { UserSecurityTab } from "./user-security-tab";

type Props = {
  appKey: string;
  userId: number;
  fromHref?: string;
};

type TabDef = { value: string; label: string; icon: React.ComponentType<{ className?: string }> };

/** 页签按工作面分组；宽屏是左侧分组导航，窄屏是横向滑动的一条。 */
const TAB_GROUPS: Array<{ label: string; tabs: TabDef[] }> = [
  {
    label: "账户",
    tabs: [
      { value: "overview", label: "概览", icon: Layers },
      { value: "profile", label: "资料", icon: UserRound },
      { value: "security", label: "安全", icon: ShieldCheck },
      { value: "activity", label: "活动", icon: Activity }
    ]
  },
  {
    label: "资产",
    tabs: [
      { value: "assets", label: "资产与权益", icon: Wallet },
      { value: "growth", label: "奖惩", icon: Scale },
      { value: "ledger", label: "流水", icon: Receipt },
      { value: "records", label: "互动记录", icon: ListChecks }
    ]
  },
  {
    label: "服务",
    tabs: [
      { value: "cloud", label: "云存储", icon: Cloud },
      { value: "ad-policy", label: "广告", icon: ShieldHalf }
    ]
  },
  {
    label: "管理",
    tabs: [
      { value: "risk", label: "风险", icon: ShieldAlert },
      { value: "governance", label: "处置", icon: Gavel }
    ]
  }
];

const TABS = TAB_GROUPS.flatMap((group) => group.tabs);
const TAB_VALUES = new Set(TABS.map((item) => item.value));

/** 顶栏高度（console-topbar 的 h-14） */
const TOPBAR_HEIGHT = 56;

/**
 * 应用用户详情。
 *
 * 宽屏两栏：左侧是常驻的身份栏（这是谁、状态、四个余额、分组导航、来源信息），
 * 右侧是当前工作面；身份栏吸顶，长页面滚到底也能直接换页签。
 * 窄屏单栏：身份卡在上，页签条吸顶横向滑动。
 *
 * 页签同步到 `?tab=`（流水等页签的子页再用 `?sub=`），深链能直接落到「处置」或「积分流水」。
 * `from` 参数原样保留，返回时回到来源列表的筛选态与页码。
 */
export function AppUserDetailPage({ appKey, userId, fromHref }: Props) {
  const router = useRouter();
  const searchParams = useSearchParams();

  const userQuery = useAdminAppUserQuery(appKey, userId);
  const appsQuery = useAdminAppsQuery();
  const activeBanQuery = useAdminUserActiveBanQuery(appKey, userId);
  const walletQuery = useAdminUserWalletQuery(appKey, userId);
  const deleteMutation = useDeleteAdminAppUserMutation(appKey);

  const [showDelete, setShowDelete] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);

  const user = userQuery.data as AdminAppUserDetail | undefined;
  const activeBan = activeBanQuery.data ?? null;
  const app = useMemo(() => appsQuery.data?.find((item) => item.appKey === appKey) ?? null, [appKey, appsQuery.data]);

  const rawTab = searchParams.get("tab");
  const tab = rawTab && TAB_VALUES.has(rawTab) ? rawTab : "overview";
  const backHref = fromHref || `/app-users?app=${encodeURIComponent(appKey)}`;
  const signals = useMemo(() => deriveUserSignals(user, activeBan), [user, activeBan]);

  /** 换页签；内容区顶部已滚出视口时拉回来，避免停在新内容的半中间。子页参数只在显式给出时保留。 */
  function goTab(next: string, sub?: string) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("tab", next);
    if (sub) params.set("sub", sub);
    else params.delete("sub");
    router.replace(`?${params.toString()}`, { scroll: false });

    const anchor = contentRef.current;
    if (!anchor) return;
    const top = anchor.getBoundingClientRect().top;
    if (top < TOPBAR_HEIGHT || top > window.innerHeight * 0.7) {
      window.scrollTo({ top: window.scrollY + top - TOPBAR_HEIGHT - 12, behavior: "smooth" });
    }
  }

  const title = textValue(user?.nickname || user?.profile?.nickname, textValue(user?.account, `用户 #${userId}`));

  async function handleDelete() {
    try {
      await deleteMutation.mutateAsync(userId);
      toast.success("用户已删除");
      router.push(backHref);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  const activeTab = TABS.find((item) => item.value === tab) ?? TABS[0];

  return (
    <>
      <div className="page-stack">
        <div className="flex min-w-0 items-center gap-1.5">
          <Button asChild variant="ghost" size="sm" className="-ml-2 shrink-0 text-muted-foreground hover:text-foreground">
            <Link href={backHref}>
              <ArrowLeft />
              <span className="sm:hidden">返回</span>
              <span className="hidden sm:inline">返回列表</span>
            </Link>
          </Button>
          <span className="hidden h-4 w-px shrink-0 bg-border sm:block" aria-hidden />
          <Breadcrumb className="hidden min-w-0 pl-1.5 sm:block">
            <BreadcrumbList className="flex-nowrap">
              <BreadcrumbItem className="shrink-0">
                <BreadcrumbLink asChild>
                  <Link href="/app-users">应用用户</Link>
                </BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem className="shrink-0">
                <BreadcrumbLink asChild>
                  <Link href={backHref}>{app?.name || "用户列表"}</Link>
                </BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem className="min-w-0">
                <BreadcrumbPage className="truncate">{userQuery.isLoading ? "加载中" : title}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
        </div>

        {userQuery.isLoading ? (
          <DetailSkeleton />
        ) : !user ? (
          <Card className="py-0">
            <StateBlock
              icon={UserRound}
              kind={userQuery.isError ? "error" : "empty"}
              title={userQuery.isError ? "用户信息加载失败" : "未找到该用户"}
              description={
                userQuery.isError
                  ? undefined
                  : `应用 ${app?.name || appKey} 中不存在用户 #${userId}，可能已被删除。`
              }
              onRetry={() => userQuery.refetch()}
              action={
                <Button asChild size="sm" variant="outline">
                  <Link href={backHref}>返回列表</Link>
                </Button>
              }
            />
          </Card>
        ) : (
          <div className="grid gap-5 xl:grid-cols-[296px_minmax(0,1fr)] xl:items-start">
            <aside className="flex min-w-0 flex-col gap-4 xl:sticky xl:top-[72px]">
              <IdentityCard
                user={user}
                appKey={appKey}
                walletBalance={hasWallet(walletQuery.data) ? walletQuery.data?.balance : undefined}
                activeBanScope={activeBan?.banScope}
                onNavigate={goTab}
                onDelete={() => setShowDelete(true)}
              />
              <SideNav tab={tab} hasActiveBan={Boolean(activeBan)} onSelect={goTab} />
              <SourceCard user={user} appKey={appKey} appName={app?.name} />
            </aside>

            <div ref={contentRef} className="flex min-w-0 flex-col gap-4">
              <MobileTabBar tab={tab} hasActiveBan={Boolean(activeBan)} onSelect={goTab} />

              <div className="hidden items-center gap-3 xl:flex">
                <ToneIcon icon={activeTab.icon} tone="primary" />
                <h2 className="text-lg font-semibold tracking-tight">{activeTab.label}</h2>
              </div>

              {signals.length ? <SignalRail signals={signals} onNavigate={(next) => goTab(next)} /> : null}

              <div key={tab} className="animate-in fade-in-0 duration-200 motion-reduce:animate-none">
                {tab === "overview" ? (
                  <UserOverviewTab appKey={appKey} userId={userId} user={user} wallet={walletQuery.data} onNavigate={goTab} />
                ) : tab === "profile" ? (
                  <UserProfileTab appKey={appKey} userId={userId} user={user} />
                ) : tab === "security" ? (
                  <UserSecurityTab appKey={appKey} userId={userId} user={user} />
                ) : tab === "activity" ? (
                  <UserActivityTab appKey={appKey} userId={userId} user={user} />
                ) : tab === "assets" ? (
                  <UserAssetsTab appKey={appKey} userId={userId} user={user} onNavigate={goTab} />
                ) : tab === "growth" ? (
                  <UserGrowthTab appKey={appKey} userId={userId} />
                ) : tab === "ledger" ? (
                  <UserLedgerTab appKey={appKey} userId={userId} user={user} onNavigate={goTab} />
                ) : tab === "records" ? (
                  <UserRecordsTab appKey={appKey} userId={userId} user={user} />
                ) : tab === "cloud" ? (
                  <UserCloudTab appKey={appKey} userId={userId} />
                ) : tab === "ad-policy" ? (
                  <UserAdPolicyTab appKey={appKey} userId={userId} />
                ) : tab === "risk" ? (
                  <UserRiskTab appKey={appKey} userId={userId} user={user} />
                ) : (
                  <UserGovernanceTab
                    appKey={appKey}
                    userId={userId}
                    user={user}
                    activeBan={activeBan}
                    onDelete={() => setShowDelete(true)}
                    deletePending={deleteMutation.isPending}
                    onUserDeleted={() => router.push(backHref)}
                  />
                )}
              </div>
            </div>
          </div>
        )}
      </div>

      <UserDeleteDialog
        open={showDelete}
        onOpenChange={setShowDelete}
        userName={title}
        onConfirm={handleDelete}
        isPending={deleteMutation.isPending}
      />
    </>
  );
}

// ── 身份卡 ──────────────────────────

function IdentityCard({
  user,
  appKey,
  walletBalance,
  activeBanScope,
  onNavigate,
  onDelete
}: {
  user: AdminAppUserDetail;
  appKey: string;
  walletBalance?: string;
  activeBanScope?: string;
  onNavigate: (tab: string, sub?: string) => void;
  onDelete: () => void;
}) {
  const enabled = user.enabled !== false;
  const vipLifetime = Boolean(user.vipLifetimeAt);
  const vipActive = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);
  const avatar = user.avatar || user.profile?.avatar || "";
  const name = textValue(user.nickname || user.profile?.nickname, textValue(user.account));
  const account = textValue(user.account, "");
  const restricted = Boolean(activeBanScope) || !enabled;

  function copyLink() {
    // 分享出去的链接不带 `from`：对方没有你的列表筛选态，返回时落到该应用的列表即可
    const url = new URL(window.location.href);
    url.searchParams.delete("from");
    void copyToClipboard(url.toString(), "页面链接");
  }

  const ids: Array<{ label: string; value: string }> = [
    ...(user.publicId ? [{ label: "对外 ID", value: String(user.publicId) }] : []),
    { label: "用户 ID", value: String(user.id) },
    ...(account ? [{ label: "账号", value: account }] : [])
  ];

  const balances: Array<{ label: string; value: string; icon: React.ComponentType<{ className?: string }>; tone: Tone; tab: string; sub: string }> = [
    { label: "积分", value: formatInt(user.integral), icon: Coins, tone: "warning", tab: "ledger", sub: "integral" },
    { label: "经验", value: formatInt(user.experience), icon: Sparkles, tone: "violet", tab: "ledger", sub: "experience" },
    { label: "余额", value: walletBalance !== undefined ? `¥${formatMoney(walletBalance)}` : "—", icon: Wallet, tone: "success", tab: "ledger", sub: "wallet" },
    {
      label: "会员",
      value: vipLifetime ? "永久" : vipActive ? formatRelative(user.vipExpireAt).replace("后", "") : user.vipExpireAt ? "已过期" : "无",
      icon: Crown,
      tone: vipLifetime || vipActive ? "primary" : "neutral",
      tab: "ledger",
      sub: "vip"
    }
  ];

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="flex items-start gap-3.5 p-4 xl:flex-col xl:items-center xl:gap-3 xl:px-5 xl:pt-6 xl:text-center">
        <Avatar className="size-14 shrink-0 rounded-2xl xl:size-20">
          <AvatarImage src={avatar} alt={name} className="object-cover" />
          <AvatarFallback className="rounded-2xl text-base font-medium xl:text-xl">
            {userInitials(user.nickname || user.profile?.nickname, user.account)}
          </AvatarFallback>
        </Avatar>
        <div className="flex min-w-0 flex-1 flex-col gap-1.5 xl:items-center">
          <h1 className="truncate text-lg font-semibold tracking-tight xl:max-w-full" title={name}>
            {name}
          </h1>
          <div className="flex flex-wrap items-center gap-1.5 xl:justify-center">
            {activeBanScope ? (
              <Badge variant="danger" size="sm">
                <Ban />
                封禁中（{BAN_SCOPE_LABEL[activeBanScope] ?? activeBanScope}）
              </Badge>
            ) : (
              <Badge variant={enabled ? "success" : "danger"} size="sm">
                {enabled ? "正常" : "已限制"}
              </Badge>
            )}
            {vipLifetime ? (
              <Badge variant="warning" size="sm">
                <Crown />
                永久会员
              </Badge>
            ) : vipActive ? (
              <Badge variant="warning" size="sm">
                <Crown />
                会员
              </Badge>
            ) : null}
          </div>
        </div>
      </div>

      <dl className="flex flex-col gap-1 border-t px-4 py-3 xl:px-5">
        {ids.map((item) => (
          <div key={item.label} className="flex min-w-0 items-center justify-between gap-3 text-xs">
            <dt className="shrink-0 text-muted-foreground">{item.label}</dt>
            <dd className="flex min-w-0 items-center gap-0.5">
              <span className="truncate font-mono text-foreground/90" title={item.value}>
                {item.value}
              </span>
              <CopyButton value={item.value} label={item.label} />
            </dd>
          </div>
        ))}
      </dl>

      <div className="grid grid-cols-4 border-t xl:grid-cols-2">
        {balances.map((item, index) => (
          <button
            key={item.label}
            type="button"
            onClick={() => onNavigate(item.tab, item.sub)}
            className={cn(
              "flex min-w-0 flex-col items-start gap-1 px-3 py-2.5 text-left transition-colors hover:bg-muted/50 xl:px-4",
              index > 0 && "border-l",
              "xl:[&:nth-child(3)]:border-l-0 xl:[&:nth-child(n+3)]:border-t"
            )}
          >
            <span className="flex items-center gap-1 text-xs text-muted-foreground">
              <item.icon className="size-3.5" />
              {item.label}
            </span>
            <span className="max-w-full truncate text-sm font-semibold tabular-nums">{item.value}</span>
          </button>
        ))}
      </div>

      <div className="grid grid-cols-[1fr_1fr_auto] gap-2 border-t p-3 xl:px-4">
        <Button variant="outline" size="sm" onClick={() => onNavigate("profile")}>
          <PencilLine />
          编辑资料
        </Button>
        <Button variant={restricted ? "default" : "outline"} size="sm" onClick={() => onNavigate("governance")}>
          <Gavel />
          {restricted ? "处置记录" : "封禁限制"}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon-sm" aria-label="更多操作">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            {user.publicId ? (
              <DropdownMenuItem onSelect={() => void copyToClipboard(String(user.publicId), "对外 ID")}>
                <Copy />
                复制对外 ID
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuItem onSelect={() => void copyToClipboard(String(user.id), "用户 ID")}>
              <Copy />
              复制用户 ID
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={copyLink}>
              <Link2 />
              复制页面链接
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onNavigate("ledger", "integral")}>
              <Receipt />
              查看积分流水
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link href={`/apps/${encodeURIComponent(appKey)}`}>
                <AppWindow />
                查看所属应用
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 />
              删除用户
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </Card>
  );
}

// ── 来源信息 ──────────────────────────

function SourceCard({ user, appKey, appName }: { user: AdminAppUserDetail; appKey: string; appName?: string }) {
  const profile = user.profile;
  const registerTime = user.registerTime || profile?.registerTime;
  const region = joinText(
    [user.registerProvince || profile?.registerProvince, user.registerCity || profile?.registerCity],
    textValue(user.registerIp || profile?.registerIp)
  );
  const rows: Array<{ label: string; value: React.ReactNode; hint?: string; title?: string }> = [
    { label: "注册时间", value: formatDateTime(registerTime), hint: registerTime ? formatRelative(registerTime) : undefined },
    { label: "最近更新", value: formatDateTime(user.updatedAt), hint: user.updatedAt ? formatRelative(user.updatedAt) : undefined },
    { label: "注册归属地", value: region, hint: textValue(user.registerIsp || profile?.registerIsp, "") || undefined, title: region }
  ];
  return (
    <Card className="hidden gap-0 py-0 xl:flex">
      <dl className="flex flex-col divide-y">
        {rows.map((row) => (
          <div key={row.label} className="flex flex-col gap-0.5 px-4 py-2.5">
            <dt className="text-xs text-muted-foreground">{row.label}</dt>
            <dd className="truncate text-sm tabular-nums" title={row.title}>
              {row.value}
            </dd>
            {row.hint ? <dd className="text-xs text-muted-foreground">{row.hint}</dd> : null}
          </div>
        ))}
        <div className="flex flex-col gap-0.5 px-4 py-2.5">
          <dt className="text-xs text-muted-foreground">所属应用</dt>
          <dd>
            <Link
              href={`/apps/${encodeURIComponent(appKey)}`}
              className="inline-flex max-w-full items-center gap-1 text-sm font-medium transition-colors hover:text-primary"
            >
              <span className="truncate">{appName || appKey}</span>
              <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
            </Link>
          </dd>
          <dd className="truncate font-mono text-xs text-muted-foreground" title={appKey}>
            {appKey}
          </dd>
        </div>
      </dl>
    </Card>
  );
}

// ── 导航 ──────────────────────────

function SideNav({
  tab,
  hasActiveBan,
  onSelect
}: {
  tab: string;
  hasActiveBan: boolean;
  onSelect: (tab: string) => void;
}) {
  return (
    <Card className="hidden gap-0 py-2 xl:flex">
      <nav aria-label="用户详情" className="flex flex-col gap-2">
        {TAB_GROUPS.map((group) => (
          <div key={group.label} className="flex flex-col gap-0.5 px-2">
            <div className="px-2 pt-1 pb-0.5 text-xs text-muted-foreground">{group.label}</div>
            {group.tabs.map(({ value, label, icon: Icon }) => {
              const active = tab === value;
              return (
                <button
                  key={value}
                  type="button"
                  aria-current={active ? "page" : undefined}
                  onClick={() => onSelect(value)}
                  className={cn(
                    "flex h-9 items-center gap-2.5 rounded-lg px-2.5 text-sm transition-colors",
                    active ? "bg-primary/10 font-medium text-primary" : "text-foreground/80 hover:bg-muted hover:text-foreground"
                  )}
                >
                  <Icon className="size-4 shrink-0" />
                  <span className="flex-1 truncate text-left">{label}</span>
                  {value === "governance" && hasActiveBan ? (
                    <span className="size-1.5 rounded-full bg-red-500" aria-label="有生效中的封禁" />
                  ) : null}
                  {active ? <ChevronRight className="size-3.5 opacity-60" /> : null}
                </button>
              );
            })}
          </div>
        ))}
      </nav>
    </Card>
  );
}

/** 窄屏页签条：吸顶、横向滑动，当前项自动滚到中间。 */
function MobileTabBar({
  tab,
  hasActiveBan,
  onSelect
}: {
  tab: string;
  hasActiveBan: boolean;
  onSelect: (tab: string) => void;
}) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const scroller = scrollerRef.current;
    const active = scroller?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!scroller || !active || scroller.scrollWidth <= scroller.clientWidth) return;
    const left = active.offsetLeft - (scroller.clientWidth - active.offsetWidth) / 2;
    scroller.scrollTo({ left: Math.max(left, 0), behavior: "smooth" });
  }, [tab]);

  return (
    <div
      className={cn(
        "sticky top-14 z-20 -mx-4 border-b bg-background px-4 lg:-mx-6 lg:px-6 xl:hidden",
        "dark:bg-[color-mix(in_srgb,var(--card)_35%,var(--sidebar))]"
      )}
    >
      <div ref={scrollerRef} className="-mx-1 flex overflow-x-auto px-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        {TABS.map(({ value, label, icon: Icon }) => {
          const active = tab === value;
          return (
            <button
              key={value}
              type="button"
              aria-current={active ? "page" : undefined}
              onClick={() => onSelect(value)}
              className={cn(
                "relative flex h-11 shrink-0 items-center gap-1.5 px-3 text-sm transition-colors",
                active ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <Icon className="size-4" />
              {label}
              {value === "governance" && hasActiveBan ? <span className="size-1.5 rounded-full bg-red-500" /> : null}
              {active ? <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-primary" /> : null}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ── 信号带 ──────────────────────────

function SignalRail({ signals, onNavigate }: { signals: UserSignal[]; onNavigate: (tab: string) => void }) {
  return (
    <div className={cn("grid gap-2", signals.length > 1 && "lg:grid-cols-2")}>
      {signals.map((signal) => {
        const danger = signal.tone === "danger";
        const body = (
          <>
            <span
              className={cn(
                "flex size-8 shrink-0 items-center justify-center rounded-[10px]",
                danger ? "bg-red-500/10 text-red-600 dark:text-red-400" : "bg-amber-500/10 text-amber-600 dark:text-amber-400"
              )}
            >
              {signal.icon}
            </span>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="text-sm font-medium">{signal.title}</span>
              {signal.detail ? <span className="text-xs leading-5 text-muted-foreground">{signal.detail}</span> : null}
            </span>
            {signal.tab ? (
              <span className="flex shrink-0 items-center gap-0.5 text-xs font-medium text-muted-foreground">
                <span className="hidden sm:inline">{signal.tabLabel ?? "处理"}</span>
                <ChevronRight className="size-4" />
              </span>
            ) : null}
          </>
        );
        const className = cn(
          "flex w-full items-center gap-3 rounded-xl border bg-card px-3.5 py-3 text-left transition-colors",
          danger ? "border-red-200 dark:border-red-900/60" : "border-amber-200 dark:border-amber-900/60",
          signal.tab && "hover:bg-muted/40"
        );
        return signal.tab ? (
          <button key={signal.id} type="button" className={className} onClick={() => onNavigate(signal.tab as string)}>
            {body}
          </button>
        ) : (
          <div key={signal.id} className={className}>
            {body}
          </div>
        );
      })}
    </div>
  );
}

// ── 骨架 ──────────────────────────

function DetailSkeleton() {
  return (
    <div className="grid gap-5 xl:grid-cols-[296px_minmax(0,1fr)]">
      <div className="flex flex-col gap-4">
        <Card className="gap-0 py-0">
          <div className="flex items-center gap-3.5 p-4 xl:flex-col xl:pt-6">
            <Skeleton className="size-14 rounded-2xl xl:size-20" />
            <div className="flex flex-col gap-2 xl:items-center">
              <Skeleton className="h-6 w-36" />
              <Skeleton className="h-4 w-20" />
            </div>
          </div>
          <div className="flex flex-col gap-2 border-t p-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-4 w-full" />
            ))}
          </div>
          <div className="grid grid-cols-4 gap-2 border-t p-3 xl:grid-cols-2">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-10" />
            ))}
          </div>
        </Card>
        <Skeleton className="hidden h-80 rounded-xl xl:block" />
      </div>
      <div className="flex flex-col gap-5">
        <Skeleton className="h-8 w-40" />
        <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-[104px] rounded-xl" />
          ))}
        </div>
        <div className="grid gap-5 xl:grid-cols-2">
          <Skeleton className="h-72 rounded-xl" />
          <Skeleton className="h-72 rounded-xl" />
        </div>
      </div>
    </div>
  );
}
