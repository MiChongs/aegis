"use client";

import { useMemo, useState } from "react";
import {
  ArrowDownLeft,
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  Download,
  Loader2,
  Mail,
  RefreshCw,
  Search,
  SlidersHorizontal,
  X
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useAdminWalletTransactionsQuery,
  useDownloadWalletReceiptMutation,
  useEmailWalletReceiptMutation,
  useReceiptRecipients,
  type CommerceWindow
} from "@/lib/commerce-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { SignedAmount, WalletTypeBadge, formatMoney, formatTime, transactionActor } from "./commerce-format";
import { WalletAdjustDialog } from "./wallet-adjust-dialog";
import { ReceiptEmailDialog } from "./receipt-email-dialog";
import { UserPicker, type PickedUser } from "./user-picker";

const ALL = "__all__";

const typeOptions = [
  { value: "recharge", label: "余额充值" },
  { value: "consume", label: "余额消费" },
  { value: "refund", label: "退款入账" },
  { value: "admin_adjust", label: "管理员调整" },
  { value: "vip_purchase", label: "会员开通" },
  { value: "order_pay", label: "订单支付" }
];

type EmailTarget = { transactionNo: string; subject: string; userId: number };

/**
 * 钱包流水台。
 *
 * 这里是「用钱包付的钱」在控制台里唯一的落点：余额直购会员、业务消费、
 * 管理员调账都不产生支付订单，在订单页一条都看不到。每行都能直接开凭证 ——
 * 凭证由后端决定是流水自己出还是委派给关联订单，前端不做这个判断。
 *
 * 筛选一律**即时生效**（关键词 300ms 防抖），没有「查询」按钮：
 * 让人先打字、再去点一个按钮，是把本该由机器承担的节流转嫁给了操作者。
 */
export function WalletTransactionsPanel({
  appKey,
  window,
  receiptLocale,
  receiptLocaleLabel,
  walletCurrency
}: {
  appKey?: string | null;
  window: CommerceWindow;
  receiptLocale?: string;
  receiptLocaleLabel?: string;
  walletCurrency?: string;
}) {
  const [type, setType] = useState(ALL);
  const [direction, setDirection] = useState(ALL);
  const [keywordInput, setKeywordInput] = useState("");
  const keyword = useDebouncedValue(keywordInput.trim(), 300);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [page, setPage] = useState(1);
  const [adjustOpen, setAdjustOpen] = useState(false);
  // 每次打开换一个 key，让调账表单以空白重挂（不用「open 变 true 就清空」的副作用）
  const [adjustSession, setAdjustSession] = useState(0);
  const [emailTarget, setEmailTarget] = useState<EmailTarget | null>(null);

  const filters = useMemo(
    () => ({
      type: type === ALL ? undefined : type,
      direction: direction === ALL ? undefined : direction,
      keyword: keyword || undefined,
      userId: user?.id,
      start: window.start,
      end: window.end,
      // 任一筛选条件变了都要回到第 1 页，否则会停在一个新条件下不存在的页码上
      page,
      limit: 20
    }),
    [type, direction, keyword, user?.id, window.start, window.end, page]
  );

  const query = useAdminWalletTransactionsQuery(appKey, filters);
  const downloadMutation = useDownloadWalletReceiptMutation(appKey);
  const emailMutation = useEmailWalletReceiptMutation(appKey);
  const recipients = useReceiptRecipients(appKey, emailTarget?.userId ?? null);

  const data = query.data;
  const items = data?.items ?? [];
  const totalPages = data?.totalPages ?? 1;
  const filtered = type !== ALL || direction !== ALL || Boolean(keyword) || Boolean(user);

  /** 任何筛选变化都走这里：改条件 + 回首页是一件事，分开写迟早漏一处 */
  function refine(apply: () => void) {
    apply();
    setPage(1);
  }

  async function handleDownload(transactionNo: string) {
    try {
      await downloadMutation.mutateAsync({ transactionNo, locale: receiptLocale });
      toast.success("凭证已生成");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "凭证生成失败");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 w-60 pl-8 pr-7 text-xs"
            placeholder="流水号 / 订单号 / 账号 / 说明"
            value={keywordInput}
            onChange={(event) => refine(() => setKeywordInput(event.target.value))}
          />
          {keywordInput ? (
            <button
              type="button"
              aria-label="清空关键词"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => refine(() => setKeywordInput(""))}
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>

        <div className="w-52">
          <UserPicker
            appKey={appKey}
            value={user}
            onChange={(picked) => refine(() => setUser(picked))}
            placeholder="全部用户"
          />
        </div>

        <Select value={type} onValueChange={(value) => refine(() => setType(value))}>
          <SelectTrigger className="h-8 w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部类型</SelectItem>
            {typeOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* 收支只有三档，用开关比下拉少一次点击 */}
        <ToggleGroup
          type="single"
          value={direction}
          onValueChange={(value) => refine(() => setDirection(value || ALL))}
          className="gap-1"
        >
          <ToggleGroupItem
            value="in"
            className="h-8 gap-1 border px-2.5 text-xs data-[state=on]:border-emerald-500/40 data-[state=on]:bg-emerald-500/10 data-[state=on]:text-emerald-600"
          >
            <ArrowUpRight className="size-3.5" />
            入账
          </ToggleGroupItem>
          <ToggleGroupItem
            value="out"
            className="h-8 gap-1 border px-2.5 text-xs data-[state=on]:border-amber-500/40 data-[state=on]:bg-amber-500/10 data-[state=on]:text-amber-600"
          >
            <ArrowDownLeft className="size-3.5" />
            出账
          </ToggleGroupItem>
        </ToggleGroup>

        {filtered ? (
          <Button
            size="sm"
            variant="ghost"
            className="h-8 gap-1 text-xs text-muted-foreground"
            onClick={() =>
              refine(() => {
                setType(ALL);
                setDirection(ALL);
                setKeywordInput("");
                setUser(null);
              })
            }
          >
            <X className="size-3" />
            清空筛选
          </Button>
        ) : null}

        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs tabular-nums text-muted-foreground">
            {query.isFetching ? <Loader2 className="inline size-3 animate-spin" /> : null} 共 {data?.total ?? 0} 笔
          </span>
          <Button
            size="sm"
            variant="outline"
            className="h-8 gap-1 text-xs"
            disabled={!appKey}
            onClick={() => {
              setAdjustSession((value) => value + 1);
              setAdjustOpen(true);
            }}
          >
            <SlidersHorizontal className="size-3" />
            调整余额
          </Button>
          <Button size="sm" variant="ghost" className="h-8 gap-1 text-xs" onClick={() => void query.refetch()}>
            <RefreshCw className="size-3" />
            刷新
          </Button>
        </div>
      </div>

      {query.isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className="h-10 w-full rounded-lg" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <div className="space-y-2 py-12 text-center">
          <p className="text-sm text-muted-foreground">{filtered ? "该条件下没有钱包流水" : "还没有钱包流水"}</p>
          {filtered ? (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              onClick={() =>
                refine(() => {
                  setType(ALL);
                  setDirection(ALL);
                  setKeywordInput("");
                  setUser(null);
                })
              }
            >
              清空筛选
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>流水号</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>说明</TableHead>
                <TableHead className="text-right">金额</TableHead>
                <TableHead className="text-right">变动后余额</TableHead>
                <TableHead>关联订单</TableHead>
                <TableHead>时间</TableHead>
                <TableHead className="text-right">凭证</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="font-mono text-xs">{item.transactionNo}</TableCell>
                  <TableCell className="max-w-32 truncate text-xs">
                    {/* 点账号即按该用户筛选：比「复制 UID、粘进筛选框」快一个数量级 */}
                    <button
                      type="button"
                      className="truncate hover:underline"
                      title={`只看 ${transactionActor(item)} 的流水`}
                      onClick={() =>
                        refine(() =>
                          setUser({
                            id: item.userId,
                            account: item.account,
                            nickname: item.nickname
                          })
                        )
                      }
                    >
                      {transactionActor(item)}
                    </button>
                  </TableCell>
                  <TableCell>
                    <WalletTypeBadge type={item.type} />
                  </TableCell>
                  <TableCell className="max-w-48 truncate text-xs text-muted-foreground" title={item.remark || item.title}>
                    {item.title}
                    {item.remark ? <span className="text-muted-foreground/70"> · {item.remark}</span> : null}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">
                    <SignedAmount value={item.amount} currency={walletCurrency} />
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {formatMoney(item.balanceAfter)}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {item.relatedOrderNo ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="inline-flex items-center gap-1.5">
                            {item.relatedOrderNo}
                            <Badge variant="outline" className="h-4 px-1 text-[10px]">
                              订单出具
                            </Badge>
                          </span>
                        </TooltipTrigger>
                        <TooltipContent side="top" className="max-w-64 text-[11px] leading-relaxed">
                          同一笔钱只出一份凭证。这条流水的凭证由订单 {item.relatedOrderNo} 出具，
                          与在订单页下载到的是同一份文件。
                        </TooltipContent>
                      </Tooltip>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{formatTime(item.createdAt)}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-xs"
                        disabled={downloadMutation.isPending}
                        onClick={() => void handleDownload(item.transactionNo)}
                      >
                        <Download className="size-3" />
                        下载
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 gap-1 text-xs"
                        onClick={() =>
                          setEmailTarget({
                            transactionNo: item.transactionNo,
                            subject: item.title,
                            userId: item.userId
                          })
                        }
                      >
                        <Mail className="size-3" />
                        寄送
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-end gap-2">
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={page <= 1}
            onClick={() => setPage((value) => value - 1)}
          >
            <ChevronLeft className="size-3" />
            上一页
          </Button>
          <span className="text-xs tabular-nums text-muted-foreground">
            {page} / {totalPages}
          </span>
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={page >= totalPages}
            onClick={() => setPage((value) => value + 1)}
          >
            下一页
            <ChevronRight className="size-3" />
          </Button>
        </div>
      )}

      <WalletAdjustDialog
        key={adjustSession}
        appKey={appKey}
        open={adjustOpen}
        onOpenChange={setAdjustOpen}
        walletCurrency={walletCurrency}
      />
      <ReceiptEmailDialog
        open={Boolean(emailTarget)}
        onOpenChange={(open) => !open && setEmailTarget(null)}
        title="寄送钱包流水凭证"
        subject={emailTarget?.subject ?? ""}
        reference={emailTarget?.transactionNo ?? ""}
        suggestions={recipients}
        localeLabel={receiptLocaleLabel}
        pending={emailMutation.isPending}
        onSubmit={async (to) => {
          if (!emailTarget) return;
          await emailMutation.mutateAsync({ transactionNo: emailTarget.transactionNo, to, locale: receiptLocale });
          setEmailTarget(null);
        }}
      />
    </div>
  );
}
