"use client";

import { useState } from "react";
import Link from "next/link";
import { Crown, Hand, Pause, Play, UserMinus, XCircle } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { ListenTogetherMember, ListenTogetherRoomDetail } from "@/lib/api/listen-together";
import { formatClockSeconds, formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import {
  useCloseListenTogetherRoomMutation,
  useKickListenTogetherMemberMutation,
  useListenTogetherRoomQuery
} from "@/lib/listen-together-hooks";

const STATE_LABEL: Record<string, string> = {
  buffering: "缓冲中",
  playing: "收听中",
  paused: "已暂停",
  error: "播放出错",
  idle: "空闲"
};

/** 毫秒时长写成 分:秒。 */
function duration(ms?: number) {
  if (!ms || ms <= 0) return "";
  const seconds = Math.floor(ms / 1000);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

/**
 * 一起听房间详情：正在放的歌、成员（可移出）、共享队列、最近聊天与被移出的人；可解散房间。
 * 详情每 5 秒刷新，房间被解散后显示「房间不存在或已解散」。
 */
export function ListenTogetherRoomSheet({
  appKey,
  roomId,
  onOpenChange
}: {
  appKey: string;
  roomId: string | null;
  onOpenChange: (open: boolean) => void;
}) {
  const query = useListenTogetherRoomQuery(appKey, roomId);
  const closeMutation = useCloseListenTogetherRoomMutation(appKey);
  const kickMutation = useKickListenTogetherMemberMutation(appKey);
  const [reason, setReason] = useState("");
  const room = query.data;

  const close = async (target: ListenTogetherRoomDetail) => {
    try {
      await closeMutation.mutateAsync({ roomId: target.roomId, reason: reason.trim() || undefined });
      toast.success("房间已解散");
      setReason("");
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解散失败");
    }
  };

  const kick = async (target: ListenTogetherRoomDetail, member: ListenTogetherMember) => {
    try {
      await kickMutation.mutateAsync({ roomId: target.roomId, userId: member.userId });
      toast.success(`已移出 ${member.nickname}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "移出失败");
    }
  };

  return (
    <Sheet open={roomId != null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[92vw] max-w-3xl flex-col p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle className="pr-8 break-all">{room?.name || "一起听房间"}</SheetTitle>
          <SheetDescription>
            {room ? (
              <span className="tabular-nums">
                房间号 <span className="font-mono">{room.code}</span>，创建于 {formatDateTimeMinute(room.createdAt)}
              </span>
            ) : (
              "实时状态，每 5 秒刷新"
            )}
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-4">
          {query.isLoading ? (
            <div className="space-y-2">
              {[0, 1, 2, 3].map((index) => (
                <Skeleton key={index} className="h-10 w-full" />
              ))}
            </div>
          ) : query.isError || !room ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
              {query.error instanceof ApiError ? query.error.message : "房间不存在或已解散"}
            </p>
          ) : (
            <>
              <section className="rounded-xl border border-border p-4">
                <p className="mb-2 text-xs font-medium text-muted-foreground">正在放</p>
                {room.playback.track ? (
                  <div className="flex items-center gap-3">
                    {room.playback.track.cover ? (
                      // eslint-disable-next-line @next/next/no-img-element
                      <img src={room.playback.track.cover} alt="" className="size-12 rounded-lg object-cover" />
                    ) : null}
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">{room.playback.track.title}</p>
                      <p className="truncate text-xs text-muted-foreground">
                        {[room.playback.track.artist, room.playback.track.album].filter(Boolean).join(" - ") || "—"}
                      </p>
                    </div>
                    <div className="text-right text-xs text-muted-foreground">
                      <p className="flex items-center justify-end gap-1">
                        {room.playback.playing ? <Play className="size-3.5 text-emerald-600" /> : <Pause className="size-3.5" />}
                        {room.playback.playing ? "播放中" : "已暂停"}
                      </p>
                      <p className="tabular-nums">
                        {duration(room.playback.positionMs) || "0:00"}
                        {room.playback.track.durationMs ? ` / ${duration(room.playback.track.durationMs)}` : ""}
                      </p>
                      <p>更新于 {formatClockSeconds(room.playback.updatedAt)}</p>
                    </div>
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">房主还没有开始播放</p>
                )}
                <div className="mt-3 flex flex-wrap gap-1">
                  <Badge variant="outline" size="sm">
                    上限 {room.capacity} 人
                  </Badge>
                  <Badge variant={room.controlMode === "all" ? "info" : "outline"} size="sm">
                    {room.controlMode === "all" ? "所有人可控制播放" : "房主与授权的人控制播放"}
                  </Badge>
                  <Badge variant={room.allowRequests ? "outline" : "secondary"} size="sm">
                    {room.allowRequests ? "允许点歌" : "关闭点歌"}
                  </Badge>
                  {room.locked ? (
                    <Badge variant="warning" size="sm">
                      已锁定
                    </Badge>
                  ) : null}
                </div>
              </section>

              <section>
                <p className="mb-2 text-xs font-medium">
                  成员 <span className="text-muted-foreground tabular-nums">{room.members.length}</span>
                </p>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>成员</TableHead>
                      <TableHead>身份</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead>加入</TableHead>
                      <TableHead className="w-16" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {room.members.map((member) => (
                      <TableRow key={member.publicId}>
                        <TableCell className="max-w-48">
                          {member.userId ? (
                            <Link
                              href={`/app-users/${encodeURIComponent(appKey)}/${member.userId}`}
                              className="block truncate text-xs font-medium hover:underline"
                            >
                              {member.nickname}
                            </Link>
                          ) : (
                            <p className="truncate text-xs font-medium">{member.nickname}</p>
                          )}
                          <p className="font-mono text-[11px] text-muted-foreground tabular-nums">{member.publicId}</p>
                        </TableCell>
                        <TableCell className="text-xs">
                          {member.role === "host" ? (
                            <span className="flex items-center gap-1">
                              <Crown className="size-3.5 text-amber-500" />
                              房主
                            </span>
                          ) : member.canControl ? (
                            <span className="flex items-center gap-1">
                              <Hand className="size-3.5" />
                              可控制
                            </span>
                          ) : (
                            <span className="text-muted-foreground">成员</span>
                          )}
                        </TableCell>
                        <TableCell className="text-xs">
                          {member.online ? (
                            <Badge variant="success" size="sm">
                              {STATE_LABEL[member.state ?? "idle"] ?? "在线"}
                            </Badge>
                          ) : (
                            <Badge variant="secondary" size="sm">
                              离线
                            </Badge>
                          )}
                          {member.platform ? <span className="ml-1 text-[11px] text-muted-foreground">{member.platform}</span> : null}
                        </TableCell>
                        <TableCell className="text-xs whitespace-nowrap text-muted-foreground">{formatRelative(member.joinedAt)}</TableCell>
                        <TableCell className="text-right">
                          <AlertDialog>
                            <AlertDialogTrigger asChild>
                              <Button
                                size="sm"
                                variant="ghost"
                                className="text-destructive"
                                disabled={kickMutation.isPending || !member.userId}
                                title="移出房间"
                              >
                                <UserMinus className="size-3.5" />
                              </Button>
                            </AlertDialogTrigger>
                            <AlertDialogContent>
                              <AlertDialogHeader>
                                <AlertDialogTitle>移出 {member.nickname}？</AlertDialogTitle>
                                <AlertDialogDescription>
                                  移出后此人在这个房间存续期间不能再加入。
                                  {member.role === "host" ? "房主会转交给下一位在线成员。" : ""}
                                </AlertDialogDescription>
                              </AlertDialogHeader>
                              <AlertDialogFooter>
                                <AlertDialogCancel>取消</AlertDialogCancel>
                                <AlertDialogAction
                                  className="bg-destructive text-white hover:bg-destructive/90"
                                  onClick={() => void kick(room, member)}
                                >
                                  移出
                                </AlertDialogAction>
                              </AlertDialogFooter>
                            </AlertDialogContent>
                          </AlertDialog>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                {room.bannedPublicIds.length > 0 ? (
                  <p className="mt-2 text-[11px] text-muted-foreground">
                    已移出：<span className="font-mono tabular-nums">{room.bannedPublicIds.join("、")}</span>
                  </p>
                ) : null}
              </section>

              <section>
                <p className="mb-2 text-xs font-medium">
                  共享队列 <span className="text-muted-foreground tabular-nums">{room.queue.length}</span>
                </p>
                {room.queue.length === 0 ? (
                  <p className="text-xs text-muted-foreground">队列为空</p>
                ) : (
                  <ol className="max-h-72 space-y-0.5 overflow-y-auto rounded-xl border border-border p-1.5">
                    {room.queue.map((track, index) => {
                      const current = track.itemId === room.playback.track?.itemId;
                      return (
                        <li
                          key={track.itemId}
                          className={`flex items-center gap-2 rounded-lg px-2 py-1 text-xs ${current ? "bg-emerald-500/10" : ""}`}
                        >
                          <span className="w-6 text-right text-[11px] text-muted-foreground tabular-nums">{index + 1}</span>
                          <span className={`min-w-0 flex-1 truncate ${current ? "font-medium" : ""}`}>
                            {track.title}
                            {track.artist ? <span className="text-muted-foreground"> - {track.artist}</span> : null}
                          </span>
                          {track.requestedBy ? (
                            <Badge variant="info" size="sm">
                              点歌
                            </Badge>
                          ) : null}
                          <span className="font-mono text-[11px] text-muted-foreground">{track.source}</span>
                          <span className="w-10 text-right text-[11px] text-muted-foreground tabular-nums">{duration(track.durationMs)}</span>
                        </li>
                      );
                    })}
                  </ol>
                )}
              </section>

              <section>
                <p className="mb-2 text-xs font-medium">
                  最近聊天 <span className="text-muted-foreground tabular-nums">{room.chat.length}</span>
                </p>
                {room.chat.length === 0 ? (
                  <p className="text-xs text-muted-foreground">还没有人说话</p>
                ) : (
                  <ul className="max-h-72 space-y-1.5 overflow-y-auto rounded-xl border border-border p-3">
                    {room.chat.map((message) => (
                      <li key={message.id} className="text-xs">
                        <span className="font-medium">{message.nickname}</span>
                        <span className="ml-2 text-[11px] text-muted-foreground">{formatClockSeconds(message.sentAt)}</span>
                        <p className="mt-0.5 break-words whitespace-pre-wrap">{message.text}</p>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            </>
          )}
        </div>

        {room ? (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button size="sm" variant="outline" className="text-destructive" disabled={closeMutation.isPending}>
                  <XCircle className="size-3.5" />
                  解散房间
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>解散「{room.name || room.code}」？</AlertDialogTitle>
                  <AlertDialogDescription>所有成员会收到「房间已被管理员解散」并离开房间，操作记入审计日志。</AlertDialogDescription>
                </AlertDialogHeader>
                <Input value={reason} onChange={(event) => setReason(event.target.value)} placeholder="原因（选填，只记入日志）" maxLength={200} />
                <AlertDialogFooter>
                  <AlertDialogCancel>取消</AlertDialogCancel>
                  <AlertDialogAction className="bg-destructive text-white hover:bg-destructive/90" onClick={() => void close(room)}>
                    解散
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}
