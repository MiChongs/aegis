"use client";

import { useState } from "react";
import { useMotionValueEvent, useScroll } from "motion/react";

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
        "@container/topbar sticky top-0 z-30 h-12 shrink-0",
        "border-b border-border/50 bg-background/70 backdrop-blur-xl backdrop-saturate-150",
        "transition-[background-color,border-color,box-shadow] duration-200 ease-out",
        "data-[scrolled=true]:border-border data-[scrolled=true]:bg-background/85",
        "data-[scrolled=true]:shadow-[0_6px_20px_-18px_rgb(0_0_0/0.6)]",
        // 不支持 backdrop-filter 时退回不透明底色，否则内容会从顶栏底下透出来
        "supports-[backdrop-filter]:bg-background/70 supports-[backdrop-filter]:data-[scrolled=true]:bg-background/80"
      )}
    >
      <div className="flex h-full items-center gap-1.5 px-3 lg:px-4">
        {/* ── 左：导航入口 + 面包屑 ── */}
        <MobileNav
          pathname={pathname}
          operator={operator}
          onOpenPalette={onOpenPalette}
          onLogout={onLogout}
        />
        <TopbarBreadcrumb pathname={pathname} className="flex-1" />

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

          <Separator orientation="vertical" className="mx-1 !h-5 bg-border/70" />

          <UserMenu operator={operator} onOpenPalette={onOpenPalette} onLogout={onLogout} />
        </div>
      </div>
    </header>
  );
}
