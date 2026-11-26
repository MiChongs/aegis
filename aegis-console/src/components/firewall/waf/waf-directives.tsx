"use client";

import { useDeferredValue, useMemo, useState } from "react";
import { Check, Copy, FileCode2, Loader2, Search } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useFirewallDirectivesQuery } from "@/lib/firewall-hooks";
import { cn } from "@/lib/utils";

/**
 * 生成的完整 Coraza 指令（internal/firewall/directives.go）。按**已保存**的设置生成，
 * 未保存的草稿不在里面。只在打开时请求。
 */
export function DirectivesButton() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button type="button" size="sm" variant="outline" onClick={() => setOpen(true)}>
        <FileCode2 />
        查看指令
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex max-h-[90vh] flex-col gap-3 sm:max-w-5xl">
          {open ? <DirectivesBody /> : null}
        </DialogContent>
      </Dialog>
    </>
  );
}

function DirectivesBody() {
  const query = useFirewallDirectivesQuery(true);
  const [filter, setFilter] = useState("");
  const [copied, setCopied] = useState(false);
  const deferred = useDeferredValue(filter);
  const text = query.data?.directives ?? "";

  const lines = useMemo(() => text.replace(/\r\n/g, "\n").split("\n"), [text]);
  const visible = useMemo(() => {
    const q = deferred.trim().toLowerCase();
    const all = lines.map((line, index) => ({ line, number: index + 1 }));
    return q ? all.filter((item) => item.line.toLowerCase().includes(q)) : all;
  }, [deferred, lines]);
  const gutter = String(lines.length).length;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      toast.success("已复制");
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("复制失败");
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>Coraza 指令</DialogTitle>
        <DialogDescription>按已保存的设置生成{query.data ? ` · ${lines.length} 行` : ""}</DialogDescription>
      </DialogHeader>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="筛选行，如 SecRuleRemoveById、1100300" className="h-8 pl-8" aria-label="筛选指令" />
        </div>
        {filter ? <span className="text-xs text-muted-foreground tabular-nums">{visible.length} 行</span> : null}
        <Button type="button" size="sm" variant="outline" onClick={() => void copy()} disabled={!text}>
          {copied ? <Check /> : <Copy />}
          复制全部
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto rounded-xl border border-border bg-muted/40">
        {query.isLoading ? (
          <div className="flex items-center justify-center gap-2 py-16 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            生成中
          </div>
        ) : query.isError ? (
          <div className="py-16 text-center text-sm text-muted-foreground">加载失败</div>
        ) : (
          <div className="min-w-max py-2 font-mono text-xs leading-5">
            {visible.map(({ line, number }) => (
              <div key={number} className={cn("flex hover:bg-accent/60", isComment(line) && "text-muted-foreground")}>
                <span
                  className="sticky left-0 shrink-0 bg-muted px-3 text-right text-muted-foreground/70 tabular-nums select-none"
                  style={{ width: `${gutter + 3}ch` }}
                >
                  {number}
                </span>
                <span className="pr-4 pl-3 whitespace-pre">{line || " "}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  );
}

function isComment(line: string) {
  return line.trimStart().startsWith("#");
}
