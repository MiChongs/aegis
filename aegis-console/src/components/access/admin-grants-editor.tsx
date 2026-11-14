"use client";

import { useMemo, useState } from "react";
import { Lock, Save, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useSetAdminGrantsMutation } from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { tierOf, type AdminDetail, type AdminGrant } from "@/lib/api/access";
import { cn } from "@/lib/utils";
import { Pane } from "./access-kit";
import {
  PermissionTreeEditor,
  type GrantState,
} from "./permission-tree-editor";
import { AppPicker, useAppOptions } from "./pickers";
import { expandPatterns, type TreeIndex } from "./tree-model";

const ALL = "all";

/**
 * 个人授权：不经过角色，直接授予或禁止到这一个人（金蝶的「用户权限」）。
 *
 * 作用域分「不限」（对所有应用与平台生效）与「指定应用」两类，各自一棵树；
 * 禁止压过他从任何角色得到的允许。保存时整组替换。
 */
export function AdminGrantsEditor({
  detail,
  index,
}: {
  detail: AdminDetail;
  index: TreeIndex;
}) {
  const mutation = useSetAdminGrantsMutation();
  const apps = useAppOptions();
  const caps = detail.capabilities;
  const superTier = tierOf(detail.account) !== "admin";

  const initial = useMemo(() => {
    const map = new Map<string, GrantState>();
    for (const grant of detail.grants) {
      const key = grant.appid ? String(grant.appid) : ALL;
      const state = map.get(key) ?? new Map();
      for (const code of expandPatterns(index, [grant.permission]))
        state.set(code, grant.effect);
      map.set(key, state);
    }
    if (!map.has(ALL)) map.set(ALL, new Map());
    return map;
  }, [detail.grants, index]);

  const [draft, setDraft] = useState(initial);
  const [scope, setScope] = useState<string>(ALL);
  const scopes = [...draft.keys()];

  const serialize = (map: Map<string, GrantState>) =>
    JSON.stringify(
      [...map.entries()]
        .flatMap(([key, state]) =>
          [...state.entries()].map(
            ([code, effect]) => `${key}|${code}|${effect}`,
          ),
        )
        .sort(),
    );
  const dirty = serialize(draft) !== serialize(initial);

  const save = async () => {
    const grants: AdminGrant[] = [];
    for (const [key, state] of draft) {
      for (const [permission, effect] of state) {
        grants.push({
          permission,
          effect,
          appid: key === ALL ? null : Number(key),
        });
      }
    }
    try {
      await mutation.mutateAsync({ adminId: detail.account.id, grants });
      toast.success("个人授权已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  const appName = (key: string) =>
    apps.find((app) => String(app.id) === key)?.name ?? `应用 #${key}`;
  const count = (key: string) => draft.get(key)?.size ?? 0;
  const readOnly = !caps.canGrant;

  if (superTier) {
    return (
      <Pane>
        <div className="flex items-center gap-2 p-5 text-sm text-muted-foreground">
          <Lock className="size-4" />
          超级管理员与系统管理员不受权限点约束
        </div>
      </Pane>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {readOnly && (
        <div className="flex items-center gap-2 rounded-xl border bg-muted/40 px-3.5 py-2.5 text-xs text-muted-foreground">
          <Lock className="size-3.5" />
          {caps.manageReason ?? "缺少「管理员：个人授权」权限"}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {scopes.map((key) => (
          <button
            key={key}
            type="button"
            onClick={() => setScope(key)}
            className={cn(
              "inline-flex h-8 items-center gap-1.5 rounded-lg border px-3 text-xs transition-colors",
              scope === key
                ? "border-primary/40 bg-primary/10 font-medium text-primary"
                : "text-muted-foreground hover:bg-accent hover:text-foreground",
            )}
          >
            {key === ALL ? "不限应用" : appName(key)}
            <span className="rounded bg-background/70 px-1 tabular-nums">
              {count(key)}
            </span>
          </button>
        ))}
        {!readOnly && (
          <div className="w-52">
            <AppPicker
              value={[]}
              placeholder="添加应用作用域"
              onChange={(ids) => {
                const id = ids[0];
                if (!id) return;
                const key = String(id);
                if (!draft.has(key)) {
                  const next = new Map(draft);
                  next.set(key, new Map());
                  setDraft(next);
                }
                setScope(key);
              }}
              className="h-8 text-xs"
            />
          </div>
        )}
      </div>

      <Pane bodyClassName="flex flex-col">
        <PermissionTreeEditor
          key={scope}
          index={index}
          mode={readOnly ? "view" : "edit"}
          value={draft.get(scope) ?? new Map()}
          onChange={(state) => {
            const next = new Map(draft);
            next.set(scope, state);
            setDraft(next);
          }}
          roleScope={scope === ALL ? "any" : "app"}
          heightClassName="max-h-[calc(100dvh-26rem)] min-h-72"
        />
        {!readOnly && (
          <div className="flex items-center gap-2 border-t border-border/70 px-4 py-3">
            <span className="text-xs text-muted-foreground">
              共{" "}
              {[...draft.values()].reduce((sum, state) => sum + state.size, 0)}{" "}
              项
            </span>
            <Button
              variant="ghost"
              size="sm"
              className="ml-auto h-8 gap-1.5"
              disabled={!dirty || mutation.isPending}
              onClick={() => setDraft(initial)}
            >
              <Undo2 className="size-3.5" />
              撤销
            </Button>
            <Button
              size="sm"
              className="h-8 gap-1.5"
              disabled={!dirty || mutation.isPending}
              onClick={save}
            >
              <Save className="size-3.5" />
              保存
            </Button>
          </div>
        )}
      </Pane>
    </div>
  );
}
