"use client";

import { useMemo, useState } from "react";
import { ArrowDownLeft, ArrowUpRight, Loader2, SlidersHorizontal, Wallet } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAdjustWalletMutation } from "@/lib/commerce-hooks";
import { useAdminUserWalletQuery } from "@/lib/app-user-hooks";
import { formatMoney } from "./commerce-format";
import { UserPicker, displayName, type PickedUser } from "./user-picker";

/** 常用面额。覆盖了绝大多数客服补偿场景，剩下的才需要手打。 */
const QUICK_AMOUNTS = ["5", "10", "20", "50", "100", "200"];

/**
 * 常用调账理由。
 *
 * 不是限定选项 —— 选中之后仍然可以接着改。它解决的是「每次都要把同一句话
 * 重新打一遍」，而不是限制管理员能写什么。
 */
const QUICK_REASONS = [
  "客服补偿",
  "活动奖励发放",
  "误充值回收",
  "线下支付补录",
  "退款差额补偿",
  "测试数据清理"
];

type Direction = "credit" | "debit";

/**
 * 管理员调账。
 *
 * 三处刻意不让人手打：
 *   - **用户**：搜索选择而不是输 ID —— 那个数字没人记得住；
 *   - **方向**：充入 / 扣减用开关表达，而不是靠「负数表示扣减」这条只写在提示语里的约定。
 *     金额输入框恒为正数，符号由服务端拼 —— 少打一个减号就变成反向调账，
 *     而这类操作恰恰是不可撤销的；
 *   - **理由**：常用理由一键填入，仍可继续编辑。
 *
 * 调账是唯一一种「没有对应交易」的资金变动，因此理由必填：
 * 事后追责时，一条没有理由的调账流水与一次盗刷无法区分。
 */
export function WalletAdjustDialog({
  appKey,
  open,
  onOpenChange,
  walletCurrency
}: {
  appKey?: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  walletCurrency?: string;
}) {
  const mutation = useAdjustWalletMutation(appKey);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [direction, setDirection] = useState<Direction>("credit");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");

  // 选中用户后立刻显示当前余额：扣减时「够不够扣」不该等到提交被拒才知道
  const walletQuery = useAdminUserWalletQuery(user ? appKey : null, user?.id);
  const balance = walletQuery.data?.balance;

  const parsedAmount = Number(amount.trim());
  const amountValid = Number.isFinite(parsedAmount) && parsedAmount > 0;
  const reasonValid = reason.trim().length > 0;
  const overdraft = useMemo(() => {
    if (direction !== "debit" || !amountValid || balance == null) return false;
    return parsedAmount > Number(balance);
  }, [direction, amountValid, parsedAmount, balance]);
  const valid = Boolean(user) && amountValid && reasonValid && !overdraft;

  const afterBalance = useMemo(() => {
    if (balance == null || !amountValid) return null;
    const delta = direction === "credit" ? parsedAmount : -parsedAmount;
    return (Number(balance) + delta).toFixed(2);
  }, [balance, amountValid, parsedAmount, direction]);

  async function handleSubmit() {
    if (!appKey || !user || !valid) return;
    try {
      await mutation.mutateAsync({
        userId: user.id,
        // 符号在这里统一拼上，输入框永远只收正数
        amount: direction === "credit" ? amount.trim() : `-${amount.trim()}`,
        reason: reason.trim()
      });
      toast.success(direction === "credit" ? "已充入余额" : "已扣减余额", {
        description: `${displayName(user)} · ${formatMoney(amount.trim(), walletCurrency)}`
      });
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "调整失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <SlidersHorizontal className="size-4" />
            调整用户余额
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs">用户</Label>
            <UserPicker appKey={appKey} value={user} onChange={setUser} />
            {user ? (
              <div className="flex items-center gap-2 rounded-lg bg-muted px-3 py-2 text-xs">
                <Wallet className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="text-muted-foreground">当前余额</span>
                <span className="font-mono tabular-nums">
                  {walletQuery.isLoading ? "读取中..." : formatMoney(balance ?? "0", walletCurrency)}
                </span>
                {afterBalance != null ? (
                  <>
                    <span className="text-muted-foreground">→</span>
                    <span className={`font-mono tabular-nums ${overdraft ? "text-destructive" : "text-foreground"}`}>
                      {formatMoney(afterBalance, walletCurrency)}
                    </span>
                  </>
                ) : null}
              </div>
            ) : null}
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">调整方向</Label>
            <ToggleGroup
              type="single"
              value={direction}
              onValueChange={(value) => value && setDirection(value as Direction)}
              className="grid grid-cols-2 gap-2"
            >
              <ToggleGroupItem
                value="credit"
                className="h-9 gap-1.5 border text-xs data-[state=on]:border-emerald-500/40 data-[state=on]:bg-emerald-500/10 data-[state=on]:text-emerald-600"
              >
                <ArrowUpRight className="size-3.5" />
                充入余额
              </ToggleGroupItem>
              <ToggleGroupItem
                value="debit"
                className="h-9 gap-1.5 border text-xs data-[state=on]:border-amber-500/40 data-[state=on]:bg-amber-500/10 data-[state=on]:text-amber-600"
              >
                <ArrowDownLeft className="size-3.5" />
                扣减余额
              </ToggleGroupItem>
            </ToggleGroup>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="wallet-adjust-amount" className="text-xs">
              金额
            </Label>
            <div className="flex flex-wrap gap-1.5">
              {QUICK_AMOUNTS.map((value) => (
                <Button
                  key={value}
                  size="sm"
                  variant={amount === value ? "default" : "outline"}
                  className="h-7 px-2.5 font-mono text-xs tabular-nums"
                  onClick={() => setAmount(value)}
                >
                  {direction === "credit" ? "+" : "−"}
                  {value}
                </Button>
              ))}
            </div>
            <Input
              id="wallet-adjust-amount"
              inputMode="decimal"
              className="h-8 font-mono text-xs tabular-nums"
              placeholder="或直接输入金额"
              value={amount}
              onChange={(event) => setAmount(event.target.value.replace(/[^\d.]/g, ""))}
            />
            {overdraft ? (
              <p className="text-[11px] text-destructive">
                扣减额超过当前余额。后端会直接拒绝 —— 余额不会被扣成负数。
              </p>
            ) : (
              <p className="text-[11px] text-muted-foreground">
                只填正数，充入还是扣减由上面的开关决定。
              </p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="wallet-adjust-reason" className="text-xs">
              调整理由
            </Label>
            <div className="flex flex-wrap gap-1.5">
              {QUICK_REASONS.map((preset) => (
                <Badge
                  key={preset}
                  variant={reason === preset ? "default" : "outline"}
                  className="cursor-pointer text-[11px] font-normal"
                  onClick={() => setReason(preset)}
                >
                  {preset}
                </Badge>
              ))}
            </div>
            <Textarea
              id="wallet-adjust-reason"
              className="min-h-14 text-xs"
              placeholder="选一个常用理由，或补充具体说明（如工单号）"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
            <p className="text-[11px] text-muted-foreground">
              理由会随经办人一起写进流水，并出现在这笔调账的凭证上。
            </p>
          </div>

          <div className="flex justify-end gap-2">
            <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button size="sm" className="h-8 text-xs" disabled={!valid || mutation.isPending} onClick={() => void handleSubmit()}>
              {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              {mutation.isPending
                ? "提交中..."
                : direction === "credit"
                  ? `充入 ${amountValid ? formatMoney(amount, walletCurrency) : ""}`.trim()
                  : `扣减 ${amountValid ? formatMoney(amount, walletCurrency) : ""}`.trim()}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
