"use client";

import { useState } from "react";
import { Coins, Undo2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { newRewardRequestId, type FeedbackReward, type FeedbackRewardInfo } from "@/lib/api/feedback-rewards";
import type { TicketItem } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { formatDateTime, formatRelativeTime } from "@/components/tickets/ticket-shared";
import { useDeskActions } from "./use-desk-actions";
import { PanelSection } from "./desk-kit";
import { useDesk } from "./desk-context";

// 资料栏「详情」里的反馈奖励（只对 kind=feedback 显示）。
//
// 能不能发 / 撤回只看后端给的 permissions.reward / revokeReward 与 reward.ineligible；
// 建议积分、本次最多可发、今日与本月已领都由后端算好，这里只展示与预填。
// 一张反馈同一时刻只有一笔有效奖励：要改数额先撤回再发。

/** 本次可发上限的说明文字 */
export function rewardLimitHint(info: FeedbackRewardInfo) {
  const parts = [`今日已领 ${info.usage.daily}`, `本月已领 ${info.usage.monthly}`];
  if (info.maxGrantable !== null) parts.push(`本次最多 ${info.maxGrantable}`);
  return parts.join(" · ");
}

/** 发放数额能不能提交：正整数、不超过本次可发上限 */
export function rewardPointsError(points: number, info: FeedbackRewardInfo) {
  if (!Number.isInteger(points) || points <= 0) return "请填写正整数积分";
  if (info.maxGrantable !== null && points > info.maxGrantable) {
    return info.maxGrantable <= 0 ? "该用户今天或本月的反馈奖励已达上限" : `超过本次可发上限 ${info.maxGrantable}`;
  }
  return "";
}

/** 能否在这张单子上发放（权限 + 策略开启 + 单子本身可发 + 还没有有效奖励） */
export function canGrantReward(ticket: TicketItem) {
  const info = ticket.reward;
  return Boolean(ticket.permissions?.reward && info?.enabled && !info.ineligible && !info.active);
}

export function DeskRewardSection({ ticket }: { ticket: TicketItem }) {
  const info = ticket.reward;
  const desk = useDesk();
  const [grantOpen, setGrantOpen] = useState(false);
  const [revokeOpen, setRevokeOpen] = useState(false);
  if (!info) return null;

  const revoked = info.history.filter((item) => item.status === "revoked");
  const canGrant = canGrantReward(ticket);

  return (
    <PanelSection
      title="反馈奖励"
      action={
        canGrant ? (
          <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={() => setGrantOpen(true)}>
            <Coins className="size-3.5" />
            发放奖励
          </Button>
        ) : null
      }
    >
      {info.active ? (
        <ActiveReward reward={info.active} canRevoke={Boolean(ticket.permissions?.revokeReward)} onRevoke={() => setRevokeOpen(true)} />
      ) : (
        <p className="text-xs leading-5 text-muted-foreground">
          {!info.enabled
            ? "该应用未开启反馈奖励，可在「工单配置 → 反馈奖励」里开启。"
            : info.ineligible
              ? info.ineligible
              : ticket.permissions?.reward
                ? `建议 ${info.suggestedPoints} 积分 · ${rewardLimitHint(info)}`
                : `尚未发放。发放需要能处理这条${desk.noun}，并持有该应用的积分调整权限。`}
        </p>
      )}

      {revoked.length ? (
        <ul className="space-y-1.5 pt-1">
          {revoked.map((item) => (
            <li key={item.id} className="text-xs leading-5 text-muted-foreground">
              <span className="line-through">{item.points} 积分</span>
              <span className="mx-1">·</span>
              {item.revokedByName || "管理员"} 于 {formatDateTime(item.revokedAt)} 撤回，扣回 {item.clawedBack}
              {item.shortfall > 0 ? `（差 ${item.shortfall} 未扣）` : ""}
              {item.revokeReason ? `：${item.revokeReason}` : ""}
            </li>
          ))}
        </ul>
      ) : null}

      {grantOpen ? <GrantRewardDialog ticket={ticket} info={info} onClose={() => setGrantOpen(false)} /> : null}
      {revokeOpen && info.active ? (
        <RevokeRewardDialog ticket={ticket} reward={info.active} onClose={() => setRevokeOpen(false)} />
      ) : null}
    </PanelSection>
  );
}

function ActiveReward({ reward, canRevoke, onRevoke }: { reward: FeedbackReward; canRevoke: boolean; onRevoke: () => void }) {
  return (
    <div className="flex items-start gap-3 rounded-xl border bg-muted/30 px-3 py-2.5">
      <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg bg-amber-500/15 text-amber-600 dark:text-amber-400">
        <Coins className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold tabular-nums text-foreground">已发放 {reward.points} 积分</p>
        <p className="truncate text-xs text-muted-foreground" title={formatDateTime(reward.grantedAt)}>
          {reward.grantedByName || "管理员"} · {formatRelativeTime(reward.grantedAt)}
        </p>
        {reward.reason ? <p className="mt-1 text-xs leading-5 text-foreground/80">{reward.reason}</p> : null}
      </div>
      {canRevoke ? (
        <Button size="sm" variant="ghost" className="h-7 shrink-0 px-2 text-xs text-muted-foreground" onClick={onRevoke}>
          <Undo2 className="size-3.5" />
          撤回
        </Button>
      ) : null}
    </div>
  );
}

function GrantRewardDialog({ ticket, info, onClose }: { ticket: TicketItem; info: FeedbackRewardInfo; onClose: () => void }) {
  const actions = useDeskActions(ticket);
  const desk = useDesk();
  const initial = info.maxGrantable !== null ? Math.min(info.suggestedPoints, info.maxGrantable) : info.suggestedPoints;
  const [points, setPoints] = useState(String(initial > 0 ? initial : ""));
  const [reason, setReason] = useState("");
  // 一次弹窗一个请求编号：连点「发放」只落一笔
  const [requestId] = useState(newRewardRequestId);
  const value = Number(points);
  const error = points.trim() === "" ? "请填写积分" : rewardPointsError(value, info);

  const submit = async () => {
    if (error) return;
    if (await actions.grantReward({ points: value, reason: reason.trim() || undefined, requestId })) onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>发放反馈奖励</DialogTitle>
          <DialogDescription>
            积分立即到账，并给{desk.person}发一条站内信「你的{desk.noun} #{ticket.ticketNo} 被采纳，获得 N 积分」。一条{desk.noun}只能有一笔有效奖励。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="reward-points">积分</Label>
            <Input
              id="reward-points"
              inputMode="numeric"
              value={points}
              onChange={(event) => setPoints(event.target.value.replace(/[^\d]/g, ""))}
              aria-invalid={Boolean(error) && points !== ""}
            />
            <p className={cn("text-xs", error && points !== "" ? "text-destructive" : "text-muted-foreground")}>
              {error && points !== "" ? error : `建议 ${info.suggestedPoints} · ${rewardLimitHint(info)}`}
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="reward-reason">理由</Label>
            <Textarea
              id="reward-reason"
              rows={3}
              maxLength={200}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder={`选填，附在给${desk.person}的站内信里`}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button disabled={Boolean(error) || actions.rewarding} onClick={() => void submit()}>
            发放 {Number.isInteger(value) && value > 0 ? `${value} 积分` : ""}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RevokeRewardDialog({ ticket, reward, onClose }: { ticket: TicketItem; reward: FeedbackReward; onClose: () => void }) {
  const actions = useDeskActions(ticket);
  const desk = useDesk();
  const [reason, setReason] = useState("");
  const [allowPartial, setAllowPartial] = useState(false);
  const [requestId] = useState(newRewardRequestId);

  const submit = async () => {
    if (await actions.revokeReward({ reason: reason.trim() || undefined, allowPartial, requestId })) onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>撤回 {reward.points} 积分奖励？</DialogTitle>
          <DialogDescription>
            从{desk.person}的积分里扣回 {reward.points}，删掉当初的到账通知并另发一条撤回通知；记录与积分流水都会留档。撤回后可以按正确的数额重新发。
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Textarea
            autoFocus
            rows={3}
            maxLength={200}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder={`撤回原因，选填，${desk.person}可见`}
            aria-label="撤回原因"
          />
          <label className="flex cursor-pointer items-start gap-2 text-sm">
            <Checkbox checked={allowPartial} onCheckedChange={(checked) => setAllowPartial(checked === true)} className="mt-0.5" />
            <span>
              余额不足时扣到 0
              <span className="block text-xs text-muted-foreground">不勾选时，{desk.person}积分不够就整笔不撤回</span>
            </span>
          </label>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="destructive" disabled={actions.rewarding} onClick={() => void submit()}>
            撤回并扣回
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
