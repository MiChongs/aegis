"use client";

import { siAlibabacloud, siMailgun, siQq, siResend } from "simple-icons";

/**
 * 邮件服务商品牌图标。
 *
 * 与支付渠道（`payment-brand-icon.tsx`）同源同规范：图形一律取自 Simple Icons
 * （官方品牌 mark，24×24 单路径，`fill: currentColor`），slug 由后端
 * `Describe().Icon` 下发。因此后端新增服务商时，只要填对 slug 前端即自动显示，
 * 无需改动本文件；未收录的用下方中性 mark 兜底，不冒充任何品牌。
 */
type IconEntry = { title: string; hex: string; path: string };

/** Simple Icons 未收录：通用 SMTP（中性信封，非品牌标识） */
const smtpMark: IconEntry = {
  title: "SMTP",
  hex: "0F766E",
  path: "M2 6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6zm2.4.8L12 12.4l7.6-5.6H4.4zM20 8.6l-7.4 5.5a1 1 0 0 1-1.2 0L4 8.6V18h16V8.6z"
};

const genericMark: IconEntry = {
  title: "邮件服务商",
  hex: "64748B",
  path: "M3 5h18a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1zm1.6 2L12 12.2 19.4 7H4.6z"
};

/**
 * slug → 图标；键名与后端下发的 Simple Icons slug 保持一致。
 *
 * 只登记 Simple Icons **确实收录**的那几家。AWS / Twilio / Postmark / Zeabur
 * 已因商标政策从该图标集移除，硬造一个形似的 mark 顶上去等于伪造品牌标识 ——
 * 它们统一落到下面的中性信封，与支付渠道对未收录渠道的处理一致。
 */
const registry: Record<string, IconEntry> = {
  // 后端给 SMTP 用的 slug 是 maildotru，这里换成中性信封而不是 mail.ru 的品牌 mark：
  // 「SMTP 直连」与那家邮箱服务商没有任何关系。
  maildotru: smtpMark,
  resend: siResend,
  mailgun: siMailgun,
  alibabacloud: siAlibabacloud,
  tencentqq: siQq
};

export function getEmailBrandIcon(slug?: string | null): IconEntry {
  if (!slug) return genericMark;
  return registry[slug.trim().toLowerCase()] ?? genericMark;
}

export function getEmailBrandColor(slug?: string | null, override?: string | null): string {
  if (override && /^#[0-9a-f]{6}$/i.test(override.trim())) return override.trim();
  return `#${getEmailBrandIcon(slug).hex}`;
}

/**
 * 带品牌底色的图标徽章。
 *
 * 品牌色直接做背景在深色模式下常常糊成一团（Postmark 的亮黄尤甚），
 * 因此统一用 12% 透明度品牌色作底、纯品牌色作前景，两种主题下都能保证对比度。
 */
export function EmailBrandBadge({
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
  const icon = getEmailBrandIcon(slug);
  const color = getEmailBrandColor(slug, brandColor);
  const box = size === "sm" ? "size-7 rounded-lg" : size === "lg" ? "size-12 rounded-2xl" : "size-9 rounded-xl";
  const glyph = size === "sm" ? "size-3.5" : size === "lg" ? "size-6" : "size-4.5";
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center ${box} ${className}`}
      style={{ backgroundColor: `${color}1F`, color }}
    >
      <svg role="img" viewBox="0 0 24 24" className={glyph} fill="currentColor" aria-label={name} aria-hidden={name ? undefined : true}>
        {name && <title>{name}</title>}
        <path d={icon.path} />
      </svg>
    </span>
  );
}
