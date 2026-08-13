"use client";

import { useMemo, useRef, useState } from "react";
import type { editor } from "monaco-editor";
import {
  AlertTriangle,
  BookOpen,
  CheckCircle2,
  FileJson,
  FlaskConical,
  GitCompare,
  History,
  Info,
  Loader2,
  Plus,
  RotateCcw,
  Save,
  ShieldCheck,
  Sparkles,
  Trash2,
  UploadCloud,
  WandSparkles,
  XCircle
} from "lucide-react";
import { toast } from "sonner";
import type {
  AppFunction,
  AppFunctionDiagnostic,
  AppFunctionTestResult,
  FunctionCatalog
} from "@/lib/api/app-functions";
import {
  useActivateVersionMutation,
  useCreateVersionMutation,
  useDeleteVersionMutation,
  useFunctionAnalysisQuery,
  useFunctionVersionSourceQuery,
  useFunctionVersionsQuery,
  useTestFunctionMutation,
  useUpdateFunctionMutation
} from "@/lib/function-hooks";
import { getAppFunctionVersion } from "@/lib/api/app-functions";
import { useAdminToken } from "@/lib/admin-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { useFunctionWorkbenchStore, type ScriptTestCase } from "@/lib/function-workbench-store";
import { JsonEditor } from "@/components/functions/json-editor";
import { ScriptEditor } from "@/components/functions/script-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { EffectList, errorMessage, formatBytes, formatDuration, formatTime } from "./function-shared";

/**
 * 脚本工作台：写 → 检查 → 试跑 → 发布 → 回滚，全部在一屏内完成。
 *
 * 「检查」这一步是后加的，但它才是这条链路上最省时间的一环：沙箱是
 * deny-by-default 的，调了没声明的能力在运行时只是一句
 * `Cannot read property 'add' of undefined` —— 不说缺什么、不说在哪行，
 * 而且要等到真实调用才出现。现在它在保存那一刻就被标在对应的行上，
 * 并且发布走的是同一套判定，不会出现「这里全绿、发布被拦」。
 */
export function FunctionEditorPanel({
  appKey,
  selected,
  catalog,
  initialTemplate
}: {
  appKey: string;
  selected: AppFunction;
  catalog?: FunctionCatalog;
  initialTemplate?: string;
}) {
  const token = useAdminToken();
  const scope = `${appKey}:${selected.name}`;

  const versionsQuery = useFunctionVersionsQuery(appKey, selected.name);
  const versions = useMemo(() => versionsQuery.data ?? [], [versionsQuery.data]);

  // 编辑器的初始正文来自当前激活版本；没有激活版本时用建函数时选的模板。
  const activeSourceQuery = useFunctionVersionSourceQuery(
    appKey,
    selected.name,
    selected.runtime === "script" ? selected.activeVersion || null : null
  );

  const templateSource = useMemo(() => {
    const templates = catalog?.templates ?? [];
    return (
      templates.find((item) => item.key === initialTemplate)?.source ??
      templates.find((item) => item.key === "starter")?.source ??
      ""
    );
  }, [catalog, initialTemplate]);

  const drafts = useFunctionWorkbenchStore((state) => state.drafts);
  const saveDraft = useFunctionWorkbenchStore((state) => state.saveDraft);
  const dropDraft = useFunctionWorkbenchStore((state) => state.dropDraft);
  const editorOptions = useFunctionWorkbenchStore((state) => state.editor);
  const setEditorOption = useFunctionWorkbenchStore((state) => state.setEditorOption);

  // 草稿按 (应用, 函数) 绑定：切换函数时上一个函数的未保存改动不会串过来，
  // 也就不需要一个 effect 去同步（与配置面板同一约束）。
  // 落 localStorage 是因为误刷新一次就清零半小时的改动，是这个界面上
  // 最常见也最没必要的一次损失。
  const seeded = activeSourceQuery.data?.source ?? templateSource;
  const draft = drafts[scope];
  const source = draft?.source ?? seeded;
  const dirty = draft != null && draft.source !== seeded;

  // 试跑输入框的起点，按「越贴近这个函数真实收到的东西」排序：
  // 契约造出的样例 → 模板自带的样例 → 空对象。
  //
  // 兜底刻意是 `{}` 而不是 `{"action":"ping"}`：后者对任何一个配了契约的
  // 函数都通不过校验，作者建完函数点一下试跑，第一眼看到的就是报错。
  // 这条 useState 初始化只跑一次，而面板按函数名 key 重挂载，正好对上。
  const [testInput, setTestInput] = useState(
    () =>
      selected.inputSample ||
      (catalog?.templates ?? []).find((item) => item.key === initialTemplate)?.sampleInput ||
      "{}"
  );
  const [asUserId, setAsUserId] = useState("");
  const [testResult, setTestResult] = useState<TestResultWithSource | null>(null);
  const [publishOpen, setPublishOpen] = useState(false);
  const [diffAgainst, setDiffAgainst] = useState<string | null>(null);
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);

  const testMutation = useTestFunctionMutation(appKey);
  const activateMutation = useActivateVersionMutation(appKey);
  const deleteVersionMutation = useDeleteVersionMutation(appKey);
  const updateFunction = useUpdateFunctionMutation(appKey);

  // 静态检查跟着正文自动重跑。防抖 600ms：每敲一个字符问一次后端既浪费，
  // 也会让诊断在半句话写到一半时先红起来。
  const debouncedSource = useDebouncedValue(source, 600);
  const analysisQuery = useFunctionAnalysisQuery(
    appKey,
    selected.name,
    debouncedSource,
    selected.runtime === "script"
  );
  const diagnostics = analysisQuery.data?.diagnostics ?? [];
  const blocking = diagnostics.filter((item) => item.severity === "error");

  const typeSource = useMemo(
    () =>
      catalog
        ? { baseTypes: catalog.baseTypes, capabilities: catalog.capabilities }
        : undefined,
    [catalog]
  );

  // 试跑失败时把抛错位置也标到编辑器上。正文一改这个标记就该消失 ——
  // 它说的是「上一次跑到这里挂了」，而那一行可能已经不存在了。
  const errorMarker = useMemo(() => {
    if (!testResult?.errorLine || debouncedSource !== testResult.source) return null;
    return {
      line: testResult.errorLine,
      column: testResult.errorColumn || 1,
      message: testResult.error || "执行失败"
    };
  }, [testResult, debouncedSource]);

  async function loadVersionIntoEditor(version: string, mode: "edit" | "diff") {
    if (!token) return;
    try {
      const detail = await getAppFunctionVersion(token, appKey, selected.name, version);
      if (mode === "diff") {
        setDiffAgainst(detail.source);
        toast.info(`正在与版本 ${version} 对比`);
        return;
      }
      saveDraft(scope, detail.source);
      setDiffAgainst(null);
      toast.success(`已载入版本 ${version} 的正文`);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  async function runTest(overrides?: { input?: string; asUserId?: string }) {
    const rawInput = overrides?.input ?? testInput;
    let input: unknown;
    try {
      input = JSON.parse(rawInput);
    } catch {
      toast.error("input 不是合法 JSON");
      return;
    }
    const userId = overrides?.asUserId ?? asUserId;
    try {
      const result = await testMutation.mutateAsync({
        name: selected.name,
        payload: { source, input, asUserId: userId ? Number(userId) : undefined }
      });
      // 记下这次跑的是哪份正文：正文一改，错误行标记就不该再指向它
      setTestResult({ ...result, source });
      // 试跑失败是正常结果，不是接口错误 —— 用 warning 而不是 error，
      // 免得作者以为是平台出了问题
      if (result.ok) toast.success(`试跑通过（${formatDuration(result.durationMs)}）`);
      else toast.warning("试跑未通过，见下方结果");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  /** 把诊断指名的能力补进函数设置 —— 否则作者要把能力键抄到设置页去 */
  async function grantCapabilities(keys: string[]) {
    const merged = Array.from(new Set([...selected.capabilities, ...keys]));
    try {
      await updateFunction.mutateAsync({ name: selected.name, payload: { capabilities: merged } });
      toast.success(`已声明能力：${keys.join("、")}`);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  async function revokeCapabilities(keys: string[]) {
    const remaining = selected.capabilities.filter((key) => !keys.includes(key));
    try {
      await updateFunction.mutateAsync({
        name: selected.name,
        payload: { capabilities: remaining }
      });
      toast.success(`已取消勾选：${keys.join("、")}`);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  function revealLine(line: number) {
    editorRef.current?.revealLineInCenter(line);
    editorRef.current?.setPosition({ lineNumber: line, column: 1 });
    editorRef.current?.focus();
  }

  // 编辑器里那批平台语义提供者（悬浮、配置补全、快速修复、code lens）
  // 读的就是这份上下文。传函数而不是值：provider 被 Monaco 长期持有，
  // 传值会让它永远看到首次注册那一刻的能力与配置。
  function languageContext() {
    return {
      capabilities: catalog?.capabilities ?? [],
      declared: selected.capabilities,
      config: selected.config ?? {},
      diagnostics,
      onGrantCapabilities: (keys: string[]) => void grantCapabilities(keys),
      onRevokeCapabilities: (keys: string[]) => void revokeCapabilities(keys),
      onRun: () => void runTest()
    };
  }

  if (selected.runtime !== "script") {
    return (
      <NonScriptVersionPanel
        appKey={appKey}
        selected={selected}
        versions={versions}
        loading={versionsQuery.isLoading}
      />
    );
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2">
              脚本
              {dirty ? (
                <Badge variant="warning" size="sm">
                  未发布的改动
                </Badge>
              ) : null}
              <AnalysisBadge
                loading={analysisQuery.isFetching}
                diagnostics={diagnostics}
                enabled={Boolean(debouncedSource.trim())}
              />
            </CardTitle>
            <CardDescription>
              正文只保存在服务端，任何接口都不会下发给接入方。编辑器已按当前能力载入 SDK 类型：
              输入 <code className="font-mono">aegis.</code> 看全部可用成员，输入{" "}
              <code className="font-mono">aegis-</code> 取代码片段。
              <span className="mt-1 block">
                <Kbd>⌘/Ctrl</Kbd> + <Kbd>Enter</Kbd> 试跑 · <Kbd>⌘/Ctrl</Kbd> + <Kbd>S</Kbd> 发布
              </span>
            </CardDescription>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {diffAgainst != null ? (
              <Button variant="outline" size="sm" onClick={() => setDiffAgainst(null)}>
                <XCircle className="size-4" />
                退出对比
              </Button>
            ) : null}
            {dirty ? (
              <Button variant="ghost" size="sm" onClick={() => dropDraft(scope)}>
                <RotateCcw className="size-4" />
                还原
              </Button>
            ) : null}
            <EditorToolbar
              catalog={catalog}
              options={editorOptions}
              onOptionChange={setEditorOption}
              onFormat={() => editorRef.current?.getAction("editor.action.formatDocument")?.run()}
              onInsertTemplate={(next) => {
                saveDraft(scope, next);
                setDiffAgainst(null);
              }}
              onDiffWithActive={
                selected.activeVersion
                  ? () => loadVersionIntoEditor(selected.activeVersion as string, "diff")
                  : undefined
              }
            />
            <Button size="sm" onClick={() => setPublishOpen(true)} disabled={!source.trim()}>
              <UploadCloud className="size-4" />
              发布版本
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          <ScriptEditor
            value={source}
            onChange={(next) => saveDraft(scope, next)}
            capabilities={selected.capabilities}
            typeSource={typeSource}
            inputTypes={selected.inputTypes}
            diagnostics={diagnostics}
            languageContext={languageContext}
            errorMarker={errorMarker}
            diffAgainst={diffAgainst}
            options={editorOptions}
            onEditorReady={(instance) => {
              editorRef.current = instance;
            }}
            onSave={() => setPublishOpen(true)}
            onRun={() => void runTest()}
            height={460}
          />
          <p className="text-xs text-muted-foreground">
            已声明能力：
            {selected.capabilities.length ? selected.capabilities.join("、") : "无"}
            {catalog ? (
              <>
                {" · "}单次调用额度：SDK {catalog.limits.maxSdkCalls} 次 / 写{" "}
                {catalog.limits.maxSdkMutations} 次 / 出站 {catalog.limits.maxSdkFetches} 次
              </>
            ) : null}
            {draft ? <> · 草稿保存于 {formatTime(new Date(draft.updatedAt).toISOString())}</> : null}
          </p>
        </CardContent>
      </Card>

      <DiagnosticsPanel
        diagnostics={diagnostics}
        loading={analysisQuery.isFetching}
        applying={updateFunction.isPending}
        onReveal={revealLine}
        onGrant={grantCapabilities}
      />

      <div className="grid gap-4 xl:grid-cols-2">
        <TestRunnerCard
          scope={scope}
          input={testInput}
          onInputChange={setTestInput}
          inputSchema={selected.inputSchema}
          inputSample={selected.inputSample}
          asUserId={asUserId}
          onAsUserIdChange={setAsUserId}
          pending={testMutation.isPending}
          disabled={!source.trim()}
          blocking={blocking.length}
          onRun={runTest}
        />
        <TestResultCard result={testResult} onReveal={revealLine} />
      </div>

      <VersionTable
        versions={versions}
        loading={versionsQuery.isLoading}
        activeVersion={selected.activeVersion}
        showSource
        onLoadSource={(version) => loadVersionIntoEditor(version, "edit")}
        onDiffSource={(version) => loadVersionIntoEditor(version, "diff")}
        onActivate={async (version) => {
          try {
            await activateMutation.mutateAsync({ name: selected.name, version });
            toast.success(`版本 ${version} 已激活`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
        onDelete={async (version) => {
          try {
            await deleteVersionMutation.mutateAsync({ name: selected.name, version });
            toast.success(`版本 ${version} 已删除`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
      />

      <PublishDialog
        open={publishOpen}
        onOpenChange={setPublishOpen}
        appKey={appKey}
        selected={selected}
        source={source}
        blocking={blocking}
        suggestedVersion={suggestNextVersion(versions.map((item) => item.version))}
        onPublished={() => dropDraft(scope)}
      />
    </div>
  );
}

/** 试跑结果额外记住「跑的是哪份正文」，正文一改错误行标记就该失效 */
type TestResultWithSource = AppFunctionTestResult & { source?: string };

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="rounded border bg-muted px-1 py-0.5 font-mono text-[10px] text-muted-foreground">
      {children}
    </kbd>
  );
}

/**
 * 检查状态徽标。
 *
 * 「全绿」这件事必须显式说出来 —— 一个什么都不显示的界面，作者无从判断
 * 是「检查通过」还是「检查根本没跑」，而这两种情况下他该做的事完全相反。
 */
function AnalysisBadge({
  loading,
  diagnostics,
  enabled
}: {
  loading: boolean;
  diagnostics: AppFunctionDiagnostic[];
  enabled: boolean;
}) {
  if (!enabled) return null;
  if (loading) {
    return (
      <Badge variant="outline" size="sm" className="gap-1 font-normal">
        <Loader2 className="size-3 animate-spin" />
        检查中
      </Badge>
    );
  }
  const errors = diagnostics.filter((item) => item.severity === "error").length;
  const warnings = diagnostics.filter((item) => item.severity === "warning").length;
  if (errors) {
    return (
      <Badge variant="danger" size="sm" className="gap-1">
        <XCircle className="size-3" />
        {errors} 处会挡住发布
      </Badge>
    );
  }
  if (warnings) {
    return (
      <Badge variant="warning" size="sm" className="gap-1">
        <AlertTriangle className="size-3" />
        {warnings} 处提示
      </Badge>
    );
  }
  return (
    <Badge variant="success" size="sm" className="gap-1">
      <ShieldCheck className="size-3" />
      检查通过
    </Badge>
  );
}

type EditorPreferences = {
  fontSize: number;
  wordWrap: boolean;
  minimap: boolean;
  smoothAnimation: boolean;
};

function EditorToolbar({
  catalog,
  options,
  onOptionChange,
  onFormat,
  onInsertTemplate,
  onDiffWithActive
}: {
  catalog?: FunctionCatalog;
  options: EditorPreferences;
  onOptionChange: <K extends keyof EditorPreferences>(key: K, value: EditorPreferences[K]) => void;
  onFormat: () => void;
  onInsertTemplate: (source: string) => void;
  onDiffWithActive?: () => void;
}) {
  return (
    <div className="flex items-center gap-1">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-sm" onClick={onFormat} aria-label="格式化">
            <WandSparkles className="size-4" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>格式化</TooltipContent>
      </Tooltip>

      {onDiffWithActive ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onDiffWithActive}
              aria-label="与激活版本对比"
            >
              <GitCompare className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>与线上激活的那一版对比</TooltipContent>
        </Tooltip>
      ) : null}

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label="模板与编辑器设置">
            <Sparkles className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-64">
          <DropdownMenuLabel>从模板替换</DropdownMenuLabel>
          {(catalog?.templates ?? []).map((template) => (
            <DropdownMenuItem key={template.key} onClick={() => onInsertTemplate(template.source)}>
              <BookOpen className="size-4" />
              <span className="min-w-0">
                <span className="block">{template.title}</span>
                <span className="block text-[11px] text-muted-foreground">{template.summary}</span>
              </span>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuLabel>编辑器</DropdownMenuLabel>
          <DropdownMenuCheckboxItem
            checked={options.wordWrap}
            onCheckedChange={(next) => onOptionChange("wordWrap", next === true)}
          >
            自动换行
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={options.minimap}
            onCheckedChange={(next) => onOptionChange("minimap", next === true)}
          >
            缩略图
          </DropdownMenuCheckboxItem>
          <DropdownMenuCheckboxItem
            checked={options.smoothAnimation}
            onCheckedChange={(next) => onOptionChange("smoothAnimation", next === true)}
          >
            <span className="min-w-0">
              <span className="block">平滑动效</span>
              <span className="block text-[11px] text-muted-foreground">
                呼吸光标 · 插入符滑动 · 惯性滚动
              </span>
            </span>
          </DropdownMenuCheckboxItem>
          <DropdownMenuSeparator />
          <div className="flex items-center justify-between px-2 py-1.5 text-sm">
            <span>字号</span>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="icon-sm"
                aria-label="减小字号"
                onClick={() => onOptionChange("fontSize", Math.max(11, options.fontSize - 1))}
              >
                −
              </Button>
              <span className="w-6 text-center font-mono text-xs">{options.fontSize}</span>
              <Button
                variant="outline"
                size="icon-sm"
                aria-label="增大字号"
                onClick={() => onOptionChange("fontSize", Math.min(20, options.fontSize + 1))}
              >
                +
              </Button>
            </div>
          </div>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

const SEVERITY_META = {
  error: { icon: XCircle, tone: "text-destructive", label: "错误" },
  warning: { icon: AlertTriangle, tone: "text-amber-600 dark:text-amber-400", label: "提示" },
  info: { icon: Info, tone: "text-muted-foreground", label: "建议" }
} as const;

/**
 * 诊断列表。
 *
 * 编辑器上的波浪线只在那一行可见，而作者要的是「一共几处、分别在哪」。
 * 缺声明那一类还带一个「补上」按钮 —— 后端已经算出该补哪几项，
 * 让作者自己把能力键抄到设置页去纯属多余。
 */
function DiagnosticsPanel({
  diagnostics,
  loading,
  applying,
  onReveal,
  onGrant
}: {
  diagnostics: AppFunctionDiagnostic[];
  loading: boolean;
  applying: boolean;
  onReveal: (line: number) => void;
  onGrant: (capabilities: string[]) => void;
}) {
  if (!diagnostics.length) return null;
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          静态检查
          {loading ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
        </CardTitle>
        <CardDescription>
          与发布门禁同一套判定：这里的「错误」会挡住发布，「提示」与「建议」不会。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-1.5">
        {diagnostics.map((diagnostic, index) => {
          const meta = SEVERITY_META[diagnostic.severity] ?? SEVERITY_META.info;
          const Icon = meta.icon;
          return (
            <div
              key={`${diagnostic.rule}-${index}`}
              className="flex items-start gap-2 rounded-lg border p-2.5 text-xs"
            >
              <Icon className={cn("mt-0.5 size-3.5 shrink-0", meta.tone)} />
              <button
                type="button"
                className="min-w-0 flex-1 text-left hover:underline"
                onClick={() => onReveal(diagnostic.line)}
              >
                <span className="font-mono text-[11px] text-muted-foreground">
                  第 {diagnostic.line} 行
                </span>
                <span className="ml-1.5">{diagnostic.message}</span>
              </button>
              {diagnostic.capabilities?.length ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={applying}
                  onClick={() => onGrant(diagnostic.capabilities as string[])}
                >
                  {applying ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
                  补上声明
                </Button>
              ) : null}
            </div>
          );
        })}
      </CardContent>
    </Card>
  );
}

const NO_TEST_CASES: ScriptTestCase[] = [];

/** 试跑面板：input、身份、以及存下来的那几组用例。 */
function TestRunnerCard({
  scope,
  input,
  onInputChange,
  inputSchema,
  inputSample,
  asUserId,
  onAsUserIdChange,
  pending,
  disabled,
  blocking,
  onRun
}: {
  scope: string;
  input: string;
  onInputChange: (value: string) => void;
  inputSchema?: Record<string, unknown>;
  inputSample?: string;
  asUserId: string;
  onAsUserIdChange: (value: string) => void;
  pending: boolean;
  disabled: boolean;
  blocking: number;
  onRun: (overrides?: { input?: string; asUserId?: string }) => void;
}) {
  // 兜底数组必须是模块级常量：selector 每次返回一个新的 []，
  // zustand v5 走 useSyncExternalStore，引用不稳定会直接抛
  // 「getSnapshot should be cached」并把组件打进无限重渲染。
  const testCases = useFunctionWorkbenchStore((state) => state.testCases[scope] ?? NO_TEST_CASES);
  const addTestCase = useFunctionWorkbenchStore((state) => state.addTestCase);
  const removeTestCase = useFunctionWorkbenchStore((state) => state.removeTestCase);
  const [caseName, setCaseName] = useState("");
  // 空对象与「没有 properties 的对象」都不算约束 —— 后者在 JSON Schema
  // 语义下同样放行任何输入，把它当成「已配置」会让徽标撒谎。
  const hasSchema = Boolean(
    inputSchema && Object.keys((inputSchema.properties as object) ?? {}).length > 0
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <FlaskConical className="size-4" />
          试跑
        </CardTitle>
        <CardDescription>
          不创建版本、不写调用审计。读的是真实数据，写操作只记录不执行 ——
          因此额度判断、会员判定这些分支都能测到，而不会真的发出去。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {testCases.length ? (
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">已存用例</Label>
            <div className="flex flex-wrap gap-1.5">
              {testCases.map((testCase) => (
                <span
                  key={testCase.id}
                  className="group flex items-center gap-1 rounded-full border py-0.5 pl-2.5 pr-1 text-xs"
                >
                  <button
                    type="button"
                    className="hover:underline"
                    onClick={() => {
                      onInputChange(testCase.input);
                      onAsUserIdChange(testCase.asUserId);
                      onRun({ input: testCase.input, asUserId: testCase.asUserId });
                    }}
                  >
                    {testCase.name}
                  </button>
                  <button
                    type="button"
                    aria-label={`删除用例 ${testCase.name}`}
                    className="rounded-full p-0.5 text-muted-foreground hover:text-destructive"
                    onClick={() => removeTestCase(scope, testCase.id)}
                  >
                    <XCircle className="size-3" />
                  </button>
                </span>
              ))}
            </div>
          </div>
        ) : null}

        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <Label>input</Label>
            <div className="flex items-center gap-1.5">
              {hasSchema ? (
                <Badge variant="outline" size="sm" className="gap-1 font-normal">
                  <FileJson className="size-3" />
                  按入参契约补全与校验
                </Badge>
              ) : null}
              {inputSample ? (
                <Button
                  size="xs"
                  variant="ghost"
                  title="按契约生成一份只含必填字段的样例"
                  onClick={() => onInputChange(inputSample)}
                >
                  <Sparkles className="size-3" />
                  按契约填
                </Button>
              ) : null}
            </div>
          </div>
          {/* 配了契约就把它喂给 JSON 语言服务：键名补全、枚举值补全、
              必填校验、悬浮显示字段说明，全部是现成的。没配时退回纯语法检查。 */}
          <JsonEditor
            value={input}
            onChange={onInputChange}
            schema={hasSchema ? inputSchema : undefined}
            height={140}
          />
          {!hasSchema ? (
            <p className="text-[11px] text-muted-foreground">
              这个函数还没有入参契约。在「设置 → 入参契约」里配一份，这里会有补全与校验，
              编辑器里 <code className="font-mono">ctx.input</code> 也会有真实字段。
            </p>
          ) : null}
        </div>
        <div className="space-y-2">
          <Label>以哪个用户的身份跑</Label>
          <Input
            value={asUserId}
            onChange={(event) => onAsUserIdChange(event.target.value.replace(/\D/g, ""))}
            placeholder="用户 ID，留空则以当前管理员身份"
            inputMode="numeric"
          />
          <p className="text-[11px] text-muted-foreground">
            多数脚本第一行就是 <code className="font-mono">aegis.user.get()</code>；
            不填的话只能测到那句 fail。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button disabled={pending || disabled} onClick={() => onRun()}>
            {pending ? <Loader2 className="size-4 animate-spin" /> : <FlaskConical className="size-4" />}
            试跑
          </Button>
          <div className="flex flex-1 items-center gap-1.5">
            <Input
              className="h-8 text-xs"
              value={caseName}
              onChange={(event) => setCaseName(event.target.value)}
              placeholder="存成用例，例如「过期会员」"
            />
            <Button
              variant="outline"
              size="sm"
              disabled={!caseName.trim()}
              onClick={() => {
                addTestCase(scope, {
                  id: `${Date.now()}`,
                  name: caseName.trim(),
                  input,
                  asUserId
                });
                setCaseName("");
                toast.success("用例已保存到本机");
              }}
            >
              <Save className="size-3.5" />
              存
            </Button>
          </div>
        </div>
        {blocking > 0 ? (
          <p className="text-[11px] text-amber-600 dark:text-amber-400">
            静态检查有 {blocking} 处错误，试跑仍可进行（它跑的是你现在这份正文），
            但发布会被挡下。
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function TestResultCard({
  result,
  onReveal
}: {
  result: TestResultWithSource | null;
  onReveal: (line: number) => void;
}) {
  const [logLevel, setLogLevel] = useState("all");
  const logs = useMemo(() => {
    if (!result) return [];
    if (logLevel === "all") return result.logs;
    return result.logs.filter((entry) => entry.level === logLevel);
  }, [result, logLevel]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>试跑结果</CardTitle>
        <CardDescription>返回值、日志与本该发生的副作用</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {!result ? (
          <p className="py-12 text-center text-sm text-muted-foreground">还没有跑过</p>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <Badge variant={result.ok ? "success" : "danger"}>
                {result.ok ? "通过" : result.businessCode ? "被脚本拒绝" : "执行失败"}
              </Badge>
              <span className="text-muted-foreground">
                {formatDuration(result.durationMs)} · SDK {result.sdkCalls} 次 / 写{" "}
                {result.sdkMutations} 次 / 出站 {result.sdkFetches} 次
              </span>
            </div>

            {result.error ? (
              <div className="space-y-1.5 rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
                <div>
                  {result.businessCode ? (
                    <span className="mr-1.5 font-mono text-[11px] text-muted-foreground">
                      {result.businessCode}
                    </span>
                  ) : null}
                  {result.error}
                </div>
                {result.errorLine ? (
                  <button
                    type="button"
                    className="font-mono text-[11px] text-muted-foreground hover:underline"
                    onClick={() => onReveal(result.errorLine as number)}
                  >
                    跳到第 {result.errorLine} 行
                  </button>
                ) : null}
                {result.stack?.length ? (
                  <pre className="max-h-24 overflow-auto rounded border bg-background/60 p-1.5 font-mono text-[10px] text-muted-foreground">
                    {result.stack.join("\n")}
                  </pre>
                ) : null}
              </div>
            ) : null}

            {result.output !== undefined && result.output !== null ? (
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">返回值</p>
                <pre className="max-h-48 overflow-auto rounded-lg border bg-muted/40 p-2.5 font-mono text-[11px]">
                  {JSON.stringify(result.output, null, 2)}
                </pre>
              </div>
            ) : null}

            {result.logs.length ? (
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <p className="text-xs text-muted-foreground">日志（{result.logs.length} 行）</p>
                  <Select value={logLevel} onValueChange={setLogLevel}>
                    <SelectTrigger className="h-7 w-24 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">全部</SelectItem>
                      <SelectItem value="info">info</SelectItem>
                      <SelectItem value="warn">warn</SelectItem>
                      <SelectItem value="error">error</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="max-h-40 space-y-0.5 overflow-auto rounded-lg border bg-muted/40 p-2">
                  {logs.map((entry, index) => (
                    <div key={index} className="flex gap-2 font-mono text-[11px]">
                      {/* 相对耗时是脚本里唯一的计时手段：沙箱没有 timer，
                          「哪一步慢」只能靠日志之间的间隔看出来 */}
                      <span className="w-14 shrink-0 text-right text-muted-foreground">
                        {entry.elapsedMs.toFixed(1)}ms
                      </span>
                      <span
                        className={cn(
                          "w-9 shrink-0",
                          entry.level === "error" && "text-destructive",
                          entry.level === "warn" && "text-amber-600 dark:text-amber-400",
                          entry.level === "info" && "text-muted-foreground"
                        )}
                      >
                        {entry.level}
                      </span>
                      <span className="min-w-0 break-all">{entry.message}</span>
                    </div>
                  ))}
                  {!logs.length ? (
                    <p className="py-2 text-center text-[11px] text-muted-foreground">
                      该级别没有日志
                    </p>
                  ) : null}
                </div>
              </div>
            ) : null}

            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">副作用</p>
              <EffectList effects={result.effects} />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * 版本号建议：把最后一段数字加一。
 *
 * 建议而不是强制 —— 版本号的格式由接入方自己定，平台只负责它唯一且不可变。
 * 但每次都让人手打一遍 `1.0.7` 是没必要的，而手打恰恰会打错。
 */
function suggestNextVersion(existing: string[]) {
  if (!existing.length) return "1.0.0";
  const match = existing[0].match(/^(.*?)(\d+)([^\d]*)$/);
  if (!match) return "";
  return `${match[1]}${Number(match[2]) + 1}${match[3]}`;
}

function PublishDialog({
  open,
  onOpenChange,
  appKey,
  selected,
  source,
  blocking,
  suggestedVersion,
  onPublished
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  selected: AppFunction;
  source: string;
  blocking: AppFunctionDiagnostic[];
  suggestedVersion: string;
  onPublished: () => void;
}) {
  const [version, setVersion] = useState(suggestedVersion);
  const [notes, setNotes] = useState("");
  // 默认发布即激活：绝大多数情况下发新版本就是为了让它生效，
  // 而「发了却没生效」是最难自己发现的一种状态。
  const [activate, setActivate] = useState(true);
  const createVersion = useCreateVersionMutation(appKey);
  const activateVersion = useActivateVersionMutation(appKey);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) setVersion(suggestedVersion);
        onOpenChange(next);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>发布 {selected.name} 的新版本</DialogTitle>
          <DialogDescription>
            版本记录不可修改。发布前会做语法、入口与能力声明检查，跑不起来的脚本进不了线上。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          {blocking.length ? (
            <div className="space-y-1 rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs">
              <p className="font-medium text-destructive">静态检查未通过，发布会被拒绝：</p>
              {blocking.slice(0, 3).map((diagnostic, index) => (
                <p key={index} className="text-muted-foreground">
                  第 {diagnostic.line} 行：{diagnostic.message}
                </p>
              ))}
              {blocking.length > 3 ? (
                <p className="text-muted-foreground">…另有 {blocking.length - 3} 处</p>
              ) : null}
            </div>
          ) : null}
          <div className="space-y-2">
            <Label>版本号</Label>
            <Input
              value={version}
              onChange={(event) => setVersion(event.target.value)}
              placeholder="1.0.0"
            />
          </div>
          <div className="space-y-2">
            <Label>发版说明</Label>
            <Textarea
              className="min-h-20 text-xs"
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              placeholder="这一版改了什么 —— 回滚时要靠它决定滚到哪一版"
            />
          </div>
          <label className="flex items-center justify-between rounded-lg border p-2.5 text-sm">
            <span>
              发布后立即激活
              <span className="mt-0.5 block text-[11px] text-muted-foreground">
                关掉的话这一版只是暂存，线上仍跑当前激活的版本
              </span>
            </span>
            <Switch checked={activate} onCheckedChange={setActivate} />
          </label>
          <p className="text-xs text-muted-foreground">
            正文 {formatBytes(new TextEncoder().encode(source).length)}
          </p>
        </div>
        <DialogFooter>
          <Button
            disabled={createVersion.isPending || !version.trim() || blocking.length > 0}
            onClick={async () => {
              try {
                const created = await createVersion.mutateAsync({
                  name: selected.name,
                  payload: { version: version.trim(), source, notes }
                });
                if (activate) {
                  await activateVersion.mutateAsync({
                    name: selected.name,
                    version: created.version
                  });
                }
                onPublished();
                onOpenChange(false);
                setNotes("");
                toast.success(activate ? "版本已发布并激活" : "版本已发布（未激活）");
              } catch (error) {
                toast.error(errorMessage(error));
              }
            }}
          >
            {createVersion.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <UploadCloud className="size-4" />
            )}
            发布
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function VersionTable({
  versions,
  loading,
  activeVersion,
  showSource,
  onLoadSource,
  onDiffSource,
  onActivate,
  onDelete
}: {
  versions: Array<{
    id: number;
    version: string;
    status: string;
    notes: string;
    sourceBytes: number;
    endpointUrl?: string;
    artifactSha256: string;
    createdAt: string;
  }>;
  loading?: boolean;
  activeVersion?: string;
  showSource?: boolean;
  onLoadSource?: (version: string) => void;
  onDiffSource?: (version: string) => void;
  onActivate: (version: string) => void;
  onDelete: (version: string) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>版本</CardTitle>
        <CardDescription>
          版本记录不可修改：发布新功能即创建新版本并激活，回滚则重新激活旧版本。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>版本</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>说明</TableHead>
              <TableHead>产物</TableHead>
              <TableHead>发布时间</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {versions.map((version) => (
              <TableRow key={version.id}>
                <TableCell className="font-mono">{version.version}</TableCell>
                <TableCell>
                  <Badge variant={version.status === "active" ? "success" : "outline"}>
                    {version.status}
                  </Badge>
                </TableCell>
                <TableCell className="max-w-60 truncate text-xs text-muted-foreground">
                  {version.notes || "—"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {version.endpointUrl
                    ? version.endpointUrl
                    : version.sourceBytes > 0
                      ? formatBytes(version.sourceBytes)
                      : "WASM"}
                  <span className="ml-1.5 font-mono text-[10px]">
                    {version.artifactSha256.slice(0, 8)}
                  </span>
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {formatTime(version.createdAt)}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    {showSource && onDiffSource ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        title="与当前编辑器里的正文对比"
                        aria-label={`对比版本 ${version.version}`}
                        onClick={() => onDiffSource(version.version)}
                      >
                        <GitCompare className="size-4" />
                      </Button>
                    ) : null}
                    {showSource && onLoadSource ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        title="把这一版的正文载入编辑器"
                        aria-label={`载入版本 ${version.version}`}
                        onClick={() => onLoadSource(version.version)}
                      >
                        <History className="size-4" />
                      </Button>
                    ) : null}
                    {version.version !== activeVersion ? (
                      <>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onActivate(version.version)}
                        >
                          <CheckCircle2 className="size-4" />
                          激活
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={`删除版本 ${version.version}`}
                          onClick={() => onDelete(version.version)}
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      </>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {!versions.length ? (
              <TableRow>
                <TableCell colSpan={6} className="py-10 text-center text-sm text-muted-foreground">
                  {loading ? "加载中…" : "暂无版本"}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

/** wasm / http 运行时的发布面板：没有脚本编辑器，也没有试跑。 */
function NonScriptVersionPanel({
  appKey,
  selected,
  versions,
  loading
}: {
  appKey: string;
  selected: AppFunction;
  versions: Array<{
    id: number;
    version: string;
    status: string;
    notes: string;
    sourceBytes: number;
    endpointUrl?: string;
    artifactSha256: string;
    createdAt: string;
  }>;
  loading: boolean;
}) {
  const [version, setVersion] = useState("");
  const [notes, setNotes] = useState("");
  const [endpointUrl, setEndpointUrl] = useState("");
  const [responsePublicKey, setResponsePublicKey] = useState("");
  const [wasmBase64, setWasmBase64] = useState("");
  const createVersion = useCreateVersionMutation(appKey);
  const activateVersion = useActivateVersionMutation(appKey);
  const deleteVersion = useDeleteVersionMutation(appKey);

  const isHttp = selected.runtime === "http";

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>发布版本</CardTitle>
          <CardDescription>
            {isHttp
              ? "Endpoint 仅允许 HTTPS，禁止重定向，并在实际连接时重新解析 IP 拒绝内网与元数据地址。"
              : "WASM 最大 2MB，每次调用独立实例，内存上限 16MB，不提供网络与文件系统。"}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>版本号</Label>
              <Input
                value={version}
                onChange={(event) => setVersion(event.target.value)}
                placeholder="1.0.0"
              />
            </div>
            <div className="space-y-2">
              <Label>发版说明</Label>
              <Input value={notes} onChange={(event) => setNotes(event.target.value)} />
            </div>
          </div>
          {isHttp ? (
            <>
              <div className="space-y-2">
                <Label>Endpoint URL</Label>
                <Input
                  value={endpointUrl}
                  onChange={(event) => setEndpointUrl(event.target.value)}
                  placeholder="https://functions.example.com/aegis"
                />
              </div>
              <div className="space-y-2">
                <Label>响应 Ed25519 公钥</Label>
                <Textarea
                  className="min-h-24 font-mono text-xs"
                  value={responsePublicKey}
                  onChange={(event) => setResponsePublicKey(event.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Worker 必须对响应签名，Aegis 用此公钥验签。
                </p>
              </div>
            </>
          ) : (
            <div className="space-y-2">
              <Label>WASM Base64</Label>
              <Textarea
                className="min-h-48 font-mono text-xs"
                value={wasmBase64}
                onChange={(event) => setWasmBase64(event.target.value)}
              />
            </div>
          )}
          <Button
            disabled={createVersion.isPending || !version.trim()}
            onClick={async () => {
              try {
                await createVersion.mutateAsync({
                  name: selected.name,
                  payload: {
                    version: version.trim(),
                    notes,
                    endpointUrl: isHttp ? endpointUrl : undefined,
                    responsePublicKey: isHttp ? responsePublicKey : undefined,
                    wasmBase64: isHttp ? undefined : wasmBase64
                  }
                });
                setVersion("");
                setNotes("");
                toast.success("版本已创建，激活后生效");
              } catch (error) {
                toast.error(errorMessage(error));
              }
            }}
          >
            {createVersion.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <UploadCloud className="size-4" />
            )}
            创建版本
          </Button>
        </CardContent>
      </Card>

      <VersionTable
        versions={versions}
        loading={loading}
        activeVersion={selected.activeVersion}
        onActivate={async (target) => {
          try {
            await activateVersion.mutateAsync({ name: selected.name, version: target });
            toast.success(`版本 ${target} 已激活`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
        onDelete={async (target) => {
          try {
            await deleteVersion.mutateAsync({ name: selected.name, version: target });
            toast.success(`版本 ${target} 已删除`);
          } catch (error) {
            toast.error(errorMessage(error));
          }
        }}
      />
    </div>
  );
}
