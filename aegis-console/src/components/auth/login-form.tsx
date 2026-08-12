"use client";

import { FormEvent, useCallback, useId, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
  ArrowLeft,
  Check,
  Eye,
  EyeOff,
  KeyRound,
  LoaderCircle,
  ShieldAlert
} from "lucide-react";
import { z } from "zod";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { loginAsAdmin, verifyAdminMFA, ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminCaptchaPublicConfigQuery, useOIDCPublicConfigQuery } from "@/lib/admin-hooks";
import { getOIDCAuthURL } from "@/lib/api/system";
import { CaptchaField } from "./captcha-field";
import { AUTH_EASE } from "./auth-motion";
import { LegalDialog } from "@/components/legal/legal-dialog";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const loginSchema = z.object({
  account: z.string().min(3, "请输入账号"),
  password: z.string().min(4, "请输入密码")
});

const TOTP_LENGTH = 6;

type FormPhase = "login" | "mfa" | "success";

/** 三态的抬头文案。集中一处，省得三个分支各写一遍 `phase === ...`。 */
const PHASE_COPY: Record<FormPhase, { title: string; desc: string }> = {
  login: { title: "登录 Aegis", desc: "使用管理员账号进入控制台" },
  mfa: { title: "两步验证", desc: "输入认证器中的验证码" },
  success: { title: "登录成功", desc: "正在进入控制台" }
};

/**
 * 管理员登录表单 —— 一张卡片承载「登录 → 两步验证 → 成功」三态。
 *
 * 三态共用同一张卡而不是三个页面：切换发生在同一个视觉容器里，
 * 用户不会怀疑自己被跳到了别处。代价是高度会变，靠外层 `layout` 补成一次形变。
 *
 * 控件全部走 shadcn 原语（Card / Input / Label / Button / Tabs / InputOTP / Alert），
 * 不再有手搓的错误横幅与分隔线 —— 那些东西不跟着主题变量走。
 */
export function LoginForm() {
  const router = useRouter();
  const reduced = useReducedMotion();
  const setSession = useAuthStore((state) => state.setSession);

  const [phase, setPhase] = useState<FormPhase>("login");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [captchaId, setCaptchaId] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [legalDialog, setLegalDialog] = useState<"terms" | "privacy" | null>(null);
  const [mfaChallenge, setMfaChallenge] = useState<{ challengeId: string; methods: string[] } | null>(null);
  const [mfaLoading, setMfaLoading] = useState(false);

  const captchaConfigQuery = useAdminCaptchaPublicConfigQuery();
  const captchaConfig = captchaConfigQuery.data;
  const needCaptcha = Boolean(captchaConfig?.enabled && captchaConfig?.requireForLogin);

  const handleCaptchaIdChange = useCallback((id: string) => setCaptchaId(id), []);

  const doRedirect = useCallback(() => {
    const next = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("next") : null;
    router.replace(next || "/overview");
  }, [router]);

  const showSuccess = useCallback(
    (result: { accessToken?: string; refreshToken?: string | null; operator?: unknown }) => {
      setSession(result as Parameters<typeof setSession>[0]);
      setPhase("success");
      setTimeout(doRedirect, 650);
    },
    [setSession, doRedirect]
  );

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const formData = new FormData(event.currentTarget);
    const parsed = loginSchema.safeParse({
      account: formData.get("account"),
      password: formData.get("password")
    });
    if (!parsed.success) {
      setError(parsed.error.issues[0]?.message || "表单校验失败");
      return;
    }

    const captchaAnswer = String(formData.get("captchaAnswer") || "").trim();
    if (needCaptcha && (!captchaId || !captchaAnswer)) {
      setError("请先完成验证码");
      return;
    }

    setLoading(true);
    try {
      const result = await loginAsAdmin({
        ...parsed.data,
        captchaId: needCaptcha ? captchaId : undefined,
        captchaAnswer: needCaptcha ? captchaAnswer : undefined
      });
      if (result.requiresSecondFactor && result.challenge) {
        setMfaChallenge({ challengeId: result.challenge.challengeId, methods: result.challenge.methods });
        setPhase("mfa");
        return;
      }
      if (!result.accessToken) throw new ApiError("登录成功，但未获取到访问令牌");
      showSuccess(result);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "登录失败，请稍后重试");
    } finally {
      setLoading(false);
    }
  }

  const verifyMFA = useCallback(
    async (payload: { code?: string; recoveryCode?: string }) => {
      if (!mfaChallenge || mfaLoading) return;
      setError(null);
      setMfaLoading(true);
      try {
        const result = await verifyAdminMFA({
          challengeId: mfaChallenge.challengeId,
          code: payload.code?.trim() || undefined,
          recoveryCode: payload.recoveryCode?.trim() || undefined
        });
        if (!result.accessToken) throw new ApiError("验证成功，但未获取到访问令牌");
        showSuccess(result);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "验证失败");
      } finally {
        setMfaLoading(false);
      }
    },
    [mfaChallenge, mfaLoading, showSuccess]
  );

  const backToLogin = useCallback(() => {
    setPhase("login");
    setMfaChallenge(null);
    setError(null);
  }, []);

  const copy = PHASE_COPY[phase];

  return (
    <>
      <m.div
        layout={!reduced}
        transition={reduced ? { duration: 0 } : { duration: 0.35, ease: AUTH_EASE }}
        initial={reduced ? false : { opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        className="w-full"
      >
        <Card className="gap-0 py-0 shadow-none">
          <CardHeader className="items-center gap-2 px-6 pt-8 pb-6 text-center">
            <div className="mx-auto mb-1 flex size-11 items-center justify-center rounded-xl border bg-muted/50">
              <AegisMark className="size-6 text-foreground" />
            </div>
            {/* 标题随阶段切换，容器不变 —— 换的是这一步在做什么，不是换了个页面 */}
            <AnimatePresence mode="wait" initial={false}>
              <m.div
                key={phase}
                initial={reduced ? false : { opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                exit={reduced ? undefined : { opacity: 0, y: -5 }}
                transition={{ duration: 0.2, ease: AUTH_EASE }}
                className="flex flex-col gap-1.5"
              >
                <CardTitle className="text-xl font-semibold tracking-tight">{copy.title}</CardTitle>
                <CardDescription className="text-[13px]">{copy.desc}</CardDescription>
              </m.div>
            </AnimatePresence>
          </CardHeader>

          <CardContent className="px-6 pt-0 pb-7">
            <AnimatePresence mode="wait" initial={false}>
              {phase === "success" ? (
                <SuccessPanel key="success" reduced={reduced} />
              ) : phase === "mfa" ? (
                <m.div
                  key="mfa"
                  initial={reduced ? false : { opacity: 0, x: 16 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={reduced ? undefined : { opacity: 0, x: -16 }}
                  transition={{ duration: 0.25, ease: AUTH_EASE }}
                >
                  <MFAPanel
                    methods={mfaChallenge?.methods ?? []}
                    loading={mfaLoading}
                    error={error}
                    onVerify={verifyMFA}
                    onBack={backToLogin}
                  />
                </m.div>
              ) : (
                <m.div
                  key="login"
                  initial={reduced ? false : { opacity: 0, x: -16 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={reduced ? undefined : { opacity: 0, x: 16 }}
                  transition={{ duration: 0.25, ease: AUTH_EASE }}
                >
                  <form onSubmit={handleSubmit} className="flex flex-col gap-4" noValidate>
                    <div className="grid gap-2">
                      <Label htmlFor="account">账号</Label>
                      <Input
                        id="account"
                        name="account"
                        autoComplete="username"
                        placeholder="管理员账号"
                        aria-invalid={Boolean(error)}
                        className="h-10"
                        autoFocus
                      />
                    </div>

                    <div className="grid gap-2">
                      <Label htmlFor="password">密码</Label>
                      <div className="relative">
                        <Input
                          id="password"
                          name="password"
                          type={showPassword ? "text" : "password"}
                          autoComplete="current-password"
                          placeholder="登录密码"
                          aria-invalid={Boolean(error)}
                          className="h-10 pr-10"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          tabIndex={-1}
                          aria-label={showPassword ? "隐藏密码" : "显示密码"}
                          className="absolute top-1/2 right-1.5 -translate-y-1/2 text-muted-foreground/70 hover:text-foreground"
                          onClick={() => setShowPassword((v) => !v)}
                        >
                          {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                        </Button>
                      </div>
                    </div>

                    {/* 验证码配置未到位时先占位，否则按钮会在加载完那一刻整体下跳 */}
                    {captchaConfigQuery.isPending ? (
                      <div className="grid gap-2">
                        <Skeleton className="h-4 w-14" />
                        <Skeleton className="h-10 w-full" />
                      </div>
                    ) : needCaptcha && captchaConfig ? (
                      <CaptchaField
                        config={captchaConfig}
                        captchaId={captchaId}
                        onCaptchaIdChange={handleCaptchaIdChange}
                      />
                    ) : null}

                    <ErrorAlert error={error} reduced={reduced} />

                    <Button type="submit" className="mt-1 h-10 w-full" disabled={loading}>
                      {loading ? <LoaderCircle className="size-4 animate-spin" /> : null}
                      {loading ? "登录中…" : "登录"}
                    </Button>

                    <SSOSection />

                    <p className="text-center text-[13px] text-muted-foreground">
                      还没有账号？{" "}
                      <Link
                        href="/register"
                        className="font-medium text-foreground underline-offset-4 transition-colors hover:underline"
                      >
                        立即注册
                      </Link>
                    </p>

                    <p className="text-center text-[11px] leading-5 text-muted-foreground/70">
                      登录即表示同意{" "}
                      <button
                        type="button"
                        className="underline underline-offset-2 transition-colors hover:text-foreground"
                        onClick={() => setLegalDialog("terms")}
                      >
                        用户协议
                      </button>{" "}
                      与{" "}
                      <button
                        type="button"
                        className="underline underline-offset-2 transition-colors hover:text-foreground"
                        onClick={() => setLegalDialog("privacy")}
                      >
                        隐私政策
                      </button>
                    </p>
                  </form>
                </m.div>
              )}
            </AnimatePresence>
          </CardContent>
        </Card>
      </m.div>

      {legalDialog ? (
        <LegalDialog
          type={legalDialog}
          open
          onOpenChange={(open) => {
            if (!open) setLegalDialog(null);
          }}
        />
      ) : null}
    </>
  );
}

/* ────────────────────────────── 两步验证 ────────────────────────────── */

/**
 * 两种方式都可用时用 Tabs 分开，而不是把两个输入框并排堆着 ——
 * 恢复码是一次性凭据，和日常的 6 位动态码摆在一起会被误当成"另一个格子也要填"。
 * 只有一种可用时不渲染 Tabs：一个页签的页签栏是纯噪声。
 */
function MFAPanel({
  methods,
  loading,
  error,
  onVerify,
  onBack
}: {
  methods: string[];
  loading: boolean;
  error: string | null;
  onVerify: (payload: { code?: string; recoveryCode?: string }) => void;
  onBack: () => void;
}) {
  const hasTOTP = methods.includes("totp");
  const hasRecovery = methods.includes("recovery_code");
  const both = hasTOTP && hasRecovery;

  const totp = <TOTPForm loading={loading} error={error} onVerify={onVerify} />;
  const recovery = <RecoveryForm loading={loading} error={error} onVerify={onVerify} />;

  return (
    <div className="flex flex-col gap-4">
      {both ? (
        <Tabs defaultValue="totp" className="gap-4">
          <TabsList className="w-full">
            <TabsTrigger value="totp" className="flex-1">
              验证码
            </TabsTrigger>
            <TabsTrigger value="recovery" className="flex-1">
              恢复码
            </TabsTrigger>
          </TabsList>
          <TabsContent value="totp">{totp}</TabsContent>
          <TabsContent value="recovery">{recovery}</TabsContent>
        </Tabs>
      ) : hasRecovery ? (
        recovery
      ) : (
        totp
      )}

      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={onBack}
        className="group self-center text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-3.5 transition-transform group-hover:-translate-x-0.5" />
        返回登录
      </Button>
    </div>
  );
}

/** 6 位动态码：填满即自动提交，不再要求多按一次「确认」 */
function TOTPForm({
  loading,
  error,
  onVerify
}: {
  loading: boolean;
  error: string | null;
  onVerify: (payload: { code?: string }) => void;
}) {
  const reduced = useReducedMotion();
  const [code, setCode] = useState("");
  const submittedRef = useRef("");

  const submit = useCallback(
    (value: string) => {
      if (value.length !== TOTP_LENGTH || submittedRef.current === value) return;
      submittedRef.current = value;
      onVerify({ code: value });
    },
    [onVerify]
  );

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        submit(code);
      }}
    >
      <InputOTP
        maxLength={TOTP_LENGTH}
        value={code}
        onChange={(value) => {
          setCode(value);
          if (submittedRef.current && value !== submittedRef.current) submittedRef.current = "";
        }}
        onComplete={submit}
        disabled={loading}
        autoFocus
        containerClassName="w-full justify-center"
      >
        <InputOTPGroup className="gap-2">
          {Array.from({ length: TOTP_LENGTH }, (_, index) => (
            <InputOTPSlot
              key={index}
              index={index}
              aria-invalid={Boolean(error)}
              className="size-11 rounded-md border font-data text-base"
            />
          ))}
        </InputOTPGroup>
      </InputOTP>

      <ErrorAlert error={error} reduced={reduced} />

      <Button type="submit" className="h-10 w-full" disabled={loading || code.length !== TOTP_LENGTH}>
        {loading ? <LoaderCircle className="size-4 animate-spin" /> : null}
        {loading ? "验证中…" : "确认"}
      </Button>
    </form>
  );
}

/** 恢复码：一次性，格式交给服务端判 */
function RecoveryForm({
  loading,
  error,
  onVerify
}: {
  loading: boolean;
  error: string | null;
  onVerify: (payload: { recoveryCode?: string }) => void;
}) {
  const reduced = useReducedMotion();
  const [value, setValue] = useState("");
  const fieldId = useId();

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (value.trim()) onVerify({ recoveryCode: value });
      }}
    >
      <div className="grid gap-2">
        <Label htmlFor={fieldId}>一次性恢复码</Label>
        <Input
          id={fieldId}
          value={value}
          onChange={(event) => setValue(event.target.value)}
          placeholder="XXXX-XXXX-XXXX"
          autoComplete="one-time-code"
          aria-invalid={Boolean(error)}
          className="h-10 font-data tracking-[0.1em]"
        />
      </div>

      <ErrorAlert error={error} reduced={reduced} />

      <Button type="submit" className="h-10 w-full" disabled={loading || !value.trim()}>
        {loading ? <LoaderCircle className="size-4 animate-spin" /> : null}
        {loading ? "验证中…" : "确认"}
      </Button>
    </form>
  );
}

/* ────────────────────────────── 成功态 ────────────────────────────── */

/**
 * 成功态停留 650ms 再跳转 —— 立即跳会让人怀疑"我到底登进去没有"，
 * 而这一下确认的成本远低于回头再看一遍。
 */
function SuccessPanel({ reduced }: { reduced: boolean | null }) {
  return (
    <m.div
      key="success"
      initial={reduced ? false : { opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={reduced ? undefined : { opacity: 0 }}
      transition={{ duration: 0.2 }}
      className="flex flex-col items-center py-6"
    >
      <div className="relative flex size-14 items-center justify-center">
        {reduced ? null : (
          <m.span
            aria-hidden
            className="absolute inset-0 rounded-full bg-emerald-500/20"
            initial={{ scale: 0.6, opacity: 0.8 }}
            animate={{ scale: 1.8, opacity: 0 }}
            transition={{ duration: 1.1, ease: "easeOut", repeat: Infinity, repeatDelay: 0.1 }}
          />
        )}
        <m.span
          className="relative flex size-12 items-center justify-center rounded-full bg-emerald-500/12 text-emerald-600 ring-1 ring-emerald-500/30 dark:text-emerald-400"
          initial={reduced ? false : { scale: 0.4, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          transition={reduced ? undefined : { type: "spring", stiffness: 320, damping: 18 }}
        >
          <Check className="size-6" strokeWidth={2.6} />
        </m.span>
      </div>
    </m.div>
  );
}

/* ────────────────────────────── 错误 ────────────────────────────── */

/**
 * 高度从 0 展开而不是直接出现：按钮被往下推的过程本身就是"有新东西出现了"的信号。
 * 再叠一次短促横移 —— 登录失败是需要被打断注意力的事件。
 */
function ErrorAlert({ error, reduced }: { error: string | null; reduced: boolean | null }) {
  return (
    <AnimatePresence initial={false}>
      {error ? (
        <m.div
          key="error"
          initial={reduced ? false : { opacity: 0, height: 0 }}
          animate={
            reduced
              ? { opacity: 1, height: "auto" }
              : { opacity: 1, height: "auto", x: [0, -5, 5, -3, 3, 0] }
          }
          exit={reduced ? undefined : { opacity: 0, height: 0 }}
          transition={{ duration: 0.3, ease: AUTH_EASE }}
          className="overflow-hidden"
        >
          <Alert variant="destructive" className="border-destructive/30 bg-destructive/5 py-2.5">
            <ShieldAlert />
            <AlertDescription className="text-destructive">{error}</AlertDescription>
          </Alert>
        </m.div>
      ) : null}
    </AnimatePresence>
  );
}

/* ────────────────────────────── SSO ────────────────────────────── */

/**
 * 单点登录入口。开关来自 `/api/admin/auth/oidc/config`（免登录可读）。
 * **没启用就整段不渲染** —— 一个点了会报错的按钮比没有这个按钮更糟。
 */
function SSOSection() {
  const { data } = useOIDCPublicConfigQuery();
  const [redirecting, setRedirecting] = useState(false);

  const handleSSO = useCallback(async () => {
    setRedirecting(true);
    try {
      const { url } = await getOIDCAuthURL();
      window.location.href = url;
    } catch {
      setRedirecting(false);
    }
  }, []);

  if (!data?.enabled) return null;

  return (
    <>
      <div className="flex items-center gap-3">
        <Separator className="flex-1" />
        <span className="text-[11px] text-muted-foreground/60 select-none">或</span>
        <Separator className="flex-1" />
      </div>
      <Button
        type="button"
        variant="outline"
        className="h-10 w-full"
        onClick={handleSSO}
        disabled={redirecting}
      >
        {redirecting ? <LoaderCircle className="size-4 animate-spin" /> : <KeyRound className="size-4" />}
        {redirecting ? "正在跳转…" : "使用单点登录"}
      </Button>
    </>
  );
}
