"use client";

import { useState } from "react";
import { AlertTriangle, FlaskConical, Pencil, Plus, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ChipInput, Field, Segmented } from "@/components/traffic/traffic-kit";
import type { RiskModel } from "@/lib/api/risk";
import { formatShortDateTime } from "@/lib/datetime";
import { useDeleteModel, usePredictModel, useRiskFeatures, useRiskModels, useUpdateModel, useUploadModel } from "@/lib/risk-hooks";

import { errorText, keyError } from "./studio/kit";

/**
 * 树模型（LightGBM / XGBoost）：上传文件、映射输入、试算。
 * 输入顺序就是模型特征顺序，可以是特征键（ip_scene_10m）或请求字段（amount、device.emulator、net.proxy）。
 */

const REQUEST_INPUTS = ["amount", "device.emulator", "device.rooted", "net.proxy", "net.vpn", "net.tor", "net.datacenter", "net.score", "client.bot", "client.headless", "hour", "weekday"];

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

function InputsEditor({ value, onChange, expected }: { value: string[]; onChange: (v: string[]) => void; expected?: number | null }) {
  const features = useRiskFeatures();
  const suggestions = [...(features.data ?? []).map((f) => f.key), ...REQUEST_INPUTS].filter((k) => !value.includes(k));
  const [showAll, setShowAll] = useState(false);
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <ChipInput value={value} onChange={onChange} placeholder="特征键或请求字段，回车添加" ariaLabel="模型输入" />
      <div className="flex items-center gap-2 text-xs">
        <span className={expected && value.length !== expected ? "text-red-600 dark:text-red-400" : "text-muted-foreground"}>
          已映射 {value.length}{expected ? ` / ${expected}` : ""}
        </span>
      </div>
      <div className="flex flex-wrap gap-1">
        {(showAll ? suggestions : suggestions.slice(0, 18)).map((k) => (
          <button key={k} type="button" onClick={() => onChange([...value, k])} className="rounded-md border px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
            + {k}
          </button>
        ))}
        {suggestions.length > 18 ? (
          <Button size="xs" variant="ghost" onClick={() => setShowAll(!showAll)}>{showAll ? "收起" : `更多 ${suggestions.length - 18}`}</Button>
        ) : null}
      </div>
    </div>
  );
}

function UploadDialog({ existing, onClose }: { existing: Set<string>; onClose: () => void }) {
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
        toast.success(`模型已上传，${m.nFeatures} 个输入`);
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
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field label="模型文件" error={touched ? fErr : null}>
            <Input type="file" accept=".txt,.json,.model,.bin" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="模型键（M.键）" error={touched ? kErr : null}>
              <Input className="font-mono" value={form.key} onChange={(e) => setForm({ ...form, key: e.target.value.trim() })} />
            </Field>
            <Field label="名称">
              <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </Field>
          </div>
          {replacing ? <p className="text-xs text-amber-600 dark:text-amber-400">同键模型将被替换</p> : null}
          <Field label="格式">
            <Segmented
              value={form.format}
              options={[{ value: "lightgbm" as const, label: "LightGBM" }, { value: "xgboost" as const, label: "XGBoost" }]}
              onChange={(format) => setForm({ ...form, format })}
            />
          </Field>
          <Field label="输入（按模型特征顺序）">
            <InputsEditor value={form.inputs} onChange={(inputs) => setForm({ ...form, inputs })} expected={expected} />
          </Field>
          {expected !== null ? (
            <div className="flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <span>模型有 {expected} 个输入特征，当前映射 {form.inputs.length} 个，请补齐后重新上传。</span>
            </div>
          ) : null}
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={form.transform} onCheckedChange={(transform) => setForm({ ...form, transform })} />
            套用模型自带的输出变换（如 sigmoid）
          </label>
          <Field label="说明">
            <Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={submit} disabled={upload.isPending}>
            <Upload />
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
    update.mutate({ key: model.key, ...form }, {
      onSuccess: () => {
        toast.success("模型已更新");
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>编辑模型 {model.key}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <Field label="名称"><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
          <Field label={`输入（需要 ${model.nFeatures} 个）`} error={mismatch ? `已映射 ${form.inputs.length} 个` : null}>
            <InputsEditor value={form.inputs} onChange={(inputs) => setForm({ ...form, inputs })} expected={model.nFeatures} />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={form.transform} onCheckedChange={(transform) => setForm({ ...form, transform })} />
            套用模型自带的输出变换
          </label>
          <Field label="说明"><Textarea rows={2} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={submit} disabled={update.isPending || mismatch}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TestDialog({ model, onClose }: { model: RiskModel; onClose: () => void }) {
  const predict = usePredictModel();
  const [values, setValues] = useState<Record<string, string>>({});
  const run = () => {
    const parsed = Object.fromEntries(model.inputs.map((k) => [k, Number(values[k] ?? 0) || 0]));
    predict.mutate({ key: model.key, values: parsed }, { onError: (e) => toast.error(errorText(e, "试算失败")) });
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>试算 {model.name}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-2 sm:grid-cols-2">
          {model.inputs.map((k) => (
            <div key={k} className="flex min-w-0 items-center gap-2">
              <span className="min-w-0 flex-1 truncate font-mono text-xs">{k}</span>
              <Input className="h-8 w-28 text-right tabular-nums" inputMode="decimal" placeholder="0" value={values[k] ?? ""} onChange={(e) => setValues({ ...values, [k]: e.target.value })} />
            </div>
          ))}
        </div>
        {predict.data ? (
          <div className="flex items-baseline gap-2 rounded-lg border bg-muted/30 px-4 py-3">
            <span className="text-xs text-muted-foreground">模型分</span>
            <span className="text-2xl font-semibold tabular-nums">{predict.data.score.toFixed(4)}</span>
          </div>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>关闭</Button>
          <Button onClick={run} disabled={predict.isPending}>
            <FlaskConical />
            试算
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function RiskModels() {
  const models = useRiskModels();
  const del = useDeleteModel();
  const [uploading, setUploading] = useState(false);
  const [editing, setEditing] = useState<RiskModel | null>(null);
  const [testing, setTesting] = useState<RiskModel | null>(null);
  const [deleting, setDeleting] = useState<RiskModel | null>(null);
  const list = models.data ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="min-w-0 flex-1 text-sm text-muted-foreground">{models.data ? `${list.length} 个模型` : null}</span>
        <Button size="sm" onClick={() => setUploading(true)}>
          <Plus />
          上传模型
        </Button>
      </div>

      {models.isLoading ? (
        <div className="flex flex-col gap-2">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-12 rounded-md" />)}</div>
      ) : models.isError ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">{errorText(models.error, "加载失败")}</div>
      ) : list.length === 0 ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">暂无模型</div>
      ) : (
        <div className="min-w-0 overflow-x-auto rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>模型</TableHead>
                <TableHead>格式</TableHead>
                <TableHead className="text-right">输入数</TableHead>
                <TableHead className="min-w-56">输入</TableHead>
                <TableHead className="text-right">大小</TableHead>
                <TableHead>摘要</TableHead>
                <TableHead>更新</TableHead>
                <TableHead className="w-28" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((m) => (
                <TableRow key={m.key}>
                  <TableCell>
                    <div className="text-sm">{m.name}</div>
                    <div className="font-mono text-[11px] text-muted-foreground">M.{m.key}</div>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      <Badge variant="outline" size="sm">{m.format === "lightgbm" ? "LightGBM" : "XGBoost"}</Badge>
                      {m.transform ? <Badge variant="secondary" size="sm">变换</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{m.nFeatures}</TableCell>
                  <TableCell>
                    <div className="flex max-w-96 flex-wrap gap-1">
                      {m.inputs.slice(0, 6).map((k, i) => <span key={i} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{k}</span>)}
                      {m.inputs.length > 6 ? <span className="text-[11px] text-muted-foreground">+{m.inputs.length - 6}</span> : null}
                    </div>
                  </TableCell>
                  <TableCell className="text-right text-sm tabular-nums">{formatBytes(m.sizeBytes)}</TableCell>
                  <TableCell className="font-mono text-[11px] text-muted-foreground">{m.checksum.slice(0, 10)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatShortDateTime(m.updatedAt)}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-0.5">
                      <Button size="icon-sm" variant="ghost" aria-label="试算" onClick={() => setTesting(m)}><FlaskConical className="size-3.5" /></Button>
                      <Button size="icon-sm" variant="ghost" aria-label="编辑" onClick={() => setEditing(m)}><Pencil className="size-3.5" /></Button>
                      <Button size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setDeleting(m)}><Trash2 className="size-3.5" /></Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {uploading ? <UploadDialog existing={new Set(list.map((m) => m.key))} onClose={() => setUploading(false)} /> : null}
      {editing ? <EditDialog key={editing.key} model={editing} onClose={() => setEditing(null)} /> : null}
      {testing ? <TestDialog key={testing.key} model={testing} onClose={() => setTesting(null)} /> : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除模型</AlertDialogTitle>
            <AlertDialogDescription>删除 M.{deleting?.key}。仍被场景使用的模型不能删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => deleting && del.mutate(deleting.key, {
                onSuccess: () => toast.success("模型已删除"),
                onError: (e) => toast.error(errorText(e, "删除失败")),
              })}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
