"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  BellRing,
  CheckCircle2,
  ExternalLink,
  Plus,
  RefreshCw,
  Save,
  Send,
  Trash2,
  XCircle
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useIsSuperAdmin } from "@/lib/permissions";
import {
  useDeleteNotifyChannelMutation,
  useDeleteNotifySubscriptionMutation,
  useDeleteNotifyTemplateMutation,
  useNotifyCatalogQuery,
  useNotifyChannelsQuery,
  useNotifyDeliveriesQuery,
  useNotifyDeliveryStatsQuery,
  useNotifySubscriptionsQuery,
  useNotifyTemplatesQuery,
  usePreviewNotifyTemplateMutation,
  usePurgeNotifyDeliveriesMutation,
  useSaveNotifyChannelMutation,
  useSaveNotifySubscriptionMutation,
  useSaveNotifyTemplateMutation,
  useTestNotifyChannelMutation
} from "@/lib/ticket-hooks";
import type {
  NotifyChannel,
  NotifyChannelKind,
  NotifyChannelKindMeta,
  NotifyConfigField,
  NotifySubscription,
  NotifyTemplate
} from "@/lib/api/notify-channels";
import { formatDateTime, formatRelativeTime } from "./ticket-shared";

// 统一通知出口控制台。
//
// 这一屏就是「封装统一入口」的落点：业务侧只发事件 key，
// 管理员在这里决定「哪个事件 → 投到哪个渠道 → 用什么模板 → 什么条件下静默」。
// 渠道配置表单完全由后端 /catalog 的元数据驱动，新增一种 IM 时前端零改动。

function reportError(error: unknown, fallback: string) {
  notify.error(error instanceof ApiError ? error.message : fallback);
}

export function NotifyCenterPanel() {
  const isSuperAdmin = useIsSuperAdmin();

  return (
    <Tabs defaultValue="channels" className="space-y-4">
      <TabsList className="flex-wrap">
        <TabsTrigger value="channels">渠道</TabsTrigger>
        <TabsTrigger value="subscriptions">事件订阅</TabsTrigger>
        <TabsTrigger value="templates">模板</TabsTrigger>
        <TabsTrigger value="deliveries">投递记录</TabsTrigger>
      </TabsList>
      <TabsContent value="channels">
        <ChannelsSection canWrite={isSuperAdmin} />
      </TabsContent>
      <TabsContent value="subscriptions">
        <SubscriptionsSection canWrite={isSuperAdmin} />
      </TabsContent>
      <TabsContent value="templates">
        <TemplatesSection canWrite={isSuperAdmin} />
      </TabsContent>
      <TabsContent value="deliveries">
        <DeliveriesSection canWrite={isSuperAdmin} />
      </TabsContent>
    </Tabs>
  );
}

// ─────────────── 渠道 ───────────────

function ChannelsSection({ canWrite }: { canWrite: boolean }) {
  const catalogQuery = useNotifyCatalogQuery();
  const channelsQuery = useNotifyChannelsQuery();
  const saveMut = useSaveNotifyChannelMutation();
  const deleteMut = useDeleteNotifyChannelMutation();
  const testMut = useTestNotifyChannelMutation();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<NotifyChannel | null>(null);
  const [kind, setKind] = useState<NotifyChannelKind>("feishu_bot");
  const [form, setForm] = useState({ key: "", name: "", enabled: true, rateLimit: 0, secret: "" });
  const [config, setConfig] = useState<Record<string, unknown>>({});

  // 单独 memo 一次，避免 ?? [] 每次渲染都产生新数组把下面的 useMemo 打穿
  const kinds = useMemo(() => catalogQuery.data?.kinds ?? [], [catalogQuery.data]);
  const meta = useMemo<NotifyChannelKindMeta | undefined>(
    () => kinds.find((item) => item.kind === kind),
    [kinds, kind]
  );

  const openCreate = () => {
    setEditing(null);
    setKind("feishu_bot");
    setForm({ key: "", name: "", enabled: true, rateLimit: 0, secret: "" });
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

  const handleTest = async (channel: NotifyChannel) => {
    try {
      const result = await testMut.mutateAsync(channel.id);
      // 耗时缺失时不要拼出「undefinedms」——后端字段名变更这类问题
      // 不该以一句看不懂的提示暴露给使用者
      const latency = Number(result?.latencyMs);
      const title = Number.isFinite(latency) ? `测试消息已发送（${latency}ms）` : "测试消息已发送";
      notify.success(title, {
        description: result?.responseSnippet?.slice(0, 120) || "请到对应群聊或收件箱确认是否收到"
      });
    } catch (error) {
      reportError(error, "测试发送失败");
    }
  };

  const channels = channelsQuery.data ?? [];

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">
          一行 = 一个可投递的目标。飞书 / 钉钉 / 企微 / Slack / Webhook / 邮件 / 站内信 / 实时推送共用同一套出口
        </p>
        {canWrite ? (
          <Button size="sm" onClick={openCreate}>
            <Plus className="mr-1 size-3.5" />
            新建渠道
          </Button>
        ) : null}
      </div>

      {!canWrite ? (
        <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
          渠道中存有 IM 凭据，属平台级敏感资源：只有超级管理员可以新建、修改与测试发送。
        </p>
      ) : null}

      {channels.length === 0 ? (
        <EmptyState
          title="尚未配置通知渠道"
          description="新建一个飞书群机器人，工单创建 / SLA 超时等事件就会自动推送到群里。"
        />
      ) : (
        <div className="grid gap-2 md:grid-cols-2">
          {channels.map((channel) => {
            const kindMeta = kinds.find((item) => item.kind === channel.kind);
            return (
              <Card key={channel.id}>
                <CardContent className="space-y-2 p-4">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-sm font-medium text-foreground">{channel.name}</span>
                        <Badge variant="outline" size="sm">
                          {kindMeta?.name || channel.kind}
                        </Badge>
                        {channel.enabled ? null : (
                          <Badge variant="danger" size="sm">
                            已停用
                          </Badge>
                        )}
                        {channel.secretSet ? (
                          <Badge variant="secondary" size="sm">
                            已配置密钥 {channel.secretHint}
                          </Badge>
                        ) : null}
                      </div>
                      <p className="truncate font-mono text-[11px] text-muted-foreground">{channel.key}</p>
                    </div>
                    <StatusDot status={channel.lastStatus} error={channel.lastError} />
                  </div>

                  <div className="flex flex-wrap items-center gap-1 text-[11px] text-muted-foreground">
                    <span>订阅 {channel.subscriptions?.length ?? 0} 个事件</span>
                    {channel.rateLimitPerMinute > 0 ? (
                      <>
                        <span>·</span>
                        <span>限流 {channel.rateLimitPerMinute}/分钟</span>
                      </>
                    ) : null}
                    {channel.lastSentAt ? (
                      <>
                        <span>·</span>
                        <span>最近投递 {formatRelativeTime(channel.lastSentAt)}</span>
                      </>
                    ) : null}
                  </div>

                  {canWrite ? (
                    <div className="flex flex-wrap gap-1">
                      <Button size="sm" variant="outline" onClick={() => handleTest(channel)} disabled={testMut.isPending}>
                        <Send className="mr-1 size-3.5" />
                        测试发送
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => openEdit(channel)}>
                        编辑
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="text-destructive"
                        onClick={async () => {
                          try {
                            await deleteMut.mutateAsync(channel.id);
                            notify.success("渠道已删除");
                          } catch (error) {
                            reportError(error, "删除失败");
                          }
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  ) : null}
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[85vh] max-w-xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑通知渠道" : "新建通知渠道"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label>渠道类型</Label>
              <Select
                value={kind}
                onValueChange={(value) => {
                  setKind(value as NotifyChannelKind);
                  setConfig({});
                }}
                disabled={Boolean(editing)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {kinds.map((item) => (
                    <SelectItem key={item.kind} value={item.kind}>
                      {item.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {meta ? (
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
              ) : null}
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>标识</Label>
                <Input
                  value={form.key}
                  onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
                  placeholder="feishu-ops"
                />
              </div>
              <div className="space-y-1">
                <Label>名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="运维群机器人"
                />
              </div>
            </div>

            {/* 配置项完全由后端元数据驱动 */}
            {meta?.fields.map((field) => (
              <ConfigFieldInput
                key={field.key}
                field={field}
                value={config[field.key]}
                onChange={(value) => setConfig((prev) => ({ ...prev, [field.key]: value }))}
              />
            ))}

            {meta?.needsSecret ? (
              <div className="space-y-1">
                <Label>{meta.secretLabel || "密钥"}</Label>
                <Input
                  type="password"
                  value={form.secret}
                  onChange={(event) => setForm((prev) => ({ ...prev, secret: event.target.value }))}
                  placeholder={editing?.secretSet ? "留空保持不变，填 - 清空" : "请输入密钥"}
                />
                <p className="text-[11px] text-muted-foreground">
                  密钥以 AES-GCM 落库，任何接口都不会回传明文
                </p>
              </div>
            ) : null}

            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2">
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))}
                />
                <Label className="text-xs">启用</Label>
              </div>
              <div className="flex items-center gap-2">
                <Label className="text-xs">每分钟上限</Label>
                <Input
                  type="number"
                  className="h-8 w-24"
                  value={form.rateLimit}
                  onChange={(event) => setForm((prev) => ({ ...prev, rateLimit: Number(event.target.value) }))}
                />
                <span className="text-[11px] text-muted-foreground">0 = 不限</span>
              </div>
            </div>

            <Button className="w-full" disabled={saveMut.isPending} onClick={handleSave}>
              <Save className="mr-1 size-3.5" />
              保存
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function StatusDot({ status, error }: { status?: string; error?: string }) {
  if (!status) {
    return (
      <Badge variant="outline" size="sm">
        未投递过
      </Badge>
    );
  }
  if (status === "success") {
    return (
      <Badge variant="success" size="sm">
        <CheckCircle2 className="mr-1 size-3" />
        正常
      </Badge>
    );
  }
  return (
    <Badge variant="danger" size="sm" title={error}>
      <XCircle className="mr-1 size-3" />
      异常
    </Badge>
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

  const label = (
    <div className="space-y-0.5">
      <Label>
        {field.label}
        {field.required ? <span className="ml-0.5 text-destructive">*</span> : null}
      </Label>
      {field.help ? <p className="text-[11px] text-muted-foreground">{field.help}</p> : null}
    </div>
  );

  if (field.type === "switch") {
    return (
      <div className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
        {label}
        <Switch checked={Boolean(value)} onCheckedChange={(next) => onChange(next)} />
      </div>
    );
  }

  if (field.type === "select") {
    return (
      <div className="space-y-1">
        {label}
        <Select value={String(value ?? field.default ?? "")} onValueChange={(next) => onChange(next)}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(field.options ?? []).map((option) => (
              <SelectItem key={option} value={option}>
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    );
  }

  if (field.type === "textarea" || field.type === "tags") {
    const text = Array.isArray(value) ? (value as string[]).join("\n") : String(value ?? "");
    return (
      <div className="space-y-1">
        {label}
        <Textarea
          rows={3}
          value={text}
          placeholder={field.placeholder}
          onChange={(event) => {
            const raw = event.target.value;
            // tags 类型按行拆成数组，后端 cfgStrings 也能吃字符串，这里统一成数组更直观
            onChange(field.type === "tags" ? raw.split("\n").map((item) => item.trim()).filter(Boolean) : raw);
          }}
        />
      </div>
    );
  }

  return (
    <div className="space-y-1">
      {label}
      <Input
        type={field.type === "password" ? "password" : "text"}
        value={String(value ?? "")}
        placeholder={field.placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  );
}

// ─────────────── 事件订阅 ───────────────

function SubscriptionsSection({ canWrite }: { canWrite: boolean }) {
  const catalogQuery = useNotifyCatalogQuery();
  const channelsQuery = useNotifyChannelsQuery();
  const subsQuery = useNotifySubscriptionsQuery();
  const templatesQuery = useNotifyTemplatesQuery(0);
  const saveMut = useSaveNotifySubscriptionMutation();
  const deleteMut = useDeleteNotifySubscriptionMutation();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<NotifySubscription | null>(null);
  const [form, setForm] = useState({
    channelId: "",
    eventKey: "",
    minPriority: "any",
    templateId: "none",
    quietEnabled: false,
    quietStart: "23:00",
    quietEnd: "08:00",
    timezone: "Asia/Shanghai",
    enabled: true
  });

  const events = catalogQuery.data?.events ?? [];
  const channels = channelsQuery.data ?? [];
  const subs = subsQuery.data ?? [];
  const templates = templatesQuery.data ?? [];

  const handleSave = async () => {
    if (!form.channelId || !form.eventKey) {
      notify.warning("请选择渠道与事件");
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
          quietHours: form.quietEnabled
            ? { timezone: form.timezone, start: form.quietStart, end: form.quietEnd }
            : null,
          enabled: form.enabled
        }
      });
      notify.success("订阅已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">
          决定「哪个事件投到哪个渠道」。支持按优先级过滤与静默窗口；critical 事件会穿透静默
        </p>
        {canWrite ? (
          <Button
            size="sm"
            onClick={() => {
              setEditing(null);
              setForm({
                channelId: channels[0] ? String(channels[0].id) : "",
                eventKey: events[0]?.key ?? "",
                minPriority: "any",
                templateId: "none",
                quietEnabled: false,
                quietStart: "23:00",
                quietEnd: "08:00",
                timezone: "Asia/Shanghai",
                enabled: true
              });
              setOpen(true);
            }}
          >
            <Plus className="mr-1 size-3.5" />
            新建订阅
          </Button>
        ) : null}
      </div>

      {subs.length === 0 ? (
        <EmptyState
          title="尚未配置事件订阅"
          description="渠道建好后还需要订阅事件，工单动态才会真正推送出去。"
        />
      ) : (
        <div className="space-y-2">
          {subs.map((sub) => {
            const meta = events.find((item) => item.key === sub.eventKey);
            return (
              <Card key={sub.id}>
                <CardContent className="flex flex-wrap items-center justify-between gap-2 p-3">
                  <div className="min-w-0 space-y-0.5">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <Badge variant="info" size="sm">
                        {meta?.name || sub.eventKey}
                      </Badge>
                      <span className="text-xs text-muted-foreground">→</span>
                      <span className="text-sm font-medium text-foreground">{sub.channelName}</span>
                      <Badge variant="outline" size="sm">
                        {sub.channelKind}
                      </Badge>
                      {!sub.enabled ? (
                        <Badge variant="danger" size="sm">
                          已停用
                        </Badge>
                      ) : null}
                    </div>
                    <p className="font-mono text-[11px] text-muted-foreground">{sub.eventKey}</p>
                    <div className="flex flex-wrap gap-2 text-[11px] text-muted-foreground">
                      {sub.minPriority ? <span>仅 {sub.minPriority} 及以上</span> : <span>不限优先级</span>}
                      {sub.quietHours ? (
                        <span>
                          静默 {sub.quietHours.start}–{sub.quietHours.end}
                        </span>
                      ) : null}
                      {sub.templateId ? <span>使用自定义模板</span> : null}
                    </div>
                  </div>
                  {canWrite ? (
                    <div className="flex gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setEditing(sub);
                          setForm({
                            channelId: String(sub.channelId),
                            eventKey: sub.eventKey,
                            minPriority: sub.minPriority || "any",
                            templateId: sub.templateId ? String(sub.templateId) : "none",
                            quietEnabled: Boolean(sub.quietHours),
                            quietStart: sub.quietHours?.start || "23:00",
                            quietEnd: sub.quietHours?.end || "08:00",
                            timezone: sub.quietHours?.timezone || "Asia/Shanghai",
                            enabled: sub.enabled
                          });
                          setOpen(true);
                        }}
                      >
                        编辑
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="text-destructive"
                        onClick={async () => {
                          try {
                            await deleteMut.mutateAsync(sub.id);
                            notify.success("订阅已删除");
                          } catch (error) {
                            reportError(error, "删除失败");
                          }
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  ) : null}
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑订阅" : "新建订阅"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label>投递渠道</Label>
              <Select value={form.channelId} onValueChange={(value) => setForm((prev) => ({ ...prev, channelId: value }))}>
                <SelectTrigger>
                  <SelectValue placeholder="选择渠道" />
                </SelectTrigger>
                <SelectContent>
                  {channels.map((channel) => (
                    <SelectItem key={channel.id} value={String(channel.id)}>
                      {channel.name}（{channel.kind}）
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label>事件</Label>
              <Select value={form.eventKey} onValueChange={(value) => setForm((prev) => ({ ...prev, eventKey: value }))}>
                <SelectTrigger>
                  <SelectValue placeholder="选择事件" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ticket.*">全部工单事件（ticket.*）</SelectItem>
                  {events.map((event) => (
                    <SelectItem key={event.key} value={event.key}>
                      {event.group} · {event.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>最低优先级</Label>
                <Select
                  value={form.minPriority}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, minPriority: value }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="any">不限</SelectItem>
                    <SelectItem value="low">低及以上</SelectItem>
                    <SelectItem value="normal">中及以上</SelectItem>
                    <SelectItem value="high">高及以上</SelectItem>
                    <SelectItem value="urgent">仅紧急</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label>模板</Label>
                <Select
                  value={form.templateId}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, templateId: value }))}
                >
                  <SelectTrigger>
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
            <div className="space-y-2 rounded-lg border p-3">
              <div className="flex items-center gap-2">
                <Switch
                  checked={form.quietEnabled}
                  onCheckedChange={(value) => setForm((prev) => ({ ...prev, quietEnabled: value }))}
                />
                <Label className="text-xs">启用静默窗口</Label>
              </div>
              {form.quietEnabled ? (
                <div className="grid gap-2 sm:grid-cols-3">
                  <Input
                    value={form.timezone}
                    onChange={(event) => setForm((prev) => ({ ...prev, timezone: event.target.value }))}
                    placeholder="Asia/Shanghai"
                  />
                  <Input
                    value={form.quietStart}
                    onChange={(event) => setForm((prev) => ({ ...prev, quietStart: event.target.value }))}
                    placeholder="23:00"
                  />
                  <Input
                    value={form.quietEnd}
                    onChange={(event) => setForm((prev) => ({ ...prev, quietEnd: event.target.value }))}
                    placeholder="08:00"
                  />
                </div>
              ) : null}
              <p className="text-[11px] text-muted-foreground">
                静默期内普通事件不投递；SLA 超时等 critical 事件仍会穿透送达
              </p>
            </div>
            <div className="flex items-center gap-2">
              <Switch
                checked={form.enabled}
                onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))}
              />
              <Label className="text-xs">启用</Label>
            </div>
            <Button className="w-full" disabled={saveMut.isPending} onClick={handleSave}>
              <Save className="mr-1 size-3.5" />
              保存
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ─────────────── 模板 ───────────────

const TEMPLATE_VARS = [
  "TicketNo",
  "TicketTitle",
  "StatusLabel",
  "PriorityText",
  "Requester",
  "Assignee",
  "Group",
  "Category",
  "AppName",
  "Link",
  "Time"
];

function TemplatesSection({ canWrite }: { canWrite: boolean }) {
  const catalogQuery = useNotifyCatalogQuery();
  const templatesQuery = useNotifyTemplatesQuery(0);
  const saveMut = useSaveNotifyTemplateMutation();
  const deleteMut = useDeleteNotifyTemplateMutation();
  const previewMut = usePreviewNotifyTemplateMutation();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<NotifyTemplate | null>(null);
  const [form, setForm] = useState({
    key: "",
    name: "",
    eventKey: "",
    channelKind: "all",
    titleTemplate: "",
    bodyTemplate: "",
    enabled: true
  });
  const [preview, setPreview] = useState<{ title: string; body: string } | null>(null);

  const events = catalogQuery.data?.events ?? [];
  const kinds = catalogQuery.data?.kinds ?? [];
  const templates = templatesQuery.data ?? [];

  const handleSave = async () => {
    if (!form.key.trim() || !form.name.trim() || !form.eventKey) {
      notify.warning("请填写标识、名称并选择事件");
      return;
    }
    try {
      await saveMut.mutateAsync({
        id: editing?.id,
        payload: {
          appid: 0,
          key: form.key.trim(),
          name: form.name.trim(),
          eventKey: form.eventKey,
          channelKind: form.channelKind === "all" ? "" : form.channelKind,
          titleTemplate: form.titleTemplate,
          bodyTemplate: form.bodyTemplate,
          enabled: form.enabled
        }
      });
      notify.success("模板已保存");
      setOpen(false);
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  const handlePreview = async () => {
    try {
      const result = await previewMut.mutateAsync({
        titleTemplate: form.titleTemplate,
        bodyTemplate: form.bodyTemplate,
        vars: {
          TicketNo: "TK20260810000042",
          TicketTitle: "支付未到账",
          StatusLabel: "处理中",
          PriorityText: "紧急",
          Requester: "张三",
          Assignee: "李四",
          Group: "支付支持组",
          Category: "支付问题",
          AppName: "示例应用",
          Link: "https://console.example.com/tickets?id=42",
          Time: "2026-08-10 15:04:05"
        }
      });
      setPreview(result);
    } catch (error) {
      reportError(error, "预览失败");
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">
          自定义标题与正文，支持 <code className="rounded bg-muted px-1">{"{{.TicketNo}}"}</code> 之类的变量占位
        </p>
        {canWrite ? (
          <Button
            size="sm"
            onClick={() => {
              setEditing(null);
              setForm({
                key: "",
                name: "",
                eventKey: events[0]?.key ?? "",
                channelKind: "all",
                titleTemplate: "【{{.StatusLabel}}】{{.TicketNo}} {{.TicketTitle}}",
                bodyTemplate: "提单人：{{.Requester}}\n优先级：{{.PriorityText}}\n受理人：{{.Assignee}}",
                enabled: true
              });
              setPreview(null);
              setOpen(true);
            }}
          >
            <Plus className="mr-1 size-3.5" />
            新建模板
          </Button>
        ) : null}
      </div>

      {templates.length === 0 ? (
        <EmptyState
          title="暂无自定义模板"
          description="不配模板也能用：出口会按事件自动生成飞书卡片 / Markdown / 邮件排版。"
        />
      ) : (
        <div className="grid gap-2 md:grid-cols-2">
          {templates.map((template) => (
            <Card key={template.id}>
              <CardContent className="space-y-1 p-4">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-sm font-medium text-foreground">{template.name}</span>
                      <Badge variant="outline" size="sm">
                        {template.eventKey}
                      </Badge>
                      {template.channelKind ? (
                        <Badge variant="secondary" size="sm">
                          {template.channelKind}
                        </Badge>
                      ) : null}
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{template.titleTemplate || "（无标题模板）"}</p>
                  </div>
                  {canWrite ? (
                    <div className="flex gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setEditing(template);
                          setForm({
                            key: template.key,
                            name: template.name,
                            eventKey: template.eventKey,
                            channelKind: template.channelKind || "all",
                            titleTemplate: template.titleTemplate,
                            bodyTemplate: template.bodyTemplate,
                            enabled: template.enabled
                          });
                          setPreview(null);
                          setOpen(true);
                        }}
                      >
                        编辑
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="text-destructive"
                        onClick={async () => {
                          try {
                            await deleteMut.mutateAsync(template.id);
                            notify.success("模板已删除");
                          } catch (error) {
                            reportError(error, "删除失败");
                          }
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  ) : null}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑模板" : "新建模板"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>标识</Label>
                <Input
                  value={form.key}
                  onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
                  placeholder="ticket-created-feishu"
                />
              </div>
              <div className="space-y-1">
                <Label>名称</Label>
                <Input
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="工单创建 · 飞书"
                />
              </div>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label>绑定事件</Label>
                <Select value={form.eventKey} onValueChange={(value) => setForm((prev) => ({ ...prev, eventKey: value }))}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择事件" />
                  </SelectTrigger>
                  <SelectContent>
                    {events.map((event) => (
                      <SelectItem key={event.key} value={event.key}>
                        {event.group} · {event.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label>适用渠道类型</Label>
                <Select
                  value={form.channelKind}
                  onValueChange={(value) => setForm((prev) => ({ ...prev, channelKind: value }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部渠道</SelectItem>
                    {kinds.map((kind) => (
                      <SelectItem key={kind.kind} value={kind.kind}>
                        {kind.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-1">
              <Label>标题模板</Label>
              <Input
                value={form.titleTemplate}
                onChange={(event) => setForm((prev) => ({ ...prev, titleTemplate: event.target.value }))}
              />
            </div>
            <div className="space-y-1">
              <Label>正文模板</Label>
              <Textarea
                rows={4}
                value={form.bodyTemplate}
                onChange={(event) => setForm((prev) => ({ ...prev, bodyTemplate: event.target.value }))}
              />
              <div className="flex flex-wrap gap-1 pt-1">
                {TEMPLATE_VARS.map((variable) => (
                  <button
                    key={variable}
                    type="button"
                    className="rounded border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground hover:bg-accent"
                    onClick={() =>
                      setForm((prev) => ({ ...prev, bodyTemplate: `${prev.bodyTemplate}{{.${variable}}}` }))
                    }
                  >
                    {`{{.${variable}}}`}
                  </button>
                ))}
              </div>
            </div>

            {preview ? (
              <div className="space-y-1 rounded-lg border bg-muted/40 p-3">
                <p className="text-xs font-medium text-foreground">{preview.title}</p>
                <p className="whitespace-pre-wrap text-xs text-muted-foreground">{preview.body}</p>
              </div>
            ) : null}

            <div className="flex items-center gap-2">
              <Switch
                checked={form.enabled}
                onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))}
              />
              <Label className="text-xs">启用</Label>
              <Button size="sm" variant="outline" className="ml-auto" onClick={handlePreview}>
                <RefreshCw className="mr-1 size-3.5" />
                预览
              </Button>
              <Button size="sm" disabled={saveMut.isPending} onClick={handleSave}>
                <Save className="mr-1 size-3.5" />
                保存
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ─────────────── 投递记录 ───────────────

function DeliveriesSection({ canWrite }: { canWrite: boolean }) {
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const statsQuery = useNotifyDeliveryStatsQuery(7);
  const listQuery = useNotifyDeliveriesQuery({
    status: status === "all" ? undefined : status,
    page,
    limit: 20
  });
  const purgeMut = usePurgeNotifyDeliveriesMutation();

  const stats = statsQuery.data;
  const page1 = listQuery.data;

  return (
    <div className="space-y-3">
      {stats ? (
        <div className="grid gap-2 sm:grid-cols-4">
          <StatTile label="近 7 天投递" value={String(stats.total)} />
          <StatTile label="成功率" value={`${stats.successPct.toFixed(1)}%`} tone={stats.successPct >= 95 ? "good" : "warn"} />
          <StatTile label="失败" value={String(stats.failed)} tone={stats.failed > 0 ? "warn" : "good"} />
          <StatTile label="平均耗时" value={`${stats.avgLatencyMs} ms`} />
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={status}
          onValueChange={(value) => {
            setStatus(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="success">成功</SelectItem>
            <SelectItem value="failed">失败</SelectItem>
            <SelectItem value="skipped">已跳过</SelectItem>
          </SelectContent>
        </Select>
        <Button size="sm" variant="outline" onClick={() => listQuery.refetch()}>
          <RefreshCw className="mr-1 size-3.5" />
          刷新
        </Button>
        {canWrite ? (
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto text-destructive"
            onClick={async () => {
              try {
                const result = await purgeMut.mutateAsync(30);
                notify.success(`已清理 ${result.deleted} 条 30 天前的记录`);
              } catch (error) {
                reportError(error, "清理失败");
              }
            }}
          >
            <Trash2 className="mr-1 size-3.5" />
            清理 30 天前
          </Button>
        ) : null}
      </div>

      {!page1?.items.length ? (
        <EmptyState title="暂无投递记录" description="工单产生动态后，每一次投递尝试都会记录在这里，便于排障。" />
      ) : (
        <div className="space-y-1">
          {page1.items.map((delivery) => (
            <Card key={delivery.id}>
              <CardContent className="flex flex-wrap items-center gap-2 p-3 text-xs">
                <DeliveryStatusBadge status={delivery.status} />
                <Badge variant="outline" size="sm">
                  {delivery.channelKind}
                </Badge>
                <span className="font-mono text-muted-foreground">{delivery.eventKey}</span>
                <span className="text-foreground">{delivery.channelName || "已删除渠道"}</span>
                {delivery.resourceId ? (
                  <span className="text-muted-foreground">
                    {delivery.resource}#{delivery.resourceId}
                  </span>
                ) : null}
                <span className="ml-auto text-muted-foreground">{delivery.latencyMs} ms</span>
                <span className="text-muted-foreground">{formatDateTime(delivery.createdAt)}</span>
                {delivery.error ? (
                  <p className="w-full truncate text-destructive" title={delivery.error}>
                    {delivery.error}
                  </p>
                ) : null}
              </CardContent>
            </Card>
          ))}

          {page1.totalPages > 1 ? (
            <div className="flex items-center justify-center gap-2 pt-2">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((prev) => prev - 1)}>
                上一页
              </Button>
              <span className="text-xs text-muted-foreground">
                {page1.page} / {page1.totalPages}
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= page1.totalPages}
                onClick={() => setPage((prev) => prev + 1)}
              >
                下一页
              </Button>
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}

function DeliveryStatusBadge({ status }: { status: string }) {
  if (status === "success") {
    return (
      <Badge variant="success" size="sm">
        成功
      </Badge>
    );
  }
  if (status === "failed" || status === "dropped") {
    return (
      <Badge variant="danger" size="sm">
        <AlertTriangle className="mr-1 size-3" />
        失败
      </Badge>
    );
  }
  if (status === "skipped") {
    return (
      <Badge variant="secondary" size="sm">
        跳过
      </Badge>
    );
  }
  return (
    <Badge variant="outline" size="sm">
      <BellRing className="mr-1 size-3" />
      投递中
    </Badge>
  );
}

function StatTile({ label, value, tone }: { label: string; value: string; tone?: "good" | "warn" }) {
  return (
    <Card>
      <CardContent className="space-y-1 p-4">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p
          className={
            tone === "warn"
              ? "text-lg font-semibold text-amber-600 dark:text-amber-400"
              : "text-lg font-semibold text-foreground"
          }
        >
          {value}
        </p>
      </CardContent>
    </Card>
  );
}
