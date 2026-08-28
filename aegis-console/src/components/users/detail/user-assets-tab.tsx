"use client";

import { useMemo, useState } from "react";
import {
  BadgeCheck,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ApiError } from "@/lib/api-client";
import {
  useAdjustUserExperienceMutation,
  useAdjustUserIntegralMutation
} from "@/lib/admin-hooks";
import {
  useAdjustAdminUserWalletMutation,
  useAdminUserVipEntitlementQuery,
  useAdminUserVipTransactionsQuery,
  useAdminUserWalletQuery,
  useAdminUserWalletTransactionsQuery,
  useAdminVipFeaturesQuery,
  useAdminVipPlansQuery,
  useClaimAdminVipTrialMutation,
  useGrantAdminUserVipMutation,
  useResetAdminVipTrialMutation
} from "@/lib/app-user-hooks";
import type { AdminAppUserDetail, UserWallet, VipPlan } from "@/lib/api/types";
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
 *   会员          —— vip，套餐 / 天数发放，权益快照落账本
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

      <VipPanel appKey={appKey} userId={userId} />

      <Panel
        title="钱包流水"
        icon={<Receipt className="size-4" />}
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
          <EmptyRow text="暂无流水记录" />
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
      toast.error("请输入非零整数");
      return;
    }
    if (!appId) {
      toast.error("缺少应用标识");
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
      toast.success(`${kind === "integral" ? "积分" : "经验"}已调整（${amount > 0 ? "+" : ""}${amount}）`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
    }
  }

  return (
    <Panel
      title="积分与经验"
      icon={<Coins className="size-4" />}
      description="正数增加，负数扣减，调整计入流水。"
    >
      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">调整原因（选填）</Label>
          <Input
            value={reason}
            placeholder="将写入流水备注"
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
          aria-label={`${label}切换正负`}
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
      toast.error("金额格式无效", { description: "最多两位小数，负数为扣款" });
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
      description="调整即时入账并计入流水。"
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
                // 后端不为只读请求建行：没有钱包行时返回零值钱包，createdAt 是 Go 零值时间
                isZeroTime(wallet?.createdAt) || !wallet?.createdAt
                  ? "首次入账时自动开户"
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
              placeholder="调整原因（选填，写入流水备注）"
              onChange={(event) => setReason(event.target.value)}
            />
          </div>
        </div>
      )}
    </Panel>
  );
}

// ── 会员 ──────────────────────────

/** 会员来源标签。unknown 是老系统迁移的历史数据，账本里找不到对应流水。 */
const VIP_SOURCE_LABEL: Record<string, string> = {
  none: "—",
  unknown: "历史数据",
  trial: "试用",
  wallet: "余额购买",
  payment_order: "在线支付",
  admin_grant: "管理员发放"
};

/** 自定义发放的快捷时长档位。 */
const QUICK_DAYS = [7, 30, 90, 365];

/**
 * 会员运营面板：权益概览 + 两种发放方式 + 试用管理 + 发放记录。
 *
 * 概览读的是与用户端 /vip/status 同一个判定入口 —— 管理员看到的结论
 * 必须与用户手机上显示的一字不差，否则客服每通电话都要先争论"你是不是会员"。
 *
 * 发放分「按套餐」与「自定义」两条路：套餐是运营定好的商品（时长 × 数量、
 * 权益、赠送积分整体发放），自定义用于套餐覆盖不了的补偿场景，可单独附带权益。
 */
function VipPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const entitlementQuery = useAdminUserVipEntitlementQuery(appKey, userId);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);
  const txnsQuery = useAdminUserVipTransactionsQuery(appKey, userId, { limit: 20 });

  const entitlement = entitlementQuery.data;
  const featureName = useMemo(() => {
    const map = new Map<string, string>();
    for (const feature of featuresQuery.data ?? []) map.set(feature.tag, feature.name);
    return (tag: string) => map.get(tag) ?? tag;
  }, [featuresQuery.data]);

  const statusBadge = !entitlement ? null : entitlement.isVip ? (
    <Badge variant={entitlement.isTrial ? "warning" : "success"} size="sm">
      {entitlement.isTrial ? "试用中" : "有效"} · 剩 {Math.max(entitlement.remainingDays, 0)} 天
    </Badge>
  ) : entitlement.expireAt ? (
    <Badge variant="warning" size="sm">已过期</Badge>
  ) : (
    <Badge variant="outline" size="sm">非会员</Badge>
  );

  return (
    <Panel title="会员" icon={<Crown className="size-4" />} action={statusBadge} bodyClassName="p-0">
      {entitlementQuery.isLoading ? (
        <div className="space-y-2 p-5">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-9 w-full rounded-lg" />
          ))}
        </div>
      ) : entitlement ? (
        <div className="space-y-3 border-b px-5 py-4">
          <Facts columns={2}>
            <Fact
              label="状态"
              value={
                entitlement.isVip
                  ? entitlement.isTrial
                    ? "试用中"
                    : "有效"
                  : entitlement.expireAt
                    ? "已过期"
                    : "非会员"
              }
            />
            <Fact label="来源" value={VIP_SOURCE_LABEL[entitlement.source] ?? entitlement.source} />
            <Fact label="当前套餐" value={textValue(entitlement.planName)} />
            <Fact
              label="到期时间"
              value={formatTime(entitlement.expireAt)}
              hint={entitlement.isVip ? `剩余 ${Math.max(entitlement.remainingDays, 0)} 天` : undefined}
            />
          </Facts>
          {entitlement.features.length ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[11px] text-muted-foreground">生效权益</span>
              {entitlement.features.map((tag) => (
                <Badge key={tag} variant="secondary" size="sm" className="gap-1">
                  <BadgeCheck className="size-3" />
                  {featureName(tag)}
                </Badge>
              ))}
            </div>
          ) : null}
        </div>
      ) : (
        <div className="border-b px-5 py-4 text-sm text-muted-foreground">权益信息不可用</div>
      )}

      <GrantSection
        appKey={appKey}
        userId={userId}
        plans={plansQuery.data ?? []}
        featureName={featureName}
        featureTags={(featuresQuery.data ?? []).filter((f) => f.isActive).map((f) => f.tag)}
      />

      <TrialSection appKey={appKey} userId={userId} entitlement={entitlement} />

      {txnsQuery.isLoading ? (
        <div className="space-y-2 p-5">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-9 w-full rounded-lg" />
          ))}
        </div>
      ) : !txnsQuery.data?.items?.length ? (
        <EmptyRow text="暂无发放记录" />
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
              {txnsQuery.data.items.map((txn) => (
                <TableRow key={txn.id}>
                  <TableCell
                    className="whitespace-nowrap text-xs tabular-nums text-muted-foreground"
                    title={formatTime(txn.createdAt)}
                  >
                    {formatShortTime(txn.createdAt)}
                  </TableCell>
                  <TableCell className="max-w-[200px]">
                    <div className="truncate text-xs">{textValue(txn.planName)}</div>
                    {txn.features?.length ? (
                      <div className="truncate text-[11px] text-muted-foreground">
                        {txn.features.map(featureName).join(" · ")}
                      </div>
                    ) : null}
                  </TableCell>
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

/** 发放会员：按套餐 / 自定义 两种方式。 */
function GrantSection({
  appKey,
  userId,
  plans,
  featureName,
  featureTags
}: {
  appKey: string;
  userId: number;
  plans: VipPlan[];
  featureName: (tag: string) => string;
  featureTags: string[];
}) {
  const grant = useGrantAdminUserVipMutation(appKey, userId);

  // 只列在售付费套餐：试用有独立的资格体系，下架套餐不应继续发放
  const grantablePlans = useMemo(
    () => plans.filter((plan) => plan.isActive && plan.kind !== "trial"),
    [plans]
  );

  const [planId, setPlanId] = useState("");
  const [quantity, setQuantity] = useState(1);
  const [planReason, setPlanReason] = useState("");

  const [days, setDays] = useState("");
  const [bonus, setBonus] = useState("");
  const [customReason, setCustomReason] = useState("");
  const [selectedFeatures, setSelectedFeatures] = useState<string[]>([]);

  const plan = grantablePlans.find((item) => String(item.id) === planId);
  const dayCount = Number.parseInt(days, 10);
  const customValid = Number.isFinite(dayCount) && dayCount > 0;

  async function submit(payload: Parameters<typeof grant.mutateAsync>[0]) {
    try {
      const result = await grant.mutateAsync(payload);
      toast.success("会员已发放", { description: `到期时间 ${formatTime(result.expireAfter)}` });
      return true;
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "发放失败");
      return false;
    }
  }

  async function grantPlan() {
    if (!plan) return;
    const ok = await submit({
      planId: plan.id,
      quantity,
      reason: planReason.trim() || undefined
    });
    if (ok) {
      setQuantity(1);
      setPlanReason("");
    }
  }

  async function grantCustom() {
    if (!customValid) return;
    const ok = await submit({
      days: dayCount,
      bonusIntegral: Number.parseInt(bonus, 10) || undefined,
      features: selectedFeatures.length ? selectedFeatures : undefined,
      reason: customReason.trim() || undefined
    });
    if (ok) {
      setDays("");
      setBonus("");
      setSelectedFeatures([]);
      setCustomReason("");
    }
  }

  return (
    <div className="border-b px-5 py-4">
      <Tabs defaultValue="plan">
        <TabsList className="h-8">
          <TabsTrigger value="plan" className="text-xs">按套餐发放</TabsTrigger>
          <TabsTrigger value="custom" className="text-xs">自定义发放</TabsTrigger>
        </TabsList>

        <TabsContent value="plan" className="mt-3 space-y-3">
          {grantablePlans.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              当前应用暂无在售套餐，可在「配置 → 会员套餐」中创建，或使用自定义发放。
            </p>
          ) : (
            <>
              <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">套餐</Label>
                  <Select value={planId} onValueChange={setPlanId}>
                    <SelectTrigger size="sm" className="w-full text-xs">
                      <SelectValue placeholder="选择套餐" />
                    </SelectTrigger>
                    <SelectContent>
                      {grantablePlans.map((item) => (
                        <SelectItem key={item.id} value={String(item.id)} className="text-xs">
                          {item.name} · {item.durationDays} 天 · ¥{formatMoney(item.price)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">数量</Label>
                  <div className="flex items-center gap-1">
                    <Button
                      size="icon"
                      variant="outline"
                      className="size-8"
                      aria-label="减少数量"
                      disabled={quantity <= 1}
                      onClick={() => setQuantity((prev) => Math.max(1, prev - 1))}
                    >
                      <Minus className="size-3.5" />
                    </Button>
                    <Input
                      value={String(quantity)}
                      inputMode="numeric"
                      className="h-8 w-14 text-center text-xs tabular-nums"
                      onChange={(event) => {
                        const next = Number.parseInt(event.target.value, 10);
                        setQuantity(Number.isFinite(next) ? Math.min(100, Math.max(1, next)) : 1);
                      }}
                    />
                    <Button
                      size="icon"
                      variant="outline"
                      className="size-8"
                      aria-label="增加数量"
                      disabled={quantity >= 100}
                      onClick={() => setQuantity((prev) => Math.min(100, prev + 1))}
                    >
                      <Plus className="size-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
              <Input
                value={planReason}
                placeholder="发放原因（选填，计入记录）"
                className="h-8 text-xs"
                onChange={(event) => setPlanReason(event.target.value)}
              />
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-xs text-muted-foreground">
                  {plan
                    ? `合计 ${plan.durationDays * quantity} 天` +
                      (plan.bonusIntegral
                        ? ` · 附赠 ${(plan.bonusIntegral * quantity).toLocaleString("zh-CN")} 积分`
                        : "") +
                      (plan.features?.length ? ` · 含 ${plan.features.length} 项权益` : "")
                    : "选择套餐后显示发放内容"}
                </p>
                <Button size="sm" disabled={!plan || grant.isPending} onClick={grantPlan}>
                  {grant.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Crown className="size-3.5" />}
                  发放
                </Button>
              </div>
            </>
          )}
        </TabsContent>

        <TabsContent value="custom" className="mt-3 space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">时长（天）</Label>
              <div className="flex gap-2">
                <Input
                  value={days}
                  inputMode="numeric"
                  placeholder="30"
                  className="h-8 text-xs"
                  onChange={(event) => setDays(event.target.value)}
                />
              </div>
              <div className="flex flex-wrap gap-1">
                {QUICK_DAYS.map((preset) => (
                  <Button
                    key={preset}
                    size="sm"
                    variant="outline"
                    className="h-6 px-2 text-[11px] tabular-nums"
                    onClick={() => setDays(String(preset))}
                  >
                    {preset} 天
                  </Button>
                ))}
              </div>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">附赠积分（选填）</Label>
              <Input
                value={bonus}
                inputMode="numeric"
                placeholder="0"
                className="h-8 text-xs"
                onChange={(event) => setBonus(event.target.value)}
              />
            </div>
          </div>

          {featureTags.length ? (
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">附带权益（选填）</Label>
              <div className="flex flex-wrap gap-1.5">
                {featureTags.map((tag) => {
                  const selected = selectedFeatures.includes(tag);
                  return (
                    <button
                      key={tag}
                      type="button"
                      aria-pressed={selected}
                      className={cn(
                        "rounded-full border px-2.5 py-1 text-[11px] transition-colors",
                        selected
                          ? "border-foreground/50 bg-accent text-accent-foreground"
                          : "text-muted-foreground hover:bg-accent/40"
                      )}
                      onClick={() =>
                        setSelectedFeatures((prev) =>
                          selected ? prev.filter((item) => item !== tag) : [...prev, tag]
                        )
                      }
                    >
                      {featureName(tag)}
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}

          <Input
            value={customReason}
            placeholder="发放原因（选填，计入记录）"
            className="h-8 text-xs"
            onChange={(event) => setCustomReason(event.target.value)}
          />
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">
              {customValid
                ? `发放 ${dayCount} 天` +
                  (Number.parseInt(bonus, 10) > 0
                    ? ` · 附赠 ${Number.parseInt(bonus, 10).toLocaleString("zh-CN")} 积分`
                    : "") +
                  (selectedFeatures.length ? ` · 含 ${selectedFeatures.length} 项权益` : "")
                : "时长按当前到期时间顺延，到期时间只增不减"}
            </p>
            <Button size="sm" disabled={!customValid || grant.isPending} onClick={grantCustom}>
              {grant.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Crown className="size-3.5" />}
              发放
            </Button>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

/** 试用管理：代领 / 恢复资格。资格判据与文案由服务端给出。 */
function TrialSection({
  appKey,
  userId,
  entitlement
}: {
  appKey: string;
  userId: number;
  entitlement?: ReturnType<typeof useAdminUserVipEntitlementQuery>["data"];
}) {
  const claim = useClaimAdminVipTrialMutation(appKey, userId);
  const reset = useResetAdminVipTrialMutation(appKey, userId);

  if (!entitlement) return null;
  const offer = entitlement.trialOffer;
  // 应用未配置试用套餐时整段隐藏 —— 没有可执行的操作就不占位
  if (!offer || offer.reason === "not_configured") return null;

  const trial = entitlement.trial;

  async function handleClaim() {
    try {
      await claim.mutateAsync();
      toast.success("试用已开通");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  async function handleReset() {
    try {
      await reset.mutateAsync();
      toast.success("试用资格已恢复");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-3">
      <div className="min-w-0 text-xs text-muted-foreground">
        <span className="font-medium text-foreground">试用</span>
        <span className="px-1.5 text-muted-foreground/50">·</span>
        {trial
          ? `已于 ${formatShortTime(trial.claimedAt)} 领取${trial.planName ? `（${trial.planName}）` : ""}，${
              trial.active ? "试用中" : "已结束"
            }`
          : offer.message}
        {!trial && offer.planName ? (
          <span className="text-muted-foreground/70">
            {" "}
            — {offer.planName} · {offer.durationDays} 天
          </span>
        ) : null}
      </div>
      <div className="flex items-center gap-1.5">
        {offer.available ? (
          <Button size="sm" variant="outline" className="h-7 text-xs" disabled={claim.isPending} onClick={handleClaim}>
            {claim.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            代领试用
          </Button>
        ) : null}
        {trial ? (
          <Button size="sm" variant="ghost" className="h-7 text-xs" disabled={reset.isPending} onClick={handleReset}>
            {reset.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            恢复试用资格
          </Button>
        ) : null}
      </div>
    </div>
  );
}
