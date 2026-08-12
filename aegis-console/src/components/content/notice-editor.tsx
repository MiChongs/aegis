"use client";

import { useState } from "react";
import { z } from "zod";
import { toast } from "sonner";
import { Pin } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { NoticeItem } from "@/lib/api/types";
import { useCreateAdminNoticeMutation, useUpdateAdminNoticeMutation } from "@/lib/content-hooks";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RichEditor } from "@/components/ui/rich-editor";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { NOTICE_LEVELS, NOTICE_TYPES, fromLocalInput, noticeLevel, toLocalInput } from "./content-shared";

/**
 * 公告编辑抽屉。
 *
 * 底部有两个提交动作：**存草稿**与**发布**。做成一个「状态」下拉再加一个
 * 保存按钮的话，「写完顺手发出去」这件最常见的事要点三下，而且很容易
 * 在没改状态的情况下以为自己发布了。
 */

const schema = z
  .object({
    title: z.string().trim().min(1, "填写标题"),
    content: z.string(),
    type: z.string(),
    level: z.string(),
    pinned: z.boolean(),
    startTime: z.string(),
    endTime: z.string()
  })
  .refine((form) => stripHtml(form.content).length > 0, {
    path: ["content"],
    message: "填写正文"
  })
  .refine(
    (form) => {
      if (!form.startTime || !form.endTime) return true;
      return new Date(form.endTime).getTime() > new Date(form.startTime).getTime();
    },
    { path: ["endTime"], message: "结束时间需晚于开始时间" }
  );

type NoticeForm = z.infer<typeof schema>;

/**
 * 富文本编辑器在用户清空之后留下的是 `<p></p>` —— 长度不为 0，一个字没有。
 * 只判空串会让这种公告提交上去，然后被服务端以同样的理由拒掉，
 * 而错误信息要绕一圈网络才回来。
 */
function stripHtml(html: string) {
  return html
    .replace(/<[^>]*>/g, "")
    .replace(/&nbsp;/g, " ")
    .trim();
}

const emptyForm: NoticeForm = {
  title: "",
  content: "",
  type: "notice",
  level: "normal",
  pinned: false,
  startTime: "",
  endTime: ""
};

function toForm(item: NoticeItem): NoticeForm {
  return {
    title: item.title ?? "",
    content: item.content ?? "",
    type: item.type ?? "notice",
    level: item.level ?? "normal",
    pinned: item.pinned === true,
    startTime: toLocalInput(item.startTime),
    endTime: toLocalInput(item.endTime)
  };
}

export function NoticeEditor({
  appId,
  item,
  open,
  onOpenChange
}: {
  appId?: number | null;
  item?: NoticeItem | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [form, setForm] = useState<NoticeForm>(() => (item ? toForm(item) : emptyForm));
  const [errors, setErrors] = useState<Partial<Record<keyof NoticeForm, string>>>({});

  const createMutation = useCreateAdminNoticeMutation(appId);
  const updateMutation = useUpdateAdminNoticeMutation(appId);
  const busy = createMutation.isPending || updateMutation.isPending;

  const published = item?.status === "published";

  function patch<K extends keyof NoticeForm>(key: K, value: NoticeForm[K]) {
    setForm((state) => ({ ...state, [key]: value }));
    setErrors((state) => ({ ...state, [key]: undefined }));
  }

  async function submit(status: "draft" | "published") {
    const parsed = schema.safeParse(form);
    if (!parsed.success) {
      const next: Partial<Record<keyof NoticeForm, string>> = {};
      for (const issue of parsed.error.issues) {
        const key = issue.path[0] as keyof NoticeForm;
        next[key] = next[key] ?? issue.message;
      }
      setErrors(next);
      return;
    }

    const payload = {
      title: parsed.data.title,
      content: parsed.data.content,
      type: parsed.data.type,
      level: parsed.data.level,
      pinned: parsed.data.pinned,
      status,
      startTime: fromLocalInput(parsed.data.startTime),
      endTime: fromLocalInput(parsed.data.endTime)
    };

    try {
      if (item) {
        await updateMutation.mutateAsync({ noticeId: item.id, data: payload });
        toast.success(status === "published" ? "已发布" : "已存为草稿");
      } else {
        await createMutation.mutateAsync(payload);
        toast.success(status === "published" ? "已发布" : "已存为草稿");
      }
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[92vw] max-w-3xl flex-col p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{item ? "编辑公告" : "新建公告"}</SheetTitle>
          <SheetDescription>
            {published ? "已发布的公告，保存后立即对客户端生效" : "草稿不会下发到客户端"}
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className="flex-1">
          <div className="space-y-5 px-6 py-5">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">类型</Label>
                <Select value={form.type} onValueChange={(value) => patch("type", value)}>
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {NOTICE_TYPES.map((option) => {
                      const Icon = option.icon;
                      return (
                        <SelectItem key={option.value} value={option.value}>
                          <span className="flex items-center gap-2">
                            <Icon className="size-3.5 text-muted-foreground" />
                            {option.label}
                          </span>
                        </SelectItem>
                      );
                    })}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">级别</Label>
                <Select value={form.level} onValueChange={(value) => patch("level", value)}>
                  <SelectTrigger className="h-9">
                    <span className="flex items-center gap-2">
                      <span className={cn("size-2 rounded-full", noticeLevel(form.level).dot)} />
                      <SelectValue />
                    </span>
                  </SelectTrigger>
                  <SelectContent>
                    {NOTICE_LEVELS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        <span className="flex items-center gap-2">
                          <span className={cn("size-2 rounded-full", option.dot)} />
                          {option.label}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs">标题</Label>
              <Input
                className="h-10 text-base font-medium"
                value={form.title}
                onChange={(event) => patch("title", event.target.value)}
                placeholder="一句话说清这条公告讲什么"
              />
              {errors.title ? <p className="text-xs text-destructive">{errors.title}</p> : null}
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs">正文</Label>
              <RichEditor
                value={form.content}
                onChange={(html) => patch("content", html)}
                placeholder="支持标题、列表、引用、链接，可全屏编辑"
                fullscreenable
              />
              {errors.content ? <p className="text-xs text-destructive">{errors.content}</p> : null}
              <p className="text-[11px] text-muted-foreground">
                保存时服务端会净化脚本与内联样式，并提取纯文本摘要供列表与推送使用
              </p>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">开始时间</Label>
                <Input
                  className="h-9"
                  type="datetime-local"
                  value={form.startTime}
                  onChange={(event) => patch("startTime", event.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">结束时间</Label>
                <Input
                  className="h-9"
                  type="datetime-local"
                  value={form.endTime}
                  onChange={(event) => patch("endTime", event.target.value)}
                />
                {errors.endTime ? <p className="text-xs text-destructive">{errors.endTime}</p> : null}
              </div>
            </div>

            <div className="flex items-center gap-2 rounded-lg border px-3 py-2.5">
              <Pin className="size-3.5 text-muted-foreground" />
              <div className="flex-1">
                <div className="text-sm">置顶</div>
                <div className="text-[11px] text-muted-foreground">排在其它公告之前</div>
              </div>
              <Switch checked={form.pinned} onCheckedChange={(value) => patch("pinned", value)} />
            </div>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button variant="outline" size="sm" onClick={() => void submit("draft")} disabled={busy}>
            存草稿
          </Button>
          <Button size="sm" onClick={() => void submit("published")} disabled={busy}>
            {busy ? "提交中" : published ? "保存并生效" : "发布"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
