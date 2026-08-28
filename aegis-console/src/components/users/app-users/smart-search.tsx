"use client";

import * as React from "react";
import { Command as CommandPrimitive } from "cmdk";
import {
  AtSign,
  CornerDownLeft,
  Crown,
  Globe2,
  Hash,
  Loader2,
  Phone,
  Search,
  UserRound,
  X
} from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { useAdminAppUsersQuery } from "@/lib/admin-hooks";
import type { AdminAppUserItem } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import type { UserQueryState } from "./shared";

/**
 * 智能搜索框：一个输入框同时解决三件事。
 *
 * 1. **识别输入形态**：邮箱、IP、手机号、`#ID` 各有明确意图，识别出来直接给
 *    对应的精确检索方式 —— 旧版只有一个全字段模糊搜，查邮箱会撞上昵称里的
 *    同名片段，查 IP 只能进「更多条件」抽屉再多点两下。
 * 2. **即时结果预览**：敲字即出前几条命中（头像/账号/状态），大多数时候管理员
 *    要找的是"某一个人"，直接点行进详情，根本不需要让整张表跑一次筛选。
 * 3. **按需下发筛选**：Enter 才把条件应用到列表。旧版每敲一个字符就重写一次
 *    URL 并全表重查，输入"13800138000"等于连发 11 次列表请求。
 *
 * 预览请求有 250ms 防抖 + limit 6，与主表查询互不影响。
 */

type IntentKey = "keyword" | "account" | "email" | "phone" | "registerIp";

type SearchIntent = {
  key: IntentKey;
  label: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
};

const IPV4_LIKE = /^(\d{1,3}\.){1,3}\d{0,3}$/;
const IPV6_LIKE = /^[0-9a-fA-F:]{3,}$/;

/** 识别输入形态。返回的第一项是 Enter 的默认动作（cmdk 高亮首项，所见即所得）。 */
export function detectIntents(raw: string): SearchIntent[] {
  const value = raw.trim();
  if (!value) return [];

  if (/^#\s*\d+$/.test(value)) {
    return [
      {
        key: "keyword",
        label: "按用户 ID 直达",
        description: `只匹配 ID = ${value.replace(/\D/g, "")} 的用户`,
        icon: Hash
      }
    ];
  }

  const intents: SearchIntent[] = [];
  if (value.includes("@")) {
    intents.push({
      key: "email",
      label: "只搜邮箱",
      description: "在邮箱字段里模糊匹配",
      icon: AtSign
    });
  }
  if ((IPV4_LIKE.test(value) || (value.includes(":") && IPV6_LIKE.test(value))) && !value.includes("@")) {
    intents.push({
      key: "registerIp",
      label: "只搜注册 IP",
      description: "找同源批量注册",
      icon: Globe2
    });
  }
  if (/^\d{5,20}$/.test(value)) {
    intents.push({
      key: "phone",
      label: "只搜手机号",
      description: "在手机字段里模糊匹配",
      icon: Phone
    });
  }
  intents.push({
    key: "keyword",
    label: "全字段搜索",
    description: "账号 / 昵称 / 邮箱 / 手机 / 邀请码 / IP / 标识码 / 自定义 ID",
    icon: Search
  });
  if (!value.includes("@") && /^[A-Za-z][\w.-]*$/.test(value)) {
    intents.push({
      key: "account",
      label: "只搜账号",
      description: "在账号字段里模糊匹配",
      icon: UserRound
    });
  }
  return intents;
}

/** 意图 → 列表查询参数。keyword 意图保留 `#id` 原文，后端认识这个形态。 */
function intentParams(intent: SearchIntent, value: string): Partial<UserQueryState> {
  if (intent.key === "keyword") return { keyword: value };
  return { [intent.key]: value } as Partial<UserQueryState>;
}

function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = React.useState(value);
  React.useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}

function initials(nickname?: string | null, account?: string | null) {
  return String(nickname || account || "U").trim().slice(0, 2).toUpperCase();
}

function isVipActive(expireAt?: string | null) {
  if (!expireAt) return false;
  const time = new Date(expireAt).getTime();
  return !Number.isNaN(time) && time > Date.now();
}

export function SmartUserSearch({
  appKey,
  state,
  onChange,
  onOpenUser
}: {
  appKey: string | null;
  state: UserQueryState;
  onChange: (next: UserQueryState) => void;
  onOpenUser: (user: AdminAppUserItem) => void;
}) {
  const inputRef = React.useRef<HTMLInputElement>(null);
  const [input, setInput] = React.useState(state.keyword);
  const [open, setOpen] = React.useState(false);

  // 外部改了 keyword（点「已筛」胶囊清除、应用保存的视图）时输入框要跟上。
  const appliedKeyword = state.keyword;
  const lastApplied = React.useRef(appliedKeyword);
  React.useEffect(() => {
    if (lastApplied.current !== appliedKeyword) {
      lastApplied.current = appliedKeyword;
      setInput(appliedKeyword);
    }
  }, [appliedKeyword]);

  const trimmed = input.trim();
  const intents = React.useMemo(() => detectIntents(trimmed), [trimmed]);
  const primary = intents[0];

  // 预览查询：跟第一意图走，250ms 防抖，只取前 6 条。
  const debounced = useDebouncedValue(trimmed, 250);
  const previewEnabled = open && Boolean(debounced) && Boolean(appKey);
  const previewIntents = React.useMemo(() => detectIntents(debounced), [debounced]);
  const previewQuery = useAdminAppUsersQuery(
    previewEnabled ? appKey : null,
    previewIntents.length
      ? {
          ...Object.fromEntries(
            Object.entries(intentParams(previewIntents[0], debounced)).filter(([, v]) => v)
          ),
          page: 1,
          limit: 6
        }
      : undefined
  );
  const previewItems = previewQuery.data?.items ?? [];
  const previewTotal = previewQuery.data?.total ?? 0;

  // `/` 聚焦搜索：查人是这页最高频动作，不该先摸鼠标。
  React.useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }
      event.preventDefault();
      inputRef.current?.focus();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const applyIntent = React.useCallback(
    (intent: SearchIntent) => {
      const value = input.trim();
      if (!value) return;
      lastApplied.current = intent.key === "keyword" ? value : "";
      onChange({
        ...state,
        // 换检索方式时清掉旧关键字，避免两个条件叠加成空集
        keyword: "",
        ...intentParams(intent, value),
        page: 1
      });
      if (intent.key !== "keyword") setInput("");
      setOpen(false);
      inputRef.current?.blur();
    },
    [input, onChange, state]
  );

  const clear = React.useCallback(() => {
    setInput("");
    if (state.keyword) {
      lastApplied.current = "";
      onChange({ ...state, keyword: "", page: 1 });
    }
    inputRef.current?.focus();
  }, [onChange, state]);

  // 关闭且没应用：输入框回到已应用的关键字，不让"看起来筛了、其实没筛"的状态存在。
  const closeWithoutApply = React.useCallback(() => {
    setOpen(false);
    setInput(appliedKeyword);
  }, [appliedKeyword]);

  return (
    <CommandPrimitive
      shouldFilter={false}
      loop
      className="relative min-w-[260px] flex-1 overflow-visible bg-transparent sm:max-w-md"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          closeWithoutApply();
          inputRef.current?.blur();
        }
      }}
    >
      <div className="relative">
        <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <CommandPrimitive.Input
          ref={inputRef}
          value={input}
          onValueChange={(value) => {
            setInput(value);
            if (!open) setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onBlur={closeWithoutApply}
          placeholder="搜索账号 / 昵称 / 邮箱 / 手机 / IP，# + ID 直达"
          className={cn(
            "h-8 w-full rounded-md border border-input bg-transparent pl-8 pr-14 text-xs shadow-xs outline-none transition-[color,box-shadow]",
            "placeholder:text-muted-foreground",
            "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
          )}
        />
        <div className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-1">
          {previewQuery.isFetching && previewEnabled ? (
            <Loader2 className="size-3 animate-spin text-muted-foreground" />
          ) : null}
          {input ? (
            <button
              type="button"
              aria-label="清空搜索"
              className="text-muted-foreground hover:text-foreground"
              // onMouseDown 抢在 blur 之前，否则 blur 先把输入框还原，点击就落空了
              onMouseDown={(event) => {
                event.preventDefault();
                clear();
              }}
            >
              <X className="size-3.5" />
            </button>
          ) : (
            <kbd className="pointer-events-none rounded border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">
              /
            </kbd>
          )}
        </div>
      </div>

      {open && trimmed ? (
        <div
          className="absolute top-full z-50 mt-1.5 w-full min-w-[320px] overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-md"
          // 列表点击先于 blur 生效
          onMouseDown={(event) => event.preventDefault()}
        >
          <CommandPrimitive.List className="max-h-[380px] overflow-y-auto p-1">
            <CommandPrimitive.Group
              heading="检索方式"
              className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:text-muted-foreground"
            >
              {intents.map((intent) => (
                <CommandPrimitive.Item
                  key={intent.key}
                  value={`intent-${intent.key}`}
                  onSelect={() => applyIntent(intent)}
                  className={cn(
                    "flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-xs",
                    "data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
                  )}
                >
                  <intent.icon className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="shrink-0 font-medium">{intent.label}</span>
                  <span className="truncate text-muted-foreground">{intent.description}</span>
                  <CornerDownLeft className="ml-auto size-3 shrink-0 opacity-0 data-[selected=true]:opacity-40" />
                </CommandPrimitive.Item>
              ))}
            </CommandPrimitive.Group>

            {appKey ? (
              <CommandPrimitive.Group
                heading={
                  previewQuery.isLoading
                    ? "匹配的用户"
                    : `匹配的用户${previewTotal ? `（共 ${previewTotal.toLocaleString("zh-CN")} 个，预览前 ${Math.min(previewItems.length, 6)} 个）` : ""}`
                }
                className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:text-muted-foreground"
              >
                {previewQuery.isLoading || debounced !== trimmed ? (
                  <div className="flex items-center gap-2 px-2 py-3 text-xs text-muted-foreground">
                    <Loader2 className="size-3.5 animate-spin" />
                    正在匹配…
                  </div>
                ) : previewItems.length ? (
                  previewItems.map((user) => {
                    const enabled = user.enabled !== false;
                    const vipActive = isVipActive(user.vipExpireAt);
                    return (
                      <CommandPrimitive.Item
                        key={user.id}
                        value={`user-${user.id}`}
                        onSelect={() => {
                          setOpen(false);
                          onOpenUser(user);
                        }}
                        className={cn(
                          "flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5",
                          "data-[selected=true]:bg-accent data-[selected=true]:text-accent-foreground"
                        )}
                      >
                        <Avatar className="size-7 rounded-md">
                          <AvatarImage src={typeof user.avatar === "string" ? user.avatar : ""} />
                          <AvatarFallback className="rounded-md text-[10px]">
                            {initials(user.nickname, user.account)}
                          </AvatarFallback>
                        </Avatar>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-1.5">
                            <span className="truncate text-xs font-medium">
                              {user.nickname || user.account || `#${user.id}`}
                            </span>
                            {vipActive ? <Crown className="size-3 shrink-0 text-amber-500" /> : null}
                            {!enabled ? (
                              <Badge variant="danger" size="sm" className="shrink-0 text-[9px]">
                                受限
                              </Badge>
                            ) : null}
                          </div>
                          <div className="truncate text-[11px] text-muted-foreground">
                            {[`ID ${user.id}`, user.account, user.email || user.phone]
                              .filter(Boolean)
                              .join(" · ")}
                          </div>
                        </div>
                      </CommandPrimitive.Item>
                    );
                  })
                ) : (
                  <div className="px-2 py-3 text-xs text-muted-foreground">
                    没有匹配的用户，换个关键字或检索方式试试
                  </div>
                )}
              </CommandPrimitive.Group>
            ) : null}
          </CommandPrimitive.List>
          <div className="flex items-center gap-3 border-t px-2.5 py-1.5 text-[10px] text-muted-foreground">
            <span className="flex items-center gap-1">
              <kbd className="rounded border bg-muted px-1 font-mono">↑↓</kbd>选择
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border bg-muted px-1 font-mono">Enter</kbd>应用 / 打开
            </span>
            <span className="flex items-center gap-1">
              <kbd className="rounded border bg-muted px-1 font-mono">Esc</kbd>关闭
            </span>
          </div>
        </div>
      ) : null}
    </CommandPrimitive>
  );
}
