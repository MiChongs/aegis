"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Braces, Eye, FileText, Loader2, Pencil, Plus, Save, Trash2 } from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { formatRelative } from "@/lib/datetime";
import { useDebouncedValue } from "@/lib/use-debounced-value";
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
  useDeleteNotifyTemplateMutation,
  useNotifyCatalogQuery,
  useNotifyTemplatesQuery,
  usePreviewNotifyTemplateMutation,
  useSaveNotifyTemplateMutation
} from "@/lib/ticket-hooks";
import type { NotifyTemplate } from "@/lib/api/notify-channels";
import { cn } from "@/lib/utils";
import { ChannelMark, SAMPLE_VARS, SectionEmpty, SectionError, SectionHeader, TEMPLATE_VARS, reportError, useConfirm } from "./notify-kit";

// 模板：Go text/template 语法。编辑时左边写、右边按示例数据实时渲染，
// 变量点一下插到光标处（标题框或正文框，取最后聚焦的那个）。

type TemplateForm = {
  key: string;
  name: string;
  eventKey: string;
  channelKind: string;
  titleTemplate: string;
  bodyTemplate: string;
  enabled: boolean;
};

const DEFAULT_TITLE = "【{{.StatusLabel}}】{{.TicketNo}} {{.TicketTitle}}";
const DEFAULT_BODY = "提单人：{{.Requester}}\n优先级：{{.PriorityText}}\n受理人：{{.Assignee}}";

export function TemplatesSection({ canWrite }: { canWrite: boolean }) {
  const catalogQuery = useNotifyCatalogQuery();
  const templatesQuery = useNotifyTemplatesQuery(0);
  const deleteMut = useDeleteNotifyTemplateMutation();
  const { confirm, dialog: confirmDialog } = useConfirm();

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<NotifyTemplate | null>(null);
  const [initial, setInitial] = useState<TemplateForm | null>(null);

  const events = useMemo(() => catalogQuery.data?.events ?? [], [catalogQuery.data]);
  const kinds = useMemo(() => catalogQuery.data?.kinds ?? [], [catalogQuery.data]);
  const templates = templatesQuery.data ?? [];
  const eventName = useMemo(() => new Map(events.map((item) => [item.key, item.name])), [events]);
  const kindName = useMemo(() => new Map(kinds.map((item) => [item.kind as string, item.name])), [kinds]);

  const openCreate = () => {
    setEditing(null);
    setInitial({
      key: "",
      name: "",
      eventKey: events[0]?.key ?? "",
      channelKind: "all",
      titleTemplate: DEFAULT_TITLE,
      bodyTemplate: DEFAULT_BODY,
      enabled: true
    });
    setOpen(true);
  };

  const openEdit = (template: NotifyTemplate) => {
    setEditing(template);
    setInitial({
      key: template.key,
      name: template.name,
      eventKey: template.eventKey,
      channelKind: template.channelKind || "all",
      titleTemplate: template.titleTemplate,
      bodyTemplate: template.bodyTemplate,
      enabled: template.enabled
    });
    setOpen(true);
  };

  const askDelete = (template: NotifyTemplate) =>
    confirm({
      title: `删除模板「${template.name}」？`,
      description: "使用它的订阅会回到默认渲染。",
      action: "删除",
      run: async () => {
        try {
          await deleteMut.mutateAsync(template.id);
          notify.success("模板已删除");
        } catch (error) {
          reportError(error, "删除失败");
        }
      }
    });

  return (
    <div className="space-y-4">
      <SectionHeader title="模板" description="自定义某个事件在渠道里的标题与正文；没有模板的订阅使用默认渲染。">
        {canWrite ? (
          <Button size="sm" onClick={openCreate}>
            <Plus />
            新建模板
          </Button>
        ) : null}
      </SectionHeader>

      {templatesQuery.isPending ? (
        <div className="grid gap-3 md:grid-cols-2">
          {Array.from({ length: 4 }, (_, index) => (
            <Card key={index} className="gap-3 p-4">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-16 w-full" />
            </Card>
          ))}
        </div>
      ) : templatesQuery.isError ? (
        <SectionError error={templatesQuery.error} onRetry={() => templatesQuery.refetch()} />
      ) : templates.length === 0 ? (
        <SectionEmpty
          icon={FileText}
          title="还没有自定义模板"
          description="默认渲染已包含编号、标题、状态与链接，有特殊格式要求时再建模板。"
          action={
            canWrite ? (
              <Button size="sm" onClick={openCreate}>
                <Plus />
                新建模板
              </Button>
            ) : null
          }
        />
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {templates.map((template) => (
            <Card key={template.id} className={cn("gap-0 overflow-hidden p-0", !template.enabled && "opacity-70")}>
              <div className="flex items-start gap-3 p-4 pb-3">
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                    <span className="truncate text-sm font-semibold text-foreground">{template.name}</span>
                    {!template.enabled ? (
                      <Badge variant="secondary" size="sm">
                        已停用
                      </Badge>
                    ) : null}
                  </div>
                  <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
                    <Badge variant="outline" size="sm">
                      {eventName.get(template.eventKey) ?? template.eventKey}
                    </Badge>
                    <Badge variant="outline" size="sm">
                      {template.channelKind ? (
                        <>
                          <ChannelMark kind={template.channelKind} size="sm" className="-ml-1 size-4 rounded" />
                          {kindName.get(template.channelKind) ?? template.channelKind}
                        </>
                      ) : (
                        "全部渠道"
                      )}
                    </Badge>
                    <span className="font-mono">{template.key}</span>
                  </div>
                </div>
                {canWrite ? (
                  <div className="flex gap-0.5">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button size="icon-sm" variant="ghost" onClick={() => openEdit(template)} aria-label="编辑模板">
                          <Pencil />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>编辑</TooltipContent>
                    </Tooltip>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          className="text-destructive"
                          onClick={() => askDelete(template)}
                          aria-label="删除模板"
                        >
                          <Trash2 />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>删除</TooltipContent>
                    </Tooltip>
                  </div>
                ) : null}
              </div>
              <div className="mx-4 mb-4 space-y-1 rounded-lg border bg-muted/30 p-3 font-mono text-[11px] leading-5">
                <p className="truncate text-foreground">{template.titleTemplate || "（无标题模板）"}</p>
                <p className="line-clamp-3 whitespace-pre-wrap text-muted-foreground">{template.bodyTemplate}</p>
              </div>
              <p className="border-t px-4 py-2 text-[11px] text-muted-foreground">更新于 {formatRelative(template.updatedAt)}</p>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑模板" : "新建模板"}</DialogTitle>
            <DialogDescription>使用 Go 模板语法，变量写作 {"{{.变量名}}"}。右侧按示例数据实时预览。</DialogDescription>
          </DialogHeader>
          {initial ? (
            <TemplateEditor
              key={editing?.id ?? "new"}
              initial={initial}
              editing={editing}
              events={events}
              kinds={kinds}
              onDone={() => setOpen(false)}
            />
          ) : null}
        </DialogContent>
      </Dialog>

      {confirmDialog}
    </div>
  );
}

function TemplateEditor({
  initial,
  editing,
  events,
  kinds,
  onDone
}: {
  initial: TemplateForm;
  editing: NotifyTemplate | null;
  events: { key: string; name: string; group: string }[];
  kinds: { kind: string; name: string }[];
  onDone: () => void;
}) {
  const saveMut = useSaveNotifyTemplateMutation();
  const previewMut = usePreviewNotifyTemplateMutation();
  const [form, setForm] = useState<TemplateForm>(initial);
  const titleRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const lastFocus = useRef<"title" | "body">("body");

  // 实时预览：停止输入 400ms 后渲染一次
  const draft = useDebouncedValue(`${form.titleTemplate}\u0000${form.bodyTemplate}`, 400);
  const { mutate: runPreview } = previewMut;
  useEffect(() => {
    const [titleTemplate, bodyTemplate] = draft.split("\u0000");
    if (!titleTemplate && !bodyTemplate) return;
    runPreview({ titleTemplate, bodyTemplate, vars: SAMPLE_VARS });
  }, [draft, runPreview]);

  const insertVar = (name: string) => {
    const token = `{{.${name}}}`;
    const target = lastFocus.current;
    const el = target === "title" ? titleRef.current : bodyRef.current;
    const field = target === "title" ? "titleTemplate" : "bodyTemplate";
    const current = form[field];
    const start = el?.selectionStart ?? current.length;
    const end = el?.selectionEnd ?? current.length;
    const next = current.slice(0, start) + token + current.slice(end);
    setForm((prev) => ({ ...prev, [field]: next }));
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(start + token.length, start + token.length);
    });
  };

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
      onDone();
    } catch (error) {
      reportError(error, "保存失败");
    }
  };

  const preview = previewMut.data;
  const previewError = previewMut.error;

  return (
    <>
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,20rem)]">
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="notify-tpl-key">标识</Label>
              <Input
                id="notify-tpl-key"
                className="font-mono"
                value={form.key}
                onChange={(event) => setForm((prev) => ({ ...prev, key: event.target.value }))}
                placeholder="ticket-created-feishu"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="notify-tpl-name">名称</Label>
              <Input
                id="notify-tpl-name"
                value={form.name}
                onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="工单创建 · 飞书"
              />
            </div>
            <div className="space-y-1.5">
              <Label>绑定事件</Label>
              <Select value={form.eventKey} onValueChange={(value) => setForm((prev) => ({ ...prev, eventKey: value }))}>
                <SelectTrigger className="w-full">
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
            <div className="space-y-1.5">
              <Label>适用渠道类型</Label>
              <Select value={form.channelKind} onValueChange={(value) => setForm((prev) => ({ ...prev, channelKind: value }))}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部渠道</SelectItem>
                  {kinds.map((kind) => (
                    <SelectItem key={kind.kind} value={kind.kind}>
                      <ChannelMark kind={kind.kind} size="sm" />
                      {kind.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="notify-tpl-title">标题模板</Label>
            <Input
              id="notify-tpl-title"
              ref={titleRef}
              className="font-mono text-xs"
              value={form.titleTemplate}
              onFocus={() => {
                lastFocus.current = "title";
              }}
              onChange={(event) => setForm((prev) => ({ ...prev, titleTemplate: event.target.value }))}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="notify-tpl-body">正文模板</Label>
            <Textarea
              id="notify-tpl-body"
              ref={bodyRef}
              rows={7}
              className="font-mono text-xs leading-5"
              value={form.bodyTemplate}
              onFocus={() => {
                lastFocus.current = "body";
              }}
              onChange={(event) => setForm((prev) => ({ ...prev, bodyTemplate: event.target.value }))}
            />
          </div>

          <div className="space-y-2 rounded-xl border bg-muted/20 p-3">
            <p className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
              <Braces className="size-3.5" />
              可用变量（点击插入到光标处）
            </p>
            <div className="flex flex-wrap gap-1.5">
              {TEMPLATE_VARS.map((variable) => (
                <Tooltip key={variable.key}>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => insertVar(variable.key)}
                      className="inline-flex items-center gap-1 rounded-md border bg-background px-1.5 py-0.5 text-[11px] transition-colors hover:border-primary/50 hover:bg-primary/5"
                    >
                      <span className="text-foreground">{variable.label}</span>
                      <span className="font-mono text-[10px] text-muted-foreground">.{variable.key}</span>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>示例：{variable.sample}</TooltipContent>
                </Tooltip>
              ))}
            </div>
          </div>
        </div>

        <div className="space-y-2">
          <p className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <Eye className="size-3.5" />
            预览
            {previewMut.isPending ? <Loader2 className="size-3 animate-spin" /> : null}
          </p>
          <div className="rounded-xl border bg-muted/30 p-3">
            <div className="flex items-start gap-2.5">
              <ChannelMark kind={form.channelKind === "all" ? "feishu_bot" : form.channelKind} />
              <div className="min-w-0 flex-1 space-y-1.5 rounded-lg border bg-background p-3 shadow-sm">
                {previewError ? (
                  <p className="whitespace-pre-wrap font-mono text-[11px] leading-5 text-red-600 dark:text-red-400">
                    {previewError instanceof ApiError ? previewError.message : "模板渲染失败"}
                  </p>
                ) : preview ? (
                  <>
                    <p className="text-sm font-semibold leading-5 text-foreground">{preview.title || "（无标题）"}</p>
                    <p className="whitespace-pre-wrap text-xs leading-5 text-muted-foreground">{preview.body}</p>
                  </>
                ) : (
                  <div className="space-y-1.5">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-3 w-full" />
                    <Skeleton className="h-3 w-2/3" />
                  </div>
                )}
              </div>
            </div>
          </div>
          <p className="text-[11px] leading-4 text-muted-foreground">示例数据仅用于预览，实际投递使用事件里的真实值。</p>
        </div>
      </div>

      <DialogFooter className="items-center">
        <label className="mr-auto flex items-center gap-2 text-sm">
          <Switch checked={form.enabled} onCheckedChange={(value) => setForm((prev) => ({ ...prev, enabled: value }))} />
          启用
        </label>
        <Button variant="ghost" onClick={onDone}>
          取消
        </Button>
        <Button disabled={saveMut.isPending} onClick={handleSave}>
          {saveMut.isPending ? <Loader2 className="animate-spin" /> : <Save />}
          保存
        </Button>
      </DialogFooter>
    </>
  );
}
