"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ExternalLink, Moon, Sun } from "lucide-react";
import { AegisMark } from "@/components/brand/aegis-mark";
import { appConfig } from "@/lib/env";
import { useIsClient } from "@/lib/use-client-value";
import { useThemeTransition } from "@/lib/theme-transition";
import { cn } from "@/lib/utils";

const navItems = [
  { href: "/developers", label: "快速接入", exact: true },
  { href: "/developers/api", label: "接口文档", exact: false }
] as const;

function ThemeToggle() {
  const { isDark: dark, toggle } = useThemeTransition();
  const isClient = useIsClient();

  // 服务端渲染阶段主题未知，先渲染占位避免水合不一致
  if (!isClient) return <span className="size-8" aria-hidden />;

  return (
    <button
      type="button"
      onClick={(event) => toggle(event)}
      className="inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      aria-label={dark ? "切换到浅色主题" : "切换到深色主题"}
    >
      {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
    </button>
  );
}

/**
 * 开发者门户外壳 —— 公开访问，不经过 AuthGate。
 *
 * 与控制台 Shell 刻意区分：没有侧边栏、没有应用选择器，
 * 因为门户面向的是「还没有账号的接入方」。
 */
export function PortalShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      <header className="sticky top-0 z-40 border-b bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 w-full max-w-[1400px] items-center gap-4 px-4 md:px-6">
          <Link href="/developers" className="flex shrink-0 items-center gap-2.5" aria-label="Aegis 开发者">
            <AegisMark className="size-5" />
            <span className="flex items-baseline gap-2">
              <strong
                className="text-sm font-bold tracking-[0.28em]"
                style={{ fontFamily: "var(--font-data)" }}
              >
                AEGIS
              </strong>
              <span className="text-sm text-muted-foreground max-sm:hidden">开发者</span>
            </span>
          </Link>

          <nav className="flex min-w-0 items-center gap-1 text-sm" aria-label="开发者门户导航">
            {navItems.map((item) => {
              const active = item.exact ? pathname === item.href : pathname.startsWith(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "rounded-md px-3 py-1.5 transition-colors",
                    active
                      ? "bg-muted font-medium text-foreground"
                      : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>

          <div className="ml-auto flex shrink-0 items-center gap-1">
            <a
              href={`${appConfig.apiBaseUrl}/openapi.json`}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground max-md:hidden"
            >
              OpenAPI
              <ExternalLink className="size-3.5" />
            </a>
            <ThemeToggle />
            <Link
              href="/overview"
              className="rounded-md border px-3 py-1.5 text-sm transition-colors hover:bg-muted"
            >
              控制台
            </Link>
          </div>
        </div>
      </header>

      <main className="flex-1">{children}</main>

      <footer className="border-t py-6">
        <div className="mx-auto flex w-full max-w-[1400px] flex-wrap items-center justify-between gap-3 px-4 text-xs text-muted-foreground md:px-6">
          <span>{appConfig.platformName} 开放接口 · Auth Protocol v2</span>
          <span className="font-mono">{appConfig.environment}</span>
        </div>
      </footer>
    </div>
  );
}
