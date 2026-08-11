"use client";

import { Shield } from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RoleMatrixEditor } from "@/components/roles/role-matrix-editor";
import { RoleGraphCanvas } from "@/components/roles/role-graph-canvas";
import { RoleManagePanel } from "@/components/roles/role-manage-panel";

export default function RolesPage() {
  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="角色权限" />
      <Tabs defaultValue="matrix">
        <TabsList>
          <TabsTrigger value="matrix"><Shield className="size-4" />权限矩阵</TabsTrigger>
          <TabsTrigger value="graph"><Shield className="size-4" />角色关系图</TabsTrigger>
          <TabsTrigger value="manage"><Shield className="size-4" />角色管理</TabsTrigger>
        </TabsList>
        <TabsContent value="matrix"><RoleMatrixEditor /></TabsContent>
        <TabsContent value="graph"><RoleGraphCanvas /></TabsContent>
        <TabsContent value="manage"><RoleManagePanel /></TabsContent>
      </Tabs>
    </div>
  );
}
