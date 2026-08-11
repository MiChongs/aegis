"use client";

import { useMemo, useState } from "react";
import { Check, Clipboard } from "lucide-react";
import { cn } from "@/lib/utils";
import { CodeLanguageIcon, codeLanguage } from "@/lib/code-languages";
import { highlightCode } from "@/lib/highlight";

type CodeBlockProps = {
  code: string;
  /** 语言 id，见 src/lib/code-languages.tsx（typescript / go / curl / …） */
  language?: string;
  /** 代码块上方的说明标题；省略时显示语言名 */
  title?: string;
  className?: string;
  /** 超过该高度出现内部滚动，默认不限制 */
  maxHeight?: number;
};

/**
 * 文档用代码块：品牌图标 + 语法高亮 + 一键复制，横向溢出自行滚动。
 *
 * 高亮走 highlight.js 的 core + 按需注册（见 lib/highlight.ts），
 * token 颜色取自 globals.css 里的主题变量，深浅色都可读。
 */
export function CodeBlock({ code, language = "text", title, className, maxHeight }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const meta = codeLanguage(language);
  // 代码是静态字符串，只在语言或内容变化时重新着色
  const html = useMemo(() => highlightCode(code, language), [code, language]);

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
    <figure className={cn("overflow-hidden rounded-xl border bg-muted/30", className)}>
      <figcaption className="flex items-center justify-between gap-3 border-b bg-muted/50 px-3 py-2">
        <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <CodeLanguageIcon id={language} />
          <span className="truncate">{title || meta.label}</span>
        </span>
        <div className="flex shrink-0 items-center gap-2">
          {title ? (
            <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70">
              {meta.label}
            </span>
          ) : null}
          <button
            type="button"
            onClick={() => void handleCopy()}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
            aria-label="复制代码"
          >
            {copied ? <Check className="size-3.5 text-emerald-500" /> : <Clipboard className="size-3.5" />}
            {copied ? "已复制" : "复制"}
          </button>
        </div>
      </figcaption>
      <pre
        className="overflow-x-auto p-3 font-mono text-[12.5px] leading-relaxed"
        style={maxHeight ? { maxHeight, overflowY: "auto" } : undefined}
      >
        {/* highlight.js 已对输入做过 HTML 转义，失败时也会走 escapeHtml 分支 */}
        <code className="hljs bg-transparent" dangerouslySetInnerHTML={{ __html: html }} />
      </pre>
    </figure>
  );
}

/** 行内可复制的等宽值（AppKey、Key ID、端点路径等） */
export function InlineCode({ children, copyable = true }: { children: string; copyable?: boolean }) {
  const [copied, setCopied] = useState(false);

  if (!copyable) {
    return (
      <code className="rounded border bg-muted/60 px-1.5 py-0.5 font-mono text-[12px]">{children}</code>
    );
  }

  return (
    <button
      type="button"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(children);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1600);
        } catch {
          // 同上：静默降级
        }
      }}
      className="inline-flex max-w-full items-center gap-1.5 rounded border bg-muted/60 px-1.5 py-0.5 text-left font-mono text-[12px] transition-colors hover:bg-muted"
      title="点击复制"
    >
      <span className="truncate">{children}</span>
      {copied ? (
        <Check className="size-3 shrink-0 text-emerald-500" />
      ) : (
        <Clipboard className="size-3 shrink-0 text-muted-foreground/60" />
      )}
    </button>
  );
}
