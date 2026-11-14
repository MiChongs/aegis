"use client";

import type { ReactNode } from "react";
import { EmptyState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { usePermission } from "@/lib/permissions";

/** 流量管理的页面入口：查看用 system:traffic:read，新建规则用 system:traffic:write */
export function TrafficGate({
  title,
  write,
  children
}: {
  title: string;
  /** 页面本身就是写操作（新建规则） */
  write?: boolean;
  children: ReactNode;
}) {
  const { allowed } = usePermission(write ? "system:traffic:write" : "system:traffic:read");
  if (!allowed) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="流量管理" title={title} />
        <EmptyState title={write ? "缺少「流量管理：编辑规则与回滚」权限" : "缺少「流量管理：查看」权限"} />
      </div>
    );
  }
  return <>{children}</>;
}
