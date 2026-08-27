"use client";

import { useMemo, useRef, useState } from "react";
import type { editor } from "monaco-editor";
import {
  AlertTriangle,
  BookOpen,
  Bot,
  CheckCircle2,
  ChevronDown,
  FileJson,
  FlaskConical,
  GitCompare,
  History,
  Info,
  Loader2,
  PanelBottomClose,
  PanelBottomOpen,
  PanelRightClose,
  PanelRightOpen,
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
import { FunctionAIAssistant } from "@/components/functions/function-ai-assistant";
import { JsonEditor } from "@/components/functions/json-editor";
import { ScriptEditor } from "@/components/functions/script-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  ResizableGroup,
  ResizableHandle,
  ResizablePanel,
  usePanelLayout,
  usePanelRef,
  type PanelImperativeHandle
} from "@/components/ui/resizable";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { EffectList, errorMessage, formatBytes, formatDuration, formatTime } from "./function-shared";

/**
 * 脚本工作台：写 → 检查 → 试跑 → 发布 → 回滚，**一屏之内**完成。
 *
 * 形状是一个 IDE：中间编辑器、底部 dock（问题 / 版本）、右侧 dock（试跑输入 / 结果），
 * 三条分隔线都能拖，两个 dock 都能整块折叠起来。
 *
 * 旧形状是竖着堆卡片：编辑器写死 460px，下面依次是诊断、试跑、结果、版本表。
 * 于是最常做的那件事 —— 改一行、跑一次、看日志 —— 变成了「滚下去点试跑、
 * 滚回来改代码、再滚下去看结果」。而屏幕右边整块是空的。
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
  const [dockTab, setDockTab] = useState<DockTab>("problems");
  const [cursor, setCursor] = useState({ line: 1, column: 1 });
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);

  // 三个 dock 的折叠态各自记一份布尔位：按钮图标要跟着变，
  // 而库只在 ref 上提供 isCollapsed()，读它不会触发重渲染。
  const dockRef = usePanelRef();
  const runnerRef = usePanelRef();
  const assistantRef = usePanelRef();
  const [dockOpen, setDockOpen] = useState(true);
  const [runnerOpen, setRunnerOpen] = useState(true);
  const [assistantOpen, setAssistantOpen] = useState(false);

  // 布局键带 -v2：面板从两格变三格，旧存档的比例数组对不上新形状。
  const shellLayout = usePanelLayout({
    id: "aegis-function-script-shell-v2",
    panelIds: ["workspace", "runner", "assistant"]
  });
  const workspaceLayout = usePanelLayout({
    id: "aegis-function-script-workspace",
    panelIds: ["editor", "dock"]
  });
  const runnerLayout = usePanelLayout({
    id: "aegis-function-script-runner",
    panelIds: ["input", "result"]
  });

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
      toast.success(`已载入版本 ${version}`);
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
      toast.error("输入不是合法 JSON");
      return;
    }
    // 跑之前先把右侧 dock 打开：结果没地方显示的话，那一声 toast
    // 就是作者唯一能拿到的信息
    expand(runnerRef, setRunnerOpen);
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
      else toast.warning("试跑未通过，请查看试跑结果");
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
      toast.success(`已移除能力：${keys.join("、")}`);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  function revealLine(line: number) {
    editorRef.current?.revealLineInCenter(line);
    editorRef.current?.setPosition({ lineNumber: line, column: 1 });
    editorRef.current?.focus();
  }

  /** AI 助手默认收起（defaultSize 0），首次展开时 expand() 无「最近尺寸」可回，兜底给 32%。 */
  function toggleAssistant() {
    const handle = assistantRef.current;
    if (!handle) return;
    if (handle.isCollapsed()) {
      handle.expand();
      if (handle.isCollapsed()) handle.resize("32");
      setAssistantOpen(true);
    } else {
      handle.collapse();
      setAssistantOpen(false);
    }
  }

  /** 跳到某个问题：顺手把底部 dock 切到「问题」并展开 */
  function revealDiagnostic(line: number) {
    setDockTab("problems");
    expand(dockRef, setDockOpen);
    revealLine(line);
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
      <div className="h-full overflow-y-auto p-3">
        <NonScriptVersionPanel
          appKey={appKey}
          selected={selected}
          versions={versions}
          loading={versionsQuery.isLoading}
        />
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* ── 工具条：一行，永远在原地 ── */}
      <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b px-2 py-1.5">
        <AnalysisBadge
          loading={analysisQuery.isFetching}
          diagnostics={diagnostics}
          enabled={Boolean(debouncedSource.trim())}
          onClick={() => {
            setDockTab("problems");
            expand(dockRef, setDockOpen);
          }}
        />
        {dirty ? (
          <>
            <Badge variant="warning" size="sm">
              未发布的改动
            </Badge>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => dropDraft(scope)}
                  aria-label="还原到激活版本"
                >
                  <RotateCcw className="size-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>丢弃草稿，还原为激活版本</TooltipContent>
            </Tooltip>
          </>
        ) : null}
        {diffAgainst != null ? (
          <Button variant="outline" size="xs" onClick={() => setDiffAgainst(null)}>
            <XCircle className="size-3" />
            退出对比
          </Button>
        ) : null}

        <div className="ml-auto flex items-center gap-1">
          <Button
            variant={assistantOpen ? "secondary" : "ghost"}
            size="sm"
            onClick={toggleAssistant}
            aria-pressed={assistantOpen}
          >
            <Bot className="size-4" />
            AI 助手
          </Button>
          <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
          <HelpPopover />
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
          <PanelToggle
            open={dockOpen}
            onToggle={() => toggle(dockRef, setDockOpen)}
            openIcon={<PanelBottomClose className="size-4" />}
            closedIcon={<PanelBottomOpen className="size-4" />}
            label="问题与版本"
          />
          <PanelToggle
            open={runnerOpen}
            onToggle={() => toggle(runnerRef, setRunnerOpen)}
            openIcon={<PanelRightClose className="size-4" />}
            closedIcon={<PanelRightOpen className="size-4" />}
            label="试跑面板"
          />
          <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
          <Button
            variant="secondary"
            size="sm"
            disabled={testMutation.isPending || !source.trim()}
            onClick={() => void runTest()}
          >
            {testMutation.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <FlaskConical className="size-4" />
            )}
            试跑
          </Button>
          <Button size="sm" onClick={() => setPublishOpen(true)} disabled={!source.trim()}>
            <UploadCloud className="size-4" />
            发布
          </Button>
        </div>
      </div>

      {/* ── 三格布局：编辑器 / 底部 dock / 右侧试跑 ── */}
      <ResizableGroup
        orientation="horizontal"
        id="script-shell"
        className="min-h-0 flex-1"
        defaultLayout={shellLayout.defaultLayout}
        onLayoutChanged={shellLayout.onLayoutChanged}
      >
        <ResizablePanel id="workspace" minSize="30" className="flex flex-col">
          <ResizableGroup
            orientation="vertical"
            id="script-workspace"
            defaultLayout={workspaceLayout.defaultLayout}
            onLayoutChanged={workspaceLayout.onLayoutChanged}
          >
            <ResizablePanel id="editor" minSize="25" className="flex flex-col">
              <ScriptEditor
                className="min-h-0 flex-1 rounded-none border-0"
                height="100%"
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
                onCursorChange={setCursor}
                onSave={() => setPublishOpen(true)}
                onRun={() => void runTest()}
              />
            </ResizablePanel>

            <ResizableHandle />

            <ResizablePanel
              id="dock"
              className="flex flex-col"
              defaultSize="25"
              minSize="12"
              collapsible
              collapsedSize={0}
              panelRef={dockRef}
              onResize={(size) => setDockOpen(size.inPixels > 1)}
            >
              <BottomDock
                tab={dockTab}
                onTabChange={setDockTab}
                onCollapse={() => collapse(dockRef, setDockOpen)}
                diagnostics={diagnostics}
                analysing={analysisQuery.isFetching}
                analysed={Boolean(analysisQuery.data)}
                applying={updateFunction.isPending}
                onReveal={revealDiagnostic}
                onGrant={grantCapabilities}
                versions={versions}
                versionsLoading={versionsQuery.isLoading}
                activeVersion={selected.activeVersion}
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
            </ResizablePanel>
          </ResizableGroup>
        </ResizablePanel>

        <ResizableHandle />

        <ResizablePanel
          id="runner"
          className="flex flex-col"
          defaultSize="30"
          minSize="16"
          maxSize="55"
          collapsible
          collapsedSize={0}
          panelRef={runnerRef}
          onResize={(size) => setRunnerOpen(size.inPixels > 1)}
        >
          <ResizableGroup
            orientation="vertical"
            id="script-runner"
            defaultLayout={runnerLayout.defaultLayout}
            onLayoutChanged={runnerLayout.onLayoutChanged}
          >
            <ResizablePanel id="input" defaultSize="55" minSize="20" className="flex flex-col">
              <TestRunnerPane
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
            </ResizablePanel>
            <ResizableHandle />
            <ResizablePanel id="result" minSize="15" className="flex flex-col">
              <TestResultPane result={testResult} onReveal={revealLine} />
            </ResizablePanel>
          </ResizableGroup>
        </ResizablePanel>

        <ResizableHandle />

        {/* AI 助手常驻挂载、默认收起：收起时流式对话仍在后台继续，
            卸载它等于把正在跑的 Agent 一并掐死。 */}
        <ResizablePanel
          id="assistant"
          className="flex flex-col"
          defaultSize="0"
          minSize="20"
          maxSize="60"
          collapsible
          collapsedSize={0}
          panelRef={assistantRef}
          onResize={(size) => setAssistantOpen(size.inPixels > 1)}
        >
          <FunctionAIAssistant
            appKey={appKey}
            functionName={selected.name}
            draftSource={source}
            onApplySource={(next) => {
              saveDraft(scope, next);
              setDiffAgainst(null);
            }}
            onCollapse={() => collapse(assistantRef, setAssistantOpen)}
          />
        </ResizablePanel>
      </ResizableGroup>

      {/* ── 状态栏 ── */}
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-0.5 border-t px-2.5 py-1 text-[11px] text-muted-foreground">
        <span className="font-mono">
          第 {cursor.line} 行，第 {cursor.column} 列
        </span>
        <span>
          能力 {selected.capabilities.length}
          {selected.capabilities.length ? `（${selected.capabilities.join("、")}）` : "：无"}
        </span>
        {catalog ? (
          <span className="hidden lg:inline">
            单次额度 SDK {catalog.limits.maxSdkCalls} / 写 {catalog.limits.maxSdkMutations} / 出站{" "}
            {catalog.limits.maxSdkFetches}
          </span>
        ) : null}
        <span className="hidden sm:inline">
          {formatBytes(new TextEncoder().encode(source).length)}
        </span>
        {draft ? (
          <span className="hidden xl:inline">
            草稿保存于 {formatTime(new Date(draft.updatedAt).toISOString())}
          </span>
        ) : null}
        <span className="ml-auto flex items-center gap-1">
          <Kbd>⌘/Ctrl</Kbd>
          <Kbd>↵</Kbd>
          试跑
          <span className="mx-1">·</span>
          <Kbd>⌘/Ctrl</Kbd>
          <Kbd>S</Kbd>
          发布
        </span>
      </div>

      <PublishDialog
        open={publishOpen}
        onOpenChange={setPublishOpen}
        appKey={appKey}
        selected={selected}
        source={source}
        blocking={blocking}
        suggestedVersion={suggestNextVersion(versions.map((item) => item.version))}
        onPublished={() => dropDraft(scope)}
        onShowProblems={() => {
          setDockTab("problems");
          expand(dockRef, setDockOpen);
        }}
      />
    </div>
  );
}

/** 试跑结果额外记住「跑的是哪份正文」，正文一改错误行标记就该失效 */
type TestResultWithSource = AppFunctionTestResult & { source?: string };

type DockTab = "problems" | "versions";

type PanelRef = { current: PanelImperativeHandle | null };

function expand(ref: PanelRef, notify: (open: boolean) => void) {
  ref.current?.expand();
  notify(true);
}

function collapse(ref: PanelRef, notify: (open: boolean) => void) {
  ref.current?.collapse();
  notify(false);
}

function toggle(ref: PanelRef, notify: (open: boolean) => void) {
  const instance = ref.current;
  if (!instance) return;
  if (instance.isCollapsed()) expand(ref, notify);
  else collapse(ref, notify);
}

function PanelToggle({
  open,
  onToggle,
  openIcon,
  closedIcon,
  label
}: {
  open: boolean;
  onToggle: () => void;
  openIcon: React.ReactNode;
  closedIcon: React.ReactNode;
  label: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onToggle}
          aria-label={`${open ? "收起" : "展开"}${label}`}
        >
          {open ? openIcon : closedIcon}
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        {open ? "收起" : "展开"}
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="rounded border bg-muted px-1 font-mono text-[10px] text-muted-foreground">
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
  enabled,
  onClick
}: {
  loading: boolean;
  diagnostics: AppFunctionDiagnostic[];
  enabled: boolean;
  onClick: () => void;
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
  const badge = errors ? (
    <Badge variant="danger" size="sm" className="gap-1">
      <XCircle className="size-3" />
      {errors} 个错误
    </Badge>
  ) : warnings ? (
    <Badge variant="warning" size="sm" className="gap-1">
      <AlertTriangle className="size-3" />
      {warnings} 个提示
    </Badge>
  ) : (
    <Badge variant="success" size="sm" className="gap-1">
      <ShieldCheck className="size-3" />
      检查通过
    </Badge>
  );
  return (
    <button type="button" onClick={onClick} title="查看问题列表">
      {badge}
    </button>
  );
}

/**
 * 这一屏的说明书。
 *
 * 旧版把它写成编辑器上方的一段常驻说明 —— 每个人都只读一次，
 * 却永久占着三行高度。收进一个图标里，需要时点开。
 */
function HelpPopover() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="使用说明">
          <Info className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 space-y-2 text-xs">
        <p>
          脚本正文仅保存在服务端，不会下发给接入方。编辑器已按当前能力载入 SDK 类型：
          输入 <code className="font-mono">aegis.</code> 查看可用成员，输入{" "}
          <code className="font-mono">aegis-</code> 插入代码片段。
        </p>
        <p className="text-muted-foreground">
          试跑<strong>读真写假</strong>：读取真实数据，写操作仅记录不执行；
          不创建版本，不计入调用审计。
        </p>
        <p className="text-muted-foreground">
          <Kbd>⌘/Ctrl</Kbd> <Kbd>↵</Kbd> 试跑 · <Kbd>⌘/Ctrl</Kbd> <Kbd>S</Kbd> 发布 ·
          分隔条可拖拽，双击复位；两侧面板可折叠。
        </p>
      </PopoverContent>
    </Popover>
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
    <>
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
          <TooltipContent>与激活版本对比</TooltipContent>
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
    </>
  );
}

/** dock / 面板通用的小标题条：左边一组按钮，右边一个收起。 */
function PaneHeader({
  children,
  action
}: {
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex h-8 shrink-0 items-center gap-1 border-b bg-muted/30 px-1.5">
      {children}
      {action ? <div className="ml-auto flex items-center gap-1">{action}</div> : null}
    </div>
  );
}

function PaneTab({
  active,
  onClick,
  children
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1 text-xs transition-colors",
        active ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
      )}
    >
      {children}
    </button>
  );
}

const SEVERITY_META = {
  error: { icon: XCircle, tone: "text-destructive", label: "错误" },
  warning: { icon: AlertTriangle, tone: "text-amber-600 dark:text-amber-400", label: "提示" },
  info: { icon: Info, tone: "text-muted-foreground", label: "建议" }
} as const;

/**
 * 底部 dock：问题 / 版本。
 *
 * 编辑器上的波浪线只在那一行可见，而作者要的是「一共几处、分别在哪」；
 * 版本表则是回滚时唯一的入口。两者都属于「看一眼就回去继续写」，
 * 因此放在编辑器正下方而不是另一页。
 */
function BottomDock({
  tab,
  onTabChange,
  onCollapse,
  diagnostics,
  analysing,
  analysed,
  applying,
  onReveal,
  onGrant,
  versions,
  versionsLoading,
  activeVersion,
  onLoadSource,
  onDiffSource,
  onActivate,
  onDelete
}: {
  tab: DockTab;
  onTabChange: (tab: DockTab) => void;
  onCollapse: () => void;
  diagnostics: AppFunctionDiagnostic[];
  analysing: boolean;
  analysed: boolean;
  applying: boolean;
  onReveal: (line: number) => void;
  onGrant: (capabilities: string[]) => void;
  versions: FunctionVersion[];
  versionsLoading: boolean;
  activeVersion?: string;
  onLoadSource: (version: string) => void;
  onDiffSource: (version: string) => void;
  onActivate: (version: string) => void;
  onDelete: (version: string) => void;
}) {
  return (
    <>
      <PaneHeader
        action={
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon-xs" onClick={onCollapse} aria-label="收起">
                <ChevronDown className="size-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>收起面板</TooltipContent>
          </Tooltip>
        }
      >
        <PaneTab active={tab === "problems"} onClick={() => onTabChange("problems")}>
          问题
          {diagnostics.length ? (
            <Badge
              variant={diagnostics.some((item) => item.severity === "error") ? "danger" : "warning"}
              size="sm"
            >
              {diagnostics.length}
            </Badge>
          ) : null}
          {analysing ? <Loader2 className="size-3 animate-spin" /> : null}
        </PaneTab>
        <PaneTab active={tab === "versions"} onClick={() => onTabChange("versions")}>
          版本
          {versions.length ? (
            <Badge variant="outline" size="sm">
              {versions.length}
            </Badge>
          ) : null}
        </PaneTab>
      </PaneHeader>

      <div className="min-h-0 flex-1 overflow-auto">
        {tab === "problems" ? (
          <DiagnosticsList
            diagnostics={diagnostics}
            analysed={analysed}
            applying={applying}
            onReveal={onReveal}
            onGrant={onGrant}
          />
        ) : (
          <VersionTable
            versions={versions}
            loading={versionsLoading}
            activeVersion={activeVersion}
            showSource
            onLoadSource={onLoadSource}
            onDiffSource={onDiffSource}
            onActivate={onActivate}
            onDelete={onDelete}
          />
        )}
      </div>
    </>
  );
}

/**
 * 问题列表。
 *
 * 缺声明那一类带一个「补上」按钮 —— 后端已经算出该补哪几项，
 * 让作者自己把能力键抄到设置页去纯属多余。
 */
function DiagnosticsList({
  diagnostics,
  analysed,
  applying,
  onReveal,
  onGrant
}: {
  diagnostics: AppFunctionDiagnostic[];
  analysed: boolean;
  applying: boolean;
  onReveal: (line: number) => void;
  onGrant: (capabilities: string[]) => void;
}) {
  if (!diagnostics.length) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 p-4 text-xs text-muted-foreground">
        {analysed ? (
          <>
            <ShieldCheck className="size-4 text-emerald-600 dark:text-emerald-400" />
            <span>静态检查通过</span>
          </>
        ) : (
          <span>编写脚本后将自动执行静态检查</span>
        )}
      </div>
    );
  }
  return (
    <div className="divide-y">
      {diagnostics.map((diagnostic, index) => {
        const meta = SEVERITY_META[diagnostic.severity] ?? SEVERITY_META.info;
        const Icon = meta.icon;
        return (
          <div
            key={`${diagnostic.rule}-${index}`}
            className="flex items-start gap-2 px-2.5 py-1.5 text-xs hover:bg-muted/40"
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
                size="xs"
                variant="outline"
                disabled={applying}
                onClick={() => onGrant(diagnostic.capabilities as string[])}
              >
                {applying ? <Loader2 className="size-3 animate-spin" /> : <Plus className="size-3" />}
                声明能力
              </Button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

const NO_TEST_CASES: ScriptTestCase[] = [];

/** 试跑输入：input、身份、以及存下来的那几组用例。 */
function TestRunnerPane({
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
  const [caseOpen, setCaseOpen] = useState(false);
  // 空对象与「没有 properties 的对象」都不算约束 —— 后者在 JSON Schema
  // 语义下同样放行任何输入，把它当成「已配置」会让徽标撒谎。
  const hasSchema = Boolean(
    inputSchema && Object.keys((inputSchema.properties as object) ?? {}).length > 0
  );

  return (
    <>
      <PaneHeader
        action={
          <>
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge
                  variant="outline"
                  size="sm"
                  className={cn("gap-1 font-normal", !hasSchema && "text-muted-foreground")}
                >
                  <FileJson className="size-3" />
                  {hasSchema ? "按契约校验" : "未配置契约"}
                </Badge>
              </TooltipTrigger>
              <TooltipContent className="max-w-72">
                {hasSchema
                  ? "已配置入参契约，提供键名补全、枚举取值与必填校验"
                  : "未配置入参契约。在「设置 → 入参契约」中配置后，此处将提供补全与校验，编辑器内 ctx.input 同步获得字段类型"}
              </TooltipContent>
            </Tooltip>
            {inputSample ? (
              <Button
                size="xs"
                variant="ghost"
                title="按契约生成仅含必填字段的样例"
                onClick={() => onInputChange(inputSample)}
              >
                <Sparkles className="size-3" />
                生成样例
              </Button>
            ) : null}
          </>
        }
      >
        <span className="flex items-center gap-1.5 px-1 text-xs font-medium">
          <FlaskConical className="size-3.5" />
          试跑输入
        </span>
      </PaneHeader>

      <div className="flex min-h-0 flex-1 flex-col gap-1.5 p-1.5">
        {testCases.length ? (
          <div className="flex shrink-0 flex-wrap gap-1">
            {testCases.map((testCase) => (
              <span
                key={testCase.id}
                className="flex items-center gap-1 rounded-full border py-0.5 pl-2 pr-1 text-[11px]"
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
                  className="rounded-full text-muted-foreground hover:text-destructive"
                  onClick={() => removeTestCase(scope, testCase.id)}
                >
                  <XCircle className="size-3" />
                </button>
              </span>
            ))}
          </div>
        ) : null}

        {/* 配了契约就把它喂给 JSON 语言服务：键名补全、枚举值补全、
            必填校验、悬浮显示字段说明，全部是现成的。没配时退回纯语法检查。 */}
        <JsonEditor
          className="min-h-0 flex-1"
          height="100%"
          value={input}
          onChange={onInputChange}
          schema={hasSchema ? inputSchema : undefined}
        />

        <div className="flex shrink-0 items-center gap-1.5">
          <Tooltip>
            <TooltipTrigger asChild>
              <Input
                className="h-7 flex-1 text-xs"
                value={asUserId}
                onChange={(event) => onAsUserIdChange(event.target.value.replace(/\D/g, ""))}
                placeholder="以指定用户身份执行（用户 ID）"
                inputMode="numeric"
              />
            </TooltipTrigger>
            <TooltipContent side="top">
              未填写时，涉及用户身份的调用（如 aegis.user.get()）将失败
            </TooltipContent>
          </Tooltip>
          <Popover open={caseOpen} onOpenChange={setCaseOpen}>
            <PopoverTrigger asChild>
              <Button variant="outline" size="icon-sm" aria-label="保存为用例">
                <Save className="size-3.5" />
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-64 space-y-2">
              <Label className="text-xs">保存为用例（仅保存在本机）</Label>
              <Input
                className="h-8 text-xs"
                value={caseName}
                onChange={(event) => setCaseName(event.target.value)}
                placeholder="用例名称"
              />
              <Button
                size="sm"
                className="w-full"
                disabled={!caseName.trim()}
                onClick={() => {
                  addTestCase(scope, {
                    id: `${Date.now()}`,
                    name: caseName.trim(),
                    input,
                    asUserId
                  });
                  setCaseName("");
                  setCaseOpen(false);
                  toast.success("用例已保存");
                }}
              >
                保存
              </Button>
            </PopoverContent>
          </Popover>
          <Button size="sm" disabled={pending || disabled} onClick={() => onRun()}>
            {pending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <FlaskConical className="size-3.5" />
            )}
            试跑
          </Button>
        </div>

        {blocking > 0 ? (
          <p className="shrink-0 text-[11px] text-amber-600 dark:text-amber-400">
            静态检查存在 {blocking} 个错误：不影响试跑，发布将被拒绝。
          </p>
        ) : null}
      </div>
    </>
  );
}

function TestResultPane({
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
    <>
      <PaneHeader
        action={
          result?.logs.length ? (
            <Select value={logLevel} onValueChange={setLogLevel}>
              <SelectTrigger className="h-6 w-20 text-[11px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部日志</SelectItem>
                <SelectItem value="info">info</SelectItem>
                <SelectItem value="warn">warn</SelectItem>
                <SelectItem value="error">error</SelectItem>
              </SelectContent>
            </Select>
          ) : null
        }
      >
        <span className="px-1 text-xs font-medium">试跑结果</span>
        {result ? (
          <>
            <Badge variant={result.ok ? "success" : "danger"} size="sm">
              {result.ok ? "通过" : result.businessCode ? "被脚本拒绝" : "执行失败"}
            </Badge>
            <span className="truncate text-[11px] text-muted-foreground">
              {formatDuration(result.durationMs)} · SDK {result.sdkCalls} / 写 {result.sdkMutations}{" "}
              / 出站 {result.sdkFetches}
            </span>
          </>
        ) : null}
      </PaneHeader>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-2">
        {!result ? (
          <p className="py-8 text-center text-xs text-muted-foreground">
            暂无试跑结果，按 <Kbd>⌘/Ctrl</Kbd> <Kbd>↵</Kbd> 执行试跑。
          </p>
        ) : (
          <>
            {result.error ? (
              <div className="space-y-1 rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-xs">
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
                    定位到第 {result.errorLine} 行
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
                <p className="text-[11px] text-muted-foreground">返回值</p>
                <pre className="overflow-auto rounded-lg border bg-muted/40 p-2 font-mono text-[11px]">
                  {JSON.stringify(result.output, null, 2)}
                </pre>
              </div>
            ) : null}

            {result.logs.length ? (
              <div className="space-y-1">
                <p className="text-[11px] text-muted-foreground">日志（{result.logs.length} 行）</p>
                <div className="space-y-0.5 rounded-lg border bg-muted/40 p-1.5">
                  {logs.map((entry, index) => (
                    <div key={index} className="flex gap-2 font-mono text-[11px]">
                      {/* 相对耗时是脚本里唯一的计时手段：沙箱没有 timer，
                          「哪一步慢」只能靠日志之间的间隔看出来 */}
                      <span className="w-12 shrink-0 text-right text-muted-foreground">
                        {entry.elapsedMs.toFixed(1)}ms
                      </span>
                      <span
                        className={cn(
                          "w-8 shrink-0",
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
                    <p className="py-1.5 text-center text-[11px] text-muted-foreground">
                      当前级别无日志
                    </p>
                  ) : null}
                </div>
              </div>
            ) : null}

            <div className="space-y-1">
              <p className="text-[11px] text-muted-foreground">副作用</p>
              <EffectList effects={result.effects} />
            </div>
          </>
        )}
      </div>
    </>
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
  onPublished,
  onShowProblems
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  selected: AppFunction;
  source: string;
  blocking: AppFunctionDiagnostic[];
  suggestedVersion: string;
  onPublished: () => void;
  onShowProblems: () => void;
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
            版本发布后不可修改。发布前将进行语法、入口与能力声明检查。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          {blocking.length ? (
            <button
              type="button"
              className="w-full space-y-1 rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-left text-xs"
              onClick={() => {
                onShowProblems();
                onOpenChange(false);
              }}
            >
              <p className="font-medium text-destructive">静态检查未通过，无法发布：</p>
              {blocking.slice(0, 3).map((diagnostic, index) => (
                <p key={index} className="text-muted-foreground">
                  第 {diagnostic.line} 行：{diagnostic.message}
                </p>
              ))}
              {blocking.length > 3 ? (
                <p className="text-muted-foreground">…另有 {blocking.length - 3} 处</p>
              ) : null}
              <p className="text-muted-foreground">查看问题列表</p>
            </button>
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
              placeholder="描述本次变更，供回滚时参考"
            />
          </div>
          <label className="flex items-center justify-between rounded-lg border p-2.5 text-sm">
            <span>
              发布后立即激活
              <span className="mt-0.5 block text-[11px] text-muted-foreground">
                关闭后版本仅保存，线上仍运行当前激活版本
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

type FunctionVersion = {
  id: number;
  version: string;
  status: string;
  notes: string;
  sourceBytes: number;
  endpointUrl?: string;
  artifactSha256: string;
  createdAt: string;
};

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
  versions: FunctionVersion[];
  loading?: boolean;
  activeVersion?: string;
  showSource?: boolean;
  onLoadSource?: (version: string) => void;
  onDiffSource?: (version: string) => void;
  onActivate: (version: string) => void;
  onDelete: (version: string) => void;
}) {
  return (
    <Table className="text-xs">
      <TableHeader>
        <TableRow>
          <TableHead className="h-8">版本</TableHead>
          <TableHead className="h-8">状态</TableHead>
          <TableHead className="h-8">说明</TableHead>
          <TableHead className="h-8">产物</TableHead>
          <TableHead className="h-8">发布时间</TableHead>
          <TableHead className="h-8" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {versions.map((version) => (
          <TableRow key={version.id}>
            <TableCell className="py-1.5 font-mono">{version.version}</TableCell>
            <TableCell className="py-1.5">
              <Badge variant={version.status === "active" ? "success" : "outline"} size="sm">
                {version.status}
              </Badge>
            </TableCell>
            <TableCell className="max-w-60 truncate py-1.5 text-muted-foreground">
              {version.notes || "—"}
            </TableCell>
            <TableCell className="py-1.5 text-muted-foreground">
              {version.endpointUrl
                ? version.endpointUrl
                : version.sourceBytes > 0
                  ? formatBytes(version.sourceBytes)
                  : "WASM"}
              <span className="ml-1.5 font-mono text-[10px]">
                {version.artifactSha256.slice(0, 8)}
              </span>
            </TableCell>
            <TableCell className="py-1.5 text-muted-foreground">
              {formatTime(version.createdAt)}
            </TableCell>
            <TableCell className="py-1.5 text-right">
              <div className="flex justify-end gap-0.5">
                {showSource && onDiffSource ? (
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    title="与编辑器正文对比"
                    aria-label={`对比版本 ${version.version}`}
                    onClick={() => onDiffSource(version.version)}
                  >
                    <GitCompare className="size-3.5" />
                  </Button>
                ) : null}
                {showSource && onLoadSource ? (
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    title="载入编辑器"
                    aria-label={`载入版本 ${version.version}`}
                    onClick={() => onLoadSource(version.version)}
                  >
                    <History className="size-3.5" />
                  </Button>
                ) : null}
                {version.version !== activeVersion ? (
                  <>
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      title="激活此版本"
                      aria-label={`激活版本 ${version.version}`}
                      onClick={() => onActivate(version.version)}
                    >
                      <CheckCircle2 className="size-3.5" />
                    </Button>
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      title="删除此版本"
                      aria-label={`删除版本 ${version.version}`}
                      onClick={() => onDelete(version.version)}
                    >
                      <Trash2 className="size-3.5 text-destructive" />
                    </Button>
                  </>
                ) : null}
              </div>
            </TableCell>
          </TableRow>
        ))}
        {!versions.length ? (
          <TableRow>
            <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
              {loading ? "加载中…" : "暂无版本"}
            </TableCell>
          </TableRow>
        ) : null}
      </TableBody>
    </Table>
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
  versions: FunctionVersion[];
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
      <div className="space-y-3 rounded-xl border p-4">
        <div>
          <h3 className="text-sm font-medium">发布版本</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {isHttp
              ? "Endpoint 仅允许 HTTPS，禁止重定向，并在实际连接时重新解析 IP 拒绝内网与元数据地址。"
              : "WASM 最大 2MB，每次调用独立实例，内存上限 16MB，不提供网络与文件系统。"}
          </p>
        </div>
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
                响应需由 Worker 签名，Aegis 使用此公钥验签。
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
      </div>

      <div className="overflow-hidden rounded-xl border">
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
    </div>
  );
}
