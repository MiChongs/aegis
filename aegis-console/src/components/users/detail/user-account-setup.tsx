"use client";

/**
 * 用户名修改与账号初始化。
 *
 *   UserRenameDialog          管理员改用户名：资料页签的账号字段与下面的区块共用
 *   UserAccountSetupSection   安全页签：快速登录建的号补设用户名、密码的进度，「要求重新设置」
 *
 * 管理员改名沿用注册账号规则（3–64 字节、不含空白），不限次数，改完即算用户名已设置；
 * 这里只做同一份前置检查，最终以后端为准（40963 已被使用、40005 不合规）。
 * 自己注册的账号没有初始化记录（tracked = false），不提供「要求重新设置」：后端会把它补登记成第三方登录建的号。
 * 契约见 docs/account-setup.md。
 */

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { Loader2, PencilLine, RotateCcw, UserCheck, UserCog } from "lucide-react";
import { toast } from "sonner";
import { accountSetupSourceLabel } from "@/components/apps/account-setup/account-setup-shared";
import { PROVIDER } from "@/components/reports/report-labels";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useRenameAppUserMutation, useResetUserAccountSetupMutation, useUserAccountSetupQuery } from "@/lib/account-setup-hooks";
import { ApiError } from "@/lib/api-client";
import type { AdminAccountSetupView } from "@/lib/api/account-setup";
import { appSectionHref } from "@/lib/app-sections";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { InfoGrid, Section, StateBlock, type InfoItem } from "./kit";
import { isPast } from "./user-detail-shared";

/** 与后端 validateAccount 一致：按字节计 */
const ACCOUNT_MIN_BYTES = 3;
const ACCOUNT_MAX_BYTES = 64;

function accountProblem(value: string, current: string) {
  const account = value.trim();
  if (!account) return "请输入用户名";
  if (/\s/.test(account)) return "用户名不能包含空白字符";
  const bytes = new TextEncoder().encode(account).length;
  if (bytes < ACCOUNT_MIN_BYTES || bytes > ACCOUNT_MAX_BYTES) return `用户名长度应为 ${ACCOUNT_MIN_BYTES}–${ACCOUNT_MAX_BYTES} 个字节`;
  if (account === current) return "与当前用户名相同";
  return null;
}

// ── 改用户名 ──────────────────────────

/** 挂载即打开；关闭由父级卸载，下次打开是一份干净的表单 */
export function UserRenameDialog({
  appKey,
  userId,
  currentAccount,
  onClose
}: {
  appKey: string;
  userId: number;
  currentAccount: string;
  onClose: () => void;
}) {
  const mutation = useRenameAppUserMutation(appKey, userId);
  const [value, setValue] = useState(currentAccount);
  const [touched, setTouched] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);

  const account = value.trim();
  const problem = accountProblem(value, currentAccount);
  const shownError = serverError ?? (touched && account !== currentAccount ? problem : null);

  async function submit() {
    try {
      const result = await mutation.mutateAsync(account);
      toast.success(`用户名已改为 ${result.account}`);
      onClose();
    } catch (err) {
      setConfirming(false);
      if (err instanceof ApiError && (err.code === 40963 || err.code === 40005)) {
        setServerError(err.message);
        return;
      }
      toast.error(err instanceof ApiError ? err.message : "修改失败");
    }
  }

  return (
    <>
      <Dialog open onOpenChange={(open) => !open && !mutation.isPending && onClose()}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="text-sm">修改用户名</DialogTitle>
            <DialogDescription className="break-all">当前：{currentAccount || "—"}</DialogDescription>
          </DialogHeader>
          <form
            className="space-y-1.5 py-1"
            noValidate
            onSubmit={(event) => {
              event.preventDefault();
              setTouched(true);
              if (!problem) setConfirming(true);
            }}
          >
            <Label htmlFor="rename-account" className="text-xs">
              新用户名
            </Label>
            <Input
              id="rename-account"
              className="font-mono text-sm"
              autoComplete="off"
              autoFocus
              value={value}
              aria-invalid={shownError ? true : undefined}
              onChange={(event) => {
                setValue(event.target.value);
                setTouched(true);
                setServerError(null);
              }}
            />
            {shownError ? (
              <p className="text-xs text-destructive">{shownError}</p>
            ) : (
              <p className="text-xs text-muted-foreground">
                {ACCOUNT_MIN_BYTES}–{ACCOUNT_MAX_BYTES} 个字节，不含空白
              </p>
            )}
            <button type="submit" className="hidden" aria-hidden tabIndex={-1} />
          </form>
          <DialogFooter>
            <Button variant="ghost" size="sm" disabled={mutation.isPending} onClick={onClose}>
              取消
            </Button>
            <Button
              size="sm"
              disabled={Boolean(problem) || Boolean(serverError) || mutation.isPending}
              onClick={() => setConfirming(true)}
            >
              修改
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirming} onOpenChange={(open) => !mutation.isPending && setConfirming(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="break-all">将用户名改为 {account}？</AlertDialogTitle>
            <AlertDialogDescription>用户需用新用户名登录，已登录设备不受影响。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              {mutation.isPending ? <Loader2 className="animate-spin" /> : null}
              确认修改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

// ── 账号初始化 ──────────────────────────

function statusBadge(view: AdminAccountSetupView) {
  if (view.completedAt) {
    return (
      <Badge variant="success" size="sm">
        已完成
      </Badge>
    );
  }
  if (view.required) {
    return (
      <Badge variant="warning" size="sm">
        待完成
      </Badge>
    );
  }
  return (
    <Badge variant="outline" size="sm">
      不提醒
    </Badge>
  );
}

function timeItem(label: string, value?: string | null, empty?: string): InfoItem {
  return {
    label,
    value: value ? formatDateTime(value) : empty,
    hint: value ? formatRelative(value) : undefined
  };
}

function pendingText(pending: boolean, doneAt?: string | null): ReactNode {
  if (pending) {
    return (
      <Badge variant="warning" size="sm">
        待设置
      </Badge>
    );
  }
  return doneAt ? `已设置（${formatDateTime(doneAt)}）` : "已设置";
}

export function UserAccountSetupSection({
  appKey,
  userId,
  currentAccount
}: {
  appKey: string;
  userId: number;
  currentAccount: string;
}) {
  const query = useUserAccountSetupQuery(appKey, userId);
  const resetMutation = useResetUserAccountSetupMutation(appKey, userId);
  const can = useAppKeyPermissionChecker(appKey);
  const canWrite = can("app:user:edit");
  const [renaming, setRenaming] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [resetAccount, setResetAccount] = useState(false);

  const view = query.data;

  async function handleReset() {
    try {
      await resetMutation.mutateAsync(resetAccount);
      toast.success("已要求用户重新设置");
      setResetting(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  let body: ReactNode;
  if (query.isLoading) {
    body = (
      <div className="grid gap-4 sm:grid-cols-2">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="flex flex-col gap-1.5">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-4 w-32" />
          </div>
        ))}
      </div>
    );
  } else if (query.isError || !view) {
    body = (
      <StateBlock
        compact
        kind="error"
        description={query.error instanceof Error ? query.error.message : undefined}
        onRetry={() => void query.refetch()}
      />
    );
  } else if (!view.tracked) {
    body = <StateBlock compact icon={UserCheck} title="自行注册的账号，无需初始化" />;
  } else {
    const source = accountSetupSourceLabel(view.source);
    const provider = view.provider ? (PROVIDER[view.provider] ?? view.provider) : "";
    const snoozeActive = Boolean(view.snoozedUntil) && !isPast(view.snoozedUntil);
    body = (
      <InfoGrid
        items={[
          { label: "建号方式", value: provider && provider !== source ? `${source}（${provider}）` : source },
          {
            label: "应用策略",
            value: view.policyCovers ? "在提醒范围内" : "不在提醒范围内",
            hint: (
              <Link href={appSectionHref(appKey, "account-setup")} className="underline-offset-2 hover:text-foreground hover:underline">
                查看策略
              </Link>
            )
          },
          { label: "用户名", value: pendingText(view.accountPending, view.accountSetAt) },
          { label: "密码", value: pendingText(view.passwordPending) },
          timeItem("首次弹窗", view.promptedAt, "未弹窗"),
          {
            label: "横幅暂停至",
            value: view.snoozedUntil ? formatDateTime(view.snoozedUntil) : undefined,
            hint: view.snoozedUntil ? (snoozeActive ? formatRelative(view.snoozedUntil) : "已到期") : undefined
          },
          timeItem("完成时间", view.completedAt),
          timeItem("登记时间", view.createdAt)
        ]}
      />
    );
  }

  return (
    <>
      <Section
        title="账号初始化"
        description="快速登录建号后补设用户名与密码"
        icon={UserCog}
        tone="info"
        actions={
          <>
            {view?.tracked ? statusBadge(view) : null}
            {canWrite ? (
              <Button size="sm" variant="outline" onClick={() => setRenaming(true)}>
                <PencilLine />
                修改用户名
              </Button>
            ) : null}
            {canWrite && view?.tracked ? (
              <Button
                size="sm"
                variant="outline"
                disabled={resetMutation.isPending}
                onClick={() => {
                  setResetAccount(false);
                  setResetting(true);
                }}
              >
                <RotateCcw />
                要求重新设置
              </Button>
            ) : null}
          </>
        }
      >
        {body}
      </Section>

      {renaming ? (
        <UserRenameDialog appKey={appKey} userId={userId} currentAccount={currentAccount} onClose={() => setRenaming(false)} />
      ) : null}

      <AlertDialog open={resetting} onOpenChange={(open) => !resetMutation.isPending && setResetting(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>要求用户重新设置？</AlertDialogTitle>
            <AlertDialogDescription>用户下次打开应用时从首次提醒重新开始。</AlertDialogDescription>
          </AlertDialogHeader>
          {/* 用户名本来就待设置时这一项没有意义 */}
          {view?.accountPending ? null : (
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox checked={resetAccount} onCheckedChange={(checked) => setResetAccount(checked === true)} />
              用户名也重新设置
            </label>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetMutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={resetMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void handleReset();
              }}
            >
              {resetMutation.isPending ? <Loader2 className="animate-spin" /> : null}
              确认
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
