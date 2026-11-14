"use client";

import { useMemo } from "react";
import Link from "next/link";
import { ArrowUpRight, Crown, Laptop, ShieldAlert, Smartphone, UserX } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useTicketRequesterQuery, useTicketsQuery } from "@/lib/ticket-hooks";
import type { TicketItem, TicketRequesterAccount } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import {
  PersonAvatar,
  SOURCE_LABEL,
  StatusDot,
  formatDateTime,
  formatPlatform,
  formatRelativeTime,
  getTicketClient
} from "@/components/tickets/ticket-shared";
import { TicketAttachmentList } from "@/components/tickets/ticket-attachments";
import { SlaMeter, TagEditor, Timeline, Watchers } from "@/components/tickets/workspace/ticket-inspector";
import { Fact, Facts, PanelSection } from "./feedback-kit";

// 反馈详情右栏。
//
// 「用户」页签回答「对面是谁」：头像、账号资料、会员与账号状态、最近登录、这次提交用的设备，
// 以及他以前提过什么。处理反馈时最常做的事是去用户页查这个人，这里把那一趟省掉。
// 「详情」页签放这条反馈自身的属性：时效、标签、关注人、附件、操作记录。

export type ContextTab = "user" | "detail";

export function FeedbackContextPanel({
  ticket,
  tab,
  onTabChange,
  onOpenFeedback
}: {
  ticket: TicketItem;
  tab: ContextTab;
  onTabChange: (tab: ContextTab) => void;
  onOpenFeedback: (id: number) => void;
}) {
  return (
    <Tabs value={tab} onValueChange={(value) => onTabChange(value as ContextTab)} className="flex h-full min-h-0 flex-col gap-0">
      <div className="border-b px-4 py-2.5">
        <TabsList className="h-8 w-full">
          <TabsTrigger value="user" className="text-xs">
            用户
          </TabsTrigger>
          <TabsTrigger value="detail" className="text-xs">
            详情
          </TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="user" className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        <UserTab ticket={ticket} onOpenFeedback={onOpenFeedback} />
      </TabsContent>
      <TabsContent value="detail" className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        <DetailTab ticket={ticket} />
      </TabsContent>
    </Tabs>
  );
}

// ─────────────── 用户 ───────────────

function membership(user: TicketRequesterAccount) {
  if (user.vipLifetimeAt) return { label: "永久会员", active: true };
  if (user.vipExpireAt) {
    const expire = new Date(user.vipExpireAt);
    if (expire.getTime() > Date.now()) {
      return { label: `会员至 ${expire.toLocaleDateString("zh-CN")}`, active: true };
    }
    return { label: "会员已过期", active: false };
  }
  return null;
}

function UserTab({ ticket, onOpenFeedback }: { ticket: TicketItem; onOpenFeedback: (id: number) => void }) {
  const profileQuery = useTicketRequesterQuery(ticket.id);
  const profile = profileQuery.data;
  const user = profile?.user;
  const client = useMemo(() => getTicketClient(ticket.metadata), [ticket.metadata]);

  const name = profile?.name || ticket.requesterName || "匿名用户";
  const vip = user ? membership(user) : null;
  const registerPlace = user ? [user.registerProvince, user.registerCity, user.registerIsp].filter(Boolean).join(" ") : "";
  const detailHref =
    profile?.appKey && user
      ? `/users/app-users/${encodeURIComponent(profile.appKey)}/${user.id}?from=${encodeURIComponent(`/feedback?id=${ticket.id}`)}`
      : null;

  return (
    <div className="space-y-5 p-4">
      {/* 身份 */}
      <div className="flex flex-col items-center gap-3 pt-2 text-center">
        <PersonAvatar name={name} src={profile?.avatar ?? ticket.requesterAvatar} preview className="size-16 text-lg" />
        <div className="min-w-0 space-y-1">
          <p className="truncate text-base font-semibold text-foreground">{name}</p>
          <p className="truncate text-xs text-muted-foreground">
            {user ? `@${user.account}` : ticket.requesterType === "admin" ? "管理员代提" : ticket.appName || "应用用户"}
          </p>
        </div>
        <div className="flex flex-wrap justify-center gap-1.5">
          {profile?.removed ? (
            <Badge variant="outline" size="sm">
              <UserX />
              账号已删除
            </Badge>
          ) : null}
          {user && !user.enabled ? (
            <Badge variant="danger" size="sm">
              <ShieldAlert />
              账号已限制
            </Badge>
          ) : null}
          {vip ? (
            <Badge variant={vip.active ? "warning" : "outline"} size="sm">
              <Crown />
              {vip.label}
            </Badge>
          ) : null}
        </div>
        {detailHref ? (
          <Button size="sm" variant="outline" className="w-full" asChild>
            <Link href={detailHref}>
              用户详情
              <ArrowUpRight />
            </Link>
          </Button>
        ) : null}
      </div>

      {profileQuery.isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-8" />
          ))}
        </div>
      ) : null}

      {profile ? (
        <div className="grid grid-cols-3 divide-x rounded-xl border text-center">
          <Stat label="反馈" value={profile.tickets.feedback} />
          <Stat label="未结" value={profile.tickets.open} tone={profile.tickets.open > 1 ? "warning" : undefined} />
          <Stat label="积分" value={user ? user.integral : null} />
        </div>
      ) : null}

      {user ? (
        <PanelSection title="账号">
          <Facts>
            <Fact label="用户 ID" value={user.publicId || user.id} mono copyable />
            <Fact label="账号" value={user.account} copyable />
            <Fact label="昵称" value={user.nickname} />
            <Fact label="邮箱" value={user.email} copyable />
            <Fact label="手机" value={user.phone} copyable />
            <Fact label="联系方式" value={profile?.contact} copyable />
            <Fact label="经验" value={user.experience} />
            <Fact label="注册" value={formatDateTime(user.registerTime || user.createdAt)} />
            <Fact label="注册地" value={registerPlace} />
            <Fact label="注册 IP" value={user.registerIp} mono copyable />
            {!user.enabled ? (
              <Fact label="限制原因" value={user.disabledReason || "未填写"} />
            ) : null}
            {!user.enabled && user.disabledEndTime ? <Fact label="限制至" value={formatDateTime(user.disabledEndTime)} /> : null}
          </Facts>
        </PanelSection>
      ) : profile && !profile.removed && profile.type === "user" && !profile.canViewUser ? (
        <p className="rounded-lg bg-muted/60 px-3 py-2 text-xs text-muted-foreground">无该应用的用户查看权限</p>
      ) : profile?.contact ? (
        <PanelSection title="账号">
          <Facts>
            <Fact label="联系方式" value={profile.contact} copyable />
          </Facts>
        </PanelSection>
      ) : null}

      {profile?.lastLogin ? (
        <PanelSection title="最近登录">
          <Facts>
            <Fact label="时间" value={formatRelativeTime(profile.lastLogin.at)} />
            <Fact label="位置" value={[profile.lastLogin.location, profile.lastLogin.isp].filter(Boolean).join(" ")} />
            <Fact label="IP" value={profile.lastLogin.ip} mono copyable />
            <Fact label="设备" value={profile.lastLogin.deviceName} />
          </Facts>
        </PanelSection>
      ) : null}

      {client ? (
        <PanelSection title="提交端">
          <div className="flex items-center gap-3 rounded-xl border px-3 py-2.5">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
              {client.platform && ["windows", "macos", "linux", "web"].includes(client.platform.toLowerCase()) ? (
                <Laptop className="size-4" />
              ) : (
                <Smartphone className="size-4" />
              )}
            </span>
            <div className="min-w-0 text-sm">
              <p className="truncate font-medium text-foreground">
                {[formatPlatform(client.platform), client.version].filter(Boolean).join(" ") || "未知平台"}
              </p>
              {client.device ? <p className="truncate text-xs text-muted-foreground">{client.device}</p> : null}
            </div>
          </div>
        </PanelSection>
      ) : null}

      {profile?.type === "user" && ticket.requesterUserId ? (
        <History ticket={ticket} userId={ticket.requesterUserId} onOpen={onOpenFeedback} />
      ) : null}
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: number | null; tone?: "warning" }) {
  return (
    <div className="px-2 py-2.5">
      <p className={cn("text-base font-semibold tabular-nums", tone === "warning" ? "text-amber-600 dark:text-amber-400" : "text-foreground")}>
        {value === null ? "—" : value.toLocaleString("zh-CN")}
      </p>
      <p className="text-xs text-muted-foreground">{label}</p>
    </div>
  );
}

/** 这个人以前提过的反馈：重复反馈、老问题没解决，一眼能看出来 */
function History({ ticket, userId, onOpen }: { ticket: TicketItem; userId: number; onOpen: (id: number) => void }) {
  const historyQuery = useTicketsQuery({
    kind: "feedback",
    appid: ticket.appid,
    requesterId: userId,
    includeClosed: true,
    sortBy: "created",
    limit: 8
  });
  const others = (historyQuery.data?.items ?? []).filter((item) => item.id !== ticket.id);
  if (!others.length) return null;

  return (
    <PanelSection title="其他反馈">
      <ul className="-mx-2">
        {others.map((item) => (
          <li key={item.id}>
            <button
              type="button"
              onClick={() => onOpen(item.id)}
              className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-sm outline-none transition-colors hover:bg-muted focus-visible:bg-muted"
            >
              <StatusDot status={item.status} />
              <span className="min-w-0 flex-1 truncate text-foreground">{item.title}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{formatRelativeTime(item.createdAt)}</span>
            </button>
          </li>
        ))}
      </ul>
    </PanelSection>
  );
}

// ─────────────── 详情 ───────────────

function DetailTab({ ticket }: { ticket: TicketItem }) {
  const attachments = ticket.attachments ?? [];
  return (
    <div className="space-y-5 p-4">
      <PanelSection title="时效">
        <div className="space-y-3">
          <SlaMeter label="首次响应" start={ticket.createdAt} due={ticket.firstResponseDueAt} doneAt={ticket.firstRespondedAt} />
          <SlaMeter label="解决" start={ticket.createdAt} due={ticket.resolveDueAt} doneAt={ticket.resolvedAt} />
        </div>
      </PanelSection>

      <PanelSection title="标签">
        <TagEditor ticket={ticket} editable={Boolean(ticket.permissions?.edit)} />
      </PanelSection>

      <PanelSection title="关注人">
        <Watchers ticket={ticket} />
      </PanelSection>

      {attachments.length ? (
        <PanelSection title={`附件 ${attachments.length}`}>
          <TicketAttachmentList attachments={attachments} />
        </PanelSection>
      ) : null}

      <PanelSection title="信息">
        <Facts>
          <Fact label="编号" value={ticket.ticketNo} mono copyable />
          <Fact label="应用" value={ticket.appName} />
          <Fact label="来源" value={SOURCE_LABEL[ticket.source] ?? ticket.source} />
          <Fact label="处理组" value={ticket.groupName} />
          <Fact label="提交" value={formatDateTime(ticket.createdAt)} />
          <Fact label="更新" value={formatRelativeTime(ticket.updatedAt)} />
          {ticket.reopenedCount > 0 ? <Fact label="重开" value={`${ticket.reopenedCount} 次`} /> : null}
        </Facts>
      </PanelSection>

      <PanelSection title="操作记录">
        <Timeline ticket={ticket} />
      </PanelSection>
    </div>
  );
}
