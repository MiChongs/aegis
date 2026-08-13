"use client";

import { FormEvent, useMemo, useState } from "react";
import { AlertTriangle, Mail, Plus, Save, Send, Share2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { EmailConfig, EmailProviderMeta } from "@/lib/api/types";
import {
  useDeleteEmailConfigMutation,
  useEmailChannelQuery,
  useEmailConfigsQuery,
  useEmailProviderCatalogQuery,
  useEmailStatsQuery,
  useSaveEmailConfigMutation,
  useTestEmailConfigMutation,
  type EmailScope
} from "@/lib/email-hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmailBrandBadge } from "./email-brand-icon";
import {
  EMAIL_CAPABILITY_LABELS,
  EmailProviderFields,
  buildDefaultEmailSettings
} from "./email-provider-fields";
import { EmailDeliveryTable } from "./email-delivery-table";

/**
 * 邮件通道面板。平台级与应用级**共用同一个组件**，只差一个 scope：
 * 两边的后端也是同一批方法，各写一份 UI 迟早会漂移成两套不一样的能力。
 */
export function EmailChannelPanel({ scope }: { scope: EmailScope }) {
  const catalogQuery = useEmailProviderCatalogQuery();
  const configsQuery = useEmailConfigsQuery(scope);
  const channelQuery = useEmailChannelQuery(scope);
  const statsQuery = useEmailStatsQuery(scope);
  const saveMutation = useSaveEmailConfigMutation(scope);
  const deleteMutation = useDeleteEmailConfigMutation(scope);
  const testMutation = useTestEmailConfigMutation(scope);

  const [editing, setEditing] = useState<EmailDraft | null>(null);
  const providers = catalogQuery.data?.providers ?? [];
  const configs = configsQuery.data ?? [];
  const isPlatform = scope.kind === "platform";
  const scopeSegment = isPlatform ? "platform" : String(scope.appId);

  function openCreate() {
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
  }

  function openEdit(config: EmailConfig) {
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
  }

  async function handleSave(event: FormEvent) {
    event.preventDefault();
    if (!editing) return;
    try {
      await saveMutation.mutateAsync({
        configId: editing.configId,
        payload: {
          config_name: editing.name,
          provider: editing.provider,
          description: editing.description,
          enabled: editing.enabled,
          is_default: editing.isDefault,
          ...(isPlatform ? { shared: editing.shared } : {}),
          settings: editing.settings,
          // 密钥留空的键在这里就不发出去，后端也会再挡一道 ——
          // 两处都做是刻意的：这条约定一旦失效，代价是用户的凭据被静默清空。
          secrets: Object.fromEntries(Object.entries(editing.secrets).filter(([, v]) => v.trim() !== "")),
          // 换服务商时整体替换，避免上一家的字段残留在库里
          replace_settings: true
        }
      });
      toast.success(editing.configId ? "配置已更新" : "配置已创建");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function handleDelete() {
    if (!editing?.configId) return;
    try {
      await deleteMutation.mutateAsync(editing.configId);
      toast.success("已删除");
      setEditing(null);
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "删除失败");
    }
  }

  async function handleTest() {
    if (!editing?.configId) return;
    const target = editing.testEmail.trim();
    if (!target) {
      toast.error("请填写测试收件地址");
      return;
    }
    try {
      await testMutation.mutateAsync({ configId: editing.configId, testEmail: target });
      toast.success("测试邮件已发送，可在「投递记录」里核对结果");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试失败");
    }
  }

  if (scope.kind === "app" && scope.appId <= 0) {
    return <EmptyState title="请先选择应用" description="选择应用后可管理它的邮件通道。" />;
  }

  const activeMeta = providers.find((p) => p.provider === editing?.provider);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">{isPlatform ? "平台邮件通道" : "邮件通道"}</h2>
          <p className="text-sm text-muted-foreground">
            {isPlatform
              ? "管理员通知与平台告警的发信出口；打开「共享」后还能作为应用的兜底通道。"
              : "该应用的验证码、密码重置、欢迎信与凭证邮件都从这里发出。"}
          </p>
        </div>
        <Button size="sm" onClick={openCreate} disabled={!providers.length}>
          <Plus className="size-3.5" /> 新建通道
        </Button>
      </div>

      <ChannelStatusBand
        scope={scope}
        resolution={channelQuery.data ?? null}
        loading={channelQuery.isLoading}
        providers={providers}
        stats={statsQuery.data}
      />

      <Tabs defaultValue="configs">
        <TabsList>
          <TabsTrigger value="configs">通道配置</TabsTrigger>
          <TabsTrigger value="deliveries">投递记录</TabsTrigger>
        </TabsList>

        <TabsContent value="configs" className="mt-4">
          {configs.length === 0 ? (
            <EmptyState
              title="还没有邮件通道"
              description={
                isPlatform
                  ? "新建一条通道后，管理员通知与平台告警才发得出去。"
                  : "新建一条通道；也可以让平台管理员把平台通道设为共享，本应用即可直接借用。"
              }
            />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {configs.map((config) => (
                <ConfigCard
                  key={config.id}
                  config={config}
                  meta={providers.find((p) => p.provider === (config.provider ?? "smtp"))}
                  onClick={() => openEdit(config)}
                />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="deliveries" className="mt-4">
          <EmailDeliveryTable scope={scope} providers={providers} />
        </TabsContent>
      </Tabs>

      <Dialog open={Boolean(editing)} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-w-2xl p-0! gap-0!">
          <DialogHeader className="px-6 pt-6 pb-4 border-b">
            <DialogTitle>{editing?.configId ? "编辑邮件通道" : "新建邮件通道"}</DialogTitle>
          </DialogHeader>

          {editing && (
            <form onSubmit={handleSave} className="max-h-[70vh] space-y-4 overflow-y-auto px-6 py-5">
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label className="text-xs">
                    通道名称<span className="text-destructive"> *</span>
                  </Label>
                  <Input
                    className="h-8 text-sm"
                    placeholder="default"
                    value={editing.name}
                    onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                    required
                  />
                  <p className="text-[11px] text-muted-foreground">
                    业务侧按名字指定通道（如给凭证单独配一条），留空的调用走默认通道。
                  </p>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">服务商</Label>
                  <ProviderPicker
                    providers={providers}
                    value={editing.provider}
                    onChange={(provider) => {
                      const meta = providers.find((p) => p.provider === provider);
                      // 换服务商时把字段重置成新服务商的默认值：
                      // 保留上一家的键会让「AWS 地域」这类字段带着 SMTP 的残留值提交上去。
                      setEditing({ ...editing, provider, settings: buildDefaultEmailSettings(meta), secrets: {} });
                    }}
                  />
                </div>
              </div>

              <Separator />

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

              <Separator />

              <div className="space-y-1.5">
                <Label className="text-xs">说明</Label>
                <Textarea
                  className="text-sm"
                  rows={2}
                  placeholder="可选备注"
                  value={editing.description}
                  onChange={(e) => setEditing({ ...editing, description: e.target.value })}
                />
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <SwitchRow
                  label="启用"
                  help="关掉之后这条通道不会被任何发信选中"
                  checked={editing.enabled}
                  onCheckedChange={(v) => setEditing({ ...editing, enabled: v })}
                />
                <SwitchRow
                  label="设为默认"
                  help="没有指名通道的发信都走它"
                  checked={editing.isDefault}
                  onCheckedChange={(v) => setEditing({ ...editing, isDefault: v })}
                />
              </div>

              {isPlatform && (
                <SwitchRow
                  label="共享给应用作为兜底"
                  help="应用自己一条通道都没有时，它的信会用这条通道发出去 —— 收件人看到的是平台的发件人身份。默认关闭。"
                  checked={editing.shared}
                  onCheckedChange={(v) => setEditing({ ...editing, shared: v })}
                />
              )}

              {editing.configId && (
                <>
                  <Separator />
                  <div className="flex items-end gap-2">
                    <div className="flex-1 space-y-1.5">
                      <Label className="text-xs">测试收件地址</Label>
                      <Input
                        className="h-8 text-sm"
                        placeholder="you@example.com"
                        value={editing.testEmail}
                        onChange={(e) => setEditing({ ...editing, testEmail: e.target.value })}
                      />
                    </div>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="h-8"
                      onClick={() => void handleTest()}
                      disabled={testMutation.isPending}
                    >
                      <Send className="size-3.5" /> {testMutation.isPending ? "发送中…" : "发送测试"}
                    </Button>
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    试发用的是**已保存**的配置：刚改过的字段要先保存再测，否则测的还是上一版。
                  </p>
                </>
              )}
            </form>
          )}

          <DialogFooter className="border-t px-6 py-4">
            <div className="flex w-full items-center justify-between">
              <div>
                {editing?.configId && (
                  <Button
                    type="button"
                    size="sm"
                    variant="destructive"
                    onClick={() => void handleDelete()}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="size-3.5" /> 删除
                  </Button>
                )}
              </div>
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => setEditing(null)}>
                  取消
                </Button>
                <Button
                  type="button"
                  size="sm"
                  onClick={(e) => void handleSave(e as unknown as FormEvent)}
                  disabled={saveMutation.isPending}
                >
                  <Save className="size-3.5" /> {saveMutation.isPending ? "保存中…" : "保存"}
                </Button>
              </div>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

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
 * 当前通道状态带。
 *
 * 「这个作用域现在到底用哪条通道发信」是这个页面最该一眼看到的结论，
 * 而它并不总等于列表里的第一条：应用没配时会回落到平台共享通道。
 * 不显式说出来的话，管理员会对着一个空列表纳闷验证码是怎么发出去的。
 */
function ChannelStatusBand({
  scope,
  resolution,
  loading,
  providers,
  stats
}: {
  scope: EmailScope;
  resolution: ReturnType<typeof useEmailChannelQuery>["data"];
  loading: boolean;
  providers: EmailProviderMeta[];
  stats?: { total: number; delivered: number; failed: number; bounced: number; last24h: number };
}) {
  if (loading) {
    return <div className="h-16 animate-pulse rounded-xl border bg-muted/30" />;
  }
  if (!resolution) {
    return (
      <div className="flex items-start gap-2.5 rounded-xl border border-amber-500/40 bg-amber-500/5 px-4 py-3">
        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" />
        <div className="min-w-0 text-sm">
          <p className="font-medium">当前没有可用的邮件通道</p>
          <p className="text-xs text-muted-foreground">
            {scope.kind === "platform"
              ? "管理员通知与平台告警现在发不出去。"
              : "验证码、密码重置、凭证邮件现在都发不出去。"}
          </p>
        </div>
      </div>
    );
  }

  const meta = providers.find((p) => p.provider === resolution.provider);
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-3 rounded-xl border px-4 py-3">
      <div className="flex min-w-0 items-center gap-2.5">
        <EmailBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} size="sm" />
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-sm font-medium">
            <span className="truncate">{resolution.configName}</span>
            <Badge variant="secondary" size="sm" className="font-normal">
              {meta?.name ?? resolution.provider}
            </Badge>
          </div>
          <p className="text-[11px] text-muted-foreground">
            {resolution.attachments ? "支持附件，凭证 PDF 直接随信寄出" : "不支持附件，凭证会改发签名下载链接"}
          </p>
        </div>
      </div>

      {resolution.inherited && (
        <div className="flex items-center gap-1.5 rounded-lg border border-sky-500/40 bg-sky-500/5 px-2.5 py-1.5 text-[11px] text-sky-600 dark:text-sky-400">
          <Share2 className="size-3.5 shrink-0" />
          <span>本应用没有自己的通道，正在借用平台共享通道 —— 发件人身份是平台的</span>
        </div>
      )}

      {stats && (
        <div className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <Stat label="近 24 小时" value={stats.last24h} />
          <Stat label="累计" value={stats.total} />
          <Stat label="已送达" value={stats.delivered} tone={stats.delivered > 0 ? "ok" : undefined} />
          <Stat label="退信" value={stats.bounced} tone={stats.bounced > 0 ? "bad" : undefined} />
          <Stat label="失败" value={stats.failed} tone={stats.failed > 0 ? "bad" : undefined} />
        </div>
      )}
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: "ok" | "bad" }) {
  const color = tone === "bad" ? "text-destructive" : tone === "ok" ? "text-emerald-600 dark:text-emerald-400" : "";
  return (
    <span className="inline-flex items-baseline gap-1">
      <span>{label}</span>
      <span className={`font-mono text-xs font-medium ${color}`}>{value}</span>
    </span>
  );
}

function ConfigCard({
  config,
  meta,
  onClick
}: {
  config: EmailConfig;
  meta?: EmailProviderMeta;
  onClick: () => void;
}) {
  const capabilities = EMAIL_CAPABILITY_LABELS.filter((c) => meta?.capabilities?.[c.key]);
  const from = config.settings?.fromAddress;
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex flex-col gap-2 rounded-xl border p-4 text-left transition-colors hover:border-primary/30 hover:bg-muted/30"
    >
      <div className="flex w-full items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <EmailBrandBadge slug={meta?.icon} brandColor={meta?.brandColor} name={meta?.name} size="sm" />
          <span className="truncate text-sm font-semibold">{config.name || `配置 ${config.id}`}</span>
        </div>
        <div className="flex shrink-0 gap-1.5">
          {config.isDefault && (
            <Badge variant="outline" size="sm">
              默认
            </Badge>
          )}
          {config.shared && (
            <Badge variant="info" size="sm">
              共享
            </Badge>
          )}
          <Badge variant={config.enabled === false ? "warning" : "success"} size="sm">
            {config.enabled === false ? "停用" : "启用"}
          </Badge>
        </div>
      </div>

      <span className="text-xs text-muted-foreground">{meta?.name ?? config.provider}</span>
      <span className="flex items-center gap-1 truncate text-xs text-muted-foreground">
        <Mail className="size-3 shrink-0" />
        {from || "未设置发件地址"}
      </span>

      {capabilities.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {capabilities.map((c) => (
            <Badge key={c.key} variant="secondary" size="sm" className="font-normal">
              {c.label}
            </Badge>
          ))}
        </div>
      )}
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
  // 按目录里的分类分组。分类名由后端下发，前端不硬编码 ——
  // 后端加一个分类时这里自动多一组。
  const groups = useMemo(() => {
    const map = new Map<string, EmailProviderMeta[]>();
    for (const provider of providers) {
      const key = provider.categoryName || "其他";
      const list = map.get(key) ?? [];
      list.push(provider);
      map.set(key, list);
    }
    return [...map.entries()];
  }, [providers]);

  return (
    <div className="space-y-2">
      {groups.map(([category, items]) => (
        <div key={category} className="space-y-1">
          <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{category}</span>
          <div className="flex flex-wrap gap-1.5">
            {items.map((provider) => (
              <button
                key={provider.provider}
                type="button"
                onClick={() => onChange(provider.provider)}
                title={provider.description}
                className={`inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 text-xs transition-colors ${
                  value === provider.provider
                    ? "border-primary bg-primary/10 text-foreground"
                    : "hover:border-primary/30 hover:bg-muted/40"
                }`}
              >
                <EmailBrandBadge
                  slug={provider.icon}
                  brandColor={provider.brandColor}
                  name={provider.name}
                  size="sm"
                  className="size-5! rounded-md!"
                />
                {provider.name}
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function SwitchRow({
  label,
  help,
  checked,
  onCheckedChange
}: {
  label: string;
  help?: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-start justify-between gap-3 rounded-xl border px-4 py-3">
      <span className="min-w-0 space-y-0.5">
        <span className="block text-sm">{label}</span>
        {help && <span className="block text-[11px] leading-relaxed text-muted-foreground">{help}</span>}
      </span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} className="mt-0.5 shrink-0" />
    </label>
  );
}

