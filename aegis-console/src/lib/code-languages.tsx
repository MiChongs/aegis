import type { ComponentType } from "react";
import {
  SiCurl,
  SiDart,
  SiGnubash,
  SiGo,
  SiJson,
  SiKotlin,
  SiOpenjdk,
  SiPhp,
  SiPython,
  SiRuby,
  SiRust,
  SiSharp,
  SiSwift,
  SiTypescript
} from "@icons-pack/react-simple-icons";
import { FileCode2, Globe } from "lucide-react";

/**
 * 代码语言登记表。
 *
 * 一处登记，三处生效：Tab 上的品牌图标、代码块的语法高亮、右上角的语言名。
 * 新增语言只改这个文件 —— 示例生成器只写 `lang: "python"` 之类的 id。
 *
 * `hljs` 是 highlight.js 的语法 id；`src/lib/highlight.ts` 按这里的清单
 * 按需注册语法，没登记的语言不会被打进包里。
 */
export type CodeLanguageId =
  | "bash"
  | "curl"
  | "typescript"
  | "python"
  | "go"
  | "java"
  | "kotlin"
  | "swift"
  | "php"
  | "csharp"
  | "ruby"
  | "rust"
  | "dart"
  | "json"
  | "http"
  | "text";

type IconComponent = ComponentType<{ className?: string; size?: number; color?: string }>;

export type CodeLanguage = {
  /** Tab 与代码块右上角显示的名字 */
  label: string;
  /** highlight.js 语法 id */
  hljs: string;
  icon: IconComponent;
  /**
   * 品牌色。留空表示该语言的官方色接近纯黑/纯白，
   * 在深浅两色主题下都不可读，改用 currentColor。
   */
  color?: string;
};

export const CODE_LANGUAGES: Record<CodeLanguageId, CodeLanguage> = {
  curl: { label: "cURL", hljs: "bash", icon: SiCurl, color: "#073551" },
  bash: { label: "Shell", hljs: "bash", icon: SiGnubash, color: "#4EAA25" },
  typescript: { label: "TypeScript", hljs: "typescript", icon: SiTypescript, color: "#3178C6" },
  python: { label: "Python", hljs: "python", icon: SiPython, color: "#3776AB" },
  go: { label: "Go", hljs: "go", icon: SiGo, color: "#00ADD8" },
  java: { label: "Java", hljs: "java", icon: SiOpenjdk, color: "#437291" },
  kotlin: { label: "Kotlin", hljs: "kotlin", icon: SiKotlin, color: "#7F52FF" },
  swift: { label: "Swift", hljs: "swift", icon: SiSwift, color: "#F05138" },
  php: { label: "PHP", hljs: "php", icon: SiPhp, color: "#777BB4" },
  csharp: { label: "C#", hljs: "csharp", icon: SiSharp, color: "#512BD4" },
  ruby: { label: "Ruby", hljs: "ruby", icon: SiRuby, color: "#CC342D" },
  rust: { label: "Rust", hljs: "rust", icon: SiRust },
  dart: { label: "Dart", hljs: "dart", icon: SiDart, color: "#0175C2" },
  json: { label: "JSON", hljs: "json", icon: SiJson },
  http: { label: "HTTP", hljs: "http", icon: Globe as IconComponent },
  text: { label: "说明", hljs: "plaintext", icon: FileCode2 as IconComponent }
};

export function codeLanguage(id: string): CodeLanguage {
  return CODE_LANGUAGES[id as CodeLanguageId] ?? CODE_LANGUAGES.text;
}

/** 语言图标。品牌色留空时跟随文字颜色，避免纯黑图标在深色主题下消失。 */
export function CodeLanguageIcon({
  id,
  className = "size-3.5"
}: {
  id: string;
  className?: string;
}) {
  const { icon: Icon, color } = codeLanguage(id);
  return <Icon className={className} color={color ?? "currentColor"} />;
}
