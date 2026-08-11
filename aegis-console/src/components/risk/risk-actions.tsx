"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, ArrowRight, Loader2, Pencil, Plus, ShieldAlert, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import {
  useCreateRiskActionMutation, useDeleteRiskActionMutation,
  useRiskActionsQuery, useUpdateRiskActionMutation,
} from "@/lib/risk-hooks";
import type { RiskAction, RiskLevelCatalog } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { ActionBadge, LEVEL_COLORS, fmtDuration, useRiskCatalog } from "./risk-shared";

/**
 * 处置策略：分数区间 → 动作。
 *
 * 这一页最容易出的事故是**区间重叠**：两条策略都覆盖 60 分时，
 * 到底走哪一条取决于 SQL 的排序，事后极难排查。后端已经在创建 / 更新时
 * 拒绝重叠；这里再画一条覆盖条，让「哪一段分数没有任何策略」一眼可见 ——
 * 没被覆盖的区间会静默回落到「放行」，而管理员以为自己配全了。
 */

type ActionForm = {
  scene: string; minScore: string; maxScore: string;
  action: string; banDuration: string; description: string;
};

const EMPTY_FORM: ActionForm = {
  scene: "login", minScore: "41", maxScore: "", action: "captcha", banDuration: "0", description: "",
};

export function RiskActionsPanel() {
  const { scenes, levels } = useRiskCatalog();
  const [sceneFilter, setSceneFilter] = useState("login");
  const [editing, setEditing] = useState<RiskAction | "new" | null>(null);
  const [pendingDelete, setPendingDelete] = useState<RiskAction | null>(null);

  const query = useRiskActionsQuery(sceneFilter || undefined);
  const updateMut = useUpdateRiskActionMutation();
  const deleteMut = useDeleteRiskActionMutation();

  const actions = useMemo(
    () => [...(query.data ?? [])].sort((a, b) => a.minScore - b.minScore),
    [query.data],
  );

  const toggle = async (action: RiskAction, isActive: boolean) => {
    try {
      await updateMut.mutateAsync({ id: action.id, data: { isActive } });
      toast.success(isActive ? "策略已启用" : "策略已停用");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={sceneFilter || "all"} onValueChange={(v) => setSceneFilter(v === "all" ? "" : v)}>
          <SelectTrigger className="h-8 w-32 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部场景</SelectItem>
            {scenes.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground">
          总分落入哪个区间就执行对应动作，未被覆盖的分数按放行处理
        </span>
        <Button size="sm" className="ml-auto" onClick={() => setEditing("new")}>
          <Plus className="size-3.5" />新建策略
        </Button>
      </div>

      {sceneFilter && <CoverageBar actions={actions.filter((a) => a.isActive)} levels={levels} />}

      {query.isLoading ? (
        <div className="space-y-2">{Array.from({ length: 3 }, (_, i) => (
          <div key={i} className="h-16 animate-pulse rounded-xl bg-muted/50" />
        ))}</div>
      ) : actions.length === 0 ? (
        <EmptyState title="暂无处置策略"
          description="没有策略时命中规则也不会拦截，风控只会记录" />
      ) : (
        <div className="space-y-2">
          {actions.map((action) => (
            <div key={action.id}
              className={cn("group flex items-center gap-3 rounded-xl border bg-card px-4 py-3",
                !action.isActive && "opacity-60")}>
              <ShieldAlert className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5 text-sm">
                  <Badge variant="outline" size="sm" className="font-mono">
                    {action.minScore} – {action.maxScore ?? "∞"} 分
                  </Badge>
                  <ArrowRight className="size-3 text-muted-foreground" />
                  <ActionBadge action={action.action} />
                  {action.banDuration > 0 && (
                    <Badge variant="outline" size="sm">封禁 {fmtDuration(action.banDuration)}</Badge>
                  )}
                </div>
                {action.description && (
                  <p className="mt-0.5 text-xs text-muted-foreground">{action.description}</p>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <Switch checked={action.isActive} onCheckedChange={(v) => toggle(action, v)} />
                <Button variant="ghost" size="sm" className="h-7 px-2 opacity-0 group-hover:opacity-100"
                  onClick={() => setEditing(action)}>
                  <Pencil className="size-3.5" />
                </Button>
                <Button variant="ghost" size="sm" className="h-7 px-2 text-destructive opacity-0 group-hover:opacity-100"
                  onClick={() => setPendingDelete(action)}>
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing && (
        <ActionEditorDialog action={editing === "new" ? null : editing}
          defaultScene={sceneFilter || "login"} onClose={() => setEditing(null)} />
      )}

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这条处置策略？</AlertDialogTitle>
            <AlertDialogDescription>
              删除后这段分数不再有策略覆盖，落入的请求按放行处理。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={async () => {
              if (!pendingDelete) return;
              try {
                await deleteMut.mutateAsync(pendingDelete.id);
                toast.success("策略已删除");
              } catch (error) {
                toast.error(error instanceof ApiError ? error.message : "删除失败");
              } finally {
                setPendingDelete(null);
              }
            }}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * 分数覆盖条。0–100 分的横轴上画出每条策略覆盖的区段，
 * 空白处即「没有策略、会回落到放行」的分数段。
 */
function CoverageBar({ actions, levels }: { actions: RiskAction[]; levels: RiskLevelCatalog[] }) {
  const MAX = 100;
  const gaps = useMemo(() => {
    const covered = actions
      .map((a) => ({ from: a.minScore, to: Math.min(a.maxScore ?? MAX, MAX) }))
      .sort((a, b) => a.from - b.from);
    const result: Array<{ from: number; to: number }> = [];
    let cursor = 0;
    for (const range of covered) {
      if (range.from > cursor) result.push({ from: cursor, to: range.from - 1 });
      cursor = Math.max(cursor, range.to + 1);
    }
    if (cursor <= MAX) result.push({ from: cursor, to: MAX });
    return result;
  }, [actions]);

  return (
    <Card><CardContent className="space-y-2 p-3">
      <div className="flex items-center gap-2">
        <span className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">分数覆盖</span>
        {gaps.length > 0 && (
          <span className="flex items-center gap-1 text-[10px] text-amber-600 dark:text-amber-400">
            <AlertTriangle className="size-3" />
            {gaps.map((g) => `${g.from}-${g.to}`).join("、")} 分无策略覆盖，按放行处理
          </span>
        )}
      </div>

      {/* 底层：等级分档（提供分数语义参照） */}
      <div className="flex h-2 w-full overflow-hidden rounded-full">
        {levels.map((level) => {
          const max = Math.min(level.maxScore ?? MAX, MAX);
          const width = ((max - level.minScore + 1) / (MAX + 1)) * 100;
          return (
            <div key={level.value} title={`${level.label} ${level.minScore}–${level.maxScore ?? "∞"}`}
              style={{ width: `${width}%`, background: LEVEL_COLORS[level.value]?.light, opacity: 0.35 }} />
          );
        })}
      </div>

      {/* 上层：策略区段 */}
      <div className="relative h-6 w-full rounded-md bg-muted/40">
        {actions.map((action) => {
          const to = Math.min(action.maxScore ?? MAX, MAX);
          const left = (action.minScore / (MAX + 1)) * 100;
          const width = ((to - action.minScore + 1) / (MAX + 1)) * 100;
          return (
            <div key={action.id}
              className="absolute inset-y-0 flex items-center justify-center overflow-hidden rounded border border-background text-[9px] font-medium text-white"
              style={{ left: `${left}%`, width: `${width}%`, background: ACTION_BAR_COLORS[action.action] ?? "#64748b" }}
              title={`${action.minScore}–${action.maxScore ?? "∞"} → ${action.action}`}>
              {width > 8 ? action.action : ""}
            </div>
          );
        })}
      </div>

      <div className="flex justify-between text-[9px] text-muted-foreground">
        {[0, 20, 40, 60, 80, 100].map((tick) => <span key={tick}>{tick}</span>)}
      </div>
    </CardContent></Card>
  );
}

const ACTION_BAR_COLORS: Record<string, string> = {
  pass: "#10b981", captcha: "#3b82f6", review: "#f59e0b", block: "#f43f5e", ban: "#9f1239",
};

function ActionEditorDialog({ action, defaultScene, onClose }: {
  action: RiskAction | null; defaultScene: string; onClose: () => void;
}) {
  const { scenes, actions: actionCatalog } = useRiskCatalog();
  const createMut = useCreateRiskActionMutation();
  const updateMut = useUpdateRiskActionMutation();

  const [form, setForm] = useState<ActionForm>(() => action ? {
    scene: action.scene,
    minScore: String(action.minScore),
    maxScore: action.maxScore == null ? "" : String(action.maxScore),
    action: action.action,
    banDuration: String(action.banDuration),
    description: action.description,
  } : { ...EMPTY_FORM, scene: defaultScene });

  const needsDuration = form.action === "ban";
  const pending = createMut.isPending || updateMut.isPending;

  const submit = async () => {
    const minScore = Number(form.minScore);
    const maxScore = form.maxScore.trim() === "" ? null : Number(form.maxScore);
    if (!Number.isFinite(minScore) || minScore < 0) { toast.error("请填写有效的最低分"); return; }
    if (maxScore != null && (!Number.isFinite(maxScore) || maxScore < minScore)) {
      toast.error("最高分不能小于最低分"); return;
    }
    const banDuration = Number(form.banDuration) || 0;
    if (needsDuration && banDuration <= 0) {
      // 后端也会拒绝：封禁 0 秒等于没封，让它存进去只会制造一条
      // 看起来生效、实际不生效的策略。这里提前拦下少一次往返。
      toast.error("封禁动作必须指定大于 0 的封禁时长"); return;
    }

    const payload = {
      scene: form.scene, minScore, maxScore,
      action: form.action, banDuration, description: form.description.trim(),
    };
    try {
      if (action) {
        await updateMut.mutateAsync({ id: action.id, data: payload });
        toast.success("策略已更新");
      } else {
        await createMut.mutateAsync(payload);
        toast.success("策略已创建");
      }
      onClose();
    } catch (error) {
      // 区间重叠等冲突由后端判定并给出具体是与哪条策略冲突
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  return (
    <AlertDialog open onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent className="max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>{action ? "编辑处置策略" : "新建处置策略"}</AlertDialogTitle>
          <AlertDialogDescription>
            分数区间不能与同场景的其他策略重叠。
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <Label className="text-xs">场景</Label>
            <Select value={form.scene} onValueChange={(v) => setForm({ ...form, scene: v })} disabled={Boolean(action)}>
              <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {scenes.map((s) => <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>)}
              </SelectContent>
            </Select>
            {action && <p className="text-[10px] text-muted-foreground">场景不可修改，换场景请新建</p>}
          </div>
          <div className="space-y-1">
            <Label className="text-xs">处置动作</Label>
            <Select value={form.action} onValueChange={(v) => setForm({ ...form, action: v })}>
              <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                {actionCatalog.map((a) => <SelectItem key={a.value} value={a.value}>{a.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">最低分（含）</Label>
            <Input type="number" min={0} className="h-8 font-mono text-xs" value={form.minScore}
              onChange={(e) => setForm({ ...form, minScore: e.target.value })} />
          </div>
          <div className="space-y-1">
            <Label className="text-xs">最高分（含）</Label>
            <Input type="number" min={0} className="h-8 font-mono text-xs" value={form.maxScore}
              placeholder="留空表示无上限"
              onChange={(e) => setForm({ ...form, maxScore: e.target.value })} />
          </div>
          {needsDuration && (
            <div className="space-y-1">
              <Label className="text-xs">封禁时长（秒）<span className="text-rose-500">*</span></Label>
              <Input type="number" min={1} className="h-8 font-mono text-xs" value={form.banDuration}
                onChange={(e) => setForm({ ...form, banDuration: e.target.value })} />
              <p className="text-[10px] text-muted-foreground">
                {Number(form.banDuration) > 0 ? `约 ${fmtDuration(Number(form.banDuration))}` : "必须大于 0"}
              </p>
            </div>
          )}
          <div className={cn("space-y-1", needsDuration ? "" : "sm:col-span-2")}>
            <Label className="text-xs">说明</Label>
            <Input className="h-8 text-xs" value={form.description} placeholder="会显示在评估记录的处置详情里"
              onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </div>
        </div>

        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <Button size="sm" disabled={pending} onClick={submit}>
            {pending && <Loader2 className="size-3.5 animate-spin" />}
            {action ? "保存修改" : "创建策略"}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
