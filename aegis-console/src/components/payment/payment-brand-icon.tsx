"use client";

import {
  siAlipay,
  siBitcoin,
  siCoinbase,
  siLemonsqueezy,
  siPaddle,
  siPaypal,
  siRazorpay,
  siSquare,
  siStripe,
  siWechat
} from "simple-icons";

/**
 * 支付渠道品牌图标。
 *
 * 图形一律取自 Simple Icons（官方品牌 mark，24×24 单路径，`fill: currentColor`），
 * 与 `components/ui/brand-icon.tsx`（第三方登录渠道）同源同规范。
 *
 * slug 由后端 `Provider.Describe().Icon` 下发并与 Simple Icons 的 slug 对齐，
 * 因此后端新增支付渠道时，只要填对 slug 前端即自动显示品牌图标，无需改动本文件。
 * Simple Icons 未收录的渠道（内部钱包等）由下方中性 mark 兜底，不冒充任何品牌。
 */
type IconEntry = { title: string; hex: string; path: string };

/** Simple Icons 未收录：内部钱包（中性图形，非品牌标识） */
const walletMark: IconEntry = {
  title: "钱包",
  hex: "0EA5E9",
  path: "M21 7.5V6a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-1.5h-6a3 3 0 1 1 0-6h6zm0 1.5h-6a1.5 1.5 0 0 0 0 3h6V9z"
};

/** Simple Icons 未收录：未知渠道的中性占位 */
const genericMark: IconEntry = {
  title: "支付渠道",
  hex: "64748B",
  path: "M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1H2V7zm0 3h20v7a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-7zm3 4a1 1 0 0 0 0 2h4a1 1 0 1 0 0-2H5z"
};

/** slug → 图标；键名与后端下发的 Simple Icons slug 保持一致 */
const registry: Record<string, IconEntry> = {
  alipay: siAlipay,
  wechat: siWechat,
  stripe: siStripe,
  paypal: siPaypal,
  paddle: siPaddle,
  lemonsqueezy: siLemonsqueezy,
  razorpay: siRazorpay,
  coinbase: siCoinbase,
  square: siSquare,
  bitcoin: siBitcoin,
  wallet: walletMark
};

export function getPaymentBrandIcon(slug?: string | null): IconEntry {
  if (!slug) return genericMark;
  return registry[slug.trim().toLowerCase()] ?? genericMark;
}

/** 取渠道官方品牌色（`#RRGGBB`）。后端已下发 brandColor 时优先用后端值。 */
export function getPaymentBrandColor(slug?: string | null, override?: string | null): string {
  if (override && /^#[0-9a-f]{6}$/i.test(override.trim())) return override.trim();
  return `#${getPaymentBrandIcon(slug).hex}`;
}

export function PaymentBrandIcon({
  slug,
  className,
  title
}: {
  slug?: string | null;
  className?: string;
  title?: string;
}) {
  const icon = getPaymentBrandIcon(slug);
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

/**
 * 带品牌底色的图标徽章。
 *
 * 品牌色直接做背景在深色模式下常常糊成一团（Paddle 的亮黄、Square 的深灰尤甚），
 * 因此统一用 12% 透明度品牌色作底、纯品牌色作前景，两种主题下都能保证对比度。
 */
export function PaymentBrandBadge({
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
  const color = getPaymentBrandColor(slug, brandColor);
  const box = size === "sm" ? "size-7 rounded-lg" : size === "lg" ? "size-12 rounded-2xl" : "size-9 rounded-xl";
  const glyph = size === "sm" ? "size-3.5" : size === "lg" ? "size-6" : "size-4.5";
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center ${box} ${className}`}
      style={{ backgroundColor: `${color}1F`, color }}
    >
      <PaymentBrandIcon slug={slug} className={glyph} title={name} />
    </span>
  );
}
