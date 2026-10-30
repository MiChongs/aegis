"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  closeListenTogetherRoom,
  getListenTogetherRoom,
  getListenTogetherStats,
  kickListenTogetherMember,
  listListenTogetherRooms
} from "@/lib/api/listen-together";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 一起听域 hooks。房间是实时的，统计与列表每 15 秒刷新、详情每 5 秒刷新；
 * 解散与移出后整组失效（前缀 `admin-listen-together`）。服务不可用时不重试，免得面板一直转圈。
 */

const KEY = {
  stats: "admin-listen-together-stats",
  rooms: "admin-listen-together-rooms",
  room: "admin-listen-together-room"
} as const;

function useInvalidateListenTogether() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(Object.values(KEY).map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useListenTogetherStatsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey],
    queryFn: () => getListenTogetherStats(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    refetchInterval: 15_000,
    retry: false
  });
}

export function useListenTogetherRoomsQuery(appKey?: string | null, page = 1, pageSize = 20, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rooms, token, appKey, page, pageSize],
    queryFn: () => listListenTogetherRooms(token as string, appKey as string, { page, pageSize }),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: keepPreviousData,
    refetchInterval: 15_000,
    retry: false
  });
}

export function useListenTogetherRoomQuery(appKey?: string | null, roomId?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.room, token, appKey, roomId],
    queryFn: () => getListenTogetherRoom(token as string, appKey as string, roomId as string),
    enabled: Boolean(token && appKey && roomId),
    refetchInterval: 5_000,
    retry: false
  });
}

export function useCloseListenTogetherRoomMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateListenTogether();
  return useMutation({
    mutationFn: ({ roomId, reason }: { roomId: string; reason?: string }) =>
      closeListenTogetherRoom(token as string, appKey as string, roomId, reason),
    onSuccess: invalidate
  });
}

export function useKickListenTogetherMemberMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateListenTogether();
  return useMutation({
    mutationFn: ({ roomId, userId }: { roomId: string; userId: number }) =>
      kickListenTogetherMember(token as string, appKey as string, roomId, userId),
    onSuccess: invalidate
  });
}
