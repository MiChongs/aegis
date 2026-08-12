"use client";

import { useMemo } from "react";
import { useAdminProfileQuery } from "@/lib/admin-hooks";
import { avatarUrl, useAuthStore } from "@/lib/auth-store";

/**
 * 当前登录管理员的展示身份。
 *
 * 顶栏账户菜单、侧边栏底部用户块、移动端抽屉三处都要这一份，
 * 各自再算一遍必然漂移（同一个人在侧边栏叫「admin」、在菜单里叫「管理员」）。
 * React Query 会把 `admin-profile` 去重，多处调用不会多打请求。
 */
export type OperatorIdentity = {
  /** 昵称优先，退回账号 */
  name: string;
  /** 角色显示名：超管固定文案，否则取角色 key */
  role: string;
  /** 次要标识：邮箱优先，退回 `@账号` —— 顶栏只显示昵称，重名时靠它区分 */
  identity?: string;
  /** 头像地址（带破缓存版本号） */
  avatarSrc: string;
  /** 头像兜底字母 */
  initials: string;
  superAdmin: boolean;
};

type Operator = ReturnType<typeof useAuthStore.getState>["operator"];

function roleName(op: Operator) {
  if (!op) return "管理员";
  if (op.isSuperAdmin) return "超级管理员";
  return op.role || op.assignments?.[0]?.roleKey || "管理员";
}

function initialsOf(displayName?: string, account?: string) {
  return (displayName || account || "AG").trim().slice(0, 2).toUpperCase();
}

export function useOperatorIdentity(): OperatorIdentity {
  const operator = useAuthStore((s) => s.operator);
  const profileQuery = useAdminProfileQuery();
  const account = profileQuery.data?.account;

  return useMemo(
    () => ({
      name: operator?.displayName || operator?.account || "管理员",
      role: roleName(operator),
      identity: account?.email || (operator?.account ? `@${operator.account}` : undefined),
      avatarSrc: avatarUrl(account?.avatar || operator?.avatar, operator?.avatarVersion),
      initials: initialsOf(operator?.displayName, operator?.account),
      superAdmin: Boolean(operator?.isSuperAdmin)
    }),
    [operator, account]
  );
}
