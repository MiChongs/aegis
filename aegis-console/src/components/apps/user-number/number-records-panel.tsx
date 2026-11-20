"use client";

import { useState } from "react";
import { ArrowRight, Dices, History, ReceiptText } from "lucide-react";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import {
  FilterBar,
  FilterSelect,
  Pager,
  RecordTable,
  Section,
  SignedAmount,
  StateBlock,
  formatInt,
  type Column
} from "@/components/users/detail/kit";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { NumberDraw, NumberHistory, NumberLedgerEntry, NumberTierSpec } from "@/lib/api/user-number";
import { formatDateTime } from "@/lib/datetime";
import { useNumberDrawsQuery, useNumberHistoryQuery, useNumberLedgerQuery } from "@/lib/user-number-hooks";
import { cn } from "@/lib/utils";
import { DrawStatusBadge, NumberText, NumberTierBadge, NumberUserCell, RightLabel, SOURCE_LABEL, rightUnit, tierSpec } from "./number-shared";

const KINDS = [
  { key: "history", label: "换号记录", icon: History },
  { key: "ledger", label: "权益流水", icon: ReceiptText },
  { key: "draws", label: "摇号单", icon: Dices }
] as const;

type RecordKind = (typeof KINDS)[number]["key"];

const RIGHT_KIND_OPTIONS = [
  { value: "change", label: "改号卡" },
  { value: "ticket", label: "靓号券" },
  { value: "draw", label: "摇号次数" }
];

function TimeCell({ value }: { value: string }) {
  return <span className="text-[11px] tabular-nums text-muted-foreground">{formatDateTime(value)}</span>;
}

/** 三类记录共用一个用户筛选：换个列表看同一个人，不用重选 */
export function NumberRecordsPanel({ appKey, tiers }: { appKey: string; tiers?: NumberTierSpec[] | null }) {
  const [kind, setKind] = useState<RecordKind>("history");
  const [user, setUser] = useState<PickedUser | null>(null);
  const userId = user?.id;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup type="single" size="sm" variant="outline" value={kind} onValueChange={(value) => value && setKind(value as RecordKind)}>
          {KINDS.map((item) => (
            <ToggleGroupItem key={item.key} value={item.key} className="gap-1.5 px-3 text-xs">
              <item.icon className="size-3.5" />
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="w-full sm:ml-auto sm:w-64">
          <UserPicker appKey={appKey} value={user} onChange={setUser} placeholder="按用户筛选" />
        </div>
      </div>
      {kind === "history" ? <HistoryList key={userId ?? 0} appKey={appKey} userId={userId} tiers={tiers} /> : null}
      {kind === "ledger" ? <LedgerList key={userId ?? 0} appKey={appKey} userId={userId} /> : null}
      {kind === "draws" ? <DrawList key={userId ?? 0} appKey={appKey} userId={userId} tiers={tiers} /> : null}
    </div>
  );
}

function usePaging() {
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  return {
    page,
    limit,
    setPage,
    onLimit: (value: number) => {
      setLimit(value);
      setPage(1);
    }
  };
}

export function historySourceText(row: Pick<NumberHistory, "source" | "rightKind" | "rightLevel">) {
  const label = SOURCE_LABEL[row.source] ?? row.source;
  if (row.source === "ticket" && row.rightLevel > 0) return `${tierSpec(row.rightLevel).name}靓号券`;
  return label;
}

export function HistoryList({ appKey, userId, tiers }: { appKey: string; userId?: number; tiers?: NumberTierSpec[] | null }) {
  const paging = usePaging();
  const query = useNumberHistoryQuery(appKey, { userId, page: paging.page, limit: paging.limit });
  const data = query.data;
  const columns: Array<Column<NumberHistory>> = [
    { id: "time", header: "时间", width: "150px", cell: (row) => <TimeCell value={row.createdAt} /> },
    {
      id: "user",
      header: "用户",
      width: "16%",
      cell: (row) => <NumberUserCell appKey={appKey} userId={row.userId} account={row.account} nickname={row.nickname} />
    },
    {
      id: "change",
      header: "号码",
      cell: (row) => (
        <span className="flex flex-wrap items-center gap-1.5">
          <span className="font-mono text-xs text-muted-foreground tabular-nums">{row.oldNumber ?? "—"}</span>
          <ArrowRight className="size-3 text-muted-foreground" />
          <NumberText number={row.newNumber} tier={row.tier} tiers={tiers} />
        </span>
      )
    },
    { id: "source", header: "方式", width: "110px", cell: (row) => <span className="text-xs">{historySourceText(row)}</span> },
    {
      id: "operator",
      header: "操作人与备注",
      width: "20%",
      hideBelow: "lg",
      cell: (row) => (
        <div className="min-w-0 text-[11px] text-muted-foreground">
          <div className="truncate">{row.operator || "—"}</div>
          {row.note ? (
            <div className="truncate" title={row.note}>
              {row.note}
            </div>
          ) : null}
        </div>
      )
    }
  ];
  return (
    <Section
      title="换号记录"
      icon={History}
      tone="info"
      flush
      footer={data ? <Pager page={paging.page} limit={paging.limit} total={data.total} onPage={paging.setPage} onLimit={paging.onLimit} /> : null}
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        empty={<StateBlock compact icon={History} title="暂无换号记录" />}
      />
    </Section>
  );
}

function LedgerList({ appKey, userId }: { appKey: string; userId?: number }) {
  const paging = usePaging();
  const [kind, setKind] = useState("");
  const query = useNumberLedgerQuery(appKey, { userId, kind: kind || undefined, page: paging.page, limit: paging.limit });
  const data = query.data;
  const columns: Array<Column<NumberLedgerEntry>> = [
    { id: "time", header: "时间", width: "150px", cell: (row) => <TimeCell value={row.createdAt} /> },
    {
      id: "user",
      header: "用户",
      width: "16%",
      cell: (row) => <NumberUserCell appKey={appKey} userId={row.userId} account={row.account} nickname={row.nickname} />
    },
    { id: "right", header: "权益", width: "120px", cell: (row) => <span className="text-xs"><RightLabel kind={row.kind} level={row.level} name={row.name} /></span> },
    { id: "delta", header: "变动", width: "80px", align: "right", cell: (row) => <SignedAmount value={row.delta} /> },
    {
      id: "balance",
      header: "余额",
      width: "72px",
      align: "right",
      cell: (row) => (
        <span className="text-xs tabular-nums">
          {formatInt(row.balanceAfter)}
          <span className="ml-0.5 text-muted-foreground">{rightUnit(row.kind)}</span>
        </span>
      )
    },
    {
      id: "origin",
      header: "来源",
      cell: (row) => (
        <div className="min-w-0 text-xs">
          <div className="truncate">{row.originLabel || row.origin}</div>
          {row.title ? (
            <div className="truncate text-[11px] text-muted-foreground" title={row.title}>
              {row.title}
            </div>
          ) : null}
        </div>
      )
    },
    {
      id: "operator",
      header: "操作人",
      width: "100px",
      hideBelow: "lg",
      cell: (row) => <span className="truncate text-[11px] text-muted-foreground">{row.operator || "—"}</span>
    }
  ];
  return (
    <Section
      title="权益流水"
      icon={ReceiptText}
      tone="violet"
      flush
      toolbar={
        <FilterBar>
          <FilterSelect
            label="种类"
            value={kind}
            onChange={(value) => {
              setKind(value);
              paging.setPage(1);
            }}
            options={RIGHT_KIND_OPTIONS}
            allLabel="全部"
          />
        </FilterBar>
      }
      footer={data ? <Pager page={paging.page} limit={paging.limit} total={data.total} onPage={paging.setPage} onLimit={paging.onLimit} /> : null}
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        empty={<StateBlock compact icon={ReceiptText} title="暂无权益流水" />}
      />
    </Section>
  );
}

/** 一张摇号单的候选号：选中的那个加粗描边 */
export function DrawCandidates({ draw, tiers }: { draw: NumberDraw; tiers?: NumberTierSpec[] | null }) {
  const list = draw.candidates ?? [];
  if (!list.length) return <span className="text-xs text-muted-foreground">—</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {list.map((candidate) => {
        const picked = draw.picked === candidate.number;
        return (
          <span
            key={candidate.number}
            className={cn(
              "inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5",
              picked ? "border-primary bg-primary/5" : "border-border bg-muted/30"
            )}
          >
            <span className={cn("font-mono text-[11px] tabular-nums", picked && "font-semibold")}>{candidate.number}</span>
            {candidate.tier > 0 ? <NumberTierBadge tier={candidate.tier} tiers={tiers} className="px-1 py-0 text-[10px]" /> : null}
          </span>
        );
      })}
    </span>
  );
}

function DrawList({ appKey, userId, tiers }: { appKey: string; userId?: number; tiers?: NumberTierSpec[] | null }) {
  const paging = usePaging();
  const query = useNumberDrawsQuery(appKey, { userId, page: paging.page, limit: paging.limit });
  const data = query.data;
  const columns: Array<Column<NumberDraw>> = [
    { id: "time", header: "摇号时间", width: "150px", cell: (row) => <TimeCell value={row.createdAt} /> },
    { id: "user", header: "用户", width: "96px", cell: (row) => <NumberUserCell appKey={appKey} userId={row.userId} /> },
    { id: "candidates", header: "候选号", cell: (row) => <DrawCandidates draw={row} tiers={tiers} /> },
    {
      id: "luck",
      header: "号运",
      width: "64px",
      align: "right",
      cell: (row) => <span className="font-mono text-xs tabular-nums">{row.luck ? row.luck.toFixed(2) : "—"}</span>
    },
    { id: "status", header: "状态", width: "84px", cell: (row) => <DrawStatusBadge status={row.status} /> },
    {
      id: "closed",
      header: "到期或结束",
      width: "150px",
      hideBelow: "lg",
      cell: (row) => <TimeCell value={row.closedAt || row.expiresAt} />
    }
  ];
  return (
    <Section
      title="摇号单"
      icon={Dices}
      tone="warning"
      flush
      footer={data ? <Pager page={paging.page} limit={paging.limit} total={data.total} onPage={paging.setPage} onLimit={paging.onLimit} /> : null}
    >
      <RecordTable
        columns={columns}
        rows={data?.items}
        rowKey={(row) => row.id}
        loading={query.isLoading}
        fetching={query.isFetching}
        error={query.error}
        onRetry={() => void query.refetch()}
        minWidth={820}
        empty={<StateBlock compact icon={Dices} title="暂无摇号单" />}
      />
    </Section>
  );
}
