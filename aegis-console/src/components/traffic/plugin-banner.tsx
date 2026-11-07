"use client";

import Link from "next/link";
import { Loader2, Power, PowerOff, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { NativePlugin, TrafficSettings } from "@/lib/api/traffic";
import { useNativePluginActionMutation, useSaveTrafficSettingsMutation } from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { PLUGIN_STATE_LABEL } from "./traffic-format";

/**
 * 插件没在运行、或总开关关着时，总览顶部的一条状态横幅。两件事都能就地处理：
 * 启用 / 重启插件走内置插件接口，打开总开关走全局设置。
 */
export function TrafficStateBanner({
  plugin,
  settings
}: {
  plugin: NativePlugin | null | undefined;
  settings: TrafficSettings | null | undefined;
}) {
  const action = useNativePluginActionMutation();
  const saveSettings = useSaveTrafficSettingsMutation();

  const pluginDown = !plugin || plugin.state !== "running";
  const switchedOff = !pluginDown && settings && !settings.enabled;
  if (!pluginDown && !switchedOff) return null;

  const failed = plugin?.state === "failed";
  const transitioning = plugin?.state === "starting" || plugin?.state === "stopping";

  return (
    <div
      role="status"
      className={cn(
        "flex min-w-0 flex-wrap items-center gap-x-4 gap-y-3 rounded-2xl border px-4 py-3 sm:px-5",
        failed ? "border-rose-500/30 bg-rose-500/8" : "border-amber-500/30 bg-amber-500/8"
      )}
    >
      <span
        aria-hidden
        className={cn(
          "grid size-8 shrink-0 place-items-center rounded-lg",
          failed ? "bg-rose-500/15 text-rose-600 dark:text-rose-400" : "bg-amber-500/15 text-amber-600 dark:text-amber-400"
        )}
      >
        <PowerOff className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">
          {pluginDown
            ? plugin
              ? `流量管理插件${PLUGIN_STATE_LABEL[plugin.state]}`
              : "流量管理插件未安装"
            : "流量管理总开关已关闭"}
        </div>
        {pluginDown && plugin?.error ? (
          <div className="mt-0.5 font-mono text-xs break-all text-rose-700 dark:text-rose-300">{plugin.error}</div>
        ) : null}
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {pluginDown && plugin && !plugin.enabled ? (
          <Button size="sm" disabled={action.isPending} onClick={() => action.mutate({ id: plugin.id, action: "enable" })}>
            {action.isPending ? <Loader2 className="animate-spin" /> : <Power />}
            启用插件
          </Button>
        ) : null}
        {pluginDown && plugin && plugin.enabled && !transitioning ? (
          <Button size="sm" disabled={action.isPending} onClick={() => action.mutate({ id: plugin.id, action: "restart" })}>
            {action.isPending ? <Loader2 className="animate-spin" /> : <RotateCw />}
            重启插件
          </Button>
        ) : null}
        {switchedOff && settings ? (
          <Button size="sm" disabled={saveSettings.isPending} onClick={() => saveSettings.mutate({ ...settings, enabled: true })}>
            {saveSettings.isPending ? <Loader2 className="animate-spin" /> : <Power />}
            开启
          </Button>
        ) : null}
        <Button size="sm" variant="ghost" asChild>
          <Link href="/plugins?tab=native">内置插件</Link>
        </Button>
      </div>
    </div>
  );
}
