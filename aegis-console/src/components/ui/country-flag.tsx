"use client";

// 国旗渲染组件 —— 统一走 Twemoji 图片
//
// 为什么不用原生 emoji：Windows / 部分 Linux 不渲染区域指示符国旗，
// 只显示两个字母（如 "CN"）。Twemoji 用 SVG 图片在所有平台一致呈现。
// CDN 抖动 / 无效代码时回退为两字母代码小字，永不破版。

import { useState } from "react";
import { Globe } from "lucide-react";
import { cn } from "@/lib/utils";
import { flagTwemojiUrl } from "@/lib/geo/geo-map-shared";

type CountryFlagProps = {
  code?: string | null;
  /** 像素尺寸，默认 16 */
  size?: number;
  className?: string;
};

export function CountryFlag({ code, size = 16, className }: CountryFlagProps) {
  const [failed, setFailed] = useState(false);
  const cc = (code || "").trim().toUpperCase();
  const url = flagTwemojiUrl(cc);

  if (!url || failed) {
    // 回退：两字母代码小字徽标（仍可辨识来源），无效则 lucide 地球图标（不用 Emoji）
    return (
      <span
        className={cn(
          "inline-flex items-center justify-center rounded-[2px] bg-muted px-1 font-mono text-[9px] font-medium leading-none text-muted-foreground",
          className
        )}
        style={{ minWidth: size, height: Math.round(size * 0.72) }}
        title={cc || undefined}
      >
        {/^[A-Z]{2}$/.test(cc) ? cc : <Globe style={{ width: size * 0.62, height: size * 0.62 }} />}
      </span>
    );
  }

  return (
    <img
      src={url}
      alt={cc}
      width={size}
      height={size}
      loading="lazy"
      decoding="async"
      onError={() => setFailed(true)}
      className={cn("inline-block shrink-0 object-contain align-[-0.15em]", className)}
      style={{ width: size, height: size }}
    />
  );
}
