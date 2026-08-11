"use client";

import { FormEvent, Suspense, useState } from "react";
import { useSearchParams } from "next/navigation";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { AnnouncementsPanel } from "./announcements-panel";
import { ApiError, type BannerItem, type NoticeItem } from "@/lib/api-client";
import {
  useAdminAppsQuery,
  useAdminBannersQuery,
  useAdminNoticesQuery,
  useCreateAdminBannerMutation,
  useCreateAdminNoticeMutation,
  useDeleteAdminBannerMutation,
  useDeleteAdminNoticeMutation,
  useUpdateAdminBannerMutation,
  useUpdateAdminNoticeMutation
} from "@/lib/admin-hooks";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { SurfaceCard } from "@/components/ui/surface-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";

type BannerFormState = {
  id?: number;
  header: string;
  title: string;
  content: string;
  url: string;
  type: string;
  position: string;
  status: "enabled" | "disabled";
  startTime: string;
  endTime: string;
};

type NoticeFormState = {
  id?: number;
  title: string;
  content: string;
};

const defaultBannerForm: BannerFormState = {
  header: "",
  title: "",
  content: "",
  url: "",
  type: "hero",
  position: "0",
  status: "enabled",
  startTime: "",
  endTime: ""
};

const defaultNoticeForm: NoticeFormState = {
  title: "",
  content: ""
};

function formatDateTime(value?: string | null) {
  if (!value) {
    return "未设置";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "未设置";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

function toInputDateTime(value?: string | null) {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const offset = date.getTimezoneOffset();
  const local = new Date(date.getTime() - offset * 60_000);
  return local.toISOString().slice(0, 16);
}

function ContentPageInner() {
  const searchParams = useSearchParams();
  const appsQuery = useAdminAppsQuery();
  // `?app=<appKey>` 来自应用详情页的「内容」入口：从那里跳过来必须落在同一个应用上，
  // 否则跨页之后作用域悄悄换成了第一个应用，改错对象都不会有任何提示。
  const scopedAppKey = searchParams.get("app");
  const [selectedAppId, setSelectedAppId] = useState<number | null>(null);
  const [bannerDialogOpen, setBannerDialogOpen] = useState(false);
  const [noticeDialogOpen, setNoticeDialogOpen] = useState(false);
  const [bannerForm, setBannerForm] = useState<BannerFormState>(defaultBannerForm);
  const [noticeForm, setNoticeForm] = useState<NoticeFormState>(defaultNoticeForm);
  const [bannerError, setBannerError] = useState<string | null>(null);
  const [noticeError, setNoticeError] = useState<string | null>(null);

  const scopedApp = scopedAppKey ? (appsQuery.data || []).find((item) => item.appKey === scopedAppKey) : undefined;
  const resolvedAppId = selectedAppId ?? scopedApp?.id ?? (appsQuery.data || [])[0]?.id ?? null;
  const selectedApp = (appsQuery.data || []).find((item) => item.id === resolvedAppId) || null;
  const bannersQuery = useAdminBannersQuery(selectedApp?.id);
  const noticesQuery = useAdminNoticesQuery(selectedApp?.id);
  const createBannerMutation = useCreateAdminBannerMutation(selectedApp?.id);
  const updateBannerMutation = useUpdateAdminBannerMutation(selectedApp?.id);
  const deleteBannerMutation = useDeleteAdminBannerMutation(selectedApp?.id);
  const createNoticeMutation = useCreateAdminNoticeMutation(selectedApp?.id);
  const updateNoticeMutation = useUpdateAdminNoticeMutation(selectedApp?.id);
  const deleteNoticeMutation = useDeleteAdminNoticeMutation(selectedApp?.id);

  function openCreateBanner() {
    setBannerError(null);
    setBannerForm(defaultBannerForm);
    setBannerDialogOpen(true);
  }

  function openEditBanner(item: BannerItem) {
    setBannerError(null);
    setBannerForm({
      id: item.id,
      header: item.header || "",
      title: item.title || "",
      content: item.content || "",
      url: item.url || "",
      type: item.type || "hero",
      position: String(item.position ?? 0),
      status: item.status === false ? "disabled" : "enabled",
      startTime: toInputDateTime(item.startTime),
      endTime: toInputDateTime(item.endTime)
    });
    setBannerDialogOpen(true);
  }

  function openCreateNotice() {
    setNoticeError(null);
    setNoticeForm(defaultNoticeForm);
    setNoticeDialogOpen(true);
  }

  function openEditNotice(item: NoticeItem) {
    setNoticeError(null);
    setNoticeForm({
      id: item.id,
      title: item.title || "",
      content: item.content || ""
    });
    setNoticeDialogOpen(true);
  }

  async function handleSubmitBanner(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBannerError(null);
    try {
      const payload = {
        header: bannerForm.header || undefined,
        title: bannerForm.title || undefined,
        content: bannerForm.content || undefined,
        url: bannerForm.url || undefined,
        type: bannerForm.type || undefined,
        position: Number(bannerForm.position || 0),
        status: bannerForm.status === "enabled",
        startTime: bannerForm.startTime ? new Date(bannerForm.startTime).toISOString() : undefined,
        endTime: bannerForm.endTime ? new Date(bannerForm.endTime).toISOString() : undefined
      };
      if (bannerForm.id) {
        await updateBannerMutation.mutateAsync({ bannerId: bannerForm.id, data: payload });
        toast.success("Banner 已更新");
      } else {
        await createBannerMutation.mutateAsync(payload);
        toast.success("Banner 已创建");
      }
      setBannerDialogOpen(false);
      setBannerForm(defaultBannerForm);
    } catch (error) {
      setBannerError(error instanceof ApiError ? error.message : "保存 Banner 失败");
    }
  }

  async function handleSubmitNotice(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setNoticeError(null);
    try {
      const payload = {
        title: noticeForm.title || undefined,
        content: noticeForm.content || undefined
      };
      if (noticeForm.id) {
        await updateNoticeMutation.mutateAsync({ noticeId: noticeForm.id, data: payload });
        toast.success("公告已更新");
      } else {
        await createNoticeMutation.mutateAsync(payload);
        toast.success("公告已创建");
      }
      setNoticeDialogOpen(false);
      setNoticeForm(defaultNoticeForm);
    } catch (error) {
      setNoticeError(error instanceof ApiError ? error.message : "保存公告失败");
    }
  }

  async function handleDeleteBanner(id: number) {
    try {
      await deleteBannerMutation.mutateAsync(id);
      toast.success("Banner 已删除");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除 Banner 失败");
    }
  }

  async function handleDeleteNotice(id: number) {
    try {
      await deleteNoticeMutation.mutateAsync(id);
      toast.success("公告已删除");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除公告失败");
    }
  }

  if (appsQuery.isLoading) {
    return <LoadingState title="正在加载内容模块" description="正在读取应用内容配置。" />;
  }

  if (!selectedApp) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="Content" title="内容" description="当前没有可管理的应用。" />
        <EmptyState title="暂无应用" description="请先创建应用。" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="Content"
        title="内容"
        description="Banner 与公告维护。"
        action={
          <Select value={String(resolvedAppId)} onValueChange={(value) => setSelectedAppId(Number(value))}>
            <SelectTrigger className="w-[240px]">
              <SelectValue placeholder="选择应用" />
            </SelectTrigger>
            <SelectContent>
              {(appsQuery.data || []).map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>
                  {item.name} ({item.id})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
      />

      <section className="metrics-grid">
        <SurfaceCard>
          <div className="space-y-1">
            <div className="text-sm text-muted-foreground">Banner 数量</div>
            <div className="text-2xl font-semibold text-foreground">{bannersQuery.data?.length || 0}</div>
          </div>
        </SurfaceCard>
        <SurfaceCard>
          <div className="space-y-1">
            <div className="text-sm text-muted-foreground">公告数量</div>
            <div className="text-2xl font-semibold text-foreground">{noticesQuery.data?.length || 0}</div>
          </div>
        </SurfaceCard>
      </section>

      <SurfaceCard>
        <Tabs defaultValue="banners" className="space-y-5">
          <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div className="space-y-2">
              <Badge variant="outline">Operations</Badge>
              <h2 className="text-lg font-semibold text-foreground">内容维护</h2>
            </div>
            <div className="flex flex-wrap gap-2">
              <TabsList>
                <TabsTrigger value="banners">Banner</TabsTrigger>
                <TabsTrigger value="notices">公告</TabsTrigger>
              </TabsList>
            </div>
          </div>

          <TabsContent value="banners" className="space-y-4">
            <div className="flex justify-end">
              <Dialog open={bannerDialogOpen} onOpenChange={setBannerDialogOpen}>
                <DialogTrigger asChild>
                  <Button onClick={openCreateBanner}>
                    <Plus className="size-4" />
                    新建 Banner
                  </Button>
                </DialogTrigger>
                <DialogContent className="sm:max-w-2xl">
                  <DialogHeader>
                    <DialogTitle>{bannerForm.id ? "编辑 Banner" : "新建 Banner"}</DialogTitle>
                    <DialogDescription>维护轮播位、活动位和宣传位内容。</DialogDescription>
                  </DialogHeader>
                  <form className="grid gap-4 md:grid-cols-2" onSubmit={handleSubmitBanner}>
                    <div className="space-y-2">
                      <Label htmlFor="banner-header">头图</Label>
                      <Input
                        id="banner-header"
                        value={bannerForm.header}
                        onChange={(event) => setBannerForm((state) => ({ ...state, header: event.target.value }))}
                        placeholder="https://..."
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="banner-title">标题</Label>
                      <Input
                        id="banner-title"
                        value={bannerForm.title}
                        onChange={(event) => setBannerForm((state) => ({ ...state, title: event.target.value }))}
                        placeholder="首页活动标题"
                      />
                    </div>
                    <div className="space-y-2 md:col-span-2">
                      <Label htmlFor="banner-content">内容</Label>
                      <Textarea
                        id="banner-content"
                        value={bannerForm.content}
                        onChange={(event) => setBannerForm((state) => ({ ...state, content: event.target.value }))}
                        rows={4}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="banner-url">跳转地址</Label>
                      <Input
                        id="banner-url"
                        value={bannerForm.url}
                        onChange={(event) => setBannerForm((state) => ({ ...state, url: event.target.value }))}
                        placeholder="https://..."
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="banner-type">类型</Label>
                      <Input
                        id="banner-type"
                        value={bannerForm.type}
                        onChange={(event) => setBannerForm((state) => ({ ...state, type: event.target.value }))}
                        placeholder="hero"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="banner-position">排序</Label>
                      <Input
                        id="banner-position"
                        type="number"
                        value={bannerForm.position}
                        onChange={(event) => setBannerForm((state) => ({ ...state, position: event.target.value }))}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>状态</Label>
                      <Select
                        value={bannerForm.status}
                        onValueChange={(value: "enabled" | "disabled") => setBannerForm((state) => ({ ...state, status: value }))}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="enabled">启用</SelectItem>
                          <SelectItem value="disabled">停用</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="banner-start">开始时间</Label>
                      <Input
                        id="banner-start"
                        type="datetime-local"
                        value={bannerForm.startTime}
                        onChange={(event) => setBannerForm((state) => ({ ...state, startTime: event.target.value }))}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="banner-end">结束时间</Label>
                      <Input
                        id="banner-end"
                        type="datetime-local"
                        value={bannerForm.endTime}
                        onChange={(event) => setBannerForm((state) => ({ ...state, endTime: event.target.value }))}
                      />
                    </div>
                    {bannerError ? <p className="text-sm text-red-600 md:col-span-2">{bannerError}</p> : null}
                    <DialogFooter className="md:col-span-2">
                      <Button disabled={createBannerMutation.isPending || updateBannerMutation.isPending} type="submit">
                        {bannerForm.id ? "保存变更" : "创建 Banner"}
                      </Button>
                    </DialogFooter>
                  </form>
                </DialogContent>
              </Dialog>
            </div>

            {!bannersQuery.data?.length ? (
              <EmptyState title="暂无 Banner" description="当前应用还没有配置 Banner。" />
            ) : (
              <div className="table-shell">
                <ScrollArea className="h-[520px]">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>标题</TableHead>
                        <TableHead>类型</TableHead>
                        <TableHead>状态</TableHead>
                        <TableHead>时间</TableHead>
                        <TableHead className="text-right">操作</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {bannersQuery.data.map((item) => (
                        <TableRow key={item.id}>
                          <TableCell>
                            <div className="space-y-1">
                              <div className="font-semibold text-foreground">{item.title || "未命名 Banner"}</div>
                              <div className="text-xs text-muted-foreground">{item.url || item.header || "未配置链接"}</div>
                            </div>
                          </TableCell>
                          <TableCell>{item.type || "hero"}</TableCell>
                          <TableCell>
                            <Badge variant={item.status === false ? "warning" : "success"}>
                              {item.status === false ? "停用" : "启用"}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {formatDateTime(item.startTime)} / {formatDateTime(item.endTime)}
                          </TableCell>
                          <TableCell className="text-right">
                            <div className="flex justify-end gap-2">
                              <Button variant="outline" size="sm" onClick={() => openEditBanner(item)}>
                                <Pencil className="size-4" />
                                编辑
                              </Button>
                              <Button variant="outline" size="sm" onClick={() => void handleDeleteBanner(item.id)}>
                                <Trash2 className="size-4" />
                                删除
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </ScrollArea>
              </div>
            )}
          </TabsContent>

          <TabsContent value="notices" className="space-y-4">
            <div className="flex justify-end">
              <Dialog open={noticeDialogOpen} onOpenChange={setNoticeDialogOpen}>
                <DialogTrigger asChild>
                  <Button onClick={openCreateNotice}>
                    <Plus className="size-4" />
                    新建公告
                  </Button>
                </DialogTrigger>
                <DialogContent className="sm:max-w-xl">
                  <DialogHeader>
                    <DialogTitle>{noticeForm.id ? "编辑公告" : "新建公告"}</DialogTitle>
                    <DialogDescription>维护应用公告内容。</DialogDescription>
                  </DialogHeader>
                  <form className="grid gap-4" onSubmit={handleSubmitNotice}>
                    <div className="space-y-2">
                      <Label htmlFor="notice-title">标题</Label>
                      <Input
                        id="notice-title"
                        value={noticeForm.title}
                        onChange={(event) => setNoticeForm((state) => ({ ...state, title: event.target.value }))}
                        placeholder="系统维护通知"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="notice-content">内容</Label>
                      <Textarea
                        id="notice-content"
                        value={noticeForm.content}
                        onChange={(event) => setNoticeForm((state) => ({ ...state, content: event.target.value }))}
                        rows={6}
                      />
                    </div>
                    {noticeError ? <p className="text-sm text-red-600">{noticeError}</p> : null}
                    <DialogFooter>
                      <Button disabled={createNoticeMutation.isPending || updateNoticeMutation.isPending} type="submit">
                        {noticeForm.id ? "保存变更" : "创建公告"}
                      </Button>
                    </DialogFooter>
                  </form>
                </DialogContent>
              </Dialog>
            </div>

            {!noticesQuery.data?.length ? (
              <EmptyState title="暂无公告" description="当前应用还没有公告内容。" />
            ) : (
              <div className="table-shell">
                <ScrollArea className="h-[520px]">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>标题</TableHead>
                        <TableHead>内容摘要</TableHead>
                        <TableHead>更新时间</TableHead>
                        <TableHead className="text-right">操作</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {noticesQuery.data.map((item) => (
                        <TableRow key={item.id}>
                          <TableCell className="font-semibold text-foreground">{item.title || "未命名公告"}</TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {(item.content || "无内容").slice(0, 64)}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">{formatDateTime(item.updatedAt)}</TableCell>
                          <TableCell className="text-right">
                            <div className="flex justify-end gap-2">
                              <Button variant="outline" size="sm" onClick={() => openEditNotice(item)}>
                                <Pencil className="size-4" />
                                编辑
                              </Button>
                              <Button variant="outline" size="sm" onClick={() => void handleDeleteNotice(item.id)}>
                                <Trash2 className="size-4" />
                                删除
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </ScrollArea>
              </div>
            )}
          </TabsContent>
        </Tabs>
      </SurfaceCard>

      {/* 系统公告管理 */}
      <SurfaceCard>
        <AnnouncementsPanel />
      </SurfaceCard>
    </div>
  );
}

// `useSearchParams()` 必须处在 Suspense 边界内，否则 next build 报错、整页退化为客户端渲染
export default function ContentPage() {
  return (
    <Suspense fallback={<LoadingState title="正在加载内容模块" description="正在读取应用内容配置。" />}>
      <ContentPageInner />
    </Suspense>
  );
}
