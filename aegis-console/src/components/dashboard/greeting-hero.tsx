"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  Activity,
  ArrowUpRight,
  CloudSun,
  LayoutGrid,
  LogIn,
  Moon,
  Sun,
  Sunrise,
  Sunset,
  Timer,
  Users,
} from "lucide-react";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { Card, CardContent } from "@/components/ui/card";
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

/**
 * 时段光晕配色。
 * 只在 hero 内部作为极低透明度的径向渐变出现，深浅两套主题共用同一组色相，
 * 靠 `opacity` 分档，避免浅色模式被染色。
 */
const periodGlow: Record<Period, string> = {
  dawn:      "#f59e0b",
  morning:   "#38bdf8",
  noon:      "#0ea5e9",
  afternoon: "#6366f1",
  evening:   "#f97316",
  night:     "#8b5cf6",
};

/* ------------------------------------------------------------------ */
/*  短语                                                                */
/* ------------------------------------------------------------------ */

const mottos: Record<Period, string[]> = {
  dawn: [
    "清晨的代码总是格外清醒",
    "新的一天，新的版本号",
    "日出而作，bug 无所遁形",
  ],
  morning: [
    "上午效率最高，值得好好利用",
    "系统运行平稳，适合推进工作",
    "把最难的任务留给精力最好的时候",
  ],
  noon: [
    "午间小憩，下午更有精神",
    "记得补充能量再继续",
    "中场休息也是生产力的一部分",
  ],
  afternoon: [
    "下午茶时间到了，别忘了喝水",
    "保持节奏，离下班又近了一步",
    "专注模式开启中",
  ],
  evening: [
    "今天辛苦了，收尾后好好休息",
    "傍晚适合回顾，不适合大改",
    "明天的你会感谢现在收工的你",
  ],
  night: [
    "深夜值班辛苦了，注意身体",
    "夜猫子模式，记得明天补觉",
    "安静的夜晚适合思考架构",
  ],
};

function pickMotto(period: Period): string {
  const pool = mottos[period];
  const seed = new Date().toDateString();
  let hash = 0;
  for (let i = 0; i < seed.length; i++) hash = ((hash << 5) - hash + seed.charCodeAt(i)) | 0;
  return pool[Math.abs(hash) % pool.length];
}

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
/*  组件                                                                */
/* ------------------------------------------------------------------ */

export function GreetingHero() {
  const operator = useAuthStore((s) => s.operator);
  const accessToken = useAuthStore((s) => s.accessToken);
  const dashQuery = useAdminDashboardQuery();
  const stats = dashQuery.data?.stats;
  const now = useClock();
  const onlineDuration = useOnlineDuration(accessToken);

  const period = getPeriod(now.getHours());
  const PeriodIcon = periodIcons[period];
  const displayName = operator?.displayName || operator?.account || "管理员";
  const motto = useMemo(() => pickMotto(period), [period]);
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
    { icon: Users, label: "用户", value: stats?.totalUsers, href: "/users?tab=app-users" },
    { icon: LogIn, label: "今日登录", value: stats?.todayLogins, href: "/reports?tab=login" },
    { icon: Activity, label: "活跃会话", value: stats?.activeSessions, href: "/users?tab=online" },
  ];

  return (
    <Card className="relative h-full overflow-hidden py-0">
      {/* 时段光晕：右上角单点径向渐变，浅色模式压到 6% 以免染脏白底 */}
      <div
        aria-hidden
        className="pointer-events-none absolute -top-24 -right-16 size-72 rounded-full opacity-[0.06] blur-3xl dark:opacity-[0.14]"
        style={{ background: periodGlow[period] }}
      />
      {/* 细网格底纹，给纯色卡面一点材质 */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 opacity-[0.035] dark:opacity-[0.05]"
        style={{
          backgroundImage:
            "linear-gradient(to right, currentColor 1px, transparent 1px), linear-gradient(to bottom, currentColor 1px, transparent 1px)",
          backgroundSize: "28px 28px",
          maskImage: "radial-gradient(120% 90% at 85% 0%, #000 0%, transparent 70%)",
        }}
      />

      <CardContent className="relative flex h-full flex-col gap-5 p-6">
        {/* ── 问候 + 时钟 ── */}
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0 space-y-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <PeriodIcon className="size-3.5" />
              <span>{dateStr}</span>
            </div>
            <h1 className="truncate text-2xl font-semibold tracking-tight text-foreground">
              {greetings[period]}，{displayName}
            </h1>
            <p className="text-sm text-muted-foreground">{motto}</p>
            <div className="flex flex-wrap items-center gap-1.5 pt-0.5">
              <span className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                {roleLabel}
              </span>
              {onlineDuration ? (
                <span className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                  <Timer className="size-3" />
                  本次在线 {onlineDuration}
                </span>
              ) : null}
            </div>
          </div>

          <div className="shrink-0 sm:text-right">
            <p className="font-data text-4xl leading-none font-semibold tabular-nums tracking-tight text-foreground">
              {timeStr}
            </p>
          </div>
        </div>

        {/* ── 平台指标 ── */}
        <div className="mt-auto grid grid-cols-2 gap-px overflow-hidden rounded-xl border bg-border lg:grid-cols-4">
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
      </CardContent>
    </Card>
  );
}

/* ------------------------------------------------------------------ */
/*  指标格子 —— 1px 描边网格里的一格，整格可点击跳转到对应明细页            */
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
        "group/metric relative flex flex-col gap-1.5 bg-card px-4 py-3 transition-colors",
        "hover:bg-muted/60"
      )}
    >
      <span className="flex items-center gap-1.5 text-[11px] font-medium tracking-wide text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
        <ArrowUpRight className="ml-auto size-3 opacity-0 transition-opacity group-hover/metric:opacity-60" />
      </span>
      {loading ? (
        <Skeleton className="h-7 w-12" />
      ) : (
        <span className="font-data text-2xl leading-none font-semibold tabular-nums text-foreground">
          {value ?? "--"}
        </span>
      )}
    </Link>
  );
}
