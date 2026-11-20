"use client";

import Link from "next/link";
import {
  ArrowUpRight,
  FormInput,
  History,
  Info,
  Lock,
  LogIn,
  Plus,
  ShieldQuestion,
  Trash2,
  TriangleAlert,
  UserCheck
} from "lucide-react";
import type { RegistrationField } from "@/lib/api/app-auth-protocol";
import { useAppOAuthProvidersQuery } from "@/lib/admin-hooks";
import { FieldGroup, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  FIELD_TYPES,
  IDENTIFIERS,
  InlineNotice,
  LOGIN_METHODS,
  MethodToggle,
  REGISTER_METHODS,
  RESERVED_FIELDS,
  toggleValue,
  type PolicyDraft
} from "./auth-protocol-shared";

/**
 * 认证策略：开放哪些登录、注册方式，按什么标识登录，注册表单收哪些字段。
 *
 * 方式勾选下面紧跟着它依赖的别处配置（第三方渠道、短信服务商），配置分在两个区块时
 * 最容易出的事是「这边开了、那边没配」，客户端一个入口都没有而两边看上去都正常。
 */
export function AuthProtocolPolicy({
  appKey,
  draft,
  update
}: {
  appKey: string;
  draft: PolicyDraft;
  update: (patch: Partial<PolicyDraft>) => void;
}) {
  const providersQuery = useAppOAuthProvidersQuery(appKey);
  const enabledProviders = (providersQuery.data?.items ?? []).filter((item) => item.enabled && item.ready);
  const oauthOn = draft.loginMethods.includes("oauth");
  const smsOn = draft.loginMethods.includes("sms") || draft.registerMethods.includes("sms");
  const phoneMissing = smsOn && !draft.identifiers.includes("phone");
  const sectionHref = (tab: string) => `/apps/${encodeURIComponent(appKey)}?tab=${tab}`;

  const jump = (tab: string, label: string) => (
    <Link
      href={sectionHref(tab)}
      className="inline-flex shrink-0 items-center gap-0.5 font-medium text-foreground underline-offset-2 hover:underline"
    >
      {label}
      <ArrowUpRight className="size-3" />
    </Link>
  );

  return (
    <div className="space-y-4">
      <SectionCard icon={<LogIn className="size-4" />} title="登录与注册">
        <div className="space-y-6">
          <FieldGroup label="登录方式">
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
              {LOGIN_METHODS.map((option) => (
                <MethodToggle
                  key={option.value}
                  option={option}
                  checked={draft.loginMethods.includes(option.value)}
                  onToggle={() => update({ loginMethods: toggleValue(draft.loginMethods, option.value) })}
                />
              ))}
            </div>
            {oauthOn && !providersQuery.isLoading ? (
              enabledProviders.length ? (
                <InlineNotice icon={Info} action={jump("oauth", "管理渠道")}>
                  已启用 {enabledProviders.length} 个第三方渠道：{enabledProviders.map((item) => item.displayName).join("、")}
                </InlineNotice>
              ) : (
                <InlineNotice tone="warning" icon={TriangleAlert} action={jump("oauth", "添加渠道")}>
                  尚无可用的第三方渠道
                </InlineNotice>
              )
            ) : null}
            {!oauthOn && enabledProviders.length ? (
              <InlineNotice tone="warning" icon={TriangleAlert}>
                已配置 {enabledProviders.length} 个第三方渠道，未勾选第三方时不会下发到客户端
              </InlineNotice>
            ) : null}
          </FieldGroup>

          <FieldGroup label="注册方式" hint="第三方与卡密的建号在各自区块设置">
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
              {REGISTER_METHODS.map((option) => (
                <MethodToggle
                  key={option.value}
                  option={option}
                  checked={draft.registerMethods.includes(option.value)}
                  onToggle={() => update({ registerMethods: toggleValue(draft.registerMethods, option.value) })}
                />
              ))}
            </div>
            {smsOn ? (
              <InlineNotice icon={Info} action={jump("captcha", "验证码设置")}>
                短信需配置短信服务商
              </InlineNotice>
            ) : null}
          </FieldGroup>

          <FieldGroup label="登录标识">
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
              {IDENTIFIERS.map((option) => (
                <MethodToggle
                  key={option.value}
                  option={option}
                  checked={draft.identifiers.includes(option.value)}
                  onToggle={() => update({ identifiers: toggleValue(draft.identifiers, option.value) })}
                />
              ))}
            </div>
            <InlineNotice icon={Info}>
              密码登录时，账号框可输入用户名、号码、邮箱、手机号或 16 位对外 ID，按勾选的标识识别用户；账号名始终可用；号码只在本应用内识别，勾选对外 ID 时同样可用号码登录
            </InlineNotice>
            {phoneMissing ? (
              <InlineNotice tone="warning" icon={TriangleAlert}>
                已开启短信，登录标识需包含手机号
              </InlineNotice>
            ) : null}
          </FieldGroup>
        </div>
      </SectionCard>

      <SectionCard icon={<UserCheck className="size-4" />} title="行为">
        <div className="grid gap-2 md:grid-cols-3">
          <SwitchRow
            icon={<ShieldQuestion className="size-4" />}
            label="登录与注册要求验证码"
            checked={draft.requireCaptcha}
            onChange={(requireCaptcha) => update({ requireCaptcha })}
          />
          <SwitchRow
            icon={<LogIn className="size-4" />}
            label="注册后自动登录"
            checked={draft.autoLoginAfterRegister}
            onChange={(autoLoginAfterRegister) => update({ autoLoginAfterRegister })}
          />
          <SwitchRow
            icon={<History className="size-4" />}
            label="保留旧接口 /api/auth/*"
            checked={draft.allowLegacy}
            onChange={(allowLegacy) => update({ allowLegacy })}
          />
        </div>
      </SectionCard>

      <RegistrationFields
        fields={draft.registrationSchema}
        onChange={(registrationSchema) => update({ registrationSchema })}
      />
    </div>
  );
}

/**
 * 注册字段：一个字段一行，名称、显示名、类型、必填与客户端可写。
 * account / password / nickname 是内置字段，名称锁定、不能删除。
 */
function RegistrationFields({
  fields,
  onChange
}: {
  fields: RegistrationField[];
  onChange: (fields: RegistrationField[]) => void;
}) {
  const patch = (index: number, next: Partial<RegistrationField>) =>
    onChange(fields.map((field, position) => (position === index ? { ...field, ...next } : field)));

  return (
    <SectionCard
      icon={<FormInput className="size-4" />}
      title="注册字段"
      aside={
        <Button
          size="sm"
          variant="outline"
          className="h-8 gap-1.5"
          onClick={() => onChange([...fields, { name: "", type: "text", required: false, mutable: true }])}
        >
          <Plus className="size-3.5" />
          新增字段
        </Button>
      }
    >
      {fields.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">暂无注册字段</p>
      ) : (
        <div className="space-y-2">
          <div className="hidden grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)_8rem_auto_2rem] gap-3 px-3 text-[11px] text-muted-foreground lg:grid">
            <span>字段名</span>
            <span>显示名</span>
            <span>类型</span>
            <span>约束</span>
            <span />
          </div>
          {fields.map((field, index) => {
            const reserved = RESERVED_FIELDS.has(field.name);
            return (
              <div
                key={index}
                className="grid gap-2 rounded-xl bg-muted px-3 py-2.5 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)_8rem_auto_2rem] lg:items-center lg:gap-3"
              >
                <div className="relative">
                  <Input
                    value={field.name}
                    disabled={reserved}
                    placeholder="company"
                    aria-label="字段名"
                    className="h-8 bg-background pr-14 font-mono text-xs"
                    onChange={(event) => patch(index, { name: event.target.value.trim() })}
                  />
                  {reserved ? (
                    <Badge variant="outline" size="sm" className="absolute right-1.5 top-1/2 -translate-y-1/2 gap-1">
                      <Lock className="size-2.5" />
                      内置
                    </Badge>
                  ) : null}
                </div>
                <Input
                  value={field.label ?? ""}
                  placeholder="显示名"
                  aria-label="显示名"
                  className="h-8 bg-background text-xs"
                  onChange={(event) => patch(index, { label: event.target.value })}
                />
                <Select value={field.type || "text"} onValueChange={(type) => patch(index, { type })}>
                  <SelectTrigger className="h-8 bg-background text-xs" aria-label="类型">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {FIELD_TYPES.map((type) => (
                      <SelectItem key={type.value} value={type.value}>
                        {type.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className="flex items-center gap-4">
                  <label className="flex cursor-pointer items-center gap-1.5 text-xs">
                    <Checkbox
                      checked={field.required}
                      onCheckedChange={(checked) => patch(index, { required: checked === true })}
                    />
                    必填
                  </label>
                  <label className="flex cursor-pointer items-center gap-1.5 text-xs whitespace-nowrap">
                    <Checkbox
                      checked={field.mutable}
                      onCheckedChange={(checked) => patch(index, { mutable: checked === true })}
                    />
                    客户端可写
                  </label>
                </div>
                <div className="flex justify-end">
                  {reserved ? null : (
                    <Button
                      size="icon"
                      variant="ghost"
                      className="size-8"
                      aria-label={`删除字段 ${field.name || index + 1}`}
                      onClick={() => onChange(fields.filter((_, position) => position !== index))}
                    >
                      <Trash2 className="size-3.5 text-destructive" />
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </SectionCard>
  );
}
