"use client";

import { UserMenu } from "@/components/layout/topbar/user-menu";
import { TooltipProvider } from "@/components/ui/tooltip";

export default function PreviewUserMenuPage() {
  return (
    <TooltipProvider delayDuration={180}>
      <div className="flex min-h-screen flex-col">
        <header className="@container/topbar sticky top-0 z-30 flex h-12 shrink-0 items-center justify-end gap-3 border-b border-border bg-background/80 px-4 backdrop-blur-md">
          <UserMenu
            operator={{
              name: "张三丰运营中心内容审核负责人",
              role: "content_moderator_lead",
              identity: "@zhangsanfeng.operations",
              avatarSrc: "",
              initials: "张三",
              superAdmin: false
            }}
            onOpenPalette={() => {}}
            onLogout={() => {}}
          />
          <UserMenu
            operator={{
              name: "Super Administrator",
              role: "超级管理员",
              identity: "mikucyll@gmail.com",
              avatarSrc: "",
              initials: "SU",
              superAdmin: true
            }}
            onOpenPalette={() => {}}
            onLogout={() => {}}
          />
        </header>
        <main className="flex-1 p-6" />
      </div>
    </TooltipProvider>
  );
}
