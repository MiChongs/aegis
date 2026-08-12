"use client";

import Link from "next/link";
import { Clock, Fingerprint, IdCard, Lock, ShieldAlert, ShieldCheck, TriangleAlert } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { EMPTY, Fact, Facts, Panel, formatTime, isPast, relativeTime } from "@/components/users/detail/user-detail-shared";
import { CopyableValue, authSourceLabel } from "@/components/profile/profile-shared";
import type { AdminAccount, AdminLoginAvailability, AdminSession } from "@/lib/api/types";

/**
 * 账户与会话。
 *
 * 这一屏全是**只读事实**，所以刻意不与上面的表单重叠：能改的在表单里改，
 * 改不了的在这里看。旧版把邮箱 / 手机号 / 生日在右栏又列了一遍，
 * 于是编辑中的草稿和服务端旧值同屏打架。
 */
export function ProfileAccountPanel({
  account,
  session,
  sessionLoading,
  loginAvailability
}: {
  account: AdminAccount;
  session?: AdminSession;
  sessionLoading: boolean;
  loginAvailability?: AdminLoginAvailability;
}) {
  const active = account.status === "active" || !account.status;
  const expired = isPast(session?.expiresAt);

  return (
    <div className="space-y-4">
      {/* 认证源探测失败时必须显式说出来：否则下次登录被挡住，人只会看到一句"账号或密码错误" */}
      {loginAvailability && !loginAvailability.available ? (
        <Alert variant="destructive">
          <ShieldAlert />
          <AlertTitle>你的登录方式当前不可用</AlertTitle>
          <AlertDescription>
            <p>
              账号绑定在 {authSourceLabel(loginAvailability.source)} 上，平台探测到它连不通
              {loginAvailability.reason ? `：${loginAvailability.reason}` : "。"}
            </p>
            <p className="text-xs">
              当前会话不受影响，但<strong className="font-semibold">退出后可能登不回来</strong>。
              请在会话过期前联系另一位超级管理员，或到「平台配置」修好该认证源。
              {loginAvailability.checkedAt ? `（探测于 ${formatTime(loginAvailability.checkedAt)}）` : null}
            </p>
          </AlertDescription>
        </Alert>
      ) : null}

      {session?.fallbackToken ? (
        <Alert>
          <TriangleAlert />
          <AlertTitle>当前会话来自静态管理令牌</AlertTitle>
          <AlertDescription>
            这条会话由 <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">ADMIN_API_TOKEN</code>{" "}
            直接签发，不对应任何管理员账号，因此没有角色、没有操作留痕、也无法被单独吊销。
            它是给自动化与救急用的，日常操作请用自己的账号登录。
          </AlertDescription>
        </Alert>
      ) : null}

      <Panel
        title="账户信息"
        icon={<IdCard className="size-4" />}
        description="账号与认证方式由平台管理员分配，这里只能看，改不了。"
        bodyClassName="px-5 py-2"
      >
        <Facts>
          <Fact label="账号 ID" value={<CopyableValue value={String(account.id)} />} />
          <Fact label="登录账号" value={<CopyableValue value={account.account} />} />
          <Fact
            label="认证方式"
            icon={<Lock className="size-3" />}
            value={<Badge variant="outline" size="sm">{authSourceLabel(account.authSource)}</Badge>}
            hint={account.authSource && account.authSource !== "password" ? "密码由外部身份源托管，控制台改不了" : undefined}
          />
          <Fact
            label="账号状态"
            value={
              <Badge variant={active ? "success" : "danger"} size="sm">
                {active ? "正常" : account.status || "异常"}
              </Badge>
            }
          />
          <Fact
            label="权限级别"
            value={account.isSuperAdmin ? "超级管理员" : "普通管理员"}
            tone={account.isSuperAdmin ? "warning" : "default"}
          />
          <Fact label="创建时间" value={formatTime(account.createdAt)} hint={relativeTime(account.createdAt)} />
          <Fact label="资料更新" value={formatTime(account.updatedAt)} hint={relativeTime(account.updatedAt)} />
          <Fact
            label="最近登录"
            icon={<Clock className="size-3" />}
            value={formatTime(account.lastLoginAt)}
            hint={relativeTime(account.lastLoginAt)}
          />
        </Facts>
      </Panel>

      <Panel
        title="当前会话"
        icon={<Fingerprint className="size-4" />}
        description="你现在这一次登录。要看所有设备上的会话并逐个下线，去账户安全页。"
        action={
          <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs" asChild>
            <Link href="/security" prefetch>
              <ShieldCheck className="size-3.5" />
              管理会话
            </Link>
          </Button>
        }
        bodyClassName="px-5 py-2"
      >
        {sessionLoading ? (
          <div className="space-y-2 py-3">
            <Skeleton className="h-5 w-full" />
            <Skeleton className="h-5 w-2/3" />
          </div>
        ) : session ? (
          <Facts>
            <Fact label="签发时间" value={formatTime(session.issuedAt)} hint={relativeTime(session.issuedAt)} />
            <Fact
              label="过期时间"
              value={formatTime(session.expiresAt)}
              hint={expired ? "已过期" : `${relativeTime(session.expiresAt)}失效`}
              tone={expired ? "danger" : "default"}
            />
            <Fact
              label="会话标识"
              value={session.tokenId ? <CopyableValue value={session.tokenId} /> : EMPTY}
              hint="排查登录问题时把它发给平台管理员"
            />
          </Facts>
        ) : (
          <p className="py-6 text-center text-sm text-muted-foreground">拿不到会话信息</p>
        )}
      </Panel>
    </div>
  );
}
