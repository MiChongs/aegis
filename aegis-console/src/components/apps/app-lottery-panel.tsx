"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ChevronLeft,
  ChevronRight,
  Dices,
  ExternalLink,
  Gift,
  Loader2,
  Lock,
  Pencil,
  Plus,
  ScrollText,
  ShieldCheck,
  Trash2,
  Unlock
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  useAdminLotteryActivitiesQuery,
  useCreateAdminLotteryActivityMutation,
  useUpdateAdminLotteryActivityMutation,
  useDeleteAdminLotteryActivityMutation,
  useAdminLotteryPrizesQuery,
  useCreateAdminLotteryPrizeMutation,
  useUpdateAdminLotteryPrizeMutation,
  useDeleteAdminLotteryPrizeMutation,
  useCommitLotterySeedMutation,
  useRevealLotterySeedMutation,
  useAdminLotteryDrawsQuery,
  useAdminLotteryStatsQuery
} from "@/lib/admin-hooks";
import { verifyLotteryActivity as verifyLotteryActivityApi } from "@/lib/api/lottery";
import { useAuthStore } from "@/lib/auth-store";
import type { LotteryActivity, LotteryPrize } from "@/lib/api/lottery";

function useAdminTokenDirect() {
  return useAuthStore((state) => state.accessToken);
}

// ── 常量 ──

const STATUS_CONFIG: Record<string, { label: string; variant: "secondary" | "success" | "warning" | "outline" }> = {
  draft: { label: "草稿", variant: "secondary" },
  active: { label: "进行中", variant: "success" },
  paused: { label: "已暂停", variant: "warning" },
  ended: { label: "已结束", variant: "outline" }
};

const PRIZE_TYPE_LABELS: Record<string, string> = {
  points: "积分",
  experience: "经验值",
  item: "实物",
  coupon: "兑换码",
  custom: "自定义"
};

function fmtDate(v?: string | null) {
  if (!v) return "---";
  const d = new Date(v);
  return isNaN(d.getTime())
    ? "---"
    : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function toLocalDatetimeString(v?: string | null) {
  if (!v) return "";
  const d = new Date(v);
  if (isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// ── 主面板 ──

export function AppLotteryPanel({ appKey }: { appKey?: string | null }) {
  const [tab, setTab] = useState("activities");

  if (!appKey) {
    return <div className="py-12 text-center text-sm text-muted-foreground">请先选择应用</div>;
  }

  return (
    <Tabs value={tab} onValueChange={setTab} className="space-y-4">
      <TabsList className="w-fit">
        <TabsTrigger value="activities"><Dices className="size-4" />活动管理</TabsTrigger>
        <TabsTrigger value="prizes"><Gift className="size-4" />奖品管理</TabsTrigger>
        <TabsTrigger value="draws"><ScrollText className="size-4" />抽奖记录</TabsTrigger>
        <TabsTrigger value="fairness"><ShieldCheck className="size-4" />公平性</TabsTrigger>
      </TabsList>

      <TabsContent value="activities">
        <ActivityTab appKey={appKey} />
      </TabsContent>
      <TabsContent value="prizes">
        <PrizeTab appKey={appKey} />
      </TabsContent>
      <TabsContent value="draws">
        <DrawsTab appKey={appKey} />
      </TabsContent>
      <TabsContent value="fairness">
        <FairnessTab appKey={appKey} />
      </TabsContent>
    </Tabs>
  );
}

// ── 活动管理 Tab ──

function ActivityTab({ appKey }: { appKey: string }) {
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const query = useAdminLotteryActivitiesQuery(appKey, {
    status: statusFilter === "all" ? undefined : statusFilter,
    page,
    limit: 10
  });
  const deleteMutation = useDeleteAdminLotteryActivityMutation(appKey);

  const [showCreate, setShowCreate] = useState(false);
  const [editActivity, setEditActivity] = useState<LotteryActivity | null>(null);

  const activities = query.data?.items || [];
  const total = query.data?.total || 0;
  const totalPages = query.data?.totalPages || 1;

  const handleDelete = useCallback(
    async (id: number) => {
      try {
        await deleteMutation.mutateAsync(id);
        toast.success("活动已删除");
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : "删除失败");
      }
    },
    [deleteMutation]
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Select value={statusFilter} onValueChange={(v) => { setStatusFilter(v); setPage(1); }}>
            <SelectTrigger className="h-8 w-32 text-xs">
              <SelectValue placeholder="筛选状态" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="draft">草稿</SelectItem>
              <SelectItem value="active">进行中</SelectItem>
              <SelectItem value="paused">已暂停</SelectItem>
              <SelectItem value="ended">已结束</SelectItem>
            </SelectContent>
          </Select>
          <span className="text-xs text-muted-foreground">共 {total} 条</span>
        </div>
        <Button size="sm" className="h-8 gap-1 text-xs" onClick={() => setShowCreate(true)}>
          <Plus className="size-3" />
          创建活动
        </Button>
      </div>

      {query.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : activities.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无抽奖活动</div>
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>模式</TableHead>
                <TableHead>费用</TableHead>
                <TableHead>开始时间</TableHead>
                <TableHead>结束时间</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {activities.map((act) => {
                const sc = STATUS_CONFIG[act.status] || STATUS_CONFIG.draft;
                return (
                  <TableRow key={act.id}>
                    <TableCell className="font-mono text-xs">{act.id}</TableCell>
                    <TableCell className="font-medium">{act.name}</TableCell>
                    <TableCell>
                      <Badge variant={sc.variant}>{sc.label}</Badge>
                    </TableCell>
                    <TableCell className="text-xs">{act.uiMode === "wheel" ? "转盘" : "九宫格"}</TableCell>
                    <TableCell className="text-xs">
                      {act.costType === "free" ? "免费" : `${act.costAmount} 积分`}
                    </TableCell>
                    <TableCell className="text-xs">{fmtDate(act.startTime)}</TableCell>
                    <TableCell className="text-xs">{fmtDate(act.endTime)}</TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 px-2 text-xs"
                          onClick={() => setEditActivity(act)}
                        >
                          <Pencil className="size-3" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 px-2 text-xs text-destructive hover:text-destructive"
                          disabled={deleteMutation.isPending}
                          onClick={() => handleDelete(act.id)}
                        >
                          <Trash2 className="size-3" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>

          {/* 分页 */}
          {totalPages > 1 && (
            <div className="flex items-center justify-center gap-2">
              <Button size="sm" variant="outline" className="h-7 px-2" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="size-3.5" />
              </Button>
              <span className="text-xs text-muted-foreground">{page} / {totalPages}</span>
              <Button size="sm" variant="outline" className="h-7 px-2" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                <ChevronRight className="size-3.5" />
              </Button>
            </div>
          )}
        </>
      )}

      {/* 创建对话框 */}
      <ActivityFormDialog
        appKey={appKey}
        open={showCreate}
        onOpenChange={setShowCreate}
      />

      {/* 编辑对话框 */}
      {editActivity && (
        <ActivityFormDialog
          appKey={appKey}
          open={!!editActivity}
          onOpenChange={(v) => !v && setEditActivity(null)}
          activity={editActivity}
        />
      )}
    </div>
  );
}

// ── 活动表单对话框 ──

type ActivityFormData = {
  name: string;
  description: string;
  uiMode: string;
  joinMode: string;
  costType: string;
  costAmount: number;
  dailyLimit: number;
  totalLimit: number;
  startTime: string;
  endTime: string;
  status?: string;
};

const defaultForm: ActivityFormData = {
  name: "",
  description: "",
  uiMode: "wheel",
  joinMode: "manual",
  costType: "free",
  costAmount: 0,
  dailyLimit: 3,
  totalLimit: 0,
  startTime: "",
  endTime: ""
};

function ActivityFormDialog({
  appKey,
  open,
  onOpenChange,
  activity
}: {
  appKey: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  activity?: LotteryActivity;
}) {
  const createMutation = useCreateAdminLotteryActivityMutation(appKey);
  const updateMutation = useUpdateAdminLotteryActivityMutation(appKey);
  const isEdit = !!activity;

  const [form, setForm] = useState<ActivityFormData>(defaultForm);

  useEffect(() => {
    if (activity) {
      setForm({
        name: activity.name,
        description: activity.description || "",
        uiMode: activity.uiMode,
        joinMode: activity.joinMode,
        costType: activity.costType,
        costAmount: activity.costAmount,
        dailyLimit: activity.dailyLimit,
        totalLimit: activity.totalLimit,
        startTime: toLocalDatetimeString(activity.startTime),
        endTime: toLocalDatetimeString(activity.endTime),
        status: activity.status
      });
    } else {
      setForm(defaultForm);
    }
  }, [activity, open]);

  const set = (k: keyof ActivityFormData, v: string | number) => setForm((s) => ({ ...s, [k]: v }));

  async function handleSubmit() {
    if (!form.name.trim()) {
      toast.error("请输入活动名称");
      return;
    }
    if (!form.startTime || !form.endTime) {
      toast.error("请设置开始和结束时间");
      return;
    }

    try {
      if (isEdit) {
        await updateMutation.mutateAsync({
          id: activity!.id,
          data: {
            name: form.name,
            description: form.description || undefined,
            uiMode: form.uiMode,
            joinMode: form.joinMode,
            costType: form.costType,
            costAmount: form.costType === "points" ? form.costAmount : 0,
            dailyLimit: form.dailyLimit,
            totalLimit: form.totalLimit,
            startTime: new Date(form.startTime).toISOString(),
            endTime: new Date(form.endTime).toISOString(),
            status: form.status
          }
        });
        toast.success("活动已更新");
      } else {
        await createMutation.mutateAsync({
          name: form.name,
          description: form.description || undefined,
          uiMode: form.uiMode,
          joinMode: form.joinMode,
          costType: form.costType,
          costAmount: form.costType === "points" ? form.costAmount : 0,
          dailyLimit: form.dailyLimit,
          totalLimit: form.totalLimit,
          startTime: new Date(form.startTime).toISOString(),
          endTime: new Date(form.endTime).toISOString()
        });
        toast.success("活动已创建");
      }
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : isEdit ? "更新失败" : "创建失败");
    }
  }

  const isPending = createMutation.isPending || updateMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? "编辑活动" : "创建抽奖活动"}</DialogTitle>
          <DialogDescription>{isEdit ? "修改活动配置" : "设置抽奖活动的基本信息和规则"}</DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="活动名称" full>
            <Input className="h-8 text-sm" value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="如：年终大抽奖" />
          </Field>
          <Field label="描述" full>
            <Input className="h-8 text-sm" value={form.description} onChange={(e) => set("description", e.target.value)} placeholder="可选" />
          </Field>
          <Field label="展示模式">
            <Select value={form.uiMode} onValueChange={(v) => set("uiMode", v)}>
              <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="wheel">转盘</SelectItem>
                <SelectItem value="grid">九宫格</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="参与方式">
            <Select value={form.joinMode} onValueChange={(v) => set("joinMode", v)}>
              <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="manual">手动参与</SelectItem>
                <SelectItem value="auto">自动参与</SelectItem>
                <SelectItem value="both">两者皆可</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="费用类型">
            <Select value={form.costType} onValueChange={(v) => set("costType", v)}>
              <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="free">免费</SelectItem>
                <SelectItem value="points">积分</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {form.costType === "points" && (
            <Field label="消耗积分">
              <Input className="h-8 text-sm" type="number" min={0} value={form.costAmount} onChange={(e) => set("costAmount", Number(e.target.value))} />
            </Field>
          )}
          <Field label="每日限制次数">
            <Input className="h-8 text-sm" type="number" min={0} value={form.dailyLimit} onChange={(e) => set("dailyLimit", Number(e.target.value))} placeholder="0=不限" />
          </Field>
          <Field label="总次数限制">
            <Input className="h-8 text-sm" type="number" min={0} value={form.totalLimit} onChange={(e) => set("totalLimit", Number(e.target.value))} placeholder="0=不限" />
          </Field>
          <Field label="开始时间">
            <Input className="h-8 text-sm" type="datetime-local" value={form.startTime} onChange={(e) => set("startTime", e.target.value)} />
          </Field>
          <Field label="结束时间">
            <Input className="h-8 text-sm" type="datetime-local" value={form.endTime} onChange={(e) => set("endTime", e.target.value)} />
          </Field>
          {isEdit && (
            <Field label="状态">
              <Select value={form.status || "draft"} onValueChange={(v) => set("status", v)}>
                <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="draft">草稿</SelectItem>
                  <SelectItem value="active">进行中</SelectItem>
                  <SelectItem value="paused">已暂停</SelectItem>
                  <SelectItem value="ended">已结束</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" className="h-8 gap-1 text-xs" disabled={isPending} onClick={handleSubmit}>
            {isPending && <Loader2 className="size-3 animate-spin" />}
            {isEdit ? "保存" : "创建"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── 奖品管理 Tab ──

function PrizeTab({ appKey }: { appKey: string }) {
  const activitiesQuery = useAdminLotteryActivitiesQuery(appKey, { limit: 100 });
  const activities = activitiesQuery.data?.items || [];

  const [selectedActivityId, setSelectedActivityId] = useState<number | null>(null);

  // 自动选中第一个
  useEffect(() => {
    if (activities.length > 0 && !selectedActivityId) {
      setSelectedActivityId(activities[0].id);
    }
  }, [activities, selectedActivityId]);

  const prizesQuery = useAdminLotteryPrizesQuery(appKey, selectedActivityId);
  const deleteMutation = useDeleteAdminLotteryPrizeMutation(appKey, selectedActivityId);
  const prizes = prizesQuery.data || [];

  const [showCreate, setShowCreate] = useState(false);
  const [editPrize, setEditPrize] = useState<LotteryPrize | null>(null);

  const handleDelete = useCallback(
    async (prizeId: number) => {
      try {
        await deleteMutation.mutateAsync(prizeId);
        toast.success("奖品已删除");
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : "删除失败");
      }
    },
    [deleteMutation]
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Select
            value={selectedActivityId ? String(selectedActivityId) : ""}
            onValueChange={(v) => setSelectedActivityId(Number(v))}
          >
            <SelectTrigger className="h-8 w-48 text-xs">
              <SelectValue placeholder="选择活动" />
            </SelectTrigger>
            <SelectContent>
              {activities.map((act) => (
                <SelectItem key={act.id} value={String(act.id)}>
                  {act.name} (#{act.id})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          size="sm"
          className="h-8 gap-1 text-xs"
          disabled={!selectedActivityId}
          onClick={() => setShowCreate(true)}
        >
          <Plus className="size-3" />
          添加奖品
        </Button>
      </div>

      {!selectedActivityId ? (
        <div className="py-12 text-center text-sm text-muted-foreground">请先选择活动</div>
      ) : prizesQuery.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : prizes.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无奖品，请添加</div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>位置</TableHead>
              <TableHead>名称</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>权重</TableHead>
              <TableHead>数量</TableHead>
              <TableHead>已用</TableHead>
              <TableHead>默认</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {prizes
              .sort((a, b) => a.position - b.position)
              .map((prize) => (
                <TableRow key={prize.id}>
                  <TableCell className="font-mono text-xs">{prize.position}</TableCell>
                  <TableCell className="font-medium">{prize.name}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">{PRIZE_TYPE_LABELS[prize.type] || prize.type}</Badge>
                  </TableCell>
                  <TableCell className="text-xs">{prize.weight}</TableCell>
                  <TableCell className="text-xs">{prize.quantity}</TableCell>
                  <TableCell className="text-xs">{prize.used}</TableCell>
                  <TableCell>
                    {prize.isDefault && <Badge variant="outline">默认</Badge>}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 px-2 text-xs"
                        onClick={() => setEditPrize(prize)}
                      >
                        <Pencil className="size-3" />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 px-2 text-xs text-destructive hover:text-destructive"
                        disabled={deleteMutation.isPending}
                        onClick={() => handleDelete(prize.id)}
                      >
                        <Trash2 className="size-3" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
          </TableBody>
        </Table>
      )}

      {selectedActivityId && (
        <>
          <PrizeFormDialog
            appKey={appKey}
            activityId={selectedActivityId}
            open={showCreate}
            onOpenChange={setShowCreate}
          />
          {editPrize && (
            <PrizeFormDialog
              appKey={appKey}
              activityId={selectedActivityId}
              open={!!editPrize}
              onOpenChange={(v) => !v && setEditPrize(null)}
              prize={editPrize}
            />
          )}
        </>
      )}
    </div>
  );
}

// ── 奖品表单对话框 ──

type PrizeFormData = {
  name: string;
  type: string;
  value: string;
  imageUrl: string;
  quantity: number;
  weight: number;
  position: number;
  isDefault: boolean;
};

const defaultPrizeForm: PrizeFormData = {
  name: "",
  type: "points",
  value: "",
  imageUrl: "",
  quantity: 100,
  weight: 10,
  position: 0,
  isDefault: false
};

function PrizeFormDialog({
  appKey,
  activityId,
  open,
  onOpenChange,
  prize
}: {
  appKey: string;
  activityId: number;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  prize?: LotteryPrize;
}) {
  const createMutation = useCreateAdminLotteryPrizeMutation(appKey, activityId);
  const updateMutation = useUpdateAdminLotteryPrizeMutation(appKey, activityId);
  const isEdit = !!prize;

  const [form, setForm] = useState<PrizeFormData>(defaultPrizeForm);

  useEffect(() => {
    if (prize) {
      setForm({
        name: prize.name,
        type: prize.type,
        value: prize.value || "",
        imageUrl: prize.imageUrl || "",
        quantity: prize.quantity,
        weight: prize.weight,
        position: prize.position,
        isDefault: prize.isDefault
      });
    } else {
      setForm(defaultPrizeForm);
    }
  }, [prize, open]);

  const set = (k: keyof PrizeFormData, v: string | number | boolean) => setForm((s) => ({ ...s, [k]: v }));

  async function handleSubmit() {
    if (!form.name.trim()) {
      toast.error("请输入奖品名称");
      return;
    }

    try {
      if (isEdit) {
        await updateMutation.mutateAsync({
          prizeId: prize!.id,
          data: {
            name: form.name,
            type: form.type,
            value: form.value || undefined,
            imageUrl: form.imageUrl || undefined,
            quantity: form.quantity,
            weight: form.weight,
            position: form.position,
            isDefault: form.isDefault
          }
        });
        toast.success("奖品已更新");
      } else {
        await createMutation.mutateAsync({
          name: form.name,
          type: form.type,
          value: form.value || undefined,
          imageUrl: form.imageUrl || undefined,
          quantity: form.quantity,
          weight: form.weight,
          position: form.position,
          isDefault: form.isDefault
        });
        toast.success("奖品已添加");
      }
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : isEdit ? "更新失败" : "添加失败");
    }
  }

  const isPending = createMutation.isPending || updateMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "编辑奖品" : "添加奖品"}</DialogTitle>
          <DialogDescription>{isEdit ? "修改奖品信息" : "为活动添加一个新奖品"}</DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="奖品名称" full>
            <Input className="h-8 text-sm" value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="如：100积分" />
          </Field>
          <Field label="奖品类型">
            <Select value={form.type} onValueChange={(v) => set("type", v)}>
              <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="points">积分</SelectItem>
                <SelectItem value="experience">经验值</SelectItem>
                <SelectItem value="item">实物</SelectItem>
                <SelectItem value="coupon">兑换码</SelectItem>
                <SelectItem value="custom">自定义</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="奖品价值">
            <Input className="h-8 text-sm" value={form.value} onChange={(e) => set("value", e.target.value)} placeholder="如：100" />
          </Field>
          <Field label="图片地址">
            <Input className="h-8 text-sm" value={form.imageUrl} onChange={(e) => set("imageUrl", e.target.value)} placeholder="可选" />
          </Field>
          <Field label="数量">
            <Input className="h-8 text-sm" type="number" min={0} value={form.quantity} onChange={(e) => set("quantity", Number(e.target.value))} />
          </Field>
          <Field label="权重">
            <Input className="h-8 text-sm" type="number" min={1} value={form.weight} onChange={(e) => set("weight", Number(e.target.value))} />
          </Field>
          <Field label="位置">
            <Input className="h-8 text-sm" type="number" min={0} value={form.position} onChange={(e) => set("position", Number(e.target.value))} />
          </Field>
          <Field label="默认奖品">
            <div className="flex h-8 items-center">
              <Switch checked={form.isDefault} onCheckedChange={(v) => set("isDefault", v)} />
            </div>
          </Field>
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" className="h-8 gap-1 text-xs" disabled={isPending} onClick={handleSubmit}>
            {isPending && <Loader2 className="size-3 animate-spin" />}
            {isEdit ? "保存" : "添加"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── 抽奖记录 Tab ──

function DrawsTab({ appKey }: { appKey: string }) {
  const activitiesQuery = useAdminLotteryActivitiesQuery(appKey, { limit: 100 });
  const activities = activitiesQuery.data?.items || [];

  const [selectedActivityId, setSelectedActivityId] = useState<number | null>(null);
  const [page, setPage] = useState(1);

  useEffect(() => {
    if (activities.length > 0 && !selectedActivityId) {
      setSelectedActivityId(activities[0].id);
    }
  }, [activities, selectedActivityId]);

  const drawsQuery = useAdminLotteryDrawsQuery(appKey, selectedActivityId, { page, limit: 15 });
  const statsQuery = useAdminLotteryStatsQuery(appKey, selectedActivityId);

  const draws = drawsQuery.data?.items || [];
  const totalPages = drawsQuery.data?.totalPages || 1;
  const stats = statsQuery.data;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Select
          value={selectedActivityId ? String(selectedActivityId) : ""}
          onValueChange={(v) => { setSelectedActivityId(Number(v)); setPage(1); }}
        >
          <SelectTrigger className="h-8 w-48 text-xs">
            <SelectValue placeholder="选择活动" />
          </SelectTrigger>
          <SelectContent>
            {activities.map((act) => (
              <SelectItem key={act.id} value={String(act.id)}>
                {act.name} (#{act.id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* 统计信息 */}
      {stats && (
        <div className="grid grid-cols-3 gap-3">
          <StatCell label="总抽奖次数" value={String(stats.totalDraws)} />
          <StatCell label="总用户数" value={String(stats.totalUsers)} />
          <StatCell label="参与人数" value={String(stats.participants)} />
        </div>
      )}

      {!selectedActivityId ? (
        <div className="py-12 text-center text-sm text-muted-foreground">请先选择活动</div>
      ) : drawsQuery.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : draws.length === 0 ? (
        <div className="py-12 text-center text-sm text-muted-foreground">暂无抽奖记录</div>
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>用户 ID</TableHead>
                <TableHead>奖品 ID</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>抽奖时间</TableHead>
                <TableHead>领取时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {draws.map((draw) => (
                <TableRow key={draw.id}>
                  <TableCell className="font-mono text-xs">{draw.id}</TableCell>
                  <TableCell className="font-mono text-xs">{draw.userId}</TableCell>
                  <TableCell className="font-mono text-xs">{draw.prizeId}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        draw.status === "awarded"
                          ? "success"
                          : draw.status === "claimed"
                            ? "secondary"
                            : "warning"
                      }
                    >
                      {draw.status === "awarded" ? "已发奖" : draw.status === "claimed" ? "已领取" : "已过期"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs">{fmtDate(draw.createdAt)}</TableCell>
                  <TableCell className="text-xs">{fmtDate(draw.claimedAt)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          {totalPages > 1 && (
            <div className="flex items-center justify-center gap-2">
              <Button size="sm" variant="outline" className="h-7 px-2" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                <ChevronLeft className="size-3.5" />
              </Button>
              <span className="text-xs text-muted-foreground">{page} / {totalPages}</span>
              <Button size="sm" variant="outline" className="h-7 px-2" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                <ChevronRight className="size-3.5" />
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}

// ── 公平性 Tab ──

function FairnessTab({ appKey }: { appKey: string }) {
  const activitiesQuery = useAdminLotteryActivitiesQuery(appKey, { limit: 100 });
  const activities = activitiesQuery.data?.items || [];

  const [selectedActivityId, setSelectedActivityId] = useState<number | null>(null);

  useEffect(() => {
    if (activities.length > 0 && !selectedActivityId) {
      setSelectedActivityId(activities[0].id);
    }
  }, [activities, selectedActivityId]);

  const selectedActivity = useMemo(
    () => activities.find((a) => a.id === selectedActivityId) || null,
    [activities, selectedActivityId]
  );

  const commitMutation = useCommitLotterySeedMutation(appKey);
  const revealMutation = useRevealLotterySeedMutation(appKey);

  const handleCommit = useCallback(async () => {
    if (!selectedActivityId) return;
    try {
      await commitMutation.mutateAsync(selectedActivityId);
      toast.success("种子已提交");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "提交失败");
    }
  }, [selectedActivityId, commitMutation]);

  const handleReveal = useCallback(async () => {
    if (!selectedActivityId) return;
    try {
      await revealMutation.mutateAsync(selectedActivityId);
      toast.success("种子已揭示");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "揭示失败");
    }
  }, [selectedActivityId, revealMutation]);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Select
          value={selectedActivityId ? String(selectedActivityId) : ""}
          onValueChange={(v) => setSelectedActivityId(Number(v))}
        >
          <SelectTrigger className="h-8 w-48 text-xs">
            <SelectValue placeholder="选择活动" />
          </SelectTrigger>
          <SelectContent>
            {activities.map((act) => (
              <SelectItem key={act.id} value={String(act.id)}>
                {act.name} (#{act.id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {!selectedActivity ? (
        <div className="py-12 text-center text-sm text-muted-foreground">请先选择活动</div>
      ) : (
        <div className="space-y-4">
          {/* 种子状态 */}
          <div className="rounded-xl border border-border/60 bg-muted/30 p-4 space-y-3">
            <div className="text-sm font-medium text-foreground">种子承诺状态</div>

            <div className="space-y-2">
              <InfoRow
                label="种子哈希"
                value={selectedActivity.seedHash || "未提交"}
                mono={!!selectedActivity.seedHash}
              />
              <InfoRow
                label="种子值"
                value={selectedActivity.seedValue || "未揭示"}
                mono={!!selectedActivity.seedValue}
              />
              {selectedActivity.chainTxHash && (
                <div className="flex items-start gap-2 text-xs">
                  <span className="shrink-0 text-muted-foreground">链上哈希:</span>
                  <a
                    href={getExplorerUrl(selectedActivity.chainNetwork, selectedActivity.chainTxHash)}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 break-all font-mono text-primary hover:underline"
                  >
                    {selectedActivity.chainTxHash}
                    <ExternalLink className="size-3 shrink-0" />
                  </a>
                </div>
              )}
            </div>
          </div>

          {/* 操作按钮 */}
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              className="h-8 gap-1 text-xs"
              disabled={commitMutation.isPending || !!selectedActivity.seedHash}
              onClick={handleCommit}
            >
              {commitMutation.isPending ? (
                <Loader2 className="size-3 animate-spin" />
              ) : (
                <Lock className="size-3" />
              )}
              提交种子
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-8 gap-1 text-xs"
              disabled={revealMutation.isPending || !selectedActivity.seedHash || !!selectedActivity.seedValue}
              onClick={handleReveal}
            >
              {revealMutation.isPending ? (
                <Loader2 className="size-3 animate-spin" />
              ) : (
                <Unlock className="size-3" />
              )}
              揭示种子
            </Button>
          </div>

          {/* 验证面板（复用独立组件） */}
          {selectedActivityId && (
            <LotteryVerifyPanelInline activityId={selectedActivityId} />
          )}
        </div>
      )}
    </div>
  );
}

// ── 内联验证面板 ──

function LotteryVerifyPanelInline({ activityId }: { activityId: number }) {
  const token = useAdminTokenDirect();
  const [verifying, setVerifying] = useState(false);
  const [result, setResult] = useState<{ valid: boolean; message: string } | null>(null);

  async function handleVerify() {
    if (!token) return;
    setVerifying(true);
    try {
      const res = await verifyLotteryActivityApi(token, activityId);
      setResult(res);
      if (res.valid) toast.success("验证通过");
      else toast.error("验证失败: " + res.message);
    } catch (err: unknown) {
      toast.error(err instanceof ApiError ? err.message : "验证失败");
    } finally {
      setVerifying(false);
    }
  }

  return (
    <div className="rounded-xl border border-border/60 bg-muted/30 p-4 space-y-3">
      <div className="text-sm font-medium text-foreground">公平性验证</div>
      {result && (
        <div className="flex items-center gap-2">
          <Badge variant={result.valid ? "success" : "danger"}>
            {result.valid ? "验证通过" : "验证失败"}
          </Badge>
          <span className="text-xs text-muted-foreground">{result.message}</span>
        </div>
      )}
      <Button
        size="sm"
        variant="outline"
        className="h-8 gap-1 text-xs"
        disabled={verifying}
        onClick={handleVerify}
      >
        {verifying ? (
          <Loader2 className="size-3 animate-spin" />
        ) : (
          <ShieldCheck className="size-3" />
        )}
        验证公平性
      </Button>
    </div>
  );
}

// ── 工具组件 ──

function Field({ label, children, full }: { label: string; children: React.ReactNode; full?: boolean }) {
  return (
    <div className={`space-y-1 ${full ? "sm:col-span-2" : ""}`}>
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

function StatCell({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border/60 bg-muted/30 p-3">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold text-foreground">{value}</div>
    </div>
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start gap-2 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}:</span>
      <span className={`break-all text-foreground ${mono ? "font-mono" : ""}`}>{value}</span>
    </div>
  );
}

function getExplorerUrl(network?: string | null, txHash?: string) {
  if (!txHash) return "#";
  switch (network) {
    case "ethereum":
      return `https://etherscan.io/tx/${txHash}`;
    case "bsc":
      return `https://bscscan.com/tx/${txHash}`;
    case "polygon":
      return `https://polygonscan.com/tx/${txHash}`;
    default:
      return `https://etherscan.io/tx/${txHash}`;
  }
}
