"use client";

import { createContext, useContext, useEffect } from "react";
import {
  WorkflowNodePortsData,
  WorkflowPortRender,
  useNodeRender,
  type WorkflowNodeEntity,
  type WorkflowPortEntity
} from "@flowgram.ai/free-layout-editor";
import { AlertTriangle, ArrowDownUp, Check, ChevronsUpDown, CircleAlert, Copy, Gauge, Loader2, Trash2 } from "lucide-react";
import { NODE_WIDTH } from "@/components/apps/item-grant/item-grant-graph";
import {
  ACTION_LABEL,
  NODE_VISUAL,
  PORT_COLOR,
  PROTOCOL_LABEL,
  delayTone,
  formatBytes,
  formatSpeed,
  matchSummary,
  nodeSummary,
  nodeTitle
} from "@/components/configuration/egress/egress-meta";
import { ToneIcon, TONE_TEXT, type Tone } from "@/components/users/detail/kit";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import type {
  EgressAction,
  EgressEndpointStat,
  EgressGraphCatalog,
  EgressIssue,
  EgressNodeType,
  EgressRuleData,
  EgressRuleStat,
  MihomoCoreStatusView,
  MihomoProviderInfo,
  MihomoProxyInfo,
  MihomoRulesData
} from "@/lib/api/egress";
import { cn } from "@/lib/utils";

/**
 * 出海网关画布上的节点卡片（Flowgram 的 renderDefaultNode）。
 *
 * 规则链竖排：网关设置与规则的「规则链 / 下一条」在底边中点，规则的输入在顶边；其余端口在右侧逐行排开，
 * 线路、策略组、节点、订阅、分流的输入在左边。端口是带 `data-port-id` 的元素（useDynamicPort）。
 * 叠加信息走编辑器上下文：校验问题、规则顺序、线路健康与延迟、规则命中数、核心里的延迟与当前选择、追踪路径。
 */

export type EgressCanvasOverlay = {
  catalog?: EgressGraphCatalog;
  issues: Map<string, EgressIssue[]>;
  /** 规则链顺序：节点 → 第几条 */
  chainIndex: Map<string, number>;
  /** 节点 → 网关端点名 */
  bindings: Record<string, string>;
  /** 节点 → 核心里的名字 */
  mihomoNames: Record<string, string>;
  endpoints: Map<string, EgressEndpointStat>;
  rules: Map<string, EgressRuleStat>;
  proxies?: Record<string, MihomoProxyInfo>;
  providers?: Record<string, MihomoProviderInfo> | null;
  core?: MihomoCoreStatusView;
  /** 追踪：路径上的节点 */
  traced: Set<string>;
  testing: Set<string>;
  onPortClick?: (port: WorkflowPortEntity, event: React.MouseEvent) => void;
  onDuplicate?: (node: WorkflowNodeEntity) => void;
  onDelete?: (node: WorkflowNodeEntity) => void;
  onDelay?: (name: string, group: boolean) => void;
  onSelect?: (group: string, member: string) => void;
};

export const EgressOverlayContext = createContext<EgressCanvasOverlay>({
  issues: new Map(),
  chainIndex: new Map(),
  bindings: {},
  mihomoNames: {},
  endpoints: new Map(),
  rules: new Map(),
  traced: new Set(),
  testing: new Set()
});

const ACTION_TONE: Record<EgressAction, Tone> = { proxy: "success", direct: "info", reject: "danger" };

function Pill({ tone, children, className }: { tone: Tone; children: React.ReactNode; className?: string }) {
  const surface: Record<Tone, string> = {
    neutral: "bg-muted text-muted-foreground",
    primary: "bg-primary/10 text-primary",
    success: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    warning: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
    danger: "bg-red-500/10 text-red-700 dark:text-red-300",
    info: "bg-sky-500/10 text-sky-700 dark:text-sky-300",
    violet: "bg-violet-500/10 text-violet-700 dark:text-violet-300"
  };
  return (
    <span className={cn("inline-flex max-w-full shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium leading-5 tabular-nums", surface[tone], className)}>
      {children}
    </span>
  );
}

function DelayPill({ delay, testing, onClick }: { delay?: number; testing?: boolean; onClick?: () => void }) {
  const tone = delayTone(delay);
  return (
    <button
      type="button"
      title="测试延迟"
      onMouseDown={(event) => event.stopPropagation()}
      onClick={(event) => {
        event.stopPropagation();
        onClick?.();
      }}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-semibold leading-5 tabular-nums transition-colors hover:bg-accent",
        TONE_TEXT[tone],
        tone === "neutral" && "text-muted-foreground"
      )}
    >
      {testing ? <Loader2 className="size-3 animate-spin" /> : <Gauge className="size-3" />}
      {testing ? "测试中" : delay ? `${delay} ms` : "测速"}
    </button>
  );
}

export function EgressNodeCard() {
  const overlay = useContext(EgressOverlayContext);
  const { node, data: rawData, selected, startDrag, ports, selectNode, nodeRef, onFocus, onBlur, readonly } = useNodeRender();
  const data = (rawData ?? {}) as Record<string, unknown>;
  const type = node.flowNodeType as EgressNodeType;
  const visual = NODE_VISUAL[type] ?? NODE_VISUAL.rule;
  const spec = overlay.catalog?.nodes.find((candidate) => candidate.type === type);
  const issues = overlay.issues.get(node.id) ?? [];
  const hasError = issues.some((issue) => issue.level === "error");
  const traced = overlay.traced.has(node.id);
  const dimmed = overlay.traced.size > 0 && !traced;
  const note = (data.note as string | undefined)?.trim();

  const outputs = spec?.outputs ?? [];
  const portKey = outputs.map((output) => output.id).join("|");
  useEffect(() => {
    const frame = window.requestAnimationFrame(() => node.getData(WorkflowNodePortsData).updateDynamicPorts());
    return () => window.cancelAnimationFrame(frame);
  }, [node, portKey]);

  const title = nodeTitle(type, data);
  const summary = nodeSummary(type, data, overlay.catalog);
  const mihomoName = overlay.mihomoNames[node.id];
  const endpointName = overlay.bindings[node.id];
  const endpointStat = endpointName ? overlay.endpoints.get(endpointName) : undefined;

  return (
    <>
      <div
        ref={nodeRef}
        draggable
        onDragStart={startDrag}
        onClick={selectNode}
        onFocus={onFocus}
        onBlur={onBlur}
        data-node-selected={String(selected)}
        style={{ width: NODE_WIDTH }}
        className={cn(
          "group/node relative cursor-default rounded-2xl border bg-card text-card-foreground shadow-[0_1px_2px_rgba(15,23,42,0.06),0_4px_16px_-6px_rgba(15,23,42,0.12)] transition-[box-shadow,opacity,border-color] duration-200",
          selected ? "border-primary ring-4 ring-primary/15" : "border-border hover:border-foreground/20",
          hasError && !selected && "border-red-400/80 dark:border-red-500/60",
          traced && !selected && "border-emerald-500 ring-4 ring-emerald-500/15",
          dimmed && "opacity-40"
        )}
      >
        <span className="absolute inset-x-5 top-0 h-[3px] rounded-b-full" style={{ background: visual.accent }} />

        {selected && !readonly ? (
          <div className="absolute -top-10 right-0 flex items-center gap-0.5 rounded-xl border bg-popover p-0.5 shadow-md">
            {!spec?.single ? (
              <NodeAction label="复制" onClick={() => overlay.onDuplicate?.(node)}>
                <Copy />
              </NodeAction>
            ) : null}
            <NodeAction label="删除" danger onClick={() => overlay.onDelete?.(node)}>
              <Trash2 />
            </NodeAction>
          </div>
        ) : null}

        <div className="flex items-start gap-2.5 px-3.5 pt-3.5">
          <ToneIcon icon={visual.icon} tone={visual.tone} size="md" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              {type === "rule" ? (
                <span
                  className="shrink-0 rounded-md px-1.5 text-[11px] font-semibold leading-5 tabular-nums"
                  style={{ background: `color-mix(in oklab, ${visual.accent} 14%, transparent)`, color: visual.accent }}
                >
                  {overlay.chainIndex.has(node.id) ? `#${(overlay.chainIndex.get(node.id) as number) + 1}` : "未接入"}
                </span>
              ) : null}
              <span className="truncate text-[13px] font-semibold leading-5" title={title}>
                {title}
              </span>
              {issues.length ? (
                hasError ? (
                  <CircleAlert className="size-3.5 shrink-0 text-red-500" aria-label="有错误" />
                ) : (
                  <AlertTriangle className="size-3.5 shrink-0 text-amber-500" aria-label="有提醒" />
                )
              ) : null}
            </div>
            <div className="truncate text-[11px] leading-4 text-muted-foreground">{spec?.label ?? type}</div>
          </div>
          <HeaderStatus type={type} data={data} overlay={overlay} endpointStat={endpointStat} mihomoName={mihomoName} />
        </div>

        <div className="space-y-1.5 px-3.5 pt-2 pb-3">
          <CardBody type={type} data={data} summary={summary} overlay={overlay} mihomoName={mihomoName} endpointStat={endpointStat} />
          {note ? <div className="line-clamp-2 pt-0.5 text-[11px] leading-4 text-muted-foreground/80">{note}</div> : null}
        </div>

        {outputs.map((output) => {
          const color = PORT_COLOR[output.id] ?? visual.accent;
          return output.side === "bottom" ? (
            <div key={output.id} className="relative flex h-6 items-center justify-center border-t border-dashed">
              <span className="text-[11px] font-medium text-muted-foreground">{output.label}</span>
              <span data-port-id={output.id} data-port-type="output" data-port-location="bottom" className="absolute -bottom-2.5 left-1/2 size-5 -translate-x-1/2" />
            </div>
          ) : (
            <div key={output.id} className="relative flex h-7 items-center justify-end gap-1.5 border-t border-dashed pr-3.5 pl-3">
              <span className="size-1.5 shrink-0 rounded-full" style={{ background: color }} />
              <span className="truncate text-xs font-medium" style={{ color }}>
                {output.label}
              </span>
              <span data-port-id={output.id} data-port-type="output" data-port-location="right" className="absolute top-1/2 -right-2.5 size-5 -translate-y-1/2" />
            </div>
          );
        })}
      </div>
      {ports.map((port) => {
        const color = port.portType === "output" ? (PORT_COLOR[String(port.portID)] ?? visual.accent) : "#6366f1";
        return (
          <WorkflowPortRender
            key={port.id}
            entity={port}
            primaryColor={color}
            secondaryColor="color-mix(in oklab, var(--muted-foreground) 80%, transparent)"
            backgroundColor="var(--card)"
            onClick={!readonly && port.portType === "output" ? (event, entity) => overlay.onPortClick?.(entity, event) : undefined}
          />
        );
      })}
    </>
  );
}

function HeaderStatus({
  type,
  data,
  overlay,
  endpointStat,
  mihomoName
}: {
  type: EgressNodeType;
  data: Record<string, unknown>;
  overlay: EgressCanvasOverlay;
  endpointStat?: EgressEndpointStat;
  mihomoName?: string;
}) {
  switch (type) {
    case "gateway":
      return data.enabled ? <Pill tone="success">已启用</Pill> : <Pill tone="neutral">未启用</Pill>;
    case "rule": {
      const action = ((data as EgressRuleData).action ?? "proxy") as EgressAction;
      return (data as EgressRuleData).enabled === false ? <Pill tone="neutral">已停用</Pill> : <Pill tone={ACTION_TONE[action]}>{ACTION_LABEL[action]}</Pill>;
    }
    case "endpoint":
      if (!endpointStat) return null;
      return endpointStat.healthy ? (
        <Pill tone={delayTone(endpointStat.latencyMs)}>{endpointStat.latencyMs ? `${endpointStat.latencyMs} ms` : "可用"}</Pill>
      ) : (
        <Pill tone="danger">不可用</Pill>
      );
    case "m_proxy": {
      const info = mihomoName ? overlay.proxies?.[mihomoName] : undefined;
      if (!mihomoName) return null;
      return (
        <DelayPill
          delay={info ? lastDelay(info) : undefined}
          testing={overlay.testing.has(mihomoName)}
          onClick={() => overlay.onDelay?.(mihomoName, false)}
        />
      );
    }
    case "m_group": {
      if (!mihomoName) return null;
      return <DelayPill delay={groupDelay(mihomoName, overlay.proxies)} testing={overlay.testing.has(mihomoName)} onClick={() => overlay.onDelay?.(mihomoName, true)} />;
    }
    case "m_core": {
      const status = overlay.core?.status;
      if (!status) return null;
      const tone: Tone = status === "running" ? "success" : status === "crashed" || status === "failed" ? "danger" : status === "disabled" || status === "stopped" ? "neutral" : "warning";
      const label: Record<string, string> = {
        running: "运行中",
        disabled: "未启用",
        missing: "未安装",
        installing: "安装中",
        starting: "启动中",
        crashed: "异常退出",
        stopped: "已停止",
        failed: "启动失败"
      };
      return <Pill tone={tone}>{label[status] ?? status}</Pill>;
    }
    default:
      return null;
  }
}

function lastDelay(info: MihomoProxyInfo) {
  const history = info.history ?? [];
  return history.length ? history[history.length - 1].delay : undefined;
}

/** 策略组的延迟取它当前选中的成员（逐层往下找）。 */
function groupDelay(name: string, proxies?: Record<string, MihomoProxyInfo>, depth = 0): number | undefined {
  const info = proxies?.[name];
  if (!info) return undefined;
  if (info.now && depth < 6) return groupDelay(info.now, proxies, depth + 1) ?? lastDelay(info);
  return lastDelay(info);
}

function CardBody({
  type,
  data,
  summary,
  overlay,
  mihomoName,
  endpointStat
}: {
  type: EgressNodeType;
  data: Record<string, unknown>;
  summary: string[];
  overlay: EgressCanvasOverlay;
  mihomoName?: string;
  endpointStat?: EgressEndpointStat;
}) {
  if (type === "rule") {
    const rule = data as EgressRuleData;
    const stat = overlay.rules.get(rule.name);
    const chips = [
      ...(rule.match?.domainSuffixes ?? []),
      ...(rule.match?.domains ?? []),
      ...(rule.match?.profiles ?? []).map((item) => `调用方 ${item}`),
      ...(rule.match?.cidrs ?? [])
    ];
    return (
      <>
        {chips.length ? (
          <div className="flex flex-wrap gap-1">
            {chips.slice(0, 5).map((chip) => (
              <span key={chip} className="max-w-full truncate rounded-md bg-muted px-1.5 font-mono text-[11px] leading-5 text-foreground/80">
                {chip}
              </span>
            ))}
            {chips.length > 5 ? <span className="px-1 text-[11px] leading-5 text-muted-foreground">另 {chips.length - 5} 项</span> : null}
          </div>
        ) : (
          <div className="truncate text-xs text-foreground/85">{matchSummary(rule.match)[0]}</div>
        )}
        <div className="flex items-center justify-between text-[11px] text-muted-foreground">
          <span className="truncate">{rule.match?.matchAll ? "兜底规则" : matchSummary(rule.match).length > 1 ? matchSummary(rule.match)[1] : ""}</span>
          {stat ? <span className="shrink-0 tabular-nums">命中 {stat.matched.toLocaleString()} 次</span> : null}
        </div>
      </>
    );
  }

  if (type === "endpoint") {
    const protocol = String(data.protocol ?? "");
    return (
      <>
        <div className="flex items-center gap-1.5">
          <Pill tone="info">{PROTOCOL_LABEL[protocol] ?? protocol}</Pill>
          <span className="truncate font-mono text-[11px] text-foreground/80">{String(data.address ?? "") || "未填地址"}</span>
        </div>
        {endpointStat ? (
          <div className="flex items-center justify-between text-[11px] text-muted-foreground">
            <span className="tabular-nums">
              {endpointStat.dials.toLocaleString()} 次连接，{endpointStat.failures} 次失败
            </span>
            <span className="tabular-nums">{formatBytes(endpointStat.bytesIn + endpointStat.bytesOut)}</span>
          </div>
        ) : null}
        {endpointStat?.lastError ? <div className="line-clamp-2 text-[11px] leading-4 text-red-600 dark:text-red-400">{endpointStat.lastError}</div> : null}
      </>
    );
  }

  if (type === "m_group" && mihomoName) {
    const info = overlay.proxies?.[mihomoName];
    const group = (data.group ?? {}) as Record<string, unknown>;
    return (
      <>
        <div className="truncate text-xs text-foreground/85">{summary.join("，")}</div>
        {info?.now ? (
          <GroupSelector name={mihomoName} info={info} selectable={group.type === "select" || group.type === "url-test" || group.type === "fallback"} overlay={overlay} />
        ) : (
          <div className="text-[11px] text-muted-foreground">应用后显示当前节点</div>
        )}
      </>
    );
  }

  if (type === "m_provider" && mihomoName) {
    const info = overlay.providers?.[mihomoName];
    const usage = info?.subscriptionInfo;
    const used = usage ? usage.Upload + usage.Download : 0;
    return (
      <>
        {summary.map((line, index) => (
          <div key={index} className={cn("truncate text-xs leading-[18px]", index === 0 ? "text-foreground/85" : "text-muted-foreground")}>
            {line}
          </div>
        ))}
        {info ? <div className="text-[11px] text-muted-foreground">{(info.proxies ?? []).length} 个节点</div> : null}
        {usage && usage.Total > 0 ? (
          <div className="space-y-1">
            <div className="h-1.5 overflow-hidden rounded-full bg-muted">
              <div className="h-full rounded-full bg-cyan-500" style={{ width: `${Math.min(100, (used / usage.Total) * 100)}%` }} />
            </div>
            <div className="flex justify-between text-[11px] tabular-nums text-muted-foreground">
              <span>
                {formatBytes(used)} / {formatBytes(usage.Total)}
              </span>
              {usage.Expire ? <span>{new Date(usage.Expire * 1000).toLocaleDateString()} 到期</span> : null}
            </div>
          </div>
        ) : null}
      </>
    );
  }

  if (type === "m_rules") {
    const rules = (data as MihomoRulesData).rules ?? [];
    const counts = new Map<string, number>();
    for (const rule of rules) if (rule.enabled !== false) counts.set(rule.target, (counts.get(rule.target) ?? 0) + 1);
    const top = [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4);
    return (
      <>
        <div className="truncate text-xs text-foreground/85">{summary[0]}</div>
        <div className="flex flex-wrap gap-1">
          {top.map(([target, count]) => (
            <span key={target} className="inline-flex max-w-full items-center gap-1 rounded-md bg-pink-500/10 px-1.5 text-[11px] leading-5 text-pink-700 dark:text-pink-300">
              <span className="truncate">{target}</span>
              <span className="tabular-nums opacity-70">{count}</span>
            </span>
          ))}
        </div>
      </>
    );
  }

  if (type === "m_core" && overlay.core) {
    const core = overlay.core;
    return (
      <>
        <div className="truncate text-xs text-foreground/85">{core.version ? `mihomo ${core.version}` : summary[0]}</div>
        {core.status === "running" ? (
          <div className="grid grid-cols-3 gap-1 text-[11px] tabular-nums">
            <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
              <ArrowDownUp className="size-3" />
              {formatSpeed(core.downloadSpeed)}
            </span>
            <span className="text-sky-600 dark:text-sky-400">{formatSpeed(core.uploadSpeed)}</span>
            <span className="text-right text-muted-foreground">{formatBytes(core.memoryInUse)}</span>
          </div>
        ) : core.lastError ? (
          <div className="line-clamp-2 text-[11px] leading-4 text-red-600 dark:text-red-400">{core.lastError}</div>
        ) : (
          <div className="text-[11px] text-muted-foreground">{summary[1]}</div>
        )}
      </>
    );
  }

  if (type === "gateway") {
    return (
      <>
        {summary.map((line, index) => (
          <div key={index} className={cn("truncate text-xs leading-[18px]", index === 0 ? "text-foreground/85" : "text-muted-foreground")}>
            {line}
          </div>
        ))}
      </>
    );
  }

  return (
    <>
      {summary.map((line, index) => (
        <div key={index} className={cn("truncate text-xs leading-[18px]", index === 0 ? "text-foreground/85" : "font-mono text-[11px] text-muted-foreground")} title={line}>
          {line}
        </div>
      ))}
    </>
  );
}

function GroupSelector({ name, info, selectable, overlay }: { name: string; info: MihomoProxyInfo; selectable: boolean; overlay: EgressCanvasOverlay }) {
  const members = info.all ?? [];
  const content = (
    <span className="flex min-w-0 items-center gap-1.5">
      <span className="size-1.5 shrink-0 rounded-full bg-amber-500" />
      <span className="truncate text-xs font-medium">{info.now}</span>
      {info.fixed ? <span className="shrink-0 text-[10px] text-muted-foreground">已固定</span> : null}
    </span>
  );
  if (!selectable || !members.length) return <div className="flex items-center rounded-lg bg-muted/60 px-2 py-1">{content}</div>;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          onMouseDown={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
          className="flex w-full items-center justify-between gap-2 rounded-lg bg-muted/60 px-2 py-1 text-left transition-colors hover:bg-muted"
        >
          {content}
          <ChevronsUpDown className="size-3 shrink-0 text-muted-foreground" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 w-64 overflow-y-auto">
        <DropdownMenuLabel className="text-xs">切换「{name}」的节点（立即生效）</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {members.map((member) => {
          const delay = overlay.proxies?.[member] ? lastDelay(overlay.proxies[member]) : undefined;
          return (
            <DropdownMenuItem key={member} className="justify-between gap-2 text-xs" onSelect={() => overlay.onSelect?.(name, member)}>
              <span className="flex min-w-0 items-center gap-1.5">
                {member === info.now ? <Check className="size-3.5 shrink-0" /> : <span className="size-3.5 shrink-0" />}
                <span className="truncate">{member}</span>
              </span>
              <span className={cn("shrink-0 tabular-nums", TONE_TEXT[delayTone(delay)], !delay && "text-muted-foreground")}>{delay ? `${delay} ms` : "—"}</span>
            </DropdownMenuItem>
          );
        })}
        {info.fixed ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-xs" onSelect={() => overlay.onSelect?.(name, "")}>
              取消固定，恢复自动选择
            </DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function NodeAction({ label, danger, onClick, children }: { label: string; danger?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onMouseDown={(event) => event.stopPropagation()}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
      className={cn(
        "flex size-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent [&>svg]:size-3.5",
        danger ? "hover:text-destructive" : "hover:text-foreground"
      )}
    >
      {children}
    </button>
  );
}
