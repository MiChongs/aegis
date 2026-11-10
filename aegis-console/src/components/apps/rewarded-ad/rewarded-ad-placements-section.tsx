"use client";

import { useMemo, useState } from "react";
import {
  ClipboardPaste,
  Eye,
  EyeOff,
  KeyRound,
  Plus,
  RadioTower,
  RotateCcw,
  Trash2,
  Undo2
} from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import {
  MAX_PLACEMENTS,
  blankPlacement,
  draftKeyState,
  parsePlacementPaste,
  type DraftKeyState,
  type Issue,
  type PlacementDraft,
  type SceneDraft
} from "@/components/apps/rewarded-ad/rewarded-ad-config-draft";
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
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { RewardedAdPlacement } from "@/lib/api/rewarded-ad";
import { formatDate, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/**
 * 广告位与 Security Key。
 *
 * Apex 按广告位签发 Security Key，回调用观看所在广告位的那一把签名，所以 Key 挂在广告位上；
 * 应用的默认 Key 给没单独配 Key 的广告位兜底。每个广告位同时展示「哪些场景在用」与「最近有没有验签通过的回调」，
 * 后者是判断 Key 与回调地址配对了没有的直接证据。
 */
export function PlacementsSection({
  placements,
  scenes,
  saved,
  activityDays,
  defaultKey,
  hasSavedDefaultKey,
  savedDefaultKeyHint,
  clearDefaultKey,
  issues,
  onChange,
  onDefaultKeyChange
}: {
  placements: PlacementDraft[];
  scenes: SceneDraft[];
  saved: RewardedAdPlacement[];
  activityDays: number;
  defaultKey: string;
  hasSavedDefaultKey: boolean;
  savedDefaultKeyHint?: string;
  clearDefaultKey: boolean;
  issues: Issue[];
  /** rename：某个广告位的 ID 改了，草稿里引用旧 ID 的场景要跟着改（与列表变更在同一次更新里提交） */
  onChange: (placements: PlacementDraft[], rename?: { from: string; to: string }) => void;
  onDefaultKeyChange: (changes: { defaultKey?: string; clearDefaultKey?: boolean }) => void;
}) {
  const [pasteOpen, setPasteOpen] = useState(false);
  const savedById = useMemo(() => new Map(saved.map((item) => [item.id, item])), [saved]);
  const hasDefault = Boolean(defaultKey.trim()) || (hasSavedDefaultKey && !clearDefaultKey);

  const patch = (uid: string, changes: Partial<PlacementDraft>) => {
    const target = placements.find((item) => item.uid === uid);
    const rename =
      target && changes.id !== undefined && target.id.trim() && changes.id.trim() !== target.id.trim()
        ? { from: target.id.trim(), to: changes.id.trim() }
        : undefined;
    onChange(
      placements.map((item) => (item.uid === uid ? { ...item, ...changes } : item)),
      rename
    );
  };

  const states = placements.map((item) => draftKeyState(item, savedById.get(item.savedId), hasDefault));
  const ownCount = states.filter((state) => state === "own" || state === "new").length;
  const fallbackCount = states.filter((state) => state === "default").length;

  return (
    <SectionCard
      icon={<KeyRound className="size-4" />}
      title="广告位与 Security Key"
      description="Apex 为每个广告位单独签发 Security Key，回调按广告位验签"
      aside={
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setPasteOpen(true)}>
            <ClipboardPaste className="size-3.5" />
            批量粘贴
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={placements.length >= MAX_PLACEMENTS}
            onClick={() => onChange([...placements, blankPlacement()])}
          >
            <Plus className="size-3.5" />
            添加广告位
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        {placements.length === 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border px-4 py-10 text-center">
            <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground">
              <RadioTower className="size-5" />
            </span>
            <div className="space-y-1">
              <p className="text-sm font-medium">尚未登记广告位</p>
              <p className="text-xs text-muted-foreground">
                在 Apex 后台复制广告位名称、ID 与 Security Key，可直接批量粘贴
              </p>
            </div>
            <div className="flex gap-2">
              <Button size="sm" onClick={() => setPasteOpen(true)}>
                <ClipboardPaste className="size-3.5" />
                批量粘贴
              </Button>
              <Button size="sm" variant="outline" onClick={() => onChange([blankPlacement()])}>
                <Plus className="size-3.5" />
                手动添加
              </Button>
            </div>
          </div>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
              <span>
                共 <b className="font-semibold text-foreground tabular-nums">{placements.length}</b> 个广告位
              </span>
              <span>
                独立 Key <b className="font-semibold text-foreground tabular-nums">{ownCount}</b> 个
              </span>
              <span>回调统计为近 {activityDays} 天</span>
            </div>
            <div className="space-y-2.5">
              {placements.map((placement) => (
                <PlacementCard
                  key={placement.uid}
                  placement={placement}
                  saved={savedById.get(placement.savedId)}
                  keyState={states[placements.indexOf(placement)]}
                  usedBy={scenes.filter((scene) => scene.placementId && scene.placementId === placement.id.trim())}
                  activityDays={activityDays}
                  issues={issues.filter((issue) => issue.uid === placement.uid)}
                  onPatch={(changes) => patch(placement.uid, changes)}
                  onRemove={() => onChange(placements.filter((item) => item.uid !== placement.uid))}
                />
              ))}
            </div>
          </>
        )}

        <DefaultKeyField
          value={defaultKey}
          hasSaved={hasSavedDefaultKey}
          savedHint={savedDefaultKeyHint}
          cleared={clearDefaultKey}
          fallbackCount={fallbackCount}
          onChange={onDefaultKeyChange}
        />
      </div>

      <PasteDialog
        open={pasteOpen}
        onOpenChange={setPasteOpen}
        existing={placements}
        onApply={(next) => {
          onChange(next);
          setPasteOpen(false);
        }}
      />
    </SectionCard>
  );
}

const KEY_STATE_META: Record<DraftKeyState, { label: string; variant: "success" | "info" | "outline" | "danger" }> = {
  new: { label: "新 Key 待保存", variant: "info" },
  own: { label: "独立 Key", variant: "success" },
  default: { label: "使用默认 Key", variant: "outline" },
  missing: { label: "缺少 Key", variant: "danger" }
};

function PlacementCard({
  placement,
  saved,
  keyState,
  usedBy,
  activityDays,
  issues,
  onPatch,
  onRemove
}: {
  placement: PlacementDraft;
  saved?: RewardedAdPlacement;
  keyState: DraftKeyState;
  usedBy: SceneDraft[];
  activityDays: number;
  issues: Issue[];
  onPatch: (changes: Partial<PlacementDraft>) => void;
  onRemove: () => void;
}) {
  // 新加的广告位直接展开 Key 输入框；已保存的默认收起，只显示状态
  const [editingKey, setEditingKey] = useState(!placement.savedId);
  const showKeyInput = editingKey || Boolean(placement.key);
  const [reveal, setReveal] = useState(false);
  const meta = KEY_STATE_META[keyState];
  const errors = issues.filter((issue) => issue.level === "error");
  const hasSavedKey = Boolean(saved?.hasSecurityKey);

  return (
    <div
      className={cn(
        "rounded-xl border bg-card p-3.5 transition-colors sm:p-4",
        errors.length > 0 ? "border-red-300 dark:border-red-900" : "border-border"
      )}
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
        <div className="grid flex-1 gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">名称</Label>
            <Input
              className="h-8 text-xs"
              value={placement.name}
              maxLength={32}
              placeholder="如 激励视频"
              onChange={(event) => onPatch({ name: event.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">广告位 ID</Label>
            <Input
              className="h-8 font-mono text-xs"
              value={placement.id}
              maxLength={64}
              placeholder="Apex 后台的聚合广告位 ID"
              aria-invalid={errors.length > 0 && !placement.id.trim() ? true : undefined}
              onChange={(event) => onPatch({ id: event.target.value.trim() })}
            />
          </div>
        </div>
        <div className="flex items-center justify-between gap-2 sm:flex-col sm:items-end sm:pt-6">
          <Badge variant={meta.variant} size="sm">
            {meta.label}
            {keyState === "own" && saved?.securityKeyHint ? (
              <span className="font-mono opacity-80">{saved.securityKeyHint}</span>
            ) : null}
          </Badge>
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7 text-muted-foreground hover:text-destructive"
                  aria-label="删除广告位"
                  disabled={usedBy.length > 0}
                  onClick={onRemove}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent>
              {usedBy.length > 0 ? `有 ${usedBy.length} 个场景在使用，请先为这些场景更换广告位` : "删除广告位"}
            </TooltipContent>
          </Tooltip>
        </div>
      </div>

      <div className="mt-3 rounded-lg bg-muted/50 px-3 py-2.5">
        {showKeyInput ? (
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <div className="relative flex-1">
              <Input
                type={reveal ? "text" : "password"}
                autoComplete="off"
                spellCheck={false}
                className="h-8 bg-background pr-9 font-mono text-xs"
                placeholder={hasSavedKey ? "输入新的 Security Key，留空则保留原 Key" : "该广告位在 Apex 后台的 Security Key"}
                value={placement.key}
                onChange={(event) => onPatch({ key: event.target.value, clearKey: false })}
              />
              {placement.key ? (
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  onClick={() => setReveal(!reveal)}
                  aria-label={reveal ? "隐藏密钥" : "显示密钥"}
                >
                  {reveal ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                </button>
              ) : null}
            </div>
            {placement.savedId ? (
              <Button
                size="sm"
                variant="ghost"
                className="h-8 text-xs"
                onClick={() => {
                  onPatch({ key: "" });
                  setEditingKey(false);
                }}
              >
                取消
              </Button>
            ) : null}
          </div>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">
              {placement.clearKey ? (
                "保存后清除独立 Key，改用默认 Key 验签"
              ) : hasSavedKey ? (
                <>
                  已配置独立 Key
                  {saved?.keyUpdatedAt ? `，${formatDate(saved.keyUpdatedAt)} 更新` : ""}
                </>
              ) : (
                "未配置独立 Key，回调使用默认 Key 验签"
              )}
            </p>
            <div className="flex items-center gap-1">
              {placement.clearKey ? (
                <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => onPatch({ clearKey: false })}>
                  <Undo2 className="size-3.5" />
                  撤销
                </Button>
              ) : (
                <>
                  <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setEditingKey(true)}>
                    <KeyRound className="size-3.5" />
                    {hasSavedKey ? "更换 Key" : "设置独立 Key"}
                  </Button>
                  {hasSavedKey ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 text-xs text-muted-foreground hover:text-destructive"
                      onClick={() => onPatch({ clearKey: true, key: "" })}
                    >
                      <RotateCcw className="size-3.5" />
                      清除
                    </Button>
                  ) : null}
                </>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="text-[11px] text-muted-foreground">使用场景</span>
          {usedBy.length === 0 ? (
            <span className="text-[11px] text-amber-600 dark:text-amber-400">暂无</span>
          ) : (
            usedBy.map((scene) => (
              <Badge key={scene.uid} variant="outline" size="sm" className={cn(!scene.enabled && "opacity-50")}>
                {scene.name || scene.key}
              </Badge>
            ))
          )}
        </div>
        <CallbackActivity saved={saved} activityDays={activityDays} />
      </div>

      {errors.length > 0 ? (
        <ul className="mt-2.5 space-y-0.5">
          {errors.map((issue) => (
            <li key={issue.text} className="text-[11px] text-red-600 dark:text-red-400">
              {issue.text}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** 近 N 天验签通过的回调：有就说明 Key 与回调地址都配对了。 */
function CallbackActivity({ saved, activityDays }: { saved?: RewardedAdPlacement; activityDays: number }) {
  if (!saved) {
    return <span className="text-[11px] text-muted-foreground">保存后开始统计回调</span>;
  }
  if (!saved.callbacks || !saved.lastCallbackAt) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <span className="size-1.5 rounded-full bg-muted-foreground/40" />
        近 {activityDays} 天未收到回调
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] text-emerald-700 dark:text-emerald-400">
      <span className="size-1.5 rounded-full bg-emerald-500" />
      近 {activityDays} 天 <b className="font-semibold tabular-nums">{saved.callbacks}</b> 次回调，最近一次{" "}
      {formatRelative(saved.lastCallbackAt)}
    </span>
  );
}

/** 默认 Key：给没单独配 Key 的广告位兜底；整个应用只有一把 Key 时只填这里即可。 */
function DefaultKeyField({
  value,
  hasSaved,
  savedHint,
  cleared,
  fallbackCount,
  onChange
}: {
  value: string;
  hasSaved: boolean;
  savedHint?: string;
  cleared: boolean;
  fallbackCount: number;
  onChange: (changes: { defaultKey?: string; clearDefaultKey?: boolean }) => void;
}) {
  const [reveal, setReveal] = useState(false);
  const status = value.trim()
    ? { label: "新 Key 待保存", variant: "info" as const }
    : cleared
      ? { label: "保存后清除", variant: "warning" as const }
      : hasSaved
        ? { label: `已配置 ${savedHint ?? ""}`.trim(), variant: "success" as const }
        : { label: "未配置", variant: "outline" as const };

  return (
    <div className="rounded-xl border border-dashed border-border p-3.5 sm:p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="space-y-0.5">
          <div className="text-xs font-semibold">默认 Key</div>
          <p className="text-[11px] text-muted-foreground">
            {fallbackCount > 0
              ? `${fallbackCount} 个广告位未配置独立 Key，使用此 Key 验签`
              : "所有广告位均已配置独立 Key，此项可留空"}
          </p>
        </div>
        <Badge variant={status.variant} size="sm" className="font-mono">
          {status.label}
        </Badge>
      </div>
      <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative flex-1">
          <Input
            type={reveal ? "text" : "password"}
            autoComplete="off"
            spellCheck={false}
            className="h-8 pr-9 font-mono text-xs"
            placeholder={hasSaved ? "留空不修改" : "选填"}
            value={value}
            onChange={(event) => onChange({ defaultKey: event.target.value, clearDefaultKey: false })}
          />
          {value ? (
            <button
              type="button"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => setReveal(!reveal)}
              aria-label={reveal ? "隐藏密钥" : "显示密钥"}
            >
              {reveal ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          ) : null}
        </div>
        {hasSaved && !value ? (
          <Button
            size="sm"
            variant="ghost"
            className={cn("h-8 text-xs", !cleared && "text-muted-foreground hover:text-destructive")}
            onClick={() => onChange({ clearDefaultKey: !cleared })}
          >
            {cleared ? <Undo2 className="size-3.5" /> : <RotateCcw className="size-3.5" />}
            {cleared ? "撤销清除" : "清除"}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

/** 掩码展示：只露末 4 位 */
function maskKey(key: string) {
  if (!key) return "";
  return key.length <= 8 ? "••••" : `••••${key.slice(-4)}`;
}

/**
 * 批量粘贴：把 Apex 后台复制出来的「名称 / 广告位 ID / Security Key」一次登记进来。
 * 已登记的 ID 只填入新 Key（名称为空时补上名称），没登记的新增。应用到草稿后仍需保存。
 */
function PasteDialog({
  open,
  onOpenChange,
  existing,
  onApply
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existing: PlacementDraft[];
  onApply: (next: PlacementDraft[]) => void;
}) {
  const [text, setText] = useState("");
  const parsed = useMemo(() => parsePlacementPaste(text), [text]);
  const rows = parsed.map((item) => ({
    ...item,
    existing: existing.find((placement) => placement.id.trim() === item.id)
  }));
  const invalid = rows.filter((row) => !row.id);
  const valid = rows.filter((row) => row.id);
  const added = valid.filter((row) => !row.existing).length;
  const updated = valid.length - added;
  const overflow = existing.length + added > MAX_PLACEMENTS;

  const apply = () => {
    let next = [...existing];
    for (const row of valid) {
      if (row.existing) {
        next = next.map((placement) =>
          placement.uid === row.existing!.uid
            ? {
                ...placement,
                name: placement.name.trim() ? placement.name : row.name,
                key: row.key || placement.key,
                clearKey: row.key ? false : placement.clearKey
              }
            : placement
        );
      } else {
        next.push(blankPlacement({ id: row.id, name: row.name, key: row.key }));
      }
    }
    onApply(next);
    setText("");
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        onOpenChange(value);
        if (!value) setText("");
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>批量粘贴广告位</DialogTitle>
          <DialogDescription>
            每个广告位包含名称、广告位 ID 与 Security Key，每项一行或用制表符分隔，顺序不限。
          </DialogDescription>
        </DialogHeader>
        <Textarea
          value={text}
          rows={7}
          spellCheck={false}
          className="font-mono text-xs"
          placeholder={"激励视频\n1234567890123456\nabcd1234efgh5678ijkl\n\n激励视频2\n6543210987654321\nwxyz9876stuv5432qrst"}
          onChange={(event) => setText(event.target.value)}
        />
        {rows.length > 0 ? (
          <div className="overflow-hidden rounded-lg border border-border">
            <table className="w-full text-xs">
              <thead className="bg-muted/50 text-[11px] text-muted-foreground">
                <tr>
                  <th className="px-3 py-1.5 text-left font-medium">名称</th>
                  <th className="px-3 py-1.5 text-left font-medium">广告位 ID</th>
                  <th className="px-3 py-1.5 text-left font-medium">Security Key</th>
                  <th className="px-3 py-1.5 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={`${row.id}-${index}`} className="border-t border-border">
                    <td className="max-w-40 truncate px-3 py-1.5">{row.name || <span className="text-muted-foreground">未命名</span>}</td>
                    <td className="px-3 py-1.5 font-mono">{row.id || <span className="text-red-600">未识别</span>}</td>
                    <td className="px-3 py-1.5 font-mono">
                      {row.key ? maskKey(row.key) : <span className="text-muted-foreground">无</span>}
                    </td>
                    <td className="px-3 py-1.5 text-right">
                      {!row.id ? (
                        <Badge variant="danger" size="sm">跳过</Badge>
                      ) : row.existing ? (
                        <Badge variant="info" size="sm">更新</Badge>
                      ) : (
                        <Badge variant="success" size="sm">新增</Badge>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {invalid.length > 0 ? (
          <p className="text-[11px] text-red-600 dark:text-red-400">{invalid.length} 条未识别出广告位 ID，将被跳过</p>
        ) : null}
        {overflow ? (
          <p className="text-[11px] text-red-600 dark:text-red-400">超出上限，最多登记 {MAX_PLACEMENTS} 个广告位</p>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button disabled={valid.length === 0 || overflow} onClick={apply}>
            {valid.length === 0 ? "应用" : `新增 ${added} 个，更新 ${updated} 个`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
