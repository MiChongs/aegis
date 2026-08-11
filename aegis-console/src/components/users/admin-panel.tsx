"use client";

import { useCallback, useMemo, useState } from "react";
import { Search } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Input } from "@/components/ui/input";
import { LoadingState } from "@/components/ui/data-state";
import { useAdminAccountsQuery, useUpdateAdminAccountStatusMutation } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import type { AdminAccount, AdminLoginAvailability } from "@/lib/api/types";
import { DataTable } from "./data-table";
import { createAdminColumns, type AdminAction } from "./admin-columns";
import { AdminCreateDialog } from "./admin-create-dialog";
import { AdminEditDialog } from "./admin-edit-dialog";

// 后端返回 Profile[]（{ account: AdminAccount, assignments: [], loginAvailability? }），需要展平
// loginAvailability 仅当当前登录会话为超管时由后端附加
export type AdminWithAssignments = AdminAccount & {
  _assignments?: Array<{ roleKey?: string; appid?: number | null }>;
  _loginAvailability?: AdminLoginAvailability;
};

function flattenAdmins(data: unknown): AdminWithAssignments[] {
  if (!Array.isArray(data)) return [];
  return data.map((item) => {
    if (item && typeof item === "object" && "account" in item && typeof item.account === "object" && item.account !== null) {
      const profile = item as {
        account: AdminAccount;
        assignments?: Array<{ roleKey?: string; appid?: number | null }>;
        loginAvailability?: AdminLoginAvailability;
      };
      return {
        ...profile.account,
        _assignments: profile.assignments,
        _loginAvailability: profile.loginAvailability
      };
    }
    return item as AdminWithAssignments;
  });
}

export function AdminPanel() {
  const [keyword, setKeyword] = useState("");
  const [editAdmin, setEditAdmin] = useState<AdminWithAssignments | null>(null);
  const adminsQuery = useAdminAccountsQuery();
  const statusMutation = useUpdateAdminAccountStatusMutation();
  // 仅超管可以看到"账号可能无法登录"告警列 —— 与后端的权限门控一致，双重保护
  const isSuperAdmin = useAuthStore((s) => s.operator?.isSuperAdmin ?? false);

  const admins = useMemo(() => {
    const list = flattenAdmins(adminsQuery.data);
    if (!keyword.trim()) return list;
    const q = keyword.toLowerCase();
    return list.filter((a) =>
      (a.account?.toLowerCase().includes(q)) ||
      (a.displayName?.toLowerCase().includes(q)) ||
      (a.email?.toLowerCase().includes(q))
    );
  }, [adminsQuery.data, keyword]);

  const handleAction = useCallback(async (action: AdminAction, admin: AdminAccount) => {
    if (action === "toggle-status") {
      const next = admin.status === "disabled" ? "active" : "disabled";
      try {
        await statusMutation.mutateAsync({ adminId: admin.id, status: next });
        toast.success(next === "active" ? "已启用" : "已停用");
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : "操作失败");
      }
    } else if (action === "edit-access") {
      // 从 admins 列表中找到带 _assignments 的完整数据
      const full = admins.find((a) => a.id === admin.id);
      setEditAdmin(full || { ...admin, _assignments: [] });
    }
  }, [statusMutation, admins]);

  const columns = useMemo(
    () => createAdminColumns(handleAction, { showLoginAvailability: isSuperAdmin }),
    [handleAction, isSuperAdmin]
  );

  if (adminsQuery.isLoading) return <LoadingState title="加载中" />;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div className="relative w-64">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input placeholder="搜索管理员..." className="h-8 pl-8 text-xs" value={keyword} onChange={(e) => setKeyword(e.target.value)} />
        </div>
        <AdminCreateDialog />
      </div>
      <DataTable columns={columns} data={admins} emptyText="暂无管理员" />

      <AdminEditDialog
        open={editAdmin !== null}
        onOpenChange={(v) => { if (!v) setEditAdmin(null); }}
        admin={editAdmin ? {
          id: editAdmin.id,
          account: editAdmin.account,
          displayName: editAdmin.displayName,
          isSuperAdmin: !!editAdmin.isSuperAdmin,
          assignments: editAdmin._assignments
        } : null}
      />
    </div>
  );
}
