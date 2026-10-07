"use client";

import { useMemo, useState } from "react";
import { toast } from "sonner";
import { CalendarClock, Download, Pause, Pencil, Play, Rocket, Trash2, Undo2 } from "lucide-react";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import { ApiError } from "@/lib/api-client";
import type { Release, ReleaseFunnel } from "@/lib/api/types";
import {
  useDeleteReleaseMutation,
  usePublishReleaseMutation,
  useReleaseDetailQuery,
  useReleaseRolloutMutation,
  useReleaseStatsQuery,
  useTransitionReleaseMutation
} from "@/lib/release-hooks";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { formatDateTime } from "@/components/content/content-shared";
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
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RichContent } from "@/components/ui/rich-editor";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { cn } from "@/lib/utils";
import {
  ChannelDot,
  DATA_FONT,
  FieldRow,
  ROLLOUT_PRESETS,
  ReleaseStatusBadge,
  UpdateTypeBadge,
  formatBytes,
  localInputToIso,
  percent,
  platformLabel,
  visibilityLabel
} from "./release-shared";

const FUNNEL_SERIES: Array<{ key: keyof ReleaseFunnel; label: string; color: ThemedColor }> = [
  { key: "offered", label: "下发", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "downloaded", label: "下载", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "installed", label: "安装", color: { light: "#10b981", dark: "#34d399" } },
  { key: "failed", label: "失败", color: { light: "#f43f5e", dark: "#fb7185" } },
  { key: "dismissed", label: "跳过", color: { light: "#a1a1aa", dark: "#71717a" } }
];

/** 近 14 天补齐空白日：图上断档会被读成「那几天服务挂了」。 */
function fillDays(daily: Array<ReleaseFunnel & { day: string }>) {
  const byDay = new Map(daily.map((row) => [row.day, row]));
  const rows: Array<ReleaseFunnel & { label: string }> = [];
  const today = new Date();
  for (let i = 13; i >= 0; i--) {
    const date = new Date(today.getFullYear(), today.getMonth(), today.getDate() - i);
    const key = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
    const row = byDay.get(key);
    rows.push({
      label: key.slice(5),
      offered: row?.offered ?? 0,
      downloaded: row?.downloaded ?? 0,
      installed: row?.installed ?? 0,
      failed: row?.failed ?? 0,
      dismissed: row?.dismissed ?? 0
    });
  }
  return rows;
}

export function ReleaseDetail({
  appKey,
  releaseId,
  open,
  onOpenChange,
  onEdit
}: {
  appKey: string;
  releaseId: number | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onEdit: (release: Release) => void;
}) {
  const detailQuery = useReleaseDetailQuery(appKey, open ? releaseId : null);
  const statsQuery = useReleaseStatsQuery(appKey, open ? releaseId : null);
  const release = detailQuery.data;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-[94vw] max-w-2xl flex-col p-0">
        <SheetHeader className="shrink-0 border-b px-6 py-4">
          <SheetTitle className="flex items-center gap-2">
            {release ? (
              <>
                <span className={DATA_FONT}>{release.version}</span>
                <ReleaseStatusBadge release={release} />
              </>
            ) : (
              "版本详情"
            )}
          </SheetTitle>
          <SheetDescription>{release?.title || (release ? `版本码 ${release.versionCode}` : "正在加载")}</SheetDescription>
        </SheetHeader>
        <ScrollArea className="min-h-0 flex-1">
          {detailQuery.isLoading || !release ? (
            <div className="space-y-4 px-6 py-5">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-28 w-full" />
              <Skeleton className="h-64 w-full" />
            </div>
          ) : (
            <DetailBody appKey={appKey} release={release} stats={statsQuery.data} statsLoading={statsQuery.isLoading} onEdit={onEdit} onDeleted={() => onOpenChange(false)} />
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}

function DetailBody({
  appKey,
  release,
  stats,
  statsLoading,
  onEdit,
  onDeleted
}: {
  appKey: string;
  release: Release;
  stats?: { total: ReleaseFunnel; daily: Array<ReleaseFunnel & { day: string }> };
  statsLoading: boolean;
  onEdit: (release: Release) => void;
  onDeleted: () => void;
}) {
  const publish = usePublishReleaseMutation(appKey);
  const transition = useTransitionReleaseMutation(appKey);
  const remove = useDeleteReleaseMutation(appKey);
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [confirm, setConfirm] = useState<null | "revoke" | "delete">(null);

  const live = release.effectiveStatus === "published";
  const busy = publish.isPending || transition.isPending || remove.isPending;

  async function run(task: () => Promise<unknown>, success: string) {
    try {
      await task();
      toast.success(success);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  }

  const chartConfig = useMemo(() => buildChartConfig(FUNNEL_SERIES), []);
  const rows = useMemo(() => fillDays(stats?.daily ?? []), [stats]);
  const total = stats?.total;
  const t = release.targeting ?? {};
  const targetingRows = [
    t.regions?.length ? ["地区", t.regions.join("、")] : null,
    t.locales?.length ? ["语言", t.locales.join("、")] : null,
    t.deviceModels?.length ? ["机型", t.deviceModels.join("、")] : null,
    t.abis?.length ? ["ABI", t.abis.join("、")] : null,
    t.minOsVersion || t.maxOsVersion ? ["系统版本", `${t.minOsVersion || "不限"} 至 ${t.maxOsVersion || "不限"}`] : null,
    t.minSourceVersionCode || t.maxSourceVersionCode
      ? ["来源版本码", `${t.minSourceVersionCode || "不限"} 至 ${t.maxSourceVersionCode || "不限"}`]
      : null,
    t.testerUserIds?.length || t.testerDeviceIds?.length
      ? ["内测名单", `${t.testerUserIds?.length ?? 0} 个用户，${t.testerDeviceIds?.length ?? 0} 台设备`]
      : null,
    t.excludeUserIds?.length || t.excludeDeviceIds?.length
      ? ["排除名单", `${t.excludeUserIds?.length ?? 0} 个用户，${t.excludeDeviceIds?.length ?? 0} 台设备`]
      : null
  ].filter(Boolean) as Array<[string, string]>;

  return (
    <div className="space-y-6 px-6 py-5">
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" onClick={() => onEdit(release)}>
          <Pencil className="size-3.5" />
          编辑
        </Button>
        {release.status === "draft" || release.status === "revoked" || release.status === "scheduled" ? (
          <>
            <Button
              size="sm"
              disabled={busy || release.assets.length === 0}
              onClick={() => void run(() => publish.mutateAsync({ releaseId: release.id }), "已发布")}
            >
              <Rocket className="size-3.5" />
              立即发布
            </Button>
            <Button size="sm" variant="outline" disabled={busy || release.assets.length === 0} onClick={() => setScheduleOpen(true)}>
              <CalendarClock className="size-3.5" />
              定时发布
            </Button>
          </>
        ) : null}
        {live || release.status === "scheduled" ? (
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => void run(() => transition.mutateAsync({ releaseId: release.id, action: "pause" }), "已暂停下发")}
          >
            <Pause className="size-3.5" />
            暂停
          </Button>
        ) : null}
        {release.status === "paused" ? (
          <Button
            size="sm"
            disabled={busy}
            onClick={() => void run(() => transition.mutateAsync({ releaseId: release.id, action: "resume" }), "已恢复下发")}
          >
            <Play className="size-3.5" />
            恢复
          </Button>
        ) : null}
        {release.status !== "revoked" && release.status !== "draft" ? (
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirm("revoke")}>
            <Undo2 className="size-3.5" />
            撤回
          </Button>
        ) : null}
        {!live ? (
          <Button size="sm" variant="ghost" className="text-destructive" disabled={busy} onClick={() => setConfirm("delete")}>
            <Trash2 className="size-3.5" />
            删除
          </Button>
        ) : null}
      </div>
      {release.assets.length === 0 ? <p className="-mt-3 text-xs text-muted-foreground">添加安装包后才能发布</p> : null}

      {release.status !== "draft" && release.status !== "revoked" ? <RolloutControl appKey={appKey} release={release} /> : null}

      <div className="rounded-xl border px-4 py-2">
        <FieldRow label="版本码">
          <span className={DATA_FONT}>{release.versionCode}</span>
        </FieldRow>
        <FieldRow label="平台">{platformLabel(release.platform)}</FieldRow>
        <FieldRow label="更新类型">
          <UpdateTypeBadge value={release.updateType} />
        </FieldRow>
        {release.minSupportedCode ? (
          <FieldRow label="最低支持版本码">
            <span className={DATA_FONT}>{release.minSupportedCode}</span>
          </FieldRow>
        ) : null}
        <FieldRow label="渠道">
          {release.channel ? (
            <span className="inline-flex items-center gap-1.5">
              <ChannelDot color={release.channel.color} />
              {release.channel.name}
            </span>
          ) : (
            "不限渠道"
          )}
        </FieldRow>
        <FieldRow label="可见范围">{visibilityLabel(release.visibility)}</FieldRow>
        {release.status === "scheduled" && release.publishAt ? (
          <FieldRow label="定时发布">{formatDateTime(release.publishAt)}</FieldRow>
        ) : null}
        <FieldRow label="发布时间">{formatDateTime(release.publishedAt)}</FieldRow>
        <FieldRow label="更新时间">{formatDateTime(release.updatedAt)}</FieldRow>
        {targetingRows.map(([label, value]) => (
          <FieldRow key={label} label={label}>
            {value}
          </FieldRow>
        ))}
      </div>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold">更新漏斗</h3>
        <div className="grid grid-cols-3 gap-2 sm:grid-cols-5">
          {FUNNEL_SERIES.map((series) => (
            <div key={series.key} className="rounded-lg border px-3 py-2">
              <div className="text-[11px] text-muted-foreground">{series.label}</div>
              <div className={cn("text-lg font-semibold", DATA_FONT)}>{statsLoading ? "—" : (total?.[series.key] ?? 0)}</div>
            </div>
          ))}
        </div>
        {total ? (
          <p className="text-xs text-muted-foreground">
            下载率 {percent(total.downloaded, total.offered)}，安装率 {percent(total.installed, total.offered)}
          </p>
        ) : null}
        <ChartCard
          title="近 14 天"
          loading={statsLoading}
          empty={rows.every((row) => FUNNEL_SERIES.every((series) => row[series.key] === 0))}
          height={220}
        >
          <ChartContainer config={chartConfig} className="h-[220px] w-full">
            <LineChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              {FUNNEL_SERIES.map((series) => (
                <Line key={series.key} dataKey={series.key} type="monotone" stroke={`var(--color-${series.key})`} strokeWidth={2} dot={false} />
              ))}
              <ChartLegend content={<ChartLegendContent />} />
            </LineChart>
          </ChartContainer>
        </ChartCard>
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold">安装包</h3>
        {release.assets.length === 0 ? (
          <p className="text-sm text-muted-foreground">暂无安装包</p>
        ) : (
          <div className="divide-y rounded-xl border">
            {release.assets.map((asset) => (
              <div key={asset.id || asset.abi} className="flex items-center gap-3 px-4 py-2.5">
                <Badge variant="outline" className={DATA_FONT}>
                  {asset.abi}
                </Badge>
                <div className="min-w-0 flex-1 text-xs text-muted-foreground">
                  <div className="truncate">{asset.label || formatBytes(asset.fileSize)}</div>
                  {asset.sha256 ? <div className={cn("truncate", DATA_FONT)}>{asset.sha256}</div> : null}
                </div>
                <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{asset.downloadCount} 次</span>
                {asset.downloadUrl ? (
                  <Button size="icon" variant="ghost" className="size-8" asChild>
                    <a href={asset.downloadUrl} target="_blank" rel="noreferrer" aria-label="下载">
                      <Download className="size-3.5" />
                    </a>
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold">更新说明</h3>
        {release.notes ? (
          <RichContent html={release.notes} className="rounded-xl border px-4 py-3" />
        ) : (
          <p className="text-sm text-muted-foreground">未填写</p>
        )}
      </section>

      <ScheduleDialog
        open={scheduleOpen}
        onOpenChange={setScheduleOpen}
        busy={publish.isPending}
        onSubmit={async (iso) => {
          await run(() => publish.mutateAsync({ releaseId: release.id, publishAt: iso }), "已设定定时发布");
          setScheduleOpen(false);
        }}
      />

      <AlertDialog open={confirm !== null} onOpenChange={(value) => !value && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm === "delete" ? `删除 ${release.version}` : `撤回 ${release.version}`}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "delete" ? "删除后无法恢复，统计数据一并清除。" : "撤回后不再向任何客户端下发，已安装的用户不受影响。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirm === "delete") {
                  void run(() => remove.mutateAsync(release.id), "已删除").then(onDeleted);
                } else {
                  void run(() => transition.mutateAsync({ releaseId: release.id, action: "revoke" }), "已撤回");
                }
                setConfirm(null);
              }}
            >
              {confirm === "delete" ? "删除" : "撤回"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function RolloutControl({ appKey, release }: { appKey: string; release: Release }) {
  const rollout = useReleaseRolloutMutation(appKey);
  const [value, setValue] = useState(release.rolloutPct);
  const [synced, setSynced] = useState(release.rolloutPct);
  if (synced !== release.rolloutPct) {
    setSynced(release.rolloutPct);
    setValue(release.rolloutPct);
  }
  const dirty = value !== release.rolloutPct;

  async function apply(pct: number) {
    try {
      await rollout.mutateAsync({ releaseId: release.id, rolloutPct: pct });
      toast.success(`灰度已调整为 ${pct}%`);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "调整失败");
    }
  }

  return (
    <div className="space-y-2 rounded-xl border px-4 py-3">
      <div className="flex items-center justify-between">
        <Label className="text-xs">灰度比例</Label>
        <span className={cn("text-sm font-semibold", DATA_FONT)}>{value}%</span>
      </div>
      <Slider value={[value]} min={0} max={100} step={1} onValueChange={(next) => setValue(next[0] ?? 0)} />
      <div className="flex flex-wrap items-center gap-1.5">
        {ROLLOUT_PRESETS.map((pct) => (
          <Button
            key={pct}
            size="sm"
            variant={value === pct ? "default" : "outline"}
            className="h-7 px-2.5 text-xs"
            onClick={() => setValue(pct)}
          >
            {pct}%
          </Button>
        ))}
        <div className="flex-1" />
        <Button size="sm" className="h-7" disabled={!dirty || rollout.isPending} onClick={() => void apply(value)}>
          应用
        </Button>
      </div>
    </div>
  );
}

function ScheduleDialog({
  open,
  onOpenChange,
  busy,
  onSubmit
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  busy: boolean;
  onSubmit: (iso: string) => Promise<void>;
}) {
  const [value, setValue] = useState("");
  // 「是否在一分钟之后」在输入变化时判一次：渲染期间读当前时间会让结果随重渲染漂移
  const [valid, setValid] = useState(false);
  const iso = localInputToIso(value);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>定时发布</DialogTitle>
          <DialogDescription>到点后自动开始下发</DialogDescription>
        </DialogHeader>
        <Input
          type="datetime-local"
          value={value}
          onChange={(event) => {
            const next = localInputToIso(event.target.value);
            setValue(event.target.value);
            setValid(Boolean(next && new Date(next).getTime() > Date.now() + 60_000));
          }}
        />
        {value && !valid ? <p className="text-xs text-destructive">请选择至少一分钟之后的时间</p> : null}
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={!valid || busy} onClick={() => iso && void onSubmit(iso)}>
            确定
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
