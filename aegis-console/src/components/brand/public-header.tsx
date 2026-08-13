"use client";

import { useState } from "react";
import Link from "next/link";
import { ExternalLink, Menu, Moon, Sun } from "lucide-react";
import { useMotionValueEvent, useScroll } from "motion/react";
import { AegisMark } from "@/components/brand/aegis-mark";
import { Button } from "@/components/ui/button";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
  navigationMenuTriggerStyle,
} from "@/components/ui/navigation-menu";
import { Separator } from "@/components/ui/separator";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { useAuthStore } from "@/lib/auth-store";
import { appConfig } from "@/lib/env";
import { useThemeTransition } from "@/lib/theme-transition";
import { useIsClient } from "@/lib/use-client-value";
import { cn } from "@/lib/utils";

type PublicHeaderProps = {
  current?: "home" | "status" | "developers" | "login" | "console";
  navLabel?: string;
};

/**
 * 公开站点顶栏。
 *
 * 导航与操作分成两组，中间不再混为一排。旧版把「首页 / 状态 / 开发者 /
 * 登录 / 控制台」五个词并排摆着、字重字色完全一样，于是这个页面最想让人
 * 做的那件事（进控制台）和「状态页在哪」一样重 —— 结果就是没有主操作。
 * 左边一组回答**去哪看**，右边一组回答**要做什么**。
 *
 * 分区锚点一律写成绝对路径（`/#features`），因为这个顶栏也挂在状态页上，
 * 裸 `#features` 在那里会指向一个不存在的锚点。
 */

const PRODUCT_LINKS = [
  { href: "/#features", label: "核心能力", description: "平台层提供的六项基础能力" },
  { href: "/#capabilities", label: "能力全景", description: "按使用场景分组的完整清单" },
  { href: "/#architecture", label: "架构分层", description: "请求经过的五层结构" },
  { href: "/#integration", label: "接入方式", description: "单一命名空间与三档安全等级" },
] as const;

const DEVELOPER_LINKS = [
  { href: "/developers", label: "快速接入", description: "协议规格与多语言示例" },
  { href: "/developers/api", label: "接口文档", description: "支持在线调试的接口参考" },
] as const;

const MOBILE_LINKS = [
  { href: "/", label: "首页" },
  ...PRODUCT_LINKS,
  { href: "/developers", label: "快速接入" },
  { href: "/developers/api", label: "接口文档" },
  { href: "/status", label: "服务状态" },
] as const;

export function PublicHeader({ current, navLabel = "公开导航" }: PublicHeaderProps) {
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);
  const [menuOpen, setMenuOpen] = useState(false);

  // 顶到最上面时栏是透明的，让首屏从整块画面开始；往下滚一点才落下毛玻璃
  // 和分隔线。布尔值只在跨过阈值时翻转 —— 每一帧都 setState 会把整棵
  // 导航树重渲一次，与控制台顶栏同一条约束。
  const { scrollY } = useScroll();
  const [scrolled, setScrolled] = useState(false);
  useMotionValueEvent(scrollY, "change", (value) => {
    setScrolled(value > 8);
  });

  return (
    <header
      data-scrolled={scrolled}
      className={cn(
        "sticky top-0 z-50 w-full border-b transition-colors duration-200",
        scrolled
          ? "border-border bg-background/80 backdrop-blur-md supports-[backdrop-filter]:bg-background/70"
          : "border-transparent bg-transparent"
      )}
    >
      <div className="mx-auto flex h-16 w-full max-w-7xl items-center gap-3 px-5 md:px-8">
        <Link href="/" className="flex shrink-0 items-center gap-2.5" aria-label={appConfig.platformName}>
          <AegisMark className="size-6" />
          <span
            className="text-sm font-bold tracking-[0.26em]"
            style={{ fontFamily: "var(--font-data)" }}
          >
            AEGIS
          </span>
        </Link>

        {/* ── 去哪看 ── */}
        <NavigationMenu className="ml-4 max-lg:hidden" aria-label={navLabel} viewport={false}>
          <NavigationMenuList>
            <NavigationMenuItem>
              <NavigationMenuTrigger className="bg-transparent">产品</NavigationMenuTrigger>
              <NavigationMenuContent>
                <ul className="grid w-[22rem] gap-1 p-1.5">
                  {PRODUCT_LINKS.map((link) => (
                    <li key={link.href}>
                      <NavigationMenuLink asChild>
                        <Link href={link.href}>
                          <span className="text-sm font-medium">{link.label}</span>
                          <span className="text-xs text-muted-foreground">
                            {link.description}
                          </span>
                        </Link>
                      </NavigationMenuLink>
                    </li>
                  ))}
                </ul>
              </NavigationMenuContent>
            </NavigationMenuItem>

            <NavigationMenuItem>
              <NavigationMenuTrigger className="bg-transparent">开发者</NavigationMenuTrigger>
              <NavigationMenuContent>
                <ul className="grid w-[22rem] gap-1 p-1.5">
                  {DEVELOPER_LINKS.map((link) => (
                    <li key={link.href}>
                      <NavigationMenuLink asChild>
                        <Link href={link.href}>
                          <span className="text-sm font-medium">{link.label}</span>
                          <span className="text-xs text-muted-foreground">
                            {link.description}
                          </span>
                        </Link>
                      </NavigationMenuLink>
                    </li>
                  ))}
                  <li>
                    <NavigationMenuLink asChild>
                      <a
                        href={`${appConfig.apiBaseUrl}/openapi.json`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        <span className="flex items-center gap-1.5 text-sm font-medium">
                          OpenAPI 规范
                          <ExternalLink className="size-3" />
                        </span>
                        <span className="text-xs text-muted-foreground">
                          由运行中的路由树实时生成
                        </span>
                      </a>
                    </NavigationMenuLink>
                  </li>
                </ul>
              </NavigationMenuContent>
            </NavigationMenuItem>

            <NavigationMenuItem>
              <NavigationMenuLink
                asChild
                active={current === "status"}
                className={cn(navigationMenuTriggerStyle(), "bg-transparent")}
              >
                <Link href="/status">服务状态</Link>
              </NavigationMenuLink>
            </NavigationMenuItem>
          </NavigationMenuList>
        </NavigationMenu>

        {/* ── 要做什么 ── */}
        <div className="ml-auto flex items-center gap-1.5">
          <ThemeToggle />
          {authenticated ? null : (
            <Button asChild variant="ghost" size="sm" className="max-sm:hidden">
              <Link href="/login">登录</Link>
            </Button>
          )}
          {/* 已登录时只写「控制台」：首屏主按钮上已经有一个「进入控制台」，
              两个一模一样的词并排出现，看起来像是渲染重复了。
              栏上这个是导航，首屏那个才是这一页的主操作。 */}
          <Button asChild size="sm">
            <Link href={authenticated ? "/overview" : "/login"}>
              {authenticated ? "控制台" : "登录控制台"}
            </Link>
          </Button>

          {/* 小屏把导航收进抽屉：并排摆在 375px 宽度里会换行，
              把 logo 顶掉一行，滚动时那一栏还会跟着变高。 */}
          <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon-sm" className="lg:hidden" aria-label="打开导航">
                <Menu />
              </Button>
            </SheetTrigger>
            <SheetContent side="right" className="w-72">
              <SheetHeader>
                <SheetTitle>{navLabel}</SheetTitle>
              </SheetHeader>
              <nav className="flex flex-col gap-1 px-4">
                {MOBILE_LINKS.map((item) => (
                  <Link
                    key={item.href}
                    href={item.href}
                    onClick={() => setMenuOpen(false)}
                    aria-current={
                      (item.href === "/" && current === "home") ||
                      (item.href === "/status" && current === "status")
                        ? "page"
                        : undefined
                    }
                    className={cn(
                      "rounded-md px-3 py-2.5 text-sm transition-colors",
                      (item.href === "/" && current === "home") ||
                        (item.href === "/status" && current === "status")
                        ? "bg-muted font-medium text-foreground"
                        : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                    )}
                  >
                    {item.label}
                  </Link>
                ))}
                {authenticated ? null : (
                  <>
                    <Separator className="my-2" />
                    <Link
                      href="/login"
                      onClick={() => setMenuOpen(false)}
                      className="rounded-md px-3 py-2.5 text-sm text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
                    >
                      登录
                    </Link>
                  </>
                )}
              </nav>
            </SheetContent>
          </Sheet>
        </div>
      </div>
    </header>
  );
}

function ThemeToggle() {
  const { isDark, toggle } = useThemeTransition();
  const isClient = useIsClient();

  // 服务端渲染阶段主题未知，先占位避免水合不一致
  if (!isClient) return <span className="size-8" aria-hidden />;

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={(event) => toggle(event)}
      aria-label={isDark ? "切换到浅色主题" : "切换到深色主题"}
    >
      {isDark ? <Sun /> : <Moon />}
    </Button>
  );
}
