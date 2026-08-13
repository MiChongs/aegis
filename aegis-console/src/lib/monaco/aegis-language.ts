"use client";

import type { Monaco } from "@monaco-editor/react";
import type { editor, IDisposable, languages, Position } from "monaco-editor";
import type { AppFunctionDiagnostic, FunctionCapability } from "@/lib/api/app-functions";
import { AEGIS_SNIPPETS } from "./aegis-snippets";

/**
 * Aegis 专属的语言能力。
 *
 * Monaco 内置的 TypeScript worker 就是 tsserver 本体，补全 / 悬浮 / 诊断 /
 * 签名帮助 / 重命名 / 跳转定义 / 引用查找全部由它提供。它唯一不知道的是
 * **平台语义**：
 *
 *   - 「`aegis.points` 存不存在」取决于这个函数勾了哪些能力，而不是类型系统
 *   - 「`aegis.config.dailyQuota` 现在是多少」在数据库里
 *   - 「这行缺 points.write」这条诊断由服务端算出，TS 无从得知
 *
 * 这个文件补的就是这三类：把服务端诊断变成可以「一键修复」的 code action、
 * 把配置值变成悬浮提示与补全项、把能力目录变成悬浮文档。
 *
 * 全部 provider 在一次注册里返回一个 dispose 句柄 —— Monaco 的注册是全局的，
 * 组件重挂载时不解绑会让同一个补全项出现两遍、三遍。
 */

export type AegisLanguageContext = {
  /** 能力目录（后端下发） */
  capabilities: FunctionCapability[];
  /** 这个函数已声明的能力 */
  declared: string[];
  /** 函数配置的当前值，用于 aegis.config 的补全与悬浮 */
  config: Record<string, unknown>;
  /** 服务端静态检查结论，code action 从这里取「该补哪项能力」 */
  diagnostics: AppFunctionDiagnostic[];
  /** 采纳「补上声明」时执行 */
  onGrantCapabilities: (keys: string[]) => void;
  /** 采纳「取消勾选」时执行 */
  onRevokeCapabilities: (keys: string[]) => void;
  /** code lens 上的「试跑」 */
  onRun: () => void;
};

const GRANT_COMMAND = "aegis.grantCapabilities";
const REVOKE_COMMAND = "aegis.revokeCapabilities";
const RUN_COMMAND = "aegis.runFunction";
/**
 * 纯展示的 code lens 也必须挂一个真实存在的命令。
 *
 * Monaco 的 lens 只要带 command 就会渲染成可点的链接，点击时把 id 交给
 * command service —— 给一个空 id 或没注册过的 id，点下去会抛错。
 */
const NOOP_COMMAND = "aegis.noop";

/**
 * 注册全部 provider，返回一个统一的 dispose。
 *
 * context 通过一个 getter 传入而不是直接传值：provider 的回调在 Monaco 侧
 * 长期持有，直接闭包捕获会永远看到首次注册那一刻的能力与配置 ——
 * 表现是勾了新能力之后，悬浮提示里它还是「未声明」。
 */
export function registerAegisLanguageFeatures(
  monaco: Monaco,
  getContext: () => AegisLanguageContext
): IDisposable {
  const disposables: IDisposable[] = [
    monaco.editor.registerCommand(GRANT_COMMAND, (_accessor: unknown, keys: string[]) => {
      getContext().onGrantCapabilities(keys);
    }),
    monaco.editor.registerCommand(REVOKE_COMMAND, (_accessor: unknown, keys: string[]) => {
      getContext().onRevokeCapabilities(keys);
    }),
    monaco.editor.registerCommand(RUN_COMMAND, () => getContext().onRun()),
    monaco.editor.registerCommand(NOOP_COMMAND, () => undefined),

    monaco.languages.registerCompletionItemProvider("javascript", {
      // `.` 触发：aegis.config. 之后要立刻列出实际配置键
      triggerCharacters: ["."],
      provideCompletionItems: (model: editor.ITextModel, position: Position) =>
        provideCompletions(monaco, getContext(), model, position)
    }),

    monaco.languages.registerHoverProvider("javascript", {
      provideHover: (model: editor.ITextModel, position: Position) =>
        provideHover(getContext(), model, position)
    }),

    monaco.languages.registerCodeActionProvider("javascript", {
      provideCodeActions: (
        _model: editor.ITextModel,
        _range: unknown,
        context: languages.CodeActionContext
      ) => provideCodeActions(getContext(), context)
    }),

    monaco.languages.registerCodeLensProvider("javascript", {
      provideCodeLenses: (model: editor.ITextModel) => provideCodeLenses(getContext(), model)
    })
  ];

  return {
    dispose() {
      for (const item of disposables) item.dispose();
    }
  };
}

// ── 补全 ────────────────────────────────────────────────────────────

function provideCompletions(
  monaco: Monaco,
  context: AegisLanguageContext,
  model: editor.ITextModel,
  position: Position
): languages.CompletionList {
  const word = model.getWordUntilPosition(position);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn
  };
  const linePrefix = model
    .getValueInRange({
      startLineNumber: position.lineNumber,
      startColumn: 1,
      endLineNumber: position.lineNumber,
      endColumn: position.column
    })
    .trimEnd();

  // aegis.config.<光标> —— 列出真实存在的键与它们**当前的值**。
  //
  // TS 那边 AegisConfig 是一个索引签名，补不出任何具体键；而作者最常
  // 犯的错就是读一个设置页上根本没有的键，那在运行时是静默的 undefined。
  if (/aegis\s*\.\s*config\s*\.\s*[\w$]*$/.test(linePrefix)) {
    const entries = Object.entries(context.config ?? {});
    if (!entries.length) {
      return {
        suggestions: [
          {
            label: "（函数配置为空）",
            kind: monaco.languages.CompletionItemKind.Text,
            insertText: "",
            detail: "去「设置 → 函数配置」里加一个键，改它不需要发新版本",
            range
          }
        ]
      };
    }
    return {
      suggestions: entries.map(([key, value]) => ({
        label: key,
        kind: monaco.languages.CompletionItemKind.Constant,
        insertText: key,
        detail: `当前值 ${previewValue(value)}`,
        documentation: {
          value: [
            "函数配置项，脚本里读作 `aegis.config." + key + "`。",
            "",
            "```json",
            JSON.stringify(value, null, 2),
            "```",
            "",
            "改它**不需要**发新版本。"
          ].join("\n")
        },
        range
      }))
    };
  }

  // 片段只在写标识符时给，避免在 `aegis.` 之后混进补全列表里 ——
  // 那个位置作者要的是成员名，一堆多行片段会把它挤下去。
  if (linePrefix.endsWith(".")) return { suggestions: [] };

  return {
    suggestions: AEGIS_SNIPPETS.map((snippet) => ({
      label: snippet.label,
      kind: monaco.languages.CompletionItemKind.Snippet,
      insertText: snippet.body,
      insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
      documentation: { value: snippet.detail },
      detail: snippet.summary,
      range
    }))
  };
}

function previewValue(value: unknown) {
  const text = typeof value === "string" ? value : JSON.stringify(value);
  return text && text.length > 40 ? `${text.slice(0, 40)}…` : text;
}

// ── 悬浮 ────────────────────────────────────────────────────────────

function provideHover(
  context: AegisLanguageContext,
  model: editor.ITextModel,
  position: Position
): languages.Hover | null {
  const word = model.getWordAtPosition(position);
  if (!word) return null;
  const line = model.getLineContent(position.lineNumber);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn
  };

  // aegis.config.<键>：直接把当前值贴出来。
  // 不给的话，「这个阈值现在是多少」要切到设置页去看，而那一页在另一个 Tab。
  if (new RegExp(`aegis\\s*\\.\\s*config\\s*\\.\\s*${escapeRegExp(word.word)}\\b`).test(line)) {
    const value = (context.config ?? {})[word.word];
    return {
      range,
      contents: [
        { value: `**函数配置** \`${word.word}\`` },
        {
          value:
            value === undefined
              ? "⚠️ 设置里**没有**这个键，运行时会是 `undefined` —— 而那不会报错，只会让阈值静默变回代码里的默认值。"
              : "```json\n" + JSON.stringify(value, null, 2) + "\n```"
        }
      ]
    };
  }

  // aegis.<命名空间>：把目录里的说明、风险档与「有没有声明」摆出来。
  // 类型定义只说得出签名，说不出「这是高风险能力」与「你还没勾它」。
  if (!new RegExp(`aegis\\s*\\.\\s*${escapeRegExp(word.word)}\\b`).test(line)) return null;
  const matched = context.capabilities.filter(
    (item) => !item.deprecated && (item.namespace === word.word || item.members?.includes(word.word))
  );
  if (!matched.length) return null;

  const declared = new Set(context.declared);
  const lines: string[] = [];
  for (const capability of matched) {
    const ok = declared.has(capability.key);
    lines.push(
      `${ok ? "✅" : "⚠️"} \`${capability.key}\` · ${capability.label} · ${riskLabel(capability.risk)}` +
        (capability.requiresUser ? " · 需用户身份" : "")
    );
    lines.push(capability.hint);
    if (!ok) {
      lines.push("**未声明** —— 运行时这个对象是 `undefined`，发布会被静态检查挡下。");
    }
    lines.push("");
  }
  return { range, contents: [{ value: `**aegis.${word.word}**` }, { value: lines.join("\n\n") }] };
}

function riskLabel(risk: string) {
  return risk === "high" ? "高风险" : risk === "medium" ? "中风险" : "低风险";
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// ── 快速修复 ────────────────────────────────────────────────────────

/**
 * 把服务端诊断变成灯泡里的快速修复。
 *
 * 缺能力那一类，后端已经算出该补哪几项 —— 让作者把能力键抄到设置页去，
 * 是把机器已经知道的答案又交回给人。侧栏里那个「补上声明」按钮做的是同一件事，
 * 但灯泡在光标所在的那一行，不需要先把视线移开。
 */
function provideCodeActions(
  context: AegisLanguageContext,
  actionContext: languages.CodeActionContext
): languages.CodeActionList {
  const actions: languages.CodeAction[] = [];
  const seen = new Set<string>();

  for (const marker of actionContext.markers) {
    // 只认我们自己那批（source 是 "Aegis"）：TS 的诊断有它自己的 quick fix
    if (marker.source !== "Aegis") continue;
    const diagnostic = context.diagnostics.find(
      (item) => item.line === marker.startLineNumber && item.message === marker.message
    );
    if (!diagnostic?.capabilities?.length) continue;

    const key = diagnostic.capabilities.join(",");
    if (seen.has(key)) continue;
    seen.add(key);

    // 多项能力任选其一即可满足（裸取 aegis.user 时读写都算数），
    // 因此逐项各给一个动作，而不是一次全勾上 —— 后者会静默放大权限面。
    for (const capability of diagnostic.capabilities) {
      actions.push({
        title: `声明能力 ${capability}`,
        kind: "quickfix",
        diagnostics: [marker],
        isPreferred: diagnostic.capabilities.length === 1,
        command: { id: GRANT_COMMAND, title: "声明能力", arguments: [[capability]] }
      });
    }
  }

  // 「勾了没用到」的反向修复：这条诊断挂在第 1 行，不进上面的循环
  const idle = context.diagnostics.find((item) => item.rule === "unused-capability");
  if (idle) {
    const unused = context.declared.filter((key) => idle.message.includes(key));
    if (unused.length) {
      actions.push({
        title: `取消勾选未使用的能力（${unused.join("、")}）`,
        kind: "refactor",
        command: { id: REVOKE_COMMAND, title: "取消勾选", arguments: [unused] }
      });
    }
  }

  return { actions, dispose: () => undefined };
}

// ── Code Lens ───────────────────────────────────────────────────────

/**
 * `handle` 上方的一行状态：已声明几项能力、入参契约有没有配、以及一个试跑入口。
 *
 * 这三件事决定了这份脚本能不能跑通，而它们原本分散在三个面板上。
 */
function provideCodeLenses(
  context: AegisLanguageContext,
  model: editor.ITextModel
): languages.CodeLensList {
  const match = model.findMatches(
    "function\\s+handle\\s*\\(", false, true, false, null, false, 1
  );
  if (!match.length) return { lenses: [], dispose: () => undefined };

  const line = match[0].range.startLineNumber;
  const declared = context.declared.length;
  const risky = context.capabilities.filter(
    (item) => item.risk === "high" && context.declared.includes(item.key)
  ).length;

  const lenses: languages.CodeLens[] = [
    {
      range: { startLineNumber: line, startColumn: 1, endLineNumber: line, endColumn: 1 },
      command: {
        id: RUN_COMMAND,
        title: `▶ 试跑（⌘/Ctrl + Enter）`
      }
    },
    {
      range: { startLineNumber: line, startColumn: 1, endLineNumber: line, endColumn: 1 },
      command: {
        id: NOOP_COMMAND,
        title:
          `已声明 ${declared} 项能力` +
          (risky ? `（含 ${risky} 项高风险）` : "") +
          " · 未声明的成员在运行时是 undefined"
      }
    }
  ];
  return { lenses, dispose: () => undefined };
}
