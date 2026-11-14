"use client";

import { useMemo, useState } from "react";
import { Inbox, MessageSquareWarning, Scale, Search, Settings2 } from "lucide-react";

import { TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { ErrorState } from "@/components/risk/decisions";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import type { AppealQueue, BanAppeal } from "@/lib/api/ban-appeals";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useBanAppeals, useBanAppealStats } from "@/lib/ban-appeal-hooks";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { usePermissionChecker } from "@/lib/permissions";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";

import { AppealStateBadge, QUEUES, VerdictChip, banTitle, reasonCodeLabel, useWideScreen, userName } from "./appeal-kit";
import { AppealReview } from "./appeal-review";
import { AppealSettingsDialog } from "./appeal-settings";

const PAGE_SIZE = 20;

/**
 * 风控平台 → 封禁申诉。
 *
 * 左边是按状态分好的申诉队列（每条一句话：谁、什么时候、申诉哪条封禁、模型怎么看），右边是复审面板；
 * 窄屏时复审面板改用对话框。选了应用就走应用级接口并可改申诉设置，没选时看全部应用（system:platform_ban:read）。
 */
export function BanAppeals({ app }: { app?: number }) {
  const apps = useAdminAppsQuery();
  const byId = useMemo(() => new Map((apps.data ?? []).map((a) => [a.id, a])), [apps.data]);
  const scoped = app ? byId.get(app) : undefined;
  const appKey = scoped?.appKey;
  // 跨应用申诉列表挂在 /system/ban-appeals 下，归「平台封禁：查看」
  const canCrossApp = usePermissionChecker()("system:platform_ban:read");
  const wide = useWideScreen();

  const [queue, setQueue] = useState<AppealQueue>("open");
  const [keyword, setKeyword] = useState("");
  const q = useDebouncedValue(keyword.trim(), 300);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<{ appid: number; id: number } | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);

  // 选了应用但应用列表还没回来：先别用跨应用接口查
  const waiting = !!app && !appKey;
  const blocked = !app && !canCrossApp;
  const list = useBanAppeals(appKey, { status: queue, q: q || undefined, page, pageSize: PAGE_SIZE }, !waiting && !blocked);
  const stats = useBanAppealStats(appKey, !waiting && !blocked);
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;

  const pick = (next: AppealQueue) => {
    setQueue(next);
    setPage(1);
    setSelected(null);
  };

  const keyOf = (appid: number) => byId.get(appid)?.appKey;
  const nameOf = (appid: number, fallback?: string) => fallback || byId.get(appid)?.name || `应用 #${appid}`;

  /** 处理完一条后跳到队列里的下一条 */
  const goNext = () => {
    if (!selected) return;
    const idx = items.findIndex((i) => i.id === selected.id && i.appid === selected.appid);
    const next = idx >= 0 ? items[idx + 1] ?? items[idx - 1] : undefined;
    setSelected(next ? { appid: next.appid, id: next.id } : null);
  };

  if (blocked) {
    return (
      <section className="rounded-xl border bg-card">
        <CardEmpty icon={Scale} title="请先选择应用" description="查看全部应用的申诉需要「平台封禁：查看」权限" />
      </section>
    );
  }

  const current = selected ? items.find((i) => i.id === selected.id && i.appid === selected.appid) : undefined;
  const currentKey = selected ? keyOf(selected.appid) : undefined;
  const review =
    selected && currentKey ? (
      <AppealReview
        key={`${selected.appid}-${selected.id}`}
        appKey={currentKey}
        appealId={selected.id}
        appName={app ? undefined : nameOf(selected.appid, current?.appName)}
        onDecided={goNext}
      />
    ) : null;

  const s = stats.data;
  const auto = (s?.autoApproved ?? 0) + (s?.autoRejected ?? 0);

  return (
    <div className="space-y-4">
      <section className="flex flex-col gap-3 rounded-xl border bg-card px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <TonalIcon icon={Scale} tone="violet" />
          <div className="min-w-0">
            <h3 className="text-sm font-medium">封禁申诉</h3>
            <p className="truncate text-xs text-muted-foreground">
              {scoped ? scoped.name : "全部应用"}
              {s ? `，${s.open + s.suggested} 条等人工处理` : ""}
              {auto ? `，模型已自动通过 ${s?.autoApproved ?? 0} 条、自动驳回 ${s?.autoRejected ?? 0} 条` : ""}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative min-w-0 flex-1 sm:w-64 sm:flex-none">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => {
                setKeyword(e.target.value);
                setPage(1);
              }}
              placeholder="搜登录名、用户 ID 或申诉内容"
              className="h-8 pl-8 text-sm"
            />
          </div>
          {appKey ? (
            <Button size="sm" variant="outline" onClick={() => setSettingsOpen(true)}>
              <Settings2 className="size-3.5" />
              申诉设置
            </Button>
          ) : null}
        </div>
      </section>

      <div className="flex flex-wrap gap-1.5" role="tablist" aria-label="申诉状态">
        {QUEUES.map((item) => {
          const count = s ? s[item.value] : undefined;
          const active = queue === item.value;
          return (
            <button
              key={item.value}
              type="button"
              role="tab"
              aria-selected={active}
              title={item.hint}
              onClick={() => pick(item.value)}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs transition-colors",
                active ? "border-primary/50 bg-primary/[0.06] font-medium text-foreground" : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
              )}
            >
              {item.label}
              {count !== undefined ? (
                <span
                  className={cn(
                    "min-w-5 rounded-full px-1.5 text-center text-[11px] leading-4 tabular-nums",
                    (item.value === "open" || item.value === "suggested") && count > 0 ? "bg-amber-500/15 text-amber-700 dark:text-amber-300" : "bg-muted text-muted-foreground",
                  )}
                >
                  {count}
                </span>
              ) : null}
            </button>
          );
        })}
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
        <section className="min-w-0 overflow-hidden rounded-xl border bg-card">
          {list.isLoading || waiting ? (
            <QueueSkeleton />
          ) : list.isError ? (
            <ErrorState error={list.error} onRetry={() => void list.refetch()} />
          ) : !items.length ? (
            <CardEmpty
              icon={Inbox}
              title={q ? "没有匹配的申诉" : queue === "open" || queue === "suggested" ? "没有等待处理的申诉" : "这里还没有申诉"}
              description={q ? "换个关键词试试" : QUEUES.find((x) => x.value === queue)?.hint}
            />
          ) : (
            <>
              <ul className="divide-y">
                {items.map((it) => (
                  <QueueRow
                    key={`${it.appid}-${it.id}`}
                    item={it}
                    appName={app ? undefined : nameOf(it.appid, it.appName)}
                    active={selected?.id === it.id && selected.appid === it.appid}
                    onOpen={() => setSelected({ appid: it.appid, id: it.id })}
                  />
                ))}
              </ul>
              {total > PAGE_SIZE ? (
                <ListPagination page={page} totalPages={Math.max(1, Math.ceil(total / PAGE_SIZE))} total={total} unit="条申诉" onPage={setPage} />
              ) : null}
            </>
          )}
        </section>

        {wide ? (
          <aside className="min-w-0 self-start rounded-xl border bg-card p-5 lg:sticky lg:top-4 lg:max-h-[calc(100vh-2rem)] lg:overflow-y-auto">
            {review ?? (
              selected && !currentKey ? (
                <CardEmpty icon={MessageSquareWarning} title="找不到这条申诉所属的应用" description="应用可能已被删除，或当前账号无权访问。" />
              ) : (
                <CardEmpty icon={MessageSquareWarning} title="选择一条申诉开始复审" description="左侧点开任意一条，可以查看封禁、风控证据与模型建议，并通过或驳回。" />
              )
            )}
          </aside>
        ) : null}
      </div>

      {!wide ? (
        <Dialog open={!!selected} onOpenChange={(o) => !o && setSelected(null)}>
          <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-2xl">
            <DialogHeader className="sr-only">
              <DialogTitle>复审申诉</DialogTitle>
              <DialogDescription>查看封禁、风控证据与模型建议，并通过或驳回</DialogDescription>
            </DialogHeader>
            {review ?? <CardEmpty icon={MessageSquareWarning} title="找不到这条申诉所属的应用" />}
          </DialogContent>
        </Dialog>
      ) : null}

      {appKey ? <AppealSettingsDialog appKey={appKey} appName={scoped?.name} open={settingsOpen} onOpenChange={setSettingsOpen} /> : null}
    </div>
  );
}

/** 一句话：「张三 3 小时前申诉了全站永久封禁，原因类别为欺诈或作弊」 */
function QueueRow({ item, appName, active, onOpen }: { item: BanAppeal; appName?: string; active: boolean; onOpen: () => void }) {
  const name = userName(item.user, item.userId);
  const code = reasonCodeLabel(item.ban?.reasonCode);
  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        aria-current={active || undefined}
        className={cn(
          "flex w-full min-w-0 flex-col gap-1.5 px-4 py-3 text-left transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none",
          active && "bg-primary/[0.05] shadow-[inset_3px_0_0_var(--color-primary)]",
        )}
      >
        <div className="flex min-w-0 items-start gap-2">
          <p className="min-w-0 flex-1 text-sm leading-5">
            <span className="font-medium">{name}</span>
            <span className="text-muted-foreground" title={formatDateTime(item.submittedAt)}>
              {" "}
              {formatRelative(item.submittedAt)}
            </span>
            申诉了{banTitle(item.ban)}
            {code ? <span className="text-muted-foreground">，原因类别为{code}</span> : null}
          </p>
          <AppealStateBadge appeal={item} />
        </div>
        <p className="line-clamp-2 text-xs leading-5 text-foreground/75">{item.content}</p>
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
          <VerdictChip model={item.model} />
          {appName ? <span>{appName}</span> : null}
          <span className="tabular-nums">申诉 #{item.id}</span>
          {item.decidedAt ? <span>{formatRelative(item.decidedAt)}结案</span> : null}
        </div>
      </button>
    </li>
  );
}

function QueueSkeleton() {
  return (
    <div className="divide-y">
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="space-y-2 px-4 py-3">
          <div className="flex items-center justify-between gap-3">
            <Skeleton className="h-4 w-64" />
            <Skeleton className="h-4 w-14 rounded-full" />
          </div>
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-24" />
        </div>
      ))}
    </div>
  );
}
