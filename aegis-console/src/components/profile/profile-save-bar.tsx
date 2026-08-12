"use client";

import { Loader2, RotateCcw, Save, TriangleAlert } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * 未保存改动的提示条。
 *
 * 旧版是一个常驻的「保存」按钮：无论有没有改过东西它都亮着，于是既看不出自己
 * 改了什么，点完也不知道到底存没存。这里反过来 —— 没有改动时它根本不出现，
 * 一旦出现就明写**改了哪几项**，并给一个后悔的出口。
 *
 * 「放弃」会丢东西，所以走 AlertDialog 二次确认；「保存」不会，所以直接执行。
 * 条子是 `sticky bottom-4`：它必须在长表单滚到一半时也够得着，
 * 否则改完最上面那个字段的人要一路滚到底才能保存。
 */
export function ProfileSaveBar({
  visible,
  changes,
  blocked,
  saving,
  onSave,
  onDiscard
}: {
  visible: boolean;
  /** 会被真正写入的变更项名称，用来告诉用户"你改了什么" */
  changes: string[];
  blocked: boolean;
  saving: boolean;
  onSave: () => void;
  onDiscard: () => void;
}) {
  if (!visible) return null;

  return (
    <div className="sticky bottom-4 z-20 mx-auto w-full max-w-3xl animate-in fade-in-0 slide-in-from-bottom-2">
      <div
        className={cn(
          "flex flex-wrap items-center gap-3 rounded-xl border bg-popover/95 px-4 py-3 shadow-lg backdrop-blur",
          blocked && "border-destructive/40"
        )}
      >
        {blocked ? (
          <TriangleAlert className="size-4 shrink-0 text-destructive" />
        ) : (
          <span className="size-2 shrink-0 rounded-full bg-amber-500" aria-hidden />
        )}

        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">
            {blocked ? "有字段填得不对" : changes.length ? `${changes.length} 处未保存的改动` : "有未保存的改动"}
          </p>
          <p className="truncate text-xs text-muted-foreground">
            {blocked ? "修正标红的字段后才能保存" : changes.length ? changes.join("、") : "联系方式有调整"}
          </p>
        </div>

        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button variant="ghost" size="sm" className="h-8 gap-1.5 text-xs" disabled={saving}>
              <RotateCcw className="size-3.5" />
              放弃
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>放弃这些改动？</AlertDialogTitle>
              <AlertDialogDescription>
                {changes.length ? `${changes.join("、")}将恢复成保存前的样子。` : "表单会恢复成保存前的样子。"}
                这一步不能撤销。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>继续编辑</AlertDialogCancel>
              <AlertDialogAction onClick={onDiscard}>放弃改动</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <Button size="sm" className="h-8 gap-1.5 text-xs" disabled={blocked || saving} onClick={onSave}>
          {saving ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
          {saving ? "保存中" : "保存"}
        </Button>
      </div>
    </div>
  );
}
