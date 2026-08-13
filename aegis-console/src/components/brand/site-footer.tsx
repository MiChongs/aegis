"use client";

import Link from "next/link";
import { AegisMark } from "@/components/brand/aegis-mark";
import { footerColumns } from "@/components/brand/home/home-content";
import { SECTION_CONTAINER } from "@/components/brand/home/section";
import { appConfig } from "@/lib/env";

/**
 * 公开站点页脚。
 *
 * 条款入口是**真链接**而不是弹窗触发器：法律文本需要一个可分享、可收藏、
 * 搜索引擎能索引到的稳定地址，弹窗三样都做不到。
 */
export function SiteFooter() {
  return (
    <footer className="border-t">
      <div className={`${SECTION_CONTAINER} py-12 md:py-16`}>
        <div className="grid gap-10 md:grid-cols-[minmax(0,1fr)_auto] md:gap-16">
          <div className="max-w-sm">
            <Link href="/" className="flex w-fit items-center gap-2.5" aria-label={appConfig.platformName}>
              <AegisMark className="size-5" />
              <span
                className="text-sm font-bold tracking-[0.26em]"
                style={{ fontFamily: "var(--font-data)" }}
              >
                AEGIS
              </span>
            </Link>
            <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
              自托管的多租户用户系统平台。认证、授权、组织、资产与风控由同一套后端提供，
              数据存储于自有数据库。
            </p>
          </div>

          <nav
            aria-label="页脚导航"
            className="grid grid-cols-2 gap-8 sm:grid-cols-3 md:gap-14"
          >
            {footerColumns.map((column) => (
              <div key={column.title}>
                <h2 className="text-xs font-medium tracking-[0.16em] text-muted-foreground uppercase">
                  {column.title}
                </h2>
                <ul className="mt-4 flex flex-col gap-2.5">
                  {column.links.map((link) => (
                    <li key={link.href}>
                      <Link
                        href={link.href}
                        className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                      >
                        {link.label}
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </nav>
        </div>

        <div className="mt-12 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t pt-6 text-xs text-muted-foreground">
          <span>
            &copy; {new Date().getFullYear()} {appConfig.platformName}. All rights reserved.
          </span>
          <span className="font-mono">{appConfig.environment}</span>
        </div>
      </div>
    </footer>
  );
}
