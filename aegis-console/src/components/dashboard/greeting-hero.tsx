"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { GrainGradient } from "@paper-design/shaders-react";
import { useReducedMotion } from "motion/react";
import {
  Activity,
  ArrowUpRight,
  CloudSun,
  LayoutGrid,
  LogIn,
  Moon,
  ShieldCheck,
  Sun,
  Sunrise,
  Sunset,
  Timer,
  Users,
} from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { useBranding } from "@/lib/branding-provider";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  时段                                                                */
/* ------------------------------------------------------------------ */

type Period = "dawn" | "morning" | "noon" | "afternoon" | "evening" | "night";

function getPeriod(h: number): Period {
  if (h >= 5  && h < 8)  return "dawn";
  if (h >= 8  && h < 11) return "morning";
  if (h >= 11 && h < 13) return "noon";
  if (h >= 13 && h < 17) return "afternoon";
  if (h >= 17 && h < 19) return "evening";
  return "night";
}

const greetings: Record<Period, string> = {
  dawn:      "早安",
  morning:   "上午好",
  noon:      "中午好",
  afternoon: "下午好",
  evening:   "傍晚好",
  night:     "晚上好",
};

/** 时段图标 —— 一律使用 lucide 线性图标，不使用 Emoji */
const periodIcons: Record<Period, React.ComponentType<{ className?: string }>> = {
  dawn:      Sunrise,
  morning:   Sun,
  noon:      Sun,
  afternoon: CloudSun,
  evening:   Sunset,
  night:     Moon,
};

/* ------------------------------------------------------------------ */
/*  实时时钟                                                            */
/* ------------------------------------------------------------------ */

function useClock() {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const msToNext = 1000 - Date.now() % 1000;
    let interval: ReturnType<typeof setInterval>;
    const align = setTimeout(() => {
      setNow(new Date());
      interval = setInterval(() => setNow(new Date()), 1000);
    }, msToNext);
    return () => { clearTimeout(align); clearInterval(interval); };
  }, []);

  return now;
}

/* ------------------------------------------------------------------ */
/*  本次在线时长                                                        */
/*  sessionStorage：刷新保留，新标签页/关闭重开自动重置                     */
/*  token 比对：重新登录后自动重置                                        */
/* ------------------------------------------------------------------ */

const SESSION_KEY = "aegis_session_start";

function useOnlineDuration(token: string | null) {
  const [display, setDisplay] = useState<string | null>(null);

  useEffect(() => {
    // 无 token 时直接不订阅；「清空展示」由下方 return 处派生，避免在 effect 里同步 setState
    if (!token) return;

    // 读取或初始化起始时间
    let startedAt: number;
    try {
      const raw = sessionStorage.getItem(SESSION_KEY);
      if (raw) {
        const stored = JSON.parse(raw) as { t: number; k: string };
        if (stored.k === token) {
          // 同一登录会话，沿用起始时间
          startedAt = stored.t;
        } else {
          // token 变了（重新登录），重置
          startedAt = Date.now();
          sessionStorage.setItem(SESSION_KEY, JSON.stringify({ t: startedAt, k: token }));
        }
      } else {
        // 新标签页 / 首次访问
        startedAt = Date.now();
        sessionStorage.setItem(SESSION_KEY, JSON.stringify({ t: startedAt, k: token }));
      }
    } catch {
      startedAt = Date.now();
    }

    const tick = () => {
      const elapsed = Date.now() - startedAt;
      const totalMin = Math.floor(elapsed / 60_000);
      const h = Math.floor(totalMin / 60);
      const m = totalMin % 60;
      setDisplay(h > 0 ? `${h} 小时 ${m} 分钟` : `${m} 分钟`);
    };
    tick();
    const id = setInterval(tick, 60_000);
    return () => clearInterval(id);
  }, [token]);

  return token ? display : null;
}

/* ------------------------------------------------------------------ */
/*  数字格式化                                                          */
/* ------------------------------------------------------------------ */

function fmtNum(n: number): string {
  if (n >= 10000) return `${(n / 10000).toFixed(1)}w`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

/* ------------------------------------------------------------------ */
/*  首屏主视觉                                                          */
/*                                                                     */
/*  深色 GrainGradient 着色器背景（@paper-design/shaders-react），        */
/*  两套主题共用同一深色画面，文字恒为白色，无需分主题调色。               */
/*  prefers-reduced-motion 下动画速度归零，画面退化为静态渐变。           */
/* ------------------------------------------------------------------ */

const HERO_COLORS = ["#7300ff", "#eba8ff", "#00bfff", "#2b00ff"];

export function GreetingHero() {
  const operator = useAuthStore((s) => s.operator);
  const accessToken = useAuthStore((s) => s.accessToken);
  const branding = useBranding();
  const dashQuery = useAdminDashboardQuery();
  const stats = dashQuery.data?.stats;
  const now = useClock();
  const onlineDuration = useOnlineDuration(accessToken);
  const reducedMotion = useReducedMotion();

  const period = getPeriod(now.getHours());
  const PeriodIcon = periodIcons[period];
  const displayName = operator?.displayName || operator?.account || "管理员";
  const roleLabel = operator?.isSuperAdmin
    ? "超级管理员"
    : operator?.role || operator?.assignments?.[0]?.roleKey || "管理员";

  const dateStr = now.toLocaleDateString("zh-CN", {
    year: "numeric",
    month: "long",
    day: "numeric",
    weekday: "short",
  });

  const timeStr = now.toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });

  const metrics = [
    { icon: LayoutGrid, label: "应用", value: stats?.totalApps, href: "/apps" },
    { icon: Users, label: "用户", value: stats?.totalUsers, href: "/app-users" },
    { icon: LogIn, label: "今日登录", value: stats?.todayLogins, href: "/reports?tab=login" },
    { icon: Activity, label: "活跃会话", value: stats?.activeSessions, href: "/users?tab=online" },
  ];

  return (
    <section
      aria-label="平台概览"
      className="relative overflow-hidden rounded-xl border border-black/40 bg-[#050505] text-white dark:border-white/10"
    >
      {/* 着色器背景：canvas 初始化前由容器底色兜底，文字始终可读 */}
      <GrainGradient
        aria-hidden
        className="absolute inset-0"
        width="100%"
        height="100%"
        colors={HERO_COLORS}
        colorBack="#000000"
        softness={0.5}
        intensity={0.5}
        noise={0.25}
        shape="corners"
        speed={reducedMotion ? 0 : 1}
      />
      {/* 可读性压暗层：非装饰，保证白字在亮色色带上的对比度 */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/55 via-black/15 to-black/25"
      />

      <div className="relative flex min-h-[320px] flex-col gap-6 p-6 md:min-h-[360px] md:p-8">
        {/* ── 问候 + 时钟 ── */}
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0 space-y-2.5">
            <div className="flex items-center gap-2 text-xs text-white/60">
              <PeriodIcon className="size-3.5" />
              <span>{dateStr}</span>
            </div>
            <h1 className="truncate text-2xl font-semibold tracking-tight md:text-3xl">
              {greetings[period]}，{displayName}
            </h1>
            <p className="max-w-2xl text-sm leading-relaxed text-white/70">
              欢迎使用 {branding.platformName} {branding.consoleName}
              ，统一管理应用接入、用户账户、权限与安全策略。
            </p>
            <div className="flex flex-wrap items-center gap-1.5 pt-1">
              <span className="inline-flex items-center gap-1 rounded-md border border-white/15 bg-white/10 px-2 py-0.5 text-[11px] font-medium text-white/85">
                <ShieldCheck className="size-3" />
                {roleLabel}
              </span>
              {onlineDuration ? (
                <span className="inline-flex items-center gap-1 rounded-md border border-white/15 bg-white/10 px-2 py-0.5 text-[11px] text-white/85">
                  <Timer className="size-3" />
                  本次在线 {onlineDuration}
                </span>
              ) : null}
            </div>
          </div>

          <div className="shrink-0 sm:text-right">
            <p className="font-data text-4xl leading-none font-semibold tabular-nums tracking-tight md:text-5xl">
              {timeStr}
            </p>
          </div>
        </div>

        {/* ── 平台指标 ── */}
        <div className="mt-auto grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-white/15 bg-white/15 lg:grid-cols-4">
          {metrics.map((m) => (
            <MetricSlot
              key={m.label}
              icon={m.icon}
              label={m.label}
              href={m.href}
              value={typeof m.value === "number" ? fmtNum(m.value) : null}
              loading={dashQuery.isLoading}
            />
          ))}
        </div>
      </div>
    </section>
  );
}

/* ------------------------------------------------------------------ */
/*  指标格子 —— 深色玻璃底，整格可点击跳转到对应明细页                     */
/*  刻意不用 backdrop-blur：着色器画布每帧都在变，背景模糊会成倍放大合成开销 */
/* ------------------------------------------------------------------ */

function MetricSlot({ icon: Icon, label, value, href, loading }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string | null;
  href: string;
  loading: boolean;
}) {
  return (
    <Link
      href={href}
      className={cn(
        "group/metric relative flex flex-col gap-1.5 bg-black/45 px-4 py-3 transition-colors",
        "hover:bg-black/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-white/50"
      )}
    >
      <span className="flex items-center gap-1.5 text-[11px] font-medium tracking-wide text-white/65">
        <Icon className="size-3.5" />
        {label}
        <ArrowUpRight className="ml-auto size-3 opacity-0 transition-opacity group-hover/metric:opacity-70" />
      </span>
      {loading ? (
        <Skeleton className="h-7 w-12 bg-white/15" />
      ) : (
        <span className="font-data text-2xl leading-none font-semibold tabular-nums text-white">
          {value ?? "--"}
        </span>
      )}
    </Link>
  );
}
