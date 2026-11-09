"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlarmClock,
  Bell,
  BellOff,
  Copy,
  Hash,
  Laptop,
  Plus,
  Smartphone,
  Sparkles,
  UserCheck,
  X,
  Zap
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { useAuthStore } from "@/lib/auth-store";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { AvatarGroup } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useAdminOptions,
  useAssignTicketMutation,
  useTicketCategoriesQuery,
  useTicketGroupsQuery,
  useUpdateTicketMutation,
  useWatchTicketMutation
} from "@/lib/ticket-hooks";
import type { TicketItem, TicketPriority } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import {
  PRIORITY_LABEL,
  PersonAvatar,
  SOURCE_LABEL,
  formatDateTime,
  formatDuration,
  formatPlatform,
  formatRelativeTime,
  getTicketClient
} from "../ticket-shared";
import { TicketAttachmentList } from "../ticket-attachments";

// 详情右栏：人、端、归属、时效、附件、时间线。
// 每个可改的字段都在原位直接改，改完立即生效，不设「保存」按钮。

export async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    notify.success(`已复制${label}`);
  } catch {
    notify.error("复制失败，请手动选择文本");
  }
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function TicketInspector({ ticket }: { ticket: TicketItem }) {
  const perms = ticket.permissions;
  const client = useMemo(() => getTicketClient(ticket.metadata), [ticket.metadata]);
  const attachments = ticket.attachments ?? [];

  return (
    <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
      <RequesterCard ticket={ticket} />
      <Accordion
        type="multiple"
        defaultValue={["handling", "client", "sla", "tags", "watchers", "files", "timeline"]}
        className="px-4"
      >
        {client ? (
          <AccordionItem value="client">
            <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">提交端</AccordionTrigger>
            <AccordionContent>
              <div className="flex items-start gap-3 rounded-lg bg-muted/50 p-3">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-background text-muted-foreground">
                  {client.platform && ["windows", "macos", "linux", "web"].includes(client.platform.toLowerCase()) ? (
                    <Laptop className="size-4" />
                  ) : (
                    <Smartphone className="size-4" />
                  )}
                </span>
                <div className="min-w-0 space-y-0.5 text-sm">
                  <p className="font-medium text-foreground">
                    {[formatPlatform(client.platform), client.version].filter(Boolean).join(" ") || "未知平台"}
                  </p>
                  <p className="truncate text-xs text-muted-foreground">{client.device || "未上报设备型号"}</p>
                </div>
              </div>
            </AccordionContent>
          </AccordionItem>
        ) : null}

        <AccordionItem value="handling">
          <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">处理</AccordionTrigger>
          <AccordionContent className="space-y-3">
            <HandlingFields ticket={ticket} />
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="sla">
          <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">时效</AccordionTrigger>
          <AccordionContent className="space-y-3">
            <SlaMeter
              label="首次响应"
              start={ticket.createdAt}
              due={ticket.firstResponseDueAt}
              doneAt={ticket.firstRespondedAt}
            />
            <SlaMeter label="解决" start={ticket.createdAt} due={ticket.resolveDueAt} doneAt={ticket.resolvedAt} />
            <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
              <Meta label="创建于" value={formatDateTime(ticket.createdAt)} />
              <Meta label="最后更新" value={formatRelativeTime(ticket.updatedAt)} />
              <Meta label="来源" value={SOURCE_LABEL[ticket.source] ?? ticket.source} />
              <Meta label="重开次数" value={String(ticket.reopenedCount)} />
            </dl>
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="tags">
          <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">
            标签{ticket.tags?.length ? `（${ticket.tags.length}）` : ""}
          </AccordionTrigger>
          <AccordionContent>
            <TagEditor ticket={ticket} editable={Boolean(perms?.edit)} />
          </AccordionContent>
        </AccordionItem>

        <AccordionItem value="watchers">
          <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">关注人</AccordionTrigger>
          <AccordionContent>
            <Watchers ticket={ticket} />
          </AccordionContent>
        </AccordionItem>

        {attachments.length ? (
          <AccordionItem value="files">
            <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">
              全部附件（{attachments.length}）
            </AccordionTrigger>
            <AccordionContent>
              <TicketAttachmentList attachments={attachments} />
            </AccordionContent>
          </AccordionItem>
        ) : null}

        <AccordionItem value="timeline" className="border-b-0">
          <AccordionTrigger className="py-3 text-xs font-medium text-muted-foreground hover:no-underline">
            操作记录{ticket.events?.length ? `（${ticket.events.length}）` : ""}
          </AccordionTrigger>
          <AccordionContent>
            <Timeline ticket={ticket} />
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

// 提单人被删除（管理员删除用户、账号注销、删除管理员）后，数据库把对应 ID 置空，工单本身保留。
function requesterRemoved(ticket: TicketItem) {
  return ticket.requesterType === "admin" ? !ticket.requesterAdminId : !ticket.requesterUserId;
}

function RequesterCard({ ticket }: { ticket: TicketItem }) {
  const isFeedback = ticket.kind === "feedback";
  return (
    <div className="border-b p-4">
      <div className="flex items-center gap-3">
        <PersonAvatar name={ticket.requesterName} size="lg" />
        <div className="min-w-0 flex-1">
          <HoverCard openDelay={300}>
            <HoverCardTrigger asChild>
              <p className="cursor-default truncate text-sm font-semibold text-foreground">
                {ticket.requesterName || "匿名用户"}
              </p>
            </HoverCardTrigger>
            <HoverCardContent align="start" className="w-64 space-y-2 text-xs">
              <p className="font-medium text-foreground">{isFeedback ? "反馈人" : "提单人"}</p>
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                <dt className="text-muted-foreground">类型</dt>
                <dd>{ticket.requesterType === "admin" ? "管理员" : "应用用户"}</dd>
                {ticket.requesterUserId ? (
                  <>
                    <dt className="text-muted-foreground">用户 ID</dt>
                    <dd className="font-mono">{ticket.requesterUserId}</dd>
                  </>
                ) : null}
                {requesterRemoved(ticket) ? (
                  <>
                    <dt className="text-muted-foreground">账号</dt>
                    <dd>已删除，工单留档</dd>
                  </>
                ) : null}
                <dt className="text-muted-foreground">应用</dt>
                <dd>{ticket.appName || (ticket.appid ? `#${ticket.appid}` : "平台")}</dd>
              </dl>
            </HoverCardContent>
          </HoverCard>
          <p className="truncate text-xs text-muted-foreground">{ticket.appName || (ticket.appid ? `应用 #${ticket.appid}` : "平台内部")}</p>
        </div>
      </div>
      {ticket.requesterContact ? (
        <button
          type="button"
          onClick={() => copyText(ticket.requesterContact as string, "联系方式")}
          className="mt-3 flex w-full items-center gap-2 rounded-lg border px-3 py-2 text-left text-xs transition-colors hover:bg-muted/50"
        >
          <span className="text-muted-foreground">联系方式</span>
          <span className="min-w-0 flex-1 truncate text-foreground">{ticket.requesterContact}</span>
          <Copy className="size-3 text-muted-foreground" />
        </button>
      ) : null}
      <button
        type="button"
        onClick={() => copyText(ticket.ticketNo, "编号")}
        className="mt-2 flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/50"
      >
        <Hash className="size-3" />
        <span className="flex-1 font-mono">{ticket.ticketNo}</span>
        <Copy className="size-3" />
      </button>
    </div>
  );
}

function HandlingFields({ ticket }: { ticket: TicketItem }) {
  const perms = ticket.permissions;
  const admins = useAdminOptions();
  const operatorId = useAuthStore((state) => Number(state.operator?.id) || 0);
  const groupsQuery = useTicketGroupsQuery(ticket.appid);
  const categoriesQuery = useTicketCategoriesQuery(ticket.appid);
  const assignMut = useAssignTicketMutation();
  const updateMut = useUpdateTicketMutation();

  const kind = ticket.kind ?? "ticket";
  const categories = (categoriesQuery.data ?? []).filter((item) => (item.kind ?? "ticket") === kind && item.enabled);
  const groups = groupsQuery.data ?? [];

  const assign = async (assigneeAdminId: number | null, groupId: number | null = ticket.groupId ?? null, autoPick = false) => {
    try {
      await assignMut.mutateAsync({ id: ticket.id, payload: autoPick ? { groupId, autoPick } : { assigneeAdminId, groupId } });
      notify.success(autoPick ? "已按组策略分派" : "指派已更新");
    } catch (error) {
      notify.error(errorMessage(error, "指派失败"));
    }
  };

  const update = async (payload: Parameters<typeof updateMut.mutateAsync>[0]["payload"], success: string) => {
    try {
      await updateMut.mutateAsync({ id: ticket.id, payload });
      notify.success(success);
    } catch (error) {
      notify.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <>
      <Field label="受理人">
        {perms?.assign ? (
          <div className="flex gap-1.5">
            <Select
              value={ticket.assigneeAdminId ? String(ticket.assigneeAdminId) : "none"}
              onValueChange={(value) => void assign(value === "none" ? null : Number(value))}
            >
              <SelectTrigger size="sm" className="min-w-0 flex-1">
                <SelectValue placeholder="未指派" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">未指派</SelectItem>
                {admins.map((admin) => (
                  <SelectItem key={admin.id} value={String(admin.id)}>
                    {admin.name}
                    {admin.id === operatorId ? "（我）" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {operatorId && ticket.assigneeAdminId !== operatorId ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button size="icon-sm" variant="outline" onClick={() => void assign(operatorId)} aria-label="指派给我">
                    <UserCheck />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>指派给我</TooltipContent>
              </Tooltip>
            ) : null}
          </div>
        ) : (
          <p className="text-sm text-foreground">{ticket.assigneeName || "未指派"}</p>
        )}
      </Field>

      <Field label="处理组">
        {perms?.assign ? (
          <div className="flex gap-1.5">
            <Select
              value={ticket.groupId ? String(ticket.groupId) : "none"}
              onValueChange={(value) => void assign(ticket.assigneeAdminId ?? null, value === "none" ? null : Number(value))}
            >
              <SelectTrigger size="sm" className="min-w-0 flex-1">
                <SelectValue placeholder="不指定" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">不指定</SelectItem>
                {groups.map((group) => (
                  <SelectItem key={group.id} value={String(group.id)}>
                    {group.name}（{group.memberCount} 人）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {ticket.groupId ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon-sm"
                    variant="outline"
                    onClick={() => void assign(null, ticket.groupId ?? null, true)}
                    aria-label="按组策略自动分派"
                  >
                    <Sparkles />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>按组策略自动分派</TooltipContent>
              </Tooltip>
            ) : null}
          </div>
        ) : (
          <p className="text-sm text-foreground">{ticket.groupName || "未指定"}</p>
        )}
      </Field>

      <Field label="优先级">
        {perms?.edit ? (
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={ticket.priority}
            onValueChange={(value) => {
              if (value && value !== ticket.priority) {
                void update({ priority: value as TicketPriority }, `优先级已改为「${PRIORITY_LABEL[value as TicketPriority]}」`);
              }
            }}
            className="w-full"
          >
            {(["low", "normal", "high", "urgent"] as TicketPriority[]).map((priority) => (
              <ToggleGroupItem
                key={priority}
                value={priority}
                className={cn(
                  "flex-1 text-xs",
                  priority === "urgent" && "data-[state=on]:bg-red-50 data-[state=on]:text-red-700 dark:data-[state=on]:bg-red-950/60 dark:data-[state=on]:text-red-300",
                  priority === "high" && "data-[state=on]:bg-amber-50 data-[state=on]:text-amber-700 dark:data-[state=on]:bg-amber-950/60 dark:data-[state=on]:text-amber-300"
                )}
              >
                {priority === "urgent" ? <Zap className="size-3" /> : null}
                {PRIORITY_LABEL[priority]}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        ) : (
          <p className="text-sm text-foreground">{PRIORITY_LABEL[ticket.priority]}</p>
        )}
      </Field>

      <Field label={ticket.kind === "feedback" ? "反馈分类" : "分类"}>
        {perms?.edit && categories.length > 0 ? (
          <Select
            value={ticket.categoryId ? String(ticket.categoryId) : undefined}
            onValueChange={(value) => void update({ categoryId: Number(value) }, "分类已更新")}
          >
            <SelectTrigger size="sm" className="w-full">
              <SelectValue placeholder="未分类" />
            </SelectTrigger>
            <SelectContent>
              {categories.map((category) => (
                <SelectItem key={category.id} value={String(category.id)}>
                  {category.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <p className="text-sm text-foreground">{ticket.categoryName || "未分类"}</p>
        )}
      </Field>
    </>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      {children}
    </div>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="truncate text-foreground">{value}</dd>
    </div>
  );
}

/** 每隔一段时间刷新的当前时刻，倒计时随之走动 */
function useNow(intervalMs: number) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return now;
}

/** 时效条：从创建到时限的已用比例；完成后变绿并显示实际用时 */
function SlaMeter({ label, start, due, doneAt }: { label: string; start: string; due?: string; doneAt?: string }) {
  const now = useNow(30_000);
  const startMs = new Date(start).getTime();
  if (doneAt) {
    const used = new Date(doneAt).getTime() - startMs;
    const late = due ? new Date(doneAt).getTime() > new Date(due).getTime() : false;
    return (
      <div className="space-y-1.5">
        <div className="flex items-center justify-between text-xs">
          <span className="text-muted-foreground">{label}</span>
          <span className={late ? "text-destructive" : "text-emerald-600 dark:text-emerald-400"}>
            {late ? "超时完成" : "已达标"}，用时 {formatDuration(used) === "—" ? "不到 1 分钟" : formatDuration(used)}
          </span>
        </div>
        <Progress value={100} className={cn("h-1.5", late ? "[&>*]:bg-destructive" : "[&>*]:bg-emerald-500")} />
      </div>
    );
  }
  if (!due) {
    return (
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="text-muted-foreground">未设时限</span>
      </div>
    );
  }
  const dueMs = new Date(due).getTime();
  const ratio = Math.min(100, Math.max(0, ((now - startMs) / Math.max(1, dueMs - startMs)) * 100));
  const overdue = now > dueMs;
  const warn = ratio >= 75;
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className={cn("inline-flex items-center gap-1", overdue ? "font-medium text-destructive" : warn ? "text-amber-600 dark:text-amber-400" : "text-foreground")}>
          {overdue ? <AlarmClock className="size-3" /> : null}
          {overdue ? `已超时 ${formatDuration(now - dueMs)}` : `剩余 ${formatDuration(dueMs - now)}`}
        </span>
      </div>
      <Progress
        value={ratio}
        className={cn("h-1.5", overdue ? "[&>*]:bg-destructive" : warn ? "[&>*]:bg-amber-500" : "[&>*]:bg-primary")}
      />
    </div>
  );
}

function TagEditor({ ticket, editable }: { ticket: TicketItem; editable: boolean }) {
  const updateMut = useUpdateTicketMutation();
  const [draft, setDraft] = useState("");
  const tags = ticket.tags ?? [];

  const save = async (next: string[]) => {
    try {
      await updateMut.mutateAsync({ id: ticket.id, payload: { tags: next } });
    } catch (error) {
      notify.error(errorMessage(error, "标签保存失败"));
    }
  };

  const add = () => {
    const values = draft
      .split(/[,，\s]+/)
      .map((item) => item.trim())
      .filter(Boolean)
      .filter((item) => !tags.includes(item));
    setDraft("");
    if (values.length) void save([...tags, ...values]);
  };

  return (
    <div className="space-y-2">
      {tags.length ? (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <Badge key={tag} variant="secondary" className="gap-1 pr-1">
              {tag}
              {editable ? (
                <button
                  type="button"
                  aria-label={`移除标签 ${tag}`}
                  onClick={() => void save(tags.filter((item) => item !== tag))}
                  className="rounded-full p-0.5 transition-colors hover:bg-background"
                >
                  <X className="size-3" />
                </button>
              ) : null}
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">暂无标签</p>
      )}
      {editable ? (
        <div className="flex gap-1.5">
          <Input
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                add();
              }
            }}
            placeholder="输入标签后回车，可用逗号分隔"
            className="h-8 text-xs"
          />
          <Button size="icon-sm" variant="outline" onClick={add} disabled={!draft.trim()} aria-label="添加标签">
            <Plus />
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function Watchers({ ticket }: { ticket: TicketItem }) {
  const operatorId = useAuthStore((state) => Number(state.operator?.id) || 0);
  const watchMut = useWatchTicketMutation();
  const watchers = ticket.watchers ?? [];
  const watching = watchers.some((watcher) => watcher.adminId === operatorId);

  const toggle = async () => {
    try {
      await watchMut.mutateAsync({ id: ticket.id, watch: !watching });
      notify.success(watching ? "已取消关注" : "已关注，有新动态会通知你");
    } catch (error) {
      notify.error(errorMessage(error, "操作失败"));
    }
  };

  return (
    <div className="flex items-center gap-3">
      {watchers.length ? (
        <AvatarGroup>
          {watchers.slice(0, 5).map((watcher) => (
            <Tooltip key={watcher.adminId}>
              <TooltipTrigger asChild>
                <span>
                  <PersonAvatar name={watcher.displayName || watcher.account} size="sm" />
                </span>
              </TooltipTrigger>
              <TooltipContent>{watcher.displayName || watcher.account}</TooltipContent>
            </Tooltip>
          ))}
        </AvatarGroup>
      ) : (
        <span className="text-xs text-muted-foreground">还没有人关注</span>
      )}
      {ticket.permissions?.watch ? (
        <Button size="sm" variant={watching ? "secondary" : "outline"} className="ml-auto" onClick={() => void toggle()} disabled={watchMut.isPending}>
          {watching ? <BellOff /> : <Bell />}
          {watching ? "取消关注" : "关注"}
        </Button>
      ) : null}
    </div>
  );
}

function Timeline({ ticket }: { ticket: TicketItem }) {
  const events = [...(ticket.events ?? [])].reverse();
  if (!events.length) return <p className="text-xs text-muted-foreground">暂无操作记录</p>;
  return (
    <ol className="relative space-y-3 pl-4">
      <span aria-hidden className="absolute bottom-1 left-[3px] top-1 w-px bg-border" />
      {events.map((event) => (
        <li key={event.id} className="relative">
          <span
            aria-hidden
            className={cn(
              "absolute -left-4 top-1.5 size-[7px] rounded-full ring-2 ring-background",
              event.event === "sla_breached" ? "bg-destructive" : event.actorType === "user" ? "bg-sky-500" : "bg-muted-foreground/50"
            )}
          />
          <p className="text-xs leading-5 text-foreground">{event.summary || event.event}</p>
          <p className="text-xs text-muted-foreground">
            {event.actorName || "系统"}，{formatRelativeTime(event.createdAt)}
          </p>
        </li>
      ))}
    </ol>
  );
}

