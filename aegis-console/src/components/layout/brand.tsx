"use client";

import { Command as CommandIcon } from "lucide-react";
import { useBranding } from "@/lib/branding-provider";
import { cn } from "@/lib/utils";

/**
 * 品牌标识（读 BrandingContext）。
 *
 * 尺寸走固定映射而不是 `size-${n}` 模板串 —— Tailwind 只扫描源码里**字面出现**的类名，
 * 拼出来的那种在生产构建里可能整条规则都不存在，表现为 logo 突然变成 0×0。
 */
const LOGO_BOX = {
  sm: "size-6",
  md: "size-8",
  lg: "size-9"
} as const;

const LOGO_GLYPH = {
  sm: "size-3",
  md: "size-3.5",
  lg: "size-4"
} as const;

export type BrandLogoSize = keyof typeof LOGO_BOX;

export function BrandLogo({ size = "md", className }: { size?: BrandLogoSize; className?: string }) {
  const { logoURL } = useBranding();

  if (logoURL) {
    // 品牌 logo 是运行期从平台配置下发的任意地址，走不了 next/image 的域名白名单
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={logoURL} alt="" className={cn(LOGO_BOX[size], "rounded-lg object-contain", className)} />;
  }

  return (
    <div
      className={cn(
        LOGO_BOX[size],
        "flex items-center justify-center rounded-lg bg-primary text-primary-foreground",
        className
      )}
    >
      <CommandIcon className={LOGO_GLYPH[size]} />
    </div>
  );
}

/** 品牌两行标题（平台名 + 控制台名） */
export function BrandTitle({ className }: { className?: string }) {
  const { platformName, consoleName } = useBranding();
  return (
    <div className={cn("min-w-0", className)}>
      <p className="truncate text-sm leading-tight font-semibold">{platformName || "Aegis"}</p>
      <p className="truncate text-[10px] leading-tight text-muted-foreground">{consoleName || "控制台"}</p>
    </div>
  );
}

export function BrandNameText() {
  const { platformName } = useBranding();
  return <>{platformName || "Aegis"}</>;
}

export function BrandSubtitleText() {
  const { consoleName } = useBranding();
  return <>{consoleName || "控制台"}</>;
}
