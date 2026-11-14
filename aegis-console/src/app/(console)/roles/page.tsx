"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { LoadingState } from "@/components/ui/data-state";

/**
 * 角色管理已并入「管理员与权限」（/users?tab=roles）。保留这个路由只为旧书签与审计记录里的链接。
 */
export default function RolesPage() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/users?tab=roles");
  }, [router]);
  return <LoadingState title="正在打开角色…" />;
}
