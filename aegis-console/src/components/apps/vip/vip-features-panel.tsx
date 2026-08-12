"use client";

import { useMemo, useState } from "react";
import { Loader2, Plus, Puzzle, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  useAdminVipFeaturesQuery,
  useAdminVipPlansQuery,
  useDeleteAdminVipFeatureMutation,
  useSaveAdminVipFeatureMutation
} from "@/lib/vip-hooks";
import type { VipFeature } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

/** 与后端 `vipdomain.FeatureTagPattern` 同一条规则，改一处必须改两处 */
const TAG_PATTERN = /^[a-z][a-z0-9._-]{1,63}$/;

/**
 * 会员功能标识目录。
 *
 * 这是「两档会员」得以成立的前提：接入方后端问的是
 * `verify(token, feature="export")` 而不是拿套餐名做字符串比较 ——
 * 套餐名是运营随时会改的展示文案，改一次就会让线上的判定集体失效。
 *
 * 做成目录而不是让接入方随手传字符串，是为了让拼错有报错：
 * `exprot` 在自由字符串方案下表现为"校验永远返回 false"，没有任何一处说得出为什么。
 */
export function VipFeaturesPanel({ appKey }: { appKey: string }) {
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const saveMutation = useSaveAdminVipFeatureMutation(appKey);
  const deleteMutation = useDeleteAdminVipFeatureMutation(appKey);

  const features = useMemo(() => featuresQuery.data ?? [], [featuresQuery.data]);
  const plans = useMemo(() => plansQuery.data ?? [], [plansQuery.data]);

  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ tag: "", name: "", description: "" });
  const [pendingDelete, setPendingDelete] = useState<VipFeature | null>(null);

  /** 有哪几个套餐挂着这个标识 —— 停用/删除前必须说清影响面 */
  const plansUsing = (tag: string) => plans.filter((plan) => plan.features?.includes(tag));

  const tagValid = TAG_PATTERN.test(form.tag.trim().toLowerCase());
  const tagTaken = features.some((item) => item.tag === form.tag.trim().toLowerCase());

  const create = async () => {
    const tag = form.tag.trim().toLowerCase();
    if (!tagValid) {
      toast.error("功能标识只能是小写字母开头、含数字与 . _ - 的 2~64 位短标识");
      return;
    }
    if (!form.name.trim()) {
      toast.error("功能名称不能为空");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        tag,
        name: form.name.trim(),
        description: form.description.trim(),
        isActive: true
      });
      toast.success(`已创建「${form.name.trim()}」`);
      setForm({ tag: "", name: "", description: "" });
      setCreating(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "创建失败");
    }
  };

  const toggleActive = async (feature: VipFeature, isActive: boolean) => {
    try {
      await saveMutation.mutateAsync({ tag: feature.tag, isActive });
      toast.success(isActive ? `已启用「${feature.name}」` : `已停用「${feature.name}」：校验一律判不通过`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      const result = await deleteMutation.mutateAsync(pendingDelete.tag);
      toast.success(
        result.affectedPlans > 0
          ? `已删除；${result.affectedPlans} 个套餐从此不再发放这项权益`
          : "已删除"
      );
      setPendingDelete(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  return (
    <div className="space-y-5">
      <SectionCard
        icon={<Puzzle className="size-4" />}
        title="功能标识"
        description="接入方服务端按标识校验权益：verify(token, feature) —— 套餐改名不影响判定"
        aside={
          <Button size="sm" variant={creating ? "ghost" : "default"} onClick={() => setCreating((prev) => !prev)}>
            {creating ? "取消" : <><Plus className="size-3.5" /> 新建标识</>}
          </Button>
        }
      >
        {creating ? (
          <div className="mb-4 space-y-3 rounded-xl border border-border bg-muted/40 p-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">标识（tag）</Label>
                <Input
                  value={form.tag}
                  onChange={(event) => setForm((prev) => ({ ...prev, tag: event.target.value }))}
                  placeholder="export / ai.chat / hd_video"
                  className="font-mono"
                />
                <p
                  className={cn(
                    "text-[10px] leading-snug",
                    form.tag && !tagValid ? "text-destructive" : "text-muted-foreground"
                  )}
                >
                  {form.tag && !tagValid
                    ? "只能小写字母开头，含数字与 . _ -，2~64 位"
                    : tagTaken
                      ? "该标识已存在"
                      : "创建后不可改名 —— 它会被写进接入方的代码与每一条开通记录的快照里"}
                </p>
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">展示名</Label>
                <Input
                  value={form.name}
                  onChange={(event) => setForm((prev) => ({ ...prev, name: event.target.value }))}
                  placeholder="如：批量导出"
                />
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <Label className="text-xs">说明（可空）</Label>
                <Textarea
                  rows={2}
                  value={form.description}
                  onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))}
                  placeholder="这项能力解锁了什么，给运营与客服看"
                />
              </div>
            </div>
            <div className="flex justify-end">
              <Button size="sm" onClick={create} disabled={saveMutation.isPending || !tagValid || tagTaken}>
                {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                创建
              </Button>
            </div>
          </div>
        ) : null}

        {featuresQuery.isLoading ? (
          <div className="space-y-2">
            {[0, 1].map((index) => (
              <Skeleton key={index} className="h-16 w-full rounded-xl" />
            ))}
          </div>
        ) : features.length === 0 ? (
          <div className="space-y-2 rounded-xl border border-dashed border-border px-4 py-8 text-center">
            <p className="text-xs text-muted-foreground">
              还没有功能标识。此时会员只有一个维度：是或不是。
            </p>
            <p className="text-[11px] text-muted-foreground">
              有两档会员（基础版能导出、高级版还能用 AI）时才需要它 —— 建标识、勾进套餐，
              接入方后端就能问「他能不能用导出」而不是猜套餐名。
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {features.map((feature) => {
              const used = plansUsing(feature.tag);
              return (
                <div
                  key={feature.tag}
                  className={cn(
                    "flex flex-col gap-2 rounded-xl border border-border p-3 sm:flex-row sm:items-center sm:justify-between",
                    !feature.isActive && "bg-muted/40"
                  )}
                >
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-xs font-semibold">{feature.name}</span>
                      <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                        {feature.tag}
                      </code>
                      {!feature.isActive ? (
                        <Badge variant="secondary" size="sm">
                          已停用
                        </Badge>
                      ) : null}
                    </div>
                    {feature.description ? (
                      <p className="text-[11px] leading-snug text-muted-foreground">{feature.description}</p>
                    ) : null}
                    <p className="text-[10px] text-muted-foreground">
                      {used.length > 0 ? (
                        <>被 {used.length} 个套餐使用：{used.map((plan) => plan.name).join("、")}</>
                      ) : (
                        <>还没有套餐使用它 —— 没有任何用户会拿到这项权益</>
                      )}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Switch
                      checked={feature.isActive}
                      onCheckedChange={(value) => toggleActive(feature, value)}
                      disabled={saveMutation.isPending}
                    />
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => setPendingDelete(feature)}
                    >
                      <Trash2 className="size-3.5" />
                      <span className="sr-only">删除</span>
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除功能标识「{pendingDelete?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete && plansUsing(pendingDelete.tag).length > 0 ? (
                <>
                  还有 {plansUsing(pendingDelete.tag).length} 个套餐挂着它。删除后那些套餐
                  <b>新开通</b>的用户不再拿到这项权益，而接入方调 verify 传这个标识会收到
                  「未登记的功能标识」而不是静默的 false。已经开通的用户不受影响（他们拿的是账本快照）。
                </>
              ) : (
                <>没有套餐在使用它，删除不影响任何用户。</>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
