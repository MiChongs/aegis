"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import type { Monaco } from "@monaco-editor/react";
import type { editor } from "monaco-editor";
import { Loader2 } from "lucide-react";
import { buildAegisSDKTypes } from "@/lib/monaco/aegis-sdk-types";
import { loadMonaco } from "@/lib/monaco/loader";
import { cn } from "@/lib/utils";

// Monaco 体积较大且强依赖 window，必须在客户端按需加载
const MonacoEditor = dynamic(() => import("@monaco-editor/react").then((mod) => mod.Editor), {
  ssr: false,
  loading: () => (
    <div className="flex h-full min-h-72 items-center justify-center rounded-lg border bg-muted/20">
      <Loader2 className="size-5 animate-spin text-muted-foreground" />
    </div>
  )
});

const AEGIS_LIB_URI = "ts:aegis-function-sdk.d.ts";

type ScriptEditorProps = {
  value: string;
  onChange: (value: string) => void;
  /** 已声明的能力，决定 .d.ts 里出现哪些 aegis.* 成员 */
  capabilities: string[];
  className?: string;
  height?: number | string;
  readOnly?: boolean;
};

/**
 * 服务端脚本编辑器 —— Monaco + 完整 TypeScript 语言服务。
 *
 * 关于「LSP」：Monaco 内置的 TypeScript worker 就是 tsserver 本体，
 * 补全 / 悬浮文档 / 诊断 / 签名帮助 / 重命名 / 跳转全部由它提供，
 * 无需另起语言服务器进程。我们要做的是把两件事告诉它：
 *   1. Aegis SDK 的类型（按 capabilities 动态生成，见 aegis-sdk-types.ts）；
 *   2. 运行环境没有 DOM —— lib 只给 ES2020，于是 document / window / setTimeout
 *      会被正确标红，与 goja 沙箱的实际情况一致。
 */
export function ScriptEditor({
  value,
  onChange,
  capabilities,
  className,
  height = 460,
  readOnly = false
}: ScriptEditorProps) {
  const { resolvedTheme } = useTheme();
  const monacoRef = useRef<Monaco | null>(null);
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  // 必须等自托管实例就绪再渲染 Editor：否则 @monaco-editor/react 会自行 init 并回退到 CDN
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    loadMonaco().then(
      () => alive && setReady(true),
      () => alive && setFailed(true)
    );
    return () => {
      alive = false;
    };
  }, []);

  const applySDKTypes = useCallback((monaco: Monaco, declared: string[]) => {
    const defaults = monaco.languages.typescript.javascriptDefaults;
    defaults.setCompilerOptions({
      target: monaco.languages.typescript.ScriptTarget.ES2020,
      // 沙箱里没有 DOM、没有 Node、没有定时器；不给 DOM lib 才能如实报错
      lib: ["es2020"],
      allowJs: true,
      // 打开 checkJs，模板里的 JSDoc @param 才能让 ctx 获得类型
      checkJs: true,
      allowNonTsExtensions: true,
      noEmit: true,
      strict: false,
      moduleResolution: monaco.languages.typescript.ModuleResolutionKind.NodeJs
    });
    defaults.setDiagnosticsOptions({
      noSemanticValidation: false,
      noSyntaxValidation: false,
      // 2304: Cannot find name —— 顶层 handle 是给宿主调用的，不视为未使用
      diagnosticCodesToIgnore: [80001]
    });

    // 重设 extraLib：能力变化时旧声明必须被替换掉
    defaults.setExtraLibs([{ content: buildAegisSDKTypes(declared), filePath: AEGIS_LIB_URI }]);
  }, []);

  function handleBeforeMount(monaco: Monaco) {
    monacoRef.current = monaco;
    applySDKTypes(monaco, capabilities);
  }

  function handleMount(instance: editor.IStandaloneCodeEditor) {
    editorRef.current = instance;
  }

  // 勾选能力后立刻刷新类型，编辑器里的可见成员随之增减
  useEffect(() => {
    if (monacoRef.current) {
      applySDKTypes(monacoRef.current, capabilities);
    }
  }, [capabilities, applySDKTypes]);

  if (failed) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-lg border bg-muted/20 p-6 text-sm text-muted-foreground",
          className
        )}
        style={{ height }}
      >
        编辑器加载失败：请确认 /monaco/vs 已同步（pnpm monaco:sync）
      </div>
    );
  }

  if (!ready) {
    return (
      <div
        className={cn("flex items-center justify-center rounded-lg border bg-muted/20", className)}
        style={{ height }}
      >
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className={cn("overflow-hidden rounded-lg border", className)}>
      <MonacoEditor
        height={height}
        defaultLanguage="javascript"
        path="aegis-function.js"
        value={value}
        theme={resolvedTheme === "dark" ? "vs-dark" : "vs"}
        beforeMount={handleBeforeMount}
        onMount={handleMount}
        onChange={(next) => onChange(next ?? "")}
        options={{
          readOnly,
          fontSize: 13,
          fontFamily: "var(--font-mono), Consolas, monospace",
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          smoothScrolling: true,
          tabSize: 2,
          renderWhitespace: "selection",
          automaticLayout: true,
          padding: { top: 12, bottom: 12 },
          quickSuggestions: { other: true, comments: false, strings: false },
          suggestOnTriggerCharacters: true,
          parameterHints: { enabled: true },
          formatOnPaste: true,
          bracketPairColorization: { enabled: true },
          scrollbar: { alwaysConsumeMouseWheel: false }
        }}
      />
    </div>
  );
}
