"use client";

import { useMemo, useState } from "react";
import { Activity, Code2, FileCode2, Loader2, Plus, ScrollText, Settings2 } from "lucide-react";
import type { AppFunction } from "@/lib/api/app-functions";
import { useAppFunctionsQuery, useFunctionCatalogQuery } from "@/lib/function-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { CreateFunctionDialog } from "./function-create-dialog";
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
  active: "已激活",
  draft: "草稿",
  disabled: "已停用"
};

/**
 * 远程函数工作台。
 *
 * 四个面板对应四件不同的事，刻意分开：
 *   概览 —— 它现在跑得怎么样（以及为什么调不通）
 *   脚本 —— 写、试跑、发布、回滚
 *   调用 —— 发生过什么（含真实调用入口）
 *   设置 —— 能力、闸门、函数配置
 *
 * 挤在一屏里的旧形状最大的问题不是拥挤，是**没有试跑**：
 * 唯一的验证方式是把半成品激活到线上。
 */
export function FunctionManager({ appKey }: { appKey?: string | null }) {
  const [selectedName, setSelectedName] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [panel, setPanel] = useState("overview");
  // 建函数时选的模板：新函数还没有任何版本，编辑器要用它当起始正文
  const [pendingTemplate, setPendingTemplate] = useState<Record<string, string>>({});

  const catalogQuery = useFunctionCatalogQuery(appKey);
  const functionsQuery = useAppFunctionsQuery(appKey);
  const functions = useMemo(() => functionsQuery.data || [], [functionsQuery.data]);

  // selectedName 只记录用户的显式选择；应用切换或函数被删后自动回落到第一个，
  // 因此这里是纯派生，不需要用 effect 去回写 state。
  const selected = functions.find((item) => item.name === selectedName) || functions[0] || null;

  if (!appKey) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          请先选择应用。
        </CardContent>
      </Card>
    );
  }

  if (functionsQuery.isLoading) {
    return (
      <div className="flex min-h-48 items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
      <Card className="h-fit">
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle>函数</CardTitle>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            创建
          </Button>
        </CardHeader>
        <CardContent className="space-y-2">
          {functions.map((item) => (
            <button
              type="button"
              key={item.id}
              onClick={() => setSelectedName(item.name)}
              className={cn(
                "w-full rounded-lg border p-3 text-left transition-colors",
                selected?.name === item.name ? "border-primary bg-muted" : "hover:bg-muted/50"
              )}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="min-w-0 truncate font-mono text-sm font-medium">{item.name}</span>
                <Badge variant="outline" size="sm">
                  {item.runtime}
                </Badge>
              </div>
              <div className="mt-2 flex items-center justify-between gap-2 text-xs">
                <Badge variant={STATUS_VARIANT[item.status]} size="sm">
                  {STATUS_LABEL[item.status]}
                </Badge>
                <span className="truncate font-mono text-muted-foreground">
                  {item.activeVersion || "无活动版本"}
                </span>
              </div>
            </button>
          ))}
          {!functions.length ? (
            <p className="py-10 text-center text-sm text-muted-foreground">
              暂无函数，点击「创建」开始。
            </p>
          ) : null}
        </CardContent>
      </Card>

      {selected ? (
        <div className="min-w-0 space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="font-mono text-lg font-medium">{selected.name}</h2>
            {selected.description ? (
              <span className="text-sm text-muted-foreground">{selected.description}</span>
            ) : null}
          </div>

          <Tabs value={panel} onValueChange={setPanel} className="space-y-4">
            <TabsList className="w-fit">
              <TabsTrigger value="overview">
                <Activity className="size-4" />
                概览
              </TabsTrigger>
              <TabsTrigger value="script">
                <FileCode2 className="size-4" />
                {selected.runtime === "script" ? "脚本" : "版本"}
              </TabsTrigger>
              <TabsTrigger value="invocations">
                <ScrollText className="size-4" />
                调用
              </TabsTrigger>
              <TabsTrigger value="settings">
                <Settings2 className="size-4" />
                设置
              </TabsTrigger>
            </TabsList>

            {/* key 绑到函数名：切换函数时整块重挂载，
                草稿、筛选、试跑结果一并重置，不需要任何同步 effect */}
            <TabsContent value="overview">
              <FunctionOverviewPanel key={selected.name} appKey={appKey} selected={selected} />
            </TabsContent>
            <TabsContent value="script">
              <FunctionEditorPanel
                key={selected.name}
                appKey={appKey}
                selected={selected}
                catalog={catalogQuery.data}
                initialTemplate={pendingTemplate[selected.name]}
              />
            </TabsContent>
            <TabsContent value="invocations">
              <FunctionInvocationsPanel key={selected.name} appKey={appKey} selected={selected} />
            </TabsContent>
            <TabsContent value="settings">
              <FunctionSettingsPanel
                key={selected.name}
                appKey={appKey}
                selected={selected}
                catalog={catalogQuery.data}
                onDeleted={() => {
                  setSelectedName("");
                  setPanel("overview");
                }}
              />
            </TabsContent>
          </Tabs>
        </div>
      ) : (
        <Card>
          <CardContent className="flex min-h-72 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
            <Code2 className="size-6" />
            选择或创建一个函数。
          </CardContent>
        </Card>
      )}

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
    </div>
  );
}
