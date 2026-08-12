"use client";

import Link from "next/link";
import { appConfig } from "@/lib/env";

/**
 * 品牌页脚。
 *
 * 条款入口是**真链接**而不是弹窗触发器：法律文本需要一个可分享、可收藏、
 * 搜索引擎能索引到的稳定地址，弹窗三样都做不到。
 */
export function LegalFooter() {
  return (
    <footer className="relative z-10 flex flex-wrap items-center justify-center gap-x-3 gap-y-1 py-4 text-[11px] text-white/40">
      <span>&copy; {new Date().getFullYear()} {appConfig.platformName}. All rights reserved.</span>
      <span className="hidden sm:inline">|</span>
      <Link href="/legal/terms" className="underline underline-offset-2 transition-colors hover:text-white/70">
        用户协议 Terms
      </Link>
      <span>|</span>
      <Link href="/legal/privacy" className="underline underline-offset-2 transition-colors hover:text-white/70">
        隐私政策 Privacy
      </Link>
    </footer>
  );
}
