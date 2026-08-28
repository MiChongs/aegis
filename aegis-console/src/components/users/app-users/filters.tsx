"use client";

import { useState } from "react";
import type { DateRange } from "react-day-picker";
import {
  BookmarkPlus,
  CalendarDays,
  ChevronDown,
  Download,
  Loader2,
  SlidersHorizontal,
  Star,
  Trash2,
  X
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { useUserViewStore, type SavedUserView } from "@/lib/app-users-view-store";
import type { AdminAppUserItem } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  DATE_PRESETS,
  PRECISE_FIELDS,
  activeFilters,
  clearAllFilters,
  clearFilter,
  parseDateInput,
  presetRange,
  toDateInput,
  type UserQueryState
} from "./shared";
import { SmartUserSearch } from "./smart-search";

/**
 * 筛选区：一行常用 + 一个抽屉装剩下的 + 一排「已筛」胶囊。
 *
 * 为什么精确字段要收进抽屉而不是平铺：六个输入框平铺会把主表挤下屏，
 * 而它们的使用频率与关键字搜索差一个数量级 —— 平时用不到，用到时是在查案
 *（按注册 IP 找同源小号、按邀请码查下线），那种场景下多点一下完全可接受。
 *
 * 「已筛」胶囊是必须的：条件收进抽屉之后，如果不在外面显式列出来，
 * 就会出现「列表明明有数据却显示 0 条」而管理员找不到原因的情况。
 */
export function AppUsersFilters({
  state,
  onChange,
  onExport,
  exporting,
  total,
  onOpenUser
}: {
  state: UserQueryState;
  onChange: (next: UserQueryState) => void;
  onExport: () => void;
  exporting: boolean;
  total: number;
  onOpenUser: (user: AdminAppUserItem) => void;
}) {
  const filters = activeFilters(state);
  const patch = (partial: Partial<UserQueryState>) => onChange({ ...state, ...partial, page: 1 });

  return (
    <div className="space-y-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <SmartUserSearch
          appKey={state.appKey}
          state={state}
          onChange={onChange}
          onOpenUser={onOpenUser}
        />

        <DateRangeFilter
          from={state.createdFrom}
          to={state.createdTo}
          onChange={(range) => patch(range)}
        />

        <MoreFiltersPopover state={state} onChange={onChange} />

        <div className="ml-auto flex items-center gap-2">
          <SavedViews state={state} onChange={onChange} />
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            disabled={exporting || !state.appKey}
            onClick={onExport}
          >
            {exporting ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
            导出 CSV
          </Button>
        </div>
      </div>

      {filters.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[11px] text-muted-foreground">已筛</span>
          {filters.map((filter) => (
            <Badge key={`${filter.key}-${filter.value}`} variant="secondary" size="sm" className="gap-1 pr-1">
              <span className="text-muted-foreground">{filter.label}</span>
              <span className="max-w-[160px] truncate">{filter.value}</span>
              <button
                type="button"
                aria-label={`移除 ${filter.label} 筛选`}
                className="rounded-full p-0.5 hover:bg-background/60"
                onClick={() => onChange(clearFilter(state, filter.key))}
              >
                <X className="size-3" />
              </button>
            </Badge>
          ))}
          <Button
            size="sm"
            variant="ghost"
            className="h-6 px-1.5 text-[11px] text-muted-foreground"
            onClick={() => onChange(clearAllFilters(state))}
          >
            清空全部
          </Button>
          <span className="ml-auto text-[11px] tabular-nums text-muted-foreground">
            命中 {total.toLocaleString("zh-CN")} 条
          </span>
        </div>
      ) : null}
    </div>
  );
}

// ── 注册时间范围 ──────────────────────────

function DateRangeFilter({
  from,
  to,
  onChange
}: {
  from: string;
  to: string;
  onChange: (range: { createdFrom: string; createdTo: string }) => void;
}) {
  const [open, setOpen] = useState(false);
  const selected: DateRange | undefined = from
    ? { from: parseDateInput(from), to: parseDateInput(to) }
    : undefined;

  const label = from || to ? `${from || "不限"} ~ ${to || "至今"}` : "注册时间";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          size="sm"
          variant="outline"
          data-active={Boolean(from || to)}
          className="h-8 gap-1.5 text-xs data-[active=true]:border-foreground/40"
        >
          <CalendarDays className="size-3.5" />
          <span className="max-w-[168px] truncate tabular-nums">{label}</span>
          <ChevronDown className="size-3 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-auto p-0">
        <div className="flex flex-wrap gap-1 border-b p-2">
          {DATE_PRESETS.map((preset) => (
            <Button
              key={preset.key}
              size="sm"
              variant="ghost"
              className="h-7 text-xs"
              onClick={() => {
                onChange(presetRange(preset.days));
                setOpen(false);
              }}
            >
              {preset.label}
            </Button>
          ))}
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-xs text-muted-foreground"
            onClick={() => {
              onChange({ createdFrom: "", createdTo: "" });
              setOpen(false);
            }}
          >
            不限
          </Button>
        </div>
        <Calendar
          mode="range"
          numberOfMonths={2}
          defaultMonth={selected?.from}
          selected={selected}
          onSelect={(range) =>
            onChange({
              createdFrom: range?.from ? toDateInput(range.from) : "",
              createdTo: range?.to ? toDateInput(range.to) : ""
            })
          }
        />
        <p className="border-t px-3 py-2 text-[11px] leading-4 text-muted-foreground">
          按注册时间筛选，含起止当天。只选起始日即「从这天到现在」。
        </p>
      </PopoverContent>
    </Popover>
  );
}

// ── 精确字段 ──────────────────────────

function MoreFiltersPopover({
  state,
  onChange
}: {
  state: UserQueryState;
  onChange: (next: UserQueryState) => void;
}) {
  const [open, setOpen] = useState(false);
  // 抽屉里的输入是**草稿**：每敲一个字就发一次请求既浪费也让人没法完整填一组条件。
  // 打开时从当前状态派生，点「应用」才提交 —— 与配置面板的草稿约定同源。
  const [draft, setDraft] = useState<Partial<UserQueryState> | null>(null);
  const form = draft ?? state;
  const activeCount = PRECISE_FIELDS.filter((field) => (state[field.key] as string).trim()).length;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setDraft(null);
      }}
    >
      <PopoverTrigger asChild>
        <Button
          size="sm"
          variant="outline"
          data-active={activeCount > 0}
          className="h-8 gap-1.5 text-xs data-[active=true]:border-foreground/40"
        >
          <SlidersHorizontal className="size-3.5" />
          更多条件
          {activeCount ? (
            <Badge variant="default" size="sm" className="ml-0.5 px-1">
              {activeCount}
            </Badge>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[340px] p-3">
        <div className="mb-2 text-xs text-muted-foreground">
          精确字段走等值 / 前缀匹配，与上面的关键字全字段模糊搜不是一回事。
        </div>
        <div className="grid gap-2.5">
          {PRECISE_FIELDS.map((field) => (
            <div key={field.key} className="grid grid-cols-[64px_minmax(0,1fr)] items-center gap-2">
              <Label className="text-xs text-muted-foreground">{field.label}</Label>
              <Input
                value={form[field.key] as string}
                placeholder={field.placeholder}
                className="h-8 text-xs"
                onChange={(event) => setDraft({ ...form, [field.key]: event.target.value })}
                onKeyDown={(event) => {
                  if (event.key !== "Enter") return;
                  onChange({ ...state, ...form, page: 1 });
                  setDraft(null);
                  setOpen(false);
                }}
              />
            </div>
          ))}
        </div>
        <Separator className="my-3" />
        <div className="flex justify-end gap-2">
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-xs"
            onClick={() => {
              const cleared = Object.fromEntries(
                PRECISE_FIELDS.map((field) => [field.key, ""])
              ) as Partial<UserQueryState>;
              onChange({ ...state, ...cleared, page: 1 });
              setDraft(null);
              setOpen(false);
            }}
          >
            清空
          </Button>
          <Button
            size="sm"
            className="h-7 text-xs"
            onClick={() => {
              onChange({ ...state, ...form, page: 1 });
              setDraft(null);
              setOpen(false);
            }}
          >
            应用
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ── 保存的视图 ──────────────────────────

function SavedViews({
  state,
  onChange
}: {
  state: UserQueryState;
  onChange: (next: UserQueryState) => void;
}) {
  const views = useUserViewStore((store) => store.views);
  const save = useUserViewStore((store) => store.save);
  const remove = useUserViewStore((store) => store.remove);
  const [name, setName] = useState("");
  const [open, setOpen] = useState(false);

  const scoped = views.filter((view) => view.appKey === state.appKey);

  function applyView(view: SavedUserView) {
    const params = new URLSearchParams(view.query);
    // 走 URL 而不是存结构化对象：新增筛选字段时旧视图自动继续可用
    onChange({
      ...state,
      ...paramsToPatch(params),
      page: 1
    });
    setOpen(false);
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button size="sm" variant="outline" className="h-8 gap-1.5 text-xs">
          <Star className="size-3.5" />
          视图
          {scoped.length ? (
            <Badge variant="secondary" size="sm" className="px-1">
              {scoped.length}
            </Badge>
          ) : null}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel className="text-xs">保存的筛选</DropdownMenuLabel>
        {scoped.length ? (
          scoped.map((view) => (
            <DropdownMenuItem
              key={view.id}
              className="justify-between gap-2 text-xs"
              onSelect={(event) => {
                event.preventDefault();
                applyView(view);
              }}
            >
              <span className="truncate">{view.name}</span>
              <button
                type="button"
                aria-label={`删除视图 ${view.name}`}
                className="shrink-0 text-muted-foreground hover:text-destructive"
                onClick={(event) => {
                  event.stopPropagation();
                  remove(view.id);
                }}
              >
                <Trash2 className="size-3" />
              </button>
            </DropdownMenuItem>
          ))
        ) : (
          <div className="px-2 py-3 text-center text-xs text-muted-foreground">
            还没有保存的视图
          </div>
        )}
        <DropdownMenuSeparator />
        <div className="flex items-center gap-1.5 p-1.5">
          <Input
            value={name}
            placeholder="给当前筛选起个名"
            className="h-7 text-xs"
            onChange={(event) => setName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Enter" || !name.trim() || !state.appKey) return;
              save({ name, appKey: state.appKey, query: buildQueryString(state) });
              setName("");
            }}
          />
          <Button
            size="icon"
            variant="ghost"
            className="size-7 shrink-0"
            aria-label="保存当前筛选为视图"
            disabled={!name.trim() || !state.appKey}
            onClick={() => {
              if (!state.appKey) return;
              save({ name, appKey: state.appKey, query: buildQueryString(state) });
              setName("");
            }}
          >
            <BookmarkPlus className="size-3.5" />
          </Button>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function buildQueryString(state: UserQueryState) {
  const params = new URLSearchParams();
  const put = (key: string, value: string) => {
    if (value.trim()) params.set(key, value.trim());
  };
  put("keyword", state.keyword);
  if (state.status !== "all") params.set("status", state.status);
  for (const field of PRECISE_FIELDS) put(field.key, state[field.key] as string);
  put("createdFrom", state.createdFrom);
  put("createdTo", state.createdTo);
  return params.toString();
}

/** 视图只覆盖筛选维度，不带排序与分页 —— 那两项属于"我现在怎么看"，不属于"我要看谁"。 */
function paramsToPatch(params: URLSearchParams): Partial<UserQueryState> {
  const text = (key: string) => params.get(key)?.trim() ?? "";
  const status = params.get("status");
  return {
    keyword: text("keyword"),
    status: status === "enabled" || status === "disabled" ? status : "all",
    account: text("account"),
    nickname: text("nickname"),
    email: text("email"),
    phone: text("phone"),
    inviteCode: text("inviteCode"),
    registerIp: text("registerIp"),
    markcode: text("markcode"),
    customId: text("customId"),
    createdFrom: text("createdFrom"),
    createdTo: text("createdTo")
  };
}
