"use client";

/**
 * 抽奖记录。
 *
 * 状态：已到账（自动发放）/ 待发放（实物、兑换码等需人工发放）/ 已发放（人工标记过）。
 * 待发放的记录可在这里标记发放，备注会写进给用户的站内信。
 *
 * 指标口径：都是该用户抽奖记录的总数，分别不加筛选、只看中奖（排除谢谢参与）、只看待发放。
 */

import * as React from "react";
import { Clock3, Dices, PackageCheck, Sparkles, Trophy } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { LIMITS } from "@/components/apps/lottery/lottery-activity-form";
import { DrawPlanView } from "@/components/apps/lottery/lottery-plan-view";
import {
  ConversionBadge,
  DRAW_STATUSES,
  DrawStatusBadge,
  SOURCE_LABEL,
  drawStatusLabel,
  kindLabelOf
} from "@/components/apps/lottery/lottery-shared";
import { useAdminToken } from "@/lib/admin-hooks";
import { listLotteryDraws, type LotteryDraw, type LotteryDrawStatus, type LotterySource } from "@/lib/api/lottery";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { useFulfillLotteryDrawMutation, useLotteryActivitiesQuery, useLotteryCatalogQuery } from "@/lib/lottery-hooks";
import { useUserLotteryCountsQuery, useUserLotteryDrawsQuery } from "@/lib/user-records-hooks";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  Section,
  StateBlock,
  TimeRangePicker,
  errorMessage,
  formatInt,
  rangeToParams,
  type Column,
  type CsvColumn,
  type TimeRange
} from "../kit";
import {
  ConfirmActionDialog,
  ExportButton,
  MetricGrid,
  ResultChips,
  ResultList,
  exportStamp,
  percent,
  resultsCsv,
  usePaging
} from "./records-shared";

export function LotteryRecords({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const countsQuery = useUserLotteryCountsQuery(appKey, userId);
  const counts = countsQuery.data;
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const catalogQuery = useLotteryCatalogQuery(appKey);

  const [activityId, setActivityId] = React.useState("");
  const [status, setStatus] = React.useState("");
  const [source, setSource] = React.useState("");
  const [winning, setWinning] = React.useState("");
  const [range, setRange] = React.useState<TimeRange>({ preset: "all" });
  const paging = usePaging(20);
  const time = rangeToParams(range);
  const filters = {
    userId,
    activityId: activityId ? Number(activityId) : undefined,
    status: (status || undefined) as LotteryDrawStatus | undefined,
    source: (source || undefined) as LotterySource | undefined,
    winning: winning === "yes" ? true : undefined,
    start: time.from,
    end: time.to
  };
  const listQuery = useUserLotteryDrawsQuery(appKey, { ...filters, page: paging.page, limit: paging.limit });

  const [selected, setSelected] = React.useState<LotteryDraw | null>(null);
  const [fulfilling, setFulfilling] = React.useState<LotteryDraw | null>(null);
  const [note, setNote] = React.useState("");
  const fulfillMutation = useFulfillLotteryDrawMutation(appKey);

  const filtered = Boolean(activityId || status || source || winning || range.preset !== "all");
  const reset = () => {
    setActivityId("");
    setStatus("");
    setSource("");
    setWinning("");
    setRange({ preset: "all" });
    paging.reset();
  };
  const change = (setter: (value: string) => void) => (value: string) => {
    setter(value);
    paging.reset();
  };

  const openFulfill = (draw: LotteryDraw) => {
    setNote("");
    setFulfilling(draw);
  };

  const submitFulfill = async () => {
    if (!fulfilling) return;
    try {
      const updated = await fulfillMutation.mutateAsync({ drawId: fulfilling.id, note: note.trim() });
      toast.success("已标记为已发放，并通知了用户");
      if (selected?.id === updated.id) setSelected(updated);
      setFulfilling(null);
    } catch (error) {
      toast.error(errorMessage(error, "标记失败"));
    }
  };

  const prizeNameOf = (draw: LotteryDraw) => (id: number, position: number) => {
    const activity = activitiesQuery.data?.find((item) => item.id === draw.activityId);
    return activity?.prizes.find((prize) => prize.id === id)?.name || (draw.trace?.picked === id ? draw.prizeName : `位置 ${position + 1}`);
  };
  const prizeKindOf = (draw: LotteryDraw) => (id: number) => {
    const activity = activitiesQuery.data?.find((item) => item.id === draw.activityId);
    return activity?.prizes.find((prize) => prize.id === id)?.kind || (draw.trace?.picked === id ? draw.prizeKind : undefined);
  };

  const columns: Array<Column<LotteryDraw>> = [
    {
      id: "time",
      header: "时间",
      cell: (row) => (
        <div className="flex flex-col">
          <span className="tabular-nums">{formatDateTime(row.createdAt)}</span>
          <span className="truncate text-xs text-muted-foreground">{row.activityName || `活动 #${row.activityId}`}</span>
        </div>
      )
    },
    {
      id: "prize",
      header: "奖品",
      cell: (row) => (
        <div className="flex flex-wrap items-center gap-1">
          <span className={row.prizeKind === "none" ? "text-muted-foreground" : "font-medium"}>{row.prizeName || "未中奖"}</span>
          {row.pity ? (
            <Badge variant="warning" size="sm">
              保底
            </Badge>
          ) : null}
          {row.conversion ? <ConversionBadge /> : null}
        </div>
      )
    },
    {
      id: "source",
      header: "消耗",
      hideBelow: "sm",
      cell: (row) => (
        <span className="text-xs text-muted-foreground">
          {SOURCE_LABEL[row.source] ?? row.source}
          {row.source === "points" && row.pointsCost ? <span className="tabular-nums">，{formatInt(row.pointsCost)} 积分</span> : null}
        </span>
      )
    },
    { id: "status", header: "状态", cell: (row) => <DrawStatusBadge status={row.status} /> },
    { id: "results", header: "到账", hideBelow: "lg", cell: (row) => <ResultChips results={row.results} /> },
    {
      id: "actions",
      header: "",
      align: "right",
      width: "88px",
      cell: (row) =>
        row.status === "pending" ? (
          <Button
            size="xs"
            variant="outline"
            onClick={(event) => {
              event.stopPropagation();
              openFulfill(row);
            }}
          >
            发放
          </Button>
        ) : null
    }
  ];

  const csvColumns: Array<CsvColumn<LotteryDraw>> = [
    { header: "抽奖单号", value: (row) => row.drawNo },
    { header: "时间", value: (row) => formatDateTime(row.createdAt, "") },
    { header: "活动", value: (row) => row.activityName || row.activityId },
    { header: "奖品", value: (row) => row.prizeName },
    { header: "奖品类型", value: (row) => kindLabelOf(row.prizeKind, catalogQuery.data?.kinds ?? []) },
    { header: "中奖", value: (row) => (row.prizeKind === "none" ? "否" : "是") },
    { header: "保底", value: (row) => (row.pity ? "是" : "否") },
    { header: "已折算", value: (row) => (row.conversion ? row.conversion.label : "") },
    { header: "消耗方式", value: (row) => SOURCE_LABEL[row.source] ?? row.source },
    { header: "消耗积分", value: (row) => row.pointsCost ?? 0 },
    { header: "状态", value: (row) => drawStatusLabel(row.status) },
    { header: "到账", value: (row) => resultsCsv(row.results) },
    { header: "备注", value: (row) => row.note },
    { header: "处理人", value: (row) => row.handledBy },
    { header: "处理时间", value: (row) => formatDateTime(row.handledAt, "") },
    { header: "设备", value: (row) => row.deviceId },
    { header: "IP", value: (row) => row.clientIp }
  ];

  return (
    <div className="flex flex-col gap-5">
      <MetricGrid>
        <MetricCard
          label="抽奖次数"
          icon={Dices}
          tone="primary"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.total ?? 0)}
          sub={counts?.latestAt ? `最近 ${formatRelative(counts.latestAt)}` : "暂无抽奖"}
        />
        <MetricCard
          label="中奖次数"
          icon={Trophy}
          tone="success"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.wins ?? 0)}
          sub={`中奖率 ${percent(counts?.wins ?? 0, counts?.total ?? 0)}，不含谢谢参与`}
          onClick={
            counts?.wins
              ? () => {
                  setWinning("yes");
                  paging.reset();
                }
              : undefined
          }
        />
        <MetricCard
          label="待发放"
          icon={Clock3}
          tone="warning"
          loading={countsQuery.isLoading}
          value={formatInt(counts?.pending ?? 0)}
          sub="需人工发放的奖品"
          onClick={
            counts?.pending
              ? () => {
                  setStatus("pending");
                  paging.reset();
                }
              : undefined
          }
        />
        <MetricCard
          label="未中奖"
          icon={Sparkles}
          tone="neutral"
          loading={countsQuery.isLoading}
          value={formatInt(Math.max(0, (counts?.total ?? 0) - (counts?.wins ?? 0)))}
          sub="谢谢参与"
        />
      </MetricGrid>

      <Section
        flush
        title="抽奖记录"
        description="待发放的奖品发出后在这里标记，用户会收到站内信"
        icon={Dices}
        tone="violet"
        actions={
          <ExportButton
            filename={`抽奖记录-${userId}-${exportStamp()}`}
            columns={csvColumns}
            disabled={!token || !listQuery.data?.total}
            fetchPage={(page, limit) => listLotteryDraws(token as string, appKey, { ...filters, page, limit })}
          />
        }
        toolbar={
          <FilterBar>
            <TimeRangePicker
              value={range}
              onChange={(next) => {
                setRange(next);
                paging.reset();
              }}
            />
            {activitiesQuery.data?.length ? (
              <FilterSelect
                label="活动"
                value={activityId}
                onChange={change(setActivityId)}
                options={activitiesQuery.data.map((item) => ({ value: String(item.id), label: item.name }))}
                className="max-w-[220px]"
              />
            ) : null}
            <FilterSelect
              label="状态"
              value={status}
              onChange={change(setStatus)}
              options={DRAW_STATUSES.map((item) => ({ value: item, label: drawStatusLabel(item) }))}
            />
            <FilterSelect
              label="消耗"
              value={source}
              onChange={change(setSource)}
              options={(Object.keys(SOURCE_LABEL) as LotterySource[]).map((item) => ({ value: item, label: SOURCE_LABEL[item] }))}
            />
            <FilterSelect label="结果" value={winning} onChange={change(setWinning)} allLabel="全部结果" options={[{ value: "yes", label: "只看中奖" }]} />
            <ResetButton visible={filtered} onClick={reset} />
          </FilterBar>
        }
        footer={
          listQuery.data?.total ? (
            <Pager page={paging.page} limit={paging.limit} total={listQuery.data.total} onPage={paging.setPage} onLimit={paging.setLimit} />
          ) : null
        }
      >
        <RecordTable
          columns={columns}
          rows={listQuery.data?.items}
          rowKey={(row) => row.id}
          loading={listQuery.isLoading}
          fetching={listQuery.isFetching}
          error={listQuery.error}
          onRetry={() => listQuery.refetch()}
          onRowClick={setSelected}
          minWidth={720}
          empty={<StateBlock title={filtered ? "没有符合条件的抽奖" : "暂无抽奖记录"} />}
        />
      </Section>

      <RecordSheet
        open={Boolean(selected)}
        onOpenChange={(open) => (!open ? setSelected(null) : undefined)}
        title="抽奖详情"
        description={selected ? `${selected.activityName || `活动 #${selected.activityId}`}，${formatDateTime(selected.createdAt)}` : undefined}
        hero={
          selected ? (
            <div className="flex flex-col gap-2 rounded-lg border px-4 py-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-lg font-semibold">{selected.prizeName || "未中奖"}</span>
                <DrawStatusBadge status={selected.status} />
                {selected.pity ? (
                  <Badge variant="warning" size="sm">
                    保底
                  </Badge>
                ) : null}
                {selected.conversion ? <ConversionBadge /> : null}
              </div>
              {selected.conversion ? <p className="text-xs leading-5 text-muted-foreground">{selected.conversion.message}</p> : null}
            </div>
          ) : null
        }
        fields={
          selected
            ? [
                { label: "抽奖单号", value: selected.drawNo, mono: true, copy: selected.drawNo },
                { label: "活动", value: selected.activityName || `#${selected.activityId}` },
                { label: "奖品类型", value: kindLabelOf(selected.prizeKind, catalogQuery.data?.kinds ?? []) },
                { label: "奖盘位置", value: String(selected.position + 1) },
                {
                  label: "消耗",
                  value: `${SOURCE_LABEL[selected.source] ?? selected.source}${selected.pointsCost ? `，${formatInt(selected.pointsCost)} 积分` : ""}`
                },
                { label: "抽奖时间", value: formatDateTime(selected.createdAt) },
                { label: "发放备注", value: selected.note, wide: true },
                { label: "处理人", value: selected.handledBy },
                { label: "处理时间", value: selected.handledAt ? formatDateTime(selected.handledAt) : undefined },
                { label: "请求 ID", value: selected.requestId, mono: true, copy: selected.requestId || null },
                { label: "设备", value: selected.deviceId, mono: true, copy: selected.deviceId || null },
                { label: "IP", value: selected.clientIp, mono: true, copy: selected.clientIp || null }
              ]
            : undefined
        }
        raw={selected}
        actions={
          selected?.status === "pending" ? (
            <Button size="sm" onClick={() => openFulfill(selected)}>
              <PackageCheck />
              标记已发放
            </Button>
          ) : undefined
        }
      >
        {selected ? (
          <>
            <ResultList results={selected.results} />
            {selected.trace ? (
              <div className="flex flex-col gap-2">
                <div className="text-xs font-medium text-muted-foreground">出奖方案</div>
                <DrawPlanView
                  plan={selected.trace}
                  signals={catalogQuery.data?.signals}
                  catalog={catalogQuery.data}
                  prizeName={prizeNameOf(selected)}
                  prizeKind={prizeKindOf(selected)}
                />
              </div>
            ) : null}
          </>
        ) : null}
      </RecordSheet>

      <ConfirmActionDialog
        open={Boolean(fulfilling)}
        onOpenChange={(open) => (!open ? setFulfilling(null) : undefined)}
        title="标记为已发放"
        description={fulfilling ? `${fulfilling.activityName || `活动 #${fulfilling.activityId}`}，奖品「${fulfilling.prizeName}」。确认后用户会收到「奖品已发放」的站内信。` : undefined}
        confirmLabel="确认发放"
        pending={fulfillMutation.isPending}
        note={{
          value: note,
          onChange: setNote,
          placeholder: "兑换码、快递单号等，会附在站内信里",
          hint: "选填，写给用户看",
          max: LIMITS.note
        }}
        onConfirm={submitFulfill}
      />
    </div>
  );
}
