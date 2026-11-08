import { Badge } from "@/components/ui/badge";
import type { ListKind, RiskAction, RiskLevel } from "@/lib/api/risk";

import { ACTION_LABEL, ACTION_TONE, KIND_LABEL, KIND_TONE, LEVEL_LABEL, LEVEL_TONE } from "./labels";

export function ActionBadge({ action, size = "sm" }: { action: RiskAction | string; size?: "sm" | "default" }) {
  const a = action as RiskAction;
  return <Badge variant={ACTION_TONE[a] ?? "secondary"} size={size}>{ACTION_LABEL[a] ?? action}</Badge>;
}

export function LevelBadge({ level, size = "sm" }: { level: RiskLevel | string; size?: "sm" | "default" }) {
  const l = level as RiskLevel;
  return <Badge variant={LEVEL_TONE[l] ?? "secondary"} size={size}>{LEVEL_LABEL[l] ?? level}</Badge>;
}

export function KindBadge({ kind }: { kind: ListKind | string }) {
  const k = kind as ListKind;
  return <Badge variant={KIND_TONE[k] ?? "secondary"} size="sm">{KIND_LABEL[k] ?? kind}</Badge>;
}

/** 分数刻度条：0–100+，到 100 封顶 */
export function ScoreMeter({ score, className }: { score: number; className?: string }) {
  const pct = Math.max(0, Math.min(100, score));
  const tone = score >= 100 ? "bg-destructive" : score >= 80 ? "bg-orange-500" : score >= 60 ? "bg-amber-500" : score >= 30 ? "bg-sky-500" : "bg-emerald-500";
  return (
    <div className={className}>
      <div className="flex items-center gap-2">
        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
          <div className={`h-full rounded-full ${tone}`} style={{ width: `${pct}%` }} />
        </div>
        <span className="w-8 text-right text-xs tabular-nums text-muted-foreground">{score}</span>
      </div>
    </div>
  );
}
