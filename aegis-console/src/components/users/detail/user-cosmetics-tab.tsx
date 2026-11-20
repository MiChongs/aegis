"use client";

import { useState } from "react";
import { Gift, Loader2, Plus, Shirt, Sparkles, Undo2, UserRound, X } from "lucide-react";
import { toast } from "sonner";
import { AvatarFrameCanvas, DesignCanvas, NameStyleCanvas } from "@/components/apps/cosmetics/cosmetic-canvas";
import { CosmeticPicker } from "@/components/apps/cosmetics/cosmetic-reward";
import {
  OWNERSHIP_STATE_META,
  RarityBadge,
  SLOT_META,
  ownershipState,
  reasonLabel,
  slotLabel,
  sourceLabel,
  termText
} from "@/components/apps/cosmetics/cosmetic-shared";
import { useNow } from "@/components/users/ban-form";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import type { AdminAppUserDetail } from "@/lib/api/types";
import type { CosmeticCatalog, CosmeticEquipRow, CosmeticItem, CosmeticLoadout, CosmeticOwnership } from "@/lib/api/cosmetics";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import {
  useCosmeticCatalogQuery,
  useGrantUserCosmeticMutation,
  useRevokeUserCosmeticMutation,
  useUnequipUserCosmeticMutation,
  useUserCosmeticsQuery
} from "@/lib/cosmetic-hooks";
import { formatDateTimeMinute, formatShortDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { Pager, RecordTable, Section, StateBlock, TONE_TEXT, errorMessage, type Column } from "./kit";
import { QuickChips, Segmented, type SegmentOption } from "./user-assets-adjust";

/**
 * 用户详情「装扮」页签：此刻身上的装扮（按 Eligibility 过滤后的结论，直接渲染）、佩戴行与是否生效、
 * 持有记录（收回带原因）、发放、摘下某个槽位。按钮按 app:cosmetic:grant 显隐，只读管理员只看得到。
 */

type Term = "7" | "30" | "90" | "365" | "permanent" | "custom";
const TERMS: Array<SegmentOption<Term>> = [
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" },
  { value: "90", label: "90 天" },
  { value: "365", label: "1 年" },
  { value: "permanent", label: "永久" },
  { value: "custom", label: "自定义" }
];
const REVOKE_REASONS = ["误操作发放", "违规使用", "活动结束", "退款收回"];
const PAGE_SIZE = 10;

export function UserCosmeticsTab({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  const query = useUserCosmeticsQuery(appKey, userId);
  const catalog = useCosmeticCatalogQuery(appKey).data;
  const can = useAppKeyPermissionChecker(appKey);
  const canGrant = can("app:cosmetic:grant");
  const [grantOpen, setGrantOpen] = useState(false);
  const data = query.data;
  const nickname = user.nickname || user.profile?.nickname || user.account || `用户 #${userId}`;

  if (query.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-56 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  if (query.isError || !data) {
    return (
      <Section title="装扮" icon={Shirt} tone="violet">
        <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
      </Section>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Section
        title="此刻身上的装扮"
        icon={Sparkles}
        tone="violet"
        description="已按会员、期限、上下架过滤，与客户端看到的一致"
        actions={
          canGrant ? (
            <Button size="sm" onClick={() => setGrantOpen(true)}>
              <Gift />
              发放装扮
            </Button>
          ) : null
        }
      >
        <LoadoutView loadout={data.loadout} avatarUrl={user.avatar} nickname={nickname} catalog={catalog} />
        <div className="mt-4 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
          <Badge variant={data.facts.isVip ? "success" : "outline"} size="sm">
            {data.facts.isVip ? "有效会员" : "非会员"}
          </Badge>
          <Badge variant="outline" size="sm">
            Lv.{data.facts.level}
          </Badge>
          {(data.facts.features ?? []).slice(0, 6).map((feature) => (
            <Badge key={feature} variant="outline" size="sm" className="font-mono font-normal">
              {feature}
            </Badge>
          ))}
        </div>
      </Section>

      <EquipsSection appKey={appKey} userId={userId} equips={data.equips} catalog={catalog} canGrant={canGrant} />
      <OwnershipsSection appKey={appKey} userId={userId} rows={data.ownerships} catalog={catalog} canGrant={canGrant} onGrant={() => setGrantOpen(true)} />

      {grantOpen ? <GrantDialog appKey={appKey} userId={userId} nickname={nickname} onClose={() => setGrantOpen(false)} /> : null}
    </div>
  );
}

function LoadoutView({
  loadout,
  avatarUrl,
  nickname,
  catalog
}: {
  loadout: CosmeticLoadout;
  avatarUrl?: string;
  nickname: string;
  catalog?: CosmeticCatalog | null;
}) {
  const card = loadout.profileCard?.design;
  const frame = loadout.avatarFrame?.design;
  const name = loadout.nameStyle?.design?.nameStyle;
  const badges = loadout.badges ?? [];
  const empty = !loadout.avatarFrame && !loadout.profileCard && !loadout.nameStyle && badges.length === 0;
  const width = 340;
  return (
    <div className="grid gap-5 md:grid-cols-[340px_minmax(0,1fr)]">
      <div className="overflow-hidden rounded-xl border bg-card" style={{ width }}>
        <div className="relative h-[150px] overflow-hidden bg-muted">
          {card ? (
            <div className="absolute inset-x-0 top-0">
              <DesignCanvas design={card} width={width} />
            </div>
          ) : null}
        </div>
        <div className="-mt-9 flex flex-col items-center gap-1.5 px-4 pb-5">
          {frame ? (
            <AvatarFrameCanvas design={frame} size={72} avatarUrl={avatarUrl} />
          ) : (
            <span className="flex size-[72px] items-center justify-center rounded-full border-4 border-card bg-muted">
              <UserRound className="size-7 text-muted-foreground" />
            </span>
          )}
          <div className="mt-2 flex items-center gap-1.5">
            {name ? <NameStyleCanvas style={name} text={nickname} fontSize={17} /> : <span className="text-[17px] font-semibold">{nickname}</span>}
            {badges.map((badge) => (badge.design ? <DesignCanvas key={badge.id} design={badge.design} width={20} /> : null))}
          </div>
        </div>
      </div>
      <dl className="grid content-start gap-3 sm:grid-cols-2">
        {empty ? <p className="text-sm text-muted-foreground sm:col-span-2">没有佩戴任何装扮</p> : null}
        {(
          [
            ["avatar_frame", loadout.avatarFrame],
            ["profile_card", loadout.profileCard],
            ["name_style", loadout.nameStyle]
          ] as const
        ).map(([slot, view]) => {
          const meta = SLOT_META[slot];
          return (
            <div key={slot} className="flex items-center gap-2.5 rounded-lg border px-3 py-2.5">
              <meta.icon className="size-4 text-muted-foreground" />
              <div className="flex min-w-0 flex-col">
                <dt className="text-xs text-muted-foreground">{slotLabel(slot, catalog)}</dt>
                <dd className="truncate text-sm">{view ? view.name : "—"}</dd>
              </div>
              {view ? <RarityBadge rarity={view.rarity} catalog={catalog} className="ml-auto" /> : null}
            </div>
          );
        })}
        <div className="flex items-center gap-2.5 rounded-lg border px-3 py-2.5">
          <SLOT_META.badge.icon className="size-4 text-muted-foreground" />
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">{slotLabel("badge", catalog)}</dt>
            <dd className="truncate text-sm">{badges.length ? badges.map((badge) => badge.name).join("、") : "—"}</dd>
          </div>
        </div>
      </dl>
    </div>
  );
}

function EquipsSection({
  appKey,
  userId,
  equips,
  catalog,
  canGrant
}: {
  appKey: string;
  userId: number;
  equips: CosmeticEquipRow[];
  catalog?: CosmeticCatalog | null;
  canGrant: boolean;
}) {
  const unequip = useUnequipUserCosmeticMutation(appKey, userId);
  const [target, setTarget] = useState<{ slot: CosmeticEquipRow["slot"]; position: number; label: string } | null>(null);
  const sorted = [...equips].sort((a, b) => a.slot.localeCompare(b.slot) || a.position - b.position);
  const badgeCount = equips.filter((row) => row.slot === "badge").length;

  const submit = async () => {
    if (!target) return;
    try {
      await unequip.mutateAsync({ slot: target.slot, position: target.position });
      toast.success(`已摘下${target.label}`);
      setTarget(null);
    } catch (error) {
      toast.error(errorMessage(error, "摘下失败"));
    }
  };

  const columns: Array<Column<CosmeticEquipRow>> = [
    {
      id: "slot",
      header: "槽位",
      cell: (row) => (
        <span className="text-sm">
          {slotLabel(row.slot, catalog)}
          {row.slot === "badge" ? <span className="ml-1 text-xs text-muted-foreground">第 {row.position + 1} 位</span> : null}
        </span>
      )
    },
    { id: "name", header: "装扮", cell: (row) => <span className="text-sm">{row.name || `#${row.cosmeticId}`}</span> },
    {
      id: "effective",
      header: "是否生效",
      cell: (row) =>
        row.effective ? (
          <Badge variant="success" size="sm">
            生效
          </Badge>
        ) : (
          <span className="flex items-center gap-1.5">
            <Badge variant="outline" size="sm">
              不生效
            </Badge>
            <span className={cn("text-xs", TONE_TEXT.warning)}>{reasonLabel(row.reason)}</span>
          </span>
        )
    },
    {
      id: "time",
      header: "佩戴时间",
      hideBelow: "md",
      cell: (row) => <span className="text-xs tabular-nums text-muted-foreground">{formatDateTimeMinute(row.equippedAt)}</span>
    },
    {
      id: "actions",
      header: <span className="sr-only">操作</span>,
      align: "right",
      width: "80px",
      cell: (row) =>
        canGrant ? (
          <Button
            size="xs"
            variant="ghost"
            onClick={() => setTarget({ slot: row.slot, position: row.position, label: `「${row.name || slotLabel(row.slot, catalog)}」` })}
          >
            <X />
            摘下
          </Button>
        ) : null
    }
  ];

  return (
    <Section
      title="佩戴"
      icon={Shirt}
      tone="primary"
      description="每个槽位一行，勋章按位置多行；不生效的读取时自动过滤"
      flush
      actions={
        canGrant && badgeCount > 1 ? (
          <Button size="xs" variant="outline" onClick={() => setTarget({ slot: "badge", position: -1, label: "全部勋章" })}>
            摘下全部勋章
          </Button>
        ) : null
      }
    >
      <RecordTable
        columns={columns}
        rows={sorted}
        rowKey={(row) => `${row.slot}-${row.position}`}
        empty={<StateBlock compact icon={Shirt} title="暂无佩戴" />}
        minWidth={560}
      />
      <AlertDialog open={Boolean(target)} onOpenChange={(open) => !open && !unequip.isPending && setTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>摘下{target?.label}？</AlertDialogTitle>
            <AlertDialogDescription>只摘下佩戴，持有记录不变，用户可以重新戴上。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={unequip.isPending}>取消</AlertDialogCancel>
            <Button onClick={() => void submit()} disabled={unequip.isPending}>
              {unequip.isPending ? <Loader2 className="animate-spin" /> : null}
              摘下
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}

function OwnershipsSection({
  appKey,
  userId,
  rows,
  catalog,
  canGrant,
  onGrant
}: {
  appKey: string;
  userId: number;
  rows: CosmeticOwnership[];
  catalog?: CosmeticCatalog | null;
  canGrant: boolean;
  onGrant: () => void;
}) {
  const now = useNow();
  const revoke = useRevokeUserCosmeticMutation(appKey, userId);
  const [page, setPage] = useState(1);
  const [target, setTarget] = useState<CosmeticOwnership | null>(null);
  const [reason, setReason] = useState("");
  const pages = Math.max(1, Math.ceil(rows.length / PAGE_SIZE));
  const current = Math.min(page, pages);
  const visible = rows.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE);

  const submit = async () => {
    if (!target) return;
    try {
      await revoke.mutateAsync({ ownershipId: target.id, reason: reason.trim() });
      toast.success(`已收回「${target.cosmeticName || `#${target.cosmeticId}`}」`);
      setTarget(null);
    } catch (error) {
      toast.error(errorMessage(error, "收回失败"));
    }
  };

  const columns: Array<Column<CosmeticOwnership>> = [
    {
      id: "cosmetic",
      header: "装扮",
      cell: (row) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-sm">{row.cosmeticName || `#${row.cosmeticId}`}</span>
          <span className="text-xs text-muted-foreground">{slotLabel(row.slot, catalog)}</span>
        </div>
      )
    },
    {
      id: "source",
      header: "来源",
      cell: (row) => (
        <div className="flex min-w-0 flex-col">
          <span className="text-sm">{sourceLabel(row.source, catalog)}</span>
          <span className="max-w-[160px] truncate font-mono text-xs text-muted-foreground">{row.sourceRef || row.operator || ""}</span>
        </div>
      )
    },
    {
      id: "term",
      header: "期限",
      cell: (row) => (
        <div className="flex flex-col text-xs tabular-nums whitespace-nowrap">
          <span>{termText(row.durationDays)}</span>
          <span className="text-muted-foreground">
            {row.endsAt ? `${formatShortDateTime(row.startsAt)} → ${formatShortDateTime(row.endsAt)}` : `${formatShortDateTime(row.startsAt)} 起`}
          </span>
        </div>
      )
    },
    {
      id: "state",
      header: "状态",
      cell: (row) => {
        const meta = OWNERSHIP_STATE_META[ownershipState(row, now)];
        return (
          <Badge variant={meta.variant} size="sm" title={row.revokeReason || undefined}>
            {meta.label}
          </Badge>
        );
      }
    },
    {
      id: "actions",
      header: <span className="sr-only">操作</span>,
      align: "right",
      width: "80px",
      cell: (row) =>
        canGrant && ownershipState(row, now) === "live" ? (
          <Button
            size="xs"
            variant="ghost"
            onClick={() => {
              setReason("");
              setTarget(row);
            }}
          >
            <Undo2 />
            收回
          </Button>
        ) : null
    }
  ];

  return (
    <Section
      title="持有记录"
      icon={Gift}
      tone="info"
      flush
      actions={<Badge variant="outline" size="sm">{rows.length} 条</Badge>}
      footer={rows.length > PAGE_SIZE ? <Pager page={current} limit={PAGE_SIZE} total={rows.length} onPage={setPage} /> : undefined}
    >
      <RecordTable
        columns={columns}
        rows={visible}
        rowKey={(row) => row.id}
        rowClassName={(row) => (ownershipState(row, now) === "live" ? undefined : "text-muted-foreground")}
        empty={
          <StateBlock
            compact
            icon={Gift}
            title="暂无持有记录"
            action={
              canGrant ? (
                <Button size="sm" variant="outline" onClick={onGrant}>
                  <Plus />
                  发放装扮
                </Button>
              ) : undefined
            }
          />
        }
        minWidth={620}
      />
      <AlertDialog open={Boolean(target)} onOpenChange={(open) => !open && !revoke.isPending && setTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>收回「{target?.cosmeticName || `#${target?.cosmeticId}`}」？</AlertDialogTitle>
            <AlertDialogDescription>只收回这一条，同一件装扮的其它持有不受影响。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">收回原因</Label>
            <Input value={reason} maxLength={128} onChange={(event) => setReason(event.target.value)} placeholder="选填" />
            <QuickChips items={REVOKE_REASONS} value={reason.trim()} onPick={setReason} />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoke.isPending}>取消</AlertDialogCancel>
            <Button variant="destructive" onClick={() => void submit()} disabled={revoke.isPending}>
              {revoke.isPending ? <Loader2 className="animate-spin" /> : null}
              确认收回
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}

function GrantDialog({ appKey, userId, nickname, onClose }: { appKey: string; userId: number; nickname: string; onClose: () => void }) {
  const grant = useGrantUserCosmeticMutation(appKey, userId);
  const catalog = useCosmeticCatalogQuery(appKey).data;
  const maxDays = catalog?.vocabulary?.limits?.maxGrantDays ?? 3650;
  const [cosmetic, setCosmetic] = useState<{ id: number; item?: CosmeticItem }>({ id: 0 });
  const [term, setTerm] = useState<Term>("30");
  const [days, setDays] = useState("30");
  const count = Number(days);
  const daysValid = days !== "" && Number.isInteger(count) && count >= 0 && count <= maxDays;
  const ready = cosmetic.id > 0 && daysValid;

  const submit = async () => {
    if (!ready) return;
    try {
      const result = await grant.mutateAsync({ cosmeticId: cosmetic.id, days: count });
      if (result.alreadyPermanent) toast.info(`${nickname} 已永久持有「${result.cosmeticName}」，未重复发放`);
      else toast.success(`已发放「${result.cosmeticName}」${termText(count)}`);
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "发放失败"));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !grant.isPending && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>发放装扮</DialogTitle>
          <DialogDescription>给 {nickname} 发一件上架中的装扮；同一件限时发放会顺延。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">装扮</Label>
            <CosmeticPicker appKey={appKey} value={cosmetic.id} onChange={(id, item) => setCosmetic({ id, item })} className="sm:w-full" />
          </div>
          <div className="flex flex-col gap-2">
            <Label className="text-xs text-muted-foreground">期限</Label>
            <Segmented
              value={term}
              onChange={(value) => {
                setTerm(value);
                if (value !== "custom") setDays(value === "permanent" ? "0" : value);
              }}
              options={TERMS}
              ariaLabel="期限"
            />
            {term === "custom" ? (
              <Input
                autoFocus
                value={days}
                inputMode="numeric"
                className="sm:max-w-48"
                placeholder="天数，0 为永久"
                onChange={(event) => setDays(event.target.value.replace(/\D/g, ""))}
              />
            ) : null}
            {!daysValid ? <p className={cn("text-xs", TONE_TEXT.warning)}>天数需为 0 到 {maxDays} 的整数</p> : null}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={grant.isPending}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={!ready || grant.isPending}>
            {grant.isPending ? <Loader2 className="animate-spin" /> : <Gift />}
            {ready ? `发放 ${termText(count)}` : "发放"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
