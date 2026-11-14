"use client";

import { memo, useCallback, useMemo, useState, type ReactNode } from "react";
import {
  Ban,
  Check,
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  CircleDot,
  Lock,
  Minus,
  Search,
  ShieldAlert,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { usePermissionRoutesQuery } from "@/lib/access-hooks";
import type { PermissionNode } from "@/lib/api/access";
import { cn } from "@/lib/utils";
import { CodeText, DangerTag, NodeIcon, ScopeTag } from "./access-kit";
import {
  METHOD_TONE,
  scopeApplies,
  tallyNode,
  visibleKeys,
  type NodeTally,
  type TreeFilter,
  type TreeIndex,
} from "./tree-model";

/**
 * 权限树编辑器（对标若依「菜单权限」与金蝶「功能权限」的勾选树）。
 *
 * 三种用法：
 *   - edit：角色功能权限、个人授权。每个操作节点三态（未设置 / 允许 / 禁止），
 *     落笔模式决定点击一个节点是在「允许」还是「禁止」它（连同全部后代）。
 *   - view：生效权限的只读展示。
 *   - pick：单选一个权限点（临时权限、权限诊断）。
 *
 * 内置角色的代码定义经 `locked` 传入：锁定为已允许，只能额外允许或用禁止压过它。
 * 对应用级角色，平台级权限点不会生效，经 `roleScope="app"` 置灰且不可勾选。
 */

export type GrantState = Map<string, "allow" | "deny">;

type Mode = "edit" | "view" | "pick";

type Props = {
  index: TreeIndex;
  mode?: Mode;
  value?: GrantState;
  onChange?: (next: GrantState) => void;
  locked?: Set<string>;
  allowDeny?: boolean;
  roleScope?: "global" | "app" | "any";
  picked?: string | null;
  onPick?: (code: string) => void;
  toolbarExtra?: ReactNode;
  className?: string;
  /** 树区域高度（滚动容器） */
  heightClassName?: string;
  defaultExpandDepth?: number;
};

export function PermissionTreeEditor({
  index,
  mode = "edit",
  value,
  onChange,
  locked,
  allowDeny = true,
  roleScope = "any",
  picked,
  onPick,
  toolbarExtra,
  className,
  heightClassName = "max-h-[62vh]",
  defaultExpandDepth = 0,
}: Props) {
  const [keyword, setKeyword] = useState("");
  const [onlySelected, setOnlySelected] = useState(false);
  const [onlyDanger, setOnlyDanger] = useState(false);
  const [paint, setPaint] = useState<"allow" | "deny">("allow");
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const initial = new Set<string>();
    for (const key of index.order) {
      const entry = index.entries.get(key)!;
      if (entry.node.kind !== "action" && entry.depth <= defaultExpandDepth)
        initial.add(key);
    }
    return initial;
  });

  const lockedSet = useMemo(() => locked ?? new Set<string>(), [locked]);
  const state = useCallback((code: string) => value?.get(code), [value]);
  const applicable = useCallback(
    (code: string) => {
      const entry = index.entries.get(index.codeKey.get(code) ?? "");
      return entry ? scopeApplies(entry.node.scope, roleScope) : false;
    },
    [index, roleScope],
  );

  const filter: TreeFilter = { keyword, onlySelected, onlyDanger };
  const visible = useMemo(
    () =>
      visibleKeys(index, filter, (code) => {
        if (mode === "pick") return code === picked;
        return Boolean(value?.get(code)) || lockedSet.has(code);
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [index, keyword, onlySelected, onlyDanger, value, lockedSet, picked, mode],
  );
  const filtering = visible !== null;

  const tallies = useMemo(() => {
    const map = new Map<string, NodeTally>();
    for (const key of index.order) {
      map.set(
        key,
        tallyNode(index.entries.get(key)!, state, lockedSet, applicable),
      );
    }
    return map;
  }, [index, state, lockedSet, applicable]);

  const totals = useMemo(() => {
    let allow = 0;
    let deny = 0;
    let applicableCount = 0;
    for (const code of index.codes) {
      if (!applicable(code)) continue;
      applicableCount++;
      const v = value?.get(code);
      if (v === "deny") deny++;
      else if (v === "allow" || lockedSet.has(code)) allow++;
    }
    return { allow, deny, applicable: applicableCount };
  }, [index, value, lockedSet, applicable]);

  const toggleExpand = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const expandAll = () =>
    setExpanded(
      new Set(
        index.order.filter(
          (key) => index.entries.get(key)!.node.kind !== "action",
        ),
      ),
    );
  const collapseAll = () => setExpanded(new Set());

  const apply = useCallback(
    (key: string) => {
      if (mode !== "edit" || !onChange) return;
      const entry = index.entries.get(key);
      if (!entry) return;
      const tally = tallies.get(key)!;
      const next = new Map(value ?? []);
      const leaves = entry.leaves.filter(applicable);
      if (paint === "allow") {
        const full = tally.allow === tally.applicable && tally.deny === 0;
        for (const code of leaves) {
          if (full) {
            if (next.get(code) === "allow") next.delete(code);
          } else if (!lockedSet.has(code)) {
            next.set(code, "allow");
          } else {
            next.delete(code); // 锁定项本来就允许，清掉可能压着它的禁止
          }
        }
      } else {
        const full = tally.deny === tally.applicable;
        for (const code of leaves) {
          if (full) next.delete(code);
          else next.set(code, "deny");
        }
      }
      onChange(next);
    },
    [mode, onChange, index, tallies, value, applicable, paint, lockedSet],
  );

  const setAll = (target: "allow" | "none") => {
    if (mode !== "edit" || !onChange) return;
    const next: GrantState = new Map();
    if (target === "allow") {
      for (const code of index.codes)
        if (applicable(code) && !lockedSet.has(code)) next.set(code, "allow");
    }
    onChange(next);
  };

  const renderNodes = (nodes: PermissionNode[]): ReactNode =>
    nodes.map((node) => {
      if (visible && !visible.has(node.key)) return null;
      const isOpen = filtering || expanded.has(node.key);
      return (
        <div
          key={node.key}
          role="treeitem"
          aria-selected={node.code ? node.code === picked : false}
          aria-expanded={node.kind !== "action" ? isOpen : undefined}
        >
          <TreeRow
            node={node}
            depth={index.entries.get(node.key)!.depth}
            tally={tallies.get(node.key)!}
            open={isOpen}
            mode={mode}
            state={node.code ? value?.get(node.code) : undefined}
            locked={Boolean(node.code && lockedSet.has(node.code))}
            applicable={
              node.code
                ? applicable(node.code)
                : tallies.get(node.key)!.applicable > 0
            }
            picked={Boolean(node.code && node.code === picked)}
            onToggleOpen={toggleExpand}
            onApply={apply}
            onPick={onPick}
          />
          {node.kind !== "action" && isOpen && node.children && (
            <div role="group">{renderNodes(node.children)}</div>
          )}
        </div>
      );
    });

  return (
    <div className={cn("flex min-w-0 flex-col", className)}>
      <div className="flex flex-wrap items-center gap-2 border-b border-border/70 px-3 py-2.5">
        <div className="relative min-w-44 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索名称或权限字符"
            className="h-8 pl-8 text-xs"
          />
          {keyword && (
            <button
              type="button"
              aria-label="清除"
              onClick={() => setKeyword("")}
              className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          )}
        </div>
        {mode === "edit" && allowDeny && (
          <div
            className="flex h-8 items-center rounded-lg border bg-muted/40 p-0.5"
            role="radiogroup"
            aria-label="落笔模式"
          >
            <PaintButton
              active={paint === "allow"}
              onClick={() => setPaint("allow")}
              tone="allow"
            >
              <Check className="size-3.5" />
              允许
            </PaintButton>
            <PaintButton
              active={paint === "deny"}
              onClick={() => setPaint("deny")}
              tone="deny"
            >
              <Ban className="size-3.5" />
              禁止
            </PaintButton>
          </div>
        )}
        <FilterChip
          active={onlySelected}
          onClick={() => setOnlySelected((v) => !v)}
        >
          <CircleDot className="size-3.5" />
          {mode === "pick" ? "已选" : "已设置"}
        </FilterChip>
        <FilterChip
          active={onlyDanger}
          onClick={() => setOnlyDanger((v) => !v)}
        >
          <ShieldAlert className="size-3.5" />
          高危
        </FilterChip>
        <div className="ml-auto flex items-center gap-1">
          <IconAction
            label="全部展开"
            onClick={expandAll}
            icon={ChevronsUpDown}
          />
          <IconAction
            label="全部折叠"
            onClick={collapseAll}
            icon={ChevronsDownUp}
          />
          {mode === "edit" && (
            <>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8 px-2 text-xs"
                onClick={() => setAll("allow")}
              >
                全选
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8 px-2 text-xs"
                onClick={() => setAll("none")}
              >
                清空
              </Button>
            </>
          )}
          {toolbarExtra}
        </div>
      </div>

      {mode !== "pick" && (
        <div className="flex items-center gap-4 border-b border-border/70 bg-muted/20 px-3 py-1.5 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-primary" />
            允许{" "}
            <b className="font-semibold text-foreground tabular-nums">
              {totals.allow}
            </b>
          </span>
          {allowDeny && (
            <span className="inline-flex items-center gap-1.5">
              <span className="size-2 rounded-full bg-rose-500" />
              禁止{" "}
              <b className="font-semibold text-foreground tabular-nums">
                {totals.deny}
              </b>
            </span>
          )}
          {lockedSet.size > 0 && (
            <span className="inline-flex items-center gap-1.5">
              <Lock className="size-3" />
              内置{" "}
              <b className="font-semibold text-foreground tabular-nums">
                {lockedSet.size}
              </b>
            </span>
          )}
          <span className="ml-auto tabular-nums">
            共 {totals.applicable} 项
          </span>
        </div>
      )}

      <div
        role="tree"
        className={cn(
          "min-h-0 overflow-y-auto overscroll-contain py-1",
          heightClassName,
        )}
      >
        {renderNodes(index.roots)}
        {visible && visible.size === 0 && (
          <div className="py-10 text-center text-xs text-muted-foreground">
            暂无匹配项
          </div>
        )}
      </div>
    </div>
  );
}

// ── 行 ──

type RowProps = {
  node: PermissionNode;
  depth: number;
  tally: NodeTally;
  open: boolean;
  mode: Mode;
  state?: "allow" | "deny";
  locked: boolean;
  applicable: boolean;
  picked: boolean;
  onToggleOpen: (key: string) => void;
  onApply: (key: string) => void;
  onPick?: (code: string) => void;
};

const TreeRow = memo(function TreeRow({
  node,
  depth,
  tally,
  open,
  mode,
  state,
  locked,
  applicable,
  picked,
  onToggleOpen,
  onApply,
  onPick,
}: RowProps) {
  const isAction = node.kind === "action";
  const interactive =
    applicable && (mode === "edit" || (mode === "pick" && isAction));

  const handleRow = () => {
    if (!isAction) {
      onToggleOpen(node.key);
      return;
    }
    if (mode === "pick" && node.code && applicable) onPick?.(node.code);
    else if (mode === "edit" && applicable) onApply(node.key);
  };

  return (
    <div
      className={cn(
        "group flex h-9 min-w-0 items-center gap-1.5 pr-3 text-sm transition-colors",
        node.kind === "dir" && "mt-1 h-10 font-semibold",
        interactive || !isAction
          ? "cursor-pointer hover:bg-accent/60"
          : "cursor-default",
        picked && "bg-primary/8 hover:bg-primary/10",
        !applicable && "opacity-45",
      )}
      style={{ paddingLeft: 8 + depth * 22 }}
      onClick={handleRow}
    >
      <span className="grid size-5 shrink-0 place-items-center text-muted-foreground">
        {!isAction && (
          <ChevronRight
            className={cn(
              "size-3.5 transition-transform duration-200",
              open && "rotate-90",
            )}
          />
        )}
      </span>

      {mode !== "pick" ? (
        <TriBox
          tally={tally}
          isAction={isAction}
          state={state}
          locked={locked}
          disabled={!interactive}
          onClick={(event) => {
            event.stopPropagation();
            if (mode === "edit" && applicable) onApply(node.key);
          }}
        />
      ) : (
        isAction && (
          <span
            className={cn(
              "grid size-4 shrink-0 place-items-center rounded-full border transition-colors",
              picked
                ? "border-primary bg-primary text-primary-foreground"
                : "border-input",
            )}
          >
            {picked && <span className="size-1.5 rounded-full bg-current" />}
          </span>
        )
      )}

      {!isAction && (
        <span
          className={cn(
            "grid shrink-0 place-items-center rounded-md",
            node.kind === "dir"
              ? "size-6 bg-primary/10 text-primary"
              : "size-5 text-muted-foreground",
          )}
        >
          <NodeIcon name={node.icon} className="size-3.5" />
        </span>
      )}

      <span
        className={cn(
          "min-w-0 truncate",
          isAction ? "text-[13px]" : node.kind === "menu" ? "font-medium" : "",
          state === "deny" &&
            "text-rose-600 line-through decoration-rose-400/60 dark:text-rose-400",
        )}
      >
        {node.name}
      </span>

      {!isAction && mode !== "pick" && tally.applicable > 0 && (
        <span className="ml-1 shrink-0 text-[11px] text-muted-foreground tabular-nums">
          {tally.allow}/{tally.applicable}
        </span>
      )}

      {!isAction &&
        node.kind === "menu" &&
        mode !== "pick" &&
        tally.applicable > 0 && (
          <span className="hidden h-1 w-14 shrink-0 overflow-hidden rounded-full bg-muted sm:block">
            <span
              className={cn(
                "block h-full",
                tally.deny > 0 ? "bg-rose-500" : "bg-primary",
              )}
              style={{
                width: `${((tally.allow + tally.deny) / tally.applicable) * 100}%`,
              }}
            />
          </span>
        )}

      <span className="ml-auto flex shrink-0 items-center gap-1.5 pl-2">
        {node.danger && <DangerTag />}
        {isAction && <ScopeTag scope={node.scope} />}
        {isAction && node.code && (
          <RoutesPopover code={node.code} count={node.routes ?? 0} />
        )}
        {isAction && node.code && (
          <CodeText className="hidden max-w-56 truncate md:inline">
            {node.code}
          </CodeText>
        )}
      </span>
    </div>
  );
});

function TriBox({
  tally,
  isAction,
  state,
  locked,
  disabled,
  onClick,
}: {
  tally: NodeTally;
  isAction: boolean;
  state?: "allow" | "deny";
  locked: boolean;
  disabled: boolean;
  onClick: (event: React.MouseEvent) => void;
}) {
  let look: "none" | "allow" | "partial" | "deny" | "denyPartial" | "locked" =
    "none";
  if (isAction) {
    if (state === "deny") look = "deny";
    else if (state === "allow") look = "allow";
    else if (locked) look = "locked";
  } else if (tally.applicable > 0) {
    if (tally.deny === tally.applicable) look = "deny";
    else if (tally.allow === tally.applicable && tally.deny === 0)
      look = tally.locked === tally.applicable ? "locked" : "allow";
    else if (tally.deny > 0 && tally.allow === 0) look = "denyPartial";
    else if (tally.allow > 0 || tally.deny > 0) look = "partial";
  }
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={
        look === "allow" || look === "locked"
          ? true
          : look === "partial"
            ? "mixed"
            : false
      }
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "relative grid size-4 shrink-0 place-items-center rounded-[5px] border transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-not-allowed",
        look === "none" && "border-input bg-background",
        (look === "allow" || look === "partial") &&
          "border-primary bg-primary text-primary-foreground",
        look === "locked" &&
          "border-primary/50 bg-primary/55 text-primary-foreground",
        (look === "deny" || look === "denyPartial") &&
          "border-rose-500 bg-rose-500 text-white",
      )}
    >
      {(look === "allow" || look === "locked") && (
        <Check className="size-3" strokeWidth={3} />
      )}
      {(look === "partial" || look === "denyPartial") && (
        <Minus className="size-3" strokeWidth={3} />
      )}
      {look === "deny" && <X className="size-3" strokeWidth={3} />}
      {!isAction && look === "partial" && tally.deny > 0 && (
        <span className="absolute -top-1 -right-1 size-2 rounded-full border border-background bg-rose-500" />
      )}
    </button>
  );
}

function RoutesPopover({ code, count }: { code: string; count: number }) {
  const [open, setOpen] = useState(false);
  const query = usePermissionRoutesQuery(open ? code : null);
  if (count === 0) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="inline-flex h-[18px] items-center rounded bg-muted px-1.5 text-[10px] text-muted-foreground">
            业务
          </span>
        </TooltipTrigger>
        <TooltipContent>在业务逻辑中判定，不对应单独的接口</TooltipContent>
      </Tooltip>
    );
  }
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          onClick={(event) => event.stopPropagation()}
          className="inline-flex h-[18px] items-center rounded bg-muted px-1.5 text-[10px] text-muted-foreground tabular-nums transition-colors hover:bg-accent hover:text-foreground"
        >
          {count} 接口
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-[26rem] p-0"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="border-b px-3 py-2">
          <div className="text-sm font-medium">{query.data?.name ?? code}</div>
          <CodeText>{code}</CodeText>
          {query.data?.legacy && (
            <div className="mt-0.5 text-[11px] text-muted-foreground">
              隶属旧权限 {query.data.legacy}
            </div>
          )}
        </div>
        <div className="max-h-72 overflow-y-auto py-1">
          {query.isLoading && (
            <div className="px-3 py-4 text-xs text-muted-foreground">
              加载中
            </div>
          )}
          {query.data?.routes.map((route, i) => (
            <div
              key={`${route.pattern}-${i}`}
              className="flex items-start gap-2 px-3 py-1.5"
            >
              <span className="flex shrink-0 flex-wrap gap-0.5">
                {(route.methods && route.methods.length > 0
                  ? route.methods
                  : ["ANY"]
                )
                  .slice(0, 3)
                  .map((method) => (
                    <span
                      key={method}
                      className={cn(
                        "rounded px-1 font-mono text-[10px] leading-4 font-semibold",
                        METHOD_TONE[method] ?? "bg-muted text-muted-foreground",
                      )}
                    >
                      {method === "OPTIONS" || method === "HEAD"
                        ? method.slice(0, 3)
                        : method}
                    </span>
                  ))}
              </span>
              <code className="min-w-0 font-mono text-[11px] leading-4 break-all">
                {route.pattern}
              </code>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function PaintButton({
  active,
  onClick,
  tone,
  children,
}: {
  active: boolean;
  onClick: () => void;
  tone: "allow" | "deny";
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center gap-1 rounded-md px-2.5 text-xs font-medium transition-colors",
        active
          ? tone === "allow"
            ? "bg-primary text-primary-foreground shadow-sm"
            : "bg-rose-500 text-white shadow-sm"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function FilterChip({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-8 items-center gap-1 rounded-lg border px-2.5 text-xs transition-colors",
        active
          ? "border-primary/40 bg-primary/10 text-primary"
          : "text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function IconAction({
  label,
  onClick,
  icon: Icon,
}: {
  label: string;
  onClick: () => void;
  icon: typeof ChevronsUpDown;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-8"
          aria-label={label}
          onClick={onClick}
        >
          <Icon className="size-4" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
