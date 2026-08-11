"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Code2, ExternalLink, KeyRound } from "lucide-react";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { FunctionManager } from "@/components/functions/function-manager";
import { FunctionKeysPanel } from "@/components/functions/function-keys-panel";
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
  const [selectedAppKey, setSelectedAppKey] = useState<string | null>(null);

  useEffect(() => {
    if (apps.length > 0 && !selectedAppKey) setSelectedAppKey(apps[0].appKey);
  }, [apps, selectedAppKey]);

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
            <TabsTrigger value="keys">
              <KeyRound className="size-4" />
              调用密钥
            </TabsTrigger>
          </TabsList>

          <TabsContent value="functions">
            <FunctionManager appKey={selectedApp?.appKey} />
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
