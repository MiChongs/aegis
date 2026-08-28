"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getTrafficRampSettings,
  getTrafficRampStats,
  resetTrafficRampStats,
  updateTrafficRampSettings,
  type TrafficRampSettingsPatch,
} from "@/lib/api/traffic-ramp";
import { useAdminToken } from "@/lib/admin-hooks";

const SETTINGS_KEY = ["admin-traffic-ramp-settings"] as const;
const STATS_KEY = ["admin-traffic-ramp-stats"] as const;

export function useTrafficRampSettingsQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...SETTINGS_KEY, token],
    queryFn: () => getTrafficRampSettings(token as string),
    enabled: Boolean(token && enabled),
  });
}

export function useTrafficRampStatsQuery(seconds: number, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...STATS_KEY, token, seconds],
    queryFn: () => getTrafficRampStats(token as string, seconds),
    enabled: Boolean(token && enabled),
    // 爬坡状态与队列水位是秒级变化的运行态，页面停留时保持近实时。
    refetchInterval: 3_000,
  });
}

export function useUpdateTrafficRampSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: TrafficRampSettingsPatch) => updateTrafficRampSettings(token as string, payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: SETTINGS_KEY }),
        // 配置直接改变统计里的基线 / 上限 / 队列容量，一并刷新。
        queryClient.invalidateQueries({ queryKey: STATS_KEY }),
      ]);
    },
  });
}

export function useResetTrafficRampStatsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => resetTrafficRampStats(token as string),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: STATS_KEY });
    },
  });
}
