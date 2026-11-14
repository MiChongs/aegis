"use client";

import { useMemo, useState } from "react";
import { ChevronRight, ListChecks } from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { DeletionRequestSheet } from "@/components/apps/account-deletion/account-deletion-request-sheet";
import {
  DeletionPager,
  DeletionStatusBadge,
  MODE_META,
  SOURCE_META,
  STATUSES,
  displayName,
  modeLabel,
  reasonText,
  sourceLabel,
  statusLabel
} from "@/components/apps/account-deletion/account-deletion-shared";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
import { AutoSkeleton } from "@/components/ui/auto-skeleton";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import type {
  DeletionMode,
  DeletionRequest,
  DeletionSource,
  DeletionStatus
} from "@/lib/api/account-deletion";
import { useDeletionRequestsQuery, useDeletionSettingsQuery } from "@/lib/account-deletion-hooks";
import { formatDateTimeMinute, formatRelative, formatShortDateTime } from "@/lib/datetime";
import { useAppKeyPermissionChecker } from "@/lib/app-permission-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;

/** 加载期间喂给骨架的占位行：长度接近真实值，内容不可见 */
const PLACEHOLDER_ROWS: DeletionRequest[] = Array.from({ length: 6 }, (_, index) => ({
  id: -1 - index,
  appId: 0,
  userId: 100000 + index,
  account: "placeholder_account",
  nickname: "占位昵称",
  mode: "cooling",
  status: "scheduled",
  source: "user",
  reasonCode: "",
  reasonText: "占位原因文字",
  coolingDays: 15,
  scheduledAt: "2026-01-01T00:00:00Z",
  reviewedBy: "",
  reviewNote: "",
  cancelledBy: "",
  cancelReason: "",
  failure: "",
  attempts: 0,
  verifiedBy: [],
  operator: "",
  ip: "",
  deviceId: "",
  device: "",
  userAgent: "",
  snapshot: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  userExists: true
}));

/**
 * 申请列表：状态 / 方式 / 来源筛选、关键词（账号、昵称、原因、用户 ID）、提交时间范围、分页。
 * 点一行打开详情抽屉，处置在抽屉里做。任何筛选变化都回到第 1 页。
 */
export function DeletionRequestsPanel({
  appKey,
  status,
  onStatusChange
}: {
  appKey: string;
  status?: DeletionStatus;
  onStatusChange: (status?: DeletionStatus) => void;
}) {
  const can = useAppKeyPermissionChecker(appKey);
  const canReview = can("app:user:deletion:review");
  const canExecute = can("app:user:deletion:execute");
  const [mode, setMode] = useState<DeletionMode | undefined>(undefined);
  const [source, setSource] = useState<DeletionSource | undefined>(undefined);
  const [keyword, setKeyword] = useState("");
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<DeletionRequest | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);

  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const settingsQuery = useDeletionSettingsQuery(appKey);
  const listQuery = useDeletionRequestsQuery(appKey, {
    status,
    mode,
    source,
    keyword: debouncedKeyword || undefined,
    from: range.start,
    to: range.end,
    page,
    limit: LIMIT
  });

  const loading = listQuery.isLoading;
  const items = useMemo(() => (loading ? PLACEHOLDER_ROWS : listQuery.data?.items ?? []), [loading, listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const filtered = Boolean(keyword || status || mode || source || range.start || range.end);
  const config = settingsQuery.data?.config;

  const resetPage = () => setPage(1);
  const open = (item: DeletionRequest) => {
    setSelected(item);
    setSheetOpen(true);
  };

  return (
    <SectionCard icon={<ListChecks className="size-4" />} title="注销申请">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            resetPage();
          }}
          placeholder="搜索账号、昵称、原因或用户 ID"
          className="h-8 w-full sm:w-60"
        />
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            onStatusChange(value === "all" ? undefined : (value as DeletionStatus));
            resetPage();
          }}
        >
          <SelectTrigger className="h-8 w-[7.5rem]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {STATUSES.map((item) => (
              <SelectItem key={item} value={item}>
                {statusLabel(item)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={mode ?? "all"}
          onValueChange={(value) => {
            setMode(value === "all" ? undefined : (value as DeletionMode));
            resetPage();
          }}
        >
          <SelectTrigger className="h-8 w-[7.5rem]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部方式</SelectItem>
            {(Object.keys(MODE_META) as DeletionMode[]).map((item) => (
              <SelectItem key={item} value={item}>
                {MODE_META[item].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={source ?? "all"}
          onValueChange={(value) => {
            setSource(value === "all" ? undefined : (value as DeletionSource));
            resetPage();
          }}
        >
          <SelectTrigger className="h-8 w-[7.5rem]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部来源</SelectItem>
            {(Object.keys(SOURCE_META) as DeletionSource[]).map((item) => (
              <SelectItem key={item} value={item}>
                {SOURCE_META[item].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <CommerceRangePicker
          value={range}
          onChange={(next) => {
            setRange(next);
            resetPage();
          }}
        />
      </div>

      {!loading && items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配的申请" : "暂无注销申请"}
        </p>
      ) : (
        <AutoSkeleton loading={loading}>
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>账号</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="hidden md:table-cell">方式与来源</TableHead>
                  <TableHead className="hidden lg:table-cell">原因</TableHead>
                  <TableHead className="text-right">时间</TableHead>
                  <TableHead className="w-0" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => {
                  const reason = reasonText(item, config);
                  return (
                    <TableRow key={item.id} className="cursor-pointer" onClick={() => !loading && open(item)}>
                      <TableCell>
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <span className="block max-w-28 truncate text-xs font-medium sm:max-w-36">{displayName(item)}</span>
                            {!item.userExists && item.status !== "completed" ? (
                              <Badge variant="secondary" size="sm">
                                已删除
                              </Badge>
                            ) : null}
                          </div>
                          {item.nickname && item.account ? (
                            <span className="block max-w-28 truncate font-mono text-[11px] text-muted-foreground sm:max-w-36">
                              {item.account}
                            </span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        <DeletionStatusBadge status={item.status} />
                        <p className="mt-1 text-[11px] text-muted-foreground md:hidden">{modeLabel(item.mode)}</p>
                      </TableCell>
                      <TableCell className="hidden whitespace-nowrap md:table-cell">
                        <div className="text-xs">
                          {modeLabel(item.mode)}
                          {item.mode === "cooling" && item.coolingDays ? (
                            <span className="text-muted-foreground">，{item.coolingDays} 天</span>
                          ) : null}
                        </div>
                        <div className="mt-0.5 text-[11px] text-muted-foreground">{sourceLabel(item.source)}</div>
                      </TableCell>
                      <TableCell className="hidden max-w-56 lg:table-cell">
                        <span className="line-clamp-2 text-xs text-muted-foreground">{reason || "未填写"}</span>
                      </TableCell>
                      <TableCell className="text-right text-xs whitespace-nowrap">
                        <RowTime item={item} />
                      </TableCell>
                      <TableCell className="pr-2">
                        <ChevronRight className="size-4 text-muted-foreground" />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </AutoSkeleton>
      )}

      {!loading ? <DeletionPager total={total} page={page} limit={LIMIT} onPage={setPage} /> : null}

      <DeletionRequestSheet
        appKey={appKey}
        request={selected}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        config={config}
        canReview={canReview}
        canExecute={canExecute}
      />
    </SectionCard>
  );
}

/** 时间列：进行中的看到期，结束的看结局时刻，下面一行是提交时间 */
function RowTime({ item }: { item: DeletionRequest }) {
  const primary =
    item.status === "scheduled" && item.scheduledAt
      ? { label: `${formatRelative(item.scheduledAt)}注销`, title: formatDateTimeMinute(item.scheduledAt) }
      : item.status === "completed" && item.completedAt
        ? { label: `注销于 ${formatShortDateTime(item.completedAt)}`, title: formatDateTimeMinute(item.completedAt) }
        : item.status === "cancelled" && item.cancelledAt
          ? { label: `撤销于 ${formatShortDateTime(item.cancelledAt)}`, title: formatDateTimeMinute(item.cancelledAt) }
          : item.status === "rejected" && item.reviewedAt
            ? { label: `驳回于 ${formatShortDateTime(item.reviewedAt)}`, title: formatDateTimeMinute(item.reviewedAt) }
            : null;
  return (
    <>
      {primary ? (
        <div className="tabular-nums" title={primary.title}>
          {primary.label}
        </div>
      ) : null}
      <div className={primary ? "mt-0.5 text-[11px] text-muted-foreground tabular-nums" : "tabular-nums text-muted-foreground"}>
        提交于 {formatShortDateTime(item.createdAt)}
      </div>
    </>
  );
}
