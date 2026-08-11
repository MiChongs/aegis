"use client";

import { useState } from "react";
import {
  Coins,
  Crown,
  Loader2,
  Minus,
  Plus,
  Receipt,
  Sparkles,
  Wallet
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api-client";
import {
  useAdjustUserExperienceMutation,
  useAdjustUserIntegralMutation
} from "@/lib/admin-hooks";
import {
  useAdjustAdminUserWalletMutation,
  useAdminUserVipTransactionsQuery,
  useAdminUserWalletQuery,
  useAdminUserWalletTransactionsQuery,
  useGrantAdminUserVipMutation
} from "@/lib/app-user-hooks";
import type { AdminAppUserDetail, UserWallet } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  EMPTY,
  EmptyRow,
  Fact,
  Facts,
  Panel,
  StatTile,
  WALLET_TXN_LABEL,
  formatMoney,
  formatShortTime,
  formatTime,
  hasWallet,
  isNegativeAmount,
  isPast,
  isZeroTime,
  numberText,
  relativeTime,
  textValue
} from "./user-detail-shared";

/** 金额输入：最多两位小数，允许前导负号。 */
const AMOUNT_RE = /^-?\d+(\.\d{1,2})?$/;

/**
 * 资产：这个用户在平台上拥有的一切可计价的东西，以及改动它们的入口。
 *
 * 四种资产分属四条完全不同的账：
 *   积分 / 经验  —— points_service，整数，无小数
 *   钱包余额     —— wallet，decimal 字符串，带流水与幂等
 *   会员时长     —— vip，按天累加，记 expireBefore/expireAfter
 * 放在一页是因为管理员的问题是"这人有什么"，而不是"平台有几张表"；
 * 但调整入口各自独立，绝不做成一个统一的"调整资产"表单 —— 那会掩盖它们不同的结算语义。
 */
export function UserAssetsTab({
  appKey,
  userId,
  user
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
}) {
  const appId = user.appid;

  const walletQuery = useAdminUserWalletQuery(appKey, userId);
  const txnQuery = useAdminUserWalletTransactionsQuery(appKey, userId, { limit: 20 });
  const vipQuery = useAdminUserVipTransactionsQuery(appKey, userId, { limit: 20 });

  const wallet = walletQuery.data as UserWallet | undefined;
  const walletReady = hasWallet(wallet);
  const vipActive = Boolean(user.vipExpireAt) && !isPast(user.vipExpireAt);

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatTile label="积分" value={numberText(user.integral)} icon={<Coins className="size-3.5" />} />
        <StatTile
          label="经验"
          value={numberText(user.experience)}
          icon={<Sparkles className="size-3.5" />}
        />
        <StatTile
          label="可用余额"
          value={walletReady ? `¥ ${formatMoney(wallet?.balance)}` : EMPTY}
          icon={<Wallet className="size-3.5" />}
          hint={walletReady ? `冻结 ¥ ${formatMoney(wallet?.frozen)}` : "钱包数据不可用"}
        />
        <StatTile
          label="会员到期"
          value={vipActive ? relativeTime(user.vipExpireAt) : user.vipExpireAt ? "已过期" : "非会员"}
          icon={<Crown className="size-3.5" />}
          tone={vipActive ? "success" : user.vipExpireAt ? "warning" : "default"}
          hint={user.vipExpireAt ? formatTime(user.vipExpireAt) : undefined}
        />
      </div>

      <div className="grid gap-5 xl:grid-cols-2">
        <PointsPanel appId={appId} userId={userId} />
        <WalletPanel
          appKey={appKey}
          userId={userId}
          wallet={wallet}
          loading={walletQuery.isLoading}
        />
      </div>

      <Panel
        title="钱包流水"
        icon={<Receipt className="size-4" />}
        description="金额为负表示出账。余额由流水推导，没有「只改余额不记账」的路径。"
        action={
          <Badge variant="outline" size="sm">
            共 {txnQuery.data?.total ?? 0} 条
          </Badge>
        }
        bodyClassName="p-0"
      >
        {txnQuery.isLoading ? (
          <div className="space-y-2 p-5">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-9 w-full rounded-lg" />
            ))}
          </div>
        ) : !txnQuery.data?.items?.length ? (
          <EmptyRow text="没有钱包流水" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>时间</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>说明</TableHead>
                  <TableHead className="text-right">金额</TableHead>
                  <TableHead className="text-right">余额</TableHead>
                  <TableHead>操作人</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {txnQuery.data.items.map((txn) => {
                  const negative = isNegativeAmount(txn.amount);
                  return (
                    <TableRow key={txn.id}>
                      <TableCell
                        className="whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                        title={formatTime(txn.createdAt)}
                      >
                        {formatShortTime(txn.createdAt)}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" size="sm">
                          {WALLET_TXN_LABEL[txn.type] ?? txn.type}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-[240px]">
                        <div className="truncate text-xs" title={txn.title}>
                          {textValue(txn.title)}
                        </div>
                        {txn.remark || txn.relatedOrderNo ? (
                          <div className="truncate text-[11px] text-muted-foreground">
                            {[txn.remark, txn.relatedOrderNo].filter(Boolean).join(" · ")}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell
                        className={cn(
                          "text-right text-xs font-medium tabular-nums",
                          negative
                            ? "text-red-600 dark:text-red-400"
                            : "text-emerald-600 dark:text-emerald-400"
                        )}
                      >
                        {negative ? "" : "+"}
                        {formatMoney(txn.amount)}
                      </TableCell>
                      <TableCell className="text-right text-xs tabular-nums text-muted-foreground">
                        {formatMoney(txn.balanceAfter)}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {textValue(txn.operator)}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>

      <VipPanel appKey={appKey} userId={userId} vipExpireAt={user.vipExpireAt} query={vipQuery} />
    </div>
  );
}

// ── 积分与经验 ──────────────────────────

function PointsPanel({ appId, userId }: { appId?: number; userId: number }) {
  const integralMutation = useAdjustUserIntegralMutation();
  const experienceMutation = useAdjustUserExperienceMutation();
  const [integral, setIntegral] = useState("");
  const [experience, setExperience] = useState("");
  const [reason, setReason] = useState("");

  async function adjust(kind: "integral" | "experience") {
    const raw = kind === "integral" ? integral : experience;
    const amount = Number.parseInt(raw, 10);
    if (!Number.isFinite(amount) || amount === 0) {
      toast.error("请填写非零整数，负数表示扣减");
      return;
    }
    if (!appId) {
      toast.error("缺少应用标识，无法调整");
      return;
    }
    const mutation = kind === "integral" ? integralMutation : experienceMutation;
    try {
      await mutation.mutateAsync({
        userId,
        appid: appId,
        amount,
        reason: reason.trim() || undefined
      });
      if (kind === "integral") setIntegral("");
      else setExperience("");
      toast.success(`${kind === "integral" ? "积分" : "经验"}${amount > 0 ? "+" : ""}${amount}`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
    }
  }

  return (
    <Panel
      title="积分与经验"
      icon={<Coins className="size-4" />}
      description="整数，正数增加、负数扣减。调整会留一条积分流水。"
    >
      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">调整原因（两项共用，可选）</Label>
          <Input
            value={reason}
            placeholder="例如：客服补偿 / 活动发放"
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
        <DeltaRow
          label="积分"
          value={integral}
          pending={integralMutation.isPending}
          onChange={setIntegral}
          onSubmit={() => void adjust("integral")}
        />
        <DeltaRow
          label="经验"
          value={experience}
          pending={experienceMutation.isPending}
          onChange={setExperience}
          onSubmit={() => void adjust("experience")}
        />
      </div>
    </Panel>
  );
}

function DeltaRow({
  label,
  value,
  pending,
  onChange,
  onSubmit
}: {
  label: string;
  value: string;
  pending: boolean;
  onChange: (value: string) => void;
  onSubmit: () => void;
}) {
  const amount = Number.parseInt(value, 10);
  const valid = Number.isFinite(amount) && amount !== 0;
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <div className="flex gap-2">
        <Button
          size="icon"
          variant="outline"
          className="size-9 shrink-0"
          aria-label={`${label}减少`}
          onClick={() => onChange(value.startsWith("-") ? value.slice(1) : `-${value || "0"}`)}
        >
          {value.startsWith("-") ? <Plus className="size-3.5" /> : <Minus className="size-3.5" />}
        </Button>
        <Input
          value={value}
          inputMode="numeric"
          placeholder="100 或 -50"
          onChange={(event) => onChange(event.target.value)}
        />
        <Button size="sm" variant="outline" disabled={!valid || pending} onClick={onSubmit}>
          {pending ? <Loader2 className="size-3.5 animate-spin" /> : "应用"}
        </Button>
      </div>
    </div>
  );
}

// ── 钱包 ──────────────────────────

function WalletPanel({
  appKey,
  userId,
  wallet,
  loading
}: {
  appKey: string;
  userId: number;
  wallet?: UserWallet;
  loading: boolean;
}) {
  const mutation = useAdjustAdminUserWalletMutation(appKey, userId);
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const valid = AMOUNT_RE.test(amount.trim()) && Number.parseFloat(amount) !== 0;

  async function handleAdjust() {
    if (!valid) {
      toast.error("金额格式无效", { description: "最多两位小数，负数表示扣款，例如 -12.50" });
      return;
    }
    try {
      const result = await mutation.mutateAsync({
        amount: amount.trim(),
        reason: reason.trim() || undefined
      });
      setAmount("");
      toast.success("余额已调整", {
        description: `当前余额 ¥ ${formatMoney(result.wallet?.balance)}`
      });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
    }
  }

  return (
    <Panel
      title="钱包"
      icon={<Wallet className="size-4" />}
      description="金额为 decimal，前端只做展示格式化。调整即入账，同时生成一条 admin_adjust 流水。"
    >
      {loading ? (
        <Skeleton className="h-32 w-full rounded-xl" />
      ) : (
        <div className="space-y-4">
          <Facts>
            <Fact label="可用余额" value={`¥ ${formatMoney(wallet?.balance)}`} />
            <Fact label="冻结" value={`¥ ${formatMoney(wallet?.frozen)}`} tone="muted" />
            <Fact label="累计充值" value={`¥ ${formatMoney(wallet?.totalRecharged)}`} tone="muted" />
            <Fact label="累计消费" value={`¥ ${formatMoney(wallet?.totalConsumed)}`} tone="muted" />
            <Fact
              label="开户时间"
              value={formatTime(wallet?.createdAt)}
              hint={
                // 后端不为只读请求建行：没有钱包行时返回的是零值钱包，
                // createdAt 因此是 Go 零值时间。这是唯一能区分"没开户"的旁证。
                isZeroTime(wallet?.createdAt) || !wallet?.createdAt
                  ? "尚未产生任何入账，首次充值 / 调整时自动开户"
                  : undefined
              }
            />
          </Facts>
          <div className="space-y-2 border-t pt-3">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">调整金额</Label>
              <div className="flex gap-2">
                <Input
                  value={amount}
                  inputMode="decimal"
                  placeholder="10.00 或 -12.50"
                  onChange={(event) => setAmount(event.target.value)}
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!valid || mutation.isPending}
                  onClick={handleAdjust}
                >
                  {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : "调整"}
                </Button>
              </div>
            </div>
            <Input
              value={reason}
              placeholder="调整原因（可选，会写进流水备注）"
              onChange={(event) => setReason(event.target.value)}
            />
          </div>
        </div>
      )}
    </Panel>
  );
}

// ── 会员 ──────────────────────────

function VipPanel({
  appKey,
  userId,
  vipExpireAt,
  query
}: {
  appKey: string;
  userId: number;
  vipExpireAt?: string | null;
  query: ReturnType<typeof useAdminUserVipTransactionsQuery>;
}) {
  const grant = useGrantAdminUserVipMutation(appKey, userId);
  const [days, setDays] = useState("");
  const [bonus, setBonus] = useState("");
  const [reason, setReason] = useState("");

  const dayCount = Number.parseInt(days, 10);
  const valid = Number.isFinite(dayCount) && dayCount !== 0;

  async function handleGrant() {
    if (!valid) {
      toast.error("请填写非零天数，负数表示扣减时长");
      return;
    }
    try {
      const result = await grant.mutateAsync({
        days: dayCount,
        reason: reason.trim() || undefined,
        bonusIntegral: Number.parseInt(bonus, 10) || undefined
      });
      setDays("");
      setBonus("");
      toast.success("会员时长已调整", {
        description: `到期时间 ${formatTime(result.expireAfter)}`
      });
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "赠送失败");
    }
  }

  return (
    <Panel
      title="会员"
      icon={<Crown className="size-4" />}
      description={
        vipExpireAt
          ? `当前到期时间 ${formatTime(vipExpireAt)}。赠送按天累加，从当前到期时间起算。`
          : "该用户尚未开通会员。赠送后从此刻起算。"
      }
      bodyClassName="p-0"
    >
      <div className="grid gap-3 border-b px-5 py-4 sm:grid-cols-[repeat(3,minmax(0,1fr))_auto] sm:items-end">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">天数</Label>
          <Input
            value={days}
            inputMode="numeric"
            placeholder="30"
            onChange={(event) => setDays(event.target.value)}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">附赠积分（可选）</Label>
          <Input
            value={bonus}
            inputMode="numeric"
            placeholder="0"
            onChange={(event) => setBonus(event.target.value)}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">原因（可选）</Label>
          <Input
            value={reason}
            placeholder="例如：活动赠送"
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
        <Button size="sm" disabled={!valid || grant.isPending} onClick={handleGrant}>
          {grant.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Crown className="size-3.5" />}
          赠送
        </Button>
      </div>

      {query.isLoading ? (
        <div className="space-y-2 p-5">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-9 w-full rounded-lg" />
          ))}
        </div>
      ) : !query.data?.items?.length ? (
        <EmptyRow text="没有会员记录" />
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>时间</TableHead>
                <TableHead>套餐</TableHead>
                <TableHead className="text-right">时长</TableHead>
                <TableHead>渠道</TableHead>
                <TableHead className="text-right">金额</TableHead>
                <TableHead>到期变化</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.items.map((txn) => (
                <TableRow key={txn.id}>
                  <TableCell
                    className="whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                    title={formatTime(txn.createdAt)}
                  >
                    {formatShortTime(txn.createdAt)}
                  </TableCell>
                  <TableCell className="text-xs">{textValue(txn.planName)}</TableCell>
                  <TableCell className="text-right text-xs tabular-nums">
                    {numberText(txn.durationDays)} 天
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline" size="sm">
                      {textValue(txn.payChannel, "—")}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">
                    {formatMoney(txn.payAmount)}
                  </TableCell>
                  <TableCell className="text-[11px] text-muted-foreground">
                    {txn.expireBefore ? `${formatShortTime(txn.expireBefore)} → ` : "开通 → "}
                    {formatShortTime(txn.expireAfter)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Panel>
  );
}
