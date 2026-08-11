import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import csharp from "highlight.js/lib/languages/csharp";
import dart from "highlight.js/lib/languages/dart";
import go from "highlight.js/lib/languages/go";
import http from "highlight.js/lib/languages/http";
import java from "highlight.js/lib/languages/java";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import php from "highlight.js/lib/languages/php";
import plaintext from "highlight.js/lib/languages/plaintext";
import python from "highlight.js/lib/languages/python";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";

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
  dart,
  go,
  http,
  java,
  json,
  kotlin,
  php,
  plaintext,
  python,
  ruby,
  rust,
  swift,
  typescript
} as const;

let registered = false;

function ensureRegistered() {
  if (registered) return;
  for (const [name, definition] of Object.entries(LANGUAGES)) {
    hljs.registerLanguage(name, definition);
  }
  // JS 与 TS 共用一套语法，示例里两者都会出现
  hljs.registerAliases(["javascript", "js"], { languageName: "typescript" });
  hljs.registerAliases(["shell", "sh", "curl"], { languageName: "bash" });
  hljs.registerAliases(["text", "txt"], { languageName: "plaintext" });
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

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
