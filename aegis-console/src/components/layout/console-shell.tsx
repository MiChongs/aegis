"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronsLeft,
  ChevronsRight,
  Command as CommandIcon,
  LogOut,
  Menu,
  Monitor,
  Moon,
  Search,
  ShieldCheck,
  Sun,
  UserRound
} from "lucide-react";
import { AnnouncementBanner } from "@/components/layout/announcement-banner";
import { NotificationBell } from "@/components/layout/notification-bell";
import { CommandPalette } from "@/components/layout/sidebar/command-palette";
import { RecentTracker } from "@/components/layout/sidebar/recent-tracker";
import { SidebarNavTree, type NavTreeProps } from "@/components/layout/sidebar/sidebar-nav";
import { SidebarRailTree } from "@/components/layout/sidebar/sidebar-rail";
import { SidebarResizer } from "@/components/layout/sidebar/sidebar-resizer";
import { Kbd, useMetaKeyLabel } from "@/components/layout/sidebar/sidebar-shared";
import { logoutAdmin } from "@/lib/api-client";
import { useAdminProfileQuery } from "@/lib/admin-hooks";
import { avatarUrl, useAuthStore } from "@/lib/auth-store";
import { preloadPinyin } from "@/lib/pinyin-search";
import { SIDEBAR_RAIL_WIDTH, useSidebarStore } from "@/lib/sidebar-store";
import { useBranding } from "@/lib/branding-provider";
import { matchNavigation } from "@/lib/navigation";
import { useThemeTransition } from "@/lib/theme-transition";
import { cn } from "@/lib/utils";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

/* ------------------------------------------------------------------ */
/*  工具函数                                                            */
/* ------------------------------------------------------------------ */

function initials(displayName?: string, account?: string) {
  return (displayName || account || "AG").trim().slice(0, 2).toUpperCase();
}

function roleName(op: ReturnType<typeof useAuthStore.getState>["operator"]) {
  if (!op) return "管理员";
  if (op.isSuperAdmin) return "超级管理员";
  return op.role || op.assignments?.[0]?.roleKey || "管理员";
}

const themeOptions = [
  { value: "system", label: "跟随系统", icon: Monitor },
  { value: "light", label: "浅色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon },
] as const;

/* ------------------------------------------------------------------ */
/*  `?tab=` 读取：必须处在 Suspense 边界内                                */
/* ------------------------------------------------------------------ */

/**
 * `useSearchParams()` 必须处在 Suspense 边界内（否则整页退化为客户端渲染并在 build 期报错）。
 * fallback 直接按「无 tab」渲染同一棵树，因此不会有空白闪烁，只是子项高亮晚一帧。
 */
function withActiveTab<P extends { activeTab: string | null }>(Tree: React.ComponentType<P>) {
  type OuterProps = Omit<P, "activeTab">;
  function WithTab(props: OuterProps) {
    const activeTab = useSearchParams().get("tab");
    return <Tree {...(props as P)} activeTab={activeTab} />;
  }
  function Wrapped(props: OuterProps) {
    return (
      <Suspense fallback={<Tree {...(props as P)} activeTab={null} />}>
        <WithTab {...props} />
      </Suspense>
    );
  }
  Wrapped.displayName = `WithActiveTab(${Tree.displayName || Tree.name})`;
  return Wrapped;
}

/* ── 面包屑：控制台 › 分组 › 页面 › 页内子项 ── */

function ConsoleBreadcrumbTree({ pathname, activeTab }: NavTreeProps) {
  const match = matchNavigation(pathname, activeTab);
  const leaf = match ? null : pathname === "/profile" ? "个人信息" : null;

  return (
    <Breadcrumb className="hidden lg:block">
      <BreadcrumbList>
        <BreadcrumbItem>
          <span className="text-xs text-muted-foreground">控制台</span>
        </BreadcrumbItem>
        {match ? (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <span className="text-xs text-muted-foreground">{match.group.title}</span>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              {match.child ? (
                <BreadcrumbLink asChild className="text-sm">
                  <Link href={match.item.href}>{match.item.title}</Link>
                </BreadcrumbLink>
              ) : (
                <BreadcrumbPage className="text-sm font-medium">{match.item.title}</BreadcrumbPage>
              )}
            </BreadcrumbItem>
            {match.child ? (
              <>
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  <BreadcrumbPage className="text-sm font-medium">{match.child.title}</BreadcrumbPage>
                </BreadcrumbItem>
              </>
            ) : null}
          </>
        ) : null}
        {leaf ? (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage className="text-sm font-medium">{leaf}</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        ) : null}
      </BreadcrumbList>
    </Breadcrumb>
  );
}

const SidebarNav = withActiveTab(SidebarNavTree);
const SidebarRail = withActiveTab(SidebarRailTree);
const ConsoleBreadcrumb = withActiveTab(ConsoleBreadcrumbTree);

/* ── 搜索入口：展开态是一条输入框状的按钮，折叠态退化成图标 ── */

function SearchTrigger({ collapsed, onOpen }: { collapsed: boolean; onOpen: () => void }) {
  const metaKey = useMetaKeyLabel();

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={onOpen}
            className="flex size-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground"
          >
            <Search className="size-4" />
            <span className="sr-only">搜索</span>
          </button>
        </TooltipTrigger>
        <TooltipContent side="right" sideOffset={8}>
          <p>搜索与跳转</p>
          <p className="text-[10px] text-background/70">{metaKey} K</p>
        </TooltipContent>
      </Tooltip>
    );
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "flex w-full items-center gap-2 rounded-lg border border-sidebar-border bg-background/60 px-2.5 py-1.5 text-xs text-muted-foreground",
        "transition-colors hover:border-ring/40 hover:bg-background hover:text-foreground"
      )}
    >
      <Search className="size-3.5 shrink-0" />
      <span className="min-w-0 flex-1 truncate text-left">搜索或跳转…</span>
      <Kbd className="shrink-0">{metaKey} K</Kbd>
    </button>
  );
}

/* ------------------------------------------------------------------ */
/*  Shell                                                              */
/* ------------------------------------------------------------------ */

export function ConsoleShell({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [resizing, setResizing] = useState(false);
  const [themeReady] = useState(typeof window !== "undefined");
  const token = useAuthStore((s) => s.accessToken);
  const operator = useAuthStore((s) => s.operator);
  const clearSession = useAuthStore((s) => s.clearSession);
  const collapsed = useSidebarStore((s) => s.collapsed);
  const width = useSidebarStore((s) => s.width);
  const toggleSidebar = useSidebarStore((s) => s.toggle);
  const { theme, setTheme } = useThemeTransition();
  const profileQuery = useAdminProfileQuery();

  const operatorName = operator?.displayName || operator?.account || "管理员";
  const operatorRole = useMemo(() => roleName(operator), [operator]);
  const pageTitle = useMemo(() => {
    const match = matchNavigation(pathname);
    if (match) return match.item.title;
    if (pathname === "/profile") return "个人信息";
    return null;
  }, [pathname]);
  const operatorAvatar = avatarUrl(profileQuery.data?.account?.avatar || operator?.avatar, operator?.avatarVersion);
  const activeTheme = themeReady ? theme || "system" : "system";
  const ThemeIcon = (themeOptions.find((o) => o.value === activeTheme) || themeOptions[0]).icon;

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
                <BrandLogo size={8} />
                {!collapsed && (
                  <div className="min-w-0">
                    <BrandTitle />
                  </div>
                )}
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
                          <AvatarImage src={operatorAvatar} alt={operatorName} />
                          <AvatarFallback className="text-[9px]">{initials(operator?.displayName, operator?.account)}</AvatarFallback>
                        </Avatar>
                      </Link>
                    </TooltipTrigger>
                    <TooltipContent side="right" sideOffset={8}>
                      <p className="font-medium">{operatorName}</p>
                      <p className="text-xs text-background/70">{operatorRole}</p>
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
                      <AvatarImage src={operatorAvatar} alt={operatorName} />
                      <AvatarFallback className="text-[9px]">{initials(operator?.displayName, operator?.account)}</AvatarFallback>
                    </Avatar>
                    <div className="min-w-0">
                      <p className="truncate text-xs font-medium leading-tight">{operatorName}</p>
                      <p className="truncate text-[10px] leading-tight text-muted-foreground">{operatorRole}</p>
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
            {/* 顶栏 */}
            <header className="sticky top-0 z-30 flex h-12 shrink-0 items-center justify-between gap-3 border-b border-border bg-background/80 px-4 backdrop-blur-md">
              {/* 左：移动端菜单 + 面包屑 */}
              <div className="flex items-center gap-2">
                <div className="lg:hidden">
                  <Sheet open={open} onOpenChange={setOpen}>
                    <SheetTrigger asChild>
                      <Button variant="ghost" size="icon" className="size-8">
                        <Menu className="size-4" />
                        <span className="sr-only">导航</span>
                      </Button>
                    </SheetTrigger>
                    <SheetContent side="left" className="w-72 p-0">
                      <SheetHeader className="border-b border-border px-4 py-3">
                        <SheetTitle className="flex items-center gap-2 text-sm">
                          <BrandLogo size={4} /> <BrandNameText />
                        </SheetTitle>
                        <SheetDescription className="text-xs"><BrandSubtitleText /></SheetDescription>
                      </SheetHeader>
                      <div className="border-b border-border px-3 py-2">
                        <SearchTrigger
                          collapsed={false}
                          onOpen={() => {
                            setOpen(false);
                            openPalette();
                          }}
                        />
                      </div>
                      <ScrollArea className="min-h-0 flex-1 p-2">
                        <SidebarNav pathname={pathname} scope="mobile" onNavigate={() => setOpen(false)} />
                      </ScrollArea>
                      <div className="border-t border-border p-3">
                        <SheetClose asChild>
                          <Button className="w-full" variant="outline" size="sm" onClick={handleLogout}>
                            <LogOut className="size-3.5" /> 退出登录
                          </Button>
                        </SheetClose>
                      </div>
                    </SheetContent>
                  </Sheet>
                </div>

                <ConsoleBreadcrumb pathname={pathname} />

                <span className="text-sm font-semibold lg:hidden">{pageTitle || "控制台"}</span>
              </div>

              {/* 右：搜索 + 通知 + 主题 + 用户 */}
              <div className="flex items-center gap-1">
                <Button variant="ghost" size="icon" className="size-8 lg:hidden" onClick={openPalette}>
                  <Search className="size-3.5" />
                  <span className="sr-only">搜索</span>
                </Button>
                <NotificationBell />
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon" className="size-8">
                      <ThemeIcon className="size-3.5" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-32">
                    {themeOptions.map((o) => (
                      <DropdownMenuItem key={o.value} onClick={(event) => setTheme(o.value, event)}>
                        <o.icon className="size-3.5" />
                        <span className="text-xs">{o.label}</span>
                        {activeTheme === o.value && <Check className="ml-auto size-3" />}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>

                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button type="button" className="flex items-center gap-1.5 rounded-lg px-1.5 py-1 transition-colors hover:bg-accent">
                      <Avatar className="size-6" preview={false}>
                        <AvatarImage src={operatorAvatar} alt={operatorName} />
                        <AvatarFallback className="text-[8px]">{initials(operator?.displayName, operator?.account)}</AvatarFallback>
                      </Avatar>
                      <span className="hidden text-xs font-medium sm:inline">{operatorName}</span>
                      <ChevronDown className="size-3 text-muted-foreground" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuLabel className="py-1.5 font-normal">
                      <p className="text-xs font-medium">{operatorName}</p>
                      <p className="text-[10px] text-muted-foreground">{operatorRole}</p>
                    </DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onClick={openPalette}>
                      <CommandIcon className="size-3.5" />命令面板
                      <span className="ml-auto text-[10px] text-muted-foreground">K</span>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild>
                      <Link href="/profile"><UserRound className="size-3.5" />个人资料</Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild>
                      <Link href="/security"><ShieldCheck className="size-3.5" />账户安全</Link>
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onClick={handleLogout}>
                      <LogOut className="size-3.5" />退出登录
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </header>

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

/* ── 品牌组件（从 BrandingContext 读取） ── */

function BrandLogo({ size = 8 }: { size?: number }) {
  const { logoURL } = useBranding();
  if (logoURL) {
    return <img src={logoURL} alt="Logo" className={`size-${size} rounded-lg object-contain`} />;
  }
  return (
    <div className={`flex size-${size} items-center justify-center rounded-lg bg-primary text-primary-foreground`}>
      <CommandIcon className="size-3.5" />
    </div>
  );
}

function BrandTitle() {
  const { platformName, consoleName } = useBranding();
  return (
    <>
      <h1 className="truncate text-sm font-semibold leading-tight">{platformName || "Aegis"}</h1>
      <p className="text-[10px] leading-tight text-muted-foreground">{consoleName || "控制台"}</p>
    </>
  );
}

function BrandNameText() {
  const { platformName } = useBranding();
  return <>{platformName || "Aegis"}</>;
}

function BrandSubtitleText() {
  const { consoleName } = useBranding();
  return <>{consoleName || "控制台"}</>;
}
