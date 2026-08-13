import fs from "node:fs";
import path from "node:path";

/**
 * 赞助商品牌标识的取数脚本。
 *
 * 图形与字标一律取自 LobeHub 的官方静态资源包 `@lobehub/icons-static-svg`
 * （零依赖，只是一堆 SVG 文件；它的 React 版 `@lobehub/icons` peer 依赖 antd 全家桶，
 * 为了几个 logo 把 antd 拖进来不划算）。
 *
 * 为什么要「生成一份 TS」而不是直接 import SVG：
 *   - 本项目走 Turbopack，没有配 svgr，`import logo from "*.svg"` 拿到的是 URL 而不是组件；
 *   - 放 public/ 用 <img> 又会丢掉 `currentColor` —— 单色字标在深色模式下会直接消失。
 *
 * 产物需要一起提交。换版本或加赞助商后跑 `pnpm logos:sync`。
 */

/**
 * 要收录的品牌。`slug` 同时是产物里的键与静态包的文件名前缀。
 *
 * 字标默认取 `<slug>-text.svg`；`wordmarkFile` 用于少数不守这个命名的品牌
 * （Google 的字标叫 `google-brand`）。`title` 用于纠正上游写错的 <title>。
 *
 * `mono: true` 表示放弃彩色版、强制走 `currentColor` 单色版。只有一种情况用它：
 * 彩色版的主体是白色（Kimi 是白色字形加一个小蓝点），放在浅色底的带子上
 * 只剩那个点，看起来像图裂了。单色版反而两种主题都成立。
 */
const BRANDS = [
  { slug: "zeabur" },
  { slug: "cloudflare" },
  { slug: "alibabacloud", title: "Alibaba Cloud" },
  { slug: "tencentcloud", title: "Tencent Cloud" },
  { slug: "huaweicloud", title: "Huawei Cloud" },
  { slug: "volcengine" },
  { slug: "aws" },
  { slug: "azure" },
  { slug: "qiniu" },
  { slug: "google", wordmarkFile: "google-brand" },
  { slug: "vercel" },
  { slug: "github", title: "GitHub" },
  { slug: "nvidia" },
  { slug: "anthropic" },
  { slug: "openai", title: "OpenAI" },
  { slug: "gemini" },
  { slug: "deepseek" },
  { slug: "qwen" },
  { slug: "kimi", mono: true },
  { slug: "mistral", title: "Mistral AI" },
  { slug: "huggingface", title: "Hugging Face" },
  { slug: "cursor" },
  { slug: "figma" },
  { slug: "notion" }
];

const rootDir = path.resolve(import.meta.dirname, "..");
const iconsDir = path.join(rootDir, "node_modules", "@lobehub", "icons-static-svg", "icons");
const outputPath = path.join(rootDir, "src", "components", "brand", "sponsors", "brand-logos.generated.ts");

/** 明确不允许出现在图标里的元素：它们要么能发起请求，要么能执行脚本 */
const FORBIDDEN = ["script", "image", "foreignObject", "use", "a"];

/**
 * 把一个 SVG 文件拆成 `{ title, viewBox, fillRule, body }`。
 *
 * `body` 是原样保留的内部标记（去掉 <title>）。彩色版会带 <defs> / <linearGradient>，
 * 拆成结构化数据等于在这里实现半个 SVG 解析器；而保留原文还顺带绕开了
 * JSX 的属性改名问题（`fill-rule` / `stop-color` 在原文里本来就是对的）。
 */
function parseGlyph(file) {
  const source = fs.readFileSync(file, "utf8");

  const viewBox = source.match(/viewBox="([^"]+)"/)?.[1];
  if (!viewBox) throw new Error(`${path.basename(file)}: 缺少 viewBox`);

  const title = source.match(/<title>([^<]*)<\/title>/)?.[1] ?? "";
  const fillRule = source.match(/<svg[^>]*\sfill-rule="([^"]+)"/)?.[1];
  if (fillRule && fillRule !== "evenodd" && fillRule !== "nonzero") {
    throw new Error(`${path.basename(file)}: 未知的 fill-rule "${fillRule}"`);
  }

  const body = source
    .replace(/<\?xml[^>]*\?>/g, "")
    .replace(/<svg[^>]*>|<\/svg>/g, "")
    .replace(/<title>[^<]*<\/title>/g, "")
    .trim();

  const found = [...body.matchAll(/<([a-zA-Z]+)/g)].map((match) => match[1]);
  const forbidden = found.filter((tag) => FORBIDDEN.includes(tag));
  if (forbidden.length > 0) {
    throw new Error(`${path.basename(file)}: 含不允许的元素 <${[...new Set(forbidden)].join("> <")}>`);
  }
  if (body.includes("on") && /\son[a-z]+=/.test(body)) {
    throw new Error(`${path.basename(file)}: 含事件属性`);
  }
  if (found.length === 0) throw new Error(`${path.basename(file)}: 内容为空`);

  return { title, viewBox, fillRule, body };
}

function readVariant(name, { optional = false } = {}) {
  const file = path.join(iconsDir, `${name}.svg`);
  if (!fs.existsSync(file)) {
    if (optional) return null;
    throw new Error(`缺少图标文件 ${path.relative(rootDir, file)}`);
  }
  return parseGlyph(file);
}

const version = JSON.parse(
  fs.readFileSync(path.join(rootDir, "node_modules", "@lobehub", "icons-static-svg", "package.json"), "utf8")
).version;

/**
 * 图形标只存一份：有彩色版就用彩色版，没有就用单色版。
 *
 * 两份都存的话产物会多出四分之一体积，而单色版永远不会被渲染到 ——
 * 这份数据是要进客户端包的，二十几个品牌堆起来不是可以忽略的量。
 */
const entries = BRANDS.map((brand) => {
  const mono = readVariant(brand.slug);
  const color = brand.mono ? null : readVariant(`${brand.slug}-color`, { optional: true });
  const wordmark = readVariant(brand.wordmarkFile ?? `${brand.slug}-text`);
  return {
    slug: brand.slug,
    title: brand.title ?? mono.title ?? wordmark.title,
    mark: color ?? mono,
    colored: Boolean(color),
    wordmark
  };
});

const serializeGlyph = (glyph, indent) => {
  if (!glyph) return "null";
  const pad = " ".repeat(indent);
  return [
    `{`,
    `${pad}  viewBox: ${JSON.stringify(glyph.viewBox)},`,
    glyph.fillRule ? `${pad}  fillRule: ${JSON.stringify(glyph.fillRule)},` : null,
    `${pad}  body: ${JSON.stringify(glyph.body)}`,
    `${pad}}`
  ]
    .filter(Boolean)
    .join("\n");
};

const body = entries
  .map(
    (entry) =>
      `  ${entry.slug}: {\n` +
      `    title: ${JSON.stringify(entry.title)},\n` +
      `    mark: ${serializeGlyph(entry.mark, 4)},\n` +
      `    wordmark: ${serializeGlyph(entry.wordmark, 4)}\n` +
      `  }`
  )
  .join(",\n");

const colored = entries.filter((entry) => entry.colored).length;

const output = `// 本文件由 scripts/sync-brand-logos.mjs 生成，请勿手工编辑。
// 数据源：@lobehub/icons-static-svg@${version}（商标归各自所有者所有）
// 重新生成：pnpm logos:sync

export type BrandGlyph = {
  viewBox: string;
  fillRule?: "evenodd" | "nonzero";
  /** SVG 内部标记原文，由 brand-logo.tsx 内联进 <svg> */
  body: string;
};

export type BrandLogo = {
  /** 官方名，用作 <title> 与无障碍标签 */
  title: string;
  /**
   * 图形标，优先彩色版。
   *
   * 少数品牌（Vercel / GitHub / Anthropic / OpenAI 等）的标识本身就是单色的，
   * 上游没有彩色版，这里落回 \`currentColor\` 单色版 —— 这反而正确：
   * 它们会跟着主题翻转，而不是在深色背景上留一团黑。
   */
  mark: BrandGlyph;
  /** 字标：品牌自有字体轮廓化后的路径，不是用系统字体排出来的 */
  wordmark: BrandGlyph;
};

export type BrandLogoSlug = keyof typeof brandLogos;

export const brandLogos = {
${body}
} as const satisfies Record<string, BrandLogo>;
`;

fs.mkdirSync(path.dirname(outputPath), { recursive: true });
fs.writeFileSync(outputPath, output);

console.log(
  `brand logos synced: ${entries.length} brands (${colored} colored) from @lobehub/icons-static-svg@${version}`
);
