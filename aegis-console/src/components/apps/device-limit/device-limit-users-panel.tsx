"use client";

import { useState } from "react";
import {
  Crown,
  Eraser,
  Gem,
  History,
  LogOut,
  MonitorSmartphone,
  ShieldPlus,
  Sparkles,
  Trash2,
  UserSearch,
  Wand2
} from "lucide-react";
import { toast } from "sonner";
import { DeviceLimitOverrideDialog } from "@/components/apps/device-limit/device-limit-override-dialog";
import {
  ActionBadge,
  Block,
  CLASS_META,
  ClassGlyph,
  ClassIcon,
  classOf,
  DEVICE_CLASSES,
  DeviceSlots,
  formatCount,
  limitText,
  SOURCE_META
} from "@/components/apps/device-limit/device-limit-shared";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { DeviceInfoChip } from "@/components/users/device-info-chip";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError } from "@/lib/api-client";
import type { DeviceLimitUserView, LoginDevice } from "@/lib/api/device-limit";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import {
  useDeviceLimitUserQuery,
  useEnforceDeviceLimitMutation,
  useRemoveDeviceLimitDevicesMutation,
  useResetDeviceLimitLedgerMutation
} from "@/lib/device-limit-hooks";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { cn } from "@/lib/utils";

type Confirm =
  | { kind: "remove"; keys: string[]; label: string }
  | { kind: "enforce" }
  | { kind: "ledger"; key?: string; label?: string };

/**
 * 用户设备：某个人此刻在哪几台设备上登录、上限出自哪一层（默认 / 会员档位 / 单人例外）、
 * 登录过哪些设备、最近被怎么处置过。下线、按策略整理、例外与清台账都在这一屏完成。
 */
export function DeviceLimitUsersPanel({
  appKey,
  user,
  onUserChange
}: {
  appKey: string;
  user: PickedUser | null;
  onUserChange: (user: PickedUser | null) => void;
}) {
  const can = useAppKeyPermissionChecker(appKey);
  const canWrite = can("app:user:device:manage");
  const query = useDeviceLimitUserQuery(appKey, user?.id);
  const view = query.data;
  const [selected, setSelected] = useState<string[]>([]);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [overrideOpen, setOverrideOpen] = useState(false);
  const removeMutation = useRemoveDeviceLimitDevicesMutation(appKey);
  const enforceMutation = useEnforceDeviceLimitMutation(appKey);
  const ledgerMutation = useResetDeviceLimitLedgerMutation(appKey);

  const pickUser = (next: PickedUser | null) => {
    setSelected([]);
    onUserChange(next);
  };

  async function runConfirm() {
    if (!confirm || !user) return;
    try {
      if (confirm.kind === "remove") {
        const result = await removeMutation.mutateAsync({ userId: user.id, deviceKeys: confirm.keys });
        toast.success(`已下线 ${result.removed} 台设备`);
        setSelected([]);
      } else if (confirm.kind === "enforce") {
        const result = await enforceMutation.mutateAsync(user.id);
        toast.success(result.removed ? `已下线 ${result.removed} 台设备` : "未超出上限");
      } else {
        const result = await ledgerMutation.mutateAsync({ userId: user.id, key: confirm.key });
        toast.success(`已清除 ${result.removed} 条`);
      }
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    } finally {
      setConfirm(null);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="w-full sm:w-80">
          <UserPicker appKey={appKey} value={user} onChange={pickUser} />
        </div>
        {view && canWrite ? (
          <div className="ml-auto flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={() => setOverrideOpen(true)}>
              <ShieldPlus className="size-3.5" />
              {view.override ? "编辑例外" : "设置例外"}
            </Button>
            <Button size="sm" variant="outline" disabled={!view.status.enabled} onClick={() => setConfirm({ kind: "enforce" })}>
              <Wand2 className="size-3.5" /> 按策略整理
            </Button>
            <Button size="sm" variant="outline" disabled={!view.ledger.length} onClick={() => setConfirm({ kind: "ledger" })}>
              <Eraser className="size-3.5" /> 清空台账
            </Button>
          </div>
        ) : null}
      </div>

      {!user ? (
        <div className="flex flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-border py-20 text-center">
          <UserSearch className="size-6 text-muted-foreground/50" />
          <span className="text-sm text-muted-foreground">选择用户</span>
        </div>
      ) : query.isLoading ? (
        <div className="space-y-4">
          <Skeleton className="h-28 w-full rounded-2xl" />
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-40 w-full rounded-xl" />
            ))}
          </div>
        </div>
      ) : !view ? (
        <p className="py-16 text-center text-sm text-muted-foreground">{query.error instanceof ApiError ? query.error.message : "加载失败"}</p>
      ) : (
        <>
          <IdentityStrip view={view} />

          <Block
            title={
              <span className="flex items-center gap-2">
                <MonitorSmartphone className="size-4 text-muted-foreground" />
                在线设备
                <Badge variant="secondary" size="sm" className="tabular-nums">
                  {view.status.devices.length}
                </Badge>
              </span>
            }
            aside={
              canWrite && selected.length ? (
                <Button
                  size="sm"
                  variant="destructive"
                  className="h-7"
                  onClick={() => setConfirm({ kind: "remove", keys: selected, label: `${selected.length} 台设备` })}
                >
                  <LogOut className="size-3.5" /> 下线 {selected.length} 台
                </Button>
              ) : null
            }
          >
            {view.status.devices.length === 0 ? (
              <p className="py-8 text-center text-xs text-muted-foreground">暂无在线设备</p>
            ) : (
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                {view.status.devices.map((device) => (
                  <DeviceCard
                    key={device.key}
                    device={device}
                    canWrite={canWrite}
                    selected={selected.includes(device.key)}
                    onSelect={(value) =>
                      setSelected((current) => (value ? [...current, device.key] : current.filter((key) => key !== device.key)))
                    }
                    onRemove={() =>
                      setConfirm({ kind: "remove", keys: [device.key], label: device.deviceInfo?.name || device.name || "该设备" })
                    }
                  />
                ))}
              </div>
            )}
          </Block>

          <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
            <Block
              title={
                <span className="flex items-center gap-2">
                  <History className="size-4 text-muted-foreground" />
                  登录过的设备
                </span>
              }
              bodyClassName="p-0"
            >
              {view.ledger.length === 0 ? (
                <p className="py-8 text-center text-xs text-muted-foreground">暂无</p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>设备</TableHead>
                      <TableHead>首次</TableHead>
                      <TableHead>最近</TableHead>
                      <TableHead className="text-right">登录</TableHead>
                      {canWrite ? <TableHead className="w-10" /> : null}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {view.ledger.map((entry) => (
                      <TableRow key={entry.key}>
                        <TableCell className="max-w-56">
                          <div className="flex items-center gap-2">
                            <span
                              className={cn("size-1.5 shrink-0 rounded-full", entry.online ? "bg-emerald-500" : "bg-muted-foreground/30")}
                              title={entry.online ? "在线" : "离线"}
                            />
                            <DeviceInfoChip info={entry.deviceInfo} fallback={entry.name || entry.key} />
                          </div>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground" title={formatDateTime(entry.firstSeenAt)}>
                          {formatRelative(entry.firstSeenAt)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground" title={formatDateTime(entry.lastSeenAt)}>
                          {formatRelative(entry.lastSeenAt)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">{formatCount(entry.loginCount)}</TableCell>
                        {canWrite ? (
                          <TableCell>
                            <Button
                              size="icon-xs"
                              variant="ghost"
                              aria-label="移出台账"
                              onClick={() =>
                                setConfirm({ kind: "ledger", key: entry.key, label: entry.deviceInfo?.name || entry.name || entry.key })
                              }
                            >
                              <Trash2 className="size-3" />
                            </Button>
                          </TableCell>
                        ) : null}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </Block>

            <Block title="最近处置" bodyClassName="p-0">
              {view.events.length === 0 ? (
                <p className="py-8 text-center text-xs text-muted-foreground">暂无</p>
              ) : (
                <ol className="relative space-y-0 px-4 py-3">
                  {view.events.map((event, index) => (
                    <li key={event.id} className="relative flex gap-3 pb-4 last:pb-0">
                      {index < view.events.length - 1 ? (
                        <span className="absolute left-[5px] top-4 h-[calc(100%-8px)] w-px bg-border" aria-hidden />
                      ) : null}
                      <span className="mt-1.5 size-[11px] shrink-0 rounded-full border-2 border-background bg-muted-foreground/40 ring-1 ring-border" />
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <ActionBadge action={event.action} />
                          <span className="text-[11px] text-muted-foreground" title={formatDateTime(event.createdAt)}>
                            {formatRelative(event.createdAt)}
                          </span>
                        </div>
                        <div className="truncate text-xs">
                          {event.deviceName || "未知设备"}
                          {event.triggerName ? <span className="text-muted-foreground">，因 {event.triggerName} 登录</span> : null}
                        </div>
                      </div>
                    </li>
                  ))}
                </ol>
              )}
            </Block>
          </div>

          <DeviceLimitOverrideDialog
            appKey={appKey}
            open={overrideOpen}
            onOpenChange={setOverrideOpen}
            user={user}
            override={view.override}
          />
        </>
      )}

      <AlertDialog open={Boolean(confirm)} onOpenChange={(open) => !open && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm?.kind === "remove"
                ? `下线${confirm.label}？`
                : confirm?.kind === "enforce"
                  ? "按当前策略下线超出上限的设备？"
                  : confirm?.key
                    ? `从台账移除 ${confirm.label}？`
                    : "清空该用户的设备台账？"}
            </AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={runConfirm}>确认</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 身份栏：谁、什么会员、上限出自哪一层，以及各维度占用 */
function IdentityStrip({ view }: { view: DeviceLimitUserView }) {
  const { status, member } = view;
  const limits = status.limits;
  const source = SOURCE_META[limits.source] ?? SOURCE_META.default;
  const name = view.nickname || view.account;
  const classes = DEVICE_CLASSES.filter((cls) => (limits.classLimits?.[cls] ?? 0) > 0 || (status.usage.byClass?.[cls] ?? 0) > 0);

  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
      <div className="flex flex-wrap items-center gap-4 px-5 py-4">
        <Avatar className="size-11">
          {view.avatar ? <AvatarImage src={view.avatar} alt={name} /> : null}
          <AvatarFallback>{name.slice(0, 1).toUpperCase()}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{name}</span>
            {view.nickname ? <span className="truncate text-xs text-muted-foreground">{view.account}</span> : null}
            <span className="font-mono text-[10px] text-muted-foreground">UID-{view.userId}</span>
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            {member.isLifetime ? (
              <Badge variant="warning" size="sm" className="gap-1">
                <Gem className="size-3" /> 永久会员
              </Badge>
            ) : member.isVip ? (
              <Badge variant="warning" size="sm" className="gap-1">
                {member.isTrial ? <Sparkles className="size-3" /> : <Crown className="size-3" />}
                {member.isTrial ? "试用会员" : "会员"}
              </Badge>
            ) : (
              <Badge variant="outline" size="sm">非会员</Badge>
            )}
            <Badge variant={source.variant} size="sm">
              {source.label}
            </Badge>
            <span className="text-[11px] text-muted-foreground">{limits.sourceLabel}</span>
            {view.override?.expiresAt ? (
              <span className="text-[11px] text-muted-foreground">例外到 {formatDateTime(view.override.expiresAt)}</span>
            ) : null}
          </div>
        </div>
        <div className="text-right">
          <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">同时登录</div>
          <div className="mt-1 flex items-center justify-end gap-2">
            <span className="text-lg font-semibold tabular-nums">
              {status.usage.total}
              <span className="text-sm font-normal text-muted-foreground"> / {limits.unlimited ? "不限" : limitText(limits.maxDevices)}</span>
            </span>
          </div>
          {!limits.unlimited && limits.maxDevices > 0 ? (
            <div className="mt-1 flex justify-end">
              <DeviceSlots limit={limits.maxDevices} used={status.usage.total} size="sm" />
            </div>
          ) : null}
        </div>
      </div>
      {classes.length || (limits.newDevice?.enabled && !limits.unlimited) ? (
        <div className="flex flex-wrap gap-2 border-t border-border bg-muted/30 px-5 py-2.5">
          {classes.map((cls) => {
            const used = status.usage.byClass?.[cls] ?? 0;
            const limit = limits.unlimited ? 0 : (limits.classLimits?.[cls] ?? 0);
            return (
              <span key={cls} className="inline-flex items-center gap-2 rounded-lg bg-background px-2.5 py-1 ring-1 ring-inset ring-border">
                <ClassIcon cls={cls} className="size-3.5 text-muted-foreground" />
                <span className="text-xs">{CLASS_META[cls].label}</span>
                <DeviceSlots limit={limit} used={used} cls={cls} size="sm" />
              </span>
            );
          })}
          {limits.newDevice?.enabled && !limits.unlimited ? (
            <span className="inline-flex items-center gap-1.5 rounded-lg bg-background px-2.5 py-1 text-xs ring-1 ring-inset ring-border">
              {limits.newDevice.windowDays} 天内新设备
              <DeviceSlots limit={limits.newDevice.maxNew} used={status.newDevicesInWindow} size="sm" />
            </span>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

function DeviceCard({
  device,
  canWrite,
  selected,
  onSelect,
  onRemove
}: {
  device: LoginDevice;
  canWrite: boolean;
  selected: boolean;
  onSelect: (value: boolean) => void;
  onRemove: () => void;
}) {
  const cls = classOf(device.class);
  return (
    <div
      className={cn(
        "group flex flex-col gap-3 rounded-xl border border-border bg-background p-3 transition-colors",
        selected && "border-primary/50 bg-primary/[0.03]"
      )}
    >
      <div className="flex items-start gap-2.5">
        <ClassGlyph cls={cls} />
        <div className="min-w-0 flex-1">
          <DeviceInfoChip info={device.deviceInfo} fallback={device.name || device.key} />
        </div>
        {canWrite ? <Checkbox checked={selected} onCheckedChange={(value) => onSelect(value === true)} aria-label="选择" /> : null}
      </div>
      <dl className="grid grid-cols-[3rem_minmax(0,1fr)] gap-x-2 gap-y-1 text-[11px]">
        <dt className="text-muted-foreground">登录</dt>
        <dd title={formatDateTime(device.loginAt)}>{formatRelative(device.loginAt)}</dd>
        <dt className="text-muted-foreground">活动</dt>
        <dd title={formatDateTime(device.lastActiveAt)}>{formatRelative(device.lastActiveAt)}</dd>
        {device.ip ? (
          <>
            <dt className="text-muted-foreground">IP</dt>
            <dd className="truncate font-mono">{device.ip}</dd>
          </>
        ) : null}
      </dl>
      <div className="mt-auto flex items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap gap-1">
          {device.sessions > 1 ? (
            <Badge variant="outline" size="sm" className="font-normal">
              {device.sessions} 次登录
            </Badge>
          ) : null}
          {device.provider ? (
            <Badge variant="secondary" size="sm" className="font-normal">
              {device.provider}
            </Badge>
          ) : null}
        </div>
        {canWrite ? (
          <Button size="xs" variant="ghost" className="text-destructive hover:text-destructive" onClick={onRemove}>
            <LogOut className="size-3" /> 下线
          </Button>
        ) : null}
      </div>
    </div>
  );
}
