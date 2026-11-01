"use client";

import { useMemo, useState } from "react";
import { Ban, LockKeyhole, Network, ShieldCheck, Smartphone, Timer, Undo2, Users } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import {
  Block,
  DecisionBanner,
  DeviceQuality,
  DeviceText,
  durationLabel,
  Fact,
  formatCount,
  IPText,
  KindBadge,
  LinkedList,
  ListKindBadge,
  methodLabel,
  QuotaMeter,
  reasonLabel,
  ScopeGlyph,
  unitOf,
  windowLabel
} from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { RegIPDeviceInspection } from "@/lib/api/register-ip";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useAddRegDeviceListsMutation, useRegDeviceInspectQuery, useReleaseRegIPMutation } from "@/lib/register-ip-hooks";
import { cn } from "@/lib/utils";

/**
 * 设备查询：一台设备此刻的全部状况。判定按「此刻这台设备、不带地址、用密码注册」推演（只数不占），
 * 所以这里只看设备一侧的规则；地址一侧在 IP 查询里看。「用过的地址」点一下跳到 IP 查询。
 */
export function RegDeviceInspectPanel({
  appKey,
  device,
  onDeviceChange,
  onOpenIP
}: {
  appKey: string;
  device: string;
  onDeviceChange: (device: string) => void;
  onOpenIP: (ip: string) => void;
}) {
  const [text, setText] = useState(device);
  const [seen, setSeen] = useState(device);
  if (device !== seen) {
    setSeen(device);
    setText(device);
  }
  const query = useRegDeviceInspectQuery(appKey, device);
  const [releasing, setReleasing] = useState(false);
  const data = query.data;

  return (
    <div className="space-y-4">
      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          onDeviceChange(text.trim());
        }}
      >
        <div className="relative min-w-64 flex-1 sm:max-w-lg">
          <Smartphone className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-fuchsia-500" />
          <Input value={text} onChange={(event) => setText(event.target.value)} placeholder="输入设备标识（X-Device-Id）" className="h-9 pl-8 font-mono text-sm" />
        </div>
        <Button type="submit" size="sm" disabled={!text.trim()}>
          查询
        </Button>
        {data?.device.valid ? (
          <Button type="button" size="sm" variant="outline" onClick={() => setReleasing(true)}>
            <Undo2 className="size-3.5" /> 解除限制
          </Button>
        ) : null}
      </form>

      {!device ? (
        <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed border-border py-16 text-center">
          <Smartphone className="size-6 text-muted-foreground/50" />
          <span className="text-sm text-muted-foreground">输入一个设备标识，查看它注册过的账号、用过的地址、名单与惩罚期</span>
          <span className="text-[11px] text-muted-foreground">在概览、IP 查询、记录与台账里点设备也会跳到这里</span>
        </div>
      ) : query.isLoading ? (
        <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <Skeleton className="h-80 w-full rounded-xl" />
          <Skeleton className="h-80 w-full rounded-xl" />
        </div>
      ) : query.error ? (
        <p className="rounded-xl border border-rose-500/30 bg-rose-500/[0.06] px-4 py-3 text-sm text-rose-700 dark:text-rose-300">
          {query.error instanceof ApiError ? query.error.message : "查询失败"}
        </p>
      ) : data ? (
        <DeviceResult appKey={appKey} data={data} onOpenIP={onOpenIP} />
      ) : null}

      {data?.device.valid ? <DeviceReleaseDialog appKey={appKey} data={data} open={releasing} onOpenChange={setReleasing} /> : null}
    </div>
  );
}

function DeviceResult({ appKey, data, onOpenIP }: { appKey: string; data: RegIPDeviceInspection; onOpenIP: (ip: string) => void }) {
  const [now] = useState(() => Date.now());
  const addList = useAddRegDeviceListsMutation(appKey);
  const live = useMemo(() => new Set(data.recent.map((item) => item.key.split("/")[0])), [data.recent]);
  const quickList = async (kind: "allow" | "deny") => {
    try {
      await addList.mutateAsync({ values: [data.device.id ?? ""], kind, note: "设备查询中添加" });
      toast.success(kind === "allow" ? "已加入设备放行名单" : "已加入设备拒绝名单");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "添加失败");
    }
  };
  const listExpired = data.list?.expiresAt ? new Date(data.list.expiresAt).getTime() < now : false;

  return (
    <div className="space-y-4">
      {!data.device.valid ? (
        <p className="rounded-xl border border-amber-500/30 bg-amber-500/[0.06] px-4 py-3 text-xs text-amber-800 dark:text-amber-200">
          这个标识无效（{data.device.invalidReason}），不参与任何设备规则；按策略「无效设备」的处置判定。下面的台账与记录仍按原样查找。
        </p>
      ) : null}
      <DecisionBanner decision={data.decision} />
      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <div className="space-y-4">
          <Block title="设备规则用量" aside={<span className="text-[11px] text-muted-foreground">地址一侧的规则在 IP 查询里看</span>}>
            {!data.usage.length ? (
              <p className="py-4 text-center text-xs text-muted-foreground">没有生效的设备规则</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.usage.map((usage) => (
                  <li key={usage.ruleId} className={cn("flex items-center gap-3 py-2.5", usage.skipped && "opacity-50")}>
                    <ScopeGlyph scope={usage.scope} className="size-7" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium">{usage.name}</div>
                      <div className="truncate text-[10px] text-muted-foreground">
                        {usage.skipped ? usage.skipReason : windowLabel(usage.windowSeconds)}
                      </div>
                    </div>
                    {usage.skipped ? null : (
                      <div className="w-48 shrink-0 space-y-0.5">
                        <QuotaMeter count={usage.count} limit={usage.limit} action={usage.action} />
                        <div className="text-right text-[10px] text-muted-foreground">
                          {usage.remaining > 0 ? `还能 ${usage.remaining} ${unitOf(usage.scope)}` : "已满"}
                          {usage.resetAt && usage.remaining === 0 ? `，${formatRelative(usage.resetAt)}腾出名额` : ""}
                        </div>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </Block>

          <Block
            title={
              <span className="flex items-center gap-1.5">
                <Network className="size-4 text-muted-foreground" />
                用过的地址（{formatCount(data.addressesTotal)}）
              </span>
            }
            aside={live.size ? <Badge variant="info" size="sm">窗口内 {live.size} 个</Badge> : null}
            bodyClassName="p-0"
          >
            <LinkedList items={data.addresses} kind="ip" onOpen={onOpenIP} live={live} />
          </Block>

          <Block
            title={
              <span className="flex items-center gap-1.5">
                <Users className="size-4 text-muted-foreground" />
                这台设备注册的账号（计入上限 {formatCount(data.active)}）
              </span>
            }
            bodyClassName="p-0"
          >
            {!data.registrations.length ? (
              <p className="px-4 py-6 text-center text-xs text-muted-foreground">没有注册记录</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.registrations.map((item) => (
                  <li key={item.id} className={cn("flex items-center gap-3 px-4 py-2", item.releasedAt && "opacity-60")}>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium">{item.account || (item.userId ? `UID-${item.userId}` : "已删除的账号")}</div>
                      <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                        <IPText value={item.ip} className="text-[10px]" onClick={() => onOpenIP(item.ip)} />
                        <span>· {methodLabel(item.method)}</span>
                      </div>
                    </div>
                    {item.releasedAt ? <Badge variant="outline" size="sm">已解除</Badge> : null}
                    <span className="text-[11px] text-muted-foreground" title={formatDateTime(item.createdAt)}>
                      {formatRelative(item.createdAt)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Block>
        </div>

        <div className="space-y-4">
          <Block title="设备">
            <Fact label="标识">
              <DeviceText value={data.device.id} />
            </Fact>
            <Fact label="质量">
              <DeviceQuality device={data.device} />
            </Fact>
            {data.device.raw && data.device.raw !== data.device.id ? (
              <Fact label="原样">
                <span className="font-mono">{data.device.raw}</span>
              </Fact>
            ) : null}
          </Block>

          <Block
            title="惩罚期"
            aside={
              data.penalty.remaining > 0 ? (
                <Badge variant="danger" size="sm" className="gap-1">
                  <LockKeyhole className="size-3" /> 惩罚中
                </Badge>
              ) : (
                <Badge variant="outline" size="sm">无</Badge>
              )
            }
          >
            <Fact label="剩余">{data.penalty.remaining > 0 ? durationLabel(data.penalty.remaining) : "—"}</Fact>
            <Fact label="累计等级">{data.penalty.level ? `第 ${data.penalty.level} 级` : "—"}</Fact>
            {data.penalty.expiresAt ? <Fact label="到期">{formatDateTime(data.penalty.expiresAt)}</Fact> : null}
          </Block>

          <Block
            title="设备名单"
            aside={
              data.device.valid ? (
                <div className="flex gap-1">
                  <Button size="xs" variant="outline" disabled={addList.isPending} onClick={() => quickList("allow")}>
                    <ShieldCheck /> 放行
                  </Button>
                  <Button size="xs" variant="outline" disabled={addList.isPending} onClick={() => quickList("deny")}>
                    <Ban /> 拒绝
                  </Button>
                </div>
              ) : null
            }
          >
            {data.list ? (
              <div className="flex items-center gap-2 text-xs">
                <ListKindBadge kind={data.list.kind} factor={data.list.factor} />
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{data.list.note || "—"}</span>
                {listExpired ? <Badge variant="outline" size="sm">已过期</Badge> : null}
              </div>
            ) : (
              <p className="py-1 text-center text-xs text-muted-foreground">不在设备名单里</p>
            )}
            {data.match && data.match.target === "ip" ? (
              <p className="mt-2 text-[11px] text-muted-foreground">另命中地址名单 {data.match.cidr}</p>
            ) : null}
          </Block>

          <Block title="最近拦截" bodyClassName="p-0">
            {!data.events.length ? (
              <p className="px-4 py-6 text-center text-xs text-muted-foreground">没有记录</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.events.slice(0, 12).map((event) => (
                  <li key={event.id} className="space-y-0.5 px-4 py-2">
                    <div className="flex items-center gap-2">
                      <KindBadge kind={event.kind} />
                      <span className="text-[11px] text-muted-foreground">{reasonLabel(event.reason)}</span>
                      <span className="ml-auto text-[10px] text-muted-foreground">{formatRelative(event.createdAt)}</span>
                    </div>
                    <div className="truncate text-[11px] text-muted-foreground">
                      {event.ip ? (
                        <button type="button" className="font-mono hover:underline" onClick={() => onOpenIP(event.ip)}>
                          {event.ip}
                        </button>
                      ) : null}
                      {event.account ? ` · ${event.account}` : ""} {event.detail ? `· ${event.detail}` : ""}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </Block>
        </div>
      </div>
    </div>
  );
}

function DeviceReleaseDialog({
  appKey,
  data,
  open,
  onOpenChange
}: {
  appKey: string;
  data: RegIPDeviceInspection;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const mutation = useReleaseRegIPMutation(appKey);
  const [counters, setCounters] = useState(true);
  const [penalty, setPenalty] = useState(true);
  const [resetLevel, setResetLevel] = useState(false);
  const [ledger, setLedger] = useState(false);
  const [note, setNote] = useState("");

  async function submit() {
    try {
      const result = await mutation.mutateAsync({ device: data.device.id, scope: "ip", counters, penalty, resetLevel, ledger, note: note.trim() });
      toast.success(
        [
          result.countersCleared ? `清空 ${result.countersCleared} 项计数` : "",
          result.penaltyCleared ? "解除惩罚期" : "",
          result.ledgerReleased ? `${result.ledgerReleased} 条台账不再计入` : ""
        ]
          .filter(Boolean)
          .join("，") || "已解除"
      );
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "解除失败");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>解除设备注册限制</DialogTitle>
          <DialogDescription>
            <span className="break-all font-mono">{data.device.id}</span>，已注册的账号不受影响，操作记入拦截记录
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <SwitchRow icon={<Timer className="size-3.5" />} label="清空设备规则的计数" hint="同一设备、设备换地址都从零算起" checked={counters} onChange={setCounters} />
          <SwitchRow icon={<LockKeyhole className="size-3.5" />} label="解除设备惩罚期" checked={penalty} onChange={setPenalty} />
          {penalty ? <SwitchRow label="同时清空惩罚等级" hint="下次再犯从首次时长算起" checked={resetLevel} onChange={setResetLevel} /> : null}
          <SwitchRow
            icon={<Users className="size-3.5" />}
            label="台账不再计入设备累计上限"
            hint={`目前计入 ${data.active} 个`}
            checked={ledger}
            onChange={setLedger}
          />
          <Input value={note} onChange={(event) => setNote(event.target.value)} maxLength={100} placeholder="备注（可选），例如：用户换机后申诉" className="text-xs" />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button disabled={mutation.isPending || (!counters && !penalty && !ledger)} onClick={submit}>
            {mutation.isPending ? "解除中..." : "解除"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
