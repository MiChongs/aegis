"use client";

/**
 * 余额流水。金额是 decimal 字符串，全程不转 number（formatMoney 只做展示）。
 * 后端只支持按类型过滤与分页，所以这里的筛选只有「类型」。
 */

import * as React from "react";
import { ArrowDownLeft, ArrowUpRight, Download, Loader2, Lock, Receipt, SlidersHorizontal, Wallet } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useAdminToken } from "@/lib/admin-hooks";
import { getAdminUserWalletTransactions } from "@/lib/api/app-users";
import type { UserWalletTransaction } from "@/lib/api/types";
import { useAdminUserWalletQuery, useAdminUserWalletTransactionsQuery } from "@/lib/app-user-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  Section,
  StateBlock,
  collectPages,
  downloadCsv,
  errorMessage,
  type Column
} from "../kit";
import {
  WALLET_TXN_LABEL,
  formatMoney,
  hasWallet,
  isNegativeAmount,
  txnSplitLabel,
  walletSplitLabel
} from "../user-detail-shared";

const TYPE_OPTIONS = Object.entries(WALLET_TXN_LABEL).map(([value, label]) => ({ value, label }));

function Money({ value, signed, className }: { value?: string; signed?: boolean; className?: string }) {
  const negative = isNegativeAmount(value);
  const zero = !value || /^[+-]?0*(\.0*)?$/.test(value.trim());
  return (
    <span
      className={cn(
        "font-medium tabular-nums",
        signed && !zero && (negative ? "text-red-600 dark:text-red-400" : "text-emerald-600 dark:text-emerald-400"),
        className
      )}
    >
      {signed && !negative && !zero ? "+" : ""}¥ {formatMoney(value)}
    </span>
  );
}

export function WalletLedgerView({ appKey, userId, onAdjust }: { appKey: string; userId: number; onAdjust?: () => void }) {
  const token = useAdminToken();
  const walletQuery = useAdminUserWalletQuery(appKey, userId);
  const wallet = walletQuery.data;
  const ready = hasWallet(wallet);

  const [type, setType] = React.useState("");
  const [page, setPage] = React.useState(1);
  const [limit, setLimit] = React.useState(20);
  const txnQuery = useAdminUserWalletTransactionsQuery(appKey, userId, { type: type || undefined, page, limit });
  const data = txnQuery.data;
  const [selected, setSelected] = React.useState<UserWalletTransaction | null>(null);
  const [shown, setShown] = React.useState<UserWalletTransaction | null>(null);
  const [exporting, setExporting] = React.useState(false);
  const txn = selected ?? shown;

  function open(row: UserWalletTransaction) {
    setSelected(row);
    setShown(row);
  }

  async function exportCsv() {
    if (!token) return;
    setExporting(true);
    try {
      const { rows, total, truncated } = await collectPages(async (p, l) => {
        const result = await getAdminUserWalletTransactions(token, appKey, userId, { type: type || undefined, page: p, limit: l });
        return { items: result.items ?? [], total: result.total };
      });
      downloadCsv(`余额流水-${userId}`, [
        { header: "时间", value: (r: UserWalletTransaction) => formatDateTime(r.createdAt) },
        { header: "流水号", value: (r) => r.transactionNo },
        { header: "类型", value: (r) => WALLET_TXN_LABEL[r.type] ?? r.type },
        { header: "标题", value: (r) => r.title },
        { header: "金额", value: (r) => r.amount },
        { header: "充值余额部分", value: (r) => r.cashAmount },
        { header: "赠送余额部分", value: (r) => r.bonusAmount },
        { header: "变动前", value: (r) => r.balanceBefore },
        { header: "变动后", value: (r) => r.balanceAfter },
        { header: "关联订单", value: (r) => r.relatedOrderNo },
        { header: "备注", value: (r) => r.remark },
        { header: "操作人", value: (r) => r.operator },
        { header: "IP", value: (r) => r.clientIp }
      ], rows);
      toast.success(truncated ? `已导出前 ${rows.length} 条（共 ${total} 条）` : `已导出 ${rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  }

  const columns: Array<Column<UserWalletTransaction>> = [
    {
      id: "time",
      header: "时间",
      width: "150px",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="text-xs text-muted-foreground">{formatRelative(row.createdAt)}</span>
        </div>
      )
    },
    {
      id: "title",
      header: "事项",
      cell: (row) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate font-medium">{row.title || WALLET_TXN_LABEL[row.type] || row.type}</span>
          <span className="truncate text-xs text-muted-foreground">
            {WALLET_TXN_LABEL[row.type] ?? row.type}
            {row.remark ? `，${row.remark}` : ""}
          </span>
        </div>
      )
    },
    {
      id: "amount",
      header: "金额",
      align: "right",
      width: "150px",
      cell: (row) => {
        const split = txnSplitLabel(row);
        return (
          <div className="flex flex-col items-end">
            <Money value={row.amount} signed />
            {split ? <span className="text-xs text-muted-foreground tabular-nums">{split}</span> : null}
          </div>
        );
      }
    },
    {
      id: "after",
      header: "变动后",
      align: "right",
      width: "140px",
      hideBelow: "sm",
      cell: (row) => (
        <div className="flex flex-col items-end">
          <Money value={row.balanceAfter} className="font-normal" />
          <span className="text-xs text-muted-foreground tabular-nums">原 ¥ {formatMoney(row.balanceBefore)}</span>
        </div>
      )
    },
    {
      id: "order",
      header: "关联订单",
      width: "170px",
      hideBelow: "lg",
      cell: (row) => <span className="font-mono text-xs text-muted-foreground">{row.relatedOrderNo || "—"}</span>
    },
    {
      id: "operator",
      header: "操作人",
      width: "110px",
      hideBelow: "xl",
      cell: (row) => <span className="text-xs text-muted-foreground">{row.operator || "—"}</span>
    }
  ];

  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        <MetricCard label="可用余额" value={ready ? `¥ ${formatMoney(wallet?.balance)}` : "—"} icon={Wallet} tone="primary" loading={walletQuery.isLoading} sub={ready ? (walletSplitLabel(wallet) ?? undefined) : "钱包数据不可用"} />
        <MetricCard
          label="冻结金额"
          value={ready ? `¥ ${formatMoney(wallet?.frozen)}` : "—"}
          icon={Lock}
          tone="warning"
          loading={walletQuery.isLoading}
          sub={ready && wallet?.frozenBonus != null ? `其中赠送 ¥ ${formatMoney(wallet.frozenBonus)}` : undefined}
        />
        <MetricCard
          label="累计充值"
          value={ready ? `¥ ${formatMoney(wallet?.totalRecharged)}` : "—"}
          icon={ArrowDownLeft}
          tone="success"
          loading={walletQuery.isLoading}
          sub={ready && wallet?.totalBonus != null ? `累计获赠 ¥ ${formatMoney(wallet.totalBonus)}` : undefined}
          onClick={() => setType("recharge")}
        />
        <MetricCard
          label="累计消费"
          value={ready ? `¥ ${formatMoney(wallet?.totalConsumed)}` : "—"}
          icon={ArrowUpRight}
          tone="danger"
          loading={walletQuery.isLoading}
          sub={ready && wallet?.totalExchanged != null ? `累计兑换积分 ¥ ${formatMoney(wallet.totalExchanged)}` : undefined}
          onClick={() => setType("consume")}
        />
      </div>

      <Section
        title="余额流水"
        icon={Receipt}
        flush
        actions={
          <>
            {onAdjust ? (
              <Button size="sm" variant="outline" onClick={onAdjust}>
                <SlidersHorizontal />
                调整余额
              </Button>
            ) : null}
            <Button size="sm" variant="outline" disabled={exporting || !data?.total} onClick={exportCsv}>
              {exporting ? <Loader2 className="animate-spin" /> : <Download />}
              导出 CSV
            </Button>
          </>
        }
        toolbar={
          <FilterBar>
            <FilterSelect
              label="类型"
              value={type}
              onChange={(value) => {
                setType(value);
                setPage(1);
              }}
              options={TYPE_OPTIONS}
            />
          </FilterBar>
        }
        footer={
          data ? (
            <Pager
              page={page}
              limit={limit}
              total={data.total}
              onPage={setPage}
              onLimit={(value) => {
                setLimit(value);
                setPage(1);
              }}
            />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={data?.items}
          rowKey={(row) => row.id}
          loading={txnQuery.isLoading}
          fetching={txnQuery.isFetching}
          error={txnQuery.error}
          onRetry={() => txnQuery.refetch()}
          onRowClick={open}
          empty={<StateBlock title={type ? "该类型下没有流水" : "还没有余额流水"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(value) => !value && setSelected(null)}
        title={txn?.title || "余额流水"}
        description={txn ? WALLET_TXN_LABEL[txn.type] ?? txn.type : undefined}
        hero={
          txn ? (
            <div className="flex items-end justify-between gap-4 rounded-xl border bg-muted/30 px-4 py-3.5">
              <div className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">金额</span>
                <Money value={txn.amount} signed className="text-2xl font-semibold" />
                {txnSplitLabel(txn) ? <span className="text-xs text-muted-foreground tabular-nums">{txnSplitLabel(txn)}</span> : null}
              </div>
              <span className="text-xs text-muted-foreground tabular-nums">
                ¥ {formatMoney(txn.balanceBefore)} → ¥ {formatMoney(txn.balanceAfter)}
              </span>
            </div>
          ) : null
        }
        fields={
          txn
            ? [
                { label: "流水号", value: txn.transactionNo, mono: true, copy: txn.transactionNo, wide: true },
                { label: "类型", value: WALLET_TXN_LABEL[txn.type] ?? txn.type, hint: txn.type },
                ...(txn.cashAmount != null && txn.bonusAmount != null
                  ? [
                      { label: "充值余额部分", value: `¥ ${formatMoney(txn.cashAmount)}` },
                      { label: "赠送余额部分", value: `¥ ${formatMoney(txn.bonusAmount)}` }
                    ]
                  : []),
                { label: "操作人", value: txn.operator },
                { label: "关联订单", value: txn.relatedOrderNo, mono: true, copy: txn.relatedOrderNo || null, wide: true },
                ...(txn.restoresTransactionNo
                  ? [
                      {
                        label: "退回的出账",
                        value: txn.restoresTransactionNo,
                        mono: true,
                        copy: txn.restoresTransactionNo,
                        wide: true
                      }
                    ]
                  : []),
                { label: "备注", value: txn.remark, wide: true },
                { label: "IP", value: txn.clientIp, mono: true },
                { label: "时间", value: formatDateTime(txn.createdAt), hint: formatRelative(txn.createdAt) }
              ]
            : []
        }
        raw={txn?.metadata && Object.keys(txn.metadata).length ? txn.metadata : undefined}
      />
    </div>
  );
}
