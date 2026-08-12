"use client";

import { useState } from "react";
import Link from "next/link";
import { LogOut, Menu } from "lucide-react";

import { BrandLogo, BrandNameText, BrandSubtitleText } from "@/components/layout/brand";
import { SearchTrigger } from "@/components/layout/sidebar/search-trigger";
import { SidebarNavTree } from "@/components/layout/sidebar/sidebar-nav";
import { withActiveTab } from "@/components/layout/with-active-tab";
import { TopbarButton } from "@/components/layout/topbar/topbar-actions";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger
} from "@/components/ui/sheet";
import type { OperatorIdentity } from "@/lib/operator";

const MobileNavTree = withActiveTab(SidebarNavTree);

/**
 * 移动端导航抽屉。
 *
 * 装的是**同一棵**侧边栏导航树（`scope="mobile"` 只用来隔离滑动高亮的 layoutId），
 * 不是另写一份精简菜单 —— 那种"手机上少几项"的差异使用者永远发现不了。
 */
export function MobileNav({
  pathname,
  operator,
  onOpenPalette,
  onLogout
}: {
  pathname: string;
  operator: OperatorIdentity;
  onOpenPalette: () => void;
  onLogout: () => void;
}) {
  // 关闭时机全部走显式回调（导航树的 onNavigate、页脚两个链接），
  // 不用 effect 盯 pathname —— 那既是 `react-hooks/set-state-in-effect` 拒绝的形状，
  // 也会和抽屉自己的关闭动画抢时机。
  const [open, setOpen] = useState(false);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <TopbarButton label="打开导航" className="lg:hidden">
          <Menu className="size-4" />
        </TopbarButton>
      </SheetTrigger>

      <SheetContent side="left" className="w-[19rem] gap-0 p-0 sm:max-w-[19rem]">
        <SheetHeader className="shrink-0 flex-row items-center gap-2.5 space-y-0 border-b border-border px-4 py-3">
          <BrandLogo size="md" />
          <div className="min-w-0">
            <SheetTitle className="truncate text-sm leading-tight">
              <BrandNameText />
            </SheetTitle>
            <SheetDescription className="truncate text-[11px] leading-tight">
              <BrandSubtitleText />
            </SheetDescription>
          </div>
        </SheetHeader>

        <div className="shrink-0 border-b border-border px-3 py-2">
          <SearchTrigger
            collapsed={false}
            onOpen={() => {
              setOpen(false);
              onOpenPalette();
            }}
          />
        </div>

        <ScrollArea className="min-h-0 flex-1">
          <div className="p-2">
            <MobileNavTree pathname={pathname} scope="mobile" onNavigate={() => setOpen(false)} />
          </div>
        </ScrollArea>

        <div className="flex shrink-0 items-center gap-2 border-t border-border p-3">
          <Link
            href="/profile"
            onClick={() => setOpen(false)}
            className="-mx-1 flex min-w-0 flex-1 items-center gap-2 rounded-lg px-1 py-1 transition-colors hover:bg-accent"
          >
            <Avatar className="size-8 shrink-0 ring-1 ring-border/70" preview={false}>
              <AvatarImage src={operator.avatarSrc} alt={operator.name} />
              <AvatarFallback className="text-[9px] font-medium">{operator.initials}</AvatarFallback>
            </Avatar>
            <div className="min-w-0 space-y-0.5">
              <p className="truncate text-xs leading-tight font-medium">{operator.name}</p>
              <Badge variant={operator.superAdmin ? "info" : "secondary"} size="sm" className="max-w-full truncate">
                {operator.role}
              </Badge>
            </div>
          </Link>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="退出登录"
            title="退出登录"
            onClick={onLogout}
            className="shrink-0 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
          >
            <LogOut className="size-4" />
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
