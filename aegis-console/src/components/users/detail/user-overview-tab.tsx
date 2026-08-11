"use client";

import {
  CalendarClock,
  Coins,
  Crown,
  Fingerprint,
  Globe2,
  IdCard,
  KeyRound,
  Link2,
  Mail,
  MapPin,
  Phone,
  Sparkles,
  UserRound,
  Wallet
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AdminAppUserDetail, UserWallet } from "@/lib/api/types";
import {
  EMPTY,
  Fact,
  Facts,
  Panel,
  StatTile,
  formatMoney,
  formatTime,
  hasWallet,
  isPast,
  joinText,
  numberText,
  numberValue,
  relativeTime,
  textValue
} from "./user-detail-shared";

/**
 * 概览：这个用户是谁、值多少、从哪来。
 *
 * 刻意**不**放操作。所有会改变状态的动作都在各自的页签里，
 * 概览页混进按钮会让人在只想看一眼的时候误触。
 */
export function UserOverviewTab({
  user,
  wallet,
  onNavigate
}: {
  user: AdminAppUserDetail;
  wallet?: UserWallet;
  onNavigate: (tab: string) => void;
}) {
  const profile = user.profile;
  const security = user.security;
  const vipActive = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);
  const walletReady = hasWallet(wallet);

  const credentials = [
    security?.hasPassword ? "密码" : null,
    security?.twoFactorEnabled ? "二次验证" : null,
    security?.passkeyEnabled ? "Passkey" : null,
    numberValue(security?.oauth2Bindings) > 0
      ? `第三方 ${numberValue(security?.oauth2Bindings)}`
      : null
  ].filter(Boolean) as string[];

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatTile
          label="积分"
          value={numberText(user.integral)}
          icon={<Coins className="size-3.5" />}
          hint="可在「资产」页签调整"
        />
        <StatTile
          label="经验"
          value={numberText(user.experience)}
          icon={<Sparkles className="size-3.5" />}
          hint="等级由经验推导"
        />
        <StatTile
          label="钱包余额"
          value={walletReady ? `¥ ${formatMoney(wallet?.balance)}` : EMPTY}
          icon={<Wallet className="size-3.5" />}
          hint={walletReady ? `冻结 ¥ ${formatMoney(wallet?.frozen)}` : "钱包数据不可用"}
        />
        <StatTile
          label="会员"
          value={vipActive ? relativeTime(user.vipExpireAt) : user.vipExpireAt ? "已过期" : "非会员"}
          icon={<Crown className="size-3.5" />}
          tone={vipActive ? "success" : user.vipExpireAt ? "warning" : "default"}
          hint={user.vipExpireAt ? `到期 ${formatTime(user.vipExpireAt)}` : "从未开通"}
        />
      </div>

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel title="账户档案" icon={<UserRound className="size-4" />}>
          <Facts>
            <Fact label="账号" value={textValue(user.account)} mono />
            <Fact label="用户 ID" value={String(user.id)} mono />
            <Fact label="昵称" value={textValue(user.nickname || profile?.nickname)} />
            <Fact
              label="邮箱"
              icon={<Mail className="size-3" />}
              value={textValue(user.email || profile?.email)}
            />
            <Fact
              label="手机"
              icon={<Phone className="size-3" />}
              value={textValue(user.phone || profile?.phone)}
            />
            <Fact
              label="自定义 ID"
              icon={<IdCard className="size-3" />}
              value={textValue(profile?.customId)}
              hint={
                typeof profile?.customIdCount === "number"
                  ? `已修改 ${profile.customIdCount} 次`
                  : undefined
              }
            />
            <Fact label="邀请码" value={textValue(user.inviteCode || profile?.inviteCode)} mono />
            <Fact
              label="上级邀请人"
              icon={<Link2 className="size-3" />}
              value={textValue(profile?.parentInviteAccount)}
            />
            <Fact label="设备标识码" value={textValue(user.markcode || profile?.markcode)} mono />
            <Fact
              label="创建时间"
              value={formatTime(user.createdAt)}
              hint={user.createdAt ? relativeTime(user.createdAt) : undefined}
            />
          </Facts>
        </Panel>

        <div className="space-y-5">
          <Panel title="注册来源" icon={<MapPin className="size-4" />}>
            <Facts>
              <Fact
                label="注册时间"
                icon={<CalendarClock className="size-3" />}
                value={formatTime(user.registerTime || profile?.registerTime)}
                hint={
                  user.registerTime || profile?.registerTime
                    ? relativeTime(user.registerTime || profile?.registerTime)
                    : undefined
                }
              />
              <Fact
                label="注册 IP"
                icon={<Globe2 className="size-3" />}
                value={textValue(user.registerIP || profile?.registerIp)}
                mono
              />
              <Fact
                label="归属地"
                value={joinText([
                  user.registerProvince || profile?.registerProvince,
                  user.registerCity || profile?.registerCity
                ])}
              />
              <Fact label="运营商" value={textValue(user.registerIsp || profile?.registerIsp)} />
            </Facts>
          </Panel>

          <Panel
            title="登录方式"
            icon={<KeyRound className="size-4" />}
            description="任一项可用即可登录；全部缺失时账号本人也进不来。"
            action={
              <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => onNavigate("security")}>
                安全详情
              </Button>
            }
          >
            {credentials.length ? (
              <div className="flex flex-wrap gap-1.5">
                {credentials.map((item) => (
                  <Badge key={item} variant="success" size="sm">
                    {item}
                  </Badge>
                ))}
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400">
                <Fingerprint className="size-4" />
                没有任何可用的登录凭据
              </div>
            )}
            {security?.oauth2Providers?.length ? (
              <div className="mt-3 flex flex-wrap items-center gap-1.5 border-t pt-3">
                <span className="text-xs text-muted-foreground">第三方渠道</span>
                {security.oauth2Providers.map((provider) => (
                  <Badge key={provider} variant="outline" size="sm">
                    {provider}
                  </Badge>
                ))}
              </div>
            ) : null}
          </Panel>
        </div>
      </div>

      {textValue(profile?.bio, "") || profile?.contacts?.length ? (
        <Panel title="用户自述" icon={<UserRound className="size-4" />}>
          <div className="grid gap-5 lg:grid-cols-[1.2fr_0.8fr]">
            <div>
              <div className="text-xs text-muted-foreground">个人简介</div>
              <p className="mt-2 whitespace-pre-wrap text-sm leading-6">
                {textValue(profile?.bio)}
              </p>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">联系方式</div>
              {profile?.contacts?.length ? (
                <Facts className="mt-1">
                  {profile.contacts.map((contact, index) => (
                    <Fact
                      key={`${contact.platform}-${index}`}
                      label={textValue(contact.label || contact.platform, "联系项")}
                      value={textValue(contact.value)}
                    />
                  ))}
                </Facts>
              ) : (
                <div className="mt-2 text-sm text-muted-foreground">{EMPTY}</div>
              )}
            </div>
          </div>
        </Panel>
      ) : null}
    </div>
  );
}
