"use client";

import * as React from "react";
import { AlertTriangle, RefreshCw, Copy } from "lucide-react";
import {
  ErrorBoundary as RbErrorBoundary,
  type FallbackProps
} from "react-error-boundary";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// ─────────────────────────── 类型 ───────────────────────────

type Variant = "full" | "widget" | "inline";

type FallbackUIProps = FallbackProps & {
  variant?: Variant;
  title?: string;
  description?: string;
  /** 默认展示"重试"按钮；传 false 可隐藏 */
  showRetry?: boolean;
};

type BoundaryProps = {
  children: React.ReactNode;
  /** 外部传入的重置回调（常用于清空 state 后配合 React Query 自动重跑） */
  onReset?: () => void;
  /** key 变化时自动重置（例如路由切换、筛选条件变化） */
  resetKeys?: unknown[];
  variant?: Variant;
  title?: string;
  description?: string;
  showRetry?: boolean;
  /** 自定义 fallback（完全覆盖默认 UI） */
  fallbackRender?: (props: FallbackProps) => React.ReactNode;
  /** 上报 / 日志钩子（react-error-boundary v6 泛化为 unknown） */
  onError?: (error: unknown, info: React.ErrorInfo) => void;
  /** 单独控制容器 className（仅默认 fallback 生效） */
  className?: string;
};

// ─────────────────────────── 默认 Fallback UI ───────────────────────────

/**
 * 三种变体：
 *   - full  ：整屏占位（页面级兜底，大号居中图标 + 文案）
 *   - widget：卡片式（图表/表格/异步模块等局部兜底）
 *   - inline：两行紧凑文案（表单字段、按钮旁等狭窄区域）
 *
 * 配色：全程 Zinc token + destructive 语义色，深浅色兼容；浅色扁平无阴影，深色可叠辉光。
 */
export function ErrorFallback({
  error,
  resetErrorBoundary,
  variant = "widget",
  title,
  description,
  showRetry = true
}: FallbackUIProps) {
  const message = error instanceof Error ? error.message : String(error ?? "未知错误");
  const defaultTitle = title ?? (variant === "full" ? "页面出错了" : "加载失败");
  const defaultDescription = description ?? "已被错误边界捕获，不影响其它模块。你可以重试，或稍后再试。";

  const copyDiagnostics = async () => {
    try {
      const stack = error instanceof Error ? error.stack ?? "" : "";
      await navigator.clipboard.writeText(`${defaultTitle}\n${message}\n\n${stack}`);
    } catch {
      // 无 clipboard 权限时静默失败
    }
  };

  if (variant === "inline") {
    return (
      <div
        role="alert"
        className="inline-flex items-center gap-2 rounded-md border border-destructive/40 bg-destructive/5 px-2.5 py-1.5 text-xs text-destructive"
      >
        <AlertTriangle className="size-3.5 shrink-0" aria-hidden />
        <span className="truncate max-w-[32ch]">{message || defaultTitle}</span>
        {showRetry ? (
          <button
            type="button"
            onClick={resetErrorBoundary}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium hover:bg-destructive/10"
          >
            <RefreshCw className="size-3" /> 重试
          </button>
        ) : null}
      </div>
    );
  }

  const outerClass =
    variant === "full"
      ? "flex min-h-[60vh] w-full items-center justify-center p-6"
      : "flex min-h-32 w-full items-center justify-center p-4";

  return (
    <div role="alert" className={outerClass}>
      <div
        className={cn(
          "flex flex-col items-center gap-3 rounded-xl border bg-card px-6 py-6 text-center",
          "max-w-lg w-full",
          "dark:shadow-[0_0_0_1px_color-mix(in_srgb,var(--border)_60%,transparent),0_12px_40px_-24px_rgba(0,0,0,0.4)]"
        )}
      >
        <div className="grid size-10 place-items-center rounded-full bg-destructive/10 text-destructive">
          <AlertTriangle className="size-5" aria-hidden />
        </div>
        <div className="space-y-1">
          <div className="text-sm font-semibold text-foreground">{defaultTitle}</div>
          <p className="text-xs text-muted-foreground">{defaultDescription}</p>
        </div>
        {message ? (
          <pre className="mt-1 max-h-32 w-full overflow-auto rounded-md border bg-muted/40 px-3 py-2 text-left text-[11px] font-mono leading-5 text-muted-foreground whitespace-pre-wrap break-words">
            {message}
          </pre>
        ) : null}
        <div className="mt-1 flex items-center gap-2">
          {showRetry ? (
            <Button size="sm" variant="default" onClick={resetErrorBoundary} className="h-8 gap-1.5">
              <RefreshCw className="size-3.5" /> 重试
            </Button>
          ) : null}
          <Button size="sm" variant="outline" onClick={copyDiagnostics} className="h-8 gap-1.5">
            <Copy className="size-3.5" /> 复制错误
          </Button>
        </div>
      </div>
    </div>
  );
}

// ─────────────────────────── 核心容器 ───────────────────────────

/**
 * 通用错误边界：配合 `QueryErrorResetBoundary` 自动重跑 Suspense/Query。
 *
 * 使用指引（项目规范）：
 *   - 路由/页面级：<PageBoundary>（variant="full"）—— 已在 Providers 默认挂载
 *   - 图表/表格/async 模块：<WidgetBoundary>（variant="widget"）
 *   - 小型 inline（字段/按钮旁）：<InlineBoundary>
 *
 * 细节：
 *   - 传 `resetKeys={[filterA, filterB]}`，任一变化自动 reset
 *   - 传 `onError` 做日志上报（未来可接入 Sentry / posthog）
 */
export function ErrorBoundary({
  children,
  onReset,
  resetKeys,
  variant = "widget",
  title,
  description,
  showRetry = true,
  fallbackRender,
  onError,
  className
}: BoundaryProps) {
  const defaultFallback = React.useCallback(
    (fbp: FallbackProps) => (
      <div className={className}>
        <ErrorFallback
          {...fbp}
          variant={variant}
          title={title}
          description={description}
          showRetry={showRetry}
        />
      </div>
    ),
    [className, variant, title, description, showRetry]
  );

  return (
    <QueryErrorResetBoundary>
      {({ reset }) => (
        <RbErrorBoundary
          onReset={() => {
            reset();
            onReset?.();
          }}
          resetKeys={resetKeys}
          onError={onError}
          fallbackRender={fallbackRender ?? defaultFallback}
        >
          {children}
        </RbErrorBoundary>
      )}
    </QueryErrorResetBoundary>
  );
}

// ─────────────────────────── 变体快捷封装 ───────────────────────────

/** 页面级：整屏兜底，一般只在根 Providers 用一次 */
export function PageBoundary(props: Omit<BoundaryProps, "variant">) {
  return <ErrorBoundary variant="full" {...props} />;
}

/** 组件级：图表 / 表格 / 异步模块，推荐默认选项 */
export function WidgetBoundary(props: Omit<BoundaryProps, "variant">) {
  return <ErrorBoundary variant="widget" {...props} />;
}

/** 窄区域：inline 小贴士 */
export function InlineBoundary(props: Omit<BoundaryProps, "variant">) {
  return <ErrorBoundary variant="inline" {...props} />;
}

// 原始 API 透出给高级用法（Suspense 搭配、异步错误手动抛等）
export { useErrorBoundary } from "react-error-boundary";
export type { FallbackProps } from "react-error-boundary";
