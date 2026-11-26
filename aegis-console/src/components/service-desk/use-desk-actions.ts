"use client";

import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { useAuthStore } from "@/lib/auth-store";
import {
  useAssignTicketMutation,
  useChangeTicketStatusMutation,
  useDeleteTicketMutation,
  useRetractTicketMessageMutation,
  useUpdateTicketMutation
} from "@/lib/ticket-hooks";
import type { TicketItem, TicketPriority, TicketStatus } from "@/lib/api/tickets";
import type { FeedbackRewardGrantPayload, FeedbackRewardRevokePayload } from "@/lib/api/feedback-rewards";
import { useGrantFeedbackRewardMutation, useRevokeFeedbackRewardMutation } from "@/lib/feedback-reward-hooks";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/components/tickets/ticket-shared";
import { useDesk } from "./desk-context";

// 对一条工单 / 反馈能做的全部写操作。
//
// 页头的属性胶囊、操作菜单、快捷键三处入口共用这一份：
// 同一个动作各写一遍，提示文案与失败处理迟早不一致。
// 每个动作返回是否成功，调用方据此决定要不要收起弹层。

function message(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function useDeskActions(ticket: TicketItem) {
  const desk = useDesk();
  const operatorId = useAuthStore((state) => Number(state.operator?.id) || 0);
  const statusMut = useChangeTicketStatusMutation();
  const updateMut = useUpdateTicketMutation();
  const assignMut = useAssignTicketMutation();
  const deleteMut = useDeleteTicketMutation();
  const retractMut = useRetractTicketMessageMutation();
  const grantMut = useGrantFeedbackRewardMutation();
  const revokeMut = useRevokeFeedbackRewardMutation();

  /**
   * solution 随「已解决」提交；reason 随「不采纳」等提交，不采纳时后端会通知提单人 / 反馈人。
   * reward：同时给这条反馈发放积分（后端先发奖励，失败则状态不变）。
   */
  const changeStatus = async (
    status: TicketStatus,
    detail?: { solution?: string; reason?: string; reward?: FeedbackRewardGrantPayload }
  ) => {
    try {
      await statusMut.mutateAsync({ id: ticket.id, status, solution: detail?.solution, reason: detail?.reason, reward: detail?.reward });
      notify.success(
        detail?.reward ? `已改为「${STATUS_LABEL[status]}」，并发放了反馈奖励` : `已改为「${STATUS_LABEL[status]}」`
      );
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

  /** 换处理组；autoPick 时按组的分配策略自动挑人 */
  const assignGroup = async (groupId: number | null, autoPick = false) => {
    try {
      await assignMut.mutateAsync({
        id: ticket.id,
        payload: autoPick ? { groupId, autoPick: true } : { assigneeAdminId: ticket.assigneeAdminId ?? null, groupId }
      });
      notify.success(autoPick ? "已按组策略分派" : groupId ? "处理组已更新" : "已移出处理组");
      return true;
    } catch (error) {
      notify.error(message(error, "指派失败"));
      return false;
    }
  };

  const remove = async () => {
    try {
      await deleteMut.mutateAsync(ticket.id);
      notify.success(`${desk.noun}已删除`);
      return true;
    } catch (error) {
      notify.error(message(error, "删除失败"));
      return false;
    }
  };

  /** 撤回一条回复 / 备注；reason 选填，只在控制台与操作记录里可见 */
  const retractMessage = async (messageId: number, reason?: string) => {
    try {
      const result = await retractMut.mutateAsync({ ticketId: ticket.id, messageId, reason: reason?.trim() || undefined });
      notify.success(
        result.notificationsRemoved > 0 ? `已撤回，并删除了${desk.person}收到的 ${result.notificationsRemoved} 条通知` : "已撤回"
      );
      return true;
    } catch (error) {
      notify.error(message(error, "撤回失败"));
      return false;
    }
  };

  /** 发放反馈奖励积分；同一个 requestId 重复提交后端原样返回 */
  const grantReward = async (payload: FeedbackRewardGrantPayload) => {
    try {
      const result = await grantMut.mutateAsync({ ticketId: ticket.id, payload });
      notify.success(
        result.replayed ? `这条${desk.noun}已发放过 ${result.reward.points} 积分` : `已发放 ${result.reward.points} 积分`
      );
      return true;
    } catch (error) {
      notify.error(message(error, "发放失败"));
      return false;
    }
  };

  /** 撤回反馈奖励并扣回积分；allowPartial 时余额不足扣到 0 */
  const revokeReward = async (payload: FeedbackRewardRevokePayload) => {
    try {
      const result = await revokeMut.mutateAsync({ ticketId: ticket.id, payload });
      const { clawedBack, shortfall } = result.reward;
      notify.success(shortfall > 0 ? `已撤回，扣回 ${clawedBack} 积分（余额不足，差 ${shortfall}）` : `已撤回，扣回 ${clawedBack} 积分`);
      return true;
    } catch (error) {
      notify.error(message(error, "撤回失败"));
      return false;
    }
  };

  return {
    operatorId,
    grantReward,
    revokeReward,
    rewarding: grantMut.isPending || revokeMut.isPending,
    busy:
      statusMut.isPending ||
      updateMut.isPending ||
      assignMut.isPending ||
      deleteMut.isPending ||
      retractMut.isPending ||
      grantMut.isPending ||
      revokeMut.isPending,
    retractMessage,
    retracting: retractMut.isPending,
    changeStatus,
    assign,
    assignGroup,
    assignToMe: () => (operatorId ? assign(operatorId) : Promise.resolve(false)),
    setPriority: (priority: TicketPriority) => update({ priority }, `优先级已改为「${PRIORITY_LABEL[priority]}」`),
    setCategory: (categoryId: number) => update({ categoryId }, "分类已更新"),
    setTitle: (title: string) => update({ title }, "标题已更新"),
    toggleLock: () => update({ locked: !ticket.locked }, ticket.locked ? "已解除锁定" : "已锁定，用户不能再回复"),
    remove
  };
}

export type DeskActions = ReturnType<typeof useDeskActions>;
