"use client";

import { useState } from "react";
import Link from "next/link";
import { Menu } from "lucide-react";
import { motion, useScroll, useTransform } from "motion/react";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { useAuthStore } from "@/lib/auth-store";
import { cn } from "@/lib/utils";

type PublicHeaderProps = {
  current?: "home" | "status" | "developers" | "login" | "console";
  navLabel?: string;
};

/**
 * 导航与操作分成两组，中间不再混为一排。
 *
 * 旧版把「首页 / 状态 / 开发者 / 登录 / 控制台」五个词并排摆着、字重字色完全一样，
 * 于是这个页面最想让人做的那件事（进控制台）和「状态页在哪」一样重 ——
 * 结果就是没有主操作。左边一组是**去哪看**，右边一组是**要做什么**。
 */
const NAV_ITEMS = [
  { key: "home", label: "首页", href: "/" },
  { key: "status", label: "状态", href: "/status" },
  { key: "developers", label: "开发者", href: "/developers" }
] as const;

export function PublicHeader({ current, navLabel = "公开导航" }: PublicHeaderProps) {
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);
  const [menuOpen, setMenuOpen] = useState(false);

  // 顶到最上面时栏是透明的，让首屏从整块画面开始；一往下滚就落下毛玻璃和分隔线，
  // 否则深色内容滚过去会和栏糊成一片。用 motion value 直接插值，不经过 state ——
  // 滚动每一帧都 setState 会把整棵导航树重渲一次。
  const { scrollY } = useScroll();
  const background = useTransform(scrollY, [0, 72], ["rgba(6, 10, 18, 0)", "rgba(6, 10, 18, 0.72)"]);
  const borderColor = useTransform(scrollY, [0, 72], ["rgba(255, 255, 255, 0)", "rgba(255, 255, 255, 0.08)"]);
  const blur = useTransform(scrollY, [0, 72], ["blur(0px)", "blur(14px)"]);

  return (
    <motion.header
      className="sticky top-0 z-50 w-full border-b"
      style={{ background, borderColor, backdropFilter: blur, WebkitBackdropFilter: blur }}
    >
      <div className="mx-auto flex h-16 w-full max-w-360 items-center gap-3 px-5 md:px-8 xl:px-12">
        <Link href="/" className="flex shrink-0 items-center gap-2.5" aria-label="AEGIS">
          <AegisMark className="size-6 text-white" />
          <span
            className="text-white"
            style={{
              fontFamily: "var(--font-data)",
              fontSize: "0.9rem",
              fontWeight: 700,
              letterSpacing: "0.26em"
            }}
          >
            AEGIS
          </span>
        </Link>

        {/* ── 去哪看 ── */}
        <nav className="ml-6 hidden items-center gap-1 md:flex" aria-label={navLabel}>
          {NAV_ITEMS.map((item) => (
            <Link
              key={item.key}
              href={item.href}
              aria-current={current === item.key ? "page" : undefined}
              className={cn(
                "rounded-full px-3 py-1.5 text-sm transition-colors",
                current === item.key
                  ? "bg-white/10 text-white"
                  : "text-white/60 hover:bg-white/5 hover:text-white"
              )}
            >
              {item.label}
            </Link>
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          {/* ── 要做什么 ── */}
          {authenticated ? null : (
            <Button
              asChild
              variant="ghost"
              size="sm"
              className="hidden text-white/70 hover:bg-white/5 hover:text-white sm:inline-flex"
            >
              <Link href="/login">登录</Link>
            </Button>
          )}
          <Button
            asChild
            size="sm"
            className="rounded-full bg-white px-4 text-[#0b0d12] hover:bg-white hover:brightness-95"
          >
            {/* 已登录时只写「控制台」：首屏主按钮上已经有一个「进入控制台」，
                两个一模一样的词并排出现，看起来像是渲染重复了。
                栏上这个是导航，首屏那个才是这一页的主操作。 */}
            <Link href={authenticated ? "/overview" : "/login"}>
              {authenticated ? "控制台" : "登录控制台"}
            </Link>
          </Button>

          {/* 小屏把导航收进抽屉：五个链接挤在 375px 宽度里会换行，
              把 logo 顶掉一行，滚动时那一栏还会跟着变高。 */}
          <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
            <SheetTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-white/70 hover:bg-white/5 hover:text-white md:hidden"
                aria-label="打开导航"
              >
                <Menu className="size-4" />
              </Button>
            </SheetTrigger>
            <SheetContent side="right" className="w-64 border-white/10 bg-[#0b0f17] text-white">
              <SheetHeader>
                <SheetTitle className="text-white">{navLabel}</SheetTitle>
              </SheetHeader>
              <nav className="flex flex-col gap-1 px-4">
                {NAV_ITEMS.map((item) => (
                  <Link
                    key={item.key}
                    href={item.href}
                    onClick={() => setMenuOpen(false)}
                    aria-current={current === item.key ? "page" : undefined}
                    className={cn(
                      "rounded-lg px-3 py-2.5 text-sm transition-colors",
                      current === item.key ? "bg-white/10 text-white" : "text-white/65 hover:bg-white/5 hover:text-white"
                    )}
                  >
                    {item.label}
                  </Link>
                ))}
                {authenticated ? null : (
                  <Link
                    href="/login"
                    onClick={() => setMenuOpen(false)}
                    className="rounded-lg px-3 py-2.5 text-sm text-white/65 transition-colors hover:bg-white/5 hover:text-white"
                  >
                    登录
                  </Link>
                )}
              </nav>
            </SheetContent>
          </Sheet>
        </div>
      </div>
    </motion.header>
  );
}
