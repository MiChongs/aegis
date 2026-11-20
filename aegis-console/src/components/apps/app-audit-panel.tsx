"use client";

import { useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { X } from "lucide-react";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useNow } from "@/components/users/ban-form";
import { useAdminAppLoginAuditsQuery, useAdminAppSessionAuditsQuery, useAdminAppNotificationsQuery } from "@/lib/admin-hooks";
import type { NotificationItem } from "@/lib/api/types";
import { formatShortDateTime } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import {
  USER_NOTIFICATION_CATEGORIES,
  describeUserNotificationAction,
  userNotificationCategoryLabel,
  userNotificationLevelLabel
} from "@/lib/user-notification-meta";

function fmtTime(v?: string | null) {
  return formatShortDateTime(v);
}

function str(v: unknown, fb = "—") { return typeof v === "string" && v.trim() ? v : fb; }

type AuditView = "login" | "session" | "notification";

/**
 * 应用审计区块。深链 `?tab=audit&view=notification&campaignId=N` 直接打开「通知」并按群发任务筛选
 * （站内信群发详情里的「查看已发消息」）；参数变化时按 key 重新挂载，筛选跟着链接走。
 */
export function AppAuditPanel({ appKey }: { appKey?: string | null }) {
  const searchParams = useSearchParams();
  const viewParam = searchParams.get("view");
  const campaignParam = Number(searchParams.get("campaignId"));
  const initialView: AuditView = viewParam === "session" || viewParam === "notification" ? viewParam : "login";
  const initialCampaignId = Number.isSafeInteger(campaignParam) && campaignParam > 0 ? campaignParam : undefined;

  if (!appKey) return <div className="py-12 text-center text-sm text-muted-foreground">请先选择应用</div>;

  return (
    <AuditViews
      key={`${initialView}:${initialCampaignId ?? ""}`}
      appKey={appKey}
      initialView={initialView}
      initialCampaignId={initialCampaignId}
    />
  );
}

function AuditViews({
  appKey,
  initialView,
  initialCampaignId
}: {
  appKey: string;
  initialView: AuditView;
  initialCampaignId?: number;
}) {
  const [view, setView] = useState<AuditView>(initialView);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5 w-fit">
        {(["login", "session", "notification"] as const).map((v) => (
          <button
            key={v}
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${view === v ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
            onClick={() => setView(v)}
          >
            {{ login: "登录审计", session: "会话审计", notification: "通知" }[v]}
          </button>
        ))}
      </div>

      {view === "login" && <LoginAuditTable appKey={appKey} />}
      {view === "session" && <SessionAuditTable appKey={appKey} />}
      {view === "notification" && <NotificationTable appKey={appKey} initialCampaignId={initialCampaignId} />}
    </div>
  );
}

function LoginAuditTable({ appKey }: { appKey: string }) {
  const query = useAdminAppLoginAuditsQuery(appKey, { page: 1, limit: 20 });
  const items = useMemo(() => {
    const list = query.data;
    return Array.isArray(list) ? list : (list as any)?.items || [];
  }, [query.data]);

  if (query.isLoading) return <Skeleton className="h-40 w-full rounded-lg" />;
  if (!items.length) return <div className="py-8 text-center text-xs text-muted-foreground">暂无登录审计数据</div>;

  return (
    <div className="overflow-hidden rounded-xl border">
      <Table>
        <TableHeader><TableRow className="hover:bg-transparent">
          <TableHead>账号</TableHead><TableHead>登录方式</TableHead><TableHead>提供方</TableHead><TableHead>状态</TableHead><TableHead>IP</TableHead><TableHead>时间</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {items.map((item: Record<string, unknown>, i: number) => (
            <TableRow key={`${str(item.account)}-${i}`}>
              <TableCell className="text-xs font-medium">{str(item.account)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.loginType)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.provider)}</TableCell>
              <TableCell className="text-xs">{str(item.status) === "success" ? <span className="text-emerald-600">成功</span> : <span className="text-red-500">{str(item.status)}</span>}</TableCell>
              <TableCell className="font-mono text-xs text-muted-foreground">{str(item.loginIP)}</TableCell>
              <TableCell className="text-xs tabular-nums text-muted-foreground">{fmtTime(item.createdAt as string)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function SessionAuditTable({ appKey }: { appKey: string }) {
  const query = useAdminAppSessionAuditsQuery(appKey, { page: 1, limit: 20 });
  const items = useMemo(() => {
    const list = query.data;
    return Array.isArray(list) ? list : (list as any)?.items || [];
  }, [query.data]);

  if (query.isLoading) return <Skeleton className="h-40 w-full rounded-lg" />;
  if (!items.length) return <div className="py-8 text-center text-xs text-muted-foreground">暂无会话审计数据</div>;

  return (
    <div className="overflow-hidden rounded-xl border">
      <Table>
        <TableHeader><TableRow className="hover:bg-transparent">
          <TableHead>账号</TableHead><TableHead>事件</TableHead><TableHead>Token</TableHead><TableHead>时间</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {items.map((item: Record<string, unknown>, i: number) => (
            <TableRow key={`${str(item.account)}-${i}`}>
              <TableCell className="text-xs font-medium">{str(item.account)}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{str(item.eventType)}</TableCell>
              <TableCell className="max-w-[120px] truncate font-mono text-xs text-muted-foreground">{str(item.tokenJTI)}</TableCell>
              <TableCell className="text-xs tabular-nums text-muted-foreground">{fmtTime(item.createdAt as string)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

const NOTIFICATION_PAGE_SIZE = 20;

const READ_STATUS_OPTIONS = [
  { value: "all", label: "全部状态" },
  { value: "unread", label: "未读" },
  { value: "read", label: "已读" }
] as const;

/** 应用用户站内信列表：按分类、已读状态与群发任务筛选，带分类、动作、状态与过期时间。 */
function NotificationTable({ appKey, initialCampaignId }: { appKey: string; initialCampaignId?: number }) {
  const [category, setCategory] = useState("all");
  const [readStatus, setReadStatus] = useState<"all" | "read" | "unread">("all");
  const [campaignText, setCampaignText] = useState(initialCampaignId ? String(initialCampaignId) : "");
  const [page, setPage] = useState(1);
  const now = useNow();
  const campaignInput = useDebouncedValue(campaignText.trim(), 300);
  const campaignNumber = Number(campaignInput);
  const campaignId = campaignInput && Number.isSafeInteger(campaignNumber) && campaignNumber > 0 ? campaignNumber : undefined;
  const query = useAdminAppNotificationsQuery(appKey, {
    category: category === "all" ? undefined : category,
    status: readStatus === "all" ? undefined : readStatus,
    campaignId,
    page,
    limit: NOTIFICATION_PAGE_SIZE
  });
  const data = query.data;
  const items: NotificationItem[] = data?.items ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, data?.totalPages ?? 1);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={category}
          onValueChange={(value) => {
            setCategory(value);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all" className="text-xs">
              全部分类
            </SelectItem>
            {USER_NOTIFICATION_CATEGORIES.map((item) => (
              <SelectItem key={item.value} value={item.value} className="text-xs">
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={readStatus}
          onValueChange={(value) => {
            setReadStatus(value as typeof readStatus);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-28 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {READ_STATUS_OPTIONS.map((item) => (
              <SelectItem key={item.value} value={item.value} className="text-xs">
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="relative">
          <Input
            value={campaignText}
            inputMode="numeric"
            placeholder="群发任务 ID"
            className="h-8 w-36 pr-7 text-xs tabular-nums"
            onChange={(event) => {
              setCampaignText(event.target.value.replace(/[^\d]/g, ""));
              setPage(1);
            }}
          />
          {campaignText ? (
            <button
              type="button"
              aria-label="清除"
              onClick={() => {
                setCampaignText("");
                setPage(1);
              }}
              className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
      </div>

      {query.isLoading ? (
        <Skeleton className="h-40 w-full rounded-lg" />
      ) : !items.length ? (
        <div className="py-8 text-center text-xs text-muted-foreground">暂无通知数据</div>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>标题</TableHead>
                <TableHead>收件人</TableHead>
                <TableHead>分类</TableHead>
                <TableHead>级别</TableHead>
                <TableHead>跳转</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>群发</TableHead>
                <TableHead>过期时间</TableHead>
                <TableHead>时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => {
                const expired = item.expiresAt ? new Date(item.expiresAt).getTime() <= now : false;
                return (
                  <TableRow key={item.id}>
                    <TableCell className="max-w-[240px] truncate text-xs font-medium" title={item.content}>
                      {str(item.title)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{str(item.account, item.userId ? `#${item.userId}` : "—")}</TableCell>
                    <TableCell className="text-xs">{item.categoryLabel || userNotificationCategoryLabel(item.category) || "—"}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{userNotificationLevelLabel(item.level) || "—"}</TableCell>
                    <TableCell className="max-w-[160px] truncate text-xs text-muted-foreground" title={item.action?.target}>
                      {describeUserNotificationAction(item.action) || "—"}
                    </TableCell>
                    <TableCell className="text-xs">
                      {item.status === "read" ? (
                        <Badge variant="outline" size="sm">已读</Badge>
                      ) : (
                        <Badge variant="info" size="sm">未读</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-xs tabular-nums text-muted-foreground">
                      {item.campaignId ? (
                        <button
                          type="button"
                          title="只看这次群发"
                          className="hover:text-foreground hover:underline"
                          onClick={() => {
                            setCampaignText(String(item.campaignId));
                            setPage(1);
                          }}
                        >
                          #{item.campaignId}
                        </button>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="text-xs tabular-nums text-muted-foreground">
                      {item.expiresAt ? (expired ? <span className="text-amber-600">已过期</span> : fmtTime(item.expiresAt)) : "—"}
                    </TableCell>
                    <TableCell className="text-xs tabular-nums text-muted-foreground">{fmtTime(item.createdAt)}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
          <ListPagination page={page} totalPages={totalPages} total={total} unit="条" onPage={setPage} />
        </div>
      )}
    </div>
  );
}
