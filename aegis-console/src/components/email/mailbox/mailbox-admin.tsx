"use client";

import { useState } from "react";
import { AtSign, Dices, History, KeyRound, Loader2, Plus, RefreshCw, Search, Trash2, UserRound } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { SkymailAccount, SkymailOverview, SkymailRegKey, SkymailUser } from "@/lib/api/email-skymail";
import { formatDateTimeMinute } from "@/lib/datetime";
import type { EmailScope } from "@/lib/email-hooks";
import { useSkymailAction, useSkymailQuery } from "@/lib/email-skymail-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { ConfirmDialog, EmptyNote, ErrorNote, Pager, TableSkeleton, errorText } from "./mailbox-shared";

type MailboxContext = { scope: EmailScope; configId: number; overview: SkymailOverview; canWrite: boolean };

function domainOf(address: string) {
  const at = address.lastIndexOf("@");
  return at >= 0 ? address.slice(at) : "";
}

// ── 邮箱账号 ──

const ACCOUNT_PAGE = 30;

/** 登录用户名下的邮箱地址。登录主邮箱不能删。 */
export function AccountsView({ scope, configId, overview, canWrite }: MailboxContext) {
  const [cursors, setCursors] = useState<SkymailAccount[]>([]);
  const last = cursors[cursors.length - 1];
  const query = useSkymailQuery(scope, configId, "accounts", {
    size: ACCOUNT_PAGE,
    ...(last ? { accountId: last.accountId, lastSort: last.sort } : {})
  });
  const action = useSkymailAction(scope, configId);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<SkymailAccount | null>(null);
  const rows = query.data ?? [];
  const mainAccountId = overview.profile.account?.accountId;

  const remove = async () => {
    if (!removing) return;
    try {
      await action.mutateAsync({ action: "deleteAccount", params: { accountId: removing.accountId } });
      toast.success(`已删除 ${removing.email}`);
      setRemoving(null);
    } catch (error) {
      toast.error(errorText(error));
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-xs text-muted-foreground">
          {overview.profile.email} 名下的邮箱
          {overview.profile.role?.accountCount ? `，最多 ${overview.profile.role.accountCount} 个` : ""}
        </p>
        <div className="ml-auto flex items-center gap-2">
          <Button size="sm" variant="outline" className="h-8" onClick={() => void query.refetch()} disabled={query.isFetching}>
            <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} /> 刷新
          </Button>
          {canWrite ? (
            <Button size="sm" className="h-8" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" /> 添加邮箱
            </Button>
          ) : null}
        </div>
      </div>
      {query.error ? <ErrorNote error={query.error} onRetry={() => void query.refetch()} /> : null}
      <div className="overflow-hidden rounded-2xl border bg-card">
        {query.isLoading ? (
          <TableSkeleton rows={4} />
        ) : rows.length === 0 ? (
          <EmptyNote icon={<AtSign className="size-6" />} title="还没有邮箱" />
        ) : (
          <table className="w-full text-sm">
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">地址</th>
                <th className="px-3 py-2.5 text-left font-medium">名称</th>
                <th className="px-3 py-2.5 text-left font-medium">收取</th>
                <th className="px-3 py-2.5 text-left font-medium">创建时间</th>
                {canWrite ? <th className="w-12 px-3 py-2.5" /> : null}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.accountId} className={cn("border-b last:border-b-0", row.isDel === 1 && "opacity-60")}>
                  <td className="px-4 py-2.5 font-mono text-xs">
                    {row.email}
                    {row.accountId === mainAccountId ? (
                      <Badge variant="secondary" size="sm" className="ml-1.5 font-sans">
                        登录邮箱
                      </Badge>
                    ) : null}
                    {row.accountId === overview.senderAccountId ? (
                      <Badge variant="info" size="sm" className="ml-1.5 font-sans">
                        发信账号
                      </Badge>
                    ) : null}
                  </td>
                  <td className="px-3 py-2.5 text-xs">{row.name || "—"}</td>
                  <td className="px-3 py-2.5 text-xs text-muted-foreground">{row.allReceive === 1 ? "收取全部邮箱" : "仅本邮箱"}</td>
                  <td className="whitespace-nowrap px-3 py-2.5 text-xs tabular-nums text-muted-foreground">
                    {formatDateTimeMinute(row.createTime)}
                  </td>
                  {canWrite ? (
                    <td className="px-3 py-2.5 text-right">
                      <Button
                        size="icon"
                        variant="ghost"
                        className="size-7 text-muted-foreground hover:text-destructive"
                        aria-label="删除邮箱"
                        disabled={row.accountId === mainAccountId || row.isDel === 1}
                        title={row.accountId === mainAccountId ? "登录主邮箱不能删除" : undefined}
                        onClick={() => setRemoving(row)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </td>
                  ) : null}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <Pager
        canPrev={cursors.length > 0}
        canNext={rows.length === ACCOUNT_PAGE}
        onPrev={() => setCursors((prev) => prev.slice(0, -1))}
        onNext={() => setCursors((prev) => [...prev, rows[rows.length - 1]])}
      />

      <AddAccountDialog
        open={adding}
        onOpenChange={setAdding}
        domainHint={domainOf(overview.profile.email)}
        pending={action.isPending}
        onSubmit={async (email) => {
          try {
            await action.mutateAsync({ action: "addAccount", params: { email } });
            toast.success(`已添加 ${email}`);
            setAdding(false);
          } catch (error) {
            toast.error(errorText(error, "添加失败"));
          }
        }}
      />
      <ConfirmDialog
        open={Boolean(removing)}
        title={`删除 ${removing?.email ?? ""}`}
        description={
          <>
            <p>这个地址将不再收信。邮局默认标记删除，开启同步删除时会连同邮件永久删除。</p>
            {removing?.accountId === overview.senderAccountId ? (
              <p className="text-destructive">它是这条通道的发信账号，删除后通道发信会失败，需要改通道里的发件地址。</p>
            ) : null}
          </>
        }
        confirmLabel="删除"
        destructive
        pending={action.isPending}
        onConfirm={() => void remove()}
        onOpenChange={(open) => !open && setRemoving(null)}
      />
    </div>
  );
}

function AddAccountDialog({
  open,
  onOpenChange,
  domainHint,
  pending,
  onSubmit
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domainHint: string;
  pending: boolean;
  onSubmit: (email: string) => Promise<void>;
}) {
  const [email, setEmail] = useState("");
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) setEmail("");
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>添加邮箱</DialogTitle>
          <DialogDescription>域名必须是邮局已配置的域名，不能添加已存在或已注销的地址。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            void onSubmit(email.trim());
          }}
        >
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">完整邮箱地址</Label>
            <Input type="email" value={email} placeholder={`notice${domainHint}`} onChange={(event) => setEmail(event.target.value)} />
          </div>
          <p className="text-[11px] text-muted-foreground">
            邮局若开启了「添加邮箱人机验证」，这里会被拒绝，需要到邮局网页里添加。
          </p>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" disabled={!email.trim() || pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              添加
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ── 用户（管理员） ──

const USER_PAGE = 20;

export function UsersView({ scope, configId, overview, canWrite }: MailboxContext) {
  const [keyword, setKeyword] = useState("");
  const debounced = useDebouncedValue(keyword.trim(), 400);
  const [status, setStatus] = useState<"-1" | "0" | "1">("-1");
  const [deleted, setDeleted] = useState<"0" | "1">("0");
  const [page, setPage] = useState(1);
  const query = useSkymailQuery(scope, configId, "users", {
    num: page,
    size: USER_PAGE,
    timeSort: 0,
    email: debounced || undefined,
    status: Number(status) as -1 | 0 | 1,
    isDel: Number(deleted) as 0 | 1
  });
  const action = useSkymailAction(scope, configId);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<SkymailUser | null>(null);
  const rows = query.data?.list ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / USER_PAGE));

  const toggle = async (user: SkymailUser) => {
    const next = user.status === 0 ? 1 : 0;
    try {
      await action.mutateAsync({ action: "setUserStatus", params: { userId: user.userId, status: next } });
      toast.success(next === 1 ? `已禁用 ${user.email}，它的登录会话全部失效` : `已启用 ${user.email}`);
    } catch (error) {
      toast.error(errorText(error));
    }
  };

  const remove = async () => {
    if (!removing) return;
    try {
      await action.mutateAsync({ action: "deleteUsers", params: { userIds: [removing.userId] } });
      toast.success(`已永久删除 ${removing.email}`);
      setRemoving(null);
    } catch (error) {
      toast.error(errorText(error));
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-56">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8 text-sm"
            placeholder="邮箱前缀"
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
          />
        </div>
        <Select
          value={status}
          onValueChange={(value) => {
            setStatus(value as typeof status);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="-1">全部状态</SelectItem>
            <SelectItem value="0">正常</SelectItem>
            <SelectItem value="1">已禁用</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={deleted}
          onValueChange={(value) => {
            setDeleted(value as typeof deleted);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">未注销</SelectItem>
            <SelectItem value="1">已注销</SelectItem>
          </SelectContent>
        </Select>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs text-muted-foreground">共 {total} 人</span>
          {canWrite ? (
            <Button size="sm" className="h-8" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" /> 添加用户
            </Button>
          ) : null}
        </div>
      </div>
      {query.error ? <ErrorNote error={query.error} onRetry={() => void query.refetch()} /> : null}
      <div className="overflow-hidden rounded-2xl border bg-card">
        {query.isLoading ? (
          <TableSkeleton />
        ) : rows.length === 0 ? (
          <EmptyNote icon={<UserRound className="size-6" />} title="没有符合条件的用户" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 text-left font-medium">用户</th>
                  <th className="px-3 py-2.5 text-left font-medium">收 / 发 / 邮箱</th>
                  <th className="px-3 py-2.5 text-left font-medium">发件额度</th>
                  <th className="px-3 py-2.5 text-left font-medium">最近活跃</th>
                  <th className="px-3 py-2.5 text-left font-medium">注册</th>
                  {canWrite ? <th className="px-3 py-2.5 text-right font-medium">启用</th> : null}
                  {canWrite ? <th className="w-12 px-3 py-2.5" /> : null}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const self = row.userId === overview.profile.userId;
                  return (
                    <tr key={row.userId} className={cn("border-b last:border-b-0", row.isDel === 1 && "opacity-60")}>
                      <td className="px-4 py-2.5">
                        <div className="flex items-center gap-1.5">
                          <span className="font-mono text-xs">{row.email}</span>
                          {row.type === 0 ? (
                            <Badge variant="info" size="sm">
                              管理员
                            </Badge>
                          ) : null}
                          {row.status === 1 ? (
                            <Badge variant="danger" size="sm">
                              已禁用
                            </Badge>
                          ) : null}
                          {self ? (
                            <Badge variant="secondary" size="sm">
                              通道账号
                            </Badge>
                          ) : null}
                        </div>
                        {row.platform ? (
                          <p className="text-[11px] text-muted-foreground">
                            {row.platform} 登录：{row.name || row.username}
                          </p>
                        ) : null}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs tabular-nums">
                        {row.receiveEmailCount} / {row.sendEmailCount} / {row.accountCount}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs text-muted-foreground">
                        {!row.sendAction?.hasPerm
                          ? "无发件权限"
                          : row.sendAction.sendCount
                            ? `${row.sendCount}/${row.sendAction.sendCount}${row.sendAction.sendType === "day" ? " 每日" : " 总量"}`
                            : `${row.sendCount}，不限`}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs text-muted-foreground">
                        <span className="tabular-nums">{formatDateTimeMinute(row.activeTime)}</span>
                        {row.activeIp ? <p className="text-[11px]">{row.activeIp}</p> : null}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-xs tabular-nums text-muted-foreground">
                        {formatDateTimeMinute(row.createTime)}
                      </td>
                      {canWrite ? (
                        <td className="px-3 py-2.5 text-right">
                          <Switch
                            checked={row.status === 0}
                            disabled={self || row.isDel === 1 || action.isPending}
                            aria-label={row.status === 0 ? "禁用" : "启用"}
                            onCheckedChange={() => void toggle(row)}
                          />
                        </td>
                      ) : null}
                      {canWrite ? (
                        <td className="px-3 py-2.5 text-right">
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7 text-muted-foreground hover:text-destructive"
                            aria-label="永久删除用户"
                            disabled={self}
                            title={self ? "不能删除这条通道自己的登录账号" : undefined}
                            onClick={() => setRemoving(row)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </td>
                      ) : null}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <Pager
        label={`${page} / ${totalPages}`}
        canPrev={page > 1}
        canNext={page < totalPages}
        onPrev={() => setPage(page - 1)}
        onNext={() => setPage(page + 1)}
      />

      <AddUserDialog
        open={adding}
        onOpenChange={setAdding}
        domainHint={domainOf(overview.profile.email)}
        pending={action.isPending}
        onSubmit={async (email, password) => {
          try {
            await action.mutateAsync({ action: "addUser", params: { email, password } });
            toast.success(`已添加用户 ${email}`);
            setAdding(false);
          } catch (error) {
            toast.error(errorText(error, "添加失败"));
          }
        }}
      />
      <ConfirmDialog
        open={Boolean(removing)}
        title={`永久删除用户 ${removing?.email ?? ""}`}
        description="用户连同他的全部邮箱、邮件、星标与第三方登录绑定一起删除，无法恢复。只想暂停使用的话，关掉「启用」即可。"
        confirmLabel="永久删除"
        destructive
        pending={action.isPending}
        onConfirm={() => void remove()}
        onOpenChange={(open) => !open && setRemoving(null)}
      />
    </div>
  );
}

function AddUserDialog({
  open,
  onOpenChange,
  domainHint,
  pending,
  onSubmit
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  domainHint: string;
  pending: boolean;
  onSubmit: (email: string, password: string) => Promise<void>;
}) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const close = (next: boolean) => {
    onOpenChange(next);
    if (!next) {
      setEmail("");
      setPassword("");
    }
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>添加用户</DialogTitle>
          <DialogDescription>新用户获得邮局的默认权限身份，可以用这个邮箱和密码登录邮局网页。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          autoComplete="off"
          onSubmit={(event) => {
            event.preventDefault();
            void onSubmit(email.trim(), password);
          }}
        >
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">邮箱地址</Label>
            <Input type="email" value={email} placeholder={`someone${domainHint}`} onChange={(event) => setEmail(event.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">初始密码</Label>
            <Input
              type="password"
              autoComplete="new-password"
              value={password}
              placeholder="至少 6 位"
              onChange={(event) => setPassword(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => close(false)}>
              取消
            </Button>
            <Button type="submit" disabled={!email.trim() || password.length < 6 || pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              添加
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ── 注册码（管理员） ──

export function RegKeysView({ scope, configId, canWrite }: MailboxContext) {
  const [keyword, setKeyword] = useState("");
  const debounced = useDebouncedValue(keyword.trim(), 400);
  const query = useSkymailQuery(scope, configId, "regKeys", { code: debounced || undefined });
  const action = useSkymailAction(scope, configId);
  const [adding, setAdding] = useState(false);
  const [history, setHistory] = useState<SkymailRegKey | null>(null);
  const [removing, setRemoving] = useState<SkymailRegKey | null>(null);
  const rows = query.data ?? [];

  const remove = async () => {
    if (!removing) return;
    try {
      await action.mutateAsync({ action: "deleteRegKeys", params: { regKeyIds: [removing.regKeyId] } });
      toast.success(`已删除注册码 ${removing.code}`);
      setRemoving(null);
    } catch (error) {
      toast.error(errorText(error));
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-56">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="h-8 pl-8 text-sm" placeholder="注册码前缀" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
        </div>
        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs text-muted-foreground">共 {rows.length} 个</span>
          {canWrite ? (
            <Button size="sm" className="h-8" onClick={() => setAdding(true)}>
              <Plus className="size-3.5" /> 新建注册码
            </Button>
          ) : null}
        </div>
      </div>
      {query.error ? <ErrorNote error={query.error} onRetry={() => void query.refetch()} /> : null}
      <div className="overflow-hidden rounded-2xl border bg-card">
        {query.isLoading ? (
          <TableSkeleton rows={4} />
        ) : rows.length === 0 ? (
          <EmptyNote icon={<KeyRound className="size-6" />} title="还没有注册码" hint="邮局开启「注册码注册」后，凭码注册的用户获得对应的权限身份" />
        ) : (
          <table className="w-full text-sm">
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 text-left font-medium">注册码</th>
                <th className="px-3 py-2.5 text-left font-medium">权限身份</th>
                <th className="px-3 py-2.5 text-left font-medium">剩余次数</th>
                <th className="px-3 py-2.5 text-left font-medium">有效期</th>
                <th className="px-3 py-2.5 text-left font-medium">创建时间</th>
                <th className="w-20 px-3 py-2.5" />
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const expired = row.expireTime === null;
                return (
                  <tr key={row.regKeyId} className={cn("border-b last:border-b-0", (expired || row.count <= 0) && "opacity-60")}>
                    <td className="px-4 py-2.5 font-mono text-xs">{row.code}</td>
                    <td className="px-3 py-2.5 text-xs">{row.roleName || `#${row.roleId}`}</td>
                    <td className="px-3 py-2.5 text-xs tabular-nums">{row.count}</td>
                    <td className="whitespace-nowrap px-3 py-2.5 text-xs">
                      {expired ? <span className="text-muted-foreground">已过期</span> : `至 ${formatDateTimeMinute(row.expireTime)}`}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2.5 text-xs tabular-nums text-muted-foreground">
                      {formatDateTimeMinute(row.createTime)}
                    </td>
                    <td className="px-3 py-2.5 text-right">
                      <div className="flex justify-end gap-0.5">
                        <Button size="icon" variant="ghost" className="size-7 text-muted-foreground" aria-label="使用记录" onClick={() => setHistory(row)}>
                          <History className="size-3.5" />
                        </Button>
                        {canWrite ? (
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-7 text-muted-foreground hover:text-destructive"
                            aria-label="删除注册码"
                            onClick={() => setRemoving(row)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {adding ? (
        <AddRegKeyDialog
          scope={scope}
          configId={configId}
          pending={action.isPending}
          onClose={() => setAdding(false)}
          onSubmit={async (params) => {
            try {
              await action.mutateAsync({ action: "addRegKey", params });
              toast.success(`已新建注册码 ${params.code}`);
              setAdding(false);
            } catch (error) {
              toast.error(errorText(error, "新建失败"));
            }
          }}
        />
      ) : null}
      <RegKeyHistoryDialog scope={scope} configId={configId} regKey={history} onClose={() => setHistory(null)} />
      <ConfirmDialog
        open={Boolean(removing)}
        title={`删除注册码 ${removing?.code ?? ""}`}
        description="已经用它注册的用户不受影响，之后不能再用它注册。"
        confirmLabel="删除"
        destructive
        pending={action.isPending}
        onConfirm={() => void remove()}
        onOpenChange={(open) => !open && setRemoving(null)}
      />
    </div>
  );
}

function randomCode() {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
  const bytes = new Uint8Array(12);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join("");
}

function defaultExpire() {
  const date = new Date();
  date.setDate(date.getDate() + 30);
  return date.toISOString().slice(0, 10);
}

function AddRegKeyDialog({
  scope,
  configId,
  pending,
  onClose,
  onSubmit
}: {
  scope: EmailScope;
  configId: number;
  pending: boolean;
  onClose: () => void;
  onSubmit: (params: { code: string; roleId: number; count: number; expireTime: string }) => Promise<void>;
}) {
  const roles = useSkymailQuery(scope, configId, "roles", {});
  const [code, setCode] = useState(randomCode);
  const [roleId, setRoleId] = useState("");
  const [count, setCount] = useState("1");
  const [expire, setExpire] = useState(defaultExpire);
  const roleOptions = (roles.data ?? []).filter((role) => role.roleId);
  const today = new Date().toISOString().slice(0, 10);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>新建注册码</DialogTitle>
          <DialogDescription>凭这个码注册的用户获得所选权限身份。注册码不能重复。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            void onSubmit({ code: code.trim(), roleId: Number(roleId), count: Number(count), expireTime: expire });
          }}
        >
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">注册码</Label>
            <div className="flex gap-2">
              <Input value={code} maxLength={64} className="font-mono" onChange={(event) => setCode(event.target.value)} />
              <Button type="button" size="icon" variant="outline" aria-label="随机生成" onClick={() => setCode(randomCode())}>
                <Dices className="size-4" />
              </Button>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">权限身份</Label>
            {roleOptions.length ? (
              <Select value={roleId} onValueChange={setRoleId}>
                <SelectTrigger className="w-full text-sm">
                  <SelectValue placeholder="选择身份" />
                </SelectTrigger>
                <SelectContent>
                  {roleOptions.map((role) => (
                    <SelectItem key={role.roleId} value={String(role.roleId)}>
                      {role.name}
                      {role.sendCount ? `（发件 ${role.sendCount}${role.sendType === "day" ? "/天" : ""}）` : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <>
                <Input
                  type="number"
                  min={1}
                  value={roleId}
                  placeholder="身份 id"
                  onChange={(event) => setRoleId(event.target.value)}
                />
                <p className="text-[11px] text-muted-foreground">
                  {roles.isLoading ? "正在读取身份列表…" : "没取到身份列表（邮局版本可能较旧），请填邮局「权限」页里的身份 id。"}
                </p>
              </>
            )}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">可用次数</Label>
              <Input type="number" min={1} value={count} onChange={(event) => setCount(event.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">有效期至</Label>
              <Input type="date" min={today} value={expire} onChange={(event) => setExpire(event.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" disabled={!code.trim() || !(Number(roleId) > 0) || !(Number(count) > 0) || !expire || pending}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              新建
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RegKeyHistoryDialog({
  scope,
  configId,
  regKey,
  onClose
}: {
  scope: EmailScope;
  configId: number;
  regKey: SkymailRegKey | null;
  onClose: () => void;
}) {
  const query = useSkymailQuery(scope, configId, "regKeyHistory", { regKeyId: regKey?.regKeyId ?? 0 }, { enabled: Boolean(regKey) });
  const rows = query.data ?? [];
  return (
    <Dialog open={Boolean(regKey)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{regKey?.code} 的使用记录</DialogTitle>
          <DialogDescription>用这个注册码注册的用户。</DialogDescription>
        </DialogHeader>
        {query.error ? <ErrorNote error={query.error} /> : null}
        {query.isLoading ? (
          <TableSkeleton rows={3} />
        ) : rows.length === 0 ? (
          <EmptyNote icon={<History className="size-6" />} title="还没有人用过" />
        ) : (
          <ul className="max-h-80 divide-y overflow-auto rounded-xl border text-sm">
            {rows.map((row, index) => (
              <li key={`${row.email}-${index}`} className="flex items-center justify-between px-3 py-2">
                <span className="font-mono text-xs">{row.email}</span>
                <span className="text-xs tabular-nums text-muted-foreground">{formatDateTimeMinute(row.createTime)}</span>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
