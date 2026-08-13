"use client";

import type { Monaco } from "@monaco-editor/react";

/**
 * 把 Monaco 的配色接到控制台的设计令牌上。
 *
 * 内置的 `vs` / `vs-dark` 各自带一套写死的背景色（`#fffffe` / `#1e1e1e`），
 * 而控制台通体是 zinc（浅色卡片 `#ffffff`、深色卡片 `#18181b`）。
 * 两者差着一档，表现是编辑器像一块从别处贴过来的白板 / 黑板，
 * 边缘那一圈色差在深色模式下尤其明显。
 *
 * 语法着色复用 `--hljs-*` —— 那套令牌本来就在给开发者门户的代码块上色。
 * 共用一份的直接好处是：同一段脚本在文档里和在编辑器里长得一模一样。
 *
 * 主题只有一个名字（`aegis`），深浅切换时**就地重定义**而不是注册两份：
 * 令牌值是从 `document.documentElement` 上现读的，读的那一刻 `.dark`
 * 已经挂上去了，所以同一个名字每次都能拿到当前这套颜色。
 */
export const AEGIS_THEME = "aegis";

/** 单独一份给差异视图用：diff 的增删底色需要在两种主题下各自压暗/提亮 */
type Palette = Record<string, string>;

/**
 * 把任意 CSS 颜色规范化成 `#rrggbb`。
 *
 * Monaco 只认十六进制（内部走 `parseHex`），给它 `oklch()` 或
 * `color-mix()` 会静默失败 —— 那一项颜色直接不生效，而不会报错。
 * 用 canvas 做转换是因为它接受浏览器能解析的**任何**颜色语法，
 * 并且 getter 恒定返回 `#rrggbb`；令牌以后从 hex 换成 oklch 也不用改这里。
 */
function normalizeColor(value: string, fallback: string): string {
  const trimmed = value.trim();
  if (!trimmed) return fallback;
  try {
    const context = document.createElement("canvas").getContext("2d");
    if (!context) return fallback;
    context.fillStyle = fallback;
    context.fillStyle = trimmed;
    const resolved = context.fillStyle;
    return typeof resolved === "string" && resolved.startsWith("#") ? resolved : fallback;
  } catch {
    return fallback;
  }
}

function readTokens(names: Record<string, string>): Palette {
  const computed = getComputedStyle(document.documentElement);
  const palette: Palette = {};
  for (const [key, fallback] of Object.entries(names)) {
    palette[key] = normalizeColor(computed.getPropertyValue(`--${key}`), fallback);
  }
  return palette;
}

/** `#rrggbb` + 透明度 → `#rrggbbaa`。Monaco 的颜色表支持八位十六进制。 */
function alpha(color: string, ratio: number): string {
  const clamped = Math.max(0, Math.min(1, ratio));
  const hex = Math.round(clamped * 255)
    .toString(16)
    .padStart(2, "0");
  return `${color}${hex}`;
}

/** Monaco 的 token 规则里颜色**不带 `#`**（它内部会自己拼上去）。 */
function bare(color: string): string {
  return color.replace(/^#/, "");
}

/**
 * 定义并切换到 Aegis 主题。深浅切换后重新调用即可就地更新。
 *
 * 返回主题名，调用方直接把它交给 `<Editor theme=...>`。
 */
export function applyAegisTheme(monaco: Monaco, dark: boolean): string {
  const palette = readTokens({
    background: dark ? "#09090b" : "#f4f4f5",
    foreground: dark ? "#f4f4f5" : "#18181b",
    card: dark ? "#18181b" : "#ffffff",
    popover: dark ? "#18181b" : "#ffffff",
    muted: dark ? "#27272a" : "#f4f4f5",
    "muted-foreground": dark ? "#a1a1aa" : "#71717a",
    accent: dark ? "#27272a" : "#f4f4f5",
    border: dark ? "#27272a" : "#e4e4e7",
    ring: dark ? "#d4d4d8" : "#18181b",
    destructive: dark ? "#ef4444" : "#dc2626",
    "hljs-keyword": dark ? "#ff7b72" : "#8250df",
    "hljs-string": dark ? "#7ee787" : "#0a7c42",
    "hljs-number": dark ? "#79c0ff" : "#0550ae",
    "hljs-comment": dark ? "#8b949e" : "#6e7781",
    "hljs-function": dark ? "#d2a8ff" : "#6639ba",
    "hljs-variable": dark ? "#ffa657" : "#953800",
    "hljs-type": dark ? "#79c0ff" : "#0550ae",
    "hljs-punctuation": dark ? "#a1a1aa" : "#57606a"
  });

  const surface = palette.card;
  const warning = dark ? "#fbbf24" : "#b45309";

  monaco.editor.defineTheme(AEGIS_THEME, {
    base: dark ? "vs-dark" : "vs",
    // inherit 保留内置主题里那几百条我们没覆盖的 token 规则；
    // 从零写一份的代价是某个语言的某类 token 突然变成纯黑。
    inherit: true,
    rules: [
      { token: "", foreground: bare(palette.foreground), background: bare(surface) },
      { token: "comment", foreground: bare(palette["hljs-comment"]), fontStyle: "italic" },
      { token: "keyword", foreground: bare(palette["hljs-keyword"]) },
      { token: "keyword.json", foreground: bare(palette["hljs-keyword"]) },
      { token: "string", foreground: bare(palette["hljs-string"]) },
      { token: "string.key.json", foreground: bare(palette["hljs-variable"]) },
      { token: "string.value.json", foreground: bare(palette["hljs-string"]) },
      { token: "number", foreground: bare(palette["hljs-number"]) },
      { token: "regexp", foreground: bare(palette["hljs-string"]) },
      { token: "type", foreground: bare(palette["hljs-type"]) },
      { token: "type.identifier", foreground: bare(palette["hljs-type"]) },
      { token: "identifier", foreground: bare(palette.foreground) },
      { token: "delimiter", foreground: bare(palette["hljs-punctuation"]) },
      { token: "operator", foreground: bare(palette["hljs-punctuation"]) },
      { token: "tag", foreground: bare(palette["hljs-keyword"]) },
      { token: "attribute.name", foreground: bare(palette["hljs-variable"]) },
      { token: "attribute.value", foreground: bare(palette["hljs-string"]) }
    ],
    colors: {
      "editor.background": surface,
      "editor.foreground": palette.foreground,
      "editorGutter.background": surface,
      "editorWidget.background": palette.popover,
      "editorWidget.border": palette.border,
      "editorWidget.foreground": palette.foreground,

      // 行号：非当前行压到很淡。默认那档在深色下几乎和正文一样亮，
      // 一眼看过去分不清哪些是代码、哪些是编号。
      "editorLineNumber.foreground": alpha(palette["muted-foreground"], 0.55),
      "editorLineNumber.activeForeground": palette.foreground,

      "editorCursor.foreground": palette.foreground,
      "editor.selectionBackground": alpha(palette.ring, dark ? 0.28 : 0.16),
      "editor.inactiveSelectionBackground": alpha(palette.ring, dark ? 0.16 : 0.09),
      "editor.selectionHighlightBackground": alpha(palette.ring, dark ? 0.14 : 0.08),
      "editor.wordHighlightBackground": alpha(palette.ring, 0.12),
      "editor.wordHighlightStrongBackground": alpha(palette.ring, 0.18),
      "editor.findMatchBackground": alpha(warning, 0.35),
      "editor.findMatchHighlightBackground": alpha(warning, 0.2),

      // 当前行只给底色，**不给边框**：默认会画一个上下双线的框，
      // 在扁平设计里那两条线比高亮本身还抢眼。
      "editor.lineHighlightBackground": alpha(palette.muted, dark ? 0.55 : 0.7),
      "editor.lineHighlightBorder": "#00000000",

      "editorIndentGuide.background1": alpha(palette.border, 0.7),
      "editorIndentGuide.activeBackground1": palette.border,
      "editorBracketMatch.background": alpha(palette.ring, 0.16),
      "editorBracketMatch.border": alpha(palette.ring, 0.4),

      // 滚动条按 macOS 的覆盖式来：平时几乎看不见，滑过去才浮出来。
      // useShadows 在编辑器选项里另外关掉 —— 那圈内阴影是 2015 年的审美。
      "scrollbarSlider.background": alpha(palette["muted-foreground"], 0.22),
      "scrollbarSlider.hoverBackground": alpha(palette["muted-foreground"], 0.35),
      "scrollbarSlider.activeBackground": alpha(palette["muted-foreground"], 0.5),
      "editorOverviewRuler.border": "#00000000",
      "editorOverviewRuler.background": surface,

      "editorSuggestWidget.background": palette.popover,
      "editorSuggestWidget.border": palette.border,
      "editorSuggestWidget.foreground": palette.foreground,
      "editorSuggestWidget.selectedBackground": palette.accent,
      "editorSuggestWidget.highlightForeground": palette.ring,
      "editorHoverWidget.background": palette.popover,
      "editorHoverWidget.border": palette.border,

      "editorError.foreground": palette.destructive,
      "editorWarning.foreground": warning,
      "editorInfo.foreground": palette["muted-foreground"],
      "editorCodeLens.foreground": alpha(palette["muted-foreground"], 0.85),
      "editorInlayHint.background": alpha(palette.muted, 0.6),
      "editorInlayHint.foreground": palette["muted-foreground"],
      "editorStickyScroll.background": surface,
      "editorStickyScrollHover.background": palette.accent,

      "editorGhostText.foreground": alpha(palette["muted-foreground"], 0.75),
      "editorLink.activeForeground": palette.ring,
      "editorWhitespace.foreground": alpha(palette["muted-foreground"], 0.3),

      // 差异视图：只给底色不给边框，两侧对照时边框会把版面切碎
      "diffEditor.insertedTextBackground": alpha(dark ? "#22c55e" : "#16a34a", 0.16),
      "diffEditor.removedTextBackground": alpha(palette.destructive, 0.16),
      "diffEditor.border": palette.border,

      // 外层容器自己已经有一圈 border，编辑器再画一圈就是双线
      focusBorder: "#00000000"
    }
  });
  monaco.editor.setTheme(AEGIS_THEME);
  return AEGIS_THEME;
}
