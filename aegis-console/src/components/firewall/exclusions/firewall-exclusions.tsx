"use client";

import { useEffect, useMemo, useState } from "react";
import { FlaskConical, ListChecks, MoreHorizontal, Pencil, Plus, ShieldAlert, ShieldCheck, Trash2 } from "lucide-react";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { TabStack } from "@/components/traffic/traffic-kit";
import type { FirewallExclusion, FirewallExclusionInput, FirewallRuleInfo } from "@/lib/api/firewall";
import {
  useDeleteFirewallExclusionMutation,
  useFirewallCatalogQuery,
  useFirewallExclusionsQuery,
  useToggleFirewallExclusionMutation
} from "@/lib/firewall-hooks";
import { useFirewallIntents } from "@/lib/firewall-store";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { ExclusionEditorDialog, type ExclusionEditorTarget } from "./exclusion-editor-dialog";
import { PATH_MATCH_META, RuleIdChip, TAG_LABEL, pathDisplay, sandboxSampleFor, useExclusionHandoff } from "./exclusion-format";

type FirewallTabKey = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/**
 * WAF 例外页签（docs/firewall.md §3「管理员例外 1190000+」）。
 *
 * 例外分两种，列表上刻意分得很开：
 * - 只移除检查目标（targets 非空）：规则照常检查其他部分，只是不看某个字段——影响面小，推荐；
 * - 整条移除（targets 为空）：匹配路径上这条规则完全不执行——等于在这段路径上关掉一类检测。
 *
 * 从事件「为这个误报建例外」或测试台「回到例外编辑」过来时，store 里有草稿：挂载时先读出来
 * 打开编辑器，effect 里再清掉（读与清分开，StrictMode 下初始化函数被调两次也不会把草稿吃掉）。
 */
export function FirewallExclusionsTab({ onNavigate }: { onNavigate: (tab: FirewallTabKey) => void }) {
  const exclusionsQuery = useFirewallExclusionsQuery();
  const catalog = useFirewallCatalogQuery();
  const toggle = useToggleFirewallExclusionMutation();
  const remove = useDeleteFirewallExclusionMutation();
  const canWrite = usePermissionChecker()("system:firewall:policy");
  const setSandboxDraft = useFirewallIntents((state) => state.setSandboxDraft);

  const [initialTarget] = useState<ExclusionEditorTarget | null>(() => {
    const draft = useFirewallIntents.getState().exclusionDraft;
    if (!draft || !canWrite) return null;
    return { key: 1, id: useExclusionHandoff.getState().editId ?? undefined, seed: draft };
  });
  useEffect(() => {
    useFirewallIntents.getState().takeExclusionDraft();
    useExclusionHandoff.getState().takeEditId();
  }, [initialTarget]);

  const [editor, setEditor] = useState<ExclusionEditorTarget | null>(initialTarget);
  const [editorOpen, setEditorOpen] = useState(Boolean(initialTarget));
  const [pendingDelete, setPendingDelete] = useState<FirewallExclusion | null>(null);

  const items = useMemo(
    () => [...(exclusionsQuery.data ?? [])].sort((a, b) => a.position - b.position || a.id - b.id),
    [exclusionsQuery.data]
  );
  const ruleInfo = useMemo(() => new Map((catalog.data?.rules ?? []).map((rule) => [rule.id, rule])), [catalog.data]);
  const scopedCount = items.filter((item) => item.targets.length > 0).length;

  const openEditor = (item?: FirewallExclusion) => {
    setEditor({
      key: Date.now(),
      id: item?.id,
      seed: item
        ? {
            name: item.name,
            note: item.note,
            enabled: item.enabled,
            pathMatch: item.pathMatch,
            path: item.path,
            methods: item.methods,
            ruleIds: item.ruleIds,
            tags: item.tags,
            targets: item.targets
          }
        : { name: "", pathMatch: "prefix", path: "", methods: [], ruleIds: [], tags: [], targets: [] }
    });
    setEditorOpen(true);
  };

  const toSandbox = (input: FirewallExclusionInput, editId?: number) => {
    setSandboxDraft({ ...sandboxSampleFor(input), draftExclusion: input });
    useExclusionHandoff.getState().setEditId(editId ?? null);
    onNavigate("sandbox");
  };

  return (
    <TabStack>
      <div className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-3 rounded-2xl border border-border bg-card px-4 py-3 sm:px-5">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <span className="text-2xl leading-7 font-semibold tracking-tight tabular-nums">{items.length}</span>
          <span className="text-xs text-muted-foreground">条例外</span>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <ShieldCheck className="size-3.5 text-emerald-600 dark:text-emerald-400" />
            只移除检查目标
            <span className="font-medium text-foreground tabular-nums">{scopedCount}</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <ShieldAlert className="size-3.5 text-amber-600 dark:text-amber-400" />
            整条移除
            <span className="font-medium text-foreground tabular-nums">{items.length - scopedCount}</span>
          </span>
        </div>
        {canWrite ? (
          <Button size="sm" className="ml-auto" onClick={() => openEditor()}>
            <Plus />
            新建例外
          </Button>
        ) : null}
      </div>

      {exclusionsQuery.isLoading ? (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 3 }).map((_, index) => (
            <Skeleton key={index} className="h-36 rounded-2xl" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <section className="flex min-w-0 items-start gap-3 rounded-2xl border border-border bg-card px-4 py-5 sm:px-6">
          <TonalIcon icon={ListChecks} tone="emerald" size="lg" />
          <div className="min-w-0">
            <h3 className="text-sm font-semibold">暂无 WAF 例外</h3>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              某个接口被误拦时，在事件详情里点「为这个误报建例外」即可预填。优先只移除误报的字段，而不是整条规则。
            </p>
          </div>
        </section>
      ) : (
        <ul className="flex flex-col gap-3">
          {items.map((item) => (
            <ExclusionCard
              key={item.id}
              item={item}
              ruleInfo={ruleInfo}
              canWrite={canWrite}
              onToggle={(enabled) => toggle.mutate({ id: item.id, enabled })}
              onEdit={() => openEditor(item)}
              onSandbox={() =>
                toSandbox(
                  {
                    name: item.name,
                    note: item.note,
                    enabled: true,
                    pathMatch: item.pathMatch,
                    path: item.path,
                    methods: item.methods,
                    ruleIds: item.ruleIds,
                    tags: item.tags,
                    targets: item.targets
                  },
                  item.id
                )
              }
              onDelete={() => setPendingDelete(item)}
            />
          ))}
        </ul>
      )}

      <ExclusionEditorDialog target={editor} open={editorOpen} onOpenChange={setEditorOpen} onSandbox={toSandbox} />

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除例外「{pendingDelete?.name}」</AlertDialogTitle>
            <AlertDialogDescription>删除后被它放过的请求会重新接受这些规则检查，所有副本立即生效。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (pendingDelete) remove.mutate(pendingDelete.id);
                setPendingDelete(null);
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

function ExclusionCard({
  item,
  ruleInfo,
  canWrite,
  onToggle,
  onEdit,
  onSandbox,
  onDelete
}: {
  item: FirewallExclusion;
  ruleInfo: Map<number, FirewallRuleInfo>;
  canWrite: boolean;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onSandbox: () => void;
  onDelete: () => void;
}) {
  const scoped = item.targets.length > 0;
  return (
    <li
      className={cn(
        "relative flex min-w-0 flex-col gap-3 overflow-hidden rounded-2xl border border-border bg-card py-3 pr-3 pl-4 sm:pr-4 sm:pl-5",
        !item.enabled && "bg-muted/30"
      )}
    >
      <span aria-hidden className={cn("absolute inset-y-0 left-0 w-1", scoped ? "bg-emerald-500" : "bg-amber-500")} />
      <div className="flex min-w-0 items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {canWrite ? (
              <button type="button" onClick={onEdit} className="min-w-0 truncate text-left text-sm font-semibold hover:underline">
                {item.name}
              </button>
            ) : (
              <span className="min-w-0 truncate text-sm font-semibold">{item.name}</span>
            )}
            {!item.enabled ? (
              <Badge variant="outline" size="sm" className="text-muted-foreground">
                停用
              </Badge>
            ) : null}
          </div>
          {item.note ? <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{item.note}</p> : null}
        </div>
        <Switch checked={item.enabled} onCheckedChange={onToggle} disabled={!canWrite} aria-label={item.enabled ? "停用" : "启用"} className="mt-0.5" />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon-sm" variant="ghost" aria-label="更多" className="-mt-1">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onSandbox}>
              <FlaskConical />
              在测试台验证
            </DropdownMenuItem>
            {canWrite ? (
              <>
                <DropdownMenuItem onSelect={onEdit}>
                  <Pencil />
                  编辑
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                  <Trash2 />
                  删除
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <div className={cn("flex min-w-0 flex-col gap-2.5", !item.enabled && "opacity-70")}>
        {/* 请求 */}
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="w-10 shrink-0 text-[11px] text-muted-foreground">请求</span>
          {item.methods.length ? (
            item.methods.map((method) => (
              <span key={method} className="rounded-md bg-muted px-1.5 font-mono text-[11px] leading-5">
                {method}
              </span>
            ))
          ) : (
            <span className="rounded-md bg-muted px-1.5 text-[11px] leading-5 text-muted-foreground">任意方法</span>
          )}
          <span className="rounded-md border border-border px-1.5 text-[11px] leading-5 text-muted-foreground" title={PATH_MATCH_META[item.pathMatch]?.hint}>
            {PATH_MATCH_META[item.pathMatch]?.label ?? item.pathMatch}
          </span>
          <code className="min-w-0 font-mono text-xs break-all">{pathDisplay(item.pathMatch, item.path)}</code>
        </div>

        {/* 规则 */}
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="w-10 shrink-0 text-[11px] text-muted-foreground">规则</span>
          {item.ruleIds.map((id) => (
            <RuleIdChip key={id} id={id} info={ruleInfo.get(id)} />
          ))}
          {item.tags.map((tag) => (
            <span key={tag} className="inline-flex items-center gap-1 rounded-md border border-border px-1.5 text-[11px] leading-5">
              <span className="font-mono">{tag}</span>
              {TAG_LABEL[tag] ? <span className="text-muted-foreground">{TAG_LABEL[tag]}</span> : null}
            </span>
          ))}
        </div>

        {/* 范围 */}
        {scoped ? (
          <div className="flex min-w-0 flex-wrap items-center gap-1.5 rounded-lg bg-emerald-500/8 px-2.5 py-1.5">
            <ShieldCheck className="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
            <span className="text-[11px] font-medium text-emerald-700 dark:text-emerald-300">只移除检查目标</span>
            {item.targets.map((target) => (
              <code key={target} className="rounded-md bg-background/80 px-1.5 font-mono text-[11px] leading-5 break-all">
                {target}
              </code>
            ))}
          </div>
        ) : (
          <div className="flex min-w-0 items-center gap-1.5 rounded-lg bg-amber-500/10 px-2.5 py-1.5">
            <ShieldAlert className="size-3.5 shrink-0 text-amber-600 dark:text-amber-400" />
            <span className="text-[11px] font-medium text-amber-800 dark:text-amber-300">整条移除</span>
            <span className="min-w-0 truncate text-[11px] text-amber-800/80 dark:text-amber-300/80">匹配的请求完全不执行这些规则</span>
          </div>
        )}
      </div>
    </li>
  );
}
