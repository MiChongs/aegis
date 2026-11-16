"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Loader2, Save } from "lucide-react";
import { Field } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { ApiError } from "@/lib/api/client";
import type { MusicTags, MusicTagsInput, MusicTrack } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useUpdateMusicTagsMutation } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { pct } from "./music-format";
import { EmptyLine, TrackCell } from "./music-kit";
import {
  ERA_OPTIONS,
  FLAG_OPTIONS,
  GENRE_OPTIONS,
  MOOD_OPTIONS,
  TAG_KIND_LABEL,
  TAG_MAX,
  TAG_SCENE_OPTIONS,
  VOCAL_OPTIONS,
  energyWord,
  sanitizeTagsInput,
  tagLabel,
  toTagsInput,
  type TagKind,
  type TagOption
} from "./music-tags";

/**
 * 内容标签的展示与人工修改：作品详情、内容理解的作品标签列表共用。
 * 曲风、情绪、场景分三种底色，同屏出现时一眼能分开；年代、人声、版本只用文字。
 */

const KIND_BADGE: Partial<Record<TagKind, "info" | "warning" | "success" | "secondary">> = {
  genres: "info",
  moods: "warning",
  scenes: "success",
  flags: "secondary"
};

/** 列表里的一组标签：只显示中文名，空时一条横线 */
export function TagChips({ kind, keys, className }: { kind: TagKind; keys: string[] | null | undefined; className?: string }) {
  if (!keys?.length) return <span className="text-xs text-muted-foreground">—</span>;
  return (
    <span className={cn("flex min-w-0 flex-wrap gap-1", className)}>
      {keys.map((key) => (
        <Badge key={key} variant={KIND_BADGE[kind] ?? "secondary"} size="sm" title={key}>
          {tagLabel(kind, key)}
        </Badge>
      ))}
    </span>
  );
}

/** 年代、人声、能量的一句话：「2000 年代，男声，能量适中」 */
export function tagsBrief(tags: MusicTags | null | undefined) {
  if (!tags) return "";
  const parts: string[] = [];
  if (tags.era) parts.push(tagLabel("era", tags.era));
  if (tags.vocal) parts.push(tagLabel("vocal", tags.vocal));
  if (Number.isFinite(tags.energy)) parts.push(`能量${energyWord(tags.energy)}`);
  return parts.join("，");
}

/** 作品详情里的完整标签：分行列出各类，下方注明来源与时间 */
export function TagsDetail({ tags }: { tags: MusicTags | null | undefined }) {
  if (!tags) return <EmptyLine>还没有打标签，内容理解开启后会排队处理</EmptyLine>;
  const rows: Array<{ kind: TagKind; node: React.ReactNode }> = [
    { kind: "genres", node: <TagChips kind="genres" keys={tags.genres} /> },
    { kind: "moods", node: <TagChips kind="moods" keys={tags.moods} /> },
    { kind: "scenes", node: <TagChips kind="scenes" keys={tags.scenes} /> },
    { kind: "era", node: <span className="text-xs">{tags.era ? tagLabel("era", tags.era) : "—"}</span> },
    { kind: "vocal", node: <span className="text-xs">{tags.vocal ? tagLabel("vocal", tags.vocal) : "—"}</span> },
    { kind: "flags", node: <TagChips kind="flags" keys={tags.flags} /> }
  ];
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <dl className="grid min-w-0 gap-x-6 gap-y-2.5 @2xl:grid-cols-2">
        {rows.map((row) => (
          <div key={row.kind} className="grid min-w-0 grid-cols-[3rem_minmax(0,1fr)] items-start gap-3">
            <dt className="pt-0.5 text-xs text-muted-foreground">{TAG_KIND_LABEL[row.kind]}</dt>
            <dd className="min-w-0">{row.node}</dd>
          </div>
        ))}
      </dl>
      <div className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4">
        <Gauge label="能量" value={tags.energy} text={`${energyWord(tags.energy)}，${Number.isFinite(tags.energy) ? tags.energy.toFixed(2) : "—"}`} />
        <Gauge label="可信度" value={tags.confidence} text={pct(tags.confidence, 0)} warn={tags.confidence < 0.5} />
      </div>
      <p className="text-[11px] text-muted-foreground">
        {tags.manual ? "人工修改" : tags.model ? `由 ${tags.model} 生成` : "自动生成"}
        {tags.taggedAt ? <span title={formatDateTimeMinute(tags.taggedAt)}>，{formatRelative(tags.taggedAt)}</span> : null}
        {!tags.manual && tags.confidence < 0.5 ? "；可信度偏低，大模型可能不熟悉这首歌，建议人工核对。" : "。"}
      </p>
    </div>
  );
}

function Gauge({ label, value, text, warn }: { label: string; value: number; text: string; warn?: boolean }) {
  const v = Number.isFinite(value) ? Math.min(1, Math.max(0, value)) : 0;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 rounded-xl border border-border p-2.5">
      <div className="flex min-w-0 items-baseline justify-between gap-2">
        <span className="truncate text-[11px] text-muted-foreground">{label}</span>
        <span className={cn("truncate text-xs font-medium tabular-nums", warn && "text-amber-600 dark:text-amber-400")}>{text}</span>
      </div>
      <span className="h-1.5 overflow-hidden rounded-full bg-muted">
        <span className={cn("block h-full rounded-full", warn ? "bg-amber-500" : "bg-sky-500")} style={{ width: `${v * 100}%` }} />
      </span>
    </div>
  );
}

/* ── 人工修改 ── */

const NONE = "none-selected";

/**
 * 人工修改标签对话框。曲风、情绪、场景各最多 3 个，选满后其余按钮不可点；年代、人声可以留空。
 * 只能从标签表里选，保存时再过一遍表；服务端改完重算语义向量。
 */
export function TagEditorDialog({
  appKey,
  target,
  onOpenChange
}: {
  appKey: string;
  target: { workKey: string; track: MusicTrack | null; tags: MusicTags | null } | null;
  onOpenChange: (open: boolean) => void;
}) {
  const mutation = useUpdateMusicTagsMutation(appKey);
  return (
    <Dialog open={Boolean(target)} onOpenChange={(open) => !mutation.isPending && onOpenChange(open)}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>修改标签</DialogTitle>
          <DialogDescription>保存后重新计算这首歌的语义向量，下次训练与推荐即生效。</DialogDescription>
        </DialogHeader>
        {/* 换一首歌就换一份表单：key 绑作品键，初值从当前标签派生 */}
        {target ? <EditorForm key={target.workKey} target={target} mutation={mutation} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function EditorForm({
  target,
  mutation,
  onDone
}: {
  target: { workKey: string; track: MusicTrack | null; tags: MusicTags | null };
  mutation: ReturnType<typeof useUpdateMusicTagsMutation>;
  onDone: () => void;
}) {
  const [form, setForm] = useState<MusicTagsInput>(() => toTagsInput(target.tags));
  const patch = (next: Partial<MusicTagsInput>) => setForm((f) => ({ ...f, ...next }));

  const save = async () => {
    try {
      await mutation.mutateAsync({ workKey: target.workKey, input: sanitizeTagsInput(form) });
      toast.success("标签已保存");
      onDone();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  return (
    <>
      <div className="flex min-w-0 flex-col gap-4">
        <TrackCell track={target.track} fallback={`作品 ${target.workKey}`} />
        <MultiPick kind="genres" options={GENRE_OPTIONS} value={form.genres} max={TAG_MAX} onChange={(genres) => patch({ genres })} />
        <MultiPick kind="moods" options={MOOD_OPTIONS} value={form.moods} max={TAG_MAX} onChange={(moods) => patch({ moods })} />
        <MultiPick kind="scenes" options={TAG_SCENE_OPTIONS} value={form.scenes} max={TAG_MAX} onChange={(scenes) => patch({ scenes })} />
        <div className="grid min-w-0 gap-4 sm:grid-cols-2">
          <Field label={TAG_KIND_LABEL.era}>
            <Select value={form.era || NONE} onValueChange={(v) => patch({ era: v === NONE ? "" : v })}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs" aria-label={TAG_KIND_LABEL.era}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>不确定</SelectItem>
                {ERA_OPTIONS.map((o) => (
                  <SelectItem key={o.key} value={o.key}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label={TAG_KIND_LABEL.vocal}>
            <Select value={form.vocal || NONE} onValueChange={(v) => patch({ vocal: v === NONE ? "" : v })}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs" aria-label={TAG_KIND_LABEL.vocal}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>不确定</SelectItem>
                {VOCAL_OPTIONS.map((o) => (
                  <SelectItem key={o.key} value={o.key}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
        <Field label={`能量：${energyWord(form.energy)}，${form.energy.toFixed(2)}`}>
          <Slider
            value={[form.energy]}
            min={0}
            max={1}
            step={0.05}
            onValueChange={([v]) => patch({ energy: Math.round((v ?? 0) * 100) / 100 })}
            aria-label="能量"
            className="py-1.5"
          />
        </Field>
        <MultiPick kind="flags" options={FLAG_OPTIONS} value={form.flags} onChange={(flags) => patch({ flags })} />
      </div>
      <DialogFooter>
        <Button variant="outline" disabled={mutation.isPending} onClick={onDone}>
          取消
        </Button>
        <Button disabled={mutation.isPending} onClick={save}>
          {mutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
          保存
        </Button>
      </DialogFooter>
    </>
  );
}

function MultiPick({
  kind,
  options,
  value,
  max,
  onChange
}: {
  kind: TagKind;
  options: TagOption[];
  value: string[];
  max?: number;
  onChange: (next: string[]) => void;
}) {
  const set = new Set(value);
  const full = typeof max === "number" && set.size >= max;
  const toggle = (key: string) => {
    const next = new Set(set);
    if (next.has(key)) next.delete(key);
    else if (!full) next.add(key);
    // 保持标签表的顺序
    onChange(options.filter((o) => next.has(o.key)).map((o) => o.key));
  };
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">
        {TAG_KIND_LABEL[kind]}
        {typeof max === "number" ? <span className="tabular-nums">（{set.size} / {max}）</span> : null}
      </span>
      <div className="flex flex-wrap gap-1.5">
        {options.map((o) => {
          const on = set.has(o.key);
          return (
            <Button
              key={o.key}
              type="button"
              size="xs"
              variant={on ? "secondary" : "outline"}
              aria-pressed={on}
              disabled={!on && full}
              onClick={() => toggle(o.key)}
            >
              {o.label}
            </Button>
          );
        })}
      </div>
    </div>
  );
}
