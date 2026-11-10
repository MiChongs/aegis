"use client";

import { useState, type ReactNode } from "react";
import Link from "next/link";
import { Bot, Check, ExternalLink, Gavel, Loader2, Quote, RefreshCw, ShieldCheck, Sparkles, Undo2, UserRound } from "lucide-react";
import { toast } from "sonner";

import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { ErrorState, errorText } from "@/components/risk/decisions";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { BAN_SOURCE_LABEL } from "@/components/users/ban-form";
import type { BanAppeal, BanAppealDetail } from "@/lib/api/ban-appeals";
import { useBanAppeal, useBanAppealSettings, useDecideBanAppeal, useRerunBanAppealReview } from "@/lib/ban-appeal-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";

import {
  AppealStateBadge,
  Block,
  ConfidenceMeter,
  Facts,
  LevelBadge,
  banTitle,
  evidenceMeta,
  isOpen,
  pct,
  reasonCodeLabel,
  userName,
  verdictMeta,
} from "./appeal-kit";

const LOCALE_LABEL: Record<string, string> = { en: "英语", "zh-CN": "简体中文", "zh-TW": "繁体中文", ja: "日语", ko: "韩语" };
const REPLY_MAX = 2000;

/**
 * 一条申诉的复审面板：封禁、账号、申诉原文、模型结论、风控证据、历次申诉，以及通过或驳回。
 * 宽屏放在申诉队列右栏，窄屏装进对话框；两处是同一个组件。
 */
export function AppealReview({
  appKey,
  appealId,
  appName,
  onDecided,
}: {
  appKey: string;
  appealId: number;
  /** 跨应用查看时显示应用名 */
  appName?: string;
  /** 通过或驳回之后（用来跳到下一条） */
  onDecided?: () => void;
}) {
  const detail = useBanAppeal(appKey, appealId);

  if (detail.isLoading) return <ReviewSkeleton />;
  if (detail.isError || !detail.data) return <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />;
  return <ReviewBody appKey={appKey} data={detail.data} appName={appName} onDecided={onDecided} refreshing={detail.isFetching} />;
}

function ReviewBody({
  appKey,
  data,
  appName,
  onDecided,
  refreshing,
}: {
  appKey: string;
  data: BanAppealDetail;
  appName?: string;
  onDecided?: () => void;
  refreshing: boolean;
}) {
  const { appeal, ban, user, evidence } = data;
  const history = (data.history ?? []).filter((h) => h.id !== appeal.id);
  const rerun = useRerunBanAppealReview(appKey);
  const settings = useBanAppealSettings(appKey);
  const model = appeal.model;
  const vm = verdictMeta(model?.verdict);
  const VIcon = vm.icon;
  const ev = evidenceMeta(evidence?.level);
  const threshold =
    model?.verdict === "approve" ? settings.data?.approveThreshold : model?.verdict === "reject" ? settings.data?.rejectThreshold : undefined;
  const name = userName(user, appeal.userId);
  const href = `/app-users/${encodeURIComponent(appKey)}/${appeal.userId}?tab=risk`;

  async function runAgain() {
    try {
      await rerun.mutateAsync(appeal.id);
      toast.success("已重新提交模型复审", { description: "结论出来后这里会自动更新" });
    } catch (e) {
      toast.error("重新复审失败", { description: errorText(e) });
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-6">
      {/* 头部：谁、什么时候、申诉的是哪条封禁 */}
      <header className="flex min-w-0 items-start gap-3">
        <Avatar className="size-11 shrink-0">
          {user?.avatar ? <AvatarImage src={user.avatar} alt="" /> : null}
          <AvatarFallback className="bg-muted text-xs text-muted-foreground">
            <UserRound className="size-4" />
          </AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h3 className="truncate text-base font-semibold">{name}</h3>
            <AppealStateBadge appeal={appeal} />
            {refreshing ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
          </div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
            {[
              appName ? appName : "",
              `申诉 #${appeal.id}`,
              `${formatRelative(appeal.submittedAt)}提交`,
              `针对${banTitle(ban)} #${appeal.banId}`,
            ]
              .filter(Boolean)
              .join("，")}
          </p>
        </div>
        <Button asChild size="sm" variant="outline" className="shrink-0">
          <Link href={href}>
            <ExternalLink className="size-3.5" />
            风险详情
          </Link>
        </Button>
      </header>

      {/* 申诉原文 */}
      <Block title="用户的申诉">
        <figure className="rounded-xl border bg-muted/30 px-4 py-3">
          <Quote className="mb-1 size-4 text-muted-foreground/60" />
          <blockquote className="text-sm leading-6 break-words whitespace-pre-wrap">{appeal.content || "（没有填写内容）"}</blockquote>
          <figcaption className="mt-2 text-xs text-muted-foreground">
            {[
              formatDateTime(appeal.submittedAt),
              appeal.locale ? LOCALE_LABEL[appeal.locale] ?? appeal.locale : "",
              appeal.contactEmail ? `联系邮箱 ${appeal.contactEmail}` : "",
            ]
              .filter(Boolean)
              .join("，")}
          </figcaption>
        </figure>
      </Block>

      {/* 模型结论 */}
      <Block
        title="模型结论"
        aside={
          <Button size="xs" variant="ghost" disabled={rerun.isPending || !isOpen(appeal)} onClick={() => void runAgain()} title={isOpen(appeal) ? undefined : "已结案的申诉不能重新复审"}>
            {rerun.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            重新复审
          </Button>
        }
      >
        {model?.verdict ? (
          <div className="flex flex-col gap-3 rounded-xl border p-4">
            <div className="flex items-center gap-3">
              <span className={cn("grid size-10 shrink-0 place-items-center rounded-xl border", vm.chip)}>
                <VIcon className="size-5" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline gap-2">
                  <span className="text-sm font-semibold">{vm.label}</span>
                  <span className="text-2xl leading-7 font-semibold tabular-nums">{pct(model.confidence)}</span>
                  <span className="text-xs text-muted-foreground">置信度</span>
                </div>
                <ConfidenceMeter value={model.confidence} verdict={model.verdict} threshold={threshold} className="mt-1.5" />
              </div>
            </div>
            {threshold !== undefined && settings.data?.autoReview === "auto" ? (
              <p className="text-xs text-muted-foreground">
                竖线是自动{model.verdict === "approve" ? "通过" : "驳回"}门槛 {pct(threshold)}
                {model.confidence >= threshold ? "，置信度已达到" : "，置信度未达到，转人工"}
              </p>
            ) : null}
            {model.summary ? <p className="text-sm leading-6 break-words">{model.summary}</p> : null}
            {model.suggestedReply ? (
              <div className="rounded-lg bg-muted/40 px-3 py-2.5">
                <div className="mb-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                  <Sparkles className="size-3" />
                  建议给用户的回复
                </div>
                <p className="text-sm leading-6 break-words whitespace-pre-wrap">{model.suggestedReply}</p>
              </div>
            ) : null}
            <p className="text-[11px] text-muted-foreground">
              {[model.model ? `模型 ${model.model}` : "", model.reviewedAt ? `${formatRelative(model.reviewedAt)}给出` : ""].filter(Boolean).join("，")}
            </p>
          </div>
        ) : (
          <div className="flex items-start gap-3 rounded-xl border border-dashed px-4 py-3">
            <TonalIcon icon={Bot} tone="muted" size="sm" />
            <div className="min-w-0 text-sm">
              <p>{appeal.status === "reviewing" ? "模型正在复审" : "模型还没有给出结论"}</p>
              <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
                {model?.error
                  ? `上次复审没有完成：${model.error}`
                  : settings.data?.autoReview === "off"
                    ? "这个应用的自动复审已关闭，只由人工处理。"
                    : "提交后一分钟内会自动复审，也可以点「重新复审」立即再跑一次。"}
              </p>
            </div>
          </div>
        )}
      </Block>

      {/* 风控证据 */}
      <Block title="风控证据" aside={<Badge variant={ev.variant} size="sm">{ev.label}</Badge>}>
        {evidence ? (
          <div className="flex flex-col gap-3 rounded-xl border p-4">
            <p className={cn("text-sm", ev.tone)}>{ev.detail}</p>
            <div className="grid grid-cols-3 gap-2">
              <Metric label="近 30 天最高风险分" value={evidence.maxScore ?? 0} />
              <Metric label="近 30 天被拦截" value={`${evidence.blockedCount ?? 0} 次`} />
              <Metric label="关联账号" value={`${evidence.relatedAccounts ?? 0} 个`} />
            </div>
            {evidence.facts?.length ? (
              <ul className="flex flex-col gap-1.5">
                {evidence.facts.map((f, i) => (
                  <li key={i} className="flex gap-2 text-sm leading-6">
                    <span aria-hidden className={cn("mt-2.5 size-1.5 shrink-0 rounded-full", evidence.level === "strong" ? "bg-rose-500" : evidence.level === "medium" ? "bg-amber-500" : "bg-emerald-500")} />
                    <span className="min-w-0 break-words">{f}</span>
                  </li>
                ))}
              </ul>
            ) : null}
            {evidence.rules?.length || evidence.lists?.length ? (
              <div className="flex flex-wrap gap-1.5">
                {(evidence.rules ?? []).map((r) => (
                  <span key={`r-${r}`} className="rounded-md border bg-muted/40 px-1.5 py-0.5 text-[11px]">命中规则 {r}</span>
                ))}
                {(evidence.lists ?? []).map((l) => (
                  <span key={`l-${l}`} className="rounded-md border border-dashed px-1.5 py-0.5 text-[11px] text-muted-foreground">{l}</span>
                ))}
              </div>
            ) : null}
          </div>
        ) : (
          <p className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">风控平台没有这个账号的记录</p>
        )}
      </Block>

      {/* 封禁 */}
      <Block title="被申诉的封禁" aside={ban ? <LevelBadge level={ban.level} /> : null}>
        {ban ? (
          <div className="flex flex-col gap-3 rounded-xl border p-4">
            <Facts
              items={[
                { label: "类型", value: banTitle(ban) },
                { label: "范围", value: ban.banScope === "login" ? "仅登录" : "全部访问" },
                { label: "原因类别", value: reasonCodeLabel(ban.reasonCode) || "未分类" },
                { label: "来源", value: ban.source ? BAN_SOURCE_LABEL[ban.source] ?? ban.source : "管理员" },
                { label: "开始", value: formatDateTime(ban.startAt) },
                { label: "到期", value: ban.endAt ? formatDateTime(ban.endAt) : "永久" },
                { label: "操作人", value: ban.bannedByAdminName || (ban.source === "risk" ? "风控平台" : "系统") },
                { label: "状态", value: ban.status === "active" ? "生效中" : ban.status === "revoked" ? "已解除" : ban.status === "expired" ? "已到期" : ban.status },
                { label: "全站封禁", value: ban.platformBanId ? `#${ban.platformBanId}` : null, mono: true },
                { label: "内部原因", value: ban.reason || "未填写", wide: true },
                { label: "对用户显示的说明", value: ban.publicReason || "使用类别的标准说明", wide: true },
              ]}
            />
          </div>
        ) : (
          <p className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">封禁记录已不存在</p>
        )}
      </Block>

      {/* 账号 */}
      <Block title="账号">
        <div className="rounded-xl border p-4">
          <Facts
            items={[
              { label: "显示名", value: name },
              { label: "登录名", value: user?.account || null },
              { label: "对外 ID", value: user?.publicId ? String(user.publicId) : String(appeal.userId), mono: true },
              { label: "邮箱", value: user?.email || null },
              { label: "手机号", value: user?.phone || null },
              { label: "注册", value: user?.createdAt ? formatDateTime(user.createdAt) : null },
              { label: "最近登录", value: user?.lastLoginAt ? formatRelative(user.lastLoginAt) : null },
            ]}
          />
        </div>
      </Block>

      {/* 历次申诉 */}
      {history.length ? (
        <Block title={`这条封禁的历次申诉（${history.length}）`}>
          <ol className="flex flex-col">
            {history.map((h, i) => (
              <HistoryItem key={h.id} item={h} last={i === history.length - 1} />
            ))}
          </ol>
        </Block>
      ) : null}

      {/* 处理 */}
      {isOpen(appeal) ? (
        <DecisionForm key={appeal.id} appKey={appKey} data={data} onDecided={onDecided} />
      ) : (
        <DecidedNote appeal={appeal} />
      )}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-lg bg-muted/40 px-3 py-2">
      <div className="text-lg leading-6 font-semibold tabular-nums">{value}</div>
      <div className="truncate text-[11px] text-muted-foreground">{label}</div>
    </div>
  );
}

function HistoryItem({ item, last }: { item: BanAppeal; last: boolean }) {
  const outcome =
    item.status === "approved"
      ? `${item.decidedBy === "model" ? "被模型" : "被人工"}通过`
      : item.status === "rejected"
        ? `${item.decidedBy === "model" ? "被模型" : "被人工"}驳回`
        : item.status === "withdrawn"
          ? "用户撤回"
          : "仍在处理";
  return (
    <li className={cn("relative flex gap-3", !last && "pb-4")}>
      {!last ? <span aria-hidden className="absolute top-5 bottom-0 left-[5px] w-px bg-border" /> : null}
      <span aria-hidden className="mt-1.5 size-2.5 shrink-0 rounded-full border-2 border-muted-foreground/40 bg-background" />
      <div className="min-w-0 flex-1">
        <p className="text-sm">
          {formatDateTime(item.submittedAt)} 提交，{outcome}
          {item.decidedAt ? <span className="text-muted-foreground">（{formatRelative(item.decidedAt)}）</span> : null}
        </p>
        <p className="mt-0.5 line-clamp-2 text-xs leading-5 text-muted-foreground">{item.content}</p>
        {item.reply ? <p className="mt-1 rounded-md bg-muted/40 px-2 py-1 text-xs leading-5">回复：{item.reply}</p> : null}
      </div>
    </li>
  );
}

function DecidedNote({ appeal }: { appeal: BanAppeal }) {
  if (appeal.status === "withdrawn") {
    return <p className="rounded-xl bg-muted/40 px-4 py-3 text-sm text-muted-foreground">用户已于 {formatDateTime(appeal.decidedAt)} 撤回这条申诉。</p>;
  }
  const approved = appeal.status === "approved";
  const who = appeal.decidedBy === "model" ? "决策模型" : appeal.decidedByAdminName || "管理员";
  return (
    <section className={cn("flex flex-col gap-2 rounded-xl border px-4 py-3", approved ? "border-emerald-500/30 bg-emerald-500/[0.05]" : "border-rose-500/30 bg-rose-500/[0.05]")}>
      <p className="flex items-center gap-2 text-sm font-medium">
        {approved ? <ShieldCheck className="size-4 text-emerald-600 dark:text-emerald-400" /> : <Gavel className="size-4 text-rose-600 dark:text-rose-400" />}
        {who}于 {formatDateTime(appeal.decidedAt)} {approved ? "通过申诉并解除封禁" : "驳回申诉，维持封禁"}
      </p>
      {appeal.reply ? <p className="text-sm leading-6 break-words whitespace-pre-wrap">给用户的回复：{appeal.reply}</p> : null}
      {appeal.note ? <p className="text-xs leading-5 text-muted-foreground">内部备注：{appeal.note}</p> : null}
    </section>
  );
}

function DecisionForm({ appKey, data, onDecided }: { appKey: string; data: BanAppealDetail; onDecided?: () => void }) {
  const suggested = data.appeal.model?.suggestedReply ?? "";
  const [reply, setReply] = useState(suggested);
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState(false);
  const decide = useDecideBanAppeal(appKey);
  const settings = useBanAppealSettings(appKey);
  const platform = data.ban?.level === "platform";
  const allowDays = settings.data?.allowDays ?? 0;

  async function submit(decision: "approve" | "reject") {
    try {
      await decide.mutateAsync({ id: data.appeal.id, decision, reply: reply.trim(), note: note.trim() });
      setConfirm(false);
      toast.success(decision === "approve" ? "已通过，封禁已解除" : "已驳回，封禁维持不变", { description: "结果已按用户语言通知" });
      onDecided?.();
    } catch (e) {
      toast.error(decision === "approve" ? "通过失败" : "驳回失败", { description: errorText(e) });
    }
  }

  return (
    <section className="flex flex-col gap-4 rounded-xl border bg-card p-4">
      <div className="flex flex-col gap-1.5">
        <div className="flex items-center justify-between gap-2">
          <Label htmlFor="appeal-reply" className="text-xs text-muted-foreground">给用户的回复</Label>
          <div className="flex items-center gap-2">
            {suggested && reply !== suggested ? (
              <Button size="xs" variant="ghost" onClick={() => setReply(suggested)}>
                <Undo2 />
                用模型建议
              </Button>
            ) : null}
            <span className="text-xs tabular-nums text-muted-foreground">{reply.length}/{REPLY_MAX}</span>
          </div>
        </div>
        <Textarea
          id="appeal-reply"
          rows={4}
          maxLength={REPLY_MAX}
          value={reply}
          placeholder="随结果一起发给用户；留空时发送标准文案"
          onChange={(e) => setReply(e.target.value)}
          className="resize-none"
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="appeal-note" className="text-xs text-muted-foreground">内部备注（可选）</Label>
        <Textarea id="appeal-note" rows={2} maxLength={500} value={note} placeholder="只在控制台显示" onChange={(e) => setNote(e.target.value)} className="resize-none" />
      </div>
      <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button variant="outline" disabled={decide.isPending} onClick={() => void submit("reject")}>
          {decide.isPending && decide.variables?.decision === "reject" ? <Loader2 className="animate-spin" /> : <Gavel />}
          维持封禁
        </Button>
        <Button disabled={decide.isPending} onClick={() => setConfirm(true)}>
          <Check />
          通过并解封
        </Button>
      </div>

      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>通过申诉并解除封禁</AlertDialogTitle>
            <AlertDialogDescription>
              {platform ? "这是全站封禁，会整条解除，连带的所有账号一起恢复。" : "这个账号的封禁会立即解除。"}
              同时撤掉风控对这个账号的名单与复核单{allowDays ? `，并加 ${allowDays} 天白名单保护` : ""}，然后按用户语言发送「账号已恢复」通知。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={decide.isPending}
              onClick={(e) => {
                e.preventDefault();
                void submit("approve");
              }}
            >
              {decide.isPending ? <Loader2 className="animate-spin" /> : <Check />}
              确认通过
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

function ReviewSkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-3">
        <Skeleton className="size-11 rounded-full" />
        <div className="flex-1 space-y-1.5">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-3 w-64" />
        </div>
      </div>
      <Skeleton className="h-28 rounded-xl" />
      <Skeleton className="h-36 rounded-xl" />
      <Skeleton className="h-32 rounded-xl" />
      <Skeleton className="h-40 rounded-xl" />
    </div>
  );
}
