"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { useHotkeys } from "react-hotkeys-hook";
import { AnimatePresence, motion } from "motion/react";
import { Keyboard, Settings2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ResizableGroup, ResizableHandle, ResizablePanel, usePanelLayout } from "@/components/ui/resizable";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useTicketStatsQuery, useTicketsInfiniteQuery } from "@/lib/ticket-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import type { FeedbackComposerHandle } from "./feedback-composer";
import { FeedbackContextPanel, type ContextTab } from "./feedback-context-panel";
import { FeedbackInbox } from "./feedback-inbox";
import { FeedbackOverview, FeedbackStatsLine, ShortcutList } from "./feedback-overview";
import { feedbackListParams, useFeedbackState } from "./feedback-state";
import { FeedbackThread } from "./feedback-thread";

// 反馈台：左边收件箱，中间会话，右边反馈人资料。
//
// 反馈就是 kind=feedback 的工单，回复、改状态、关闭全部复用工单接口；
// 界面是独立的一套（工单中心仍用 components/tickets/workspace）。
// 窄屏（< lg）退回「收件箱全宽 + 详情抽屉」。

const PANEL_KEY = "aegis.feedback.panel";
// 详情区宽于这个值时资料面板并排显示，否则浮在会话上方
const WIDE_DETAIL = 860;
const PANEL_WIDTH = 320;

function subscribeMedia(callback: () => void) {
  const media = window.matchMedia("(min-width: 1024px)");
  media.addEventListener("change", callback);
  return () => media.removeEventListener("change", callback);
}

function useIsDesktop() {
  return useSyncExternalStore(subscribeMedia, () => window.matchMedia("(min-width: 1024px)").matches, () => true);
}

function subscribeStorage(callback: () => void) {
  window.addEventListener("storage", callback);
  return () => window.removeEventListener("storage", callback);
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

export function FeedbackWorkspace() {
  const [state, setState] = useFeedbackState();
  const isDesktop = useIsDesktop();
  const layout = usePanelLayout({ id: "aegis-feedback-inbox", panelIds: ["list", "detail"] });

  const keyword = useDebouncedValue(state.q.trim(), 300);
  const params = useMemo(() => feedbackListParams(state, keyword), [state, keyword]);
  const listQuery = useTicketsInfiniteQuery(params);
  const items = useMemo(() => listQuery.data?.pages.flatMap((page) => page.items) ?? [], [listQuery.data]);
  const total = listQuery.data?.pages[0]?.total ?? 0;
  const statsQuery = useTicketStatsQuery(state.app ?? undefined, "feedback");

  const selectedId = state.id;
  const select = useCallback((id: number | null) => void setState({ id }), [setState]);

  const selectedIndex = selectedId ? items.findIndex((item) => item.id === selectedId) : -1;
  const step = useCallback(
    (direction: 1 | -1) => {
      if (!items.length) return;
      const next = selectedIndex < 0 ? 0 : selectedIndex + direction;
      if (next >= 0 && next < items.length) select(items[next].id);
    },
    [items, selectedIndex, select]
  );

  // ── 资料面板：宽屏并排并记住开关，窄了改成浮层 ──
  const [detailRef, detailWidth] = useElementWidth<HTMLDivElement>();
  const wide = detailWidth >= WIDE_DETAIL;
  const pinned = useSyncExternalStore(subscribeStorage, () => window.localStorage.getItem(PANEL_KEY) !== "0", () => true);
  const [overlayOpen, setOverlayOpen] = useState(false);
  const [panelTab, setPanelTab] = useState<ContextTab>("user");
  const panelOpen = isDesktop && wide ? pinned : overlayOpen;

  const setPanel = useCallback(
    (open: boolean) => {
      if (isDesktop && wide) {
        window.localStorage.setItem(PANEL_KEY, open ? "1" : "0");
        window.dispatchEvent(new StorageEvent("storage", { key: PANEL_KEY }));
      } else {
        setOverlayOpen(open);
      }
    },
    [isDesktop, wide]
  );
  const showUser = useCallback(() => {
    setPanelTab("user");
    setPanel(true);
  }, [setPanel]);

  const searchRef = useRef<HTMLInputElement>(null);
  const composerRef = useRef<FeedbackComposerHandle>(null);
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
        '[data-radix-popper-content-wrapper], [role="alertdialog"], [role="dialog"][data-state="open"]'
      );
      if (event.defaultPrevented || overlay) return;
      if (overlayOpen) setOverlayOpen(false);
      else select(null);
    },
    { enabled: Boolean(selectedId) && isDesktop },
    [overlayOpen, select]
  );

  const inbox = (
    <FeedbackInbox
      state={state}
      setState={setState}
      stats={statsQuery.data}
      query={listQuery}
      items={items}
      total={total}
      searchRef={searchRef}
      onOpen={select}
    />
  );

  const thread = selectedId ? (
    <motion.div
      key={selectedId}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: 0.15 }}
      className="h-full"
    >
      <FeedbackThread
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
                transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
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
                  <FeedbackContextPanel ticket={ticket} tab={panelTab} onTabChange={setPanelTab} onOpenFeedback={select} />
                </div>
              </motion.aside>
            ) : null}
          </AnimatePresence>
        )}
      </FeedbackThread>
    </motion.div>
  ) : (
    <FeedbackOverview stats={statsQuery.data} />
  );

  return (
    <div className="flex h-[calc(100dvh-5.5rem)] min-h-[560px] flex-col gap-3 lg:h-[calc(100dvh-6rem)]">
      <div className="flex items-center gap-4">
        <h1 className="shrink-0 text-xl font-semibold tracking-tight text-foreground">意见反馈</h1>
        <div className="hidden min-w-0 flex-1 md:block">
          <FeedbackStatsLine stats={statsQuery.data} />
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-1">
          <Popover>
            <Tooltip>
              <TooltipTrigger asChild>
                <PopoverTrigger asChild>
                  <Button size="icon-sm" variant="ghost" aria-label="快捷键">
                    <Keyboard />
                  </Button>
                </PopoverTrigger>
              </TooltipTrigger>
              <TooltipContent>快捷键</TooltipContent>
            </Tooltip>
            <PopoverContent align="end" className="w-80">
              <ShortcutList />
            </PopoverContent>
          </Popover>
          <Button size="sm" variant="ghost" asChild>
            <Link href="/tickets?tab=settings">
              <Settings2 />
              分类与快捷回复
            </Link>
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 overflow-hidden rounded-xl border bg-card">
        {isDesktop ? (
          <ResizableGroup orientation="horizontal" defaultLayout={layout.defaultLayout} onLayoutChanged={layout.onLayoutChanged}>
            <ResizablePanel id="list" defaultSize={380} minSize={320} maxSize="45%">
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
                <SheetTitle className="sr-only">反馈详情</SheetTitle>
                <div className="relative h-full overflow-hidden bg-background">{selectedId ? thread : null}</div>
              </SheetContent>
            </Sheet>
          </>
        )}
      </div>
    </div>
  );
}
