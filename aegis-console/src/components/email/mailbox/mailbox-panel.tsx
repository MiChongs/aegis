"use client";

import { useState } from "react";
import { AtSign, ExternalLink, Inbox, KeyRound, Mails, Send, ShieldCheck, TriangleAlert, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import type { EmailConfig } from "@/lib/api/types";
import type { EmailScope } from "@/lib/email-hooks";
import { useSkymailQuery } from "@/lib/email-skymail-hooks";
import { cn } from "@/lib/utils";
import { AccountsView, RegKeysView, UsersView } from "./mailbox-admin";
import { AllMailView, MailFolderView } from "./mailbox-mail";
import { EmptyNote, ErrorNote } from "./mailbox-shared";

/**
 * 邮局：在控制台里直接管 Cloud Mail 通道背后的那套邮局。
 *
 * | 页 | 谁能看 | 对应接口 |
 * |---|---|---|
 * | 收件箱 / 已发送 | 所有账号 | email/list、email/send、email/delete |
 * | 邮箱账号 | 所有账号 | account/list、account/add、account/delete |
 * | 用户 / 全部邮件 / 注册码 | 邮局管理员（permKeys 含 *） | user/*、allEmail/*、regKey/* |
 *
 * 凭据就是通道配置里的登录邮箱与密码，不需要再登录一次；能做什么由那个邮局账号的权限决定。
 */

const TABS = [
  { key: "inbox", label: "收件箱", icon: Inbox, admin: false },
  { key: "sent", label: "已发送", icon: Send, admin: false },
  { key: "accounts", label: "邮箱账号", icon: AtSign, admin: false },
  { key: "users", label: "用户", icon: Users, admin: true },
  { key: "all", label: "全部邮件", icon: Mails, admin: true },
  { key: "regkeys", label: "注册码", icon: KeyRound, admin: true }
] as const;

type TabKey = (typeof TABS)[number]["key"];

export function MailboxPanel({ scope, configs, canWrite }: { scope: EmailScope; configs: EmailConfig[]; canWrite: boolean }) {
  const [configId, setConfigId] = useState<number>(() => (configs.find((item) => item.enabled) ?? configs[0])?.id ?? 0);
  const current = configs.find((item) => item.id === configId) ?? configs[0];

  if (!current) {
    return (
      <div className="rounded-2xl border bg-card">
        <EmptyNote icon={<Mails className="size-6" />} title="还没有 Cloud Mail 通道" hint="在「通道」里新建一条 Cloud Mail 配置，这里就能管理它的邮局" />
      </div>
    );
  }
  // 换通道时整块重建：各页的游标、选择、筛选都属于上一个邮局
  return (
    <MailboxBody
      key={current.id}
      scope={scope}
      config={current}
      configs={configs}
      canWrite={canWrite}
      onSwitch={setConfigId}
    />
  );
}

function MailboxBody({
  scope,
  config,
  configs,
  canWrite,
  onSwitch
}: {
  scope: EmailScope;
  config: EmailConfig;
  configs: EmailConfig[];
  canWrite: boolean;
  onSwitch: (id: number) => void;
}) {
  const [tab, setTab] = useState<TabKey>("inbox");
  const overviewQuery = useSkymailQuery(scope, config.id, "overview", {});
  const overview = overviewQuery.data;
  const tabs = TABS.filter((item) => !item.admin || overview?.isAdmin);
  const ctx = overview ? { scope, configId: config.id, overview, canWrite } : null;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 rounded-2xl border bg-card px-4 py-3">
        {configs.length > 1 ? (
          <Select value={String(config.id)} onValueChange={(value) => onSwitch(Number(value))}>
            <SelectTrigger className="h-8 w-48 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {configs.map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>
                  {item.name || `通道 #${item.id}`}
                  {item.enabled ? "" : "（已停用）"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <span className="text-sm font-medium">{config.name || `通道 #${config.id}`}</span>
        )}
        {overviewQuery.isLoading ? (
          <Skeleton className="h-5 w-64" />
        ) : overview ? (
          <>
            <div className="min-w-0">
              <p className="flex items-center gap-1.5 truncate text-sm">
                <span className="font-mono text-xs">{overview.profile.email}</span>
                {overview.isAdmin ? (
                  <Badge variant="info" size="sm" className="gap-1">
                    <ShieldCheck className="size-3" /> 邮局管理员
                  </Badge>
                ) : (
                  <Badge variant="outline" size="sm">
                    {overview.profile.role?.name || "普通用户"}
                  </Badge>
                )}
              </p>
              <p className="text-[11px] text-muted-foreground">
                已发件 {overview.profile.sendCount} 次
                {overview.profile.role?.sendCount
                  ? `，上限 ${overview.profile.role.sendCount}${overview.profile.role.sendType === "day" ? " / 天" : "（总量）"}`
                  : overview.isAdmin
                    ? "，不限次数"
                    : ""}
                · 发信地址 {overview.senderAddress}
              </p>
            </div>
            <a
              href={overview.baseUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            >
              打开邮局 <ExternalLink className="size-3" />
            </a>
          </>
        ) : null}
      </div>

      {overviewQuery.error ? <ErrorNote error={overviewQuery.error} onRetry={() => void overviewQuery.refetch()} /> : null}
      {overview?.senderError ? (
        <div className="flex items-start gap-2 rounded-xl border border-amber-300/60 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-800/60 dark:bg-amber-950/40 dark:text-amber-200">
          <TriangleAlert className="mt-0.5 size-4 shrink-0" />
          <p>这条通道现在发不了信：{overview.senderError}</p>
        </div>
      ) : null}

      {ctx ? (
        <>
          <div className="flex flex-wrap items-center gap-1.5">
            {tabs.map((item) => (
              <button
                key={item.key}
                type="button"
                onClick={() => setTab(item.key)}
                className={cn(
                  "flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                  tab === item.key
                    ? "border-foreground/20 bg-foreground text-background"
                    : "border-border text-muted-foreground hover:text-foreground"
                )}
              >
                <item.icon className="size-3.5" />
                {item.label}
              </button>
            ))}
          </div>
          {tab === "inbox" ? <MailFolderView key="inbox" folder={0} {...ctx} /> : null}
          {tab === "sent" ? <MailFolderView key="sent" folder={1} {...ctx} /> : null}
          {tab === "accounts" ? <AccountsView {...ctx} /> : null}
          {tab === "users" && overview?.isAdmin ? <UsersView {...ctx} /> : null}
          {tab === "all" && overview?.isAdmin ? <AllMailView {...ctx} /> : null}
          {tab === "regkeys" && overview?.isAdmin ? <RegKeysView {...ctx} /> : null}
        </>
      ) : null}
    </div>
  );
}
