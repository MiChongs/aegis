"use client";

import { FormEvent, useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Check, Eye, EyeOff, KeyRound, LoaderCircle, Lock, ShieldAlert, ShieldCheck, User } from "lucide-react";
import { z } from "zod";
import { LazyMotion, domAnimation, m, AnimatePresence } from "motion/react";
import { loginAsAdmin, verifyAdminMFA, ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminCaptchaPublicConfigQuery } from "@/lib/admin-hooks";
import { getOIDCPublicConfig, getOIDCAuthURL } from "@/lib/api/system";
import { CaptchaField } from "./captcha-field";
import { LegalDialog } from "@/components/legal/legal-dialog";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

const loginSchema = z.object({
  account: z.string().min(3, "请输入账号"),
  password: z.string().min(4, "请输入密码"),
});

const EASE = [0.22, 1, 0.36, 1] as const;
const SLIDE = { duration: 0.45, ease: EASE };

type FormPhase = "login" | "mfa" | "success";

export function LoginForm() {
  const router = useRouter();
  const setSession = useAuthStore((state) => state.setSession);
  const [phase, setPhase] = useState<FormPhase>("login");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [captchaId, setCaptchaId] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [legalDialog, setLegalDialog] = useState<"terms" | "privacy" | null>(null);
  const [mfaChallenge, setMfaChallenge] = useState<{ challengeId: string; methods: string[] } | null>(null);
  const [mfaCode, setMfaCode] = useState("");
  const [mfaRecoveryCode, setMfaRecoveryCode] = useState("");
  const [mfaLoading, setMfaLoading] = useState(false);
  const captchaConfigQuery = useAdminCaptchaPublicConfigQuery();
  const captchaConfig = captchaConfigQuery.data;
  const needCaptcha = captchaConfig?.enabled && captchaConfig?.requireForLogin;

  const handleCaptchaIdChange = useCallback((id: string) => setCaptchaId(id), []);

  const doRedirect = useCallback(() => {
    const next = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("next") : null;
    router.replace(next || "/overview");
  }, [router]);

  const showSuccess = useCallback((result: { accessToken?: string; refreshToken?: string | null; operator?: unknown }) => {
    setSession(result as Parameters<typeof setSession>[0]);
    setPhase("success");
    setTimeout(doRedirect, 500);
  }, [setSession, doRedirect]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const formData = new FormData(event.currentTarget);
    const parsed = loginSchema.safeParse({ account: formData.get("account"), password: formData.get("password") });
    if (!parsed.success) { setError(parsed.error.issues[0]?.message || "表单校验失败"); return; }

    const captchaAnswer = String(formData.get("captchaAnswer") || "").trim();
    if (needCaptcha && (!captchaId || !captchaAnswer)) { setError("请输入验证码"); return; }

    setLoading(true);
    try {
      const result = await loginAsAdmin({
        ...parsed.data,
        captchaId: needCaptcha ? captchaId : undefined,
        captchaAnswer: needCaptcha ? captchaAnswer : undefined,
      });
      if (result.requiresSecondFactor && result.challenge) {
        setMfaChallenge({ challengeId: result.challenge.challengeId, methods: result.challenge.methods });
        setPhase("mfa");
        setMfaCode("");
        setMfaRecoveryCode("");
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

  async function handleVerifyMFA(e: FormEvent) {
    e.preventDefault();
    if (!mfaChallenge) return;
    setError(null);
    setMfaLoading(true);
    try {
      const result = await verifyAdminMFA({
        challengeId: mfaChallenge.challengeId,
        code: mfaCode.trim() || undefined,
        recoveryCode: mfaRecoveryCode.trim() || undefined,
      });
      if (!result.accessToken) throw new ApiError("验证成功，但未获取到访问令牌");
      showSuccess(result);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "验证失败");
    } finally {
      setMfaLoading(false);
    }
  }

  // ── 共享卡片标题 ──
  const headerTitle = phase === "mfa" ? "双因子验证" : phase === "success" ? "认证成功" : "管理入口";
  const headerDesc = phase === "mfa" ? "请输入认证器中的验证码，或使用恢复码" : phase === "success" ? "正在进入控制台..." : "请使用管理员账号登录控制台";
  const headerBadge = phase === "mfa" ? "MFA" : phase === "success" ? "Verified" : "Admin Access";

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.6, ease: EASE }}
        className="w-full max-w-xl"
      >
        {/* 表单卡片：
            - 轻微的 backdrop-blur 让卡片在流动渐变背景上"浮"起来
            - 深色模式下保留阴影；浅色模式下不设阴影（符合项目"浅色无阴影"规范）
            - bg-card/90 + backdrop-blur 一起构成 Radix UI 的典型 surface-panel 视感 */}
        <Card className="border-border/60 bg-card/90 backdrop-blur-xl dark:shadow-2xl dark:ring-1 dark:ring-white/5">
          {/* ── 统一卡片头部 ── */}
          <CardHeader className="pb-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-border bg-muted/60">
                <AegisMark className="size-5 text-foreground" />
              </div>
              <m.div key={headerBadge} initial={{ opacity: 0, scale: 0.9 }} animate={{ opacity: 1, scale: 1 }} transition={{ duration: 0.3 }}>
                <Badge variant="secondary" className="text-[10px] font-medium tracking-wide select-none">{headerBadge}</Badge>
              </m.div>
            </div>
            <AnimatePresence mode="wait">
              <m.div key={headerTitle} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -6 }} transition={{ duration: 0.25 }}>
                <CardTitle className="text-2xl font-semibold tracking-tight">{headerTitle}</CardTitle>
                <CardDescription className="mt-1">{headerDesc}</CardDescription>
              </m.div>
            </AnimatePresence>
          </CardHeader>

          <Separator className="opacity-60" />

          {/* ── 卡片内容（动画切换） ── */}
          <CardContent className="pt-5 pb-6">
            <AnimatePresence mode="wait" initial={false}>
              {phase === "success" ? (
                <m.div key="success" initial={{ opacity: 0, scale: 0.95 }} animate={{ opacity: 1, scale: 1 }} transition={{ duration: 0.35 }} className="flex flex-col items-center gap-3 py-8">
                  <m.div initial={{ scale: 0 }} animate={{ scale: 1 }} transition={{ type: "spring", stiffness: 300, damping: 20, delay: 0.1 }}
                    className="flex size-14 items-center justify-center rounded-full bg-emerald-500/15 text-emerald-600 dark:text-emerald-400">
                    <Check className="size-7" strokeWidth={2.5} />
                  </m.div>
                  <p className="text-sm font-medium text-emerald-600 dark:text-emerald-400">认证成功，正在跳转</p>
                </m.div>
              ) : phase === "mfa" ? (
                <m.div key="mfa" initial={{ opacity: 0, x: 24 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -24 }} transition={SLIDE}>
                  <form onSubmit={handleVerifyMFA} className="flex flex-col gap-4">
                    {mfaChallenge?.methods.includes("totp") && (
                      <div className="grid gap-1.5">
                        <Label htmlFor="mfaCode" className="text-xs font-medium">验证码</Label>
                        <div className="relative">
                          <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground/50" />
                          <Input id="mfaCode" className="pl-9 h-10" placeholder="6 位验证码" autoComplete="one-time-code" autoFocus
                            value={mfaCode} onChange={(e) => setMfaCode(e.target.value)} />
                        </div>
                      </div>
                    )}
                    {mfaChallenge?.methods.includes("recovery_code") && (
                      <div className="grid gap-1.5">
                        <Label htmlFor="mfaRecovery" className="text-xs font-medium">恢复码</Label>
                        <Input id="mfaRecovery" className="h-10 font-mono" placeholder="或输入恢复码" value={mfaRecoveryCode} onChange={(e) => setMfaRecoveryCode(e.target.value)} />
                      </div>
                    )}
                    <ErrorBanner error={error} />
                    <Button className="h-11 font-semibold" size="lg" type="submit" disabled={mfaLoading || (!mfaCode.trim() && !mfaRecoveryCode.trim())}>
                      {mfaLoading ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : <ShieldCheck className="mr-2 size-4" />}
                      {mfaLoading ? "验证中..." : "确认验证"}
                    </Button>
                    <button type="button" className="text-center text-xs text-muted-foreground hover:text-foreground transition-colors" onClick={() => { setPhase("login"); setMfaChallenge(null); setError(null); }}>
                      返回登录
                    </button>
                  </form>
                </m.div>
              ) : (
                <m.div key="login" initial={{ opacity: 0, x: -24 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: 24 }} transition={SLIDE}>
                  <form onSubmit={handleSubmit} className="flex flex-col gap-4">
                    {/* 账号 */}
                    <div className="grid gap-1.5">
                      <Label htmlFor="account" className="text-xs font-medium">账号</Label>
                      <div className="relative">
                        <User className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground/50" />
                        <Input id="account" name="account" autoComplete="username" placeholder="请输入账号" className="pl-9 h-10" />
                      </div>
                    </div>

                    {/* 密码 */}
                    <div className="grid gap-1.5">
                      <Label htmlFor="password" className="text-xs font-medium">密码</Label>
                      <div className="relative">
                        <Lock className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground/50" />
                        <Input id="password" name="password" type={showPassword ? "text" : "password"} autoComplete="current-password" placeholder="请输入密码" className="pl-9 pr-10 h-10" />
                        <button type="button" tabIndex={-1} className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground/50 hover:text-muted-foreground transition-colors" onClick={() => setShowPassword(!showPassword)}>
                          {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                        </button>
                      </div>
                    </div>

                    {/* 验证码 */}
                    {needCaptcha && captchaConfig ? (
                      <CaptchaField config={captchaConfig} captchaId={captchaId} onCaptchaIdChange={handleCaptchaIdChange} />
                    ) : null}

                    {/* 错误 */}
                    <ErrorBanner error={error} />

                    {/* 主按钮 */}
                    <Button className="h-11 font-semibold text-[15px]" size="lg" type="submit" disabled={loading}>
                      {loading ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
                      {loading ? "登录中..." : "进入控制台"}
                    </Button>

                    {/* SSO */}
                    <SSOButton />

                    {/* 注册链接 */}
                    <div className="text-center text-sm text-muted-foreground">
                      没有账号？{" "}
                      <Link href="/register" className="font-medium text-foreground underline-offset-4 hover:underline">立即注册</Link>
                    </div>

                    {/* 法律 */}
                    <p className="text-center text-[11px] leading-relaxed text-muted-foreground/60">
                      登录即表示您同意{" "}
                      <button type="button" className="underline underline-offset-2 hover:text-foreground transition-colors" onClick={() => setLegalDialog("terms")}>用户协议</button>
                      {" "}和{" "}
                      <button type="button" className="underline underline-offset-2 hover:text-foreground transition-colors" onClick={() => setLegalDialog("privacy")}>隐私政策</button>
                    </p>
                  </form>
                </m.div>
              )}
            </AnimatePresence>
          </CardContent>
        </Card>
      </m.div>

      {legalDialog && <LegalDialog type={legalDialog} open onOpenChange={(open) => { if (!open) setLegalDialog(null); }} />}
    </LazyMotion>
  );
}

/* ── 错误横幅 ── */
function ErrorBanner({ error }: { error: string | null }) {
  return (
    <AnimatePresence initial={false}>
      {error ? (
        <m.div
          key="error"
          initial={{ opacity: 0, height: 0, y: -4 }}
          animate={{ opacity: 1, height: "auto", y: 0, x: [0, -5, 5, -3, 3, 0] }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.35, ease: "easeOut" }}
          className="overflow-hidden"
        >
          <div className="flex items-start gap-2.5 rounded-lg border border-destructive/25 bg-destructive/8 px-3 py-2.5 text-sm text-destructive">
            <ShieldAlert className="size-4 shrink-0 mt-0.5" />
            <span className="leading-snug">{error}</span>
          </div>
        </m.div>
      ) : null}
    </AnimatePresence>
  );
}

/* ── SSO 登录 ── */
function SSOButton() {
  const [oidcEnabled, setOidcEnabled] = useState<boolean | null>(null);
  const [ssoLoading, setSsoLoading] = useState(false);

  useState(() => { getOIDCPublicConfig().then((r) => setOidcEnabled(r.enabled)).catch(() => setOidcEnabled(false)); });
  if (!oidcEnabled) return null;

  const handleSSO = async () => {
    setSsoLoading(true);
    try {
      const { url } = await getOIDCAuthURL();
      window.location.href = url;
    } catch {
      setSsoLoading(false);
    }
  };

  return (
    <>
      <div className="relative my-0.5">
        <div className="absolute inset-0 flex items-center"><Separator className="opacity-50" /></div>
        <div className="relative flex justify-center">
          <span className="bg-card px-3 text-[10px] font-medium text-muted-foreground/50 uppercase tracking-widest select-none">或</span>
        </div>
      </div>
      <Button type="button" variant="outline" className="h-10 w-full gap-2 font-medium text-muted-foreground hover:text-foreground border-border/50" onClick={handleSSO} disabled={ssoLoading}>
        {ssoLoading ? <LoaderCircle className="size-4 animate-spin" /> : <ShieldCheck className="size-4" />}
        {ssoLoading ? "正在跳转..." : "使用 SSO 登录"}
      </Button>
    </>
  );
}
