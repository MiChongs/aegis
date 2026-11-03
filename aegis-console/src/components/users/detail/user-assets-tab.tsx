"use client";

import { useState, type ComponentType, type ReactNode } from "react";
import { Coins, Crown, Infinity as InfinityIcon, Receipt, SlidersHorizontal, Sparkles, Wallet } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useNow } from "@/components/users/ban-form";
import { ApiError } from "@/lib/api-client";
import { useAdjustUserExperienceMutation, useAdjustUserIntegralMutation } from "@/lib/admin-hooks";
import {
  useAdjustAdminUserWalletMutation,
  useAdminUserVipEntitlementQuery,
  useAdminUserWalletQuery
} from "@/lib/app-user-hooks";
import type { ExperienceLevelMode } from "@/lib/api/apps";
import type { AdminAppUserDetail, UserWallet, VipEntitlement } from "@/lib/api/types";
import { useUserPointSummaryQuery } from "@/lib/user-ledger-hooks";
import { InfoGrid, MetricCard, Section, StateBlock, type Tone } from "./kit";
import { AdjustForm, Segmented, formatIntegerString, type AdjustFormProps, type SegmentOption } from "./user-assets-adjust";
import { EMPTY, formatMoney, formatTime, hasWallet, isNegativeAmount, isZeroTime } from "./user-detail-shared";
import { ExperienceLevelChoice, ExperienceLevelConfirm } from "./user-experience-level";
import { UserFeatureUnlocksPanel } from "./user-feature-unlocks-panel";
import { UserVipPanel } from "./user-vip-panel";

const POINT_QUICK = ["10", "50", "100", "500", "1000"];
const MONEY_QUICK = ["1", "5", "10", "50", "100"];
const POINT_REASONS = ["活动奖励", "客诉补偿", "签到补发", "错误更正", "违规扣除"];
const EXPERIENCE_REASONS = ["活动奖励", "客诉补偿", "签到补发", "错误更正", "违规扣除"];
/** 与后端 pointdomain.MaxAdminAdjustAmount / MaxBalance 一致 */
const POINT_MAX_ADJUST = "100000000";
const POINT_MAX_BALANCE = "9007199254740991";
const MONEY_REASONS = ["客诉补偿", "线下充值", "退款补录", "错误更正"];
/** 与后端 wallet maxAdjustAmount 一致 */
const WALLET_MAX_ADJUST = "100000000";
const DAY = 86_400_000;

type AssetKind = "integral" | "experience" | "wallet";
/** 「流水」页签的子类目 */
type LedgerSub = AssetKind | "vip";

const KIND_OPTIONS: Array<SegmentOption<AssetKind>> = [
  { value: "integral", label: "积分", icon: Coins },
  { value: "experience", label: "经验", icon: Sparkles },
  { value: "wallet", label: "钱包余额", icon: Wallet }
];

const numberString = (value: unknown) => (typeof value === "number" && Number.isFinite(value) ? String(value) : undefined);
const formatCount = (value: string) => formatIntegerString(value);

/**
 * 资产：这个用户在平台上拥有的一切可计价的东西，以及改动它们的入口。流水在独立的「流水」页签。
 *
 * 四种资产分属四条完全不同的账：
 *   积分 / 经验  —— points_service，整数，无小数，带请求编号幂等
 *   钱包余额     —— wallet，decimal 字符串
 *   会员          —— vip，套餐 / 天数发放，功能权益跟随套餐当前配置
 *   功能解锁      —— 积分兑换或管理员授予的单项功能，不让人成为会员
 * 放在一页是因为管理员的问题是"这人有什么"，而不是"平台有几张表"；
 * 调整表单共用一套交互，但请求各走各的接口 —— 统一请求会掩盖它们不同的结算语义。
 *
 * 自上而下：四张指标卡 → 资产调整（分段切换积分 / 经验 / 余额）→ 会员与会员记录 → 功能解锁。
 */
export function UserAssetsTab({
  appKey,
  userId,
  user,
  onNavigate
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  /** 切换到其它页签；给出时各区块出现「查看流水」 */
  onNavigate?: (tab: string, sub?: string) => void;
}) {
  const appId = user.appid;
  const walletQuery = useAdminUserWalletQuery(appKey, userId);
  const wallet = walletQuery.data as UserWallet | undefined;
  const walletReady = hasWallet(wallet);
  const entitlementQuery = useAdminUserVipEntitlementQuery(appKey, userId);
  const integralSummary = useUserPointSummaryQuery(appKey, userId, "integral", 30);
  const experienceSummary = useUserPointSummaryQuery(appKey, userId, "experience", 30);

  const integralMutation = useAdjustUserIntegralMutation();
  const experienceMutation = useAdjustUserExperienceMutation();
  const walletMutation = useAdjustAdminUserWalletMutation(appKey, userId);

  const [kind, setKind] = useState<AssetKind>("integral");
  /** 扣减经验时的等级处理，默认保留现有等级 */
  const [levelMode, setLevelMode] = useState<ExperienceLevelMode>("keep");

  const viewLedger = onNavigate ? (sub: LedgerSub) => () => onNavigate("ledger", sub) : undefined;

  async function adjustPoints(target: "integral" | "experience", signed: string, reason: string, requestId: string) {
    if (!appId) {
      toast.error("缺少应用标识");
      return false;
    }
    // 表单已把数量限制在单次上限内，远小于 2^53，转 Number 不丢精度
    const amount = Number.parseInt(signed, 10);
    if (!Number.isSafeInteger(amount) || amount === 0) {
      toast.error("调整数量无效");
      return false;
    }
    const label = target === "integral" ? "积分" : "经验";
    const mutation = target === "integral" ? integralMutation : experienceMutation;
    try {
      const result = await mutation.mutateAsync({
        userId,
        appid: appId,
        amount,
        reason: reason || undefined,
        requestId,
        levelMode: target === "experience" && amount < 0 ? levelMode : undefined
      });
      const after = typeof result?.afterAmount === "number" ? `，当前 ${formatIntegerString(String(result.afterAmount))}` : "";
      const level =
        result?.levelChanged && typeof result.newLevel === "number"
          ? `等级 ${result.oldLevel ?? "—"} → ${result.newLevel}`
          : undefined;
      if (result?.replayed) {
        toast.info(`该笔调整已处理过，未重复${amount > 0 ? "增加" : "扣减"}${after}`);
      } else {
        toast.success(`${label}已${amount > 0 ? "增加" : "扣减"} ${formatIntegerString(String(Math.abs(amount)))}${after}`, {
          description: [level, result?.transactionNo ? `流水号 ${result.transactionNo}` : ""].filter(Boolean).join("，") || undefined
        });
      }
      return true;
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
      return false;
    }
  }

  async function adjustWallet(signed: string, reason: string) {
    try {
      const result = await walletMutation.mutateAsync({ amount: signed, reason: reason || undefined });
      toast.success(isNegativeAmount(signed) ? "余额已扣减" : "余额已增加", {
        description: `当前余额 ¥${formatMoney(result.wallet?.balance)}`
      });
      return true;
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
      return false;
    }
  }

  const walletUnavailable =
    !walletQuery.isLoading && !walletReady ? (
      <span className="inline-flex flex-wrap items-center gap-1.5">
        {walletQuery.isError ? "钱包数据加载失败" : "钱包数据不可用"}
        <button
          type="button"
          className="text-foreground underline-offset-2 hover:underline"
          onClick={() => void walletQuery.refetch()}
        >
          重试
        </button>
      </span>
    ) : undefined;

  const forms: Record<AssetKind, AdjustFormProps> = {
    integral: {
      title: "积分",
      unit: "积分",
      current: numberString(user.integral),
      format: formatCount,
      hint: "扣减不能超过当前积分",
      quick: POINT_QUICK,
      reasons: POINT_REASONS,
      shortageText: "积分不足",
      maxAmount: POINT_MAX_ADJUST,
      maxBalance: POINT_MAX_BALANCE,
      pending: integralMutation.isPending,
      onSubmit: (signed, reason, requestId) => adjustPoints("integral", signed, reason, requestId)
    },
    experience: {
      title: "经验",
      unit: "经验",
      current: numberString(user.experience),
      format: formatCount,
      hint: "扣减不能超过当前经验；扣减时可选择是否降级",
      quick: POINT_QUICK,
      reasons: EXPERIENCE_REASONS,
      shortageText: "经验不足",
      deductExtra: (amount) => {
        const experience = typeof user.experience === "number" ? user.experience : 0;
        return {
          form: (
            <ExperienceLevelChoice
              appKey={appKey}
              userId={userId}
              experience={experience}
              amount={amount}
              mode={levelMode}
              onModeChange={setLevelMode}
            />
          ),
          confirm: <ExperienceLevelConfirm appKey={appKey} userId={userId} experience={experience} amount={amount} mode={levelMode} />
        };
      },
      maxAmount: POINT_MAX_ADJUST,
      maxBalance: POINT_MAX_BALANCE,
      pending: experienceMutation.isPending,
      onSubmit: (signed, reason, requestId) => adjustPoints("experience", signed, reason, requestId)
    },
    wallet: {
      title: "余额",
      unit: "元",
      decimal: true,
      loading: walletQuery.isLoading,
      current: walletReady ? wallet?.balance : undefined,
      format: (value) => `¥${formatMoney(value)}`,
      hint: walletReady ? `冻结 ¥${formatMoney(wallet?.frozen)}` : undefined,
      unavailable: walletUnavailable,
      quick: MONEY_QUICK,
      reasons: MONEY_REASONS,
      shortageText: "余额不足",
      maxAmount: WALLET_MAX_ADJUST,
      pending: walletMutation.isPending,
      onSubmit: (signed, reason) => adjustWallet(signed, reason)
    }
  };

  const summaryFor = (target: AssetKind) =>
    target === "integral" ? integralSummary.data : target === "experience" ? experienceSummary.data : undefined;

  return (
    <div className="flex flex-col gap-5">
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <PointMetric
          label="积分"
          icon={Coins}
          tone="warning"
          value={user.integral}
          summary={integralSummary.data}
          onClick={viewLedger?.("integral")}
        />
        <PointMetric
          label="经验"
          icon={Sparkles}
          tone="info"
          value={user.experience}
          summary={experienceSummary.data}
          onClick={viewLedger?.("experience")}
        />
        <MetricCard
          label="钱包余额"
          icon={Wallet}
          tone="success"
          loading={walletQuery.isLoading}
          value={walletReady ? `¥${formatMoney(wallet?.balance)}` : EMPTY}
          sub={walletReady ? `冻结 ¥${formatMoney(wallet?.frozen)}` : walletQuery.isError ? "加载失败" : "钱包数据不可用"}
          onClick={viewLedger?.("wallet")}
        />
        <VipMetric
          loading={entitlementQuery.isLoading}
          error={entitlementQuery.isError}
          entitlement={entitlementQuery.data}
          onClick={viewLedger?.("vip")}
        />
      </div>

      <Section
        title="资产调整"
        description="调整立即生效并写入流水；积分与经验同一笔重试只生效一次"
        icon={SlidersHorizontal}
        tone="primary"
        actions={
          viewLedger ? (
            <Button size="sm" variant="outline" onClick={viewLedger(kind)}>
              <Receipt />
              查看{kind === "integral" ? "积分" : kind === "experience" ? "经验" : "余额"}流水
            </Button>
          ) : null
        }
        toolbar={<Segmented value={kind} onChange={setKind} options={KIND_OPTIONS} ariaLabel="调整对象" />}
      >
        <div className="flex flex-col gap-5">
          <AdjustForm key={kind} {...forms[kind]} />
          <KindFacts kind={kind} wallet={walletReady ? wallet : undefined} summary={summaryFor(kind)} />
        </div>
      </Section>

      <UserVipPanel appKey={appKey} userId={userId} onViewLedger={viewLedger?.("vip")} />

      <UserFeatureUnlocksPanel appKey={appKey} userId={userId} />
    </div>
  );
}

// ── 指标卡 ──────────────────────────

type PointSummary = NonNullable<ReturnType<typeof useUserPointSummaryQuery>["data"]>;

function PointMetric({
  label,
  icon,
  tone,
  value,
  summary,
  onClick
}: {
  label: string;
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
  value?: number;
  summary?: PointSummary;
  onClick?: () => void;
}) {
  const daily = summary?.daily ?? [];
  const spark = daily.map((day) => day.balance);
  const recentIn = daily.reduce((sum, day) => sum + day.in, 0);
  const recentOut = daily.reduce((sum, day) => sum + day.out, 0);
  const sub = summary
    ? `近 ${summary.days} 天 +${recentIn.toLocaleString("zh-CN")} / -${recentOut.toLocaleString("zh-CN")}`
    : "近 30 天变动";
  return (
    <MetricCard
      label={label}
      icon={icon}
      tone={tone}
      value={typeof value === "number" ? value.toLocaleString("zh-CN") : EMPTY}
      sub={sub}
      spark={spark.length > 1 ? spark : undefined}
      onClick={onClick}
    />
  );
}

function VipMetric({
  loading,
  error,
  entitlement,
  onClick
}: {
  loading: boolean;
  error: boolean;
  entitlement?: VipEntitlement;
  onClick?: () => void;
}) {
  const now = useNow();
  let value: ReactNode = EMPTY;
  let sub: ReactNode = error ? "加载失败" : "权益信息不可用";
  let tone: Tone = "neutral";
  if (entitlement) {
    if (entitlement.isLifetime) {
      value = (
        <span className="inline-flex items-center gap-1.5">
          <InfinityIcon className="size-5" />
          永久会员
        </span>
      );
      sub = entitlement.planName || `开通于 ${formatTime(entitlement.lifetimeSince)}`;
      tone = "violet";
    } else if (entitlement.isVip) {
      const expire = entitlement.expireAt ? new Date(entitlement.expireAt).getTime() : Number.NaN;
      const days = Number.isFinite(expire) ? Math.max(0, Math.ceil((expire - now) / DAY)) : entitlement.remainingDays;
      value = `剩余 ${days} 天`;
      sub = [entitlement.isTrial ? "试用" : entitlement.planName, entitlement.expireAt ? `${formatTime(entitlement.expireAt)} 到期` : ""]
        .filter(Boolean)
        .join("，");
      tone = entitlement.isTrial ? "info" : "violet";
    } else {
      value = entitlement.expireAt ? "已过期" : "非会员";
      sub = entitlement.expireAt ? `${formatTime(entitlement.expireAt)} 到期` : "没有会员记录";
      tone = entitlement.expireAt ? "warning" : "neutral";
    }
  }
  return <MetricCard label="会员" icon={Crown} tone={tone} loading={loading} value={value} sub={sub} onClick={onClick} />;
}

// ── 调整区下方的账户概况 ──────────────────────────

function KindFacts({ kind, wallet, summary }: { kind: AssetKind; wallet?: UserWallet; summary?: PointSummary }) {
  if (kind === "wallet") {
    if (!wallet) return null;
    return (
      <div className="border-t pt-4">
        <InfoGrid
          columns={3}
          items={[
            { label: "冻结金额", value: `¥${formatMoney(wallet.frozen)}` },
            { label: "累计充值", value: `¥${formatMoney(wallet.totalRecharged)}` },
            { label: "累计消费", value: `¥${formatMoney(wallet.totalConsumed)}` },
            { label: "开户时间", value: wallet.createdAt && !isZeroTime(wallet.createdAt) ? formatTime(wallet.createdAt) : "未开户" },
            { label: "最近变动", value: wallet.updatedAt && !isZeroTime(wallet.updatedAt) ? formatTime(wallet.updatedAt) : EMPTY }
          ]}
        />
      </div>
    );
  }
  if (!summary) return null;
  if (!summary.count) {
    return (
      <div className="border-t pt-2">
        <StateBlock compact title="还没有任何流水" />
      </div>
    );
  }
  return (
    <div className="border-t pt-4">
      <InfoGrid
        columns={3}
        items={[
          { label: "累计获得", value: `+${summary.totalIn.toLocaleString("zh-CN")}`, tone: "success" },
          { label: "累计支出", value: `-${summary.totalOut.toLocaleString("zh-CN")}`, tone: summary.totalOut ? "danger" : undefined },
          { label: "流水笔数", value: summary.count.toLocaleString("zh-CN") },
          { label: "管理员累计增加", value: summary.adminIn.toLocaleString("zh-CN") },
          { label: "管理员累计扣减", value: summary.adminOut.toLocaleString("zh-CN") },
          { label: "最近变动", value: summary.lastAt ? formatTime(summary.lastAt) : EMPTY }
        ]}
      />
    </div>
  );
}
