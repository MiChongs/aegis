"use client";

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useAdminAppsQuery } from "@/lib/admin-hooks";

/** 应用作用域：undefined = 全部应用 */
export function AppScopeSelect({ value, onChange, className, allLabel = "全部应用" }: {
  value?: number;
  onChange: (next?: number) => void;
  className?: string;
  allLabel?: string;
}) {
  const apps = useAdminAppsQuery();
  return (
    <Select value={value ? String(value) : "all"} onValueChange={(v) => onChange(v === "all" ? undefined : Number(v))}>
      <SelectTrigger className={className ?? "h-9 w-48"} size="sm">
        <SelectValue placeholder={allLabel} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">{allLabel}</SelectItem>
        {(apps.data ?? []).map((app) => (
          <SelectItem key={app.id} value={String(app.id)}>
            {app.name}
            <span className="ml-1 text-xs text-muted-foreground">#{app.id}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** 应用 ID → 名称 */
export function useAppName() {
  const apps = useAdminAppsQuery();
  const map = new Map((apps.data ?? []).map((a) => [a.id, a.name]));
  return (id?: number) => (id ? map.get(id) ?? `应用 #${id}` : "全部应用");
}
