"use client";

import { useMemo, useState } from "react";
import { History, Info, Lock, Save, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useSetRolePermissionsMutation } from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import type { AccessRole, RoleDetail } from "@/lib/api/access";
import { CodeText, Pane } from "./access-kit";
import {
  PermissionTreeEditor,
  type GrantState,
} from "./permission-tree-editor";
import { expandPatterns, type TreeIndex } from "./tree-model";

/**
 * 角色「功能权限」页签。
 *
 * 自定义角色：允许 / 禁止直接写进角色定义。
 * 内置角色：代码定义锁定为已允许（升级会跟着变），这里的允许只记超出定义的部分，
 * 禁止压过定义 —— 两者都存在 override 里，启动重刷内置定义时不会被冲掉。
 * 仍在使用旧合并权限点（app:read 之类）的角色会展开成细分项显示，保存后落成细分代码。
 */
export function RolePermissionsTab({
  role,
  detail,
  index,
  base,
  canEdit,
}: {
  role: AccessRole;
  detail: RoleDetail;
  index: TreeIndex;
  base: Set<string>;
  canEdit: boolean;
}) {
  const initial = useMemo<GrantState>(() => {
    const state: GrantState = new Map();
    const allow = detail.builtin ? detail.overrideAllow : role.allow;
    const deny = detail.builtin ? detail.overrideDeny : role.deny;
    for (const code of expandPatterns(index, allow))
      if (!base.has(code)) state.set(code, "allow");
    for (const code of expandPatterns(index, deny)) state.set(code, "deny");
    return state;
  }, [detail, role.allow, role.deny, index, base]);

  const [draft, setDraft] = useState<GrantState>(initial);
  const mutation = useSetRolePermissionsMutation();

  const diff = useMemo(() => {
    let added = 0;
    let removed = 0;
    let denied = 0;
    const keys = new Set([...initial.keys(), ...draft.keys()]);
    for (const code of keys) {
      const before = initial.get(code);
      const after = draft.get(code);
      if (before === after) continue;
      if (after === "allow") added++;
      else if (after === "deny") denied++;
      else removed++;
    }
    return { added, removed, denied, dirty: added + removed + denied > 0 };
  }, [initial, draft]);

  const save = async () => {
    const allow = [...draft]
      .filter(([, v]) => v === "allow")
      .map(([code]) => code);
    const deny = [...draft]
      .filter(([, v]) => v === "deny")
      .map(([code]) => code);
    try {
      await mutation.mutateAsync({
        roleKey: role.key,
        allow: detail.builtin ? [...base, ...allow] : allow,
        deny,
      });
      toast.success(`已保存「${role.name}」的功能权限`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  };

  const readOnly = !canEdit;

  return (
    <div className="flex flex-col gap-3">
      {detail.legacy.length > 0 && (
        <div className="flex items-start gap-2.5 rounded-xl border border-amber-500/30 bg-amber-500/8 px-3.5 py-2.5 text-xs text-amber-800 dark:text-amber-200">
          <History className="mt-0.5 size-3.5 shrink-0" />
          <div className="min-w-0">
            使用旧版合并权限：
            {detail.legacy.map((code) => (
              <CodeText
                key={code}
                className="mx-1 text-amber-800 dark:text-amber-200"
              >
                {code}
              </CodeText>
            ))}
            {detail.builtin ? "（内置定义，随版本维护）" : "保存后换成细分权限"}
          </div>
        </div>
      )}
      {readOnly && (
        <div className="flex items-center gap-2 rounded-xl border bg-muted/40 px-3.5 py-2.5 text-xs text-muted-foreground">
          <Lock className="size-3.5" />
          {detail.reason ?? "只读"}
        </div>
      )}
      {detail.builtin && !readOnly && (
        <div className="flex items-center gap-2 rounded-xl border bg-muted/30 px-3.5 py-2.5 text-xs text-muted-foreground">
          <Info className="size-3.5" />
          内置定义已锁定，可额外允许或禁止
        </div>
      )}

      <Pane bodyClassName="flex flex-col">
        <PermissionTreeEditor
          index={index}
          mode={readOnly ? "view" : "edit"}
          value={readOnly ? withBase(initial, base) : draft}
          onChange={setDraft}
          locked={readOnly ? undefined : base}
          roleScope={role.scope === "app" ? "app" : "global"}
          heightClassName="max-h-[calc(100dvh-24rem)] min-h-80"
        />
        {!readOnly && (
          <div className="sticky bottom-0 flex flex-wrap items-center gap-3 border-t border-border/70 bg-card px-4 py-3">
            <div className="text-xs text-muted-foreground">
              {diff.dirty ? (
                <>
                  新增{" "}
                  <b className="text-foreground tabular-nums">{diff.added}</b>
                  <span className="mx-2">
                    移除{" "}
                    <b className="text-foreground tabular-nums">
                      {diff.removed}
                    </b>
                  </span>
                  禁止{" "}
                  <b className="text-rose-600 tabular-nums dark:text-rose-400">
                    {diff.denied}
                  </b>
                </>
              ) : (
                "未修改"
              )}
            </div>
            <div className="ml-auto flex items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                className="h-8 gap-1.5"
                disabled={!diff.dirty || mutation.isPending}
                onClick={() => setDraft(initial)}
              >
                <Undo2 className="size-3.5" />
                撤销
              </Button>
              <Button
                size="sm"
                className="h-8 gap-1.5"
                disabled={!diff.dirty || mutation.isPending}
                onClick={save}
              >
                <Save className="size-3.5" />
                {mutation.isPending ? "保存中" : "保存"}
              </Button>
            </div>
          </div>
        )}
      </Pane>
    </div>
  );
}

function withBase(state: GrantState, base: Set<string>): GrantState {
  const merged = new Map(state);
  for (const code of base) if (!merged.has(code)) merged.set(code, "allow");
  return merged;
}
