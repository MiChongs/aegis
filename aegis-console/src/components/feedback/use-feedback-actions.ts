"use client";

import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { useAuthStore } from "@/lib/auth-store";
import {
  useAssignTicketMutation,
  useChangeTicketStatusMutation,
  useDeleteTicketMutation,
  useUpdateTicketMutation
} from "@/lib/ticket-hooks";
import type { TicketItem, TicketPriority, TicketStatus } from "@/lib/api/tickets";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/components/tickets/ticket-shared";

// 对一条反馈能做的全部写操作。
//
// 页头的属性胶囊、操作菜单、快捷键三处入口共用这一份：
// 同一个动作各写一遍，提示文案与失败处理迟早不一致。
// 每个动作返回是否成功，调用方据此决定要不要收起弹层。

function message(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function useFeedbackActions(ticket: TicketItem) {
  const operatorId = useAuthStore((state) => Number(state.operator?.id) || 0);
  const statusMut = useChangeTicketStatusMutation();
  const updateMut = useUpdateTicketMutation();
  const assignMut = useAssignTicketMutation();
  const deleteMut = useDeleteTicketMutation();

  const changeStatus = async (status: TicketStatus, solution?: string) => {
    try {
      await statusMut.mutateAsync({ id: ticket.id, status, solution });
      notify.success(`已改为「${STATUS_LABEL[status]}」`);
      return true;
    } catch (error) {
      notify.error(message(error, "状态更新失败"));
      return false;
    }
  };

  const update = async (payload: Parameters<typeof updateMut.mutateAsync>[0]["payload"], done: string) => {
    try {
      await updateMut.mutateAsync({ id: ticket.id, payload });
      notify.success(done);
      return true;
    } catch (error) {
      notify.error(message(error, "保存失败"));
      return false;
    }
  };

  const assign = async (assigneeAdminId: number | null) => {
    try {
      await assignMut.mutateAsync({ id: ticket.id, payload: { assigneeAdminId, groupId: ticket.groupId ?? null } });
      notify.success(assigneeAdminId ? (assigneeAdminId === operatorId ? "已指派给你" : "受理人已更新") : "已取消指派");
      return true;
    } catch (error) {
      notify.error(message(error, "指派失败"));
      return false;
    }
  };

  const remove = async () => {
    try {
      await deleteMut.mutateAsync(ticket.id);
      notify.success("反馈已删除");
      return true;
    } catch (error) {
      notify.error(message(error, "删除失败"));
      return false;
    }
  };

  return {
    operatorId,
    busy: statusMut.isPending || updateMut.isPending || assignMut.isPending || deleteMut.isPending,
    changeStatus,
    assign,
    assignToMe: () => (operatorId ? assign(operatorId) : Promise.resolve(false)),
    setPriority: (priority: TicketPriority) => update({ priority }, `优先级已改为「${PRIORITY_LABEL[priority]}」`),
    setCategory: (categoryId: number) => update({ categoryId }, "分类已更新"),
    setTitle: (title: string) => update({ title }, "标题已更新"),
    toggleLock: () => update({ locked: !ticket.locked }, ticket.locked ? "已解除锁定" : "已锁定，用户不能再回复"),
    remove
  };
}

export type FeedbackActions = ReturnType<typeof useFeedbackActions>;
