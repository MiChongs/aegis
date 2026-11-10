"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ExternalLink, Fingerprint, Globe2, KeyRound, Loader2, Mail, Phone, Search, ShieldCheck, Undo2, Users, type LucideIcon } from "lucide-react";
import { toast } from "sonner";

import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { ErrorState, errorText } from "@/components/risk/decisions";
import { Segmented } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { BAN_SOURCE_LABEL } from "@/components/users/ban-form";
import type { PlatformBan, PlatformBanDetail, PlatformBanSubject } from "@/lib/api/ban-appeals";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { usePlatformBan, usePlatformBans, useRevokePlatformBan } from "@/lib/ban-appeal-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useCanOperateRisk } from "@/lib/risk-ops-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { Block, Facts, reasonCodeLabel } from "./appeal-kit";

const PAGE_SIZE = 20;

const STATUS = [
  { value: "active", label: "生效中" },
  { value: "revoked", label: "已解除" },
  { value: "expired", label: "已到期" },
  { value: "all", label: "全部" },
] as const;
type StatusFilter = (typeof STATUS)[number]["value"];

const STATUS_BADGE: Record<string, { label: string; variant: "danger" | "outline" | "secondary" }> = {
  active: { label: "生效中", variant: "danger" },
  revoked: { label: "已解除", variant: "outline" },
  expired: { label: "已到期", variant: "secondary" },
};

const SUBJECT: Record<string, { label: string; icon: LucideIcon }> = {
  email: { label: "邮箱", icon: Mail },
  phone: { label: "手机号", icon: Phone },
  identity: { label: "第三方账号", icon: KeyRound },
  device: { label: "设备", icon: Fingerprint },
};
const subjectMeta = (kind: string) => SUBJECT[kind] ?? { label: kind, icon: Fingerprint };

const ACCOUNT_STATUS: Record<string, string> = { active: "封禁中", revoked: "已解除", expired: "已到期" };

/**
 * 风控平台 → 全站封禁（超级管理员）。
 *
 * 一条全站封禁封的是「这个人」：他绑定的邮箱、手机号、第三方账号与用过的设备（只存打码值），
 * 以及所有应用里与这些主体相连的账号。解除会一并解除所有连带的账号封禁。
 */
export function PlatformBans() {
  const isSuper = useCanOperateRisk();
  const [status, setStatus] = useState<StatusFilter>("active");
  const [keyword, setKeyword] = useState("");
  const q = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const [openId, setOpenId] = useState<number | null>(null);
  const list = usePlatformBans({ status: status === "all" ? undefined : status, q: q || undefined, page, pageSize: PAGE_SIZE }, isSuper);
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;

  if (!isSuper) {
    return (
      <section className="rounded-xl border bg-card">
        <CardEmpty icon={Globe2} title="仅超级管理员可查看全站封禁" description="全站封禁跨越所有应用，查看与解除都需要超级管理员。" />
      </section>
    );
  }

  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-xl border bg-card">
        <div className="flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-center gap-3">
            <TonalIcon icon={Globe2} tone="rose" />
            <div className="min-w-0">
              <h3 className="text-sm font-medium">全站封禁</h3>
              <p className="text-xs text-muted-foreground">封禁一个人在所有应用的账号，并拦截他用相同邮箱、手机号、第三方账号、设备再注册或登录</p>
            </div>
          </div>
          <div className="relative sm:w-64">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => {
                setKeyword(e.target.value);
                setPage(1);
              }}
              placeholder="搜登录名、用户 ID 或封禁 ID"
              className="h-8 pl-8 text-sm"
            />
          </div>
        </div>
        <div className="border-b px-4 py-2.5">
          <Segmented
            value={status}
            options={STATUS}
            onChange={(v) => {
              setStatus(v);
              setPage(1);
            }}
            ariaLabel="状态"
          />
        </div>

        {list.isLoading ? (
          <ListSkeleton />
        ) : list.isError ? (
          <ErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : !items.length ? (
          <CardEmpty icon={ShieldCheck} title={q ? "没有匹配的全站封禁" : "没有全站封禁"} description={q ? "换个关键词试试" : status === "active" ? "目前没有生效中的全站封禁" : undefined} />
        ) : (
          <>
            <ul className="divide-y">
              {items.map((b) => (
                <PlatformBanRow key={b.id} ban={b} onOpen={() => setOpenId(b.id)} />
              ))}
            </ul>
            {total > PAGE_SIZE ? (
              <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} total={total} unit="条全站封禁" onPage={setPage} />
            ) : null}
          </>
        )}
      </section>

      <Dialog open={openId !== null} onOpenChange={(o) => !o && setOpenId(null)}>
        <DialogContent className="max-h-[90vh] gap-5 overflow-y-auto sm:max-w-2xl">
          {openId !== null ? <PlatformBanDetailView id={openId} onClose={() => setOpenId(null)} /> : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function origin(ban: PlatformBan) {
  const who = ban.originAccount || (ban.originUserId ? `用户 #${ban.originUserId}` : "");
  const where = ban.originAppName || (ban.originAppId ? `应用 #${ban.originAppId}` : "");
  return who && where ? `从${where}的 ${who} 发起` : where ? `从${where}发起` : "";
}

function by(ban: PlatformBan) {
  if (ban.createdByAdminName) return `${ban.createdByAdminName}`;
  return ban.source ? BAN_SOURCE_LABEL[ban.source] ?? ban.source : "系统";
}

/** 「张三 2 天前由风控平台全站封禁，原因类别为欺诈或作弊，连带 4 个账号与 6 个主体」 */
function PlatformBanRow({ ban, onOpen }: { ban: PlatformBan; onOpen: () => void }) {
  const st = STATUS_BADGE[ban.status] ?? { label: ban.status, variant: "secondary" as const };
  const code = reasonCodeLabel(ban.reasonCode);
  const preview = (ban.subjects ?? []).slice(0, 4);
  return (
    <li>
      <button type="button" onClick={onOpen} className="flex w-full min-w-0 items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none">
        <TonalIcon icon={Globe2} tone={ban.status === "active" ? "rose" : "muted"} size="sm" className="mt-0.5" />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-start gap-2">
            <p className="min-w-0 flex-1 text-sm leading-5">
              <span className="font-medium">#{ban.id}</span>
              <span className="text-muted-foreground" title={formatDateTime(ban.createdAt)}> {formatRelative(ban.createdAt)}</span>
              由{by(ban)}{ban.banType === "permanent" ? "永久" : ""}全站封禁
              {code ? <span className="text-muted-foreground">，原因类别为{code}</span> : null}
            </p>
            <Badge variant={st.variant} size="sm">{st.label}</Badge>
          </div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
            {[origin(ban), `连带 ${ban.accountCount} 个账号与 ${ban.subjectCount} 个主体`, ban.endAt ? `到 ${formatDateTime(ban.endAt)} 解除` : ""].filter(Boolean).join("，")}
          </p>
          {preview.length ? (
            <div className="mt-1.5 flex flex-wrap gap-1.5">
              {preview.map((s, i) => (
                <SubjectChip key={`${s.kind}-${i}`} subject={s} />
              ))}
              {ban.subjectCount > preview.length ? <span className="px-1 text-[11px] leading-5 text-muted-foreground">等 {ban.subjectCount} 个</span> : null}
            </div>
          ) : null}
        </div>
      </button>
    </li>
  );
}

function SubjectChip({ subject }: { subject: PlatformBanSubject }) {
  const m = subjectMeta(subject.kind);
  const Icon = m.icon;
  return (
    <span className="inline-flex max-w-full items-center gap-1 rounded-md border bg-muted/40 px-1.5 py-0.5 text-[11px] leading-4">
      <Icon className="size-3 shrink-0 text-muted-foreground" />
      <span className="truncate font-mono">{subject.masked}</span>
      {subject.provider ? <span className="shrink-0 text-muted-foreground">{subject.provider}</span> : null}
    </span>
  );
}

function PlatformBanDetailView({ id, onClose }: { id: number; onClose: () => void }) {
  const detail = usePlatformBan(id);
  if (detail.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="h-32 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  if (detail.isError || !detail.data) {
    return (
      <>
        <DialogHeader>
          <DialogTitle>全站封禁 #{id}</DialogTitle>
          <DialogDescription className="sr-only">读取失败</DialogDescription>
        </DialogHeader>
        <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />
      </>
    );
  }
  return <DetailBody data={detail.data} onClose={onClose} />;
}

function DetailBody({ data, onClose }: { data: PlatformBanDetail; onClose: () => void }) {
  const { ban } = data;
  const subjects = data.subjects ?? [];
  const accounts = data.accounts ?? [];
  const apps = useAdminAppsQuery();
  const keyOf = useMemo(() => new Map((apps.data ?? []).map((a) => [a.id, a.appKey])), [apps.data]);
  const groups = useMemo(() => {
    const m = new Map<string, PlatformBanSubject[]>();
    for (const s of data.subjects ?? []) m.set(s.kind, [...(m.get(s.kind) ?? []), s]);
    return [...m.entries()];
  }, [data.subjects]);
  const st = STATUS_BADGE[ban.status] ?? { label: ban.status, variant: "secondary" as const };
  const [revoking, setRevoking] = useState(false);

  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          全站封禁 #{ban.id}
          <Badge variant={st.variant} size="sm">{st.label}</Badge>
        </DialogTitle>
        <DialogDescription>
          {[`${formatDateTime(ban.createdAt)} 由${by(ban)}发起`, origin(ban)].filter(Boolean).join("，")}
        </DialogDescription>
      </DialogHeader>

      <div className="flex flex-col gap-6">
        <Block title="封禁">
          <div className="rounded-xl border p-4">
            <Facts
              items={[
                { label: "类型", value: ban.banType === "permanent" ? "永久" : "临时" },
                { label: "到期", value: ban.endAt ? formatDateTime(ban.endAt) : "永久" },
                { label: "原因类别", value: reasonCodeLabel(ban.reasonCode) || "未分类" },
                { label: "来源", value: ban.source ? BAN_SOURCE_LABEL[ban.source] ?? ban.source : null },
                { label: "连带账号", value: `${ban.accountCount} 个` },
                { label: "主体", value: `${ban.subjectCount} 个` },
                { label: "内部原因", value: ban.reason || "未填写", wide: true },
                { label: "对用户显示的说明", value: ban.publicReason || "使用类别的标准说明", wide: true },
                ...(ban.status === "revoked"
                  ? [{ label: "解除", value: `${ban.revokedByAdminName || "系统"}于 ${formatDateTime(ban.revokedAt)} 解除${ban.revokeReason ? `：${ban.revokeReason}` : ""}`, wide: true }]
                  : []),
              ]}
            />
          </div>
        </Block>

        <Block title={`被拦截的主体（${subjects.length}）`}>
          {groups.length ? (
            <div className="grid gap-2 sm:grid-cols-2">
              {groups.map(([kind, list]) => {
                const m = subjectMeta(kind);
                return (
                  <div key={kind} className="rounded-xl border p-3">
                    <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
                      <m.icon className="size-3.5" />
                      {m.label}
                      <span className="tabular-nums">{list.length}</span>
                    </div>
                    <ul className="flex flex-col gap-1">
                      {list.map((s, i) => (
                        <li key={i} className="flex min-w-0 items-center justify-between gap-2 text-sm">
                          <span className="truncate font-mono text-xs">{s.masked}</span>
                          {s.provider ? <span className="shrink-0 text-[11px] text-muted-foreground">{s.provider}</span> : null}
                        </li>
                      ))}
                    </ul>
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">没有记录到主体</p>
          )}
        </Block>

        <Block title={`连带的账号（${accounts.length}）`}>
          {accounts.length ? (
            <ul className="divide-y rounded-xl border">
              {accounts.map((a) => {
                const key = keyOf.get(a.appid);
                return (
                  <li key={a.banId} className="flex min-w-0 items-center gap-3 px-3 py-2.5">
                    <TonalIcon icon={Users} tone="muted" size="sm" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm">{a.nickname || a.account || `用户 #${a.userId}`}</p>
                      <p className="truncate text-xs text-muted-foreground">
                        {[a.appName || `应用 #${a.appid}`, a.account && a.account !== a.nickname ? `登录名 ${a.account}` : "", `用户 ID ${a.userId}`].filter(Boolean).join("，")}
                      </p>
                    </div>
                    <Badge variant={a.status === "active" ? "danger" : "outline"} size="sm">{ACCOUNT_STATUS[a.status] ?? a.status}</Badge>
                    {key ? (
                      <Button asChild size="sm" variant="ghost" className="h-8 px-2" title="查看风险详情">
                        <Link href={`/app-users/${encodeURIComponent(key)}/${a.userId}?tab=risk`}>
                          <ExternalLink className="size-3.5" />
                        </Link>
                      </Button>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">没有连带账号</p>
          )}
        </Block>
      </div>

      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>关闭</Button>
        {ban.status === "active" ? (
          <Button variant="outline" onClick={() => setRevoking(true)}>
            <Undo2 />
            解除全站封禁
          </Button>
        ) : null}
      </DialogFooter>

      <RevokeDialog ban={ban} open={revoking} onOpenChange={setRevoking} />
    </>
  );
}

function RevokeDialog({ ban, open, onOpenChange }: { ban: PlatformBan; open: boolean; onOpenChange: (o: boolean) => void }) {
  const [reason, setReason] = useState("");
  const revoke = useRevokePlatformBan();

  async function submit() {
    try {
      await revoke.mutateAsync({ id: ban.id, reason: reason.trim() });
      toast.success("全站封禁已解除", { description: `连带的 ${ban.accountCount} 个账号一并恢复` });
      setReason("");
      onOpenChange(false);
    } catch (e) {
      toast.error("解除失败", { description: errorText(e) });
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>解除全站封禁</DialogTitle>
          <DialogDescription>
            立即解除这条全站封禁，连带的 {ban.accountCount} 个账号一并恢复，{ban.subjectCount} 个主体不再拦截注册与登录。
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="platform-revoke-reason" className="text-xs text-muted-foreground">解除原因</Label>
          <Textarea
            id="platform-revoke-reason"
            rows={3}
            maxLength={500}
            value={reason}
            placeholder="例如：核实为误封"
            onChange={(e) => setReason(e.target.value)}
            className="resize-none"
          />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>取消</Button>
          <Button disabled={revoke.isPending || !reason.trim()} onClick={() => void submit()} className={cn(revoke.isPending && "cursor-wait")}>
            {revoke.isPending ? <Loader2 className="animate-spin" /> : <Undo2 />}
            确认解除
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ListSkeleton() {
  return (
    <div className="divide-y">
      {Array.from({ length: 5 }, (_, i) => (
        <div key={i} className="flex items-start gap-3 px-4 py-3">
          <Skeleton className="size-7 rounded-lg" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-4 w-72" />
            <Skeleton className="h-3 w-56" />
            <div className="flex gap-1.5">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-4 w-20" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
