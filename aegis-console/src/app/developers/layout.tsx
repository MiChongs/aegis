import type { Metadata } from "next";
import { PortalShell } from "@/components/developers/portal-shell";

export const metadata: Metadata = {
  title: "Aegis 开发者",
  description: "Aegis 开放接口的快速接入指南与完整接口文档"
};

/**
 * 开发者门户布局 —— 公开路由，刻意不套 AuthGate：
 * 接入方在拿到管理员账号之前就应该能读文档、抄代码。
 */
export default function DevelopersLayout({ children }: { children: React.ReactNode }) {
  return <PortalShell>{children}</PortalShell>;
}
