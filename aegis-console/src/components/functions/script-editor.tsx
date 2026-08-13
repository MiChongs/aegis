"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import type { Monaco } from "@monaco-editor/react";
import type { editor, IDisposable, Position } from "monaco-editor";
import { Loader2 } from "lucide-react";
import type { AppFunctionDiagnostic } from "@/lib/api/app-functions";
import type { SDKTypeSource } from "@/lib/monaco/aegis-sdk-types";
import { buildAegisSDKTypes } from "@/lib/monaco/aegis-sdk-types";
import type { AegisLanguageContext } from "@/lib/monaco/aegis-language";
import { registerAegisLanguageFeatures } from "@/lib/monaco/aegis-language";
import { AEGIS_THEME, applyAegisTheme } from "@/lib/monaco/aegis-theme";
import { useEditorMotion } from "@/lib/monaco/editor-motion";
import { loadMonaco } from "@/lib/monaco/loader";
import { cn } from "@/lib/utils";

// Monaco 体积较大且强依赖 window，必须在客户端按需加载
const MonacoEditor = dynamic(() => import("@monaco-editor/react").then((mod) => mod.Editor), {
  ssr: false,
  loading: () => <EditorFallback />
});

const MonacoDiffEditor = dynamic(
  () => import("@monaco-editor/react").then((mod) => mod.DiffEditor),
  { ssr: false, loading: () => <EditorFallback /> }
);

function EditorFallback() {
  return (
    <div className="flex h-full min-h-72 items-center justify-center rounded-lg border bg-muted/20">
      <Loader2 className="size-5 animate-spin text-muted-foreground" />
    </div>
  );
}

const AEGIS_LIB_URI = "ts:aegis-function-sdk.d.ts";
/** 服务端诊断单独占一个 owner：与 TS 语言服务自己的标记互不覆盖 */
const DIAGNOSTIC_OWNER = "aegis-analysis";

export type ScriptEditorOptions = {
  fontSize: number;
  wordWrap: boolean;
  minimap: boolean;
  /** 呼吸光标、平滑插入符、惯性滚动。系统的 prefers-reduced-motion 有否决权 */
  smoothAnimation: boolean;
};

export const DEFAULT_EDITOR_OPTIONS: ScriptEditorOptions = {
  fontSize: 13,
  wordWrap: false,
  minimap: false,
  smoothAnimation: true
};

type ScriptEditorProps = {
  value: string;
  onChange: (value: string) => void;
  /** 已声明的能力，决定 .d.ts 里出现哪些 aegis.* 成员 */
  capabilities: string[];
  /** 后端下发的能力目录：类型声明的真实来源，缺它时退回最小兜底类型 */
  typeSource?: SDKTypeSource;
  /** 由入参 schema 生成的 `declare interface AegisInput {...}`（后端生成，前端不转换） */
  inputTypes?: string;
  /** 服务端静态检查结果，直接标在对应行上 */
  diagnostics?: AppFunctionDiagnostic[];
  /**
   * 平台语义提供者所需的上下文：配置值、诊断、以及采纳修复时的回调。
   *
   * 传 getter 而不是值 —— provider 的回调被 Monaco 长期持有，
   * 闭包捕获会永远看到首次注册那一刻的能力与配置。
   */
  languageContext?: () => AegisLanguageContext;
  /** 运行时抛错位置（试跑失败时），比诊断更值得一眼看到 */
  errorMarker?: { line: number; column: number; message: string } | null;
  /** 对比基线；非空时渲染差异视图（只读） */
  diffAgainst?: string | null;
  options?: ScriptEditorOptions;
  /** ⌘S / Ctrl+S */
  onSave?: () => void;
  /** ⌘Enter / Ctrl+Enter */
  onRun?: () => void;
  /** 把实例交给工具条，用于「格式化」这类由外部按钮触发的编辑器动作 */
  onEditorReady?: (instance: editor.IStandaloneCodeEditor | null) => void;
  className?: string;
  height?: number | string;
  readOnly?: boolean;
};

/**
 * 服务端脚本编辑器 —— Monaco + 完整 TypeScript 语言服务。
 *
 * 关于「LSP」：Monaco 内置的 TypeScript worker 就是 tsserver 本体，
 * 补全 / 悬浮文档 / 诊断 / 签名帮助 / 重命名 / 跳转全部由它提供，
 * 无需另起语言服务器进程。我们要做的是把四件事告诉它：
 *   1. Aegis SDK 的类型（按 capabilities 动态生成，见 aegis-sdk-types.ts）；
 *   2. 运行环境没有 DOM —— lib 只给 ES2020，于是 document / window / setTimeout
 *      会被正确标红，与 goja 沙箱的实际情况一致；
 *   3. 服务端静态检查的结论（TS 看不出「这项能力没声明」，那是平台语义）；
 *   4. 一组以 aegis 开头的代码片段（额度、锁、签名这些写法有固定形状）。
 */
export function ScriptEditor({
  value,
  onChange,
  capabilities,
  typeSource,
  inputTypes,
  diagnostics,
  languageContext,
  errorMarker,
  diffAgainst,
  options = DEFAULT_EDITOR_OPTIONS,
  onSave,
  onRun,
  onEditorReady,
  className,
  height = 460,
  readOnly = false
}: ScriptEditorProps) {
  const { resolvedTheme } = useTheme();
  const monacoRef = useRef<Monaco | null>(null);
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const languageRef = useRef<IDisposable | null>(null);
  const contextRef = useRef(languageContext);
  // 快捷键在 addCommand 时就把回调闭包捕获了，直接传 props 会永远调到首次渲染
  // 那一版（拿到的是过期的 source）。用 ref 让命令始终读到最新的一个。
  const saveRef = useRef(onSave);
  const runRef = useRef(onRun);
  const readyRef = useRef(onEditorReady);
  // 每次渲染后同步（不能在渲染期写 ref）。命令与挂载回调都发生在渲染之后，
  // 因此它们读到的一定是最新的一版。
  useEffect(() => {
    saveRef.current = onSave;
    runRef.current = onRun;
    readyRef.current = onEditorReady;
    contextRef.current = languageContext;
  });

  // 必须等自托管实例就绪再渲染 Editor：否则 @monaco-editor/react 会自行 init 并回退到 CDN
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  const dark = resolvedTheme === "dark";
  const motion = useEditorMotion(options.smoothAnimation);

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

  // 主题令牌是从 <html> 上现读的，因此深浅切换后必须重新定义一次。
  // 放到下一帧读：切换是由 next-themes 换 class 触发的，
  // 同一帧里读到的可能还是上一套值。
  useEffect(() => {
    if (!ready) return;
    const frame = requestAnimationFrame(() => {
      if (monacoRef.current) applyAegisTheme(monacoRef.current, dark);
    });
    return () => cancelAnimationFrame(frame);
  }, [dark, ready]);

  const applySDKTypes = useCallback(
    (monaco: Monaco, declared: string[], source?: SDKTypeSource, input?: string) => {
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
      defaults.setInlayHintsOptions({
        includeInlayParameterNameHints: "literals",
        includeInlayFunctionParameterTypeHints: false,
        includeInlayVariableTypeHints: false,
        includeInlayPropertyDeclarationTypeHints: false,
        includeInlayFunctionLikeReturnTypeHints: false,
        includeInlayEnumMemberValueHints: true
      });

      // 重设 extraLib：能力变化时旧声明必须被替换掉。
      // 入参类型由后端从 JSON Schema 生成后下发 —— 前端再写一个转换器
      // 就有了第二份真相，而两份类型不一致的表现是「补全里有、运行时没有」。
      defaults.setExtraLibs([
        { content: buildAegisSDKTypes(declared, source, input), filePath: AEGIS_LIB_URI }
      ]);
    },
    []
  );

  function handleBeforeMount(monaco: Monaco) {
    monacoRef.current = monaco;
    applyAegisTheme(monaco, dark);
    applySDKTypes(monaco, capabilities, typeSource, inputTypes);
    languageRef.current?.dispose();
    // 平台语义（能力目录、配置值、服务端诊断的快速修复）不在 TS 的认知范围内，
    // 由我们自己的 provider 补上。注册是全局的，因此必须成对 dispose。
    languageRef.current = registerAegisLanguageFeatures(monaco, () => {
      const resolve = contextRef.current;
      if (!resolve) {
        return {
          capabilities: [],
          declared: [],
          config: {},
          diagnostics: [],
          onGrantCapabilities: () => undefined,
          onRevokeCapabilities: () => undefined,
          onRun: () => undefined
        } satisfies AegisLanguageContext;
      }
      return resolve();
    });
  }

  function handleMount(instance: editor.IStandaloneCodeEditor, monaco: Monaco) {
    editorRef.current = instance;
    readyRef.current?.(instance);
    // 发布与试跑是这个界面上最高频的两个动作，而它们的按钮在编辑器外面 ——
    // 每次都要把手从键盘挪到鼠标上。
    instance.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => saveRef.current?.());
    instance.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => runRef.current?.());
  }

  // 勾选能力后（或目录 / 入参契约到位后）立刻刷新类型，可见成员随之增减
  useEffect(() => {
    if (monacoRef.current) {
      applySDKTypes(monacoRef.current, capabilities, typeSource, inputTypes);
    }
  }, [capabilities, typeSource, inputTypes, applySDKTypes]);

  useEffect(() => () => languageRef.current?.dispose(), []);

  // 把服务端诊断画到编辑器上。TS 语言服务看不出「这项能力没声明」——
  // 那是平台语义，只有后端目录说了算。
  useEffect(() => {
    const monaco = monacoRef.current;
    const model = editorRef.current?.getModel();
    if (!monaco || !model) return;

    const severity = (level: string) => {
      if (level === "error") return monaco.MarkerSeverity.Error;
      if (level === "warning") return monaco.MarkerSeverity.Warning;
      return monaco.MarkerSeverity.Info;
    };
    const markers: editor.IMarkerData[] = (diagnostics ?? []).map((diagnostic) => ({
      severity: severity(diagnostic.severity),
      message: diagnostic.message,
      startLineNumber: diagnostic.line,
      startColumn: diagnostic.column,
      endLineNumber: diagnostic.line,
      endColumn: diagnostic.endColumn || diagnostic.column + 1,
      source: "Aegis"
    }));
    if (errorMarker) {
      markers.push({
        severity: monaco.MarkerSeverity.Error,
        message: errorMarker.message,
        startLineNumber: errorMarker.line,
        startColumn: errorMarker.column,
        endLineNumber: errorMarker.line,
        endColumn: errorMarker.column + 1,
        source: "试跑"
      });
    }
    monaco.editor.setModelMarkers(model, DIAGNOSTIC_OWNER, markers);
  }, [diagnostics, errorMarker, value]);

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

  const shared: editor.IStandaloneEditorConstructionOptions = {
    ...motion,
    fontSize: options.fontSize,
    fontFamily: "var(--font-mono), Consolas, monospace",
    // Maple Mono 的连字在控制台其它地方（普通 HTML）本来就是开的，
    // 而 Monaco 默认显式关掉它们 —— 不打开的话同一段代码在文档页
    // 和编辑器里长得不一样。
    fontLigatures: true,
    // 行高按字号算而不是写死像素：改字号时行距要跟着走，
    // 否则调大字号会挤成一团。1.7 是长时间读代码比较舒服的一档。
    lineHeight: Math.round(options.fontSize * 1.7),
    letterSpacing: 0.2,
    minimap: { enabled: options.minimap, renderCharacters: false, maxColumn: 90 },
    wordWrap: options.wordWrap ? "on" : "off",
    scrollBeyondLastLine: false,
    tabSize: 2,
    renderWhitespace: "selection",
    automaticLayout: true,
    padding: { top: 14, bottom: 14 },
    roundedSelection: true,
    cursorStyle: "line",
    cursorWidth: 2,
    // 失焦时不画当前行高亮：编辑器旁边就是试跑面板，
    // 一条常亮的高亮条会让人以为焦点还在编辑器里
    renderLineHighlight: "all",
    renderLineHighlightOnlyWhenFocus: true,
    guides: { indentation: true, highlightActiveIndentation: true, bracketPairs: "active" },
    overviewRulerBorder: false,
    scrollbar: {
      alwaysConsumeMouseWheel: false,
      // useShadows 是那圈内阴影，关掉之后滚动条才是 macOS 的覆盖式手感
      useShadows: false,
      verticalScrollbarSize: 10,
      horizontalScrollbarSize: 10
    }
  };

  // 差异视图是只读的：在 diff 里编辑会让「哪一侧是当前草稿」变得含糊，
  // 而这个视图存在的意义恰恰是把两侧分清楚。
  if (diffAgainst != null) {
    return (
      <div className={cn("aegis-code-editor overflow-hidden rounded-lg border", className)}>
        <MonacoDiffEditor
          height={height}
          language="javascript"
          original={diffAgainst}
          modified={value}
          theme={AEGIS_THEME}
          options={{ ...shared, readOnly: true, renderSideBySide: true, originalEditable: false }}
        />
      </div>
    );
  }

  return (
    <div className={cn("aegis-code-editor overflow-hidden rounded-lg border", className)}>
      <MonacoEditor
        height={height}
        defaultLanguage="javascript"
        path="aegis-function.js"
        value={value}
        theme={AEGIS_THEME}
        beforeMount={handleBeforeMount}
        onMount={handleMount}
        onChange={(next) => onChange(next ?? "")}
        options={{
          ...shared,
          readOnly,
          quickSuggestions: { other: true, comments: false, strings: false },
          suggestOnTriggerCharacters: true,
          parameterHints: { enabled: true },
          formatOnPaste: true,
          bracketPairColorization: { enabled: true },
          stickyScroll: { enabled: true },
          // LSP 那一整套在 Monaco 里都是现成的，只是默认没全打开
          codeLens: true,
          inlayHints: { enabled: "on" },
          "semanticHighlighting.enabled": true,
          lightbulb: { enabled: "on" as never },
          occurrencesHighlight: "singleFile",
          definitionLinkOpensInPeek: true,
          suggest: { showWords: false, preview: true, shareSuggestSelections: true },
          linkedEditing: true,
          foldingStrategy: "indentation"
        }}
      />
    </div>
  );
}