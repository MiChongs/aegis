"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Building2, Crown, LayoutGrid } from "lucide-react";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { useTicketAppsQuery } from "@/lib/ticket-hooks";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { nameInitials } from "../ticket-shared";
import { cn } from "@/lib/utils";

// 配置范围：平台级（appid 0）或某个应用。
//
// 应用列表合并两路：工单应用汇总（后端已把我创建的排在前面、带 owned 标记）与应用列表（补上还没有工单的应用）。
// 选中值放在地址栏 ?scope=，刷新与分享链接都能落回同一个范围；没写时默认第一个我创建的应用，没有则平台级。

export type ScopeApp = { id: number; name: string; owned: boolean; active?: number };

export function useSettingsScope() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const appsQuery = useAdminAppsQuery();
  const summaryQuery = useTicketAppsQuery();

  const apps = useMemo(() => {
    const map = new Map<number, ScopeApp>();
    for (const item of summaryQuery.data ?? []) {
      if (item.appid === 0) continue;
      map.set(item.appid, { id: item.appid, name: item.appName, owned: item.owned, active: item.active });
    }
    for (const app of appsQuery.data ?? []) {
      if (!map.has(app.id)) map.set(app.id, { id: app.id, name: app.name, owned: false });
    }
    return Array.from(map.values());
  }, [appsQuery.data, summaryQuery.data]);

  const owned = useMemo(() => apps.filter((app) => app.owned), [apps]);
  const others = useMemo(() => apps.filter((app) => !app.owned), [apps]);
  const loading = appsQuery.isLoading || summaryQuery.isLoading;

  const raw = searchParams.get("scope");
  const parsed = raw === null ? null : Number(raw);
  const fallback = owned[0]?.id ?? 0;
  const appId = parsed !== null && Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;

  const setAppId = useCallback(
    (next: number) => {
      const params = new URLSearchParams(searchParams.toString());
      params.set("scope", String(next));
      router.replace(`${pathname}?${params.toString()}`, { scroll: false });
    },
    [pathname, router, searchParams]
  );

  const current = appId === 0 ? null : apps.find((app) => app.id === appId) ?? null;
  return { appId, setAppId, owned, others, current, loading, resolved: raw !== null || !loading };
}

export function ScopePicker({
  appId,
  onChange,
  owned,
  others,
  loading
}: {
  appId: number;
  onChange: (appId: number) => void;
  owned: ScopeApp[];
  others: ScopeApp[];
  loading: boolean;
}) {
  const isSuperAdmin = useAuthStore((state) => Boolean(state.operator?.isSuperAdmin));
  if (loading) return <Skeleton className="h-9 w-64 rounded-md" />;

  return (
    <Select value={String(appId)} onValueChange={(value) => onChange(Number(value))}>
      <SelectTrigger className="h-9 w-64" aria-label="配置范围">
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="max-h-96">
        {owned.length > 0 ? (
          <SelectGroup>
            <SelectLabel className="flex items-center gap-1.5">
              <Crown className="size-3" />
              我创建的应用
            </SelectLabel>
            {owned.map((app) => (
              <AppOption key={app.id} app={app} />
            ))}
          </SelectGroup>
        ) : null}
        {others.length > 0 ? (
          <>
            {owned.length > 0 ? <SelectSeparator /> : null}
            <SelectGroup>
              <SelectLabel className="flex items-center gap-1.5">
                <LayoutGrid className="size-3" />
                其他应用
              </SelectLabel>
              {others.map((app) => (
                <AppOption key={app.id} app={app} />
              ))}
            </SelectGroup>
          </>
        ) : null}
        <SelectSeparator />
        <SelectGroup>
          <SelectLabel className="flex items-center gap-1.5">
            <Building2 className="size-3" />
            平台级
          </SelectLabel>
          <SelectItem value="0">
            <span className="flex items-center gap-2">
              <span className="flex size-5 items-center justify-center rounded bg-muted text-muted-foreground">
                <Building2 className="size-3" />
              </span>
              平台级配置
              {!isSuperAdmin ? <span className="text-[11px] text-muted-foreground">只读</span> : null}
            </span>
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function AppOption({ app }: { app: ScopeApp }) {
  return (
    <SelectItem value={String(app.id)}>
      <span className="flex min-w-0 items-center gap-2">
        <AppMark name={app.name} />
        <span className="truncate">{app.name}</span>
        {app.active ? <span className="text-[11px] tabular-nums text-muted-foreground">{app.active} 未结</span> : null}
      </span>
    </SelectItem>
  );
}

const APP_TONES = [
  "bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300",
  "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
  "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  "bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
  "bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300",
  "bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-300"
];

function appTone(seed: string) {
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) hash = (hash * 31 + seed.charCodeAt(i)) | 0;
  return APP_TONES[Math.abs(hash) % APP_TONES.length];
}

export function AppMark({ name, className }: { name: string; className?: string }) {
  return (
    <span
      className={cn(
        "flex size-5 shrink-0 items-center justify-center rounded text-[10px] font-semibold",
        appTone(name),
        className
      )}
    >
      {nameInitials(name)}
    </span>
  );
}
