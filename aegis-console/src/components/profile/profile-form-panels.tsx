"use client";

import * as React from "react";
import { CalendarDays, ChevronsUpDown, Check, IdCard, Mail, Phone, Plus, Trash2, UserRound } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList
} from "@/components/ui/command";
import { CountryFlag } from "@/components/ui/country-flag";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Panel } from "@/components/users/detail/user-detail-shared";
import {
  BIO_MAX,
  DISPLAY_NAME_MAX,
  Field,
  contactPlatformOf,
  contactPlatforms,
  countryCodes,
  fromDateInput,
  nextContactUid,
  toDateInput,
  type ContactDraft,
  type ProfileForm,
  type ProfileIssues
} from "@/components/profile/profile-shared";
import { cn } from "@/lib/utils";

type Patch = <K extends keyof ProfileForm>(key: K, value: ProfileForm[K]) => void;

/* ------------------------------------------------------------------ */
/*  区号选择：130 个国家不能塞进一个滚动列表                              */
/* ------------------------------------------------------------------ */

/**
 * 国际区号选择器。
 *
 * 原来是个原生 `Select`，130 个国家全在里面滚 —— 找"新西兰"要拖到底。
 * 换成可搜索的 combobox 之后，中文名、拼音、英文名、`+64` 四种输入都能命中，
 * 因为没人能确定自己记得的是哪一种。
 */
function CountryCombobox({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const [open, setOpen] = React.useState(false);
  const [keyword, setKeyword] = React.useState("");

  const current = countryCodes.find((item) => item.id === value) || countryCodes[0];
  const query = keyword.trim().toLowerCase();
  const filtered = query
    ? countryCodes.filter(
        (item) =>
          item.label.includes(keyword.trim()) ||
          item.code.includes(query) ||
          item.id.toLowerCase().includes(query) ||
          item.pinyin.includes(query)
      )
    : countryCodes;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="h-9 w-36 shrink-0 justify-between px-2.5 text-xs font-normal"
        >
          <span className="flex min-w-0 items-center gap-1.5">
            <CountryFlag code={current.iso} size={14} />
            <span className="font-mono">{current.code}</span>
            <span className="truncate text-muted-foreground">{current.label}</span>
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="搜国家、拼音或 +区号"
            value={keyword}
            onValueChange={setKeyword}
            className="text-xs"
          />
          <CommandList>
            {filtered.length === 0 ? <CommandEmpty className="py-6 text-xs">没有匹配的国家或地区</CommandEmpty> : null}
            <CommandGroup>
              {filtered.map((item) => (
                <CommandItem
                  key={item.id}
                  value={item.id}
                  onSelect={() => {
                    onChange(item.id);
                    setKeyword("");
                    setOpen(false);
                  }}
                  className="gap-2 text-xs"
                >
                  <Check className={cn("size-3.5", value === item.id ? "opacity-100" : "opacity-0")} />
                  <CountryFlag code={item.iso} size={14} />
                  <span className="w-12 font-mono">{item.code}</span>
                  <span className="flex-1 truncate">{item.label}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

/* ------------------------------------------------------------------ */
/*  生日：日历要能直接跳年份                                             */
/* ------------------------------------------------------------------ */

/**
 * 出生日期。
 *
 * `<input type="date">` 的样式、格式与图标由浏览器决定，深色模式下在 Chrome 上
 * 是一个白底控件，和整个控制台格格不入；而且要一个月一个月往回翻到 1990 年。
 * 这里用日历 + 年月下拉，`captionLayout="dropdown"` 的槽位样式在调用点补齐 ——
 * `ui/calendar.tsx` 的默认 classNames 只覆盖了 label 版的标题。
 */
function BirthdayPicker({
  value,
  onChange,
  invalid
}: {
  value: string;
  onChange: (value: string) => void;
  invalid?: boolean;
}) {
  const [open, setOpen] = React.useState(false);
  const selected = fromDateInput(value);
  const today = new Date();

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          aria-invalid={invalid}
          className={cn(
            "h-9 w-full justify-start gap-2 px-3 text-sm font-normal",
            !selected && "text-muted-foreground"
          )}
        >
          <CalendarDays className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="tabular-nums">{selected ? value : "选择日期"}</span>
          {selected ? (
            <span
              role="button"
              tabIndex={0}
              aria-label="清除出生日期"
              className="ml-auto rounded px-1 text-[11px] text-muted-foreground hover:text-foreground"
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onChange("");
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  event.stopPropagation();
                  onChange("");
                }
              }}
            >
              清除
            </span>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-auto p-0">
        <Calendar
          mode="single"
          captionLayout="dropdown"
          startMonth={new Date(1920, 0)}
          endMonth={today}
          defaultMonth={selected ?? new Date(today.getFullYear() - 25, today.getMonth())}
          selected={selected}
          disabled={{ after: today }}
          onSelect={(date) => {
            onChange(date ? toDateInput(date) : "");
            setOpen(false);
          }}
          classNames={{
            // ui/calendar.tsx 没有覆盖 dropdown 版标题的槽位，这里补齐；
            // 键名对不上不会报错，只会静默丢样式（见该文件顶部注释）
            dropdowns: "flex items-center justify-center gap-1.5",
            dropdown_root: "relative inline-flex items-center gap-1 rounded-md border bg-background px-2 py-1",
            dropdown: "absolute inset-0 cursor-pointer opacity-0",
            caption_label: "flex items-center gap-1 text-xs font-medium",
            // 选中态：v10 把 data-selected / aria-selected 放在**单元格**上，
            // 而 ui/calendar.tsx 的 `aria-selected:` 规则写在 day_button 上，
            // 那条选择器永远不命中 —— 表现为"点了日期但没有任何高亮"。
            // 这里按真实结构从单元格选中里面的按钮。单选没有区间槽位，不会打架。
            selected: "[&>button]:bg-primary [&>button]:font-medium [&>button]:text-primary-foreground"
          }}
        />
      </PopoverContent>
    </Popover>
  );
}

/* ------------------------------------------------------------------ */
/*  基本信息                                                            */
/* ------------------------------------------------------------------ */

export function BasicInfoPanel({
  form,
  issues,
  patch
}: {
  form: ProfileForm;
  issues: ProfileIssues;
  patch: Patch;
}) {
  const bioLength = form.bio.trim().length;

  return (
    <Panel
      title="基本信息"
      icon={<UserRound className="size-4" />}
      description="显示名称和简介会出现在成员列表、审批记录与操作日志里，其他人靠它认出你。"
      bodyClassName="space-y-5 px-5 py-5"
    >
      <div className="grid gap-x-5 gap-y-4 sm:grid-cols-2">
        <Field
          label="显示名称"
          htmlFor="profile-display-name"
          icon={<IdCard className="size-3" />}
          error={issues.errors.displayName}
          notice={issues.notices.displayName}
          hint="留空时别人只能看到你的登录账号"
          counter={
            <span className="text-[11px] tabular-nums text-muted-foreground">
              {form.displayName.trim().length}/{DISPLAY_NAME_MAX}
            </span>
          }
        >
          <Input
            id="profile-display-name"
            className="h-9"
            value={form.displayName}
            aria-invalid={Boolean(issues.errors.displayName)}
            placeholder="张三"
            onChange={(event) => patch("displayName", event.target.value)}
          />
        </Field>

        <Field
          label="邮箱"
          htmlFor="profile-email"
          icon={<Mail className="size-3" />}
          error={issues.errors.email}
          notice={issues.notices.email}
          hint="用于接收系统通知与找回入口"
        >
          <Input
            id="profile-email"
            type="email"
            className="h-9"
            value={form.email}
            aria-invalid={Boolean(issues.errors.email)}
            placeholder="name@example.com"
            onChange={(event) => patch("email", event.target.value)}
          />
        </Field>

        <Field
          label="手机号"
          htmlFor="profile-phone"
          icon={<Phone className="size-3" />}
          error={issues.errors.phone}
          notice={issues.notices.phone}
          hint="区号可搜中文名或拼音"
        >
          <div className="flex gap-2">
            <CountryCombobox value={form.countryId} onChange={(id) => patch("countryId", id)} />
            <Input
              id="profile-phone"
              className="h-9 flex-1"
              inputMode="tel"
              value={form.phone}
              aria-invalid={Boolean(issues.errors.phone)}
              placeholder="138 0000 0000"
              onChange={(event) => patch("phone", event.target.value.replace(/[^\d\s-]/g, ""))}
            />
          </div>
        </Field>

        <Field
          label="出生日期"
          error={issues.errors.birthday}
          notice={issues.notices.birthday}
          hint="只用于生日提醒，不参与任何风控判定"
        >
          <BirthdayPicker
            value={form.birthday}
            onChange={(next) => patch("birthday", next)}
            invalid={Boolean(issues.errors.birthday)}
          />
        </Field>
      </div>

      <Field
        label="个人简介"
        htmlFor="profile-bio"
        error={issues.errors.bio}
        notice={issues.notices.bio}
        counter={
          <span
            className={cn(
              "text-[11px] tabular-nums",
              bioLength > BIO_MAX ? "text-destructive" : "text-muted-foreground"
            )}
          >
            {bioLength}/{BIO_MAX}
          </span>
        }
      >
        <Textarea
          id="profile-bio"
          rows={3}
          className="resize-none text-sm"
          value={form.bio}
          aria-invalid={Boolean(issues.errors.bio)}
          placeholder="一句话说清你负责什么，比如：负责支付与对账，工作日 10:00–19:00 在线。"
          onChange={(event) => patch("bio", event.target.value)}
        />
      </Field>
    </Panel>
  );
}

/* ------------------------------------------------------------------ */
/*  联系方式                                                            */
/* ------------------------------------------------------------------ */

export function ContactsPanel({
  form,
  issues,
  patch
}: {
  form: ProfileForm;
  issues: ProfileIssues;
  patch: Patch;
}) {
  const update = (uid: string, next: Partial<ContactDraft>) => {
    patch(
      "contacts",
      form.contacts.map((item) => (item.uid === uid ? { ...item, ...next } : item))
    );
  };
  const remove = (uid: string) => {
    patch(
      "contacts",
      form.contacts.filter((item) => item.uid !== uid)
    );
  };
  const add = () => {
    const used = new Set(form.contacts.map((item) => item.platform));
    const fresh = contactPlatforms.find((item) => !used.has(item.value)) || contactPlatforms[0];
    patch("contacts", [...form.contacts, { uid: nextContactUid(), platform: fresh.value, value: "", label: "" }]);
  };

  return (
    <Panel
      title="联系方式"
      icon={<Mail className="size-4" />}
      description="出事时同事怎么找到你。这一段是唯一可以真正删空的 —— 其余字段留空只会保留原值。"
      action={
        form.contacts.length > 0 ? (
          <Badge variant="secondary" size="sm">
            {form.contacts.length} 条
          </Badge>
        ) : null
      }
      bodyClassName="space-y-2 px-5 py-5"
    >
      {form.contacts.length === 0 ? (
        <div className="rounded-lg border border-dashed px-4 py-8 text-center">
          <p className="text-sm text-muted-foreground">还没有留下任何联系方式</p>
          <p className="mt-1 text-xs text-muted-foreground/80">
            值班、告警升级、跨组协作时，同事只能靠这里找到你。
          </p>
          <Button variant="outline" size="sm" className="mt-3 h-8 gap-1.5 text-xs" onClick={add}>
            <Plus className="size-3.5" />
            添加第一条
          </Button>
        </div>
      ) : (
        <>
          {form.contacts.map((contact) => {
            const platform = contactPlatformOf(contact.platform);
            const error = issues.contactErrors[contact.uid];
            const dropping = error === "留空的这条在保存时会被删除";

            return (
              <div key={contact.uid} className="space-y-1">
                <div className="flex items-center gap-2">
                  <Select value={contact.platform} onValueChange={(next) => update(contact.uid, { platform: next })}>
                    <SelectTrigger className="h-9 w-36 shrink-0 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {contactPlatforms.map((item) => (
                        <SelectItem key={item.value} value={item.value} className="text-xs">
                          <item.Icon className="size-3.5 text-muted-foreground" />
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  <Input
                    className={cn("h-9 flex-1 text-sm", dropping && "text-muted-foreground")}
                    placeholder={platform.placeholder}
                    value={contact.value}
                    aria-invalid={Boolean(error) && !dropping}
                    aria-label={`${platform.label}账号`}
                    onChange={(event) => update(contact.uid, { value: event.target.value })}
                  />

                  <Input
                    className="h-9 w-28 shrink-0 text-sm"
                    placeholder="备注"
                    value={contact.label || ""}
                    aria-label={`${platform.label}备注`}
                    onChange={(event) => update(contact.uid, { label: event.target.value })}
                  />

                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`删除${platform.label}`}
                    className="size-9 shrink-0 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                    onClick={() => remove(contact.uid)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
                {error ? (
                  <p className={cn("pl-38 text-[11px] leading-4", dropping ? "text-muted-foreground" : "text-destructive")}>
                    {error}
                  </p>
                ) : null}
              </div>
            );
          })}

          <Button variant="outline" size="sm" className="mt-2 h-8 w-full gap-1.5 text-xs" onClick={add}>
            <Plus className="size-3.5" />
            添加联系方式
          </Button>
        </>
      )}
    </Panel>
  );
}
