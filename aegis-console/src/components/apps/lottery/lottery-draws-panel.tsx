"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { CircleHelp, Loader2, PackageCheck, ReceiptText, Route, Search, X } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { AdPolicyPager } from "@/components/apps/ad-policy/ad-policy-pager";
import { LIMITS } from "@/components/apps/lottery/lottery-activity-form";
import { DrawPlanView } from "@/components/apps/lottery/lottery-plan-view";
import { RiskActionChip, riskSignalOf, riskSignals } from "@/components/apps/lottery/lottery-risk-shared";
import {
  ConversionBadge,
  DRAW_STATUSES,
  DrawStatusBadge,
  SOURCE_LABEL,
  drawStatusLabel,
  formatCount,
  kindLabelOf
} from "@/components/apps/lottery/lottery-shared";
import { formatStoreDate } from "@/components/apps/points-store/points-store-shared";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api/client";
import type { LotteryDraw, LotteryDrawParams, LotteryDrawStatus, LotterySource } from "@/lib/api/lottery";
import {
  useFulfillLotteryDrawMutation,
  useLotteryActivitiesQuery,
  useLotteryCatalogQuery,
  useLotteryDrawsQuery
} from "@/lib/lottery-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const LIMIT = 20;
const SOURCES: LotterySource[] = ["free", "bonus", "points"];

type Outcome = NonNullable<LotteryDrawParams["outcome"]>;
const OUTCOMES: Array<{ value: Outcome; label: string }> = [
  { value: "pity", label: "保底" },
  { value: "fallback", label: "改发兜底" },
  { value: "conversion", label: "已拥有改发" }
];

/** 检索式说明：点一下示例填进输入框 */
const SEARCH_SYNTAX: Array<{ example: string; label: string }> = [
  { example: "LTD20261102…", label: "单号，完整单号最快" },
  { example: "203.0.113.7", label: "IP" },
  { example: "10086", label: "记录 ID、用户 ID、数字账号或奖品名里的数" },
  { example: "月度会员", label: "奖品名、账号或昵称" },
  { example: "user:张三", label: "只查账号或昵称" },
  { example: "prize:\"会员 15 天\"", label: "只查奖品名，引号里可以有空格" },
  { example: "device:abc123", label: "设备号" },
  { example: "user:张三 prize:会员", label: "空格分隔的多段同时满足" }
];

/**
 * 抽奖记录。
 *
 * 奖品列是抽中那一刻的快照（奖品后来被改、被删都不影响），「实际发放」是 `results`：
 * 两者的差别正是排障要看的（例如限量奖恰好被抢光时改发了兜底奖）。
 * 人工发放的奖品在这里「标记已发放」，写给用户的话进站内信。只有待发放的能处理。
 */
export function LotteryDrawsPanel({
  appKey,
  activityId,
  status,
  risk,
  keyword,
  onActivityIdChange,
  onStatusChange,
  onRiskChange,
  onKeywordChange
}: {
  appKey: string;
  activityId?: number;
  status?: LotteryDrawStatus;
  /** any / clean / 信号键 */
  risk?: string;
  keyword: string;
  onActivityIdChange: (activityId?: number) => void;
  onStatusChange: (status?: LotteryDrawStatus) => void;
  onRiskChange: (risk?: string) => void;
  onKeywordChange: (keyword: string) => void;
}) {
  const setKeyword = onKeywordChange;
  const [outcome, setOutcome] = useState<Outcome | undefined>(undefined);
  const [source, setSource] = useState<LotterySource | undefined>(undefined);
  const [winning, setWinning] = useState(false);
  const [user, setUser] = useState<PickedUser | null>(null);
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [handling, setHandling] = useState<LotteryDraw | null>(null);
  const [tracing, setTracing] = useState<LotteryDraw | null>(null);
  const [note, setNote] = useState("");

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const listQuery = useLotteryDrawsQuery(appKey, {
    activityId,
    userId: user?.id,
    status,
    source,
    winning: winning || undefined,
    keyword: debouncedKeyword.trim() || undefined,
    risk,
    outcome,
    start: range.start,
    end: range.end,
    page,
    limit: LIMIT
  });
  const fulfillMutation = useFulfillLotteryDrawMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? 1);
  const filtered = Boolean(keyword || activityId || status || source || winning || user || range.start || range.end || risk || outcome);
  const kinds = catalogQuery.data?.kinds ?? [];
  const catalog = catalogQuery.data;
  const activityPrizes = (activityId: number) =>
    activitiesQuery.data?.find((activity) => activity.id === activityId)?.prizes ?? [];
  const selectableSignals = riskSignals(catalog).filter((spec) => !spec.hidden);

  const userHref = (userId: number) => `/app-users/${encodeURIComponent(appKey)}/${userId}?tab=assets`;

  const openFulfill = (draw: LotteryDraw) => {
    setNote("");
    setHandling(draw);
  };

  const submitFulfill = async () => {
    if (!handling) return;
    try {
      await fulfillMutation.mutateAsync({ drawId: handling.id, note: note.trim() });
      toast.success("已标记为已发放");
      setHandling(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  return (
    <SectionCard icon={<ReceiptText className="size-4" />} title="抽奖记录">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-80">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
            placeholder="单号、账号、奖品、IP，或 user: prize: device:"
            className="h-8 pl-8 pr-14 font-mono text-xs"
          />
          <div className="absolute inset-y-0 right-1 flex items-center gap-0.5">
            {keyword ? (
              <button
                type="button"
                onClick={() => {
                  setKeyword("");
                  setPage(1);
                }}
                className="grid size-6 place-items-center rounded text-muted-foreground hover:text-foreground"
                aria-label="清空"
              >
                <X className="size-3.5" />
              </button>
            ) : null}
            <Popover>
              <PopoverTrigger asChild>
                <button type="button" className="grid size-6 place-items-center rounded text-muted-foreground hover:text-foreground" aria-label="检索语法">
                  <CircleHelp className="size-3.5" />
                </button>
              </PopoverTrigger>
              <PopoverContent align="end" className="w-80 p-2">
                <p className="px-1.5 pb-1.5 text-[11px] font-medium">检索语法</p>
                {SEARCH_SYNTAX.map((item) => (
                  <button
                    key={item.example}
                    type="button"
                    onClick={() => {
                      if (!item.example.includes("…")) {
                        setKeyword(item.example);
                        setPage(1);
                      }
                    }}
                    className="flex w-full items-baseline gap-2 rounded-md px-1.5 py-1 text-left hover:bg-muted"
                  >
                    <span className="shrink-0 font-mono text-[11px]">{item.example}</span>
                    <span className="truncate text-[10px] text-muted-foreground">{item.label}</span>
                  </button>
                ))}
                <p className="px-1.5 pt-1.5 text-[10px] text-muted-foreground">每一段都走索引；账号与昵称先解析成用户再查记录</p>
              </PopoverContent>
            </Popover>
          </div>
        </div>
        <Select
          value={activityId ? String(activityId) : "all"}
          onValueChange={(value) => {
            onActivityIdChange(value === "all" ? undefined : Number(value));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-44">
            <SelectValue placeholder="全部活动" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部活动</SelectItem>
            {(activitiesQuery.data ?? []).map((activity) => (
              <SelectItem key={activity.id} value={String(activity.id)}>
                {activity.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            onStatusChange(value === "all" ? undefined : (value as LotteryDrawStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {DRAW_STATUSES.map((item) => (
              <SelectItem key={item} value={item}>
                {drawStatusLabel(item)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={source ?? "all"}
          onValueChange={(value) => {
            setSource(value === "all" ? undefined : (value as LotterySource));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部来源</SelectItem>
            {SOURCES.map((item) => (
              <SelectItem key={item} value={item}>
                {SOURCE_LABEL[item]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {/* UserPicker 的触发器是 w-full，套一层定宽容器 */}
        <div className="w-52">
          <UserPicker
            appKey={appKey}
            value={user}
            onChange={(next) => {
              setUser(next);
              setPage(1);
            }}
            placeholder="全部用户"
          />
        </div>
        <CommerceRangePicker
          value={range}
          onChange={(next) => {
            setRange(next);
            setPage(1);
          }}
        />
        <Select
          value={risk ?? "all"}
          onValueChange={(value) => {
            onRiskChange(value === "all" ? undefined : value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部风控</SelectItem>
            <SelectItem value="any">命中风控</SelectItem>
            <SelectItem value="clean">未命中</SelectItem>
            {selectableSignals.map((spec) => (
              <SelectItem key={spec.key} value={spec.key}>
                命中：{spec.label}
              </SelectItem>
            ))}
            {risk && !["any", "clean"].includes(risk) && !selectableSignals.some((spec) => spec.key === risk) ? (
              <SelectItem value={risk}>命中：{riskSignalOf(risk, catalog).label}</SelectItem>
            ) : null}
          </SelectContent>
        </Select>
        <Select
          value={outcome ?? "all"}
          onValueChange={(value) => {
            setOutcome(value === "all" ? undefined : (value as Outcome));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部结果</SelectItem>
            {OUTCOMES.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <label className="flex items-center gap-2 text-xs font-medium">
          <Switch
            checked={winning}
            onCheckedChange={(next) => {
              setWinning(next);
              setPage(1);
            }}
            aria-label="只看中奖"
          />
          只看中奖
        </label>
      </div>

      <div className="mb-3 flex items-center gap-2 text-[11px] text-muted-foreground">
        <span>
          共 <span className="font-mono tabular-nums text-foreground">{formatCount(total)}</span> 条
        </span>
        {listQuery.isFetching && !listQuery.isLoading ? <Loader2 className="size-3 animate-spin" /> : null}
        {filtered ? (
          <button
            type="button"
            className="ml-auto hover:text-foreground"
            onClick={() => {
              setKeyword("");
              onActivityIdChange(undefined);
              onStatusChange(undefined);
              onRiskChange(undefined);
              setSource(undefined);
              setOutcome(undefined);
              setWinning(false);
              setUser(null);
              setRange({});
              setPage(1);
            }}
          >
            清除全部筛选
          </button>
        ) : null}
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无抽奖记录"}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>奖品</TableHead>
                <TableHead>账号</TableHead>
                <TableHead>活动</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>实际发放</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">时间</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((draw) => (
                <TableRow key={draw.id}>
                  <TableCell className="max-w-56">
                    <div className="flex items-center gap-1.5">
                      <span className={draw.prizeKind === "none" ? "truncate text-xs text-muted-foreground" : "truncate text-xs font-medium"}>
                        {draw.prizeName}
                      </span>
                      {draw.pity ? (
                        <Badge variant="info" size="sm" className="shrink-0">
                          保底
                        </Badge>
                      ) : null}
                      {draw.conversion ? <ConversionBadge /> : null}
                      {draw.riskAction ? (
                        <span title={(draw.riskRules ?? []).map((key) => riskSignalOf(key, catalog).label).join("、")}>
                          <RiskActionChip action={draw.riskAction} catalog={catalog} className="shrink-0" />
                        </span>
                      ) : null}
                    </div>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">{kindLabelOf(draw.prizeKind, kinds)}</p>
                    <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{draw.drawNo}</p>
                  </TableCell>
                  <TableCell className="text-xs">
                    <Link href={userHref(draw.userId)} className="font-medium hover:underline">
                      {draw.account || `#${draw.userId}`}
                    </Link>
                    {draw.nickname ? <p className="mt-0.5 text-[11px] text-muted-foreground">{draw.nickname}</p> : null}
                  </TableCell>
                  <TableCell className="max-w-40">
                    <button
                      type="button"
                      className="truncate text-left text-xs hover:underline"
                      onClick={() => {
                        onActivityIdChange(draw.activityId);
                        setPage(1);
                      }}
                    >
                      {draw.activityName || `#${draw.activityId}`}
                    </button>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs">
                    {SOURCE_LABEL[draw.source] ?? draw.source}
                    {draw.ticket ? (
                      <p className="mt-0.5 text-[11px] text-muted-foreground">
                        {draw.ticket.originLabel}
                        {draw.ticket.paid ? "（兑换）" : ""}
                      </p>
                    ) : null}
                    {draw.pointsCost ? (
                      <p className="mt-0.5 text-[11px] tabular-nums text-muted-foreground">
                        {formatCount(draw.pointsCost)} 积分
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="max-w-72">
                    {draw.results?.length ? (
                      <div className="flex flex-wrap gap-1">
                        {draw.results.map((result, index) => (
                          <Badge
                            key={`${result.type}-${index}`}
                            variant={result.pending ? "warning" : "outline"}
                            size="sm"
                            className="font-normal"
                            title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
                          >
                            {result.detail || result.label}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                    {draw.conversion ? (
                      <p className="mt-1 text-[11px] text-muted-foreground" title={draw.conversion.message}>
                        {draw.conversion.reason === "feature" ? "已永久拥有该功能" : "永久会员已包含"}，改发 {draw.conversion.label}
                      </p>
                    ) : null}
                    {draw.note ? <p className="mt-1 text-[11px] text-muted-foreground">{draw.note}</p> : null}
                  </TableCell>
                  <TableCell>
                    <DrawStatusBadge status={draw.status} />
                    {draw.handledBy ? <p className="mt-0.5 text-[11px] text-muted-foreground">{draw.handledBy}</p> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-right text-xs text-muted-foreground">
                    {formatStoreDate(draw.createdAt)}
                    {draw.clientIp ? <p className="mt-0.5 font-mono text-[11px]">{draw.clientIp}</p> : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {draw.trace ? (
                      <Button size="sm" variant="ghost" className="h-7" onClick={() => setTracing(draw)}>
                        <Route className="size-3.5" /> 方案
                      </Button>
                    ) : null}
                    {draw.status === "pending" ? (
                      <Button size="sm" variant="outline" className="h-7" onClick={() => openFulfill(draw)}>
                        <PackageCheck className="size-3.5" /> 标记已发放
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <AdPolicyPager total={total} page={page} totalPages={totalPages} onPage={setPage} />

      <Dialog open={Boolean(tracing)} onOpenChange={(open) => !open && setTracing(null)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>出奖方案</DialogTitle>
            <DialogDescription asChild>
              {tracing ? (
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                  <span className="font-mono">{tracing.drawNo}</span>
                  <span>{tracing.account || `#${tracing.userId}`}</span>
                  <span>{tracing.activityName || `#${tracing.activityId}`}</span>
                  <span>{formatStoreDate(tracing.createdAt)}</span>
                  {tracing.clientIp ? <span className="font-mono">{tracing.clientIp}</span> : null}
                  {tracing.deviceId ? (
                    <span className="max-w-48 truncate font-mono" title={tracing.deviceId}>
                      {tracing.deviceId}
                    </span>
                  ) : null}
                </div>
              ) : (
                <span />
              )}
            </DialogDescription>
          </DialogHeader>
          {tracing?.trace ? (
            <DrawPlanView
              plan={tracing.trace}
              signals={catalog?.signals}
              catalog={catalog}
              prizeName={(id, position) =>
                activityPrizes(tracing.activityId).find((prize) => prize.id === id)?.name ||
                (tracing.trace?.picked === id ? tracing.prizeName : `位置 ${position + 1}`)
              }
              prizeKind={(id) =>
                activityPrizes(tracing.activityId).find((prize) => prize.id === id)?.kind ||
                (tracing.trace?.picked === id ? tracing.prizeKind : undefined)
              }
            />
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(handling)} onOpenChange={(open) => !open && setHandling(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>标记为已发放</DialogTitle>
            <DialogDescription>
              {handling ? `${handling.account || `#${handling.userId}`}，${handling.prizeName}` : null}
            </DialogDescription>
          </DialogHeader>
          <Textarea
            value={note}
            rows={3}
            maxLength={LIMITS.note}
            onChange={(event) => setNote(event.target.value)}
            placeholder="兑换码、快递单号"
          />
          <DialogFooter>
            <Button variant="ghost" size="sm" onClick={() => setHandling(null)}>
              返回
            </Button>
            <Button size="sm" onClick={submitFulfill} disabled={fulfillMutation.isPending}>
              {fulfillMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              确认发放
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SectionCard>
  );
}
