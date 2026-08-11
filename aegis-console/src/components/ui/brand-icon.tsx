"use client";

import {
  siDiscord,
  siGitee,
  siGithub,
  siGitlab,
  siGoogle,
  siLinux,
  siOpenid,
  siQq,
  siSinaweibo,
  siWechat
} from "simple-icons";

/**
 * 第三方渠道品牌图标。
 *
 * 图形一律取自 Simple Icons（官方品牌 mark，24×24 单路径，`fill: currentColor`）。
 * Simple Icons 因商标原因未收录的两个渠道由内置 mark 兜底：
 *   - Microsoft：官方四方格标志，形状简单且无版权路径依赖；
 *   - 自定义渠道：中性的「插件」几何图形，不冒充任何品牌。
 *
 * slug 由后端渠道模板下发（`Template.Icon` / `Provider.Icon`），与 Simple Icons 的
 * slug 对齐，因此新增渠道时只要后端填对 slug，前端无需改动即可显示品牌图标。
 */
type IconEntry = { title: string; hex: string; path: string };

/** Simple Icons 未收录：Microsoft 四方格 */
const microsoftMark: IconEntry = {
  title: "Microsoft",
  hex: "00A4EF",
  path: "M11.4 11.4H2V2h9.4v9.4zM22 11.4h-9.4V2H22v9.4zM11.4 22H2v-9.4h9.4V22zM22 22h-9.4v-9.4H22V22z"
};

/** Simple Icons 未收录：自定义渠道的中性占位（非品牌标识） */
const customMark: IconEntry = {
  title: "自定义",
  hex: "64748B",
  path: "M12 2a3 3 0 0 1 3 3v1h3a1 1 0 0 1 1 1v3h1a3 3 0 1 1 0 6h-1v3a1 1 0 0 1-1 1h-3v-1a3 3 0 1 0-6 0v1H6a1 1 0 0 1-1-1v-3H4a3 3 0 1 1 0-6h1V7a1 1 0 0 1 1-1h3V5a3 3 0 0 1 3-3z"
};

/** slug → 图标；键名与 Simple Icons slug 保持一致 */
const registry: Record<string, IconEntry> = {
  wechat: siWechat,
  qq: siQq,
  sinaweibo: siSinaweibo,
  gitee: siGitee,
  github: siGithub,
  gitlab: siGitlab,
  google: siGoogle,
  discord: siDiscord,
  linux: siLinux,
  openid: siOpenid,
  microsoft: microsoftMark,
  custom: customMark
};

export function getBrandIcon(slug?: string | null): IconEntry | undefined {
  if (!slug) return undefined;
  return registry[slug.trim().toLowerCase()];
}

/** 取渠道官方品牌色（`#RRGGBB`）；未收录时返回 undefined，由调用方决定兜底 */
export function getBrandColor(slug?: string | null): string | undefined {
  const icon = getBrandIcon(slug);
  return icon ? `#${icon.hex}` : undefined;
}

export function BrandIcon({
  slug,
  className,
  title
}: {
  slug?: string | null;
  className?: string;
  title?: string;
}) {
  const icon = getBrandIcon(slug) ?? customMark;
  return (
    <svg
      role="img"
      viewBox="0 0 24 24"
      className={className}
      fill="currentColor"
      aria-hidden={title ? undefined : true}
      aria-label={title}
    >
      {title && <title>{title}</title>}
      <path d={icon.path} />
    </svg>
  );
}
