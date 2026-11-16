"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Loader2, Pencil, Plus, ShieldBan, Trash2 } from "lucide-react";
import { Field, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { MusicRule, MusicRuleInput, MusicRuleKind } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative, fromLocalInputValue, toLocalInputValue } from "@/lib/datetime";
import { useDeleteMusicRuleMutation, useMusicRulesQuery, useSaveMusicRuleMutation } from "@/lib/music-hooks";
import { RULE_KIND, SOURCE_LABEL, compact, pct } from "./music-format";
import { ArtistCell, EmptyLine, MusicError, RowsSkeleton, SceneToggles, TrackCell, musicArtistHref, musicTrackHref, scenesText, useNow } from "./music-kit";

/**
 * 运营干预规则（GET / POST / PUT / DELETE /rules）：全应用生效。
 *
 * - 屏蔽：直接过滤，恒为全部场景；加权：分数乘以权重（0.1–10）；置顶：插入到第几位（1–20），每人每天每条最多一次。
 * - 目标可以是作品（直接给作品键，或给「音源 + 歌曲 ID」由服务端换成作品键）或歌手名。
 * - 列表里的曝光与点击是近 7 天被这条规则影响的量（屏蔽恒为 0）。
 */

const KINDS: MusicRuleKind[] = ["block", "boost", "pin"];

type RuleState = "disabled" | "pending" | "ended" | "active";

const RULE_STATE: Record<RuleState, { label: string; tone: "secondary" | "outline" | "success" }> = {
  disabled: { label: "已停用", tone: "secondary" },
  pending: { label: "未开始", tone: "outline" },
  ended: { label: "已结束", tone: "outline" },
  active: { label: "生效中", tone: "success" }
};

function ruleState(rule: MusicRule, now: number): RuleState {
  if (!rule.enabled) return "disabled";
  if (rule.startsAt && now < rule.startsAt) return "pending";
  if (rule.endsAt && now > rule.endsAt) return "ended";
  return "active";
}

function ruleToInput(rule: MusicRule, patch?: Partial<MusicRuleInput>): MusicRuleInput {
  const target =
    rule.target.type === "artist" ? { type: "artist" as const, artist: rule.target.artist ?? "" } : { type: "work" as const, workKey: rule.target.workKey ?? "" };
  return {
    kind: rule.kind,
    target,
    scenes: rule.scenes,
    weight: rule.weight,
    position: rule.position,
    startsAt: rule.startsAt,
    endsAt: rule.endsAt,
    enabled: rule.enabled,
    note: rule.note,
    ...patch
  };
}

export function MusicRules({ appKey, canManage }: { appKey: string; canManage: boolean }) {
  const query = useMusicRulesQuery(appKey);
  const save = useSaveMusicRuleMutation(appKey);
  const remove = useDeleteMusicRuleMutation(appKey);
  const now = useNow();
  const [editing, setEditing] = useState<MusicRule | "new" | null>(null);
  const [deleting, setDeleting] = useState<MusicRule | null>(null);
  const [kindFilter, setKindFilter] = useState<"all" | MusicRuleKind>("all");
  const items = useMemo(() => query.data?.items ?? [], [query.data]);
  const shown = kindFilter === "all" ? items : items.filter((r) => r.kind === kindFilter);
  const activeCount = items.filter((r) => ruleState(r, now) === "active").length;

  const toggle = async (rule: MusicRule, enabled: boolean) => {
    try {
      await save.mutateAsync({ id: rule.id, input: ruleToInput(rule, { enabled }) });
      toast.success(enabled ? "规则已启用" : "规则已停用");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  const confirmDelete = async () => {
    if (!deleting) return;
    try {
      await remove.mutateAsync(deleting.id);
      toast.success("规则已删除");
      setDeleting(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  };

  return (
    <TabStack>
      <Panel
        icon={ShieldBan}
        tone="rose"
        title="运营干预"
        aside={
          <>
            <Segmented
              value={kindFilter}
              options={[{ value: "all" as const, label: "全部" }, ...KINDS.map((k) => ({ value: k, label: RULE_KIND[k].label }))]}
              onChange={setKindFilter}
              ariaLabel="规则类型"
            />
            {canManage ? (
              <Button size="sm" onClick={() => setEditing("new")}>
                <Plus />
                新建规则
              </Button>
            ) : null}
          </>
        }
      >
        {query.isError && !query.data ? (
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        ) : query.isLoading ? (
          <RowsSkeleton />
        ) : (
          <div className="flex min-w-0 flex-col gap-3">
            <p className="text-sm leading-5 text-muted-foreground">
              {items.length ? `共 ${items.length} 条规则，${activeCount} 条正在生效。规则对全应用所有用户生效，修改后下一次推荐即按新规则执行。` : "屏蔽、加权或置顶指定的歌曲与歌手，对全应用所有用户生效。"}
            </p>
            {!shown.length ? (
              <EmptyLine>{items.length ? "没有这一类规则" : "暂无规则"}</EmptyLine>
            ) : (
              <div className="min-w-0 overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>类型</TableHead>
                      <TableHead>目标</TableHead>
                      <TableHead>场景</TableHead>
                      <TableHead>生效时间</TableHead>
                      <TableHead className="text-right">近 7 天曝光</TableHead>
                      <TableHead className="text-right">点击</TableHead>
                      <TableHead>状态</TableHead>
                      {canManage ? <TableHead className="text-right">操作</TableHead> : null}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {shown.map((rule) => {
                      const state = ruleState(rule, now);
                      const kind = RULE_KIND[rule.kind];
                      return (
                        <TableRow key={rule.id}>
                          <TableCell>
                            <div className="flex flex-col items-start gap-0.5">
                              <Badge variant={kind?.tone ?? "secondary"} size="sm">
                                {kind?.label ?? rule.kind}
                              </Badge>
                              <span className="text-[11px] text-muted-foreground tabular-nums">
                                {rule.kind === "boost" ? `× ${rule.weight}` : rule.kind === "pin" ? `第 ${rule.position} 位` : "过滤"}
                              </span>
                            </div>
                          </TableCell>
                          <TableCell className="max-w-64">
                            <RuleTarget appKey={appKey} rule={rule} />
                            {rule.note ? (
                              <p className="mt-0.5 max-w-60 truncate text-[11px] text-muted-foreground" title={rule.note}>
                                {rule.note}
                              </p>
                            ) : null}
                          </TableCell>
                          <TableCell className="max-w-48 text-xs">
                            <span className="line-clamp-2">{rule.kind === "block" ? "全部场景" : scenesText(rule.scenes)}</span>
                          </TableCell>
                          <TableCell className="text-[11px] leading-4 whitespace-nowrap text-muted-foreground tabular-nums">
                            <span className="block">{rule.startsAt ? formatDateTimeMinute(rule.startsAt) : "立即"}</span>
                            <span className="block">至 {rule.endsAt ? formatDateTimeMinute(rule.endsAt) : "长期"}</span>
                          </TableCell>
                          <TableCell className="text-right text-xs tabular-nums">{rule.kind === "block" ? "—" : compact(rule.stats.served)}</TableCell>
                          <TableCell className="text-right text-xs tabular-nums">
                            {rule.kind === "block" ? (
                              "—"
                            ) : (
                              <>
                                {compact(rule.stats.clicks)}
                                <span className="block text-[11px] text-muted-foreground">
                                  {rule.stats.served ? `点击率 ${pct(rule.stats.clicks / rule.stats.served)}` : " "}
                                </span>
                              </>
                            )}
                          </TableCell>
                          <TableCell>
                            <div className="flex items-center gap-2">
                              {canManage ? (
                                <Switch
                                  checked={rule.enabled}
                                  disabled={save.isPending}
                                  onCheckedChange={(v) => void toggle(rule, v)}
                                  aria-label={rule.enabled ? "停用规则" : "启用规则"}
                                />
                              ) : null}
                              <Badge variant={RULE_STATE[state].tone} size="sm">
                                {RULE_STATE[state].label}
                              </Badge>
                            </div>
                            <span className="mt-0.5 block text-[11px] text-muted-foreground" title={formatDateTimeMinute(rule.updatedAt)}>
                              {rule.operator ? `${rule.operator}，` : ""}
                              {formatRelative(rule.updatedAt)}
                            </span>
                          </TableCell>
                          {canManage ? (
                            <TableCell className="text-right">
                              <div className="flex justify-end gap-1">
                                <Button size="icon-sm" variant="ghost" aria-label="编辑" title="编辑" onClick={() => setEditing(rule)}>
                                  <Pencil />
                                </Button>
                                <Button
                                  size="icon-sm"
                                  variant="ghost"
                                  aria-label="删除"
                                  title="删除"
                                  className="text-rose-600 hover:text-rose-700 dark:text-rose-400"
                                  onClick={() => setDeleting(rule)}
                                >
                                  <Trash2 />
                                </Button>
                              </div>
                            </TableCell>
                          ) : null}
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        )}
      </Panel>

      {editing ? (
        <RuleDialog key={editing === "new" ? "new" : editing.id} appKey={appKey} rule={editing === "new" ? null : editing} onClose={() => setEditing(null)} />
      ) : null}

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && !remove.isPending && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这条规则？</AlertDialogTitle>
            <AlertDialogDescription>删除后立即不再生效，近 7 天的影响统计一并丢弃。只想暂停时可以改为停用。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={remove.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={(e) => {
                e.preventDefault();
                void confirmDelete();
              }}
            >
              {remove.isPending ? <Loader2 className="animate-spin" /> : null}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

function RuleTarget({ appKey, rule }: { appKey: string; rule: MusicRule }) {
  const t = rule.target;
  if (t.type === "artist") return <ArtistCell name={t.artist ?? ""} href={t.artist ? musicArtistHref(appKey, t.artist) : undefined} sub="歌手" />;
  const track = t.track ?? (t.title ? { source: "", songId: "", title: t.title } : null);
  return <TrackCell track={track} fallback={t.workKey ? `作品 ${t.workKey}` : "未知作品"} href={t.workKey ? musicTrackHref(appKey, t.workKey) : undefined} />;
}

/* ── 新建 / 编辑 ── */

type TargetMode = "workKey" | "song" | "artist";

type RuleForm = {
  kind: MusicRuleKind;
  mode: TargetMode;
  workKey: string;
  source: string;
  songId: string;
  artist: string;
  scenes: string[];
  weight: string;
  position: string;
  startsAt: string;
  endsAt: string;
  enabled: boolean;
  note: string;
};

const TARGET_MODES: Array<{ value: TargetMode; label: string }> = [
  { value: "workKey", label: "作品键" },
  { value: "song", label: "音源与歌曲 ID" },
  { value: "artist", label: "歌手" }
];

function toForm(rule: MusicRule | null): RuleForm {
  if (!rule) {
    return {
      kind: "boost",
      mode: "workKey",
      workKey: "",
      source: "wy",
      songId: "",
      artist: "",
      scenes: [],
      weight: "1.5",
      position: "1",
      startsAt: "",
      endsAt: "",
      enabled: true,
      note: ""
    };
  }
  return {
    kind: rule.kind,
    mode: rule.target.type === "artist" ? "artist" : "workKey",
    workKey: rule.target.workKey ?? "",
    source: "wy",
    songId: "",
    artist: rule.target.artist ?? "",
    scenes: rule.scenes,
    weight: String(rule.weight || 1.5),
    position: String(rule.position || 1),
    startsAt: rule.startsAt ? toLocalInputValue(rule.startsAt) : "",
    endsAt: rule.endsAt ? toLocalInputValue(rule.endsAt) : "",
    enabled: rule.enabled,
    note: rule.note
  };
}

function localMs(value: string) {
  const iso = fromLocalInputValue(value);
  return iso ? Date.parse(iso) : null;
}

function validate(form: RuleForm): Record<string, string> {
  const errors: Record<string, string> = {};
  if (form.mode === "workKey" && !form.workKey.trim()) errors.target = "请填写作品键";
  if (form.mode === "song" && (!form.source || !form.songId.trim())) errors.target = "请选择音源并填写歌曲 ID";
  if (form.mode === "artist" && !form.artist.trim()) errors.target = "请填写歌手名";
  if (form.kind === "boost") {
    const w = Number(form.weight);
    if (!form.weight.trim() || !Number.isFinite(w) || w < 0.1 || w > 10) errors.weight = "范围 0.1 到 10";
  }
  if (form.kind === "pin") {
    const p = Number(form.position);
    if (!Number.isInteger(p) || p < 1 || p > 20) errors.position = "1 到 20 的整数";
  }
  const start = localMs(form.startsAt);
  const end = localMs(form.endsAt);
  if (start !== null && end !== null && end <= start) errors.endsAt = "结束时间要晚于开始时间";
  return errors;
}

function toInput(form: RuleForm): MusicRuleInput {
  const target: MusicRuleInput["target"] =
    form.mode === "artist"
      ? { type: "artist", artist: form.artist.trim() }
      : form.mode === "song"
        ? { type: "work", source: form.source, songId: form.songId.trim() }
        : { type: "work", workKey: form.workKey.trim() };
  return {
    kind: form.kind,
    target,
    // 屏蔽恒为全部场景
    scenes: form.kind === "block" ? [] : form.scenes,
    weight: Number(form.weight) || 1,
    position: Math.round(Number(form.position)) || 1,
    startsAt: localMs(form.startsAt),
    endsAt: localMs(form.endsAt),
    enabled: form.enabled,
    note: form.note.trim()
  };
}

function RuleDialog({ appKey, rule, onClose }: { appKey: string; rule: MusicRule | null; onClose: () => void }) {
  const [form, setForm] = useState<RuleForm>(() => toForm(rule));
  const [touched, setTouched] = useState(false);
  const save = useSaveMusicRuleMutation(appKey);
  const errors = validate(form);
  const shownErrors = touched ? errors : {};
  const patch = <K extends keyof RuleForm>(key: K, value: RuleForm[K]) => setForm((f) => ({ ...f, [key]: value }));

  const submit = async () => {
    setTouched(true);
    if (Object.keys(errors).length) return;
    try {
      await save.mutateAsync({ id: rule?.id, input: toInput(form) });
      toast.success(rule ? "规则已保存" : "规则已创建");
      onClose();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !save.isPending && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{rule ? "编辑规则" : "新建规则"}</DialogTitle>
          <DialogDescription>{RULE_KIND[form.kind].note}</DialogDescription>
        </DialogHeader>
        <div className="flex min-w-0 flex-col gap-4">
          <Field label="类型">
            <Segmented value={form.kind} options={KINDS.map((k) => ({ value: k, label: RULE_KIND[k].label }))} onChange={(v) => patch("kind", v)} ariaLabel="规则类型" />
          </Field>

          <Field label="目标" error={shownErrors.target}>
            <div className="flex min-w-0 flex-col gap-2">
              <Segmented value={form.mode} options={TARGET_MODES} onChange={(v) => patch("mode", v)} ariaLabel="目标类型" />
              {form.mode === "workKey" ? (
                <Input value={form.workKey} onChange={(e) => patch("workKey", e.target.value)} placeholder="作品键，可在作品详情页复制" className="font-mono text-xs" aria-label="作品键" />
              ) : form.mode === "song" ? (
                <div className="grid min-w-0 grid-cols-[9rem_minmax(0,1fr)] gap-2">
                  <Select value={form.source} onValueChange={(v) => patch("source", v)}>
                    <SelectTrigger className="w-full text-xs" aria-label="音源">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {Object.entries(SOURCE_LABEL).map(([value, label]) => (
                        <SelectItem key={value} value={value}>
                          {label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input value={form.songId} onChange={(e) => patch("songId", e.target.value)} placeholder="该音源的歌曲 ID" className="font-mono text-xs" aria-label="歌曲 ID" />
                </div>
              ) : (
                <Input value={form.artist} onChange={(e) => patch("artist", e.target.value)} placeholder="歌手名，与曲库里的写法一致" maxLength={64} aria-label="歌手名" />
              )}
              {form.mode === "song" ? <p className="text-[11px] text-muted-foreground">保存时由服务端换成作品键，曲库里没有这首时无法保存。</p> : null}
            </div>
          </Field>

          <Field label="场景">
            {form.kind === "block" ? (
              <p className="text-xs text-muted-foreground">屏蔽对全部场景生效。</p>
            ) : (
              <SceneToggles value={form.scenes} onChange={(v) => patch("scenes", v)} allowAll />
            )}
          </Field>

          {form.kind === "boost" ? (
            <Field label="权重（分数乘以该值，0.1 到 10；小于 1 为降权）" error={shownErrors.weight}>
              <Input value={form.weight} inputMode="decimal" onChange={(e) => patch("weight", e.target.value.replace(/[^\d.]/g, ""))} className="w-32 text-right tabular-nums" aria-label="权重" />
            </Field>
          ) : null}
          {form.kind === "pin" ? (
            <Field label="插入到第几位（1 到 20）" error={shownErrors.position}>
              <Input value={form.position} inputMode="numeric" onChange={(e) => patch("position", e.target.value.replace(/[^\d]/g, ""))} className="w-32 text-right tabular-nums" aria-label="位置" />
            </Field>
          ) : null}

          <div className="grid min-w-0 gap-3 sm:grid-cols-2">
            <Field label="开始时间（留空立即生效）">
              <Input type="datetime-local" value={form.startsAt} onChange={(e) => patch("startsAt", e.target.value)} aria-label="开始时间" />
            </Field>
            <Field label="结束时间（留空长期有效）" error={shownErrors.endsAt}>
              <Input type="datetime-local" value={form.endsAt} onChange={(e) => patch("endsAt", e.target.value)} aria-label="结束时间" />
            </Field>
          </div>

          <label className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2.5">
            <span className="text-sm">启用</span>
            <Switch checked={form.enabled} onCheckedChange={(v) => patch("enabled", v)} aria-label="启用" />
          </label>

          <Field label="备注">
            <Textarea value={form.note} onChange={(e) => patch("note", e.target.value)} placeholder="写明干预原因，便于其他人理解（选填）" maxLength={200} rows={2} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" disabled={save.isPending} onClick={onClose}>
            取消
          </Button>
          <Button disabled={save.isPending || (touched && Object.keys(errors).length > 0)} onClick={() => void submit()}>
            {save.isPending ? <Loader2 className="animate-spin" /> : null}
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
