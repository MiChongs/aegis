"use client";

import { useState } from "react";
import { AlertTriangle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  userName: string;
  onConfirm: () => void;
  isPending: boolean;
};

export function UserDeleteDialog({ open, onOpenChange, userName, onConfirm, isPending }: Props) {
  const [confirmText, setConfirmText] = useState("");
  const canDelete = confirmText === "DELETE";

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) setConfirmText(""); onOpenChange(v); }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm text-destructive">
            <AlertTriangle className="size-4" />
            删除用户
          </DialogTitle>
          <DialogDescription>
            即将永久删除用户 <span className="font-semibold text-foreground">{userName || "未知"}</span>，此操作不可撤销。用户的所有数据（资料、设置、安全凭证、会话）将被清除。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2 py-1">
          <p className="text-xs text-muted-foreground">请输入 <span className="font-mono font-semibold text-foreground">DELETE</span> 确认删除：</p>
          <Input className="font-mono text-sm" placeholder="DELETE" value={confirmText} onChange={(e) => setConfirmText(e.target.value)} />
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>取消</Button>
          <Button variant="destructive" size="sm" disabled={!canDelete || isPending} onClick={onConfirm}>
            {isPending ? <><Loader2 className="mr-1 size-3 animate-spin" />删除中...</> : "确认删除"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
