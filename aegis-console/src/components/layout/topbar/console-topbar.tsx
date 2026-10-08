"use client";

import { useState } from "react";
import { useMotionValueEvent, useScroll } from "motion/react";

import { SearchTrigger } from "@/components/layout/sidebar/search-trigger";
import { MobileNav } from "@/components/layout/topbar/mobile-nav";
import { NotificationBell } from "@/components/layout/topbar/notification-bell";
import {
  FullscreenToggle,
  PinCurrentPage,
  ThemeMenu,
  TopbarOverflowMenu,
  TopbarSearch
} from "@/components/layout/topbar/topbar-actions";
import { TopbarBreadcrumbTree } from "@/components/layout/topbar/topbar-breadcrumb";
import { UserMenu } from "@/components/layout/topbar/user-menu";
import { withActiveTab } from "@/components/layout/with-active-tab";
import { Separator } from "@/components/ui/separator";
import { useOperatorIdentity } from "@/lib/operator";
import { cn } from "@/lib/utils";

const TopbarBreadcrumb = withActiveTab(TopbarBreadcrumbTree);
const TopbarPin = withActiveTab(PinCurrentPage);
const TopbarOverflow = withActiveTab(TopbarOverflowMenu);

/**
 * 页面是否已经滚动过。
 *
 * 顶栏浮在内容之上，滚动之后必须有一条边把两者分开 —— 停在顶部时那条线纯属噪音。
 * 订阅走 motion 的 `useScroll`（rAF 节流 + 被动监听），
 * 布尔值只在跨过阈值时才变，因此重渲染最多两次。
 */
function useScrolled(threshold = 6) {
  const [scrolled, setScrolled] = useState(false);
  const { scrollY } = useScroll();
  useMotionValueEvent(scrollY, "change", (value) => setScrolled(value > threshold));
  return scrolled;
}

export type ConsoleTopbarProps = {
  pathname: string;
  onOpenPalette: () => void;
  onLogout: () => void;
};

/**
 * 控制台顶栏。
 *
 * ── 响应式为什么按容器而不是视口 ──
 * 顶栏的可用宽度 = 视口 − 侧边栏，而侧边栏可折叠（56px）、可拖宽（208–380px）。
 * 同一个 1280px 视口下顶栏能差出 300px，按视口断点排版必然在某个组合下挤成一团。
 * 所以这里用容器查询（`@container/topbar`）：**面包屑折叠、次级动作收进溢出菜单、
 * 账户名显隐都以顶栏自身宽度为准**。
 * 只有一件事仍按视口：移动端导航按钮与顶栏搜索 —— 它们要回答的是
 * "侧边栏在不在"，而那正是 `lg` 断点定义的。
 *
 * ── 右侧动作的取舍 ──
 * 常驻的只有通知与账户（一个是会打断你的、一个是身份），其余（收藏本页 / 主题 / 全屏）
 * 在窄顶栏下整体收进 `⋯`，一项不少。八个图标一字排开只会让每一个都不被看见。
 */
export function ConsoleTopbar({ pathname, onOpenPalette, onLogout }: ConsoleTopbarProps) {
  const scrolled = useScrolled();
  const operator = useOperatorIdentity();

  return (
    <header
      data-scrolled={scrolled ? "true" : "false"}
      className={cn(
        "@container/topbar sticky top-0 z-30 h-14 shrink-0 bg-sidebar",
        // 桌面端顶栏与图标轨道同属外框，靠下方内容区的圆角描边分隔，不再自带底边；
        // 移动端没有外框，滚动后仍需要一条边
        "max-lg:border-b max-lg:border-transparent max-lg:data-[scrolled=true]:border-border"
      )}
    >
      <div className="flex h-full items-center gap-1.5 px-3 lg:pr-4 lg:pl-2">
        {/* ── 左：导航入口 + 面包屑 ── */}
        <MobileNav
          pathname={pathname}
          operator={operator}
          onOpenPalette={onOpenPalette}
          onLogout={onLogout}
        />
        <TopbarBreadcrumb pathname={pathname} className="min-w-0 flex-1" />

        {/* 居中的搜索框：桌面端唯一的搜索入口（侧栏里不再重复放一个） */}
        <div className="hidden w-[min(22rem,32vw)] shrink-0 lg:block">
          <SearchTrigger collapsed={false} onOpen={onOpenPalette} className="h-9 rounded-xl bg-background/70" />
        </div>
        <div className="hidden flex-1 lg:block" aria-hidden />

        {/* ── 右：动作区 ── */}
        <div className="flex shrink-0 items-center gap-0.5">
          {/* 侧边栏藏起来了才需要，否则与侧边栏顶部那条搜索重复。
              `lg:hidden` 套在外层而不是直接给按钮：媒体查询与容器查询谁压过谁只取决于
              生成顺序，写在同一个元素上等于把显隐交给运气 */}
          <div className="flex items-center lg:hidden">
            <TopbarSearch onOpen={onOpenPalette} />
          </div>

          <TopbarPin pathname={pathname} className="hidden @2xl/topbar:inline-flex" />
          <ThemeMenu className="hidden @2xl/topbar:inline-flex" />
          <FullscreenToggle className="hidden @2xl/topbar:inline-flex" />
          <TopbarOverflow pathname={pathname} className="@2xl/topbar:hidden" />

          <NotificationBell />

          {/* 桌面端账户入口在图标轨道底部 */}
          <div className="flex items-center lg:hidden">
            <Separator orientation="vertical" className="mx-1 !h-5 bg-border/70" />
            <UserMenu operator={operator} onOpenPalette={onOpenPalette} onLogout={onLogout} />
          </div>
        </div>
      </div>
    </header>
  );
}
