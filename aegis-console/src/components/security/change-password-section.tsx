"use client";

import { FormEvent, useMemo, useState } from "react";
import { Check, Eye, EyeOff, KeySquare, LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api-client";
import { adminPasswordError, passwordScore } from "@/lib/admin-account-rules";
import { useAdminProfileQuery, useAdminSecurityStatusQuery, useChangeAdminPasswordMutation } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";

function fmtDate(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  return isNaN(date.getTime())
    ? ""
    : date.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

/**
 * 修改登录密码（账户安全页的一个折叠项）。
 *
 * 规则与后端一致：8–72 位、同时含字母和数字、不含用户名、不能与当前密码相同。
 * 默认同时下线其他设备：改密码最常见的原因就是怀疑密码泄露，此时保留别处的会话没有意义。
 * 当前会话不受影响，改完不用重新登录。
 * 开启了两步验证的账号还须输入验证码，验证器不在手边时可改用恢复码（一次性）。
 */
export function ChangePasswordSection() {
  const profileQ = useAdminProfileQuery();
  const account = profileQ.data?.account;
  const securityQ = useAdminSecurityStatusQuery();
  const twoFactor = Boolean(securityQ.data?.twoFactorEnabled);
  const mutation = useChangeAdminPasswordMutation();

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [reveal, setReveal] = useState(false);
  const [signOutOthers, setSignOutOthers] = useState(true);
  const [useRecovery, setUseRecovery] = useState(false);
  const [secondFactor, setSecondFactor] = useState("");
  const [error, setError] = useState<string | null>(null);

  const strength = useMemo(() => passwordScore(next), [next]);
  const ruleError = next ? adminPasswordError(next, account?.account || "") : null;
  const sameAsCurrent = Boolean(next && current && next === current);
  const confirmState = !confirm ? "idle" : confirm === next ? "match" : "mismatch";
  const secondFactorReady = !twoFactor || (useRecovery ? secondFactor.trim().length > 0 : /^\d{6}$/.test(secondFactor.trim()));
  const canSubmit = Boolean(
    current && next && !ruleError && !sameAsCurrent && confirmState === "match" && secondFactorReady
  );

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmit) return;
    setError(null);
    try {
      const factor = secondFactor.trim();
      const result = await mutation.mutateAsync({
        currentPassword: current,
        newPassword: next,
        signOutOthers,
        code: twoFactor && !useRecovery ? factor : undefined,
        recoveryCode: twoFactor && useRecovery ? factor : undefined
      });
      toast.success("密码已修改", {
        description: result.revokedSessions > 0 ? `已下线其他设备上的 ${result.revokedSessions} 个会话` : undefined
      });
      setCurrent("");
      setNext("");
      setConfirm("");
      setReveal(false);
      setSecondFactor("");
      setUseRecovery(false);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "修改失败，请稍后重试");
    }
  }

  const external = account?.canChangePassword === false;
  const changedAt = fmtDate(account?.passwordChangedAt);

  return (
    <AccordionItem value="password" className="rounded-xl border px-4">
      <AccordionTrigger className="py-3 text-sm font-semibold hover:no-underline">
        <span className="flex items-center gap-2">
          <KeySquare className="size-4 text-muted-foreground" />
          登录密码
          {changedAt ? (
            <Badge variant="outline" className="ml-2 font-normal">
              修改于 {changedAt}
            </Badge>
          ) : null}
        </span>
      </AccordionTrigger>
      <AccordionContent className="pb-4">
        {profileQ.isLoading ? (
          <div className="space-y-3">
            <div className="h-9 animate-pulse rounded-md bg-muted" />
            <div className="h-9 animate-pulse rounded-md bg-muted" />
          </div>
        ) : external ? (
          <p className="text-sm text-muted-foreground">该账号由外部身份源管理，请在对应身份源中修改密码。</p>
        ) : (
          <form className="max-w-md space-y-4" onSubmit={handleSubmit} noValidate>
            {/* 帮助密码管理器把新密码记到正确的账号下 */}
            <input type="text" name="username" autoComplete="username" value={account?.account || ""} readOnly hidden />

            <div className="space-y-1.5">
              <Label htmlFor="pw-current" className="text-xs">当前密码</Label>
              <Input
                id="pw-current"
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(event) => {
                  setCurrent(event.target.value);
                  setError(null);
                }}
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="pw-new" className="text-xs">新密码</Label>
              <div className="relative">
                <Input
                  id="pw-new"
                  type={reveal ? "text" : "password"}
                  autoComplete="new-password"
                  value={next}
                  placeholder="至少 8 位，含字母和数字"
                  className="pr-10"
                  aria-invalid={Boolean(ruleError || sameAsCurrent)}
                  onChange={(event) => {
                    setNext(event.target.value);
                    setError(null);
                  }}
                />
                <button
                  type="button"
                  onClick={() => setReveal((value) => !value)}
                  className="absolute top-1/2 right-2 inline-flex size-7 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                  aria-label={reveal ? "隐藏密码" : "显示密码"}
                >
                  {reveal ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              </div>
              {next ? (
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <Progress value={strength.percent} className="h-1.5 flex-1" />
                    <span className={cn("w-8 text-right text-[11px]", strength.tone)}>{strength.label}</span>
                  </div>
                  {ruleError || sameAsCurrent ? (
                    <p className="text-xs text-destructive">{sameAsCurrent ? "新密码不能与当前密码相同" : ruleError}</p>
                  ) : null}
                </div>
              ) : null}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="pw-confirm" className="text-xs">确认新密码</Label>
              <div className="relative">
                <Input
                  id="pw-confirm"
                  type={reveal ? "text" : "password"}
                  autoComplete="new-password"
                  value={confirm}
                  className="pr-10"
                  aria-invalid={confirmState === "mismatch"}
                  onChange={(event) => setConfirm(event.target.value)}
                />
                {confirmState === "match" ? (
                  <Check className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-emerald-500" />
                ) : null}
              </div>
              {confirmState === "mismatch" ? <p className="text-xs text-destructive">两次输入的密码不一致</p> : null}
            </div>

            {twoFactor ? (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label htmlFor="pw-2fa" className="text-xs">{useRecovery ? "恢复码" : "两步验证码"}</Label>
                  <button
                    type="button"
                    className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                    onClick={() => {
                      setUseRecovery((value) => !value);
                      setSecondFactor("");
                      setError(null);
                    }}
                  >
                    {useRecovery ? "改用验证码" : "改用恢复码"}
                  </button>
                </div>
                <Input
                  id="pw-2fa"
                  value={secondFactor}
                  autoComplete="one-time-code"
                  inputMode={useRecovery ? "text" : "numeric"}
                  maxLength={useRecovery ? 64 : 6}
                  placeholder={useRecovery ? "输入一枚未使用的恢复码" : "验证器中的 6 位数字"}
                  className="font-mono tracking-wider placeholder:font-sans placeholder:tracking-normal"
                  onChange={(event) => {
                    setSecondFactor(useRecovery ? event.target.value : event.target.value.replace(/\D/g, ""));
                    setError(null);
                  }}
                />
                <p className="text-xs text-muted-foreground">
                  {useRecovery ? "每枚恢复码只能使用一次，使用后即失效。" : "已开启两步验证，修改密码需要验证身份。"}
                </p>
              </div>
            ) : null}

            <label className="flex cursor-pointer items-center justify-between gap-4 rounded-lg border px-3 py-2.5">
              <span className="min-w-0">
                <span className="block text-sm">同时退出其他设备</span>
                <span className="block text-xs text-muted-foreground">当前会话保持登录</span>
              </span>
              <Switch checked={signOutOthers} onCheckedChange={setSignOutOthers} />
            </label>

            {error ? <p className="text-sm text-destructive">{error}</p> : null}

            <Button type="submit" size="sm" disabled={!canSubmit || mutation.isPending}>
              {mutation.isPending ? <LoaderCircle className="animate-spin" /> : null}
              修改密码
            </Button>
          </form>
        )}
      </AccordionContent>
    </AccordionItem>
  );
}
