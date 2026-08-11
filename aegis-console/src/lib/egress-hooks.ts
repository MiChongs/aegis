"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  explainEgress,
  getEgressSettings,
  probeEgress,
  resetEgressSettings,
  testEgress,
  updateEgressSettings,
  type EgressSettingsUpdate,
} from "@/lib/api/egress";
import { useAdminToken } from "@/lib/admin-hooks";

const KEY = ["admin-egress-settings"] as const;

export function useEgressSettingsQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...KEY, token],
    queryFn: () => getEgressSettings(token as string),
    enabled: Boolean(token && enabled),
    // 端点健康与流量统计随时间变化，页面停留时定期刷新一次即可。
    refetchInterval: 30_000,
  });
}

export function useUpdateEgressSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: EgressSettingsUpdate) => updateEgressSettings(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: KEY });
    },
  });
}

export function useResetEgressSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => resetEgressSettings(token as string),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: KEY });
    },
  });
}

export function useEgressTestMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof testEgress>[1]) => testEgress(token as string, payload),
  });
}

export function useEgressExplainMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof explainEgress>[1]) => explainEgress(token as string, payload),
  });
}

export function useEgressProbeMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => probeEgress(token as string),
    onSuccess: async () => {
      // 探测会改写端点健康状态，拉一次最新运行态。
      await queryClient.invalidateQueries({ queryKey: KEY });
    },
  });
}
