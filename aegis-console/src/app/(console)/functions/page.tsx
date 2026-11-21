"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Code2, Database, ExternalLink, KeyRound, LayoutDashboard } from "lucide-react";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { FunctionAppOverview } from "@/components/functions/function-app-overview";
import { FunctionManager } from "@/components/functions/function-manager";
import { useFunctionCatalogQuery } from "@/lib/function-hooks";
import { FunctionKeysPanel } from "@/components/functions/function-keys-panel";
import { FunctionKvPanel } from "@/components/functions/function-kv-panel";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/data-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

/**
 * 远程函数工作台。
 *
 * 整页占满视口高度（`workbench-viewport`），滚动条一律长在各面板自己的内容区上。
 * 页面级的滚动在这里是有害的：编辑器一旦被卷出屏幕，写脚本这件事就变成了
 * 「滚上去改一行、滚下来看结果」—— 而那正是这个界面此前最大的问题。
 *
 * 因此页头压成一行：标题、页签、应用选择器、文档入口全部并排，
 * 省下来的纵向空间归编辑器。
 *
 * 落地页是应用全景（?tab=overview）：函数多了以后，「整体跑得怎么样、哪里在出错」
 * 比「先打开第一个函数的脚本」更常被问到。全景里点函数、点错误会带着
 * `fn` / `panel` / `inv` 跳到工作台，工作台按这几个参数挂载一次初始定位。
 */
function FunctionsPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "overview";
  const focusName = searchParams.get("fn") || undefined;
  const focusPanel = searchParams.get("panel") || undefined;
  const focusInvocation = Number(searchParams.get("inv")) || undefined;
  const focusCreate = searchParams.get("create") === "1";

  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data || [], [appsQuery.data]);
  // 只记录用户的显式选择，当前应用由它与列表**派生**出来。
  // 用 effect 把首项同步进 state 会触发级联渲染，也过不了
  // react-hooks/set-state-in-effect —— 与配置面板草稿同一条约束。
  const [selectedAppKey, setSelectedAppKey] = useState<string | null>(null);

  const selectedApp = useMemo(
    () => apps.find((app) => app.appKey === selectedAppKey) || apps[0] || null,
    [apps, selectedAppKey]
  );

  const catalogQuery = useFunctionCatalogQuery(selectedApp?.appKey);

  function openFunction(name: string, panel?: string, invocationId?: number) {
    const params = new URLSearchParams({ tab: "functions", fn: name });
    if (panel) params.set("panel", panel);
    if (invocationId) params.set("inv", String(invocationId));
    router.replace(`/functions?${params.toString()}`, { scroll: false });
  }

  if (appsQuery.isLoading) return <LoadingState title="加载应用" />;

  if (!apps.length) {
    return (
      <div className="rounded-xl border py-16 text-center text-sm text-muted-foreground">
        暂无应用
      </div>
    );
  }

  return (
    <Tabs
      value={tab}
      onValueChange={(value) => router.replace(`/functions?tab=${value}`, { scroll: false })}
      className="workbench-viewport flex flex-col gap-3"
    >
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        <TabsList className="h-8">
          <TabsTrigger value="overview" className="text-xs">
            <LayoutDashboard className="size-3.5" />
            全景
          </TabsTrigger>
          <TabsTrigger value="functions" className="text-xs">
            <Code2 className="size-3.5" />
            函数
          </TabsTrigger>
          <TabsTrigger value="kv" className="text-xs">
            <Database className="size-3.5" />
            键值存储
          </TabsTrigger>
          <TabsTrigger value="keys" className="text-xs">
            <KeyRound className="size-3.5" />
            调用密钥
          </TabsTrigger>
        </TabsList>

        <div className="ml-auto flex items-center gap-2">
          <Select value={selectedApp?.appKey ?? ""} onValueChange={setSelectedAppKey}>
            <SelectTrigger className="h-8 w-48 text-xs">
              <SelectValue placeholder="选择应用" />
            </SelectTrigger>
            <SelectContent>
              {apps.map((app) => (
                <SelectItem key={app.id} value={app.appKey}>
                  {app.name} ({app.id})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button asChild size="sm" variant="ghost" className="h-8 gap-1 text-xs">
            <Link href="/developers#functions" target="_blank">
              接入文档 <ExternalLink className="size-3" />
            </Link>
          </Button>
        </div>
      </div>

      <TabsContent value="overview" className="min-h-0 flex-1 overflow-y-auto">
        {selectedApp ? (
          <FunctionAppOverview
            appKey={selectedApp.appKey}
            catalog={catalogQuery.data}
            onOpen={openFunction}
            onCreate={() => router.replace("/functions?tab=functions&create=1", { scroll: false })}
          />
        ) : null}
      </TabsContent>
      <TabsContent value="functions" className="min-h-0 flex-1">
        {/* key 带上定位参数：从全景再点一次别的函数时，工作台按新参数重新定位 */}
        <FunctionManager
          key={`${selectedApp?.appKey}:${focusName ?? ""}:${focusPanel ?? ""}:${focusInvocation ?? ""}:${focusCreate}`}
          appKey={selectedApp?.appKey}
          initialName={focusName}
          initialPanel={focusPanel}
          initialInvocationId={focusInvocation}
          initialCreate={focusCreate}
        />
      </TabsContent>
      {/* 这两张是表格，内容长过一屏时在自己的容器里滚 */}
      <TabsContent value="kv" className="min-h-0 flex-1 overflow-y-auto">
        <FunctionKvPanel appKey={selectedApp?.appKey} />
      </TabsContent>
      <TabsContent value="keys" className="min-h-0 flex-1 overflow-y-auto">
        <FunctionKeysPanel appKey={selectedApp?.appKey} />
      </TabsContent>
    </Tabs>
  );
}

export default function FunctionsPage() {
  return (
    <Suspense>
      <FunctionsPageInner />
    </Suspense>
  );
}
