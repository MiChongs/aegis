"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Progress as ProgressPrimitive } from "radix-ui";
import { AlertTriangle, Check, Loader2, LogIn, RefreshCw, ShieldAlert, ShieldCheck } from "lucide-react";
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

// 阶段文案。会话查询 retry: false，所以措辞不能许诺"系统会持续重试"——
// 慢只是还没返回，真正的重试入口在超时后的按钮上。
const PHASE_COPY: Record<Phase, { title: string; hint: string }> = {
  verifying: { title: "正在验证管理员会话", hint: "正在与服务端确认令牌有效性与权限作用域" },
  slow: { title: "仍在验证管理员会话", hint: "服务端响应较慢，连接保持中，无需刷新页面" },
  timeout: { title: "会话验证耗时较长", hint: "可以继续等待，也可以手动重试或返回登录页" },
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
      detail: "读取浏览器内保存的会话令牌",
      state: (credentialReady ? "done" : "active") as StepState,
    },
    {
      key: "session",
      label: "会话校验",
      detail: "服务端确认令牌未过期、未被吊销",
      state: (!credentialReady ? "pending" : sessionReady ? "done" : "active") as StepState,
    },
    {
      key: "scope",
      label: "权限上下文",
      detail: "同步角色、应用作用域与权限点",
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
    const stamp = [
      apiError?.status ? `HTTP ${apiError.status}` : null,
      apiError?.code ? `#${apiError.code}` : null,
    ]
      .filter(Boolean)
      .join(" · ");

    return (
      <AuthGateStage tone="danger" busy={false}>
        <AuthGateHeader
          tone="danger"
          scanning={false}
          title="无法验证会话"
          hint="服务端没有给出有效回应，控制台暂不放行。"
        />

        <div className="px-7 pb-5">
          <div className="rounded-lg border bg-muted/50 px-4 py-3 text-left">
            <div className="flex items-center justify-between gap-3">
              <span className="text-[11px] font-medium text-muted-foreground">错误详情</span>
              {stamp ? <span className="auth-gate__accent font-data text-[11px]">{stamp}</span> : null}
            </div>
            <p className="mt-1.5 font-data text-[12px] leading-relaxed break-words text-foreground/85">
              {message}
            </p>
            {apiError?.requestId ? (
              <p className="mt-1.5 font-data text-[11px] text-muted-foreground">
                request-id {apiError.requestId}
              </p>
            ) : null}
          </div>
        </div>

        <AuthGateActions
          onRetry={handleRetry}
          onBackToLogin={handleBackToLogin}
          retrying={sessionQuery.isFetching}
        />
      </AuthGateStage>
    );
  }

  // ── 正常 loading 态（含 hydration / 无 token 跳转前） ───
  if (verifying) {
    const tone: Tone = phase === "timeout" ? "warning" : "default";
    const copy = PHASE_COPY[phase];
    const steps = buildSteps(hydrated && Boolean(token), sessionQuery.isSuccess);

    return (
      <AuthGateStage tone={tone} busy>
        <AuthGateHeader tone={tone} scanning title={copy.title} hint={copy.hint} />

        <div className="px-7 pb-5">
          <ol className="rounded-lg border bg-muted/50 px-4 py-3.5">
            {steps.map((step, index) => (
              <li
                key={step.key}
                className={cn(
                  "relative flex items-start gap-3",
                  index < steps.length - 1 &&
                    "pb-3.5 after:absolute after:top-[1.375rem] after:bottom-1 after:left-[0.59375rem] after:w-px after:bg-border",
                )}
              >
                <StepMarker state={step.state} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline justify-between gap-3">
                    <span
                      className={cn(
                        "text-[13px] leading-5 font-medium",
                        step.state === "pending" ? "text-muted-foreground" : "text-foreground",
                      )}
                    >
                      {step.label}
                    </span>
                    <span
                      className={cn(
                        "font-data text-[11px] whitespace-nowrap",
                        step.state === "pending" ? "text-muted-foreground/60" : "text-muted-foreground",
                      )}
                    >
                      {STEP_STATE_LABEL[step.state]}
                    </span>
                  </div>
                  <p className="mt-0.5 text-[11.5px] leading-relaxed text-muted-foreground/80">
                    {step.detail}
                  </p>
                </div>
              </li>
            ))}
          </ol>
        </div>

        {phase === "timeout" ? (
          <AuthGateActions
            notice={
              <>
                <AlertTriangle className="size-3.5 shrink-0" />
                <span>已等待 {Math.round(elapsedMs / 1000)}s，服务端仍未响应</span>
              </>
            }
            onRetry={handleRetry}
            onBackToLogin={handleBackToLogin}
            retrying={sessionQuery.isFetching}
          />
        ) : (
          <footer className="flex items-center justify-between gap-3 border-t px-7 py-3.5">
            <span className="flex items-center gap-2 text-muted-foreground">
              <ShieldCheck className="size-3.5 shrink-0" />
              <span className="text-[11.5px]">校验通过后自动进入控制台</span>
            </span>
            <span className="font-data text-[11px] tabular-nums text-muted-foreground/70">
              {seconds}s
            </span>
          </footer>
        )}
      </AuthGateStage>
    );
  }

  return <>{children}</>;
}

// ─── 卡片外壳 ──────────────────────────────────
// 背景（网格 + 辉光）、卡片 chrome、顶边流光都收在这里，
// loading 与 error 两态共用同一副骨架，只换 tone 与内容。
function AuthGateStage({
  tone,
  busy,
  children,
}: {
  tone: Tone;
  busy: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className="auth-gate relative grid min-h-svh place-items-center overflow-hidden bg-background px-4 py-10"
      data-tone={tone}
    >
      <div aria-hidden className="auth-gate__grid pointer-events-none absolute inset-0" />
      <div aria-hidden className="auth-gate__glow pointer-events-none absolute inset-0" />

      <section
        role="status"
        aria-live="polite"
        aria-busy={busy}
        className={cn(
          "relative w-full max-w-[27rem] overflow-hidden rounded-xl border",
          "bg-card/85 text-card-foreground backdrop-blur-xl",
          "shadow-[var(--shadow-medium)]",
        )}
      >
        {busy ? (
          <ProgressPrimitive.Root
            value={null}
            className="absolute inset-x-0 top-0 z-10 h-[2px] overflow-hidden"
          >
            <ProgressPrimitive.Indicator className="auth-gate__beam animate-indeterminate-progress h-full w-2/5" />
          </ProgressPrimitive.Root>
        ) : null}

        {children}
      </section>
    </div>
  );
}

// ─── 徽标 + 标题 ───────────────────────────────
function AuthGateHeader({
  tone,
  scanning,
  title,
  hint,
}: {
  tone: Tone;
  scanning: boolean;
  title: string;
  hint: string;
}) {
  return (
    <div className="flex flex-col items-center gap-5 px-7 pt-9 pb-6 text-center">
      <div className="relative grid size-[4.75rem] place-items-center">
        {scanning ? (
          <>
            <span aria-hidden className="auth-gate__halo absolute inset-0 rounded-full" />
            <span aria-hidden className="auth-gate__halo auth-gate__halo--lag absolute inset-0 rounded-full" />
            <span aria-hidden className="auth-gate__sweep absolute inset-[6px] rounded-full" />
          </>
        ) : null}
        <span aria-hidden className="absolute inset-[6px] rounded-full border border-border/70" />
        <div
          className={cn(
            "relative grid size-12 place-items-center rounded-2xl border bg-card",
            tone === "default" ? "border-border text-foreground" : "auth-gate__accent border-current/35",
          )}
        >
          {tone === "danger" ? (
            <ShieldAlert className="size-6" strokeWidth={1.7} />
          ) : (
            <AegisMark className="size-7" />
          )}
        </div>
      </div>

      <div className="space-y-2">
        <h1 className="text-[15px] font-semibold tracking-tight text-foreground">{title}</h1>
        <p className="mx-auto max-w-[19rem] text-[13px] leading-relaxed text-muted-foreground">{hint}</p>
      </div>
    </div>
  );
}

// ─── 步骤标记 ──────────────────────────────────
function StepMarker({ state }: { state: StepState }) {
  return (
    <span
      data-state={state}
      className="auth-gate__marker relative z-10 mt-0.5 grid size-[1.1875rem] shrink-0 place-items-center rounded-full border bg-card text-muted-foreground"
    >
      {state === "done" ? <Check className="size-3" strokeWidth={3} /> : null}
      {state === "active" ? <Loader2 className="size-3 animate-spin" /> : null}
      {state === "pending" ? <span className="size-1.5 rounded-full bg-current opacity-40" /> : null}
    </span>
  );
}

// ─── 底部操作条 ────────────────────────────────
function AuthGateActions({
  notice,
  onRetry,
  onBackToLogin,
  retrying,
}: {
  notice?: ReactNode;
  onRetry: () => void;
  onBackToLogin: () => void;
  retrying: boolean;
}) {
  return (
    <footer className="flex flex-col gap-3 border-t px-7 py-4">
      {notice ? (
        <p className="auth-gate__accent flex items-center justify-center gap-2 text-[11.5px]">{notice}</p>
      ) : null}
      <div className="flex items-center gap-2">
        <Button size="sm" className="flex-1" onClick={onRetry} disabled={retrying}>
          <RefreshCw className={cn("size-3.5", retrying && "animate-spin")} />
          重试
        </Button>
        <Button size="sm" variant="outline" className="flex-1" onClick={onBackToLogin}>
          <LogIn className="size-3.5" />
          返回登录
        </Button>
      </div>
    </footer>
  );
}
