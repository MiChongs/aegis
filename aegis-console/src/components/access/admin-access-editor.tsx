"use client";

import { useMemo, useState } from "react";
import { Check, KeyRound, Lock, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  useAccessRolesQuery,
  useUpdateAdminAccessMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import {
  DATA_SCOPE_LABEL,
  TIER_LABEL,
  tierOf,
  type AdminDetail,
  type AdminTier,
} from "@/lib/api/access";
import { cn } from "@/lib/utils";
import { Pane, TIER_TONE } from "./access-kit";
import { AppPicker } from "./pickers";

type Row = { id: number; roleKey: string; appid: number | null };

let rowSeq = 0;
const nextId = () => ++rowSeq;

/**
 * 角色授权：身份档位 + 角色分配。
 *
 * 档位只有系统管理员能调（后端同样校验）；超管与系统管理员不走权限点判定，选了它们角色列表就收起。
 * 「按授权绑定」的应用级角色每行要选应用；其余数据权限的角色由角色自己决定范围，这里不选应用。
 */
export function AdminAccessEditor({ detail }: { detail: AdminDetail }) {
  const rolesQuery = useAccessRolesQuery();
  const mutation = useUpdateAdminAccessMutation();
  const roles = useMemo(
    () => (rolesQuery.data ?? []).filter((role) => role.key !== "super_admin"),
    [rolesQuery.data],
  );
  const roleMap = useMemo(
    () => new Map(roles.map((role) => [role.key, role])),
    [roles],
  );
  const caps = detail.capabilities;
  const currentTier = tierOf(detail.account);

  const [tier, setTier] = useState<AdminTier>(currentTier);
  const [rows, setRows] = useState<Row[]>(() =>
    (detail.assignments ?? []).map((a) => ({
      id: nextId(),
      roleKey: a.roleKey ?? "",
      appid: a.appid ?? null,
    })),
  );

  const editable = caps.canAccess || caps.canTier;
  const needsApp = (roleKey: string) => {
    const role = roleMap.get(roleKey);
    return (
      role?.scope === "app" && (role.dataScope ?? "assigned") === "assigned"
    );
  };
  const invalid =
    tier === "admin" &&
    rows.some((row) => !row.roleKey || (needsApp(row.roleKey) && !row.appid));

  const original = JSON.stringify({
    tier: currentTier,
    rows: (detail.assignments ?? [])
      .map((a) => `${a.roleKey}:${a.appid ?? 0}`)
      .sort(),
  });
  const dirty =
    JSON.stringify({
      tier,
      rows: rows
        .map((r) => `${r.roleKey}:${needsApp(r.roleKey) ? (r.appid ?? 0) : 0}`)
        .sort(),
    }) !== original;

  const save = async () => {
    try {
      await mutation.mutateAsync({
        adminId: detail.account.id,
        tier,
        assignments:
          tier === "admin"
            ? rows
                .filter((row) => row.roleKey)
                .map((row) => ({
                  roleKey: row.roleKey,
                  appid: needsApp(row.roleKey) ? row.appid : null,
                }))
            : [],
      });
      toast.success("授权已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  const tiers: AdminTier[] = ["admin", "super", "system"];

  return (
    <div className="flex flex-col gap-4">
      <Pane
        title="身份"
        aside={
          !caps.canTier && (
            <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
              <Lock className="size-3" />
              仅系统管理员可调整
            </span>
          )
        }
      >
        <div className="grid gap-3 p-4 sm:grid-cols-3">
          {tiers.map((value) => {
            const tone = TIER_TONE[value];
            const Icon = tone.icon;
            const active = tier === value;
            const disabled = !caps.canTier && value !== tier;
            return (
              <button
                key={value}
                type="button"
                disabled={disabled}
                onClick={() => setTier(value)}
                className={cn(
                  "relative flex flex-col items-start gap-2 rounded-xl border p-3.5 text-left transition-colors",
                  active
                    ? "border-primary/50 bg-primary/5 ring-1 ring-primary/30"
                    : "hover:bg-accent/50",
                  disabled && "cursor-not-allowed opacity-45",
                )}
              >
                <span
                  className={cn(
                    "grid size-8 place-items-center rounded-lg",
                    tone.plate,
                  )}
                >
                  <Icon className="size-4" />
                </span>
                <span className="text-sm font-medium">{TIER_LABEL[value]}</span>
                {active && (
                  <Check className="absolute top-3 right-3 size-4 text-primary" />
                )}
              </button>
            );
          })}
        </div>
      </Pane>

      {tier === "admin" && (
        <Pane
          title="角色"
          icon={KeyRound}
          aside={
            editable && (
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1.5"
                onClick={() =>
                  setRows([...rows, { id: nextId(), roleKey: "", appid: null }])
                }
              >
                <Plus className="size-3.5" />
                添加角色
              </Button>
            )
          }
        >
          <div className="divide-y divide-border/60">
            {rows.length === 0 && (
              <div className="py-8 text-center text-sm text-muted-foreground">
                暂无角色
              </div>
            )}
            {rows.map((row) => {
              const role = roleMap.get(row.roleKey);
              return (
                <div
                  key={row.id}
                  className="grid items-center gap-2 px-4 py-2.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem]"
                >
                  <Select
                    value={row.roleKey || undefined}
                    disabled={!caps.canAccess}
                    onValueChange={(roleKey) =>
                      setRows(
                        rows.map((item) =>
                          item.id === row.id ? { ...item, roleKey } : item,
                        ),
                      )
                    }
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="选择角色" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectLabel>平台级</SelectLabel>
                        {roles
                          .filter((r) => r.scope === "global")
                          .map((r) => (
                            <SelectItem
                              key={r.key}
                              value={r.key}
                              disabled={r.status === "disabled"}
                            >
                              {r.name}
                            </SelectItem>
                          ))}
                      </SelectGroup>
                      <SelectGroup>
                        <SelectLabel>应用级</SelectLabel>
                        {roles
                          .filter((r) => r.scope === "app")
                          .map((r) => (
                            <SelectItem
                              key={r.key}
                              value={r.key}
                              disabled={r.status === "disabled"}
                            >
                              {r.name}
                            </SelectItem>
                          ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  {role && needsApp(row.roleKey) ? (
                    <AppPicker
                      value={row.appid ? [row.appid] : []}
                      disabled={!caps.canAccess}
                      onChange={(ids) =>
                        setRows(
                          rows.map((item) =>
                            item.id === row.id
                              ? { ...item, appid: ids[0] ?? null }
                              : item,
                          ),
                        )
                      }
                    />
                  ) : (
                    <div className="flex h-9 items-center rounded-md border border-dashed px-3 text-xs text-muted-foreground">
                      {role
                        ? role.scope === "global"
                          ? "平台级，全部应用"
                          : DATA_SCOPE_LABEL[role.dataScope]
                        : "—"}
                    </div>
                  )}
                  {caps.canAccess && (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-8 text-muted-foreground hover:text-rose-600"
                      aria-label="移除"
                      onClick={() =>
                        setRows(rows.filter((item) => item.id !== row.id))
                      }
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        </Pane>
      )}

      {editable && (
        <div className="flex items-center justify-end gap-2">
          {tier !== currentTier && tier !== "admin" && (
            <span className="text-xs text-amber-600 dark:text-amber-400">
              保存后清除全部角色分配
            </span>
          )}
          <Button
            className="gap-1.5"
            disabled={!dirty || invalid || mutation.isPending}
            onClick={save}
          >
            <Save className="size-4" />
            {mutation.isPending ? "保存中" : "保存授权"}
          </Button>
        </div>
      )}
    </div>
  );
}
