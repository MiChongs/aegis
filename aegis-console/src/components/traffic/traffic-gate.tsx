"use client";

import type { ReactNode } from "react";
import { EmptyState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { useAuthStore } from "@/lib/auth-store";

/** 流量管理的全部接口只对超级管理员开放 */
export function TrafficGate({ title, children }: { title: string; children: ReactNode }) {
  const operator = useAuthStore((state) => state.operator);
  if (!operator?.isSuperAdmin) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="流量管理" title={title} />
        <EmptyState title="需要超级管理员权限" />
      </div>
    );
  }
  return <>{children}</>;
}
