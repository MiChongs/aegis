"use client";

import { useCallback, useSyncExternalStore } from "react";
import {
  Check,
  Maximize2,
  Minimize2,
  Monitor,
  Moon,
  MoreHorizontal,
  Search,
  Star,
  Sun
} from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import screenfull from "screenfull";

import { Kbd, useMetaKeyLabel } from "@/components/layout/sidebar/sidebar-shared";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { currentTargetKey } from "@/lib/navigation";
import { useSidebarStore } from "@/lib/sidebar-store";
import { useThemeTransition, type ThemeChoice } from "@/lib/theme-transition";
import { useIsClient } from "@/lib/use-client-value";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  统一的顶栏按钮                                                       */
/* ------------------------------------------------------------------ */

/**
 * 顶栏图标按钮。
 *
 * 图标按钮天然缺少标签，所以这里把 Tooltip 焊死在组件里 ——
 * 由调用方自行决定"要不要加提示"的结果一定是有的加了、有的没加。
 */
export function TopbarButton({
  label,
  hint,
  onClick,
  active,
  className,
  children,
  ...rest
}: React.ComponentProps<"button"> & {
  label: string;
  /** 提示里的第二行，通常是快捷键 */
  hint?: string;
  active?: boolean;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={label}
          onClick={onClick}
          data-active={active ? "true" : undefined}
          className={cn(
            "rounded-lg text-muted-foreground hover:text-foreground",
            "data-[active=true]:bg-accent data-[active=true]:text-foreground",
            "data-[state=open]:bg-accent data-[state=open]:text-foreground",
            className
          )}
          {...rest}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom" sideOffset={6}>
        <p>{label}</p>
        {hint ? <p className="text-[10px] text-background/70">{hint}</p> : null}
      </TooltipContent>
    </Tooltip>
  );
}

/* ------------------------------------------------------------------ */
/*  搜索                                                                */
/* ------------------------------------------------------------------ */

/**
 * 顶栏搜索入口（仅在侧边栏收起 / 隐藏的宽度下出现，避免与侧边栏那条重复）。
 *
 * 有横向余量时长成一条带快捷键的搜索条 —— 图标按钮说不出"能搜什么"，
 * 也藏起了 `⌘K` 这件全站最该被知道的事。
 */
export function TopbarSearch({ onOpen, className }: { onOpen: () => void; className?: string }) {
  const metaKey = useMetaKeyLabel();

  return (
    <>
      <button
        type="button"
        onClick={onOpen}
        className={cn(
          "hidden h-8 min-w-40 items-center gap-2 rounded-lg border border-border bg-muted/40 px-2.5 text-xs text-muted-foreground",
          "transition-colors hover:border-ring/40 hover:bg-background hover:text-foreground @xl/topbar:flex",
          className
        )}
      >
        <Search className="size-3.5 shrink-0" />
        <span className="flex-1 truncate text-left">搜索或跳转…</span>
        <Kbd className="shrink-0">{metaKey} K</Kbd>
      </button>
      <TopbarButton
        label="搜索与跳转"
        hint={`${metaKey} K`}
        onClick={onOpen}
        className={cn("@xl/topbar:hidden", className)}
      >
        <Search className="size-3.5" />
      </TopbarButton>
    </>
  );
}

/* ------------------------------------------------------------------ */
/*  收藏当前页                                                          */
/* ------------------------------------------------------------------ */

/** 当前路径对应的收藏 key 与开关状态；不在导航目录里的页面返回 `key: null` */
export function useCurrentPin(pathname: string, activeTab: string | null) {
  const key = currentTargetKey(pathname, activeTab);
  const pinned = useSidebarStore((s) => (key ? s.pinned.includes(key) : false));
  const togglePin = useSidebarStore((s) => s.togglePin);
  const toggle = useCallback(() => {
    if (key) togglePin(key);
  }, [key, togglePin]);
  return { key, pinned, toggle };
}

/**
 * 收藏当前页。
 *
 * 与侧边栏每一行上的星标同一个 store —— 差别只是"人在页面里的时候也能收藏"，
 * 而这恰恰是想收藏的那一刻。
 */
export function PinCurrentPage({
  pathname,
  activeTab,
  className
}: {
  pathname: string;
  activeTab: string | null;
  className?: string;
}) {
  const { key, pinned, toggle } = useCurrentPin(pathname, activeTab);
  const reduced = useReducedMotion();

  // 目录之外的页面（个人资料等）没有可存的 key，收藏了也还原不出来
  if (!key) return null;

  return (
    <TopbarButton
      label={pinned ? "取消收藏本页" : "收藏本页"}
      hint="收藏会置顶在侧边栏"
      aria-pressed={pinned}
      active={pinned}
      onClick={toggle}
      className={className}
    >
      <motion.span
        key={String(pinned)}
        initial={reduced ? false : { scale: 0.6, rotate: -25 }}
        animate={{ scale: 1, rotate: 0 }}
        transition={{ type: "spring", stiffness: 520, damping: 22 }}
        className="flex items-center justify-center"
      >
        <Star className={cn("size-3.5", pinned && "fill-amber-400 text-amber-500")} />
      </motion.span>
    </TopbarButton>
  );
}

/* ------------------------------------------------------------------ */
/*  主题                                                                */
/* ------------------------------------------------------------------ */

export const THEME_OPTIONS = [
  { value: "system", label: "跟随系统", icon: Monitor },
  { value: "light", label: "浅色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon }
] as const satisfies ReadonlyArray<{ value: ThemeChoice; label: string; icon: typeof Monitor }>;

/**
 * 当前主题档位。
 *
 * 首帧统一按「跟随系统」渲染（`useIsClient` 的服务端快照），挂载后再换成真实值 ——
 * next-themes 在挂载前没有结论，直接读会水合不一致。
 */
export function useThemeChoice(): ThemeChoice {
  const { theme } = useThemeTransition();
  const isClient = useIsClient();
  if (!isClient) return "system";
  return (theme as ThemeChoice) || "system";
}

/**
 * 当前档位的图标。
 *
 * 刻意不写成 `const Icon = lookup(choice)` 再 `<Icon />` —— 那是在渲染期"创建组件"，
 * React Compiler 的 `react-hooks/static-components` 会拒绝，且每次渲染都换一个组件类型。
 */
export function ThemeGlyph({ choice, className }: { choice: ThemeChoice; className?: string }) {
  if (choice === "light") return <Sun className={className} />;
  if (choice === "dark") return <Moon className={className} />;
  return <Monitor className={className} />;
}

function themeLabel(choice: ThemeChoice) {
  return THEME_OPTIONS.find((o) => o.value === choice)?.label ?? THEME_OPTIONS[0].label;
}

/** 三个档位，菜单与子菜单共用（`setTheme` 带上事件：扩散动画要知道从哪个点开始） */
export function ThemeMenuItems() {
  const { setTheme } = useThemeTransition();
  const active = useThemeChoice();

  return (
    <>
      {THEME_OPTIONS.map((option) => (
        <DropdownMenuItem
          key={option.value}
          className="gap-2.5 text-xs"
          onClick={(event) => setTheme(option.value, event)}
        >
          <option.icon className="size-3.5" />
          {option.label}
          {active === option.value ? <Check className="ml-auto size-3" /> : null}
        </DropdownMenuItem>
      ))}
    </>
  );
}

export function ThemeMenu({ className }: { className?: string }) {
  const active = useThemeChoice();

  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="外观主题"
              className={cn(
                "rounded-lg text-muted-foreground hover:text-foreground",
                "data-[state=open]:bg-accent data-[state=open]:text-foreground",
                className
              )}
            >
              <ThemeGlyph choice={active} className="size-3.5" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom" sideOffset={6}>
          <p>外观主题</p>
          <p className="text-[10px] text-background/70">{themeLabel(active)}</p>
        </TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" sideOffset={8} className="w-36">
        <ThemeMenuItems />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/* ------------------------------------------------------------------ */
/*  全屏                                                                */
/* ------------------------------------------------------------------ */

function subscribeFullscreen(onChange: () => void) {
  if (!screenfull.isEnabled) return () => {};
  screenfull.on("change", onChange);
  return () => screenfull.off("change", onChange);
}

/**
 * 全屏开关。
 *
 * 前缀差异（老 WebKit / Firefox）交给 `screenfull`，状态用 `useSyncExternalStore`
 * 直接订阅 `fullscreenchange` —— 用 `useEffect` + `setState` 同步的话，
 * 用户按 F11 或 Esc 退出时按钮图标不会跟着变。
 */
export function useFullscreen() {
  const isClient = useIsClient();
  const active = useSyncExternalStore(
    subscribeFullscreen,
    () => screenfull.isEnabled && screenfull.isFullscreen,
    () => false
  );
  const toggle = useCallback(() => {
    if (screenfull.isEnabled) void screenfull.toggle().catch(() => {});
  }, []);

  return { supported: isClient && screenfull.isEnabled, active, toggle };
}

export function FullscreenToggle({ className }: { className?: string }) {
  const { supported, active, toggle } = useFullscreen();
  if (!supported) return null;

  return (
    <TopbarButton
      label={active ? "退出全屏" : "全屏"}
      hint="F11"
      active={active}
      onClick={toggle}
      className={className}
    >
      {active ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
    </TopbarButton>
  );
}

/* ------------------------------------------------------------------ */
/*  溢出菜单                                                            */
/* ------------------------------------------------------------------ */

/**
 * 窄顶栏下把次级动作收进来。
 *
 * 收起来的那几项在这里**一项不少**：顶栏放不下不等于这些能力在小屏上不存在，
 * 而"某个功能只在大屏有"是使用者最难自己发现的一类差异。
 */
export function TopbarOverflowMenu({
  pathname,
  activeTab,
  className
}: {
  pathname: string;
  activeTab: string | null;
  className?: string;
}) {
  const { key: pinKey, pinned, toggle: togglePin } = useCurrentPin(pathname, activeTab);
  const { supported: fullscreenSupported, active: fullscreen, toggle: toggleFullscreen } = useFullscreen();
  const themeChoice = useThemeChoice();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="更多操作"
          className={cn(
            "rounded-lg text-muted-foreground hover:text-foreground",
            "data-[state=open]:bg-accent data-[state=open]:text-foreground",
            className
          )}
        >
          <MoreHorizontal className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="w-48">
        {pinKey ? (
          <>
            <DropdownMenuItem className="gap-2.5 text-xs" onClick={togglePin}>
              <Star className={cn("size-3.5", pinned && "fill-amber-400 text-amber-500")} />
              {pinned ? "取消收藏本页" : "收藏本页"}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        ) : null}

        <DropdownMenuSub>
          <DropdownMenuSubTrigger className="gap-2.5 text-xs">
            <ThemeGlyph choice={themeChoice} className="size-3.5" />
            外观主题
            <span className="ml-auto text-[10px] text-muted-foreground">{themeLabel(themeChoice)}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-36">
            <ThemeMenuItems />
          </DropdownMenuSubContent>
        </DropdownMenuSub>

        {fullscreenSupported ? (
          <DropdownMenuItem className="gap-2.5 text-xs" onClick={toggleFullscreen}>
            {fullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
            {fullscreen ? "退出全屏" : "全屏"}
          </DropdownMenuItem>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
