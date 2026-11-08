"use client";

import { useEffect, useId, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import type { Monaco } from "@monaco-editor/react";
import type { editor, IDisposable, Position } from "monaco-editor";

import { Skeleton } from "@/components/ui/skeleton";
import { useFunctionWorkbenchStore } from "@/lib/function-workbench-store";
import { AEGIS_THEME, applyAegisTheme } from "@/lib/monaco/aegis-theme";
import { useEditorMotion } from "@/lib/monaco/editor-motion";
import { loadMonaco } from "@/lib/monaco/loader";
import { cn } from "@/lib/utils";

/**
 * 风控平台的代码编辑器：规则条件与派生特征（expr）、场景策略（Rego）、流式特征 SQL（只读）。
 *
 * 加载、主题、动效与 functions/json-editor.tsx 同一套（自托管 Monaco，等 loadMonaco 再渲染）。
 * 不复用 ScriptEditor：它把 TS 语言服务与 Aegis SDK 类型绑在模型上，Rego / expr 进去会满屏红线。
 *
 * 语言 id 用 `risk-expr` 而不是 `expr`：插件页已经注册了 `expr` 并挂着流量插件的补全，
 * Monaco 的补全提供器按语言全局叠加，同名会把 allow() / deny() 也补进风控条件里。
 */

const MonacoEditor = dynamic(() => import("@monaco-editor/react").then((mod) => mod.Editor), {
  ssr: false,
  loading: () => <Skeleton className="h-full min-h-16 w-full rounded-lg" />,
});

const MonacoDiffEditor = dynamic(() => import("@monaco-editor/react").then((mod) => mod.DiffEditor), {
  ssr: false,
  loading: () => <Skeleton className="h-full min-h-32 w-full rounded-lg" />,
});

export type CodeLanguage = "risk-expr" | "rego" | "sql" | "json";

export interface CompletionEntry {
  label: string;
  detail?: string;
  kind?: "variable" | "feature" | "model" | "function" | "keyword";
}

/** 补全目录：最近一个挂载的 expr 编辑器给的那份（提供器全局只注册一次） */
let exprCompletions: CompletionEntry[] = [];
let exprRegistration: IDisposable | null = null;
let languagesReady = false;

const EXPR_KEYWORDS = ["and", "or", "not", "in", "matches", "contains", "startsWith", "endsWith", "true", "false", "nil"];
const EXPR_FUNCS = ["len", "abs", "lower", "upper", "trim", "split", "hasPrefix", "hasSuffix", "any", "all", "none", "filter", "count", "max", "min"];
const REGO_KEYWORDS = ["package", "import", "default", "if", "else", "not", "some", "every", "in", "contains", "with", "as", "true", "false", "null"];

function registerLanguages(monaco: Monaco) {
  if (languagesReady) return;
  languagesReady = true;
  monaco.languages.register({ id: "risk-expr" });
  monaco.languages.setMonarchTokensProvider("risk-expr", {
    keywords: EXPR_KEYWORDS,
    tokenizer: {
      root: [
        [/"([^"\\]|\\.)*"/, "string"],
        [/'([^'\\]|\\.)*'/, "string"],
        [/\b\d+(\.\d+)?\b/, "number"],
        [/\b[FM]\.[a-z_][\w]*/, "type"],
        [/[a-zA-Z_][\w.]*/, { cases: { "@keywords": "keyword", "@default": "identifier" } }],
        [/(==|!=|>=|<=|&&|\|\||[<>!+\-*/%?:])/, "operator"],
      ],
    },
  });
  monaco.languages.setLanguageConfiguration("risk-expr", {
    brackets: [["(", ")"], ["[", "]"]],
    autoClosingPairs: [{ open: "(", close: ")" }, { open: "[", close: "]" }, { open: '"', close: '"' }, { open: "'", close: "'" }],
  });

  monaco.languages.register({ id: "rego" });
  monaco.languages.setMonarchTokensProvider("rego", {
    keywords: REGO_KEYWORDS,
    tokenizer: {
      root: [
        [/#.*$/, "comment"],
        [/"([^"\\]|\\.)*"/, "string"],
        [/`[^`]*`/, "string"],
        [/\b\d+(\.\d+)?\b/, "number"],
        [/[a-zA-Z_][\w]*/, { cases: { "@keywords": "keyword", "@default": "identifier" } }],
        [/(:=|==|!=|>=|<=|[<>=|&])/, "operator"],
      ],
    },
  });
  monaco.languages.setLanguageConfiguration("rego", {
    comments: { lineComment: "#" },
    brackets: [["{", "}"], ["[", "]"], ["(", ")"]],
    autoClosingPairs: [{ open: "{", close: "}" }, { open: "[", close: "]" }, { open: "(", close: ")" }, { open: '"', close: '"' }],
  });
}

function registerExprCompletion(monaco: Monaco) {
  if (exprRegistration) return;
  exprRegistration = monaco.languages.registerCompletionItemProvider("risk-expr", {
    triggerCharacters: ["."],
    provideCompletionItems(model: editor.ITextModel, position: Position) {
      const line = model.getLineContent(position.lineNumber).slice(0, position.column - 1);
      const typed = line.match(/[\w.]*$/)?.[0] ?? "";
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: position.column - typed.length,
        endColumn: position.column,
      };
      const kinds = monaco.languages.CompletionItemKind;
      const kindOf = (k?: CompletionEntry["kind"]) =>
        k === "feature" ? kinds.Field : k === "model" ? kinds.Class : k === "function" ? kinds.Function : k === "keyword" ? kinds.Keyword : kinds.Variable;
      const entries: CompletionEntry[] = [
        ...exprCompletions,
        ...EXPR_FUNCS.map((f) => ({ label: f, kind: "function" as const })),
        ...EXPR_KEYWORDS.map((k) => ({ label: k, kind: "keyword" as const })),
      ];
      return {
        suggestions: entries.map((entry) => ({
          label: entry.label,
          kind: kindOf(entry.kind),
          detail: entry.detail,
          insertText: entry.label,
          range,
        })),
      };
    },
  });
}

function useMonacoReady() {
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  useEffect(() => {
    let alive = true;
    loadMonaco().then(
      () => alive && setState("ready"),
      () => alive && setState("failed"),
    );
    return () => {
      alive = false;
    };
  }, []);
  return state;
}

function useThemeSync(monacoRef: React.RefObject<Monaco | null>, ready: boolean) {
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";
  useEffect(() => {
    if (!ready) return;
    const frame = requestAnimationFrame(() => {
      if (monacoRef.current) applyAegisTheme(monacoRef.current, dark);
    });
    return () => cancelAnimationFrame(frame);
  }, [dark, ready, monacoRef]);
  return dark;
}

const BASE_OPTIONS: editor.IStandaloneEditorConstructionOptions = {
  fontSize: 12,
  fontFamily: "var(--font-mono), Consolas, monospace",
  fontLigatures: true,
  lineHeight: 20,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  automaticLayout: true,
  tabSize: 2,
  padding: { top: 8, bottom: 8 },
  renderLineHighlight: "none",
  overviewRulerLanes: 0,
  overviewRulerBorder: false,
  scrollbar: { alwaysConsumeMouseWheel: false, useShadows: false, verticalScrollbarSize: 8, horizontalScrollbarSize: 8 },
};

export function CodeEditor({
  value,
  onChange,
  language,
  height = 120,
  readOnly = false,
  completions,
  lineNumbers = false,
  placeholder,
  className,
}: {
  value: string;
  onChange?: (value: string) => void;
  language: CodeLanguage;
  height?: number | string;
  readOnly?: boolean;
  /** 只对 risk-expr 生效：变量、F.<特征>、M.<模型> */
  completions?: CompletionEntry[];
  lineNumbers?: boolean;
  placeholder?: string;
  className?: string;
}) {
  const instanceId = useId().replace(/[^a-zA-Z0-9]/g, "");
  const monacoRef = useRef<Monaco | null>(null);
  const state = useMonacoReady();
  const dark = useThemeSync(monacoRef, state === "ready");
  const smoothAnimation = useFunctionWorkbenchStore((s) => s.editor.smoothAnimation);
  const motion = useEditorMotion(smoothAnimation);

  useEffect(() => {
    if (language === "risk-expr" && completions) exprCompletions = completions;
  }, [language, completions]);

  if (state === "failed") {
    return (
      <div className={cn("flex items-center justify-center rounded-lg border bg-muted/20 p-4 text-xs text-muted-foreground", className)} style={{ height }}>
        编辑器加载失败
      </div>
    );
  }
  if (state === "loading") return <Skeleton className={cn("w-full rounded-lg", className)} style={{ height }} />;

  return (
    <div className={cn("aegis-code-editor overflow-hidden rounded-lg border", className)}>
      <MonacoEditor
        height={height}
        language={language}
        path={`aegis-risk-${instanceId}.${language === "json" ? "json" : language === "sql" ? "sql" : "txt"}`}
        value={value}
        theme={AEGIS_THEME}
        beforeMount={(monaco) => {
          monacoRef.current = monaco;
          applyAegisTheme(monaco, dark);
          registerLanguages(monaco);
          if (language === "risk-expr") {
            if (completions) exprCompletions = completions;
            registerExprCompletion(monaco);
          }
        }}
        onChange={(next) => onChange?.(next ?? "")}
        options={{
          ...BASE_OPTIONS,
          ...motion,
          readOnly,
          domReadOnly: readOnly,
          lineNumbers: lineNumbers ? "on" : "off",
          folding: lineNumbers,
          wordWrap: language === "risk-expr" ? "on" : "off",
          quickSuggestions: language === "risk-expr" ? { other: true, strings: false } : false,
          placeholder,
        }}
      />
    </div>
  );
}

/** 并排差异（只读） */
export function CodeDiff({ original, modified, language = "json", height = 420, className }: {
  original: string;
  modified: string;
  language?: CodeLanguage;
  height?: number | string;
  className?: string;
}) {
  const monacoRef = useRef<Monaco | null>(null);
  const state = useMonacoReady();
  const dark = useThemeSync(monacoRef, state === "ready");

  if (state === "failed") {
    return (
      <div className={cn("flex items-center justify-center rounded-lg border bg-muted/20 p-4 text-xs text-muted-foreground", className)} style={{ height }}>
        编辑器加载失败
      </div>
    );
  }
  if (state === "loading") return <Skeleton className={cn("w-full rounded-lg", className)} style={{ height }} />;

  return (
    <div className={cn("aegis-code-editor overflow-hidden rounded-lg border", className)}>
      <MonacoDiffEditor
        height={height}
        language={language}
        original={original}
        modified={modified}
        theme={AEGIS_THEME}
        beforeMount={(monaco) => {
          monacoRef.current = monaco;
          applyAegisTheme(monaco, dark);
          registerLanguages(monaco);
        }}
        options={{
          ...BASE_OPTIONS,
          readOnly: true,
          renderSideBySide: true,
          lineNumbers: "on",
          originalEditable: false,
        }}
      />
    </div>
  );
}
