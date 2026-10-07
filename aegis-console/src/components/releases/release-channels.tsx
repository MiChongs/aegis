"use client";

import { useState } from "react";
import { toast } from "sonner";
import { MoreHorizontal, Pencil, Plus, Trash2, UserPlus, Users, X } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { VersionChannel } from "@/lib/api/types";
import {
  useDeleteReleaseChannelMutation,
  useReleaseChannelMembersMutation,
  useReleaseChannelUsersQuery,
  useReleaseChannelsQuery,
  useSaveReleaseChannelMutation
} from "@/lib/release-hooks";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { CHANNEL_LEVELS, ChannelDot, DATA_FONT, parseIdList } from "./release-shared";

/**
 * 发布渠道。渠道只决定「谁在这条轨道上」：成员由管理员分配，或在开放自助加入后由用户在客户端加入。
 * 灰度与定向一律设在版本上，渠道不再承担这两件事。
 */
export function ReleaseChannels({ appKey }: { appKey: string }) {
  const channelsQuery = useReleaseChannelsQuery(appKey);
  const remove = useDeleteReleaseChannelMutation(appKey);
  const [editing, setEditing] = useState<{ open: boolean; item?: VersionChannel | null }>({ open: false });
  const [members, setMembers] = useState<VersionChannel | null>(null);
  const [deleting, setDeleting] = useState<VersionChannel | null>(null);
  const channels = channelsQuery.data ?? [];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">共 {channels.length} 个渠道</p>
        <Button size="sm" onClick={() => setEditing({ open: true, item: null })}>
          <Plus className="size-3.5" />
          新建渠道
        </Button>
      </div>

      {channelsQuery.isLoading ? (
        <div className="grid gap-3 lg:grid-cols-2">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-28 rounded-xl" />
          ))}
        </div>
      ) : channels.length === 0 ? (
        <EmptyState title="暂无渠道" />
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {channels.map((channel) => (
            <div key={channel.id} className="space-y-2 rounded-xl border bg-card px-4 py-3">
              <div className="flex items-start gap-2">
                <ChannelDot color={channel.color} className="mt-1.5" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="font-medium">{channel.name}</span>
                    <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{channel.code}</span>
                    {channel.is_default ? <Badge variant="secondary">默认</Badge> : null}
                    {channel.self_join ? <Badge variant="info">可自助加入</Badge> : null}
                    {channel.status === false ? <Badge variant="warning">已停用</Badge> : null}
                  </div>
                  {channel.description ? <p className="mt-0.5 text-xs text-muted-foreground">{channel.description}</p> : null}
                </div>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon" className="size-8">
                      <MoreHorizontal className="size-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => setEditing({ open: true, item: channel })}>
                      <Pencil className="size-3.5" />
                      编辑
                    </DropdownMenuItem>
                    {!channel.is_default ? (
                      <DropdownMenuItem onClick={() => setMembers(channel)}>
                        <Users className="size-3.5" />
                        成员
                      </DropdownMenuItem>
                    ) : null}
                    {!channel.is_default ? (
                      <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={() => setDeleting(channel)}>
                        <Trash2 className="size-3.5" />
                        删除
                      </DropdownMenuItem>
                    ) : null}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
              <div className="flex items-center gap-4 text-xs text-muted-foreground">
                <span>{CHANNEL_LEVELS.find((level) => level.value === channel.level)?.label ?? channel.level ?? "正式"}</span>
                <span>{channel.is_default ? "全部用户" : `${channel.userCount ?? 0} 名成员`}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing.open ? (
        <ChannelDialog appKey={appKey} item={editing.item} open onOpenChange={(open) => setEditing((state) => ({ ...state, open }))} />
      ) : null}
      {members ? <MembersDialog appKey={appKey} channel={members} onClose={() => setMembers(null)} /> : null}

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除渠道「{deleting?.name}」</AlertDialogTitle>
            <AlertDialogDescription>成员关系将一并清除。渠道下仍有下发中、待发布或已暂停的版本时无法删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                if (!deleting) return;
                try {
                  await remove.mutateAsync(deleting.id);
                  toast.success("已删除");
                } catch (error) {
                  toast.error(error instanceof ApiError ? error.message : "删除失败");
                }
                setDeleting(null);
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ChannelDialog({
  appKey,
  item,
  open,
  onOpenChange
}: {
  appKey: string;
  item?: VersionChannel | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const save = useSaveReleaseChannelMutation(appKey);
  const [form, setForm] = useState({
    name: item?.name ?? "",
    code: item?.code ?? "",
    description: item?.description ?? "",
    level: item?.level || "stable",
    color: item?.color ?? "",
    priority: String(item?.priority ?? 0),
    status: item?.status ?? true,
    selfJoin: item?.self_join ?? false
  });
  const [error, setError] = useState("");

  async function submit() {
    if (!form.name.trim() || !form.code.trim()) {
      setError("填写名称与标识");
      return;
    }
    if (!/^[a-z0-9][a-z0-9_-]*$/.test(form.code.trim())) {
      setError("标识只能由小写字母、数字、- 与 _ 组成");
      return;
    }
    try {
      await save.mutateAsync({
        channelId: item?.id,
        payload: {
          name: form.name.trim(),
          code: form.code.trim(),
          description: form.description.trim(),
          level: form.level,
          color: form.color.trim(),
          priority: Number(form.priority) || 0,
          status: form.status,
          self_join: form.selfJoin
        }
      });
      toast.success("已保存");
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{item ? "编辑渠道" : "新建渠道"}</DialogTitle>
          <DialogDescription>渠道决定哪些用户能收到挂在其上的版本</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs">名称</Label>
              <Input value={form.name} placeholder="Beta 体验计划" onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">标识</Label>
              <Input
                className={DATA_FONT}
                value={form.code}
                placeholder="beta"
                disabled={Boolean(item?.is_default)}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">级别</Label>
              <Select value={form.level} onValueChange={(value) => setForm({ ...form, level: value })}>
                <SelectTrigger className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CHANNEL_LEVELS.map((level) => (
                    <SelectItem key={level.value} value={level.value}>
                      {level.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">标签颜色</Label>
              <div className="flex items-center gap-2">
                <input
                  type="color"
                  className="h-9 w-10 shrink-0 cursor-pointer rounded-md border bg-transparent p-1"
                  value={form.color || "#6366f1"}
                  onChange={(e) => setForm({ ...form, color: e.target.value })}
                />
                <Input className={DATA_FONT} value={form.color} placeholder="#6366f1" onChange={(e) => setForm({ ...form, color: e.target.value })} />
              </div>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">说明</Label>
            <Textarea rows={2} value={form.description} placeholder="向用户展示的渠道说明" onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">排序优先级</Label>
            <Input className={DATA_FONT} inputMode="numeric" value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value.replace(/[^\d-]/g, "") })} />
          </div>
          <div className="divide-y rounded-lg border">
            <label className="flex items-center gap-3 px-3 py-2.5">
              <div className="flex-1">
                <div className="text-sm">允许自助加入</div>
                <div className="text-xs text-muted-foreground">登录用户可在客户端加入或退出该渠道</div>
              </div>
              <Switch checked={form.selfJoin} disabled={Boolean(item?.is_default)} onCheckedChange={(value) => setForm({ ...form, selfJoin: value })} />
            </label>
            <label className="flex items-center gap-3 px-3 py-2.5">
              <div className="flex-1">
                <div className="text-sm">启用</div>
                <div className="text-xs text-muted-foreground">停用后该渠道上的版本不再下发</div>
              </div>
              <Switch checked={form.status} disabled={Boolean(item?.is_default)} onCheckedChange={(value) => setForm({ ...form, status: value })} />
            </label>
          </div>
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={save.isPending} onClick={() => void submit()}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MembersDialog({ appKey, channel, onClose }: { appKey: string; channel: VersionChannel; onClose: () => void }) {
  const [page, setPage] = useState(1);
  const [input, setInput] = useState("");
  const usersQuery = useReleaseChannelUsersQuery(appKey, channel.id, page);
  const members = useReleaseChannelMembersMutation(appKey);
  const items = usersQuery.data?.items ?? [];
  const total = usersQuery.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / 20));

  async function change(userIds: number[], remove: boolean) {
    if (!userIds.length) return;
    try {
      await members.mutateAsync({ channelId: channel.id, userIds, remove });
      toast.success(remove ? "已移出" : "已添加");
      if (!remove) setInput("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{channel.name} 成员</DialogTitle>
          <DialogDescription>共 {total} 名</DialogDescription>
        </DialogHeader>
        <div className="flex gap-2">
          <Input className={DATA_FONT} value={input} placeholder="用户 ID，多个以逗号分隔" onChange={(e) => setInput(e.target.value)} />
          <Button size="sm" className="h-9" disabled={members.isPending} onClick={() => void change(parseIdList(input), false)}>
            <UserPlus className="size-3.5" />
            添加
          </Button>
        </div>
        <div className="max-h-80 divide-y overflow-y-auto rounded-lg border">
          {usersQuery.isLoading ? (
            Array.from({ length: 4 }, (_, index) => (
              <div key={index} className="px-3 py-2.5">
                <Skeleton className="h-4 w-40" />
              </div>
            ))
          ) : items.length === 0 ? (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">暂无成员</p>
          ) : (
            items.map((user) => (
              <div key={user.userId || user.id} className="flex items-center gap-3 px-3 py-2">
                <span className={cn("w-16 shrink-0 text-xs text-muted-foreground", DATA_FONT)}>{user.userId || user.id}</span>
                <span className="min-w-0 flex-1 truncate text-sm">{user.nickname || user.account || "—"}</span>
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7"
                  aria-label="移出"
                  disabled={members.isPending}
                  onClick={() => void change([user.userId || user.id], true)}
                >
                  <X className="size-3.5" />
                </Button>
              </div>
            ))
          )}
        </div>
        {pages > 1 ? (
          <div className="flex items-center justify-center gap-2">
            <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              上一页
            </Button>
            <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>
              {page} / {pages}
            </span>
            <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>
              下一页
            </Button>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
