"use client";

import { FormEvent, useCallback, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Check, Eye, EyeOff, LoaderCircle, ShieldAlert, X } from "lucide-react";
import { z } from "zod";
import { AnimatePresence, m, useReducedMotion } from "motion/react";
import { registerAdmin, ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminCaptchaPublicConfigQuery } from "@/lib/admin-hooks";
import { CaptchaField } from "./captcha-field";
import { AUTH_EASE } from "./auth-motion";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const registerSchema = z
  .object({
    account: z.string().min(3, "账号至少 3 个字符").max(64, "账号最多 64 个字符"),
    password: z.string().min(8, "密码至少 8 个字符").max(72, "密码最多 72 个字符"),
    confirmPassword: z.string(),
    displayName: z.string().max(64, "显示名称最多 64 个字符").optional(),
    // zod v4：字符串格式校验改为顶层函数（z.string().email() 已废弃，顶层写法可被 tree-shake）
    email: z.email("邮箱格式不正确").optional().or(z.literal(""))
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: "两次输入的密码不一致",
    path: ["confirmPassword"]
  });

type FieldName = "account" | "password" | "confirmPassword" | "displayName" | "email";
type FieldErrors = Partial<Record<FieldName, string>>;

const EMPTY_FORM: Record<FieldName, string> = {
  account: "",
  password: "",
  confirmPassword: "",
  displayName: "",
  email: ""
};

/**
 * 密码强度：只用**这个表单自己能验证的东西**算分。
 *
 * 服务端用的是 zxcvbn（猜测次数估算 + 中文语境弱口令表），前端不复刻那一套：
 * 复刻一份必然和服务端算出不同的分，于是会出现「这里显示很强、提交后被拒」——
 * 那比没有强度提示更糟。这里画的是长度与字符多样性，措辞也只说"长度 / 组成"，
 * 不冒充最终判定。真正的裁决在服务端，被拒时错误会原样显示在上方。
 */
function passwordScore(value: string): { percent: number; label: string; tone: string } {
  if (!value) return { percent: 0, label: "", tone: "" };

  const variety =
    Number(/[a-z]/.test(value)) +
    Number(/[A-Z]/.test(value)) +
    Number(/\d/.test(value)) +
    Number(/[^\w\s]/.test(value));
  const lengthScore = Math.min(value.length / 16, 1);
  const percent = Math.round(Math.min(lengthScore * 0.6 + (variety / 4) * 0.4, 1) * 100);

  if (value.length < 8) return { percent: Math.max(percent, 8), label: "太短", tone: "text-destructive" };
  if (percent < 45) return { percent, label: "偏弱", tone: "text-amber-600 dark:text-amber-400" };
  if (percent < 75) return { percent, label: "一般", tone: "text-amber-600 dark:text-amber-400" };
  return { percent, label: "较强", tone: "text-emerald-600 dark:text-emerald-400" };
}

/**
 * 管理员注册表单。
 *
 * 与登录页共用同一张卡片形状：同一套 shadcn 原语、同一条缓动曲线、同一个成功态。
 * 两个页面互为跳转目标，长得不一样会让人怀疑自己点错了地方。
 *
 * 校验落到**每个字段自己下面**，不再是顶部一条只显示第一个问题的横幅 ——
 * 旧版账号和密码同时不合规时，改完一个才发现还有另一个。
 */
export function RegisterForm() {
  const router = useRouter();
  const reduced = useReducedMotion();
  const setSession = useAuthStore((state) => state.setSession);

  const [form, setForm] = useState(EMPTY_FORM);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [done, setDone] = useState(false);
  const [captchaId, setCaptchaId] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [agreeTerms, setAgreeTerms] = useState(false);

  const captchaConfigQuery = useAdminCaptchaPublicConfigQuery();
  const captchaConfig = captchaConfigQuery.data;
  const needCaptcha = Boolean(captchaConfig?.enabled && captchaConfig?.requireForRegister);

  const handleCaptchaIdChange = useCallback((id: string) => setCaptchaId(id), []);

  // 改哪个字段就清哪个字段的错，不要等到下次提交才消失
  const patch = useCallback((name: FieldName, value: string) => {
    setForm((current) => ({ ...current, [name]: value }));
    setFieldErrors((current) => (current[name] ? { ...current, [name]: undefined } : current));
  }, []);

  const strength = useMemo(() => passwordScore(form.password), [form.password]);
  const confirmState = !form.confirmPassword
    ? "idle"
    : form.confirmPassword === form.password
      ? "match"
      : "mismatch";

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const parsed = registerSchema.safeParse(form);
    if (!parsed.success) {
      // 一次把所有问题都摊开，而不是只报第一个
      const next: FieldErrors = {};
      for (const issue of parsed.error.issues) {
        const key = issue.path[0] as FieldName | undefined;
        if (key && !next[key]) next[key] = issue.message;
      }
      setFieldErrors(next);
      return;
    }

    const captchaAnswer = String(new FormData(event.currentTarget).get("captchaAnswer") || "").trim();
    if (needCaptcha && (!captchaId || !captchaAnswer)) {
      setError("请先完成验证码");
      return;
    }

    setLoading(true);
    try {
      const result = await registerAdmin({
        account: parsed.data.account,
        password: parsed.data.password,
        displayName: parsed.data.displayName || undefined,
        email: parsed.data.email || undefined,
        captchaId: needCaptcha ? captchaId : undefined,
        captchaAnswer: needCaptcha ? captchaAnswer : undefined
      });

      setDone(true);
      if (result.accessToken) {
        setSession({
          accessToken: result.accessToken,
          refreshToken: result.refreshToken,
          operator: result.operator
        });
        setTimeout(() => router.replace("/overview"), 650);
      } else {
        // 没直接签发令牌时后端要求走一次登录，说清去向再跳
        setTimeout(() => router.replace("/login"), 900);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "注册失败，请稍后重试");
      if (cause instanceof ApiError) setFieldErrors({});
    } finally {
      setLoading(false);
    }
  }

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
            <AnimatePresence mode="wait" initial={false}>
              <m.div
                key={done ? "done" : "form"}
                initial={reduced ? false : { opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                exit={reduced ? undefined : { opacity: 0, y: -5 }}
                transition={{ duration: 0.2, ease: AUTH_EASE }}
                className="flex flex-col gap-1.5"
              >
                <CardTitle className="text-xl font-semibold tracking-tight">
                  {done ? "账号已创建" : "创建管理员账号"}
                </CardTitle>
                <CardDescription className="text-[13px]">
                  {done ? "正在进入控制台" : "注册后即可创建应用并获得管理权限"}
                </CardDescription>
              </m.div>
            </AnimatePresence>
          </CardHeader>

          <CardContent className="px-6 pt-0 pb-7">
            <AnimatePresence mode="wait" initial={false}>
              {done ? (
                <m.div
                  key="done"
                  initial={reduced ? false : { opacity: 0 }}
                  animate={{ opacity: 1 }}
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
              ) : (
                <m.form
                  key="form"
                  initial={reduced ? false : { opacity: 0 }}
                  animate={{ opacity: 1 }}
                  exit={reduced ? undefined : { opacity: 0 }}
                  transition={{ duration: 0.2 }}
                  onSubmit={handleSubmit}
                  className="flex flex-col gap-4"
                  noValidate
                >
                  <Field label="账号" htmlFor="account" error={fieldErrors.account} reduced={reduced}>
                    <Input
                      id="account"
                      name="account"
                      autoComplete="username"
                      placeholder="3–64 个字符"
                      value={form.account}
                      onChange={(event) => patch("account", event.target.value)}
                      aria-invalid={Boolean(fieldErrors.account)}
                      className="h-10"
                      autoFocus
                    />
                  </Field>

                  <Field label="密码" htmlFor="password" error={fieldErrors.password} reduced={reduced}>
                    <div className="relative">
                      <Input
                        id="password"
                        name="password"
                        type={showPassword ? "text" : "password"}
                        autoComplete="new-password"
                        placeholder="至少 8 个字符"
                        value={form.password}
                        onChange={(event) => patch("password", event.target.value)}
                        aria-invalid={Boolean(fieldErrors.password)}
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

                    {/* 强度条只在开始输入后出现：空密码给一条空槽等于先扣一分 */}
                    <AnimatePresence initial={false}>
                      {form.password ? (
                        <m.div
                          initial={reduced ? false : { opacity: 0, height: 0 }}
                          animate={{ opacity: 1, height: "auto" }}
                          exit={reduced ? undefined : { opacity: 0, height: 0 }}
                          transition={{ duration: 0.22, ease: AUTH_EASE }}
                          className="overflow-hidden"
                        >
                          <div className="flex items-center gap-2 pt-2">
                            <Progress value={strength.percent} className="h-1 flex-1" />
                            <span className={cn("w-8 shrink-0 text-right text-[11px]", strength.tone)}>
                              {strength.label}
                            </span>
                          </div>
                          <p className="pt-1 text-[11px] text-muted-foreground/70">
                            按长度与字符组成估算，最终强度以服务端判定为准
                          </p>
                        </m.div>
                      ) : null}
                    </AnimatePresence>
                  </Field>

                  <Field
                    label="确认密码"
                    htmlFor="confirmPassword"
                    error={fieldErrors.confirmPassword}
                    reduced={reduced}
                  >
                    <div className="relative">
                      <Input
                        id="confirmPassword"
                        name="confirmPassword"
                        type={showPassword ? "text" : "password"}
                        autoComplete="new-password"
                        placeholder="再次输入密码"
                        value={form.confirmPassword}
                        onChange={(event) => patch("confirmPassword", event.target.value)}
                        aria-invalid={Boolean(fieldErrors.confirmPassword) || confirmState === "mismatch"}
                        className="h-10 pr-9"
                      />
                      {/* 一致与否当场给结论 —— 这件事不需要等提交才知道 */}
                      <AnimatePresence initial={false}>
                        {confirmState === "idle" ? null : (
                          <m.span
                            key={confirmState}
                            initial={reduced ? false : { opacity: 0, scale: 0.6 }}
                            animate={{ opacity: 1, scale: 1 }}
                            exit={reduced ? undefined : { opacity: 0, scale: 0.6 }}
                            transition={{ duration: 0.18 }}
                            className={cn(
                              "absolute top-1/2 right-3 -translate-y-1/2",
                              confirmState === "match"
                                ? "text-emerald-600 dark:text-emerald-400"
                                : "text-destructive"
                            )}
                          >
                            {confirmState === "match" ? (
                              <Check className="size-4" strokeWidth={2.5} />
                            ) : (
                              <X className="size-4" strokeWidth={2.5} />
                            )}
                          </m.span>
                        )}
                      </AnimatePresence>
                    </div>
                  </Field>

                  <Field label="显示名称" htmlFor="displayName" optional error={fieldErrors.displayName} reduced={reduced}>
                    <Input
                      id="displayName"
                      name="displayName"
                      placeholder="控制台里显示的名字"
                      value={form.displayName}
                      onChange={(event) => patch("displayName", event.target.value)}
                      aria-invalid={Boolean(fieldErrors.displayName)}
                      className="h-10"
                    />
                  </Field>

                  <Field label="邮箱" htmlFor="email" optional error={fieldErrors.email} reduced={reduced}>
                    <Input
                      id="email"
                      name="email"
                      type="email"
                      autoComplete="email"
                      placeholder="用于接收通知与凭证"
                      value={form.email}
                      onChange={(event) => patch("email", event.target.value)}
                      aria-invalid={Boolean(fieldErrors.email)}
                      className="h-10"
                    />
                  </Field>

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

                  <div className="flex items-start gap-2.5">
                    <Checkbox
                      id="agreeTerms"
                      checked={agreeTerms}
                      onCheckedChange={(value) => setAgreeTerms(value === true)}
                      className="mt-0.5"
                    />
                    <Label
                      htmlFor="agreeTerms"
                      className="text-[13px] leading-5 font-normal text-muted-foreground"
                    >
                      我已阅读并同意{" "}
                      <Link
                        href="/legal/terms"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-foreground underline underline-offset-2 transition-colors hover:text-primary"
                      >
                        用户协议
                      </Link>{" "}
                      与{" "}
                      <Link
                        href="/legal/privacy"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-foreground underline underline-offset-2 transition-colors hover:text-primary"
                      >
                        隐私政策
                      </Link>
                    </Label>
                  </div>

                  <Button type="submit" className="mt-1 h-10 w-full" disabled={loading || !agreeTerms}>
                    {loading ? <LoaderCircle className="size-4 animate-spin" /> : null}
                    {loading ? "创建中…" : "创建账号"}
                  </Button>

                  <p className="text-center text-[13px] text-muted-foreground">
                    已有账号？{" "}
                    <Link
                      href="/login"
                      className="font-medium text-foreground underline-offset-4 transition-colors hover:underline"
                    >
                      返回登录
                    </Link>
                  </p>
                </m.form>
              )}
            </AnimatePresence>
          </CardContent>
        </Card>
      </m.div>

    </>
  );
}

/**
 * 单个字段：标签（可选标记）+ 控件 + 该字段自己的报错。
 *
 * 错误从 0 高度展开，把下面的内容推开 —— 位移本身就是"这里出问题了"的信号，
 * 比只把边框染红更难被略过。
 */
function Field({
  label,
  htmlFor,
  optional,
  error,
  reduced,
  children
}: {
  label: string;
  htmlFor: string;
  optional?: boolean;
  error?: string;
  reduced: boolean | null;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-2">
      <div className="flex items-baseline justify-between">
        <Label htmlFor={htmlFor}>{label}</Label>
        {optional ? <span className="text-[11px] text-muted-foreground/60">可选</span> : null}
      </div>
      {children}
      <AnimatePresence initial={false}>
        {error ? (
          <m.p
            initial={reduced ? false : { opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={reduced ? undefined : { opacity: 0, height: 0 }}
            transition={{ duration: 0.2, ease: AUTH_EASE }}
            className="overflow-hidden text-[12px] text-destructive"
          >
            {error}
          </m.p>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
