"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Plus, X } from "lucide-react";
import { Field, FieldError } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { MusicBlend, MusicExperiment, MusicExperimentInput, MusicExperimentOverrides } from "@/lib/api/music";
import { useMusicModelsQuery, useSaveMusicExperimentMutation } from "@/lib/music-hooks";
import { CHANNEL_LABEL } from "./music-format";
import { TrafficSplit, VARIANT_COLOR } from "./music-experiment-parts";
import { SceneToggles } from "./music-kit";

/**
 * 实验编辑器：名称、说明、场景（至少一个）与变体。变体键按顺序自动取 A、B、C…，A 为对照。
 * 每个变体的覆盖参数留空即「沿用应用配置」，只有填了的才随请求覆盖；取值范围与应用配置一致
 * （融合权重 0–5、探索占比 0–0.5、同歌手上限 1–10），流量合计不超过 100%。
 */

const BLEND_KEYS: Array<keyof MusicBlend> = ["als", "itemcf", "artist", "trending", "explore"];
const KEYS = "ABCDEF";
type Tri = "inherit" | "on" | "off";

type VariantForm = {
  name: string;
  traffic: string;
  blend: Record<keyof MusicBlend, string>;
  exploreRatio: string;
  artistCap: string;
  ranker: Tri;
  foldIn: Tri;
  /** "" = 跟随当前发布的模型 */
  modelRunId: string;
};

type Form = { name: string; description: string; scenes: string[]; variants: VariantForm[] };

const EMPTY_BLEND = { als: "", itemcf: "", artist: "", trending: "", explore: "" };

function emptyVariant(name: string, traffic: string): VariantForm {
  return { name, traffic, blend: { ...EMPTY_BLEND }, exploreRatio: "", artistCap: "", ranker: "inherit", foldIn: "inherit", modelRunId: "" };
}

const str = (v: number | undefined) => (typeof v === "number" ? String(v) : "");
const tri = (v: boolean | undefined): Tri => (v === undefined ? "inherit" : v ? "on" : "off");

function toForm(exp: MusicExperiment | null): Form {
  if (!exp) return { name: "", description: "", scenes: ["home"], variants: [emptyVariant("对照", "50"), emptyVariant("实验组", "50")] };
  return {
    name: exp.name,
    description: exp.description,
    scenes: exp.scenes,
    variants: exp.variants.map((v) => {
      const o = v.overrides ?? {};
      return {
        name: v.name,
        traffic: String(v.traffic),
        blend: {
          als: str(o.blend?.als),
          itemcf: str(o.blend?.itemcf),
          artist: str(o.blend?.artist),
          trending: str(o.blend?.trending),
          explore: str(o.blend?.explore)
        },
        exploreRatio: str(o.exploreRatio),
        artistCap: str(o.artistCap),
        ranker: tri(o.ranker),
        foldIn: tri(o.foldIn),
        modelRunId: o.modelRunId ? String(o.modelRunId) : ""
      };
    })
  };
}

/** 选填数值：空串合法；有值时校验范围 */
function rangeError(raw: string, min: number, max: number, integer = false) {
  if (!raw.trim()) return undefined;
  const v = Number(raw);
  if (!Number.isFinite(v) || v < min || v > max || (integer && !Number.isInteger(v))) return `${min} 到 ${max}${integer ? " 的整数" : ""}`;
  return undefined;
}

type Errors = { name?: string; scenes?: string; traffic?: string; variants: Array<Record<string, string>> };

function validate(form: Form): { errors: Errors; ok: boolean } {
  const errors: Errors = { variants: [] };
  if (!form.name.trim()) errors.name = "请填写名称";
  if (!form.scenes.length) errors.scenes = "至少选一个场景";
  if (form.variants.length < 2) errors.traffic = "至少需要对照组和一个实验组";
  let sum = 0;
  form.variants.forEach((v, i) => {
    const e: Record<string, string> = {};
    if (!v.name.trim()) e.name = "请填写变体名称";
    const t = Number(v.traffic);
    if (!v.traffic.trim() || !Number.isInteger(t) || t < 1 || t > 100) e.traffic = "1 到 100 的整数";
    else sum += t;
    for (const k of BLEND_KEYS) {
      const msg = rangeError(v.blend[k], 0, 5);
      if (msg) e[`blend.${k}`] = msg;
    }
    const er = rangeError(v.exploreRatio, 0, 0.5);
    if (er) e.exploreRatio = er;
    const ac = rangeError(v.artistCap, 1, 10, true);
    if (ac) e.artistCap = ac;
    errors.variants[i] = e;
  });
  if (!errors.traffic && sum > 100) errors.traffic = `流量合计 ${sum}%，不能超过 100%`;
  const ok = !errors.name && !errors.scenes && !errors.traffic && errors.variants.every((e) => !Object.keys(e).length);
  return { errors, ok };
}

function toInput(form: Form): MusicExperimentInput {
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    scenes: form.scenes,
    variants: form.variants.map((v, i) => {
      const overrides: MusicExperimentOverrides = {};
      const blend: Partial<MusicBlend> = {};
      for (const k of BLEND_KEYS) if (v.blend[k].trim()) blend[k] = Number(v.blend[k]);
      if (Object.keys(blend).length) overrides.blend = blend;
      if (v.exploreRatio.trim()) overrides.exploreRatio = Number(v.exploreRatio);
      if (v.artistCap.trim()) overrides.artistCap = Number(v.artistCap);
      if (v.ranker !== "inherit") overrides.ranker = v.ranker === "on";
      if (v.foldIn !== "inherit") overrides.foldIn = v.foldIn === "on";
      if (v.modelRunId) overrides.modelRunId = Number(v.modelRunId);
      return { key: KEYS[i], name: v.name.trim(), traffic: Number(v.traffic), overrides };
    })
  };
}

export function MusicExperimentEditor({ appKey, experiment, onClose }: { appKey: string; experiment: MusicExperiment | null; onClose: () => void }) {
  const [form, setForm] = useState<Form>(() => toForm(experiment));
  const [touched, setTouched] = useState(false);
  const save = useSaveMusicExperimentMutation(appKey);
  const models = useMusicModelsQuery(appKey, 1, 50);
  const succeeded = (models.data?.items ?? []).filter((r) => r.status === "succeeded");
  const { errors, ok } = validate(form);
  const shown: Errors = touched ? errors : { variants: [] };

  const patchVariant = (index: number, patch: Partial<VariantForm>) =>
    setForm((f) => ({ ...f, variants: f.variants.map((v, i) => (i === index ? { ...v, ...patch } : v)) }));

  const submit = async () => {
    setTouched(true);
    if (!ok) return;
    try {
      await save.mutateAsync({ id: experiment?.id, input: toInput(form) });
      toast.success(experiment ? "实验已保存" : "实验已创建");
      onClose();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !save.isPending && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{experiment ? "编辑实验" : "新建实验"}</DialogTitle>
          <DialogDescription>
            {experiment?.status === "running" ? "实验正在运行，修改变体或流量会立即改变分桶，之前的数据与之后的数据不再可比。" : "保存后为草稿，点「开始」才会生效。"}
          </DialogDescription>
        </DialogHeader>

        <div className="flex min-w-0 flex-col gap-4">
          <div className="grid min-w-0 gap-3 sm:grid-cols-2">
            <Field label="名称" error={shown.name}>
              <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} maxLength={64} placeholder="如「提高探索占比」" />
            </Field>
            <Field label="说明">
              <Textarea
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                maxLength={500}
                rows={1}
                placeholder="实验假设与预期（选填）"
                className="min-h-9"
              />
            </Field>
          </div>

          <Field label="场景" error={shown.scenes}>
            <SceneToggles value={form.scenes} onChange={(scenes) => setForm((f) => ({ ...f, scenes }))} invalid={Boolean(shown.scenes)} />
          </Field>

          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">变体与流量</span>
              <Button
                type="button"
                size="xs"
                variant="outline"
                disabled={form.variants.length >= KEYS.length}
                onClick={() => setForm((f) => ({ ...f, variants: [...f.variants, emptyVariant(`实验组 ${KEYS[f.variants.length]}`, "")] }))}
              >
                <Plus />
                添加变体
              </Button>
            </div>
            <TrafficSplit variants={form.variants.map((v, i) => ({ key: KEYS[i], traffic: Number(v.traffic) || 0 }))} />
            <FieldError message={shown.traffic} />
          </div>

          <ol className="flex min-w-0 flex-col gap-3">
            {form.variants.map((v, i) => {
              const e = shown.variants[i] ?? {};
              return (
                <li key={i} className="flex min-w-0 flex-col gap-3 rounded-xl border border-border p-3">
                  <div className="flex min-w-0 items-start gap-2">
                    <span
                      className="mt-1.5 grid size-6 shrink-0 place-items-center rounded-md text-xs font-semibold text-white"
                      style={{ background: VARIANT_COLOR[i % VARIANT_COLOR.length] }}
                    >
                      {KEYS[i]}
                    </span>
                    <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-[minmax(0,1fr)_7rem]">
                      <Field label={i === 0 ? "名称（对照组）" : "名称"} error={e.name}>
                        <Input value={v.name} onChange={(ev) => patchVariant(i, { name: ev.target.value })} maxLength={32} className="h-8 text-xs" />
                      </Field>
                      <Field label="流量" error={e.traffic}>
                        <div className="relative flex items-center">
                          <Input
                            value={v.traffic}
                            inputMode="numeric"
                            onChange={(ev) => patchVariant(i, { traffic: ev.target.value.replace(/[^\d]/g, "") })}
                            className="h-8 pr-7 text-right text-xs tabular-nums"
                            aria-label={`变体 ${KEYS[i]} 流量`}
                          />
                          <span className="pointer-events-none absolute right-2.5 text-xs text-muted-foreground">%</span>
                        </div>
                      </Field>
                    </div>
                    {form.variants.length > 2 && i > 0 ? (
                      <Button
                        type="button"
                        size="icon-sm"
                        variant="ghost"
                        className="mt-5"
                        aria-label={`移除变体 ${KEYS[i]}`}
                        onClick={() => setForm((f) => ({ ...f, variants: f.variants.filter((_, j) => j !== i) }))}
                      >
                        <X />
                      </Button>
                    ) : null}
                  </div>

                  <div className="flex min-w-0 flex-col gap-2 pl-8">
                    <span className="text-[11px] text-muted-foreground">覆盖参数（留空沿用应用配置）</span>
                    <div className="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-5">
                      {BLEND_KEYS.map((k) => (
                        <OptionalNumber
                          key={k}
                          label={`${CHANNEL_LABEL[k]}权重`}
                          value={v.blend[k]}
                          error={e[`blend.${k}`]}
                          onChange={(val) => patchVariant(i, { blend: { ...v.blend, [k]: val } })}
                        />
                      ))}
                    </div>
                    <div className="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-5">
                      <OptionalNumber label="探索占比" value={v.exploreRatio} error={e.exploreRatio} onChange={(val) => patchVariant(i, { exploreRatio: val })} />
                      <OptionalNumber label="同歌手最多" value={v.artistCap} error={e.artistCap} onChange={(val) => patchVariant(i, { artistCap: val })} />
                      <TriSelect label="精排" value={v.ranker} onChange={(val) => patchVariant(i, { ranker: val })} />
                      <TriSelect label="实时折入" value={v.foldIn} onChange={(val) => patchVariant(i, { foldIn: val })} />
                      <Field label="模型版本">
                        <Select value={v.modelRunId || "current"} onValueChange={(val) => patchVariant(i, { modelRunId: val === "current" ? "" : val })}>
                          <SelectTrigger size="sm" className="h-8 w-full text-xs" aria-label="模型版本">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="current">当前发布</SelectItem>
                            {v.modelRunId && !succeeded.some((r) => String(r.id) === v.modelRunId) ? (
                              <SelectItem value={v.modelRunId}>第 {v.modelRunId} 版</SelectItem>
                            ) : null}
                            {succeeded.map((r) => (
                              <SelectItem key={r.id} value={String(r.id)}>
                                第 {r.id} 版{r.current ? "（当前）" : ""}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </Field>
                    </div>
                  </div>
                </li>
              );
            })}
          </ol>
        </div>

        <DialogFooter>
          <Button variant="outline" disabled={save.isPending} onClick={onClose}>
            取消
          </Button>
          <Button disabled={save.isPending || (touched && !ok)} onClick={() => void submit()}>
            {save.isPending ? <Loader2 className="animate-spin" /> : null}
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function OptionalNumber({ label, value, error, onChange }: { label: string; value: string; error?: string; onChange: (value: string) => void }) {
  return (
    <Field label={label} error={error}>
      <Input
        value={value}
        inputMode="decimal"
        placeholder="沿用"
        onChange={(e) => onChange(e.target.value.replace(/[^\d.]/g, ""))}
        aria-label={label}
        aria-invalid={Boolean(error) || undefined}
        className="h-8 text-right text-xs tabular-nums"
      />
    </Field>
  );
}

function TriSelect({ label, value, onChange }: { label: string; value: Tri; onChange: (value: Tri) => void }) {
  return (
    <Field label={label}>
      <Select value={value} onValueChange={(v) => onChange(v as Tri)}>
        <SelectTrigger size="sm" className="h-8 w-full text-xs" aria-label={label}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="inherit">沿用</SelectItem>
          <SelectItem value="on">开启</SelectItem>
          <SelectItem value="off">关闭</SelectItem>
        </SelectContent>
      </Select>
    </Field>
  );
}
