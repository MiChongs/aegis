"use client";

import {
  CheckCircle2,
  Copy,
  Link2,
  Lock,
  LockOpen,
  MessageSquareReply,
  NotebookPen,
  RotateCcw,
  Trash2,
  UserCheck,
  UserRound,
  UserX,
  XCircle
} from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut
} from "@/components/ui/command";
import type { TicketItem, TicketPriority, TicketStatus } from "@/lib/api/tickets";
import { PRIORITY_LABEL, STATUS_LABEL, StatusDot, isTerminalStatus } from "@/components/tickets/ticket-shared";
import { copyText } from "@/components/tickets/ticket-shared";
import type { DeskActions } from "./use-desk-actions";
import { deskLink, useDesk } from "./desk-context";

/** 需要二次确认的动作：关闭、不采纳（带理由）、删除 */
export type DeskConfirm = "close" | "reject" | "delete";

// 操作菜单（cmdk）：对当前这一条能做的事都列在这里，可检索。
// 页头的「⋯」和快捷键 . 打开的是同一个菜单，不必记每个动作各自的快捷键。

// 进行中的三个状态直接切换；「不采纳」「关闭」要先确认（不采纳还要填理由），走 onConfirm
const STATUSES: TicketStatus[] = ["open", "processing", "pending"];
const PRIORITIES: TicketPriority[] = ["urgent", "high", "normal", "low"];

export function DeskActionMenu({
  open,
  onOpenChange,
  ticket,
  actions,
  onCompose,
  onResolve,
  onShowUser,
  onConfirm
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  ticket: TicketItem;
  actions: DeskActions;
  onCompose: (mode: "reply" | "internal") => void;
  onResolve: () => void;
  onShowUser: () => void;
  onConfirm: (kind: DeskConfirm) => void;
}) {
  const desk = useDesk();
  const perms = ticket.permissions;
  const terminal = isTerminalStatus(ticket.status);

  // 先收起菜单再执行：后续动作可能要聚焦别处（回复框、解决说明）
  const run = (action: () => void) => () => {
    onOpenChange(false);
    requestAnimationFrame(action);
  };

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} title="操作" description={`对当前${desk.noun}执行操作`}>
      <CommandInput placeholder="搜索操作" />
      <CommandList>
        <CommandEmpty>没有匹配的操作</CommandEmpty>

        <CommandGroup heading="处理">
          {perms?.reply ? (
            <CommandItem onSelect={run(() => onCompose("reply"))}>
              <MessageSquareReply />
              回复
              <CommandShortcut>R</CommandShortcut>
            </CommandItem>
          ) : null}
          {perms?.internalNote ? (
            <CommandItem onSelect={run(() => onCompose("internal"))}>
              <NotebookPen />
              写内部备注
              <CommandShortcut>N</CommandShortcut>
            </CommandItem>
          ) : null}
          {perms?.changeStatus && !terminal ? (
            <CommandItem onSelect={run(onResolve)}>
              <CheckCircle2 />
              标记为已解决
              <CommandShortcut>E</CommandShortcut>
            </CommandItem>
          ) : null}
          {terminal && perms?.reopen ? (
            <CommandItem onSelect={run(() => void actions.changeStatus("processing"))}>
              <RotateCcw />
              重新打开
            </CommandItem>
          ) : null}
          {perms?.assign && ticket.assigneeAdminId !== actions.operatorId ? (
            <CommandItem onSelect={run(() => void actions.assignToMe())}>
              <UserCheck />
              指派给我
              <CommandShortcut>A</CommandShortcut>
            </CommandItem>
          ) : null}
          {perms?.assign && ticket.assigneeAdminId ? (
            <CommandItem onSelect={run(() => void actions.assign(null))}>
              <UserX />
              取消指派
            </CommandItem>
          ) : null}
        </CommandGroup>

        {perms?.changeStatus && !terminal ? (
          <>
            <CommandSeparator />
            <CommandGroup heading="状态">
              {STATUSES.filter((status) => status !== ticket.status).map((status) => (
                <CommandItem key={status} value={`状态 ${STATUS_LABEL[status]}`} onSelect={run(() => void actions.changeStatus(status))}>
                  <StatusDot status={status} className="mx-1" />
                  改为{STATUS_LABEL[status]}
                </CommandItem>
              ))}
              <CommandItem value={`状态 ${STATUS_LABEL.rejected}`} onSelect={run(() => onConfirm("reject"))}>
                <StatusDot status="rejected" className="mx-1" />
                改为{STATUS_LABEL.rejected}
              </CommandItem>
            </CommandGroup>
          </>
        ) : null}

        {perms?.edit ? (
          <>
            <CommandSeparator />
            <CommandGroup heading="优先级">
              {PRIORITIES.filter((priority) => priority !== ticket.priority).map((priority) => (
                <CommandItem key={priority} value={`优先级 ${PRIORITY_LABEL[priority]}`} onSelect={run(() => void actions.setPriority(priority))}>
                  优先级改为{PRIORITY_LABEL[priority]}
                </CommandItem>
              ))}
            </CommandGroup>
          </>
        ) : null}

        <CommandSeparator />
        <CommandGroup heading="其他">
          <CommandItem onSelect={run(onShowUser)}>
            <UserRound />
            查看{desk.person}资料
          </CommandItem>
          <CommandItem onSelect={run(() => void copyText(ticket.ticketNo, "编号"))}>
            <Copy />
            复制编号
          </CommandItem>
          <CommandItem onSelect={run(() => void copyText(`${window.location.origin}${deskLink(desk, ticket.id)}`, "链接"))}>
            <Link2 />
            复制链接
          </CommandItem>
          {perms?.edit ? (
            <CommandItem onSelect={run(() => void actions.toggleLock())}>
              {ticket.locked ? <LockOpen /> : <Lock />}
              {ticket.locked ? "解除锁定" : "锁定"}
            </CommandItem>
          ) : null}
          {perms?.close && ticket.status !== "closed" ? (
            <CommandItem onSelect={run(() => onConfirm("close"))}>
              <XCircle />
              关闭{desk.noun}
            </CommandItem>
          ) : null}
          {perms?.delete ? (
            <CommandItem className="text-destructive data-[selected=true]:text-destructive [&_svg]:!text-destructive" onSelect={run(() => onConfirm("delete"))}>
              <Trash2 />
              删除{desk.noun}
            </CommandItem>
          ) : null}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
