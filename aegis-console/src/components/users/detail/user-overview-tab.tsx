"use client";

/**
 * 概览：这个用户是谁、手里有什么、最近做了什么。
 *
 * 不放会改变状态的操作；每一块右上角都是「去哪里看全」的入口：
 * 指标卡点进流水，最近流水点进流水页，最近登录点进活动页，登录方式点进安全页。
 */

import {
  ArrowRight,
  CalendarClock,
  Coins,
  Crown,
  Fingerprint,
  Globe2,
  History,
  IdCard,
  KeyRound,
  Link2,
  Mail,
  MapPin,
  MessageSquareText,
  Phone,
  Receipt,
  ShieldCheck,
  Smartphone,
  Sparkles,
  UserRound,
  Wallet
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { AdminAppUserDetail, UserLoginAuditRecord, UserWallet } from "@/lib/api/types";
import type { PointTransaction } from "@/lib/api/user-ledger";
import { useAdminUserLoginAuditsQuery } from "@/lib/app-user-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useUserPointLedgerQuery, useUserPointSummaryQuery } from "@/lib/user-ledger-hooks";
import { InfoGrid, MetricCard, Section, SignedAmount, StateBlock, ToneIcon, errorMessage, formatInt } from "./kit";
import { categoryLabel, fillDays } from "./ledger/ledger-shared";
import {
  LoginStatusBadge,
  describeUserAgent,
  formatMoney,
  hasWallet,
  isPast,
  joinText,
  numberValue,
  textValue,
  walletSplitLabel
} from "./user-detail-shared";

type Navigate = (tab: string, sub?: string) => void;

export function UserOverviewTab({
  appKey,
  userId,
  user,
  wallet,
  onNavigate
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  wallet?: UserWallet;
  onNavigate: Navigate;
}) {
  const profile = user.profile;
  const vipLifetime = Boolean(user.vipLifetimeAt);
  const vipActive = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);
  const walletReady = hasWallet(wallet);

  const integralSummary = useUserPointSummaryQuery(appKey, userId, "integral", 30);
  const experienceSummary = useUserPointSummaryQuery(appKey, userId, "experience", 30);
  const spark = (summary: typeof integralSummary.data) =>
    summary ? fillDays(summary.daily, summary.days, summary.balanceBefore).map((d) => d.balance) : undefined;

  const registerTime = user.registerTime || profile?.registerTime;

  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        <MetricCard
          label="积分"
          value={formatInt(user.integral)}
          icon={Coins}
          tone="warning"
          spark={spark(integralSummary.data)}
          sub={integralSummary.data ? `30 天净变动 ${netText(integralSummary.data)}` : "近 30 天"}
          onClick={() => onNavigate("ledger", "integral")}
        />
        <MetricCard
          label="经验"
          value={formatInt(user.experience)}
          icon={Sparkles}
          tone="violet"
          spark={spark(experienceSummary.data)}
          sub={experienceSummary.data ? `30 天净变动 ${netText(experienceSummary.data)}` : "近 30 天"}
          onClick={() => onNavigate("ledger", "experience")}
        />
        <MetricCard
          label="钱包余额"
          value={walletReady ? `¥ ${formatMoney(wallet?.balance)}` : "—"}
          icon={Wallet}
          tone="success"
          sub={walletReady ? (walletSplitLabel(wallet) ?? `冻结 ¥ ${formatMoney(wallet?.frozen)}`) : "钱包数据不可用"}
          onClick={() => onNavigate("ledger", "wallet")}
        />
        <MetricCard
          label="会员"
          value={vipLifetime ? "永久会员" : vipActive ? formatRelative(user.vipExpireAt) : user.vipExpireAt ? "已过期" : "非会员"}
          icon={Crown}
          tone={vipLifetime || vipActive ? "primary" : "neutral"}
          sub={
            vipLifetime
              ? `开通于 ${formatDateTime(user.vipLifetimeAt)}`
              : user.vipExpireAt
                ? `到期 ${formatDateTime(user.vipExpireAt)}`
                : "从未开通"
          }
          onClick={() => onNavigate("ledger", "vip")}
        />
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
        <Section
          title="账户档案"
          icon={UserRound}
          tone="primary"
          actions={
            <Button size="sm" variant="ghost" onClick={() => onNavigate("profile")}>
              编辑
              <ArrowRight />
            </Button>
          }
        >
          <InfoGrid
            items={[
              { label: "账号", value: textValue(user.account, ""), mono: true, copy: textValue(user.account, "") || null },
              { label: "昵称", value: textValue(user.nickname || profile?.nickname, "") },
              { label: "邮箱", icon: Mail, value: textValue(user.email || profile?.email, ""), copy: textValue(user.email || profile?.email, "") || null },
              { label: "手机", icon: Phone, value: textValue(user.phone || profile?.phone, ""), copy: textValue(user.phone || profile?.phone, "") || null },
              {
                label: "自定义 ID",
                icon: IdCard,
                value: textValue(profile?.customId, ""),
                hint: typeof profile?.customIdCount === "number" ? `已修改 ${profile.customIdCount} 次` : undefined
              },
              { label: "邀请码", value: textValue(user.inviteCode || profile?.inviteCode, ""), mono: true, copy: textValue(user.inviteCode || profile?.inviteCode, "") || null },
              { label: "上级邀请人", icon: Link2, value: textValue(profile?.parentInviteAccount, "") },
              { label: "设备标识码", icon: Smartphone, value: textValue(user.markcode || profile?.markcode, ""), mono: true, copy: textValue(user.markcode || profile?.markcode, "") || null },
              ...(textValue(profile?.bio, "")
                ? [{ label: "个人简介", icon: MessageSquareText, value: <span className="whitespace-pre-wrap">{profile?.bio}</span>, wide: true }]
                : []),
              ...(profile?.contacts?.length
                ? profile.contacts.map((contact) => ({
                    label: textValue(contact.label || contact.platform, "联系方式"),
                    value: textValue(contact.value, "")
                  }))
                : [])
            ]}
          />
        </Section>

        <div className="flex flex-col gap-5">
          <Section title="注册来源" icon={MapPin} tone="info">
            <InfoGrid
              items={[
                { label: "注册时间", icon: CalendarClock, value: formatDateTime(registerTime, ""), hint: registerTime ? formatRelative(registerTime) : undefined },
                { label: "注册 IP", icon: Globe2, value: textValue(user.registerIp || profile?.registerIp, ""), mono: true, copy: textValue(user.registerIp || profile?.registerIp, "") || null },
                {
                  label: "归属地",
                  value: joinText([user.registerProvince || profile?.registerProvince, user.registerCity || profile?.registerCity], "")
                },
                { label: "运营商", value: textValue(user.registerIsp || profile?.registerIsp, "") }
              ]}
            />
          </Section>
          <CredentialsSection user={user} onNavigate={onNavigate} />
        </div>
      </div>

      <div className="grid gap-5 xl:grid-cols-2">
        <RecentLedger appKey={appKey} userId={userId} onNavigate={onNavigate} />
        <RecentLogins appKey={appKey} userId={userId} onNavigate={onNavigate} />
      </div>
    </div>
  );
}

function netText(summary: { daily: { in: number; out: number }[] | null }) {
  const net = (summary.daily ?? []).reduce((sum, d) => sum + d.in - d.out, 0);
  return `${net > 0 ? "+" : ""}${net.toLocaleString("zh-CN")}`;
}

function CredentialsSection({ user, onNavigate }: { user: AdminAppUserDetail; onNavigate: Navigate }) {
  const security = user.security;
  const bindings = numberValue(security?.oauth2Bindings);
  const items = [
    { label: "密码", on: Boolean(security?.hasPassword), icon: KeyRound },
    { label: "二次验证", on: Boolean(security?.twoFactorEnabled), icon: ShieldCheck },
    { label: "通行密钥", on: Boolean(security?.passkeyEnabled), icon: Fingerprint },
    { label: bindings ? `第三方 ${bindings}` : "第三方", on: bindings > 0, icon: Link2 }
  ];
  const none = !security?.hasPassword && !security?.passkeyEnabled && bindings === 0;
  return (
    <Section
      title="登录方式"
      icon={KeyRound}
      tone={none ? "danger" : "success"}
      description={none ? "没有任何可用凭据，本人也无法登录" : undefined}
      actions={
        <Button size="sm" variant="ghost" onClick={() => onNavigate("security")}>
          安全详情
          <ArrowRight />
        </Button>
      }
    >
      <div className="grid grid-cols-2 gap-2">
        {items.map(({ label, on, icon }) => (
          <div key={label} className="flex items-center gap-2.5 rounded-lg border px-3 py-2">
            <ToneIcon icon={icon} tone={on ? "success" : "neutral"} size="sm" />
            <span className="min-w-0 flex-1 truncate text-sm">{label}</span>
            <Badge variant={on ? "success" : "outline"} size="sm">
              {on ? "已启用" : "未启用"}
            </Badge>
          </div>
        ))}
      </div>
      {security?.oauth2Providers?.length ? (
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted-foreground">第三方渠道</span>
          {security.oauth2Providers.map((provider) => (
            <Badge key={provider} variant="outline" size="sm">
              {provider}
            </Badge>
          ))}
        </div>
      ) : null}
    </Section>
  );
}

function RecentLedger({ appKey, userId, onNavigate }: { appKey: string; userId: number; onNavigate: Navigate }) {
  const query = useUserPointLedgerQuery(appKey, userId, "integral", { page: 1, limit: 6 });
  const items = query.data?.items ?? [];
  return (
    <Section
      title="最近积分流水"
      icon={Receipt}
      tone="warning"
      flush
      actions={
        <Button size="sm" variant="ghost" onClick={() => onNavigate("ledger", "integral")}>
          全部流水
          <ArrowRight />
        </Button>
      }
    >
      {query.isLoading ? (
        <ListSkeleton />
      ) : query.isError ? (
        <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => query.refetch()} compact />
      ) : !items.length ? (
        <StateBlock title="还没有积分流水" compact />
      ) : (
        <ul className="divide-y">
          {items.map((txn: PointTransaction) => (
            <li key={txn.id} className="flex items-center gap-3 px-4 py-2.5 sm:px-5">
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-sm font-medium">{txn.title || categoryLabel(txn.category)}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {categoryLabel(txn.category, txn.title)}，{formatRelative(txn.createdAt)}
                </span>
              </div>
              <div className="flex shrink-0 flex-col items-end">
                <SignedAmount value={txn.amount} />
                <span className="text-xs text-muted-foreground tabular-nums">余 {formatInt(txn.balanceAfter)}</span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function RecentLogins({ appKey, userId, onNavigate }: { appKey: string; userId: number; onNavigate: Navigate }) {
  const query = useAdminUserLoginAuditsQuery(appKey, userId, { page: 1, limit: 6 });
  const items = query.data?.items ?? [];
  return (
    <Section
      title="最近登录"
      icon={History}
      tone="info"
      flush
      actions={
        <Button size="sm" variant="ghost" onClick={() => onNavigate("activity")}>
          全部记录
          <ArrowRight />
        </Button>
      }
    >
      {query.isLoading ? (
        <ListSkeleton />
      ) : query.isError ? (
        <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => query.refetch()} compact />
      ) : !items.length ? (
        <StateBlock title="还没有登录记录" compact />
      ) : (
        <ul className="divide-y">
          {items.map((item: UserLoginAuditRecord) => {
            const place = textValue(item.location, "") || joinText([item.country, item.region, item.city], "");
            const device = item.deviceInfo?.marketingName || describeUserAgent(item.userAgent).label;
            return (
              <li key={item.id} className="flex items-center gap-3 px-4 py-2.5 sm:px-5">
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate font-mono text-[13px]">{item.loginIp || "—"}</span>
                    {place ? <span className="truncate text-xs text-muted-foreground">{place}</span> : null}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {[item.provider || item.loginType, device].filter(Boolean).join("，")}
                  </span>
                </div>
                <div className="flex shrink-0 flex-col items-end gap-0.5">
                  <LoginStatusBadge status={item.status} />
                  <span className="text-xs text-muted-foreground" title={formatDateTime(item.createdAt)}>
                    {formatRelative(item.createdAt)}
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}

function ListSkeleton() {
  return (
    <div className="flex flex-col divide-y">
      {[0, 1, 2, 3, 4].map((i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3 sm:px-5">
          <div className="flex flex-1 flex-col gap-1.5">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3 w-24" />
          </div>
          <Skeleton className="h-4 w-14" />
        </div>
      ))}
    </div>
  );
}
