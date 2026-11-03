"use client";

import { useMemo, useState } from "react";
import { Check, Loader2, Mail, Plus, Save, Send, Share2, Star, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { FieldGroup, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import type { EmailConfig, EmailProviderMeta } from "@/lib/api/types";
import {
  useDeleteEmailConfigMutation,
  useEmailChannelQuery,
  useEmailConfigsQuery,
  useSaveEmailConfigMutation,
  useTestEmailConfigMutation,
  type EmailScope
} from "@/lib/email-hooks";
import { cn } from "@/lib/utils";
import { EmailBrandBadge } from "./email-brand-icon";
import { EMAIL_CAPABILITY_LABELS, EmailProviderFields, buildDefaultEmailSettings } from "./email-provider-fields";

type EmailDraft = {
  configId?: number;
  provider: string;
  name: string;
  description: string;
  enabled: boolean;
  isDefault: boolean;
  shared: boolean;
  settings: Record<string, string>;
  secrets: Record<string, string>;
  secretSet: Record<string, boolean>;
  testEmail: string;
};

/**
 * 通道：这个作用域能从哪些出口发信。平台级与应用级共用，只差一个 scope。
 *
 * 卡片上标出「正在使用」的那一条 —— 它不总是列表里的第一条（指名配置、默认配置、平台共享通道三档回落）。
 */
export function EmailChannelsPanel({
  scope,
  providers,
  canWrite
}: {
  scope: EmailScope;
  providers: EmailProviderMeta[];
  canWrite: boolean;
}) {
  const configsQuery = useEmailConfigsQuery(scope);
  const channelQuery = useEmailChannelQuery(scope);
  const saveMutation = useSaveEmailConfigMutation(scope);
  const deleteMutation = useDeleteEmailConfigMutation(scope);
  const testMutation = useTestEmailConfigMutation(scope);
  const [editing, setEditing] = useState<EmailDraft | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const configs = configsQuery.data ?? [];
  const isPlatform = scope.kind === "platform";
  const scopeSegment = isPlatform ? "platform" : String(scope.appId);
  const active = channelQuery.data;
  const activeMeta = providers.find((item) => item.provider === editing?.provider);

  const openCreate = () => {
    const first = providers[0];
    setEditing({
      provider: first?.provider ?? "smtp",
      name: configs.length ? "" : "default",
      description: "",
      enabled: true,
      isDefault: configs.length === 0,
      shared: false,
      settings: buildDefaultEmailSettings(first),
      secrets: {},
      secretSet: {},
      testEmail: ""
    });
  };

  const openEdit = (config: EmailConfig) =>
    setEditing({
      configId: config.id,
      provider: config.provider ?? "smtp",
      name: config.name ?? "",
      description: config.description ?? "",
      enabled: config.enabled !== false,
      isDefault: Boolean(config.isDefault),
      shared: Boolean(config.shared),
      settings: { ...(config.settings ?? {}) },
      secrets: {},
      secretSet: { ...(config.secretSet ?? {}) },
      testEmail: ""
    });

  const save = async () => {
    if (!editing) return;
    if (!editing.name.trim()) {
      toast.error("请填写通道名称");
      return;
    }
    try {
      await saveMutation.mutateAsync({
        configId: editing.configId,
        payload: {
          config_name: editing.name.trim(),
          provider: editing.provider,
          description: editing.description,
          enabled: editing.enabled,
          is_default: editing.isDefault,
          ...(isPlatform ? { shared: editing.shared } : {}),
          settings: editing.settings,
          // 留空的密钥不发出去：后端也会再挡一道，两处都做是因为这条约定失效的代价是凭据被静默清空
          secrets: Object.fromEntries(Object.entries(editing.secrets).filter(([, value]) => value.trim() !== "")),
          // 换服务商时整体替换，避免上一家的字段残留在库里
          replace_settings: true
        }
      });
      toast.success(editing.configId ? "通道已更新" : "通道已创建");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  };

  const remove = async () => {
    if (!editing?.configId) return;
    try {
      await deleteMutation.mutateAsync(editing.configId);
      toast.success("通道已删除");
      setConfirmDelete(false);
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  };

  const test = async () => {
    if (!editing?.configId) return;
    const target = editing.testEmail.trim();
    if (!target) {
      toast.error("请填写测试收件地址");
      return;
    }
    try {
      await testMutation.mutateAsync({ configId: editing.configId, testEmail: target });
      toast.success("测试邮件已发出，可在投递记录中查看结果");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试失败");
    }
  };

  if (configsQuery.isLoading) {
    return (
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-[148px] rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {active?.inherited ? (
        <div className="flex items-center gap-2.5 rounded-xl border border-sky-200 bg-sky-50/60 px-4 py-2.5 text-sm text-sky-800 dark:border-sky-900/60 dark:bg-sky-950/30 dark:text-sky-300">
          <Share2 className="size-4 shrink-0" />
          <span>本应用没有可用的通道，正在借用平台共享通道「{active.configName}」发信</span>
        </div>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {configs.map((config) => (
          <ChannelCard
            key={config.id}
            config={config}
            meta={providers.find((item) => item.provider === (config.provider ?? "smtp"))}
            inUse={!active?.inherited && active?.configId === config.id}
            onClick={() => openEdit(config)}
          />
        ))}
        {canWrite ? (
          <button
            type="button"
            onClick={openCreate}
            disabled={!providers.length}
            className="flex min-h-[148px] flex-col items-center justify-center gap-2 rounded-2xl border border-dashed text-muted-foreground transition-colors hover:border-primary/40 hover:bg-muted/40 hover:text-foreground disabled:opacity-50"
          >
            <span className="grid size-9 place-items-center rounded-xl bg-muted">
              <Plus className="size-4" />
            </span>
            <span className="text-sm font-medium">新建通道</span>
            <span className="text-[11px]">SMTP、云服务商与部署平台共 {providers.length} 种</span>
          </button>
        ) : null}
      </div>

      <Dialog open={Boolean(editing)} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="sm:max-w-3xl gap-0! p-0!">
          <DialogHeader className="border-b px-6 pb-4 pt-6">
            <div className="flex items-center gap-3">
              <EmailBrandBadge slug={activeMeta?.icon} brandColor={activeMeta?.brandColor} name={activeMeta?.name} />
              <div className="min-w-0 space-y-0.5 text-left">
                <DialogTitle>{editing?.configId ? "编辑通道" : "新建通道"}</DialogTitle>
                <DialogDescription className="text-xs">
                  {activeMeta ? `${activeMeta.name}${activeMeta.description ? `，${activeMeta.description}` : ""}` : "选择服务商"}
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          {editing ? (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void save();
              }}
              className="max-h-[68vh] space-y-6 overflow-y-auto px-6 py-5"
            >
              <FieldGroup label="服务商">
                <ProviderPicker
                  providers={providers}
                  value={editing.provider}
                  onChange={(provider) => {
                    const meta = providers.find((item) => item.provider === provider);
                    // 换服务商时字段重置成新服务商的默认值，避免带着上一家的残留值提交
                    setEditing({ ...editing, provider, settings: buildDefaultEmailSettings(meta), secrets: {} });
                  }}
                />
              </FieldGroup>

              <FieldGroup label="基本信息">
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">
                      通道名称<span className="text-destructive"> *</span>
                    </Label>
                    <Input
                      className="text-sm"
                      placeholder="default"
                      value={editing.name}
                      onChange={(event) => setEditing({ ...editing, name: event.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label className="text-[11px] font-medium">备注</Label>
                    <Textarea
                      className="min-h-9 text-sm"
                      rows={1}
                      placeholder="可选"
                      value={editing.description}
                      onChange={(event) => setEditing({ ...editing, description: event.target.value })}
                    />
                  </div>
                </div>
                <div className="grid gap-2 sm:grid-cols-2">
                  <SwitchRow
                    label="启用"
                    hint="停用后不再从这条通道发信"
                    checked={editing.enabled}
                    onChange={(value) => setEditing({ ...editing, enabled: value })}
                  />
                  <SwitchRow
                    label="设为默认"
                    hint="未指名通道的信都从这里发"
                    checked={editing.isDefault}
                    onChange={(value) => setEditing({ ...editing, isDefault: value })}
                  />
                  {isPlatform ? (
                    <SwitchRow
                      label="共享给应用"
                      hint="应用自己没有通道时借用这一条"
                      checked={editing.shared}
                      onChange={(value) => setEditing({ ...editing, shared: value })}
                    />
                  ) : null}
                </div>
              </FieldGroup>

              <EmailProviderFields
                meta={activeMeta}
                settings={editing.settings}
                secrets={editing.secrets}
                secretSet={editing.secretSet}
                scopeSegment={scopeSegment}
                configName={editing.name}
                onSetting={(key, value) => setEditing({ ...editing, settings: { ...editing.settings, [key]: value } })}
                onSecret={(key, value) => setEditing({ ...editing, secrets: { ...editing.secrets, [key]: value } })}
              />

              {editing.configId ? (
                <FieldGroup label="发送测试" hint="使用已保存的配置">
                  <div className="flex flex-col gap-2 sm:flex-row">
                    <Input
                      className="text-sm"
                      type="email"
                      placeholder="you@example.com"
                      value={editing.testEmail}
                      onChange={(event) => setEditing({ ...editing, testEmail: event.target.value })}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      className="shrink-0"
                      onClick={() => void test()}
                      disabled={testMutation.isPending || !canWrite}
                    >
                      {testMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}
                      发送测试
                    </Button>
                  </div>
                </FieldGroup>
              ) : null}
            </form>
          ) : null}

          <div className="flex items-center justify-between gap-2 border-t px-6 py-4">
            <div>
              {editing?.configId && canWrite ? (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                  onClick={() => setConfirmDelete(true)}
                >
                  <Trash2 className="size-3.5" /> 删除通道
                </Button>
              ) : null}
            </div>
            <div className="flex gap-2">
              <Button type="button" size="sm" variant="outline" onClick={() => setEditing(null)}>
                取消
              </Button>
              {canWrite ? (
                <Button type="button" size="sm" onClick={() => void save()} disabled={saveMutation.isPending}>
                  {saveMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                  保存
                </Button>
              ) : null}
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除通道「{editing?.name}」</AlertDialogTitle>
            <AlertDialogDescription>
              删除后指名这条通道的发信会失败；如果它是默认通道，未指名的信会改走其余通道或平台共享通道。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={(event) => {
                event.preventDefault();
                void remove();
              }}
              disabled={deleteMutation.isPending}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ChannelCard({
  config,
  meta,
  inUse,
  onClick
}: {
  config: EmailConfig;
  meta?: EmailProviderMeta;
  inUse: boolean;
  onClick: () => void;
}) {
  const capabilities = EMAIL_CAPABILITY_LABELS.filter((item) => meta?.capabilities?.[item.key]);
  const disabled = config.enabled === false;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex min-h-[148px] flex-col gap-3 rounded-2xl border bg-card p-4 text-left transition-colors hover:border-primary/30 hover:bg-muted/30",
        inUse && "border-emerald-300 ring-1 ring-inset ring-emerald-500/20 dark:border-emerald-800",
        disabled && "opacity-70"
      )}
    >
      <div className="flex w-full items-start gap-3">
        <EmailBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{config.name || `通道 ${config.id}`}</span>
            {config.isDefault ? <Star className="size-3.5 shrink-0 fill-amber-400 text-amber-400" aria-label="默认" /> : null}
          </div>
          <span className="text-xs text-muted-foreground">{meta?.name ?? config.provider}</span>
        </div>
        {inUse ? (
          <Badge variant="success" size="sm" className="shrink-0 gap-1">
            <Check className="size-3" /> 使用中
          </Badge>
        ) : disabled ? (
          <Badge variant="warning" size="sm" className="shrink-0">
            已停用
          </Badge>
        ) : null}
      </div>

      <span className="flex items-center gap-1.5 truncate text-xs text-muted-foreground">
        <Mail className="size-3.5 shrink-0" />
        <span className="truncate font-mono">{config.settings?.fromAddress || "未设置发件地址"}</span>
      </span>

      <div className="mt-auto flex flex-wrap gap-1">
        {config.shared ? (
          <Badge variant="info" size="sm" className="gap-1 font-normal">
            <Share2 className="size-3" /> 共享给应用
          </Badge>
        ) : null}
        {capabilities.map((item) => (
          <Badge key={item.key} variant="secondary" size="sm" className="font-normal">
            {item.label}
          </Badge>
        ))}
      </div>
    </button>
  );
}

function ProviderPicker({
  providers,
  value,
  onChange
}: {
  providers: EmailProviderMeta[];
  value: string;
  onChange: (provider: string) => void;
}) {
  // 分组名由后端下发，后端加一个分类时这里自动多一组
  const groups = useMemo(() => {
    const map = new Map<string, EmailProviderMeta[]>();
    for (const provider of providers) {
      const key = provider.categoryName || "其他";
      map.set(key, [...(map.get(key) ?? []), provider]);
    }
    return [...map.entries()];
  }, [providers]);

  return (
    <div className="space-y-3">
      {groups.map(([category, items]) => (
        <div key={category} className="space-y-1.5">
          <span className="text-[11px] text-muted-foreground">{category}</span>
          <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3 lg:grid-cols-4">
            {items.map((provider) => {
              const selected = value === provider.provider;
              return (
                <button
                  key={provider.provider}
                  type="button"
                  onClick={() => onChange(provider.provider)}
                  title={provider.description}
                  className={cn(
                    "flex items-center gap-2 rounded-xl border px-2.5 py-2 text-left text-xs transition-colors",
                    selected ? "border-primary bg-primary/5 font-medium" : "hover:border-primary/30 hover:bg-muted/40"
                  )}
                >
                  <EmailBrandBadge
                    slug={provider.icon}
                    brandColor={provider.brandColor}
                    name={provider.name}
                    size="sm"
                    className="size-6! rounded-md!"
                  />
                  <span className="min-w-0 flex-1 truncate">{provider.name}</span>
                  {selected ? <Check className="size-3.5 shrink-0 text-primary" /> : null}
                </button>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}
