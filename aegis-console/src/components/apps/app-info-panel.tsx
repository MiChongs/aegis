"use client";

import { useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import Link from "next/link";
import {
  Activity,
  ChevronRight,
  Copy,
  Fingerprint,
  Info,
  LogIn,
  Power,
  RotateCcw,
  Save,
  UserPlus,
  Users2,
  type LucideIcon
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { formatAppDate, formatCount, useCopyAppKey } from "@/components/apps/app-shared";
import { useAdminAppQuery, useAdminAppStatsQuery, useUpdateAdminAppMutation } from "@/lib/admin-hooks";
import { appSectionHref, findAppSection } from "@/lib/app-sections";
import { cn } from "@/lib/utils";
import type { AppDetail, AppStats } from "@/lib/api/types";

/**
 * 应用概览：先回答「这个应用多大、今天活不活」，再给三个开关和标识。
 *
 * 三个开关各自是一块磁贴，关闭说明长在磁贴里而不是另起一段：说明是发给终端用户的文案，
 * 跟着「哪个开关关了」出现，开着时不留输入框，免得让人以为它还在生效。
 *
 * 宽屏两栏（左：运行概况与开关，右：标识与常用入口）；窄屏单栏，
 * 有未保存的修改时底部浮出保存条，不用滚回卡片底下找按钮。
 */

type Form = {
  name: string;
  status: boolean;
  registerStatus: boolean;
  loginStatus: boolean;
  disabledReason: string;
  disabledRegisterReason: string;
  disabledLoginReason: string;
};

function seed(app?: AppDetail | null): Form {
  return {
    name: app?.name ?? "",
    status: app?.status ?? true,
    registerStatus: app?.registerStatus ?? true,
    loginStatus: app?.loginStatus ?? true,
    disabledReason: app?.disabledReason ?? "",
    disabledRegisterReason: app?.disabledRegisterReason ?? "",
    disabledLoginReason: app?.disabledLoginReason ?? ""
  };
}

/** 概览里的常用入口：按配置频率挑的几个区块 */
const SHORTCUTS = ["stats", "policy", "oauth", "auth-protocol", "payment", "vip"];

export function AppInfoPanel({ appKey }: { appKey?: string | null }) {
  const query = useAdminAppQuery(appKey);
  const statsQuery = useAdminAppStatsQuery(appKey);
  const mutation = useUpdateAdminAppMutation(appKey);
  const app = query.data;

  // 草稿按 appKey 绑定作用域：切换应用时上一个应用的未保存改动不会串过来（不用 effect 同步）
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const scope = appKey ?? "";
  const server = seed(app);
  const form = draft?.scope === scope ? draft.value : server;
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const dirty = (Object.keys(server) as (keyof Form)[]).some((key) => form[key] !== server[key]);

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error("应用名称不能为空");
      return;
    }
    try {
      await mutation.mutateAsync({
        name: form.name.trim(),
        status: form.status,
        registerStatus: form.registerStatus,
        loginStatus: form.loginStatus,
        disabledReason: form.disabledReason,
        disabledRegisterReason: form.disabledRegisterReason,
        disabledLoginReason: form.disabledLoginReason
      });
      setDraft(null); // 清掉草稿，重新从服务端值派生
      toast.success("已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (!appKey) return <NoAppSelected icon={<Info className="size-8" />} />;
  if (query.isLoading) {
    return (
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="space-y-4">
          <Skeleton className="h-40 w-full rounded-2xl" />
          <Skeleton className="h-72 w-full rounded-2xl" />
        </div>
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }
  if (!app) return <div className="py-12 text-center text-sm text-muted-foreground">应用不存在</div>;

  const saveActions = (
    <>
      {dirty && (
        <Button size="sm" variant="ghost" className="h-9 gap-1 text-xs sm:h-8" onClick={() => setDraft(null)}>
          <RotateCcw className="size-3" />
          重置
        </Button>
      )}
      <Button
        size="sm"
        className="h-9 gap-1 text-xs sm:h-8"
        disabled={mutation.isPending || !dirty}
        onClick={handleSave}
      >
        <Save className="size-3" />
        {mutation.isPending ? "保存中..." : "保存配置"}
      </Button>
    </>
  );

  return (
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <div className="min-w-0 space-y-4">
        <StatsBoard stats={statsQuery.data} loading={statsQuery.isLoading} />

        <Card
          icon={Power}
          title="名称与开关"
          aside={
            <span className={cn("text-[11px]", dirty ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
              {dirty ? "有未保存的修改" : "与服务端一致"}
            </span>
          }
          footer={
            <footer className="hidden items-center justify-end gap-2 border-t border-border bg-muted/30 px-5 py-3 sm:flex">
              {saveActions}
            </footer>
          }
        >
          <div className="space-y-4">
            <label className="block space-y-1.5">
              <span className="text-[11px] font-medium text-muted-foreground">应用名称</span>
              <Input
                className="h-10 text-base sm:h-9 sm:text-sm"
                value={form.name}
                placeholder="我的应用"
                onChange={(event) => patch("name", event.target.value)}
              />
            </label>

            <div className="grid gap-2.5 md:grid-cols-3">
              <AccessTile
                icon={Power}
                title="应用启用"
                onHint="客户端可正常调用全部接口"
                offHint="所有接口拒绝服务"
                reasonLabel="停用说明"
                checked={form.status}
                reason={form.disabledReason}
                onToggle={(value) => patch("status", value)}
                onReason={(value) => patch("disabledReason", value)}
              />
              <AccessTile
                icon={UserPlus}
                title="开放注册"
                onHint="新用户可以注册账号"
                offHint="注册入口关闭"
                reasonLabel="注册关闭说明"
                checked={form.registerStatus}
                reason={form.disabledRegisterReason}
                onToggle={(value) => patch("registerStatus", value)}
                onReason={(value) => patch("disabledRegisterReason", value)}
              />
              <AccessTile
                icon={LogIn}
                title="开放登录"
                onHint="已有用户可以登录"
                offHint="登录入口关闭"
                reasonLabel="登录关闭说明"
                checked={form.loginStatus}
                reason={form.disabledLoginReason}
                onToggle={(value) => patch("loginStatus", value)}
                onReason={(value) => patch("disabledLoginReason", value)}
              />
            </div>
          </div>
        </Card>
      </div>

      <div className="min-w-0 space-y-4">
        <IdentityCard app={app} />
        <ShortcutCard appKey={app.appKey} />
      </div>

      {dirty && <MobileSaveBar>{saveActions}</MobileSaveBar>}
    </div>
  );
}

/* ── 卡片外壳 ── */

function Card({
  icon: Icon,
  title,
  aside,
  footer,
  children,
  bodyClassName
}: {
  icon: LucideIcon;
  title: string;
  aside?: React.ReactNode;
  footer?: React.ReactNode;
  children: React.ReactNode;
  bodyClassName?: string;
}) {
  return (
    <section
      className="min-w-0 overflow-hidden rounded-2xl border border-border bg-card"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <header className="flex items-center justify-between gap-2 border-b border-border px-4 py-3 sm:px-5">
        <div className="flex min-w-0 items-center gap-2">
          <Icon className="size-4 shrink-0 text-muted-foreground" />
          <h3 className="truncate text-sm font-semibold tracking-tight">{title}</h3>
        </div>
        {aside}
      </header>
      <div className={cn("p-4 sm:p-5", bodyClassName)}>{children}</div>
      {footer}
    </section>
  );
}

/* ── 运行概况 ── */

function StatsBoard({ stats, loading }: { stats?: AppStats; loading: boolean }) {
  const total = stats?.totalUsers ?? 0;
  const enabledShare = total > 0 ? Math.round(((stats?.enabledUsers ?? 0) / total) * 1000) / 10 : null;
  const attempts = (stats?.loginSuccessToday ?? 0) + (stats?.loginFailureToday ?? 0);
  const failRate = attempts > 0 ? Math.round(((stats?.loginFailureToday ?? 0) / attempts) * 1000) / 10 : null;

  return (
    <Card icon={Activity} title="运行概况" bodyClassName="p-0 sm:p-0">
      <div className="grid min-w-0 md:grid-cols-2">
        {/* 用户规模 */}
        <div className="min-w-0 space-y-3 p-4 sm:p-5">
          <div className="flex items-end justify-between gap-3">
            <div className="min-w-0">
              <div className="text-[11px] text-muted-foreground">总用户</div>
              {loading ? (
                <Skeleton className="mt-1 h-8 w-24 rounded" />
              ) : (
                <div className="truncate text-3xl font-semibold tracking-tight tabular-nums">{formatCount(stats?.totalUsers)}</div>
              )}
            </div>
            <Users2 className="mb-1 size-5 shrink-0 text-muted-foreground/50" />
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-muted" title={enabledShare === null ? undefined : `启用占比 ${enabledShare}%`}>
            <div
              className="h-full rounded-full bg-emerald-500 transition-[width] duration-500"
              style={{ width: `${enabledShare ?? 0}%` }}
            />
          </div>
          <div className="grid grid-cols-3 gap-2">
            <MiniStat label="启用" value={stats?.enabledUsers} loading={loading} dot="bg-emerald-500" />
            <MiniStat label="停用" value={stats?.disabledUsers} loading={loading} dot="bg-zinc-400" />
            <MiniStat label="近 30 日新增" value={stats?.newUsersLast30Days} loading={loading} />
          </div>
        </div>

        {/* 今日动态 */}
        <div className="min-w-0 border-t border-border p-4 sm:p-5 md:border-t-0 md:border-l">
          <div className="mb-3 flex items-center justify-between gap-2">
            <span className="text-[11px] text-muted-foreground">今日</span>
            {failRate !== null && (
              <span
                className={cn(
                  "rounded-full px-2 py-0.5 text-[10px] font-medium tabular-nums",
                  failRate >= 20 ? "bg-red-500/10 text-red-600 dark:text-red-400" : "bg-muted text-muted-foreground"
                )}
              >
                登录失败率 {failRate}%
              </span>
            )}
          </div>
          <div className="grid grid-cols-3 gap-2">
            <BigStat label="新增" value={stats?.newUsersToday} loading={loading} />
            <BigStat label="登录成功" value={stats?.loginSuccessToday} loading={loading} />
            <BigStat
              label="登录失败"
              value={stats?.loginFailureToday}
              loading={loading}
              warn={(stats?.loginFailureToday ?? 0) > 0}
            />
          </div>
          <div className="mt-3 flex items-center justify-between gap-2 rounded-xl bg-muted/50 px-3 py-2 text-[11px]">
            <span className="text-muted-foreground">近 7 日新增</span>
            {loading ? (
              <Skeleton className="h-4 w-10 rounded" />
            ) : (
              <span className="font-semibold tabular-nums">{formatCount(stats?.newUsersLast7Days)}</span>
            )}
          </div>
        </div>
      </div>
    </Card>
  );
}

function MiniStat({ label, value, loading, dot }: { label: string; value?: number | null; loading?: boolean; dot?: string }) {
  return (
    <div className="min-w-0">
      <div className="flex items-center gap-1 truncate text-[10px] text-muted-foreground">
        {dot && <span className={cn("size-1.5 shrink-0 rounded-full", dot)} />}
        <span className="truncate">{label}</span>
      </div>
      {loading ? (
        <Skeleton className="mt-1 h-4 w-10 rounded" />
      ) : (
        <div className="truncate text-sm font-semibold tabular-nums">{formatCount(value)}</div>
      )}
    </div>
  );
}

function BigStat({ label, value, loading, warn }: { label: string; value?: number | null; loading?: boolean; warn?: boolean }) {
  return (
    <div className="min-w-0 rounded-xl border border-border px-2.5 py-2 sm:px-3">
      <div className="truncate text-[10px] text-muted-foreground">{label}</div>
      {loading ? (
        <Skeleton className="mt-1 h-6 w-10 rounded" />
      ) : (
        <div className={cn("truncate text-lg font-semibold tabular-nums sm:text-xl", warn && "text-amber-600 dark:text-amber-400")}>
          {formatCount(value)}
        </div>
      )}
    </div>
  );
}

/* ── 开关磁贴 ── */

function AccessTile({
  icon: Icon,
  title,
  onHint,
  offHint,
  reasonLabel,
  checked,
  reason,
  onToggle,
  onReason
}: {
  icon: LucideIcon;
  title: string;
  onHint: string;
  offHint: string;
  reasonLabel: string;
  checked: boolean;
  reason: string;
  onToggle: (value: boolean) => void;
  onReason: (value: string) => void;
}) {
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col rounded-xl border p-3 transition-colors duration-200",
        checked
          ? "border-emerald-500/30 bg-emerald-500/[0.06]"
          : "border-dashed border-amber-500/50 bg-amber-500/[0.05]"
      )}
    >
      <label className="flex cursor-pointer items-center gap-2.5">
        <span
          className={cn(
            "grid size-8 shrink-0 place-items-center rounded-lg",
            checked ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" : "bg-amber-500/15 text-amber-600 dark:text-amber-400"
          )}
        >
          <Icon className="size-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-xs font-semibold">{title}</span>
          <span className="block truncate text-[11px] text-muted-foreground">{checked ? onHint : offHint}</span>
        </span>
        <Switch checked={checked} onCheckedChange={onToggle} />
      </label>
      {!checked && (
        <div className="mt-3 space-y-1">
          <span className="text-[10px] font-medium text-muted-foreground">{reasonLabel}（展示给用户）</span>
          <Input
            className="h-9 bg-background text-base sm:h-8 sm:text-xs"
            placeholder="例如：系统升级中，预计 12:00 恢复"
            value={reason}
            onChange={(event) => onReason(event.target.value)}
          />
        </div>
      )}
    </div>
  );
}

/* ── 标识 ── */

function IdentityCard({ app }: { app: AppDetail }) {
  const copy = useCopyAppKey();
  return (
    <Card icon={Fingerprint} title="应用标识">
      <dl className="space-y-3.5">
        <div>
          <dt className="text-[10px] text-muted-foreground">AppKey</dt>
          <dd className="mt-1 flex items-start gap-2">
            {/* 完整显示并允许任意处换行：截断后的 key 复制给别人也没用 */}
            <code className="min-w-0 flex-1 rounded-lg bg-muted/60 px-2.5 py-1.5 font-mono text-xs leading-relaxed break-all">
              {app.appKey}
            </code>
            <button
              type="button"
              aria-label="复制 AppKey"
              onClick={() => copy(app.appKey)}
              className="grid size-8 shrink-0 place-items-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            >
              <Copy className="size-3.5" />
            </button>
          </dd>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <InfoCell label="应用 ID" value={String(app.id)} mono />
          <InfoCell label="创建时间" value={formatAppDate(app.createdAt)} />
          <InfoCell label="更新时间" value={formatAppDate(app.updatedAt)} />
        </div>
      </dl>
    </Card>
  );
}

function InfoCell({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-[10px] text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 truncate text-xs font-medium", mono && "font-mono")}>{value}</dd>
    </div>
  );
}

/* ── 常用入口 ── */

function ShortcutCard({ appKey }: { appKey: string }) {
  return (
    <Card icon={ChevronRight} title="常用配置" bodyClassName="p-1.5 sm:p-1.5">
      <div className="grid grid-cols-2 gap-1 xl:grid-cols-1">
        {SHORTCUTS.map((key) => {
          const meta = findAppSection(key);
          if (!meta) return null;
          const Icon = meta.icon;
          return (
            <Link
              key={key}
              href={appSectionHref(appKey, key)}
              scroll={false}
              className="group flex min-w-0 items-center gap-2.5 rounded-xl px-2.5 py-2 transition-colors hover:bg-accent active:bg-accent"
            >
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:text-foreground">
                <Icon className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-xs font-medium">{meta.title}</span>
                <span className="block truncate text-[10px] text-muted-foreground max-sm:hidden">{meta.summary}</span>
              </span>
              <ChevronRight className="size-3.5 shrink-0 text-muted-foreground/60 max-xl:hidden" />
            </Link>
          );
        })}
      </div>
    </Card>
  );
}

/* ── 窄屏保存条 ── */

const noopSubscribe = () => () => {};

/**
 * 挂到 body 上而不是留在原地：路由过渡容器带着 `will-change: transform`，
 * 会把里面的 `position: fixed` 变成相对它定位，保存条就跟着内容一起滚走了。
 */
function MobileSaveBar({ children }: { children: React.ReactNode }) {
  const mounted = useSyncExternalStore(noopSubscribe, () => true, () => false);
  if (!mounted) return null;
  return createPortal(
    <div className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-background/95 px-4 pt-2.5 pb-[max(0.625rem,env(safe-area-inset-bottom))] backdrop-blur-md sm:hidden">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-amber-600 dark:text-amber-400">有未保存的修改</span>
        <div className="flex items-center gap-2">{children}</div>
      </div>
    </div>,
    document.body
  );
}
