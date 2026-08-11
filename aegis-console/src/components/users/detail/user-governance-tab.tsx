"use client";

import { useState } from "react";
import {
  Ban,
  CircleCheck,
  Gavel,
  History,
  Loader2,
  ShieldAlert,
  Trash2,
  Undo2,
  Unlock
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import { useUpdateAdminAppUserStatusMutation } from "@/lib/admin-hooks";
import {
  useAdminUserBansQuery,
  useCreateAdminUserBanMutation,
  useRevokeAdminUserBanMutation
} from "@/lib/app-user-hooks";
import type {
  AccountBanScope,
  AccountBanType,
  AdminAppUserDetail,
  AdminUserBan
} from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  BAN_SCOPE_LABEL,
  BAN_TYPE_LABEL,
  BanStatusBadge,
  EmptyRow,
  Panel,
  ValueTree,
  formatShortTime,
  formatTime,
  relativeTime,
  textValue
} from "./user-detail-shared";

/** `datetime-local` 的值是本地时间且无时区，转 RFC3339 交给后端。 */
function toRFC3339(local: string) {
  if (!local) return undefined;
  const date = new Date(local);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

/**
 * 处置：平台对这个账号做过 / 要做什么。
 *
 * 页面上把两件常被混为一谈的事**显式分开**：
 *
 *   账号开关（users.enabled + disabledReason）
 *     一个布尔位。改了立刻生效，没有起止、没有操作人、没有历史。
 *     适合"先停一下看看"这种临时动作。
 *
 *   封禁记录（app_user_bans）
 *     一条有类型（临时 / 永久）、有范围（仅登录 / 全部访问）、有起止、
 *     有操作人、有证据、可撤销、留痕的处置。到期自动失效。
 *     追责与申诉都依赖它。
 *
 * 旧版详情页只有前者，且藏在叫「状态控制」的卡片里与"重置密码/删除"并列 ——
 * 于是所有需要留痕的处置都只能靠在原因里写一句话。
 */
export function UserGovernanceTab({
  appKey,
  userId,
  user,
  activeBan,
  onDelete,
  deletePending
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  activeBan?: AdminUserBan | null;
  onDelete: () => void;
  deletePending: boolean;
}) {
  const enabled = user.enabled !== false;
  const bansQuery = useAdminUserBansQuery(appKey, userId, { limit: 20 });
  const bans = bansQuery.data?.items ?? [];

  return (
    <div className="space-y-5">
      <div className="grid gap-5 xl:grid-cols-2">
        <AccountSwitchPanel appKey={appKey} userId={userId} user={user} enabled={enabled} />
        <CreateBanPanel appKey={appKey} userId={userId} activeBan={activeBan} />
      </div>

      <Panel
        title="封禁历史"
        icon={<History className="size-4" />}
        description="到期的封禁会自动转为「已到期」，不需要人工撤销；撤销用于提前解除。"
        action={
          <Badge variant="outline" size="sm">
            共 {bansQuery.data?.total ?? 0} 条
          </Badge>
        }
        bodyClassName="p-0"
      >
        {bansQuery.isLoading ? (
          <div className="space-y-2 p-5">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-10 w-full rounded-lg" />
            ))}
          </div>
        ) : !bans.length ? (
          <EmptyRow text="这个账号从未被封禁过" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>状态</TableHead>
                  <TableHead>类型 / 范围</TableHead>
                  <TableHead>原因</TableHead>
                  <TableHead>时间区间</TableHead>
                  <TableHead>操作人</TableHead>
                  <TableHead className="w-20" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {bans.map((ban) => (
                  <BanRow key={ban.id} appKey={appKey} userId={userId} ban={ban} />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>

      <Panel
        title="危险操作"
        icon={<Trash2 className="size-4" />}
        description="删除是物理删除：资料、设置、安全凭证、会话全部清除，无法恢复。绝大多数场景应该用封禁而不是删除。"
        className="border-destructive/40"
      >
        <Button size="sm" variant="destructive" disabled={deletePending} onClick={onDelete}>
          {deletePending ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
          删除用户
        </Button>
      </Panel>
    </div>
  );
}

// ── 账号开关 ──────────────────────────

function AccountSwitchPanel({
  appKey,
  userId,
  user,
  enabled
}: {
  appKey: string;
  userId: number;
  user: AdminAppUserDetail;
  enabled: boolean;
}) {
  const mutation = useUpdateAdminAppUserStatusMutation(appKey, userId);
  const scope = `${appKey}:${userId}`;
  const [draft, setDraft] = useState<{ scope: string; reason: string } | null>(null);
  const reason = draft?.scope === scope ? draft.reason : textValue(user.disabledReason, "");

  async function toggle(next: boolean) {
    try {
      await mutation.mutateAsync({
        enabled: next,
        disabledReason: next ? undefined : reason.trim() || "控制台已手动限制",
        clearDisabledEndTime: next
      });
      setDraft(null);
      toast.success(next ? "账号已启用" : "账号已限制");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  return (
    <Panel
      title="账号开关"
      icon={enabled ? <CircleCheck className="size-4" /> : <ShieldAlert className="size-4" />}
      description="users.enabled 上的一个布尔位，改了立刻生效但不留处置记录。需要追责或申诉时请用封禁。"
      action={
        <Badge variant={enabled ? "success" : "danger"} size="sm">
          {enabled ? "正常" : "已限制"}
        </Badge>
      }
    >
      <div className="space-y-3">
        {!enabled ? (
          <div className="rounded-xl border border-red-200 bg-red-50/60 px-3 py-2.5 text-xs leading-5 dark:border-red-900/60 dark:bg-red-950/30">
            <div className="font-medium text-red-700 dark:text-red-300">当前处于限制状态</div>
            <div className="mt-0.5 text-muted-foreground">
              {textValue(user.disabledReason, "未填写原因")}
              {user.disabledEndTime ? ` · 解除于 ${formatTime(user.disabledEndTime)}` : ""}
            </div>
          </div>
        ) : null}

        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">限制原因</Label>
          <Textarea
            rows={3}
            value={reason}
            placeholder="控制台已手动限制"
            onChange={(event) => setDraft({ scope, reason: event.target.value })}
          />
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={mutation.isPending || enabled}
            onClick={() => void toggle(true)}
          >
            <Unlock className="size-3.5" />
            启用账号
          </Button>
          <Button
            size="sm"
            variant="destructive"
            disabled={mutation.isPending || !enabled}
            onClick={() => void toggle(false)}
          >
            {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Ban className="size-3.5" />}
            限制账号
          </Button>
        </div>
      </div>
    </Panel>
  );
}

// ── 新建封禁 ──────────────────────────

const BAN_TYPES: Array<{ value: AccountBanType; title: string; detail: string }> = [
  { value: "temporary", title: "临时", detail: "到期自动解除" },
  { value: "permanent", title: "永久", detail: "只能人工撤销" }
];

const BAN_SCOPES: Array<{ value: AccountBanScope; title: string; detail: string }> = [
  { value: "login", title: "仅登录", detail: "已签发的会话仍然有效" },
  { value: "all", title: "全部访问", detail: "同时踢掉现有会话" }
];

function CreateBanPanel({
  appKey,
  userId,
  activeBan
}: {
  appKey: string;
  userId: number;
  activeBan?: AdminUserBan | null;
}) {
  const mutation = useCreateAdminUserBanMutation(appKey, userId);
  const [banType, setBanType] = useState<AccountBanType>("temporary");
  const [banScope, setBanScope] = useState<AccountBanScope>("login");
  const [reason, setReason] = useState("");
  const [endAt, setEndAt] = useState("");

  const needsEnd = banType === "temporary";
  const canSubmit = reason.trim().length > 0 && (!needsEnd || Boolean(endAt));

  async function handleSubmit() {
    if (!canSubmit) {
      toast.error(needsEnd && !endAt ? "临时封禁必须指定到期时间" : "请填写封禁原因");
      return;
    }
    try {
      await mutation.mutateAsync({
        banType,
        banScope,
        reason: reason.trim(),
        endAt: needsEnd ? toRFC3339(endAt) : null
      });
      setReason("");
      setEndAt("");
      toast.success("封禁已生效");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "封禁失败");
    }
  }

  return (
    <Panel
      title="新建封禁"
      icon={<Gavel className="size-4" />}
      description="留痕的处置记录：有类型、有范围、有起止、有操作人，可撤销、可申诉。"
      action={
        activeBan ? (
          <Badge variant="danger" size="sm">
            已有生效封禁
          </Badge>
        ) : null
      }
    >
      <div className="space-y-4">
        {activeBan ? (
          <div className="rounded-xl border border-red-200 bg-red-50/60 px-3 py-2.5 text-xs leading-5 dark:border-red-900/60 dark:bg-red-950/30">
            <div className="font-medium text-red-700 dark:text-red-300">
              当前生效：{BAN_TYPE_LABEL[activeBan.banType] ?? activeBan.banType} ·{" "}
              {BAN_SCOPE_LABEL[activeBan.banScope] ?? activeBan.banScope}
            </div>
            <div className="mt-0.5 text-muted-foreground">
              {textValue(activeBan.reason, "未填写原因")}
              {activeBan.endAt ? ` · 到期 ${formatTime(activeBan.endAt)}` : " · 永久"}
            </div>
            <div className="mt-1 text-muted-foreground">
              再建一条会叠加，不会替换现有封禁。要解除请到下方历史里撤销。
            </div>
          </div>
        ) : null}

        <div className="grid gap-3 sm:grid-cols-2">
          <ChoiceGroup
            label="封禁类型"
            value={banType}
            options={BAN_TYPES}
            onChange={(value) => setBanType(value as AccountBanType)}
          />
          <ChoiceGroup
            label="生效范围"
            value={banScope}
            options={BAN_SCOPES}
            onChange={(value) => setBanScope(value as AccountBanScope)}
          />
        </div>

        {needsEnd ? (
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">到期时间</Label>
            <Input
              type="datetime-local"
              value={endAt}
              onChange={(event) => setEndAt(event.target.value)}
            />
          </div>
        ) : null}

        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">封禁原因（必填，会展示在申诉记录里）</Label>
          <Textarea
            rows={3}
            value={reason}
            placeholder="例如：批量注册小号，关联 12 个账号"
            onChange={(event) => setReason(event.target.value)}
          />
        </div>

        <Button
          size="sm"
          variant="destructive"
          disabled={!canSubmit || mutation.isPending}
          onClick={handleSubmit}
        >
          {mutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Gavel className="size-3.5" />}
          执行封禁
        </Button>
      </div>
    </Panel>
  );
}

function ChoiceGroup({
  label,
  value,
  options,
  onChange
}: {
  label: string;
  value: string;
  options: Array<{ value: string; title: string; detail: string }>;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <RadioGroup value={value} onValueChange={onChange} className="gap-2">
        {options.map((option) => (
          <label
            key={option.value}
            className={cn(
              "flex cursor-pointer items-start gap-2.5 rounded-xl border px-3 py-2 transition-colors",
              value === option.value ? "border-foreground/30 bg-accent/40" : "hover:bg-accent/20"
            )}
          >
            <RadioGroupItem value={option.value} className="mt-0.5" />
            <span className="min-w-0">
              <span className="block text-sm font-medium">{option.title}</span>
              <span className="block text-[11px] text-muted-foreground">{option.detail}</span>
            </span>
          </label>
        ))}
      </RadioGroup>
    </div>
  );
}

// ── 历史行 ──────────────────────────

function BanRow({ appKey, userId, ban }: { appKey: string; userId: number; ban: AdminUserBan }) {
  const revoke = useRevokeAdminUserBanMutation(appKey, userId);
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");

  async function handleRevoke() {
    try {
      await revoke.mutateAsync({ banId: ban.id, reason: reason.trim() || undefined });
      setOpen(false);
      setReason("");
      toast.success("封禁已撤销");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "撤销失败");
    }
  }

  return (
    <>
      <TableRow>
        <TableCell>
          <BanStatusBadge status={ban.status} />
        </TableCell>
        <TableCell className="whitespace-nowrap text-xs">
          {BAN_TYPE_LABEL[ban.banType] ?? ban.banType}
          <span className="text-muted-foreground"> · {BAN_SCOPE_LABEL[ban.banScope] ?? ban.banScope}</span>
        </TableCell>
        <TableCell className="max-w-[260px]">
          <div className="truncate text-xs" title={ban.reason}>
            {textValue(ban.reason)}
          </div>
          {ban.status === "revoked" && ban.revokeReason ? (
            <div className="truncate text-[11px] text-muted-foreground">
              撤销原因：{ban.revokeReason}
            </div>
          ) : null}
          {ban.evidence && Object.keys(ban.evidence).length ? (
            <div className="mt-1 rounded-lg bg-muted/40 px-2 py-1">
              <ValueTree value={ban.evidence} />
            </div>
          ) : null}
        </TableCell>
        <TableCell className="whitespace-nowrap text-[11px] tabular-nums text-muted-foreground">
          <div title={formatTime(ban.startAt)}>{formatShortTime(ban.startAt)} 起</div>
          <div title={ban.endAt ? formatTime(ban.endAt) : undefined}>
            {ban.endAt ? `${formatShortTime(ban.endAt)} 止` : "永久"}
            {ban.status === "active" && ban.endAt ? ` · ${relativeTime(ban.endAt)}` : ""}
          </div>
        </TableCell>
        <TableCell className="text-xs text-muted-foreground">
          <div>{textValue(ban.bannedByAdminName)}</div>
          {ban.revokedByAdminName ? (
            <div className="text-[11px]">撤销：{ban.revokedByAdminName}</div>
          ) : null}
        </TableCell>
        <TableCell>
          {ban.status === "active" ? (
            <Button
              size="sm"
              variant="ghost"
              className="h-7 gap-1 px-2 text-xs"
              onClick={() => setOpen(true)}
            >
              <Undo2 className="size-3" />
              撤销
            </Button>
          ) : null}
        </TableCell>
      </TableRow>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="text-sm">撤销封禁</DialogTitle>
            <DialogDescription>
              撤销后该封禁立即失效。原记录会保留，状态转为「已撤销」并记下撤销人。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">撤销原因（可选）</Label>
            <Textarea
              rows={3}
              value={reason}
              placeholder="例如：申诉通过，误判"
              onChange={(event) => setReason(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button size="sm" disabled={revoke.isPending} onClick={handleRevoke}>
              {revoke.isPending ? <Loader2 className="mr-1 size-3 animate-spin" /> : null}
              确认撤销
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
