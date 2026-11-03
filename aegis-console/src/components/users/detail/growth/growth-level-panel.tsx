"use client";

/**
 * 等级控制：一条等级轨道把「现在几级、按经验应到几级、锁定的保底、封顶」画在一起，下面是四个动作：
 * 封顶（可同时降级）、解除封顶、强制升级（复用等级体系的调级接口）、锁定 / 解除锁定。
 *
 * 封顶是处罚：经验照常累计，等级停在封顶处；解除后按经验补升，经过的达成奖励不会重复发。
 * 强制升级不能越过封顶，这里直接把超出的目标级置灰。
 */

import * as React from "react";
import { ArrowUpToLine, ChevronsUp, Loader2, Lock, LockOpen, ShieldMinus, ShieldOff } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import type { GrowthLevelState } from "@/lib/api/growth";
import { useUserLevelCapMutation } from "@/lib/growth-hooks";
import { useAdjustLevelUserMutation } from "@/lib/level-hooks";
import { cn } from "@/lib/utils";
import { Section } from "../kit";
import { ConfirmActionDialog } from "../records/records-shared";

export function GrowthLevelPanel({ appKey, userId, level }: { appKey: string; userId: number; level?: GrowthLevelState }) {
  const capMutation = useUserLevelCapMutation(appKey, userId);
  const adjust = useAdjustLevelUserMutation(appKey);
  const [dialog, setDialog] = React.useState<"cap" | "uncap" | "force" | "lock" | "unlock" | null>(null);
  const [note, setNote] = React.useState("");

  if (!level) {
    return (
      <Section title="等级控制" icon={ChevronsUp} tone="violet" description="加载中">
        <div className="h-24 animate-pulse rounded-lg bg-muted/50" />
      </Section>
    );
  }

  const capped = level.capped > 0;
  const held = capped && level.eligible > level.level;

  async function runAdjust(payload: Parameters<typeof adjust.mutateAsync>[0]["payload"], success: string) {
    try {
      const result = await adjust.mutateAsync({ userId, payload: { ...payload, note: note.trim() || undefined } });
      toast.success(success, { description: result.from !== result.to ? `Lv.${result.from} → Lv.${result.to}` : undefined });
      setDialog(null);
      setNote("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  async function uncap() {
    try {
      const state = await capMutation.mutateAsync({ clear: true, note: note.trim() || undefined });
      toast.success("已解除等级封顶", { description: state.level !== level?.level ? `已按经验升到 Lv.${state.level}` : undefined });
      setDialog(null);
      setNote("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <Section
      title="等级控制"
      icon={ChevronsUp}
      tone="violet"
      description={
        capped
          ? held
            ? `已封顶在 Lv.${level.capped}，按经验本应是 Lv.${level.eligible}`
            : `封顶 Lv.${level.capped}，经验照常累计`
          : "经验决定等级；这里可以封顶、降级、强制升级与锁定保底"
      }
      actions={
        <div className="flex flex-wrap gap-1.5">
          {capped ? (
            <Button size="sm" variant="outline" onClick={() => setDialog("uncap")}>
              <ShieldOff />
              解除封顶
            </Button>
          ) : null}
          <Button size="sm" variant="outline" onClick={() => setDialog("cap")}>
            <ShieldMinus />
            {capped ? "调整封顶" : "封顶 / 降级"}
          </Button>
          <Button size="sm" variant="outline" onClick={() => setDialog("force")} disabled={level.level >= Math.min(level.maxLevel, capped ? level.capped : level.maxLevel)}>
            <ArrowUpToLine />
            强制升级
          </Button>
          {level.locked > 0 ? (
            <Button size="sm" variant="ghost" onClick={() => setDialog("unlock")}>
              <LockOpen />
              解除锁定
            </Button>
          ) : (
            <Button size="sm" variant="ghost" onClick={() => setDialog("lock")}>
              <Lock />
              锁定当前等级
            </Button>
          )}
        </div>
      }
    >
      <LevelTrack state={level} />

      <CapDialog
        open={dialog === "cap"}
        onOpenChange={(open) => setDialog(open ? "cap" : null)}
        state={level}
        pending={capMutation.isPending}
        onSubmit={async (target, demote, text) => {
          try {
            const state = await capMutation.mutateAsync({ level: target, demote, note: text || undefined });
            toast.success(`已封顶 Lv.${target}`, { description: state.level < level.level ? `由 Lv.${level.level} 降至 Lv.${state.level}` : undefined });
            setDialog(null);
          } catch (error) {
            toast.error(error instanceof ApiError ? error.message : "操作失败");
          }
        }}
      />
      <ForceDialog
        open={dialog === "force"}
        onOpenChange={(open) => setDialog(open ? "force" : null)}
        state={level}
        pending={adjust.isPending}
        onSubmit={(payload) => runAdjust(payload, `已强制升级到 Lv.${payload.level}`)}
        note={note}
        onNoteChange={setNote}
      />
      <ConfirmActionDialog
        open={dialog === "uncap"}
        onOpenChange={(open) => setDialog(open ? "uncap" : null)}
        title="解除等级封顶"
        description={held ? `解除后按经验立即升到 Lv.${level.eligible}（经过的达成奖励不会重复发放）。` : "解除后等级按经验正常升级。"}
        confirmLabel="解除封顶"
        pending={capMutation.isPending}
        note={{ value: note, onChange: setNote, placeholder: "备注（可选）" }}
        onConfirm={uncap}
      />
      <ConfirmActionDialog
        open={dialog === "lock"}
        onOpenChange={(open) => setDialog(open ? "lock" : null)}
        title={`锁定 Lv.${level.level} 为保底`}
        description="「重算并允许降级」时不会把这个用户降到保底以下。"
        confirmLabel="锁定"
        pending={adjust.isPending}
        note={{ value: note, onChange: setNote, placeholder: "备注（可选）" }}
        onConfirm={() => runAdjust({ level: 0, lock: true }, "已锁定当前等级")}
      />
      <ConfirmActionDialog
        open={dialog === "unlock"}
        onOpenChange={(open) => setDialog(open ? "unlock" : null)}
        title="解除保底锁定"
        description={`当前锁定 Lv.${level.locked}。解除后，重算并允许降级时按经验重新判定。`}
        confirmLabel="解除锁定"
        pending={adjust.isPending}
        note={{ value: note, onChange: setNote, placeholder: "备注（可选）" }}
        onConfirm={() => runAdjust({ level: 0, unlock: true }, "已解除锁定")}
      />
    </Section>
  );
}

/**
 * 等级轨道：每级一格（级数多时自动变细），已达到的填色，封顶之上画斜线，
 * 「按经验应到」用虚线框标出，保底锁定与封顶各有一个标记。
 */
function LevelTrack({ state }: { state: GrowthLevelState }) {
  const total = Math.max(state.maxLevel, 1);
  const levels = Array.from({ length: total }, (_, i) => i + 1);
  const dense = total > 30;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-end justify-between gap-3">
        <div className="flex items-baseline gap-2">
          <span className="text-3xl font-semibold tracking-tight tabular-nums">Lv.{state.level}</span>
          <span className="text-xs text-muted-foreground">/ {total} 级，累计经验 {state.experience.toLocaleString("zh-CN")}</span>
        </div>
        <div className="flex flex-wrap justify-end gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
          <Legend className="bg-primary" label="已达到" />
          {state.eligible > state.level ? <Legend className="border border-dashed border-primary" label={`按经验应到 Lv.${state.eligible}`} /> : null}
          {state.locked > 0 ? <Legend className="bg-violet-500" label={`保底 Lv.${state.locked}`} /> : null}
          {state.capped > 0 ? <Legend className="bg-red-500" label={`封顶 Lv.${state.capped}`} /> : null}
          {state.highest > state.level ? <Legend className="bg-muted-foreground/40" label={`历史最高 Lv.${state.highest}`} /> : null}
        </div>
      </div>
      <div className={cn("flex items-stretch", dense ? "gap-px" : "gap-1")} role="img" aria-label={`当前 Lv.${state.level}，共 ${total} 级`}>
        {levels.map((lv) => {
          const reached = lv <= state.level;
          const pending = !reached && lv <= state.eligible;
          const blocked = state.capped > 0 && lv > state.capped;
          const isLocked = state.locked > 0 && lv === state.locked;
          const isCap = state.capped > 0 && lv === state.capped;
          return (
            <div key={lv} className="flex min-w-0 flex-1 flex-col items-center gap-1" title={`Lv.${lv}`}>
              <div
                className={cn(
                  "relative h-7 w-full rounded-[5px] transition-colors",
                  reached ? "bg-primary" : "bg-muted",
                  pending && "border border-dashed border-primary bg-primary/10",
                  blocked && "bg-red-500/10",
                  lv === state.level && "ring-2 ring-primary/30 ring-offset-1 ring-offset-background"
                )}
              >
                {isCap ? <span className="absolute inset-y-0 -right-[3px] w-[3px] rounded-full bg-red-500" /> : null}
                {isLocked ? <span className="absolute -bottom-1 left-1/2 size-2 -translate-x-1/2 rounded-full border-2 border-background bg-violet-500" /> : null}
              </div>
              {!dense || lv === 1 || lv === total || lv === state.level || lv % 5 === 0 ? (
                <span className={cn("text-[10px] tabular-nums", lv === state.level ? "font-semibold text-foreground" : "text-muted-foreground")}>{lv}</span>
              ) : (
                <span className="text-[10px] opacity-0">·</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Legend({ className, label }: { className: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className={cn("size-2.5 rounded-[3px]", className)} />
      {label}
    </span>
  );
}

function LevelPicker({ value, onChange, min, max }: { value: number; onChange: (value: number) => void; min: number; max: number }) {
  return (
    <div className="flex items-center gap-3">
      <Slider value={[value]} min={min} max={Math.max(max, min)} step={1} onValueChange={([next]) => onChange(next)} className="flex-1" />
      <Input
        value={value}
        onChange={(event) => {
          const next = Number.parseInt(event.target.value.replace(/[^\d]/g, "") || "0", 10);
          onChange(Math.min(Math.max(next, min), max));
        }}
        className="h-8 w-16 text-center tabular-nums"
        aria-label="等级"
      />
    </div>
  );
}

type CapDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  state: GrowthLevelState;
  pending: boolean;
  onSubmit: (level: number, demote: boolean, note: string) => void;
};

function CapDialog(props: CapDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={(next) => (!props.pending ? props.onOpenChange(next) : undefined)}>
      <DialogContent className="sm:max-w-md">{props.open ? <CapDialogBody {...props} /> : null}</DialogContent>
    </Dialog>
  );
}

function CapDialogBody({ onOpenChange, state, pending, onSubmit }: CapDialogProps) {
  const [target, setTarget] = React.useState(() => state.capped || Math.max(state.level - 1, 1));
  const [demote, setDemote] = React.useState(false);
  const [note, setNote] = React.useState("");
  const needsDemote = target < state.level;
  return (
    <>
      <DialogHeader>
        <DialogTitle>等级封顶</DialogTitle>
        <DialogDescription>经验照常累计，等级停在封顶处。解除封顶后按经验补升，达成奖励不会重复发放。</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label className="text-xs text-muted-foreground">封顶等级</Label>
          <LevelPicker value={target} onChange={setTarget} min={1} max={state.maxLevel} />
        </div>
        {needsDemote ? (
          <label className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
            <Checkbox checked={demote} onCheckedChange={(value) => setDemote(value === true)} className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="font-medium">同时降级到 Lv.{target}</span>
              <span className="text-xs text-muted-foreground">
                当前 Lv.{state.level} 高于封顶。降级写入升级记录（原因：处罚），已发放的等级奖励不收回{state.locked > target ? `，保底锁定一并降到 Lv.${target}` : ""}。
              </span>
            </span>
          </label>
        ) : null}
        <Textarea value={note} onChange={(event) => setNote(event.target.value)} rows={2} placeholder="原因，例如：刷经验" />
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
          取消
        </Button>
        <Button variant={needsDemote ? "destructive" : "default"} disabled={pending || (needsDemote && !demote)} onClick={() => onSubmit(target, needsDemote && demote, note.trim())}>
          {pending ? <Loader2 className="animate-spin" /> : null}
          {needsDemote ? "封顶并降级" : "封顶"}
        </Button>
      </DialogFooter>
    </>
  );
}

type ForceDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  state: GrowthLevelState;
  pending: boolean;
  onSubmit: (payload: { level: number; topUp: boolean; lock: boolean; skipRewards: boolean; silent: boolean }) => void;
  note: string;
  onNoteChange: (value: string) => void;
};

function ForceDialog(props: ForceDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={(next) => (!props.pending ? props.onOpenChange(next) : undefined)}>
      <DialogContent className="sm:max-w-md">{props.open ? <ForceDialogBody {...props} /> : null}</DialogContent>
    </Dialog>
  );
}

function ForceDialogBody({ onOpenChange, state, pending, onSubmit, note, onNoteChange }: ForceDialogProps) {
  const ceiling = state.capped > 0 ? Math.min(state.capped, state.maxLevel) : state.maxLevel;
  const [target, setTarget] = React.useState(() => Math.min(state.level + 1, ceiling));
  const [options, setOptions] = React.useState({ topUp: true, lock: false, skipRewards: false, silent: false });
  const toggles: Array<{ key: keyof typeof options; label: string; hint: string }> = [
    { key: "topUp", label: "补经验到门槛", hint: "否则经验不变，只调等级" },
    { key: "lock", label: "锁定为保底", hint: "重算并允许降级时也不低于它" },
    { key: "skipRewards", label: "不发经过的达成奖励", hint: "之后也不会自动补发" },
    { key: "silent", label: "不发升级通知", hint: "升级记录照写" }
  ];
  return (
    <>
      <DialogHeader>
        <DialogTitle>强制升级</DialogTitle>
        <DialogDescription>
          越过经验门槛与晋级条件直接调到目标级。{state.capped > 0 ? `已封顶 Lv.${state.capped}，目标不能超过封顶。` : ""}
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4">
        <LevelPicker value={target} onChange={setTarget} min={Math.min(state.level + 1, ceiling)} max={ceiling} />
        <div className="grid gap-2">
          {toggles.map((item) => (
            <label key={item.key} className="flex items-start gap-2.5 text-sm">
              <Checkbox checked={options[item.key]} onCheckedChange={(value) => setOptions((prev) => ({ ...prev, [item.key]: value === true }))} className="mt-0.5" />
              <span className="flex flex-col">
                <span>{item.label}</span>
                <span className="text-xs text-muted-foreground">{item.hint}</span>
              </span>
            </label>
          ))}
        </div>
        <Textarea value={note} onChange={(event) => onNoteChange(event.target.value)} rows={2} placeholder="备注（可选）" />
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
          取消
        </Button>
        <Button disabled={pending || target <= state.level} onClick={() => onSubmit({ level: target, ...options })}>
          {pending ? <Loader2 className="animate-spin" /> : null}
          升到 Lv.{target}
        </Button>
      </DialogFooter>
    </>
  );
}
