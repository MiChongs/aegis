"use client";

import { useCallback, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useReducedMotion } from "motion/react";
import { useTheme } from "next-themes";
import { ArrowUpRight, CloudSun, LayoutGrid, LogIn, Moon, ShieldCheck, Sun, Sunrise, Sunset, Timer, UserPlus, Users } from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { avatarUrl, useAuthStore } from "@/lib/auth-store";
import { HeroClock } from "./hero-clock";
import { formatWith } from "@/lib/datetime";
import { useIsSuperAdmin } from "@/lib/permissions";
import { useIsClient } from "@/lib/use-client-value";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { formatCount, Sparkline, useCountUp } from "./overview-kit";

type Period = "dawn" | "morning" | "noon" | "afternoon" | "evening" | "night";

function getPeriod(hour: number): Period {
  if (hour >= 5 && hour < 8) return "dawn";
  if (hour >= 8 && hour < 11) return "morning";
  if (hour >= 11 && hour < 13) return "noon";
  if (hour >= 13 && hour < 17) return "afternoon";
  if (hour >= 17 && hour < 19) return "evening";
  return "night";
}

const GREETINGS: Record<Period, string> = {
  dawn: "早上好",
  morning: "上午好",
  noon: "中午好",
  afternoon: "下午好",
  evening: "傍晚好",
  night: "晚上好"
};

const PERIOD_ICONS: Record<Period, React.ComponentType<{ className?: string }>> = {
  dawn: Sunrise,
  morning: Sun,
  noon: Sun,
  afternoon: CloudSun,
  evening: Sunset,
  night: Moon
};

/** 对齐到整秒的时钟，避免秒数跳动不均。 */
function useClock() {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    let interval: ReturnType<typeof setInterval> | undefined;
    const align = setTimeout(() => {
      setNow(new Date());
      interval = setInterval(() => setNow(new Date()), 1000);
    }, 1000 - (Date.now() % 1000));
    return () => {
      clearTimeout(align);
      clearInterval(interval);
    };
  }, []);

  return now;
}

/*
 * 本次在线时长。起点存在 sessionStorage：刷新保留，新标签页重新计时；
 * 记录里带着 token，重新登录后自动重置。
 */
const SESSION_KEY = "aegis_session_start";

function readSessionStart(token: string) {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    const stored = raw ? (JSON.parse(raw) as { t: number; k: string }) : null;
    if (stored && stored.k === token) return stored.t;
    const startedAt = Date.now();
    sessionStorage.setItem(SESSION_KEY, JSON.stringify({ t: startedAt, k: token }));
    return startedAt;
  } catch {
    return Date.now();
  }
}

function useOnlineDuration(token: string | null, now: Date) {
  const [start, setStart] = useState<{ token: string; at: number } | null>(null);

  useEffect(() => {
    if (!token) return;
    const at = readSessionStart(token);
    // 外部存储只能在挂载后读取，读一次即可
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setStart({ token, at });
  }, [token]);

  if (!token || !start || start.token !== token) return null;
  const minutes = Math.max(0, Math.floor((now.getTime() - start.at) / 60_000));
  const hours = Math.floor(minutes / 60);
  return hours > 0 ? `${hours} 小时 ${minutes % 60} 分钟` : `${minutes} 分钟`;
}

// three.js 只在浏览器里加载，也不进首屏主包
const HeroGlobe = dynamic(() => import("./hero-globe").then((mod) => mod.HeroGlobe), { ssr: false });
// 彩蛋：只有触发时才加载
const CosmosJourney = dynamic(() => import("./cosmos/cosmos-journey"), { ssr: false });

/**
 * 概览首屏：问候、浏览器时区时钟、实时昼夜的点阵地球与四项平台指标。
 *
 * 时钟下方的 24 格时间轴按浏览器时区标出当前小时，地球上高亮的经度带也是本机时区，
 * 让「控制台全部按本机时区显示」这件事一眼可见。
 * 平台指标只有超管拿得到（接口对其他角色返回 0），其他角色不显示指标条。
 */
export function OverviewHero() {
  const operator = useAuthStore((s) => s.operator);
  const accessToken = useAuthStore((s) => s.accessToken);
  const isSuperAdmin = useIsSuperAdmin();
  const dashboard = useAdminDashboardQuery();
  const stats = dashboard.data?.stats;
  const trend = dashboard.data?.trend ?? [];
  const isClient = useIsClient();
  const now = useClock();
  const onlineDuration = useOnlineDuration(accessToken, now);
  const reducedMotion = Boolean(useReducedMotion());
  const { resolvedTheme } = useTheme();
  const [globeReady, setGlobeReady] = useState(false);
  // 时钟每秒重渲染首屏；回调保持稳定，地球组件（memo）才不会跟着重渲染
  const handleGlobeReady = useCallback(() => setGlobeReady(true), []);
  const [cosmosOpen, setCosmosOpen] = useState(false);
  const openCosmos = useCallback(() => setCosmosOpen(true), []);
  const closeCosmos = useCallback(() => setCosmosOpen(false), []);

  const hour = now.getHours();
  const period = getPeriod(hour);
  const PeriodIcon = PERIOD_ICONS[period];
  const displayName = operator?.displayName || operator?.account || "管理员";
  const roleLabel = operator?.isSuperAdmin ? "超级管理员" : operator?.role || operator?.assignments?.[0]?.roleKey || "管理员";
  const avatarSrc = avatarUrl(operator?.avatar, operator?.avatarVersion);

  const today = trend.at(-1);
  const week = trend.slice(-7);
  const weekRegistrations = week.reduce((sum, day) => sum + day.registrations, 0);
  const metrics: MetricProps[] = [
    { icon: LayoutGrid, label: "应用", value: stats?.totalApps, href: "/apps" },
    {
      icon: Users,
      label: "用户总数",
      value: stats?.totalUsers,
      href: "/app-users",
      hint: trend.length > 0 ? `近 7 天新增 ${formatCount(weekRegistrations)}` : undefined
    },
    {
      icon: LogIn,
      label: "今日登录",
      value: today?.logins,
      href: "/reports?tab=channels",
      spark: trend.map((day) => day.logins),
      tone: "text-blue-600 dark:text-blue-400"
    },
    {
      icon: UserPlus,
      label: "今日新增",
      value: today?.registrations,
      href: "/reports?tab=users",
      spark: trend.map((day) => day.registrations),
      tone: "text-emerald-600 dark:text-emerald-400"
    }
  ];

  return (
    <Card aria-label="平台概览" className="relative gap-0 overflow-hidden py-0">
      <div className="grid md:min-h-[320px] md:grid-cols-[minmax(0,1fr)_minmax(0,44%)]">
        <div className="relative z-10 flex flex-col justify-between gap-6 p-4 sm:p-5 md:gap-8 md:p-7">
          <div className="flex min-w-0 items-start gap-3 sm:gap-4">
            <Avatar className="size-11 shrink-0 rounded-xl ring-1 ring-border sm:size-12" preview={false}>
              {avatarSrc ? <AvatarImage src={avatarSrc} alt={displayName} className="rounded-xl object-cover" /> : null}
              <AvatarFallback className="rounded-xl bg-primary/10 text-base font-semibold text-primary">
                {Array.from(displayName)[0]?.toUpperCase()}
              </AvatarFallback>
            </Avatar>
            <div className="min-w-0 flex-1 space-y-2">
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <PeriodIcon className="size-3.5 shrink-0" />
                {isClient ? formatWith(now, { year: "numeric", month: "long", day: "numeric", weekday: "long" }) : <Skeleton className="h-3.5 w-36" />}
              </p>
              <h1 className="truncate text-xl font-semibold tracking-tight sm:text-2xl md:text-[28px] md:leading-9">
                {isClient ? GREETINGS[period] : "你好"}，{displayName}
              </h1>
              <div className="flex flex-wrap items-center gap-1.5">
                <Badge variant="outline" className="gap-1 bg-muted/40 text-muted-foreground">
                  <ShieldCheck className="size-3" />
                  {roleLabel}
                </Badge>
                {onlineDuration ? (
                  <Badge variant="outline" className="gap-1 bg-muted/40 text-muted-foreground">
                    <Timer className="size-3" />
                    本次在线 {onlineDuration}
                  </Badge>
                ) : null}
              </div>
            </div>
          </div>

          {isClient ? (
            <HeroClock now={now} animate={!reducedMotion} />
          ) : (
            <div className="space-y-4">
              <Skeleton className="h-12 w-60 md:h-[60px]" />
              <Skeleton className="h-6 w-full max-w-[22rem]" />
              <Skeleton className="h-4 w-48" />
            </div>
          )}
        </div>

        {/* 双指捏合留给地球的彩蛋手势，纵向滚动照常 */}
        <div className="relative h-[220px] touch-pan-x touch-pan-y border-t sm:h-[260px] md:h-auto md:border-t-0">
          {/* 贴图到位前的占位：与地球同位置同大小的圆 */}
          <div
            aria-hidden
            className={cn(
              "pointer-events-none absolute top-1/2 left-1/2 aspect-square h-[78%] max-w-[92%] -translate-x-1/2 -translate-y-[44%] rounded-full bg-muted transition-opacity duration-700",
              globeReady ? "opacity-0" : "animate-pulse opacity-60"
            )}
          />
          {isClient ? (
            <HeroGlobe
              theme={resolvedTheme === "light" ? "light" : "dark"}
              reducedMotion={reducedMotion}
              onReady={handleGlobeReady}
              onCosmos={openCosmos}
              className="absolute inset-0"
            />
          ) : null}
        </div>
      </div>

      {isSuperAdmin ? (
        <div className="relative z-10 grid grid-cols-2 border-t bg-card lg:grid-cols-4">
          {metrics.map((metric, index) => (
            <MetricCell
              key={metric.label}
              {...metric}
              loading={dashboard.isLoading}
              reducedMotion={reducedMotion}
              className={cn(
                index % 2 === 1 && "border-l",
                index >= 2 && "border-t lg:border-t-0",
                index === 2 && "lg:border-l"
              )}
            />
          ))}
        </div>
      ) : null}
      {cosmosOpen ? <CosmosJourney zoneLongitude={(-new Date().getTimezoneOffset() / 60) * 15 * (Math.PI / 180)} onClose={closeCosmos} /> : null}
    </Card>
  );
}

type MetricProps = {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value?: number;
  href: string;
  hint?: string;
  /** 近 14 天的逐日数值，画在数值右侧 */
  spark?: number[];
  /** 迷你折线的颜色 */
  tone?: string;
};

function MetricCell({
  icon: Icon,
  label,
  value,
  href,
  hint,
  spark,
  tone,
  loading,
  reducedMotion,
  className
}: MetricProps & {
  loading: boolean;
  reducedMotion: boolean;
  className?: string;
}) {
  const shown = useCountUp(value, reducedMotion);
  return (
    <Link
      href={href}
      className={cn(
        "group/metric flex min-w-0 flex-col gap-2 px-4 py-3.5 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none sm:px-5 sm:py-4 md:px-7",
        className
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5 shrink-0" />
        <span className="truncate">{label}</span>
        <ArrowUpRight className="ml-auto size-3.5 shrink-0 -translate-x-1 opacity-0 transition-[opacity,translate] group-hover/metric:translate-x-0 group-hover/metric:opacity-70" />
      </span>
      <span className="flex min-w-0 items-end justify-between gap-3">
        {loading ? (
          <Skeleton className="h-7 w-16" />
        ) : (
          <span className="truncate font-data text-xl leading-7 font-semibold tabular-nums sm:text-2xl">{typeof shown === "number" ? formatCount(shown) : "—"}</span>
        )}
        {spark && spark.length > 1 && !loading ? <Sparkline values={spark} className={cn("h-6 w-14 shrink-0 sm:h-7 sm:w-24", tone)} /> : null}
      </span>
      {hint ? <span className="truncate text-[11px] text-muted-foreground">{hint}</span> : null}
    </Link>
  );
}
