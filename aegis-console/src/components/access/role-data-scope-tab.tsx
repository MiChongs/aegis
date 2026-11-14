"use client";

import { useState } from "react";
import {
  AppWindow,
  Boxes,
  Check,
  Globe2,
  Link2,
  Save,
  UserRoundPen,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useUpdateRoleMutation } from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import {
  DATA_SCOPE_LABEL,
  type AccessRole,
  type DataScope,
} from "@/lib/api/access";
import { cn } from "@/lib/utils";
import { Pane } from "./access-kit";
import { AppChips, AppPicker } from "./pickers";

/**
 * 角色「数据权限」页签（对标若依的数据权限范围）。
 *
 * 只对应用级自定义角色有意义：平台级角色天然覆盖全部应用；内置应用级角色固定为「按授权绑定」。
 */

const OPTIONS: Array<{ value: DataScope; icon: typeof Globe2; hint: string }> =
  [
    { value: "assigned", icon: Link2, hint: "授权时为每个人选定应用" },
    { value: "all", icon: Globe2, hint: "在每个应用里生效，不含平台级" },
    {
      value: "created",
      icon: UserRoundPen,
      hint: "只在持有人自己创建的应用里生效",
    },
    { value: "custom", icon: Boxes, hint: "只在下方指定的应用里生效" },
  ];

export function RoleDataScopeTab({
  role,
  canEdit,
}: {
  role: AccessRole;
  canEdit: boolean;
}) {
  const [scope, setScope] = useState<DataScope>(role.dataScope ?? "assigned");
  const [apps, setApps] = useState<number[]>(role.dataAppIds ?? []);
  const mutation = useUpdateRoleMutation();
  const editable = canEdit && role.isCustom && role.scope === "app";
  const dirty =
    scope !== role.dataScope ||
    apps.join(",") !== (role.dataAppIds ?? []).join(",");

  const save = async () => {
    if (scope === "custom" && apps.length === 0) {
      toast.error("至少选择一个应用");
      return;
    }
    try {
      await mutation.mutateAsync({
        roleKey: role.key,
        name: role.name,
        description: role.description,
        level: role.level,
        permissions: role.allow,
        deny: role.deny,
        status: role.status,
        sortOrder: role.sortOrder,
        dataScope: scope,
        dataAppIds: scope === "custom" ? apps : [],
      });
      toast.success("数据权限已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  if (role.scope === "global") {
    return (
      <Pane>
        <div className="flex items-center gap-3 p-5 text-sm">
          <Globe2 className="size-5 text-violet-500" />
          <div>
            <div className="font-medium">全部应用与平台</div>
            <div className="text-xs text-muted-foreground">
              平台级角色不受数据权限约束
            </div>
          </div>
        </div>
      </Pane>
    );
  }

  return (
    <Pane
      aside={
        editable && (
          <Button
            size="sm"
            className="h-8 gap-1.5"
            disabled={!dirty || mutation.isPending}
            onClick={save}
          >
            <Save className="size-3.5" />
            保存
          </Button>
        )
      }
      title="数据范围"
      icon={AppWindow}
    >
      <div className="grid gap-3 p-4 sm:grid-cols-2">
        {OPTIONS.map((option) => {
          const Icon = option.icon;
          const active = scope === option.value;
          const disabled = !editable && !active;
          return (
            <button
              key={option.value}
              type="button"
              disabled={!editable}
              onClick={() => setScope(option.value)}
              className={cn(
                "relative flex items-start gap-3 rounded-xl border p-4 text-left transition-colors",
                active
                  ? "border-primary/50 bg-primary/6 ring-1 ring-primary/30"
                  : "hover:bg-accent/50",
                disabled && "opacity-50",
                !editable && "cursor-default",
              )}
            >
              <span
                className={cn(
                  "grid size-9 shrink-0 place-items-center rounded-lg",
                  active
                    ? "bg-primary text-primary-foreground"
                    : "bg-muted text-muted-foreground",
                )}
              >
                <Icon className="size-4" />
              </span>
              <span className="min-w-0">
                <span className="block text-sm font-medium">
                  {DATA_SCOPE_LABEL[option.value]}
                </span>
                <span className="mt-0.5 block text-xs text-muted-foreground">
                  {option.hint}
                </span>
              </span>
              {active && (
                <Check className="absolute top-3 right-3 size-4 text-primary" />
              )}
            </button>
          );
        })}
      </div>
      {scope === "custom" && (
        <div className="space-y-3 border-t border-border/70 p-4">
          {editable && (
            <AppPicker
              multiple
              value={apps}
              onChange={setApps}
              placeholder="选择应用"
              className="max-w-md"
            />
          )}
          {apps.length > 0 ? (
            <AppChips
              ids={apps}
              onRemove={
                editable
                  ? (id) => setApps(apps.filter((item) => item !== id))
                  : undefined
              }
            />
          ) : (
            <div className="text-xs text-muted-foreground">暂无应用</div>
          )}
        </div>
      )}
      {!role.isCustom && (
        <div className="border-t border-border/70 px-4 py-3 text-xs text-muted-foreground">
          内置角色固定按授权绑定的应用生效
        </div>
      )}
    </Pane>
  );
}
