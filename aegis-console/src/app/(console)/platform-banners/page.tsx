"use client";

import { FormEvent, useCallback, useMemo, useState } from "react";
import Link from "next/link";
import {
  CheckCircle2,
  ExternalLink,
  GalleryHorizontalEnd,
  Pencil,
  Plus,
  Search,
  Trash2,
  XCircle
} from "lucide-react";
import { notify } from "@/lib/notify";
import {
  useCreatePlatformBannerMutation,
  useDeletePlatformBannerMutation,
  usePlatformBannersQuery,
  useUpdatePlatformBannerMutation,
  useUploadPlatformBannerImageMutation
} from "@/lib/admin-hooks";
import { RequirePermission } from "@/lib/permissions";
import type { PlatformBannerItem, PlatformBannerMutation, PlatformBannerType } from "@/lib/api/types";
import { SectionHeading } from "@/components/ui/section-heading";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { ImageDropzone } from "@/components/ui/image-dropzone";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { cn } from "@/lib/utils";

const TYPE_OPTIONS: { value: PlatformBannerType; label: string }[] = [
  { value: "info", label: "普通" },
  { value: "notice", label: "公告" },
  { value: "maintenance", label: "维护" },
  { value: "release", label: "版本" },
  { value: "security", label: "安全" }
];

type FormState = {
  title: string;
  description: string;
  /** 持久化形态：`storage://{configId}/{key}` 或外链 URL */
  imageUrl: string;
  /** 预览展示地址：刚上传后是后端返回的 ticket 代理 URL；编辑已有时是 item.imageDisplayUrl */
  imagePreviewUrl: string;
  clickUrl: string;
  type: PlatformBannerType;
  position: number;
  status: boolean;
  startTime: string;
  endTime: string;
};

const emptyForm: FormState = {
  title: "",
  description: "",
  imageUrl: "",
  imagePreviewUrl: "",
  clickUrl: "",
  type: "info",
  position: 0,
  status: true,
  startTime: "",
  endTime: ""
};

function toForm(item: PlatformBannerItem): FormState {
  return {
    title: item.title,
    description: item.description ?? "",
    imageUrl: item.imageUrl,
    imagePreviewUrl: item.imageDisplayUrl || item.imageUrl,
    clickUrl: item.clickUrl ?? "",
    type: item.type,
    position: item.position,
    status: item.status,
    startTime: item.startTime ? item.startTime.slice(0, 16) : "",
    endTime: item.endTime ? item.endTime.slice(0, 16) : ""
  };
}

function toMutation(form: FormState): PlatformBannerMutation {
  return {
    title: form.title.trim(),
    description: form.description.trim(),
    imageUrl: form.imageUrl.trim(),
    clickUrl: form.clickUrl.trim(),
    type: form.type,
    position: Number(form.position) || 0,
    status: form.status,
    startTime: form.startTime ? new Date(form.startTime).toISOString() : null,
    endTime: form.endTime ? new Date(form.endTime).toISOString() : null
  };
}

export default function PlatformBannersPage() {
  return (
    <RequirePermission
      superAdmin
      fallback={
        <EmptyState
          title="需要超级管理员权限"
          description="平台横幅仅限超级管理员管理，请联系超管或切换账号。"
        />
      }
    >
      <WidgetBoundary title="平台横幅加载失败">
        <PlatformBannersInner />
      </WidgetBoundary>
    </RequirePermission>
  );
}

function PlatformBannersInner() {
  const [keyword, setKeyword] = useState("");
  const [typeFilter, setTypeFilter] = useState<string>("all");
  const [statusFilter, setStatusFilter] = useState<"all" | "enabled" | "disabled">("all");
  const [editing, setEditing] = useState<PlatformBannerItem | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [form, setForm] = useState<FormState>(emptyForm);

  const params = useMemo(
    () => ({
      keyword: keyword.trim() || undefined,
      type: typeFilter === "all" ? undefined : typeFilter,
      status: statusFilter === "all" ? undefined : statusFilter === "enabled",
      page: 1,
      limit: 100
    }),
    [keyword, typeFilter, statusFilter]
  );

  const listQuery = usePlatformBannersQuery(params);
  const createMut = useCreatePlatformBannerMutation();
  const updateMut = useUpdatePlatformBannerMutation();
  const deleteMut = useDeletePlatformBannerMutation();
  const uploadMut = useUploadPlatformBannerImageMutation();

  const handleUpload = useCallback(
    async (file: File) => {
      const res = await uploadMut.mutateAsync({ file });
      // url 用于立即预览；reference 挂到 extras，Dropzone 会原样透传给 onChange
      return { url: res.url, reference: res.reference };
    },
    [uploadMut]
  );

  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const hasAnyFilter = Boolean(keyword.trim() || typeFilter !== "all" || statusFilter !== "all");

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm);
    setSheetOpen(true);
  };

  const openEdit = (item: PlatformBannerItem) => {
    setEditing(item);
    setForm(toForm(item));
    setSheetOpen(true);
  };

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!form.title.trim()) {
      notify.warning("请填写标题");
      return;
    }
    if (!form.imageUrl.trim()) {
      notify.warning("请上传或填写图片");
      return;
    }
    if (form.startTime && form.endTime && new Date(form.endTime) < new Date(form.startTime)) {
      notify.warning("结束时间必须晚于开始时间");
      return;
    }
    const payload = toMutation(form);
    try {
      if (editing) {
        await updateMut.mutateAsync({ id: editing.id, data: payload });
        notify.success("已更新");
      } else {
        await createMut.mutateAsync(payload);
        notify.success("已创建");
      }
      setSheetOpen(false);
    } catch (err) {
      notify.errorDetail("保存失败", err);
    }
  };

  const handleDelete = async (item: PlatformBannerItem) => {
    if (!confirm(`确认删除「${item.title}」？此操作不可恢复。`)) return;
    try {
      await deleteMut.mutateAsync(item.id);
      notify.success("已删除");
    } catch (err) {
      notify.errorDetail("删除失败", err);
    }
  };

  const handleQuickToggle = async (item: PlatformBannerItem) => {
    try {
      await updateMut.mutateAsync({ id: item.id, data: { status: !item.status } });
    } catch {
      notify.error("切换状态失败");
    }
  };

  const resetFilters = () => {
    setKeyword("");
    setTypeFilter("all");
    setStatusFilter("all");
  };

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="控制台"
        title="平台横幅"
        action={
          <Button size="sm" onClick={openCreate} className="gap-1.5">
            <Plus className="size-4" /> 新建
          </Button>
        }
      />

      {/* 筛选条：一行扁平布局，移动端自然换行；共计数放在右侧消除空白感 */}
      <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-card px-3 py-2.5">
        <div className="relative min-w-[220px] flex-1">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索标题 / 描述..."
            className="h-8 pl-8 text-xs"
          />
        </div>
        <Select value={typeFilter} onValueChange={setTypeFilter}>
          <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部类型</SelectItem>
            {TYPE_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as typeof statusFilter)}>
          <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="enabled">启用</SelectItem>
            <SelectItem value="disabled">停用</SelectItem>
          </SelectContent>
        </Select>
        {hasAnyFilter ? (
          <Button variant="ghost" size="sm" className="h-8" onClick={resetFilters}>
            重置
          </Button>
        ) : null}
        <div className="flex-1" />
        <span className="text-xs text-muted-foreground whitespace-nowrap">
          共 <span className="font-semibold text-foreground">{total}</span> 条
        </span>
      </div>

      {/* 列表 */}
      {listQuery.isLoading ? (
        <LoadingState title="加载中" description="正在获取平台 Banner" />
      ) : items.length === 0 ? (
        <EmptyBannersState onCreate={openCreate} hasFilter={hasAnyFilter} onReset={resetFilters} />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {items.map((item) => (
            <BannerCard
              key={item.id}
              item={item}
              onEdit={() => openEdit(item)}
              onDelete={() => handleDelete(item)}
              onToggle={() => handleQuickToggle(item)}
              toggling={updateMut.isPending}
            />
          ))}
        </div>
      )}

      {/* 编辑 Sheet —— 自适应宽度：移动端几乎全屏；sm 起限宽；xl 起再宽一档 */}
      <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
        <SheetContent
          side="right"
          className={cn(
            "flex h-full flex-col gap-0 p-0",
            "w-full sm:!w-[92vw] sm:!max-w-xl lg:!max-w-2xl xl:!max-w-3xl"
          )}
        >
          <form onSubmit={handleSubmit} className="flex h-full min-h-0 flex-col">
            <SheetHeader className="space-y-1 border-b px-6 py-5">
              <SheetTitle>{editing ? "编辑平台横幅" : "新建平台横幅"}</SheetTitle>
              <SheetDescription className="text-xs">
                超级管理员维护；Banner 展示于总览页顶部轮播。
              </SheetDescription>
            </SheetHeader>

            {/* 可滚动主体 */}
            <div className="flex-1 overflow-y-auto px-6 py-5">
              <FormSections form={form} setForm={setForm} onUpload={handleUpload} />
            </div>

            {/* 底部操作条，sticky 固定；移动端端按钮铺满 */}
            <div className="flex flex-col-reverse gap-2 border-t bg-background/95 px-6 py-3 backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:flex-row sm:items-center sm:justify-end">
              <Button type="button" variant="outline" onClick={() => setSheetOpen(false)} className="sm:min-w-[88px]">
                取消
              </Button>
              <Button
                type="submit"
                disabled={createMut.isPending || updateMut.isPending || uploadMut.isPending}
                className="sm:min-w-[104px]"
              >
                {editing ? "保存" : "创建"}
              </Button>
            </div>
          </form>
        </SheetContent>
      </Sheet>
    </div>
  );
}

// ────────────────────── 辅助组件 ──────────────────────

function EmptyBannersState({
  onCreate,
  hasFilter,
  onReset
}: {
  onCreate: () => void;
  hasFilter: boolean;
  onReset: () => void;
}) {
  return (
    <div className="flex min-h-[280px] flex-col items-center justify-center gap-3 rounded-xl border border-dashed bg-card px-6 py-12 text-center">
      <div className="grid size-12 place-items-center rounded-full border bg-muted/40 text-muted-foreground">
        <GalleryHorizontalEnd className="size-5" />
      </div>
      <div className="space-y-0.5">
        <h3 className="text-sm font-semibold text-foreground">
          {hasFilter ? "没有符合条件的 Banner" : "暂无平台横幅"}
        </h3>
        <p className="text-xs text-muted-foreground">
          {hasFilter ? "尝试调整筛选条件，或新建一条 Banner。" : "新建第一条后，会立刻出现在所有管理员的总览顶部。"}
        </p>
      </div>
      <div className="flex items-center gap-2 pt-1">
        {hasFilter ? (
          <Button variant="outline" size="sm" onClick={onReset}>
            重置筛选
          </Button>
        ) : null}
        <Button size="sm" onClick={onCreate} className="gap-1.5">
          <Plus className="size-4" /> 新建横幅
        </Button>
      </div>
    </div>
  );
}

function FormSections({
  form,
  setForm,
  onUpload
}: {
  form: FormState;
  setForm: (f: FormState) => void;
  onUpload: (file: File) => Promise<{ url: string; reference: string }>;
}) {
  return (
    <div className="space-y-6">
      {/* ── 图片 ── */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h4 className="text-[11px] font-medium uppercase tracking-widest text-muted-foreground">素材</h4>
          <span className="text-[11px] text-muted-foreground">JPG / PNG / GIF / WEBP / SVG · ≤10MB</span>
        </div>
        <ImageDropzone
          value={form.imagePreviewUrl}
          onChange={(result) => {
            const url = String(result.url ?? "");
            // 上传成功时 extras 里带 reference；手动粘贴 URL / 清除时没有
            const reference = typeof result.reference === "string" && result.reference ? result.reference : url;
            setForm({
              ...form,
              imageUrl: reference,
              imagePreviewUrl: url
            });
          }}
          onUpload={onUpload}
          description="拖放或点击上传到对象存储（OSS/S3/...），也可直接粘贴外链 URL"
        />
      </section>

      <Separator />

      {/* ── 基础信息 ── */}
      <section className="space-y-3">
        <h4 className="text-[11px] font-medium uppercase tracking-widest text-muted-foreground">基础信息</h4>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1 sm:col-span-2">
            <Label htmlFor="title">标题</Label>
            <Input
              id="title"
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
              required
              placeholder="例如：双十一运营活动已上线"
            />
          </div>
          <div className="space-y-1 sm:col-span-2">
            <Label htmlFor="description">描述</Label>
            <Textarea
              id="description"
              rows={3}
              value={form.description}
              onChange={(e) => setForm({ ...form, description: e.target.value })}
              placeholder="可选：展示在标题下方的一句话说明"
            />
          </div>
          <div className="space-y-1 sm:col-span-2">
            <Label htmlFor="clickUrl">跳转 URL（可选）</Label>
            <Input
              id="clickUrl"
              value={form.clickUrl}
              onChange={(e) => setForm({ ...form, clickUrl: e.target.value })}
              placeholder="https://..."
            />
          </div>
        </div>
      </section>

      <Separator />

      {/* ── 展示配置 ── */}
      <section className="space-y-3">
        <h4 className="text-[11px] font-medium uppercase tracking-widest text-muted-foreground">展示配置</h4>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <Label htmlFor="type">类型</Label>
            <Select value={form.type} onValueChange={(v) => setForm({ ...form, type: v as PlatformBannerType })}>
              <SelectTrigger id="type"><SelectValue /></SelectTrigger>
              <SelectContent>
                {TYPE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label htmlFor="position">排序</Label>
            <Input
              id="position"
              type="number"
              value={form.position}
              onChange={(e) => setForm({ ...form, position: Number(e.target.value) })}
            />
            <p className="text-[11px] text-muted-foreground">数字越小越靠前</p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="startTime">开始时间</Label>
            <Input
              id="startTime"
              type="datetime-local"
              value={form.startTime}
              onChange={(e) => setForm({ ...form, startTime: e.target.value })}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="endTime">结束时间</Label>
            <Input
              id="endTime"
              type="datetime-local"
              value={form.endTime}
              onChange={(e) => setForm({ ...form, endTime: e.target.value })}
            />
          </div>
        </div>

        {/* 启用开关用大块区域，与表单字段拉开层次 */}
        <div className="flex items-center justify-between gap-4 rounded-lg border bg-muted/30 px-3 py-2.5">
          <div className="min-w-0 space-y-0.5">
            <Label htmlFor="status" className="cursor-pointer">启用 Banner</Label>
            <p className="text-[11px] text-muted-foreground">关闭后该 Banner 不会出现在任何管理员的轮播中</p>
          </div>
          <Switch id="status" checked={form.status} onCheckedChange={(v) => setForm({ ...form, status: v })} />
        </div>
      </section>
    </div>
  );
}

function BannerCard({
  item,
  onEdit,
  onDelete,
  onToggle,
  toggling
}: {
  item: PlatformBannerItem;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: () => void;
  toggling: boolean;
}) {
  const typeLabel = TYPE_OPTIONS.find((o) => o.value === item.type)?.label ?? item.type;
  return (
    <article className="group flex flex-col overflow-hidden rounded-xl border bg-card transition-colors hover:border-border/80">
      <div className="relative aspect-[3/1] bg-muted">
        {/* Banner 图片一律原生 <img>：Next 图像优化器对同源 /api/storage/proxy/* 回源会
            命中私网 IP SSRF 硬拦截；单张海报图走浏览器原生缓存已足够。 */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={item.imageDisplayUrl || item.imageUrl}
          alt={item.title}
          loading="lazy"
          decoding="async"
          className="absolute inset-0 size-full object-cover"
          onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = "none"; }}
        />
        {/* 状态角标 */}
        <div className="absolute right-2 top-2">
          <Badge variant={item.status ? "success" : "secondary"} className="font-normal shadow-sm">
            {item.status ? "启用" : "停用"}
          </Badge>
        </div>
      </div>
      <div className="flex flex-1 flex-col gap-2 p-4">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline" className="font-normal">{typeLabel}</Badge>
          <span className="text-[11px] font-mono text-muted-foreground">#{item.id} · P{item.position}</span>
        </div>
        <h3 className="truncate text-sm font-semibold text-foreground">{item.title}</h3>
        {item.description ? (
          <p className="line-clamp-2 text-xs text-muted-foreground">{item.description}</p>
        ) : null}
        {item.clickUrl ? (
          <ClickUrlLink url={item.clickUrl} />
        ) : null}
        <div className="mt-auto flex flex-wrap items-center gap-1 pt-2">
          <Button size="sm" variant="ghost" className="h-7 gap-1" onClick={onToggle} disabled={toggling}>
            {item.status ? <XCircle className="size-3.5" /> : <CheckCircle2 className="size-3.5" />}
            {item.status ? "停用" : "启用"}
          </Button>
          <Button size="sm" variant="ghost" className="h-7 gap-1" onClick={onEdit}>
            <Pencil className="size-3.5" /> 编辑
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 gap-1 text-destructive hover:text-destructive"
            onClick={onDelete}
          >
            <Trash2 className="size-3.5" /> 删除
          </Button>
        </div>
      </div>
    </article>
  );
}

/** clickUrl 行：内部路由走 next/link（预取）；外链走 <a target="_blank"> */
function ClickUrlLink({ url }: { url: string }) {
  const trimmed = url.trim();
  const isInternal = trimmed.startsWith("/") && !trimmed.startsWith("//") && !trimmed.startsWith("/api/");
  const className =
    "inline-flex items-center gap-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground";
  if (isInternal) {
    return (
      <Link href={trimmed} prefetch className={className} onClick={(e) => e.stopPropagation()}>
        <ExternalLink className="size-3" />
        <span className="truncate">{trimmed}</span>
      </Link>
    );
  }
  return (
    <a
      href={trimmed}
      target="_blank"
      rel="noreferrer"
      className={className}
      onClick={(e) => e.stopPropagation()}
    >
      <ExternalLink className="size-3" />
      <span className="truncate">{trimmed}</span>
    </a>
  );
}
