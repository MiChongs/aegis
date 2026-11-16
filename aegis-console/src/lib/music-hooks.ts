"use client";

import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient, type Query } from "@tanstack/react-query";
import { useMemo } from "react";
import { useAdminToken } from "@/lib/admin-hooks";
import { getAdminAppUsers } from "@/lib/api/apps";
import { ApiError } from "@/lib/api/client";
import {
  MUSIC_SERVICE_CODES,
  createMusicExperiment,
  createMusicRule,
  deleteMusicExperiment,
  deleteMusicRule,
  forgetMusicUser,
  getMusicArtist,
  getMusicExperimentResults,
  getMusicSearchStats,
  getMusicUserMixes,
  getMusicUserReport,
  listMusicArtists,
  listMusicExperiments,
  listMusicRules,
  startMusicExperiment,
  stopMusicExperiment,
  updateMusicExperiment,
  updateMusicRule,
  getMusicConfig,
  getMusicModel,
  getMusicOverview,
  getMusicTrack,
  getMusicUser,
  getMusicUserRecommendations,
  getMusicUserTimeline,
  listMusicEvents,
  listMusicModels,
  listMusicTracks,
  listMusicUsers,
  publishMusicModel,
  trainMusicModel,
  updateMusicConfig,
  type MusicConfigInput,
  type MusicEventQuery,
  type MusicExperimentInput,
  type MusicModelRun,
  type MusicRecPreviewParams,
  type MusicReportPeriod,
  type MusicRuleInput,
  type MusicTrackSort
} from "@/lib/api/music";

/**
 * 音乐推荐域 hooks。
 *
 * 查询键统一为 `["admin-music", 种类, token, appKey, ...参数]`，写操作成功后按「种类 + appKey」成组失效，
 * 不碰别的应用的缓存。音乐服务未开通或暂不可用（50396 / 50397）时不重试，免得面板一直转圈；
 * 其它错误按全局默认处理。训练记录里有排队中 / 训练中的版本时每 5 秒刷新一次，全部结束后停止。
 */

const ROOT = "admin-music";

type Kind =
  | "overview"
  | "users"
  | "user"
  | "timeline"
  | "recs"
  | "tracks"
  | "track"
  | "events"
  | "models"
  | "model"
  | "config"
  | "rules"
  | "experiments"
  | "experimentResults"
  | "search"
  | "artists"
  | "artist"
  | "mixes"
  | "report";

function noRetryOnService(failureCount: number, error: unknown) {
  if (error instanceof ApiError) {
    if (Object.values<number>(MUSIC_SERVICE_CODES).includes(error.code ?? 0)) return false;
    if (error.status && error.status >= 400 && error.status < 500) return false;
  }
  return failureCount < 2;
}

function useInvalidateMusic(appKey?: string | null) {
  const queryClient = useQueryClient();
  return (kinds: Kind[]) =>
    queryClient.invalidateQueries({
      predicate: (query: Query) => {
        const key = query.queryKey;
        return key[0] === ROOT && kinds.includes(key[1] as Kind) && key[3] === appKey;
      }
    });
}

const isRunning = (run?: Pick<MusicModelRun, "status"> | null) => run?.status === "queued" || run?.status === "running";

export function useMusicOverviewQuery(appKey: string | null | undefined, days: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "overview", token, appKey, days],
    queryFn: () => getMusicOverview(token as string, appKey as string, days),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    // 消费积压是实时量，一分钟刷一次够用
    refetchInterval: 60_000,
    retry: noRetryOnService
  });
}

export function useMusicUsersQuery(appKey: string | null | undefined, params: { days: number; q: string; page: number; pageSize: number }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "users", token, appKey, params],
    queryFn: () => listMusicUsers(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export function useMusicUserQuery(appKey: string | null | undefined, userId: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "user", token, appKey, userId],
    queryFn: () => getMusicUser(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId),
    retry: noRetryOnService
  });
}

export function useMusicUserTimelineQuery(appKey: string | null | undefined, userId: number | null, type: string, limit = 30) {
  const token = useAdminToken();
  return useInfiniteQuery({
    queryKey: [ROOT, "timeline", token, appKey, userId, type, limit],
    queryFn: ({ pageParam }) =>
      getMusicUserTimeline(token as string, appKey as string, userId as number, { type: type || undefined, before: pageParam, limit }),
    initialPageParam: null as number | null,
    getNextPageParam: (last) => last.nextBefore ?? undefined,
    enabled: Boolean(token && appKey && userId),
    retry: noRetryOnService
  });
}

export function useMusicUserRecommendationsQuery(
  appKey: string | null | undefined,
  userId: number | null,
  params: MusicRecPreviewParams,
  enabled = true
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "recs", token, appKey, userId, params],
    queryFn: () => getMusicUserRecommendations(token as string, appKey as string, userId as number, params),
    enabled: Boolean(token && appKey && userId && enabled),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export function useForgetMusicUserMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: (userId: number) => forgetMusicUser(token as string, appKey as string, userId),
    onSuccess: () => invalidate(["user", "users", "timeline", "recs", "overview", "events", "mixes", "report"])
  });
}

export function useMusicTracksQuery(
  appKey: string | null | undefined,
  params: { days: number; q: string; sort: MusicTrackSort; page: number; pageSize: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "tracks", token, appKey, params],
    queryFn: () => listMusicTracks(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export function useMusicTrackQuery(appKey: string | null | undefined, workKey: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "track", token, appKey, workKey],
    queryFn: () => getMusicTrack(token as string, appKey as string, workKey as string),
    enabled: Boolean(token && appKey && workKey),
    retry: noRetryOnService
  });
}

export function useMusicEventsQuery(appKey: string | null | undefined, params: MusicEventQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "events", token, appKey, params],
    queryFn: () => listMusicEvents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export function useMusicModelsQuery(appKey: string | null | undefined, page: number, pageSize: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "models", token, appKey, page, pageSize],
    queryFn: () => listMusicModels(token as string, appKey as string, { page, pageSize }),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    refetchInterval: (query) => (query.state.data?.items.some(isRunning) ? 5_000 : false),
    retry: noRetryOnService
  });
}

export function useMusicModelQuery(appKey: string | null | undefined, runId: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "model", token, appKey, runId],
    queryFn: () => getMusicModel(token as string, appKey as string, runId as number),
    enabled: Boolean(token && appKey && runId),
    refetchInterval: (query) => (isRunning(query.state.data) ? 5_000 : false),
    retry: noRetryOnService
  });
}

export function useTrainMusicModelMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: (note?: string) => trainMusicModel(token as string, appKey as string, note),
    onSuccess: () => invalidate(["models"])
  });
}

export function usePublishMusicModelMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: (runId: number) => publishMusicModel(token as string, appKey as string, runId),
    // 发布后推荐结果与用户画像里的「模型口味」都会变
    onSuccess: () => invalidate(["models", "model", "overview", "recs", "user", "mixes", "artist"])
  });
}

export function useMusicConfigQuery(appKey: string | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "config", token, appKey],
    queryFn: () => getMusicConfig(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    retry: noRetryOnService
  });
}

export function useUpdateMusicConfigMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: (config: MusicConfigInput) => updateMusicConfig(token as string, appKey as string, config),
    onSuccess: () => invalidate(["config", "recs"])
  });
}

/* ── 第二期：运营干预 ── */

export function useMusicRulesQuery(appKey: string | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "rules", token, appKey],
    queryFn: () => listMusicRules(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    retry: noRetryOnService
  });
}

/** 新建（id 为空）或修改一条规则；规则改了推荐预览随之变 */
export function useSaveMusicRuleMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: ({ id, input }: { id?: number | null; input: MusicRuleInput }) =>
      id ? updateMusicRule(token as string, appKey as string, id, input) : createMusicRule(token as string, appKey as string, input),
    onSuccess: () => invalidate(["rules", "recs"])
  });
}

export function useDeleteMusicRuleMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: (id: number) => deleteMusicRule(token as string, appKey as string, id),
    onSuccess: () => invalidate(["rules", "recs"])
  });
}

/* ── 第二期：A/B 实验 ── */

export function useMusicExperimentsQuery(appKey: string | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "experiments", token, appKey],
    queryFn: () => listMusicExperiments(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    retry: noRetryOnService
  });
}

export function useSaveMusicExperimentMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: ({ id, input }: { id?: number | null; input: MusicExperimentInput }) =>
      id ? updateMusicExperiment(token as string, appKey as string, id, input) : createMusicExperiment(token as string, appKey as string, input),
    onSuccess: () => invalidate(["experiments", "experimentResults", "overview", "recs", "user"])
  });
}

export function useDeleteMusicExperimentMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: (id: number) => deleteMusicExperiment(token as string, appKey as string, id),
    onSuccess: () => invalidate(["experiments", "experimentResults", "overview"])
  });
}

/** 开始 / 停止：分桶立即生效，推荐预览、用户详情里的「所在实验」与总览都跟着变 */
export function useMusicExperimentActionMutation(appKey: string | null | undefined) {
  const token = useAdminToken();
  const invalidate = useInvalidateMusic(appKey);
  return useMutation({
    mutationFn: ({ id, action }: { id: number; action: "start" | "stop" }) =>
      action === "start" ? startMusicExperiment(token as string, appKey as string, id) : stopMusicExperiment(token as string, appKey as string, id),
    onSuccess: () => invalidate(["experiments", "experimentResults", "overview", "recs", "user"])
  });
}

export function useMusicExperimentResultsQuery(appKey: string | null | undefined, id: number | null, days: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "experimentResults", token, appKey, id, days],
    queryFn: () => getMusicExperimentResults(token as string, appKey as string, id as number, days),
    enabled: Boolean(token && appKey && id),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

/* ── 第二期：搜索与歌手 ── */

export function useMusicSearchStatsQuery(appKey: string | null | undefined, days: number, limit = 50) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "search", token, appKey, days, limit],
    queryFn: () => getMusicSearchStats(token as string, appKey as string, { days, limit }),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export function useMusicArtistsQuery(appKey: string | null | undefined, params: { days: number; q: string; page: number; pageSize: number }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "artists", token, appKey, params],
    queryFn: () => listMusicArtists(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export function useMusicArtistQuery(appKey: string | null | undefined, name: string | null, days: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "artist", token, appKey, name, days],
    queryFn: () => getMusicArtist(token as string, appKey as string, name as string, days),
    enabled: Boolean(token && appKey && name),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

/* ── 第二期：用户的风格歌单与听歌报告 ── */

export function useMusicUserMixesQuery(appKey: string | null | undefined, userId: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "mixes", token, appKey, userId],
    queryFn: () => getMusicUserMixes(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId),
    retry: noRetryOnService
  });
}

export function useMusicUserReportQuery(appKey: string | null | undefined, userId: number | null, period: MusicReportPeriod, offset: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "report", token, appKey, userId, period, offset],
    queryFn: () => getMusicUserReport(token as string, appKey as string, userId as number, { period, offset }),
    enabled: Boolean(token && appKey && userId),
    placeholderData: keepPreviousData,
    retry: noRetryOnService
  });
}

export type MusicUserName = { nickname?: string; account?: string; avatar?: string };

/**
 * 按 ID 批量取用户昵称（用户列表接口的 `ids`，一次最多 500 个）。
 * 没有用户读权限或接口失败时返回空表，调用方退回「用户 #ID」，不影响音乐数据本身。
 */
export function useMusicUserNames(appKey: string | null | undefined, ids: number[]) {
  const token = useAdminToken();
  const sorted = useMemo(() => [...new Set(ids.filter((id) => id > 0))].sort((a, b) => a - b).slice(0, 500), [ids]);
  const query = useQuery({
    queryKey: ["admin-music-user-names", token, appKey, sorted.join(",")],
    queryFn: () => getAdminAppUsers(token as string, appKey as string, { ids: sorted.join(","), limit: sorted.length }),
    enabled: Boolean(token && appKey && sorted.length),
    staleTime: 5 * 60_000,
    retry: false
  });
  return useMemo(() => {
    const map = new Map<number, MusicUserName>();
    for (const item of query.data?.items ?? []) map.set(item.id, { nickname: item.nickname, account: item.account, avatar: item.avatar });
    return map;
  }, [query.data]);
}
