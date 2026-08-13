"use client";

import { cn } from "@/lib/utils";
import {
  brandLogos,
  type BrandGlyph,
  type BrandLogoSlug,
} from "@/components/brand/sponsors/brand-logos.generated";

/**
 * 赞助商品牌标识。
 *
 * 字标不是用系统字体排出来的文字，而是各品牌自有字体轮廓化后的路径。
 * 用 `font-semibold` 打一行 "Cloudflare" 单看像回事，一排 logo 摆在一起时
 * 会发现只有它是 Geist。
 *
 * 尺寸由**字号**决定：两个 glyph 都按 `height: 1em` 渲染，宽度按 viewBox 的宽高比
 * 换算成 em 一并写死。只给高度让浏览器自己推宽度也能work，但 SVG 的固有宽高比
 * 在 flex 容器里不总被采纳，写死才不会在布局稳定前抖一下。
 *
 * 图形标是彩色的，字标恒为单色（走 `currentColor`，由外层决定深浅）——
 * 彩色图形标 + 单色字标正是这些品牌自己的横向锁定版式。整排全上色会让
 * 二十几套配色互相打架，谁也读不出来。
 */

function Glyph({
  glyph,
  title,
  className,
}: {
  glyph: BrandGlyph;
  title?: string;
  className?: string;
}) {
  const [, , width, height] = glyph.viewBox.split(/[\s,]+/).map(Number);
  const ratio = width && height ? width / height : 1;

  return (
    <svg
      role={title ? "img" : undefined}
      aria-hidden={title ? undefined : true}
      aria-label={title}
      viewBox={glyph.viewBox}
      fill="currentColor"
      fillRule={glyph.fillRule}
      className={cn("block h-[1em] shrink-0", className)}
      style={{ width: `${ratio}em` }}
      // 内容是构建期从本地资源包生成的静态标记（scripts/sync-brand-logos.mjs），
      // 不含任何运行时输入。彩色版带 <defs>/<linearGradient>，拆成结构化数据
      // 等于在组件里实现半个 SVG 解析器。
      dangerouslySetInnerHTML={{ __html: glyph.body }}
    />
  );
}

export function BrandLogo({
  slug,
  variant = "combine",
  className,
}: {
  slug: BrandLogoSlug;
  /** `combine` = 图形标 + 字标，与品牌官方横向锁定版式一致 */
  variant?: "combine" | "mark" | "wordmark";
  className?: string;
}) {
  const logo = brandLogos[slug];

  if (variant !== "combine") {
    return (
      <Glyph
        glyph={variant === "mark" ? logo.mark : logo.wordmark}
        title={logo.title}
        className={className}
      />
    );
  }

  return (
    <span
      role="img"
      aria-label={logo.title}
      className={cn("inline-flex items-center gap-[0.4em]", className)}
    >
      <Glyph glyph={logo.mark} />
      <Glyph glyph={logo.wordmark} />
    </span>
  );
}
