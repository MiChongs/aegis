"use client";

import { useState } from "react";
import Link from "next/link";
import { ArrowRight, Boxes, Loader2, Power, PowerOff, RotateCw } from "lucide-react";
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
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { PluginStateBadge } from "@/components/traffic/traffic-kit";
import type { NativePlugin } from "@/lib/api/traffic";
import { formatRelative } from "@/lib/datetime";
import { trafficErrorMessage, useNativePluginActionMutation, useNativePluginsQuery } from "@/lib/traffic-hooks";

/** 有控制台页面的内置插件 */
const PLUGIN_PAGES: Record<string, { href: string; label: string }> = {
  traffic: { href: "/traffic", label: "流量管理" }
};

/**
 * 内置插件：随服务端二进制一起发布的原生插件（流量管理等）。只能启停与重启，不能删除。
 */
export function NativePluginsPanel() {
  const pluginsQuery = useNativePluginsQuery();
  const action = useNativePluginActionMutation();
  const [pendingDisable, setPendingDisable] = useState<NativePlugin | null>(null);
  const plugins = pluginsQuery.data ?? [];
  const busyId = action.isPending ? action.variables?.id : undefined;

  if (pluginsQuery.isLoading) return <LoadingState title="加载中" />;
  if (pluginsQuery.isError) return <EmptyState title={trafficErrorMessage(pluginsQuery.error, "读取失败")} />;
  if (!plugins.length) return <EmptyState title="暂无内置插件" />;

  return (
    <div className="space-y-2">
      {plugins.map((plugin) => {
        const page = PLUGIN_PAGES[plugin.id];
        const busy = busyId === plugin.id;
        const transitioning = plugin.state === "starting" || plugin.state === "stopping";
        return (
          <Card key={plugin.id}>
            <CardContent className="flex flex-wrap items-start gap-4 p-4">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/50">
                <Boxes className="size-4 text-muted-foreground" />
              </div>
              <div className="min-w-0 flex-1 basis-60">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-semibold">{plugin.name || plugin.id}</span>
                  <PluginStateBadge state={plugin.state} />
                  <Badge variant="outline" size="sm" className="font-mono">
                    v{plugin.version}
                  </Badge>
                  {plugin.builtin ? (
                    <Badge variant="outline" size="sm">
                      内置
                    </Badge>
                  ) : null}
                </div>
                {plugin.description ? <p className="mt-0.5 text-xs text-muted-foreground">{plugin.description}</p> : null}
                {plugin.capabilities?.length ? (
                  <div className="mt-2 flex flex-wrap gap-1">
                    {plugin.capabilities.map((capability) => (
                      <span key={capability} className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
                        {capability}
                      </span>
                    ))}
                  </div>
                ) : null}
                {plugin.state === "failed" && plugin.error ? (
                  <p className="mt-2 font-mono text-[11px] break-all text-rose-600 dark:text-rose-400">{plugin.error}</p>
                ) : null}
                {plugin.since && plugin.state === "running" ? (
                  <p className="mt-1 text-[11px] text-muted-foreground">启动于 {formatRelative(plugin.since)}</p>
                ) : null}
              </div>
              <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                {page ? (
                  <Button size="sm" variant="ghost" asChild>
                    <Link href={page.href}>
                      {page.label}
                      <ArrowRight />
                    </Link>
                  </Button>
                ) : null}
                {plugin.enabled ? (
                  <>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy || transitioning}
                      onClick={() => action.mutate({ id: plugin.id, action: "restart" })}
                    >
                      {busy && action.variables?.action === "restart" ? <Loader2 className="animate-spin" /> : <RotateCw />}
                      重启
                    </Button>
                    <Button size="sm" variant="outline" disabled={busy || transitioning} onClick={() => setPendingDisable(plugin)}>
                      {busy && action.variables?.action === "disable" ? <Loader2 className="animate-spin" /> : <PowerOff />}
                      停用
                    </Button>
                  </>
                ) : (
                  <Button size="sm" disabled={busy || transitioning} onClick={() => action.mutate({ id: plugin.id, action: "enable" })}>
                    {busy ? <Loader2 className="animate-spin" /> : <Power />}
                    启用
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>
        );
      })}

      <AlertDialog open={Boolean(pendingDisable)} onOpenChange={(open) => !open && setPendingDisable(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>停用「{pendingDisable?.name || pendingDisable?.id}」</AlertDialogTitle>
            <AlertDialogDescription>停用后该插件提供的能力立即失效。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (pendingDisable) action.mutate({ id: pendingDisable.id, action: "disable" });
                setPendingDisable(null);
              }}
            >
              停用
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
