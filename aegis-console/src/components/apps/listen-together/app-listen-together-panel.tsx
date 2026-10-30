"use client";

import { useState } from "react";
import { Headphones, Music2, Pause, Play, Radio, Server, Users } from "lucide-react";
import { SectionCard, StatusDot } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import { ListenTogetherRoomSheet } from "@/components/apps/listen-together/listen-together-room-sheet";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import { LISTEN_TOGETHER_SERVICE_CODES, type ListenTogetherRoom } from "@/lib/api/listen-together";
import { formatRelative } from "@/lib/datetime";
import { useListenTogetherRoomsQuery, useListenTogetherStatsQuery } from "@/lib/listen-together-hooks";

const PAGE_SIZE = 20;

/**
 * 一起听（/apps/{appKey}?tab=listen-together）：服务状态、房间列表与房间详情。
 *
 * 数据来自独立的一起听微服务（主服务经内部 RPC 转发），是实时状态：统计与列表 15 秒刷新一次。
 * 服务没部署或连不上时整块换成部署说明，列表不再请求。
 */
export function AppListenTogetherPanel({ appKey }: { appKey: string }) {
  const [page, setPage] = useState(1);
  const [openRoom, setOpenRoom] = useState<string | null>(null);
  const statsQuery = useListenTogetherStatsQuery(appKey);
  const serviceCode = statsQuery.error instanceof ApiError ? statsQuery.error.code : undefined;
  const serviceDown = serviceCode != null && Object.values<number>(LISTEN_TOGETHER_SERVICE_CODES).includes(serviceCode);
  const roomsQuery = useListenTogetherRoomsQuery(appKey, page, PAGE_SIZE, !serviceDown && !statsQuery.isError);
  const stats = statsQuery.data;
  const rooms = roomsQuery.data?.items ?? [];
  const total = roomsQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const aside = statsQuery.isLoading ? null : (
    <StatusDot active={Boolean(stats?.enabled && stats.instances.length > 0)} labelActive="服务运行中" labelInactive="服务未就绪" />
  );

  return (
    <div className="space-y-4">
      <SectionCard
        icon={<Headphones className="size-4" />}
        title="一起听"
        description="在线听歌的多人同步收听。房间与长连接在独立的一起听服务里，这里看到的是实时状态。"
        aside={aside}
      >
        {statsQuery.isLoading ? (
          <div className="grid gap-3 sm:grid-cols-4">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-20 w-full rounded-xl" />
            ))}
          </div>
        ) : serviceDown || statsQuery.isError ? (
          <ServiceNotice code={serviceCode} message={statsQuery.error instanceof Error ? statsQuery.error.message : undefined} />
        ) : stats ? (
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-4">
              <StatTile icon={<Radio className="size-4" />} label="房间" value={stats.rooms} />
              <StatTile icon={<Play className="size-4" />} label="正在播放" value={stats.playingRooms} />
              <StatTile icon={<Users className="size-4" />} label="成员" value={stats.members} />
              <StatTile icon={<Headphones className="size-4" />} label="在线" value={stats.onlineMembers} />
            </div>
            <div className="grid gap-3 lg:grid-cols-2">
              <div className="rounded-xl border border-border p-3">
                <p className="mb-2 text-xs font-medium">客户端连接地址</p>
                {stats.endpoints.length === 0 ? (
                  <p className="text-xs text-muted-foreground">未配置 LISTEN_TOGETHER_ENDPOINTS，App 会提示「一起听暂未开放」。</p>
                ) : (
                  <ul className="space-y-1">
                    {stats.endpoints.map((endpoint, index) => (
                      <li key={endpoint} className="flex items-center gap-2 font-mono text-[11px]">
                        <span className="text-muted-foreground tabular-nums">{index + 1}</span>
                        <span className="break-all">{endpoint}</span>
                      </li>
                    ))}
                  </ul>
                )}
                <p className="mt-2 text-[11px] text-muted-foreground">
                  使用需要会员权益 <code className="font-mono">{stats.feature}</code>：请在「会员」区块的功能目录里创建同名功能并加进会员档位。
                </p>
              </div>
              <div className="rounded-xl border border-border p-3">
                <p className="mb-2 flex items-center gap-1.5 text-xs font-medium">
                  <Server className="size-3.5" />
                  服务实例 <span className="text-muted-foreground tabular-nums">{stats.instances.length}</span>
                </p>
                {stats.instances.length === 0 ? (
                  <p className="text-xs text-muted-foreground">没有存活的实例。</p>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="h-7 text-[11px]">实例</TableHead>
                        <TableHead className="h-7 text-[11px]">版本</TableHead>
                        <TableHead className="h-7 text-right text-[11px]">连接</TableHead>
                        <TableHead className="h-7 text-right text-[11px]">房间</TableHead>
                        <TableHead className="h-7 text-right text-[11px]">启动</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {stats.instances.map((instance) => (
                        <TableRow key={instance.id}>
                          <TableCell className="max-w-40 truncate py-1.5 font-mono text-[11px]" title={instance.id}>
                            {instance.id}
                          </TableCell>
                          <TableCell className="py-1.5 text-[11px]">{instance.version}</TableCell>
                          <TableCell className="py-1.5 text-right text-[11px] tabular-nums">{instance.connections}</TableCell>
                          <TableCell className="py-1.5 text-right text-[11px] tabular-nums">{instance.rooms}</TableCell>
                          <TableCell className="py-1.5 text-right text-[11px] text-muted-foreground">
                            {formatRelative(instance.startedAt)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </div>
            </div>
          </div>
        ) : null}
      </SectionCard>

      {!serviceDown && !statsQuery.isError ? (
        <SectionCard
          icon={<Radio className="size-4" />}
          title="房间"
          description="按最近活动排序。点开可看成员、共享队列与最近聊天，并可解散房间或移出成员。"
          aside={<span className="text-xs text-muted-foreground tabular-nums">共 {total} 个</span>}
        >
          {roomsQuery.isLoading ? (
            <div className="space-y-2">
              {[0, 1, 2].map((index) => (
                <Skeleton key={index} className="h-12 w-full" />
              ))}
            </div>
          ) : rooms.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
              暂无房间
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>房间</TableHead>
                  <TableHead>房主</TableHead>
                  <TableHead>人数</TableHead>
                  <TableHead>正在放</TableHead>
                  <TableHead>设置</TableHead>
                  <TableHead className="text-right">最近活动</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rooms.map((room) => (
                  <RoomRow key={room.roomId} room={room} onOpen={() => setOpenRoom(room.roomId)} />
                ))}
              </TableBody>
            </Table>
          )}
          <AdPolicyPager total={total} page={page} totalPages={totalPages} onPage={setPage} />
        </SectionCard>
      ) : null}

      <ListenTogetherRoomSheet
        appKey={appKey}
        roomId={openRoom}
        onOpenChange={(open) => {
          if (!open) setOpenRoom(null);
        }}
      />
    </div>
  );
}

function StatTile({ icon, label, value }: { icon: React.ReactNode; label: string; value: number }) {
  return (
    <div className="rounded-xl border border-border px-4 py-3">
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {icon}
        {label}
      </p>
      <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
    </div>
  );
}

function ServiceNotice({ code, message }: { code?: number; message?: string }) {
  const { title, body } =
    code === LISTEN_TOGETHER_SERVICE_CODES.notEnabled
      ? {
          title: "一起听尚未开通",
          body: "主服务没有配置 LISTEN_TOGETHER_ENDPOINTS（客户端可连的 wss 地址）。部署一起听服务后配置该项即可开通。"
        }
      : code === LISTEN_TOGETHER_SERVICE_CODES.rpcMissing
        ? {
            title: "未配置内部地址",
            body: "主服务没有配置 LISTEN_TOGETHER_RPC_URL，控制台无法查看房间。用户侧的一起听不受影响。"
          }
        : {
            title: "一起听服务暂时不可用",
            body: message || "主服务连不上一起听服务的内部接口，请检查服务状态与 LISTEN_TOGETHER_INTERNAL_TOKEN。"
          };
  return (
    <div className="rounded-xl border border-dashed border-border px-4 py-6">
      <p className="text-sm font-medium">{title}</p>
      <p className="mt-1 text-xs text-muted-foreground">{body}</p>
      <p className="mt-3 text-[11px] text-muted-foreground">
        部署方式见 docs/listen-together.md：docker compose 的 listen-together 服务，或单独运行 deploy/docker/listen-together.Dockerfile 构建的镜像。
      </p>
    </div>
  );
}

function RoomRow({ room, onOpen }: { room: ListenTogetherRoom; onOpen: () => void }) {
  const track = room.playback.track;
  return (
    <TableRow className="cursor-pointer" onClick={onOpen}>
      <TableCell className="max-w-56">
        <p className="truncate text-xs font-medium" title={room.name}>
          {room.name || "未命名房间"}
        </p>
        <p className="mt-0.5 font-mono text-[11px] text-muted-foreground tabular-nums">{room.code}</p>
      </TableCell>
      <TableCell className="max-w-40 truncate text-xs">{room.hostNickname || `#${room.hostPublicId}`}</TableCell>
      <TableCell className="text-xs tabular-nums">
        <span className="font-medium">{room.onlineCount}</span>
        <span className="text-muted-foreground">
          {" "}
          在线 / {room.memberCount} / {room.capacity}
        </span>
      </TableCell>
      <TableCell className="max-w-64">
        {track ? (
          <div className="flex items-center gap-2">
            {room.playback.playing ? (
              <Play className="size-3.5 shrink-0 text-emerald-600" />
            ) : (
              <Pause className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <div className="min-w-0">
              <p className="truncate text-xs">{track.title}</p>
              {track.artist ? <p className="truncate text-[11px] text-muted-foreground">{track.artist}</p> : null}
            </div>
          </div>
        ) : (
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Music2 className="size-3.5" />
            未开始
          </span>
        )}
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {room.locked ? (
            <Badge variant="warning" size="sm">
              已锁定
            </Badge>
          ) : null}
          {room.controlMode === "all" ? (
            <Badge variant="info" size="sm">
              所有人可控
            </Badge>
          ) : null}
          {!room.allowRequests ? (
            <Badge variant="secondary" size="sm">
              关闭点歌
            </Badge>
          ) : null}
          <Badge variant="outline" size="sm">
            队列 {room.queueLength}
          </Badge>
        </div>
      </TableCell>
      <TableCell className="text-right text-xs whitespace-nowrap text-muted-foreground">{formatRelative(room.updatedAt)}</TableCell>
    </TableRow>
  );
}
