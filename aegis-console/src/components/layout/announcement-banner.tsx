"use client";

import { AlertTriangle, Info, Megaphone, X } from "lucide-react";
import { useNotificationStore } from "@/lib/notification-store";
import { cn } from "@/lib/utils";

const levelConfig = {
  critical: { bg: "bg-red-50 dark:bg-red-950/40", border: "border-red-200 dark:border-red-900", text: "text-red-800 dark:text-red-200", icon: AlertTriangle },
  important: { bg: "bg-amber-50 dark:bg-amber-950/40", border: "border-amber-200 dark:border-amber-900", text: "text-amber-800 dark:text-amber-200", icon: AlertTriangle },
  normal: { bg: "bg-blue-50 dark:bg-blue-950/40", border: "border-blue-200 dark:border-blue-900", text: "text-blue-800 dark:text-blue-200", icon: Info },
} as const;

export function AnnouncementBanner() {
  const announcements = useNotificationStore((s) => s.announcements);
  const dismissedIds = useNotificationStore((s) => s.dismissedIds);
  const dismiss = useNotificationStore((s) => s.dismiss);

  // 仅显示未关闭的置顶或重要公告
  const visible = announcements.filter(
    (a) => !dismissedIds.includes(a.id) && (a.pinned || a.level === "critical" || a.level === "important")
  );

  if (!visible.length) return null;

  return (
    <div className="space-y-2">
      {visible.map((a) => {
        const cfg = levelConfig[a.level as keyof typeof levelConfig] || levelConfig.normal;
        const Icon = cfg.icon;
        return (
          <div key={a.id} className={cn("flex items-start gap-2.5 rounded-lg border px-3 py-2", cfg.bg, cfg.border)}>
            <Icon className={cn("size-4 mt-0.5 shrink-0", cfg.text)} />
            <div className="min-w-0 flex-1">
              <p className={cn("text-xs font-medium", cfg.text)}>{a.title}</p>
              {a.content && <div className={cn("text-[11px] mt-0.5 opacity-80 *:m-0 line-clamp-2", cfg.text)} dangerouslySetInnerHTML={{ __html: a.content }} />}
            </div>
            <button
              type="button"
              title="关闭"
              className={cn("shrink-0 rounded p-0.5 transition-colors hover:bg-black/5 dark:hover:bg-white/10", cfg.text)}
              onClick={() => dismiss(a.id)}
            >
              <X className="size-3.5" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
