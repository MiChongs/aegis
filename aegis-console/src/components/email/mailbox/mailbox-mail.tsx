"use client";

import { useMemo, useRef, useState } from "react";
import { Forward, Inbox, Loader2, Paperclip, PenLine, RefreshCw, Reply, Search, Send, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import type { SkymailEmail, SkymailOverview, SkymailSendAttachment } from "@/lib/api/email-skymail";
import { formatDateTime } from "@/lib/datetime";
import type { EmailScope } from "@/lib/email-hooks";
import { useSkymailAction, useSkymailQuery } from "@/lib/email-skymail-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import {
  ConfirmDialog,
  EmptyNote,
  ErrorNote,
  MailStatusBadge,
  MailTable,
  Pager,
  TableSkeleton,
  errorText,
  formatBytes
} from "./mailbox-shared";

const PAGE_SIZE = 20;

type MailboxContext = { scope: EmailScope; configId: number; overview: SkymailOverview; canWrite: boolean };

// ── 收件箱 / 已发送 ──

/**
 * 一个邮箱账号的收件箱或已发送。
 *
 * Cloud Mail 的邮件列表是游标分页（「比这封更早的 N 封」），没有页码：
 * 这里记一摞走过的游标，「上一页」就是弹栈。
 */
export function MailFolderView({ folder, ...ctx }: MailboxContext & { folder: 0 | 1 }) {
  const { scope, configId, overview, canWrite } = ctx;
  const defaultAccount = overview.senderAccountId || overview.profile.account?.accountId || overview.accounts[0]?.accountId || 0;
  const [accountId, setAccountId] = useState(defaultAccount);
  const [cursors, setCursors] = useState<number[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [opened, setOpened] = useState<SkymailEmail | null>(null);
  const [compose, setCompose] = useState<ComposeDraft | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const cursor = cursors[cursors.length - 1];
  const query = useSkymailQuery(scope, configId, "emails", {
    accountId,
    type: folder,
    emailId: cursor,
    size: PAGE_SIZE,
    timeSort: 0
  }, { enabled: accountId > 0 });
  const action = useSkymailAction(scope, configId);
  const rows = query.data?.list ?? [];

  const reset = (next: number) => {
    setAccountId(next);
    setCursors([]);
    setSelected(new Set());
  };

  const removeSelected = async () => {
    try {
      await action.mutateAsync({ action: "deleteEmails", params: { emailIds: [...selected] } });
      toast.success(`已删除 ${selected.size} 封邮件`);
      setSelected(new Set());
      setConfirmDelete(false);
    } catch (error) {
      toast.error(errorText(error));
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <AccountSelect overview={overview} value={accountId} onChange={reset} />
        <Button size="sm" variant="outline" className="h-8" onClick={() => void query.refetch()} disabled={query.isFetching}>
          <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} /> 刷新
        </Button>
        <div className="ml-auto flex items-center gap-2">
          {query.data ? <span className="text-xs text-muted-foreground">共 {query.data.total} 封</span> : null}
          {canWrite && selected.size > 0 ? (
            <Button size="sm" variant="outline" className="h-8 text-destructive" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="size-3.5" /> 删除 {selected.size} 封
            </Button>
          ) : null}
          {canWrite ? (
            <Button size="sm" className="h-8" onClick={() => setCompose({ accountId })}>
              <PenLine className="size-3.5" /> 写信
            </Button>
          ) : null}
        </div>
      </div>

      {query.error ? <ErrorNote error={query.error} onRetry={() => void query.refetch()} /> : null}

      <div className="overflow-hidden rounded-2xl border bg-card">
        {accountId <= 0 ? (
          <EmptyNote icon={<Inbox className="size-6" />} title="这个账号下还没有邮箱" hint="先在「邮箱账号」里添加一个" />
        ) : query.isLoading ? (
          <TableSkeleton />
        ) : rows.length === 0 ? (
          <EmptyNote icon={<Inbox className="size-6" />} title={folder === 0 ? "收件箱是空的" : "还没有发出过邮件"} />
        ) : (
          <MailTable
            rows={rows}
            mode={folder === 0 ? "inbox" : "sent"}
            selected={canWrite ? selected : undefined}
            onToggle={(id) =>
              setSelected((prev) => {
                const next = new Set(prev);
                if (next.has(id)) next.delete(id);
                else next.add(id);
                return next;
              })
            }
            onToggleAll={(checked) => setSelected(checked ? new Set(rows.map((row) => row.emailId)) : new Set())}
            onOpen={setOpened}
          />
        )}
      </div>

      <Pager
        label={`第 ${cursors.length + 1} 页`}
        canPrev={cursors.length > 0}
        canNext={rows.length === PAGE_SIZE}
        onPrev={() => setCursors((prev) => prev.slice(0, -1))}
        onNext={() => setCursors((prev) => [...prev, rows[rows.length - 1].emailId])}
      />

      <MailDetailDialog
        {...ctx}
        source={opened ? { kind: "user", accountId, type: folder, emailId: opened.emailId } : null}
        onOpenChange={(open) => !open && setOpened(null)}
        onReply={(email, mode) => {
          setOpened(null);
          setCompose(draftFrom(email, mode, accountId));
        }}
      />
      <ComposeDialog {...ctx} draft={compose} onClose={() => setCompose(null)} />
      <ConfirmDialog
        open={confirmDelete}
        title={`删除 ${selected.size} 封邮件`}
        description="邮局默认标记删除；如果邮局开启了同步删除，邮件会被永久删除。"
        confirmLabel="删除"
        destructive
        pending={action.isPending}
        onConfirm={() => void removeSelected()}
        onOpenChange={setConfirmDelete}
      />
    </div>
  );
}

function AccountSelect({ overview, value, onChange }: { overview: SkymailOverview; value: number; onChange: (id: number) => void }) {
  const accounts = overview.accounts.filter((account) => account.isDel === 0);
  if (accounts.length <= 1) {
    return accounts[0] ? <Badge variant="outline">{accounts[0].email}</Badge> : null;
  }
  return (
    <Select value={String(value)} onValueChange={(next) => onChange(Number(next))}>
      <SelectTrigger className="h-8 w-60 text-sm">
        <SelectValue placeholder="选择邮箱" />
      </SelectTrigger>
      <SelectContent>
        {accounts.map((account) => (
          <SelectItem key={account.accountId} value={String(account.accountId)}>
            {account.email}
            {account.accountId === overview.senderAccountId ? "（发信账号）" : ""}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// ── 全部邮件（管理员） ──

const ALL_TYPES = [
  { value: "receive", label: "收件" },
  { value: "send", label: "发件" },
  { value: "all", label: "全部" },
  { value: "delete", label: "已删除" },
  { value: "noone", label: "无人收件" }
] as const;

/** 整个邮局的邮件。删除是**永久**的（连同附件与星标），与用户自己删信不同。 */
export function AllMailView(ctx: MailboxContext) {
  const { scope, configId, canWrite } = ctx;
  const [type, setType] = useState<(typeof ALL_TYPES)[number]["value"]>("receive");
  const [field, setField] = useState<"subject" | "accountEmail" | "userEmail" | "name">("subject");
  const [keyword, setKeyword] = useState("");
  const debounced = useDebouncedValue(keyword.trim(), 400);
  const [cursors, setCursors] = useState<number[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [opened, setOpened] = useState<SkymailEmail | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const query = useSkymailQuery(scope, configId, "allEmails", {
    type,
    emailId: cursors[cursors.length - 1],
    size: PAGE_SIZE,
    timeSort: 0,
    ...(debounced ? { [field]: debounced } : {})
  });
  const action = useSkymailAction(scope, configId);
  const rows = query.data?.list ?? [];
  const resetPaging = () => {
    setCursors([]);
    setSelected(new Set());
  };

  const removeSelected = async () => {
    try {
      await action.mutateAsync({ action: "deleteAllEmails", params: { emailIds: [...selected] } });
      toast.success(`已永久删除 ${selected.size} 封邮件`);
      setSelected(new Set());
      setConfirmDelete(false);
    } catch (error) {
      toast.error(errorText(error));
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={type}
          onValueChange={(value) => {
            setType(value as typeof type);
            resetPaging();
          }}
        >
          <SelectTrigger className="h-8 w-28 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ALL_TYPES.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex items-center">
          <Select
            value={field}
            onValueChange={(value) => {
              setField(value as typeof field);
              resetPaging();
            }}
          >
            <SelectTrigger className="h-8 w-28 rounded-r-none border-r-0 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="subject">主题</SelectItem>
              <SelectItem value="accountEmail">收发地址</SelectItem>
              <SelectItem value="userEmail">所属用户</SelectItem>
              <SelectItem value="name">发件人名</SelectItem>
            </SelectContent>
          </Select>
          <div className="relative w-56">
            <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 rounded-l-none pl-8 text-sm"
              placeholder="前缀匹配"
              value={keyword}
              onChange={(event) => {
                setKeyword(event.target.value);
                resetPaging();
              }}
            />
          </div>
        </div>
        <Button size="sm" variant="outline" className="h-8" onClick={() => void query.refetch()} disabled={query.isFetching}>
          <RefreshCw className={cn("size-3.5", query.isFetching && "animate-spin")} /> 刷新
        </Button>
        <div className="ml-auto flex items-center gap-2">
          {query.data ? <span className="text-xs text-muted-foreground">共 {query.data.total} 封</span> : null}
          {canWrite && selected.size > 0 ? (
            <Button size="sm" variant="outline" className="h-8 text-destructive" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="size-3.5" /> 永久删除 {selected.size} 封
            </Button>
          ) : null}
        </div>
      </div>

      {query.error ? <ErrorNote error={query.error} onRetry={() => void query.refetch()} /> : null}

      <div className="overflow-hidden rounded-2xl border bg-card">
        {query.isLoading ? (
          <TableSkeleton />
        ) : rows.length === 0 ? (
          <EmptyNote icon={<Inbox className="size-6" />} title="没有符合条件的邮件" />
        ) : (
          <MailTable
            rows={rows}
            mode="all"
            selected={canWrite ? selected : undefined}
            onToggle={(id) =>
              setSelected((prev) => {
                const next = new Set(prev);
                if (next.has(id)) next.delete(id);
                else next.add(id);
                return next;
              })
            }
            onToggleAll={(checked) => setSelected(checked ? new Set(rows.map((row) => row.emailId)) : new Set())}
            onOpen={setOpened}
          />
        )}
      </div>

      <Pager
        label={`第 ${cursors.length + 1} 页`}
        canPrev={cursors.length > 0}
        canNext={rows.length === PAGE_SIZE}
        onPrev={() => setCursors((prev) => prev.slice(0, -1))}
        onNext={() => setCursors((prev) => [...prev, rows[rows.length - 1].emailId])}
      />

      <MailDetailDialog
        {...ctx}
        source={opened ? { kind: "all", emailId: opened.emailId } : null}
        onOpenChange={(open) => !open && setOpened(null)}
      />
      <ConfirmDialog
        open={confirmDelete}
        title={`永久删除 ${selected.size} 封邮件`}
        description="邮件连同附件、星标一起从邮局里删掉，无法恢复。"
        confirmLabel="永久删除"
        destructive
        pending={action.isPending}
        onConfirm={() => void removeSelected()}
        onOpenChange={setConfirmDelete}
      />
    </div>
  );
}

// ── 邮件详情 ──

type DetailSource =
  | { kind: "user"; accountId: number; type: 0 | 1; emailId: number }
  | { kind: "all"; emailId: number };

/**
 * 一封信的正文。HTML 放进**完全沙箱化**的 iframe（不给 allow-scripts、不给 allow-same-origin）：
 * 收件箱里的信来自任何人，正文里的脚本和表单一个都不能在控制台的源上跑。
 */
function MailDetailDialog({
  scope,
  configId,
  canWrite,
  source,
  onOpenChange,
  onReply
}: MailboxContext & {
  source: DetailSource | null;
  onOpenChange: (open: boolean) => void;
  onReply?: (email: SkymailEmail, mode: "reply" | "forward") => void;
}) {
  const userQuery = useSkymailQuery(
    scope,
    configId,
    "email",
    source?.kind === "user" ? { accountId: source.accountId, type: source.type, emailId: source.emailId } : { emailId: 0 },
    { enabled: source?.kind === "user" }
  );
  const allQuery = useSkymailQuery(scope, configId, "allEmail", { emailId: source?.kind === "all" ? source.emailId : 0 }, {
    enabled: source?.kind === "all"
  });
  const query = source?.kind === "all" ? allQuery : userQuery;
  const email = query.data;
  const [view, setView] = useState<"html" | "text">("html");

  return (
    <Dialog open={Boolean(source)} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-3 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="pr-6 leading-snug">{email?.subject || (query.isLoading ? "加载中…" : "（无主题）")}</DialogTitle>
          <DialogDescription asChild>
            <div className="space-y-0.5 text-xs">
              {email ? (
                <>
                  <p>
                    发件人：{email.name ? `${email.name} <${email.sendEmail}>` : email.sendEmail}
                  </p>
                  <p>收件人：{email.toName ? `${email.toName} <${email.toEmail}>` : email.toEmail}</p>
                  {email.cc && email.cc !== "[]" ? <p>抄送：{email.cc}</p> : null}
                  <p className="flex items-center gap-2">
                    <span>{formatDateTime(email.createTime)}</span>
                    <MailStatusBadge status={email.status} />
                  </p>
                </>
              ) : null}
            </div>
          </DialogDescription>
        </DialogHeader>

        {query.error ? <ErrorNote error={query.error} onRetry={() => void query.refetch()} /> : null}
        {query.isLoading ? <Skeleton className="h-72 w-full" /> : null}

        {email ? (
          <>
            {email.content && email.text ? (
              <div className="flex gap-1 self-start rounded-lg border bg-muted/40 p-0.5 text-xs">
                {(["html", "text"] as const).map((key) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setView(key)}
                    className={cn(
                      "rounded-md px-2.5 py-1",
                      view === key ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {key === "html" ? "网页正文" : "纯文本"}
                  </button>
                ))}
              </div>
            ) : null}
            <div className="min-h-0 flex-1 overflow-hidden rounded-xl border bg-white">
              {email.content && view === "html" ? (
                <iframe
                  title="邮件正文"
                  sandbox=""
                  referrerPolicy="no-referrer"
                  srcDoc={email.content}
                  className="h-[50vh] w-full"
                />
              ) : (
                <pre className="h-[50vh] overflow-auto whitespace-pre-wrap break-words p-4 font-sans text-sm text-zinc-800">
                  {email.text || "（没有正文）"}
                </pre>
              )}
            </div>
            {email.attList?.length ? (
              <div className="flex flex-wrap gap-1.5">
                {email.attList.map((att) => (
                  <Badge key={att.attId} variant="outline" className="gap-1 font-normal">
                    <Paperclip className="size-3" />
                    {att.filename}
                    <span className="text-muted-foreground">{formatBytes(att.size)}</span>
                  </Badge>
                ))}
              </div>
            ) : null}
          </>
        ) : null}

        {email && canWrite && onReply ? (
          <DialogFooter>
            {email.type === 0 ? (
              <Button variant="outline" onClick={() => onReply(email, "reply")}>
                <Reply className="size-3.5" /> 回复
              </Button>
            ) : null}
            <Button variant="outline" onClick={() => onReply(email, "forward")}>
              <Forward className="size-3.5" /> 转发
            </Button>
          </DialogFooter>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

// ── 写信 ──

type ComposeDraft = {
  accountId?: number;
  to?: string;
  subject?: string;
  body?: string;
  html?: boolean;
  sendType?: "reply" | "forward";
  emailId?: number;
};

function escapeHTML(value: string) {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function textToHTML(value: string) {
  return value
    .split(/\n{2,}/)
    .map((paragraph) => `<p>${escapeHTML(paragraph).replace(/\n/g, "<br>")}</p>`)
    .join("");
}

function draftFrom(email: SkymailEmail, mode: "reply" | "forward", accountId: number): ComposeDraft {
  const quoted = (email.text || "").split("\n").map((line) => `> ${line}`).join("\n");
  const header = `${formatDateTime(email.createTime)}，${email.name || email.sendEmail} 写道：`;
  if (mode === "reply") {
    return {
      accountId,
      sendType: "reply",
      emailId: email.emailId,
      to: email.sendEmail,
      subject: /^re:/i.test(email.subject) ? email.subject : `Re: ${email.subject}`,
      body: `\n\n${header}\n${quoted}`
    };
  }
  return {
    accountId,
    sendType: "forward",
    emailId: email.emailId,
    subject: /^fwd?:/i.test(email.subject) ? email.subject : `Fwd: ${email.subject}`,
    body: `\n\n---------- 转发的邮件 ----------\n发件人：${email.sendEmail}\n主题：${email.subject}\n${formatDateTime(email.createTime)}\n\n${email.text || ""}`
  };
}

const MAX_ATTACHMENTS = 10;
const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024;

type PickedFile = SkymailSendAttachment & { size: number };

function readAsDataURL(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

function splitRecipients(value: string) {
  return value
    .split(/[\s,，;；]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

/** 写信 / 回复 / 转发。正文默认按纯文本写，转成段落 HTML 发出；切到 HTML 则原样发送。 */
function ComposeDialog({
  scope,
  configId,
  overview,
  draft,
  onClose
}: MailboxContext & { draft: ComposeDraft | null; onClose: () => void }) {
  return draft ? <ComposeForm key={JSON.stringify(draft)} scope={scope} configId={configId} overview={overview} draft={draft} onClose={onClose} /> : null;
}

function ComposeForm({
  scope,
  configId,
  overview,
  draft,
  onClose
}: Omit<MailboxContext, "canWrite"> & { draft: ComposeDraft; onClose: () => void }) {
  const action = useSkymailAction(scope, configId);
  const accounts = overview.accounts.filter((account) => account.isDel === 0);
  const [accountId, setAccountId] = useState(draft.accountId || overview.senderAccountId || accounts[0]?.accountId || 0);
  const [to, setTo] = useState(draft.to ?? "");
  const [subject, setSubject] = useState(draft.subject ?? "");
  const [name, setName] = useState("");
  const [body, setBody] = useState(draft.body ?? "");
  const [html, setHTML] = useState(draft.html ?? false);
  const [files, setFiles] = useState<PickedFile[]>([]);
  const fileInput = useRef<HTMLInputElement>(null);
  const recipients = useMemo(() => splitRecipients(to), [to]);
  const totalBytes = files.reduce((sum, file) => sum + file.size, 0);
  const title = draft.sendType === "reply" ? "回复" : draft.sendType === "forward" ? "转发" : "写信";

  const pick = async (list: FileList | null) => {
    if (!list) return;
    const picked = [...files];
    for (const file of Array.from(list)) {
      if (picked.length >= MAX_ATTACHMENTS) {
        toast.error(`最多 ${MAX_ATTACHMENTS} 个附件`);
        break;
      }
      if (totalBytes + file.size > MAX_ATTACHMENT_BYTES) {
        toast.error("附件总大小不能超过 25 MB");
        break;
      }
      try {
        picked.push({ filename: file.name, contentType: file.type || undefined, content: await readAsDataURL(file), size: file.size });
      } catch {
        toast.error(`读取 ${file.name} 失败`);
      }
    }
    setFiles(picked);
  };

  const submit = async () => {
    try {
      await action.mutateAsync({
        action: "send",
        params: {
          accountId,
          receiveEmail: recipients,
          subject: subject.trim(),
          content: html ? body : textToHTML(body),
          text: html ? undefined : body,
          name: name.trim() || undefined,
          sendType: draft.sendType ?? "",
          emailId: draft.emailId,
          attachments: files.map(({ filename, content, contentType }) => ({ filename, content, contentType }))
        }
      });
      toast.success("已发出");
      onClose();
    } catch (error) {
      toast.error(errorText(error, "发送失败"));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>经 Cloud Mail 发出，会记入对方邮局的已发送，也会消耗该账号的发件次数。</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">发件邮箱</Label>
              <Select value={String(accountId)} onValueChange={(value) => setAccountId(Number(value))}>
                <SelectTrigger className="w-full text-sm">
                  <SelectValue placeholder="选择邮箱" />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((account) => (
                    <SelectItem key={account.accountId} value={String(account.accountId)}>
                      {account.email}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-[11px] font-medium">发件人名称</Label>
              <Input value={name} placeholder="留空用通道里配置的名称" onChange={(event) => setName(event.target.value)} />
            </div>
          </div>
          <div className="space-y-1.5">
            <div className="flex items-baseline justify-between">
              <Label className="text-[11px] font-medium">
                收件人<span className="text-destructive"> *</span>
              </Label>
              <span className="text-[10px] tabular-nums text-muted-foreground">{recipients.length}/50</span>
            </div>
            <Input value={to} placeholder="多个地址用逗号或空格分开" onChange={(event) => setTo(event.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">
              主题<span className="text-destructive"> *</span>
            </Label>
            <Input value={subject} onChange={(event) => setSubject(event.target.value)} />
          </div>
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label className="text-[11px] font-medium">
                正文<span className="text-destructive"> *</span>
              </Label>
              <div className="flex gap-1 rounded-lg border bg-muted/40 p-0.5 text-[11px]">
                {[
                  { key: false, label: "纯文本" },
                  { key: true, label: "HTML" }
                ].map((item) => (
                  <button
                    key={item.label}
                    type="button"
                    onClick={() => setHTML(item.key)}
                    className={cn(
                      "rounded-md px-2 py-0.5",
                      html === item.key ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
            <Textarea
              value={body}
              rows={10}
              className={cn("text-sm", html && "font-mono text-xs")}
              placeholder={html ? "<p>你好</p>" : "直接写正文，空一行即分段"}
              onChange={(event) => setBody(event.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <Button type="button" size="sm" variant="outline" className="h-7" onClick={() => fileInput.current?.click()}>
                <Paperclip className="size-3.5" /> 添加附件
              </Button>
              <span className="text-[11px] text-muted-foreground">
                {files.length}/{MAX_ATTACHMENTS} 个，{formatBytes(totalBytes)} / 25 MB
              </span>
              <input
                ref={fileInput}
                type="file"
                multiple
                className="hidden"
                onChange={(event) => {
                  void pick(event.target.files);
                  event.target.value = "";
                }}
              />
            </div>
            {files.length ? (
              <div className="flex flex-wrap gap-1.5">
                {files.map((file, index) => (
                  <Badge key={`${file.filename}-${index}`} variant="outline" className="gap-1 font-normal">
                    {file.filename}
                    <span className="text-muted-foreground">{formatBytes(file.size)}</span>
                    <button
                      type="button"
                      aria-label="移除附件"
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => setFiles((prev) => prev.filter((_, i) => i !== index))}
                    >
                      <X className="size-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            ) : null}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <Button
              type="submit"
              disabled={action.isPending || recipients.length === 0 || !subject.trim() || !body.trim() || accountId <= 0}
            >
              {action.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}
              发送
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
