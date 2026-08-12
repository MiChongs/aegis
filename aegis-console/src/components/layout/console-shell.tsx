"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { ChevronsLeft, ChevronsRight } from "lucide-react";
import { AnnouncementBanner } from "@/components/layout/announcement-banner";
import { BrandLogo, BrandTitle } from "@/components/layout/brand";
import { CommandPalette } from "@/components/layout/sidebar/command-palette";
import { RecentTracker } from "@/components/layout/sidebar/recent-tracker";
import { SearchTrigger } from "@/components/layout/sidebar/search-trigger";
import { SidebarNavTree } from "@/components/layout/sidebar/sidebar-nav";
import { SidebarRailTree } from "@/components/layout/sidebar/sidebar-rail";
import { SidebarResizer } from "@/components/layout/sidebar/sidebar-resizer";
import { ConsoleTopbar } from "@/components/layout/topbar/console-topbar";
import { withActiveTab } from "@/components/layout/with-active-tab";
import { logoutAdmin } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth-store";
import { useOperatorIdentity } from "@/lib/operator";
import { preloadPinyin } from "@/lib/pinyin-search";
import { SIDEBAR_RAIL_WIDTH, useSidebarStore } from "@/lib/sidebar-store";
import { cn } from "@/lib/utils";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

const SidebarNav = withActiveTab(SidebarNavTree);
const SidebarRail = withActiveTab(SidebarRailTree);

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
  const operator = useOperatorIdentity();

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

      <div className="min-h-screen bg-transparent">
        <div className="mx-auto flex min-h-screen max-w-[1800px] gap-0 lg:gap-0">
          {/* ── 桌面侧边栏 ── */}
          <aside
            suppressHydrationWarning
            style={{ width: collapsed ? SIDEBAR_RAIL_WIDTH : width }}
            className={cn(
              "sticky top-0 hidden h-screen shrink-0 border-r border-sidebar-border bg-sidebar lg:flex lg:flex-col",
              // 拖拽期间关掉补间，否则每帧都在追上一帧，手感发黏
              resizing ? "transition-none" : "transition-[width] duration-200 ease-out"
            )}
          >
            {/* 品牌 + 搜索入口 */}
            <div
              className={cn(
                "flex shrink-0 flex-col gap-2 border-b border-sidebar-border py-3",
                collapsed ? "items-center px-2" : "px-3"
              )}
            >
              <div className={cn("flex w-full items-center", collapsed ? "justify-center" : "gap-2.5")}>
                <BrandLogo size="md" />
                {!collapsed && <BrandTitle />}
              </div>
              <SearchTrigger collapsed={collapsed} onOpen={openPalette} />
            </div>

            {/* 导航：min-h-0 才能真正压住高度，否则子项展开后会把底部用户区顶出屏幕 */}
            <ScrollArea className="min-h-0 flex-1">
              <div className={cn("py-2", collapsed ? "px-1.5" : "px-2")}>
                {collapsed ? <SidebarRail pathname={pathname} /> : <SidebarNav pathname={pathname} />}
              </div>
            </ScrollArea>

            {/* 底部：用户 + 折叠按钮 */}
            <div className="shrink-0 border-t border-sidebar-border">
              {collapsed ? (
                <div className="flex flex-col items-center gap-1 py-2">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Link href="/profile" className="rounded-lg p-1.5 transition-colors hover:bg-sidebar-accent">
                        <Avatar className="size-7" preview={false}>
                          <AvatarImage src={operator.avatarSrc} alt={operator.name} />
                          <AvatarFallback className="text-[9px]">{operator.initials}</AvatarFallback>
                        </Avatar>
                      </Link>
                    </TooltipTrigger>
                    <TooltipContent side="right" sideOffset={8}>
                      <p className="font-medium">{operator.name}</p>
                      <p className="text-xs text-background/70">{operator.role}</p>
                    </TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button type="button" title="展开侧边栏" onClick={toggleSidebar} className="flex size-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground">
                        <ChevronsRight className="size-3.5" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="right" sideOffset={8}>展开</TooltipContent>
                  </Tooltip>
                </div>
              ) : (
                <div className="flex items-center justify-between px-3 py-2">
                  <Link href="/profile" className="-mx-1 flex min-w-0 items-center gap-2 rounded-lg px-1 py-1 transition-colors hover:bg-sidebar-accent">
                    <Avatar className="size-7" preview={false}>
                      <AvatarImage src={operator.avatarSrc} alt={operator.name} />
                      <AvatarFallback className="text-[9px]">{operator.initials}</AvatarFallback>
                    </Avatar>
                    <div className="min-w-0">
                      <p className="truncate text-xs font-medium leading-tight">{operator.name}</p>
                      <p className="truncate text-[10px] leading-tight text-muted-foreground">{operator.role}</p>
                    </div>
                  </Link>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button type="button" title="收起侧边栏" onClick={toggleSidebar} className="flex size-7 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground">
                        <ChevronsLeft className="size-3.5" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="top" sideOffset={8}>
                      <p>收起侧边栏</p>
                      <p className="text-[10px] text-background/70">Ctrl / ⌘ B</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              )}
            </div>

            {/* 展开态才有宽度可调；折叠态是固定轨道 */}
            {collapsed ? null : <SidebarResizer onResizingChange={setResizing} />}
          </aside>

          {/* ── 主内容 ── */}
          <div className="flex min-w-0 flex-1 flex-col">
            <ConsoleTopbar pathname={pathname} onOpenPalette={openPalette} onLogout={handleLogout} />

            <div className="px-4 pt-3 empty:hidden lg:px-6 lg:pt-4">
              <AnnouncementBanner />
            </div>
            {/* 路由过渡由 src/app/(console)/template.tsx 接管（Next.js App Router 官方方案），
                它正好位于这里的 {children} 位置，天然只作用于主内容区，不动 Shell */}
            <main className="min-w-0 flex-1 px-4 py-4 lg:px-6 lg:py-5">{children}</main>
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}
