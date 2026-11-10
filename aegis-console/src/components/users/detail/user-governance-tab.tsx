"use client";

import { useState, type ComponentType, type ReactNode } from "react";
import {
  Ban,
  Download,
  Gavel,
  History,
  Hourglass,
  Loader2,
  ShieldAlert,
  ShieldBan,
  ShieldCheck,
  Trash2,
  TriangleAlert,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
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
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  BAN_LEVEL_LABEL,
  BAN_REASON_CODE_LABEL,
  BAN_SOURCE_LABEL,
  BanDurationField,
  BanLevelField,
  BanNoticeField,
  BanPublicReasonField,
  BanReasonCodeField,
  BanReasonField,
  BanScopeField,
  describeBanDraft,
  formatBanTime,
  useBanDraft,
  useNow
} from "@/components/users/ban-form";
import { useAdminToken, useUpdateAdminAppUserStatusMutation } from "@/lib/admin-hooks";
import { getAdminUserBans } from "@/lib/api/app-users";
import type { AccountBanCounts, AdminAppUserDetail, AdminUserBan } from "@/lib/api/types";
import {
  useAdminUserBansQuery,
  useCreateAdminUserBanMutation,
  useRevokeAdminUserBanMutation
} from "@/lib/app-user-hooks";
import { formatDateTime, formatRelative, parseInstant } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  ResetButton,
  Section,
  StateBlock,
  ToneIcon,
  collectPages,
  downloadCsv,
  errorMessage,
  type InfoItem,
  type Tone
} from "./kit";
import { UserAccountDeletionCard } from "./user-account-deletion-card";
import { BAN_SCOPE_LABEL, BAN_STATUS_LABEL, BAN_TYPE_LABEL, BanStatusBadge, ValueTree, textValue } from "./user-detail-shared";

/** 被新封禁顶替时后端写在旧记录上的撤销原因，与 pgrepo.AccountBanSupersededReason 一致。 */
const SUPERSEDED_REASON = "已被新的封禁替代";

type HistoryFilter = "" | "active" | "expired" | "revoked";

const banTypeLabel = (ban: AdminUserBan) => BAN_TYPE_LABEL[ban.banType] ?? ban.banType;
const banScopeLabel = (ban: AdminUserBan) => BAN_SCOPE_LABEL[ban.banScope] ?? ban.banScope;
const isPlatformBan = (ban: AdminUserBan) => ban.level === "platform";

/** 全站封禁的标记；本应用级不显示，避免每条记录都多一个徽标 */
function LevelBadge({ ban }: { ban: AdminUserBan }) {
  if (!isPlatformBan(ban)) return null;
  return (
    <Badge variant="danger" size="sm" title="同一人在所有应用的账号都被封禁">
      {BAN_LEVEL_LABEL.platform}
    </Badge>
  );
}

/** 原因类别与来源，拼成一句：「欺诈或作弊，由风控平台下发」 */
function banOrigin(ban: AdminUserBan) {
  const parts = [
    ban.reasonCode ? BAN_REASON_CODE_LABEL[ban.reasonCode] ?? ban.reasonCode : "",
    ban.source && ban.source !== "manual" ? `由${BAN_SOURCE_LABEL[ban.source] ?? ban.source}下发` : ""
  ].filter(Boolean);
  return parts.join("，");
}

/**
 * 处置：这个账号现在被怎么处置着、要怎么处置、以前被怎么处置过。
 *
 *   当前状态     正常 / 受限（迁移来的老数据）/ 封禁中，含原因、起止、剩余、操作人
 *   封禁或限制   时长、范围、原因；已有封禁时提交即替换
 *   处置历史     封禁记录时间线：状态筛选、分页、详情、解除、导出
 *   账号注销     进行中申请的审核 / 撤销 / 改期 / 执行，或代用户发起
 *   危险区       删除用户（交给外壳的确认框）
 *
 * 一切处置都落成封禁记录（app_user_bans）：有类型、范围、起止、操作人，可解除、留痕、到期自动失效。
 * 只有迁移来的老数据可能「受限却没有封禁记录」，状态横幅为它单独留了解除入口。
 */
export function UserGovernanceTab({
  appKey,
  userId,
  user,
  activeBan,
  onDelete,
  deletePending,
  onUserDeleted
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  activeBan?: AdminUserBan | null;
  onDelete: () => void;
  deletePending: boolean;
  /** 账号在注销卡片里被注销之后（用户已不存在） */
  onUserDeleted: () => void;
}) {
  const now = useNow();
  const [filter, setFilter] = useState<HistoryFilter>("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(10);
  const bansQuery = useAdminUserBansQuery(appKey, userId, { status: filter || undefined, page, limit });
  const counts = bansQuery.data?.counts;
  const [revoking, setRevoking] = useState<AdminUserBan | null>(null);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [detail, setDetail] = useState<AdminUserBan | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

  const openRevoke = (ban: AdminUserBan) => {
    setRevoking(ban);
    setRevokeOpen(true);
  };
  const pickFilter = (value: HistoryFilter) => {
    setFilter(value);
    setPage(1);
  };

  return (
    <div className="flex flex-col gap-5">
      <StatusBanner
        appKey={appKey}
        userId={userId}
        user={user}
        activeBan={activeBan ?? null}
        now={now}
        onRevoke={openRevoke}
      />

      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <CountCard label="历史封禁" value={counts?.total} icon={History} tone="neutral" loading={bansQuery.isLoading} onClick={() => pickFilter("")} />
        <CountCard label="生效中" value={counts?.active} icon={ShieldBan} tone={counts?.active ? "danger" : "neutral"} loading={bansQuery.isLoading} onClick={() => pickFilter("active")} />
        <CountCard label="已到期" value={counts?.expired} icon={Hourglass} tone="neutral" loading={bansQuery.isLoading} onClick={() => pickFilter("expired")} />
        <CountCard label="已解除" value={counts?.revoked} icon={Undo2} tone="info" loading={bansQuery.isLoading} onClick={() => pickFilter("revoked")} />
      </div>

      <BanComposer appKey={appKey} userId={userId} activeBan={activeBan ?? null} now={now} />

      <HistorySection
        appKey={appKey}
        userId={userId}
        filter={filter}
        onFilter={pickFilter}
        page={page}
        onPage={setPage}
        limit={limit}
        onLimit={(value) => {
          setLimit(value);
          setPage(1);
        }}
        counts={counts}
        query={bansQuery}
        onOpen={(ban) => {
          setDetail(ban);
          setDetailOpen(true);
        }}
        onRevoke={openRevoke}
      />

      <UserAccountDeletionCard appKey={appKey} userId={userId} account={user.account ?? ""} onUserDeleted={onUserDeleted} />

      <Section title="危险区" description="以下操作不可撤销" icon={TriangleAlert} tone="danger" className="border-red-200 dark:border-red-900/60">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="text-sm font-medium">删除用户</span>
            <span className="text-xs leading-5 text-muted-foreground">
              账号与其资产、会话、记录一并删除，无法恢复。需要保留注销流程与墓碑记录时，请改用上方「账号注销」。
            </span>
          </div>
          <Button size="sm" variant="destructive" disabled={deletePending} onClick={onDelete} className="self-start sm:self-auto">
            {deletePending ? <Loader2 className="animate-spin" /> : <Trash2 />}
            删除用户
          </Button>
        </div>
      </Section>

      <BanDetailSheet
        ban={detail}
        open={detailOpen}
        onOpenChange={setDetailOpen}
        onRevoke={(ban) => {
          setDetailOpen(false);
          openRevoke(ban);
        }}
      />
      <RevokeBanDialog appKey={appKey} userId={userId} ban={revoking} open={revokeOpen} onOpenChange={setRevokeOpen} />
    </div>
  );
}

function CountCard({
  label,
  value,
  icon,
  tone,
  loading,
  onClick
}: {
  label: string;
  value?: number;
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
  loading: boolean;
  onClick: () => void;
}) {
  return (
    <MetricCard
      label={label}
      value={typeof value === "number" ? value.toLocaleString("zh-CN") : "—"}
      sub="点击筛选记录"
      icon={icon}
      tone={tone}
      loading={loading}
      onClick={onClick}
    />
  );
}

// ── 当前状态 ──────────────────────────

const BANNER_TONE: Record<"success" | "warning" | "danger", string> = {
  success: "border-emerald-200 dark:border-emerald-900/60",
  warning: "border-amber-200 dark:border-amber-900/60",
  danger: "border-red-200 dark:border-red-900/60"
};

function BannerShell({
  tone,
  icon,
  title,
  badges,
  description,
  action,
  facts,
  progress
}: {
  tone: "success" | "warning" | "danger";
  icon: ComponentType<{ className?: string }>;
  title: string;
  badges?: ReactNode;
  description: ReactNode;
  action?: ReactNode;
  facts?: Array<{ label: string; value: string; title?: string }>;
  progress?: number | null;
}) {
  return (
    <Card className={cn("gap-0 overflow-hidden py-0", BANNER_TONE[tone])}>
      <div className="flex flex-col gap-4 px-4 py-4 sm:flex-row sm:items-start sm:px-5">
        <ToneIcon icon={icon} tone={tone} size="lg" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-base font-semibold">{title}</span>
            {badges}
          </div>
          <div className="text-sm leading-6 break-words text-muted-foreground">{description}</div>
        </div>
        {action ? <div className="flex shrink-0 flex-wrap gap-2 self-start">{action}</div> : null}
      </div>
      {facts?.length ? (
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 border-t px-4 py-3.5 sm:grid-cols-4 sm:px-5">
          {facts.map((fact) => (
            <div key={fact.label} className="flex min-w-0 flex-col gap-0.5">
              <dt className="text-xs text-muted-foreground">{fact.label}</dt>
              <dd className="truncate text-sm tabular-nums" title={fact.title ?? fact.value}>
                {fact.value}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      {typeof progress === "number" ? (
        <div className="h-1 w-full bg-red-500/10" role="progressbar" aria-valuenow={Math.round(progress)} aria-valuemin={0} aria-valuemax={100}>
          <div className="h-full bg-red-500/70 transition-[width] duration-500" style={{ width: `${progress}%` }} />
        </div>
      ) : null}
    </Card>
  );
}

function StatusBanner({
  appKey,
  userId,
  user,
  activeBan,
  now,
  onRevoke
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  activeBan: AdminUserBan | null;
  now: number;
  onRevoke: (ban: AdminUserBan) => void;
}) {
  if (activeBan) {
    const permanent = activeBan.banType === "permanent";
    const start = parseInstant(activeBan.startAt)?.getTime() ?? 0;
    const end = parseInstant(activeBan.endAt)?.getTime() ?? null;
    const progress = end && end > start ? Math.min(100, Math.max(0, ((now - start) / (end - start)) * 100)) : null;
    return (
      <BannerShell
        tone="danger"
        icon={ShieldBan}
        title={`${isPlatformBan(activeBan) ? "全站" : ""}${banTypeLabel(activeBan)}封禁中`}
        badges={
          <>
            <LevelBadge ban={activeBan} />
            <Badge variant="danger" size="sm">
              {banScopeLabel(activeBan)}
            </Badge>
            {activeBan.banScope === "login" ? (
              <span className="text-xs text-muted-foreground">已登录的设备不受影响</span>
            ) : null}
          </>
        }
        description={
          <span className="text-foreground/90">
            {textValue(activeBan.reason, "未填写原因")}
            {banOrigin(activeBan) ? <span className="text-muted-foreground">（{banOrigin(activeBan)}）</span> : null}
          </span>
        }
        action={
          <Button size="sm" variant="outline" onClick={() => onRevoke(activeBan)}>
            <Undo2 />
            解除封禁
          </Button>
        }
        facts={[
          { label: "开始", value: formatDateTime(activeBan.startAt) },
          { label: "到期", value: permanent ? "永久，需手动解除" : formatDateTime(activeBan.endAt) },
          { label: "剩余", value: permanent || !end ? "—" : formatRelative(activeBan.endAt, "—", now).replace(/后$/, "") },
          { label: "操作人", value: textValue(activeBan.bannedByAdminName, "系统") }
        ]}
        progress={progress}
      />
    );
  }

  const endMs = parseInstant(user.disabledEndTime)?.getTime() ?? null;
  const legacyRestricted = user.enabled === false || (endMs !== null && endMs > now);
  if (legacyRestricted) {
    return <LegacyRestrictionBanner appKey={appKey} userId={userId} user={user} />;
  }

  return (
    <BannerShell
      tone="success"
      icon={ShieldCheck}
      title="账号状态正常"
      badges={
        <Badge variant="success" size="sm">
          正常
        </Badge>
      }
      description="没有生效中的封禁，可正常登录与访问"
    />
  );
}

/** 迁移来的老数据：users 上标着受限，却没有对应的封禁记录。 */
function LegacyRestrictionBanner({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const mutation = useUpdateAdminAppUserStatusMutation(appKey, userId);

  async function lift() {
    try {
      await mutation.mutateAsync({ enabled: true, clearDisabledEndTime: true });
      toast.success("已解除限制");
    } catch (error) {
      toast.error(errorMessage(error, "解除失败"));
    }
  }

  return (
    <BannerShell
      tone="warning"
      icon={ShieldAlert}
      title="账号受限"
      badges={
        <Badge variant="warning" size="sm">
          无封禁记录
        </Badge>
      }
      description={textValue(user.disabledReason, "未填写原因")}
      action={
        <Button size="sm" variant="outline" disabled={mutation.isPending} onClick={() => void lift()}>
          {mutation.isPending ? <Loader2 className="animate-spin" /> : <Undo2 />}
          解除限制
        </Button>
      }
      facts={[
        { label: "状态", value: user.enabled === false ? "已停用" : "限时受限" },
        { label: "到期", value: user.disabledEndTime ? formatDateTime(user.disabledEndTime) : "未设置" },
        { label: "来源", value: "历史数据迁移" },
        { label: "操作人", value: "—" }
      ]}
    />
  );
}

// ── 新建封禁 ──────────────────────────

function BanComposer({
  appKey,
  userId,
  activeBan,
  now
}: {
  appKey: string;
  userId: number;
  activeBan: AdminUserBan | null;
  now: number;
}) {
  const mutation = useCreateAdminUserBanMutation(appKey, userId);
  const draft = useBanDraft();
  const problem = draft.validate(now);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const end = draft.resolveEnd(now);

  async function submit() {
    const submitAt = Date.now();
    const error = draft.validate(submitAt);
    if (error) {
      toast.error(error);
      return;
    }
    try {
      await mutation.mutateAsync(draft.toPayload(submitAt));
      draft.reset();
      setConfirmOpen(false);
      toast.success(activeBan ? "已替换为新的封禁" : "封禁已生效");
    } catch (error) {
      toast.error(errorMessage(error, "封禁失败"));
    }
  }

  return (
    <Section
      title={activeBan ? "替换当前封禁" : "封禁或限制"}
      description={
        activeBan
          ? "提交后当前封禁立即失效，以新的封禁为准"
          : "选择「仅登录」可只拦截新的登录，已登录设备照常使用"
      }
      icon={Gavel}
      tone="danger"
      footer={
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <span className={cn("text-xs", problem ? "text-muted-foreground" : "text-foreground/80")}>
            {problem ?? describeBanDraft(draft)}
          </span>
          <Button
            size="sm"
            variant="destructive"
            disabled={Boolean(problem) || mutation.isPending}
            onClick={() => setConfirmOpen(true)}
            className="self-end sm:self-auto"
          >
            <Gavel />
            {activeBan ? "替换封禁" : "执行封禁"}
          </Button>
        </div>
      }
    >
      <div className="grid gap-6 lg:grid-cols-2">
        <div className="flex flex-col gap-5">
          <BanDurationField draft={draft} now={now} />
          <BanScopeField draft={draft} />
          <BanLevelField draft={draft} />
        </div>
        <div className="flex flex-col gap-5">
          <BanReasonCodeField draft={draft} />
          <BanReasonField draft={draft} rows={3} />
          <BanPublicReasonField draft={draft} rows={3} />
        </div>
      </div>
      <div className="mt-5">
        <BanNoticeField draft={draft} />
      </div>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{activeBan ? "替换当前封禁" : "确认封禁"}</AlertDialogTitle>
            <AlertDialogDescription>
              {describeBanDraft(draft)}。
              {end ? `将于 ${formatBanTime(end)} 自动解除。` : end === null ? "永久有效，只能手动解除。" : ""}
              {activeBan ? "当前生效的封禁会被立即替代。" : ""}
              {draft.level === "platform"
                ? "全站封禁会连带这个人在所有应用的账号，并拦截用相同邮箱、手机号、第三方账号、设备再注册或登录。"
                : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <dl className="flex flex-col gap-2 rounded-xl border bg-muted/30 px-3 py-2.5 text-sm leading-6">
            <div className="flex gap-3">
              <dt className="w-16 shrink-0 text-xs leading-6 text-muted-foreground">内部原因</dt>
              <dd className="min-w-0 break-words">{draft.reason.trim() || "—"}</dd>
            </div>
            <div className="flex gap-3">
              <dt className="w-16 shrink-0 text-xs leading-6 text-muted-foreground">对用户</dt>
              <dd className="min-w-0 break-words">
                {draft.publicReason.trim() || `${BAN_REASON_CODE_LABEL[draft.reasonCode]}（标准说明）`}
              </dd>
            </div>
            <div className="flex gap-3">
              <dt className="w-16 shrink-0 text-xs leading-6 text-muted-foreground">通知</dt>
              <dd className="min-w-0">
                {draft.notify ? "发送邮件与站内消息" : "不通知"}，{draft.appealable ? "允许申诉" : "不可申诉"}
              </dd>
            </div>
          </dl>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={mutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              {mutation.isPending ? <Loader2 className="animate-spin" /> : <Gavel />}
              {activeBan ? "替换封禁" : "执行封禁"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}

// ── 处置历史 ──────────────────────────

function HistorySection({
  appKey,
  userId,
  filter,
  onFilter,
  page,
  onPage,
  limit,
  onLimit,
  counts,
  query,
  onOpen,
  onRevoke
}: {
  appKey: string;
  userId: number;
  filter: HistoryFilter;
  onFilter: (value: HistoryFilter) => void;
  page: number;
  onPage: (page: number) => void;
  limit: number;
  onLimit: (limit: number) => void;
  counts?: AccountBanCounts;
  query: ReturnType<typeof useAdminUserBansQuery>;
  onOpen: (ban: AdminUserBan) => void;
  onRevoke: (ban: AdminUserBan) => void;
}) {
  const token = useAdminToken();
  const [exporting, setExporting] = useState(false);
  const bans = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const countText = (key: keyof AccountBanCounts) => (counts ? `（${counts[key]}）` : "");

  const exportCsv = async () => {
    if (!token) return;
    setExporting(true);
    try {
      const result = await collectPages(async (p, l) => {
        const data = await getAdminUserBans(token, appKey, userId, { status: filter || undefined, page: p, limit: l });
        return { items: data.items ?? [], total: data.total };
      });
      downloadCsv(
        `user-bans-${userId}`,
        [
          { header: "ID", value: (row) => row.id },
          { header: "类型", value: (row) => banTypeLabel(row) },
          { header: "范围", value: (row) => banScopeLabel(row) },
          { header: "状态", value: (row) => BAN_STATUS_LABEL[row.status] ?? row.status },
          { header: "原因", value: (row) => row.reason },
          { header: "开始", value: (row) => formatDateTime(row.startAt) },
          { header: "到期", value: (row) => (row.endAt ? formatDateTime(row.endAt) : "永久") },
          { header: "操作人", value: (row) => row.bannedByAdminName ?? "" },
          { header: "解除时间", value: (row) => (row.revokedAt ? formatDateTime(row.revokedAt) : "") },
          { header: "解除人", value: (row) => row.revokedByAdminName ?? "" },
          { header: "解除原因", value: (row) => row.revokeReason ?? "" },
          { header: "创建时间", value: (row) => formatDateTime(row.createdAt) }
        ],
        result.rows
      );
      if (result.truncated) toast.warning(`共 ${result.total} 条，仅导出前 ${result.rows.length} 条`);
      else toast.success(`已导出 ${result.rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  };

  return (
    <Section
      title="处置历史"
      description="全部封禁记录，含被替代与已解除的"
      icon={History}
      tone="violet"
      flush
      actions={
        <Button size="sm" variant="outline" onClick={() => void exportCsv()} disabled={exporting || total === 0}>
          {exporting ? <Loader2 className="animate-spin" /> : <Download />}
          导出 CSV
        </Button>
      }
      toolbar={
        <FilterBar>
          <FilterSelect
            label="状态"
            value={filter}
            allLabel={`全部${countText("total")}`}
            options={[
              { value: "active", label: `生效中${countText("active")}` },
              { value: "expired", label: `已到期${countText("expired")}` },
              { value: "revoked", label: `已解除${countText("revoked")}` }
            ]}
            onChange={(value) => onFilter(value as HistoryFilter)}
          />
          <ResetButton visible={Boolean(filter)} onClick={() => onFilter("")} />
        </FilterBar>
      }
      footer={
        total > 0 ? <Pager page={page} limit={limit} total={total} onPage={onPage} onLimit={onLimit} limits={[10, 20, 50]} /> : undefined
      }
    >
      {query.isLoading ? (
        <div className="flex flex-col gap-5 px-4 py-5 sm:px-5">
          {[0, 1, 2].map((index) => (
            <div key={index} className="flex gap-3">
              <Skeleton className="size-7 shrink-0 rounded-full" />
              <div className="flex flex-1 flex-col gap-2">
                <Skeleton className="h-4 w-48" />
                <Skeleton className="h-4 w-full max-w-md" />
                <Skeleton className="h-3 w-64" />
              </div>
            </div>
          ))}
        </div>
      ) : query.error && !bans.length ? (
        <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
      ) : !bans.length ? (
        <StateBlock
          icon={ShieldCheck}
          title={filter ? "没有符合条件的记录" : "这个账号没有被封禁过"}
          description={filter ? "换一个状态筛选看看" : undefined}
        />
      ) : (
        <ol className={cn("px-4 py-5 transition-opacity sm:px-5", query.isFetching && "opacity-60")}>
          {bans.map((ban, index) => (
            <BanEntry
              key={ban.id}
              ban={ban}
              last={index === bans.length - 1}
              onOpen={() => onOpen(ban)}
              onRevoke={() => onRevoke(ban)}
            />
          ))}
        </ol>
      )}
    </Section>
  );
}

const STATUS_DOT: Record<string, { icon: ComponentType<{ className?: string }>; tone: Tone }> = {
  active: { icon: ShieldBan, tone: "danger" },
  expired: { icon: Hourglass, tone: "neutral" },
  revoked: { icon: Undo2, tone: "info" }
};

function revokeText(ban: AdminUserBan) {
  if (ban.status !== "revoked") return null;
  if (ban.revokeReason === SUPERSEDED_REASON) return `于 ${formatDateTime(ban.revokedAt)} 被新的封禁替代`;
  return `${textValue(ban.revokedByAdminName, "系统")} 于 ${formatDateTime(ban.revokedAt)} 解除${ban.revokeReason ? `：${ban.revokeReason}` : ""}`;
}

function BanEntry({
  ban,
  last,
  onOpen,
  onRevoke
}: {
  ban: AdminUserBan;
  last: boolean;
  onOpen: () => void;
  onRevoke: () => void;
}) {
  const dot = STATUS_DOT[ban.status] ?? STATUS_DOT.expired;
  const revoked = revokeText(ban);

  return (
    <li className={cn("relative flex gap-3", !last && "pb-6")}>
      {!last ? <span aria-hidden className="absolute top-9 bottom-0 left-4 w-px bg-border" /> : null}
      <ToneIcon icon={dot.icon} tone={dot.tone} className="relative rounded-full" />

      <div className="flex min-w-0 flex-1 flex-col gap-1.5 pt-0.5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <button
            type="button"
            onClick={onOpen}
            className="text-sm font-medium underline-offset-4 transition-colors hover:text-primary hover:underline"
          >
            {banTypeLabel(ban)}封禁
          </button>
          <LevelBadge ban={ban} />
          <Badge variant="outline" size="sm">
            {banScopeLabel(ban)}
          </Badge>
          <BanStatusBadge status={ban.status} />
          <span className="ml-auto text-xs text-muted-foreground tabular-nums" title={formatDateTime(ban.createdAt)}>
            {formatRelative(ban.createdAt)}
          </span>
        </div>

        <p className="text-sm leading-6 break-words">{textValue(ban.reason, "未填写原因")}</p>
        {ban.publicReason ? (
          <p className="text-xs leading-5 break-words text-muted-foreground">对用户：{ban.publicReason}</p>
        ) : null}

        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {banOrigin(ban) ? <span>{banOrigin(ban)}</span> : null}
          <span className="tabular-nums">
            {formatDateTime(ban.startAt)} 至 {ban.endAt ? formatDateTime(ban.endAt) : "永久"}
          </span>
          <span>操作人 {textValue(ban.bannedByAdminName, "系统")}</span>
        </div>

        {revoked ? <div className="rounded-lg bg-muted/50 px-3 py-2 text-xs leading-5 text-muted-foreground">{revoked}</div> : null}

        <div className="flex flex-wrap gap-1">
          <Button size="xs" variant="ghost" onClick={onOpen}>
            查看详情
          </Button>
          {ban.status === "active" ? (
            <Button size="xs" variant="ghost" onClick={onRevoke}>
              <Undo2 />
              解除
            </Button>
          ) : null}
        </div>
      </div>
    </li>
  );
}

function BanDetailSheet({
  ban,
  open,
  onOpenChange,
  onRevoke
}: {
  ban: AdminUserBan | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRevoke: (ban: AdminUserBan) => void;
}) {
  const hasEvidence = Boolean(ban?.evidence && Object.keys(ban.evidence).length > 0);
  const fields: InfoItem[] = ban
    ? [
        { label: "类型", value: banTypeLabel(ban) },
        { label: "范围", value: banScopeLabel(ban) },
        { label: "封禁级别", value: BAN_LEVEL_LABEL[ban.level ?? "app"] ?? ban.level },
        { label: "原因类别", value: ban.reasonCode ? BAN_REASON_CODE_LABEL[ban.reasonCode] ?? ban.reasonCode : null },
        { label: "来源", value: ban.source ? BAN_SOURCE_LABEL[ban.source] ?? ban.source : null },
        { label: "允许申诉", value: ban.appealable === undefined ? null : ban.appealable ? "是" : "否" },
        ...(ban.platformBanId ? [{ label: "全站封禁 ID", value: String(ban.platformBanId), mono: true }] : []),
        { label: "对用户显示的说明", value: ban.publicReason || null, wide: true },
        { label: "开始", value: formatDateTime(ban.startAt) },
        { label: "到期", value: ban.endAt ? formatDateTime(ban.endAt) : "永久" },
        { label: "操作人", value: textValue(ban.bannedByAdminName, "系统") },
        { label: "创建时间", value: formatDateTime(ban.createdAt) },
        ...(ban.status === "revoked"
          ? [
              { label: "解除时间", value: formatDateTime(ban.revokedAt) },
              { label: "解除人", value: textValue(ban.revokedByAdminName, "系统") },
              { label: "解除原因", value: ban.revokeReason || null, wide: true }
            ]
          : []),
        { label: "记录 ID", value: String(ban.id), mono: true }
      ]
    : [];

  return (
    <RecordSheet
      open={open && Boolean(ban)}
      onOpenChange={onOpenChange}
      title={ban ? `${banTypeLabel(ban)}封禁` : "封禁记录"}
      description={ban ? `#${ban.id}` : undefined}
      hero={
        ban ? (
          <div className="flex flex-col gap-2 rounded-xl border bg-muted/30 px-3.5 py-3">
            <div className="flex items-center gap-2">
              <BanStatusBadge status={ban.status} />
              <LevelBadge ban={ban} />
              <Badge variant="outline" size="sm">
                {banScopeLabel(ban)}
              </Badge>
            </div>
            <p className="text-sm leading-6 break-words">{textValue(ban.reason, "未填写原因")}</p>
          </div>
        ) : null
      }
      fields={fields}
      raw={ban}
      actions={
        ban?.status === "active" ? (
          <Button size="sm" variant="outline" onClick={() => onRevoke(ban)}>
            <Undo2 />
            解除封禁
          </Button>
        ) : null
      }
    >
      {hasEvidence ? (
        <div className="flex flex-col gap-2">
          <div className="text-xs font-medium text-muted-foreground">证据</div>
          <div className="rounded-xl border bg-muted/30 px-3 py-2.5 text-xs">
            <ValueTree value={ban?.evidence} />
          </div>
        </div>
      ) : null}
    </RecordSheet>
  );
}

// ── 解除 ──────────────────────────

function RevokeBanDialog({
  appKey,
  userId,
  ban,
  open,
  onOpenChange
}: {
  appKey: string;
  userId: number;
  /** 关闭后仍保留上一条，关闭动画期间内容不会闪空 */
  ban: AdminUserBan | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const revoke = useRevokeAdminUserBanMutation(appKey, userId);
  const [reason, setReason] = useState("");

  async function submit() {
    if (!ban) return;
    try {
      await revoke.mutateAsync({ banId: ban.id, reason: reason.trim() || undefined });
      setReason("");
      onOpenChange(false);
      toast.success("封禁已解除");
    } catch (error) {
      toast.error(errorMessage(error, "解除失败"));
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>解除封禁</DialogTitle>
          <DialogDescription>
            {ban && isPlatformBan(ban)
              ? "这是全站封禁连带的记录。解除后整条全站封禁一并解除，所有被连带的账号都会恢复，并按各自的语言收到解封通知。"
              : "解除后立即生效，用户可以重新登录。"}
          </DialogDescription>
        </DialogHeader>
        {ban ? (
          <div className="flex items-start gap-3 rounded-xl border bg-muted/30 px-3 py-2.5">
            <ToneIcon icon={Ban} tone="danger" size="sm" />
            <div className="flex min-w-0 flex-col gap-0.5 text-xs leading-5">
              <span className="font-medium">
                {banTypeLabel(ban)}封禁，{banScopeLabel(ban)}
              </span>
              <span className="break-words text-muted-foreground">{textValue(ban.reason, "未填写原因")}</span>
            </div>
          </div>
        ) : null}
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="revoke-ban-reason" className="text-xs text-muted-foreground">
            解除原因（可选）
          </Label>
          <Textarea
            id="revoke-ban-reason"
            rows={3}
            value={reason}
            maxLength={500}
            placeholder="例如：申诉通过"
            onChange={(event) => setReason(event.target.value)}
            className="resize-none"
          />
        </div>
        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={revoke.isPending} onClick={() => void submit()}>
            {revoke.isPending ? <Loader2 className="animate-spin" /> : <Undo2 />}
            确认解除
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
