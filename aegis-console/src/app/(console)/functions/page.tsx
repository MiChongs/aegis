"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Code2, Database, ExternalLink, KeyRound } from "lucide-react";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { FunctionManager } from "@/components/functions/function-manager";
import { FunctionKeysPanel } from "@/components/functions/function-keys-panel";
import { FunctionKvPanel } from "@/components/functions/function-kv-panel";
import { Button } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

function FunctionsPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "functions";

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

  if (appsQuery.isLoading) return <LoadingState title="加载应用" />;

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="开发者"
        title="远程函数"
        action={
          <div className="flex items-center gap-2">
            {apps.length > 0 ? (
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
            ) : null}
            <Button asChild size="sm" variant="ghost" className="h-8 gap-1 text-xs">
              <Link href="/developers#functions" target="_blank">
                接入文档 <ExternalLink className="size-3" />
              </Link>
            </Button>
          </div>
        }
      />

      {!apps.length ? (
        <div className="rounded-xl border py-16 text-center text-sm text-muted-foreground">
          请先在「应用」中创建应用。
        </div>
      ) : (
        <Tabs
          value={tab}
          onValueChange={(value) => router.replace(`/functions?tab=${value}`, { scroll: false })}
          className="space-y-5"
        >
          <TabsList className="w-fit">
            <TabsTrigger value="functions">
              <Code2 className="size-4" />
              函数
            </TabsTrigger>
            <TabsTrigger value="kv">
              <Database className="size-4" />
              键值存储
            </TabsTrigger>
            <TabsTrigger value="keys">
              <KeyRound className="size-4" />
              调用密钥
            </TabsTrigger>
          </TabsList>

          <TabsContent value="functions">
            <FunctionManager appKey={selectedApp?.appKey} />
          </TabsContent>
          <TabsContent value="kv">
            <FunctionKvPanel appKey={selectedApp?.appKey} />
          </TabsContent>
          <TabsContent value="keys">
            <FunctionKeysPanel appKey={selectedApp?.appKey} />
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}

export default function FunctionsPage() {
  return (
    <Suspense>
      <FunctionsPageInner />
    </Suspense>
  );
}
