"use client";

import { Fragment, useMemo } from "react";
import Link from "next/link";
import { Check, ChevronDown, House } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";

import {
  Breadcrumb,
  BreadcrumbEllipsis,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { childHref, matchNavigation, type NavigationItem } from "@/lib/navigation";
import { useVisibleGroups } from "@/lib/navigation-hooks";
import { cn } from "@/lib/utils";

/** 与侧边栏高亮滑动、路由过渡同一条曲线 */
const CRUMB_EASE: [number, number, number, number] = [0.32, 0.72, 0, 1];

type CrumbTarget = {
  key: string;
  title: string;
  href: string;
  icon?: NavigationItem["icon"];
  active: boolean;
};

type Crumb = {
  key: string;
  label: string;
  icon?: NavigationItem["icon"];
  /** 可点击时的目标 */
  href?: string;
  /** 该层可横向切换的同级目标：分组下的页面、页面下的面板 */
  menu?: { label: string; items: CrumbTarget[] };
};

/**
 * 顶栏面包屑。
 *
 * 三件事，缺一条它就只是装饰：
 *
 * 1. **说清我在哪** —— 控制台 › 分组 › 页面 › 面板，与侧边栏同一棵目录。
 * 2. **能横向走** —— 分组段与面板段是可切换的菜单（同级页面 / 同页面板），
 *    换一个兄弟页面不必先回侧边栏找。菜单里的页面按当前管理员权限过滤，
 *    否则它就成了绕过侧边栏鉴权的后门。
 * 3. **窄下来不塌** —— 超过两级时前面的段收进 `…`（菜单里永远是完整路径），
 *    末段恒可见并 truncate。折叠按**顶栏自身宽度**判断（容器查询）而不是视口：
 *    侧边栏可折叠、可拖宽，同一个视口宽度下顶栏能差出 200px。
 */
export function TopbarBreadcrumbTree({
  pathname,
  activeTab,
  className
}: {
  pathname: string;
  activeTab: string | null;
  className?: string;
}) {
  const visibleGroups = useVisibleGroups();

  const crumbs = useMemo<Crumb[]>(() => {
    const list: Crumb[] = [{ key: "root", label: "控制台", href: "/overview", icon: House }];
    const match = matchNavigation(pathname, activeTab);

    if (!match) {
      // 目录之外的页面：给一个末级标签，别让路径断在「控制台」上
      if (pathname === "/profile") list.push({ key: "/profile", label: "个人信息" });
      return list;
    }

    const siblings = visibleGroups.find((g) => g.key === match.group.key)?.items ?? [];
    list.push({
      key: `group:${match.group.key}`,
      label: match.group.title,
      menu: siblings.length
        ? {
            label: match.group.summary || match.group.title,
            items: siblings.map((item) => ({
              key: item.href,
              title: item.title,
              href: item.href,
              icon: item.icon,
              active: item.href === match.item.href
            }))
          }
        : undefined
    });

    const panels = match.child ? (match.item.children ?? []) : [];
    list.push({
      key: match.item.href,
      label: match.item.title,
      icon: match.item.icon,
      // 有面板时页面本身仍可点（回到默认面板）；没有面板时它就是终点
      href: match.child ? match.item.href : undefined
    });

    if (match.child) {
      list.push({
        key: `${match.item.href}#${match.child.tab}`,
        label: match.child.title,
        menu: panels.length
          ? {
              label: `${match.item.title} · 面板`,
              items: panels.map((child) => ({
                key: child.tab,
                title: child.title,
                href: childHref(match.item, child),
                active: child.tab === match.child?.tab
              }))
            }
          : undefined
      });
    }

    return list;
  }, [pathname, activeTab, visibleGroups]);

  const last = crumbs.length - 1;

  // 末段恒显；倒数第二段窄到 32rem 以下让位；更前面的收进 `…`
  const visibilityOf = (index: number) =>
    index === last
      ? "flex min-w-0"
      : index === last - 1
        ? "hidden @lg/topbar:flex shrink-0"
        : "hidden @3xl/topbar:flex shrink-0";

  return (
    <Breadcrumb className={cn("min-w-0", className)}>
      <BreadcrumbList className="flex-nowrap gap-1 sm:gap-1.5">
        {/* 折叠入口：两级以上才有意义，菜单里给的是完整路径 */}
        {crumbs.length > 2 ? (
          <>
            <BreadcrumbItem className="shrink-0 @3xl/topbar:hidden">
              <TrailMenu crumbs={crumbs} />
            </BreadcrumbItem>
            <BreadcrumbSeparator className="shrink-0 @3xl/topbar:hidden" />
          </>
        ) : null}

        {crumbs.map((crumb, index) => {
          const visibility = visibilityOf(index);

          return (
            <Fragment key={crumb.key}>
              {/* 分隔符跟**前一段**同步显隐：跟自己同步的话，前一段被折进 `…` 之后
                  它会和 `…` 自带的那个分隔符并排出现，路径上凭空多一个 `›` */}
              {index > 0 ? <BreadcrumbSeparator className={visibilityOf(index - 1)} /> : null}
              <BreadcrumbItem className={visibility}>
                {crumb.menu ? (
                  <CrumbMenu crumb={crumb} isLeaf={index === last} />
                ) : crumb.href ? (
                  <BreadcrumbLink asChild>
                    <Link
                      href={crumb.href}
                      className="flex items-center gap-1.5 rounded-md px-1 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    >
                      {crumb.icon ? <crumb.icon className="size-3.5 shrink-0" /> : null}
                      <span className="truncate">{crumb.label}</span>
                    </Link>
                  </BreadcrumbLink>
                ) : (
                  <BreadcrumbPage
                    className={cn(
                      "flex min-w-0 items-center gap-1.5 px-1 py-0.5",
                      index === last ? "text-sm font-semibold" : "text-xs text-muted-foreground"
                    )}
                  >
                    {crumb.icon ? <crumb.icon className="size-3.5 shrink-0" /> : null}
                    <CrumbLabel animated={index === last}>{crumb.label}</CrumbLabel>
                  </BreadcrumbPage>
                )}
              </BreadcrumbItem>
            </Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}

/**
 * 末段文字换页时轻推一下。
 *
 * 整段的 key 就是路径，换页即重挂载，`initial` 会重新播一次 ——
 * 切页是"一次动作"，路径也该跟着动，而不是原地闪出一个新词。
 */
function CrumbLabel({ children, animated }: { children: React.ReactNode; animated: boolean }) {
  const reduced = useReducedMotion();
  if (!animated || reduced) return <span className="truncate">{children}</span>;
  return (
    <motion.span
      className="truncate"
      initial={{ opacity: 0, x: -6 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.24, ease: CRUMB_EASE }}
    >
      {children}
    </motion.span>
  );
}

/** 可切换的一段：点开是同级目标列表 */
function CrumbMenu({ crumb, isLeaf }: { crumb: Crumb; isLeaf: boolean }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          "group flex min-w-0 items-center gap-1 rounded-md px-1 py-0.5 transition-colors",
          "hover:bg-accent hover:text-foreground data-[state=open]:bg-accent",
          "focus-visible:ring-[2px] focus-visible:ring-ring/50 focus-visible:outline-none",
          isLeaf ? "text-sm font-semibold text-foreground" : "text-xs text-muted-foreground"
        )}
      >
        {crumb.icon ? <crumb.icon className="size-3.5 shrink-0" /> : null}
        <CrumbLabel animated={isLeaf}>{crumb.label}</CrumbLabel>
        <ChevronDown className="size-3 shrink-0 opacity-60 transition-transform duration-200 group-data-[state=open]:rotate-180" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="max-h-[60vh] w-56 overflow-y-auto">
        <DropdownMenuLabel className="text-[10px] font-normal tracking-wide text-muted-foreground uppercase">
          {crumb.menu?.label}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {crumb.menu?.items.map((item) => (
          <DropdownMenuItem key={item.key} asChild className="gap-2 text-xs">
            <Link href={item.href}>
              {item.icon ? <item.icon className="size-3.5 shrink-0 text-muted-foreground" /> : null}
              <span className="truncate">{item.title}</span>
              {item.active ? <Check className="ml-auto size-3 shrink-0" /> : null}
            </Link>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** `…`：窄屏下被折起来的那几段，菜单里给完整路径 */
function TrailMenu({ crumbs }: { crumbs: Crumb[] }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label="展开完整路径"
        className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground data-[state=open]:bg-accent focus-visible:ring-[2px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <BreadcrumbEllipsis className="size-6 [&>svg]:size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-60">
        <DropdownMenuLabel className="text-[10px] font-normal tracking-wide text-muted-foreground uppercase">
          当前位置
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {crumbs.map((crumb, index) => {
          const target = crumb.href ?? crumb.menu?.items.find((i) => i.active)?.href;
          const label = (
            <span className="flex min-w-0 items-center gap-1.5" style={{ paddingInlineStart: index * 10 }}>
              {crumb.icon ? <crumb.icon className="size-3.5 shrink-0 text-muted-foreground" /> : null}
              <span className="truncate">{crumb.label}</span>
            </span>
          );
          return target ? (
            <DropdownMenuItem key={crumb.key} asChild className="text-xs">
              <Link href={target}>{label}</Link>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem key={crumb.key} className="pointer-events-none text-xs font-medium">
              {label}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
