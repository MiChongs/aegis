import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 一起听域 API（docs/listen-together.md）。
 *
 * 房间与长连接在独立的一起听微服务里，主服务把这几条管理端接口经内部 RPC 转过去。
 * 一起听服务没部署或连不上时返回 50360 / 50361 / 50362，面板据此提示，不当成普通错误。
 * 用户 ID：`userId` 是应用内主键（跳用户详情用），`publicId` 是对外 ID。
 */

export type ListenTogetherTrack = {
  itemId: string;
  source: string;
  songId: string;
  title: string;
  artist?: string;
  album?: string;
  cover?: string;
  durationMs?: number;
  /** 点歌人的对外 ID */
  requestedBy?: number;
};

export type ListenTogetherPlayback = {
  track?: ListenTogetherTrack;
  positionMs: number;
  playing: boolean;
  /** 毫秒时间戳：播放状态最后一次更新的时刻 */
  updatedAt: number;
};

export type ListenTogetherRoom = {
  roomId: string;
  code: string;
  name: string;
  hostUserId: number;
  hostPublicId: number;
  hostNickname: string;
  memberCount: number;
  onlineCount: number;
  capacity: number;
  controlMode: "host" | "all" | string;
  allowRequests: boolean;
  locked: boolean;
  queueLength: number;
  playback: ListenTogetherPlayback;
  createdAt: number;
  updatedAt: number;
};

export type ListenTogetherMember = {
  userId: number;
  publicId: number;
  nickname: string;
  avatar?: string;
  role: "host" | "member" | string;
  canControl: boolean;
  online: boolean;
  joinedAt: number;
  platform?: string;
  state?: string;
};

export type ListenTogetherChat = {
  id: string;
  userId: number;
  publicId: number;
  nickname: string;
  text: string;
  sentAt: number;
};

export type ListenTogetherRoomDetail = ListenTogetherRoom & {
  members: ListenTogetherMember[];
  queue: ListenTogetherTrack[];
  chat: ListenTogetherChat[];
  bannedPublicIds: number[];
};

export type ListenTogetherInstance = {
  id: string;
  version: string;
  startedAt: number;
  connections: number;
  rooms: number;
  heartbeatAt: number;
};

export type ListenTogetherStats = {
  enabled: boolean;
  endpoints: string[];
  rooms: number;
  playingRooms: number;
  members: number;
  onlineMembers: number;
  instances: ListenTogetherInstance[];
  /** 会员功能目录里应建的权益标识 */
  feature: string;
};

export type ListenTogetherRoomPage = {
  items: ListenTogetherRoom[];
  total: number;
};

/** 服务未开通、连不上、未配置内部地址：面板显示说明而不是报错。 */
export const LISTEN_TOGETHER_SERVICE_CODES = {
  notEnabled: 50360,
  unavailable: 50361,
  rpcMissing: 50362
} as const;

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/listen-together`;

export function getListenTogetherStats(token: string, appKey: string) {
  return apiRequest<ListenTogetherStats>(`${base(appKey)}/stats`, { token });
}

export function listListenTogetherRooms(token: string, appKey: string, params: { page?: number; pageSize?: number }) {
  return apiRequest<ListenTogetherRoomPage>(`${base(appKey)}/rooms${buildQuery(params)}`, { token });
}

export function getListenTogetherRoom(token: string, appKey: string, roomId: string) {
  return apiRequest<ListenTogetherRoomDetail>(`${base(appKey)}/rooms/${encodeURIComponent(roomId)}`, { token });
}

export function closeListenTogetherRoom(token: string, appKey: string, roomId: string, reason?: string) {
  return apiRequest<null>(`${base(appKey)}/rooms/${encodeURIComponent(roomId)}/close`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason: reason ?? "" })
  });
}

export function kickListenTogetherMember(token: string, appKey: string, roomId: string, userId: number) {
  return apiRequest<null>(`${base(appKey)}/rooms/${encodeURIComponent(roomId)}/kick`, {
    method: "POST",
    token,
    body: JSON.stringify({ userId })
  });
}
