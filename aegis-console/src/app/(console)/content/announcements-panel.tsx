"use client";

import { useCallback, useState } from "react";
import {
  Archive,
  Bell,
  CirclePlus,
  MoreHorizontal,
  Pencil,
  Pin,
  Rocket,
  Trash2
} from "lucide-react";
import { toast } from "sonner";
import type { SystemAnnouncement } from "@/lib/api-client";
import { ApiError } from "@/lib/api-client";
import {
  useSystemAnnouncementsQuery,
  useCreateAnnouncementMutation,
  useUpdateAnnouncementMutation,
  useDeleteAnnouncementMutation,
  usePublishAnnouncementMutation,
  useArchiveAnnouncementMutation
} from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { RichEditor, RichContent } from "@/components/ui/rich-editor";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  常量                                                               */
/* ------------------------------------------------------------------ */

const typeOptions = [
  { value: "info", label: "通知" },
  { value: "warning", label: "警告" },
  { value: "maintenance", label: "维护" },
  { value: "update", label: "更新" },
  { value: "security", label: "安全" },
];

const levelOptions = [
  { value: "normal", label: "普通" },
  { value: "important", label: "重要" },
  { value: "critical", label: "紧急" },
];

const statusOptions = [
  { value: "all", label: "全部" },
  { value: "draft", label: "草稿" },
  { value: "published", label: "已发布" },
  { value: "archived", label: "已归档" },
];

function statusBadge(s: string): "secondary" | "success" | "warning" {
  if (s === "published") return "success";
  if (s === "archived") return "warning";
  return "secondary";
}

function levelColor(l: string) {
  if (l === "critical") return "bg-red-500";
  if (l === "important") return "bg-amber-500";
  return "bg-blue-500";
}

function fmtDate(v?: string | null) {
  if (!v) return "--";
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? "--" : new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(d);
}

/* ------------------------------------------------------------------ */
/*  编辑弹窗                                                            */
/* ------------------------------------------------------------------ */

type FormData = {
  type: string;
  title: string;
  content: string;
  level: string;
  pinned: boolean;
  expiresAt: string;
};

const emptyForm: FormData = {
  type: "info", title: "", content: "", level: "normal", pinned: false, expiresAt: "",
};

function toForm(a: SystemAnnouncement): FormData {
  return {
    type: a.type || "info",
    title: a.title || "",
    content: a.content || "",
    level: a.level || "normal",
    pinned: a.pinned === true,
    expiresAt: a.expiresAt ? new Date(a.expiresAt).toISOString().slice(0, 16) : "",
  };
}

function AnnouncementDialog({
  mode,
  item,
  open,
  onOpenChange,
}: {
  mode: "create" | "edit";
  item?: SystemAnnouncement | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [form, setForm] = useState<FormData>(item ? toForm(item) : emptyForm);
  const createMut = useCreateAnnouncementMutation();
  const updateMut = useUpdateAnnouncementMutation();
  const busy = createMut.isPending || updateMut.isPending;

  const set = useCallback(<K extends keyof FormData>(k: K, v: FormData[K]) => {
    setForm((s) => ({ ...s, [k]: v }));
  }, []);

  async function handleSubmit() {
    const payload: Record<string, unknown> = {
      type: form.type,
      title: form.title,
      content: form.content,
      level: form.level,
      pinned: form.pinned,
      expiresAt: form.expiresAt || null,
    };
    try {
      if (mode === "edit" && item) {
        await updateMut.mutateAsync({ id: item.id, payload });
        toast.success("公告已更新");
      } else {
        await createMut.mutateAsync(payload);
        toast.success("公告已创建");
      }
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[92vw] max-w-2xl p-0 flex flex-col">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle>{mode === "edit" ? "编辑公告" : "创建公告"}</SheetTitle>
          <SheetDescription>{mode === "edit" ? "修改公告内容" : "创建系统公告，发布后所有管理员可见。"}</SheetDescription>
        </SheetHeader>

        <ScrollArea className="flex-1">
          <div className="space-y-5 px-6 py-5">
            {/* 元信息行 */}
            <div className="grid gap-4 sm:grid-cols-3">
              <div className="space-y-1.5">
                <Label className="text-xs">类型</Label>
                <Select value={form.type} onValueChange={(v) => set("type", v)}>
                  <SelectTrigger className="h-9"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {typeOptions.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">级别</Label>
                <Select value={form.level} onValueChange={(v) => set("level", v)}>
                  <SelectTrigger className="h-9">
                    <div className="flex items-center gap-2">
                      <span className={cn("size-2 rounded-full", levelColor(form.level))} />
                      <SelectValue />
                    </div>
                  </SelectTrigger>
                  <SelectContent>
                    {levelOptions.map((l) => (
                      <SelectItem key={l.value} value={l.value}>
                        <div className="flex items-center gap-2">
                          <span className={cn("size-2 rounded-full", levelColor(l.value))} />
                          {l.label}
                        </div>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">过期时间</Label>
                <Input className="h-9" type="datetime-local" value={form.expiresAt} onChange={(e) => set("expiresAt", e.target.value)} />
              </div>
            </div>

            {/* 标题 */}
            <div className="space-y-1.5">
              <Label className="text-xs">标题</Label>
              <Input className="h-10 text-base font-medium" value={form.title} onChange={(e) => set("title", e.target.value)} placeholder="输入公告标题..." />
            </div>

            {/* 富文本内容 — 主编辑区 */}
            <div className="space-y-1.5">
              <Label className="text-xs">内容（支持富文本，可全屏编辑）</Label>
              <RichEditor
                value={form.content}
                onChange={(html) => set("content", html)}
                placeholder="在此编写公告正文，支持标题、列表、引用、链接等格式..."
                fullscreenable
              />
            </div>

            {/* 选项行 */}
            <div className="flex items-center gap-6">
              <div className="flex items-center gap-2">
                <Switch checked={form.pinned} onCheckedChange={(v) => set("pinned", v)} />
                <Label className="text-xs">置顶显示</Label>
              </div>
            </div>
          </div>
        </ScrollArea>

        {/* 底部操作栏 */}
        <div className="shrink-0 flex items-center justify-end gap-2 border-t px-6 py-3">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>取消</Button>
          <Button size="sm" onClick={handleSubmit} disabled={busy || !form.title.trim()}>
            {busy ? "保存中..." : mode === "edit" ? "保存" : "创建"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

/* ------------------------------------------------------------------ */
/*  公告卡片                                                            */
/* ------------------------------------------------------------------ */

function AnnouncementCard({
  item,
  onEdit,
}: {
  item: SystemAnnouncement;
  onEdit: () => void;
}) {
  const publishMut = usePublishAnnouncementMutation();
  const archiveMut = useArchiveAnnouncementMutation();
  const deleteMut = useDeleteAnnouncementMutation();

  return (
    <div className="rounded-xl border bg-card overflow-hidden">
      <div className="flex items-start justify-between gap-3 px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span className={cn("size-2 rounded-full shrink-0", levelColor(item.level))} />
            <span className="text-sm font-semibold truncate">{item.title}</span>
            <Badge variant="outline" className="text-[10px]">{typeOptions.find((t) => t.value === item.type)?.label || item.type}</Badge>
            <Badge variant={statusBadge(item.status)} className="text-[10px]">
              {item.status === "published" ? "已发布" : item.status === "archived" ? "已归档" : "草稿"}
            </Badge>
            {item.pinned && <Badge variant="secondary" className="text-[10px] gap-0.5"><Pin className="size-2.5" />置顶</Badge>}
          </div>
          <div className="flex items-center gap-3 text-[11px] text-muted-foreground mt-1">
            <span>{item.adminName || `管理员 ${item.adminId}`}</span>
            <span>{fmtDate(item.publishedAt || item.createdAt)}</span>
            {item.expiresAt && <span>过期 {fmtDate(item.expiresAt)}</span>}
          </div>
          {item.content && <RichContent html={item.content} className="mt-1.5 text-xs text-muted-foreground line-clamp-2 *:m-0" />}
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 shrink-0"><MoreHorizontal className="size-4" /></Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}><Pencil className="size-3.5 mr-2" />编辑</DropdownMenuItem>
            {item.status !== "published" && (
              <DropdownMenuItem onClick={() => publishMut.mutateAsync(item.id).then(() => toast.success("已发布")).catch((e) => toast.error(e instanceof ApiError ? e.message : "失败"))}>
                <Rocket className="size-3.5 mr-2" />发布
              </DropdownMenuItem>
            )}
            {item.status === "published" && (
              <DropdownMenuItem onClick={() => archiveMut.mutateAsync(item.id).then(() => toast.success("已归档")).catch((e) => toast.error(e instanceof ApiError ? e.message : "失败"))}>
                <Archive className="size-3.5 mr-2" />归档
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-red-600 dark:text-red-400" onClick={() => deleteMut.mutateAsync(item.id).then(() => toast.success("已删除")).catch((e) => toast.error(e instanceof ApiError ? e.message : "失败"))}>
              <Trash2 className="size-3.5 mr-2" />删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  面板主组件                                                          */
/* ------------------------------------------------------------------ */

export function AnnouncementsPanel() {
  const [statusFilter, setStatusFilter] = useState("all");
  const [page, setPage] = useState(1);
  const query = useSystemAnnouncementsQuery({
    status: statusFilter !== "all" ? statusFilter : undefined,
    page,
    limit: 20,
  });
  const items = query.data?.items || [];
  const totalPages = query.data?.totalPages || 1;

  const [dialog, setDialog] = useState<{ open: boolean; mode: "create" | "edit"; item?: SystemAnnouncement | null }>({ open: false, mode: "create" });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-2">
          <Bell className="size-4 text-muted-foreground" />
          <h3 className="text-sm font-semibold">系统公告</h3>
          <Badge variant="outline" className="text-[10px]">{query.data?.total ?? 0}</Badge>
        </div>
        <div className="flex items-center gap-2">
          <Select value={statusFilter} onValueChange={(v) => { setStatusFilter(v); setPage(1); }}>
            <SelectTrigger className="w-24 h-8 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {statusOptions.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <Button size="sm" onClick={() => setDialog({ open: true, mode: "create" })}>
            <CirclePlus className="size-3.5 mr-1.5" />创建
          </Button>
        </div>
      </div>

      {!items.length ? (
        <EmptyState title="暂无公告" description="点击「创建」发布第一条系统公告。" />
      ) : (
        <div className="grid gap-3">
          {items.map((a) => (
            <AnnouncementCard key={a.id} item={a} onEdit={() => setDialog({ open: true, mode: "edit", item: a })} />
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2">
          <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
          <span className="text-xs text-muted-foreground font-mono">{page} / {totalPages}</span>
          <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>下一页</Button>
        </div>
      )}

      {dialog.open && (
        <AnnouncementDialog
          mode={dialog.mode}
          item={dialog.item}
          open={dialog.open}
          onOpenChange={(open) => setDialog((s) => ({ ...s, open }))}
        />
      )}
    </div>
  );
}
