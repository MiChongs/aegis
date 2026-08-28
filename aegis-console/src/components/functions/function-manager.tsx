"use client";

import { useMemo, useState } from "react";
import {
  Activity,
  AlertTriangle,
  BookOpenText,
  Code2,
  FileCode2,
  Loader2,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  ScrollText,
  Search,
  Settings2
} from "lucide-react";
import type { AppFunction } from "@/lib/api/app-functions";
import { useAppFunctionsQuery, useFunctionCatalogQuery } from "@/lib/function-hooks";
import { useFunctionWorkbenchStore } from "@/lib/function-workbench-store";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  ResizableGroup,
  ResizableHandle,
  ResizablePanel,
  usePanelLayout,
  usePanelRef
} from "@/components/ui/resizable";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { CreateFunctionDialog } from "./function-create-dialog";
import { FunctionDocsPanel } from "./function-docs-panel";
import { FunctionEditorPanel } from "./function-editor-panel";
import { FunctionInvocationsPanel } from "./function-invocations-panel";
import { FunctionOverviewPanel } from "./function-overview-panel";
import { FunctionSettingsPanel } from "./function-settings-panel";

const STATUS_VARIANT: Record<AppFunction["status"], "success" | "warning" | "outline"> = {
  active: "success",
  draft: "warning",
  disabled: "outline"
};

const STATUS_LABEL: Record<AppFunction["status"], string> = {
  active: "已启用",
  draft: "草稿",
  disabled: "已停用"
};

const STATUS_DOT: Record<AppFunction["status"], string> = {
  active: "bg-emerald-500",
  draft: "bg-amber-500",
  disabled: "bg-muted-foreground/50"
};

/**
 * 远程函数工作台。
 *
 * 形状是「左侧函数列表 + 右侧当前函数」，两栏之间可拖拽，整体占满一屏。
 *
 * 四个面板仍然对应四件不同的事，刻意不合并：
 *   概览 —— 它现在跑得怎么样（以及为什么调不通）
 *   脚本 —— 写、试跑、发布、回滚
 *   调用 —— 发生过什么（含真实调用入口）
 *   设置 —— 能力、闸门、函数配置
 *
 * 默认落在「脚本」上：进这个页面十次有九次是来改逻辑的，
 * 而「为什么调不通」这一条结论已经压缩成头部那枚警示徽标，不必先看一页图表。
 */
export function FunctionManager({ appKey }: { appKey?: string | null }) {
  const [selectedName, setSelectedName] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [panel, setPanel] = useState("script");
  const [keyword, setKeyword] = useState("");
  const [listCollapsed, setListCollapsed] = useState(false);
  // 建函数时选的模板：新函数还没有任何版本，编辑器要用它当起始正文
  const [pendingTemplate, setPendingTemplate] = useState<Record<string, string>>({});

  const catalogQuery = useFunctionCatalogQuery(appKey);
  const functionsQuery = useAppFunctionsQuery(appKey);
  const functions = useMemo(() => functionsQuery.data || [], [functionsQuery.data]);
  const drafts = useFunctionWorkbenchStore((state) => state.drafts);

  // selectedName 只记录用户的显式选择；应用切换或函数被删后自动回落到第一个，
  // 因此这里是纯派生，不需要用 effect 去回写 state。
  const selected = functions.find((item) => item.name === selectedName) || functions[0] || null;

  const listRef = usePanelRef();
  const layout = usePanelLayout({ id: "aegis-functions-workbench", panelIds: ["list", "detail"] });

  const filtered = useMemo(() => {
    const query = keyword.trim().toLowerCase();
    if (!query) return functions;
    return functions.filter(
      (item) =>
        item.name.toLowerCase().includes(query) ||
        item.description.toLowerCase().includes(query)
    );
  }, [functions, keyword]);

  if (!appKey) {
    return <EmptyShell>请先选择应用。</EmptyShell>;
  }

  if (functionsQuery.isLoading) {
    return (
      <EmptyShell>
        <Loader2 className="size-5 animate-spin" />
      </EmptyShell>
    );
  }

  function toggleList() {
    const instance = listRef.current;
    if (!instance) return;
    if (instance.isCollapsed()) instance.expand();
    else instance.collapse();
  }

  return (
    <>
      <div className="h-full min-h-0 overflow-hidden rounded-xl border bg-card">
        <ResizableGroup
          orientation="horizontal"
          id="functions-workbench"
          defaultLayout={layout.defaultLayout}
          onLayoutChanged={layout.onLayoutChanged}
        >
          <ResizablePanel
            id="list"
            className="flex flex-col"
            defaultSize={252}
            minSize={196}
            maxSize={420}
            groupResizeBehavior="preserve-pixel-size"
            collapsible
            collapsedSize={0}
            panelRef={listRef}
            onResize={(size) => setListCollapsed(size.inPixels < 1)}
          >
            <div className="flex items-center gap-1.5 border-b p-2">
              <div className="relative min-w-0 flex-1">
                <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  className="h-8 pl-7 text-xs"
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder={`搜索 ${functions.length} 个函数`}
                />
              </div>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button size="icon-sm" onClick={() => setCreateOpen(true)} aria-label="创建函数">
                    <Plus className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>创建函数</TooltipContent>
              </Tooltip>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
              {filtered.map((item) => {
                const dirty = Boolean(drafts[`${appKey}:${item.name}`]);
                const active = selected?.name === item.name;
                return (
                  <button
                    type="button"
                    key={item.id}
                    onClick={() => setSelectedName(item.name)}
                    className={cn(
                      "w-full rounded-lg px-2 py-1.5 text-left transition-colors",
                      active ? "bg-muted" : "hover:bg-muted/50"
                    )}
                  >
                    <span className="flex items-center gap-1.5">
                      <span
                        className={cn("size-1.5 shrink-0 rounded-full", STATUS_DOT[item.status])}
                        aria-hidden
                      />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">{item.name}</span>
                      {/* 有本地草稿的函数标一个点：上次改到哪儿了，
                          在列表上就该看得见，而不是逐个点进去找 */}
                      {dirty ? (
                        <span className="size-1.5 shrink-0 rounded-full bg-amber-500" title="有未发布的改动" />
                      ) : null}
                    </span>
                    <span className="mt-0.5 flex items-center gap-1.5 pl-3 text-[10px] text-muted-foreground">
                      <span className="truncate font-mono">
                        {item.activeVersion || "无激活版本"}
                      </span>
                      {item.runtime !== "script" ? (
                        <span className="shrink-0 rounded border px-1">{item.runtime}</span>
                      ) : null}
                    </span>
                  </button>
                );
              })}
              {!filtered.length ? (
                <p className="px-2 py-10 text-center text-xs text-muted-foreground">
                  {functions.length ? "无匹配的函数" : "暂无函数"}
                </p>
              ) : null}
            </div>
          </ResizablePanel>

          <ResizableHandle />

          <ResizablePanel id="detail" className="flex flex-col" minSize="40">
            {selected ? (
              <Tabs
                value={panel}
                onValueChange={setPanel}
                className="flex min-h-0 flex-1 flex-col gap-0"
              >
                <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-2 py-1.5">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        onClick={toggleList}
                        aria-label={listCollapsed ? "展开函数列表" : "收起函数列表"}
                      >
                        {listCollapsed ? (
                          <PanelLeftOpen className="size-4" />
                        ) : (
                          <PanelLeftClose className="size-4" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{listCollapsed ? "展开列表" : "收起列表"}</TooltipContent>
                  </Tooltip>

                  <span className="truncate font-mono text-sm font-medium">{selected.name}</span>
                  <Badge variant={STATUS_VARIANT[selected.status]} size="sm">
                    {STATUS_LABEL[selected.status]}
                  </Badge>
                  {/* 「不可被调用」是调用方收到一串 40990 的唯一原因，
                      它必须出现在任何面板上，而不是只在概览页 */}
                  <NotCallableBadge selected={selected} onFix={() => setPanel("overview")} />
                  {selected.description ? (
                    <span className="hidden min-w-0 truncate text-xs text-muted-foreground xl:block">
                      {selected.description}
                    </span>
                  ) : null}

                  <TabsList className="ml-auto h-8">
                    <TabsTrigger value="script" className="text-xs">
                      <FileCode2 className="size-3.5" />
                      {selected.runtime === "script" ? "脚本" : "版本"}
                    </TabsTrigger>
                    <TabsTrigger value="overview" className="text-xs">
                      <Activity className="size-3.5" />
                      概览
                    </TabsTrigger>
                    <TabsTrigger value="invocations" className="text-xs">
                      <ScrollText className="size-3.5" />
                      调用
                    </TabsTrigger>
                    <TabsTrigger value="docs" className="text-xs">
                      <BookOpenText className="size-3.5" />
                      接入
                    </TabsTrigger>
                    <TabsTrigger value="settings" className="text-xs">
                      <Settings2 className="size-3.5" />
                      设置
                    </TabsTrigger>
                  </TabsList>
                </div>

                {/* key 绑到函数名：切换函数时整块重挂载，
                    草稿、筛选、试跑结果一并重置，不需要任何同步 effect。
                    脚本面板自己排布满屏，因此不给它加内边距与滚动；
                    另外三个是文档式内容，在自己的容器里滚。 */}
                <TabsContent value="script" className="min-h-0 flex-1 overflow-hidden">
                  <FunctionEditorPanel
                    key={selected.name}
                    appKey={appKey}
                    selected={selected}
                    catalog={catalogQuery.data}
                    initialTemplate={pendingTemplate[selected.name]}
                  />
                </TabsContent>
                <TabsContent value="overview" className="min-h-0 flex-1 overflow-y-auto p-3">
                  <FunctionOverviewPanel key={selected.name} appKey={appKey} selected={selected} />
                </TabsContent>
                <TabsContent value="invocations" className="min-h-0 flex-1 overflow-y-auto p-3">
                  <FunctionInvocationsPanel key={selected.name} appKey={appKey} selected={selected} />
                </TabsContent>
                <TabsContent value="docs" className="min-h-0 flex-1 overflow-y-auto p-3">
                  <FunctionDocsPanel key={selected.name} appKey={appKey} selected={selected} />
                </TabsContent>
                <TabsContent value="settings" className="min-h-0 flex-1 overflow-y-auto p-3">
                  <FunctionSettingsPanel
                    key={selected.name}
                    appKey={appKey}
                    selected={selected}
                    catalog={catalogQuery.data}
                    onDeleted={() => {
                      setSelectedName("");
                      setPanel("script");
                    }}
                  />
                </TabsContent>
              </Tabs>
            ) : (
              <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
                <Code2 className="size-6" />
                <span>暂无函数</span>
                <Button size="sm" onClick={() => setCreateOpen(true)}>
                  <Plus className="size-4" />
                  创建函数
                </Button>
              </div>
            )}
          </ResizablePanel>
        </ResizableGroup>
      </div>

      <CreateFunctionDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        appKey={appKey}
        catalog={catalogQuery.data}
        onCreated={(name, template) => {
          setSelectedName(name);
          if (template) setPendingTemplate((current) => ({ ...current, [name]: template }));
          // 建完直接跳到脚本页：新函数唯一有意义的下一步就是写逻辑
          setPanel("script");
        }}
      />
    </>
  );
}

function EmptyShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0 items-center justify-center rounded-xl border bg-card text-sm text-muted-foreground">
      {children}
    </div>
  );
}

/** 只在真的调不通时出现：默认状态下不该有常驻警告。 */
function NotCallableBadge({
  selected,
  onFix
}: {
  selected: AppFunction;
  onFix: () => void;
}) {
  const callable = selected.status === "active" && Boolean(selected.activeVersion);
  if (callable) return null;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" onClick={onFix}>
          <Badge variant="warning" size="sm" className="gap-1">
            <AlertTriangle className="size-3" />
            不可被调用
          </Badge>
        </button>
      </TooltipTrigger>
      <TooltipContent>
        {!selected.activeVersion
          ? "尚未激活版本，发布并激活后方可调用"
          : "函数未启用，请在「设置」中将状态改为「已启用」"}
      </TooltipContent>
    </Tooltip>
  );
}
