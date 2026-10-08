"use client";

import { useState } from "react";
import { Monitor, Smartphone, Tablet } from "lucide-react";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import type { DeviceInfo } from "@/lib/api/device-info";
import { cn } from "@/lib/utils";

/**
 * 会话与登录记录里的一台设备。
 *
 * 行内：厂商图标 + 展示名 + 系统与应用版本，同一用户的多台设备一眼能分开。
 * 悬停：设备图与字典映射的全部字段（型号、代号、品牌、厂商、平台、名称来源）。
 * 没有 deviceInfo 的旧数据由调用方传 fallback 兜底（通常是 UA 解析结果）。
 */

const SOURCE_LABEL: Record<string, string> = {
  dictionary: "设备字典",
  client: "客户端上报",
  user_agent: "UA 推断",
  unknown: "无法识别"
};

const PLATFORM_LABEL: Record<string, string> = {
  android: "Android",
  ios: "iOS",
  harmonyos: "HarmonyOS",
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
  web: "网页"
};

function PlatformIcon({ info, className }: { info?: DeviceInfo; className?: string }) {
  const platform = info?.platform ?? "";
  const text = `${info?.name ?? ""} ${info?.identifier ?? ""}`.toLowerCase();
  const Icon = /ipad|tab|pad\b/.test(text)
    ? Tablet
    : ["android", "ios", "harmonyos"].includes(platform)
      ? Smartphone
      : Monitor;
  return <Icon className={className} />;
}

/** 远程图片：加载前显示骨架，加载失败时整块不渲染（交给调用方的兜底）。 */
function RemoteImage({
  src,
  alt,
  className,
  skeletonClassName,
  onFail
}: {
  src: string;
  alt: string;
  className?: string;
  skeletonClassName?: string;
  onFail?: () => void;
}) {
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  if (state === "failed") return null;
  return (
    <>
      {state === "loading" ? <Skeleton className={skeletonClassName} /> : null}
      {/* eslint-disable-next-line @next/next/no-img-element -- 设备字典里的任意外链，不走 next/image 的域名白名单 */}
      <img
        src={src}
        alt={alt}
        loading="lazy"
        referrerPolicy="no-referrer"
        className={cn(className, state === "loading" && "hidden")}
        onLoad={() => setState("ready")}
        onError={() => {
          setState("failed");
          onFail?.();
        }}
      />
    </>
  );
}

/** 系统与应用版本的一行副标题，如「Android 15 / v2.3.0」。 */
export function deviceSubtitle(info?: DeviceInfo | null) {
  if (!info) return "";
  const os = info.os || PLATFORM_LABEL[info.platform ?? ""] || "";
  const parts = [
    [os, info.osVersion].filter(Boolean).join(" "),
    info.appVersion ? `v${info.appVersion.replace(/^v/i, "")}` : ""
  ].filter(Boolean);
  return parts.join(" / ");
}

export function DeviceInfoChip({
  info,
  fallback,
  className
}: {
  info?: DeviceInfo | null;
  /** 没有 deviceInfo（旧数据）或展示名为空时的显示文本 */
  fallback?: string;
  className?: string;
}) {
  const [iconFailed, setIconFailed] = useState(false);
  const name = info?.name || fallback || "未知设备";
  const subtitle = deviceSubtitle(info);
  const icon =
    info?.manufacturerIconUrl && !iconFailed ? (
      <RemoteImage
        src={info.manufacturerIconUrl}
        alt={info.manufacturer ?? ""}
        className="size-4 shrink-0 object-contain"
        skeletonClassName="size-4 shrink-0 rounded"
        onFail={() => setIconFailed(true)}
      />
    ) : (
      <PlatformIcon info={info ?? undefined} className="size-3.5 shrink-0 text-muted-foreground" />
    );

  const chip = (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
      {icon}
      <span className="min-w-0">
        <span className="block truncate text-xs">{name}</span>
        {subtitle ? <span className="block truncate text-[10px] text-muted-foreground">{subtitle}</span> : null}
      </span>
    </span>
  );

  if (!info) return chip;

  return (
    <HoverCard openDelay={150} closeDelay={80}>
      <HoverCardTrigger asChild>
        <button type="button" className="min-w-0 max-w-full cursor-default text-left">
          {chip}
        </button>
      </HoverCardTrigger>
      <HoverCardContent side="top" align="start" className="w-72 p-0">
        <DeviceInfoCard info={info} />
      </HoverCardContent>
    </HoverCard>
  );
}

/** 悬停卡：设备图 + 字段表。 */
export function DeviceInfoCard({ info }: { info: DeviceInfo }) {
  const rows: Array<[string, string | undefined]> = [
    ["营销名", info.marketingName],
    ["厂商", info.manufacturer],
    ["品牌", info.brand],
    ["型号", info.identifier],
    ["代号", info.codename],
    ["平台", PLATFORM_LABEL[info.platform ?? ""] ?? info.platform],
    ["系统", [info.os, info.osVersion].filter(Boolean).join(" ") || undefined],
    ["应用版本", info.appVersion],
    ["字典条目", info.dictionaryId ? `#${info.dictionaryId}` : undefined]
  ];
  return (
    <div className="overflow-hidden rounded-md">
      <div className="flex items-center gap-3 border-b bg-muted/40 p-3">
        <div className="grid size-16 shrink-0 place-items-center overflow-hidden rounded-lg bg-background ring-1 ring-border">
          {info.deviceImageUrl ? (
            <RemoteImage
              src={info.deviceImageUrl}
              alt={info.name}
              className="max-h-14 max-w-14 object-contain"
              skeletonClassName="size-14 rounded-md"
            />
          ) : (
            <PlatformIcon info={info} className="size-6 text-muted-foreground" />
          )}
        </div>
        <div className="min-w-0 space-y-1">
          <div className="truncate text-sm font-medium">{info.name || "未知设备"}</div>
          <Badge variant={info.matched ? "success" : "outline"} size="sm">
            {SOURCE_LABEL[info.source] ?? info.source}
          </Badge>
        </div>
      </div>
      <dl className="grid grid-cols-[4.5rem_minmax(0,1fr)] gap-x-2 gap-y-1.5 p-3 text-xs">
        {rows
          .filter(([, value]) => Boolean(value))
          .map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="truncate font-mono text-[11px]" title={value}>
                {value}
              </dd>
            </div>
          ))}
      </dl>
    </div>
  );
}
