"use client";

import { useState } from "react";
import { Check, ChevronsUpDown, Loader2, UserRound } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAdminAppUsersQuery } from "@/lib/admin-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import type { AdminAppUserItem } from "@/lib/api/types";

export type PickedUser = {
  id: number;
  account?: string;
  nickname?: string;
  email?: string;
};

/**
 * 用户选择器。
 *
 * 存在的理由：调账、代开凭证这类操作原本要求管理员**手输用户 ID**。
 * 那个数字没人记得住，实际操作是「先去用户列表搜一遍、复制 ID、切回来粘贴」——
 * 三步里有两步是在替机器做检索。这里把检索放回机器：输账号、昵称或邮箱即可，
 * 选中之后 ID 由组件持有，管理员从头到尾不必看见它。
 *
 * 搜索走 300ms 防抖，不做「输入 + 点查询」那种形状。
 */
export function UserPicker({
  appKey,
  value,
  onChange,
  placeholder = "搜索账号 / 昵称 / 邮箱",
  disabled
}: {
  appKey?: string | null;
  value: PickedUser | null;
  onChange: (user: PickedUser | null) => void;
  placeholder?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const debounced = useDebouncedValue(keyword.trim(), 300);

  const query = useAdminAppUsersQuery(open ? appKey : null, {
    keyword: debounced || undefined,
    limit: 20,
    sort: "createdAt",
    order: "desc"
  });
  const users = query.data?.items ?? [];

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled || !appKey}
          className="h-9 w-full justify-between px-3 text-xs font-normal"
        >
          {value ? (
            <span className="flex min-w-0 items-center gap-1.5">
              <UserRound className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{displayName(value)}</span>
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">UID-{value.id}</span>
            </span>
          ) : (
            <span className="text-muted-foreground">{placeholder}</span>
          )}
          <ChevronsUpDown className="size-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[--radix-popover-trigger-width] p-0">
        {/* 过滤在服务端做（用户可能有几十万），关掉 cmdk 自带的本地过滤 */}
        <Command shouldFilter={false}>
          <CommandInput placeholder={placeholder} value={keyword} onValueChange={setKeyword} className="text-xs" />
          <CommandList>
            {query.isFetching ? (
              <div className="flex items-center justify-center gap-1.5 py-6 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" />
                搜索中...
              </div>
            ) : users.length === 0 ? (
              <CommandEmpty className="py-6 text-xs">
                {debounced ? "没有匹配的用户" : "输入账号、昵称或邮箱开始搜索"}
              </CommandEmpty>
            ) : (
              <CommandGroup>
                {users.map((user: AdminAppUserItem) => (
                  <CommandItem
                    key={user.id}
                    value={String(user.id)}
                    onSelect={() => {
                      onChange({
                        id: user.id,
                        account: user.account,
                        nickname: user.nickname,
                        email: user.email
                      });
                      setOpen(false);
                    }}
                    className="gap-2 text-xs"
                  >
                    <Check className={cn("size-3.5", value?.id === user.id ? "opacity-100" : "opacity-0")} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">
                        {user.nickname?.trim() || user.account?.trim() || `用户 ${user.id}`}
                      </span>
                      <span className="block truncate text-[10px] text-muted-foreground">
                        {[user.account, user.email].filter(Boolean).join(" · ") || "无账号信息"}
                      </span>
                    </span>
                    <span className="shrink-0 font-mono text-[10px] text-muted-foreground">UID-{user.id}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function displayName(user: PickedUser) {
  return user.nickname?.trim() || user.account?.trim() || `用户 ${user.id}`;
}
