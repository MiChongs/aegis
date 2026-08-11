"use client";

import { useState } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Underline from "@tiptap/extension-underline";
import Link from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import {
  Bold,
  Code,
  Heading1,
  Heading2,
  Italic,
  Link as LinkIcon,
  List,
  ListOrdered,
  Maximize2,
  Minimize2,
  Minus,
  Quote,
  Redo,
  Strikethrough,
  Underline as UnderlineIcon,
  Undo
} from "lucide-react";
import { cn } from "@/lib/utils";

type RichEditorProps = {
  value?: string;
  onChange?: (html: string) => void;
  placeholder?: string;
  className?: string;
  /** 是否允许全屏 */
  fullscreenable?: boolean;
};

function Btn({
  active,
  onClick,
  title,
  children,
}: {
  active?: boolean;
  onClick: () => void;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      onMouseDown={(e) => { e.preventDefault(); onClick(); }}
      className={cn(
        "flex size-7 items-center justify-center rounded transition-colors",
        active ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
      )}
    >
      {children}
    </button>
  );
}

function Sep() {
  return <span className="mx-0.5 h-4 w-px bg-border shrink-0" />;
}

export function RichEditor({ value, onChange, placeholder, className, fullscreenable = true }: RichEditorProps) {
  const [fullscreen, setFullscreen] = useState(false);

  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2] } }),
      Underline,
      Link.configure({ openOnClick: false }),
      Placeholder.configure({ placeholder: placeholder || "输入内容..." }),
    ],
    content: value || "",
    onUpdate: ({ editor: e }) => { onChange?.(e.getHTML()); },
    editorProps: {
      attributes: {
        class: cn(
          "prose prose-sm dark:prose-invert max-w-none px-4 py-3 focus:outline-none",
          fullscreen ? "min-h-[calc(100vh-56px)]" : "min-h-40"
        ),
      },
    },
  });

  if (!editor) return null;

  const setLink = () => {
    const url = window.prompt("输入链接地址", editor.getAttributes("link").href || "https://");
    if (url === null) return;
    if (url === "") {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
    } else {
      editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run();
    }
  };

  const toolbar = (
    <div className="flex flex-wrap items-center gap-0.5 border-b px-2 py-1 shrink-0 bg-background sticky top-0 z-10">
      <Btn active={editor.isActive("bold")} onClick={() => editor.chain().focus().toggleBold().run()} title="粗体"><Bold className="size-3.5" /></Btn>
      <Btn active={editor.isActive("italic")} onClick={() => editor.chain().focus().toggleItalic().run()} title="斜体"><Italic className="size-3.5" /></Btn>
      <Btn active={editor.isActive("underline")} onClick={() => editor.chain().focus().toggleUnderline().run()} title="下划线"><UnderlineIcon className="size-3.5" /></Btn>
      <Btn active={editor.isActive("strike")} onClick={() => editor.chain().focus().toggleStrike().run()} title="删除线"><Strikethrough className="size-3.5" /></Btn>
      <Sep />
      <Btn active={editor.isActive("heading", { level: 1 })} onClick={() => editor.chain().focus().toggleHeading({ level: 1 }).run()} title="一级标题"><Heading1 className="size-3.5" /></Btn>
      <Btn active={editor.isActive("heading", { level: 2 })} onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()} title="二级标题"><Heading2 className="size-3.5" /></Btn>
      <Sep />
      <Btn active={editor.isActive("bulletList")} onClick={() => editor.chain().focus().toggleBulletList().run()} title="无序列表"><List className="size-3.5" /></Btn>
      <Btn active={editor.isActive("orderedList")} onClick={() => editor.chain().focus().toggleOrderedList().run()} title="有序列表"><ListOrdered className="size-3.5" /></Btn>
      <Btn active={editor.isActive("blockquote")} onClick={() => editor.chain().focus().toggleBlockquote().run()} title="引用"><Quote className="size-3.5" /></Btn>
      <Btn active={editor.isActive("code")} onClick={() => editor.chain().focus().toggleCode().run()} title="行内代码"><Code className="size-3.5" /></Btn>
      <Btn active={editor.isActive("link")} onClick={setLink} title="链接"><LinkIcon className="size-3.5" /></Btn>
      <Sep />
      <Btn onClick={() => editor.chain().focus().setHorizontalRule().run()} title="分割线"><Minus className="size-3.5" /></Btn>
      <Btn onClick={() => editor.chain().focus().undo().run()} title="撤销"><Undo className="size-3.5" /></Btn>
      <Btn onClick={() => editor.chain().focus().redo().run()} title="重做"><Redo className="size-3.5" /></Btn>
      {fullscreenable && (
        <>
          <Sep />
          <Btn active={fullscreen} onClick={() => setFullscreen(!fullscreen)} title={fullscreen ? "退出全屏" : "全屏编辑"}>
            {fullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
          </Btn>
        </>
      )}
    </div>
  );

  if (fullscreen) {
    return (
      <div className="fixed inset-0 z-50 flex flex-col bg-background">
        {toolbar}
        <div className="flex-1 overflow-y-auto">
          <div className="mx-auto max-w-4xl">
            <EditorContent editor={editor} />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={cn("rounded-lg border bg-background overflow-hidden", className)}>
      {toolbar}
      <EditorContent editor={editor} />
    </div>
  );
}

/** 将 HTML 渲染为安全的富文本展示 */
export function RichContent({ html, className }: { html?: string; className?: string }) {
  if (!html) return null;
  return (
    <div
      className={cn("prose prose-sm dark:prose-invert max-w-none", className)}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
