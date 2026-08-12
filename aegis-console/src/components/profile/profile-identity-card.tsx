"use client";

import * as React from "react";
import Link from "next/link";
import { Camera, Loader2, ShieldCheck, ShieldUser, Upload } from "lucide-react";
import { toast } from "sonner";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatTime, relativeTime } from "@/components/users/detail/user-detail-shared";
import { authSourceLabel, CopyButton } from "@/components/profile/profile-shared";
import { cn } from "@/lib/utils";
import type { AdminAccount } from "@/lib/api/types";

/** 服务端只认这四种（`avatar_service.go` 的 40089），客户端先拦一次，省一次失败往返 */
const ACCEPTED = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const SOFT_MAX_BYTES = 5 * 1024 * 1024;

/**
 * 身份卡。
 *
 * 这里放的是**不可编辑的身份事实**：你是谁、以什么方式登进来的、账号什么状态。
 * 可编辑的资料在下面的表单里，两边刻意不重复 —— 旧版右栏把邮箱 / 手机号 /
 * 生日又只读地列了一遍，于是你一边打字，旁边那份还显示着服务端的旧值，
 * 同一个页面上的两个数字对不上，而看的人无从判断哪个是真的。
 */
export function ProfileIdentityCard({
  account,
  avatarSrc,
  uploading,
  onUpload
}: {
  account: AdminAccount;
  avatarSrc: string;
  uploading: boolean;
  onUpload: (file: File) => void;
}) {
  const fileRef = React.useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = React.useState(false);

  const initials = (account.displayName || account.account || "A").trim().slice(0, 2).toUpperCase();
  const active = account.status === "active" || !account.status;

  const accept = React.useCallback(
    (file?: File | null) => {
      if (!file) return;
      if (!ACCEPTED.includes(file.type)) {
        toast.error("头像仅支持 JPG / PNG / GIF / WebP", { description: file.type || file.name });
        return;
      }
      if (file.size > SOFT_MAX_BYTES) {
        toast.error("文件太大了", {
          description: `${(file.size / 1024 / 1024).toFixed(1)}MB，建议压到 5MB 以内再上传`
        });
        return;
      }
      onUpload(file);
    },
    [onUpload]
  );

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <CardContent className="flex flex-col gap-5 p-5 sm:flex-row sm:items-center">
        {/* 头像：点击或拖入都能换 */}
        <div
          className={cn(
            "group/avatar relative size-20 shrink-0 self-start rounded-2xl transition-shadow",
            dragging && "ring-2 ring-ring ring-offset-2 ring-offset-background"
          )}
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault();
            setDragging(false);
            accept(event.dataTransfer.files?.[0]);
          }}
        >
          <Avatar className="size-20 rounded-2xl" preview={false}>
            <AvatarImage src={avatarSrc} alt={account.displayName || account.account} />
            <AvatarFallback className="rounded-2xl text-xl font-medium">{initials}</AvatarFallback>
          </Avatar>

          <button
            type="button"
            disabled={uploading}
            onClick={() => fileRef.current?.click()}
            aria-label="更换头像"
            className={cn(
              "absolute inset-0 flex flex-col items-center justify-center gap-1 rounded-2xl",
              "bg-black/60 text-white opacity-0 transition-opacity",
              "group-hover/avatar:opacity-100 focus-visible:opacity-100 focus-visible:outline-none",
              (uploading || dragging) && "opacity-100"
            )}
          >
            {uploading ? (
              <Loader2 className="size-5 animate-spin" />
            ) : dragging ? (
              <Upload className="size-5" />
            ) : (
              <>
                <Camera className="size-4" />
                <span className="text-[10px] leading-none">更换</span>
              </>
            )}
          </button>

          <input
            ref={fileRef}
            type="file"
            accept={ACCEPTED.join(",")}
            className="hidden"
            aria-label="上传头像"
            onChange={(event) => {
              accept(event.target.files?.[0]);
              event.target.value = "";
            }}
          />
        </div>

        {/* 身份 */}
        <div className="min-w-0 flex-1 space-y-2">
          <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            <h2 className="truncate text-xl font-semibold tracking-tight">
              {account.displayName || account.account}
            </h2>
            <span className="inline-flex items-center gap-0.5 text-sm text-muted-foreground">
              @{account.account}
              <CopyButton value={account.account} label="复制账号" />
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {account.isSuperAdmin ? (
              <Badge variant="info" size="sm" className="gap-1">
                <ShieldUser className="size-3" />
                超级管理员
              </Badge>
            ) : null}
            <Badge variant={active ? "success" : "danger"} size="sm">
              {active ? "账号正常" : `账号${account.status === "disabled" ? "已停用" : "异常"}`}
            </Badge>
            <Badge variant="outline" size="sm">
              {authSourceLabel(account.authSource)}
            </Badge>
          </div>

          <p className={cn("text-sm leading-6", account.bio ? "text-muted-foreground" : "text-muted-foreground/60")}>
            {account.bio || "还没有个人简介 —— 在下面写一句，同事在成员列表里就能认出你。"}
          </p>
        </div>

        {/* 快速事实 + 安全入口 */}
        <div className="flex shrink-0 flex-col gap-3 sm:items-end">
          <div className="flex items-center gap-4 text-right">
            <div>
              <p className="text-[11px] text-muted-foreground">最近登录</p>
              <Tooltip>
                <TooltipTrigger asChild>
                  <p className="text-sm font-medium tabular-nums">{relativeTime(account.lastLoginAt)}</p>
                </TooltipTrigger>
                <TooltipContent>{formatTime(account.lastLoginAt)}</TooltipContent>
              </Tooltip>
            </div>
            <Separator orientation="vertical" className="h-8" />
            <div>
              <p className="text-[11px] text-muted-foreground">加入时间</p>
              <Tooltip>
                <TooltipTrigger asChild>
                  <p className="text-sm font-medium tabular-nums">{relativeTime(account.createdAt)}</p>
                </TooltipTrigger>
                <TooltipContent>{formatTime(account.createdAt)}</TooltipContent>
              </Tooltip>
            </div>
          </div>
          <Button size="sm" variant="outline" className="h-8 gap-1.5 text-xs" asChild>
            <Link href="/security" prefetch>
              <ShieldCheck className="size-3.5" />
              账户安全设置
            </Link>
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
