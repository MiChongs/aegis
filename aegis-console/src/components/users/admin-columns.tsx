"use client";

import type { DataTableColumnDef } from "./data-table";
import { MoreHorizontal, ShieldAlert } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { AdminAccount, AdminLoginAvailability } from "@/lib/api/types";

function initials(displayName?: string | null, account?: string | null) {
  const v = String(displayName || account || "AG").trim();
  return v.slice(0, 2).toUpperCase();
}

function fmtTime(value?: string | null) {
  if (!value) return "—";
  const d = new Date(value);
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

const AUTH_SOURCE_LABEL: Record<string, string> = {
  password: "本地",
  ldap: "LDAP",
  oidc: "OIDC",
  saml: "SAML"
};

export type AdminAction = "toggle-status" | "edit-access";

// 超管视图下，管理员对象会带上 _loginAvailability —— 由 admin-panel.flattenAdmins 注入
type AdminRow = AdminAccount & { _loginAvailability?: AdminLoginAvailability };

type AdminColumnOptions = {
  /** 是否显示"账号可能无法登录"告警列（仅超管可见） */
  showLoginAvailability?: boolean;
};

export function createAdminColumns(
  onAction: (action: AdminAction, admin: AdminAccount) => void,
  options: AdminColumnOptions = {}
): DataTableColumnDef<AdminRow>[] {
  const cols: DataTableColumnDef<AdminRow>[] = [
    {
      id: "admin",
      header: "管理员",
      enableSorting: false,
      cell: ({ row }) => {
        const r = row.original;
        return (
          <div className="flex items-center gap-2.5">
            <Avatar className="size-8 rounded-lg">
              <AvatarImage src={r.avatar} alt={r.displayName || r.account} />
              <AvatarFallback className="rounded-lg text-[10px]">{initials(r.displayName, r.account)}</AvatarFallback>
            </Avatar>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{r.displayName || r.account}</div>
              {r.displayName && <div className="truncate text-xs text-muted-foreground">{r.account}</div>}
            </div>
          </div>
        );
      }
    },
    {
      id: "email",
      header: "邮箱",
      cell: ({ row }) => <span className="truncate text-muted-foreground">{row.original.email || "—"}</span>
    },
    {
      id: "authSource",
      header: "登录方式",
      enableSorting: false,
      cell: ({ row }) => {
        const src = (row.original.authSource || "password").toLowerCase();
        const label = AUTH_SOURCE_LABEL[src] ?? src.toUpperCase();
        return (
          <span className="inline-flex items-center rounded-md border bg-muted/40 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            {label}
          </span>
        );
      }
    },
    {
      id: "role",
      header: "权限",
      enableSorting: false,
      cell: ({ row }) => <span className="text-xs">{row.original.isSuperAdmin ? "超管" : "管理员"}</span>
    },
    {
      id: "status",
      header: "状态",
      cell: ({ row }) => {
        const active = row.original.status === "active";
        return (
          <span className="flex items-center gap-1.5 text-xs">
            <span className={`inline-block size-1.5 rounded-full ${active ? "bg-emerald-500" : "bg-zinc-300 dark:bg-zinc-600"}`} />
            {active ? "正常" : "停用"}
          </span>
        );
      }
    }
  ];

  // 超管专属列：标注第三方认证源不可用导致该账号可能无法登录
  if (options.showLoginAvailability) {
    cols.push({
      id: "loginAvailability",
      header: "登录健康",
      enableSorting: false,
      cell: ({ row }) => <LoginAvailabilityCell value={row.original._loginAvailability} />
    });
  }

  cols.push(
    {
      id: "lastLogin",
      header: "最近登录",
      cell: ({ row }) => <span className="text-xs tabular-nums text-muted-foreground">{fmtTime(row.original.lastLoginAt)}</span>
    },
    {
      id: "actions",
      enableSorting: false,
      cell: ({ row }) => {
        const r = row.original;
        return (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="size-7"><MoreHorizontal className="size-4" /></Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => onAction("edit-access", r)}>
                编辑权限
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => onAction("toggle-status", r)}>
                {r.status === "disabled" ? "启用" : "停用"}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        );
      }
    }
  );

  return cols;
}

// ─────────────── 登录健康单元格 ───────────────
//
// 显示规则：
//   - 本地账号（authSource=password 或未上报 loginAvailability）：显示 "—"（无需标注）
//   - 第三方账号 + available=true：绿色 "可用"
//   - 第三方账号 + available=false：红色 "账号可能无法登录" 徽章，hover 显示原因
//
// 徽章样式与 shadcn/ui Badge 一致；原因文字在 Tooltip 中展开，避免行高撑变。
function LoginAvailabilityCell({ value }: { value?: AdminLoginAvailability }) {
  if (!value || !value.source || value.source === "password") {
    return <span className="text-xs text-muted-foreground">—</span>;
  }

  if (value.available) {
    return (
      <Badge variant="secondary" className="gap-1 text-[10px] font-medium">
        <span className="inline-block size-1 rounded-full bg-emerald-500" aria-hidden />
        可用
      </Badge>
    );
  }

  const reason = value.reason?.trim() || `${value.source.toUpperCase()} 认证源不可用`;
  return (
    <TooltipProvider delayDuration={120}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant="danger"
            className="max-w-[14rem] cursor-help gap-1 truncate text-[10px] font-medium"
          >
            <ShieldAlert className="size-3 shrink-0" />
            <span className="truncate">账号可能无法登录</span>
          </Badge>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-sm text-xs leading-relaxed">
          <div className="font-semibold">{value.source.toUpperCase()} 认证源异常</div>
          <div className="mt-1 text-muted-foreground">{reason}</div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
