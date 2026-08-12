"use client";

import Link from "next/link";
import {
  BookText,
  ChevronDown,
  Command as CommandIcon,
  ExternalLink,
  LifeBuoy,
  LogOut,
  ShieldCheck,
  UserRound
} from "lucide-react";

import { Kbd, useMetaKeyLabel } from "@/components/layout/sidebar/sidebar-shared";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import type { OperatorIdentity } from "@/lib/operator";
import { cn } from "@/lib/utils";

type UserMenuProps = {
  operator: OperatorIdentity;
  onOpenPalette: () => void;
  onLogout: () => void;
  className?: string;
};

/** 控制台之外的公开页面：换的是整个外壳，因此新标签页打开 */
const HELP_LINKS = [
  { href: "/developers", label: "开发者门户", icon: LifeBuoy },
  { href: "/developers/api", label: "接口文档", icon: BookText },
  { href: "/status", label: "服务状态", icon: ShieldCheck }
] as const;

/**
 * 顶栏账户菜单。
 *
 * 面板头部刻意不是"再写一遍触发器上那两行字"：触发器已经有头像和昵称，
 * 所以这里补的是它放不下的那一条 —— 邮箱 / 账号与角色徽标。
 * 同名管理员在多租户里很常见，只看昵称分不出自己现在是哪个身份登着。
 */
export function UserMenu({ operator, onOpenPalette, onLogout, className }: UserMenuProps) {
  const metaKey = useMetaKeyLabel();
  const { name, role, identity, avatarSrc, initials, superAdmin } = operator;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="账户菜单"
          className={cn(
            "group flex max-w-[12rem] shrink-0 items-center gap-2 rounded-lg py-1 pr-1.5 pl-1",
            "transition-colors hover:bg-accent data-[state=open]:bg-accent",
            "focus-visible:ring-[2px] focus-visible:ring-ring/50 focus-visible:outline-none",
            className
          )}
        >
          <Avatar
            className={cn("size-6 ring-1", superAdmin ? "ring-primary/50" : "ring-border/70")}
            preview={false}
          >
            <AvatarImage src={avatarSrc} alt={name} />
            <AvatarFallback className="text-[8px] font-medium">{initials}</AvatarFallback>
          </Avatar>
          {/* 名字按顶栏自身宽度让位：侧边栏展开时顶栏能比视口窄 200px */}
          <span className="hidden min-w-0 truncate text-xs font-medium @lg/topbar:block">{name}</span>
          {/* 箭头跟着开合翻转：面板是从这个按钮长出来的，不是凭空浮现的 */}
          <ChevronDown className="hidden size-3 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[state=open]:rotate-180 @lg/topbar:block" />
        </button>
      </DropdownMenuTrigger>

      <DropdownMenuContent align="end" sideOffset={8} className="w-64 p-1.5">
        <DropdownMenuLabel className="p-0 font-normal">
          <div className="flex items-start gap-2.5 rounded-md bg-muted/60 p-2">
            <Avatar className="size-9 shrink-0 ring-1 ring-border/70" preview={false}>
              <AvatarImage src={avatarSrc} alt={name} />
              <AvatarFallback className="text-[10px] font-medium">{initials}</AvatarFallback>
            </Avatar>
            <div className="min-w-0 flex-1 space-y-1">
              <p className="truncate text-sm leading-tight font-semibold">{name}</p>
              {identity ? (
                <p className="truncate text-[11px] leading-tight text-muted-foreground">{identity}</p>
              ) : null}
              <Badge variant={superAdmin ? "info" : "secondary"} size="sm" className="max-w-full truncate">
                {role}
              </Badge>
            </div>
          </div>
        </DropdownMenuLabel>

        <DropdownMenuGroup className="mt-1">
          <DropdownMenuItem className="gap-2.5 text-xs" onClick={onOpenPalette}>
            <CommandIcon className="size-3.5" />
            命令面板
            <Kbd className="ml-auto">{metaKey} K</Kbd>
          </DropdownMenuItem>
          <DropdownMenuItem className="gap-2.5 text-xs" asChild>
            <Link href="/profile">
              <UserRound className="size-3.5" />
              个人资料
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem className="gap-2.5 text-xs" asChild>
            <Link href="/security">
              <ShieldCheck className="size-3.5" />
              账户安全
            </Link>
          </DropdownMenuItem>
        </DropdownMenuGroup>

        <DropdownMenuSeparator />

        <DropdownMenuGroup>
          <DropdownMenuLabel className="px-2 py-1 text-[10px] font-normal tracking-wide text-muted-foreground uppercase">
            帮助与文档
          </DropdownMenuLabel>
          {HELP_LINKS.map((link) => (
            <DropdownMenuItem key={link.href} className="gap-2.5 text-xs" asChild>
              <a href={link.href} target="_blank" rel="noreferrer">
                <link.icon className="size-3.5" />
                {link.label}
                <ExternalLink className="ml-auto size-3 text-muted-foreground" />
              </a>
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>

        <DropdownMenuSeparator />

        {/* 退出是唯一有代价的一项，用 destructive 变体把它和上面几项分开 */}
        <DropdownMenuItem variant="destructive" className="gap-2.5 text-xs" onClick={onLogout}>
          <LogOut className="size-3.5" />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
