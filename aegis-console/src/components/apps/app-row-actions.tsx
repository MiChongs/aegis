"use client";

import { useRouter } from "next/navigation";
import {
  Copy,
  Ellipsis,
  LogIn,
  Power,
  Settings2,
  Trash2,
  UserPlus,
  Users2
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { useAdminAppPatchMutation } from "@/lib/admin-hooks";
import { appSectionHref } from "@/lib/app-sections";
import { useCopyAppKey } from "@/components/apps/app-shared";
import type { AppSummary } from "@/lib/api/types";

/**
 * 单个应用的操作菜单，列表卡片 / 表格行 / 详情页头部共用。
 *
 * 三个开关直接做在菜单里而不是只留一条「进入配置」：停用一个应用是运维动作，
 * 逼着人先进详情页、找到基本信息、改下拉框、再点保存，就是原来那套「太鸡肋」。
 */
export function AppRowActions({
  app,
  onDelete,
  align = "end",
  trigger
}: {
  app: AppSummary;
  onDelete: (app: AppSummary) => void;
  align?: "start" | "end";
  trigger?: React.ReactNode;
}) {
  const router = useRouter();
  const patch = useAdminAppPatchMutation();
  const copyAppKey = useCopyAppKey();

  const run = async (payload: Parameters<typeof patch.mutateAsync>[0], message: string) => {
    try {
      await patch.mutateAsync(payload);
      toast.success(message);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {trigger ?? (
          <Button size="icon" variant="ghost" className="size-8 shrink-0" title="更多操作">
            <Ellipsis className="size-4" />
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-52">
        <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">
          {app.name} · #{app.id}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />

        <DropdownMenuItem onSelect={() => router.push(appSectionHref(app.appKey))}>
          <Settings2 className="size-3.5" />
          进入配置
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => router.push(`/app-users?app=${encodeURIComponent(app.appKey)}`)}>
          <Users2 className="size-3.5" />
          查看用户
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => copyAppKey(app.appKey)}>
          <Copy className="size-3.5" />
          复制 AppKey
        </DropdownMenuItem>

        <DropdownMenuSeparator />
        <DropdownMenuItem
          disabled={patch.isPending}
          onSelect={() =>
            void run({ appKey: app.appKey, status: !app.status }, app.status ? "应用已停用" : "应用已启用")
          }
        >
          <Power className="size-3.5" />
          {app.status ? "停用应用" : "启用应用"}
        </DropdownMenuItem>
        <DropdownMenuItem
          disabled={patch.isPending}
          onSelect={() =>
            void run(
              { appKey: app.appKey, registerStatus: !app.registerStatus },
              app.registerStatus ? "注册已关闭" : "注册已开放"
            )
          }
        >
          <UserPlus className="size-3.5" />
          {app.registerStatus ? "关闭注册" : "开放注册"}
        </DropdownMenuItem>
        <DropdownMenuItem
          disabled={patch.isPending}
          onSelect={() =>
            void run(
              { appKey: app.appKey, loginStatus: !app.loginStatus },
              app.loginStatus ? "登录已关闭" : "登录已开放"
            )
          }
        >
          <LogIn className="size-3.5" />
          {app.loginStatus ? "关闭登录" : "开放登录"}
        </DropdownMenuItem>

        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => onDelete(app)}>
          <Trash2 className="size-3.5" />
          删除应用
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
