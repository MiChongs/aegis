"use client";

import { useEffect, useMemo, useState } from "react";
import { ExternalLink, KeyRound, Loader2, Pencil, Plus, Radio, Save, Send, Trash2 } from "lucide-react";
import { notify } from "@/lib/notify";
import { formatRelative } from "@/lib/datetime";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useDeleteNotifyChannelMutation,
  useNotifyCatalogQuery,
  useNotifyChannelsQuery,
  useSaveNotifyChannelMutation,
  useTestNotifyChannelMutation
} from "@/lib/ticket-hooks";
import type { NotifyChannel, NotifyChannelKind, NotifyChannelKindMeta, NotifyConfigField } from "@/lib/api/notify-channels";
import { cn } from "@/lib/utils";
import { ChannelHealth, ChannelMark, SectionEmpty, SectionError, SectionHeader, reportError, useConfirm } from "./notify-kit";

// 渠道：卡片网格。配置表单完全由 /catalog 的元数据渲染，新增一种 IM 时这里零改动。

type ChannelForm = { key: string; name: string; enabled: boolean; rateLimit: number; secret: string };

const EMPTY_FORM: ChannelForm = { key: "", name: "", enabled: true, rateLimit: 0, secret: "" };

export function ChannelsSection({ canWrite, canTest }: { canWrite: boolean; canTest: boolean }) {
  const catalogQuery = useNotifyCatalogQuery();
  const channelsQuery = useNotifyChannelsQuery();
  const saveMut = useSaveNotifyChannelMutation();
  const deleteMut = useDeleteNotifyChannelMutation();
  const testMut = useTestNotifyChannelMutation();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<NotifyChannel | null>(null);
  const [kind, setKind] = useState<NotifyChannelKind>("feishu_bot");
  const [form, setForm] = useState<ChannelForm>(EMPTY_FORM);
  const [config, setConfig] = useState<Record<string, unknown>>({});
  const [testingId, setTestingId] = useState<number | null>(null);
  const [togglingId, setTogglingId] = useState<number | null>(null);

  // 单独 memo 一次，避免 ?? [] 每次渲染都产生新数组
  const kinds = useMemo(() => catalogQuery.data?.kinds ?? [], [catalogQuery.data]);
  const kindMeta = useMemo(() => new Map(kinds.map((item) => [item.kind, item])), [kinds]);
  const meta = kindMeta.get(kind);
  const channels = channelsQuery.data ?? [];

  const openCreate = () => {
    setEditing(null);
    setKind(kinds[0]?.kind ?? "feishu_bot");
    setForm(EMPTY_FORM);
    setConfig({});
    setOpen(true);
  };

  const openEdit = (channel: NotifyChannel) => {
    setEditing(channel);
    setKind(channel.kind);
    setForm({
      key: channel.key,
      name: channel.name,
      enabled: channel.enabled,
      rateLimit: channel.rateLimitPerMinute,
      // 留空表示保持原密钥不变，与后端 SaveChannel 的语义一致
      secret: ""
    });
    setConfig({ ...channel.config });
    setOpen(true);
  };

  const handleSave = async () => {
    if (!form.key.trim() || !form.name.trim()) {
      notify.warning("请填写渠道标识与名称");
      return;
    }
    const missing = meta?.fields.find((field) => field.required && isBlank(config[field.key]));
    if (missing) {
      notify.warning(`请填写「${missing.label}」`);
      return;
    }
    try {
      await saveMut.mutateAsync({
        id: editing?.id,
        payload: {
          appid: editing?.appid ?? 0,
          key: form.key.trim(),
          name: form.name.trim(),
          kind,
          config,
          secret: form.secret,
          enabled: form.enabled,
          rateLimitPerMinute: Number(form.rateLimit) || 0
        }
      });
      notify.success("渠道已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  // 卡片上的启停开关：按原值回写一遍，密钥留空 = 不变
  const toggleEnabled = async (channel: NotifyChannel, enabled: boolean) => {
    setTogglingId(channel.id);
    try {
      await saveMut.mutateAsync({
        id: channel.id,
        payload: {
          appid: channel.appid,
          key: channel.key,
          name: channel.name,
          kind: channel.kind,
          config: channel.config,
          secret: "",
          enabled,
          rateLimitPerMinute: channel.rateLimitPerMinute
        }
      });
      notify.success(enabled ? "渠道已启用" : "渠道已停用");
    } catch (error) {
      reportError(error, "操作失败");
    } finally {
      setTogglingId(null);
    }
  };

  const handleTest = async (channel: NotifyChannel) => {
    setTestingId(channel.id);
    try {
      const result = await testMut.mutateAsync(channel.id);
      // 耗时缺失时不要拼出「undefinedms」
      const latency = Number(result?.latencyMs);
      const title = Number.isFinite(latency) ? `测试消息已发送（${latency}ms）` : "测试消息已发送";
      notify.success(title, { description: result?.responseSnippet?.slice(0, 120) });
    } catch (error) {
      reportError(error, "测试发送失败");
    } finally {
      setTestingId(null);
    }
  };

  const askDelete = (channel: NotifyChannel) =>
    confirm({
      title: `删除渠道「${channel.name}」？`,
      description:
        (channel.subscriptions?.length ?? 0) > 0
          ? `它挂着 ${channel.subscriptions?.length} 条事件订阅，删除后这些事件不再投递到这里。`
          : "删除后无法恢复，历史投递记录会保留。",
      action: "删除",
      run: async () => {
        try {
          await deleteMut.mutateAsync(channel.id);
          notify.success("渠道已删除");
        } catch (error) {
          reportError(error, "删除失败");
        }
      }
    });

  return (
    <div className="space-y-4">
      <SectionHeader title="渠道" description="事件最终投递到哪里：IM 机器人、Webhook、邮件或站内实时推送。">
        {canWrite ? (
          <Button size="sm" onClick={openCreate} disabled={kinds.length === 0}>
            <Plus />
            新建渠道
          </Button>
        ) : null}
      </SectionHeader>

      {!canWrite ? (
        <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
          只读：缺少「渠道与订阅：管理」权限
        </p>
      ) : null}

      {channelsQuery.isPending ? (
        <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
          {Array.from({ length: 4 }, (_, index) => (
            <Card key={index} className="gap-3 p-4">
              <div className="flex items-center gap-3">
                <Skeleton className="size-11 rounded-xl" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-20" />
                </div>
              </div>
              <Skeleton className="h-3 w-3/4" />
              <Skeleton className="h-8 w-full" />
            </Card>
          ))}
        </div>
      ) : channelsQuery.isError ? (
        <SectionError error={channelsQuery.error} onRetry={() => channelsQuery.refetch()} />
      ) : channels.length === 0 ? (
        <SectionEmpty
          icon={Radio}
          title="还没有通知渠道"
          description="先接入一个渠道，再到「订阅」里决定哪些事件投递过去。"
          action={
            canWrite ? (
              <Button size="sm" onClick={openCreate} disabled={kinds.length === 0}>
                <Plus />
                新建渠道
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
          {channels.map((channel) => (
            <ChannelCard
              key={channel.id}
              channel={channel}
              kindName={kindMeta.get(channel.kind)?.name ?? channel.kind}
              canWrite={canWrite}
              canTest={canTest}
              testing={testingId === channel.id}
              toggling={togglingId === channel.id}
              onTest={() => handleTest(channel)}
              onEdit={() => openEdit(channel)}
              onDelete={() => askDelete(channel)}
              onToggle={(enabled) => toggleEnabled(channel, enabled)}
            />
          ))}
          {canWrite ? (
            <button
              type="button"
              onClick={openCreate}
              className="flex min-h-40 flex-col items-center justify-center gap-2 rounded-xl border border-dashed text-sm text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary"
            >
              <Plus className="size-5" />
              接入新渠道
            </button>
          ) : null}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[88vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑通知渠道" : "新建通知渠道"}</DialogTitle>
            <DialogDescription>{editing ? "类型创建后不可更改。" : "选择渠道类型，再填写它需要的配置。"}</DialogDescription>
          </DialogHeader>

          <div className="space-y-5">
            <div className="space-y-2">
              <Label>渠道类型</Label>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                {kinds.map((item) => {
                  const active = item.kind === kind;
                  const locked = Boolean(editing) && !active;
                  return (
                    <button
                      key={item.kind}
                      type="button"
                      disabled={locked}
                      onClick={() => {
                        if (active) return;
                        setKind(item.kind);
                        setConfig({});
                      }}
                      className={cn(
                        "flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-xs transition-colors",
                        active ? "border-primary bg-primary/5 ring-1 ring-primary/30" : "hover:bg-accent",
                        locked && "cursor-not-allowed opacity-40"
                      )}
                    >
                      <ChannelMark kind={item.kind} size="sm" />
                      <span className="min-w-0 truncate font-medium text-foreground">{item.name}</span>
                    </button>
                  );
                })}
              </div>
              {meta ? <KindNote meta={meta} /> : null}
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="notify-channel-key">标识</Label>
                <Input
                  id="notify-channel-key"
                  value={form.key}
                  onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
                  placeholder="feishu-ops"
                  className="font-mono"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="notify-channel-name">名称</Label>
                <Input
                  id="notify-channel-name"
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="运维群机器人"
                />
              </div>
            </div>

            {meta && (meta.fields.length > 0 || meta.needsSecret) ? (
              <div className="space-y-3 rounded-xl border bg-muted/20 p-3">
                <p className="text-xs font-medium text-muted-foreground">{meta.name} 配置</p>
                {/* 配置项完全由后端元数据驱动；key 带上类型，切换类型时默认值重新兜底 */}
                {meta.fields.map((field) => (
                  <ConfigFieldInput
                    key={`${kind}:${field.key}`}
                    field={field}
                    value={config[field.key]}
                    onChange={(value) => setConfig((prev) => ({ ...prev, [field.key]: value }))}
                  />
                ))}
                {meta.needsSecret ? (
                  <div className="space-y-1.5">
                    <Label htmlFor="notify-channel-secret" className="flex items-center gap-1.5">
                      <KeyRound className="size-3.5 text-muted-foreground" />
                      {meta.secretLabel || "密钥"}
                      {editing?.secretSet ? (
                        <Badge variant="secondary" size="sm">
                          已配置 {editing.secretHint}
                        </Badge>
                      ) : null}
                    </Label>
                    <Input
                      id="notify-channel-secret"
                      type="password"
                      autoComplete="new-password"
                      value={form.secret}
                      onChange={(event) => setForm((prev) => ({ ...prev, secret: event.target.value }))}
                      placeholder={editing?.secretSet ? "留空不修改，输入 - 清空" : "请输入密钥"}
                    />
                  </div>
                ) : null}
              </div>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-2">
              <label className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
                <span className="space-y-0.5">
                  <span className="block text-sm font-medium">启用</span>
                  <span className="block text-[11px] text-muted-foreground">停用后订阅保留，但不再投递</span>
                </span>
                <Switch checked={form.enabled} onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))} />
              </label>
              <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5">
                <span className="space-y-0.5">
                  <Label htmlFor="notify-channel-rate" className="block text-sm font-medium">
                    每分钟上限
                  </Label>
                  <span className="block text-[11px] text-muted-foreground">0 表示不限</span>
                </span>
                <Input
                  id="notify-channel-rate"
                  type="number"
                  min={0}
                  className="h-8 w-24 text-right tabular-nums"
                  value={form.rateLimit}
                  onChange={(event) => setForm((prev) => ({ ...prev, rateLimit: Number(event.target.value) }))}
                />
              </div>
            </div>
          </div>

          <DialogFooter>
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

function isBlank(value: unknown) {
  if (value === undefined || value === null) return true;
  if (typeof value === "string") return value.trim() === "";
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

function KindNote({ meta }: { meta: NotifyChannelKindMeta }) {
  if (!meta.description && !meta.docUrl) return null;
  return (
    <p className="text-[11px] leading-5 text-muted-foreground">
      {meta.description}
      {meta.docUrl ? (
        <a
          href={meta.docUrl}
          target="_blank"
          rel="noreferrer"
          className="ml-1 inline-flex items-center gap-0.5 text-primary hover:underline"
        >
          官方文档
          <ExternalLink className="size-3" />
        </a>
      ) : null}
    </p>
  );
}

function ChannelCard({
  channel,
  kindName,
  canWrite,
  canTest,
  testing,
  toggling,
  onTest,
  onEdit,
  onDelete,
  onToggle
}: {
  channel: NotifyChannel;
  kindName: string;
  canWrite: boolean;
  canTest: boolean;
  testing: boolean;
  toggling: boolean;
  onTest: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: (enabled: boolean) => void;
}) {
  const subs = channel.subscriptions?.length ?? 0;
  const failed = channel.lastStatus && channel.lastStatus !== "success";

  return (
    <Card className={cn("gap-0 overflow-hidden p-0 transition-shadow hover:shadow-md", !channel.enabled && "bg-muted/30")}>
      <div className="flex items-start gap-3 p-4 pb-3">
        <ChannelMark kind={channel.kind} size="lg" className={cn(!channel.enabled && "grayscale")} />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-semibold text-foreground">{channel.name}</span>
            {!channel.enabled ? (
              <Badge variant="secondary" size="sm">
                已停用
              </Badge>
            ) : null}
          </div>
          <div className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <span className="shrink-0">{kindName}</span>
            <span>·</span>
            <span className="truncate font-mono">{channel.key}</span>
          </div>
        </div>
        {canWrite ? (
          <Switch
            checked={channel.enabled}
            disabled={toggling}
            onCheckedChange={onToggle}
            aria-label={channel.enabled ? "停用渠道" : "启用渠道"}
          />
        ) : null}
      </div>

      <div className="grid grid-cols-3 border-y bg-muted/20 text-center">
        <CardStat label="订阅事件" value={String(subs)} />
        <CardStat label="限流" value={channel.rateLimitPerMinute > 0 ? `${channel.rateLimitPerMinute}/分` : "不限"} />
        <CardStat label="最近投递" value={channel.lastSentAt ? formatRelative(channel.lastSentAt) : "—"} />
      </div>

      <div className="space-y-2 px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <ChannelHealth status={channel.lastStatus} error={channel.lastError} />
          {channel.secretSet ? (
            <Badge variant="outline" size="sm" className="ml-auto">
              <KeyRound className="size-3" />
              {channel.secretHint || "已配置密钥"}
            </Badge>
          ) : null}
        </div>
        {failed && channel.lastError ? (
          <p className="line-clamp-2 rounded-md bg-red-500/8 px-2 py-1.5 font-mono text-[11px] leading-4 text-red-600 dark:text-red-400" title={channel.lastError}>
            {channel.lastError}
          </p>
        ) : null}
      </div>

      {canWrite || canTest ? (
        <div className="mt-auto flex items-center gap-1 border-t px-3 py-2">
          {canTest ? (
            <Button size="sm" variant="outline" onClick={onTest} disabled={testing || !channel.enabled}>
              {testing ? <Loader2 className="animate-spin" /> : <Send />}
              发送测试
            </Button>
          ) : null}
          {canWrite ? (
            <>
              <Button size="sm" variant="ghost" onClick={onEdit} className="ml-auto">
                <Pencil />
                编辑
              </Button>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button size="icon-sm" variant="ghost" className="text-destructive" onClick={onDelete} aria-label="删除渠道">
                    <Trash2 />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>删除渠道</TooltipContent>
              </Tooltip>
            </>
          ) : null}
        </div>
      ) : null}
    </Card>
  );
}

function CardStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 px-2 py-2 [&+&]:border-l">
      <p className="truncate text-sm font-medium tabular-nums text-foreground">{value}</p>
      <p className="text-[10px] text-muted-foreground">{label}</p>
    </div>
  );
}

function ConfigFieldInput({
  field,
  value,
  onChange
}: {
  field: NotifyConfigField;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  // 元数据里的 default 只在首次渲染时兜底，避免覆盖用户已填内容
  useEffect(() => {
    if (value === undefined && field.default) onChange(field.default);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const id = `notify-field-${field.key}`;
  const label = (
    <Label htmlFor={id}>
      {field.label}
      {field.required ? <span className="ml-0.5 text-destructive">*</span> : null}
    </Label>
  );
  const help = field.help ? <p className="text-[11px] leading-4 text-muted-foreground">{field.help}</p> : null;

  if (field.type === "switch") {
    return (
      <div className="flex items-center justify-between gap-3 rounded-lg border bg-background px-3 py-2">
        <div className="space-y-0.5">
          {label}
          {help}
        </div>
        <Switch id={id} checked={Boolean(value)} onCheckedChange={(next) => onChange(next)} />
      </div>
    );
  }

  if (field.type === "select") {
    return (
      <div className="space-y-1.5">
        {label}
        <Select value={String(value ?? field.default ?? "")} onValueChange={(next) => onChange(next)}>
          <SelectTrigger id={id} className="w-full bg-background">
            <SelectValue placeholder={field.placeholder} />
          </SelectTrigger>
          <SelectContent>
            {(field.options ?? []).map((option) => (
              <SelectItem key={option} value={option}>
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {help}
      </div>
    );
  }

  if (field.type === "tags") {
    return (
      <div className="space-y-1.5">
        {label}
        <TagsField id={id} value={value} placeholder={field.placeholder} onChange={onChange} />
        {help ?? <p className="text-[11px] text-muted-foreground">每行一项</p>}
      </div>
    );
  }

  if (field.type === "textarea") {
    return (
      <div className="space-y-1.5">
        {label}
        <Textarea
          id={id}
          rows={3}
          value={String(value ?? "")}
          placeholder={field.placeholder}
          className="bg-background"
          onChange={(event) => onChange(event.target.value)}
        />
        {help}
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      {label}
      <Input
        id={id}
        type={field.type === "password" ? "password" : "text"}
        autoComplete={field.type === "password" ? "new-password" : undefined}
        value={String(value ?? "")}
        placeholder={field.placeholder}
        className="bg-background"
        onChange={(event) => onChange(event.target.value)}
      />
      {help}
    </div>
  );
}

/**
 * tags：按行拆成数组提交（后端 cfgStrings 也能吃字符串，统一成数组更直观）。
 * 输入框自己留一份原文，否则敲回车产生的空行会被立刻过滤掉，没法换行。
 */
function TagsField({
  id,
  value,
  placeholder,
  onChange
}: {
  id: string;
  value: unknown;
  placeholder?: string;
  onChange: (value: unknown) => void;
}) {
  const [text, setText] = useState(() => (Array.isArray(value) ? (value as string[]).join("\n") : String(value ?? "")));
  return (
    <Textarea
      id={id}
      rows={3}
      value={text}
      placeholder={placeholder ?? "每行一项"}
      className="bg-background"
      onChange={(event) => {
        setText(event.target.value);
        onChange(event.target.value.split("\n").map((item) => item.trim()).filter(Boolean));
      }}
    />
  );
}
