"use client";

import { useState } from "react";
import { Fingerprint, Info, LogIn, Power, RotateCcw, Save, UserPlus } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { FieldGroup, NoAppSelected, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import { AppKeyText, formatAppDate, formatCount } from "@/components/apps/app-shared";
import { useAdminAppQuery, useAdminAppStatsQuery, useUpdateAdminAppMutation } from "@/lib/admin-hooks";
import type { AppDetail } from "@/lib/api/types";

/**
 * 应用概览：标识 + 三个开关 + 当前规模。
 *
 * 三个开关做成开关行而不是下拉框，是因为它们本质上是「开 / 关」：
 * 下拉框把二元状态包装成一次「展开 → 找选项 → 点击」的三步操作，
 * 而且关闭态与开启态在视觉上一模一样，扫一眼看不出这个应用现在能不能登录。
 *
 * 关闭说明只在对应开关关闭时出现 —— 它是要发给终端用户看的文案，
 * 开关开着时留着那个输入框只会让人以为它还在生效。
 */

type Form = {
  name: string;
  status: boolean;
  registerStatus: boolean;
  loginStatus: boolean;
  disabledReason: string;
  disabledRegisterReason: string;
  disabledLoginReason: string;
};

function seed(app?: AppDetail | null): Form {
  return {
    name: app?.name ?? "",
    status: app?.status ?? true,
    registerStatus: app?.registerStatus ?? true,
    loginStatus: app?.loginStatus ?? true,
    disabledReason: app?.disabledReason ?? "",
    disabledRegisterReason: app?.disabledRegisterReason ?? "",
    disabledLoginReason: app?.disabledLoginReason ?? ""
  };
}

export function AppInfoPanel({ appKey }: { appKey?: string | null }) {
  const query = useAdminAppQuery(appKey);
  const statsQuery = useAdminAppStatsQuery(appKey);
  const mutation = useUpdateAdminAppMutation(appKey);
  const app = query.data;
  const stats = statsQuery.data;

  // 草稿按 appKey 绑定作用域：切换应用时上一个应用的未保存改动不会串过来（不用 effect 同步）
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const scope = appKey ?? "";
  const server = seed(app);
  const form = draft?.scope === scope ? draft.value : server;
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const dirty = (Object.keys(server) as (keyof Form)[]).some((key) => form[key] !== server[key]);

  async function handleSave() {
    if (!form.name.trim()) {
      toast.error("应用名称不能为空");
      return;
    }
    try {
      await mutation.mutateAsync({
        name: form.name.trim(),
        status: form.status,
        registerStatus: form.registerStatus,
        loginStatus: form.loginStatus,
        disabledReason: form.disabledReason,
        disabledRegisterReason: form.disabledRegisterReason,
        disabledLoginReason: form.disabledLoginReason
      });
      setDraft(null); // 清掉草稿，重新从服务端值派生
      toast.success("已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (!appKey) return <NoAppSelected icon={<Info className="size-8" />} />;
  if (query.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-28 w-full rounded-2xl" />
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    );
  }
  if (!app) return <div className="py-12 text-center text-sm text-muted-foreground">应用不存在</div>;

  return (
    <div className="space-y-4">
      {/* 当前规模：概览页要先回答「这个应用现在多大、今天活不活」，再谈配置 */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        <Metric label="总用户" value={stats?.totalUsers} loading={statsQuery.isLoading} />
        <Metric label="启用" value={stats?.enabledUsers} loading={statsQuery.isLoading} />
        <Metric label="停用" value={stats?.disabledUsers} loading={statsQuery.isLoading} />
        <Metric label="今日新增" value={stats?.newUsersToday} loading={statsQuery.isLoading} />
        <Metric label="今日登录" value={stats?.loginSuccessToday} loading={statsQuery.isLoading} />
        <Metric label="今日失败" value={stats?.loginFailureToday} loading={statsQuery.isLoading} />
      </div>

      <SectionCard
        icon={<Fingerprint className="size-4" />}
        title="应用标识"
        description="AppKey 由后端生成，创建后不可更改；接入方所有请求都靠它定位应用。"
      >
        <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-4">
          <InfoCell label="应用 ID" value={String(app.id)} mono />
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-wider text-muted-foreground">应用 KEY</div>
            <div className="mt-0.5">
              <AppKeyText appKey={app.appKey} />
            </div>
          </div>
          <InfoCell label="创建时间" value={formatAppDate(app.createdAt)} />
          <InfoCell label="更新时间" value={formatAppDate(app.updatedAt)} />
        </div>
      </SectionCard>

      <SectionCard
        icon={<Power className="size-4" />}
        title="基本信息与开关"
        description="三个开关互相独立：应用停用时登录与注册一并不可用，单独关闭注册不影响老用户登录。"
        footer={
          <div className="flex items-center justify-between gap-2">
            <span className="text-[11px] text-muted-foreground">
              {dirty ? "有未保存的修改" : "配置与服务端一致"}
            </span>
            <div className="flex items-center gap-2">
              {dirty && (
                <Button size="sm" variant="ghost" className="h-8 gap-1 text-xs" onClick={() => setDraft(null)}>
                  <RotateCcw className="size-3" />
                  重置
                </Button>
              )}
              <Button
                size="sm"
                className="h-8 gap-1 text-xs"
                disabled={mutation.isPending || !dirty}
                onClick={handleSave}
              >
                <Save className="size-3" />
                {mutation.isPending ? "保存中..." : "保存配置"}
              </Button>
            </div>
          </div>
        }
      >
        <div className="space-y-4">
          <FieldGroup label="应用名称" hint="展示给管理员与终端用户">
            <Input
              className="h-9 text-sm"
              value={form.name}
              placeholder="我的应用"
              onChange={(event) => patch("name", event.target.value)}
            />
          </FieldGroup>

          <FieldGroup label="运行开关">
            <div className="grid gap-2 lg:grid-cols-3">
              <SwitchRow
                label="应用启用"
                hint="关闭后该应用的全部接口停止服务"
                icon={<Power className="size-3.5" />}
                checked={form.status}
                onChange={(value) => patch("status", value)}
              />
              <SwitchRow
                label="开放注册"
                hint="关闭后新用户无法注册"
                icon={<UserPlus className="size-3.5" />}
                checked={form.registerStatus}
                onChange={(value) => patch("registerStatus", value)}
              />
              <SwitchRow
                label="开放登录"
                hint="关闭后已有用户也无法登录"
                icon={<LogIn className="size-3.5" />}
                checked={form.loginStatus}
                onChange={(value) => patch("loginStatus", value)}
              />
            </div>
          </FieldGroup>

          {(!form.status || !form.registerStatus || !form.loginStatus) && (
            <FieldGroup label="关闭说明" hint="会随拒绝响应返回给客户端">
              <div className="grid gap-2 lg:grid-cols-3">
                {!form.status && (
                  <ReasonField
                    label="应用停用说明"
                    value={form.disabledReason}
                    onChange={(value) => patch("disabledReason", value)}
                  />
                )}
                {!form.registerStatus && (
                  <ReasonField
                    label="注册关闭说明"
                    value={form.disabledRegisterReason}
                    onChange={(value) => patch("disabledRegisterReason", value)}
                  />
                )}
                {!form.loginStatus && (
                  <ReasonField
                    label="登录关闭说明"
                    value={form.disabledLoginReason}
                    onChange={(value) => patch("disabledLoginReason", value)}
                  />
                )}
              </div>
            </FieldGroup>
          )}
        </div>
      </SectionCard>
    </div>
  );
}

function Metric({ label, value, loading }: { label: string; value?: number | null; loading?: boolean }) {
  return (
    <div className="rounded-xl border border-border bg-card px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      {loading ? (
        <Skeleton className="mt-1 h-5 w-12 rounded" />
      ) : (
        <div className="mt-0.5 text-lg font-semibold tabular-nums">{formatCount(value)}</div>
      )}
    </div>
  );
}

function InfoCell({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className={`mt-0.5 truncate text-sm font-medium ${mono ? "font-mono" : ""}`}>{value}</div>
    </div>
  );
}

function ReasonField({
  label,
  value,
  onChange
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-[11px] font-medium">{label}</Label>
      <Input className="h-9 text-sm" placeholder="例如：系统升级中，预计 12:00 恢复" value={value} onChange={(event) => onChange(event.target.value)} />
    </div>
  );
}
