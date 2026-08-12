"use client";

import { useMemo, useState } from "react";
import { z } from "zod";
import { toast } from "sonner";
import { ExternalLink } from "lucide-react";
import { ApiError } from "@/lib/api-client";
import type { BannerItem } from "@/lib/api/types";
import {
  useCreateAdminBannerMutation,
  useUpdateAdminBannerMutation,
  useUploadBannerImageMutation
} from "@/lib/content-hooks";
import { Button } from "@/components/ui/button";
import { ImageDropzone } from "@/components/ui/image-dropzone";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { BANNER_SLOTS, bannerSlot, fromLocalInput, toLocalInput } from "./content-shared";

/**
 * Banner 编辑抽屉。
 *
 * 图片走 `ImageDropzone`：上传成功后表单里存的是后端返回的 `reference`
 * （`storage://…`），预览用的是同一次返回的带票据 URL。两者分开是必须的 ——
 * 票据会过期，把预览地址存进库里过两天就是死链。
 */

const schema = z
  .object({
    title: z.string().trim().min(1, "填写标题"),
    header: z.string().trim(),
    content: z.string().trim(),
    url: z.string().trim(),
    type: z.string(),
    status: z.boolean(),
    startTime: z.string(),
    endTime: z.string()
  })
  .refine((form) => !form.url || /^(https?:\/\/|\/)/i.test(form.url), {
    path: ["url"],
    message: "跳转地址需以 http(s):// 或 / 开头"
  })
  .refine(
    (form) => {
      if (!form.startTime || !form.endTime) return true;
      return new Date(form.endTime).getTime() > new Date(form.startTime).getTime();
    },
    { path: ["endTime"], message: "结束时间需晚于开始时间" }
  );

type BannerForm = z.infer<typeof schema>;

const emptyForm: BannerForm = {
  title: "",
  header: "",
  content: "",
  url: "",
  type: "hero",
  status: true,
  startTime: "",
  endTime: ""
};

function toForm(item: BannerItem): BannerForm {
  return {
    title: item.title ?? "",
    header: item.header ?? "",
    content: item.content ?? "",
    url: item.url ?? "",
    type: item.type ?? "hero",
    status: item.status !== false,
    startTime: toLocalInput(item.startTime),
    endTime: toLocalInput(item.endTime)
  };
}

export function BannerEditor({
  appId,
  item,
  open,
  onOpenChange
}: {
  appId?: number | null;
  item?: BannerItem | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [form, setForm] = useState<BannerForm>(() => (item ? toForm(item) : emptyForm));
  // 预览地址与落库地址分开：前者可能是带票据的临时 URL
  const [preview, setPreview] = useState(() => item?.headerDisplayUrl || item?.header || "");
  const [errors, setErrors] = useState<Partial<Record<keyof BannerForm, string>>>({});

  const createMutation = useCreateAdminBannerMutation(appId);
  const updateMutation = useUpdateAdminBannerMutation(appId);
  const uploadMutation = useUploadBannerImageMutation(appId);
  const busy = createMutation.isPending || updateMutation.isPending;

  const slot = useMemo(() => bannerSlot(form.type), [form.type]);

  function patch<K extends keyof BannerForm>(key: K, value: BannerForm[K]) {
    setForm((state) => ({ ...state, [key]: value }));
    setErrors((state) => ({ ...state, [key]: undefined }));
  }

  async function submit() {
    const parsed = schema.safeParse(form);
    if (!parsed.success) {
      const next: Partial<Record<keyof BannerForm, string>> = {};
      for (const issue of parsed.error.issues) {
        const key = issue.path[0] as keyof BannerForm;
        next[key] = next[key] ?? issue.message;
      }
      setErrors(next);
      return;
    }

    const payload = {
      title: parsed.data.title,
      header: parsed.data.header,
      content: parsed.data.content,
      url: parsed.data.url,
      type: parsed.data.type,
      status: parsed.data.status,
      startTime: fromLocalInput(parsed.data.startTime),
      endTime: fromLocalInput(parsed.data.endTime)
    };

    try {
      if (item) {
        await updateMutation.mutateAsync({ bannerId: item.id, data: payload });
        toast.success("已保存");
      } else {
        await createMutation.mutateAsync(payload);
        toast.success("已创建");
      }
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[92vw] max-w-xl flex-col p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{item ? "编辑 Banner" : "新建 Banner"}</SheetTitle>
          <SheetDescription>{slot.label} · {slot.hint}</SheetDescription>
        </SheetHeader>

        <ScrollArea className="flex-1">
          <div className="space-y-5 px-6 py-5">
            <div className="space-y-2">
              <Label className="text-xs">素材</Label>
              <ImageDropzone
                value={preview}
                aspect={slot.aspect}
                description="JPG / PNG / GIF / WEBP / SVG，10 MB 以内"
                onUpload={async (file) => {
                  const result = await uploadMutation.mutateAsync(file);
                  return { url: result.url, reference: result.reference };
                }}
                onChange={(result) => {
                  // 上传返回 reference 时存它；手动粘贴外链时存那条链接本身
                  const reference = typeof result.reference === "string" ? result.reference : result.url;
                  patch("header", reference);
                  setPreview(result.url);
                }}
              />
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">展示位</Label>
                <Select value={form.type} onValueChange={(value) => patch("type", value)}>
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {BANNER_SLOTS.map((option) => {
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
                <Label className="text-xs">投放开关</Label>
                <div className="flex h-9 items-center gap-2 rounded-md border px-3">
                  <Switch checked={form.status} onCheckedChange={(value) => patch("status", value)} />
                  <span className="text-sm text-muted-foreground">{form.status ? "启用" : "停用"}</span>
                </div>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs">标题</Label>
              <Input
                className="h-9"
                value={form.title}
                onChange={(event) => patch("title", event.target.value)}
                placeholder="展示在素材上的主文案"
              />
              {errors.title ? <p className="text-xs text-destructive">{errors.title}</p> : null}
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs">副文案</Label>
              <Textarea
                rows={2}
                value={form.content}
                onChange={(event) => patch("content", event.target.value)}
                placeholder="一行说明，可留空"
              />
            </div>

            <div className="space-y-1.5">
              <Label className="text-xs">跳转地址</Label>
              <div className="relative">
                <ExternalLink className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  className="h-9 pl-8"
                  value={form.url}
                  onChange={(event) => patch("url", event.target.value)}
                  placeholder="https://  或应用内路径 /activity/618"
                />
              </div>
              {errors.url ? <p className="text-xs text-destructive">{errors.url}</p> : null}
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
            <p className="text-[11px] text-muted-foreground">两端留空即长期投放</p>
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" onClick={submit} disabled={busy}>
            {busy ? "保存中" : item ? "保存" : "创建"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
