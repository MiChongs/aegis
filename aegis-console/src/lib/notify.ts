"use client";

import * as React from "react";
import { toast, type ExternalToast } from "sonner";
import {
  DetailToast,
  type DetailToastAction,
  type DetailVariant,
  type ToastDetails
} from "@/components/ui/toast-detail";

/**
 * Aegis Console 统一通知 API
 *
 * 设计原则：
 * 1. 所有组件只用 `notify.*` 调用，不直接 import sonner
 * 2. 简单场景（成功/失败/警告/信息）用链式 API
 * 3. 复杂场景（附元数据、错误堆栈、可展开详情）用 `notify.detail`
 * 4. Promise 态流、loading 态由 sonner 原生能力透传
 */

export type NotifyVariant = "success" | "error" | "warning" | "info";

type SimpleOptions = Omit<ExternalToast, "icon" | "unstyled">;

function simple(variant: NotifyVariant | null, title: React.ReactNode, options?: SimpleOptions) {
  const t = title as string | React.ReactNode;
  switch (variant) {
    case "success":
      return toast.success(t as string, options);
    case "error":
      return toast.error(t as string, options);
    case "warning":
      return toast.warning(t as string, options);
    case "info":
      return toast.info(t as string, options);
    default:
      return toast(t as string, options);
  }
}

export interface NotifyDetailOptions {
  variant?: DetailVariant;
  description?: React.ReactNode;
  details?: ToastDetails;
  actions?: DetailToastAction[];
  defaultOpen?: boolean;
  duration?: number | typeof Infinity;
}

function detail(title: React.ReactNode, options: NotifyDetailOptions = {}) {
  const { duration = 6000, ...rest } = options;
  return toast.custom(
    (id) =>
      React.createElement(DetailToast, {
        id,
        title,
        ...rest
      }),
    { duration: duration === Infinity ? Infinity : Number(duration) }
  );
}

/**
 * 把任意 unknown 错误转成适合 detail 展示的结构。
 * - `ApiError` / `Error` → title + description + details（含 stack）
 * - 其他 → JSON 序列化
 */
export function errorToDetail(err: unknown): NotifyDetailOptions {
  if (err instanceof Error) {
    const details: Record<string, React.ReactNode> = {
      name: err.name
    };
    const anyErr = err as unknown as Record<string, unknown>;
    if (typeof anyErr.status === "number") details.status = String(anyErr.status);
    if (typeof anyErr.code === "string") details.code = anyErr.code;
    if (typeof anyErr.requestId === "string") details.requestId = anyErr.requestId;
    if (err.stack) details.stack = err.stack;
    return {
      variant: "error",
      description: err.message,
      details
    };
  }
  return {
    variant: "error",
    description: "未知错误",
    details: typeof err === "string" ? err : JSON.stringify(err, null, 2)
  };
}

export const notify = {
  /** 成功提示 */
  success: (title: React.ReactNode, options?: SimpleOptions) => simple("success", title, options),
  /** 错误提示 */
  error: (title: React.ReactNode, options?: SimpleOptions) => simple("error", title, options),
  /** 警告提示 */
  warning: (title: React.ReactNode, options?: SimpleOptions) => simple("warning", title, options),
  /** 信息提示 */
  info: (title: React.ReactNode, options?: SimpleOptions) => simple("info", title, options),
  /** 普通消息（无图标语义） */
  message: (title: React.ReactNode, options?: SimpleOptions) => simple(null, title, options),
  /** Loading 态（返回 id，调用 `notify.dismiss(id)` 或再 `notify.success` 覆盖） */
  loading: (title: React.ReactNode, options?: SimpleOptions) => toast.loading(title as string, options),
  /** Promise 流（自动在 pending / resolved / rejected 之间切换） */
  promise: toast.promise,
  /** 手动关闭指定 id 或全部 */
  dismiss: toast.dismiss,
  /**
   * 详情模式：带可展开的详情区，适合错误元数据、审计日志、请求追踪等。
   *
   * @example
   * notify.detail("保存失败", {
   *   variant: "error",
   *   description: "后端返回 500",
   *   details: {
   *     requestId: "req_abc123",
   *     code: "INTERNAL_ERROR",
   *     message: "Database connection refused"
   *   },
   *   actions: [
   *     { label: "重试", onClick: () => retry() }
   *   ]
   * });
   */
  detail,
  /** 把任意 Error 直接变成 detail toast */
  errorDetail: (title: React.ReactNode, err: unknown, extra?: NotifyDetailOptions) =>
    detail(title, { ...errorToDetail(err), ...extra })
};

export type { DetailVariant, ToastDetails, DetailToastAction };
