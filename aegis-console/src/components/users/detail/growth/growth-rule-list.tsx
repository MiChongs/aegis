"use client";

/**
 * 规则卡片列表：左侧色条表示效果（奖励 / 处罚 / 冻结），大号倍率、类型、来源、有效期与剩余时间一眼可读；
 * 右侧是编辑、暂停 / 恢复、撤销、删除。撤销保留记录，删除只对已撤销或已到期的规则开放。
 */

import * as React from "react";
import { Coins, Globe, MoreHorizontal, Pause, PencilLine, Play, Sparkles, Trash2, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { ApiError } from "@/lib/api-client";
import { type GrowthCatalog, type GrowthRule } from "@/lib/api/growth";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useDeleteGrowthRuleMutation, useRevokeGrowthRuleMutation, useSaveGrowthRuleMutation } from "@/lib/growth-hooks";
import { cn } from "@/lib/utils";
import { ConfirmActionDialog } from "../records/records-shared";
import { EFFECT_SURFACE, EffectPill, FactorText, ROUNDING_LABEL, STATUS_META, WindowBar, remainingText } from "./growth-kit";

export function GrowthRuleList({
  appKey,
  rules,
  catalog,
  now,
  onEdit,
  empty
}: {
  appKey: string;
  rules: GrowthRule[];
  catalog?: GrowthCatalog;
  now: number;
  onEdit: (rule: GrowthRule) => void;
  empty: React.ReactNode;
}) {
  const save = useSaveGrowthRuleMutation(appKey);
  const revoke = useRevokeGrowthRuleMutation(appKey);
  const remove = useDeleteGrowthRuleMutation(appKey);
  const [pending, setPending] = React.useState<{ kind: "revoke" | "delete"; rule: GrowthRule } | null>(null);
  const [note, setNote] = React.useState("");

  const sourceLabel = React.useCallback((key: string) => catalog?.sources.find((s) => s.key === key)?.label ?? key, [catalog]);

  async function toggle(rule: GrowthRule) {
    try {
      await save.mutateAsync({
        ruleId: rule.id,
        payload: {
          numerator: rule.numerator,
          denominator: rule.denominator,
          rounding: rule.rounding,
          dailyCap: rule.dailyCap,
          sources: rule.sources,
          startsAt: rule.startsAt ?? null,
          endsAt: rule.endsAt ?? null,
          enabled: !rule.enabled,
          label: rule.label,
          reason: rule.reason
        }
      });
      toast.success(rule.enabled ? "规则已暂停" : "规则已恢复");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  async function confirm() {
    if (!pending) return;
    try {
      if (pending.kind === "revoke") {
        await revoke.mutateAsync({ ruleId: pending.rule.id, note: note.trim() || undefined });
        toast.success("规则已撤销，立即失效");
      } else {
        await remove.mutateAsync(pending.rule.id);
        toast.success("规则已删除");
      }
      setPending(null);
      setNote("");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  if (!rules.length) return <>{empty}</>;

  return (
    <>
      <ul className="flex flex-col divide-y">
        {rules.map((rule) => {
          const status = STATUS_META[rule.status];
          const ended = rule.status === "revoked" || rule.status === "expired";
          const left = rule.status === "active" ? remainingText(rule.endsAt, now) : rule.status === "scheduled" ? remainingText(rule.startsAt, now) : "";
          return (
            <li key={rule.id} className={cn("relative flex gap-3 py-3.5 pr-1 pl-4", ended && "opacity-60")}>
              <span className={cn("absolute inset-y-3 left-0 w-1 rounded-full", EFFECT_SURFACE[rule.effect])} aria-hidden />
              <div className="flex w-16 shrink-0 flex-col items-start gap-0.5">
                <FactorText num={rule.numerator} den={rule.denominator} dailyCap={rule.dailyCap} className="text-xl leading-7" />
                <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
                  {rule.kind === "integral" ? <Coins className="size-3" /> : <Sparkles className="size-3" />}
                  {rule.kind === "integral" ? "积分" : "经验"}
                </span>
              </div>
              <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="truncate text-sm font-medium">{rule.label || (rule.userId > 0 ? "个人规则" : "全应用规则")}</span>
                  <EffectPill effect={rule.effect} />
                  <Badge variant={status.variant} size="sm">
                    {status.label}
                  </Badge>
                  {rule.userId === 0 ? (
                    <Badge variant="outline" size="sm">
                      <Globe />
                      全应用
                    </Badge>
                  ) : null}
                </div>
                <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>{ROUNDING_LABEL[rule.rounding]}</span>
                  <span>{rule.dailyCap > 0 ? `每日上限 ${rule.dailyCap.toLocaleString("zh-CN")}` : "不限每日上限"}</span>
                  <span>{rule.sources.length ? rule.sources.map(sourceLabel).join("、") : "全部默认来源"}</span>
                </div>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>
                    {rule.startsAt ? formatDateTimeMinute(rule.startsAt) : "立即"} → {rule.endsAt ? formatDateTimeMinute(rule.endsAt) : "长期"}
                  </span>
                  {left ? <span className="font-medium text-foreground/80">{rule.status === "scheduled" ? `${left}后生效` : `剩余 ${left}`}</span> : null}
                </div>
                {rule.status === "active" ? <WindowBar startsAt={rule.startsAt} endsAt={rule.endsAt} now={now} /> : null}
                {rule.reason ? <p className="line-clamp-2 text-xs leading-5 text-foreground/80">{rule.reason}</p> : null}
                <p className="text-[11px] text-muted-foreground">
                  {rule.createdBy || "系统"} 创建于 {formatRelative(rule.createdAt)}
                  {rule.revokedAt ? `，${rule.revokedBy || "管理员"} 撤销于 ${formatRelative(rule.revokedAt)}` : ""}
                </p>
              </div>
              <div className="flex shrink-0 items-start gap-0.5">
                {!ended ? (
                  <Button size="icon-sm" variant="ghost" onClick={() => onEdit(rule)} aria-label="编辑">
                    <PencilLine />
                  </Button>
                ) : null}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="icon-sm" variant="ghost" aria-label="更多">
                      <MoreHorizontal />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    {!ended ? (
                      <>
                        <DropdownMenuItem onSelect={() => void toggle(rule)}>
                          {rule.enabled ? <Pause /> : <Play />}
                          {rule.enabled ? "暂停" : "恢复"}
                        </DropdownMenuItem>
                        <DropdownMenuItem onSelect={() => setPending({ kind: "revoke", rule })}>
                          <Undo2 />
                          撤销
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                      </>
                    ) : null}
                    <DropdownMenuItem variant="destructive" disabled={!ended} onSelect={() => setPending({ kind: "delete", rule })}>
                      <Trash2 />
                      删除{!ended ? "（先撤销）" : ""}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </li>
          );
        })}
      </ul>

      <ConfirmActionDialog
        open={Boolean(pending)}
        onOpenChange={(open) => (!open ? setPending(null) : undefined)}
        title={pending?.kind === "delete" ? "删除规则" : "撤销规则"}
        description={
          pending?.kind === "delete"
            ? "规则从列表移除，调控记录仍保留这次删除。"
            : pending?.rule.userId === 0
              ? "全应用规则撤销后，所有用户立即恢复原来的获取速度。"
              : "撤销后立即失效，之后的收入按其余规则计算。"
        }
        confirmLabel={pending?.kind === "delete" ? "删除" : "撤销"}
        destructive={pending?.kind === "delete"}
        pending={revoke.isPending || remove.isPending}
        note={pending?.kind === "revoke" ? { value: note, onChange: setNote, placeholder: "撤销原因（可选）" } : undefined}
        onConfirm={confirm}
      />
    </>
  );
}
