"use client";

import { useMemo, useState } from "react";
import {
  ArchiveRestore,
  Cloud,
  Download,
  Eraser,
  FileText,
  HardDrive,
  History,
  Loader2,
  Lock,
  Save,
  Search,
  Snowflake,
  Trash2,
  Undo2
} from "lucide-react";
import { toast } from "sonner";
import {
  EncodingBadge,
  MiB,
  NamespaceLabel,
  QuotaMeter,
  SOURCE_LABELS,
  bytesToMBInput,
  formatBytes,
  formatDateTime,
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { CloudAdminUser, CloudItem, CloudItemContent, CloudItemParams } from "@/lib/api/cloud-storage";
import {
  useCloudStorageItemActions,
  useCloudStorageUserItemQuery,
  useCloudStorageUserItemRevisionsQuery,
  useCloudStorageUserItemsQuery,
  useCloudStorageUserQuery,
  usePurgeCloudStorageUserMutation,
  useSaveCloudStorageUserMutation
} from "@/lib/cloud-storage-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { EmptyRow, Fact, Facts, Panel, StatTile } from "./user-detail-shared";

const PAGE_SIZE = 20;
const ALL_NAMESPACES = "__all__";

type Status = NonNullable<CloudItemParams["status"]>;

/**
 * 云存储：这个用户在应用里存了什么、占了多少，以及管理员能对它做的事。
 *
 *   账目与权限   用量、配额覆盖、冻结（只读）与备注
 *   条目         按命名空间与回收站筛选；点开看内容、修订，回滚 / 删除 / 恢复
 *   清空         永久删除这个人的全部云存储数据（删除用户之前应先做这一步，
 *                否则存储桶里的对象不会跟着用户一起删）
 *
 * 管理端的写入不受冻结约束，修订上会记下操作人。
 */
export function UserCloudTab({ appKey, userId }: { appKey: string; userId: number }) {
  const userQuery = useCloudStorageUserQuery(appKey, userId);
  const state = userQuery.data;

  if (userQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }
  if (!state) {
    return <EmptyRow text="云存储数据不可用" />;
  }

  const percent = usagePercent(state.usedBytes, state.quotaBytes);

  return (
    <div className="space-y-5">
      {!state.enabled ? (
        <div className="rounded-xl border border-dashed border-border px-4 py-3 text-xs text-muted-foreground">
          该应用尚未启用云存储，以下为历史数据。
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        <StatTile
          label="已用空间"
          value={formatBytes(state.usedBytes)}
          icon={<HardDrive className="size-3.5" />}
          hint={`共 ${formatBytes(state.quotaBytes)}，已用 ${percent}%`}
          tone={percent >= 90 ? "danger" : percent >= 70 ? "warning" : "default"}
        />
        <StatTile label="条目" value={state.itemCount} icon={<FileText className="size-3.5" />} hint={`上限 ${state.limits.maxItems}`} />
        <StatTile label="回收站" value={state.trashCount} icon={<Trash2 className="size-3.5" />} hint={`保留 ${state.limits.trashRetentionDays} 天`} />
        <StatTile
          label="状态"
          value={state.frozen ? "已冻结" : "正常"}
          icon={state.frozen ? <Snowflake className="size-3.5" /> : <Cloud className="size-3.5" />}
          hint={state.frozen ? state.frozenReason || "只读" : `最近写入 ${formatDateTime(state.lastWriteAt)}`}
          tone={state.frozen ? "info" : "default"}
        />
      </div>

      <AccountPanel appKey={appKey} userId={userId} state={state} />
      <ItemsPanel appKey={appKey} userId={userId} state={state} />
    </div>
  );
}

// ── 账目与权限 ──────────────────────────

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

function AccountPanel({ appKey, userId, state }: { appKey: string; userId: number; state: CloudAdminUser }) {
  const scope = `${appKey}:${userId}:${state.updatedAt ?? ""}`;
  const [draft, setDraft] = useState<AccountDraft | null>(null);
  const [confirmPurge, setConfirmPurge] = useState(false);
  const saveMutation = useSaveCloudStorageUserMutation(appKey, userId);
  const purgeMutation = usePurgeCloudStorageUserMutation(appKey, userId);

  const current = draft?.scope === scope ? draft : seedAccount(scope, state);
  const patch = (changes: Partial<AccountDraft>) => setDraft({ ...current, ...changes, scope });

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
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const purge = async () => {
    try {
      await purgeMutation.mutateAsync();
      toast.success("已清空该用户的云存储");
      setConfirmPurge(false);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "清空失败");
    }
  };

  return (
    <Panel
      title="空间与权限"
      icon={<Lock className="size-4" />}
      action={
        <>
          <Button
            size="sm"
            variant="outline"
            className="text-destructive hover:text-destructive"
            disabled={state.itemCount + state.trashCount === 0}
            onClick={() => setConfirmPurge(true)}
          >
            <Eraser className="size-3.5" />
            清空云存储
          </Button>
          <Button size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
            {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </>
      }
    >
      <div className="grid gap-6 lg:grid-cols-2">
        <div className="space-y-4">
          <QuotaMeter used={state.usedBytes} quota={state.quotaBytes} />
          <div className="flex items-center justify-between gap-3">
            <div className="space-y-0.5">
              <Label className="text-xs font-medium">自定义配额</Label>
              <p className="text-[11px] text-muted-foreground">
                {current.customQuota ? "覆盖应用默认配额" : `沿用应用默认 ${formatBytes(state.limits.quotaBytes)}`}
              </p>
            </div>
            <Switch checked={current.customQuota} onCheckedChange={(value) => patch({ customQuota: value })} />
          </div>
          {current.customQuota ? (
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={0}
                step="any"
                className="h-8 w-36 font-mono text-xs"
                value={current.quotaMB}
                onChange={(event) => patch({ quotaMB: event.target.value })}
              />
              <span className="text-xs text-muted-foreground">MB</span>
              {state.usedBytes > (mbInputToBytes(current.quotaMB) || 0) ? (
                <span className="text-[11px] text-amber-600 dark:text-amber-400">低于已用空间，之后的写入会被拒绝</span>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="space-y-0.5">
              <Label className="text-xs font-medium">冻结</Label>
              <p className="text-[11px] text-muted-foreground">冻结后只读，写入、删除与回滚均被拒绝</p>
            </div>
            <Switch checked={current.frozen} onCheckedChange={(value) => patch({ frozen: value })} />
          </div>
          {current.frozen ? (
            <Input
              className="h-8 text-xs"
              value={current.frozenReason}
              maxLength={255}
              placeholder="冻结原因（对用户可见）"
              onChange={(event) => patch({ frozenReason: event.target.value })}
            />
          ) : null}
          <div className="space-y-1.5">
            <Label className="text-xs font-medium">备注</Label>
            <Input
              className="h-8 text-xs"
              value={current.note}
              maxLength={255}
              placeholder="仅管理员可见"
              onChange={(event) => patch({ note: event.target.value })}
            />
          </div>
          {state.updatedBy ? (
            <p className="text-[11px] text-muted-foreground">
              最近由 {state.updatedBy} 于 {formatDateTime(state.updatedAt)} 修改
            </p>
          ) : null}
        </div>
      </div>

      <AlertDialog open={confirmPurge} onOpenChange={setConfirmPurge}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清空云存储</AlertDialogTitle>
            <AlertDialogDescription>
              永久删除该用户的 {state.itemCount + state.trashCount} 个条目及全部 {state.revisionCount} 个修订，存储桶中的对象一并删除，无法恢复。配额与冻结设置保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              disabled={purgeMutation.isPending}
              onClick={(event) => {
                event.preventDefault();
                void purge();
              }}
            >
              {purgeMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
              清空
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Panel>
  );
}

// ── 条目 ──────────────────────────

function ItemsPanel({ appKey, userId, state }: { appKey: string; userId: number; state: CloudAdminUser }) {
  const [namespace, setNamespace] = useState(ALL_NAMESPACES);
  const [status, setStatus] = useState<Status>("active");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [openItem, setOpenItem] = useState<CloudItem | null>(null);
  const debouncedKeyword = useDebouncedValue(keyword, 300);

  const listQuery = useCloudStorageUserItemsQuery(appKey, userId, {
    namespace: namespace === ALL_NAMESPACES ? undefined : namespace,
    status,
    keyword: debouncedKeyword || undefined,
    page,
    limit: PAGE_SIZE
  });
  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const namespaceNames = useMemo(
    () => Object.fromEntries(state.namespaces.map((item) => [item.namespace, item.name ?? ""])),
    [state.namespaces]
  );

  return (
    <Panel
      title="条目"
      icon={<FileText className="size-4" />}
      description={`单条上限 ${formatBytes(state.limits.maxItemBytes)}，每个条目保留 ${state.limits.maxRevisions} 个修订`}
      bodyClassName="space-y-3"
    >
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          type="single"
          size="sm"
          value={status}
          onValueChange={(value) => {
            if (!value) return;
            setStatus(value as Status);
            setPage(1);
          }}
        >
          <ToggleGroupItem value="active" className="h-8 px-3 text-xs">
            正常
          </ToggleGroupItem>
          <ToggleGroupItem value="deleted" className="h-8 px-3 text-xs">
            回收站
          </ToggleGroupItem>
          <ToggleGroupItem value="all" className="h-8 px-3 text-xs">
            全部
          </ToggleGroupItem>
        </ToggleGroup>
        <Select
          value={namespace}
          onValueChange={(value) => {
            setNamespace(value);
            setPage(1);
          }}
        >
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_NAMESPACES}>全部命名空间</SelectItem>
            {state.namespaces.map((item) => (
              <SelectItem key={item.namespace} value={item.namespace}>
                {item.name ? `${item.name}（${item.namespace}）` : item.namespace}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setPage(1);
            }}
            placeholder="搜索键"
            className="h-8 w-48 pl-8 text-xs"
          />
        </div>
      </div>

      {listQuery.isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyRow text={status === "deleted" ? "回收站为空" : "暂无条目"} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>条目</TableHead>
              <TableHead>类型</TableHead>
              <TableHead className="text-right">版本</TableHead>
              <TableHead className="text-right">大小 / 占用</TableHead>
              <TableHead className="text-right">更新</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id} className="cursor-pointer" onClick={() => setOpenItem(item)}>
                <TableCell className="text-xs">
                  <div className="flex items-center gap-1.5">
                    <span className="font-mono font-medium">{item.key}</span>
                    {item.deleted ? (
                      <Badge variant="warning" size="sm" className="font-normal">
                        回收站
                      </Badge>
                    ) : null}
                  </div>
                  <div className="mt-0.5">
                    <NamespaceLabel namespace={item.namespace} name={namespaceNames[item.namespace]} />
                  </div>
                </TableCell>
                <TableCell>
                  <EncodingBadge encoding={item.encoding} />
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  v{item.revision}
                  <span className="text-muted-foreground"> / {item.revisionCount}</span>
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">
                  {formatBytes(item.size)}
                  <span className="text-muted-foreground"> / {formatBytes(item.storedBytes)}</span>
                </TableCell>
                <TableCell className="text-right text-xs text-muted-foreground">
                  {item.deleted ? `清除于 ${formatDateTime(item.purgeAt)}` : formatDateTime(item.updatedAt)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {total > PAGE_SIZE ? (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span className="tabular-nums">
            共 {total} 条，第 {page}/{totalPages} 页
          </span>
          <div className="flex gap-1.5">
            <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={page >= totalPages}
              onClick={() => setPage((value) => value + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      ) : null}

      <ItemSheet
        appKey={appKey}
        userId={userId}
        item={openItem}
        namespaceName={openItem ? namespaceNames[openItem.namespace] : undefined}
        onClose={() => setOpenItem(null)}
      />
    </Panel>
  );
}

// ── 条目详情 ──────────────────────────

function ItemSheet({
  appKey,
  userId,
  item,
  namespaceName,
  onClose
}: {
  appKey: string;
  userId: number;
  item: CloudItem | null;
  namespaceName?: string;
  onClose: () => void;
}) {
  const [viewing, setViewing] = useState<{ itemId: number; revision: number } | null>(null);
  const [confirm, setConfirm] = useState<"trash" | "purge" | { rollback: number } | null>(null);
  const itemId = item?.id ?? null;
  const revision = viewing && viewing.itemId === itemId ? viewing.revision : 0;

  const contentQuery = useCloudStorageUserItemQuery(appKey, userId, itemId, revision || undefined);
  const revisionsQuery = useCloudStorageUserItemRevisionsQuery(appKey, userId, itemId);
  const actions = useCloudStorageItemActions(appKey, userId);
  const detail = contentQuery.data;
  const live = detail ?? item;
  const revisions = revisionsQuery.data?.items ?? [];
  const busy =
    actions.rollback.isPending || actions.remove.isPending || actions.restore.isPending || actions.link.isPending;

  const run = async (task: () => Promise<unknown>, success: string, closeAfter = false) => {
    try {
      await task();
      toast.success(success);
      setConfirm(null);
      setViewing(null);
      if (closeAfter) onClose();
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "操作失败");
    }
  };

  const download = async () => {
    if (!itemId) return;
    try {
      const link = await actions.link.mutateAsync({ itemId, revision: revision || undefined });
      window.open(link.url, "_blank", "noopener,noreferrer");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "获取下载地址失败");
    }
  };

  return (
    <Sheet open={item !== null} onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-2xl">
        {item && live ? (
          <>
            <SheetHeader className="border-b">
              <SheetTitle className="flex items-center gap-2 font-mono">
                {item.key}
                {live.deleted ? (
                  <Badge variant="warning" size="sm" className="font-sans font-normal">
                    回收站
                  </Badge>
                ) : null}
              </SheetTitle>
              <SheetDescription asChild>
                <div>
                  <NamespaceLabel namespace={item.namespace} name={namespaceName} />
                </div>
              </SheetDescription>
            </SheetHeader>

            <div className="space-y-5 p-4">
              <Facts columns={2}>
                <Fact label="当前版本" value={`v${live.revision}`} mono />
                <Fact label="编码" value={<EncodingBadge encoding={live.encoding} />} />
                <Fact label="内容类型" value={live.contentType} mono />
                <Fact label="大小" value={formatBytes(live.size)} hint={`全部修订 ${formatBytes(live.storedBytes)}`} />
                <Fact label="设备" value={live.deviceId || undefined} mono />
                <Fact label="更新时间" value={formatDateTime(live.updatedAt)} />
                <Fact label="创建时间" value={formatDateTime(live.createdAt)} />
                <Fact
                  label="回收站"
                  value={live.deleted ? `清除于 ${formatDateTime(live.purgeAt)}` : "否"}
                  tone={live.deleted ? "warning" : "muted"}
                />
              </Facts>
              <Fact label="SHA-256" value={live.sha256} mono />
              {Object.keys(live.metadata ?? {}).length > 0 ? (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">元数据</Label>
                  <JsonViewer value={JSON.stringify(live.metadata, null, 2)} height={120} allowFullscreen={false} />
                </div>
              ) : null}

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <Label className="text-xs text-muted-foreground">
                    内容{detail ? `（v${detail.contentRevision}）` : ""}
                    {revision ? "（历史版本）" : ""}
                  </Label>
                  <Button size="xs" variant="outline" onClick={() => void download()} disabled={busy}>
                    <Download className="size-3" />
                    下载
                  </Button>
                </div>
                <ContentView loading={contentQuery.isLoading} error={contentQuery.error} detail={detail} />
              </div>

              <div className="space-y-2">
                <Label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <History className="size-3.5" />
                  修订
                </Label>
                {revisionsQuery.isLoading ? (
                  <Skeleton className="h-24 w-full" />
                ) : revisions.length === 0 ? (
                  <EmptyRow text="暂无修订" />
                ) : (
                  <ul className="divide-y divide-border rounded-xl border border-border">
                    {revisions.map((rev) => {
                      const active = (revision || live.revision) === rev.revision;
                      return (
                        <li key={rev.id} className="flex items-center gap-3 px-3 py-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5 text-xs">
                              <span className="font-mono font-medium">v{rev.revision}</span>
                              {rev.current ? (
                                <Badge variant="success" size="sm" className="font-normal">
                                  当前
                                </Badge>
                              ) : null}
                              <Badge variant="outline" size="sm" className="font-normal">
                                {SOURCE_LABELS[rev.source] ?? rev.source}
                                {rev.restoredFrom ? ` v${rev.restoredFrom}` : ""}
                              </Badge>
                              <span className="font-mono text-muted-foreground">{formatBytes(rev.size)}</span>
                            </div>
                            <p className="mt-0.5 text-[11px] text-muted-foreground">
                              {formatDateTime(rev.createdAt)}
                              {rev.operator ? `，${rev.operator}` : ""}
                              {rev.deviceId ? `，${rev.deviceId}` : ""}
                            </p>
                          </div>
                          <Button
                            size="xs"
                            variant={active ? "secondary" : "ghost"}
                            onClick={() => setViewing({ itemId: item.id, revision: rev.current ? 0 : rev.revision })}
                          >
                            查看
                          </Button>
                          {!rev.current ? (
                            <Button
                              size="xs"
                              variant="ghost"
                              disabled={busy}
                              onClick={() => setConfirm({ rollback: rev.revision })}
                            >
                              <Undo2 className="size-3" />
                              回滚
                            </Button>
                          ) : null}
                        </li>
                      );
                    })}
                  </ul>
                )}
              </div>

              <div className="flex flex-wrap justify-end gap-2 border-t border-border pt-4">
                {live.deleted ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => void run(() => actions.restore.mutateAsync(item.id), "已恢复")}
                  >
                    <ArchiveRestore className="size-3.5" />
                    恢复
                  </Button>
                ) : (
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirm("trash")}>
                    <Trash2 className="size-3.5" />
                    移入回收站
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  className="text-destructive hover:text-destructive"
                  disabled={busy}
                  onClick={() => setConfirm("purge")}
                >
                  <Eraser className="size-3.5" />
                  永久删除
                </Button>
              </div>
            </div>

            <AlertDialog open={confirm !== null} onOpenChange={(open) => (!open ? setConfirm(null) : undefined)}>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>
                    {confirm === "trash" ? "移入回收站" : confirm === "purge" ? "永久删除" : "回滚"}
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    {confirm === "trash"
                      ? "条目移入回收站，保留期内可恢复。"
                      : confirm === "purge"
                        ? `永久删除该条目及全部 ${live.revisionCount} 个修订，无法恢复。`
                        : confirm
                          ? `以 v${confirm.rollback} 的内容生成新版本 v${live.revision + 1}，历史版本保留。`
                          : ""}
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>取消</AlertDialogCancel>
                  <AlertDialogAction
                    className={confirm === "purge" ? "bg-destructive text-white hover:bg-destructive/90" : undefined}
                    disabled={busy}
                    onClick={(event) => {
                      event.preventDefault();
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
                    {busy ? <Loader2 className="size-3.5 animate-spin" /> : null}
                    确定
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function ContentView({
  loading,
  error,
  detail
}: {
  loading: boolean;
  error: unknown;
  detail?: CloudItemContent;
}) {
  if (loading) {
    return <Skeleton className="h-40 w-full" />;
  }
  if (error || !detail) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
        {error instanceof ApiError ? error.message : "内容读取失败"}
      </p>
    );
  }
  if (detail.contentOmitted) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
        内容较大（{formatBytes(detail.size)}），请下载查看
      </p>
    );
  }
  if (detail.encoding === "json") {
    return <JsonViewer value={JSON.stringify(detail.content ?? null, null, 2)} height={detail.size > MiB / 8 ? 360 : 240} />;
  }
  if (detail.encoding === "text") {
    return <JsonViewer value={String(detail.content ?? "")} language="text" height={240} />;
  }
  return (
    <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-xs text-muted-foreground">
      二进制内容（{formatBytes(detail.size)}，{detail.contentType}），请下载查看
    </p>
  );
}
