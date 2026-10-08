"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { AlertTriangle, Check, LogIn, RefreshCw, ShieldAlert } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Button } from "@/components/ui/button";
import { useAdminSessionQuery, useHydrated } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { cn } from "@/lib/utils";

type Tone = "default" | "warning" | "danger";
type Phase = "verifying" | "slow" | "timeout";
type StepState = "pending" | "active" | "done";

// 阶段阈值（毫秒）：3s 后承认"慢了"，10s 后交出手动出口
const SLOW_MS = 3_000;
const TIMEOUT_MS = 10_000;
// 计时粒度：footer 的秒表保留一位小数，250ms 一跳刚好让它匀速走动
const TICK_MS = 250;
// 校验这么久还没结束才显示画面；在此之前只有底色，避免快速通过时闪一下
const REVEAL_MS = 500;

// 阶段文案。会话查询 retry: false，所以措辞不能许诺"系统会持续重试"——
// 慢只是还没返回，真正的重试入口在超时后的按钮上。
const PHASE_COPY: Record<Phase, { title: string; hint: string }> = {
  verifying: { title: "正在验证管理员会话", hint: "" },
  slow: { title: "仍在验证管理员会话", hint: "服务端响应较慢" },
  timeout: { title: "会话验证耗时较长", hint: "" },
};

const STEP_STATE_LABEL: Record<StepState, string> = {
  pending: "等待",
  active: "进行中",
  done: "已就绪",
};

// ─── 校验清单 ──────────────────────────────────
// 三步都对应真实状态，不做假进度：凭据读没读到、令牌校没校完，
// 都是本组件当下确实知道的事。第三步在会话返回后才点亮，随即卸载进入控制台。
function buildSteps(credentialReady: boolean, sessionReady: boolean) {
  return [
    {
      key: "credential",
      label: "本地凭据",
      state: (credentialReady ? "done" : "active") as StepState,
    },
    {
      key: "session",
      label: "会话校验",
      state: (!credentialReady ? "pending" : sessionReady ? "done" : "active") as StepState,
    },
    {
      key: "scope",
      label: "权限上下文",
      state: (sessionReady ? "active" : "pending") as StepState,
    },
  ];
}

// ─── 主组件 ────────────────────────────────────

export function AuthGate({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const hydrated = useHydrated();
  const token = useAuthStore((state) => state.accessToken);
  const operator = useAuthStore((state) => state.operator);
  const setSession = useAuthStore((state) => state.setSession);
  const clearSession = useAuthStore((state) => state.clearSession);
  const sessionQuery = useAdminSessionQuery();

  // 起点在 effect 里落定：Date.now() 属于 impure，不能在渲染期调用
  const startedAtRef = useRef(0);
  const [elapsedMs, setElapsedMs] = useState(0);

  // 响应 query 状态
  useEffect(() => {
    if (!hydrated) return;

    if (!token) {
      router.replace("/login");
      return;
    }

    if (sessionQuery.isSuccess) {
      const session = sessionQuery.data;
      setSession({
        accessToken: token,
        operator: {
          id: session.adminId,
          account: session.account,
          displayName: session.displayName,
          avatar: operator?.avatar,
          role: session.isSuperAdmin ? "super-admin" : operator?.role || "admin",
          isSuperAdmin: session.isSuperAdmin,
          assignments: session.assignments,
        },
      });
    }

    if (sessionQuery.isError) {
      const cause = sessionQuery.error;
      if (cause instanceof ApiError && cause.status === 401) {
        clearSession();
        router.replace(`/login?next=${encodeURIComponent(pathname)}`);
      }
      // 非 401 不自动跳转，让用户看到错误卡片并选择重试 / 返回登录
    }
  }, [
    clearSession,
    hydrated,
    operator?.avatar,
    operator?.role,
    pathname,
    router,
    sessionQuery.data,
    sessionQuery.error,
    sessionQuery.isError,
    sessionQuery.isSuccess,
    setSession,
    token,
  ]);

  const verifying = !hydrated || !token || sessionQuery.isLoading;

  // 计时器：只在校验中挂一个 interval，阶段由 elapsed 推导而不是存成 state。
  // 旧实现把 phase 存进 state 又列进依赖，phase 一变 effect 就重挂并把起点清零，
  // 于是永远走不到 timeout 分支 —— 超时后的重试按钮实际上从来没出现过。
  useEffect(() => {
    if (!verifying) return;
    startedAtRef.current = Date.now();
    const id = window.setInterval(() => {
      setElapsedMs(Date.now() - startedAtRef.current);
    }, TICK_MS);
    return () => window.clearInterval(id);
  }, [verifying]);

  const phase: Phase = elapsedMs >= TIMEOUT_MS ? "timeout" : elapsedMs >= SLOW_MS ? "slow" : "verifying";
  const seconds = (elapsedMs / 1000).toFixed(1);

  const handleRetry = () => {
    startedAtRef.current = Date.now();
    setElapsedMs(0);
    void sessionQuery.refetch();
  };

  const handleBackToLogin = () => {
    clearSession();
    router.replace(`/login?next=${encodeURIComponent(pathname)}`);
  };

  // ── 错误态（非 401）──────────────────────
  const isFatalError =
    sessionQuery.isError &&
    !(sessionQuery.error instanceof ApiError && sessionQuery.error.status === 401);

  if (isFatalError) {
    const cause = sessionQuery.error;
    const message = cause instanceof Error ? cause.message : "会话验证失败，请稍后重试";
    const apiError = cause instanceof ApiError ? cause : null;
    const stamp = [apiError?.status ? `HTTP ${apiError.status}` : null, apiError?.code ? `错误码 ${apiError.code}` : null]
      .filter(Boolean)
      .join("，");

    return (
      <GateScreen busy={false}>
        <GateEmblem tone="danger" spinning={false} />
        <h1 className="mt-7 text-lg font-semibold tracking-tight">无法验证会话</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">服务端返回了错误，你可以重试或重新登录</p>

        <div className="mt-6 w-full rounded-xl border bg-card px-4 py-3 text-left">
          <div className="flex items-center justify-between gap-3">
            <span className="text-[11px] font-medium text-muted-foreground">错误详情</span>
            {stamp ? <span className="font-data text-[11px] text-destructive">{stamp}</span> : null}
          </div>
          <p className="mt-1.5 font-data text-[12px] leading-relaxed break-words text-foreground/85">{message}</p>
          {apiError?.requestId ? (
            <p className="mt-1.5 truncate font-data text-[11px] text-muted-foreground" title={apiError.requestId}>
              请求 ID {apiError.requestId}
            </p>
          ) : null}
        </div>

        <GateActions onRetry={handleRetry} onBackToLogin={handleBackToLogin} retrying={sessionQuery.isFetching} />
      </GateScreen>
    );
  }

  // ── 正常 loading 态（含 hydration / 无 token 跳转前） ───
  if (verifying) {
    // 绝大多数时候校验在几百毫秒内完成。这段时间只给一块干净的底色，
    // 不把整套画面闪一下再消失 —— 那比等待本身更让人不安。
    if (elapsedMs < REVEAL_MS) {
      return <div className="min-h-svh bg-background" aria-busy role="status" aria-label="正在验证管理员会话" />;
    }

    const tone: Tone = phase === "timeout" ? "warning" : "default";
    const copy = PHASE_COPY[phase];
    const steps = buildSteps(hydrated && Boolean(token), sessionQuery.isSuccess);

    return (
      <GateScreen busy>
        <GateEmblem tone={tone} spinning />
        <h1 className="mt-7 text-lg font-semibold tracking-tight">{copy.title}</h1>
        <p className="mt-1.5 min-h-5 text-sm text-muted-foreground">{copy.hint || "正在确认你的登录状态与权限"}</p>

        <ol className="mt-8 grid w-full grid-cols-3 gap-2" aria-label="校验进度">
          {steps.map((step) => (
            <li key={step.key} className="min-w-0">
              <span
                aria-hidden
                className={cn(
                  "block h-1 overflow-hidden rounded-full",
                  step.state === "done" ? "bg-foreground" : "bg-muted"
                )}
              >
                {step.state === "active" ? (
                  <span
                    className={cn(
                      "block h-full w-2/5 animate-indeterminate-progress rounded-full",
                      tone === "warning" ? "bg-amber-500" : "bg-foreground/70"
                    )}
                  />
                ) : null}
              </span>
              <span
                className={cn(
                  "mt-2 flex items-center justify-center gap-1 text-[12px]",
                  step.state === "pending" ? "text-muted-foreground/60" : "text-foreground/85"
                )}
              >
                {step.state === "done" ? <Check className="size-3" strokeWidth={3} /> : null}
                {step.label}
              </span>
              <span className="sr-only">{STEP_STATE_LABEL[step.state]}</span>
            </li>
          ))}
        </ol>

        {phase === "timeout" ? (
          <>
            <p className="mt-7 flex items-center justify-center gap-1.5 text-[12.5px] text-amber-600 dark:text-amber-400">
              <AlertTriangle className="size-3.5 shrink-0" />
              已等待 {Math.round(elapsedMs / 1000)} 秒，服务端仍未响应
            </p>
            <GateActions onRetry={handleRetry} onBackToLogin={handleBackToLogin} retrying={sessionQuery.isFetching} />
          </>
        ) : (
          <p className="mt-7 font-data text-[11px] text-muted-foreground/70 tabular-nums">{seconds}s</p>
        )}
      </GateScreen>
    );
  }

  return <>{children}</>;
}

// ─── 全屏外壳 ──────────────────────────────────
// loading 与 error 两态共用：居中的一列内容 + 底部品牌落款，没有卡片、网格与辉光。
function GateScreen({ busy, children }: { busy: boolean; children: ReactNode }) {
  return (
    <div className="flex min-h-svh flex-col bg-background px-6 pt-[env(safe-area-inset-top)] pb-[max(1.25rem,env(safe-area-inset-bottom))]">
      <section
        role="status"
        aria-live="polite"
        aria-busy={busy}
        className="mx-auto flex w-full max-w-sm flex-1 animate-in flex-col items-center justify-center text-center duration-300 fade-in-0"
      >
        {children}
      </section>
      <p className="text-center text-[11px] text-muted-foreground/60">Aegis 控制台</p>
    </div>
  );
}

// ─── 徽标 ──────────────────────────────────────
// 校验中：品牌方块外绕一道细圆弧缓慢旋转；失败：红色盾牌。
function GateEmblem({ tone, spinning }: { tone: Tone; spinning: boolean }) {
  const accent =
    tone === "danger" ? "text-destructive" : tone === "warning" ? "text-amber-500" : "text-foreground";
  return (
    <div className="relative grid size-24 place-items-center">
      <svg aria-hidden viewBox="0 0 96 96" className="absolute inset-0 size-full">
        <circle cx="48" cy="48" r="44" fill="none" strokeWidth="1" className="stroke-border" />
      </svg>
      {spinning ? (
        <svg
          aria-hidden
          viewBox="0 0 96 96"
          className={cn("absolute inset-0 size-full animate-spin [animation-duration:1.6s] motion-reduce:animate-none", accent)}
        >
          <circle
            cx="48"
            cy="48"
            r="44"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
            strokeDasharray="56 220"
          />
        </svg>
      ) : null}
      {tone === "danger" ? (
        <span className="grid size-14 place-items-center rounded-2xl bg-destructive/10 text-destructive ring-1 ring-destructive/25">
          <ShieldAlert className="size-7" strokeWidth={1.7} />
        </span>
      ) : (
        <span className="grid size-14 place-items-center rounded-2xl bg-foreground text-background shadow-sm">
          <AegisMark static className="size-7" />
        </span>
      )}
    </div>
  );
}

// ─── 操作 ──────────────────────────────────────
function GateActions({
  onRetry,
  onBackToLogin,
  retrying
}: {
  onRetry: () => void;
  onBackToLogin: () => void;
  retrying: boolean;
}) {
  return (
    <div className="mt-6 grid w-full grid-cols-2 gap-2">
      <Button className="h-10" onClick={onRetry} disabled={retrying}>
        <RefreshCw className={cn("size-4", retrying && "animate-spin")} />
        重试
      </Button>
      <Button className="h-10" variant="outline" onClick={onBackToLogin}>
        <LogIn className="size-4" />
        返回登录
      </Button>
    </div>
  );
}
