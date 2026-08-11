"use client";

import Link from "next/link";
import { AegisMark } from "@/components/brand/aegis-mark";
import { useAuthStore } from "@/lib/auth-store";

type PublicHeaderProps = {
  current?: "home" | "status" | "developers" | "login" | "console";
  navLabel?: string;
};

const navItems = [
  { key: "home", label: "首页", href: "/" },
  { key: "status", label: "状态", href: "/status" },
  { key: "developers", label: "开发者", href: "/developers" },
  { key: "login", label: "登录", href: "/login" },
  { key: "console", label: "控制台", href: "/overview" }
] as const;

export function PublicHeader({ current, navLabel = "公开导航" }: PublicHeaderProps) {
  const hydrated = useAuthStore((state) => state.hydrated);
  const accessToken = useAuthStore((state) => state.accessToken);
  const authenticated = hydrated && Boolean(accessToken);
  const visibleNavItems = navItems.filter((item) => !(authenticated && item.key === "login"));

  return (
    <header className="relative z-20 mx-auto w-full max-w-[1440px]">
      <div
        className="flex items-center justify-between gap-4 rounded-full px-4 py-3 md:px-5"
        style={{
          borderWidth: 1,
          borderStyle: "solid",
          borderColor: "rgba(255,255,255,0.12)",
          background: "rgba(7,10,17,0.42)",
          backdropFilter: "blur(18px)",
        }}
      >
        <Link href="/" className="inline-flex min-w-0 items-center gap-3" aria-label="AEGIS">
          <span
            className="inline-flex size-10 shrink-0 items-center justify-center rounded-full"
            style={{
              border: "1px solid rgba(255,255,255,0.14)",
              background:
                "radial-gradient(circle at 30% 30%, rgba(255,255,255,0.24), transparent 56%), rgba(255,255,255,0.06)",
              boxShadow: "inset 0 1px 0 rgba(255,255,255,0.12)",
            }}
          >
            <AegisMark className="size-5" />
          </span>
          <span className="flex min-w-0 flex-col leading-none">
            <strong
              className="text-white"
              style={{
                fontFamily: "var(--font-data)",
                fontSize: "0.98rem",
                fontWeight: 700,
                letterSpacing: "0.34em",
                textTransform: "uppercase",
              }}
            >
              AEGIS
            </strong>
          </span>
        </Link>

        <nav className="flex items-center gap-2 text-sm md:gap-3" aria-label={navLabel}>
          {visibleNavItems.map((item) => (
            <Link
              key={item.key}
              href={item.href}
              className="rounded-full px-3 py-2 transition-colors hover:bg-white/[0.06] hover:text-white"
              style={{
                color:
                  current === item.key ? "rgba(255,255,255,1)" : "rgba(255,255,255,0.74)",
                background:
                  current === item.key ? "rgba(255,255,255,0.12)" : undefined,
                boxShadow:
                  current === item.key
                    ? "inset 0 0 0 1px rgba(255,255,255,0.08)"
                    : undefined,
              }}
              aria-current={current === item.key ? "page" : undefined}
            >
              {item.label}
            </Link>
          ))}
        </nav>
      </div>
    </header>
  );
}
