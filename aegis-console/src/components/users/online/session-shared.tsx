"use client";

import type { ReactNode } from "react";
import { Loader2, Monitor, Smartphone, Tablet, TerminalSquare } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { cn } from "@/lib/utils";

/**
 * 会话类视图（管理员会话、应用在线连接）共用的客户端解析与确认框。
 *
 * UA 解析只求「一眼分得清是哪台设备」：浏览器取主版本号，系统取到大版本，
 * 完整 UA 仍放在 title 里供排查时复制。
 */

export type ClientKind = "desktop" | "mobile" | "tablet" | "program";

export type ClientInfo = {
  browser: string;
  os: string;
  label: string;
  kind: ClientKind;
};

function major(version?: string) {
  return version ? version.split(/[._]/)[0] : "";
}

function detectOs(ua: string) {
  let m: RegExpMatchArray | null;
  if ((m = ua.match(/HarmonyOS[ /]?([\d.]+)?/i))) return `HarmonyOS ${major(m[1])}`.trim();
  if ((m = ua.match(/Android[ /]?([\d.]+)?/i))) return `Android ${major(m[1])}`.trim();
  if ((m = ua.match(/(?:iPhone|CPU) OS ([\d_]+)/i))) return `iOS ${major(m[1])}`;
  if (/iPad/i.test(ua)) return "iPadOS";
  if ((m = ua.match(/Windows NT ([\d.]+)/i))) return m[1] === "10.0" ? "Windows" : `Windows NT ${m[1]}`;
  if (/Mac OS X|Macintosh/i.test(ua)) return "macOS";
  if (/CrOS/i.test(ua)) return "ChromeOS";
  if (/Linux/i.test(ua)) return "Linux";
  return "";
}

function detectBrowser(ua: string) {
  let m: RegExpMatchArray | null;
  if ((m = ua.match(/Edg(?:e|A|iOS)?\/([\d.]+)/))) return `Edge ${major(m[1])}`;
  if ((m = ua.match(/OPR\/([\d.]+)/))) return `Opera ${major(m[1])}`;
  if ((m = ua.match(/(?:Firefox|FxiOS)\/([\d.]+)/))) return `Firefox ${major(m[1])}`;
  if ((m = ua.match(/MicroMessenger\/([\d.]+)/i))) return `微信 ${major(m[1])}`;
  if ((m = ua.match(/(?:Chrome|CriOS)\/([\d.]+)/))) return `Chrome ${major(m[1])}`;
  if ((m = ua.match(/Version\/([\d.]+).*Safari/))) return `Safari ${major(m[1])}`;
  if ((m = ua.match(/okhttp\/([\d.]+)/i))) return `OkHttp ${m[1]}`;
  if ((m = ua.match(/Dart\/([\d.]+)/i))) return `Dart ${m[1]}`;
  if ((m = ua.match(/(curl|Wget|PostmanRuntime|python-requests|Go-http-client|axios|node-fetch|undici|Dalvik)\/([\d.]+)/i))) {
    return `${m[1]} ${m[2]}`;
  }
  return "";
}

export function parseClient(ua?: string | null): ClientInfo {
  if (!ua) return { browser: "", os: "", label: "未知客户端", kind: "desktop" };
  const os = detectOs(ua);
  const browser = detectBrowser(ua);
  const program = /okhttp|Dart\/|curl|Wget|Postman|python-requests|Go-http-client|axios|node-fetch|undici|Dalvik/i.test(ua);
  const kind: ClientKind = /iPad|Tablet/i.test(ua)
    ? "tablet"
    : /Mobile|Android|iPhone|HarmonyOS/i.test(ua)
      ? "mobile"
      : program
        ? "program"
        : "desktop";
  const label = [browser, os].filter(Boolean).join(" / ") || (program ? "程序化客户端" : "未知客户端");
  return { browser, os, label, kind };
}

export function ClientIcon({ kind, className }: { kind: ClientKind; className?: string }) {
  const Icon = kind === "mobile" ? Smartphone : kind === "tablet" ? Tablet : kind === "program" ? TerminalSquare : Monitor;
  return <Icon className={cn("size-4 shrink-0 text-muted-foreground", className)} />;
}

/** 在线时长：「2 小时 15 分」。传入起点，返回到现在的跨度。 */
export function durationSince(value?: string | null) {
  if (!value) return "";
  const start = new Date(value).getTime();
  if (Number.isNaN(start) || new Date(value).getUTCFullYear() <= 1) return "";
  const minutes = Math.max(0, Math.floor((Date.now() - start) / 60_000));
  if (minutes < 1) return "不足 1 分钟";
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时 ${minutes % 60} 分`;
  const days = Math.floor(hours / 24);
  return `${days} 天 ${hours % 24} 小时`;
}

/** 截断长 ID，保留首尾，完整值仍放 title。 */
export function shortId(value?: string | null, head = 8, tail = 4) {
  if (!value) return "";
  return value.length > head + tail + 1 ? `${value.slice(0, head)}…${value.slice(-tail)}` : value;
}

export function ConfirmActionDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  pending,
  destructive = true,
  onConfirm
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  pending?: boolean;
  destructive?: boolean;
  onConfirm: () => void | Promise<void>;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description ? <AlertDialogDescription>{description}</AlertDialogDescription> : null}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant={destructive ? "destructive" : "default"}
            disabled={pending}
            onClick={(event) => {
              // 等请求结束再关，失败时对话框留着，管理员能看到 toast 后重试
              event.preventDefault();
              void onConfirm();
            }}
          >
            {pending ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
