"use client";

import * as React from "react";
import { toast } from "sonner";
import { motion, AnimatePresence } from "motion/react";
import {
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Info,
  Loader2,
  ChevronDown,
  Copy,
  Check,
  X
} from "lucide-react";
import { cn } from "@/lib/utils";

export type DetailVariant = "success" | "error" | "warning" | "info" | "loading" | "default";
export type ToastDetails = Record<string, React.ReactNode> | string | React.ReactNode;

export interface DetailToastAction {
  label: string;
  onClick: () => void;
  variant?: "primary" | "secondary";
}

export interface DetailToastProps {
  id: string | number;
  variant?: DetailVariant;
  title: React.ReactNode;
  description?: React.ReactNode;
  details?: ToastDetails;
  defaultOpen?: boolean;
  actions?: DetailToastAction[];
}

const VARIANT_META: Record<
  DetailVariant,
  { Icon: React.ComponentType<{ className?: string; strokeWidth?: number }>; cls: string }
> = {
  success: { Icon: CheckCircle2, cls: "text-emerald-600 dark:text-emerald-400" },
  error: { Icon: XCircle, cls: "text-red-600 dark:text-red-400" },
  warning: { Icon: AlertTriangle, cls: "text-amber-600 dark:text-amber-400" },
  info: { Icon: Info, cls: "text-sky-600 dark:text-sky-400" },
  loading: { Icon: Loader2, cls: "text-muted-foreground animate-spin" },
  default: { Icon: Info, cls: "text-muted-foreground" }
};

function isRecord(v: unknown): v is Record<string, React.ReactNode> {
  return !!v && typeof v === "object" && !React.isValidElement(v) && !Array.isArray(v);
}

function hasDetails(d: ToastDetails | undefined) {
  if (d === undefined || d === null) return false;
  if (React.isValidElement(d)) return true;
  if (typeof d === "string") return d.trim().length > 0;
  if (isRecord(d)) return Object.keys(d).length > 0;
  return false;
}

function serializeDetails(d: ToastDetails): string {
  if (typeof d === "string") return d;
  if (isRecord(d)) {
    return Object.entries(d)
      .map(([k, v]) => {
        const val =
          v === null || v === undefined
            ? "—"
            : typeof v === "object" && !React.isValidElement(v)
              ? JSON.stringify(v, null, 2)
              : String(v);
        return `${k}: ${val}`;
      })
      .join("\n");
  }
  return "";
}

export function DetailToast({
  id,
  variant = "default",
  title,
  description,
  details,
  defaultOpen = false,
  actions
}: DetailToastProps) {
  const [open, setOpen] = React.useState(defaultOpen);
  const [copied, setCopied] = React.useState(false);
  const { Icon, cls } = VARIANT_META[variant];
  const canExpand = hasDetails(details);

  async function handleCopy() {
    if (!details) return;
    try {
      await navigator.clipboard.writeText(serializeDetails(details));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* noop */
    }
  }

  return (
    <div className="relative w-[360px] overflow-hidden rounded-xl border border-border bg-popover text-popover-foreground shadow-[0_6px_20px_-6px_rgba(0,0,0,0.14),0_2px_6px_-2px_rgba(0,0,0,0.06)] dark:shadow-[0_10px_28px_-8px_rgba(0,0,0,0.6),0_2px_8px_-2px_rgba(0,0,0,0.35)]">
      {/* 头部 */}
      <div className="flex items-start gap-2.5 px-3 py-2.5 pr-9">
        <span className="mt-0.5 flex size-5 shrink-0 items-center justify-center">
          <Icon className={cn("size-5", cls)} strokeWidth={2.25} />
        </span>

        <div className="min-w-0 flex-1 space-y-0.5">
          <div className="text-[13.5px] font-medium leading-snug tracking-tight text-foreground">{title}</div>
          {description && (
            <div className="text-[12.5px] leading-relaxed text-muted-foreground">{description}</div>
          )}

          {(canExpand || (actions && actions.length > 0)) && (
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              {canExpand && (
                <button
                  type="button"
                  onClick={() => setOpen((v) => !v)}
                  className="inline-flex h-6 items-center gap-1 rounded-md bg-muted px-2 text-[11px] font-medium text-foreground transition hover:bg-muted-foreground/20"
                  aria-expanded={open}
                >
                  <ChevronDown
                    className={cn("size-3 transition-transform duration-200", open && "rotate-180")}
                  />
                  {open ? "收起" : "查看详情"}
                </button>
              )}
              {actions?.map((a, i) => (
                <button
                  key={i}
                  type="button"
                  onClick={() => {
                    a.onClick();
                    toast.dismiss(id);
                  }}
                  className={cn(
                    "inline-flex h-6 items-center rounded-md px-2 text-[11px] font-medium transition",
                    a.variant === "secondary"
                      ? "bg-muted text-foreground hover:bg-muted-foreground/20"
                      : "bg-foreground text-background hover:brightness-90"
                  )}
                >
                  {a.label}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 关闭按钮 */}
      <button
        type="button"
        onClick={() => toast.dismiss(id)}
        aria-label="关闭"
        className="absolute right-1.5 top-1.5 flex size-5 items-center justify-center rounded-md text-muted-foreground opacity-60 transition hover:bg-muted hover:text-foreground hover:opacity-100"
      >
        <X className="size-3" />
      </button>

      {/* 详情展开区 */}
      <AnimatePresence initial={false}>
        {canExpand && open && (
          <motion.div
            key="detail"
            initial={{ height: 0 }}
            animate={{ height: "auto" }}
            exit={{ height: 0 }}
            transition={{ duration: 0.18, ease: "linear" }}
            className="overflow-hidden border-t border-border/70 bg-muted/60"
          >
            <div className="px-3 py-2.5">
              <div className="mb-1.5 flex items-center justify-between">
                <span className="text-[9.5px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                  详情
                </span>
                {(typeof details === "string" || isRecord(details)) && (
                  <button
                    type="button"
                    onClick={handleCopy}
                    className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground transition hover:bg-muted hover:text-foreground"
                  >
                    {copied ? (
                      <>
                        <Check className="size-3 text-emerald-500" />
                        已复制
                      </>
                    ) : (
                      <>
                        <Copy className="size-3" />
                        复制
                      </>
                    )}
                  </button>
                )}
              </div>

              {typeof details === "string" ? (
                <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background p-2 font-mono text-[11px] leading-relaxed">
                  {details}
                </pre>
              ) : isRecord(details) ? (
                <dl className="space-y-1 text-[11.5px]">
                  {Object.entries(details).map(([k, v]) => (
                    <div key={k} className="flex gap-2">
                      <dt className="w-20 shrink-0 truncate text-muted-foreground">{k}</dt>
                      <dd className="min-w-0 flex-1 break-all font-mono text-foreground/90">
                        {v === null || v === undefined
                          ? "—"
                          : typeof v === "object" && !React.isValidElement(v)
                            ? JSON.stringify(v)
                            : (v as React.ReactNode)}
                      </dd>
                    </div>
                  ))}
                </dl>
              ) : (
                <div className="text-[12px] leading-relaxed">{details as React.ReactNode}</div>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
