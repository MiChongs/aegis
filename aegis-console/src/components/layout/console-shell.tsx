"use client";

import { usePathname, useRouter } from "next/navigation";
import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { AnnouncementBanner } from "@/components/layout/announcement-banner";
import { CommandPalette } from "@/components/layout/sidebar/command-palette";
import { NavPanel } from "@/components/layout/sidebar/nav-panel";
import { NavRail } from "@/components/layout/sidebar/nav-rail";
import { RecentTracker } from "@/components/layout/sidebar/recent-tracker";
import { SidebarResizer } from "@/components/layout/sidebar/sidebar-resizer";
import { ConsoleTopbar } from "@/components/layout/topbar/console-topbar";
import { withActiveTab } from "@/components/layout/with-active-tab";
import { logoutAdmin } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import { isItemActive } from "@/lib/navigation";
import { useVisibleGroups } from "@/lib/navigation-hooks";
import { useOperatorIdentity } from "@/lib/operator";
import { preloadPinyin } from "@/lib/pinyin-search";
import { SIDEBAR_RAIL_WIDTH, useSidebarStore } from "@/lib/sidebar-store";
import { cn } from "@/lib/utils";
import { TooltipProvider } from "@/components/ui/tooltip";

const Panel = withActiveTab(NavPanel);

/* ------------------------------------------------------------------ */
/*  Shell                                                              */
/* ------------------------------------------------------------------ */

export function ConsoleShell({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [resizing, setResizing] = useState(false);
  const token = useAuthStore((s) => s.accessToken);
  const clearSession = useAuthStore((s) => s.clearSession);
  const collapsed = useSidebarStore((s) => s.collapsed);
  const width = useSidebarStore((s) => s.width);
  const toggleSidebar = useSidebarStore((s) => s.toggle);
  const hasPins = useSidebarStore((s) => s.pinned.length > 0);
  const operator = useOperatorIdentity();
  const groups = useVisibleGroups();

  // 轨道上选中的分组：默认跟随当前路由；手动点了别的分组后，换页时再回到路由所在分组
  const routeGroupKey = useMemo(
    () => groups.find((group) => group.items.some((item) => isItemActive(pathname, item)))?.key ?? null,
    [groups, pathname]
  );
  const [picked, setPicked] = useState<{ key: string; path: string } | null>(null);
  const selectedKey =
    picked && picked.path === pathname ? picked.key : (routeGroupKey ?? groups[0]?.key ?? null);
  const selectedGroup = groups.find((group) => group.key === selectedKey) ?? null;

  const selectSection = useCallback(
    (key: string) => {
      setPicked({ key, path: pathname });
      if (collapsed) toggleSidebar();
    },
    [pathname, collapsed, toggleSidebar]
  );

  const openPalette = useCallback(() => {
    preloadPinyin();
    setPaletteOpen(true);
  }, []);

  const handleLogout = useCallback(async () => {
    if (token) { try { await logoutAdmin(token); } catch {} }
    clearSession();
    router.replace("/login");
  }, [token, clearSession, router]);

  /**
   * 全局快捷键。
   * `⌘K` 有意不排除输入框内触发 —— 它是"去哪儿"的入口，跟正在填什么表单无关；
   * `⌘B` 则会避开输入场景，免得抢走浏览器/输入法的加粗。
   */
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.altKey) return;
      const key = event.key.toLowerCase();
      if (key === "k") {
        event.preventDefault();
        preloadPinyin();
        setPaletteOpen((prev) => !prev);
        return;
      }
      if (key === "b") {
        const el = document.activeElement;
        const typing =
          el instanceof HTMLElement &&
          (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
        if (typing) return;
        event.preventDefault();
        toggleSidebar();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [toggleSidebar]);

  // 首屏稍后预热拼音词典：等真按下 ⌘K 再拉，第一次输入会赶不上
  useEffect(() => {
    const timer = window.setTimeout(preloadPinyin, 1500);
    return () => window.clearTimeout(timer);
  }, []);

  return (
    <TooltipProvider delayDuration={180}>
      {/* useSearchParams() 的 Suspense 边界收窄到这两个渲染 null 的组件上，不影响主内容 */}
      <Suspense fallback={null}>
        <RecentTracker />
      </Suspense>
      <Suspense fallback={null}>
        <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} onLogout={handleLogout} />
      </Suspense>

      <div className="flex min-h-screen bg-sidebar">
        {/* ── 图标轨道（桌面） ── */}
        <aside
          style={{ width: SIDEBAR_RAIL_WIDTH }}
          className="sticky top-0 hidden h-screen shrink-0 lg:block"
        >
          <NavRail
            groups={groups}
            selectedKey={collapsed ? null : selectedKey}
            routeGroupKey={routeGroupKey}
            hasPins={hasPins}
            operator={operator}
            onSelect={selectSection}
            onOpenPalette={openPalette}
            onLogout={handleLogout}
          />
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          <ConsoleTopbar pathname={pathname} onOpenPalette={openPalette} onLogout={handleLogout} />

          {/* ── 内容外框：次级面板 + 主内容，桌面端左上圆角描边，与轨道、顶栏分出层次 ── */}
          <div className="flex min-w-0 flex-1 bg-background lg:rounded-tl-2xl lg:border-t lg:border-l lg:border-sidebar-border dark:bg-card/35">
            <aside
              suppressHydrationWarning
              style={{ width: collapsed ? 0 : width }}
              className={cn(
                "sticky top-14 hidden h-[calc(100vh-3.5rem)] shrink-0 overflow-hidden lg:block",
                !collapsed && "border-r border-sidebar-border",
                // 拖拽期间关掉补间，否则每帧都在追上一帧，手感发黏
                resizing ? "transition-none" : "transition-[width] duration-200 ease-out"
              )}
            >
              <div className="relative h-full" style={{ width }}>
                <Panel
                  group={selectedGroup}
                  selectedKey={selectedKey}
                  pathname={pathname}
                  onOpenPalette={openPalette}
                />
                {collapsed ? null : <SidebarResizer onResizingChange={setResizing} />}
              </div>
            </aside>

            <div className="flex min-w-0 flex-1 flex-col">
              <div className="px-4 pt-3 empty:hidden lg:px-6 lg:pt-5">
                <AnnouncementBanner />
              </div>
              {/* 路由过渡由 src/app/(console)/template.tsx 接管（Next.js App Router 官方方案），
                  它正好位于这里的 {children} 位置，天然只作用于主内容区，不动 Shell */}
              <main className="min-w-0 flex-1 px-4 py-4 lg:px-6 lg:py-5">{children}</main>
            </div>
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}
