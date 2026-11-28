"use client";

import { useMemo, useState } from "react";
import {
  ArchiveRestore,
  Cloud,
  Download,
  Eraser,
  ExternalLink,
  Eye,
  FileText,
  FolderTree,
  HardDrive,
  History,
  Link2,
  Loader2,
  RotateCcw,
  Save,
  Snowflake,
  Trash2,
  TriangleAlert,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
import {
  ENCODING_LABELS,
  EncodingBadge,
  MiB,
  SOURCE_LABELS,
  bytesToMBInput,
  cloudUsedBytes,
  formatBytes,
  mbInputToBytes,
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
import { Input } from "@/components/ui/input";
import { JsonViewer } from "@/components/ui/json-viewer";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  listCloudStorageUserItems,
  type CloudAdminUser,
  type CloudItem,
  type CloudItemContent,
  type CloudItemParams,
  type CloudLink,
  type CloudRevision
} from "@/lib/api/cloud-storage";
import {
  useCloudStorageItemActions,
  useCloudStorageUserItemQuery,
  useCloudStorageUserItemRevisionsQuery,
  useCloudStorageUserItemsQuery,
  useCloudStorageUserQuery,
  usePurgeCloudStorageUserMutation,
  useSaveCloudStorageUserMutation
} from "@/lib/cloud-storage-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import {
  CopyButton,
  FilterBar,
  FilterSelect,
  InfoGrid,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  ToneIcon,
  collectPages,
  downloadCsv,
  errorMessage,
  formatInt,
  useDebouncedValue,
  type Column,
  type Tone
} from "./kit";
import {
  CloudCollectionsSection,
  CloudDevicesSection,
  CloudPlanSection,
  CloudSyncRecordsSection,
  hasSyncData,
  planQuotaHint,
  type SyncFilter
} from "./user-cloud-sync";

type Status = NonNullable<CloudItemParams["status"]>;

const STATUS_OPTIONS = [
  { value: "active", label: "正常" },
  { value: "deleted", label: "回收站" }
];

function usageTone(percent: number): Tone {
  return percent >= 90 ? "danger" : percent >= 70 ? "warning" : "success";
}

const BAR_TONE: Record<Tone, string> = {
  neutral: "bg-muted-foreground/50",
  primary: "bg-primary",
  success: "bg-emerald-500",
  warning: "bg-amber-500",
  danger: "bg-red-500",
  info: "bg-sky-500",
  violet: "bg-violet-500"
};

/**
 * 云存储：这个用户在应用里存了什么、占了多少，以及管理员能对它做的事。
 *
 *   用量         已用空间（条目 + 同步记录）、条目、回收站、修订总数
 *   云服务       生效档位、同步用量、设备、各集合与同步记录（见 user-cloud-sync.tsx）
 *   空间与权限   配额覆盖、冻结（只读）与备注
 *   命名空间     各命名空间的用量，点一下即按它筛选条目
 *   条目         按状态、命名空间、键筛选；详情里看内容与修订，回滚 / 删除 / 恢复 / 生成下载链接
 *   危险区       清空这个人的全部云存储数据（删除用户之前应先做这一步，
 *                否则存储桶里的对象不会跟着用户一起删）
 *
 * 管理端的写入不受冻结约束，修订上会记下操作人。
 */
export function UserCloudTab({ appKey, userId }: { appKey: string; userId: number }) {
  const userQuery = useCloudStorageUserQuery(appKey, userId);
  const state = userQuery.data;
  const [namespace, setNamespace] = useState("");
  const [syncFilter, setSyncFilter] = useState<SyncFilter>({ collection: "", deviceId: "" });

  if (userQuery.isLoading) {
    return <CloudSkeleton />;
  }
  if (!state) {
    return (
      <Section title="云存储" icon={Cloud} tone="info">
        <StateBlock
          kind="error"
          title="云存储数据不可用"
          description={errorMessage(userQuery.error, "未能读取该用户的云存储账目")}
          onRetry={() => void userQuery.refetch()}
        />
      </Section>
    );
  }

  const used = cloudUsedBytes(state);
  const percent = usagePercent(used, state.quotaBytes);
  const tone = usageTone(percent);
  const synced = hasSyncData(state);

  return (
    <div className="flex flex-col gap-5">
      {!state.enabled ? (
        <div className="flex items-start gap-3 rounded-xl border border-dashed px-4 py-3">
          <ToneIcon icon={TriangleAlert} tone="warning" size="sm" />
          <p className="text-xs leading-5 text-muted-foreground">
            该应用尚未启用云存储，客户端无法读写。以下为历史数据，管理端操作仍然可用。
          </p>
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <MetricCard
          label="已用空间"
          value={formatBytes(used)}
          sub={`共 ${formatBytes(state.quotaBytes)}，已用 ${percent}%`}
          icon={HardDrive}
          tone={tone}
        />
        <MetricCard
          label="条目"
          value={formatInt(state.itemCount)}
          sub={`上限 ${formatInt(state.limits.maxItems)}`}
          icon={FileText}
          tone="info"
        />
        <MetricCard
          label="回收站"
          value={formatInt(state.trashCount)}
          sub={`保留 ${state.limits.trashRetentionDays} 天`}
          icon={Trash2}
          tone={state.trashCount ? "warning" : "neutral"}
        />
        <MetricCard
          label="修订总数"
          value={formatInt(state.revisionCount)}
          sub={`每条保留 ${state.limits.maxRevisions} 个`}
          icon={History}
          tone="violet"
        />
      </div>

      <CloudPlanSection state={state} />

      <AccountSection appKey={appKey} userId={userId} state={state} />

      {synced ? (
        <>
          <CloudDevicesSection appKey={appKey} userId={userId} state={state} filter={syncFilter} onFilter={setSyncFilter} />
          <CloudCollectionsSection state={state} filter={syncFilter} onFilter={setSyncFilter} />
          <CloudSyncRecordsSection appKey={appKey} userId={userId} state={state} filter={syncFilter} onFilter={setSyncFilter} />
        </>
      ) : null}

      {state.namespaces.length ? (
        <NamespaceSection
          state={state}
          selected={namespace}
          onSelect={(value) => setNamespace((current) => (current === value ? "" : value))}
        />
      ) : null}

      <ItemsSection appKey={appKey} userId={userId} state={state} namespace={namespace} onNamespace={setNamespace} />

      <DangerSection appKey={appKey} userId={userId} state={state} />
    </div>
  );
}

function CloudSkeleton() {
  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        {[0, 1, 2, 3].map((index) => (
          <MetricCard key={index} label={<Skeleton className="h-3 w-16" />} value="" loading />
        ))}
      </div>
      <Section title="空间与权限" icon={HardDrive} tone="primary">
        <div className="flex flex-col gap-4">
          <Skeleton className="h-2.5 w-full rounded-full" />
          <div className="grid gap-4 sm:grid-cols-3">
            {[0, 1, 2].map((index) => (
              <div key={index} className="flex flex-col gap-1.5">
                <Skeleton className="h-3 w-16" />
                <Skeleton className="h-4 w-28" />
              </div>
            ))}
          </div>
        </div>
      </Section>
      <Section title="条目" icon={FileText} tone="info" flush>
        <RecordTable columns={ITEM_SKELETON_COLUMNS} rows={[]} rowKey={(_, index) => index} loading skeletonRows={5} />
      </Section>
    </div>
  );
}

const ITEM_SKELETON_COLUMNS: Array<Column<unknown>> = ["key", "type", "rev", "size", "time"].map((id) => ({
  id,
  header: "",
  cell: () => null
}));

/** 配额条：三档颜色只看比例，90% 以上是「下一次写入就可能被拒」。 */
function QuotaBar({ used, quota }: { used: number; quota: number }) {
  const percent = usagePercent(used, quota);
  const tone = usageTone(percent);
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-sm tabular-nums">
          <span className="font-semibold">{formatBytes(used)}</span>
          <span className="text-muted-foreground"> / {formatBytes(quota)}</span>
        </span>
        <span className="text-xs font-medium tabular-nums text-muted-foreground">{percent}%</span>
      </div>
      <div
        className="relative h-2.5 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
      >
        <div
          className={cn("h-full rounded-full transition-[width] duration-500", BAR_TONE[tone])}
          style={{ width: `${Math.max(percent, used > 0 ? 1 : 0)}%` }}
        />
      </div>
    </div>
  );
}

// ── 空间与权限 ──────────────────────────

type AccountDraft = {
  scope: string;
  customQuota: boolean;
  quotaMB: string;
  frozen: boolean;
  frozenReason: string;
  note: string;
};

function seedAccount(scope: string, state: CloudAdminUser): AccountDraft {
  return {
    scope,
    customQuota: Boolean(state.quotaOverride),
    quotaMB: bytesToMBInput(state.quotaOverride ?? state.quotaBytes),
    frozen: state.frozen,
    frozenReason: state.frozenReason ?? "",
    note: state.note ?? ""
  };
}

function AccountSection({ appKey, userId, state }: { appKey: string; userId: number; state: CloudAdminUser }) {
  const scope = `${appKey}:${userId}:${state.updatedAt ?? ""}`;
  const [draft, setDraft] = useState<AccountDraft | null>(null);
  const saveMutation = useSaveCloudStorageUserMutation(appKey, userId);

  const seeded = seedAccount(scope, state);
  const current = draft?.scope === scope ? draft : seeded;
  const patch = (changes: Partial<AccountDraft>) => setDraft({ ...current, ...changes, scope });
  const dirty =
    current.customQuota !== seeded.customQuota ||
    (current.customQuota && current.quotaMB !== seeded.quotaMB) ||
    current.frozen !== seeded.frozen ||
    (current.frozen && current.frozenReason !== seeded.frozenReason) ||
    current.note !== seeded.note;
  const draftQuota = current.customQuota ? mbInputToBytes(current.quotaMB) : Number.NaN;
  const quotaInvalid = current.customQuota && Number.isNaN(draftQuota);
  // 不覆盖时沿用的是生效档位的配额（没配档位时就是应用的基础档）。
  const planQuota = state.plan?.quotaBytes ?? state.limits.quotaBytes;

  const save = async () => {
    let quotaBytes: number | null = null;
    if (current.customQuota) {
      quotaBytes = mbInputToBytes(current.quotaMB);
      if (Number.isNaN(quotaBytes)) {
        toast.error("配额需为正数");
        return;
      }
    }
    try {
      await saveMutation.mutateAsync({
        quotaBytes,
        frozen: current.frozen,
        frozenReason: current.frozen ? current.frozenReason.trim() : "",
        note: current.note.trim()
      });
      toast.success("已保存");
      setDraft(null);
    } catch (error) {
      toast.error(errorMessage(error, "保存失败"));
    }
  };

  return (
    <Section
      title="空间与权限"
      description="配额覆盖档位配额；冻结后客户端只读，管理端操作不受影响"
      icon={HardDrive}
      tone="primary"
      actions={
        state.frozen ? (
          <Badge variant="info" size="sm">
            <Snowflake />
            已冻结
          </Badge>
        ) : (
          <Badge variant="success" size="sm">
            正常读写
          </Badge>
        )
      }
      footer={
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">
            {state.updatedBy
              ? `最近由 ${state.updatedBy} 于 ${formatDateTime(state.updatedAt)} 修改`
              : dirty
                ? "有未保存的修改"
                : "尚未单独设置，沿用档位"}
          </span>
          <div className="flex items-center gap-2">
            {dirty ? (
              <Button size="sm" variant="ghost" onClick={() => setDraft(null)} disabled={saveMutation.isPending}>
                <RotateCcw />
                还原
              </Button>
            ) : null}
            <Button size="sm" onClick={() => void save()} disabled={!dirty || quotaInvalid || saveMutation.isPending}>
              {saveMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存
            </Button>
          </div>
        </div>
      }
    >
      <div className="flex flex-col gap-5">
        <QuotaBar used={cloudUsedBytes(state)} quota={state.quotaBytes} />
        <InfoGrid
          columns={3}
          items={[
            {
              label: "生效配额",
              value: formatBytes(state.quotaBytes),
              hint: planQuotaHint(state)
            },
            { label: "档位配额", value: formatBytes(planQuota) },
            { label: "单条上限", value: formatBytes(state.limits.maxItemBytes) },
            {
              label: "最近写入",
              value: state.lastWriteAt ? formatDateTime(state.lastWriteAt) : null,
              hint: state.lastWriteAt ? formatRelative(state.lastWriteAt) : undefined
            },
            { label: "开通时间", value: state.createdAt ? formatDateTime(state.createdAt) : null },
            {
              label: "冻结原因",
              value: state.frozen ? state.frozenReason || "未填写" : null,
              tone: state.frozen ? "info" : undefined
            }
          ]}
        />

        <div className="grid gap-4 border-t pt-5 lg:grid-cols-2">
          <div className="flex flex-col gap-3 rounded-xl border p-3.5">
            <SwitchLine
              label="自定义配额"
              hint={current.customQuota ? "覆盖档位配额" : `沿用档位配额 ${formatBytes(planQuota)}`}
              checked={current.customQuota}
              onChange={(value) => patch({ customQuota: value })}
            />
            {current.customQuota ? (
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    min={0}
                    step="any"
                    inputMode="decimal"
                    className="w-40 font-mono tabular-nums"
                    value={current.quotaMB}
                    aria-invalid={quotaInvalid || undefined}
                    onChange={(event) => patch({ quotaMB: event.target.value })}
                  />
                  <span className="text-xs text-muted-foreground">MB</span>
                </div>
                {quotaInvalid ? (
                  <span className="text-xs text-red-600 dark:text-red-400">配额需为正数</span>
                ) : cloudUsedBytes(state) > draftQuota ? (
                  <span className="text-xs text-amber-600 dark:text-amber-400">低于已用空间，之后的写入会被拒绝</span>
                ) : null}
              </div>
            ) : null}
          </div>

          <div className="flex flex-col gap-3 rounded-xl border p-3.5">
            <SwitchLine
              label="冻结"
              hint="冻结后只读，写入、删除与回滚均被拒绝"
              checked={current.frozen}
              onChange={(value) => patch({ frozen: value })}
            />
            {current.frozen ? (
              <Input
                value={current.frozenReason}
                maxLength={255}
                placeholder="冻结原因（对用户可见）"
                onChange={(event) => patch({ frozenReason: event.target.value })}
              />
            ) : null}
          </div>

          <div className="flex flex-col gap-1.5 lg:col-span-2">
            <Label htmlFor={`cloud-note-${userId}`} className="text-xs text-muted-foreground">
              备注
            </Label>
            <Input
              id={`cloud-note-${userId}`}
              value={current.note}
              maxLength={255}
              placeholder="仅管理员可见"
              onChange={(event) => patch({ note: event.target.value })}
            />
          </div>
        </div>
      </div>
    </Section>
  );
}

function SwitchLine({
  label,
  hint,
  checked,
  onChange
}: {
  label: string;
  hint: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-3">
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">{hint}</span>
      </span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  );
}

// ── 命名空间 ──────────────────────────

function NamespaceSection({
  state,
  selected,
  onSelect
}: {
  state: CloudAdminUser;
  selected: string;
  onSelect: (namespace: string) => void;
}) {
  const max = Math.max(1, ...state.namespaces.map((item) => item.storedBytes));
  return (
    <Section
      title="命名空间"
      description="各命名空间的占用，点击可筛选下方条目"
      icon={FolderTree}
      tone="violet"
      flush
    >
      <ul className="grid divide-y sm:grid-cols-2 sm:divide-y-0">
        {state.namespaces.map((item) => {
          const active = selected === item.namespace;
          const width = Math.round((item.storedBytes / max) * 100);
          return (
            <li key={item.namespace} className="sm:border-b sm:odd:border-r">
              <button
                type="button"
                onClick={() => onSelect(item.namespace)}
                aria-pressed={active}
                className={cn(
                  "flex w-full flex-col gap-2 px-4 py-3 text-left transition-colors hover:bg-muted/40 sm:px-5",
                  active && "bg-primary/5"
                )}
              >
                <div className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 items-center gap-1.5">
                    {item.name && item.name !== item.namespace ? (
                      <span className="truncate text-sm font-medium">{item.name}</span>
                    ) : null}
                    <code className="truncate font-mono text-xs text-muted-foreground">{item.namespace}</code>
                    {active ? (
                      <Badge variant="info" size="sm">
                        筛选中
                      </Badge>
                    ) : null}
                  </span>
                  <span className="shrink-0 text-xs font-medium tabular-nums">{formatBytes(item.storedBytes)}</span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-violet-500/70" style={{ width: `${Math.max(width, item.storedBytes ? 2 : 0)}%` }} />
                </div>
                <div className="flex gap-3 text-xs text-muted-foreground tabular-nums">
                  <span>条目 {formatInt(item.itemCount)}</span>
                  <span>回收站 {formatInt(item.trashCount)}</span>
                </div>
              </button>
            </li>
          );
        })}
      </ul>
    </Section>
  );
}

// ── 条目 ──────────────────────────

function ItemsSection({
  appKey,
  userId,
  state,
  namespace,
  onNamespace
}: {
  appKey: string;
  userId: number;
  state: CloudAdminUser;
  namespace: string;
  onNamespace: (namespace: string) => void;
}) {
  const token = useAdminToken();
  const [status, setStatus] = useState<Status>("active");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [openItem, setOpenItem] = useState<CloudItem | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const debouncedKeyword = useDebouncedValue(keyword.trim(), 300);

  // 命名空间从外部（用量卡）改动时回到第一页
  const [lastNamespace, setLastNamespace] = useState(namespace);
  if (lastNamespace !== namespace) {
    setLastNamespace(namespace);
    setPage(1);
  }

  const params: CloudItemParams = {
    namespace: namespace || undefined,
    status,
    keyword: debouncedKeyword || undefined
  };
  const listQuery = useCloudStorageUserItemsQuery(appKey, userId, { ...params, page, limit });
  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const namespaceNames = useMemo(
    () => Object.fromEntries(state.namespaces.map((item) => [item.namespace, item.name ?? ""])),
    [state.namespaces]
  );
  const filtered = Boolean(namespace || keyword || status !== "active");

  const columns: Array<Column<CloudItem>> = [
    {
      id: "key",
      header: "条目",
      cell: (item) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate font-mono text-sm font-medium">{item.key}</span>
            {item.deleted ? (
              <Badge variant="warning" size="sm">
                回收站
              </Badge>
            ) : null}
          </div>
          <span className="truncate text-xs text-muted-foreground">
            {namespaceNames[item.namespace] && namespaceNames[item.namespace] !== item.namespace
              ? `${namespaceNames[item.namespace]}（${item.namespace}）`
              : item.namespace}
          </span>
        </div>
      )
    },
    { id: "encoding", header: "类型", width: "88px", cell: (item) => <EncodingBadge encoding={item.encoding} /> },
    {
      id: "revision",
      header: "版本",
      align: "right",
      width: "88px",
      cell: (item) => (
        <span className="font-mono text-xs tabular-nums">
          v{item.revision}
          <span className="text-muted-foreground"> / {item.revisionCount}</span>
        </span>
      )
    },
    {
      id: "size",
      header: "大小 / 占用",
      align: "right",
      width: "150px",
      hideBelow: "sm",
      cell: (item) => (
        <span className="text-xs tabular-nums">
          {formatBytes(item.size)}
          <span className="text-muted-foreground"> / {formatBytes(item.storedBytes)}</span>
        </span>
      )
    },
    {
      id: "time",
      header: "更新",
      align: "right",
      width: "160px",
      cell: (item) =>
        item.deleted ? (
          <span className="text-xs text-amber-600 tabular-nums dark:text-amber-400" title={formatDateTime(item.purgeAt)}>
            {formatRelative(item.purgeAt)}清除
          </span>
        ) : (
          <span className="text-xs text-muted-foreground tabular-nums" title={formatDateTime(item.updatedAt)}>
            {formatRelative(item.updatedAt)}
          </span>
        )
    }
  ];

  const exportCsv = async () => {
    if (!token) return;
    setExporting(true);
    try {
      const result = await collectPages((p, l) =>
        listCloudStorageUserItems(token, appKey, userId, { ...params, page: p, limit: l })
      );
      downloadCsv(
        `cloud-items-${userId}`,
        [
          { header: "ID", value: (row) => row.id },
          { header: "命名空间", value: (row) => row.namespace },
          { header: "键", value: (row) => row.key },
          { header: "类型", value: (row) => ENCODING_LABELS[row.encoding] ?? row.encoding },
          { header: "内容类型", value: (row) => row.contentType },
          { header: "当前版本", value: (row) => row.revision },
          { header: "修订数", value: (row) => row.revisionCount },
          { header: "大小(字节)", value: (row) => row.size },
          { header: "占用(字节)", value: (row) => row.storedBytes },
          { header: "SHA-256", value: (row) => row.sha256 },
          { header: "设备", value: (row) => row.deviceId ?? "" },
          { header: "回收站", value: (row) => (row.deleted ? "是" : "否") },
          { header: "清除时间", value: (row) => (row.purgeAt ? formatDateTime(row.purgeAt) : "") },
          { header: "创建时间", value: (row) => formatDateTime(row.createdAt) },
          { header: "更新时间", value: (row) => formatDateTime(row.updatedAt) }
        ],
        result.rows
      );
      if (result.truncated) toast.warning(`共 ${result.total} 条，仅导出前 ${result.rows.length} 条`);
      else toast.success(`已导出 ${result.rows.length} 条`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  };

  return (
    <Section
      title="条目"
      description={`单条上限 ${formatBytes(state.limits.maxItemBytes)}，每个条目保留 ${state.limits.maxRevisions} 个修订`}
      icon={FileText}
      tone="info"
      flush
      actions={
        <Button size="sm" variant="outline" onClick={() => void exportCsv()} disabled={exporting || total === 0}>
          {exporting ? <Loader2 className="animate-spin" /> : <Download />}
          导出 CSV
        </Button>
      }
      toolbar={
        <FilterBar>
          <SearchField
            value={keyword}
            onChange={(value) => {
              setKeyword(value);
              setPage(1);
            }}
            placeholder="搜索键"
          />
          <FilterSelect
            label="状态"
            value={status === "all" ? "" : status}
            allLabel="全部"
            options={STATUS_OPTIONS}
            onChange={(value) => {
              setStatus((value || "all") as Status);
              setPage(1);
            }}
          />
          {state.namespaces.length ? (
            <FilterSelect
              label="命名空间"
              value={namespace}
              allLabel="全部"
              options={state.namespaces.map((item) => ({
                value: item.namespace,
                label: item.name && item.name !== item.namespace ? `${item.name}（${item.namespace}）` : item.namespace
              }))}
              onChange={onNamespace}
            />
          ) : null}
          <ResetButton
            visible={filtered}
            onClick={() => {
              setKeyword("");
              setStatus("active");
              onNamespace("");
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
        rows={items}
        rowKey={(item) => item.id}
        loading={listQuery.isLoading}
        fetching={listQuery.isFetching}
        error={listQuery.error}
        onRetry={() => void listQuery.refetch()}
        onRowClick={(item) => {
          setOpenItem(item);
          setSheetOpen(true);
        }}
        minWidth={640}
        empty={
          <StateBlock
            icon={status === "deleted" ? Trash2 : Cloud}
            title={filtered ? "没有符合条件的条目" : status === "deleted" ? "回收站为空" : "暂无条目"}
            description={filtered ? "调整筛选条件后再试" : "用户写入云存储后会出现在这里"}
          />
        }
      />

      <ItemSheet
        appKey={appKey}
        userId={userId}
        item={openItem}
        open={sheetOpen}
        namespaceName={openItem ? namespaceNames[openItem.namespace] : undefined}
        onOpenChange={setSheetOpen}
      />
    </Section>
  );
}

// ── 条目详情 ──────────────────────────

type Confirm = "trash" | "purge" | { rollback: number } | null;

function ItemSheet({
  appKey,
  userId,
  item,
  open,
  namespaceName,
  onOpenChange
}: {
  appKey: string;
  userId: number;
  /** 关闭后仍保留上一条，关闭动画期间内容不会闪空 */
  item: CloudItem | null;
  open: boolean;
  namespaceName?: string;
  onOpenChange: (open: boolean) => void;
}) {
  const [viewing, setViewing] = useState<{ itemId: number; revision: number } | null>(null);
  const [confirm, setConfirm] = useState<Confirm>(null);
  const [link, setLink] = useState<(CloudLink & { itemId: number; revision: number }) | null>(null);
  const itemId = open ? (item?.id ?? null) : null;
  const revision = viewing && viewing.itemId === item?.id ? viewing.revision : 0;

  const contentQuery = useCloudStorageUserItemQuery(appKey, userId, itemId, revision || undefined);
  const revisionsQuery = useCloudStorageUserItemRevisionsQuery(appKey, userId, itemId);
  const actions = useCloudStorageItemActions(appKey, userId);
  const detail = contentQuery.data;
  const live: CloudItem | null = detail ?? item;
  const revisions = revisionsQuery.data?.items ?? [];
  const busy = actions.rollback.isPending || actions.remove.isPending || actions.restore.isPending;
  const currentLink = link && link.itemId === item?.id ? link : null;

  const run = async (task: () => Promise<unknown>, success: string, closeAfter = false) => {
    try {
      await task();
      toast.success(success);
      setConfirm(null);
      setViewing(null);
      setLink(null);
      if (closeAfter) onOpenChange(false);
    } catch (error) {
      toast.error(errorMessage(error, "操作失败"));
    }
  };

  const generateLink = async (targetRevision: number) => {
    if (!item) return;
    try {
      const result = await actions.link.mutateAsync({ itemId: item.id, revision: targetRevision || undefined });
      setLink({ ...result, itemId: item.id, revision: targetRevision });
      toast.success("下载链接已生成");
    } catch (error) {
      toast.error(errorMessage(error, "获取下载地址失败"));
    }
  };

  const raw = useMemo(() => {
    if (!live) return undefined;
    const { content: _content, ...rest } = live as CloudItemContent;
    void _content;
    return rest;
  }, [live]);

  return (
    <>
      <RecordSheet
        open={open && Boolean(item)}
        onOpenChange={(next) => {
          onOpenChange(next);
          if (!next) {
            setViewing(null);
            setLink(null);
          }
        }}
        title={<span className="font-mono break-all">{item?.key ?? "条目"}</span>}
        description={
          item ? (namespaceName && namespaceName !== item.namespace ? `${namespaceName}（${item.namespace}）` : item.namespace) : undefined
        }
        hero={
          live ? (
            <div className="flex flex-wrap items-center gap-2 rounded-xl border bg-muted/30 px-3.5 py-3">
              <ToneIcon icon={live.deleted ? Trash2 : FileText} tone={live.deleted ? "warning" : "info"} size="sm" />
              <span className="font-mono text-sm font-semibold tabular-nums">v{live.revision}</span>
              <EncodingBadge encoding={live.encoding} />
              {live.deleted ? (
                <Badge variant="warning" size="sm">
                  回收站，{formatDateTime(live.purgeAt)} 清除
                </Badge>
              ) : (
                <Badge variant="success" size="sm">
                  正常
                </Badge>
              )}
              <span className="ml-auto text-xs text-muted-foreground tabular-nums">{formatBytes(live.size)}</span>
            </div>
          ) : null
        }
        fields={
          live
            ? [
                { label: "内容类型", value: live.contentType, mono: true },
                { label: "大小", value: formatBytes(live.size), hint: `全部修订占用 ${formatBytes(live.storedBytes)}` },
                { label: "修订数", value: formatInt(live.revisionCount) },
                { label: "设备", value: live.deviceId || null, mono: true, copy: live.deviceId || null },
                { label: "创建时间", value: formatDateTime(live.createdAt) },
                { label: "更新时间", value: formatDateTime(live.updatedAt) },
                { label: "SHA-256", value: live.sha256, mono: true, copy: live.sha256, wide: true }
              ]
            : undefined
        }
        raw={raw}
        actions={
          item && live ? (
            <>
              {live.deleted ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => void run(() => actions.restore.mutateAsync(item.id), "已恢复")}
                >
                  {actions.restore.isPending ? <Loader2 className="animate-spin" /> : <ArchiveRestore />}
                  恢复
                </Button>
              ) : (
                <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirm("trash")}>
                  <Trash2 />
                  移入回收站
                </Button>
              )}
              <Button size="sm" variant="destructive" disabled={busy} onClick={() => setConfirm("purge")}>
                <Eraser />
                永久删除
              </Button>
            </>
          ) : null
        }
      >
        {item && live ? (
          <>
            {Object.keys(live.metadata ?? {}).length > 0 ? (
              <div className="flex flex-col gap-2">
                <div className="text-xs font-medium text-muted-foreground">元数据</div>
                <JsonViewer value={JSON.stringify(live.metadata, null, 2)} height={120} allowFullscreen={false} />
              </div>
            ) : null}

            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                  内容{detail ? `（v${detail.contentRevision}）` : ""}
                  {revision ? (
                    <Badge variant="info" size="sm">
                      历史版本
                    </Badge>
                  ) : null}
                </div>
                <div className="flex items-center gap-1.5">
                  {revision ? (
                    <Button size="xs" variant="ghost" onClick={() => setViewing(null)}>
                      回到当前
                    </Button>
                  ) : null}
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => void generateLink(revision)}
                    disabled={actions.link.isPending}
                  >
                    {actions.link.isPending ? <Loader2 className="animate-spin" /> : <Link2 />}
                    生成下载链接
                  </Button>
                </div>
              </div>
              {currentLink ? <LinkBox link={currentLink} /> : null}
              <ContentView
                loading={contentQuery.isLoading}
                error={contentQuery.error}
                detail={detail}
                onRetry={() => void contentQuery.refetch()}
              />
            </div>

            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <History className="size-3.5" />
                修订历史
                {revisions.length ? <span className="tabular-nums">（{revisions.length}）</span> : null}
              </div>
              <RevisionTimeline
                loading={revisionsQuery.isLoading}
                error={revisionsQuery.error}
                onRetry={() => void revisionsQuery.refetch()}
                revisions={revisions}
                activeRevision={revision || live.revision}
                busy={busy || actions.link.isPending}
                onView={(rev) => setViewing({ itemId: item.id, revision: rev.current ? 0 : rev.revision })}
                onRollback={(rev) => setConfirm({ rollback: rev.revision })}
                onLink={(rev) => void generateLink(rev.current ? 0 : rev.revision)}
              />
            </div>
          </>
        ) : null}
      </RecordSheet>

      <AlertDialog open={confirm !== null} onOpenChange={(next) => (!next ? setConfirm(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm === "trash" ? "移入回收站" : confirm === "purge" ? "永久删除条目" : "回滚到历史版本"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "trash"
                ? "条目移入回收站，保留期内可恢复。"
                : confirm === "purge"
                  ? `永久删除该条目及全部 ${live?.revisionCount ?? 0} 个修订，存储桶中的对象一并删除，无法恢复。`
                  : confirm && live
                    ? `以 v${confirm.rollback} 的内容生成新版本 v${live.revision + 1}，历史版本保留。`
                    : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant={confirm === "purge" ? "destructive" : "default"}
              disabled={busy}
              onClick={(event) => {
                event.preventDefault();
                if (!item) return;
                if (confirm === "trash") {
                  void run(() => actions.remove.mutateAsync({ itemId: item.id, permanent: false }), "已移入回收站", true);
                } else if (confirm === "purge") {
                  void run(() => actions.remove.mutateAsync({ itemId: item.id, permanent: true }), "已永久删除", true);
                } else if (confirm) {
                  void run(
                    () => actions.rollback.mutateAsync({ itemId: item.id, revision: confirm.rollback }),
                    `已回滚到 v${confirm.rollback}`
                  );
                }
              }}
            >
              {busy ? <Loader2 className="animate-spin" /> : null}
              {confirm === "trash" ? "移入回收站" : confirm === "purge" ? "永久删除" : "回滚"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function LinkBox({ link }: { link: CloudLink & { revision: number } }) {
  return (
    <div className="flex flex-col gap-2 rounded-xl border border-sky-200 bg-sky-50/60 px-3 py-2.5 dark:border-sky-900/60 dark:bg-sky-950/30">
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate font-mono text-xs" title={link.url}>
          {link.url}
        </code>
        <CopyButton value={link.url} label="下载链接" />
        <Button size="xs" variant="outline" asChild>
          <a href={link.url} target="_blank" rel="noopener noreferrer">
            <ExternalLink />
            打开
          </a>
        </Button>
      </div>
      <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span>{link.revision ? `v${link.revision}` : "当前版本"}</span>
        <span className="tabular-nums">{formatDateTime(link.expiresAt)} 失效</span>
        {link.proxyRequired ? <span>需经控制台代理访问</span> : null}
      </div>
    </div>
  );
}

function RevisionTimeline({
  loading,
  error,
  onRetry,
  revisions,
  activeRevision,
  busy,
  onView,
  onRollback,
  onLink
}: {
  loading: boolean;
  error: unknown;
  onRetry: () => void;
  revisions: CloudRevision[];
  activeRevision: number;
  busy: boolean;
  onView: (revision: CloudRevision) => void;
  onRollback: (revision: CloudRevision) => void;
  onLink: (revision: CloudRevision) => void;
}) {
  if (loading) {
    return (
      <div className="flex flex-col gap-4 rounded-xl border px-3.5 py-3.5">
        {[0, 1, 2].map((index) => (
          <div key={index} className="flex gap-3">
            <Skeleton className="size-6 shrink-0 rounded-full" />
            <div className="flex flex-1 flex-col gap-1.5">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-56" />
            </div>
          </div>
        ))}
      </div>
    );
  }
  if (error && !revisions.length) {
    return (
      <div className="rounded-xl border">
        <StateBlock kind="error" compact description={errorMessage(error)} onRetry={onRetry} />
      </div>
    );
  }
  if (!revisions.length) {
    return (
      <div className="rounded-xl border">
        <StateBlock compact icon={History} title="暂无修订" />
      </div>
    );
  }
  return (
    <ol className="rounded-xl border px-3.5 py-3.5">
      {revisions.map((rev, index) => {
        const last = index === revisions.length - 1;
        const active = activeRevision === rev.revision;
        return (
          <li key={rev.id} className={cn("relative flex gap-3", !last && "pb-4")}>
            {!last ? <span aria-hidden className="absolute top-7 bottom-0 left-3 w-px bg-border" /> : null}
            <span
              className={cn(
                "relative mt-0.5 grid size-6 shrink-0 place-items-center rounded-full text-xs",
                rev.current
                  ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                  : rev.source === "rollback"
                    ? "bg-violet-500/10 text-violet-600 dark:text-violet-400"
                    : rev.source === "admin"
                      ? "bg-sky-500/10 text-sky-600 dark:text-sky-400"
                      : "bg-muted text-muted-foreground"
              )}
            >
              {rev.source === "rollback" ? <Undo2 className="size-3" /> : <span className="size-1.5 rounded-full bg-current" />}
            </span>
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="font-mono text-sm font-medium tabular-nums">v{rev.revision}</span>
                {rev.current ? (
                  <Badge variant="success" size="sm">
                    当前
                  </Badge>
                ) : null}
                <Badge variant="outline" size="sm">
                  {SOURCE_LABELS[rev.source] ?? rev.source}
                  {rev.restoredFrom ? ` v${rev.restoredFrom}` : ""}
                </Badge>
                {active ? (
                  <Badge variant="info" size="sm">
                    查看中
                  </Badge>
                ) : null}
                <span className="text-xs text-muted-foreground tabular-nums">{formatBytes(rev.size)}</span>
              </div>
              <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                <span className="tabular-nums" title={formatDateTime(rev.createdAt)}>
                  {formatDateTime(rev.createdAt)}
                </span>
                {rev.operator ? <span>操作人 {rev.operator}</span> : null}
                {rev.deviceId ? <span className="truncate font-mono">{rev.deviceId}</span> : null}
              </div>
              <div className="flex flex-wrap gap-1 pt-0.5">
                <Button size="xs" variant={active ? "secondary" : "ghost"} onClick={() => onView(rev)}>
                  <Eye />
                  查看
                </Button>
                <Button size="xs" variant="ghost" disabled={busy} onClick={() => onLink(rev)}>
                  <Link2 />
                  链接
                </Button>
                {!rev.current ? (
                  <Button size="xs" variant="ghost" disabled={busy} onClick={() => onRollback(rev)}>
                    <Undo2 />
                    回滚到此版本
                  </Button>
                ) : null}
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function ContentView({
  loading,
  error,
  detail,
  onRetry
}: {
  loading: boolean;
  error: unknown;
  detail?: CloudItemContent;
  onRetry: () => void;
}) {
  if (loading) {
    return <Skeleton className="h-40 w-full rounded-xl" />;
  }
  if (error || !detail) {
    return (
      <div className="rounded-xl border">
        <StateBlock kind="error" compact title="内容读取失败" description={errorMessage(error, "")} onRetry={onRetry} />
      </div>
    );
  }
  if (detail.contentOmitted) {
    return (
      <div className="rounded-xl border border-dashed">
        <StateBlock compact icon={Download} title="内容较大" description={`${formatBytes(detail.size)}，请生成下载链接查看`} />
      </div>
    );
  }
  if (detail.encoding === "json") {
    return <JsonViewer value={JSON.stringify(detail.content ?? null, null, 2)} height={detail.size > MiB / 8 ? 360 : 240} />;
  }
  if (detail.encoding === "text") {
    return <JsonViewer value={String(detail.content ?? "")} language="text" height={240} />;
  }
  return (
    <div className="rounded-xl border border-dashed">
      <StateBlock
        compact
        icon={FileText}
        title="二进制内容"
        description={`${formatBytes(detail.size)}，${detail.contentType}，请生成下载链接查看`}
      />
    </div>
  );
}

// ── 危险区 ──────────────────────────

function DangerSection({ appKey, userId, state }: { appKey: string; userId: number; state: CloudAdminUser }) {
  const purgeMutation = usePurgeCloudStorageUserMutation(appKey, userId);
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const expected = state.account || String(userId);
  const count = state.itemCount + state.trashCount;
  const syncRecords = state.syncRecords ?? 0;
  const devices = state.devices?.length ?? 0;
  const empty = count === 0 && syncRecords === 0 && devices === 0;

  const purge = async () => {
    try {
      await purgeMutation.mutateAsync();
      toast.success("已清空该用户的云存储");
      setOpen(false);
      setTyped("");
    } catch (error) {
      toast.error(errorMessage(error, "清空失败"));
    }
  };

  return (
    <Section title="危险区" description="以下操作不可撤销" icon={TriangleAlert} tone="danger" className="border-red-200 dark:border-red-900/60">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="text-sm font-medium">清空云存储</span>
          <span className="text-xs leading-5 text-muted-foreground">
            永久删除全部 {formatInt(count)} 个条目、{formatInt(state.revisionCount)} 个修订与 {formatInt(syncRecords)}{" "}
            条同步记录，存储桶中的对象一并删除，同步设备全部移除。配额与冻结设置保留。删除用户前建议先执行此操作。
          </span>
        </div>
        <Button
          size="sm"
          variant="destructive"
          className="self-start sm:self-auto"
          disabled={empty}
          onClick={() => {
            setTyped("");
            setOpen(true);
          }}
        >
          <Eraser />
          清空云存储
        </Button>
      </div>

      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清空云存储</AlertDialogTitle>
            <AlertDialogDescription>
              永久删除该用户的 {formatInt(count)} 个条目、全部 {formatInt(state.revisionCount)} 个修订与{" "}
              {formatInt(syncRecords)} 条同步记录（共 {formatBytes(cloudUsedBytes(state))}），移除 {formatInt(devices)} 台设备，无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`cloud-purge-${userId}`} className="text-xs text-muted-foreground">
              输入 <span className="font-mono font-medium text-foreground">{expected}</span> 以确认
            </Label>
            <Input
              id={`cloud-purge-${userId}`}
              value={typed}
              autoComplete="off"
              onChange={(event) => setTyped(event.target.value)}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={typed.trim() !== expected || purgeMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void purge();
              }}
            >
              {purgeMutation.isPending ? <Loader2 className="animate-spin" /> : <Eraser />}
              清空
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Section>
  );
}
