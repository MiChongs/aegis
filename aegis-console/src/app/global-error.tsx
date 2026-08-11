"use client";

import { useEffect, useState } from "react";
import { AlertOctagon, RotateCcw, Home, Copy, CheckCircle2 } from "lucide-react";
import "./globals.css";

// global-error 会替换 root layout，因此必须自带 <html><body>
// 也不能依赖 Providers / ThemeProvider（此时它们可能尚未挂载或已崩溃）
// 仅使用 Tailwind 工具类 + prefers-color-scheme 自适应
export default function GlobalError({
  error,
  reset
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (typeof window !== "undefined") {
      // eslint-disable-next-line no-console
      console.error("[global-error]", error);
    }
  }, [error]);

  const copyDiagnostic = async () => {
    try {
      const payload = [
        error.digest ? `Error ID: ${error.digest}` : null,
        `Message: ${error.message}`,
        error.stack ? `Stack:\n${error.stack}` : null,
        `URL: ${typeof window !== "undefined" ? window.location.href : ""}`,
        `Time: ${new Date().toISOString()}`
      ]
        .filter(Boolean)
        .join("\n");
      await navigator.clipboard.writeText(payload);
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      /* noop */
    }
  };

  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body className="min-h-screen bg-white text-zinc-900 antialiased dark:bg-zinc-950 dark:text-zinc-50">
        <div className="relative flex min-h-[100dvh] items-center justify-center overflow-hidden px-6 py-12">
          <div
            aria-hidden
            className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(ellipse_55%_40%_at_50%_25%,rgba(239,68,68,0.08),transparent_70%)] dark:bg-[radial-gradient(ellipse_55%_40%_at_50%_25%,rgba(239,68,68,0.14),transparent_70%)]"
          />

          <div className="w-full max-w-lg">
            <div className="mb-10 flex items-center justify-center gap-2 text-zinc-400 dark:text-zinc-500">
              <span className="text-xs font-medium tracking-wider uppercase">Aegis Console</span>
            </div>

            <div className="rounded-2xl border border-zinc-200 bg-white px-8 py-10 dark:border-zinc-800 dark:bg-zinc-900 dark:shadow-xl dark:shadow-black/20">
              <div className="mb-6 flex justify-center">
                <div className="flex size-14 items-center justify-center rounded-2xl border border-red-200 bg-red-50 text-red-600 dark:border-red-500/20 dark:bg-red-500/10 dark:text-red-400">
                  <AlertOctagon className="size-6" strokeWidth={1.75} />
                </div>
              </div>

              <div className="mb-8 text-center">
                <h1 className="text-xl font-semibold tracking-tight">应用加载异常</h1>
                <p className="mt-2 text-sm leading-relaxed text-zinc-500 dark:text-zinc-400">
                  控制台主框架未能正常启动。请尝试重新加载；若问题持续，请联系管理员。
                </p>
              </div>

              <div className="flex flex-col gap-2 sm:flex-row sm:justify-center">
                <button
                  type="button"
                  onClick={() => reset()}
                  className="inline-flex h-10 min-w-28 items-center justify-center gap-2 rounded-lg bg-zinc-900 px-4 text-sm font-medium text-white shadow-sm transition-colors hover:bg-zinc-800 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-zinc-900/20 dark:bg-zinc-50 dark:text-zinc-900 dark:hover:bg-zinc-200 dark:focus-visible:ring-zinc-50/20"
                >
                  <RotateCcw className="size-4" />
                  重新加载
                </button>
                <a
                  href="/"
                  className="inline-flex h-10 min-w-28 items-center justify-center gap-2 rounded-lg border border-zinc-200 bg-white px-4 text-sm font-medium text-zinc-900 transition-colors hover:bg-zinc-50 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-zinc-900/10 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-50 dark:hover:bg-zinc-800"
                >
                  <Home className="size-4" />
                  返回首页
                </a>
              </div>

              {(error.digest || error.message) && (
                <div className="mt-8 border-t border-zinc-200 pt-5 dark:border-zinc-800">
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex items-baseline gap-2 overflow-hidden">
                      <span className="text-[11px] font-medium uppercase tracking-wider text-zinc-400 dark:text-zinc-500">
                        错误 ID
                      </span>
                      <code className="truncate font-mono text-xs text-red-600 dark:text-red-400">
                        {error.digest ?? "—"}
                      </code>
                    </div>
                    <button
                      type="button"
                      onClick={copyDiagnostic}
                      className="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-[11px] font-medium text-zinc-500 transition-colors hover:border-zinc-200 hover:bg-zinc-50 hover:text-zinc-900 dark:text-zinc-400 dark:hover:border-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-50"
                    >
                      {copied ? (
                        <>
                          <CheckCircle2 className="size-3" />
                          已复制
                        </>
                      ) : (
                        <>
                          <Copy className="size-3" />
                          复制诊断
                        </>
                      )}
                    </button>
                  </div>
                </div>
              )}
            </div>

            <div className="mt-6 flex items-center justify-center gap-1 text-xs text-zinc-400 dark:text-zinc-500">
              <span>问题反复出现？</span>
              <a
                href="/"
                className="font-medium text-zinc-900 hover:underline underline-offset-2 dark:text-zinc-50"
              >
                返回首页
              </a>
            </div>
          </div>
        </div>
      </body>
    </html>
  );
}
