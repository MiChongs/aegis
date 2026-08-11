"use client";

import { useMemo, useState } from "react";
import {
  CheckCircle2,
  Clock,
  Eye,
  Lock,
  Paperclip,
  Send,
  Star,
  Trash2,
  UserCheck,
  Users,
  Zap
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { LoadingState } from "@/components/ui/data-state";
import {
  useAdminOptions,
  useAssignTicketMutation,
  useChangeTicketStatusMutation,
  useDeleteTicketMutation,
  useReplyTicketMutation,
  useTicketDetailQuery,
  useTicketGroupsQuery,
  useTicketQuickRepliesQuery,
  useUpdateTicketMutation,
  useUploadTicketAttachmentMutation,
  useWatchTicketMutation
} from "@/lib/ticket-hooks";
import type { TicketAttachment, TicketPriority, TicketStatus } from "@/lib/api/tickets";
import {
  PRIORITY_LABEL,
  STATUS_LABEL,
  PriorityBadge,
  SLABadge,
  StatusBadge,
  formatBytes,
  formatDateTime,
  formatDue,
  formatRelativeTime
} from "./ticket-shared";
import { cn } from "@/lib/utils";

// 工单详情抽屉。
//
// 关键点：所有按钮的显隐都读后端返回的 `permissions`（ActionSet），
// 而不是前端自己按角色猜。这样「组成员可以回复自己名下的工单、但不能删除」
// 这类规则只在服务端实现一次，前端不会出现「点了才 403」。

type Props = {
  ticketId: number | null;
  onClose: () => void;
};

export function TicketDetailSheet({ ticketId, onClose }: Props) {
  const detailQuery = useTicketDetailQuery(ticketId);
  const ticket = detailQuery.data;

  const replyMut = useReplyTicketMutation();
  const statusMut = useChangeTicketStatusMutation();
  const assignMut = useAssignTicketMutation();
  const updateMut = useUpdateTicketMutation();
  const deleteMut = useDeleteTicketMutation();
  const watchMut = useWatchTicketMutation();
  const uploadMut = useUploadTicketAttachmentMutation();

  const admins = useAdminOptions();
  const groupsQuery = useTicketGroupsQuery(ticket?.appid ?? 0);
  const quickRepliesQuery = useTicketQuickRepliesQuery(ticket?.appid ?? 0);

  const [replyContent, setReplyContent] = useState("");
  const [internal, setInternal] = useState(false);
  const [nextStatus, setNextStatus] = useState<string>("");
  const [pendingFiles, setPendingFiles] = useState<TicketAttachment[]>([]);
  const [resolveSolution, setResolveSolution] = useState("");

  // 切换工单时重置草稿，避免把上一单的回复带过去。
  // 用「渲染期比对上一次 prop」而不是 useEffect：后者会多跑一轮渲染，
  // 且在抽屉快速切换时可能把新工单的输入清掉。
  const [draftForTicket, setDraftForTicket] = useState<number | null>(ticketId);
  if (ticketId !== draftForTicket) {
    setDraftForTicket(ticketId);
    setReplyContent("");
    setInternal(false);
    setNextStatus("");
    setPendingFiles([]);
    setResolveSolution("");
  }

  const perms = ticket?.permissions;
  const groups = groupsQuery.data ?? [];
  const quickReplies = quickRepliesQuery.data ?? [];

  const firstResponseDue = useMemo(() => formatDue(ticket?.firstResponseDueAt), [ticket?.firstResponseDueAt]);
  const resolveDue = useMemo(() => formatDue(ticket?.resolveDueAt), [ticket?.resolveDueAt]);

  const handleError = (error: unknown, fallback: string) => {
    const message = error instanceof ApiError ? error.message : fallback;
    notify.error(message);
  };

  const handleUpload = async (files: FileList | null) => {
    if (!files?.length || !ticket) return;
    try {
      const uploaded: TicketAttachment[] = [];
      for (const file of Array.from(files)) {
        uploaded.push(await uploadMut.mutateAsync({ file, appid: ticket.appid, ticketId: ticket.id }));
      }
      setPendingFiles((prev) => [...prev, ...uploaded]);
      notify.success(`已上传 ${uploaded.length} 个附件`);
    } catch (error) {
      handleError(error, "附件上传失败");
    }
  };

  const handleReply = async () => {
    if (!ticket) return;
    if (!replyContent.trim()) {
      notify.warning("请填写回复内容");
      return;
    }
    try {
      await replyMut.mutateAsync({
        id: ticket.id,
        payload: {
          content: replyContent.trim(),
          internal,
          nextStatus: (nextStatus as TicketStatus) || "",
          attachmentIds: pendingFiles.map((file) => file.id)
        }
      });
      setReplyContent("");
      setPendingFiles([]);
      setNextStatus("");
      notify.success(internal ? "内部备注已添加" : "回复已发送");
    } catch (error) {
      handleError(error, "回复失败");
    }
  };

  const handleStatus = async (status: TicketStatus, solution?: string) => {
    if (!ticket) return;
    try {
      await statusMut.mutateAsync({ id: ticket.id, status, solution });
      setResolveSolution("");
      notify.success(`已更新为「${STATUS_LABEL[status]}」`);
    } catch (error) {
      handleError(error, "状态更新失败");
    }
  };

  const handleAssign = async (assigneeAdminId: number | null, groupId?: number | null) => {
    if (!ticket) return;
    try {
      await assignMut.mutateAsync({
        id: ticket.id,
        payload: {
          assigneeAdminId,
          groupId: groupId === undefined ? ticket.groupId ?? null : groupId
        }
      });
      notify.success("指派已更新");
    } catch (error) {
      handleError(error, "指派失败");
    }
  };

  const handlePriority = async (priority: TicketPriority) => {
    if (!ticket) return;
    try {
      await updateMut.mutateAsync({ id: ticket.id, payload: { priority } });
      notify.success(`优先级已调整为「${PRIORITY_LABEL[priority]}」`);
    } catch (error) {
      handleError(error, "优先级调整失败");
    }
  };

  const handleDelete = async () => {
    if (!ticket) return;
    try {
      await deleteMut.mutateAsync(ticket.id);
      notify.success("工单已删除");
      onClose();
    } catch (error) {
      handleError(error, "删除失败");
    }
  };

  return (
    <Sheet open={Boolean(ticketId)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 p-0 sm:max-w-3xl">
        {detailQuery.isLoading || !ticket ? (
          <div className="p-6">
            <LoadingState title="加载工单" description="正在拉取工单会话与时间线..." />
          </div>
        ) : (
          <div className="flex h-full flex-col">
            <SheetHeader className="border-b px-6 py-4">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" size="sm" className="font-mono">
                  {ticket.ticketNo}
                </Badge>
                <StatusBadge status={ticket.status} />
                <PriorityBadge priority={ticket.priority} />
                <SLABadge state={ticket.slaState} />
                {ticket.locked ? (
                  <Badge variant="secondary" size="sm">
                    <Lock className="mr-1 size-3" />
                    已锁定
                  </Badge>
                ) : null}
              </div>
              <SheetTitle className="text-left text-base">{ticket.title}</SheetTitle>
              <SheetDescription className="text-left">
                {ticket.requesterName} · {ticket.appName || `应用 #${ticket.appid}`} ·{" "}
                {formatRelativeTime(ticket.createdAt)}提交
              </SheetDescription>
            </SheetHeader>

            <Tabs defaultValue="conversation" className="flex min-h-0 flex-1 flex-col">
              <TabsList className="mx-6 mt-3 w-fit">
                <TabsTrigger value="conversation">会话</TabsTrigger>
                <TabsTrigger value="timeline">时间线</TabsTrigger>
                <TabsTrigger value="properties">属性</TabsTrigger>
              </TabsList>

              {/* ── 会话 ── */}
              <TabsContent value="conversation" className="mt-0 flex min-h-0 flex-1 flex-col">
                <ScrollArea className="min-h-0 flex-1 px-6 py-4">
                  <div className="space-y-4">
                    {(ticket.messages ?? []).map((message) => (
                      <div
                        key={message.id}
                        className={cn(
                          "rounded-xl border p-3",
                          message.internal
                            ? "border-amber-200 bg-amber-50/60 dark:border-amber-900/60 dark:bg-amber-950/30"
                            : message.authorType === "agent"
                              ? "border-border bg-muted/40"
                              : "border-border bg-background"
                        )}
                      >
                        <div className="mb-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                          <span className="font-medium text-foreground">{message.authorName || "系统"}</span>
                          <Badge variant="outline" size="sm">
                            {message.authorType === "agent"
                              ? "处理人"
                              : message.authorType === "requester"
                                ? "提单人"
                                : "系统"}
                          </Badge>
                          {message.internal ? (
                            <Badge variant="warning" size="sm">
                              <Eye className="mr-1 size-3" />
                              内部可见
                            </Badge>
                          ) : null}
                          <span>{formatDateTime(message.createdAt)}</span>
                        </div>
                        <p className="whitespace-pre-wrap text-sm leading-6 text-foreground">
                          {message.content}
                        </p>
                        {message.attachments?.length ? (
                          <div className="mt-2 flex flex-wrap gap-2">
                            {message.attachments.map((file) => (
                              <a
                                key={file.id}
                                href={file.downloadUrl || "#"}
                                target="_blank"
                                rel="noreferrer"
                                className="inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-xs text-muted-foreground hover:bg-accent"
                              >
                                <Paperclip className="size-3" />
                                {file.fileName}
                                <span className="text-[10px]">{formatBytes(file.sizeBytes)}</span>
                              </a>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    ))}
                    {!ticket.messages?.length ? (
                      <p className="py-8 text-center text-sm text-muted-foreground">暂无会话记录</p>
                    ) : null}
                  </div>
                </ScrollArea>

                {/* 回复区。无回复权限（或工单已终结）时整块隐藏，避免误导 */}
                {perms?.reply || perms?.internalNote ? (
                  <div className="border-t px-6 py-3">
                    {quickReplies.length > 0 ? (
                      <div className="mb-2 flex flex-wrap gap-1">
                        {quickReplies.slice(0, 6).map((item) => (
                          <Button
                            key={item.id}
                            size="sm"
                            variant="outline"
                            className="h-7 text-xs"
                            onClick={() => setReplyContent((prev) => (prev ? `${prev}\n${item.content}` : item.content))}
                          >
                            {item.title}
                          </Button>
                        ))}
                      </div>
                    ) : null}
                    <Textarea
                      rows={3}
                      value={replyContent}
                      onChange={(event) => setReplyContent(event.target.value)}
                      placeholder={internal ? "内部备注仅处理人可见，提单人看不到" : "回复内容将展示给提单人"}
                    />
                    {pendingFiles.length > 0 ? (
                      <div className="mt-2 flex flex-wrap gap-2">
                        {pendingFiles.map((file) => (
                          <Badge key={file.id} variant="outline" size="sm">
                            <Paperclip className="mr-1 size-3" />
                            {file.fileName}
                          </Badge>
                        ))}
                      </div>
                    ) : null}
                    <div className="mt-2 flex flex-wrap items-center gap-2">
                      {perms?.internalNote ? (
                        <div className="flex items-center gap-2">
                          <Switch id="internal-note" checked={internal} onCheckedChange={setInternal} />
                          <Label htmlFor="internal-note" className="text-xs text-muted-foreground">
                            内部备注
                          </Label>
                        </div>
                      ) : null}
                      {perms?.changeStatus ? (
                        <Select value={nextStatus} onValueChange={setNextStatus}>
                          <SelectTrigger className="h-8 w-40 text-xs">
                            <SelectValue placeholder="回复后状态不变" />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="processing">处理中</SelectItem>
                            <SelectItem value="pending_user">待用户补充</SelectItem>
                            <SelectItem value="pending_third_party">等待第三方</SelectItem>
                            <SelectItem value="resolved">已解决</SelectItem>
                          </SelectContent>
                        </Select>
                      ) : null}
                      {perms?.uploadAttachment ? (
                        <label className="inline-flex cursor-pointer items-center gap-1 rounded-lg border px-2 py-1 text-xs text-muted-foreground hover:bg-accent">
                          <Paperclip className="size-3" />
                          附件
                          <input
                            type="file"
                            multiple
                            className="hidden"
                            onChange={(event) => handleUpload(event.target.files)}
                          />
                        </label>
                      ) : null}
                      <Button
                        size="sm"
                        className="ml-auto"
                        disabled={replyMut.isPending || (!perms?.reply && !internal)}
                        onClick={handleReply}
                      >
                        <Send className="mr-1 size-3.5" />
                        发送
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="border-t px-6 py-3 text-xs text-muted-foreground">
                    {ticket.locked ? "工单已关闭并锁定，无法继续回复。" : "你没有回复该工单的权限。"}
                  </div>
                )}
              </TabsContent>

              {/* ── 时间线 ── */}
              <TabsContent value="timeline" className="mt-0 min-h-0 flex-1">
                <ScrollArea className="h-full px-6 py-4">
                  <ol className="relative space-y-4 border-l pl-5">
                    {(ticket.events ?? []).map((event) => (
                      <li key={event.id} className="relative">
                        <span className="absolute -left-[23px] top-1.5 size-2 rounded-full bg-muted-foreground/60" />
                        <p className="text-sm text-foreground">{event.summary || event.event}</p>
                        <p className="text-xs text-muted-foreground">
                          {event.actorName || "系统"} · {formatDateTime(event.createdAt)}
                        </p>
                      </li>
                    ))}
                    {!ticket.events?.length ? (
                      <li className="text-sm text-muted-foreground">暂无操作记录</li>
                    ) : null}
                  </ol>
                </ScrollArea>
              </TabsContent>

              {/* ── 属性 ── */}
              <TabsContent value="properties" className="mt-0 min-h-0 flex-1">
                <ScrollArea className="h-full px-6 py-4">
                  <div className="space-y-4">
                    <section className="grid gap-3 sm:grid-cols-2">
                      <Field label="提单人" value={ticket.requesterName} />
                      <Field label="联系方式" value={ticket.requesterContact || "—"} />
                      <Field label="分类" value={ticket.categoryName || "未分类"} />
                      <Field label="来源" value={ticket.source} />
                      <Field label="受理人" value={ticket.assigneeName || "未指派"} />
                      <Field label="处理组" value={ticket.groupName || "未指定"} />
                      <Field label="创建时间" value={formatDateTime(ticket.createdAt)} />
                      <Field label="最后更新" value={formatRelativeTime(ticket.updatedAt)} />
                      <Field
                        label="首响时限"
                        value={ticket.firstRespondedAt ? `已响应 · ${formatDateTime(ticket.firstRespondedAt)}` : firstResponseDue.text}
                        danger={!ticket.firstRespondedAt && firstResponseDue.overdue}
                      />
                      <Field
                        label="解决时限"
                        value={ticket.resolvedAt ? `已解决 · ${formatDateTime(ticket.resolvedAt)}` : resolveDue.text}
                        danger={!ticket.resolvedAt && resolveDue.overdue}
                      />
                      {ticket.rating ? (
                        <Field label="满意度" value={`${ticket.rating} 星 ${ticket.ratingComment || ""}`} />
                      ) : null}
                    </section>

                    {ticket.tags?.length ? (
                      <div className="flex flex-wrap gap-1">
                        {ticket.tags.map((tag) => (
                          <Badge key={tag} variant="secondary" size="sm">
                            {tag}
                          </Badge>
                        ))}
                      </div>
                    ) : null}

                    <Separator />

                    {/* 指派与优先级 */}
                    {perms?.assign ? (
                      <section className="space-y-2">
                        <p className="text-xs font-medium text-muted-foreground">指派</p>
                        <div className="grid gap-2 sm:grid-cols-2">
                          <Select
                            value={ticket.assigneeAdminId ? String(ticket.assigneeAdminId) : "none"}
                            onValueChange={(value) => handleAssign(value === "none" ? null : Number(value))}
                          >
                            <SelectTrigger className="h-9">
                              <SelectValue placeholder="选择受理人" />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="none">未指派</SelectItem>
                              {admins.map((admin) => (
                                <SelectItem key={admin.id} value={String(admin.id)}>
                                  {admin.name}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <Select
                            value={ticket.groupId ? String(ticket.groupId) : "none"}
                            onValueChange={(value) =>
                              handleAssign(ticket.assigneeAdminId ?? null, value === "none" ? null : Number(value))
                            }
                          >
                            <SelectTrigger className="h-9">
                              <SelectValue placeholder="选择处理组" />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="none">不指定处理组</SelectItem>
                              {groups.map((group) => (
                                <SelectItem key={group.id} value={String(group.id)}>
                                  {group.name}（{group.memberCount} 人）
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                        {ticket.groupId ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={async () => {
                              try {
                                await assignMut.mutateAsync({
                                  id: ticket.id,
                                  payload: { groupId: ticket.groupId, autoPick: true }
                                });
                                notify.success("已按处理组策略自动分派");
                              } catch (error) {
                                handleError(error, "自动分派失败");
                              }
                            }}
                          >
                            <UserCheck className="mr-1 size-3.5" />
                            按组策略自动分派
                          </Button>
                        ) : null}
                      </section>
                    ) : null}

                    {perms?.edit ? (
                      <section className="space-y-2">
                        <p className="text-xs font-medium text-muted-foreground">优先级</p>
                        <div className="flex flex-wrap gap-1">
                          {(["low", "normal", "high", "urgent"] as TicketPriority[]).map((priority) => (
                            <Button
                              key={priority}
                              size="sm"
                              variant={ticket.priority === priority ? "default" : "outline"}
                              onClick={() => handlePriority(priority)}
                            >
                              {priority === "urgent" ? <Zap className="mr-1 size-3" /> : null}
                              {PRIORITY_LABEL[priority]}
                            </Button>
                          ))}
                        </div>
                      </section>
                    ) : null}

                    {ticket.watchers?.length ? (
                      <section className="space-y-1">
                        <p className="text-xs font-medium text-muted-foreground">
                          <Users className="mr-1 inline size-3" />
                          关注人
                        </p>
                        <div className="flex flex-wrap gap-1">
                          {ticket.watchers.map((watcher) => (
                            <Badge key={watcher.adminId} variant="outline" size="sm">
                              {watcher.displayName || watcher.account}
                            </Badge>
                          ))}
                        </div>
                      </section>
                    ) : null}
                  </div>
                </ScrollArea>
              </TabsContent>
            </Tabs>

            {/* 底部操作条 */}
            <div className="flex flex-wrap items-center gap-2 border-t px-6 py-3">
              {perms?.changeStatus && ticket.status !== "resolved" ? (
                <div className="flex flex-1 items-center gap-2">
                  <Input
                    value={resolveSolution}
                    onChange={(event) => setResolveSolution(event.target.value)}
                    placeholder="解决方案（可选，会作为回复发给提单人）"
                    className="h-8 flex-1 text-xs"
                  />
                  <Button size="sm" onClick={() => handleStatus("resolved", resolveSolution.trim() || undefined)}>
                    <CheckCircle2 className="mr-1 size-3.5" />
                    标记已解决
                  </Button>
                </div>
              ) : null}
              {perms?.close && ticket.status !== "closed" ? (
                <Button size="sm" variant="outline" onClick={() => handleStatus("closed")}>
                  关闭工单
                </Button>
              ) : null}
              {perms?.reopen ? (
                <Button size="sm" variant="outline" onClick={() => handleStatus("processing")}>
                  <Clock className="mr-1 size-3.5" />
                  重新打开
                </Button>
              ) : null}
              {perms?.watch ? (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={async () => {
                    try {
                      await watchMut.mutateAsync({ id: ticket.id, watch: true });
                      notify.success("已关注该工单，后续动态会通知你");
                    } catch (error) {
                      handleError(error, "关注失败");
                    }
                  }}
                >
                  <Star className="mr-1 size-3.5" />
                  关注
                </Button>
              ) : null}
              {perms?.delete ? (
                <Button size="sm" variant="ghost" className="text-destructive" onClick={handleDelete}>
                  <Trash2 className="mr-1 size-3.5" />
                  删除
                </Button>
              ) : null}
            </div>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function Field({ label, value, danger }: { label: string; value: string; danger?: boolean }) {
  return (
    <div className="space-y-0.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={cn("text-sm", danger ? "font-medium text-destructive" : "text-foreground")}>{value}</p>
    </div>
  );
}
