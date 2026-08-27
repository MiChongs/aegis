"use client";

import {
  siAlibabacloud,
  siAnthropic,
  siBytedance,
  siDeepseek,
  siGooglegemini,
  siMoonshotai,
  siOllama,
  siOpenrouter,
  siX
} from "simple-icons";

/**
 * AI 供应商品牌图标。
 *
 * 与邮件（`email-brand-icon.tsx`）同源同规范：图形取自 Simple Icons，
 * slug 由后端目录下发。OpenAI / Azure / Groq / 智谱 / 硅基流动等因商标政策
 * 未被 Simple Icons 收录（或从未收录），一律落到下方中性 spark 标记 ——
 * 硬造形似的 mark 等于伪造品牌标识。
 */
type IconEntry = { title: string; hex: string; path: string };

/** 中性 AI 标记（四角星火花，非任何品牌）。 */
const genericMark: IconEntry = {
  title: "AI 供应商",
  hex: "64748B",
  path: "M12 2l2.09 5.53L19.5 9.5l-5.41 1.97L12 17l-2.09-5.53L4.5 9.5l5.41-1.97L12 2zm7 11l1.05 2.76L22.5 16.8l-2.45.9L19 20.5l-1.05-2.8-2.45-.9 2.45-1.04L19 13zM5 14l.9 2.35 2.1.9-2.1.89L5 20.5l-.9-2.36-2.1-.89 2.1-.9L5 14z"
};

const registry: Record<string, IconEntry> = {
  anthropic: siAnthropic,
  googlegemini: siGooglegemini,
  deepseek: siDeepseek,
  moonshotai: siMoonshotai,
  alibabacloud: siAlibabacloud,
  bytedance: siBytedance,
  openrouter: siOpenrouter,
  x: siX,
  ollama: siOllama
};

export function getAIBrandIcon(slug?: string | null): IconEntry {
  if (!slug) return genericMark;
  return registry[slug.trim().toLowerCase()] ?? genericMark;
}

export function getAIBrandColor(slug?: string | null, override?: string | null): string {
  if (override && /^#[0-9a-f]{6}$/i.test(override.trim())) return override.trim();
  return `#${getAIBrandIcon(slug).hex}`;
}

/** 带品牌底色的图标徽章：12% 透明度品牌色作底、纯品牌色作前景。 */
export function AIBrandBadge({
  slug,
  brandColor,
  name,
  size = "md",
  className = ""
}: {
  slug?: string | null;
  brandColor?: string | null;
  name?: string;
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const icon = getAIBrandIcon(slug);
  const color = getAIBrandColor(slug, brandColor);
  const box = size === "sm" ? "size-7 rounded-lg" : size === "lg" ? "size-12 rounded-2xl" : "size-9 rounded-xl";
  const glyph = size === "sm" ? "size-3.5" : size === "lg" ? "size-6" : "size-4.5";
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center ${box} ${className}`}
      style={{ backgroundColor: `${color}1F`, color }}
    >
      <svg
        role="img"
        viewBox="0 0 24 24"
        className={glyph}
        fill="currentColor"
        aria-label={name}
        aria-hidden={name ? undefined : true}
      >
        {name && <title>{name}</title>}
        <path d={icon.path} />
      </svg>
    </span>
  );
}
