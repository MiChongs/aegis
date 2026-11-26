"use client";

import { useState } from "react";
import Link from "next/link";
import { ArchiveRestore, ExternalLink, EyeOff, ImageOff, Loader2, ScanEye } from "lucide-react";
import { toast } from "sonner";
import { UserPicker, type PickedUser } from "@/components/commerce/user-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { QuickChips } from "@/components/users/detail/user-assets-adjust";
import {
  FilterBar,
  FilterSelect,
  Pager,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  TONE_TEXT,
  errorMessage,
  useDebouncedValue
} from "@/components/users/detail/kit";
import type { CosmeticCatalog, WorkshopAdminItem } from "@/lib/api/cosmetics";
import { useSetWorkshopItemStatusMutation, useWorkshopItemsQuery } from "@/lib/cosmetic-hooks";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { SlotDesign } from "./cosmetic-preview";
import { SLOT_META, WORKSHOP_STATUS_META, slotLabel, workshopSlots } from "./cosmetic-shared";

/**
 * 作品巡查（docs/cosmetics.md「自定义工坊」）：用户在工坊里做的作品，服务端分页，按槽位 / 状态 / 关键字 / 作者筛选，
 * 任何一项筛选变化都回到第 1 页。列表接口直接带设计稿，每件用渲染器实时播放（离开视口即停帧）。
 *
 * 下架必须填原因，作者在工坊里看得到；下架后作品戴不上、不能再改（只能删除），恢复后照常。
 * 作品名称只有作者与管理端看得到，别人看到的是「工坊作品」。账号点一下即按该作者筛选。
 */

const PAGE_SIZE = 24;
const TAKEDOWN_REASONS = ["含有不当内容", "仿冒官方装扮", "侵犯他人权益", "违反社区规范"];
const MAX_REASON = 128;

export function CosmeticsWorkshopItemsPanel({
  appKey,
  catalog,
  canWrite
}: {
  appKey: string;
  catalog?: CosmeticCatalog | null;
  canWrite: boolean;
}) {
  const [user, setUser] = useState<PickedUser | null>(null);
  const [slot, setSlot] = useState("");
  const [status, setStatus] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(PAGE_SIZE);
  const [takingDown, setTakingDown] = useState<WorkshopAdminItem | null>(null);
  const [restoring, setRestoring] = useState<number | null>(null);
  const debounced = useDebouncedValue(keyword.trim());
  const query = useWorkshopItemsQuery(appKey, { userId: user?.id, slot, status, keyword: debounced, page, limit });
  const setItemStatus = useSetWorkshopItemStatusMutation(appKey);
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const filtered = Boolean(user || slot || status || keyword.trim());

  const reset = () => {
    setUser(null);
    setSlot("");
    setStatus("");
    setKeyword("");
    setPage(1);
  };

  const filterAuthor = (item: WorkshopAdminItem) => {
    setUser({ id: item.creatorUserId, account: item.creatorAccount, nickname: item.creatorNickname });
    setPage(1);
  };

  const restore = async (item: WorkshopAdminItem) => {
    setRestoring(item.id);
    try {
      await setItemStatus.mutateAsync({ itemId: item.id, takedown: false });
      toast.success(`已恢复「${item.name}」`);
    } catch (error) {
      toast.error(errorMessage(error, "恢复失败"));
    } finally {
      setRestoring(null);
    }
  };

  return (
    <>
      <Section
        title="作品巡查"
        icon={ScanEye}
        tone="violet"
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
              value={slot}
              onChange={(value) => {
                setSlot(value);
                setPage(1);
              }}
              label="槽位"
              options={workshopSlots(catalog).map((key) => ({ value: key, label: slotLabel(key, catalog) }))}
            />
            <FilterSelect
              value={status}
              onChange={(value) => {
                setStatus(value);
                setPage(1);
              }}
              label="状态"
              options={(Object.keys(WORKSHOP_STATUS_META) as Array<keyof typeof WORKSHOP_STATUS_META>).map((key) => ({
                value: key,
                label: WORKSHOP_STATUS_META[key].label
              }))}
            />
            <SearchField
              value={keyword}
              onChange={(value) => {
                setKeyword(value);
                setPage(1);
              }}
              placeholder="作品名称"
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
              limits={[24, 48, 96]}
              onPage={setPage}
              onLimit={(next) => {
                setLimit(next);
                setPage(1);
              }}
            />
          ) : undefined
        }
      >
        {query.isLoading ? (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
            {Array.from({ length: 8 }, (_, index) => (
              <Skeleton key={index} className="h-[260px] rounded-xl" />
            ))}
          </div>
        ) : query.isError && !query.data ? (
          <StateBlock kind="error" description={errorMessage(query.error)} onRetry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <StateBlock compact icon={ScanEye} title={filtered ? "没有符合条件的作品" : "暂无作品"} />
        ) : (
          <div className={cn("grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4", query.isFetching && "opacity-70")}>
            {rows.map((item) => (
              <WorkshopItemCard
                key={item.id}
                appKey={appKey}
                item={item}
                catalog={catalog}
                canWrite={canWrite}
                restoring={restoring === item.id}
                onFilterAuthor={() => filterAuthor(item)}
                onTakedown={() => setTakingDown(item)}
                onRestore={() => void restore(item)}
              />
            ))}
          </div>
        )}
      </Section>

      {takingDown ? <TakedownDialog key={takingDown.id} appKey={appKey} item={takingDown} onClose={() => setTakingDown(null)} /> : null}
    </>
  );
}

function WorkshopItemCard({
  appKey,
  item,
  catalog,
  canWrite,
  restoring,
  onFilterAuthor,
  onTakedown,
  onRestore
}: {
  appKey: string;
  item: WorkshopAdminItem;
  catalog?: CosmeticCatalog | null;
  canWrite: boolean;
  restoring: boolean;
  onFilterAuthor: () => void;
  onTakedown: () => void;
  onRestore: () => void;
}) {
  const meta = SLOT_META[item.slot];
  const archived = item.status === "archived";
  const statusMeta = WORKSHOP_STATUS_META[archived ? "archived" : "active"];
  const author = item.creatorNickname || item.creatorAccount || `用户 #${item.creatorUserId}`;

  return (
    <div className={cn("flex flex-col overflow-hidden rounded-xl border bg-card", archived && "opacity-80")}>
      <div className="relative flex h-40 items-center justify-center bg-muted/40">
        <ItemPreview item={item} />
        <span className="absolute top-2 left-2">
          <Badge variant="outline" size="sm" className="gap-1 bg-card font-normal">
            {meta ? <meta.icon className="size-3" /> : null}
            {slotLabel(item.slot, catalog)}
          </Badge>
        </span>
        <span className="absolute top-2 right-2">
          <Badge variant={statusMeta.variant} size="sm" className={archived ? "bg-card" : undefined}>
            {statusMeta.label}
          </Badge>
        </span>
      </div>

      <div className="flex flex-1 flex-col gap-2 border-t px-3 py-2.5">
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate text-sm font-medium" title={item.name}>
            {item.name}
          </span>
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">第 {item.revision} 版</span>
        </div>

        <div className="flex min-w-0 items-center gap-1.5">
          <button
            type="button"
            onClick={onFilterAuthor}
            className="flex min-w-0 flex-col text-left hover:underline"
            title="只看这位作者的作品"
          >
            <span className="truncate text-xs">{author}</span>
            {item.creatorNickname && item.creatorAccount ? (
              <span className="truncate text-[11px] text-muted-foreground">{item.creatorAccount}</span>
            ) : null}
          </button>
          <Link
            href={`/app-users/${encodeURIComponent(appKey)}/${item.creatorUserId}?tab=cosmetics`}
            className="shrink-0 text-muted-foreground hover:text-foreground"
            aria-label="打开用户详情"
            title="打开用户详情"
          >
            <ExternalLink className="size-3.5" />
          </Link>
          <span className="ml-auto shrink-0 text-[11px] tabular-nums text-muted-foreground" title={formatDateTimeMinute(item.updatedAt)}>
            {formatRelative(item.updatedAt)}更新
          </span>
        </div>

        {archived && item.takedownReason ? (
          <p className={cn("line-clamp-2 text-xs", TONE_TEXT.warning)} title={item.takedownReason}>
            下架原因：{item.takedownReason}
          </p>
        ) : null}

        {canWrite ? (
          <div className="mt-auto flex justify-end pt-1">
            {archived ? (
              <Button size="xs" variant="outline" disabled={restoring} onClick={onRestore}>
                {restoring ? <Loader2 className="animate-spin" /> : <ArchiveRestore />}
                恢复
              </Button>
            ) : (
              <Button size="xs" variant="outline" onClick={onTakedown}>
                <EyeOff />
                下架
              </Button>
            )}
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** 按槽位挑一个合适的预览尺寸：头像框与勋章是方的，资料卡是 16:9，昵称样式画作品名 */
function ItemPreview({ item }: { item: WorkshopAdminItem }) {
  const design = item.design;
  if (!design) return <ImageOff className="size-5 text-muted-foreground" />;
  if (item.slot === "name_style") {
    return <SlotDesign slot={item.slot} design={design} width={220} height={60} nameText={item.name} fontSize={22} />;
  }
  if (item.slot === "profile_card") return <SlotDesign slot={item.slot} design={design} width={240} height={135} />;
  if (item.slot === "badge") return <SlotDesign slot={item.slot} design={design} width={88} height={88} />;
  return <SlotDesign slot={item.slot} design={design} width={136} height={136} />;
}

function TakedownDialog({ appKey, item, onClose }: { appKey: string; item: WorkshopAdminItem; onClose: () => void }) {
  const [reason, setReason] = useState("");
  const setItemStatus = useSetWorkshopItemStatusMutation(appKey);
  const trimmed = reason.trim();

  const submit = async () => {
    if (!trimmed) {
      toast.error("请填写下架原因");
      return;
    }
    try {
      await setItemStatus.mutateAsync({ itemId: item.id, takedown: true, reason: trimmed });
      toast.success(`已下架「${item.name}」`);
      onClose();
    } catch (error) {
      toast.error(errorMessage(error, "下架失败"));
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !setItemStatus.isPending && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>下架「{item.name}」？</DialogTitle>
          <DialogDescription>作者会看到下架原因；下架后作品戴不上，也不能再修改。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <Textarea
            value={reason}
            maxLength={MAX_REASON}
            rows={3}
            onChange={(event) => setReason(event.target.value)}
            placeholder="下架原因（必填，作者会看到）"
            aria-label="下架原因"
          />
          <QuickChips items={TAKEDOWN_REASONS} value={trimmed} onPick={setReason} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={setItemStatus.isPending}>
            取消
          </Button>
          <Button variant="destructive" onClick={() => void submit()} disabled={!trimmed || setItemStatus.isPending}>
            {setItemStatus.isPending ? <Loader2 className="animate-spin" /> : <EyeOff />}
            下架
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
