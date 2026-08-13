"use client";

import { useEffect, useId, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { useTheme } from "next-themes";
import type { Monaco } from "@monaco-editor/react";
import type { editor } from "monaco-editor";
import { Loader2 } from "lucide-react";
import { useFunctionWorkbenchStore } from "@/lib/function-workbench-store";
import { AEGIS_THEME, applyAegisTheme } from "@/lib/monaco/aegis-theme";
import { useEditorMotion } from "@/lib/monaco/editor-motion";
import { loadMonaco } from "@/lib/monaco/loader";
import { cn } from "@/lib/utils";

const MonacoEditor = dynamic(() => import("@monaco-editor/react").then((mod) => mod.Editor), {
  ssr: false,
  loading: () => (
    <div className="flex h-full min-h-32 items-center justify-center rounded-lg border bg-muted/20">
      <Loader2 className="size-4 animate-spin text-muted-foreground" />
    </div>
  )
});

/**
 * 带 JSON Schema 的 JSON 编辑器。
 *
 * Monaco 的 JSON 语言服务（vscode-json-languageservice 本体）在给了 schema
 * 之后会提供：键名补全、枚举值补全、类型校验、必填项校验、悬浮显示 description、
 * 以及格式化。这三件事原本都是没有的 —— 试跑输入框、函数配置、入参契约
 * 全是纯 `<textarea>`，写错一个逗号要等到点了提交才知道。
 *
 * 关键约束：**每个实例必须有唯一的 model URI**。schema 是按 `fileMatch`
 * 绑到 URI 上的，两个实例共用一个 URI 会让后挂载的那个 schema 覆盖前一个 ——
 * 表现是「函数配置编辑器里在按入参契约校验」，而那两份 schema 毫无关系。
 */
export function JsonEditor({
  value,
  onChange,
  /** JSON Schema 对象；传 undefined 表示不校验（仍有语法检查与格式化） */
  schema,
  height = 200,
  readOnly = false,
  placeholder,
  className
}: {
  value: string;
  onChange: (value: string) => void;
  schema?: Record<string, unknown>;
  height?: number | string;
  readOnly?: boolean;
  placeholder?: string;
  className?: string;
}) {
  const { resolvedTheme } = useTheme();
  const instanceId = useId().replace(/[^a-zA-Z0-9]/g, "");
  const monacoRef = useRef<Monaco | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  const dark = resolvedTheme === "dark";
  // 动效偏好直接读同一个 store，而不是从调用方一层层传下来：
  // 同一屏上两个编辑器，一个光标在呼吸、另一个在硬闪，比两个都不动还奇怪。
  const smoothAnimation = useFunctionWorkbenchStore((state) => state.editor.smoothAnimation);
  const motion = useEditorMotion(smoothAnimation);

  // model 路径按实例唯一。用 useId 而不是随机数：随机数在 React 严格模式下
  // 两次渲染会得到两个 URI，第二个 model 建出来时第一个还挂着 schema。
  const modelPath = `aegis-json-${instanceId}.json`;
  const schemaUri = `aegis://function/${instanceId}.json`;

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

  // schema 变化时重设。这里改的是 Monaco 的**全局** JSON 配置，
  // 因此必须保留别的实例已经注册的那些条目，只替换自己这一条。
  useEffect(() => {
    const monaco = monacoRef.current;
    if (!monaco) return;
    applyJsonSchema(monaco, schemaUri, modelPath, schema);
    return () => {
      const current = monacoRef.current;
      if (current) applyJsonSchema(current, schemaUri, modelPath, undefined);
    };
  }, [schema, schemaUri, modelPath]);

  // 主题令牌现读自 <html>，深浅切换后要重定义（与脚本编辑器同一套主题名）
  useEffect(() => {
    if (!ready) return;
    const frame = requestAnimationFrame(() => {
      if (monacoRef.current) applyAegisTheme(monacoRef.current, dark);
    });
    return () => cancelAnimationFrame(frame);
  }, [dark, ready]);

  if (failed) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-lg border bg-muted/20 p-4 text-xs text-muted-foreground",
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
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className={cn("aegis-code-editor overflow-hidden rounded-lg border", className)}>
      <MonacoEditor
        height={height}
        defaultLanguage="json"
        path={modelPath}
        value={value}
        theme={AEGIS_THEME}
        beforeMount={(monaco) => {
          monacoRef.current = monaco;
          applyAegisTheme(monaco, dark);
          applyJsonSchema(monaco, schemaUri, modelPath, schema);
        }}
        onChange={(next) => onChange(next ?? "")}
        options={{
          ...motion,
          readOnly,
          fontSize: 12,
          fontFamily: "var(--font-mono), Consolas, monospace",
          fontLigatures: true,
          lineHeight: 21,
          minimap: { enabled: false },
          lineNumbers: "off",
          folding: true,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          padding: { top: 10, bottom: 10 },
          roundedSelection: true,
          cursorStyle: "line",
          cursorWidth: 2,
          renderLineHighlight: "none",
          overviewRulerLanes: 0,
          overviewRulerBorder: false,
          scrollbar: {
            alwaysConsumeMouseWheel: false,
            useShadows: false,
            verticalScrollbarSize: 8,
            horizontalScrollbarSize: 8
          },
          // 有 schema 时补全是这个编辑器的主要价值，默认就把建议列表打开
          quickSuggestions: { other: true, strings: true },
          suggestOnTriggerCharacters: true,
          placeholder
        }}
      />
    </div>
  );
}

/**
 * 增删自己那一条 schema 注册。
 *
 * `setDiagnosticsOptions` 是整体覆盖式的，读改写必须成对 —— 直接传一条新的
 * 会把页面上其它 JSON 编辑器的 schema 全部抹掉，而那种失效是静默的：
 * 补全只是「不出来了」，没有任何报错。
 */
function applyJsonSchema(
  monaco: Monaco,
  uri: string,
  modelPath: string,
  schema: Record<string, unknown> | undefined
) {
  const defaults = monaco.languages.json.jsonDefaults;
  const existing = defaults.diagnosticsOptions.schemas ?? [];
  const others = existing.filter((item: { uri: string }) => item.uri !== uri);
  defaults.setDiagnosticsOptions({
    ...defaults.diagnosticsOptions,
    validate: true,
    // allowComments 关掉：这些内容最终会被 JSON.parse，
    // 编辑器放行注释而提交时报错是最让人费解的一种不一致。
    allowComments: false,
    enableSchemaRequest: false,
    schemas: schema
      ? [...others, { uri, fileMatch: [modelPath], schema }]
      : others
  });
}
