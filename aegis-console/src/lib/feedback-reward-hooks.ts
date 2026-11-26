"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@/lib/auth-store";
import {
  getFeedbackRewardPolicy,
  grantFeedbackReward,
  revokeFeedbackReward,
  saveFeedbackRewardPolicy,
  type FeedbackRewardGrantPayload,
  type FeedbackRewardPolicyPayload,
  type FeedbackRewardRevokePayload
} from "@/lib/api/feedback-rewards";

// 反馈奖励的 React Query 封装。发放 / 撤回之后刷新详情（reward 区块、时间线）与列表。

function useToken() {
  return useAuthStore((state) => state.accessToken);
}

function invalidateAfterReward(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["ticket-detail"] });
  qc.invalidateQueries({ queryKey: ["ticket-requester"] });
  qc.invalidateQueries({ queryKey: ["tickets"] });
}

export function useFeedbackRewardPolicyQuery(appid: number) {
  const token = useToken();
  return useQuery({
    queryKey: ["feedback-reward-policy", token, appid],
    queryFn: () => getFeedbackRewardPolicy(token as string, appid),
    enabled: Boolean(token) && appid > 0
  });
}

export function useSaveFeedbackRewardPolicyMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: FeedbackRewardPolicyPayload) => saveFeedbackRewardPolicy(token as string, payload),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["feedback-reward-policy"] });
      qc.invalidateQueries({ queryKey: ["ticket-detail"] });
    }
  });
}

export function useGrantFeedbackRewardMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { ticketId: number; payload: FeedbackRewardGrantPayload }) =>
      grantFeedbackReward(token as string, args.ticketId, args.payload),
    onSuccess: () => invalidateAfterReward(qc)
  });
}

export function useRevokeFeedbackRewardMutation() {
  const token = useToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { ticketId: number; payload: FeedbackRewardRevokePayload }) =>
      revokeFeedbackReward(token as string, args.ticketId, args.payload),
    onSuccess: () => invalidateAfterReward(qc)
  });
}
