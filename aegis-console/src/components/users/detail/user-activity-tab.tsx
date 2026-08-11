"use client";

import { useState } from "react";
import {
  Activity,
  ArrowUpRight,
  FileClock,
  Globe2,
  LogIn,
  Monitor,
  MonitorSmartphone,
  Radar,
  Smartphone
} from "lucide-react";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip";
import {
  useAdminUserLoginAuditsQuery,
  useAdminUserSessionAuditsQuery
} from "@/lib/app-user-hooks";
import { useRiskAssessmentsQuery } from "@/lib/risk-hooks";
import {
  ActionBadge,
  LevelBadge,
  RiskCatalogProvider,
  SceneBadge
} from "@/components/risk/risk-shared";
import type { AdminAppUserDetail } from "@/lib/api/types";
import {
  EmptyRow,
  LoginStatusBadge,
  Panel,
  SESSION_EVENT_LABEL,
  ValueTree,
  describeUserAgent,
  formatShortTime,
  formatTime,
  relativeTime,
  textValue
} from "./user-detail-shared";
import { UserSessionsPanel } from "../user-sessions-panel";

const LOGIN_STATUS_OPTIONS = [
  { value: "all", label: "全部结果" },
  { value: "success", label: "仅成功" },
  { value: "failed", label: "仅失败" },
  { value: "blocked", label: "仅被拦截" }
];

/**
 * 活动：这个账号最近发生了什么。
 *
 * 四条流各自回答一个不同的问题，刻意不合并成一条"统一时间线"——
 * 合并之后就没法按"只看失败登录"这种真实的排查意图筛选了：
 *
 *   活跃会话   现在谁拿着这个账号的令牌（可撤销）
 *   登录记录   谁尝试过登录、成没成、从哪来
 *   会话事件   令牌的签发 / 刷新 / 撤销轨迹
 *   风控评估   引擎怎么给这个账号打的分
 */
export function UserActivityTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const [loginStatus, setLoginStatus] = useState("all");

  const loginQuery = useAdminUserLoginAuditsQuery(appKey, userId, {
    status: loginStatus === "all" ? undefined : loginStatus,
    limit: 20
  });
  const sessionAuditQuery = useAdminUserSessionAuditsQuery(appKey, userId, { limit: 20 });

  const loginItems = loginQuery.data?.items ?? [];
  const eventItems = sessionAuditQuery.data?.items ?? [];

  return (
    <div className="space-y-5">
      <Panel
        title="活跃会话"
        icon={<MonitorSmartphone className="size-4" />}
        description="当前有效的登录令牌。撤销后该设备立刻掉线。"
      >
        <UserSessionsPanel appKey={appKey} userId={userId} />
      </Panel>

      <Panel
        title="登录记录"
        icon={<LogIn className="size-4" />}
        description="按用户精确过滤，不是按关键字搜出来的。"
        action={
          <div className="flex items-center gap-2">
            <Badge variant="outline" size="sm">
              共 {loginQuery.data?.total ?? 0} 条
            </Badge>
            <Select value={loginStatus} onValueChange={setLoginStatus}>
              <SelectTrigger size="sm" className="h-8 w-[122px] text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LOGIN_STATUS_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value} className="text-xs">
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
        bodyClassName="p-0"
      >
        {loginQuery.isLoading ? (
          <div className="space-y-2 p-5">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-9 w-full rounded-lg" />
            ))}
          </div>
        ) : !loginItems.length ? (
          <EmptyRow text={loginStatus === "all" ? "没有登录记录" : "该筛选下没有记录"} />
        ) : (
          <TooltipProvider delayDuration={150}>
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>时间</TableHead>
                    <TableHead>结果</TableHead>
                    <TableHead>方式</TableHead>
                    <TableHead>来源 IP</TableHead>
                    <TableHead>客户端</TableHead>
                    <TableHead>设备标识</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loginItems.map((item) => {
                    const ua = describeUserAgent(item.userAgent);
                    const DeviceIcon = ua.mobile ? Smartphone : Monitor;
                    return (
                      <TableRow key={item.id}>
                        <TableCell
                          className="whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                          title={formatTime(item.createdAt)}
                        >
                          {formatShortTime(item.createdAt)}
                        </TableCell>
                        <TableCell>
                          <LoginStatusBadge status={item.status} />
                        </TableCell>
                        <TableCell className="text-xs">
                          {textValue(item.loginType)}
                          {item.provider ? (
                            <span className="ml-1 text-muted-foreground">/ {item.provider}</span>
                          ) : null}
                        </TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          <span className="inline-flex items-center gap-1">
                            <Globe2 className="size-3" />
                            {textValue(item.loginIp)}
                          </span>
                        </TableCell>
                        <TableCell className="max-w-[190px]">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="inline-flex items-center gap-1.5 text-xs">
                                <DeviceIcon className="size-3 shrink-0 text-muted-foreground" />
                                <span className="truncate">{ua.label}</span>
                              </span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="max-w-sm break-all text-xs">
                              {textValue(item.userAgent, "无 User-Agent")}
                            </TooltipContent>
                          </Tooltip>
                        </TableCell>
                        <TableCell className="max-w-[130px] truncate font-mono text-[11px] text-muted-foreground">
                          {textValue(item.deviceId)}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          </TooltipProvider>
        )}
      </Panel>

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel
          title="会话事件"
          icon={<FileClock className="size-4" />}
          description="令牌的签发、刷新与撤销轨迹。"
          action={
            <Badge variant="outline" size="sm">
              共 {sessionAuditQuery.data?.total ?? 0} 条
            </Badge>
          }
        >
          {sessionAuditQuery.isLoading ? (
            <div className="space-y-2">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-9 w-full rounded-lg" />
              ))}
            </div>
          ) : !eventItems.length ? (
            <EmptyRow text="没有会话事件" />
          ) : (
            <ol className="relative space-y-3 border-l pl-4">
              {eventItems.map((event) => (
                <li key={event.id} className="relative">
                  <span className="absolute -left-[21px] top-1.5 size-2 rounded-full bg-border ring-4 ring-background" />
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="text-sm font-medium">
                      {SESSION_EVENT_LABEL[event.eventType] ?? event.eventType}
                    </span>
                    <span
                      className="shrink-0 text-[11px] tabular-nums text-muted-foreground"
                      title={formatTime(event.createdAt)}
                    >
                      {relativeTime(event.createdAt)}
                    </span>
                  </div>
                  {event.tokenJti ? (
                    <div className="truncate font-mono text-[11px] text-muted-foreground">
                      {event.tokenJti}
                    </div>
                  ) : null}
                  {event.metadata && Object.keys(event.metadata).length ? (
                    <div className="mt-1 rounded-lg bg-muted/40 px-2.5 py-1.5">
                      <ValueTree value={event.metadata} />
                    </div>
                  ) : null}
                </li>
              ))}
            </ol>
          )}
        </Panel>

        <RiskCatalogProvider>
          <UserRiskPanel account={user.account} />
        </RiskCatalogProvider>
      </div>
    </div>
  );
}

/**
 * 风控评估。按 account 查 —— 风控引擎在登录成功前就已经打过分，
 * 那时还没有 userId，account 才是贯穿始终的那个标识。
 */
function UserRiskPanel({ account }: { account?: string }) {
  const query = useRiskAssessmentsQuery(account ? { account, pageSize: 12 } : undefined);
  const items = query.data?.items ?? [];

  return (
    <Panel
      title="风控评估"
      icon={<Radar className="size-4" />}
      description="引擎在登录 / 注册链路上对这个账号打过的分。"
      action={
        <Button asChild size="sm" variant="ghost" className="h-7 text-xs">
          <Link href={`/risk-control?tab=assessments`}>
            风控中心
            <ArrowUpRight className="size-3" />
          </Link>
        </Button>
      }
    >
      {!account ? (
        <EmptyRow text="该用户没有账号标识，无法关联风控记录" />
      ) : query.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : !items.length ? (
        <div className="py-10 text-center text-sm text-muted-foreground">
          <Activity className="mx-auto mb-2 size-5 opacity-40" />
          没有评估记录。可能是风控未对该场景配置规则，也可能这个账号从未触发过评估。
        </div>
      ) : (
        <div className="space-y-2">
          {items.map((item) => (
            <div key={item.id} className="rounded-xl border px-3 py-2.5">
              <div className="flex flex-wrap items-center gap-1.5">
                <SceneBadge scene={item.scene} />
                <LevelBadge level={item.riskLevel} />
                <ActionBadge action={item.action} />
                <span className="ml-auto text-xs tabular-nums text-muted-foreground">
                  {item.totalScore} 分
                </span>
              </div>
              <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                <span className="font-mono">{textValue(item.ip)}</span>
                {item.country ? <span>{item.country}</span> : null}
                <span title={formatTime(item.createdAt)}>{relativeTime(item.createdAt)}</span>
                {item.reviewed ? (
                  <Badge variant="outline" size="sm">
                    已复核
                  </Badge>
                ) : null}
              </div>
              {item.matchedRules?.length ? (
                <div className="mt-1.5 line-clamp-2 text-[11px] leading-4 text-muted-foreground">
                  {item.matchedRules.map((rule) => rule.reason || rule.ruleName).join(" · ")}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}
