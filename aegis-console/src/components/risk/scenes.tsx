"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Plus, Workflow } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/traffic/traffic-kit";
import type { Scene, SceneView } from "@/lib/api/risk";
import { formatShortDateTime } from "@/lib/datetime";
import { useRiskMeta, useRiskScenes, useRiskStatus, useSaveScene } from "@/lib/risk-hooks";

import { errorText, keyError } from "./studio/kit";

/** 场景的发布状态 → 徽标 */
export function releaseStatus(scene: Pick<SceneView, "state">, version?: string) {
  const st = scene.state;
  if (!st || !st.primaryId) return { label: "未发布", tone: "secondary" as const };
  const v = version;
  if (st.candidateId && st.shadow) return { label: "影子", tone: "info" as const };
  if (st.candidateId && st.canaryPercent > 0) return { label: `灰度 ${st.canaryPercent}%`, tone: "warning" as const };
  return { label: v ? `正式 ${v}` : "正式", tone: "success" as const };
}

function SceneCard({ scene, version, onOpen }: { scene: SceneView; version?: string; onOpen: () => void }) {
  const status = releaseStatus(scene, version);
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4 text-left transition-colors hover:border-foreground/20 hover:bg-accent/40 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <div className="flex min-w-0 items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{scene.name}</span>
            {scene.builtin ? <Badge variant="outline" size="sm">内置</Badge> : null}
          </div>
          <div className="truncate font-mono text-xs text-muted-foreground">{scene.key}</div>
        </div>
        <Badge variant={status.tone} size="sm">{status.label}</Badge>
      </div>
      <div className="flex items-baseline gap-1">
        <span className="text-2xl font-semibold tabular-nums">{scene.ruleCount ?? 0}</span>
        <span className="text-xs text-muted-foreground">条规则</span>
      </div>
      <div className="flex min-w-0 flex-wrap gap-1">
        <Badge variant={scene.captchaSupported ? "info" : "secondary"} size="sm">{scene.captchaSupported ? "可出验证码" : "无验证码"}</Badge>
        <Badge variant={scene.enforcement?.autoBanAllowed ? "danger" : "secondary"} size="sm">{scene.enforcement?.autoBanAllowed ? "允许自动封禁" : "只拦不封"}</Badge>
        <Badge variant={scene.failMode === "closed" ? "warning" : "secondary"} size="sm">{scene.failMode === "closed" ? "出错拒绝" : "出错放行"}</Badge>
        {scene.flow ? <Badge variant="outline" size="sm">决策流</Badge> : null}
        {scene.policy?.trim() ? <Badge variant="outline" size="sm">场景策略</Badge> : null}
      </div>
      <div className="mt-auto text-xs text-muted-foreground">更新于 {formatShortDateTime(scene.updatedAt)}</div>
    </button>
  );
}

function CreateSceneDialog({ open, onOpenChange, existing }: { open: boolean; onOpenChange: (open: boolean) => void; existing: Set<string> }) {
  const router = useRouter();
  const meta = useRiskMeta();
  const save = useSaveScene();
  const [form, setForm] = useState({ key: "", name: "", description: "" });
  const [touched, setTouched] = useState(false);
  const kErr = keyError(form.key) ?? (existing.has(form.key) ? "场景键已存在" : null);
  const nErr = form.name.trim() ? null : "必填";

  const submit = () => {
    setTouched(true);
    if (kErr || nErr) return;
    const scene: Partial<Scene> = {
      key: form.key,
      name: form.name.trim(),
      description: form.description.trim(),
      appIds: [],
      fields: [],
      timeoutMs: 200,
      failMode: "open",
      captchaSupported: false,
      defaultAction: "",
      enforcement: { targets: ["device"], durationSeconds: 86400, autoBanAllowed: false },
      listTypes: [],
      features: [],
      models: [],
      bands: meta.data?.defaultBands ?? [],
      flow: null,
      policy: "",
      enabled: true,
    };
    save.mutate({ key: form.key, scene }, {
      onSuccess: () => {
        toast.success("场景已创建");
        onOpenChange(false);
        router.push(`/risk-control/scenes/${encodeURIComponent(form.key)}`);
      },
      onError: (e) => toast.error(errorText(e, "创建失败")),
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新建场景</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <Field label="场景键" error={touched ? kErr : null}>
            <Input className="font-mono" value={form.key} placeholder="withdraw" onChange={(e) => setForm({ ...form, key: e.target.value.trim() })} />
          </Field>
          <Field label="名称" error={touched ? nErr : null}>
            <Input value={form.name} placeholder="提现" onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </Field>
          <Field label="说明">
            <Textarea rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button onClick={submit} disabled={save.isPending || !meta.data}>创建</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function RiskScenes() {
  const router = useRouter();
  const scenes = useRiskScenes();
  // 正式版本号取决策实例实际加载的那份（一次请求拿全部场景）
  const status = useRiskStatus();
  const versions = new Map((status.data?.decision?.scenes ?? []).map((s) => [s.scene, s.version]));
  const [creating, setCreating] = useState(false);
  const list = scenes.data ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex min-w-0 flex-1 items-center gap-2 text-sm text-muted-foreground">
          <Workflow className="size-4" />
          {scenes.data ? `${list.length} 个场景` : null}
        </div>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus />
          新建场景
        </Button>
      </div>

      {scenes.isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          {Array.from({ length: 8 }, (_, i) => <Skeleton key={i} className="h-48 rounded-xl" />)}
        </div>
      ) : scenes.isError ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">{errorText(scenes.error, "加载失败")}</div>
      ) : list.length === 0 ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">暂无场景</div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          {list.map((s) => (
            <SceneCard key={s.key} scene={s} version={versions.get(s.key)} onOpen={() => router.push(`/risk-control/scenes/${encodeURIComponent(s.key)}`)} />
          ))}
        </div>
      )}

      {creating ? <CreateSceneDialog open={creating} onOpenChange={setCreating} existing={new Set(list.map((s) => s.key))} /> : null}
    </div>
  );
}
