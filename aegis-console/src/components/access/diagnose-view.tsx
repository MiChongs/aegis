"use client";

import { useMemo, useState } from "react";
import {
  CheckCircle2,
  ChevronsUpDown,
  Grid3x3,
  ScanSearch,
  ShieldX,
  Stethoscope,
  Timer,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  useAccessAdminsQuery,
  useAccessRolesQuery,
  useExplainMutation,
  usePermissionCatalogQuery,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import {
  tierOf,
  type AccessRole,
  type AuthorizationExplain,
} from "@/lib/api/access";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import {
  AdminIdentity,
  CodeText,
  Pane,
  TierBadge,
  NodeIcon,
} from "./access-kit";
import { AppPicker } from "./pickers";
import { PermissionPicker } from "./temp-permission-form";
import { buildTreeIndex, permissionLabel, type TreeIndex } from "./tree-model";

export function DiagnoseView() {
  const catalogQuery = usePermissionCatalogQuery();
  const index = useMemo(
    () => buildTreeIndex(catalogQuery.data),
    [catalogQuery.data],
  );
  const can = usePermissionChecker();
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {can("system:admin:read") && <ExplainPanel index={index} />}
      <CoverageHeatmap index={index} loading={catalogQuery.isLoading} />
    </div>
  );
}

// ── 判定解释 ──

function ExplainPanel({ index }: { index: TreeIndex }) {
  const adminsQuery = useAccessAdminsQuery();
  const explain = useExplainMutation();
  const [adminId, setAdminId] = useState<number | null>(null);
  const [permission, setPermission] = useState<string | null>(null);
  const [app, setApp] = useState<number[]>([]);
  const [result, setResult] = useState<AuthorizationExplain | null>(null);
  const [open, setOpen] = useState(false);
  const admins = adminsQuery.data ?? [];
  const admin = admins.find((entry) => entry.account.id === adminId);

  const run = async () => {
    if (!adminId || !permission) return;
    try {
      setResult(
        await explain.mutateAsync({
          adminId,
          permission,
          appid: app[0] ?? null,
        }),
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "判定失败");
    }
  };

  return (
    <Pane title="权限判定" icon={Stethoscope}>
      <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,0.8fr)_auto] lg:items-end">
        <div className="space-y-1.5">
          <Label>管理员</Label>
          <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
              <Button
                variant="outline"
                className="h-9 w-full justify-between font-normal"
              >
                {admin ? (
                  <span className="truncate">
                    {admin.account.displayName || admin.account.account}
                  </span>
                ) : (
                  <span className="text-muted-foreground">选择管理员</span>
                )}
                <ChevronsUpDown className="size-3.5 opacity-50" />
              </Button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-80 p-0">
              <Command>
                <CommandInput placeholder="搜索账号或名称" />
                <CommandList>
                  <CommandEmpty>暂无匹配</CommandEmpty>
                  <CommandGroup>
                    {admins.map((entry) => (
                      <CommandItem
                        key={entry.account.id}
                        value={`${entry.account.displayName} ${entry.account.account}`}
                        onSelect={() => {
                          setAdminId(entry.account.id);
                          setOpen(false);
                          setResult(null);
                        }}
                      >
                        <AdminIdentity
                          name={entry.account.displayName}
                          account={entry.account.account}
                          avatar={entry.account.avatar}
                          size="sm"
                        />
                        {tierOf(entry.account) !== "admin" && (
                          <TierBadge tier={tierOf(entry.account)} size="sm" />
                        )}
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>
        </div>
        <div className="space-y-1.5">
          <Label>权限点</Label>
          <PermissionPicker
            index={index}
            value={permission}
            onChange={(code) => {
              setPermission(code);
              setResult(null);
            }}
          />
        </div>
        <div className="space-y-1.5">
          <Label>作用域</Label>
          <AppPicker
            value={app}
            onChange={(ids) => {
              setApp(ids);
              setResult(null);
            }}
            allowAll
            allLabel="平台级"
          />
        </div>
        <Button
          className="h-9 gap-1.5"
          disabled={!adminId || !permission || explain.isPending}
          onClick={run}
        >
          <ScanSearch className="size-4" />
          判定
        </Button>
      </div>
      {result && <ExplainResult result={result} index={index} />}
    </Pane>
  );
}

function ExplainResult({
  result,
  index,
}: {
  result: AuthorizationExplain;
  index: TreeIndex;
}) {
  const allowed = result.allowed;
  const denied = result.decision?.effect === "deny";
  return (
    <div
      className={cn(
        "flex flex-wrap items-start gap-4 border-t px-4 py-4",
        allowed ? "bg-emerald-500/5" : "bg-rose-500/5",
      )}
    >
      <span
        className={cn(
          "grid size-11 shrink-0 place-items-center rounded-xl",
          allowed
            ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
            : "bg-rose-500/15 text-rose-600 dark:text-rose-400",
        )}
      >
        {allowed ? (
          <CheckCircle2 className="size-6" />
        ) : (
          <ShieldX className="size-6" />
        )}
      </span>
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={cn(
              "text-base font-semibold",
              allowed
                ? "text-emerald-700 dark:text-emerald-300"
                : "text-rose-700 dark:text-rose-300",
            )}
          >
            {allowed ? "放行" : denied ? "被禁止" : "拒绝"}
          </span>
          <span className="text-sm">
            {permissionLabel(index, result.permission)}
          </span>
          <CodeText>{result.domain}</CodeText>
          {result.tempPermission && (
            <span className="inline-flex items-center gap-1 rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] text-amber-700 dark:text-amber-300">
              <Timer className="size-3" />
              临时权限
            </span>
          )}
        </div>
        <p className="text-sm text-muted-foreground">{result.summary}</p>
        {result.subjects?.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[11px] text-muted-foreground">参与判定</span>
            {result.subjects.map((subject) => (
              <span
                key={subject}
                className={cn(
                  "rounded-md border px-1.5 py-0.5 font-mono text-[11px]",
                  subject === result.decision?.subject &&
                    (allowed
                      ? "border-emerald-500/40 bg-emerald-500/10"
                      : "border-rose-500/40 bg-rose-500/10"),
                )}
              >
                {subject}
              </span>
            ))}
          </div>
        )}
        {result.decision?.rule && result.decision.rule.length > 0 && (
          <div className="text-[11px] text-muted-foreground">
            命中策略{" "}
            <CodeText className="text-foreground">
              {result.decision.rule.join(", ")}
            </CodeText>
          </div>
        )}
      </div>
    </div>
  );
}

// ── 覆盖热力图 ──

function CoverageHeatmap({
  index,
  loading,
}: {
  index: TreeIndex;
  loading: boolean;
}) {
  const can = usePermissionChecker();
  const rolesQuery = useAccessRolesQuery(can("system:role:read"));
  const roles = useMemo(
    () => (rolesQuery.data ?? []).filter((role) => role.key !== "super_admin"),
    [rolesQuery.data],
  );
  const sets = useMemo(
    () => new Map(roles.map((role) => [role.key, new Set(role.permissions)])),
    [roles],
  );

  const rows = useMemo(() => {
    const out: Array<{
      key: string;
      name: string;
      dir: string;
      icon?: string;
      leaves: string[];
      first: boolean;
    }> = [];
    for (const dir of index.roots) {
      let first = true;
      for (const menu of dir.children ?? []) {
        const entry = index.entries.get(menu.key)!;
        out.push({
          key: menu.key,
          name: menu.name,
          dir: dir.name,
          icon: dir.icon,
          leaves: entry.leaves,
          first,
        });
        first = false;
      }
    }
    return out;
  }, [index]);

  if (loading || rolesQuery.isLoading)
    return <Skeleton className="h-96 rounded-2xl" />;
  if (!can("system:role:read")) return null;

  const cell = (role: AccessRole, leaves: string[]) => {
    const set = sets.get(role.key)!;
    const held = leaves.filter((code) => set.has(code)).length;
    return { held, pct: leaves.length ? held / leaves.length : 0 };
  };

  return (
    <Pane
      title="角色覆盖"
      icon={Grid3x3}
      aside={
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          0%
          {[0.08, 0.3, 0.55, 0.8, 1].map((alpha) => (
            <span
              key={alpha}
              className="size-3 rounded-[3px] bg-primary"
              style={{ opacity: alpha }}
            />
          ))}
          100%
        </div>
      }
    >
      <div className="overflow-x-auto">
        <table className="w-full min-w-max border-separate border-spacing-0 text-xs">
          <thead>
            <tr>
              <th className="sticky left-0 z-10 bg-card px-4 py-2 text-left font-medium text-muted-foreground">
                菜单
              </th>
              {roles.map((role) => (
                <th
                  key={role.key}
                  className="px-1 py-2 align-bottom font-medium"
                >
                  <div
                    className="mx-auto w-16 truncate text-center"
                    title={role.name}
                  >
                    {role.name}
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const iconName = row.icon;
              return (
                <tr key={row.key}>
                  <td
                    className={cn(
                      "sticky left-0 z-10 bg-card px-4 py-0.5",
                      row.first && "border-t border-border/70 pt-2",
                    )}
                  >
                    <div className="flex w-52 items-center gap-2">
                      {row.first ? (
                        <NodeIcon
                          name={iconName}
                          className="size-3.5 shrink-0 text-primary"
                        />
                      ) : (
                        <span className="size-3.5 shrink-0" />
                      )}
                      <span className="truncate">{row.name}</span>
                      {row.first && (
                        <span className="truncate text-[10px] text-muted-foreground">
                          {row.dir}
                        </span>
                      )}
                    </div>
                  </td>
                  {roles.map((role) => {
                    const { held, pct } = cell(role, row.leaves);
                    return (
                      <td
                        key={role.key}
                        className={cn(
                          "px-1 py-0.5",
                          row.first && "border-t border-border/70 pt-2",
                        )}
                      >
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="mx-auto h-5 w-16 rounded-[4px] bg-muted">
                              {held > 0 && (
                                <div
                                  className="h-full rounded-[4px] bg-primary"
                                  style={{ opacity: 0.12 + pct * 0.88 }}
                                />
                              )}
                            </div>
                          </TooltipTrigger>
                          <TooltipContent>
                            {role.name}：{row.name} {held}/{row.leaves.length}
                          </TooltipContent>
                        </Tooltip>
                      </td>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Pane>
  );
}
