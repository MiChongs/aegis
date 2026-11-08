"use client";

import { Suspense, useEffect } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { KeyRound, ShieldCheck, Wifi } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { LoadingState } from "@/components/ui/data-state";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AdminPanel } from "@/components/users/admin-panel";
import { OnlinePanel } from "@/components/users/online-panel";
import { RolePermissionsPanel } from "@/components/users/role-permissions-panel";

function UsersPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const raw = searchParams.get("tab") || "admins";
  // 只剩这三个页签；以前的用户主档、标签、分群、名单、风险用户、申诉、注销页签已删，旧链接落回管理员
  const tab = ["admins", "online", "roles", "app-users"].includes(raw) ? raw : "admins";

  // 应用用户已独立成 /app-users。旧链接（书签、分享、审计记录）照常可用：
  // 带着除 tab 以外的全部参数原样转过去，筛选状态不丢。
  const isLegacyAppUsers = tab === "app-users";
  useEffect(() => {
    if (!isLegacyAppUsers) return;
    const params = new URLSearchParams(searchParams.toString());
    params.delete("tab");
    const query = params.toString();
    router.replace(query ? `/app-users?${query}` : "/app-users");
  }, [isLegacyAppUsers, router, searchParams]);

  if (isLegacyAppUsers) {
    return <LoadingState title="正在打开应用用户…" />;
  }

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="用户与身份中心" />

      <Tabs value={tab} onValueChange={(v) => router.replace(`/users?tab=${v}`, { scroll: false })} className="space-y-6">
        <TabsList className="flex-wrap">
          <TabsTrigger value="admins"><ShieldCheck className="size-3.5" />管理员</TabsTrigger>
          <TabsTrigger value="online"><Wifi className="size-3.5" />在线会话</TabsTrigger>
          <TabsTrigger value="roles"><KeyRound className="size-3.5" />角色权限</TabsTrigger>
        </TabsList>

        <TabsContent value="admins"><AdminPanel /></TabsContent>
        <TabsContent value="online"><OnlinePanel /></TabsContent>
        <TabsContent value="roles"><RolePermissionsPanel /></TabsContent>
      </Tabs>
    </div>
  );
}

export default function UsersPage() {
  return <Suspense><UsersPageInner /></Suspense>;
}
