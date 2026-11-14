"use client";

import { useMemo, useState, type ReactNode } from "react";
import { AppWindow, Check, ChevronsUpDown, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { tierOf, type AdminListEntry, type AdminTier } from "@/lib/api/access";
import { cn } from "@/lib/utils";
import { AdminIdentity, TierBadge } from "./access-kit";

/**
 * 选择器：应用（单选 / 多选）、管理员（多选）。
 *
 * 对应「别让管理员当输入机」：应用 ID 与管理员账号都不该手输。
 */

export function useAppOptions() {
  const query = useAdminAppsQuery();
  return useMemo(
    () =>
      (query.data ?? []).map((app) => ({
        id: app.id,
        name: app.name,
        appKey: app.appKey,
      })),
    [query.data],
  );
}

export function AppPicker({
  value,
  onChange,
  multiple,
  placeholder = "选择应用",
  allowAll,
  allLabel = "全部应用",
  className,
  disabled,
}: {
  value: number[];
  onChange: (next: number[]) => void;
  multiple?: boolean;
  placeholder?: string;
  /** 单选时允许选「全部」（value 为空数组） */
  allowAll?: boolean;
  allLabel?: string;
  className?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const apps = useAppOptions();
  const selected = apps.filter((app) => value.includes(app.id));
  const label = multiple
    ? selected.length === 0
      ? placeholder
      : selected.length <= 2
        ? selected.map((app) => app.name).join("、")
        : `已选 ${selected.length} 个应用`
    : (selected[0]?.name ?? (allowAll ? allLabel : placeholder));

  const toggle = (id: number) => {
    if (multiple)
      onChange(
        value.includes(id)
          ? value.filter((item) => item !== id)
          : [...value, id],
      );
    else {
      onChange([id]);
      setOpen(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          disabled={disabled}
          className={cn(
            "h-9 w-full justify-between gap-2 px-3 font-normal",
            selected.length === 0 && !allowAll && "text-muted-foreground",
            className,
          )}
        >
          <span className="flex min-w-0 items-center gap-2">
            <AppWindow className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate">{label}</span>
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[var(--radix-popover-trigger-width)] min-w-64 p-0"
      >
        <Command>
          <CommandInput placeholder="搜索应用" />
          <CommandList>
            <CommandEmpty>暂无匹配应用</CommandEmpty>
            <CommandGroup>
              {allowAll && !multiple && (
                <CommandItem
                  value="__all__"
                  onSelect={() => {
                    onChange([]);
                    setOpen(false);
                  }}
                >
                  <Check
                    className={cn(
                      "size-4",
                      value.length === 0 ? "opacity-100" : "opacity-0",
                    )}
                  />
                  {allLabel}
                </CommandItem>
              )}
              {apps.map((app) => (
                <CommandItem
                  key={app.id}
                  value={`${app.name} ${app.appKey} ${app.id}`}
                  onSelect={() => toggle(app.id)}
                >
                  <Check
                    className={cn(
                      "size-4",
                      value.includes(app.id) ? "opacity-100" : "opacity-0",
                    )}
                  />
                  <span className="min-w-0 flex-1 truncate">{app.name}</span>
                  <span className="font-mono text-[10px] text-muted-foreground">
                    #{app.id}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
        {multiple && value.length > 0 && (
          <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
            已选 {value.length} 个
            <button
              type="button"
              className="hover:text-foreground"
              onClick={() => onChange([])}
            >
              清空
            </button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

export function AppChips({
  ids,
  onRemove,
}: {
  ids: number[];
  onRemove?: (id: number) => void;
}) {
  const apps = useAppOptions();
  const byId = new Map(apps.map((app) => [app.id, app]));
  return (
    <div className="flex flex-wrap gap-1.5">
      {ids.map((id) => (
        <span
          key={id}
          className="inline-flex h-7 items-center gap-1.5 rounded-lg border bg-muted/40 px-2 text-xs"
        >
          <AppWindow className="size-3.5 text-muted-foreground" />
          {byId.get(id)?.name ?? `应用 #${id}`}
          {onRemove && (
            <button
              type="button"
              aria-label="移除"
              onClick={() => onRemove(id)}
              className="text-muted-foreground hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          )}
        </span>
      ))}
    </div>
  );
}

export function AdminMultiPicker({
  admins,
  value,
  onChange,
  disabledReason,
  trigger,
}: {
  admins: AdminListEntry[];
  value: number[];
  onChange: (next: number[]) => void;
  /** 返回不可选的原因（已是成员、超管不需要角色……） */
  disabledReason?: (entry: AdminListEntry) => string | undefined;
  trigger?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        {trigger ?? (
          <Button
            type="button"
            variant="outline"
            className="h-9 w-full justify-between font-normal"
          >
            {value.length ? `已选 ${value.length} 位管理员` : "选择管理员"}
            <ChevronsUpDown className="size-3.5 opacity-50" />
          </Button>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[var(--radix-popover-trigger-width)] min-w-80 p-0"
      >
        <Command>
          <CommandInput placeholder="搜索账号或名称" />
          <CommandList className="max-h-80">
            <CommandEmpty>暂无匹配管理员</CommandEmpty>
            <CommandGroup>
              {admins.map((entry) => {
                const id = entry.account.id;
                const reason = disabledReason?.(entry);
                const checked = value.includes(id);
                const tier: AdminTier = tierOf(entry.account);
                return (
                  <CommandItem
                    key={id}
                    disabled={Boolean(reason)}
                    value={`${entry.account.displayName} ${entry.account.account} ${id}`}
                    onSelect={() =>
                      onChange(
                        checked
                          ? value.filter((item) => item !== id)
                          : [...value, id],
                      )
                    }
                    className="gap-2"
                  >
                    <Check
                      className={cn(
                        "size-4 shrink-0",
                        checked ? "opacity-100" : "opacity-0",
                      )}
                    />
                    <div className="min-w-0 flex-1">
                      <AdminIdentity
                        name={entry.account.displayName}
                        account={entry.account.account}
                        avatar={entry.account.avatar}
                        size="sm"
                      />
                    </div>
                    {reason ? (
                      <span className="shrink-0 text-[10px] text-muted-foreground">
                        {reason}
                      </span>
                    ) : (
                      tier !== "admin" && <TierBadge tier={tier} size="sm" />
                    )}
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
