"use client";

import { useMemo, useState } from "react";
import {
  Clapperboard,
  Database,
  Layers,
  ListFilter,
  Loader2,
  Monitor,
  Pause,
  Play,
  RefreshCw,
  Smartphone,
  Tablet,
  Trash2,
  Tv
} from "lucide-react";
import { toast } from "sonner";
import {
  CLOUD_SYNC_COLLECTIONS,
  CLOUD_SYNC_DOMAINS,
  COLLECTION_KIND_LABELS,
  collectionName,
  formatBytes,
  planAllows,
  usagePercent
} from "@/components/apps/cloud-storage/cloud-storage-shared";
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
import { JsonViewer } from "@/components/ui/json-viewer";
import type {
  CloudAdminUser,
  CloudCollectionUsage,
  CloudSyncActivity,
  CloudSyncDevice,
  CloudSyncRecord
} from "@/lib/api/cloud-storage";
import { useCloudStorageUserSyncRecordsQuery, useRemoveCloudStorageUserDeviceMutation } from "@/lib/cloud-storage-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import {
  FilterBar,
  FilterSelect,
  InfoGrid,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  ToneIcon,
  errorMessage,
  formatInt,
  useDebouncedValue,
  type Column
} from "./kit";

/**
 * 云服务（docs/cloud-service.md）：用户详情「云存储」页签里与同步有关的几块。
 *
 *   档位       此刻生效的档位与它给的限制、同步用量（记录数 / 设备数 / 空间里同步占的那部分）
 *   设备       登记过的同步设备：在线、正在播放、最后同步、写了多少；移除即腾出名额
 *   集合       每个集合有多少条、按设备怎么分布；点一下即按它筛选下方记录
 *   同步记录   含墓碑，按修改时间倒序；详情里看 payload
 *
 * 档位外的集合（allowed 为假）照常列出：降档后已经同步上来的不删，只是不再接受新写入。
 * 管理端账目接口不填 allowed，这里按生效档位的 collections 自己算。
 */

/** 设备分布的颜色：按设备在列表里的顺序取，同一台设备在集合条与图例里颜色一致。 */
const DEVICE_COLORS = ["bg-sky-500", "bg-violet-500", "bg-emerald-500", "bg-amber-500", "bg-rose-500", "bg-cyan-500", "bg-indigo-500"];
const UNKNOWN_DEVICE_COLOR = "bg-muted-foreground/40";

const ACTIVITY_LABELS: Record<string, string> = {
  playing: "播放中",
  paused: "已暂停",
  reading: "阅读中",
  idle: "空闲"
};

export type SyncFilter = { collection: string; deviceId: string };

/** 这个用户有没有任何同步数据：都没有时设备 / 集合 / 记录三块不渲染，只留档位卡。 */
export function hasSyncData(state: CloudAdminUser) {
  return Boolean((state.devices?.length ?? 0) > 0 || (state.collections?.length ?? 0) > 0 || (state.syncRecords ?? 0) > 0);
}

function deviceLabel(device?: Pick<CloudSyncDevice, "name" | "model" | "deviceId"> | null, deviceId?: string) {
  if (device) return device.name || device.model || device.deviceId;
  if (!deviceId) return "未知设备";
  return deviceId.length > 12 ? `${deviceId.slice(0, 12)}…` : deviceId;
}

function useDeviceIndex(devices: CloudSyncDevice[]) {
  return useMemo(() => {
    const map = new Map<string, { device: CloudSyncDevice; color: string }>();
    devices.forEach((device, index) => map.set(device.deviceId, { device, color: DEVICE_COLORS[index % DEVICE_COLORS.length] }));
    return map;
  }, [devices]);
}

function payloadText(payload: Record<string, unknown> | null | undefined, field: string) {
  const value = payload?.[field];
  return typeof value === "string" && value.trim() ? value : "";
}

// ── 档位与同步用量 ──────────────────────────

export function CloudPlanSection({ state }: { state: CloudAdminUser }) {
  const plan = state.plan;
  const devices = state.devices ?? [];
  const itemBytes = state.usedBytes;
  const syncBytes = state.syncBytes ?? 0;
  const syncRecords = state.syncRecords ?? 0;
  const quota = state.quotaBytes;

  if (!plan) {
    return null;
  }

  const collections = plan.collections ?? [];
  const override = plan.override ?? Boolean(state.quotaOverride);

  return (
    <Section
      title="云服务档位"
      icon={Layers}
      tone="violet"
      actions={
        <>
          <Badge variant="info" size="sm">
            {plan.name}
          </Badge>
          {plan.tierName && plan.tierName !== plan.name ? (
            <Badge variant="outline" size="sm" className="font-normal">
              会员档位 {plan.tierName}
            </Badge>
          ) : plan.tierId === 0 ? (
            <Badge variant="outline" size="sm" className="font-normal">
              免费档
            </Badge>
          ) : null}
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="grid gap-4 lg:grid-cols-3">
          <SpaceSplit itemBytes={itemBytes} syncBytes={syncBytes} quota={quota} />
          <CountMeter
            label="同步记录"
            used={syncRecords}
            max={plan.syncEnabled ? plan.maxSyncRecords : 0}
            hint={state.lastSyncAt ? `最后同步 ${formatRelative(state.lastSyncAt)}` : "尚未同步"}
          />
          <CountMeter
            label="设备"
            used={devices.length}
            max={plan.syncEnabled ? plan.maxDevices : 0}
            hint={`在线 ${devices.filter((device) => device.online).length} 台`}
          />
        </div>

        <InfoGrid
          columns={3}
          items={[
            { label: "配额", value: formatBytes(plan.quotaBytes), hint: override ? `管理员覆盖为 ${formatBytes(quota)}` : undefined },
            { label: "单条目上限", value: formatBytes(plan.maxItemBytes) },
            { label: "条目 / 修订", value: `${formatInt(plan.maxItems)} 个 / 每条 ${plan.maxRevisions} 个` },
            { label: "回收站保留", value: plan.trashRetentionDays ? `${plan.trashRetentionDays} 天` : "删除即清除" },
            {
              label: "同步",
              value: plan.syncEnabled ? (plan.realtime ? "实时" : `每 ${plan.pollSeconds} 秒轮询`) : "未开通",
              tone: plan.syncEnabled ? undefined : "warning"
            },
            {
              label: "历史保留",
              value: plan.syncEnabled ? (plan.historyRetentionDays ? `${plan.historyRetentionDays} 天` : "永久") : null
            },
            { label: "接力", value: plan.syncEnabled ? (plan.handoff ? "可用" : "不可用") : null },
            {
              label: "可同步内容",
              value: collections.length === 0 ? "全部" : collections.map(collectionName).join("、"),
              wide: collections.length > 0
            }
          ]}
        />
      </div>
    </Section>
  );
}

/** 空间分成两段：条目修订与同步记录共用一份配额。 */
function SpaceSplit({ itemBytes, syncBytes, quota }: { itemBytes: number; syncBytes: number; quota: number }) {
  const used = itemBytes + syncBytes;
  const percent = usagePercent(used, quota);
  const itemWidth = quota > 0 ? Math.min(100, (itemBytes / quota) * 100) : 0;
  const syncWidth = quota > 0 ? Math.min(100 - itemWidth, (syncBytes / quota) * 100) : 0;
  return (
    <div className="flex flex-col gap-1.5 rounded-xl border px-3.5 py-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs text-muted-foreground">空间</span>
        <span className="text-xs font-medium tabular-nums text-muted-foreground">{percent}%</span>
      </div>
      <div className="text-sm tabular-nums">
        <span className="font-semibold">{formatBytes(used)}</span>
        <span className="text-muted-foreground"> / {formatBytes(quota)}</span>
      </div>
      <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted" role="img" aria-label={`已用 ${percent}%`}>
        <div className="h-full bg-sky-500" style={{ width: `${itemWidth > 0 ? Math.max(itemWidth, 1) : 0}%` }} />
        <div className="h-full bg-violet-500" style={{ width: `${syncWidth > 0 ? Math.max(syncWidth, 1) : 0}%` }} />
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground tabular-nums">
        <span className="inline-flex items-center gap-1">
          <span className="size-2 rounded-full bg-sky-500" />
          条目 {formatBytes(itemBytes)}
        </span>
        <span className="inline-flex items-center gap-1">
          <span className="size-2 rounded-full bg-violet-500" />
          同步 {formatBytes(syncBytes)}
        </span>
      </div>
    </div>
  );
}

function CountMeter({ label, used, max, hint }: { label: string; used: number; max: number; hint?: string }) {
  const percent = max > 0 ? Math.min(100, Math.round((used / max) * 1000) / 10) : 0;
  const tone = percent >= 90 ? "bg-red-500" : percent >= 70 ? "bg-amber-500" : "bg-emerald-500";
  return (
    <div className="flex flex-col gap-1.5 rounded-xl border px-3.5 py-3">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs text-muted-foreground">{label}</span>
        {max > 0 ? <span className="text-xs font-medium tabular-nums text-muted-foreground">{percent}%</span> : null}
      </div>
      <div className="text-sm tabular-nums">
        <span className="font-semibold">{formatInt(used)}</span>
        <span className="text-muted-foreground"> / {max > 0 ? formatInt(max) : "未开通"}</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full", tone)} style={{ width: `${Math.max(percent, used > 0 && max > 0 ? 1 : 0)}%` }} />
      </div>
      {hint ? <div className="text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  );
}

// ── 设备 ──────────────────────────

function DeviceIcon({ device }: { device: CloudSyncDevice }) {
  const text = `${device.platform} ${device.model} ${device.os}`.toLowerCase();
  const icon = /tv|television/.test(text)
    ? Tv
    : /pad|tablet/.test(text)
      ? Tablet
      : /android|ios|iphone|harmony|phone|mobile/.test(text)
        ? Smartphone
        : Monitor;
  return <ToneIcon icon={icon} tone={device.online ? "success" : "neutral"} size="sm" />;
}

function ActivityLine({ activity }: { activity: CloudSyncActivity }) {
  const title = payloadText(activity.payload, "title") || activity.recordId || collectionName(activity.collection);
  const subtitle = payloadText(activity.payload, "subtitle");
  const progress = Number(activity.payload?.progress);
  const playing = activity.state === "playing" || activity.state === "reading";
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-lg bg-muted/50 px-2.5 py-1.5 text-xs">
      {playing ? (
        <Play className="size-3 shrink-0 text-emerald-600 dark:text-emerald-400" />
      ) : activity.state === "paused" ? (
        <Pause className="size-3 shrink-0 text-muted-foreground" />
      ) : (
        <Clapperboard className="size-3 shrink-0 text-muted-foreground" />
      )}
      <span className="shrink-0 text-muted-foreground">{ACTIVITY_LABELS[activity.state] ?? activity.state}</span>
      <span className="min-w-0 truncate font-medium" title={subtitle ? `${title} · ${subtitle}` : title}>
        {title}
        {subtitle ? <span className="font-normal text-muted-foreground"> · {subtitle}</span> : null}
      </span>
      {Number.isFinite(progress) && progress > 0 ? (
        <span className="shrink-0 tabular-nums text-muted-foreground">{Math.round(Math.min(progress, 1) * 100)}%</span>
      ) : null}
      <span className="ml-auto shrink-0 text-muted-foreground" title={formatDateTime(activity.at)}>
        {collectionName(activity.collection)} · {formatRelative(activity.at)}
      </span>
    </div>
  );
}

export function CloudDevicesSection({
  appKey,
  userId,
  state,
  filter,
  onFilter
}: {
  appKey: string;
  userId: number;
  state: CloudAdminUser;
  filter: SyncFilter;
  onFilter: (filter: SyncFilter) => void;
}) {
  const devices = state.devices ?? [];
  const index = useDeviceIndex(devices);
  const removeMutation = useRemoveCloudStorageUserDeviceMutation(appKey, userId);
  const [removing, setRemoving] = useState<CloudSyncDevice | null>(null);
  const max = state.plan?.syncEnabled ? state.plan.maxDevices : 0;

  const remove = async () => {
    if (!removing) return;
    try {
      await removeMutation.mutateAsync(removing.deviceId);
      toast.success("设备已移除");
      if (filter.deviceId === removing.deviceId) onFilter({ ...filter, deviceId: "" });
      setRemoving(null);
    } catch (error) {
      toast.error(errorMessage(error, "移除失败"));
    }
  };

  return (
    <Section
      title="同步设备"
      icon={Smartphone}
      tone="success"
      flush
      actions={
        <span className="text-xs text-muted-foreground tabular-nums">
          {devices.length}
          {max > 0 ? ` / ${max}` : ""} 台
        </span>
      }
    >
      {devices.length === 0 ? (
        <StateBlock compact icon={Smartphone} title="暂无设备" />
      ) : (
        <ul className="divide-y">
          {devices.map((device) => {
            const color = index.get(device.deviceId)?.color ?? UNKNOWN_DEVICE_COLOR;
            const filtering = filter.deviceId === device.deviceId;
            const meta = [device.model, [device.os, device.osVersion].filter(Boolean).join(" "), device.appVersion ? `v${device.appVersion}` : ""]
              .filter(Boolean)
              .join(" · ");
            return (
              <li key={device.deviceId} className={cn("flex flex-col gap-2 px-4 py-3 sm:px-5", filtering && "bg-primary/5")}>
                <div className="flex flex-wrap items-center gap-3">
                  <DeviceIcon device={device} />
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <span className={cn("size-2 shrink-0 rounded-full", color)} aria-hidden />
                      <span className="truncate text-sm font-medium">{deviceLabel(device)}</span>
                      {device.online ? (
                        <Badge variant="success" size="sm">
                          在线
                        </Badge>
                      ) : (
                        <Badge variant="outline" size="sm" className="font-normal text-muted-foreground">
                          {formatRelative(device.lastSeenAt)}在线
                        </Badge>
                      )}
                      {device.platform ? (
                        <Badge variant="outline" size="sm" className="font-normal">
                          {device.platform}
                        </Badge>
                      ) : null}
                    </div>
                    <span className="truncate text-xs text-muted-foreground">{meta || device.deviceId}</span>
                  </div>
                  <div className="flex shrink-0 flex-col items-end gap-0.5 text-xs tabular-nums">
                    <span>
                      <span className="font-medium">{formatInt(device.recordCount)}</span>
                      <span className="text-muted-foreground"> 条</span>
                    </span>
                    <span className="text-muted-foreground" title={formatDateTime(device.lastSyncAt)}>
                      <RefreshCw className="mr-1 inline size-3" />
                      {device.lastSyncAt ? formatRelative(device.lastSyncAt) : "未同步"}
                    </span>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      size="xs"
                      variant={filtering ? "secondary" : "ghost"}
                      onClick={() => onFilter({ ...filter, deviceId: filtering ? "" : device.deviceId })}
                    >
                      <ListFilter />
                      {filtering ? "筛选中" : "看记录"}
                    </Button>
                    <Button
                      size="xs"
                      variant="ghost"
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => setRemoving(device)}
                    >
                      <Trash2 />
                      移除
                    </Button>
                  </div>
                </div>
                {device.activity && device.activity.state !== "idle" ? <ActivityLine activity={device.activity} /> : null}
                <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                  <span className="font-mono">{device.deviceId}</span>
                  <span className="tabular-nums">seq {formatInt(device.lastSeq)}</span>
                  <span className="tabular-nums">登记于 {formatDateTime(device.createdAt)}</span>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      <AlertDialog open={removing !== null} onOpenChange={(next) => (!next ? setRemoving(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除设备「{deviceLabel(removing)}」</AlertDialogTitle>
            <AlertDialogDescription>腾出一个设备名额，已同步的记录保留；该设备需重新登记才能继续同步。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={removeMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void remove();
              }}
            >
              {removeMutation.isPending ? <Loader2 className="animate-spin" /> : <Trash2 />}
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}

// ── 集合 ──────────────────────────

export function CloudCollectionsSection({
  state,
  filter,
  onFilter
}: {
  state: CloudAdminUser;
  filter: SyncFilter;
  onFilter: (filter: SyncFilter) => void;
}) {
  const devices = state.devices ?? [];
  const index = useDeviceIndex(devices);
  const collections = state.collections ?? [];
  const max = Math.max(1, ...collections.map((item) => item.count));

  if (collections.length === 0) return null;

  return (
    <Section title="同步内容" icon={Database} tone="info" flush>
      <ul className="grid divide-y sm:grid-cols-2 sm:divide-y-0">
        {collections.map((item) => (
          <CollectionRow
            key={item.key}
            item={item}
            max={max}
            allowed={planAllows(state.plan, item.key)}
            active={filter.collection === item.key}
            colorOf={(deviceId) => index.get(deviceId)?.color ?? UNKNOWN_DEVICE_COLOR}
            nameOf={(deviceId) => deviceLabel(index.get(deviceId)?.device, deviceId)}
            onSelect={() => onFilter({ ...filter, collection: filter.collection === item.key ? "" : item.key })}
          />
        ))}
      </ul>
    </Section>
  );
}

function CollectionRow({
  item,
  max,
  allowed,
  active,
  colorOf,
  nameOf,
  onSelect
}: {
  item: CloudCollectionUsage;
  max: number;
  allowed: boolean;
  active: boolean;
  colorOf: (deviceId: string) => string;
  nameOf: (deviceId: string) => string;
  onSelect: () => void;
}) {
  const parts = Object.entries(item.byDevice ?? {})
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1]);
  const width = Math.round((item.count / max) * 100);
  return (
    <li className="sm:border-b sm:odd:border-r">
      <button
        type="button"
        onClick={onSelect}
        aria-pressed={active}
        className={cn(
          "flex w-full flex-col gap-2 px-4 py-3 text-left transition-colors hover:bg-muted/40 sm:px-5",
          active && "bg-primary/5"
        )}
      >
        <div className="flex items-center justify-between gap-3">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-sm font-medium">{item.name && item.name !== item.key ? item.name : collectionName(item.key)}</span>
            <code className="truncate font-mono text-xs text-muted-foreground">{item.key}</code>
            {COLLECTION_KIND_LABELS[item.kind] ? (
              <Badge variant="outline" size="sm" className="font-normal">
                {COLLECTION_KIND_LABELS[item.kind]}
              </Badge>
            ) : null}
            {!allowed ? (
              <Badge variant="warning" size="sm">
                档位外
              </Badge>
            ) : null}
            {active ? (
              <Badge variant="info" size="sm">
                筛选中
              </Badge>
            ) : null}
          </span>
          <span className="shrink-0 text-xs font-medium tabular-nums">{formatInt(item.count)} 条</span>
        </div>
        <div className="flex h-1.5 overflow-hidden rounded-full bg-muted" style={{ width: `${Math.max(width, item.count ? 4 : 0)}%` }}>
          {parts.map(([deviceId, count]) => (
            <div
              key={deviceId || "unknown"}
              className={cn("h-full", colorOf(deviceId))}
              style={{ width: `${(count / Math.max(item.count, 1)) * 100}%` }}
              title={`${nameOf(deviceId)} ${count} 条`}
            />
          ))}
        </div>
        <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground tabular-nums">
          <span>{formatBytes(item.bytes)}</span>
          {item.lastUpdatedAt ? <span title={formatDateTime(item.lastUpdatedAt)}>{formatRelative(item.lastUpdatedAt)}更新</span> : null}
          {parts.map(([deviceId, count]) => (
            <span key={deviceId || "unknown"} className="inline-flex items-center gap-1">
              <span className={cn("size-1.5 rounded-full", colorOf(deviceId))} />
              {nameOf(deviceId)} {formatInt(count)}
            </span>
          ))}
        </div>
      </button>
    </li>
  );
}

// ── 同步记录 ──────────────────────────

export function CloudSyncRecordsSection({
  appKey,
  userId,
  state,
  filter,
  onFilter
}: {
  appKey: string;
  userId: number;
  state: CloudAdminUser;
  filter: SyncFilter;
  onFilter: (filter: SyncFilter) => void;
}) {
  const [domain, setDomain] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [openRecord, setOpenRecord] = useState<CloudSyncRecord | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);
  const devices = state.devices ?? [];
  const index = useDeviceIndex(devices);

  // 集合 / 设备从外部（集合卡、设备行）改动时回到第一页
  const filterKey = `${filter.collection}|${filter.deviceId}`;
  const [lastFilterKey, setLastFilterKey] = useState(filterKey);
  if (lastFilterKey !== filterKey) {
    setLastFilterKey(filterKey);
    setPage(1);
  }

  const listQuery = useCloudStorageUserSyncRecordsQuery(appKey, userId, {
    collection: filter.collection || undefined,
    domain: filter.collection ? undefined : domain || undefined,
    deviceId: filter.deviceId || undefined,
    keyword: debouncedKeyword || undefined,
    page,
    limit
  });
  const records = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const filtered = Boolean(filter.collection || filter.deviceId || domain || keyword);

  // 集合下拉：目录里的 + 这个人实际有的（目录外的也能同步），选了领域时只列该领域。
  const collectionOptions = useMemo(() => {
    const keys = new Set<string>(CLOUD_SYNC_COLLECTIONS.map((item) => item.key));
    for (const item of state.collections ?? []) keys.add(item.key);
    return [...keys]
      .filter((key) => !domain || key.startsWith(`${domain}.`))
      .map((key) => ({ value: key, label: collectionName(key) }));
  }, [state.collections, domain]);

  const columns: Array<Column<CloudSyncRecord>> = [
    {
      id: "record",
      header: "记录",
      cell: (record) => {
        const title = payloadText(record.payload, "title");
        const subtitle = payloadText(record.payload, "subtitle");
        return (
          <div className="flex min-w-0 flex-col gap-0.5">
            <div className="flex min-w-0 items-center gap-1.5">
              <span className={cn("truncate text-sm font-medium", !title && "font-mono", record.deleted && "text-muted-foreground line-through")}>
                {title || record.id}
              </span>
              {record.deleted ? (
                <Badge variant="warning" size="sm">
                  已删除
                </Badge>
              ) : null}
            </div>
            <span className="truncate text-xs text-muted-foreground">
              {collectionName(record.collection)}
              {subtitle ? ` · ${subtitle}` : ""}
            </span>
          </div>
        );
      }
    },
    {
      id: "device",
      header: "设备",
      width: "150px",
      hideBelow: "md",
      cell: (record) => {
        const entry = record.deviceId ? index.get(record.deviceId) : undefined;
        return (
          <span className="flex min-w-0 items-center gap-1.5 text-xs">
            <span className={cn("size-2 shrink-0 rounded-full", entry?.color ?? UNKNOWN_DEVICE_COLOR)} />
            <span className="truncate">{record.deviceName || deviceLabel(entry?.device, record.deviceId)}</span>
          </span>
        );
      }
    },
    {
      id: "seq",
      header: "序号",
      align: "right",
      width: "80px",
      hideBelow: "sm",
      cell: (record) => <span className="font-mono text-xs tabular-nums">{record.seq}</span>
    },
    {
      id: "size",
      header: "大小",
      align: "right",
      width: "88px",
      hideBelow: "sm",
      cell: (record) => <span className="text-xs tabular-nums">{record.deleted ? "—" : formatBytes(record.size)}</span>
    },
    {
      id: "time",
      header: "修改",
      align: "right",
      width: "120px",
      cell: (record) => (
        <span className="text-xs text-muted-foreground tabular-nums" title={formatDateTime(record.updatedAt)}>
          {formatRelative(record.updatedAt)}
        </span>
      )
    }
  ];

  const current = openRecord;
  const currentDevice = current?.deviceId ? index.get(current.deviceId)?.device : undefined;

  return (
    <Section
      title="同步记录"
      icon={Database}
      tone="primary"
      flush
      toolbar={
        <FilterBar>
          <SearchField
            value={keyword}
            onChange={(value) => {
              setKeyword(value);
              setPage(1);
            }}
            placeholder="搜索标题或记录 ID"
          />
          <FilterSelect
            label="领域"
            value={domain}
            options={CLOUD_SYNC_DOMAINS.map((item) => ({ value: item.key, label: item.name }))}
            onChange={(value) => {
              setDomain(value);
              if (filter.collection && value && !filter.collection.startsWith(`${value}.`)) {
                onFilter({ ...filter, collection: "" });
              }
              setPage(1);
            }}
          />
          <FilterSelect
            label="内容"
            value={filter.collection}
            options={collectionOptions}
            onChange={(value) => onFilter({ ...filter, collection: value })}
          />
          {devices.length ? (
            <FilterSelect
              label="设备"
              value={filter.deviceId}
              options={devices.map((device) => ({ value: device.deviceId, label: deviceLabel(device) }))}
              onChange={(value) => onFilter({ ...filter, deviceId: value })}
            />
          ) : null}
          <ResetButton
            visible={filtered}
            onClick={() => {
              setKeyword("");
              setDomain("");
              onFilter({ collection: "", deviceId: "" });
              setPage(1);
            }}
          />
        </FilterBar>
      }
      footer={
        total > 0 ? (
          <Pager
            page={page}
            limit={limit}
            total={total}
            onPage={setPage}
            onLimit={(value) => {
              setLimit(value);
              setPage(1);
            }}
          />
        ) : undefined
      }
    >
      <RecordTable
        columns={columns}
        rows={records}
        rowKey={(record) => `${record.collection}/${record.id}`}
        loading={listQuery.isLoading}
        fetching={listQuery.isFetching}
        error={listQuery.error}
        onRetry={() => void listQuery.refetch()}
        onRowClick={(record) => {
          setOpenRecord(record);
          setSheetOpen(true);
        }}
        minWidth={560}
        empty={<StateBlock icon={Database} title={filtered ? "没有符合条件的记录" : "暂无同步记录"} />}
      />

      <RecordSheet
        open={sheetOpen && Boolean(current)}
        onOpenChange={setSheetOpen}
        title={<span className="break-all">{payloadText(current?.payload, "title") || current?.id || "记录"}</span>}
        description={current ? `${collectionName(current.collection)}（${current.collection}）` : undefined}
        hero={
          current?.deleted ? (
            <div className="flex items-center gap-2 rounded-xl border border-amber-200 bg-amber-50/60 px-3.5 py-3 text-xs dark:border-amber-900/60 dark:bg-amber-950/30">
              <ToneIcon icon={Trash2} tone="warning" size="sm" />
              已删除（墓碑）
            </div>
          ) : null
        }
        fields={
          current
            ? [
                { label: "记录 ID", value: current.id, mono: true, copy: current.id, wide: true },
                {
                  label: "设备",
                  value: current.deviceName || deviceLabel(currentDevice, current.deviceId),
                  hint: current.deviceId || undefined
                },
                { label: "序号", value: String(current.seq), mono: true },
                { label: "大小", value: current.deleted ? null : formatBytes(current.size) },
                { label: "修改时间", value: formatDateTime(current.updatedAt) },
                { label: "首次同步", value: formatDateTime(current.createdAt) }
              ]
            : undefined
        }
      >
        {current && !current.deleted ? (
          <div className="flex flex-col gap-2">
            <div className="text-xs font-medium text-muted-foreground">内容</div>
            <JsonViewer value={JSON.stringify(current.payload ?? null, null, 2)} height={320} />
          </div>
        ) : null}
      </RecordSheet>
    </Section>
  );
}

/** 档位卡与「空间与权限」里的生效配额说明共用。 */
export function planQuotaHint(state: CloudAdminUser) {
  if (state.quotaOverride) return "管理员覆盖";
  return state.plan ? `${state.plan.name}档位` : "应用默认";
}
