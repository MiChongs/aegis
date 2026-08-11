"use client";

import { useEffect, useState, type ReactNode } from "react";
import { AlertTriangle, ArrowLeft, ChevronDown, Copy, RotateCcw, CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AegisMark } from "@/components/brand/aegis-mark";
import { cn } from "@/lib/utils";

type ErrorPageProps = {
  title?: string;
  description?: string;
  digest?: string;
  message?: string;
  stack?: string;
  icon?: ReactNode;
  primaryAction?: { label: string; onClick: () => void; icon?: ReactNode };
  secondaryAction?: { label: string; onClick: () => void; icon?: ReactNode };
  variant?: "error" | "notfound";
};

export function ErrorPage({
  title,
  description,
  digest,
  message,
  stack,
  icon,
  primaryAction,
  secondaryAction,
  variant = "error"
}: ErrorPageProps) {
  const [showDetail, setShowDetail] = useState(false);
  const [copied, setCopied] = useState(false);
  const [isDev, setIsDev] = useState(false);

  useEffect(() => {
    setIsDev(typeof process !== "undefined" && process.env.NODE_ENV === "development");
  }, []);

  const copyDiagnostic = async () => {
    try {
      const payload = [
        digest ? `Error ID: ${digest}` : null,
        message ? `Message: ${message}` : null,
        stack ? `Stack:\n${stack}` : null,
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

  const accent =
    variant === "error"
      ? "text-destructive"
      : "text-muted-foreground";

  return (
    <div className="relative flex min-h-[100dvh] items-center justify-center overflow-hidden bg-background px-6 py-12">
      {/* 背景装饰：极淡的径向光晕，深浅色自适应 */}
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute inset-0 -z-10",
          "bg-[radial-gradient(ellipse_60%_45%_at_50%_30%,hsl(var(--accent)/0.25),transparent_70%)]",
          "dark:bg-[radial-gradient(ellipse_55%_40%_at_50%_25%,rgba(120,130,160,0.12),transparent_70%)]"
        )}
      />

      <div className="w-full max-w-lg">
        {/* 品牌标 */}
        <div className="mb-10 flex items-center justify-center gap-2 text-muted-foreground/70">
          <AegisMark className="size-5" />
          <span className="text-xs font-medium tracking-wider uppercase">Aegis Console</span>
        </div>

        {/* 主卡片 */}
        <div className="rounded-2xl border border-border bg-card px-8 py-10 shadow-none dark:shadow-xl dark:shadow-black/20">
          {/* 图标 */}
          <div className="mb-6 flex justify-center">
            <div
              className={cn(
                "flex size-14 items-center justify-center rounded-2xl border",
                variant === "error"
                  ? "border-destructive/20 bg-destructive/5 text-destructive"
                  : "border-border bg-muted text-muted-foreground"
              )}
            >
              {icon ?? <AlertTriangle className="size-6" strokeWidth={1.75} />}
            </div>
          </div>

          {/* 标题与描述 */}
          <div className="mb-8 text-center">
            <h1 className="text-xl font-semibold tracking-tight text-foreground">
              {title ?? "页面加载失败"}
            </h1>
            <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
              {description ?? "页面在渲染过程中出现问题，您可以尝试重新加载或返回上一页。"}
            </p>
          </div>

          {/* 操作按钮 */}
          <div className="flex flex-col gap-2 sm:flex-row sm:justify-center">
            {primaryAction && (
              <Button
                onClick={primaryAction.onClick}
                size="default"
                className="min-w-28"
              >
                {primaryAction.icon ?? <RotateCcw className="size-4" />}
                {primaryAction.label}
              </Button>
            )}
            {secondaryAction && (
              <Button
                onClick={secondaryAction.onClick}
                variant="outline"
                size="default"
                className="min-w-28"
              >
                {secondaryAction.icon ?? <ArrowLeft className="size-4" />}
                {secondaryAction.label}
              </Button>
            )}
          </div>

          {/* 错误 ID + 详情（仅 error 变体显示） */}
          {variant === "error" && (digest || message) && (
            <div className="mt-8 border-t border-border pt-5">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-baseline gap-2 overflow-hidden">
                  <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground/80">
                    错误 ID
                  </span>
                  <code className={cn("truncate font-mono text-xs", accent)}>
                    {digest ?? "—"}
                  </code>
                </div>
                <button
                  type="button"
                  onClick={copyDiagnostic}
                  className="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-[11px] font-medium text-muted-foreground transition-colors hover:border-border hover:bg-accent hover:text-accent-foreground"
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

              {isDev && (message || stack) && (
                <div className="mt-3">
                  <button
                    type="button"
                    onClick={() => setShowDetail((v) => !v)}
                    className="flex w-full items-center gap-1.5 rounded-md text-[11px] font-medium text-muted-foreground transition-colors hover:text-foreground"
                  >
                    <ChevronDown
                      className={cn(
                        "size-3 transition-transform duration-200",
                        showDetail ? "rotate-0" : "-rotate-90"
                      )}
                    />
                    {showDetail ? "隐藏开发详情" : "显示开发详情"}
                  </button>
                  {showDetail && (
                    <pre className="mt-2 max-h-56 overflow-auto rounded-lg border border-border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed text-muted-foreground">
                      {message ? <span className="text-destructive">{message}</span> : null}
                      {message && stack ? "\n\n" : null}
                      {stack}
                    </pre>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        {/* 底部辅助链接 */}
        <div className="mt-6 flex items-center justify-center gap-1 text-xs text-muted-foreground/70">
          <span>问题反复出现？</span>
          <a href="/" className="font-medium text-foreground hover:underline underline-offset-2">
            返回首页
          </a>
        </div>
      </div>
    </div>
  );
}
