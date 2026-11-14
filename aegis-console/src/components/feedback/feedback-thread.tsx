"use client";

import { Fragment, useMemo, useState, type Ref } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import { StickToBottom, useStickToBottomContext } from "use-stick-to-bottom";
import {
  AlarmClock,
  ArrowDown,
  ArrowRightLeft,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Lock,
  MoreHorizontal,
  PanelRightClose,
  PanelRightOpen,
  Pencil,
  RotateCcw,
  Star,
  Tag,
  UserCheck,
  X,
  XCircle
} from "lucide-react";
import { ApiError } from "@/lib/api/client";
import { Markdown } from "@/components/ai/markdown";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAdminOptions, useTicketCategoriesQuery, useTicketDetailQuery } from "@/lib/ticket-hooks";
import type { TicketEvent, TicketItem, TicketMessage, TicketPriority, TicketStatus } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import {
  Kbd,
  PRIORITY_LABEL,
  PersonAvatar,
  STATUS_LABEL,
  StatusDot,
  formatClientShort,
  formatDateTime,
  formatRelativeTime,
  getTicketClient,
  isTerminalStatus
} from "@/components/tickets/ticket-shared";
import { TicketAttachmentList } from "@/components/tickets/ticket-attachments";
import { FeedbackActionMenu } from "./feedback-action-menu";
import { FeedbackComposer, type FeedbackComposerHandle } from "./feedback-composer";
import { CategoryDot, CategoryTag, PropertyMenu, type PropertyOption } from "./feedback-kit";
import { useFeedbackActions, type FeedbackActions } from "./use-feedback-actions";

// 反馈详情：页头 + 会话 + 回复框。
//
// 页头按「人 → 标题 → 属性」排：先看清是谁，再看说了什么，最后是一行可直接改的属性胶囊
// （状态、分类、优先级、受理人）。按钮显隐一律读后端返回的 permissions，不在前端按角色猜。

type Props = {
  ticketId: number;
  onClose: () => void;
  onStep: (direction: 1 | -1) => void;
  canStep: { prev: boolean; next: boolean };
  composerRef: Ref<FeedbackComposerHandle>;
  panelOpen: boolean;
  onTogglePanel: () => void;
  /** 打开右侧的用户资料 */
  onShowUser: () => void;
  /** 回复、写备注：交给工作台去聚焦回复框 */
  onCompose: (mode: "reply" | "internal") => void;
  /** 右侧面板：拿到详情数据后由工作台渲染，这里只负责留位置 */
  children?: (ticket: TicketItem) => React.ReactNode;
};

export function FeedbackThread({
  ticketId,
  onClose,
  onStep,
  canStep,
  composerRef,
  panelOpen,
  onTogglePanel,
  onShowUser,
  onCompose,
  children
}: Props) {
  const detailQuery = useTicketDetailQuery(ticketId);
  const ticket = detailQuery.data;

  if (detailQuery.isError) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
        <XCircle className="size-8 text-muted-foreground" />
        <p className="text-sm text-foreground">{detailQuery.error instanceof ApiError ? detailQuery.error.message : "详情加载失败"}</p>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => void detailQuery.refetch()}>
            重试
          </Button>
          <Button size="sm" variant="ghost" onClick={onClose}>
            关闭
          </Button>
        </div>
      </div>
    );
  }

  if (!ticket) return <ThreadSkeleton />;

  return (
    <div className="flex h-full min-h-0">
      <div className="flex min-w-0 flex-1 flex-col">
        <ThreadHeader
          key={ticket.id}
          ticket={ticket}
          onClose={onClose}
          onStep={onStep}
          canStep={canStep}
          panelOpen={panelOpen}
          onTogglePanel={onTogglePanel}
          onShowUser={onShowUser}
          onCompose={onCompose}
        />
        <Conversation key={`c-${ticket.id}`} ticket={ticket} />
        <FeedbackComposer key={`r-${ticket.id}`} ticket={ticket} ref={composerRef} />
      </div>
      {children?.(ticket)}
    </div>
  );
}

// ─────────────── 页头 ───────────────

const STATUS_ORDER: TicketStatus[] = ["open", "processing", "pending_user", "pending_third_party", "resolved", "closed"];
const PRIORITY_ORDER: TicketPriority[] = ["urgent", "high", "normal", "low"];

const PRIORITY_TONE: Record<TicketPriority, string> = {
  urgent: "bg-red-500",
  high: "bg-amber-500",
  normal: "bg-sky-500",
  low: "bg-muted-foreground/40"
};

function ThreadHeader({
  ticket,
  onClose,
  onStep,
  canStep,
  panelOpen,
  onTogglePanel,
  onShowUser,
  onCompose
}: {
  ticket: TicketItem;
  onClose: () => void;
  onStep: (direction: 1 | -1) => void;
  canStep: { prev: boolean; next: boolean };
  panelOpen: boolean;
  onTogglePanel: () => void;
  onShowUser: () => void;
  onCompose: (mode: "reply" | "internal") => void;
}) {
  const perms = ticket.permissions;
  const actions = useFeedbackActions(ticket);
  const client = formatClientShort(getTicketClient(ticket.metadata));
  const terminal = isTerminalStatus(ticket.status);

  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState(ticket.title);
  const [resolveOpen, setResolveOpen] = useState(false);
  const [solution, setSolution] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [confirm, setConfirm] = useState<"close" | "delete" | null>(null);

  const canResolve = Boolean(perms?.changeStatus) && !terminal;

  useHotkeys("e", () => setResolveOpen(true), { enabled: canResolve, preventDefault: true });
  useHotkeys("a", () => void actions.assignToMe(), { enabled: Boolean(perms?.assign) && ticket.assigneeAdminId !== actions.operatorId });
  useHotkeys("period", () => setMenuOpen(true), { preventDefault: true });

  const saveTitle = () => {
    const next = titleDraft.trim();
    setEditingTitle(false);
    if (next && next !== ticket.title) void actions.setTitle(next);
  };

  const resolve = async () => {
    if (await actions.changeStatus("resolved", solution.trim() || undefined)) {
      setSolution("");
      setResolveOpen(false);
    }
  };

  return (
    <header className="border-b px-5 pb-3 pt-3">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onShowUser}
          className="group/who flex min-w-0 items-center gap-3 rounded-lg text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
        >
          <PersonAvatar name={ticket.requesterName} src={ticket.requesterAvatar} size="lg" />
          <span className="min-w-0">
            <span className="block truncate text-sm font-semibold text-foreground underline-offset-4 group-hover/who:underline">
              {ticket.requesterName || "匿名用户"}
            </span>
            <span className="block truncate text-xs text-muted-foreground" title={formatDateTime(ticket.createdAt)}>
              {ticket.appName ? <span className="mr-2">{ticket.appName}</span> : null}
              {client ? <span className="mr-2">{client}</span> : null}
              {formatRelativeTime(ticket.createdAt)}提交
            </span>
          </span>
        </button>

        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          <IconAction label="上一条" hint="K" disabled={!canStep.prev} onClick={() => onStep(-1)}>
            <ChevronUp />
          </IconAction>
          <IconAction label="下一条" hint="J" disabled={!canStep.next} onClick={() => onStep(1)}>
            <ChevronDown />
          </IconAction>
          <span aria-hidden className="mx-1 h-4 w-px bg-border" />
          <IconAction label={panelOpen ? "收起资料" : "展开资料"} hint="I" onClick={onTogglePanel}>
            {panelOpen ? <PanelRightClose /> : <PanelRightOpen />}
          </IconAction>
          <IconAction label="操作" hint="." onClick={() => setMenuOpen(true)}>
            <MoreHorizontal />
          </IconAction>
          <IconAction label="关闭" hint="Esc" onClick={onClose}>
            <X />
          </IconAction>
        </div>
      </div>

      <div className="mt-3 flex items-start gap-3">
        {editingTitle ? (
          <Input
            autoFocus
            value={titleDraft}
            onChange={(event) => setTitleDraft(event.target.value)}
            onBlur={saveTitle}
            onKeyDown={(event) => {
              if (event.key === "Enter") saveTitle();
              if (event.key === "Escape") {
                setTitleDraft(ticket.title);
                setEditingTitle(false);
              }
            }}
            aria-label="标题"
            className="h-8 flex-1 text-lg font-semibold"
          />
        ) : (
          <h2 className="group/title flex min-w-0 flex-1 items-center gap-1.5 text-lg font-semibold leading-8 text-foreground">
            <span className="truncate">{ticket.title}</span>
            {perms?.edit ? (
              <button
                type="button"
                onClick={() => {
                  setTitleDraft(ticket.title);
                  setEditingTitle(true);
                }}
                aria-label="修改标题"
                className="rounded p-1 text-muted-foreground opacity-0 outline-none transition-opacity hover:bg-muted focus-visible:opacity-100 group-hover/title:opacity-100"
              >
                <Pencil className="size-3.5" />
              </button>
            ) : null}
          </h2>
        )}

        {canResolve ? (
          <Popover open={resolveOpen} onOpenChange={setResolveOpen}>
            <Tooltip>
              <TooltipTrigger asChild>
                <PopoverTrigger asChild>
                  <Button size="sm" className="shrink-0">
                    <CheckCircle2 />
                    解决
                  </Button>
                </PopoverTrigger>
              </TooltipTrigger>
              <TooltipContent>
                标记为已解决 <Kbd className="ml-1">E</Kbd>
              </TooltipContent>
            </Tooltip>
            <PopoverContent align="end" className="w-80 space-y-3">
              <p className="text-sm font-medium">解决说明</p>
              <Textarea
                autoFocus
                rows={3}
                value={solution}
                onChange={(event) => setSolution(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                    event.preventDefault();
                    void resolve();
                  }
                }}
                placeholder="选填，反馈人可见"
                aria-label="解决说明"
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={() => setResolveOpen(false)}>
                  取消
                </Button>
                <Button size="sm" disabled={actions.busy} onClick={() => void resolve()}>
                  确认解决
                </Button>
              </div>
            </PopoverContent>
          </Popover>
        ) : perms?.reopen ? (
          <Button size="sm" variant="outline" className="shrink-0" onClick={() => void actions.changeStatus("processing")}>
            <RotateCcw />
            重新打开
          </Button>
        ) : null}
      </div>

      <PropertyBar ticket={ticket} actions={actions} onRequestClose={() => setConfirm("close")} />

      <FeedbackActionMenu
        open={menuOpen}
        onOpenChange={setMenuOpen}
        ticket={ticket}
        actions={actions}
        onCompose={onCompose}
        onResolve={() => setResolveOpen(true)}
        onShowUser={onShowUser}
        onConfirm={setConfirm}
      />

      <AlertDialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm === "delete" ? "删除这条反馈？" : "关闭这条反馈？"}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "delete" ? "会话与附件一并删除，无法恢复。" : "关闭后反馈人不能再回复，可重新打开。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className={cn(confirm === "delete" && "bg-destructive text-white hover:bg-destructive/90")}
              onClick={async () => {
                if (confirm === "delete") {
                  if (await actions.remove()) onClose();
                } else {
                  void actions.changeStatus("closed");
                }
              }}
            >
              {confirm === "delete" ? "删除" : "关闭反馈"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </header>
  );
}

/** 一行属性胶囊：状态、分类、优先级、受理人，点开即改 */
function PropertyBar({
  ticket,
  actions,
  onRequestClose
}: {
  ticket: TicketItem;
  actions: FeedbackActions;
  onRequestClose: () => void;
}) {
  const perms = ticket.permissions;
  const admins = useAdminOptions();
  const categoriesQuery = useTicketCategoriesQuery(ticket.appid);

  const statusOptions: PropertyOption[] = STATUS_ORDER.filter(
    (status) => status !== "closed" || perms?.close || ticket.status === "closed"
  ).map((status) => ({ value: status, label: STATUS_LABEL[status], leading: <StatusDot status={status} /> }));

  const categoryOptions: PropertyOption[] = (categoriesQuery.data ?? [])
    .filter((item) => item.kind === "feedback" && (item.enabled || item.id === ticket.categoryId))
    .map((item) => ({
      value: String(item.id),
      label: item.name,
      leading: <CategoryDot name={item.name} categoryKey={item.key} />
    }));

  const priorityOptions: PropertyOption[] = PRIORITY_ORDER.map((priority) => ({
    value: priority,
    label: PRIORITY_LABEL[priority],
    leading: <span aria-hidden className={cn("size-2 rounded-full", PRIORITY_TONE[priority])} />
  }));

  const assigneeOptions: PropertyOption[] = [
    { value: "none", label: "未指派" },
    // 自己排第一：处理反馈时最常用的就是「指派给我」
    ...[...admins]
      .sort((a, b) => Number(b.id === actions.operatorId) - Number(a.id === actions.operatorId))
      .map((admin) => ({
        value: String(admin.id),
        label: admin.id === actions.operatorId ? `${admin.name}（我）` : admin.name,
        keywords: admin.account,
        leading: <PersonAvatar name={admin.name} size="sm" className="size-5" />
      }))
  ];

  return (
    <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
      <PropertyMenu
        label="状态"
        value={ticket.status}
        options={statusOptions}
        editable={Boolean(perms?.changeStatus)}
        onSelect={(value) => {
          if (value === "closed") onRequestClose();
          else void actions.changeStatus(value as TicketStatus);
        }}
      >
        <StatusDot status={ticket.status} />
        {STATUS_LABEL[ticket.status]}
      </PropertyMenu>

      <PropertyMenu
        label="分类"
        value={ticket.categoryId ? String(ticket.categoryId) : undefined}
        options={categoryOptions}
        editable={Boolean(perms?.edit) && categoryOptions.length > 0}
        onSelect={(value) => void actions.setCategory(Number(value))}
      >
        <CategoryTag name={ticket.categoryName} categoryKey={ticket.categoryKey} className="text-xs font-medium text-foreground" />
      </PropertyMenu>

      <PropertyMenu
        label="优先级"
        value={ticket.priority}
        options={priorityOptions}
        editable={Boolean(perms?.edit)}
        onSelect={(value) => void actions.setPriority(value as TicketPriority)}
      >
        <span aria-hidden className={cn("size-2 shrink-0 rounded-full", PRIORITY_TONE[ticket.priority])} />
        优先级 {PRIORITY_LABEL[ticket.priority]}
      </PropertyMenu>

      <PropertyMenu
        label="受理人"
        value={ticket.assigneeAdminId ? String(ticket.assigneeAdminId) : "none"}
        options={assigneeOptions}
        editable={Boolean(perms?.assign)}
        searchable
        onSelect={(value) => void actions.assign(value === "none" ? null : Number(value))}
      >
        {ticket.assigneeAdminId ? (
          <>
            <PersonAvatar name={ticket.assigneeName} src={ticket.assigneeAvatar} size="sm" className="-ml-1 size-5" />
            <span className="truncate">{ticket.assigneeName}</span>
          </>
        ) : (
          <>
            <UserCheck className="size-3.5 shrink-0 text-muted-foreground" />
            未指派
          </>
        )}
      </PropertyMenu>

      {ticket.slaState === "breached" || ticket.slaState === "warning" ? (
        <Badge variant={ticket.slaState === "breached" ? "danger" : "warning"} size="sm" className="h-7 gap-1 px-2.5">
          <AlarmClock />
          {ticket.slaState === "breached" ? "已超时" : "即将超时"}
        </Badge>
      ) : null}
      {ticket.locked ? (
        <Badge variant="secondary" size="sm" className="h-7 gap-1 px-2.5">
          <Lock />
          已锁定
        </Badge>
      ) : null}
    </div>
  );
}

function IconAction({
  label,
  hint,
  children,
  ...props
}: { label: string; hint?: string; children: React.ReactNode } & Pick<React.ComponentProps<"button">, "onClick" | "disabled">) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon-sm" variant="ghost" aria-label={label} {...props}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        {label}
        {hint ? <Kbd className="ml-1.5">{hint}</Kbd> : null}
      </TooltipContent>
    </Tooltip>
  );
}

// ─────────────── 会话 ───────────────

const GROUP_GAP_MS = 5 * 60_000;

// 穿插进会话的事件；回复、备注本身已经是消息，不重复画
const INLINE_EVENTS = new Set([
  "status_changed",
  "assigned",
  "group_changed",
  "reopened",
  "priority_changed",
  "category_changed",
  "tags_changed",
  "merged"
]);

type Entry =
  | { type: "day"; key: string; label: string }
  | { type: "message"; key: string; message: TicketMessage; head: boolean }
  | { type: "event"; key: string; event: TicketEvent };

function dayLabel(date: Date) {
  const today = new Date();
  const startOf = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const diff = Math.round((startOf(today) - startOf(date)) / 86_400_000);
  if (diff === 0) return "今天";
  if (diff === 1) return "昨天";
  const sameYear = date.getFullYear() === today.getFullYear();
  return date.toLocaleDateString(
    "zh-CN",
    sameYear ? { month: "long", day: "numeric", weekday: "short" } : { year: "numeric", month: "long", day: "numeric" }
  );
}

function clock(value: string) {
  return new Date(value).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function buildEntries(messages: TicketMessage[], events: TicketEvent[]): Entry[] {
  const merged = [
    ...messages.map((message) => ({ at: new Date(message.createdAt).getTime(), message })),
    ...events.filter((event) => INLINE_EVENTS.has(event.event)).map((event) => ({ at: new Date(event.createdAt).getTime(), event }))
  ].sort((a, b) => a.at - b.at);

  const entries: Entry[] = [];
  let lastDay = "";
  let prev: TicketMessage | null = null;
  for (const item of merged) {
    const date = new Date(item.at);
    const day = date.toDateString();
    if (day !== lastDay) {
      entries.push({ type: "day", key: `day-${day}`, label: dayLabel(date) });
      lastDay = day;
      prev = null;
    }
    if ("message" in item) {
      const message = item.message;
      const head =
        !prev ||
        prev.authorType !== message.authorType ||
        prev.authorName !== message.authorName ||
        prev.internal !== message.internal ||
        item.at - new Date(prev.createdAt).getTime() > GROUP_GAP_MS;
      entries.push({ type: "message", key: `m-${message.id}`, message, head });
      prev = message;
    } else {
      entries.push({ type: "event", key: `e-${item.event.id}`, event: item.event });
      prev = null;
    }
  }
  return entries;
}

/**
 * 会话流：所有发言靠左成一列，像评论串而不是聊天气泡。
 * 反馈多数只有一问一答，左右分栏的气泡在宽屏上会把两句话拉到屏幕两端。
 * 用户的话是灰底，客服回复是描边白底并带「客服」标记，内部备注是琥珀色。
 */
function Conversation({ ticket }: { ticket: TicketItem }) {
  const entries = useMemo(() => buildEntries(ticket.messages ?? [], ticket.events ?? []), [ticket.messages, ticket.events]);

  return (
    // 打开时贴到最新一条，有新消息跟着走；用户往上翻就停止跟随
    <StickToBottom className="relative min-h-0 flex-1" initial="instant" resize="smooth">
      <StickToBottom.Content className="mx-auto max-w-3xl px-5 pb-4 pt-5">
        {entries.length === 0 ? <p className="py-16 text-center text-sm text-muted-foreground">暂无会话</p> : null}
        {entries.map((entry) => (
          <Fragment key={entry.key}>
            {entry.type === "day" ? (
              <div className="flex items-center gap-3 py-3 text-xs text-muted-foreground">
                <span className="h-px flex-1 bg-border" />
                {entry.label}
                <span className="h-px flex-1 bg-border" />
              </div>
            ) : entry.type === "event" ? (
              <EventLine event={entry.event} />
            ) : (
              <MessageEntry message={entry.message} head={entry.head} />
            )}
          </Fragment>
        ))}
        {ticket.rating ? <RatingCard ticket={ticket} /> : null}
      </StickToBottom.Content>
      <JumpToLatest />
    </StickToBottom>
  );
}

function JumpToLatest() {
  const { isAtBottom, scrollToBottom } = useStickToBottomContext();
  if (isAtBottom) return null;
  return (
    <Button
      size="sm"
      variant="outline"
      onClick={() => void scrollToBottom()}
      className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-background shadow-sm"
    >
      <ArrowDown />
      最新
    </Button>
  );
}

function MessageEntry({ message, head }: { message: TicketMessage; head: boolean }) {
  const name = message.authorName || "系统";

  if (message.authorType === "system") {
    return <p className="py-2 pl-11 text-xs text-muted-foreground">{message.content}</p>;
  }

  const agent = message.authorType === "agent";
  return (
    <article className={cn("flex gap-3", head ? "pt-3" : "pt-1")}>
      <div className="w-8 shrink-0">{head ? <PersonAvatar name={name} src={message.authorAvatar} /> : null}</div>
      <div className="min-w-0 flex-1">
        {head ? (
          <header className="mb-1 flex items-center gap-2 text-xs text-muted-foreground">
            <span className="text-sm font-medium text-foreground">{name}</span>
            {message.internal ? (
              <Badge variant="warning" size="sm">
                <Lock />
                内部备注
              </Badge>
            ) : agent ? (
              <Badge variant="info" size="sm">
                客服
              </Badge>
            ) : null}
            <time dateTime={message.createdAt} title={formatDateTime(message.createdAt)}>
              {clock(message.createdAt)}
            </time>
            {message.editedAt ? <span>已编辑</span> : null}
          </header>
        ) : null}
        {message.content ? (
          <div
            className={cn(
              "w-fit max-w-full rounded-2xl px-4 py-2.5 text-sm leading-6",
              head && "rounded-tl-md",
              message.internal
                ? "border border-amber-200 bg-amber-50/70 dark:border-amber-900/60 dark:bg-amber-950/30"
                : agent
                  ? "border bg-card"
                  : "bg-muted"
            )}
          >
            <MessageBody message={message} />
          </div>
        ) : null}
        {message.attachments?.length ? <TicketAttachmentList attachments={message.attachments} size="md" className="mt-2" /> : null}
      </div>
    </article>
  );
}

// 用户的纯文本只识别网址；客服回复和用户声明为 Markdown 的内容按 Markdown 渲染（App 端也是这么显示的）
const URL_PATTERN = /(https?:\/\/[^\s<>"']+)/g;

function MessageBody({ message }: { message: TicketMessage }) {
  if (message.authorType === "agent" || message.contentType === "markdown") {
    return <Markdown text={message.content} className="text-sm leading-6 [&_p+p]:mt-2 [&_p]:my-0" />;
  }
  const parts = message.content.split(URL_PATTERN);
  return (
    <p className="whitespace-pre-wrap break-words">
      {parts.map((part, index) =>
        index % 2 === 1 ? (
          <a key={index} href={part} target="_blank" rel="noreferrer" className="text-primary underline underline-offset-2">
            {part}
          </a>
        ) : (
          <Fragment key={index}>{part}</Fragment>
        )
      )}
    </p>
  );
}

const EVENT_ICON: Record<string, typeof ArrowRightLeft> = {
  status_changed: ArrowRightLeft,
  assigned: UserCheck,
  group_changed: UserCheck,
  reopened: RotateCcw,
  tags_changed: Tag
};

function EventLine({ event }: { event: TicketEvent }) {
  const Icon = EVENT_ICON[event.event] ?? ArrowRightLeft;
  return (
    <div className="flex items-center gap-3 py-2 text-xs text-muted-foreground">
      <span className="flex w-8 shrink-0 justify-center">
        <Icon className="size-3.5" />
      </span>
      <span className="min-w-0 truncate">
        <span className="font-medium text-foreground/80">{event.actorName || "系统"}</span> {event.summary || event.event}
      </span>
      <time className="shrink-0" dateTime={event.createdAt} title={formatDateTime(event.createdAt)}>
        {clock(event.createdAt)}
      </time>
    </div>
  );
}

function RatingCard({ ticket }: { ticket: TicketItem }) {
  const rating = ticket.rating ?? 0;
  return (
    <div className="mt-4 flex items-start gap-3 pl-11">
      <div className="rounded-2xl border bg-card px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="flex gap-0.5" role="img" aria-label={`${rating} 星`}>
            {Array.from({ length: 5 }, (_, index) => (
              <Star key={index} className={cn("size-4", index < rating ? "fill-amber-400 text-amber-400" : "text-muted-foreground/30")} />
            ))}
          </span>
          <span className="text-xs text-muted-foreground">
            {ticket.requesterName || "用户"}的评价{ticket.ratedAt ? `，${formatRelativeTime(ticket.ratedAt)}` : ""}
          </span>
        </div>
        {ticket.ratingComment ? <p className="mt-1.5 text-sm text-foreground">{ticket.ratingComment}</p> : null}
      </div>
    </div>
  );
}

function ThreadSkeleton() {
  return (
    <div className="flex h-full flex-col">
      <div className="space-y-3 border-b px-5 py-3">
        <div className="flex items-center gap-3">
          <Skeleton className="size-10 rounded-full" />
          <div className="space-y-1.5">
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-3 w-44" />
          </div>
        </div>
        <Skeleton className="h-6 w-1/2" />
        <div className="flex gap-1.5">
          {[16, 20, 20, 16].map((width, index) => (
            <Skeleton key={index} className="h-7 rounded-full" style={{ width: width * 4 }} />
          ))}
        </div>
      </div>
      <div className="mx-auto w-full max-w-3xl flex-1 space-y-5 p-5">
        {[0, 1].map((index) => (
          <div key={index} className="flex gap-3">
            <Skeleton className="size-8 shrink-0 rounded-full" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-3.5 w-24" />
              <Skeleton className={cn("h-16 rounded-2xl", index === 0 ? "w-2/3" : "w-1/2")} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
