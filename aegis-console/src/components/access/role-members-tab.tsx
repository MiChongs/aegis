"use client";

import { useMemo, useState } from "react";
import { AppWindow, Search, UserMinus, UserPlus, Users2 } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useAccessAdminsQuery,
  useAddRoleMembersMutation,
  useRemoveRoleMemberMutation,
} from "@/lib/access-hooks";
import { ApiError } from "@/lib/api/client";
import { tierOf, type AccessRole, type RoleMember } from "@/lib/api/access";
import { formatDateTime } from "@/lib/datetime";
import { usePermissionChecker } from "@/lib/permissions";
import { AdminIdentity, Pane, TierBadge } from "./access-kit";
import { AdminMultiPicker, AppPicker } from "./pickers";

/** 角色「成员」页签：谁持有这个角色、绑在哪个应用上；可批量添加与逐条移除。 */
export function RoleMembersTab({
  role,
  members,
  loading,
}: {
  role: AccessRole;
  members: RoleMember[];
  loading: boolean;
}) {
  const can = usePermissionChecker();
  const canManage =
    can("system:role:edit") &&
    can("system:admin:read") &&
    role.key !== "super_admin";
  const [keyword, setKeyword] = useState("");
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<RoleMember | null>(null);
  const removeMutation = useRemoveRoleMemberMutation();

  const filtered = useMemo(() => {
    const k = keyword.trim().toLowerCase();
    if (!k) return members;
    return members.filter(
      (m) =>
        m.account.toLowerCase().includes(k) ||
        m.displayName.toLowerCase().includes(k) ||
        (m.appName ?? "").toLowerCase().includes(k),
    );
  }, [members, keyword]);

  const people = new Set(members.map((m) => m.adminId)).size;

  const confirmRemove = async () => {
    if (!removing) return;
    try {
      await removeMutation.mutateAsync({
        roleKey: role.key,
        adminId: removing.adminId,
        appid: removing.appid ?? null,
      });
      toast.success(
        `已将 ${removing.displayName || removing.account} 移出「${role.name}」`,
      );
      setRemoving(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "移除失败");
    }
  };

  return (
    <Pane
      title={
        <span className="flex items-center gap-2">
          成员
          <span className="text-xs font-normal text-muted-foreground tabular-nums">
            {people} 人
            {members.length !== people && `，${members.length} 条授权`}
          </span>
        </span>
      }
      icon={Users2}
      aside={
        <>
          <div className="relative hidden sm:block">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索成员"
              className="h-8 w-44 pl-8 text-xs"
            />
          </div>
          {canManage && (
            <Button
              size="sm"
              className="h-8 gap-1.5"
              onClick={() => setAdding(true)}
            >
              <UserPlus className="size-3.5" />
              添加成员
            </Button>
          )}
        </>
      }
    >
      <div className="divide-y divide-border/60">
        {loading &&
          Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="flex items-center gap-3 px-4 py-3">
              <Skeleton className="size-8 rounded-full" />
              <Skeleton className="h-3 w-40" />
            </div>
          ))}
        {!loading && filtered.length === 0 && (
          <div className="py-12 text-center text-sm text-muted-foreground">
            暂无成员
          </div>
        )}
        {filtered.map((member) => (
          <div
            key={`${member.adminId}-${member.appid ?? 0}`}
            className="flex flex-wrap items-center gap-3 px-4 py-2.5 transition-colors hover:bg-accent/30"
          >
            <div className="min-w-48 flex-1">
              <AdminIdentity
                name={member.displayName}
                account={member.account}
                avatar={member.avatar}
                tier={member.tier}
                muted={member.status === "disabled"}
                trailing={
                  member.status === "disabled" && (
                    <span className="rounded bg-muted px-1 text-[10px] text-muted-foreground">
                      已停用
                    </span>
                  )
                }
              />
            </div>
            {member.tier !== "admin" && (
              <TierBadge tier={member.tier} size="sm" />
            )}
            <div className="flex w-44 min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
              <AppWindow className="size-3.5 shrink-0" />
              <span className="truncate">
                {member.appid
                  ? member.appName || `应用 #${member.appid}`
                  : role.scope === "global"
                    ? "平台级"
                    : "按数据权限"}
              </span>
            </div>
            <div className="hidden w-36 text-xs text-muted-foreground tabular-nums md:block">
              {formatDateTime(member.grantedAt)}
            </div>
            {canManage && (
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-muted-foreground hover:text-rose-600"
                aria-label="移出角色"
                onClick={() => setRemoving(member)}
              >
                <UserMinus className="size-4" />
              </Button>
            )}
          </div>
        ))}
      </div>

      {adding && (
        <AddMembersDialog
          role={role}
          members={members}
          onClose={() => setAdding(false)}
        />
      )}

      <AlertDialog
        open={Boolean(removing)}
        onOpenChange={(open) => !open && setRemoving(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移出角色</AlertDialogTitle>
            <AlertDialogDescription>
              {removing?.displayName || removing?.account} 将失去「{role.name}」
              {removing?.appName ? `在 ${removing.appName} ` : ""}的全部权限。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={confirmRemove}
              disabled={removeMutation.isPending}
            >
              移出
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Pane>
  );
}

function AddMembersDialog({
  role,
  members,
  onClose,
}: {
  role: AccessRole;
  members: RoleMember[];
  onClose: () => void;
}) {
  const adminsQuery = useAccessAdminsQuery();
  const mutation = useAddRoleMembersMutation();
  const [picked, setPicked] = useState<number[]>([]);
  const [app, setApp] = useState<number[]>([]);
  const needsApp = role.scope === "app" && role.dataScope === "assigned";
  const appId = app[0] ?? null;

  const existing = useMemo(() => {
    const set = new Set<number>();
    for (const m of members)
      if (!needsApp || m.appid === appId) set.add(m.adminId);
    return set;
  }, [members, needsApp, appId]);

  const submit = async () => {
    try {
      const result = await mutation.mutateAsync({
        roleKey: role.key,
        adminIds: picked,
        appid: needsApp ? appId : null,
      });
      toast.success(`已添加 ${result.added} 位成员`);
      onClose();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "添加失败");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>添加成员到「{role.name}」</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          {needsApp && (
            <div className="space-y-1.5">
              <Label>生效应用</Label>
              <AppPicker value={app} onChange={setApp} placeholder="选择应用" />
            </div>
          )}
          <div className="space-y-1.5">
            <Label>管理员</Label>
            <AdminMultiPicker
              admins={adminsQuery.data ?? []}
              value={picked}
              onChange={setPicked}
              disabledReason={(entry) => {
                if (tierOf(entry.account) !== "admin") return "无需角色";
                if (existing.has(entry.account.id)) return "已是成员";
                if (!entry.capabilities.canAccess) return "无权调整";
                return undefined;
              }}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button
            onClick={submit}
            disabled={
              picked.length === 0 || (needsApp && !appId) || mutation.isPending
            }
          >
            添加 {picked.length > 0 && picked.length}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
