"use client";

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Check, Loader2, ShieldAlert, ShieldCheck } from "lucide-react";
import { exchangeOIDCTicket } from "@/lib/api/system";
import { useAuthStore } from "@/lib/auth-store";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

type Phase = "loading" | "success" | "error";

export default function OIDCCallbackPage() {
  return (
    <Suspense fallback={<CallbackLoading />}>
      <OIDCCallbackContent />
    </Suspense>
  );
}

function CallbackLoading() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm border-border/50 shadow-lg">
        <CardContent className="flex flex-col items-center gap-4 py-10 px-6">
          <div className="flex size-12 items-center justify-center rounded-xl border border-border bg-muted/60">
            <AegisMark className="size-6 text-foreground" />
          </div>
          <Loader2 className="size-8 animate-spin text-muted-foreground" />
          <p className="text-sm text-muted-foreground">正在加载…</p>
        </CardContent>
      </Card>
    </div>
  );
}

function OIDCCallbackContent() {
  const params = useSearchParams();
  const router = useRouter();
  const setSession = useAuthStore((s) => s.setSession);
  const [phase, setPhase] = useState<Phase>("loading");
  const [error, setError] = useState<string>("");

  useEffect(() => {
    const ticket = params.get("ticket");
    const errMsg = params.get("error");

    if (errMsg) { setError(errMsg); setPhase("error"); return; }
    if (!ticket) { setError("缺少认证凭据"); setPhase("error"); return; }

    exchangeOIDCTicket(ticket)
      .then((result) => {
        if (result.requiresSecondFactor && result.challenge) {
          const q = new URLSearchParams({
            mfa: "1",
            challengeId: result.challenge.challengeId,
            methods: (result.challenge.methods || []).join(","),
          });
          router.replace(`/login?${q.toString()}`);
          return;
        }
        if (result.accessToken && result.admin) {
          setSession({
            accessToken: result.accessToken,
            refreshToken: null,
            operator: {
              id: result.admin.id,
              account: result.admin.account,
              displayName: result.admin.displayName,
              avatar: result.admin.avatar,
              isSuperAdmin: result.admin.isSuperAdmin,
              authSource: result.admin.authSource,
              role: result.admin.isSuperAdmin ? "super-admin" : "admin",
              assignments: result.assignments,
            },
          });
          setPhase("success");
          setTimeout(() => router.replace("/overview"), 600);
        } else {
          setError("SSO 认证响应不完整");
          setPhase("error");
        }
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "SSO 认证失败");
        setPhase("error");
      });
  }, [params, router, setSession]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm border-border/50 shadow-lg">
        <CardContent className="flex flex-col items-center gap-4 py-10 px-6">
          {/* 品牌标识 */}
          <div className="flex size-12 items-center justify-center rounded-xl border border-border bg-muted/60">
            <AegisMark className="size-6 text-foreground" />
          </div>

          {phase === "loading" && (
            <>
              <div className="relative">
                <Loader2 className="size-8 animate-spin text-muted-foreground" />
                <div className="absolute inset-0 animate-ping">
                  <Loader2 className="size-8 text-muted-foreground/20" />
                </div>
              </div>
              <div className="text-center space-y-1">
                <p className="text-sm font-medium">正在完成 SSO 认证</p>
                <p className="text-xs text-muted-foreground">请稍候，正在验证身份信息...</p>
              </div>
              {/* 进度条模拟 */}
              <div className="w-full h-1 rounded-full bg-muted overflow-hidden">
                <div className="h-full w-[70%] bg-primary/60 rounded-full animate-pulse" />
              </div>
            </>
          )}

          {phase === "success" && (
            <>
              <div className="flex size-14 items-center justify-center rounded-full bg-emerald-500/15 text-emerald-600 dark:text-emerald-400">
                <Check className="size-7" strokeWidth={2.5} />
              </div>
              <div className="text-center space-y-1">
                <p className="text-sm font-medium text-emerald-600 dark:text-emerald-400">认证成功</p>
                <p className="text-xs text-muted-foreground">正在进入控制台...</p>
              </div>
            </>
          )}

          {phase === "error" && (
            <>
              <div className="flex size-14 items-center justify-center rounded-full bg-destructive/10 text-destructive">
                <ShieldAlert className="size-7" />
              </div>
              <div className="text-center space-y-1">
                <p className="text-sm font-semibold">SSO 登录失败</p>
                <p className="text-xs text-muted-foreground leading-relaxed max-w-60">{error}</p>
              </div>
              <div className="flex gap-2 mt-2">
                <Button variant="outline" size="sm" onClick={() => router.replace("/login")}>返回登录</Button>
                <Button size="sm" onClick={() => { setPhase("loading"); window.location.reload(); }}>重试</Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
