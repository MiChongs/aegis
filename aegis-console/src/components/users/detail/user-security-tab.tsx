"use client";

import { useState } from "react";
import Link from "next/link";
import {
  BadgeCheck,
  CircleAlert,
  Fingerprint,
  Gauge,
  KeyRound,
  Link2,
  Loader2,
  Lock,
  MonitorSmartphone,
  RotateCcw,
  ShieldCheck,
  Unlock
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import {
  useAdminAppLoginBaselineQuery,
  useAdminAppPolicyQuery,
  useResetAdminAppLoginBaselineMutation
} from "@/lib/admin-hooks";
import type { AdminAppUserDetail } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  EMPTY,
  Fact,
  Facts,
  Panel,
  boolText,
  formatTime,
  isPast,
  numberText,
  numberValue,
  relativeTime,
  textValue
} from "./user-detail-shared";
import { UserResetPasswordDialog } from "../user-reset-password-dialog";

type Credential = {
  key: string;
  label: string;
  icon: React.ReactNode;
  active: boolean;
  detail: string;
};

/** 强度分刻度（0~100）与后端 `password_strength_score` 同一把尺子。 */
function strengthTone(score?: number) {
  if (typeof score !== "number" || score <= 0) return { label: "未评估", tone: "muted" as const };
  if (score < 40) return { label: "弱", tone: "danger" as const };
  if (score < 60) return { label: "一般", tone: "warning" as const };
  if (score < 80) return { label: "良好", tone: "success" as const };
  return { label: "强", tone: "success" as const };
}

/**
 * 安全：这个账号怎么证明"他是他"，以及这些证明现在还好不好使。
 *
 * 顶部是**凭据矩阵**——四种登录方式一行摆开，亮着的就是能用的。
 * 旧版把这些拆成十几个「已设密码：是 / Passkey：已开启」的字段行，
 * 得逐条读完才能回答"这人能不能登进来"。
 */
export function UserSecurityTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const security = user.security;
  const [showResetPassword, setShowResetPassword] = useState(false);

  const policyQuery = useAdminAppPolicyQuery(appKey);
  const baselineQuery = useAdminAppLoginBaselineQuery(appKey, userId);
  const resetBaseline = useResetAdminAppLoginBaselineMutation(appKey);

  const policy = policyQuery.data;
  const baseline = baselineQuery.data;
  // 三项强绑定全关时不产生任何 Redis I/O，基线也就无从谈起 —— 面板照此说明。
  const bindingEnabled = Boolean(
    policy?.loginCheckDevice || policy?.loginCheckIp || policy?.loginCheckUser
  );

  const oauthCount = numberValue(security?.oauth2Bindings);
  const strength = strengthTone(security?.passwordStrengthScore);
  const passwordExpired = isPast(security?.passwordExpiresAt);

  const credentials: Credential[] = [
    {
      key: "password",
      label: "密码",
      icon: <Lock className="size-4" />,
      active: Boolean(security?.hasPassword),
      detail: security?.hasPassword
        ? passwordExpired
          ? "已过期"
          : `强度 ${numberText(security?.passwordStrengthScore)} · ${strength.label}`
        : "未设置"
    },
    {
      key: "totp",
      label: "二次验证",
      icon: <ShieldCheck className="size-4" />,
      active: Boolean(security?.twoFactorEnabled),
      detail: security?.twoFactor?.pendingSetup
        ? "配置未完成"
        : security?.twoFactorEnabled
          ? textValue(security.twoFactorMethod, "TOTP")
          : "未开启"
    },
    {
      key: "passkey",
      label: "Passkey",
      icon: <Fingerprint className="size-4" />,
      active: Boolean(security?.passkeyEnabled),
      detail: security?.passkeyEnabled
        ? `${numberValue(security.passkeys?.count)} 个凭据`
        : "未注册"
    },
    {
      key: "oauth",
      label: "第三方",
      icon: <Link2 className="size-4" />,
      active: oauthCount > 0,
      detail: oauthCount > 0 ? security?.oauth2Providers?.join(" / ") || `${oauthCount} 个` : "未绑定"
    }
  ];

  async function handleResetBaseline() {
    try {
      await resetBaseline.mutateAsync(userId);
      toast.success("登录绑定已重置", { description: "用户下次登录会重新建立基线。" });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "重置失败");
    }
  }

  return (
    <>
      <div className="space-y-5">
        <Panel
          title="凭据矩阵"
          icon={<KeyRound className="size-4" />}
          description="亮起的即为当前可用的登录方式。四项全暗时账号本人也登不进来。"
          action={
            <Button size="sm" variant="outline" onClick={() => setShowResetPassword(true)}>
              <KeyRound className="size-3.5" />
              重置密码
            </Button>
          }
        >
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {credentials.map((item) => (
              <div
                key={item.key}
                className={cn(
                  "rounded-2xl border px-4 py-3 transition-colors",
                  item.active
                    ? "border-emerald-200 bg-emerald-50/50 dark:border-emerald-900/60 dark:bg-emerald-950/25"
                    : "border-dashed bg-muted/20"
                )}
              >
                <div
                  className={cn(
                    "flex items-center gap-2 text-sm font-medium",
                    item.active ? "text-emerald-700 dark:text-emerald-300" : "text-muted-foreground"
                  )}
                >
                  {item.icon}
                  {item.label}
                </div>
                <div className="mt-1.5 truncate text-xs text-muted-foreground" title={item.detail}>
                  {item.detail}
                </div>
              </div>
            ))}
          </div>
        </Panel>

        <div className="grid gap-5 xl:grid-cols-2">
          <Panel title="密码生命周期" icon={<Gauge className="size-4" />}>
            <div className="space-y-4">
              <div>
                <div className="flex items-baseline justify-between">
                  <span className="text-xs text-muted-foreground">强度分</span>
                  <span className="text-sm tabular-nums">
                    {numberText(security?.passwordStrengthScore)}
                    <span className="ml-1.5 text-xs text-muted-foreground">{strength.label}</span>
                  </span>
                </div>
                <Progress
                  value={Math.max(0, Math.min(100, numberValue(security?.passwordStrengthScore)))}
                  className="mt-2 h-1.5"
                />
                <p className="mt-2 text-[11px] leading-4 text-muted-foreground">
                  分值在设置密码时算一次并落库。把应用的策略门槛调严不会追溯已有密码，
                  只有用户下次改密才会重算。
                </p>
              </div>
              <Facts>
                <Fact
                  label="最近变更"
                  value={formatTime(security?.passwordChangedAt)}
                  hint={
                    security?.passwordChangedAt ? relativeTime(security.passwordChangedAt) : undefined
                  }
                />
                <Fact
                  label="过期时间"
                  value={
                    security?.passwordExpiresAt ? formatTime(security.passwordExpiresAt) : "永不过期"
                  }
                  tone={passwordExpired ? "danger" : "default"}
                  hint={
                    security?.passwordExpiresAt
                      ? passwordExpired
                        ? "已过期，下次登录强制改密"
                        : relativeTime(security.passwordExpiresAt)
                      : undefined
                  }
                />
                <Fact
                  label="强制改密标记"
                  value={boolText(security?.passwordChangeRequired, "已标记", "无")}
                  tone={security?.passwordChangeRequired ? "warning" : "default"}
                />
              </Facts>
            </div>
          </Panel>

          <Panel title="二次验证与恢复码" icon={<ShieldCheck className="size-4" />}>
            <Facts>
              <Fact
                label="TOTP"
                value={
                  <Badge
                    variant={
                      security?.twoFactor?.pendingSetup
                        ? "warning"
                        : security?.twoFactor?.enabled
                          ? "success"
                          : "outline"
                    }
                    size="sm"
                  >
                    {security?.twoFactor?.pendingSetup
                      ? "配置未完成"
                      : security?.twoFactor?.enabled
                        ? "已启用"
                        : "未启用"}
                  </Badge>
                }
              />
              <Fact label="Issuer" value={textValue(security?.twoFactor?.issuer)} />
              <Fact label="账户名" value={textValue(security?.twoFactor?.accountName)} mono />
              <Fact label="启用时间" value={formatTime(security?.twoFactor?.enabledAt)} />
              <Fact
                label="最近校验"
                value={formatTime(security?.twoFactor?.lastVerifiedAt)}
                hint={
                  security?.twoFactor?.lastVerifiedAt
                    ? relativeTime(security.twoFactor.lastVerifiedAt)
                    : undefined
                }
              />
              <Fact
                label="恢复码"
                value={
                  security?.recoveryCodes?.enabled
                    ? `${numberValue(security.recoveryCodes.remaining)} / ${numberValue(security.recoveryCodes.total)} 可用`
                    : "未生成"
                }
                tone={
                  security?.recoveryCodes?.enabled && numberValue(security.recoveryCodes.remaining) <= 2
                    ? "warning"
                    : "default"
                }
                hint={
                  security?.recoveryCodes?.generatedAt
                    ? `生成于 ${formatTime(security.recoveryCodes.generatedAt)}`
                    : undefined
                }
              />
            </Facts>
          </Panel>
        </div>

        <div className="grid gap-5 xl:grid-cols-2">
          <Panel
            title="登录绑定基线"
            icon={<MonitorSmartphone className="size-4" />}
            description="开启设备 / IP / 属地强绑定后，判定依据是这个用户上一次被放行的登录指纹。"
            action={
              baseline?.bound ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={resetBaseline.isPending}
                  onClick={handleResetBaseline}
                >
                  {resetBaseline.isPending ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <RotateCcw className="size-3.5" />
                  )}
                  重置绑定
                </Button>
              ) : null
            }
          >
            {policyQuery.isLoading || baselineQuery.isLoading ? (
              <Skeleton className="h-24 w-full rounded-xl" />
            ) : !bindingEnabled ? (
              <div className="flex items-start gap-2 rounded-xl border border-dashed bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
                <Unlock className="mt-0.5 size-4 shrink-0" />
                <span>
                  该应用未开启任何强绑定策略（设备 / IP / 属地），登录不受基线约束，
                  这里也不会有数据。策略在{" "}
                  <Link
                    href={`/apps/${encodeURIComponent(appKey)}?tab=policy`}
                    className="text-foreground underline underline-offset-2"
                  >
                    应用 · 认证与会话
                  </Link>{" "}
                  里配置。
                </span>
              </div>
            ) : !baseline?.bound ? (
              <div className="flex items-start gap-2 rounded-xl border border-dashed bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
                <CircleAlert className="mt-0.5 size-4 shrink-0" />
                <span>尚未建立基线。用户下一次成功登录时会写入，之后即以那次为准。</span>
              </div>
            ) : (
              <>
                <div className="mb-3 flex flex-wrap gap-1.5">
                  {policy?.loginCheckDevice ? (
                    <Badge variant="info" size="sm">
                      设备绑定
                    </Badge>
                  ) : null}
                  {policy?.loginCheckIp ? (
                    <Badge variant="info" size="sm">
                      IP 网段
                    </Badge>
                  ) : null}
                  {policy?.loginCheckUser ? (
                    <Badge variant="info" size="sm">
                      登录属地
                    </Badge>
                  ) : null}
                </div>
                <Facts>
                  <Fact label="设备标识" value={textValue(baseline.baseline?.deviceId)} mono />
                  <Fact label="IP" value={textValue(baseline.baseline?.ip)} mono />
                  <Fact label="属地" value={textValue(baseline.baseline?.region)} />
                  <Fact label="设备绑定时间" value={formatTime(baseline.baseline?.deviceBoundAt)} />
                  <Fact label="基线更新时间" value={formatTime(baseline.baseline?.updatedAt)} />
                </Facts>
                <p className="mt-3 border-t pt-3 text-[11px] leading-4 text-muted-foreground">
                  重置是唯一的解绑出口。用户换宽带 / 换手机后登不上，处理方式就是在这里重置一次。
                </p>
              </>
            )}
          </Panel>

          <Panel
            title="Passkey"
            icon={<Fingerprint className="size-4" />}
            action={
              <Badge variant="outline" size="sm">
                {numberValue(security?.passkeys?.count)} 个
              </Badge>
            }
          >
            {security?.passkeys?.items?.length ? (
              <div className="space-y-2">
                {security.passkeys.items.map((item) => (
                  <div key={`${item.id}-${item.credentialId}`} className="rounded-xl border px-3 py-2.5">
                    <div className="flex items-center justify-between gap-3">
                      <span className="truncate text-sm font-medium">
                        {textValue(item.credentialName, `凭据 #${item.id}`)}
                      </span>
                      <span className="shrink-0 text-[11px] text-muted-foreground">
                        使用 {numberText(item.signCount)} 次
                      </span>
                    </div>
                    <div
                      className="mt-1 truncate font-mono text-[11px] text-muted-foreground"
                      title={item.credentialId}
                    >
                      {item.credentialId}
                    </div>
                    <div className="mt-1 text-[11px] text-muted-foreground">
                      最近使用 {formatTime(item.lastUsedAt)} · 注册于 {formatTime(item.createdAt)}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="py-6 text-center text-sm text-muted-foreground">未注册任何 Passkey</div>
            )}
          </Panel>
        </div>

        {security?.modules?.length ? (
          <Panel
            title="认证模块运行态"
            icon={<BadgeCheck className="size-4" />}
            description="平台侧能力开关。「已启用但未就绪」意味着配置不全，该方式实际不可用。"
          >
            <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
              {security.modules.map((module) => {
                const healthy = module.enabled && module.ready;
                return (
                  <div key={module.key} className="rounded-xl border px-3 py-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium">{module.name}</span>
                      <Badge
                        variant={healthy ? "success" : module.enabled ? "warning" : "outline"}
                        size="sm"
                      >
                        {healthy ? "就绪" : module.enabled ? "未就绪" : "未启用"}
                      </Badge>
                    </div>
                    <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                      {module.key}
                    </div>
                    {module.message ? (
                      <div className="mt-1 text-[11px] leading-4 text-muted-foreground">
                        {module.message}
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </Panel>
        ) : null}

        {!security ? (
          <div className="py-10 text-center text-sm text-muted-foreground">{EMPTY}</div>
        ) : null}
      </div>

      <UserResetPasswordDialog
        open={showResetPassword}
        onOpenChange={setShowResetPassword}
        appKey={appKey}
        userId={userId}
      />
    </>
  );
}
