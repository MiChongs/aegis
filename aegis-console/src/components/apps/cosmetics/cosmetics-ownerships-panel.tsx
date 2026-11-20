"use client";

import { useState } from "react";
import Link from "next/link";
import { ReceiptText } from "lucide-react";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { useNow } from "@/components/users/ban-form";
import { Badge } from "@/components/ui/badge";
import {
  FilterBar,
  FilterSelect,
  Pager,
  ResetButton,
  RecordTable,
  Section,
  StateBlock,
  type Column
} from "@/components/users/detail/kit";
import type { CosmeticCatalog, CosmeticOwnership, CosmeticSlot } from "@/lib/api/cosmetics";
import { useCosmeticItemsQuery, useCosmeticOwnershipsQuery } from "@/lib/cosmetic-hooks";
import { formatDateTimeMinute, formatShortDateTime } from "@/lib/datetime";
import {
  OWNERSHIP_STATE_META,
  RarityBadge,
  SLOT_META,
  ownershipState,
  slotLabel,
  sourceLabel,
  termText
} from "./cosmetic-shared";

export type OwnershipFilter = { cosmeticId?: number; source?: string; state?: string; user?: PickedUser | null };

/**
 * 持有记录：按用户 / 装扮 / 来源 / 状态筛选，服务端分页。任何一项筛选变化都回到第 1 页。
 * 账号可点，进用户详情的「装扮」页签处理收回与摘下。
 */
export function CosmeticsOwnershipsPanel({
  appKey,
  catalog,
  initial
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  initial?: OwnershipFilter;
}) {
  const [user, setUser] = useState<PickedUser | null>(initial?.user ?? null);
  const [cosmeticId, setCosmeticId] = useState(initial?.cosmeticId ? String(initial.cosmeticId) : "");
  const [source, setSource] = useState(initial?.source ?? "");
  const [state, setState] = useState(initial?.state ?? "");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(50);
  const now = useNow();
  const itemsQuery = useCosmeticItemsQuery(appKey);
  const query = useCosmeticOwnershipsQuery(appKey, {
    userId: user?.id,
    cosmeticId: cosmeticId ? Number(cosmeticId) : undefined,
    source,
    state,
    page,
    limit
  });
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const filtered = Boolean(user || cosmeticId || source || state);

  const reset = () => {
    setUser(null);
    setCosmeticId("");
    setSource("");
    setState("");
    setPage(1);
  };

  const columns: Array<Column<CosmeticOwnership>> = [
    {
      id: "user",
      header: "用户",
      cell: (row) =>
        row.userId ? (
          <Link
            href={`/app-users/${encodeURIComponent(appKey)}/${row.userId}?tab=cosmetics`}
            className="flex min-w-0 flex-col hover:underline"
          >
            <span className="truncate text-sm">{row.userNickname || row.userAccount || `用户 #${row.userId}`}</span>
            {row.userNickname && row.userAccount ? (
              <span className="truncate text-xs text-muted-foreground">{row.userAccount}</span>
            ) : null}
          </Link>
        ) : (
          "—"
        )
    },
    {
      id: "cosmetic",
      header: "装扮",
      cell: (row) => {
        const meta = row.slot ? SLOT_META[row.slot as CosmeticSlot] : undefined;
        return (
          <div className="flex min-w-0 items-center gap-2">
            {meta ? <meta.icon className="size-3.5 shrink-0 text-muted-foreground" /> : null}
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-sm">{row.cosmeticName || `#${row.cosmeticId}`}</span>
              <span className="truncate text-xs text-muted-foreground">{slotLabel(row.slot, catalog)}</span>
            </div>
            {row.rarity ? <RarityBadge rarity={row.rarity} catalog={catalog} className="hidden lg:inline-flex" /> : null}
          </div>
        );
      }
    },
    {
      id: "source",
      header: "来源",
      cell: (row) => (
        <div className="flex min-w-0 flex-col">
          <span className="text-sm">{sourceLabel(row.source, catalog)}</span>
          <span className="max-w-[180px] truncate font-mono text-xs text-muted-foreground">{row.sourceRef || row.operator || ""}</span>
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
      id: "created",
      header: "获得时间",
      align: "right",
      hideBelow: "md",
      cell: (row) => <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground">{formatDateTimeMinute(row.createdAt)}</span>
    }
  ];

  return (
    <Section
      title="持有记录"
      icon={ReceiptText}
      tone="violet"
      flush
      toolbar={
        <FilterBar>
          <div className="w-full sm:w-60">
            <UserPicker
              appKey={appKey}
              value={user}
              onChange={(next) => {
                setUser(next);
                setPage(1);
              }}
            />
          </div>
          <FilterSelect
            value={cosmeticId}
            onChange={(value) => {
              setCosmeticId(value);
              setPage(1);
            }}
            label="装扮"
            allLabel="全部装扮"
            className="max-w-[260px]"
            options={(itemsQuery.data ?? []).map((item) => ({ value: String(item.id), label: `${item.name}（${slotLabel(item.slot, catalog)}）` }))}
          />
          <FilterSelect
            value={source}
            onChange={(value) => {
              setSource(value);
              setPage(1);
            }}
            label="来源"
            options={(catalog?.sources ?? []).map((spec) => ({ value: spec.key, label: spec.label }))}
          />
          <FilterSelect
            value={state}
            onChange={(value) => {
              setState(value);
              setPage(1);
            }}
            label="状态"
            options={(Object.keys(OWNERSHIP_STATE_META) as Array<keyof typeof OWNERSHIP_STATE_META>).map((key) => ({
              value: key,
              label: OWNERSHIP_STATE_META[key].label
            }))}
          />
          <ResetButton visible={filtered} onClick={reset} />
        </FilterBar>
      }
      footer={
        total > 0 ? (
          <Pager
            page={page}
            limit={limit}
            total={total}
            onPage={setPage}
            onLimit={(next) => {
              setLimit(next);
              setPage(1);
            }}
          />
        ) : undefined
      }
    >
      <RecordTable
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        rowClassName={(row) => (ownershipState(row, now) === "live" ? undefined : "text-muted-foreground")}
        empty={<StateBlock compact icon={ReceiptText} title={filtered ? "没有符合条件的记录" : "暂无持有记录"} />}
        minWidth={820}
      />
    </Section>
  );
}
