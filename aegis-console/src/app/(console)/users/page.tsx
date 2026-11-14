"use client";

import { Suspense, useEffect, useMemo } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  BookKey,
  KeyRound,
  ShieldCheck,
  Stethoscope,
  Timer,
  Wifi,
} from "lucide-react";
import { AdminWorkspace } from "@/components/access/admin-workspace";
import { DiagnoseView } from "@/components/access/diagnose-view";
import { PermissionCatalogView } from "@/components/access/permission-catalog-view";
import { RoleWorkspace } from "@/components/access/role-workspace";
import { TempPermissionsView } from "@/components/access/temp-permissions-view";
import { LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { OnlinePanel } from "@/components/users/online-panel";
import { usePermissionChecker, usePermissionsLoading } from "@/lib/permissions";

/**
 * 管理员与权限（对标若依「系统管理」下的用户 / 角色 / 菜单，以及金蝶的功能权限与数据权限）。
 *
 * 每个页签各挂权限：只被授了「角色权限：查看」的人看得到角色、权限目录与诊断，看不到管理员列表。
 * 页签状态在地址栏（`?tab=`），侧边栏三级子项与命令面板都直达对应页签。
 */

const TABS = [
  {
    value: "admins",
    label: "管理员",
    icon: ShieldCheck,
    permission: "system:admin:read",
  },
  {
    value: "roles",
    label: "角色",
    icon: KeyRound,
    permission: "system:role:read",
  },
  {
    value: "permissions",
    label: "权限目录",
    icon: BookKey,
    permission: "system:role:read",
  },
  {
    value: "diagnose",
    label: "权限诊断",
    icon: Stethoscope,
    permission: "system:role:read",
  },
  {
    value: "temp",
    label: "临时权限",
    icon: Timer,
    permission: "system:temp_perm:read",
  },
  {
    value: "online",
    label: "在线会话",
    icon: Wifi,
    permission: ["system:online:read", "system:session:read"],
  },
] as const;

type TabValue = (typeof TABS)[number]["value"];

function UsersPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const can = usePermissionChecker();
  const loading = usePermissionsLoading();
  const visible = useMemo(
    () => TABS.filter((tab) => can(tab.permission as string | string[])),
    [can],
  );
  const raw = searchParams.get("tab") || "";

  // 应用用户已独立成 /app-users：旧链接带着其余参数原样转过去
  const isLegacyAppUsers = raw === "app-users";
  useEffect(() => {
    if (!isLegacyAppUsers) return;
    const params = new URLSearchParams(searchParams.toString());
    params.delete("tab");
    const query = params.toString();
    router.replace(query ? `/app-users?${query}` : "/app-users");
  }, [isLegacyAppUsers, router, searchParams]);

  if (isLegacyAppUsers) return <LoadingState title="正在打开应用用户…" />;
  if (loading && visible.length === 0) return <LoadingState title="加载中" />;
  if (visible.length === 0) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="控制台" title="管理员与权限" />
        <div className="rounded-2xl border bg-card py-16 text-center text-sm text-muted-foreground">
          缺少「管理员：查看」或「角色权限：查看」权限
        </div>
      </div>
    );
  }

  const tab: TabValue = (visible.find((item) => item.value === raw)?.value ??
    visible[0]!.value) as TabValue;

  const change = (next: string) => {
    // 切页签时丢掉角色页的选中参数，避免带着 role= 进到别的页签
    router.replace(`/users?tab=${next}`, { scroll: false });
  };

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="管理员与权限" />
      <Tabs value={tab} onValueChange={change} className="space-y-5">
        <TabsList className="flex-wrap">
          {visible.map((item) => {
            const Icon = item.icon;
            return (
              <TabsTrigger key={item.value} value={item.value}>
                <Icon className="size-3.5" />
                {item.label}
              </TabsTrigger>
            );
          })}
        </TabsList>
        {tab === "admins" && (
          <TabsContent value="admins">
            <AdminWorkspace />
          </TabsContent>
        )}
        {tab === "roles" && (
          <TabsContent value="roles">
            <RoleWorkspace />
          </TabsContent>
        )}
        {tab === "permissions" && (
          <TabsContent value="permissions">
            <PermissionCatalogView />
          </TabsContent>
        )}
        {tab === "diagnose" && (
          <TabsContent value="diagnose">
            <DiagnoseView />
          </TabsContent>
        )}
        {tab === "temp" && (
          <TabsContent value="temp">
            <TempPermissionsView />
          </TabsContent>
        )}
        {tab === "online" && (
          <TabsContent value="online">
            <OnlinePanel />
          </TabsContent>
        )}
      </Tabs>
    </div>
  );
}

export default function UsersPage() {
  return (
    <Suspense>
      <UsersPageInner />
    </Suspense>
  );
}
