"use client";

import { useMemo } from "react";
import { getNodeForm, useClientContext, type WorkflowNodeEntity } from "@flowgram.ai/free-layout-editor";
import { ArrowDown, ArrowUp } from "lucide-react";
import { NODE_VISUAL, nodeTitle } from "@/components/configuration/egress/egress-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import { Button } from "@/components/ui/button";
import type { EgressEndpointStat, EgressGraphCatalog, EgressNodeType, EgressRuleStat } from "@/lib/api/egress";

/** 检查器共用：写回节点配置、上下文、连出去的线路顺序。 */

export type Patch = (next: Record<string, unknown>) => void;

export type InspectorContext = {
  catalog: EgressGraphCatalog;
  /** 画布上 mihomo 节点与策略组的名字（分流目标、下载经由的候选） */
  mihomoTargets: string[];
  /** 画布变化计数：连线顺序、候选名单跟着重算 */
  revision: number;
  endpointStat?: EgressEndpointStat;
  ruleStat?: EgressRuleStat;
  onDelay?: (name: string, group: boolean) => void;
};

// ── 连出去的线路顺序 ──

type ConnectedTarget = { id: string; type: EgressNodeType; title: string };

/** 某个端口连出去的节点，按 order 排序（与后端 orderEdges 同一规则）。 */
export function useConnectedTargets(node: WorkflowNodeEntity, port: string, order: string[] | undefined, revision: number): ConnectedTarget[] {
  const ctx = useClientContext();
  return useMemo(() => {
    const lines = ctx.document.linesManager.getAllLines().filter((line) => line.from?.id === node.id && String(line.fromPort?.portID ?? "") === port && line.to);
    const targets = lines.map((line) => {
      const target = line.to as WorkflowNodeEntity;
      const values = readNodeValues(target);
      return { id: target.id, type: target.flowNodeType as EgressNodeType, title: nodeTitle(target.flowNodeType as EgressNodeType, values) };
    });
    const rank = new Map((order ?? []).map((id, index) => [id, index]));
    return targets.sort((a, b) => (rank.get(a.id) ?? 1e6) - (rank.get(b.id) ?? 1e6));
  }, [ctx, node, port, order, revision]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** 读另一个节点的配置：Flowgram 把节点配置存在节点表单里。 */
export function readNodeValues(node: WorkflowNodeEntity) {
  return (getNodeForm(node)?.values ?? {}) as Record<string, unknown>;
}

export function TargetOrder({
  targets,
  onChange,
  empty,
  hint
}: {
  targets: ConnectedTarget[];
  onChange: (order: string[]) => void;
  empty: string;
  hint?: string;
}) {
  if (!targets.length) return <div className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">{empty}</div>;
  const move = (index: number, delta: number) => {
    const next = targets.map((target) => target.id);
    const [item] = next.splice(index, 1);
    next.splice(index + delta, 0, item);
    onChange(next);
  };
  return (
    <div className="space-y-1.5">
      <div className="overflow-hidden rounded-xl border">
        {targets.map((target, index) => {
          const visual = NODE_VISUAL[target.type];
          return (
            <div key={target.id} className="flex items-center gap-2 border-b px-2 py-1.5 last:border-b-0">
              <span className="w-4 text-center text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
              <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
              <span className="min-w-0 flex-1 truncate text-xs">{target.title}</span>
              <Button size="icon" variant="ghost" className="size-6" disabled={index === 0} onClick={() => move(index, -1)} aria-label="上移">
                <ArrowUp className="size-3" />
              </Button>
              <Button size="icon" variant="ghost" className="size-6" disabled={index === targets.length - 1} onClick={() => move(index, 1)} aria-label="下移">
                <ArrowDown className="size-3" />
              </Button>
            </div>
          );
        })}
      </div>
      {hint ? <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

