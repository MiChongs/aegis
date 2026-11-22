"use client";

import { useMemo, useState } from "react";
import { Check, Film, ImagePlus, Loader2, PencilLine, Plus, Puzzle, Replace, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
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
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { QuickChips } from "@/components/users/detail/user-assets-adjust";
import {
  FilterBar,
  FilterSelect,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  errorMessage,
  type Column
} from "@/components/users/detail/kit";
import type { CosmeticAsset, CosmeticCatalog, WorkshopPart, WorkshopPartInput, WorkshopPartSlot } from "@/lib/api/cosmetics";
import { useDeleteWorkshopPartMutation, useSaveWorkshopPartMutation, useWorkshopPartsQuery } from "@/lib/cosmetic-hooks";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { AssetThumb } from "./cosmetics-assets-panel";
import { SLOT_META, WORKSHOP_PART_SLOTS, formatCount, slotLabel } from "./cosmetic-shared";
import { AssetPickerDialog } from "./designer/designer-assets";
import { SpriteFrames, SpriteGrid } from "./workshop-part-visuals";

/**
 * 工坊零件库（docs/cosmetics.md「自定义工坊 / 零件」）：运营从素材库里挑出图片或序列帧，登记成用户在工坊里
 * 能用的零件。列表含停用的，按排序值升序；筛选在本地做（一个应用最多 500 个零件，接口一次全给）。
 *
 * - 同一素材只能登记一次（40953），选素材时把已是零件的置顶标出来。
 * - 序列帧素材必须给切分（行列、帧数、帧率），作品里的序列帧图层必须照用；弹窗里叠切分线核对。
 * - 停用或改了槽位后已有作品照常能戴，只是再保存时会被拒；零件还在时素材不能删（40958）。
 * 校验与后端 `ValidatePartInput` 同规则同说法，提交以后端为准。
 */

const MAX_NAME = 32;
const MAX_CATEGORY = 16;
const MAX_FRAMES_FALLBACK = 256;

function bySortOrder(a: WorkshopPart, b: WorkshopPart) {
  return a.sortOrder - b.sortOrder || a.id - b.id;
}

function isSprite(part: Pick<WorkshopPart, "frameCount">) {
  return (part.frameCount ?? 0) > 0;
}

function cutText(part: Pick<WorkshopPart, "columns" | "rows" | "frameCount" | "fps">) {
  if (!isSprite(part)) return "图片";
  return `${part.columns}×${part.rows}，${part.frameCount} 帧，${part.fps} fps`;
}

function inputOf(part: WorkshopPart, patch: Partial<WorkshopPartInput> = {}): WorkshopPartInput {
  return {
    assetId: part.assetId,
    name: part.name,
    category: part.category,
    slots: part.slots,
    columns: part.columns ?? 0,
    rows: part.rows ?? 0,
    frameCount: part.frameCount ?? 0,
    fps: part.fps ?? 0,
    sortOrder: part.sortOrder,
    enabled: part.enabled,
    ...patch
  };
}

export function CosmeticsWorkshopPartsPanel({
  appKey,
  catalog,
  can
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  can: { write: boolean; delete: boolean };
}) {
  const query = useWorkshopPartsQuery(appKey);
  const save = useSaveWorkshopPartMutation(appKey);
  const remove = useDeleteWorkshopPartMutation(appKey);
  const [keyword, setKeyword] = useState("");
  const [slot, setSlot] = useState("");
  const [category, setCategory] = useState("");
  const [state, setState] = useState("");
  const [editing, setEditing] = useState<{ part: WorkshopPart | null; key: number } | null>(null);
  const [deleting, setDeleting] = useState<WorkshopPart | null>(null);
  const [toggling, setToggling] = useState<number | null>(null);

  const parts = useMemo(() => [...(query.data ?? [])].sort(bySortOrder), [query.data]);
  const categories = useMemo(
    () => Array.from(new Set(parts.map((part) => part.category).filter(Boolean))).sort((a, b) => a.localeCompare(b, "zh-CN")),
    [parts]
  );
  const needle = keyword.trim().toLowerCase();
  const rows = parts.filter(
    (part) =>
      (!slot || part.slots.includes(slot as WorkshopPartSlot)) &&
      (!category || part.category === category) &&
      (!state || (state === "enabled") === part.enabled) &&
      (!needle || part.name.toLowerCase().includes(needle) || part.category.toLowerCase().includes(needle))
  );
  const filtered = Boolean(needle || slot || category || state);
  const enabledCount = parts.filter((part) => part.enabled).length;

  const reset = () => {
    setKeyword("");
    setSlot("");
    setCategory("");
    setState("");
  };

  const toggle = async (part: WorkshopPart, enabled: boolean) => {
    setToggling(part.id);
    try {
      await save.mutateAsync({ partId: part.id, payload: inputOf(part, { enabled }) });
      toast.success(enabled ? `已启用「${part.name}」` : `已停用「${part.name}」`);
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    } finally {
      setToggling(null);
    }
  };

  const doDelete = async () => {
    if (!deleting) return;
    try {
      await remove.mutateAsync(deleting.id);
      toast.success(`已删除「${deleting.name}」`);
    } catch (error) {
      toast.error(errorMessage(error, "删除失败"));
    } finally {
      setDeleting(null);
    }
  };

  const columns: Array<Column<WorkshopPart>> = [
    {
      id: "part",
      header: "零件",
      cell: (part) => (
        <div className="flex min-w-0 items-center gap-3">
          <PartThumb part={part} size={44} />
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-sm font-medium">{part.name}</span>
            <span className="truncate text-xs text-muted-foreground">{part.category || "未分类"}</span>
          </div>
        </div>
      )
    },
    {
      id: "slots",
      header: "可用槽位",
      cell: (part) => (
        <div className="flex flex-wrap gap-1">
          {part.slots.map((key) => {
            const meta = SLOT_META[key];
            return (
              <Badge key={key} variant="outline" size="sm" className="gap-1 font-normal">
                {meta ? <meta.icon className="size-3" /> : null}
                {slotLabel(key, catalog)}
              </Badge>
            );
          })}
        </div>
      )
    },
    {
      id: "cut",
      header: "序列帧切分",
      cell: (part) => (
        <span className={cn("text-xs whitespace-nowrap tabular-nums", !isSprite(part) && "text-muted-foreground")}>{cutText(part)}</span>
      )
    },
    {
      id: "sort",
      header: "排序",
      align: "right",
      width: "64px",
      cell: (part) => <span className="text-sm tabular-nums">{part.sortOrder}</span>
    },
    {
      id: "enabled",
      header: "启用",
      width: "72px",
      cell: (part) => (
        <span className="flex items-center gap-1.5">
          <Switch
            checked={part.enabled}
            disabled={!can.write || toggling === part.id}
            onCheckedChange={(enabled) => void toggle(part, enabled)}
            aria-label={`启用「${part.name}」`}
          />
          {toggling === part.id ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
        </span>
      )
    },
    {
      id: "updated",
      header: "更新时间",
      hideBelow: "lg",
      cell: (part) => <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground">{formatDateTimeMinute(part.updatedAt)}</span>
    },
    {
      id: "actions",
      header: <span className="sr-only">操作</span>,
      align: "right",
      width: "120px",
      cell: (part) => (
        <div className="flex justify-end gap-1">
          {can.write ? (
            <Button size="xs" variant="ghost" onClick={() => setEditing({ part, key: Date.now() })}>
              <PencilLine />
              编辑
            </Button>
          ) : null}
          {can.delete ? (
            <Button size="icon-xs" variant="ghost" onClick={() => setDeleting(part)} aria-label={`删除「${part.name}」`}>
              <Trash2 />
            </Button>
          ) : null}
        </div>
      )
    }
  ];

  return (
    <>
      <Section
        title="零件库"
        icon={Puzzle}
        tone="violet"
        flush
        actions={
          <>
            <Badge variant="outline" size="sm" className="tabular-nums">
              {formatCount(enabledCount)} / {formatCount(parts.length)} 启用
            </Badge>
            {can.write ? (
              <Button size="sm" onClick={() => setEditing({ part: null, key: Date.now() })}>
                <Plus />
                登记零件
              </Button>
            ) : null}
          </>
        }
        toolbar={
          <FilterBar>
            <FilterSelect
              value={slot}
              onChange={setSlot}
              label="槽位"
              options={WORKSHOP_PART_SLOTS.map((key) => ({ value: key, label: slotLabel(key, catalog) }))}
            />
            {categories.length ? (
              <FilterSelect value={category} onChange={setCategory} label="分类" options={categories.map((value) => ({ value, label: value }))} />
            ) : null}
            <FilterSelect
              value={state}
              onChange={setState}
              label="状态"
              options={[
                { value: "enabled", label: "启用" },
                { value: "disabled", label: "停用" }
              ]}
            />
            <SearchField value={keyword} onChange={setKeyword} placeholder="名称或分类" />
            <ResetButton visible={filtered} onClick={reset} />
          </FilterBar>
        }
      >
        <RecordTable
          columns={columns}
          rows={rows}
          rowKey={(part) => part.id}
          loading={query.isLoading}
          fetching={query.isFetching}
          error={query.error}
          onRetry={() => void query.refetch()}
          rowClassName={(part) => (part.enabled ? undefined : "text-muted-foreground")}
          empty={<StateBlock compact icon={Puzzle} title={filtered ? "没有符合条件的零件" : "暂无零件"} />}
          minWidth={860}
        />
      </Section>

      {editing ? (
        <WorkshopPartDialog
          key={editing.key}
          appKey={appKey}
          catalog={catalog}
          part={editing.part}
          parts={parts}
          categories={categories}
          onClose={() => setEditing(null)}
        />
      ) : null}

      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && !remove.isPending && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除零件「{deleting?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>已有作品照常能戴，再保存时会被拒；素材仍留在素材库。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
            <Button variant="destructive" onClick={() => void doDelete()} disabled={remove.isPending}>
              {remove.isPending ? <Loader2 className="animate-spin" /> : null}
              删除
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** 列表缩略图：序列帧悬停时按帧率播放，平时停在第一帧 */
function PartThumb({ part, size }: { part: WorkshopPart; size: number }) {
  const [hover, setHover] = useState(false);
  const asset = part.asset;
  if (!asset) {
    return <span className="shrink-0 rounded-md border bg-muted/50" style={{ width: size, height: size }} />;
  }
  return (
    <span
      className="relative shrink-0 overflow-hidden rounded-md border bg-muted/50"
      style={{ width: size, height: size }}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      {isSprite(part) ? (
        <>
          <SpriteFrames
            asset={asset}
            cut={{ columns: part.columns ?? 1, rows: part.rows ?? 1, frameCount: part.frameCount ?? 1, fps: part.fps ?? 12 }}
            size={size - 2}
            playing={hover}
          />
          <Film className="pointer-events-none absolute right-0.5 bottom-0.5 size-3 text-muted-foreground" />
        </>
      ) : (
        <AssetThumb asset={asset} className="size-full p-1" />
      )}
    </span>
  );
}

// ── 新建 / 编辑 ──

type Form = {
  asset: CosmeticAsset | null;
  name: string;
  category: string;
  slots: WorkshopPartSlot[];
  columns: string;
  rows: string;
  frameCount: string;
  fps: string;
  sortOrder: string;
  enabled: boolean;
};

function seed(part: WorkshopPart | null, parts: WorkshopPart[]): Form {
  const nextOrder = parts.length ? Math.max(...parts.map((entry) => entry.sortOrder)) + 1 : 0;
  return {
    asset: part?.asset ?? null,
    name: part?.name ?? "",
    category: part?.category ?? "",
    slots: part?.slots ?? ["avatar_frame"],
    columns: part?.columns ? String(part.columns) : "",
    rows: part?.rows ? String(part.rows) : "",
    frameCount: part?.frameCount ? String(part.frameCount) : "",
    fps: part?.fps ? String(part.fps) : "",
    sortOrder: String(part?.sortOrder ?? nextOrder),
    enabled: part?.enabled ?? true
  };
}

/** 这份表单要不要填切分：素材是序列帧，或者编辑的零件原本就按序列帧登记 */
function wantsCut(form: Form) {
  return form.asset?.kind === "sprite" || Number(form.frameCount) > 0;
}

function validate(form: Form, maxFrames: number): string | null {
  if (!form.asset) return "请选择素材";
  if (!form.name.trim()) return "请填写零件名称";
  if (form.name.trim().length > MAX_NAME) return `零件名称不能超过 ${MAX_NAME} 字`;
  if (form.category.trim().length > MAX_CATEGORY) return `分类不能超过 ${MAX_CATEGORY} 字`;
  if (!form.slots.length) return "请至少选择一个可用的槽位";
  if (!/^-?\d+$/.test(form.sortOrder.trim())) return "排序需要是整数";
  if (wantsCut(form)) {
    const columns = Number(form.columns);
    const rows = Number(form.rows);
    const frames = Number(form.frameCount);
    if (!(columns >= 1 && rows >= 1 && frames >= 1) || frames > columns * rows || frames > maxFrames) {
      return `序列帧的行列与帧数不匹配（最多 ${maxFrames} 帧）`;
    }
    const fps = Number(form.fps);
    if (!(fps > 0 && fps <= 60)) return "序列帧的帧率需要在 0–60 之间";
  }
  return null;
}

function payloadOf(form: Form): WorkshopPartInput {
  const cut = wantsCut(form);
  return {
    assetId: form.asset?.id ?? 0,
    name: form.name.trim(),
    category: form.category.trim(),
    slots: form.slots,
    columns: cut ? Number(form.columns) : 0,
    rows: cut ? Number(form.rows) : 0,
    frameCount: cut ? Number(form.frameCount) : 0,
    fps: cut ? Number(form.fps) : 0,
    sortOrder: Number(form.sortOrder.trim()),
    enabled: form.enabled
  };
}

function WorkshopPartDialog({
  appKey,
  catalog,
  part,
  parts,
  categories,
  onClose
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  part: WorkshopPart | null;
  parts: WorkshopPart[];
  categories: string[];
  onClose: () => void;
}) {
  const create = !part;
  const [form, setForm] = useState<Form>(() => seed(part, parts));
  const [picking, setPicking] = useState(false);
  const save = useSaveWorkshopPartMutation(appKey);
  const maxFrames = catalog?.vocabulary?.limits?.maxSpriteFrames ?? MAX_FRAMES_FALLBACK;
  const patch = (next: Partial<Form>) => setForm((current) => ({ ...current, ...next }));
  const problem = validate(form, maxFrames);
  const cut = wantsCut(form);
  const usedIds = parts.filter((entry) => entry.id !== part?.id).map((entry) => entry.assetId);
  const takenBy = form.asset ? parts.find((entry) => entry.assetId === form.asset?.id && entry.id !== part?.id) : undefined;

  const pickAsset = (asset: CosmeticAsset) => {
    setForm((current) => ({
      ...current,
      asset,
      name: current.name.trim() ? current.name : asset.name.slice(0, MAX_NAME),
      // 换成图片素材时清掉切分；换成序列帧且还没填时帧率给个常用值
      ...(asset.kind === "sprite"
        ? { fps: current.fps || "12" }
        : { columns: "", rows: "", frameCount: "", fps: "" })
    }));
  };

  /** 改行列时，帧数原本等于满格（或还没填）就跟着变 */
  const patchGrid = (next: { columns?: string; rows?: string }) => {
    setForm((current) => {
      const before = Number(current.columns) * Number(current.rows);
      const columns = next.columns ?? current.columns;
      const rows = next.rows ?? current.rows;
      const after = Number(columns) * Number(rows);
      const follow = !current.frameCount || Number(current.frameCount) === before;
      return { ...current, columns, rows, frameCount: follow && after > 0 ? String(after) : current.frameCount };
    });
  };

  const toggleSlot = (slot: WorkshopPartSlot) =>
    patch({ slots: form.slots.includes(slot) ? form.slots.filter((key) => key !== slot) : [...form.slots, slot] });

  const submit = async () => {
    if (problem) {
      toast.error(problem);
      return;
    }
    try {
      const saved = await save.mutateAsync({ partId: part?.id, payload: payloadOf(form) });
      toast.success(create ? `已登记「${saved.name}」` : "已保存");
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  const cutValue = {
    columns: Number(form.columns) || 1,
    rows: Number(form.rows) || 1,
    frameCount: Number(form.frameCount) || 0,
    fps: Number(form.fps) || 12
  };

  return (
    <>
      <Dialog open onOpenChange={(open) => !open && !save.isPending && onClose()}>
        <DialogContent className="flex max-h-[92dvh] flex-col gap-0 p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 pt-5 pb-4">
            <DialogTitle>{create ? "登记零件" : `编辑「${part?.name}」`}</DialogTitle>
            <DialogDescription>从素材库挑选，用户在工坊里可用</DialogDescription>
          </DialogHeader>

          <ScrollArea className="min-h-0 flex-1">
            <div className="flex flex-col gap-6 px-6 py-5">
              <div className="flex flex-col gap-2">
                <Label className="text-xs text-muted-foreground">素材</Label>
                {form.asset ? (
                  <div className="flex items-center gap-4 rounded-xl border p-3">
                    {cut ? (
                      <SpriteFrames asset={form.asset} cut={cutValue} size={72} className="shrink-0 rounded-md bg-muted/50" />
                    ) : (
                      <AssetThumb asset={form.asset} className="size-[72px] shrink-0 rounded-md p-1" />
                    )}
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium">{form.asset.name}</span>
                        {form.asset.kind === "sprite" ? (
                          <Badge variant="info" size="sm">
                            <Film />
                            序列帧
                          </Badge>
                        ) : null}
                      </span>
                      <span className="text-xs tabular-nums text-muted-foreground">
                        {form.asset.width}×{form.asset.height}
                      </span>
                      {takenBy ? <span className="text-xs text-destructive">已登记为零件「{takenBy.name}」</span> : null}
                    </div>
                    <Button size="sm" variant="outline" onClick={() => setPicking(true)}>
                      <Replace />
                      更换
                    </Button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setPicking(true)}
                    className="flex flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed px-6 py-8 text-sm transition-colors hover:bg-muted/40"
                  >
                    <ImagePlus className="size-5 text-muted-foreground" />
                    从素材库选择
                  </button>
                )}
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="名称">
                  <Input value={form.name} maxLength={MAX_NAME} onChange={(event) => patch({ name: event.target.value })} />
                </Field>
                <Field label="分类">
                  <Input
                    value={form.category}
                    maxLength={MAX_CATEGORY}
                    onChange={(event) => patch({ category: event.target.value })}
                    placeholder="选填"
                  />
                  {categories.length ? (
                    <QuickChips items={categories.slice(0, 8)} value={form.category.trim()} onPick={(category) => patch({ category })} />
                  ) : null}
                </Field>
              </div>

              <div className="flex flex-col gap-2">
                <Label className="text-xs text-muted-foreground">可用槽位</Label>
                <div className="grid grid-cols-3 gap-2">
                  {WORKSHOP_PART_SLOTS.map((slot) => {
                    const meta = SLOT_META[slot];
                    const active = form.slots.includes(slot);
                    return (
                      <button
                        key={slot}
                        type="button"
                        aria-pressed={active}
                        onClick={() => toggleSlot(slot)}
                        className={cn(
                          "flex items-center gap-2 rounded-lg border px-3 py-2.5 text-left text-sm transition-colors",
                          active ? "border-primary/50 bg-primary/5 font-medium" : "hover:bg-accent"
                        )}
                      >
                        <meta.icon className="size-4 text-muted-foreground" />
                        <span className="flex-1 truncate">{slotLabel(slot, catalog)}</span>
                        {active ? <Check className="size-3.5 text-primary" /> : null}
                      </button>
                    );
                  })}
                </div>
              </div>

              {cut && form.asset ? (
                <div className="flex flex-col gap-3 rounded-xl border p-4">
                  <Label className="text-xs text-muted-foreground">序列帧切分</Label>
                  <div className="flex flex-col gap-4 sm:flex-row">
                    <SpriteGrid asset={form.asset} cut={cutValue} maxWidth={220} maxHeight={160} />
                    <div className="grid flex-1 grid-cols-2 gap-3">
                      <Field label="列数">
                        <NumberField value={form.columns} onChange={(columns) => patchGrid({ columns })} />
                      </Field>
                      <Field label="行数">
                        <NumberField value={form.rows} onChange={(rows) => patchGrid({ rows })} />
                      </Field>
                      <Field label="帧数">
                        <NumberField value={form.frameCount} onChange={(frameCount) => patch({ frameCount })} />
                      </Field>
                      <Field label="帧率">
                        <NumberField value={form.fps} decimal onChange={(fps) => patch({ fps })} suffix="fps" />
                      </Field>
                    </div>
                  </div>
                </div>
              ) : null}

              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="排序">
                  <Input
                    value={form.sortOrder}
                    inputMode="numeric"
                    className="tabular-nums sm:max-w-32"
                    onChange={(event) => patch({ sortOrder: event.target.value.replace(/[^\d-]/g, "") })}
                  />
                </Field>
                <label className="flex items-center justify-between gap-4 self-end rounded-xl border px-4 py-2.5">
                  <span className="text-sm font-medium">启用</span>
                  <Switch checked={form.enabled} onCheckedChange={(enabled) => patch({ enabled })} />
                </label>
              </div>
            </div>
          </ScrollArea>

          <DialogFooter className="flex-row items-center justify-between gap-2 border-t px-6 py-3 sm:justify-between">
            <span className="text-xs text-destructive">{problem && form.asset ? problem : ""}</span>
            <div className="flex gap-2">
              <Button variant="outline" onClick={onClose} disabled={save.isPending}>
                取消
              </Button>
              <Button onClick={() => void submit()} disabled={save.isPending}>
                {save.isPending ? <Loader2 className="animate-spin" /> : <Check />}
                {create ? "登记" : "保存"}
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AssetPickerDialog
        open={picking}
        onOpenChange={setPicking}
        appKey={appKey}
        catalog={catalog}
        kind={form.asset?.kind ?? "image"}
        title="选择零件素材"
        description="点选素材登记为零件，或上传后直接使用"
        usedIds={usedIds}
        usedLabel="已是零件"
        onPick={pickAsset}
      />
    </>
  );
}

function Field({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

function NumberField({
  value,
  decimal,
  suffix,
  onChange
}: {
  value: string;
  decimal?: boolean;
  suffix?: string;
  onChange: (value: string) => void;
}) {
  const input = (
    <Input
      value={value}
      inputMode={decimal ? "decimal" : "numeric"}
      maxLength={decimal ? 5 : 3}
      className="tabular-nums"
      onChange={(event) => onChange(event.target.value.replace(decimal ? /[^\d.]/g : /\D/g, ""))}
    />
  );
  if (!suffix) return input;
  return (
    <span className="flex items-center gap-1.5">
      {input}
      <span className="text-xs text-muted-foreground">{suffix}</span>
    </span>
  );
}

