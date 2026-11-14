"use client";

import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { Extension } from "@tiptap/core";
import { EditorContent, useEditor, useEditorState } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Placeholder from "@tiptap/extension-placeholder";
import { Markdown } from "@tiptap/markdown";
import { useDropzone } from "react-dropzone";
import {
  Bold,
  ChevronDown,
  Code,
  FileText,
  Italic,
  List,
  Loader2,
  Lock,
  Paperclip,
  Send,
  Upload,
  X,
  Zap
} from "lucide-react";
import { notify } from "@/lib/notify";
import { ApiError } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useReplyTicketMutation, useTicketQuickRepliesQuery, useUploadTicketAttachmentMutation } from "@/lib/ticket-hooks";
import type { TicketAttachment, TicketItem, TicketStatus } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import {
  Kbd,
  STATUS_LABEL,
  StatusDot,
  attachmentUrl,
  formatBytes,
  isImageAttachment
} from "@/components/tickets/ticket-shared";

// 回复框（Tiptap + @tiptap/markdown）。
//
// 写的时候所见即所得（**粗体**、`- 列表`、`代码` 这些 Markdown 写法边打边成形），
// 发出去的仍是 Markdown 原文：App 端按 Markdown 渲染客服回复，存储格式不变。
//
// - 「回复 / 备注」两种模式，备注模式整框变琥珀色，避免把内部讨论发给用户
// - 拖文件进来、直接粘贴截图都能上传
// - 空框里敲 / 弹出快捷回复，Ctrl / ⌘ + Enter 发送
// - 草稿按反馈记在内存里，来回切换不丢字

type Mode = "reply" | "internal";
type Draft = { markdown: string; mode: Mode; files: TicketAttachment[]; nextStatus: string };

const drafts = new Map<number, Draft>();

// 占位文字随「回复 / 备注」切换。Placeholder 的取值函数在编辑器内部调用，
// 这里借扩展的 storage 把当前文案递进去：命令改完 storage 后随手发出的那个空事务，
// 正好让 Placeholder 的装饰按新文案重画。
const ReplyHint = Extension.create<Record<string, never>, { text: string }>({
  name: "replyHint",
  addStorage: () => ({ text: "" }),
  addCommands() {
    return {
      setReplyHint:
        (text: string) =>
        ({ tr, dispatch }) => {
          this.storage.text = text;
          if (dispatch) tr.setMeta("addToHistory", false);
          return true;
        }
    };
  }
});

declare module "@tiptap/core" {
  interface Storage {
    replyHint: { text: string };
  }
  interface Commands<ReturnType> {
    replyHint: { setReplyHint: (text: string) => ReturnType };
  }
}

const NEXT_STATUS: { value: string; label: string; status?: TicketStatus }[] = [
  { value: "", label: "保持当前状态" },
  { value: "processing", label: "处理中", status: "processing" },
  { value: "pending_user", label: "待用户补充", status: "pending_user" },
  { value: "resolved", label: "已解决", status: "resolved" }
];

export type FeedbackComposerHandle = {
  /** 聚焦输入框；传 mode 时先切到对应模式 */
  focus: (mode?: Mode) => void;
};

export function FeedbackComposer({ ticket, ref }: { ticket: TicketItem; ref?: Ref<FeedbackComposerHandle> }) {
  const perms = ticket.permissions;
  const canReply = Boolean(perms?.reply);
  const canNote = Boolean(perms?.internalNote);
  const canUpload = Boolean(perms?.uploadAttachment);

  const initial = drafts.get(ticket.id);
  const [mode, setMode] = useState<Mode>(initial?.mode ?? (canReply ? "reply" : "internal"));
  const [files, setFiles] = useState<TicketAttachment[]>(initial?.files ?? []);
  const [nextStatus, setNextStatus] = useState(initial?.nextStatus ?? "");
  const [uploading, setUploading] = useState(0);
  const [quickOpen, setQuickOpen] = useState(false);

  const replyMut = useReplyTicketMutation();
  const uploadMut = useUploadTicketAttachmentMutation();
  const quickRepliesQuery = useTicketQuickRepliesQuery(ticket.appid);
  const quickReplies = (quickRepliesQuery.data ?? []).filter((item) => item.enabled);
  const internal = mode === "internal";

  // 编辑器的按键与粘贴回调只在创建时绑定一次，这里用 ref 把最新的状态与动作递进去
  const live = useRef({ send: () => {}, upload: (_files: File[]) => {}, openQuick: () => {} });

  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      // 标题、分割线在一条回复里用不上；下划线没有对应的 Markdown 写法
      StarterKit.configure({
        heading: false,
        horizontalRule: false,
        underline: false,
        link: { openOnClick: false, autolink: true }
      }),
      ReplyHint,
      Placeholder.configure({ placeholder: ({ editor: current }) => current.storage.replyHint.text }),
      Markdown
    ],
    content: initial?.markdown ?? "",
    contentType: "markdown",
    editorProps: {
      attributes: { class: "reply-editor", "aria-label": "回复内容" },
      handleKeyDown: (view, event) => {
        if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
          event.preventDefault();
          live.current.send();
          return true;
        }
        if (event.key === "/" && view.state.doc.textContent === "") {
          event.preventDefault();
          live.current.openQuick();
          return true;
        }
        if (event.key === "Escape") {
          (view.dom as HTMLElement).blur();
          return true;
        }
        return false;
      },
      handlePaste: (_view, event) => {
        const pasted = Array.from(event.clipboardData?.files ?? []);
        if (!pasted.length) return false;
        event.preventDefault();
        live.current.upload(pasted);
        return true;
      }
    },
    onUpdate: ({ editor: current }) => {
      const markdown = current.isEmpty ? "" : current.getMarkdown();
      const previous = drafts.get(ticket.id);
      const next: Draft = { mode: "reply", files: [], nextStatus: "", ...previous, markdown };
      if (!next.markdown && !next.files.length && !next.nextStatus) drafts.delete(ticket.id);
      else drafts.set(ticket.id, next);
    }
  });

  const marks = useEditorState({
    editor,
    selector: ({ editor: current }) => ({
      empty: current?.isEmpty ?? true,
      bold: current?.isActive("bold") ?? false,
      italic: current?.isActive("italic") ?? false,
      list: current?.isActive("bulletList") ?? false,
      code: current?.isActive("code") ?? false
    })
  });
  const empty = marks?.empty ?? true;

  const patchDraft = (patch: Partial<Draft>) => {
    const markdown = editor && !editor.isEmpty ? editor.getMarkdown() : "";
    const next: Draft = { markdown, mode, files, nextStatus, ...patch };
    if (!next.markdown && !next.files.length && !next.nextStatus) drafts.delete(ticket.id);
    else drafts.set(ticket.id, next);
  };

  const switchMode = (value: Mode) => {
    setMode(value);
    patchDraft({ mode: value });
  };

  const upload = async (list: File[]) => {
    if (!list.length || !canUpload) return;
    setUploading((count) => count + list.length);
    const done: TicketAttachment[] = [];
    for (const file of list) {
      try {
        done.push(await uploadMut.mutateAsync({ file, appid: ticket.appid, ticketId: ticket.id }));
      } catch (error) {
        notify.error(error instanceof ApiError ? error.message : `${file.name} 上传失败`);
      } finally {
        setUploading((count) => count - 1);
      }
    }
    if (done.length) {
      setFiles((prev) => {
        const next = [...prev, ...done];
        patchDraft({ files: next });
        return next;
      });
    }
  };

  const removeFile = (id: number) => {
    setFiles((prev) => {
      const next = prev.filter((file) => file.id !== id);
      patchDraft({ files: next });
      return next;
    });
  };

  const send = async () => {
    if (!editor || replyMut.isPending) return;
    const text = editor.isEmpty ? "" : editor.getMarkdown().trim();
    if (!text) {
      notify.warning(internal ? "请填写备注内容" : "请填写回复内容");
      return;
    }
    if (uploading > 0) {
      notify.warning("附件还在上传");
      return;
    }
    try {
      await replyMut.mutateAsync({
        id: ticket.id,
        payload: {
          content: text,
          contentType: "markdown",
          internal,
          nextStatus: internal ? "" : ((nextStatus as TicketStatus) || ""),
          attachmentIds: files.map((file) => file.id)
        }
      });
      editor.commands.clearContent(true);
      setFiles([]);
      setNextStatus("");
      drafts.delete(ticket.id);
      notify.success(
        internal ? "备注已添加" : nextStatus ? `已回复，状态改为「${STATUS_LABEL[nextStatus as TicketStatus]}」` : "回复已发送"
      );
    } catch (error) {
      notify.error(error instanceof ApiError ? error.message : "发送失败");
    }
  };

  useEffect(() => {
    live.current = {
      send: () => void send(),
      upload: (list) => void upload(list),
      openQuick: () => {
        if (quickReplies.length > 0) setQuickOpen(true);
      }
    };
  });

  const placeholder = internal ? "写给同事看的备注，用户看不到" : `回复 ${ticket.requesterName || "用户"}`;
  useEffect(() => {
    if (editor && !editor.isDestroyed) editor.commands.setReplyHint(placeholder);
  }, [editor, placeholder]);

  useImperativeHandle(
    ref,
    () => ({
      focus: (target) => {
        if (target && target !== mode && (target === "reply" ? canReply : canNote)) switchMode(target);
        editor?.commands.focus("end");
      }
    }),
    // switchMode 每次渲染都是新函数，这里只需要跟着编辑器实例与当前模式走
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [editor, mode, canReply, canNote]
  );

  const { getRootProps, getInputProps, isDragActive, open: openFilePicker } = useDropzone({
    onDrop: (accepted) => void upload(accepted),
    noClick: true,
    noKeyboard: true,
    disabled: !canUpload
  });

  if (!canReply && !canNote) {
    return (
      <div className="border-t px-5 py-3 text-center text-xs text-muted-foreground">
        {ticket.locked ? "已锁定" : ticket.status === "closed" || ticket.status === "cancelled" ? "已结束，重新打开后可回复" : "无回复权限"}
      </div>
    );
  }

  const insertQuickReply = (text: string) => {
    setQuickOpen(false);
    editor?.chain().focus("end").insertContent(text, { contentType: "markdown" }).run();
  };

  const format = (run: () => void) => (event: React.MouseEvent) => {
    // 不让按钮抢走编辑器焦点，否则选区会丢
    event.preventDefault();
    run();
  };

  return (
    <div className="px-4 pb-4 pt-1">
      <div
        {...getRootProps()}
        className={cn(
          "@container relative mx-auto max-w-3xl rounded-xl border bg-card transition-colors focus-within:border-ring/60 focus-within:ring-[3px] focus-within:ring-ring/15",
          internal &&
            "border-amber-300 bg-amber-50/60 focus-within:border-amber-400 focus-within:ring-amber-400/20 dark:border-amber-900 dark:bg-amber-950/20"
        )}
      >
        <input {...getInputProps()} />

        {isDragActive ? (
          <div className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-1 rounded-xl border-2 border-dashed border-primary bg-card text-sm text-primary">
            <Upload className="size-5" />
            松开上传
          </div>
        ) : null}

        {internal ? (
          <p className="flex items-center gap-1.5 px-3.5 pt-2.5 text-xs font-medium text-amber-700 dark:text-amber-400">
            <Lock className="size-3" />
            内部备注
          </p>
        ) : null}

        <EditorContent editor={editor} />

        {files.length > 0 || uploading > 0 ? (
          <div className="flex flex-wrap gap-2 px-3 pb-2">
            {files.map((file) => (
              <PendingFile key={file.id} file={file} onRemove={() => removeFile(file.id)} />
            ))}
            {uploading > 0 ? (
              <span className="inline-flex h-11 items-center gap-1.5 rounded-lg border border-dashed px-3 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" />
                上传中 {uploading}
              </span>
            ) : null}
          </div>
        ) : null}

        <div className="flex items-center gap-0.5 px-1.5 pb-1.5">
          {canUpload ? (
            <ToolButton label="添加附件" onClick={openFilePicker}>
              <Paperclip />
            </ToolButton>
          ) : null}

          <Popover open={quickOpen} onOpenChange={setQuickOpen}>
            <Tooltip>
              <TooltipTrigger asChild>
                <PopoverTrigger asChild>
                  <Button size="icon-sm" variant="ghost" className="text-muted-foreground" disabled={quickReplies.length === 0} aria-label="快捷回复">
                    <Zap />
                  </Button>
                </PopoverTrigger>
              </TooltipTrigger>
              <TooltipContent>
                快捷回复 <Kbd className="ml-1">/</Kbd>
              </TooltipContent>
            </Tooltip>
            <PopoverContent align="start" side="top" className="w-80 p-0">
              <Command>
                <CommandInput placeholder="搜索快捷回复" />
                <CommandList className="max-h-72">
                  <CommandEmpty>没有匹配的快捷回复</CommandEmpty>
                  <CommandGroup>
                    {quickReplies.map((item) => (
                      <CommandItem
                        key={item.id}
                        value={`${item.title} ${item.content}`}
                        onSelect={() => insertQuickReply(item.content)}
                        className="flex-col items-start gap-0.5"
                      >
                        <span className="font-medium">{item.title}</span>
                        <span className="line-clamp-2 text-xs text-muted-foreground">{item.content}</span>
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>

          {/* 格式按钮在窄的回复框里收起：Markdown 写法照样生效，发送与模式切换不能被挤掉 */}
          <div className="hidden items-center gap-0.5 @md:flex">
            <span aria-hidden className="mx-1 h-4 w-px bg-border" />

            <ToolButton label="粗体" active={marks?.bold} onMouseDown={format(() => editor?.chain().focus().toggleBold().run())}>
              <Bold />
            </ToolButton>
            <ToolButton label="斜体" active={marks?.italic} onMouseDown={format(() => editor?.chain().focus().toggleItalic().run())}>
              <Italic />
            </ToolButton>
            <ToolButton label="列表" active={marks?.list} onMouseDown={format(() => editor?.chain().focus().toggleBulletList().run())}>
              <List />
            </ToolButton>
            <ToolButton label="代码" active={marks?.code} onMouseDown={format(() => editor?.chain().focus().toggleCode().run())}>
              <Code />
            </ToolButton>
          </div>

          <div className="ml-auto flex shrink-0 items-center gap-2 whitespace-nowrap">
            {canReply && canNote ? (
              <div role="radiogroup" aria-label="发送方式" className="flex h-7 items-center rounded-lg bg-muted p-0.5 text-xs">
                {(["reply", "internal"] as Mode[]).map((value) => (
                  <button
                    key={value}
                    type="button"
                    role="radio"
                    aria-checked={mode === value}
                    onClick={() => switchMode(value)}
                    className={cn(
                      "h-6 rounded-md px-2 outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/40",
                      mode === value ? "bg-background font-medium text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {value === "reply" ? "回复" : "备注"}
                  </button>
                ))}
              </div>
            ) : null}

            <ButtonGroup>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="sm"
                    className={cn(internal && "bg-amber-500 text-white hover:bg-amber-500/90")}
                    disabled={replyMut.isPending || empty}
                    onClick={() => void send()}
                  >
                    {replyMut.isPending ? <Loader2 className="animate-spin" /> : <Send />}
                    {internal ? "添加备注" : !internal && nextStatus ? `发送并转为${STATUS_LABEL[nextStatus as TicketStatus]}` : "发送"}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <Kbd>Ctrl</Kbd> <Kbd>Enter</Kbd>
                </TooltipContent>
              </Tooltip>
              {!internal && perms?.changeStatus ? (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="icon-sm" className="border-l border-primary-foreground/20" aria-label="发送后状态">
                      <ChevronDown />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuLabel className="text-xs text-muted-foreground">发送后状态</DropdownMenuLabel>
                    <DropdownMenuRadioGroup
                      value={nextStatus}
                      onValueChange={(value) => {
                        setNextStatus(value);
                        patchDraft({ nextStatus: value });
                      }}
                    >
                      {NEXT_STATUS.map((item) => (
                        <DropdownMenuRadioItem key={item.value || "keep"} value={item.value}>
                          {item.status ? <StatusDot status={item.status} /> : null}
                          {item.label}
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </ButtonGroup>
          </div>
        </div>
      </div>
    </div>
  );
}

function ToolButton({
  label,
  active,
  children,
  ...props
}: {
  label: string;
  active?: boolean;
  children: React.ReactNode;
} & Pick<React.ComponentProps<"button">, "onClick" | "onMouseDown">) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={label}
          aria-pressed={active}
          className={cn("text-muted-foreground", active && "bg-muted text-foreground")}
          {...props}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function PendingFile({ file, onRemove }: { file: TicketAttachment; onRemove: () => void }) {
  const image = isImageAttachment(file) && file.downloadUrl;
  return (
    <div className="relative flex h-11 items-center gap-2 overflow-hidden rounded-lg border bg-background pr-7 text-xs">
      {image ? (
        // eslint-disable-next-line @next/next/no-img-element -- 同源存储代理地址，见 ticket-attachments 文件头注释
        <img src={attachmentUrl(file)} alt={file.fileName} className="size-11 object-cover" />
      ) : (
        <span className="flex size-11 items-center justify-center bg-muted text-muted-foreground">
          <FileText className="size-4" />
        </span>
      )}
      <span className="max-w-32">
        <span className="block truncate text-foreground">{file.fileName}</span>
        <span className="block text-muted-foreground">{formatBytes(file.sizeBytes)}</span>
      </span>
      <button
        type="button"
        onClick={onRemove}
        aria-label={`移除 ${file.fileName}`}
        className="absolute right-1 top-1 rounded-full p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}
