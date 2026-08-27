"use client";

import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import Link from "next/link";
import { Chat, useChat } from "@ai-sdk/react";
import { DefaultChatTransport } from "ai";
import type {
  ChatOnDataCallback,
  ChatOnFinishCallback,
  ChatStatus,
  DynamicToolUIPart,
  PrepareSendMessagesRequest,
  UIMessage
} from "ai";
import {
  Bot,
  Brain,
  CheckCircle2,
  ChevronDown,
  FileCode2,
  History,
  Loader2,
  Maximize2,
  Minimize2,
  Plus,
  SendHorizontal,
  Settings2,
  ShieldCheck,
  Square,
  Trash2,
  X,
  XCircle
} from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { aiAgentStreamPath, getAIConversationDetail, type AIAgentMessage } from "@/lib/api/ai";
import { joinApiUrl } from "@/lib/api/client";
import {
  useAIChannelQuery,
  useAIConversationsQuery,
  useDeleteAIConversationMutation
} from "@/lib/ai-hooks";
import { useAdminToken } from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { formatTime } from "./function-shared";

/**
 * 函数工作台的 AI 助手：一个真正会**动手**的 Agent，而不是聊天窗。
 *
 * 后端跑的是「模型 → 工具 → 模型」的循环（见 ai_agent_service.go）：
 * 它能读函数定义与版本、查能力目录与 SDK 类型、做静态检查、试跑（读真写假），
 * 并通过 stage_source 把完整脚本放进左侧编辑器 —— 这里收到那个工具的结果后
 * 直接调 onApplySource 落到草稿，作者看到的是「代码自己出现在编辑器里」。
 *
 * **两种视图，一份对话**：侧栏面板适合「边写边问」，全屏对话适合长回答与回放。
 * 消息与流式状态都在组件外的 Chat 实例上（useChat 只是订阅），因此全屏开关、
 * 面板折叠都不会打断正在进行的流；两个视图渲染的是同一份 messages。
 */

type AgentMetadata = {
  conversationId?: number;
  usage?: { inputTokens: number; outputTokens: number; totalTokens: number };
};

type AgentUIMessage = UIMessage<AgentMetadata>;

/** 内置工具的界面名。不认识的键（MCP 工具）走 toolLabel 的兜底分支。 */
const TOOL_LABELS: Record<string, string> = {
  list_functions: "列出函数",
  get_function: "读取函数定义",
  get_function_source: "读取脚本正文",
  list_versions: "查看版本历史",
  get_capability_catalog: "查能力目录",
  get_sdk_reference: "查 SDK 类型",
  list_script_templates: "查脚本模板",
  analyze_draft: "静态检查",
  test_draft: "试跑脚本",
  stage_source: "更新编辑器草稿",
  get_invocations: "查调用审计",
  get_invocation_stats: "查运行统计",
  browse_kv: "浏览 KV 存储",
  create_function: "创建函数",
  update_function_settings: "更新函数设置",
  publish_version: "发布版本"
};

function toolLabel(name: string): string {
  if (TOOL_LABELS[name]) return TOOL_LABELS[name];
  if (name.startsWith("mcp__")) {
    const [server, tool] = name.slice(5).split("__");
    return tool ? `MCP · ${server} · ${tool}` : `MCP · ${server}`;
  }
  return name;
}

const SUGGESTIONS = ["解释当前脚本在做什么", "静态检查并修复所有问题", "试跑一次并解决报错"];

/** "auto" 或 `${configId}::${model}`。 */
function parseModelChoice(choice: string): { configId: number; model: string } {
  if (choice === "auto") return { configId: 0, model: "" };
  const sep = choice.indexOf("::");
  if (sep < 0) return { configId: 0, model: "" };
  return { configId: Number(choice.slice(0, sep)) || 0, model: choice.slice(sep + 2) };
}

/** 传输层抛的错误可能是后端 JSON 信封的原文，取里面的 message 给人看。 */
function chatErrorText(error: Error): string {
  const raw = error.message || "对话失败";
  try {
    const parsed = JSON.parse(raw) as { message?: string; errorText?: string };
    if (typeof parsed?.message === "string" && parsed.message) return parsed.message;
    if (typeof parsed?.errorText === "string" && parsed.errorText) return parsed.errorText;
  } catch {
    // 不是 JSON，原样展示
  }
  return raw;
}

/** 服务端落库的消息 → useChat 的 UIMessage。分片形状本来就按 AI SDK 存的，直接投影。 */
function toUIMessages(items: AIAgentMessage[]): AgentUIMessage[] {
  const out: AgentUIMessage[] = [];
  for (const item of items) {
    if (item.role !== "user" && item.role !== "assistant") continue;
    const parts = (Array.isArray(item.parts) ? item.parts : []) as AgentUIMessage["parts"];
    if (!parts.length) continue;
    out.push({
      id: `db-${item.id}`,
      role: item.role,
      parts,
      metadata: item.usage ? { usage: item.usage } : undefined
    });
  }
  return out;
}

type AgentChatHandlers = {
  prepare: PrepareSendMessagesRequest<AgentUIMessage>;
  finish: ChatOnFinishCallback<AgentUIMessage>;
  notice: ChatOnDataCallback<AgentUIMessage>;
};

/**
 * Chat 实例只建一次（重建等于把正在流式的对话掐断），回调经由 ref 转发到
 * 每次渲染都会刷新的处理函数上。工厂放在组件外：渲染期不允许读写 ref
 * （react-hooks/refs），而这里的 `.current` 只会在发消息/流结束时被碰到。
 */
function createAgentChat(appKey: string, handlersRef: RefObject<AgentChatHandlers>) {
  return new Chat<AgentUIMessage>({
    transport: new DefaultChatTransport<AgentUIMessage>({
      api: joinApiUrl(aiAgentStreamPath(appKey)),
      prepareSendMessagesRequest: (options) => handlersRef.current.prepare(options)
    }),
    onFinish: (event) => handlersRef.current.finish(event),
    onData: (part) => handlersRef.current.notice(part)
  });
}

/** 从一条助手消息里找最后一次成功的 stage_source（回放「AI 写的最终版」用）。 */
function findStagedSource(message: AgentUIMessage): { source: string; note?: string } | null {
  for (let i = message.parts.length - 1; i >= 0; i--) {
    const part = message.parts[i];
    if (part.type !== "dynamic-tool") continue;
    if (part.toolName !== "stage_source" || part.state !== "output-available") continue;
    const input = part.input as { source?: string; note?: string } | undefined;
    if (typeof input?.source === "string" && input.source.trim()) {
      return { source: input.source, note: input.note };
    }
  }
  return null;
}

export function FunctionAIAssistant({
  appKey,
  functionName,
  draftSource,
  onApplySource,
  onCollapse
}: {
  appKey: string;
  functionName: string;
  /** 编辑器当前草稿：随消息一起发出去，Agent 的检查/试跑缺省作用于它。 */
  draftSource: string;
  /** stage_source 的落点：把 AI 交付的完整脚本写进编辑器草稿。 */
  onApplySource: (source: string, note?: string) => void;
  onCollapse: () => void;
}) {
  const token = useAdminToken();
  const queryClient = useQueryClient();

  const [input, setInput] = useState("");
  const [conversationId, setConversationId] = useState(0);
  const [modelChoice, setModelChoice] = useState("auto");
  const [disableWrites, setDisableWrites] = useState(false);
  const [loadingConversation, setLoadingConversation] = useState(false);
  // 全屏对话：侧栏写代码时够用，长回答/回放历史时这块面板确实太挤。
  const [expanded, setExpanded] = useState(false);

  const channelQuery = useAIChannelQuery({ kind: "app", appKey });
  const channel = useMemo(() => channelQuery.data ?? [], [channelQuery.data]);
  const noChannel = channelQuery.isSuccess && channel.length === 0;

  const conversationsQuery = useAIConversationsQuery(appKey, "function", functionName);
  const conversations = conversationsQuery.data ?? [];
  const deleteConversation = useDeleteAIConversationMutation(appKey);

  // 发请求那一刻要读的全部现场。Chat 只建一次（重建等于把流重置），
  // 回调经由 ref 读最新值 —— 否则闭包里永远是首渲染的草稿与会话号。
  // ref 的同步放在渲染后的 effect 里（渲染期写 ref 是 react-hooks/refs 禁的）。
  const stateRef = useRef({ token, conversationId, draftSource, functionName, modelChoice, disableWrites });
  const applyRef = useRef(onApplySource);

  const prepare: PrepareSendMessagesRequest<AgentUIMessage> = ({ messages }) => {
    const current = stateRef.current;
    const last = messages[messages.length - 1];
    const text = (last?.parts ?? [])
      .map((part) => (part.type === "text" ? part.text : ""))
      .filter(Boolean)
      .join("\n");
    const choice = parseModelChoice(current.modelChoice);
    return {
      headers: current.token
        ? { Authorization: `Bearer ${current.token}`, "X-Admin-Token": current.token }
        : undefined,
      body: {
        conversationId: current.conversationId,
        scene: "function",
        ref: current.functionName,
        message: text,
        draftSource: current.draftSource,
        configId: choice.configId,
        model: choice.model,
        disableWrites: current.disableWrites
      }
    };
  };

  const finish: ChatOnFinishCallback<AgentUIMessage> = ({ message }) => {
    const meta = message.metadata;
    if (meta?.conversationId && meta.conversationId !== stateRef.current.conversationId) {
      setConversationId(meta.conversationId);
    }
    void queryClient.invalidateQueries({ queryKey: ["ai", "conversations", appKey] });
    // AI 交付脚本的唯一通道：最后一次成功的 stage_source 直接落进编辑器。
    const staged = findStagedSource(message);
    if (staged) {
      applyRef.current(staged.source, staged.note);
      toast.success(staged.note ? `AI 已更新编辑器草稿：${staged.note}` : "AI 已更新编辑器草稿");
    }
  };

  const notice: ChatOnDataCallback<AgentUIMessage> = (part) => {
    if (part.type !== "data-notice") return;
    const data = part.data as { kind?: string; server?: string; error?: string; messages?: number };
    if (data?.kind === "compacted") {
      toast.info(`历史对话较长，已自动压缩 ${data.messages ?? "部分"} 条旧消息`);
    } else if (data?.kind === "mcp-unreachable") {
      toast.warning(`MCP 服务器「${data.server ?? "?"}」连接失败，本轮不带它的工具`, {
        description: data.error
      });
    }
  };

  const handlersRef = useRef<AgentChatHandlers>({ prepare, finish, notice });

  useEffect(() => {
    stateRef.current = { token, conversationId, draftSource, functionName, modelChoice, disableWrites };
    applyRef.current = onApplySource;
    handlersRef.current = { prepare, finish, notice };
  });

  // 工厂只是把 ref 存进闭包，`.current` 要到发消息/流结束才被读 —— 不在渲染期。
  // eslint-disable-next-line react-hooks/refs
  const chat = useMemo(() => createAgentChat(appKey, handlersRef), [appKey]);

  const { messages, sendMessage, regenerate, stop, status, error, setMessages, clearError } =
    useChat<AgentUIMessage>({ chat });

  const busy = status === "submitted" || status === "streaming";

  function submitText(text: string) {
    const trimmed = text.trim();
    if (!trimmed || busy || !token || noChannel) return;
    void sendMessage({ text: trimmed });
    setInput("");
  }

  function startNewConversation() {
    void stop();
    setMessages([]);
    setConversationId(0);
    clearError();
  }

  async function openConversation(id: number) {
    if (!token || id === conversationId) return;
    await stop();
    setLoadingConversation(true);
    try {
      const detail = await getAIConversationDetail(token, appKey, id);
      setMessages(toUIMessages(detail.messages));
      setConversationId(id);
      clearError();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "载入会话失败");
    } finally {
      setLoadingConversation(false);
    }
  }

  async function removeConversation(id: number) {
    try {
      await deleteConversation.mutateAsync(id);
      if (id === conversationId) startNewConversation();
      toast.success("会话已删除");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "删除失败");
    }
  }

  const modelOptions = useMemo(() => {
    const seen = new Set<string>();
    const options: { value: string; label: string }[] = [];
    for (const item of channel) {
      const models = item.models?.length ? item.models : [item.model];
      for (const model of models) {
        if (!model) continue;
        const value = `${item.configId}::${model}`;
        if (seen.has(value)) continue;
        seen.add(value);
        options.push({ value, label: `${item.configName} · ${model}` });
      }
    }
    return options;
  }, [channel]);

  const modelHint =
    modelChoice === "auto"
      ? channel.length
        ? `通道链路 · 首选 ${channel[0].configName}`
        : ""
      : modelOptions.find((option) => option.value === modelChoice)?.label ?? "";

  // 头部动作在两种视图里完全一致，只差「放大 / 还原」按钮的方向。
  const headerActions = (dense: boolean) => (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-xs" aria-label="历史会话">
                <History className="size-3.5" />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>历史会话</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="w-72">
          <DropdownMenuLabel>历史会话</DropdownMenuLabel>
          {conversations.length ? (
            conversations.map((item) => (
              <DropdownMenuItem
                key={item.id}
                className={cn("gap-2", item.id === conversationId && "bg-accent")}
                onSelect={() => void openConversation(item.id)}
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-xs">{item.title || `会话 #${item.id}`}</span>
                  <span className="block text-[10px] text-muted-foreground">
                    {formatTime(item.updatedAt)}
                    {item.compactions > 0 ? ` · 压缩 ${item.compactions} 次` : ""}
                  </span>
                </span>
                <button
                  type="button"
                  aria-label={`删除会话 ${item.title || item.id}`}
                  className="rounded p-0.5 text-muted-foreground hover:text-destructive"
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void removeConversation(item.id);
                  }}
                >
                  <Trash2 className="size-3" />
                </button>
              </DropdownMenuItem>
            ))
          ) : (
            <div className="px-2 py-3 text-center text-[11px] text-muted-foreground">暂无历史会话</div>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={startNewConversation}>
            <Plus className="size-3.5" />
            新对话
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-xs" onClick={startNewConversation} aria-label="新对话">
            <Plus className="size-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>新对话</TooltipContent>
      </Tooltip>
      <SettingsPopover
        modelChoice={modelChoice}
        onModelChange={setModelChoice}
        modelOptions={modelOptions}
        disableWrites={disableWrites}
        onDisableWritesChange={setDisableWrites}
      />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => setExpanded(!dense)}
            aria-label={dense ? "全屏对话" : "退出全屏"}
          >
            {dense ? <Maximize2 className="size-3.5" /> : <Minimize2 className="size-3.5" />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{dense ? "全屏对话" : "退出全屏"}</TooltipContent>
      </Tooltip>
      {dense ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-xs" onClick={onCollapse} aria-label="收起 AI 助手">
              <X className="size-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>收起</TooltipContent>
        </Tooltip>
      ) : null}
    </>
  );

  const chatBody = (dense: boolean) => (
    <ChatBody
      dense={dense}
      appKey={appKey}
      messages={messages}
      status={status}
      error={error}
      loadingConversation={loadingConversation}
      noChannel={noChannel}
      channelReady={channelQuery.isSuccess}
      busy={busy}
      onPickSuggestion={submitText}
      onApplySource={(source, note) => applyRef.current(source, note)}
      onRegenerate={() => void regenerate()}
      onClearError={clearError}
    />
  );

  const composer = (dense: boolean) => (
    <Composer
      dense={dense}
      input={input}
      onInputChange={setInput}
      onSubmit={() => submitText(input)}
      onStop={() => void stop()}
      busy={busy}
      disabled={noChannel || !token}
      modelHint={modelHint}
      modelChoice={modelChoice}
      onModelChange={setModelChoice}
      modelOptions={modelOptions}
      disableWrites={disableWrites}
      onDisableWritesChange={setDisableWrites}
    />
  );

  return (
    <>
      {/* ── 侧栏视图 ── */}
      <div className="flex h-8 shrink-0 items-center gap-1 border-b bg-muted/30 px-1.5">
        <span className="flex items-center gap-1.5 px-1 text-xs font-medium">
          <Bot className="size-3.5" />
          AI 助手
        </span>
        {disableWrites ? (
          <Badge variant="outline" size="sm" className="gap-1 font-normal text-muted-foreground">
            <ShieldCheck className="size-3" />
            只读
          </Badge>
        ) : null}
        <div className="ml-auto flex items-center gap-0.5">{headerActions(true)}</div>
      </div>
      {chatBody(true)}
      {composer(true)}

      {/* ── 全屏视图：同一个 Chat 实例的另一个投影，开关不打断流 ── */}
      <Dialog open={expanded} onOpenChange={setExpanded}>
        <DialogContent
          showCloseButton={false}
          aria-describedby={undefined}
          className="flex h-[88vh] w-[min(1080px,calc(100vw-2rem))] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none"
        >
          <div className="flex h-12 shrink-0 items-center gap-2 border-b bg-muted/30 px-4">
            <DialogTitle className="flex items-center gap-2 text-sm font-medium">
              <Bot className="size-4" />
              AI 助手
              <span className="font-normal text-muted-foreground">· {functionName}</span>
            </DialogTitle>
            {disableWrites ? (
              <Badge variant="outline" size="sm" className="gap-1 font-normal text-muted-foreground">
                <ShieldCheck className="size-3" />
                只读
              </Badge>
            ) : null}
            <div className="ml-auto flex items-center gap-0.5">{headerActions(false)}</div>
          </div>
          {chatBody(false)}
          {composer(false)}
        </DialogContent>
      </Dialog>
    </>
  );
}

/**
 * 消息区。面板与全屏各挂一个实例（消息数据同源），滚动状态各自独立：
 * 流式期间贴底滚动；用户往上翻过就不再打扰（离底 96px 内算贴底）。
 */
function ChatBody({
  dense,
  appKey,
  messages,
  status,
  error,
  loadingConversation,
  noChannel,
  channelReady,
  busy,
  onPickSuggestion,
  onApplySource,
  onRegenerate,
  onClearError
}: {
  dense: boolean;
  appKey: string;
  messages: AgentUIMessage[];
  status: ChatStatus;
  error: Error | undefined;
  loadingConversation: boolean;
  noChannel: boolean;
  channelReady: boolean;
  busy: boolean;
  onPickSuggestion: (text: string) => void;
  onApplySource: (source: string, note?: string) => void;
  onRegenerate: () => void;
  onClearError: () => void;
}) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const stickRef = useRef(true);
  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickRef.current) el.scrollTop = el.scrollHeight;
  }, [messages, status]);

  return (
    <div
      ref={scrollRef}
      className={cn("min-h-0 flex-1 overflow-y-auto", dense ? "px-2.5 py-2" : "px-4 py-4")}
      onScroll={(event) => {
        const el = event.currentTarget;
        stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 96;
      }}
    >
      <div className={cn("space-y-3", !dense && "mx-auto w-full max-w-3xl space-y-4")}>
        {loadingConversation ? (
          <div className="flex items-center gap-1.5 py-8 text-[11px] text-muted-foreground">
            <Loader2 className="size-3 animate-spin" />
            正在载入会话…
          </div>
        ) : null}

        {!loadingConversation && messages.length === 0 ? (
          noChannel ? (
            <EmptyChannelHint appKey={appKey} />
          ) : (
            <WelcomeHint dense={dense} onPick={onPickSuggestion} disabled={busy || !channelReady} />
          )
        ) : null}

        {messages.map((message) => (
          <MessageRow key={message.id} dense={dense} message={message} onApplySource={onApplySource} />
        ))}

        {status === "submitted" ? (
          <div
            className={cn(
              "flex items-center gap-1.5 text-muted-foreground",
              dense ? "text-[11px]" : "text-xs"
            )}
          >
            <Loader2 className="size-3 animate-spin" />
            正在思考…
          </div>
        ) : null}

        {error ? (
          <div className="space-y-1 rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-[11px]">
            <p className="text-destructive">{chatErrorText(error)}</p>
            <div className="flex gap-1">
              <Button size="xs" variant="outline" onClick={onRegenerate}>
                重试
              </Button>
              <Button size="xs" variant="ghost" onClick={onClearError}>
                忽略
              </Button>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

/**
 * 输入区。全屏视图把模型与只读开关放到明面上（侧栏里收在设置弹层，
 * 因为那点宽度只够给输入框）。
 */
function Composer({
  dense,
  input,
  onInputChange,
  onSubmit,
  onStop,
  busy,
  disabled,
  modelHint,
  modelChoice,
  onModelChange,
  modelOptions,
  disableWrites,
  onDisableWritesChange
}: {
  dense: boolean;
  input: string;
  onInputChange: (value: string) => void;
  onSubmit: () => void;
  onStop: () => void;
  busy: boolean;
  disabled: boolean;
  modelHint: string;
  modelChoice: string;
  onModelChange: (value: string) => void;
  modelOptions: { value: string; label: string }[];
  disableWrites: boolean;
  onDisableWritesChange: (value: boolean) => void;
}) {
  const textarea = (
    <Textarea
      value={input}
      onChange={(event) => onInputChange(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
          event.preventDefault();
          onSubmit();
        }
      }}
      placeholder={
        disabled
          ? "请先配置 AI 通道"
          : "描述需求：AI 会读代码、查文档、试跑，并把脚本直接写进编辑器（Enter 发送，Shift+Enter 换行）"
      }
      disabled={disabled}
      autoFocus={!dense}
      className={cn(
        "resize-none",
        dense ? "max-h-36 min-h-16 text-xs" : "max-h-56 min-h-24 border-0 p-0 text-sm shadow-none focus-visible:ring-0"
      )}
    />
  );

  const sendOrStop = busy ? (
    <Button size="sm" variant="secondary" onClick={onStop}>
      <Square className="size-3.5" />
      停止
    </Button>
  ) : (
    <Button size="sm" disabled={!input.trim() || disabled} onClick={onSubmit}>
      <SendHorizontal className="size-3.5" />
      发送
    </Button>
  );

  if (dense) {
    return (
      <div className="shrink-0 space-y-1.5 border-t p-2">
        {textarea}
        <div className="flex items-center gap-1.5">
          <p className="min-w-0 flex-1 truncate text-[10px] text-muted-foreground">{modelHint}</p>
          {sendOrStop}
        </div>
      </div>
    );
  }

  return (
    <div className="shrink-0 border-t px-4 py-3">
      <div className="mx-auto w-full max-w-3xl rounded-xl border bg-background p-3 shadow-sm focus-within:border-ring">
        {textarea}
        <div className="mt-2 flex items-center gap-2">
          <Select value={modelChoice} onValueChange={onModelChange}>
            <SelectTrigger size="sm" className="h-7 w-auto max-w-64 gap-1 border-0 bg-muted/50 px-2 text-xs shadow-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">自动（按通道链路）</SelectItem>
              {modelOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant={disableWrites ? "secondary" : "ghost"}
                size="icon-sm"
                aria-pressed={disableWrites}
                aria-label="只读模式"
                onClick={() => onDisableWritesChange(!disableWrites)}
              >
                <ShieldCheck className="size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              只读模式{disableWrites ? "（已开）" : ""}：关闭建函数、改设置、发版这类写操作
            </TooltipContent>
          </Tooltip>
          <div className="ml-auto">{sendOrStop}</div>
        </div>
      </div>
    </div>
  );
}

/** 模型与安全档位。侧栏视图的入口：那点宽度放不下明面控件。 */
function SettingsPopover({
  modelChoice,
  onModelChange,
  modelOptions,
  disableWrites,
  onDisableWritesChange
}: {
  modelChoice: string;
  onModelChange: (value: string) => void;
  modelOptions: { value: string; label: string }[];
  disableWrites: boolean;
  onDisableWritesChange: (value: boolean) => void;
}) {
  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="icon-xs" aria-label="助手设置">
              <Settings2 className="size-3.5" />
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>模型与权限</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" className="w-72 space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs">模型</Label>
          <Select value={modelChoice} onValueChange={onModelChange}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">自动（按通道链路）</SelectItem>
              {modelOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-[11px] text-muted-foreground">
            「自动」按应用的通道链路取首个可用通道；也可钉死某个通道与型号。
          </p>
        </div>
        <label className="flex items-center justify-between gap-2 rounded-lg border p-2 text-xs">
          <span>
            只读模式
            <span className="mt-0.5 block text-[11px] font-normal text-muted-foreground">
              关闭建函数、改设置、发版这类写操作；AI 仍可读取、检查、试跑与写草稿。
            </span>
          </span>
          <Switch checked={disableWrites} onCheckedChange={onDisableWritesChange} />
        </label>
      </PopoverContent>
    </Popover>
  );
}

function WelcomeHint({
  dense,
  onPick,
  disabled
}: {
  dense: boolean;
  onPick: (text: string) => void;
  disabled: boolean;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center gap-3 text-center", dense ? "h-full p-4" : "py-20")}>
      <Bot className={cn("text-muted-foreground/40", dense ? "size-8" : "size-10")} />
      <div className="space-y-1">
        <p className={cn("font-medium", dense ? "text-xs" : "text-sm")}>让 AI 帮你写这个函数</p>
        <p className={cn("leading-relaxed text-muted-foreground", dense ? "text-[11px]" : "max-w-md text-xs")}>
          它能读函数定义与历史版本、查能力目录、做静态检查、试跑验证，
          并把完整脚本直接写进左侧编辑器草稿。
        </p>
      </div>
      <div className="flex flex-wrap justify-center gap-1.5">
        {SUGGESTIONS.map((text) => (
          <Button
            key={text}
            variant="outline"
            size={dense ? "xs" : "sm"}
            disabled={disabled}
            onClick={() => onPick(text)}
          >
            {text}
          </Button>
        ))}
      </div>
    </div>
  );
}

function EmptyChannelHint({ appKey }: { appKey: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-4 py-16 text-center">
      <Bot className="size-8 text-muted-foreground/40" />
      <div className="space-y-1">
        <p className="text-xs font-medium">尚未配置 AI 通道</p>
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          在应用的「AI 服务」里添加供应商配置（或由平台在系统配置中共享通道）后，这里即可使用。
        </p>
      </div>
      <Button asChild size="xs" variant="outline">
        <Link href={`/apps/${encodeURIComponent(appKey)}?tab=ai`}>去配置</Link>
      </Button>
    </div>
  );
}

function MessageRow({
  dense,
  message,
  onApplySource
}: {
  dense: boolean;
  message: AgentUIMessage;
  onApplySource: (source: string, note?: string) => void;
}) {
  if (message.role === "user") {
    const text = message.parts
      .map((part) => (part.type === "text" ? part.text : ""))
      .filter(Boolean)
      .join("\n");
    return (
      <div className="flex justify-end">
        <div
          className={cn(
            "whitespace-pre-wrap break-words rounded-lg bg-primary/10",
            dense ? "max-w-[88%] px-2.5 py-1.5 text-xs" : "max-w-[76%] rounded-xl px-3.5 py-2 text-sm"
          )}
        >
          {text}
        </div>
      </div>
    );
  }

  const usage = message.metadata?.usage;
  const body = (
    <div className={cn("min-w-0 flex-1", dense ? "space-y-1.5" : "space-y-2")}>
      {message.parts.map((part, index) => {
        switch (part.type) {
          case "text":
            return part.text ? (
              <div
                key={index}
                className={cn(
                  "whitespace-pre-wrap break-words leading-relaxed",
                  dense ? "text-xs" : "text-sm"
                )}
              >
                {part.text}
              </div>
            ) : null;
          case "reasoning":
            return part.text ? (
              <ReasoningView key={index} text={part.text} streaming={part.state === "streaming"} />
            ) : null;
          case "dynamic-tool":
            return <ToolPartView key={index} dense={dense} part={part} onApplySource={onApplySource} />;
          default:
            return null;
        }
      })}
      {usage && usage.totalTokens > 0 ? (
        <p className="text-[10px] text-muted-foreground">
          tokens：输入 {usage.inputTokens} · 输出 {usage.outputTokens}
        </p>
      ) : null}
    </div>
  );

  // 全屏视图有富余宽度，给助手消息配一个头像列，长对话里角色一眼可辨。
  if (dense) return body;
  return (
    <div className="flex gap-3">
      <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border bg-muted/50">
        <Bot className="size-3.5 text-muted-foreground" />
      </span>
      {body}
    </div>
  );
}

function ReasoningView({ text, streaming }: { text: string; streaming: boolean }) {
  return (
    <Collapsible>
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className="flex items-center gap-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
        >
          {streaming ? <Loader2 className="size-3 animate-spin" /> : <Brain className="size-3" />}
          {streaming ? "思考中…" : "思考过程"}
          <ChevronDown className="size-3" />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 whitespace-pre-wrap break-words rounded-lg border bg-muted/30 p-2 text-[11px] leading-relaxed text-muted-foreground">
          {text}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

/** 工具结果里可能是长 JSON，展示前统一 stringify + 截断，别把消息区撑爆。 */
function JsonBlock({ title, value }: { title: string; value: unknown }) {
  const text = useMemo(() => {
    if (value === undefined) return "";
    try {
      const encoded = typeof value === "string" ? value : JSON.stringify(value, null, 2);
      return encoded.length > 4000 ? `${encoded.slice(0, 4000)}\n…（已截断）` : encoded;
    } catch {
      return String(value);
    }
  }, [value]);
  if (!text) return null;
  return (
    <div className="space-y-0.5">
      <p className="text-[10px] text-muted-foreground">{title}</p>
      <pre className="max-h-44 overflow-auto rounded border bg-background/60 p-1.5 font-mono text-[10px] leading-relaxed">
        {text}
      </pre>
    </div>
  );
}

function ToolPartView({
  dense,
  part,
  onApplySource
}: {
  dense: boolean;
  part: DynamicToolUIPart;
  onApplySource: (source: string, note?: string) => void;
}) {
  const running = part.state === "input-streaming" || part.state === "input-available";
  const failed = part.state === "output-error";

  // stage_source 是「AI 交付代码」的动作，值得一个专属外观：
  // 成功即已自动落进编辑器，这里保留「重新应用」给回放旧会话的场景。
  if (part.toolName === "stage_source") {
    const input = part.input as { source?: string; note?: string } | undefined;
    const source = typeof input?.source === "string" ? input.source : "";
    const note = typeof input?.note === "string" ? input.note : "";
    return (
      <div className="rounded-lg border bg-muted/20 px-2 py-1.5">
        <div className={cn("flex items-center gap-1.5", dense ? "text-[11px]" : "text-xs")}>
          {running ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
          ) : failed ? (
            <XCircle className="size-3.5 shrink-0 text-destructive" />
          ) : (
            <FileCode2 className="size-3.5 shrink-0 text-primary" />
          )}
          <span className="font-medium">
            {running ? "正在编写脚本…" : failed ? "写入草稿失败" : "已更新编辑器草稿"}
          </span>
          {!running && !failed && source ? (
            <Button
              size="xs"
              variant="ghost"
              className="ml-auto h-5 px-1.5"
              onClick={() => onApplySource(source, note)}
            >
              重新应用
            </Button>
          ) : null}
        </div>
        {note ? (
          <p className={cn("mt-0.5 text-muted-foreground", dense ? "text-[11px]" : "text-xs")}>{note}</p>
        ) : null}
        {failed && part.errorText ? (
          <p className="mt-0.5 text-[11px] text-destructive">{part.errorText}</p>
        ) : null}
      </div>
    );
  }

  return (
    <Collapsible className="rounded-lg border bg-muted/20">
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className={cn(
            "flex w-full items-center gap-1.5 px-2 py-1.5 text-left",
            dense ? "text-[11px]" : "text-xs"
          )}
        >
          {running ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
          ) : failed ? (
            <XCircle className="size-3.5 shrink-0 text-destructive" />
          ) : (
            <CheckCircle2 className="size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
          )}
          <span className="min-w-0 truncate font-medium">{toolLabel(part.toolName)}</span>
          <ChevronDown className="ml-auto size-3 shrink-0 text-muted-foreground" />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="space-y-1.5 border-t px-2 py-1.5">
          {part.input !== undefined ? <JsonBlock title="入参" value={part.input} /> : null}
          {part.state === "output-available" ? <JsonBlock title="结果" value={part.output} /> : null}
          {failed && part.errorText ? (
            <p className="whitespace-pre-wrap break-words text-[11px] text-destructive">{part.errorText}</p>
          ) : null}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
