"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { AlertTriangle, ArrowRight, Brain, FileUp, FlaskConical, Loader2, Pencil, RotateCw, Trash2, Upload } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { ArcGauge, ChipInput, Field, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { Feature, RiskModel } from "@/lib/api/risk";
import { useDeleteModel, usePredictModel, useRiskFeatures, useRiskMeta, useRiskModels, useRiskScenes, useUpdateModel, useUploadModel } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { featureSentence } from "./features";
import { when } from "./kit/humanize";
import { errorText, keyError } from "./studio/kit";

/**
 * 模型：上传的树模型（LightGBM / XGBoost）给每次请求打一个分。
 *
 * 选中一个模型后画成「输入 → 模型 → 输出」：左边按顺序列出它读取的特征与请求字段（可直接填值试算），
 * 中间是模型本身，右边说明输出的含义并显示试算结果。
 */

const REQUEST_INPUTS = ["amount", "device.emulator", "device.rooted", "net.proxy", "net.vpn", "net.tor", "net.datacenter", "net.score", "client.bot", "client.headless", "hour", "weekday"];
const REQUEST_LABEL: Record<string, string> = {
  amount: "金额", "device.emulator": "是否模拟器", "device.rooted": "是否已越狱或 root", "net.proxy": "是否代理", "net.vpn": "是否 VPN",
  "net.tor": "是否 Tor", "net.datacenter": "是否机房 IP", "net.score": "IP 风险分", "client.bot": "是否自动化程序", "client.headless": "是否无头浏览器",
  hour: "当前小时", weekday: "星期几",
};
const FORMAT_LABEL: Record<RiskModel["format"], string> = { lightgbm: "LightGBM", xgboost: "XGBoost" };

function formatBytes(n: number) {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(1)} KB`;
  return `${n} B`;
}

/** 422：「模型需要 N 个输入，映射了 M 个」→ N */
function expectedInputs(error: unknown): number | null {
  if (!error || (error as { status?: number }).status !== 422) return null;
  const m = errorText(error).match(/需要\s*(\d+)\s*个输入/);
  return m ? Number(m[1]) : null;
}

/** 输入名 → 说法：特征用名称，请求字段用中文名 */
function useInputNamer() {
  const features = useRiskFeatures();
  const meta = useRiskMeta();
  return useMemo(() => {
    const byKey = new Map((features.data ?? []).map((f) => [f.key, f]));
    const vars = new Map((meta.data?.variables ?? []).map((v) => [v.name, v.label]));
    return (key: string): { label: string; feature?: Feature; known: boolean } => {
      const bare = key.startsWith("F.") ? key.slice(2) : key;
      const f = byKey.get(bare);
      if (f) return { label: f.name, feature: f, known: true };
      const label = REQUEST_LABEL[key] ?? vars.get(key);
      return label ? { label, known: true } : { label: key, known: false };
    };
  }, [features.data, meta.data]);
}

function outputMeaning(m: Pick<RiskModel, "transform" | "key">) {
  return m.transform ? "0 到 1 之间的风险概率，越接近 1 越可疑" : "模型原始分，未换算为概率";
}

/* ════════════════════════ 页面 ════════════════════════ */

export function RiskModels() {
  const models = useRiskModels();
  const scenes = useRiskScenes();
  const del = useDeleteModel();
  const [uploading, setUploading] = useState(false);
  const [editing, setEditing] = useState<RiskModel | null>(null);
  const [deleting, setDeleting] = useState<RiskModel | null>(null);
  const [picked, setPicked] = useState<string | null>(null);
  const list = useMemo(() => models.data ?? [], [models.data]);
  const current = list.find((m) => m.key === picked) ?? list[0] ?? null;
  const usedBy = useMemo(() => {
    const out = new Map<string, string[]>();
    for (const s of scenes.data ?? []) for (const k of s.models ?? []) out.set(k, [...(out.get(k) ?? []), s.name]);
    return out;
  }, [scenes.data]);

  return (
    <TabStack>
      <Panel
        icon={Brain}
        tone="violet"
        title="打分模型"
        aside={
          <Button size="sm" onClick={() => setUploading(true)}>
            <Upload />
            上传模型
          </Button>
        }
        bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
      >
        {models.isError && !models.data ? (
          <div className="flex flex-col items-center gap-3 border-t px-5 py-10 text-center">
            <AlertTriangle className="size-5 text-red-500" />
            <p className="text-sm text-muted-foreground">{errorText(models.error, "模型读取失败")}</p>
            <Button size="sm" variant="outline" onClick={() => models.refetch()}>
              <RotateCw />
              重试
            </Button>
          </div>
        ) : models.isLoading ? (
          <ModelsSkeleton />
        ) : list.length === 0 ? (
          <div className="flex flex-col items-center gap-1 border-t pb-6">
            <CardEmpty icon={Brain} title="还没有模型" description="上传训练好的 LightGBM 或 XGBoost 模型，规则即可使用它的打分" />
            <Button size="sm" onClick={() => setUploading(true)}>
              <Upload />
              上传模型
            </Button>
          </div>
        ) : (
          <>
            <div className="flex min-w-0 gap-2 overflow-x-auto border-t px-4 py-3 sm:px-5">
              {list.map((m) => {
                const on = current?.key === m.key;
                return (
                  <button
                    key={m.key}
                    type="button"
                    aria-pressed={on}
                    onClick={() => setPicked(m.key)}
                    className={cn(
                      "flex min-w-44 shrink-0 flex-col items-start gap-0.5 rounded-xl border px-3 py-2 text-left transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                      on ? "border-violet-500/50 bg-violet-500/5" : "border-border hover:bg-muted/50"
                    )}
                  >
                    <span className="max-w-56 truncate text-sm font-medium">{m.name || m.key}</span>
                    <span className="text-[11px] text-muted-foreground tabular-nums">
                      {FORMAT_LABEL[m.format] ?? m.format}，{m.nFeatures} 个输入
                    </span>
                  </button>
                );
              })}
            </div>
            {current ? (
              <ModelDetail
                key={current.key}
                model={current}
                usedBy={usedBy.get(current.key) ?? []}
                onEdit={() => setEditing(current)}
                onDelete={() => setDeleting(current)}
              />
            ) : null}
          </>
        )}
      </Panel>

      {uploading ? <UploadDialog existing={new Set(list.map((m) => m.key))} onClose={() => setUploading(false)} onUploaded={setPicked} /> : null}
      {editing ? <EditDialog key={editing.key} model={editing} onClose={() => setEditing(null)} /> : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除模型「{deleting?.name || deleting?.key}」</AlertDialogTitle>
            <AlertDialogDescription>
              {deleting && usedBy.get(deleting.key)?.length
                ? `${usedBy.get(deleting.key)!.join("、")}正在使用它，需先在这些场景中移除后才能删除。`
                : "删除后模型文件一并移除，无法恢复。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() =>
                deleting &&
                del.mutate(deleting.key, {
                  onSuccess: () => {
                    toast.success("模型已删除");
                    setPicked(null);
                  },
                  onError: (e) => toast.error(errorText(e, "删除失败")),
                })
              }
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

/* ── 详情：输入 → 模型 → 输出 ── */

function ModelDetail({ model: m, usedBy, onEdit, onDelete }: { model: RiskModel; usedBy: string[]; onEdit: () => void; onDelete: () => void }) {
  const namer = useInputNamer();
  const predict = usePredictModel();
  const [values, setValues] = useState<Record<string, string>>({});
  const updated = when(m.updatedAt);
  const score = predict.data?.score;

  const run = () => {
    const parsed = Object.fromEntries(m.inputs.map((k) => [k, Number(values[k] ?? 0) || 0]));
    predict.mutate({ key: m.key, values: parsed }, { onError: (e) => toast.error(errorText(e, "试算失败")) });
  };

  return (
    <div className="flex min-w-0 flex-col gap-5 border-t p-4 sm:p-5">
      <div className="flex min-w-0 flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h4 className="text-base font-semibold tracking-tight">{m.name || m.key}</h4>
            <span className="font-mono text-xs text-muted-foreground">M.{m.key}</span>
          </div>
          <p className="mt-1 text-sm text-foreground/85">
            读取 {m.inputs.length} 项数据，输出{outputMeaning(m)}。
          </p>
          {m.description ? <p className="mt-0.5 text-xs text-muted-foreground">{m.description}</p> : null}
        </div>
        <div className="flex shrink-0 gap-1">
          <Button size="sm" variant="outline" onClick={onEdit}>
            <Pencil />
            编辑
          </Button>
          <Button size="icon-sm" variant="ghost" className="hover:text-destructive" aria-label="删除" onClick={onDelete}>
            <Trash2 />
          </Button>
        </div>
      </div>

      <div className="grid min-w-0 items-stretch gap-3 @4xl:grid-cols-[minmax(0,1fr)_auto_13rem_auto_15rem]">
        {/* 输入 */}
        <section className="flex min-w-0 flex-col gap-2 rounded-xl border bg-muted/30 p-3">
          <header className="flex items-baseline justify-between gap-2 px-1">
            <h5 className="text-xs font-medium text-muted-foreground">输入（按模型顺序）</h5>
            <span className="text-[11px] text-muted-foreground">填值可试算</span>
          </header>
          <ol className="flex min-w-0 flex-col gap-1">
            {m.inputs.map((k, i) => {
              const n = namer(k);
              return (
                <li key={`${k}-${i}`} className="flex min-w-0 items-center gap-2.5 rounded-lg bg-card px-2.5 py-1.5">
                  <span className="w-5 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{i + 1}</span>
                  <span className="min-w-0 flex-1" title={n.feature ? featureSentence(n.feature) : undefined}>
                    <span className={cn("block truncate text-sm", !n.known && "font-mono text-xs")}>{n.label}</span>
                    {n.known ? <span className="block truncate font-mono text-[10px] text-muted-foreground">{k}</span> : null}
                  </span>
                  <Input
                    className="h-7 w-24 shrink-0 text-right text-xs tabular-nums"
                    inputMode="decimal"
                    placeholder="0"
                    aria-label={`${n.label} 的试算值`}
                    value={values[k] ?? ""}
                    onChange={(e) => setValues({ ...values, [k]: e.target.value })}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") run();
                    }}
                  />
                </li>
              );
            })}
          </ol>
        </section>

        <Connector />

        {/* 模型 */}
        <section className="flex min-w-0 flex-col items-center justify-center gap-3 rounded-xl border-2 border-violet-500/40 bg-violet-500/5 p-4 text-center">
          <span className="grid size-12 place-items-center rounded-2xl bg-violet-500/15 text-violet-600 dark:text-violet-400">
            <Brain className="size-6" />
          </span>
          <div className="flex flex-col items-center gap-1">
            <span className="text-sm font-semibold">{FORMAT_LABEL[m.format] ?? m.format}</span>
            <span className="text-[11px] text-muted-foreground">树模型</span>
          </div>
          <dl className="grid w-full grid-cols-2 gap-2 text-left">
            <div className="rounded-lg bg-card px-2 py-1.5">
              <dt className="text-[10px] text-muted-foreground">输入数</dt>
              <dd className="text-sm font-medium tabular-nums">{m.nFeatures}</dd>
            </div>
            <div className="rounded-lg bg-card px-2 py-1.5">
              <dt className="text-[10px] text-muted-foreground">文件</dt>
              <dd className="text-sm font-medium tabular-nums">{formatBytes(m.sizeBytes)}</dd>
            </div>
          </dl>
          {m.transform ? (
            <Badge variant="secondary" size="sm">
              输出已换算为概率
            </Badge>
          ) : null}
        </section>

        <Connector />

        {/* 输出 */}
        <section className="flex min-w-0 flex-col items-center justify-between gap-3 rounded-xl border bg-muted/30 p-4 text-center">
          <h5 className="text-xs font-medium text-muted-foreground">输出</h5>
          {m.transform ? (
            <ArcGauge
              value={score ?? 0}
              max={1}
              label={score === undefined ? "尚未试算" : score >= 0.8 ? "高度可疑" : score >= 0.5 ? "较可疑" : "风险较低"}
              display={score === undefined ? <span className="text-muted-foreground">—</span> : score.toFixed(3)}
              size={140}
            />
          ) : (
            <div className="flex flex-col items-center gap-1 py-4">
              <span className="text-3xl font-semibold tracking-tight tabular-nums">{score === undefined ? <span className="text-muted-foreground">—</span> : score.toFixed(4)}</span>
              <span className="text-xs text-muted-foreground">{score === undefined ? "尚未试算" : "原始分"}</span>
            </div>
          )}
          <p className="text-[11px] leading-4 text-muted-foreground">{outputMeaning(m)}</p>
          <Button size="sm" className="w-full" disabled={predict.isPending} onClick={run}>
            {predict.isPending ? <Loader2 className="animate-spin" /> : <FlaskConical />}
            试算
          </Button>
        </section>
      </div>

      <div className="flex min-w-0 flex-wrap gap-x-6 gap-y-1.5 text-xs text-muted-foreground">
        <span>{usedBy.length ? <span className="text-foreground/85">{usedBy.join("、")}在用</span> : "暂无场景使用"}</span>
        <span title={updated.absolute}>
          {updated.relative ? `${updated.relative}更新${m.updatedBy ? `，${m.updatedBy}` : ""}` : null}
        </span>
        <span className="font-mono" title={m.checksum}>
          校验值 {m.checksum.slice(0, 12)}
        </span>
      </div>
    </div>
  );
}

function Connector() {
  return (
    <div aria-hidden className="hidden items-center justify-center text-muted-foreground @4xl:flex">
      <span className="h-px w-3 bg-border" />
      <ArrowRight className="size-4" />
    </div>
  );
}

function ModelsSkeleton() {
  return (
    <>
      <div className="flex gap-2 border-t px-4 py-3 sm:px-5">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-12 w-44 rounded-xl" />
        ))}
      </div>
      <div className="flex flex-col gap-5 border-t p-4 sm:p-5">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-4 w-full max-w-md" />
        <div className="grid gap-3 @4xl:grid-cols-[minmax(0,1fr)_13rem_15rem]">
          <div className="flex flex-col gap-1 rounded-xl border p-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
          <Skeleton className="min-h-48 rounded-xl" />
          <Skeleton className="min-h-48 rounded-xl" />
        </div>
      </div>
    </>
  );
}

/* ════════════════════════ 上传与编辑 ════════════════════════ */

function InputsEditor({ value, onChange, expected }: { value: string[]; onChange: (v: string[]) => void; expected?: number | null }) {
  const features = useRiskFeatures();
  const namer = useInputNamer();
  const suggestions = [...(features.data ?? []).map((f) => f.key), ...REQUEST_INPUTS].filter((k) => !value.includes(k));
  const [showAll, setShowAll] = useState(false);
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <ChipInput value={value} onChange={onChange} placeholder="特征键或请求字段，回车添加" ariaLabel="模型输入" />
      <span className={cn("text-xs", expected && value.length !== expected ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
        已填 {value.length} 项{expected ? `，模型需要 ${expected} 项` : ""}
      </span>
      <div className="flex flex-wrap gap-1">
        {(showAll ? suggestions : suggestions.slice(0, 18)).map((k) => (
          <button
            key={k}
            type="button"
            title={k}
            onClick={() => onChange([...value, k])}
            className="rounded-md border px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            + {namer(k).label}
          </button>
        ))}
        {suggestions.length > 18 ? (
          <Button size="xs" variant="ghost" onClick={() => setShowAll(!showAll)}>
            {showAll ? "收起" : `更多 ${suggestions.length - 18} 项`}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function UploadDialog({ existing, onClose, onUploaded }: { existing: Set<string>; onClose: () => void; onUploaded: (key: string) => void }) {
  const upload = useUploadModel();
  const [file, setFile] = useState<File | null>(null);
  const [form, setForm] = useState({ key: "", name: "", format: "lightgbm" as RiskModel["format"], inputs: [] as string[], transform: true, description: "" });
  const [touched, setTouched] = useState(false);
  const kErr = keyError(form.key);
  const fErr = file ? null : "请选择模型文件";
  const replacing = existing.has(form.key);
  const expected = expectedInputs(upload.error);

  const submit = () => {
    setTouched(true);
    if (kErr || fErr || !file) return;
    const fd = new FormData();
    fd.append("file", file);
    fd.append("key", form.key);
    fd.append("name", form.name.trim());
    fd.append("format", form.format);
    fd.append("inputs", form.inputs.join(","));
    fd.append("transform", String(form.transform));
    fd.append("description", form.description.trim());
    upload.mutate(fd, {
      onSuccess: (m) => {
        toast.success(`模型已上传，读取 ${m.nFeatures} 项输入`);
        onUploaded(m.key);
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "上传失败")),
    });
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>上传模型</DialogTitle>
          <DialogDescription>上传后在场景里引用它，规则即可使用模型分。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field label="模型文件" error={touched ? fErr : null}>
            <label
              className={cn(
                "flex cursor-pointer items-center gap-3 rounded-xl border border-dashed px-3 py-3 transition-colors hover:bg-muted/40",
                file && "border-solid border-violet-500/40 bg-violet-500/5"
              )}
            >
              <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                <FileUp className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{file ? file.name : "选择文件"}</span>
                <span className="block text-[11px] text-muted-foreground">{file ? formatBytes(file.size) : "支持 .txt、.json、.model、.bin"}</span>
              </span>
              <input type="file" className="sr-only" accept=".txt,.json,.model,.bin" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            </label>
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="名称">
              <Input value={form.name} placeholder="登录风险模型" onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </Field>
            <Field label="模型键（规则里写作 M.键）" error={touched ? kErr : null}>
              <Input className="font-mono" value={form.key} placeholder="login_gbm" onChange={(e) => setForm({ ...form, key: e.target.value.trim() })} />
            </Field>
          </div>
          {replacing ? <p className="text-xs text-amber-600 dark:text-amber-400">已有同键模型，上传后将替换它</p> : null}
          <Field label="格式">
            <Segmented
              value={form.format}
              options={[
                { value: "lightgbm" as const, label: "LightGBM" },
                { value: "xgboost" as const, label: "XGBoost" },
              ]}
              onChange={(format) => setForm({ ...form, format })}
            />
          </Field>
          <Field label="输入（与训练时的特征顺序一致）">
            <InputsEditor value={form.inputs} onChange={(inputs) => setForm({ ...form, inputs })} expected={expected} />
          </Field>
          {expected !== null ? (
            <div className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <span>
                模型需要 {expected} 项输入，当前填了 {form.inputs.length} 项，补齐后重新上传。
              </span>
            </div>
          ) : null}
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={form.transform} onCheckedChange={(transform) => setForm({ ...form, transform })} />
            套用模型自带的输出换算（二分类模型得到 0 到 1 的概率）
          </label>
          <Field label="说明">
            <Textarea rows={2} value={form.description} placeholder="选填，如训练数据与用途" onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={upload.isPending}>
            {upload.isPending ? <Loader2 className="animate-spin" /> : <Upload />}
            上传
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function EditDialog({ model, onClose }: { model: RiskModel; onClose: () => void }) {
  const update = useUpdateModel();
  const [form, setForm] = useState({ name: model.name, description: model.description ?? "", inputs: model.inputs, transform: model.transform });
  const mismatch = form.inputs.length !== model.nFeatures;
  const submit = () => {
    if (mismatch) return;
    update.mutate(
      { key: model.key, ...form },
      {
        onSuccess: () => {
          toast.success("模型已更新");
          onClose();
        },
        onError: (e) => toast.error(errorText(e, "保存失败")),
      }
    );
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>编辑「{model.name || model.key}」</DialogTitle>
          <DialogDescription>模型文件不变，只调整名称、输入对应关系与输出换算。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field label="名称">
            <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </Field>
          <Field label={`输入（模型需要 ${model.nFeatures} 项）`} error={mismatch ? `当前填了 ${form.inputs.length} 项` : null}>
            <InputsEditor value={form.inputs} onChange={(inputs) => setForm({ ...form, inputs })} expected={model.nFeatures} />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={form.transform} onCheckedChange={(transform) => setForm({ ...form, transform })} />
            套用模型自带的输出换算（二分类模型得到 0 到 1 的概率）
          </label>
          <Field label="说明">
            <Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={update.isPending || mismatch}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
