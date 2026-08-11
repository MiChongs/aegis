"use client";

import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useResetAdminAppUserPasswordMutation } from "@/lib/admin-hooks";

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  appKey?: string | null;
  userId?: number | null;
};

export function UserResetPasswordDialog({ open, onOpenChange, appKey, userId }: Props) {
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const mutation = useResetAdminAppUserPasswordMutation(appKey, userId);

  async function handleSubmit() {
    if (password.length < 6) {
      toast.error("密码长度不能少于 6 位");
      return;
    }
    if (password !== confirm) {
      toast.error("两次输入的密码不一致");
      return;
    }
    try {
      await mutation.mutateAsync({ newPassword: password });
      toast.success("密码已重置，用户需要重新登录");
      setPassword("");
      setConfirm("");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "重置失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="text-sm">重置用户密码</DialogTitle>
          <DialogDescription>重置后用户的所有会话将被踢出，需使用新密码重新登录。</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-1">
          <div className="space-y-1.5">
            <Label className="text-xs">新密码</Label>
            <Input type="password" className="text-sm" placeholder="至少 6 位" value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">确认密码</Label>
            <Input type="password" className="text-sm" placeholder="再次输入" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>取消</Button>
          <Button size="sm" disabled={mutation.isPending || !password} onClick={handleSubmit}>
            {mutation.isPending ? <><Loader2 className="mr-1 size-3 animate-spin" />重置中...</> : "确认重置"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
