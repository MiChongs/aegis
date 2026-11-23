"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useHotkeys } from "react-hotkeys-hook";
import { AnimatePresence, motion } from "motion/react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ResizableGroup, ResizableHandle, ResizablePanel, usePanelLayout } from "@/components/ui/resizable";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { useTicketStatsQuery, useTicketsInfiniteQuery } from "@/lib/ticket-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import { DeskAppRail, useDeskApps } from "./desk-app-rail";
import type { DeskComposerHandle } from "./desk-composer";
import { DeskContextPanel, type ContextTab } from "./desk-context-panel";
import { useDesk } from "./desk-context";
import { DeskInbox, deskViewCounts } from "./desk-inbox";
import { DeskOverview } from "./desk-overview";
import { deskListParams, type DeskState, type SetDeskState } from "./desk-state";
import { DeskThread } from "./desk-thread";

// 服务台：应用栏 | 收件箱 | 会话 | 资料栏。工单中心与意见反馈共用，差别只在 DeskProvider 给的 kind。
//
// 应用栏只在宽屏（≥ 1280）常驻，可收成窄条；更窄时收件箱顶部给应用切换器。
// 列表栏可拖宽；资料栏并排还是浮层，靠量详情区宽度决定（WIDE_DETAIL），不看视口断点：
// 列表栏可拖、应用栏与侧边栏可折叠，同一个视口宽度下详情区可以差出几百像素。
// 窄屏（< 1024）退回「收件箱全宽 + 详情抽屉」。

const PANEL_KEY = "aegis.desk.panel";
const RAIL_KEY = "aegis.desk.rail";
// 详情区宽于这个值时资料栏并排显示，否则浮在会话上方
const WIDE_DETAIL = 860;
const PANEL_WIDTH = 320;

function useMedia(query: string) {
  return useSyncExternalStore(
    (callback) => {
      const media = window.matchMedia(query);
      media.addEventListener("change", callback);
      return () => media.removeEventListener("change", callback);
    },
    () => window.matchMedia(query).matches,
    () => true
  );
}

function subscribeStorage(callback: () => void) {
  window.addEventListener("storage", callback);
  return () => window.removeEventListener("storage", callback);
}

function useStoredFlag(key: string, fallback: boolean) {
  const value = useSyncExternalStore(
    subscribeStorage,
    () => {
      const raw = window.localStorage.getItem(key);
      return raw === null ? fallback : raw === "1";
    },
    () => fallback
  );
  const set = useCallback(
    (next: boolean) => {
      window.localStorage.setItem(key, next ? "1" : "0");
      window.dispatchEvent(new StorageEvent("storage", { key }));
    },
    [key]
  );
  return [value, set] as const;
}

function useElementWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, width] as const;
}

export function DeskWorkspace({ state, setState }: { state: DeskState; setState: SetDeskState }) {
  const desk = useDesk();
  const isDesktop = useMedia("(min-width: 1024px)");
  const isWide = useMedia("(min-width: 1280px)");
  const layout = usePanelLayout({ id: `aegis-desk-${desk.kind}`, panelIds: ["list", "detail"] });
  const [railCollapsed, setRailCollapsed] = useStoredFlag(RAIL_KEY, false);
  const showRail = isWide;

  const keyword = useDebouncedValue(state.q.trim(), 300);
  const params = useMemo(() => deskListParams(desk.kind, state, keyword), [desk.kind, state, keyword]);
  const listQuery = useTicketsInfiniteQuery(params);
  const items = useMemo(() => listQuery.data?.pages.flatMap((page) => page.items) ?? [], [listQuery.data]);
  const total = listQuery.data?.pages[0]?.total ?? 0;
  const statsQuery = useTicketStatsQuery(state.app ?? undefined, desk.kind);

  // 「已超时」的数字按应用汇总算：统计接口的首响超时与解决超时会把同一条算两次
  const { apps, totals, ownedTotals } = useDeskApps();
  const overdue =
    state.app !== null ? apps.find((app) => app.appid === state.app)?.overdue : state.own ? ownedTotals.overdue : totals.overdue;
  const counts = useMemo(() => {
    const base = deskViewCounts(statsQuery.data, overdue);
    // 只看我创建的应用时，统计接口给的是全部应用的数，只留按应用汇总算得出的那两项
    if (state.app === null && state.own) return { active: ownedTotals.active, open: ownedTotals.unaccepted, overdue };
    return base;
  }, [statsQuery.data, overdue, state.app, state.own, ownedTotals]);

  const selectedId = state.id;
  const select = useCallback((id: number | null) => void setState({ id }), [setState]);
  const selectApp = useCallback(
    (appid: number | null, own = false) => void setState({ app: appid, own: appid === null && own, category: null, id: null }),
    [setState]
  );

  const selectedIndex = selectedId ? items.findIndex((item) => item.id === selectedId) : -1;
  const step = useCallback(
    (direction: 1 | -1) => {
      if (!items.length) return;
      const next = selectedIndex < 0 ? 0 : selectedIndex + direction;
      if (next >= 0 && next < items.length) select(items[next].id);
    },
    [items, selectedIndex, select]
  );

  // ── 资料栏：宽屏并排并记住开关，窄了改成浮层 ──
  const [detailRef, detailWidth] = useElementWidth<HTMLDivElement>();
  const wide = detailWidth >= WIDE_DETAIL;
  const [pinned, setPinned] = useStoredFlag(PANEL_KEY, true);
  const [overlayOpen, setOverlayOpen] = useState(false);
  const [panelTab, setPanelTab] = useState<ContextTab>("user");
  const panelOpen = isDesktop && wide ? pinned : overlayOpen;

  const setPanel = useCallback(
    (open: boolean) => {
      if (isDesktop && wide) setPinned(open);
      else setOverlayOpen(open);
    },
    [isDesktop, wide, setPinned]
  );
  const showUser = useCallback(() => {
    setPanelTab("user");
    setPanel(true);
  }, [setPanel]);

  const searchRef = useRef<HTMLInputElement>(null);
  const composerRef = useRef<DeskComposerHandle>(null);
  const compose = useCallback((mode: "reply" | "internal") => composerRef.current?.focus(mode), []);

  // ── 快捷键：输入框与回复框里不触发（库默认行为） ──
  useHotkeys("j", () => step(1), [step]);
  useHotkeys("k", () => step(-1), [step]);
  useHotkeys("slash", () => searchRef.current?.focus(), { preventDefault: true });
  useHotkeys("r", () => compose("reply"), { enabled: Boolean(selectedId), preventDefault: true }, [compose]);
  useHotkeys("n", () => compose("internal"), { enabled: Boolean(selectedId), preventDefault: true }, [compose]);
  useHotkeys("i", () => setPanel(!panelOpen), { enabled: Boolean(selectedId) }, [setPanel, panelOpen]);
  useHotkeys(
    "escape",
    (event) => {
      // 弹层、菜单、确认框开着时 Esc 只关它们，不连带关掉详情
      const overlay = document.querySelector(
        '[data-radix-popper-content-wrapper], [role="alertdialog"], [role="dialog"][data-state="open"], .PhotoView-Portal'
      );
      if (event.defaultPrevented || overlay) return;
      if (overlayOpen) setOverlayOpen(false);
      else select(null);
    },
    { enabled: Boolean(selectedId) && isDesktop },
    [overlayOpen, select]
  );

  const inbox = (
    <DeskInbox
      state={state}
      setState={setState}
      counts={counts}
      query={listQuery}
      items={items}
      total={total}
      searchRef={searchRef}
      onOpen={select}
      showAppPicker={!showRail}
      onSelectApp={selectApp}
    />
  );

  const thread = selectedId ? (
    <motion.div
      key={selectedId}
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
      className="h-full"
    >
      <DeskThread
        ticketId={selectedId}
        onClose={() => select(null)}
        onStep={step}
        canStep={{ prev: selectedIndex > 0, next: selectedIndex >= 0 && selectedIndex < items.length - 1 }}
        composerRef={composerRef}
        panelOpen={panelOpen}
        onTogglePanel={() => setPanel(!panelOpen)}
        onShowUser={showUser}
        onCompose={compose}
      >
        {(ticket) => (
          <AnimatePresence initial={false}>
            {panelOpen ? (
              <motion.aside
                key="panel"
                initial={wide ? { width: 0, opacity: 0 } : { x: PANEL_WIDTH }}
                animate={wide ? { width: PANEL_WIDTH, opacity: 1 } : { x: 0 }}
                exit={wide ? { width: 0, opacity: 0 } : { x: PANEL_WIDTH }}
                transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
                className={
                  wide
                    ? "shrink-0 overflow-hidden border-l"
                    : "absolute inset-y-0 right-0 z-30 w-[min(20rem,100%)] border-l bg-background shadow-xl"
                }
              >
                {/* 内层定宽：外层做宽度动画时内容不跟着被挤压换行 */}
                <div className="relative h-full bg-background" style={wide ? { width: PANEL_WIDTH } : undefined}>
                  {!wide ? (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      onClick={() => setOverlayOpen(false)}
                      aria-label="收起资料"
                      className="absolute right-2 top-2.5 z-10"
                    >
                      <X />
                    </Button>
                  ) : null}
                  <DeskContextPanel ticket={ticket} tab={panelTab} onTabChange={setPanelTab} onOpenFeedback={select} />
                </div>
              </motion.aside>
            ) : null}
          </AnimatePresence>
        )}
      </DeskThread>
    </motion.div>
  ) : (
    <DeskOverview stats={statsQuery.data} appid={state.app} own={state.own} onSelectApp={selectApp} />
  );

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden rounded-xl border bg-card shadow-xs">
      {showRail ? (
        <motion.div
          initial={false}
          animate={{ width: railCollapsed ? 56 : 236 }}
          transition={{ duration: 0.22, ease: [0.22, 1, 0.36, 1] }}
          className={cn("shrink-0 overflow-hidden border-r bg-muted/20")}
        >
          <div className="h-full" style={{ width: railCollapsed ? 56 : 236 }}>
            <DeskAppRail
              value={state.app}
              own={state.own}
              onSelect={selectApp}
              collapsed={railCollapsed}
              onCollapsedChange={setRailCollapsed}
            />
          </div>
        </motion.div>
      ) : null}

      {isDesktop ? (
        <ResizableGroup orientation="horizontal" defaultLayout={layout.defaultLayout} onLayoutChanged={layout.onLayoutChanged}>
          <ResizablePanel id="list" defaultSize={400} minSize={330} maxSize="48%">
            {inbox}
          </ResizablePanel>
          <ResizableHandle />
          <ResizablePanel id="detail" minSize="40">
            <div ref={detailRef} className="relative h-full overflow-hidden bg-background">
              {thread}
            </div>
          </ResizablePanel>
        </ResizableGroup>
      ) : (
        <>
          <div className="min-w-0 flex-1">{inbox}</div>
          <Sheet open={Boolean(selectedId)} onOpenChange={(open) => !open && select(null)}>
            <SheetContent side="right" showCloseButton={false} className="w-full gap-0 p-0 sm:max-w-2xl">
              <SheetTitle className="sr-only">{desk.noun}详情</SheetTitle>
              <div className="relative h-full overflow-hidden bg-background">{selectedId ? thread : null}</div>
            </SheetContent>
          </Sheet>
        </>
      )}
    </div>
  );
}
