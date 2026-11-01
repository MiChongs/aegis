"use client";

import { useMemo, useState } from "react";
import { Ban, Boxes, LockKeyhole, MapPin, SearchCode, ShieldCheck, Timer, Undo2, Users } from "lucide-react";
import { toast } from "sonner";
import { SwitchRow } from "@/components/apps/app-config-primitives";
import {
  Block,
  DecisionBanner,
  DeviceText,
  durationLabel,
  Fact,
  formatCount,
  IPText,
  KindBadge,
  LinkedList,
  ListKindBadge,
  locationText,
  methodLabel,
  QuotaMeter,
  reasonLabel,
  ScopeGlyph,
  windowLabel
} from "@/components/apps/register-ip/register-ip-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import type { RegIPInspection, RegIPRegistration } from "@/lib/api/register-ip";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useAddRegIPListsMutation, useRegIPInspectQuery, useReleaseRegIPMutation } from "@/lib/register-ip-hooks";
import { cn } from "@/lib/utils";

/**
 * IP 查询：一个地址此刻的全部状况。判定按「此刻用密码注册」推演（只数不占），计数是真实值。
 * 从概览、名单、记录里点地址都会跳到这里。
 */
export function RegIPInspectPanel({
  appKey,
  ip,
  onIPChange,
  onOpenDevice
}: {
  appKey: string;
  ip: string;
  onIPChange: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  const [text, setText] = useState(ip);
  const [seenIP, setSeenIP] = useState(ip);
  if (ip !== seenIP) {
    // 外部跳转进来换了地址：输入框跟上（渲染期间同步派生状态，免得 effect 级联渲染）
    setSeenIP(ip);
    setText(ip);
  }
  const query = useRegIPInspectQuery(appKey, ip);
  const [releasing, setReleasing] = useState(false);
  const data = query.data;

  return (
    <div className="space-y-4">
      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          onIPChange(text.trim());
        }}
      >
        <div className="relative min-w-64 flex-1 sm:max-w-md">
          <SearchCode className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={text} onChange={(event) => setText(event.target.value)} placeholder="输入 IPv4 或 IPv6 地址" className="h-9 pl-8 font-mono text-sm" />
        </div>
        <Button type="submit" size="sm" disabled={!text.trim()}>
          查询
        </Button>
        {data ? (
          <Button type="button" size="sm" variant="outline" onClick={() => setReleasing(true)}>
            <Undo2 className="size-3.5" /> 解除限制
          </Button>
        ) : null}
      </form>

      {!ip ? (
        <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed border-border py-16 text-center">
          <SearchCode className="size-6 text-muted-foreground/50" />
          <span className="text-sm text-muted-foreground">输入一个地址，查看它的名单、惩罚期、配额用量与注册过的账号</span>
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
        <InspectResult appKey={appKey} data={data} onOpenIP={onIPChange} onOpenDevice={onOpenDevice} />
      ) : null}

      {data ? <ReleaseDialog appKey={appKey} data={data} open={releasing} onOpenChange={setReleasing} /> : null}
    </div>
  );
}

function InspectResult({
  appKey,
  data,
  onOpenIP,
  onOpenDevice
}: {
  appKey: string;
  data: RegIPInspection;
  onOpenIP: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  const [now] = useState(() => Date.now());
  const live = useMemo(() => new Set(data.recent.map((item) => item.key)), [data.recent]);
  const addList = useAddRegIPListsMutation(appKey);
  const quickList = async (kind: "allow" | "deny") => {
    try {
      await addList.mutateAsync({ values: [data.address.ip], kind, note: "IP 查询中添加" });
      toast.success(kind === "allow" ? "已加入放行名单" : "已加入拒绝名单");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "添加失败");
    }
  };
  return (
    <div className="space-y-4">
      <DecisionBanner decision={data.decision} />
      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <div className="space-y-4">
          <Block title="配额用量" aside={<span className="text-[11px] text-muted-foreground">此刻再注册一个会怎样</span>}>
            {!data.usage.length ? (
              <p className="py-4 text-center text-xs text-muted-foreground">没有生效的配额规则</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.usage.map((usage) => (
                  <li key={usage.ruleId} className={cn("flex items-center gap-3 py-2.5", usage.skipped && "opacity-50")}>
                    <ScopeGlyph scope={usage.scope} className="size-7" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium">{usage.name}</div>
                      <div className="truncate font-mono text-[10px] text-muted-foreground">
                        {usage.skipped ? usage.skipReason : `${usage.subject} · ${windowLabel(usage.windowSeconds)}`}
                      </div>
                    </div>
                    {usage.skipped ? null : (
                      <div className="w-48 shrink-0 space-y-0.5">
                        <QuotaMeter count={usage.count} limit={usage.limit} action={usage.action} />
                        <div className="text-right text-[10px] text-muted-foreground">
                          {usage.remaining > 0 ? `还能注册 ${usage.remaining} 个` : "已满"}
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
                <Boxes className="size-4 text-muted-foreground" />
                地址下的设备（{formatCount(data.devicesTotal)}）
              </span>
            }
            aside={live.size ? <Badge variant="info" size="sm">窗口内 {live.size} 台</Badge> : null}
            bodyClassName="p-0"
          >
            <LinkedList items={data.devices} kind="device" onOpen={onOpenDevice} live={live} />
          </Block>

          <RegistrationsBlock
            title={`该地址注册的账号（${formatCount(data.hostRegistrations)}）`}
            items={data.registrations}
            onOpenIP={onOpenIP}
            onOpenDevice={onOpenDevice}
          />
          <RegistrationsBlock
            title={`同网段 ${data.address.subnet} 的注册（${formatCount(data.subnetRegistrations)}）`}
            items={data.subnetRecent}
            onOpenIP={onOpenIP}
            onOpenDevice={onOpenDevice}
          />
        </div>

        <div className="space-y-4">
          <Block title="地址">
            <Fact label="地址">
              <IPText value={data.address.ip} />
            </Fact>
            <Fact label="按单个地址计">
              <span className="font-mono">{data.address.host}</span>
            </Fact>
            <Fact label="按网段计">
              <span className="font-mono">{data.address.subnet}</span>
            </Fact>
            <Fact label="类型">
              IPv{data.address.family}
              {data.address.private ? <Badge variant="outline" size="sm" className="ml-1.5">内网</Badge> : null}
            </Fact>
            <Fact label="属地">
              <span className="inline-flex items-center gap-1">
                <MapPin className="size-3 text-muted-foreground" />
                {data.location.resolved ? locationText(data.location) : "查不到"}
              </span>
            </Fact>
            <Fact label="运营商">{data.location.isp || "—"}</Fact>
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
            <Fact label="主体">
              <span className="font-mono">{data.penalty.subject || "—"}</span>
            </Fact>
            <Fact label="剩余">{data.penalty.remaining > 0 ? durationLabel(data.penalty.remaining) : "—"}</Fact>
            <Fact label="累计等级">{data.penalty.level ? `第 ${data.penalty.level} 级` : "—"}</Fact>
            {data.penalty.expiresAt ? <Fact label="到期">{formatDateTime(data.penalty.expiresAt)}</Fact> : null}
          </Block>

          <Block
            title="名单"
            aside={
              <div className="flex gap-1">
                <Button size="xs" variant="outline" disabled={addList.isPending} onClick={() => quickList("allow")}>
                  <ShieldCheck /> 放行
                </Button>
                <Button size="xs" variant="outline" disabled={addList.isPending} onClick={() => quickList("deny")}>
                  <Ban /> 拒绝
                </Button>
              </div>
            }
          >
            {!data.lists.length ? (
              <p className="py-2 text-center text-xs text-muted-foreground">不在任何名单里</p>
            ) : (
              <ul className="space-y-2">
                {data.lists.map((entry) => (
                  <li key={entry.id} className="flex items-center gap-2 text-xs">
                    <ListKindBadge kind={entry.kind} factor={entry.factor} />
                    <span className="font-mono">{entry.cidr}</span>
                    <span className="min-w-0 flex-1 truncate text-muted-foreground">{entry.note}</span>
                    {entry.expiresAt && new Date(entry.expiresAt).getTime() < now ? <Badge variant="outline" size="sm">已过期</Badge> : null}
                  </li>
                ))}
              </ul>
            )}
          </Block>

          <Block title="最近拦截（同网段）" bodyClassName="p-0">
            {!data.events.length ? (
              <p className="px-4 py-6 text-center text-xs text-muted-foreground">没有记录</p>
            ) : (
              <ul className="divide-y divide-border">
                {data.events.slice(0, 10).map((event) => (
                  <li key={event.id} className="space-y-0.5 px-4 py-2">
                    <div className="flex items-center gap-2">
                      <KindBadge kind={event.kind} />
                      <span className="text-[11px] text-muted-foreground">{reasonLabel(event.reason)}</span>
                      <span className="ml-auto text-[10px] text-muted-foreground">{formatRelative(event.createdAt)}</span>
                    </div>
                    <div className="truncate text-[11px] text-muted-foreground">
                      <span className="font-mono">{event.ip}</span> {event.account ? `· ${event.account}` : ""} {event.detail ? `· ${event.detail}` : ""}
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

function RegistrationsBlock({
  title,
  items,
  onOpenIP,
  onOpenDevice
}: {
  title: string;
  items: RegIPRegistration[];
  onOpenIP: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  return (
    <Block
      title={
        <span className="flex items-center gap-1.5">
          <Users className="size-4 text-muted-foreground" />
          {title}
        </span>
      }
      bodyClassName="p-0"
    >
      {!items.length ? (
        <p className="px-4 py-6 text-center text-xs text-muted-foreground">没有注册记录</p>
      ) : (
        <ul className="divide-y divide-border">
          {items.map((item) => (
            <li key={item.id} className={cn("flex items-center gap-3 px-4 py-2", item.releasedAt && "opacity-60")}>
              <div className="min-w-0 flex-1">
                <div className="truncate text-xs font-medium">{item.account || (item.userId ? `UID-${item.userId}` : "已删除的账号")}</div>
                <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                  <IPText value={item.ip} className="text-[10px]" onClick={() => onOpenIP(item.ip)} />
                  <span>· {methodLabel(item.method)}</span>
                  {item.deviceId ? <DeviceText value={item.deviceId} className="min-w-0 text-[10px]" onClick={() => onOpenDevice(item.deviceId)} /> : null}
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
  );
}

/** 解除限制：窗口计数、惩罚期（可连等级一起清）、台账（不再计入累计上限，账号不受影响） */
function ReleaseDialog({
  appKey,
  data,
  open,
  onOpenChange
}: {
  appKey: string;
  data: RegIPInspection;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const mutation = useReleaseRegIPMutation(appKey);
  const [scope, setScope] = useState<"ip" | "subnet">("ip");
  const [counters, setCounters] = useState(true);
  const [penalty, setPenalty] = useState(true);
  const [resetLevel, setResetLevel] = useState(false);
  const [ledger, setLedger] = useState(false);
  const [note, setNote] = useState("");

  async function submit() {
    try {
      const result = await mutation.mutateAsync({ ip: data.address.ip, scope, counters, penalty, resetLevel, ledger, note: note.trim() });
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
          <DialogTitle>解除注册限制</DialogTitle>
          <DialogDescription>
            <span className="font-mono">{data.address.ip}</span>，已注册的账号不受影响，操作记入拦截记录
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            {(
              [
                ["ip", "只这个地址", data.address.host],
                ["subnet", "连同所在网段", data.address.subnet]
              ] as const
            ).map(([value, label, subject]) => (
              <button
                key={value}
                type="button"
                onClick={() => setScope(value)}
                className={cn(
                  "rounded-xl px-3 py-2.5 text-left transition-colors",
                  scope === value ? "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30" : "bg-muted hover:bg-muted-foreground/10"
                )}
              >
                <div className="text-xs font-medium">{label}</div>
                <div className="truncate font-mono text-[10px] text-muted-foreground">{subject}</div>
              </button>
            ))}
          </div>
          <SwitchRow icon={<Timer className="size-3.5" />} label="清空窗口计数" hint="各条滑动窗口规则从零算起" checked={counters} onChange={setCounters} />
          <SwitchRow icon={<LockKeyhole className="size-3.5" />} label="解除惩罚期" checked={penalty} onChange={setPenalty} />
          {penalty ? <SwitchRow label="同时清空惩罚等级" hint="下次再犯从首次时长算起" checked={resetLevel} onChange={setResetLevel} /> : null}
          <SwitchRow
            icon={<Users className="size-3.5" />}
            label="台账不再计入累计上限"
            hint={`目前该地址 ${data.hostRegistrations} 个、网段 ${data.subnetRegistrations} 个`}
            checked={ledger}
            onChange={setLedger}
          />
          <Input value={note} onChange={(event) => setNote(event.target.value)} maxLength={100} placeholder="备注（可选），例如：用户申诉，核实为学校机房" className="text-xs" />
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
