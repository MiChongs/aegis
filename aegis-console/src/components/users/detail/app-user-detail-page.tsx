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
  Check,
  ChevronRight,
  Cloud,
  Copy,
  Crown,
  Gavel,
  Layers,
  Link2,
  MoreHorizontal,
  PencilLine,
  ShieldCheck,
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
import { Card, CardContent } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ApiError } from "@/lib/api-client";
import {
  useAdminAppUserQuery,
  useAdminAppsQuery,
  useDeleteAdminAppUserMutation
} from "@/lib/admin-hooks";
import { useAdminUserActiveBanQuery, useAdminUserWalletQuery } from "@/lib/app-user-hooks";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { UserDeleteDialog } from "../user-delete-dialog";
import { UserActivityTab } from "./user-activity-tab";
import { UserAssetsTab } from "./user-assets-tab";
import { UserCloudTab } from "./user-cloud-tab";
import {
  BAN_SCOPE_LABEL,
  deriveUserSignals,
  formatDate,
  formatTime,
  isPast,
  joinText,
  relativeTime,
  textValue,
  userInitials,
  type SignalTone,
  type UserSignal
} from "./user-detail-shared";
import { UserGovernanceTab } from "./user-governance-tab";
import { UserOverviewTab } from "./user-overview-tab";
import { UserProfileTab } from "./user-profile-tab";
import { UserSecurityTab } from "./user-security-tab";

type Props = {
  appKey: string;
  userId: number;
  fromHref?: string;
};

const TABS = [
  { value: "overview", label: "概览", icon: Layers },
  { value: "profile", label: "资料", icon: UserRound },
  { value: "security", label: "安全", icon: ShieldCheck },
  { value: "assets", label: "资产", icon: Wallet },
  { value: "cloud", label: "云存储", icon: Cloud },
  { value: "activity", label: "活动", icon: Activity },
  { value: "governance", label: "处置", icon: Gavel }
];

const TAB_VALUES = new Set(TABS.map((item) => item.value));

/** 顶栏高度（console-topbar 的 h-14），页签条吸附在它下面 */
const TOPBAR_HEIGHT = 56;

async function copyText(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(`已复制${label}`);
    return true;
  } catch {
    toast.error("复制失败");
    return false;
  }
}

/**
 * 应用用户详情。
 *
 * 自上而下回答三个递进的问题：
 *
 *   身份卡   这是谁（账号、ID、状态、来源），以及最常用的两个入口：编辑资料、处置
 *   信号带   现在有什么问题（从散落字段里推导出来的结论，整行可点进去处理）
 *   页签区   我要做的那件事（七个互不重叠的工作面）
 *
 * 页签条吸附在顶栏下方，长页面滚到底也能直接换页签；换页签时若已滚过页签条，
 * 视口回到页签条位置，避免停在新内容的半中间。移动端页签横向滑动，当前项自动居中。
 *
 * 页签同步到 `?tab=`，深链能直接落到「处置」页。`from` 参数原样保留，
 * 返回时回到来源列表的筛选态与页码。
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
  const tabAnchorRef = useRef<HTMLDivElement>(null);

  const user = userQuery.data as AdminAppUserDetail | undefined;
  const activeBan = activeBanQuery.data ?? null;
  const app = useMemo(
    () => appsQuery.data?.find((item) => item.appKey === appKey) ?? null,
    [appKey, appsQuery.data]
  );

  const rawTab = searchParams.get("tab");
  const tab = rawTab && TAB_VALUES.has(rawTab) ? rawTab : "overview";
  const backHref = fromHref || `/app-users?app=${encodeURIComponent(appKey)}`;

  const signals = useMemo(() => deriveUserSignals(user, activeBan), [user, activeBan]);

  function goTab(next: string) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("tab", next);
    router.replace(`?${params.toString()}`, { scroll: false });

    const anchor = tabAnchorRef.current;
    if (!anchor) return;
    const top = anchor.getBoundingClientRect().top;
    // 只在已经滚过页签条时回拉；页签条还在视口里就别动，免得页面无故跳一下
    if (top < TOPBAR_HEIGHT) {
      window.scrollTo({ top: window.scrollY + top - TOPBAR_HEIGHT, behavior: "smooth" });
    }
  }

  const title = textValue(
    user?.nickname || user?.profile?.nickname,
    textValue(user?.account, `用户 #${userId}`)
  );

  async function handleDelete() {
    try {
      await deleteMutation.mutateAsync(userId);
      toast.success("用户已删除");
      router.push(backHref);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  return (
    <>
      <div className="page-stack">
        <div className="flex min-w-0 items-center gap-1.5">
          <Button asChild variant="ghost" size="sm" className="-ml-2 h-9 shrink-0 text-muted-foreground hover:text-foreground sm:h-8">
            <Link href={backHref}>
              <ArrowLeft className="size-4" />
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
          <Card>
            <CardContent className="flex min-h-64 flex-col items-center justify-center gap-2 px-6 py-14 text-center">
              <div className="flex size-11 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
                <UserRound className="size-5" />
              </div>
              <div className="mt-1 text-base font-semibold">未找到该用户</div>
              <p className="max-w-sm text-sm leading-6 text-muted-foreground">
                应用 {app?.name || appKey} 中不存在用户 #{userId}，可能已被删除。
              </p>
              <Button asChild size="sm" variant="outline" className="mt-2">
                <Link href={backHref}>返回列表</Link>
              </Button>
            </CardContent>
          </Card>
        ) : (
          <>
            <IdentityCard
              user={user}
              appKey={appKey}
              appName={app?.name}
              activeBanScope={activeBan?.banScope}
              onNavigate={goTab}
              onDelete={() => setShowDelete(true)}
            />

            {signals.length ? <SignalRail signals={signals} onNavigate={goTab} /> : null}

            <Tabs value={tab} onValueChange={goTab} className="gap-0">
              <div ref={tabAnchorRef} aria-hidden />
              <TabBar tab={tab} hasActiveBan={Boolean(activeBan)} />

              <div className="pt-4 sm:pt-5">
                <TabsContent value="overview">
                  <UserOverviewTab user={user} wallet={walletQuery.data} onNavigate={goTab} />
                </TabsContent>
                <TabsContent value="profile">
                  <UserProfileTab appKey={appKey} userId={userId} user={user} />
                </TabsContent>
                <TabsContent value="security">
                  <UserSecurityTab appKey={appKey} userId={userId} user={user} />
                </TabsContent>
                <TabsContent value="assets">
                  <UserAssetsTab appKey={appKey} userId={userId} user={user} />
                </TabsContent>
                <TabsContent value="cloud">
                  <UserCloudTab appKey={appKey} userId={userId} />
                </TabsContent>
                <TabsContent value="activity">
                  <UserActivityTab appKey={appKey} userId={userId} user={user} />
                </TabsContent>
                <TabsContent value="governance">
                  <UserGovernanceTab
                    appKey={appKey}
                    userId={userId}
                    user={user}
                    activeBan={activeBan}
                    onDelete={() => setShowDelete(true)}
                    deletePending={deleteMutation.isPending}
                  />
                </TabsContent>
              </div>
            </Tabs>
          </>
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
  appName,
  activeBanScope,
  onNavigate,
  onDelete
}: {
  user: AdminAppUserDetail;
  appKey: string;
  appName?: string;
  activeBanScope?: string;
  onNavigate: (tab: string) => void;
  onDelete: () => void;
}) {
  const enabled = user.enabled !== false;
  const vipActive = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);
  const avatar = user.avatar || user.profile?.avatar || "";
  const name = textValue(user.nickname || user.profile?.nickname, textValue(user.account));
  const account = textValue(user.account, "");
  const region = joinText(
    [user.registerProvince || user.profile?.registerProvince, user.registerCity || user.profile?.registerCity],
    textValue(user.registerIp || user.profile?.registerIp)
  );
  const registerTime = user.registerTime || user.profile?.registerTime;

  function copyLink() {
    // 分享出去的链接不带 `from`：对方没有你的列表筛选态，返回时落到该应用的列表即可
    const url = new URL(window.location.href);
    url.searchParams.delete("from");
    void copyText(url.toString(), "页面链接");
  }

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <CardContent className="flex flex-col gap-4 p-4 sm:flex-row sm:items-start sm:justify-between sm:p-5">
        <div className="flex min-w-0 items-center gap-3.5 sm:gap-4">
          <Avatar className="size-14 shrink-0 rounded-2xl sm:size-16">
            <AvatarImage src={avatar} alt={name} />
            <AvatarFallback className="rounded-2xl text-base font-medium">
              {userInitials(user.nickname || user.profile?.nickname, user.account)}
            </AvatarFallback>
          </Avatar>

          <div className="min-w-0 space-y-1.5">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <h1 className="min-w-0 truncate text-lg font-semibold tracking-tight sm:text-xl" title={name}>
                {name}
              </h1>
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
              {vipActive ? (
                <Badge variant="warning" size="sm" title={`到期 ${formatTime(user.vipExpireAt)}`}>
                  <Crown />
                  会员至 {formatDate(user.vipExpireAt)}
                </Badge>
              ) : null}
            </div>

            <div className="-ml-1.5 flex flex-wrap items-center gap-x-1 gap-y-0.5 text-xs text-muted-foreground">
              <CopyChip label="ID" value={String(user.id)} />
              {account && account !== name ? <CopyChip label="账号" value={account} /> : null}
              {account && account === name ? <CopyChip label="账号" value={account} hideValue /> : null}
            </div>
          </div>
        </div>

        <div className="grid shrink-0 grid-cols-[1fr_1fr_auto] gap-2 sm:flex sm:items-center">
          <Button variant="outline" size="sm" className="h-10 sm:h-8" onClick={() => onNavigate("profile")}>
            <PencilLine />
            编辑资料
          </Button>
          <Button
            variant={activeBanScope || !enabled ? "default" : "outline"}
            size="sm"
            className="h-10 sm:h-8"
            onClick={() => onNavigate("governance")}
          >
            <Gavel />
            {activeBanScope || !enabled ? "处置记录" : "封禁或限制"}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon-sm" className="size-10 sm:size-8" aria-label="更多操作">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem onSelect={() => void copyText(String(user.id), "用户 ID")}>
                <Copy />
                复制用户 ID
              </DropdownMenuItem>
              {account ? (
                <DropdownMenuItem onSelect={() => void copyText(account, "账号")}>
                  <UserRound />
                  复制账号
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuItem onSelect={copyLink}>
                <Link2 />
                复制页面链接
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
      </CardContent>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-t bg-muted/30 px-4 py-3.5 sm:grid-cols-4 sm:px-5">
        <HeaderStat label="注册时间" value={formatDate(registerTime)} hint={relativeTime(registerTime)} title={formatTime(registerTime)} />
        <HeaderStat label="最近更新" value={formatDate(user.updatedAt)} hint={relativeTime(user.updatedAt)} title={formatTime(user.updatedAt)} />
        <HeaderStat label="注册归属地" value={region} hint={textValue(user.registerIsp || user.profile?.registerIsp, "")} />
        <div className="min-w-0">
          <dt className="text-[11px] text-muted-foreground">所属应用</dt>
          <dd className="mt-0.5 min-w-0">
            <Link
              href={`/apps/${encodeURIComponent(appKey)}`}
              className="inline-flex max-w-full items-center gap-1 text-sm font-medium hover:underline hover:underline-offset-2"
            >
              <span className="truncate">{appName || appKey}</span>
              <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
            </Link>
            <div className="truncate font-mono text-[11px] text-muted-foreground" title={appKey}>
              {appKey}
            </div>
          </dd>
        </div>
      </dl>
    </Card>
  );
}

function HeaderStat({
  label,
  value,
  hint,
  title
}: {
  label: string;
  value: string;
  hint?: string;
  title?: string;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 min-w-0">
        <div className="truncate text-sm font-medium tabular-nums" title={title ?? value}>
          {value}
        </div>
        {hint && hint !== "—" ? (
          <div className="truncate text-[11px] text-muted-foreground">{hint}</div>
        ) : null}
      </dd>
    </div>
  );
}

/** 点一下就复制。账号与 ID 是排查时最常被粘进别处的两个值。 */
function CopyChip({ label, value, hideValue }: { label: string; value: string; hideValue?: boolean }) {
  const [copied, setCopied] = useState(false);
  if (!value) return null;

  async function copy() {
    if (await copyText(value, label)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    }
  }

  return (
    <button
      type="button"
      onClick={copy}
      title={`复制${label}`}
      className="group inline-flex h-7 max-w-full items-center gap-1 rounded-md px-1.5 transition-colors hover:bg-accent hover:text-foreground sm:h-6"
    >
      <span>{label}</span>
      {hideValue ? null : <span className="truncate font-mono text-foreground/80">{value}</span>}
      {copied ? (
        <Check className="size-3 shrink-0 text-emerald-500" />
      ) : (
        <Copy className="size-3 shrink-0 opacity-50 transition-opacity group-hover:opacity-100 sm:opacity-0" />
      )}
    </button>
  );
}

// ── 信号带 ──────────────────────────

const TONE_STYLES: Record<SignalTone, string> = {
  danger:
    "border-red-200 bg-red-50 text-red-900 hover:bg-red-100/70 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-100 dark:hover:bg-red-950/60",
  warning:
    "border-amber-200 bg-amber-50 text-amber-900 hover:bg-amber-100/70 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-100 dark:hover:bg-amber-950/60"
};

const TONE_ICON: Record<SignalTone, string> = {
  danger: "bg-red-100 text-red-600 dark:bg-red-900/50 dark:text-red-300",
  warning: "bg-amber-100 text-amber-600 dark:bg-amber-900/50 dark:text-amber-300"
};

function SignalRail({
  signals,
  onNavigate
}: {
  signals: UserSignal[];
  onNavigate: (tab: string) => void;
}) {
  return (
    <div className={cn("grid gap-2", signals.length > 1 && "lg:grid-cols-2")}>
      {signals.map((signal) => {
        const body = (
          <>
            <span
              className={cn(
                "flex size-8 shrink-0 items-center justify-center rounded-lg",
                TONE_ICON[signal.tone]
              )}
            >
              {signal.icon}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-medium">{signal.title}</span>
              {signal.detail ? (
                <span className="mt-0.5 block text-xs leading-5 opacity-80">{signal.detail}</span>
              ) : null}
            </span>
            {signal.tab ? (
              <span className="flex shrink-0 items-center gap-0.5 text-xs font-medium opacity-80">
                <span className="hidden sm:inline">{signal.tabLabel ?? "处理"}</span>
                <ChevronRight className="size-4" />
              </span>
            ) : null}
          </>
        );
        const className = cn(
          "flex w-full items-center gap-3 rounded-2xl border px-3.5 py-3 text-left transition-colors",
          TONE_STYLES[signal.tone]
        );
        return signal.tab ? (
          <button
            key={signal.id}
            type="button"
            className={className}
            onClick={() => onNavigate(signal.tab as string)}
          >
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

// ── 页签条 ──────────────────────────

function TabBar({ tab, hasActiveBan }: { tab: string; hasActiveBan: boolean }) {
  const scrollerRef = useRef<HTMLDivElement>(null);

  // 窄屏下页签横向滑动：当前页签滚到可视区中央，深链进来时也能看见自己在哪
  useEffect(() => {
    const scroller = scrollerRef.current;
    const active = scroller?.querySelector<HTMLElement>('[data-state="active"]');
    if (!scroller || !active || scroller.scrollWidth <= scroller.clientWidth) return;
    const left = active.offsetLeft - (scroller.clientWidth - active.offsetWidth) / 2;
    scroller.scrollTo({ left: Math.max(left, 0), behavior: "smooth" });
  }, [tab]);

  return (
    <div
      className={cn(
        "sticky top-14 z-20 -mx-4 border-b bg-background px-4 lg:-mx-6 lg:px-6",
        // 深色下内容区是 sidebar 底色叠 35% card，吸顶条用同样的实色混合，避免露出色差
        "dark:bg-[color-mix(in_srgb,var(--card)_35%,var(--sidebar))]"
      )}
    >
      <div
        ref={scrollerRef}
        className="-mx-1 overflow-x-auto px-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        <TabsList variant="line" className="h-11 w-max min-w-full justify-start gap-0.5 p-0 sm:gap-1">
          {TABS.map(({ value, label, icon: Icon }) => (
            <TabsTrigger
              key={value}
              value={value}
              className="h-full flex-none px-3 after:!bottom-[-1px] hover:bg-transparent sm:px-3.5"
            >
              <Icon className="size-4" />
              {label}
              {value === "governance" && hasActiveBan ? (
                <span className="size-1.5 rounded-full bg-red-500" aria-label="有生效中的封禁" />
              ) : null}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
    </div>
  );
}

// ── 骨架 ──────────────────────────

function DetailSkeleton() {
  return (
    <div className="space-y-5">
      <Card className="gap-0 overflow-hidden py-0">
        <CardContent className="flex flex-col gap-4 p-4 sm:flex-row sm:items-center sm:justify-between sm:p-5">
          <div className="flex items-center gap-3.5 sm:gap-4">
            <Skeleton className="size-14 rounded-2xl sm:size-16" />
            <div className="space-y-2">
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-4 w-56" />
            </div>
          </div>
          <div className="grid grid-cols-[1fr_1fr_auto] gap-2 sm:flex">
            <Skeleton className="h-10 rounded-md sm:h-8 sm:w-24" />
            <Skeleton className="h-10 rounded-md sm:h-8 sm:w-28" />
            <Skeleton className="size-10 rounded-md sm:size-8" />
          </div>
        </CardContent>
        <div className="grid grid-cols-2 gap-x-4 gap-y-3 border-t bg-muted/30 px-4 py-3.5 sm:grid-cols-4 sm:px-5">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="space-y-1.5">
              <Skeleton className="h-3 w-14" />
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-3 w-12" />
            </div>
          ))}
        </div>
      </Card>
      <div className="-mx-4 flex h-11 items-center gap-2 overflow-hidden border-b px-4 lg:-mx-6 lg:px-6">
        {TABS.map((item) => (
          <Skeleton key={item.value} className="h-5 w-14 shrink-0 rounded-md" />
        ))}
      </div>
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-[86px] rounded-2xl" />
        ))}
      </div>
      <div className="grid gap-5 xl:grid-cols-2">
        <Skeleton className="h-72 rounded-xl" />
        <Skeleton className="h-72 rounded-xl" />
      </div>
    </div>
  );
}
