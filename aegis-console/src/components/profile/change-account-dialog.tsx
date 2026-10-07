"use client";

import { FormEvent, useState } from "react";
import { AlertTriangle, Check, LoaderCircle, PencilLine, X } from "lucide-react";
import { toast } from "sonner";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/lib/api-client";
import { ADMIN_ACCOUNT_RULE_TEXT, adminAccountNameError } from "@/lib/admin-account-rules";
import { useAdminAccountAvailabilityQuery, useChangeAdminAccountMutation } from "@/lib/admin-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

/**
 * 修改用户名。注册后**永久只有一次**机会，所以这里做三件事：
 *
 *  1. 输入时即时查重 —— 本地格式不合规就不打后端，合规后去抖再查；
 *  2. 把「只能改一次、旧名不再可用、需用新名登录」写在确认框里，并要求勾选确认；
 *  3. 提交时再验一次当前密码。服务端在同一事务里再判一次重，前端的「可用」只是提示。
 */
export function ChangeAccountDialog({ currentAccount }: { currentAccount: string }) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [password, setPassword] = useState("");
  const [acknowledged, setAcknowledged] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const trimmed = value.trim();
  const localError = trimmed ? adminAccountNameError(trimmed) : null;
  const sameAsCurrent = trimmed !== "" && trimmed === currentAccount;
  // 只有本地校验通过的值才去后端查重
  const checkable = trimmed && !localError && !sameAsCurrent ? trimmed : "";
  const debounced = useDebouncedValue(checkable, 350);
  const availability = useAdminAccountAvailabilityQuery(debounced);
  const mutation = useChangeAdminAccountMutation();

  // 去抖窗口内、或请求在途时都算「检查中」，避免把上一个名字的结果套在当前输入上
  const settled = debounced === checkable && !availability.isFetching;
  const result = settled && availability.data?.account === checkable ? availability.data : undefined;
  const available = Boolean(checkable && result?.available);

  function reset() {
    setValue("");
    setPassword("");
    setAcknowledged(false);
    setError(null);
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!available || !password || !acknowledged) return;
    setError(null);
    try {
      const profile = await mutation.mutateAsync({ account: checkable, currentPassword: password });
      toast.success(`用户名已修改为 ${profile.account.account}`, { description: "下次登录请使用新用户名" });
      setOpen(false);
      reset();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "修改失败，请稍后重试");
    }
  }

  let status: { tone: "muted" | "ok" | "error"; text: string; icon?: "spin" | "ok" | "error" } | null = null;
  if (!trimmed) status = { tone: "muted", text: ADMIN_ACCOUNT_RULE_TEXT };
  else if (localError) status = { tone: "error", text: localError, icon: "error" };
  else if (sameAsCurrent) status = { tone: "error", text: "新用户名与当前用户名相同", icon: "error" };
  else if (availability.isError) status = { tone: "error", text: "暂时无法检查可用性，请稍后重试", icon: "error" };
  else if (!result) status = { tone: "muted", text: "正在检查可用性", icon: "spin" };
  else if (result.available) status = { tone: "ok", text: "该用户名可用", icon: "ok" };
  else status = { tone: "error", text: result.message || "该用户名不可用", icon: "error" };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" className="h-7 gap-1 px-2 text-xs">
          <PencilLine className="size-3.5" />
          修改
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>修改用户名</DialogTitle>
          <DialogDescription>
            当前用户名 <span className="font-mono text-foreground">{currentAccount}</span>
          </DialogDescription>
        </DialogHeader>

        <form className="space-y-4" onSubmit={handleSubmit} noValidate>
          <Alert>
            <AlertTriangle className="text-amber-500" />
            <AlertTitle>用户名仅可修改一次</AlertTitle>
            <AlertDescription>
              修改后无法撤销，也无法再次修改。原用户名将永久保留，任何人都不能再使用；之后请使用新用户名登录。
            </AlertDescription>
          </Alert>

          <div className="space-y-1.5">
            <Label htmlFor="new-account">新用户名</Label>
            <div className="relative">
              <Input
                id="new-account"
                autoComplete="off"
                spellCheck={false}
                value={value}
                maxLength={32}
                placeholder="字母开头，3–32 位"
                onChange={(event) => {
                  setValue(event.target.value);
                  setError(null);
                }}
                aria-invalid={status?.tone === "error"}
                aria-describedby="new-account-status"
                className="pr-9 font-mono"
                autoFocus
              />
              <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2">
                {status?.icon === "spin" ? (
                  <LoaderCircle className="size-4 animate-spin text-muted-foreground" />
                ) : status?.icon === "ok" ? (
                  <Check className="size-4 text-emerald-500" />
                ) : status?.icon === "error" ? (
                  <X className="size-4 text-destructive" />
                ) : null}
              </span>
            </div>
            <p
              id="new-account-status"
              aria-live="polite"
              className={cn(
                "text-xs",
                status?.tone === "ok" && "text-emerald-600 dark:text-emerald-400",
                status?.tone === "error" && "text-destructive",
                status?.tone === "muted" && "text-muted-foreground"
              )}
            >
              {status?.text}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="rename-password">当前密码</Label>
            <Input
              id="rename-password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => {
                setPassword(event.target.value);
                setError(null);
              }}
            />
          </div>

          <label className="flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5">
            <Checkbox
              className="mt-0.5"
              checked={acknowledged}
              onCheckedChange={(checked) => setAcknowledged(checked === true)}
            />
            <span className="text-sm leading-snug">我已了解用户名仅可修改一次，修改后不可撤销</span>
          </label>

          {error ? <p className="text-sm text-destructive">{error}</p> : null}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" disabled={!available || !password || !acknowledged || mutation.isPending}>
              {mutation.isPending ? <LoaderCircle className="animate-spin" /> : null}
              确认修改
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
