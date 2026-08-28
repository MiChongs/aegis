"use client";

import { isValidElement, memo, useMemo, useState, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import { Check, Clipboard } from "lucide-react";
import { cn } from "@/lib/utils";
import { highlightFencedCode } from "@/lib/highlight";
import "katex/dist/katex.min.css";

/**
 * AI 聊天消息的 Markdown 渲染器。
 *
 * 完整覆盖模型常用的排版能力：GFM（表格 / 任务列表 / 删除线 / 自动链接）、
 * 围栏代码块（主题化语法高亮 + 一键复制）、行内代码、KaTeX 数学公式、
 * 引用块、多级标题、分隔线与图片。
 *
 * 安全边界：不启用 rehype-raw —— 模型输出里的原始 HTML 一律按文本转义，
 * 代码高亮走 highlight.js（自带转义），数学走 KaTeX（不解释 HTML）。
 * 链接统一在新窗口打开并加 noopener，防止聊天内容劫持控制台窗口。
 *
 * 流式友好：组件按 memo(text) 缓存，父级用 useChat 的 throttle 限频后，
 * 每次重解析的只有正在增长的那一条消息。
 */

/**
 * 数学定界符归一化。
 *
 * remark-math 只认 `$…$` / `$$…$$`，而 OpenAI 系模型习惯输出 `\(…\)` 与
 * `\[…\]`。直接全文替换会误伤代码 —— 围栏块与行内代码里的 `\(` 是字面量
 * （正则、LaTeX 源码示例）。这里先按围栏切段、再按行内反引号切段，
 * 只在纯文本片段上做替换。
 */
function normalizeMathDelimiters(text: string): string {
  if (!text.includes("\\(") && !text.includes("\\[")) return text;
  // 奇数下标是围栏代码段（含未闭合的流式尾段），保持原样。
  return text
    .split(/(```[\s\S]*?(?:```|$))/)
    .map((segment, index) => {
      if (index % 2 === 1) return segment;
      // 奇数下标是行内代码段，保持原样。
      return segment
        .split(/(`[^`\n]*`)/)
        .map((piece, pieceIndex) => {
          if (pieceIndex % 2 === 1) return piece;
          return piece
            .replace(/\\\[([\s\S]*?)\\\]/g, (_, expr: string) => `$$${expr}$$`)
            .replace(/\\\(([\s\S]*?)\\\)/g, (_, expr: string) => `$${expr}$`);
        })
        .join("");
    })
    .join("");
}

/** 把 React children 拍平成纯文本（围栏代码的内容在 AST 里就是字符串）。 */
function childrenToText(children: ReactNode): string {
  if (typeof children === "string") return children;
  if (typeof children === "number") return String(children);
  if (Array.isArray(children)) return children.map(childrenToText).join("");
  if (isValidElement(children)) {
    return childrenToText((children.props as { children?: ReactNode }).children);
  }
  return "";
}

/** 聊天里的围栏代码块：语言标签 + 复制按钮 + 主题化高亮，超高内部滚动。 */
const FencedCodeBlock = memo(function FencedCodeBlock({
  code,
  language
}: {
  code: string;
  language: string;
}) {
  const [copied, setCopied] = useState(false);
  const html = useMemo(() => highlightFencedCode(code, language), [code, language]);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // 剪贴板不可用（非安全上下文）时静默失败，用户仍可手动选中复制
    }
  }

  return (
    <div className="group/code my-2 overflow-hidden rounded-lg border bg-muted/30">
      <div className="flex h-7 items-center justify-between border-b bg-muted/50 pl-2.5 pr-1">
        <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
          {language || "text"}
        </span>
        <button
          type="button"
          onClick={() => void handleCopy()}
          className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
          aria-label="复制代码"
        >
          {copied ? <Check className="size-3 text-emerald-500" /> : <Clipboard className="size-3" />}
          {copied ? "已复制" : "复制"}
        </button>
      </div>
      <pre className="max-h-[420px] overflow-auto p-2.5 font-mono text-[0.86em] leading-relaxed">
        {/* highlight.js 已对输入做过 HTML 转义，未知语言走 escapeHtml 分支 */}
        <code className="hljs bg-transparent" dangerouslySetInnerHTML={{ __html: html }} />
      </pre>
    </div>
  );
});

/**
 * 元素映射。字号全部用 em：同一份映射在停靠（text-xs）与全屏（text-sm）
 * 两种基准字号下都成比例，不用维护两套样式。
 */
const components: Components = {
  // 围栏代码在 AST 里是 pre > code(.language-x)：在 pre 这一层整体接管，
  // 行内代码走不到这里（没有 pre 包裹），天然区分了两种形态。
  pre({ children }) {
    const child = Array.isArray(children) ? children[0] : children;
    if (isValidElement(child)) {
      const props = child.props as { className?: string; children?: ReactNode };
      const language = /language-([\w+#.-]+)/.exec(props.className ?? "")?.[1] ?? "";
      const code = childrenToText(props.children).replace(/\n$/, "");
      return <FencedCodeBlock code={code} language={language.toLowerCase()} />;
    }
    return <pre className="my-2 overflow-x-auto">{children}</pre>;
  },
  code({ children }) {
    return (
      <code className="rounded border bg-muted/60 px-1 py-px font-mono text-[0.86em]">
        {children}
      </code>
    );
  },
  p({ children }) {
    return <p className="my-1.5 break-words leading-relaxed first:mt-0 last:mb-0">{children}</p>;
  },
  a({ href, children }) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noreferrer noopener"
        className="font-medium text-primary underline decoration-primary/40 underline-offset-2 hover:decoration-primary"
      >
        {children}
      </a>
    );
  },
  ul({ children }) {
    return <ul className="my-1.5 list-disc space-y-1 pl-5 marker:text-muted-foreground">{children}</ul>;
  },
  ol({ children }) {
    return (
      <ol className="my-1.5 list-decimal space-y-1 pl-5 marker:text-muted-foreground">{children}</ol>
    );
  },
  li({ children }) {
    return <li className="break-words leading-relaxed [&>p]:my-0.5">{children}</li>;
  },
  // GFM 任务列表的复选框：只读展示，禁用交互
  input({ type, checked, disabled }) {
    if (type !== "checkbox") return null;
    return (
      <input
        type="checkbox"
        checked={Boolean(checked)}
        disabled={Boolean(disabled)}
        readOnly
        className="mr-1.5 size-3 translate-y-px accent-primary"
      />
    );
  },
  blockquote({ children }) {
    return (
      <blockquote className="my-2 border-l-2 border-primary/30 pl-3 text-muted-foreground [&>p]:my-1">
        {children}
      </blockquote>
    );
  },
  h1({ children }) {
    return <h1 className="mb-1.5 mt-3 text-[1.25em] font-semibold first:mt-0">{children}</h1>;
  },
  h2({ children }) {
    return <h2 className="mb-1.5 mt-3 text-[1.15em] font-semibold first:mt-0">{children}</h2>;
  },
  h3({ children }) {
    return <h3 className="mb-1 mt-2.5 text-[1.08em] font-semibold first:mt-0">{children}</h3>;
  },
  h4({ children }) {
    return <h4 className="mb-1 mt-2.5 text-[1.02em] font-semibold first:mt-0">{children}</h4>;
  },
  h5({ children }) {
    return <h5 className="mb-1 mt-2 font-semibold first:mt-0">{children}</h5>;
  },
  h6({ children }) {
    return <h6 className="mb-1 mt-2 font-semibold text-muted-foreground first:mt-0">{children}</h6>;
  },
  table({ children }) {
    return (
      <div className="my-2 w-full overflow-x-auto rounded-lg border">
        <table className="w-full border-collapse text-[0.93em]">{children}</table>
      </div>
    );
  },
  thead({ children }) {
    return <thead className="bg-muted/50">{children}</thead>;
  },
  th({ children }) {
    return (
      <th className="border-b border-r px-2.5 py-1.5 text-left font-medium last:border-r-0">
        {children}
      </th>
    );
  },
  td({ children }) {
    return (
      <td className="border-b border-r px-2.5 py-1.5 align-top last:border-r-0 [tr:last-child>&]:border-b-0">
        {children}
      </td>
    );
  },
  hr() {
    return <hr className="my-3 border-border" />;
  },
  img({ src, alt }) {
    if (typeof src !== "string" || !src) return null;
    // 聊天里的图片是模型引用的外部资源，不走 next/image 的优化管线
    // eslint-disable-next-line @next/next/no-img-element
    return <img src={src} alt={alt ?? ""} loading="lazy" className="my-2 max-w-full rounded-lg border" />;
  }
};

const remarkPlugins = [remarkGfm, remarkMath];
const rehypePlugins = [rehypeKatex];

export const Markdown = memo(function Markdown({
  text,
  className
}: {
  text: string;
  className?: string;
}) {
  const normalized = useMemo(() => normalizeMathDelimiters(text), [text]);
  return (
    <div className={cn("min-w-0 break-words [&_.katex-display]:my-2 [&_.katex-display]:overflow-x-auto", className)}>
      <ReactMarkdown remarkPlugins={remarkPlugins} rehypePlugins={rehypePlugins} components={components}>
        {normalized}
      </ReactMarkdown>
    </div>
  );
});
