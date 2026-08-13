"use client";

// 底图供应商选择器
//
// 默认档是「自动」：跟着浏览器语言走，简体中文用户拿到境内供应商，
// 其余用户拿到全球供应商。手动选过之后就锁定，不再随语言变化 ——
// 一个人一旦发现某家在自己网络下更快，这个结论不该被自动逻辑推翻。
//
// 缺密钥的条目**照常列出**但不可选，并直接写出要配哪个环境变量：
// 藏起来的话，部署者永远不会知道自己还能多几家可选。

import { Check, Globe2, Languages, Layers, MapPin, WifiOff } from "lucide-react";

import { cn } from "@/lib/utils";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useMapProvider } from "@/lib/geo/map-provider-store";
import {
  AUTO_PREFERENCE_VALUE,
  GROUP_LABELS,
  MAP_PROVIDERS,
  providerAvailable,
  resolveAutoProvider,
  type MapProvider,
  type MapProviderGroup,
  type MapRegionHint
} from "@/lib/geo/map-providers";

/** 使用者所在的那一档排在最前：他真正会选的几家不该要滚动才看得见 */
const GROUP_ORDER: Record<MapRegionHint, MapProviderGroup[]> = {
  cn: ["china", "global", "offline"],
  global: ["global", "china", "offline"]
};

const GROUP_ICON: Record<MapProviderGroup, typeof Globe2> = {
  china: MapPin,
  global: Globe2,
  offline: WifiOff
};

function DatumChip({ provider }: { provider: MapProvider }) {
  if (provider.datum !== "gcj02") return null;
  return (
    <span
      className="rounded border border-amber-500/40 bg-amber-500/10 px-1 text-[9.5px] leading-4 font-medium text-amber-600 dark:text-amber-400"
      title="该供应商为 GCJ-02 偏移坐标系；放大到街道层级后前端会自动把瓦片纠偏对齐 WGS-84"
    >
      GCJ-02
    </span>
  );
}

function ProviderRow({
  provider,
  selected,
  onSelect
}: {
  provider: MapProvider;
  selected: boolean;
  onSelect: () => void;
}) {
  const available = providerAvailable(provider);
  return (
    <button
      type="button"
      disabled={!available}
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-2 rounded-lg px-2 py-1.5 text-left transition-colors",
        available ? "hover:bg-muted/70" : "cursor-not-allowed opacity-55",
        selected && "bg-primary/10 ring-1 ring-primary/25"
      )}
    >
      <Check className={cn("mt-0.5 size-3.5 shrink-0", selected ? "text-primary" : "invisible")} />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-1">
          <span className="text-xs font-medium">{provider.name}</span>
          <DatumChip provider={provider} />
          {provider.langs.length > 1 && (
            <span
              className="flex items-center gap-0.5 rounded border border-border px-1 text-[9.5px] leading-4 text-muted-foreground"
              title="注记语言随浏览器语言切换"
            >
              <Languages className="size-2.5" />
              中 / EN
            </span>
          )}
        </span>
        <span className="mt-0.5 block text-[10.5px] leading-snug text-muted-foreground">
          {provider.description}
        </span>
        {!available && provider.credential && (
          <span className="mt-0.5 block font-mono text-[10px] leading-snug text-amber-600 dark:text-amber-500">
            需配置 {provider.credential.env}
          </span>
        )}
      </span>
    </button>
  );
}

export function MapProviderPicker() {
  const { pref, auto, provider, locale, lang, select } = useMapProvider();
  const autoTarget = resolveAutoProvider(locale.region);

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-lg border border-border bg-card/95 px-2 py-1 text-[11px] font-medium backdrop-blur-sm transition-colors hover:bg-muted"
          title="切换底图供应商"
        >
          <Layers className="size-3" />
          {provider.short}
          {auto && <span className="text-[9.5px] text-muted-foreground">自动</span>}
        </button>
      </PopoverTrigger>

      <PopoverContent side="top" align="start" className="max-h-[440px] w-[318px] overflow-y-auto p-2">
        <div className="px-2 pt-1 pb-2">
          <p className="text-xs font-semibold">底图供应商</p>
          <p className="mt-0.5 text-[10.5px] leading-snug text-muted-foreground">
            浏览器语言 <span className="font-mono">{locale.tag || "未知"}</span> · 注记
            {lang === "zh-CN" ? "中文" : "英文"} · 该选择对全站地图生效
          </p>
        </div>

        <button
          type="button"
          onClick={() => select(AUTO_PREFERENCE_VALUE)}
          className={cn(
            "flex w-full items-start gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-muted/70",
            auto && "bg-primary/10 ring-1 ring-primary/25"
          )}
        >
          <Check className={cn("mt-0.5 size-3.5 shrink-0", auto ? "text-primary" : "invisible")} />
          <span className="min-w-0 flex-1">
            <span className="text-xs font-medium">自动</span>
            <span className="mt-0.5 block text-[10.5px] leading-snug text-muted-foreground">
              跟随浏览器语言，当前解析为「{autoTarget.name}」
            </span>
          </span>
        </button>

        {GROUP_ORDER[locale.region].map((group) => {
          const items = MAP_PROVIDERS.filter((p) => p.group === group);
          if (items.length === 0) return null;
          const Icon = GROUP_ICON[group];
          return (
            <div key={group} className="mt-1.5">
              <p className="flex items-center gap-1 px-2 py-1 text-[10px] font-medium tracking-wide text-muted-foreground">
                <Icon className="size-2.5" />
                {GROUP_LABELS[group]}
              </p>
              <div className="space-y-0.5">
                {items.map((item) => (
                  <ProviderRow
                    key={item.id}
                    provider={item}
                    selected={pref === item.id}
                    onSelect={() => select(item.id)}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </PopoverContent>
    </Popover>
  );
}
