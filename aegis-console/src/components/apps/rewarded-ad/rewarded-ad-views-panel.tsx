"use client";

import { useMemo, useState } from "react";
import { HandCoins, ListChecks, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { formatCardDate } from "@/components/apps/card-key/card-key-shared";
import {
  REWARDED_AD_STATUS_OPTIONS,
  RewardedAdStatusBadge,
  rewardedAdReasonLabel
} from "@/components/apps/rewarded-ad/rewarded-ad-shared";
import { CommerceRangePicker, type CommerceRange } from "@/components/commerce/commerce-range-picker";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import type { RewardedAdView, RewardedAdViewStatus } from "@/lib/api/rewarded-ad";
import {
  useGrantRewardedAdViewMutation,
  useRewardedAdConfigQuery,
  useRewardedAdViewsQuery
} from "@/lib/rewarded-ad-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";

const PAGE_SIZE = 20;

/**
 * 观看记录。
 *
 * 每一行是一次观看（一个平台 transId），两列「回调 / 上报」分别说明两方各自到没到 ——
 * 「用户说看完没到账」时，先看是哪一方没来：回调没来是平台或签名的问题，
 * 上报没来是客户端的问题，两个都到了还没发就看原因列。
 *
 * 待确认 / 未发放的记录可以补发：补发不再等另一方、不再看限额，一条记录只能补发一次。
 */
export function RewardedAdViewsPanel({ appKey }: { appKey: string }) {
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<RewardedAdViewStatus | undefined>(undefined);
  const [scene, setScene] = useState<string | undefined>(undefined);
  const [range, setRange] = useState<CommerceRange>({});
  const [page, setPage] = useState(1);
  const [grantTarget, setGrantTarget] = useState<RewardedAdView | null>(null);
  const [grantScene, setGrantScene] = useState<string>("");

  const debouncedKeyword = useDebouncedValue(keyword, 300);
  const configQuery = useRewardedAdConfigQuery(appKey);
  const listQuery = useRewardedAdViewsQuery(appKey, {
    status,
    scene,
    keyword: debouncedKeyword || undefined,
    start: range.start,
    end: range.end,
    page,
    limit: PAGE_SIZE
  });
  const grantMutation = useGrantRewardedAdViewMutation(appKey);

  const items = useMemo(() => listQuery.data?.items ?? [], [listQuery.data]);
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, listQuery.data?.totalPages ?? Math.ceil(total / PAGE_SIZE));
  const scenes = configQuery.data?.scenes ?? [];
  const filtered = Boolean(keyword || status || scene || range.start || range.end);

  const openGrant = (view: RewardedAdView) => {
    setGrantTarget(view);
    setGrantScene(view.scene || scenes[0]?.key || "");
  };

  const confirmGrant = async () => {
    if (!grantTarget) return;
    try {
      await grantMutation.mutateAsync({ viewId: grantTarget.id, scene: grantScene || undefined });
      toast.success("已补发");
      setGrantTarget(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "补发失败");
    }
  };

  return (
    <SectionCard icon={<ListChecks className="size-4" />} title="观看记录">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value);
            setPage(1);
          }}
          placeholder="搜索 transId 或账号"
          className="h-8 w-56"
        />
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            setStatus(value === "all" ? undefined : (value as RewardedAdViewStatus));
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-32">
            <SelectValue placeholder="全部状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {REWARDED_AD_STATUS_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={scene ?? "all"}
          onValueChange={(value) => {
            setScene(value === "all" ? undefined : value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-40">
            <SelectValue placeholder="全部场景" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部场景</SelectItem>
            {scenes.map((item) => (
              <SelectItem key={item.key} value={item.key}>
                {item.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <CommerceRangePicker
          value={range}
          onChange={(next) => {
            setRange(next);
            setPage(1);
          }}
        />
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          {filtered ? "暂无匹配记录" : "暂无观看记录"}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>账号</TableHead>
              <TableHead>场景</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>实际发放</TableHead>
              <TableHead>回调 / 上报</TableHead>
              <TableHead className="text-right">时间</TableHead>
              <TableHead className="w-16" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className="text-xs">
                  {item.account || (item.userId ? `#${item.userId}` : "—")}
                  <p className="mt-0.5 max-w-44 truncate font-mono text-[11px] text-muted-foreground" title={item.transId}>
                    {item.transId}
                  </p>
                </TableCell>
                <TableCell className="text-xs">
                  {item.sceneName || item.scene || "—"}
                  {item.placementId ? (
                    <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{item.placementId}</p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <RewardedAdStatusBadge status={item.status} />
                  {item.reason ? (
                    <p className="mt-1 text-[11px] text-muted-foreground">{rewardedAdReasonLabel(item.reason)}</p>
                  ) : null}
                </TableCell>
                <TableCell>
                  {item.results.length === 0 ? (
                    <span className="text-xs text-muted-foreground">—</span>
                  ) : (
                    <div className="flex flex-wrap gap-1">
                      {item.results.map((result) => (
                        <Badge
                          key={result.type}
                          variant="outline"
                          size="sm"
                          className="font-normal"
                          title={result.transactionNo ? `流水 ${result.transactionNo}` : undefined}
                        >
                          {result.detail || result.label}
                        </Badge>
                      ))}
                    </div>
                  )}
                  {item.operator ? (
                    <p className="mt-1 text-[11px] text-muted-foreground">补发：{item.operator}</p>
                  ) : null}
                </TableCell>
                <TableCell className="text-[11px] text-muted-foreground">
                  <div>回调 {item.serverVerifiedAt ? formatCardDate(item.serverVerifiedAt) : "未到"}</div>
                  <div>
                    上报 {item.clientReportedAt ? formatCardDate(item.clientReportedAt) : "未到"}
                    {item.clientIp ? <span className="ml-1 font-mono">{item.clientIp}</span> : null}
                  </div>
                </TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {formatCardDate(item.grantedAt || item.createdAt)}
                </TableCell>
                <TableCell className="text-right">
                  {item.status !== "granted" && item.userId ? (
                    <Button size="xs" variant="outline" onClick={() => openGrant(item)}>
                      <HandCoins className="size-3" />
                      补发
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {total > 0 ? (
        <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
          <span className="tabular-nums">
            共 {total} 条 · 第 {page}/{totalPages} 页
          </span>
          <div className="flex gap-1.5">
            <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((v) => v - 1)}>
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={page >= totalPages}
              onClick={() => setPage((v) => v + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      ) : null}

      <AlertDialog open={grantTarget !== null} onOpenChange={(open) => (!open ? setGrantTarget(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>补发奖励</AlertDialogTitle>
            <AlertDialogDescription>
              {grantTarget?.account || `#${grantTarget?.userId ?? ""}`} · 不再校验限额
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Select value={grantScene} onValueChange={setGrantScene}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue placeholder="选择场景" />
            </SelectTrigger>
            <SelectContent>
              {scenes.map((item) => (
                <SelectItem key={item.key} value={item.key}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={!grantScene || grantMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void confirmGrant();
              }}
            >
              {grantMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              补发
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionCard>
  );
}
