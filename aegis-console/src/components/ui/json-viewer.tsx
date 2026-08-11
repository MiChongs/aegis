"use client";

import { useCallback, useEffect, useId, useMemo, useState } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import { Dialog as DialogPrimitive } from "radix-ui";
import type { Monaco } from "@monaco-editor/react";
import { Check, Copy, Maximize2, Minimize2, Wand2 } from "lucide-react";
import { AnimatePresence, LazyMotion, domAnimation, m } from "motion/react";
import { toast } from "sonner";
import { loadMonaco } from "@/lib/monaco/loader";
import { cn } from "@/lib/utils";

// 注意：这里刻意**不做**模块作用域的预热。
//
// RoutePrefetcher 会在首屏空闲时预取全部侧边栏路由，连带执行这些路由的模块体。
// 一旦在模块体里触发 Monaco 加载，随便打开哪个页面（哪怕是登录页）都会拉起
// 整个 Monaco，而且是在自托管配置生效之前 —— 于是回落到公网 CDN。
// 改为在组件真正要渲染编辑器时调用 loadMonaco()，见 @/lib/monaco/loader。

function JsonViewerSkeleton() {
  return (
    <div className="h-[240px] animate-pulse rounded-lg border bg-muted/40" aria-hidden="true" />
  );
}

// Monaco 按需加载（SSR 不可用 + bundle 较大）
const MonacoEditor = dynamic(
  () => import("@monaco-editor/react").then((m) => m.default),
  { ssr: false, loading: () => <JsonViewerSkeleton /> },
);

// ─────────────────────────────────────────────────────────────────────
//  JsonViewer — 项目统一的 JSON 只读查看器（基于 monaco-editor）。
//
//  设计目标：
//   - 颜色 / 字体 / 圆角 / 边框 与 shadcn + Radix 的卡片风格对齐；
//   - 浅色 / 深色两套 Monaco 自定义主题（Aegis Light / Aegis Dark），
//     背景直接读取项目 CSS 变量（--muted / --card 等），保证切主题瞬间同步；
//   - 顶栏：lang 标签、行数 · 大小、格式化、全屏、复制 —— 行为全部保留；
//   - 内容 auto-wrap + 滚动；失败（非合法 JSON）回退为纯文本视图。
// ─────────────────────────────────────────────────────────────────────

export type JsonViewerProps = {
  value: string;
  language?: "json" | "text";
  /** 默认收起/展开行为的默认高度（px） */
  height?: number;
  /** 是否支持全屏切换 */
  allowFullscreen?: boolean;
  className?: string;
};

// Aegis 自定义主题：颜色全部映射到 shadcn/Radix 调色板
function defineAegisThemes(monaco: Monaco) {
  monaco.editor.defineTheme("aegis-light", {
    base: "vs",
    inherit: true,
    rules: [
      { token: "", foreground: "18181b" },
      { token: "string.key.json", foreground: "0369a1", fontStyle: "" },
      { token: "string.value.json", foreground: "15803d" },
      { token: "number", foreground: "b45309" },
      { token: "keyword.json", foreground: "9333ea" },
      { token: "delimiter", foreground: "52525b" },
      { token: "comment", foreground: "71717a", fontStyle: "italic" },
    ],
    colors: {
      "editor.background": "#f8f8f7",
      "editor.foreground": "#18181b",
      "editor.lineHighlightBackground": "#00000008",
      "editor.lineHighlightBorder": "#00000000",
      "editorLineNumber.foreground": "#a1a1aa",
      "editorLineNumber.activeForeground": "#52525b",
      "editorCursor.foreground": "#c96442",
      "editor.selectionBackground": "#c9644233",
      "editor.inactiveSelectionBackground": "#c9644222",
      "editor.findMatchHighlightBackground": "#f59e0b55",
      "editorIndentGuide.background": "#e4e4e780",
      "editorIndentGuide.activeBackground": "#d4d4d8",
      "editorWhitespace.foreground": "#e4e4e7",
      "editorBracketMatch.background": "#c9644222",
      "editorBracketMatch.border": "#c96442",
      "scrollbarSlider.background": "#71717a33",
      "scrollbarSlider.hoverBackground": "#71717a55",
      "scrollbarSlider.activeBackground": "#71717a77",
      "editorOverviewRuler.border": "#00000000",
    },
  });
  monaco.editor.defineTheme("aegis-dark", {
    base: "vs-dark",
    inherit: true,
    rules: [
      { token: "", foreground: "f4f4f5" },
      { token: "string.key.json", foreground: "7dd3fc", fontStyle: "" },
      { token: "string.value.json", foreground: "86efac" },
      { token: "number", foreground: "fcd34d" },
      { token: "keyword.json", foreground: "c4b5fd" },
      { token: "delimiter", foreground: "a1a1aa" },
      { token: "comment", foreground: "71717a", fontStyle: "italic" },
    ],
    colors: {
      "editor.background": "#141416",
      "editor.foreground": "#f4f4f5",
      "editor.lineHighlightBackground": "#ffffff08",
      "editor.lineHighlightBorder": "#00000000",
      "editorLineNumber.foreground": "#52525b",
      "editorLineNumber.activeForeground": "#a1a1aa",
      "editorCursor.foreground": "#d97757",
      "editor.selectionBackground": "#d9775733",
      "editor.inactiveSelectionBackground": "#d9775722",
      "editor.findMatchHighlightBackground": "#f59e0b44",
      "editorIndentGuide.background": "#27272a",
      "editorIndentGuide.activeBackground": "#3f3f46",
      "editorWhitespace.foreground": "#27272a",
      "editorBracketMatch.background": "#d9775722",
      "editorBracketMatch.border": "#d97757",
      "scrollbarSlider.background": "#52525b33",
      "scrollbarSlider.hoverBackground": "#52525b66",
      "scrollbarSlider.activeBackground": "#52525b88",
      "editorOverviewRuler.border": "#00000000",
    },
  });
}

export function JsonViewer({
  value,
  language = "json",
  height = 240,
  allowFullscreen = true,
  className,
}: JsonViewerProps) {
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";
  const [fullscreen, setFullscreen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [editorReady, setEditorReady] = useState(false);
  const [loaderTimeout, setLoaderTimeout] = useState(false);
  const [mounted, setMounted] = useState(false);

  const [monacoReady, setMonacoReady] = useState(false);

  useEffect(() => {
    // microtask 延迟 setState，避免 react-hooks/purity 规则告警
    queueMicrotask(() => setMounted(true));
  }, []);

  // 组件真正要用编辑器时才加载自托管 Monaco；就绪前保持骨架/文本回退
  useEffect(() => {
    let alive = true;
    loadMonaco().then(
      () => alive && setMonacoReady(true),
      () => alive && setLoaderTimeout(true)
    );
    return () => {
      alive = false;
    };
  }, []);

  // Monaco 加载 6s 仍未就绪则回退到 <pre> 文本视图，避免永远卡 "Loading..."
  useEffect(() => {
    if (editorReady) return;
    const timer = setTimeout(() => setLoaderTimeout(true), 6000);
    return () => clearTimeout(timer);
  }, [editorReady]);

  // body 滚动锁 + Esc 关闭均由 Radix Dialog 内置处理，这里不再重复绑定。

  // 尝试格式化：合法 JSON → 美化缩进；否则保持原样。
  const { displayValue, lang, formatted } = useMemo(() => {
    const raw = (value ?? "").toString();
    const trimmed = raw.trim();
    if (language === "json" && (trimmed.startsWith("{") || trimmed.startsWith("["))) {
      try {
        const parsed = JSON.parse(trimmed);
        return { displayValue: JSON.stringify(parsed, null, 2), lang: "json", formatted: true };
      } catch {
        return { displayValue: raw, lang: "plaintext", formatted: false };
      }
    }
    return { displayValue: raw, lang: language, formatted: false };
  }, [value, language]);

  const lineCount = displayValue ? displayValue.split("\n").length : 0;
  const byteCount = displayValue.length;
  const sizeLabel =
    byteCount > 1024 ? `${(byteCount / 1024).toFixed(1)} KB` : `${byteCount} B`;

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(displayValue);
      setCopied(true);
      toast.success("已复制");
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      toast.error("复制失败");
    }
  }, [displayValue]);

  // 用 useId 生成稳定的本组件实例 key（portal 里多 overlay 共存时用）
  const layoutId = `json-viewer-${useId()}`;

  const editorOptions = useMemo(
    () => ({
      readOnly: true,
      domReadOnly: true,
      minimap: { enabled: false },
      fontFamily:
        'var(--font-mono), "Maple Mono NF CN", "JetBrains Mono", "Cascadia Code", monospace',
      fontSize: 12,
      lineHeight: 20,
      lineNumbers: "on" as const,
      lineNumbersMinChars: 3,
      glyphMargin: false,
      folding: true,
      foldingHighlight: false,
      renderLineHighlight: "line" as const,
      smoothScrolling: true,
      // ── macOS 风格光标：柔和呼吸 + 平滑过渡 + 略粗竖线 ──
      cursorBlinking: "expand" as const,              // 呼吸式淡入淡出，接近 macOS 原生终端
      cursorSmoothCaretAnimation: "on" as const,      // 位移/切换时平滑过渡，非瞬移
      cursorStyle: "line" as const,
      cursorWidth: 2,
      scrollBeyondLastLine: false,
      automaticLayout: true,
      padding: { top: 10, bottom: 10 },
      wordWrap: "on" as const,
      bracketPairColorization: { enabled: true },
      guides: { indentation: true, highlightActiveIndentation: true, bracketPairs: true },
      contextmenu: false,
      stickyScroll: { enabled: false },
      overviewRulerLanes: 0,
      renderWhitespace: "none" as const,
      occurrencesHighlight: "off" as const,
      selectionHighlight: false,
      scrollbar: {
        verticalScrollbarSize: 8,
        horizontalScrollbarSize: 8,
        alwaysConsumeMouseWheel: false,
      },
    }),
    [],
  );

  // 抽出编辑器区：inline 与 fullscreen 各挂一份，尺寸由各自容器控制，
  // 不走 motion layout/transform，避免 Monaco 看不到真实宽高变化
  const renderEditorArea = () => (
    <div className="relative flex-1 min-h-0">
      {(loaderTimeout && !editorReady) || !monacoReady ? (
        <pre
          className="absolute inset-0 overflow-auto px-3 py-2.5 text-[11px] font-mono leading-5"
          style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", overflowWrap: "anywhere" }}
        >
          {displayValue || "--"}
        </pre>
      ) : (
        <MonacoEditor
          value={displayValue || ""}
          language={lang}
          height="100%"
          beforeMount={defineAegisThemes}
          onMount={() => setEditorReady(true)}
          theme={isDark ? "aegis-dark" : "aegis-light"}
          options={editorOptions}
        />
      )}
    </div>
  );

  const renderToolbar = (variant: "inline" | "fullscreen") => (
    <div className="flex shrink-0 items-center justify-between gap-2 border-b bg-muted/30 px-3 py-1.5">
      <div className="flex items-center gap-2 text-[10px] font-mono uppercase tracking-widest text-muted-foreground">
        <span className="inline-flex items-center gap-1">
          <Wand2 className={cn("size-3", formatted ? "text-foreground" : "opacity-40")} />
          {lang}
        </span>
        <span className="text-border">·</span>
        <span>{lineCount} 行</span>
        <span className="text-border">·</span>
        <span>{sizeLabel}</span>
      </div>
      <div className="flex items-center gap-1">
        {allowFullscreen ? (
          <button
            type="button"
            onClick={() => setFullscreen((v) => !v)}
            className="inline-flex items-center gap-1 rounded-md border bg-background px-1.5 py-0.5 text-[10px] text-muted-foreground transition-colors hover:text-foreground"
            aria-label={variant === "fullscreen" ? "退出全屏" : "全屏查看"}
            title={variant === "fullscreen" ? "退出全屏 (Esc)" : "全屏查看"}
          >
            {variant === "fullscreen" ? <Minimize2 className="size-3" /> : <Maximize2 className="size-3" />}
          </button>
        ) : null}
        <button
          type="button"
          onClick={handleCopy}
          className="inline-flex items-center gap-1 rounded-md border bg-background px-1.5 py-0.5 text-[10px] text-muted-foreground transition-colors hover:text-foreground"
          aria-label="复制内容"
        >
          <AnimatePresence mode="wait" initial={false}>
            <m.span
              key={copied ? "ok" : "copy"}
              initial={{ opacity: 0, scale: 0.6 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.6 }}
              transition={{ duration: 0.15 }}
              className="inline-flex"
            >
              {copied ? (
                <Check className="size-3 text-emerald-600 dark:text-emerald-400" />
              ) : (
                <Copy className="size-3" />
              )}
            </m.span>
          </AnimatePresence>
          <span>{copied ? "已复制" : "复制"}</span>
        </button>
      </div>
    </div>
  );

  // ─────────────────────────────────────────────────────────────
  //  渲染策略（修复原 FLIP 方案把 Monaco DOM 尺寸维持在 inline，只把
  //  容器 transform 放大导致全屏内容模糊且贴在左上角的问题）：
  //
  //   1) inline 版永远渲染一个 Monaco 实例（原地显示）；
  //   2) 全屏时额外挂一个 portal 覆盖层，里面再启一个 Monaco 实例，
  //      尺寸由 flex 布局决定，automaticLayout 会计算真实宽高；
  //   3) 覆盖层 / 遮罩使用 AnimatePresence 做 scale+opacity 进出场，
  //      带 macOS 风格 ease-out-back 弹性，轻盈不喧宾夺主。
  //   4) 全屏时隐藏 inline 版（visibility:hidden 保留占位）。
  // ─────────────────────────────────────────────────────────────

  // ─────────────────────────────────────────────────────────────
  //  全屏使用 Radix Dialog 嵌套。
  //  之前用 createPortal 自建 overlay，当宿主（审计页）已经在 Radix Sheet
  //  中打开时，Radix 会把所有 "非 Sheet 兄弟节点" 设置 pointer-events:none，
  //  导致我们的 portal overlay 点不动。Radix Dialog 原生支持多层嵌套，
  //  新层会接管 pointer 与 focus，不会被外层 Sheet 屏蔽。
  // ─────────────────────────────────────────────────────────────
  const fullscreenOverlay = mounted && allowFullscreen ? (
    <DialogPrimitive.Root
      open={fullscreen}
      onOpenChange={(open) => setFullscreen(open)}
      modal
    >
      <DialogPrimitive.Portal>
        <LazyMotion features={domAnimation}>
          <AnimatePresence>
            {fullscreen ? (
              <DialogPrimitive.Overlay
                key={`${layoutId}-backdrop`}
                asChild
                forceMount
              >
                <m.div
                  initial={{ opacity: 0, backdropFilter: "blur(0px)" }}
                  animate={{ opacity: 1, backdropFilter: "blur(6px)" }}
                  exit={{ opacity: 0, backdropFilter: "blur(0px)" }}
                  transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
                  className="fixed inset-0 z-[300] bg-black/55 dark:bg-black/70"
                />
              </DialogPrimitive.Overlay>
            ) : null}
            {fullscreen ? (
              <DialogPrimitive.Content
                key={`${layoutId}-shell`}
                asChild
                forceMount
                onOpenAutoFocus={(e) => e.preventDefault()}
              >
                <m.div
                  initial={{ opacity: 0, scale: 0.96, y: 8 }}
                  animate={{ opacity: 1, scale: 1, y: 0 }}
                  exit={{ opacity: 0, scale: 0.97, y: 4 }}
                  transition={{ duration: 0.28, ease: [0.22, 1, 0.36, 1] }}
                  className="fixed inset-4 md:inset-8 z-[301] flex flex-col rounded-xl border bg-background shadow-2xl overflow-hidden"
                >
                  <DialogPrimitive.Title className="sr-only">JSON 全屏查看</DialogPrimitive.Title>
                  <DialogPrimitive.Description className="sr-only">
                    只读 JSON 编辑器全屏视图，按 Esc 或点击空白退出。
                  </DialogPrimitive.Description>
                  {renderToolbar("fullscreen")}
                  {renderEditorArea()}
                </m.div>
              </DialogPrimitive.Content>
            ) : null}
          </AnimatePresence>
        </LazyMotion>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  ) : null;

  return (
    <LazyMotion features={domAnimation}>
      <div
        className={cn(
          "relative flex flex-col border bg-muted/40 overflow-hidden rounded-lg",
          className,
        )}
        style={{
          height: `${height}px`,
          visibility: fullscreen ? "hidden" : "visible",
        }}
      >
        {renderToolbar("inline")}
        {renderEditorArea()}
      </div>
      {fullscreenOverlay}
    </LazyMotion>
  );
}
