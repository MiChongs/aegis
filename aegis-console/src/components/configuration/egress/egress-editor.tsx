"use client";

import { useRef, useState } from "react";
import dynamic from "next/dynamic";
import { ArrowLeft } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { EgressGraph } from "@/lib/api/egress";
import { useEgressGraphQuery } from "@/lib/egress-hooks";

/**
 * 出海网关画布编辑器（全屏弹层）：画布（Flowgram）在浏览器里按需加载，见 egress-workspace。
 * 默认载入草稿；从版本记录打开某一版、导入配置时换一张画布重新挂载编辑器。关闭时有未保存的改动先确认。
 */

const EgressWorkspace = dynamic(() => import("@/components/configuration/egress/egress-workspace"), {
  ssr: false,
  loading: () => <EditorSkeleton error={null} />
});

export type EgressEditorSeed = { graph: EgressGraph; from: string };

export function EgressEditor({ open, seed, onOpenChange }: { open: boolean; seed?: EgressEditorSeed | null; onOpenChange: (open: boolean) => void }) {
  const query = useEgressGraphQuery(open);
  const [replacement, setReplacement] = useState<(EgressEditorSeed & { key: number }) | null>(null);
  const [confirmClose, setConfirmClose] = useState(false);
  // 打开时的视图定格：后台每 30 秒刷新运行态，不能因此重新挂载编辑器
  const [frozen, setFrozen] = useState<typeof query.data>(undefined);
  const dirtyRef = useRef(false);
  const view = frozen ?? query.data;
  if (open && !frozen && query.data) setFrozen(query.data);

  const close = () => {
    dirtyRef.current = false;
    setReplacement(null);
    setFrozen(undefined);
    onOpenChange(false);
  };
  const requestClose = () => {
    if (dirtyRef.current) setConfirmClose(true);
    else close();
  };

  const current = replacement ?? (seed ? { ...seed, key: -1 } : null);
  // 运行态（健康、命中数）跟着最新的查询结果走
  const live = view && query.data ? { ...view, runtime: query.data.runtime, core: query.data.core } : view;

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? onOpenChange(true) : requestClose())}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[100dvh] w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 p-0 sm:max-w-none"
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogTitle className="sr-only">编辑出海网关</DialogTitle>
        <DialogDescription className="sr-only">在画布上编排路由规则、线路与 mihomo 的策略组、节点、订阅和分流</DialogDescription>
        <TooltipProvider delayDuration={300}>
          {live ? (
            <EgressWorkspace
              key={current ? `seed-${current.key}` : "draft"}
              view={live}
              graph={current?.graph ?? live.graph}
              seededFrom={current?.from}
              onClose={requestClose}
              onDirtyChange={(dirty) => {
                dirtyRef.current = dirty;
              }}
              onReplace={(graph, from) => setReplacement((previous) => ({ graph, from, key: (previous?.key ?? 0) + 1 }))}
            />
          ) : (
            <EditorSkeleton error={query.error} onClose={close} />
          )}
        </TooltipProvider>
      </DialogContent>
      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改？</AlertDialogTitle>
            <AlertDialogDescription>画布上还有没保存的改动，关闭后会丢失。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction className="bg-destructive text-white hover:bg-destructive/90" onClick={close}>
              放弃修改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}

function EditorSkeleton({ error, onClose }: { error: unknown; onClose?: () => void }) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-3 border-b px-4">
        <Button size="icon" variant="ghost" onClick={onClose} aria-label="返回">
          <ArrowLeft />
        </Button>
        <Skeleton className="h-5 w-48" />
      </div>
      {error ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{error instanceof Error ? error.message : "加载失败"}</div>
      ) : (
        <div className="grid flex-1 grid-cols-1 xl:grid-cols-[minmax(0,1fr)_420px]">
          <div className="relative">
            <Skeleton className="absolute top-3 left-3 h-96 w-60 rounded-2xl" />
            <div className="absolute top-16 left-1/3 flex flex-col gap-10">
              {[0, 1, 2].map((index) => (
                <Skeleton key={index} className="h-28 w-[280px] rounded-2xl" />
              ))}
            </div>
            <div className="absolute top-28 left-[calc(33%+340px)] flex flex-col gap-8">
              {[0, 1, 2, 3].map((index) => (
                <Skeleton key={index} className="h-24 w-[280px] rounded-2xl" />
              ))}
            </div>
            <Skeleton className="absolute bottom-4 left-1/2 h-10 w-96 -translate-x-1/2 rounded-2xl" />
          </div>
          <Skeleton className="m-3 hidden rounded-xl xl:block" />
        </div>
      )}
    </div>
  );
}
