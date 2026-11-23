"use client";

import { Fragment, useMemo, useState } from "react";
import { BellOff, Grid3x3, List, Loader2, Moon, Pencil, Plus, Save, Trash2, Workflow } from "lucide-react";
import { notify } from "@/lib/notify";
import { browserTimeZone } from "@/lib/datetime";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useDeleteNotifySubscriptionMutation,
  useNotifyCatalogQuery,
  useNotifyChannelsQuery,
  useNotifySubscriptionsQuery,
  useNotifyTemplatesQuery,
  useSaveNotifySubscriptionMutation
} from "@/lib/ticket-hooks";
import type { NotifyChannel, NotifyEventMeta, NotifySubscription } from "@/lib/api/notify-channels";
import { cn } from "@/lib/utils";
import { ChannelMark, PRIORITY_TEXT, SectionEmpty, SectionError, SectionHeader, reportError, useConfirm } from "./notify-kit";

// 事件订阅：「哪个事件 → 投到哪个渠道 → 用什么模板 → 什么条件下静默」。
// 矩阵看全局覆盖（行 = 事件，列 = 渠道），列表看每条订阅的细节；两种视图同一份数据。

const WILDCARD = "ticket.*";

type SubForm = {
  channelId: string;
  eventKey: string;
  minPriority: string;
  templateId: string;
  quietEnabled: boolean;
  quietStart: string;
  quietEnd: string;
  timezone: string;
  enabled: boolean;
};

function blankForm(channelId = "", eventKey = ""): SubForm {
  return {
    channelId,
    eventKey,
    minPriority: "any",
    templateId: "none",
    quietEnabled: false,
    quietStart: "23:00",
    quietEnd: "08:00",
    timezone: browserTimeZone(),
    enabled: true
  };
}

function formOf(sub: NotifySubscription): SubForm {
  return {
    channelId: String(sub.channelId),
    eventKey: sub.eventKey,
    minPriority: sub.minPriority || "any",
    templateId: sub.templateId ? String(sub.templateId) : "none",
    quietEnabled: Boolean(sub.quietHours),
    quietStart: sub.quietHours?.start || "23:00",
    quietEnd: sub.quietHours?.end || "08:00",
    timezone: sub.quietHours?.timezone || browserTimeZone(),
    enabled: sub.enabled
  };
}

const TIME_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;

export function SubscriptionsSection({ canWrite }: { canWrite: boolean }) {
  const catalogQuery = useNotifyCatalogQuery();
  const channelsQuery = useNotifyChannelsQuery();
  const subsQuery = useNotifySubscriptionsQuery();
  const templatesQuery = useNotifyTemplatesQuery(0);
  const saveMut = useSaveNotifySubscriptionMutation();
  const deleteMut = useDeleteNotifySubscriptionMutation();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const [view, setView] = useState<"matrix" | "list">("matrix");
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<NotifySubscription | null>(null);
  const [form, setForm] = useState<SubForm>(blankForm());

  const events = useMemo(() => catalogQuery.data?.events ?? [], [catalogQuery.data]);
  const channels = useMemo(() => channelsQuery.data ?? [], [channelsQuery.data]);
  const subs = useMemo(() => subsQuery.data ?? [], [subsQuery.data]);
  const templates = templatesQuery.data ?? [];

  const eventName = useMemo(() => {
    const map = new Map(events.map((item) => [item.key, item.name]));
    map.set(WILDCARD, "全部工单事件");
    return map;
  }, [events]);

  const openCreate = (channelId?: number, eventKey?: string) => {
    setEditing(null);
    setForm(blankForm(String(channelId ?? channels[0]?.id ?? ""), eventKey ?? events[0]?.key ?? ""));
    setOpen(true);
  };

  const openEdit = (sub: NotifySubscription) => {
    setEditing(sub);
    setForm(formOf(sub));
    setOpen(true);
  };

  const handleSave = async () => {
    if (!form.channelId || !form.eventKey) {
      notify.warning("请选择渠道与事件");
      return;
    }
    if (form.quietEnabled && (!TIME_PATTERN.test(form.quietStart) || !TIME_PATTERN.test(form.quietEnd))) {
      notify.warning("静默时间请用 24 小时制 HH:MM");
      return;
    }
    try {
      await saveMut.mutateAsync({
        id: editing?.id,
        payload: {
          channelId: Number(form.channelId),
          eventKey: form.eventKey,
          minPriority: form.minPriority === "any" ? "" : form.minPriority,
          templateId: form.templateId === "none" ? null : Number(form.templateId),
          quietHours: form.quietEnabled ? { timezone: form.timezone, start: form.quietStart, end: form.quietEnd } : null,
          enabled: form.enabled
        }
      });
      notify.success("订阅已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  const askDelete = (sub: NotifySubscription) =>
    confirm({
      title: "删除这条订阅？",
      description: `「${eventName.get(sub.eventKey) ?? sub.eventKey}」将不再投递到「${sub.channelName ?? "该渠道"}」。`,
      action: "删除",
      run: async () => {
        try {
          await deleteMut.mutateAsync(sub.id);
          notify.success("订阅已删除");
        } catch (error) {
          reportError(error, "删除失败");
        }
      }
    });

  const loading = subsQuery.isPending || channelsQuery.isPending || catalogQuery.isPending;
  const error = subsQuery.error ?? channelsQuery.error ?? catalogQuery.error;

  return (
    <div className="space-y-4">
      <SectionHeader title="事件订阅" description="业务只发事件，这里决定事件投递到哪些渠道、什么条件下静默。">
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={view}
          onValueChange={(value) => value && setView(value as "matrix" | "list")}
        >
          <ToggleGroupItem value="matrix" aria-label="矩阵视图">
            <Grid3x3 />
            矩阵
          </ToggleGroupItem>
          <ToggleGroupItem value="list" aria-label="列表视图">
            <List />
            列表
          </ToggleGroupItem>
        </ToggleGroup>
        {canWrite ? (
          <Button size="sm" onClick={() => openCreate()} disabled={channels.length === 0}>
            <Plus />
            新建订阅
          </Button>
        ) : null}
      </SectionHeader>

      {loading ? (
        <Card className="gap-2 p-4">
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className="h-9 w-full" />
          ))}
        </Card>
      ) : error ? (
        <SectionError
          error={error}
          onRetry={() => {
            void subsQuery.refetch();
            void channelsQuery.refetch();
            void catalogQuery.refetch();
          }}
        />
      ) : channels.length === 0 ? (
        <SectionEmpty icon={Workflow} title="先接入一个渠道" description="订阅需要投递目标，在「渠道」里新建后再回来。" />
      ) : view === "matrix" ? (
        <SubscriptionMatrix
          events={events}
          channels={channels}
          subs={subs}
          canWrite={canWrite}
          onCreate={openCreate}
          onEdit={openEdit}
        />
      ) : subs.length === 0 ? (
        <SectionEmpty
          icon={BellOff}
          title="还没有事件订阅"
          description="没有订阅时，任何事件都不会向外投递。"
          action={
            canWrite ? (
              <Button size="sm" onClick={() => openCreate()}>
                <Plus />
                新建订阅
              </Button>
            ) : null
          }
        />
      ) : (
        <Card className="gap-0 divide-y overflow-hidden p-0">
          {subs.map((sub) => (
            <SubscriptionRow
              key={sub.id}
              sub={sub}
              eventName={eventName.get(sub.eventKey) ?? sub.eventKey}
              templateName={templates.find((item) => item.id === sub.templateId)?.name}
              canWrite={canWrite}
              onEdit={() => openEdit(sub)}
              onDelete={() => askDelete(sub)}
            />
          ))}
        </Card>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑订阅" : "新建订阅"}</DialogTitle>
            <DialogDescription>事件发生且满足条件时，按所选模板投递到渠道。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>事件</Label>
                <Select value={form.eventKey} onValueChange={(value) => setForm((prev) => ({ ...prev, eventKey: value }))}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择事件" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={WILDCARD}>全部工单事件（ticket.*）</SelectItem>
                    {groupEvents(events).map(([group, items]) => (
                      <SelectGroup key={group}>
                        <SelectLabel>{group}</SelectLabel>
                        {items.map((event) => (
                          <SelectItem key={event.key} value={event.key}>
                            {event.name}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>投递渠道</Label>
                <Select value={form.channelId} onValueChange={(value) => setForm((prev) => ({ ...prev, channelId: value }))}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择渠道" />
                  </SelectTrigger>
                  <SelectContent>
                    {channels.map((channel) => (
                      <SelectItem key={channel.id} value={String(channel.id)}>
                        <ChannelMark kind={channel.kind} size="sm" />
                        {channel.name}
                        {!channel.enabled ? <span className="text-muted-foreground">（已停用）</span> : null}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            {form.eventKey && form.eventKey !== WILDCARD ? (
              <p className="-mt-2 text-[11px] text-muted-foreground">
                {events.find((item) => item.key === form.eventKey)?.description}
              </p>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>最低优先级</Label>
                <Select value={form.minPriority} onValueChange={(value) => setForm((prev) => ({ ...prev, minPriority: value }))}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="any">不限</SelectItem>
                    {Object.entries(PRIORITY_TEXT).map(([value, label]) => (
                      <SelectItem key={value} value={value}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>模板</Label>
                <Select value={form.templateId} onValueChange={(value) => setForm((prev) => ({ ...prev, templateId: value }))}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">使用默认渲染</SelectItem>
                    {templates.map((template) => (
                      <SelectItem key={template.id} value={String(template.id)}>
                        {template.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-3 rounded-xl border p-3">
              <label className="flex items-center justify-between gap-3">
                <span className="space-y-0.5">
                  <span className="flex items-center gap-1.5 text-sm font-medium">
                    <Moon className="size-3.5 text-muted-foreground" />
                    静默窗口
                  </span>
                  <span className="block text-[11px] text-muted-foreground">窗口内的事件跳过投递，计入「已跳过」</span>
                </span>
                <Switch checked={form.quietEnabled} onCheckedChange={(value) => setForm((prev) => ({ ...prev, quietEnabled: value }))} />
              </label>
              {form.quietEnabled ? (
                <div className="grid gap-2 sm:grid-cols-[1fr_auto_auto]">
                  <div className="space-y-1">
                    <Label className="text-[11px] text-muted-foreground">时区</Label>
                    <Input
                      value={form.timezone}
                      onChange={(event) => setForm((prev) => ({ ...prev, timezone: event.target.value }))}
                      placeholder={browserTimeZone()}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-[11px] text-muted-foreground">开始</Label>
                    <Input
                      type="time"
                      className="w-28"
                      value={form.quietStart}
                      onChange={(event) => setForm((prev) => ({ ...prev, quietStart: event.target.value }))}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-[11px] text-muted-foreground">结束</Label>
                    <Input
                      type="time"
                      className="w-28"
                      value={form.quietEnd}
                      onChange={(event) => setForm((prev) => ({ ...prev, quietEnd: event.target.value }))}
                    />
                  </div>
                </div>
              ) : null}
            </div>

            <label className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
              <span className="text-sm font-medium">启用</span>
              <Switch checked={form.enabled} onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))} />
            </label>
          </div>
          <DialogFooter>
            {editing && canWrite ? (
              <Button
                variant="ghost"
                className="mr-auto text-destructive"
                onClick={() => {
                  setOpen(false);
                  askDelete(editing);
                }}
              >
                <Trash2 />
                删除
              </Button>
            ) : null}
            <Button variant="ghost" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button disabled={saveMut.isPending} onClick={handleSave}>
              {saveMut.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {confirmDialog}
    </div>
  );
}

function groupEvents(events: NotifyEventMeta[]) {
  const groups = new Map<string, NotifyEventMeta[]>();
  for (const event of events) {
    const group = event.group || "其它";
    groups.set(group, [...(groups.get(group) ?? []), event]);
  }
  return [...groups.entries()];
}

function SubscriptionRow({
  sub,
  eventName,
  templateName,
  canWrite,
  onEdit,
  onDelete
}: {
  sub: NotifySubscription;
  eventName: string;
  templateName?: string;
  canWrite: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <div className={cn("flex flex-wrap items-center gap-3 px-4 py-3", !sub.enabled && "opacity-60")}>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="text-sm font-medium text-foreground">{eventName}</span>
          <span className="text-muted-foreground">→</span>
          <span className="inline-flex items-center gap-1.5 text-sm text-foreground">
            <ChannelMark kind={sub.channelKind} size="sm" />
            {sub.channelName ?? `渠道 #${sub.channelId}`}
          </span>
          {!sub.enabled ? (
            <Badge variant="secondary" size="sm">
              已停用
            </Badge>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
          <span className="font-mono">{sub.eventKey}</span>
          <Badge variant="outline" size="sm">
            {sub.minPriority ? PRIORITY_TEXT[sub.minPriority] ?? sub.minPriority : "不限优先级"}
          </Badge>
          {sub.quietHours ? (
            <Badge variant="outline" size="sm">
              <Moon className="size-3" />
              {sub.quietHours.start}–{sub.quietHours.end}
            </Badge>
          ) : null}
          <Badge variant="outline" size="sm">
            {templateName ? `模板：${templateName}` : sub.templateId ? "自定义模板" : "默认渲染"}
          </Badge>
        </div>
      </div>
      {canWrite ? (
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" onClick={onEdit}>
            <Pencil />
            编辑
          </Button>
          <Button size="icon-sm" variant="ghost" className="text-destructive" onClick={onDelete} aria-label="删除订阅">
            <Trash2 />
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function SubscriptionMatrix({
  events,
  channels,
  subs,
  canWrite,
  onCreate,
  onEdit
}: {
  events: NotifyEventMeta[];
  channels: NotifyChannel[];
  subs: NotifySubscription[];
  canWrite: boolean;
  onCreate: (channelId: number, eventKey: string) => void;
  onEdit: (sub: NotifySubscription) => void;
}) {
  const index = useMemo(() => {
    const map = new Map<string, NotifySubscription[]>();
    for (const sub of subs) {
      const key = `${sub.eventKey}|${sub.channelId}`;
      map.set(key, [...(map.get(key) ?? []), sub]);
    }
    return map;
  }, [subs]);

  // 目录里没有、但订阅里出现过的事件（旧事件 key）也要列出，否则那些订阅在矩阵里看不到
  const known = new Set(events.map((item) => item.key));
  const orphans = [...new Set(subs.map((sub) => sub.eventKey))].filter((key) => key !== WILDCARD && !known.has(key));
  const groups = groupEvents([
    ...events,
    ...orphans.map((key) => ({ key, name: key, group: "目录外事件", description: "" }))
  ]);

  const cell = (eventKey: string, channel: NotifyChannel) => {
    const found = index.get(`${eventKey}|${channel.id}`) ?? [];
    const viaWildcard =
      eventKey !== WILDCARD && eventKey.startsWith("ticket.") && (index.get(`${WILDCARD}|${channel.id}`) ?? []).some((sub) => sub.enabled);
    return (
      <MatrixCell
        key={channel.id}
        subs={found}
        viaWildcard={viaWildcard}
        canWrite={canWrite}
        onCreate={() => onCreate(channel.id, eventKey)}
        onEdit={onEdit}
      />
    );
  };

  return (
    <Card className="gap-0 overflow-hidden p-0">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b bg-muted/30">
              <th className="sticky left-0 z-10 min-w-56 bg-muted/30 px-4 py-2.5 text-left text-xs font-medium text-muted-foreground backdrop-blur">
                事件 \ 渠道
              </th>
              {channels.map((channel) => (
                <th key={channel.id} className="min-w-24 px-2 py-2.5 text-center font-normal">
                  <div className={cn("flex flex-col items-center gap-1", !channel.enabled && "opacity-50")}>
                    <ChannelMark kind={channel.kind} size="sm" />
                    <span className="max-w-24 truncate text-[11px] text-foreground" title={channel.name}>
                      {channel.name}
                    </span>
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            <tr className="border-b bg-primary/[0.03]">
              <td className="sticky left-0 z-10 bg-background px-4 py-2">
                <p className="text-sm font-medium">全部工单事件</p>
                <p className="font-mono text-[10px] text-muted-foreground">{WILDCARD}</p>
              </td>
              {channels.map((channel) => cell(WILDCARD, channel))}
            </tr>
            {groups.map(([group, items]) => (
              <Fragment key={group}>
                <tr className="border-b bg-muted/20">
                  <td colSpan={channels.length + 1} className="sticky left-0 px-4 py-1.5 text-[11px] font-medium text-muted-foreground">
                    {group}
                  </td>
                </tr>
                {items.map((event) => (
                  <tr key={event.key} className="border-b last:border-b-0 hover:bg-muted/20">
                    <td className="sticky left-0 z-10 bg-background px-4 py-2">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="min-w-0 cursor-default">
                            <p className="truncate text-sm">{event.name}</p>
                            <p className="truncate font-mono text-[10px] text-muted-foreground">{event.key}</p>
                          </div>
                        </TooltipTrigger>
                        {event.description ? <TooltipContent className="max-w-64">{event.description}</TooltipContent> : null}
                      </Tooltip>
                    </td>
                    {channels.map((channel) => cell(event.key, channel))}
                  </tr>
                ))}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center gap-4 border-t px-4 py-2 text-[11px] text-muted-foreground">
        <Legend className="bg-primary" label="已订阅" />
        <Legend className="bg-muted-foreground/40" label="订阅已停用" />
        <Legend className="border border-dashed border-primary/60 bg-transparent" label="由 ticket.* 覆盖" />
        {canWrite ? <span className="ml-auto">点空格新建，点圆点编辑</span> : null}
      </div>
    </Card>
  );
}

function Legend({ className, label }: { className: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn("size-2.5 rounded-full", className)} />
      {label}
    </span>
  );
}

function MatrixCell({
  subs,
  viaWildcard,
  canWrite,
  onCreate,
  onEdit
}: {
  subs: NotifySubscription[];
  viaWildcard: boolean;
  canWrite: boolean;
  onCreate: () => void;
  onEdit: (sub: NotifySubscription) => void;
}) {
  if (subs.length === 0) {
    return (
      <td className="px-2 py-2 text-center">
        {canWrite ? (
          <button
            type="button"
            onClick={onCreate}
            aria-label="新建订阅"
            className={cn(
              "group mx-auto grid size-7 place-items-center rounded-full transition-colors hover:bg-primary/10",
              viaWildcard && "border border-dashed border-primary/60"
            )}
          >
            <Plus className="size-3.5 text-primary opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100" />
          </button>
        ) : viaWildcard ? (
          <span className="mx-auto block size-2.5 rounded-full border border-dashed border-primary/60" />
        ) : null}
      </td>
    );
  }
  return (
    <td className="px-2 py-2 text-center">
      <div className="flex items-center justify-center gap-1">
        {subs.map((sub) => {
          const detail = [
            sub.minPriority ? PRIORITY_TEXT[sub.minPriority] ?? sub.minPriority : "不限优先级",
            sub.quietHours ? `静默 ${sub.quietHours.start}–${sub.quietHours.end}` : null,
            sub.enabled ? null : "已停用"
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <Tooltip key={sub.id}>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  disabled={!canWrite}
                  onClick={() => onEdit(sub)}
                  aria-label={`订阅：${detail}`}
                  className="grid size-7 place-items-center rounded-full transition-colors enabled:hover:bg-primary/10"
                >
                  <span
                    className={cn(
                      "relative grid size-3 place-items-center rounded-full",
                      sub.enabled ? "bg-primary shadow-[0_0_0_3px] shadow-primary/15" : "bg-muted-foreground/40"
                    )}
                  >
                    {sub.quietHours ? <Moon className="absolute -right-2 -top-2 size-2.5 text-muted-foreground" /> : null}
                  </span>
                </button>
              </TooltipTrigger>
              <TooltipContent>{detail}</TooltipContent>
            </Tooltip>
          );
        })}
      </div>
    </td>
  );
}
