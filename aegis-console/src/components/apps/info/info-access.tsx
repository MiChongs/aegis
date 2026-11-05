"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { LogIn, MessageSquareWarning, Power, RotateCcw, Save, ShieldCheck, UserPlus, type LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { DetailPanel, TonalIcon } from "@/components/apps/detail/app-detail-kit";
import { useUpdateAdminAppMutation } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import type { AppDetail } from "@/lib/api/types";

/**
 * 名称与访问开关。
 *
 * 三个开关有主从关系：服务停用时所有接口都拒绝，注册、登录开着也不生效。
 * 这里把这层关系直接写在开关上（「随服务停用」），免得管理员以为登录还开着。
 *
 * 关闭某个开关时就地展开说明输入框，并预览客户端会读到的那句话：说明是写给终端用户的，
 * 只在对应开关关闭时有意义，开着时不留输入框，免得让人以为它还在生效。
 *
 * 草稿按 appKey 绑定作用域，切换应用时上一个应用的未保存改动不会串过来；
 * Ctrl/⌘+S 直接保存；窄屏有未保存修改时底部浮出保存条。
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

const NAME_LIMIT = 64;

function seed(app: AppDetail): Form {
  return {
    name: app.name ?? "",
    status: app.status ?? true,
    registerStatus: app.registerStatus ?? true,
    loginStatus: app.loginStatus ?? true,
    disabledReason: app.disabledReason ?? "",
    disabledRegisterReason: app.disabledRegisterReason ?? "",
    disabledLoginReason: app.disabledLoginReason ?? ""
  };
}

const FIELD_LABEL: Record<keyof Form, string> = {
  name: "名称",
  status: "应用启用",
  registerStatus: "开放注册",
  loginStatus: "开放登录",
  disabledReason: "停用说明",
  disabledRegisterReason: "注册关闭说明",
  disabledLoginReason: "登录关闭说明"
};

export function InfoAccess({ app }: { app: AppDetail }) {
  const mutation = useUpdateAdminAppMutation(app.appKey);
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const scope = app.appKey;
  const server = seed(app);
  const form = draft?.scope === scope ? draft.value : server;
  const patch = <K extends keyof Form>(key: K, value: Form[K]) => setDraft({ scope, value: { ...form, [key]: value } });

  const changed = (Object.keys(server) as (keyof Form)[]).filter((key) => form[key] !== server[key]);
  const dirty = changed.length > 0;
  const nameError = !form.name.trim() ? "名称不能为空" : form.name.length > NAME_LIMIT ? `不超过 ${NAME_LIMIT} 个字符` : null;

  async function handleSave() {
    if (!dirty || mutation.isPending) return;
    if (nameError) {
      toast.error(`应用${nameError}`);
      return;
    }
    try {
      await mutation.mutateAsync({
        name: form.name.trim(),
        status: form.status,
        registerStatus: form.registerStatus,
        loginStatus: form.loginStatus,
        disabledReason: form.disabledReason.trim(),
        disabledRegisterReason: form.disabledRegisterReason.trim(),
        disabledLoginReason: form.disabledLoginReason.trim()
      });
      setDraft(null);
      toast.success("已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  // Ctrl/⌘+S：引用最新的保存函数，监听只挂一次
  const saveRef = useRef(handleSave);
  useEffect(() => {
    saveRef.current = handleSave;
  });
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        void saveRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const exposure = !form.status
    ? { label: "全部接口拒绝服务", tone: "text-red-600 dark:text-red-400", dot: "bg-red-500" }
    : !form.registerStatus || !form.loginStatus
      ? { label: "部分入口关闭", tone: "text-amber-700 dark:text-amber-300", dot: "bg-amber-500" }
      : { label: "全部开放", tone: "text-emerald-700 dark:text-emerald-300", dot: "bg-emerald-500" };

  const actions = (
    <>
      {dirty && (
        <Button size="sm" variant="ghost" className="h-9 gap-1 text-xs sm:h-8" onClick={() => setDraft(null)}>
          <RotateCcw className="size-3" />
          放弃修改
        </Button>
      )}
      <Button size="sm" className="h-9 gap-1 text-xs sm:h-8" disabled={mutation.isPending || !dirty} onClick={() => void handleSave()}>
        <Save className="size-3" />
        {mutation.isPending ? "保存中" : "保存配置"}
      </Button>
    </>
  );

  return (
    <DetailPanel
      icon={ShieldCheck}
      tone="emerald"
      title="名称与访问"
      description="应用名称，以及服务、注册、登录三项开关"
      aside={
        <span className={cn("inline-flex items-center gap-1.5 rounded-full bg-muted/70 px-2 py-0.5 text-[11px] font-medium", exposure.tone)}>
          <span className={cn("size-1.5 rounded-full", exposure.dot)} />
          {exposure.label}
        </span>
      }
      footer={
        <footer className="hidden items-center justify-between gap-3 border-t border-border bg-muted/25 px-5 py-3 sm:flex">
          <span className={cn("min-w-0 truncate text-[11px]", dirty ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground")}>
            {dirty ? `${changed.length} 项未保存：${changed.map((key) => FIELD_LABEL[key]).join("、")}` : "与服务端一致"}
            {dirty && <kbd className="ml-2 rounded border border-border bg-background px-1 font-mono text-[10px] text-muted-foreground">Ctrl S</kbd>}
          </span>
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        </footer>
      }
    >
      <div className="space-y-4">
        <label className="block">
          <span className="mb-1.5 flex items-center justify-between text-[11px]">
            <span className="font-medium text-muted-foreground">应用名称</span>
            <span className={cn("tabular-nums", form.name.length > NAME_LIMIT ? "text-red-500" : "text-muted-foreground/70")}>
              {form.name.length}/{NAME_LIMIT}
            </span>
          </span>
          <Input
            className={cn("h-10 text-base sm:text-sm", nameError && "border-red-500/60 focus-visible:ring-red-500/30")}
            value={form.name}
            placeholder="我的应用"
            aria-invalid={Boolean(nameError)}
            onChange={(event) => patch("name", event.target.value)}
          />
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {nameError ? <span className="text-red-500">{nameError}</span> : "显示在控制台、邮件与授权页，用户可见"}
          </span>
        </label>

        <div className="overflow-hidden rounded-xl border border-border">
          <AccessRow
            icon={Power}
            title="应用启用"
            onHint="客户端可正常调用全部接口"
            offHint="所有接口拒绝服务"
            reasonLabel="停用说明"
            code={40310}
            fallback="应用已被禁用"
            checked={form.status}
            changed={form.status !== server.status}
            reason={form.disabledReason}
            onToggle={(value) => patch("status", value)}
            onReason={(value) => patch("disabledReason", value)}
          />
          <AccessRow
            icon={UserPlus}
            title="开放注册"
            onHint="新用户可以注册账号"
            offHint="注册入口关闭，已有账号不受影响"
            reasonLabel="注册关闭说明"
            code={40312}
            fallback="当前应用暂时关闭注册"
            checked={form.registerStatus}
            changed={form.registerStatus !== server.registerStatus}
            overridden={!form.status}
            reason={form.disabledRegisterReason}
            onToggle={(value) => patch("registerStatus", value)}
            onReason={(value) => patch("disabledRegisterReason", value)}
          />
          <AccessRow
            icon={LogIn}
            title="开放登录"
            onHint="已有用户可以登录"
            offHint="登录入口关闭"
            reasonLabel="登录关闭说明"
            code={40311}
            fallback="当前应用暂时关闭登录"
            checked={form.loginStatus}
            changed={form.loginStatus !== server.loginStatus}
            overridden={!form.status}
            reason={form.disabledLoginReason}
            onToggle={(value) => patch("loginStatus", value)}
            onReason={(value) => patch("disabledLoginReason", value)}
          />
        </div>
      </div>

      {dirty && <MobileSaveBar count={changed.length}>{actions}</MobileSaveBar>}
    </DetailPanel>
  );
}

function AccessRow({
  icon,
  title,
  onHint,
  offHint,
  reasonLabel,
  code,
  fallback,
  checked,
  changed,
  overridden,
  reason,
  onToggle,
  onReason
}: {
  icon: LucideIcon;
  title: string;
  onHint: string;
  offHint: string;
  reasonLabel: string;
  /** 后端拒绝时的错误码与未填说明时的默认文案（AppService.Ensure*Allowed） */
  code: number;
  fallback: string;
  checked: boolean;
  changed: boolean;
  /** 服务已停用：本开关开着也不生效 */
  overridden?: boolean;
  reason: string;
  onToggle: (value: boolean) => void;
  onReason: (value: string) => void;
}) {
  const effective = checked && !overridden;
  return (
    <div
      className={cn(
        "border-b border-border/70 transition-colors duration-200 last:border-b-0",
        !checked && "bg-amber-500/[0.04]"
      )}
    >
      <label className="flex cursor-pointer items-center gap-3 px-3.5 py-3">
        <TonalIcon icon={icon} tone={effective ? "emerald" : checked ? "muted" : "amber"} />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            <span className="truncate text-xs font-semibold">{title}</span>
            {changed && <span className="size-1.5 shrink-0 rounded-full bg-amber-500" title="未保存" />}
            {overridden && checked && (
              <span className="shrink-0 rounded-full bg-muted px-1.5 text-[10px] leading-4 text-muted-foreground">随服务停用</span>
            )}
          </span>
          <span className="block truncate text-[11px] text-muted-foreground">{checked ? onHint : offHint}</span>
        </span>
        <Switch checked={checked} onCheckedChange={onToggle} aria-label={title} />
      </label>

      {!checked && (
        <div className="grid gap-2.5 px-3.5 pb-3.5 sm:pl-[3.75rem]">
          <label className="block space-y-1">
            <span className="text-[10px] font-medium text-muted-foreground">{reasonLabel}（展示给用户）</span>
            <Input
              className="h-9 bg-background text-base sm:text-xs"
              placeholder="例如：系统升级中，预计 12:00 恢复"
              maxLength={200}
              value={reason}
              onChange={(event) => onReason(event.target.value)}
            />
          </label>
          <div className="flex items-start gap-2 rounded-lg border border-dashed border-border bg-background/70 px-2.5 py-2">
            <MessageSquareWarning className="mt-px size-3.5 shrink-0 text-amber-500" />
            <div className="min-w-0 text-[11px]">
              <div className="text-muted-foreground">
                客户端将收到 <code className="font-mono">403 / {code}</code>
                {!reason.trim() && "（未填写，使用默认文案）"}
              </div>
              <div className="break-words text-foreground">{reason.trim() || fallback}</div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

const noopSubscribe = () => () => {};

/**
 * 挂到 body 上而不是留在原地：路由过渡容器带着 `will-change: transform`，
 * 会把里面的 `position: fixed` 变成相对它定位，保存条就跟着内容一起滚走了。
 */
function MobileSaveBar({ count, children }: { count: number; children: React.ReactNode }) {
  const mounted = useSyncExternalStore(noopSubscribe, () => true, () => false);
  if (!mounted) return null;
  return createPortal(
    <div className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-background/95 px-4 pt-2.5 pb-[max(0.625rem,env(safe-area-inset-bottom))] backdrop-blur-md sm:hidden">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-amber-700 dark:text-amber-300">{count} 项未保存</span>
        <div className="flex items-center gap-2">{children}</div>
      </div>
    </div>,
    document.body
  );
}
