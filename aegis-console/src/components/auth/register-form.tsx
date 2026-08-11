"use client";

import { FormEvent, useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { AlertCircle, LoaderCircle, UserPlus } from "lucide-react";
import { z } from "zod";
import { LazyMotion, domAnimation, m, AnimatePresence } from "motion/react";
import { registerAdmin, ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import { useAdminCaptchaPublicConfigQuery } from "@/lib/admin-hooks";
import { CaptchaField } from "./captcha-field";
import { LegalDialog } from "@/components/legal/legal-dialog";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";

const registerSchema = z.object({
  account: z.string().min(3, "账号至少 3 个字符").max(64, "账号最多 64 个字符"),
  password: z.string().min(8, "密码至少 8 个字符").max(72, "密码最多 72 个字符"),
  confirmPassword: z.string(),
  displayName: z.string().optional(),
  // zod v4：字符串格式校验改为顶层函数（z.string().email() 已废弃，顶层写法可被 tree-shake）
  email: z.email("邮箱格式不正确").optional().or(z.literal(""))
}).refine((data) => data.password === data.confirmPassword, {
  message: "两次输入的密码不一致",
  path: ["confirmPassword"]
});

export function RegisterForm() {
  const router = useRouter();
  const setSession = useAuthStore((state) => state.setSession);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [captchaId, setCaptchaId] = useState("");
  const [agreeTerms, setAgreeTerms] = useState(false);
  const [legalDialog, setLegalDialog] = useState<"terms" | "privacy" | null>(null);
  const captchaConfigQuery = useAdminCaptchaPublicConfigQuery();
  const captchaConfig = captchaConfigQuery.data;
  const needCaptcha = captchaConfig?.enabled && captchaConfig?.requireForRegister;

  const handleCaptchaIdChange = useCallback((id: string) => setCaptchaId(id), []);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const formData = new FormData(event.currentTarget);
    const parsed = registerSchema.safeParse({
      account: formData.get("account"),
      password: formData.get("password"),
      confirmPassword: formData.get("confirmPassword"),
      displayName: formData.get("displayName") || "",
      email: formData.get("email") || ""
    });

    if (!parsed.success) {
      setError(parsed.error.issues[0]?.message || "表单校验失败");
      return;
    }

    const captchaAnswer = String(formData.get("captchaAnswer") || "").trim();
    if (needCaptcha && (!captchaId || !captchaAnswer)) {
      setError("请输入验证码");
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

      if (result.accessToken) {
        setSession({
          accessToken: result.accessToken,
          refreshToken: result.refreshToken,
          operator: result.operator
        });
        router.replace("/overview");
      } else {
        router.replace("/login");
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "注册失败，请稍后重试");
    } finally {
      setLoading(false);
    }
  }

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ opacity: 0, x: 24 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.55, ease: [0.22, 1, 0.36, 1] }}
        className="w-full max-w-[420px]"
      >
        <Card className="border-border/60 shadow-sm">
          <CardHeader className="pb-4">
            <div className="mb-3 flex items-center justify-between">
              <div className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-border bg-muted">
                <AegisMark className="size-5 text-foreground" />
              </div>
              <Badge variant="secondary" className="text-[10px] font-medium tracking-wide">
                <UserPlus className="mr-1 size-3" />
                注册
              </Badge>
            </div>
            <CardTitle className="text-2xl font-semibold tracking-tight">创建管理员账号</CardTitle>
            <CardDescription>注册后可创建应用并获得完整管理权限</CardDescription>
          </CardHeader>

          <Separator />

          <CardContent className="pt-5">
            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <div className="grid gap-2">
                <Label htmlFor="account">账号</Label>
                <Input id="account" name="account" autoComplete="username" placeholder="3-64 个字符" />
              </div>

              <div className="grid gap-2">
                <Label htmlFor="displayName">显示名称</Label>
                <Input id="displayName" name="displayName" placeholder="可选" />
              </div>

              <div className="grid gap-2">
                <Label htmlFor="email">邮箱</Label>
                <Input id="email" name="email" type="email" autoComplete="email" placeholder="可选" />
              </div>

              <div className="grid gap-2">
                <Label htmlFor="password">密码</Label>
                <Input id="password" name="password" type="password" autoComplete="new-password" placeholder="至少 8 个字符" />
              </div>

              <div className="grid gap-2">
                <Label htmlFor="confirmPassword">确认密码</Label>
                <Input id="confirmPassword" name="confirmPassword" type="password" autoComplete="new-password" placeholder="再次输入密码" />
              </div>

              {needCaptcha && captchaConfig ? (
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
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={{ opacity: 0, height: 0 }}
                    transition={{ duration: 0.22, ease: "easeInOut" }}
                    className="overflow-hidden"
                  >
                    <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2.5 text-sm text-destructive">
                      <AlertCircle className="size-4 shrink-0" />
                      <span>{error}</span>
                    </div>
                  </m.div>
                ) : null}
              </AnimatePresence>

              <div className="flex items-start gap-2">
                <Checkbox
                  id="agreeTerms"
                  checked={agreeTerms}
                  onCheckedChange={(v) => setAgreeTerms(v === true)}
                  className="mt-0.5"
                />
                <label htmlFor="agreeTerms" className="text-xs leading-relaxed text-muted-foreground cursor-pointer select-none">
                  我已阅读并同意{" "}
                  <button type="button" className="text-foreground underline underline-offset-2 hover:text-primary" onClick={() => setLegalDialog("terms")}>用户协议</button>
                  {" "}和{" "}
                  <button type="button" className="text-foreground underline underline-offset-2 hover:text-primary" onClick={() => setLegalDialog("privacy")}>隐私政策</button>
                </label>
              </div>

              <Button className="mt-1 h-11 font-medium" size="lg" type="submit" disabled={loading || !agreeTerms}>
                {loading ? <LoaderCircle className="mr-2 size-4 animate-spin" /> : null}
                {loading ? "注册中" : "注册并进入控制台"}
              </Button>

              <div className="text-center text-sm text-muted-foreground">
                已有账号？{" "}
                <Link href="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
                  返回登录
                </Link>
              </div>
            </form>
          </CardContent>
        </Card>
      </m.div>

      {legalDialog && (
        <LegalDialog type={legalDialog} open onOpenChange={(open) => { if (!open) setLegalDialog(null); }} />
      )}
    </LazyMotion>
  );
}
