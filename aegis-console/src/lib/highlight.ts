import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import csharp from "highlight.js/lib/languages/csharp";
import css from "highlight.js/lib/languages/css";
import dart from "highlight.js/lib/languages/dart";
import diff from "highlight.js/lib/languages/diff";
import go from "highlight.js/lib/languages/go";
import http from "highlight.js/lib/languages/http";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import markdown from "highlight.js/lib/languages/markdown";
import php from "highlight.js/lib/languages/php";
import plaintext from "highlight.js/lib/languages/plaintext";
import python from "highlight.js/lib/languages/python";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

import { codeLanguage } from "./code-languages";

/**
 * 语法高亮。
 *
 * 用 highlight.js 的 **core + 按需注册**，不是 `highlight.js` 默认入口 ——
 * 后者会把 190+ 种语法全打进包里（~900KB）。这里只注册 code-languages.tsx
 * 登记过的那些，公开门户的首屏因此不会被文档代码块拖垮。
 *
 * 配色不走 highlight.js 自带的主题 CSS：那些主题写死了颜色值，
 * 深浅色切换会失效。token 的颜色统一定义在 globals.css 的 `.hljs-*` 规则里，
 * 全部取自主题变量。
 */
const LANGUAGES = {
  bash,
  csharp,
  css,
  dart,
  diff,
  go,
  http,
  ini,
  java,
  json,
  kotlin,
  markdown,
  php,
  plaintext,
  python,
  ruby,
  rust,
  sql,
  swift,
  typescript,
  xml,
  yaml
} as const;

let registered = false;

function ensureRegistered() {
  if (registered) return;
  for (const [name, definition] of Object.entries(LANGUAGES)) {
    hljs.registerLanguage(name, definition);
  }
  // JS 与 TS 共用一套语法，示例里两者都会出现
  hljs.registerAliases(["javascript", "js", "jsx", "tsx", "mjs", "cjs"], { languageName: "typescript" });
  hljs.registerAliases(["shell", "sh", "zsh", "console", "curl"], { languageName: "bash" });
  hljs.registerAliases(["text", "txt"], { languageName: "plaintext" });
  hljs.registerAliases(["html", "svg", "vue"], { languageName: "xml" });
  hljs.registerAliases(["yml"], { languageName: "yaml" });
  hljs.registerAliases(["toml"], { languageName: "ini" });
  hljs.registerAliases(["md", "mdx"], { languageName: "markdown" });
  hljs.registerAliases(["patch"], { languageName: "diff" });
  registered = true;
}

/**
 * 把源码高亮成 HTML。
 *
 * highlight.js 会转义输入，返回值可以安全地交给 dangerouslySetInnerHTML；
 * 但为了不依赖这一点，语法不认识时走 escapeHtml 的纯文本分支。
 */
export function highlightCode(code: string, languageId: string): string {
  ensureRegistered();
  const { hljs: grammar } = codeLanguage(languageId);
  if (!hljs.getLanguage(grammar)) {
    return escapeHtml(code);
  }
  try {
    return hljs.highlight(code, { language: grammar, ignoreIllegals: true }).value;
  } catch {
    // 语法定义抛错时降级为纯文本，绝不把未转义内容送进 DOM
    return escapeHtml(code);
  }
}

/**
 * Markdown 围栏代码块的高亮入口。
 *
 * 与 highlightCode 的差别：围栏语言串（```lang）来自模型输出，大小写与
 * 别名都不受控，不适合走 code-languages 的白名单映射（那张表把未知语言
 * 一律折叠成 plaintext，`javascript`/`yaml` 这类常见围栏就全灰了）。
 * 这里直接查 hljs 的注册表（含上面登记的别名），认识就着色，不认识才转义。
 */
export function highlightFencedCode(code: string, language?: string): string {
  ensureRegistered();
  const lang = (language ?? "").trim().toLowerCase();
  if (!lang || !hljs.getLanguage(lang)) {
    return escapeHtml(code);
  }
  try {
    return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return escapeHtml(code);
  }
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
