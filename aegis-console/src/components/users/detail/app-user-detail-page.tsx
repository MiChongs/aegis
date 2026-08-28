"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Activity,
  ArrowLeft,
  ArrowUpRight,
  Ban,
  Check,
  ChevronRight,
  Copy,
  Crown,
  Gavel,
  IdCard,
  Layers,
  Loader2,
  ShieldCheck,
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
import { SectionHeading } from "@/components/ui/section-heading";
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
import {
  BAN_SCOPE_LABEL,
  deriveUserSignals,
  formatTime,
  isPast,
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
  { value: "activity", label: "活动", icon: Activity },
  { value: "governance", label: "处置", icon: Gavel }
];

const TAB_VALUES = new Set(TABS.map((item) => item.value));

/**
 * 应用用户详情。
 *
 * 三层结构，自上而下回答三个递进的问题：
 *
 *   身份栏   这是谁（不变的事实：账号、ID、所属应用、当前状态）
 *   信号带   现在有什么问题（从散落字段里推导出来的结论，可点进去处理）
 *   页签区   我要做的那件事（六个互不重叠的工作面）
 *
 * 页签同步到 `?tab=`，与控制台其余页面一致 —— 深链能直接落到「处置」页，
 * 而不是每次都从概览点两下。`from` 参数原样保留，返回时回到来源列表的筛选态。
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
        <div className="space-y-4">
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink href="/app-users">应用用户</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbLink href={backHref}>{app?.name || "用户列表"}</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>{title}</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>

          <SectionHeading
            eyebrow="应用用户"
            title={title}
            action={
              <Button asChild variant="outline" size="sm">
                <Link href={backHref}>
                  <ArrowLeft className="size-3.5" />
                  返回列表
                </Link>
              </Button>
            }
          />
        </div>

        {userQuery.isLoading ? (
          <DetailSkeleton />
        ) : !user ? (
          <Card>
            <CardContent className="flex min-h-64 flex-col items-center justify-center gap-3 py-16">
              <span className="text-sm text-muted-foreground">
                未找到用户 #{userId}（应用 {appKey}）
              </span>
              <Button asChild size="sm" variant="outline">
                <Link href={backHref}>返回列表</Link>
              </Button>
            </CardContent>
          </Card>
        ) : (
          <>
            <IdentityHeader
              user={user}
              appKey={appKey}
              appName={app?.name}
              activeBanScope={activeBan?.banScope}
            />

            {signals.length ? <SignalRail signals={signals} onNavigate={goTab} /> : null}

            <Tabs value={tab} onValueChange={goTab} className="space-y-5">
              <TabsList className="w-full flex-wrap justify-start">
                {TABS.map(({ value, label, icon: Icon }) => (
                  <TabsTrigger key={value} value={value}>
                    <Icon className="size-3.5" />
                    {label}
                    {value === "governance" && activeBan ? (
                      <span className="ml-1 size-1.5 rounded-full bg-red-500" />
                    ) : null}
                  </TabsTrigger>
                ))}
              </TabsList>

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

// ── 身份栏 ──────────────────────────

function IdentityHeader({
  user,
  appKey,
  appName,
  activeBanScope
}: {
  user: AdminAppUserDetail;
  appKey: string;
  appName?: string;
  activeBanScope?: string;
}) {
  const enabled = user.enabled !== false;
  const vipActive = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);
  const avatar = user.avatar || user.profile?.avatar || "";

  return (
    <Card className="overflow-hidden py-0">
      <CardContent className="flex flex-col gap-5 px-5 py-5 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 items-center gap-4">
          <Avatar className="size-14 rounded-2xl">
            <AvatarImage src={avatar} />
            <AvatarFallback className="rounded-2xl text-base">
              {userInitials(user.nickname || user.profile?.nickname, user.account)}
            </AvatarFallback>
          </Avatar>

          <div className="min-w-0 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="truncate text-lg font-semibold">
                {textValue(user.nickname || user.profile?.nickname, textValue(user.account))}
              </h2>
              {activeBanScope ? (
                <Badge variant="danger" size="sm">
                  <Ban className="size-3" />
                  封禁中 · {BAN_SCOPE_LABEL[activeBanScope] ?? activeBanScope}
                </Badge>
              ) : (
                <Badge variant={enabled ? "success" : "danger"} size="sm">
                  {enabled ? "正常" : "已限制"}
                </Badge>
              )}
              {vipActive ? (
                <Badge variant="warning" size="sm">
                  <Crown className="size-3" />
                  会员 · {relativeTime(user.vipExpireAt)}
                </Badge>
              ) : null}
            </div>

            <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 text-xs text-muted-foreground">
              <CopyChip icon={<IdCard className="size-3" />} label="ID" value={String(user.id)} />
              <CopyChip icon={<UserRound className="size-3" />} label="账号" value={textValue(user.account, "")} />
              <CopyChip label="AppKey" value={appKey} />
              {appName ? (
                <Link
                  href={`/apps/${encodeURIComponent(appKey)}`}
                  className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-foreground"
                >
                  {appName}
                  <ArrowUpRight className="size-3" />
                </Link>
              ) : null}
            </div>
          </div>
        </div>

        <div className="grid shrink-0 grid-cols-2 gap-x-8 gap-y-2 text-xs sm:grid-cols-3">
          <HeaderStat label="注册" value={formatTime(user.registerTime)} hint={relativeTime(user.registerTime)} />
          <HeaderStat label="最近更新" value={formatTime(user.updatedAt)} hint={relativeTime(user.updatedAt)} />
          <HeaderStat
            label="归属地"
            value={
              textValue(
                [user.registerProvince, user.registerCity].filter(Boolean).join(" "),
                textValue(user.registerIP)
              )
            }
            hint={textValue(user.registerIsp, "")}
          />
        </div>
      </CardContent>
    </Card>
  );
}

function HeaderStat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="truncate text-xs tabular-nums text-foreground" title={value}>
        {value}
      </div>
      {hint && hint !== "—" ? (
        <div className="truncate text-[11px] text-muted-foreground">{hint}</div>
      ) : null}
    </div>
  );
}

/** 点一下就复制。账号与 ID 是排查时最常被粘进别处的两个值。 */
function CopyChip({
  icon,
  label,
  value
}: {
  icon?: React.ReactNode;
  label: string;
  value: string;
}) {
  const [copied, setCopied] = useState(false);
  if (!value) return null;

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      toast.error("复制失败，请手动选中");
    }
  }

  return (
    <button
      type="button"
      onClick={copy}
      title={`复制${label}`}
      className="group inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-foreground"
    >
      {icon}
      <span>{label}</span>
      <span className="font-mono text-foreground/80">{value}</span>
      {copied ? (
        <Check className="size-3 text-emerald-500" />
      ) : (
        <Copy className="size-3 opacity-0 transition-opacity group-hover:opacity-60" />
      )}
    </button>
  );
}

// ── 信号带 ──────────────────────────

const TONE_STYLES: Record<SignalTone, string> = {
  danger:
    "border-red-200 bg-red-50/70 text-red-900 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-100",
  warning:
    "border-amber-200 bg-amber-50/70 text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-100"
};

const TONE_ICON: Record<SignalTone, string> = {
  danger: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400"
};

function SignalRail({
  signals,
  onNavigate
}: {
  signals: UserSignal[];
  onNavigate: (tab: string) => void;
}) {
  return (
    <div className="space-y-2">
      {signals.map((signal) => (
        <div
          key={signal.id}
          className={cn(
            "flex items-start gap-3 rounded-2xl border px-4 py-3",
            TONE_STYLES[signal.tone]
          )}
        >
          <span className={cn("mt-0.5 shrink-0", TONE_ICON[signal.tone])}>{signal.icon}</span>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">{signal.title}</div>
            <div className="mt-0.5 text-xs leading-5 opacity-80">{signal.detail}</div>
          </div>
          {signal.tab ? (
            <Button
              size="sm"
              variant="ghost"
              className="h-7 shrink-0 gap-0.5 px-2 text-xs hover:bg-background/60"
              onClick={() => onNavigate(signal.tab as string)}
            >
              {signal.tabLabel ?? "处理"}
              <ChevronRight className="size-3" />
            </Button>
          ) : null}
        </div>
      ))}
    </div>
  );
}

// ── 骨架 ──────────────────────────

function DetailSkeleton() {
  return (
    <div className="space-y-5">
      <Card className="py-0">
        <CardContent className="flex items-center gap-4 px-5 py-5">
          <Skeleton className="size-14 rounded-2xl" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-3 w-72" />
          </div>
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
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
