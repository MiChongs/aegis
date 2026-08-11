"use client";

import { FormEvent, useState } from "react";
import { Check, Copy, Mail, Plus, RefreshCw, Save, Send, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import {
  useAdminEmailConfigsQuery,
  useAdminEmailDeliveriesQuery,
  useCreateAdminEmailConfigMutation,
  useUpdateAdminEmailConfigMutation,
  useDeleteAdminEmailConfigMutation,
  useTestAdminEmailConfigMutation,
} from "@/lib/admin-hooks";
import type { EmailConfig, EmailDelivery } from "@/lib/api/types";
import { useOrigin } from "@/lib/use-client-value";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

// ── 类型 ──

type EmailForm = {
  config_id?: number;
  config_name: string;
  provider: string;
  description: string;
  smtp_host: string;
  smtp_port: string;
  smtp_user: string;
  smtp_password: string;
  smtp_from: string;
  smtp_from_name: string;
  smtp_reply_to: string;
  zeabur_api_key: string;
  zeabur_api_key_set: boolean;
  zeabur_base_url: string;
  zeabur_from: string;
  zeabur_from_name: string;
  zeabur_reply_to: string;
  zeabur_webhook_secret: string;
  zeabur_webhook_secret_set: boolean;
  enabled: boolean;
  is_default: boolean;
};

const defaultForm: EmailForm = {
  config_name: "",
  provider: "smtp",
  description: "",
  smtp_host: "",
  smtp_port: "465",
  smtp_user: "",
  smtp_password: "",
  smtp_from: "",
  smtp_from_name: "",
  smtp_reply_to: "",
  zeabur_api_key: "",
  zeabur_api_key_set: false,
  zeabur_base_url: "",
  zeabur_from: "",
  zeabur_from_name: "",
  zeabur_reply_to: "",
  zeabur_webhook_secret: "",
  zeabur_webhook_secret_set: false,
  enabled: true,
  is_default: false,
};

const PROVIDER_LABEL: Record<string, string> = {
  smtp: "SMTP 直连",
  zeabur: "Zeabur Email",
};

const DELIVERY_STATUS: Record<string, { label: string; variant: "success" | "warning" | "danger" | "info" | "outline" }> = {
  pending: { label: "已入队", variant: "info" },
  sent: { label: "已发送", variant: "info" },
  delivered: { label: "已送达", variant: "success" },
  bounced: { label: "已退信", variant: "danger" },
  complained: { label: "被投诉", variant: "danger" },
  rejected: { label: "被拒绝", variant: "danger" },
  failed: { label: "发送失败", variant: "danger" },
};

function fmtDate(v?: string | null) {
  if (!v) return "—";
  const d = new Date(v);
  return isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

// ── 主组件 ──

export function EmailConfigPanel({ appId }: { appId?: number | null }) {
  const configsQ = useAdminEmailConfigsQuery(appId);
  const createMut = useCreateAdminEmailConfigMutation();
  const updateMut = useUpdateAdminEmailConfigMutation();
  const deleteMut = useDeleteAdminEmailConfigMutation();
  const testMut = useTestAdminEmailConfigMutation();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [form, setForm] = useState<EmailForm>(defaultForm);
  const [testEmail, setTestEmail] = useState("");

  const isEditing = Boolean(form.config_id);
  const isZeabur = form.provider === "zeabur";
  const configs = configsQ.data || [];

  function openCreate() {
    setForm(defaultForm);
    setTestEmail("");
    setDialogOpen(true);
  }

  function openEdit(item: EmailConfig) {
    const smtp = item.smtp || {};
    const zeabur = item.zeabur || {};
    setForm({
      config_id: item.id,
      config_name: item.name || "",
      provider: item.provider || "smtp",
      description: item.description || "",
      smtp_host: smtp.host || "",
      smtp_port: String(smtp.port || 465),
      smtp_user: smtp.username || "",
      smtp_password: "",
      smtp_from: smtp.fromAddress || "",
      smtp_from_name: smtp.fromName || "",
      smtp_reply_to: smtp.replyTo || "",
      zeabur_api_key: "",
      zeabur_api_key_set: Boolean(zeabur.apiKeySet),
      zeabur_base_url: zeabur.baseUrl || "",
      zeabur_from: zeabur.fromAddress || "",
      zeabur_from_name: zeabur.fromName || "",
      zeabur_reply_to: zeabur.replyTo || "",
      zeabur_webhook_secret: "",
      zeabur_webhook_secret_set: Boolean(zeabur.webhookSecretSet),
      enabled: item.enabled !== false,
      is_default: Boolean(item.isDefault),
    });
    setTestEmail("");
    setDialogOpen(true);
  }

  const set = <K extends keyof EmailForm>(k: K, v: EmailForm[K]) => setForm(s => ({ ...s, [k]: v }));

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    if (!appId) return;
    try {
      // 密钥类字段留空即「不修改」，因此一律不发送空串，交由后端沿用既有密文。
      const payload: Record<string, unknown> = {
        appid: appId,
        config_name: form.config_name,
        provider: form.provider,
        description: form.description,
        enabled: form.enabled,
        is_default: form.is_default,
      };
      if (isZeabur) {
        Object.assign(payload, {
          zeabur_base_url: form.zeabur_base_url,
          zeabur_from: form.zeabur_from,
          zeabur_from_name: form.zeabur_from_name,
          zeabur_reply_to: form.zeabur_reply_to,
          zeabur_api_key: form.zeabur_api_key || undefined,
          zeabur_webhook_secret: form.zeabur_webhook_secret || undefined,
        });
      } else {
        Object.assign(payload, {
          smtp_host: form.smtp_host,
          smtp_port: Number(form.smtp_port || 465),
          smtp_user: form.smtp_user,
          smtp_password: form.smtp_password || undefined,
          smtp_from: form.smtp_from,
          smtp_from_name: form.smtp_from_name,
          smtp_reply_to: form.smtp_reply_to,
          smtp_tls: Number(form.smtp_port || 465) === 465,
        });
      }
      if (isEditing) {
        await updateMut.mutateAsync({ ...payload, config_id: form.config_id! });
      } else {
        await createMut.mutateAsync(payload);
      }
      toast.success(isEditing ? "配置已更新" : "配置已创建");
      setDialogOpen(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  async function handleDelete() {
    if (!appId || !form.config_id) return;
    try {
      await deleteMut.mutateAsync({ appid: appId, config_id: form.config_id });
      toast.success("已删除");
      setDialogOpen(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  }

  async function handleTest() {
    if (!appId || !form.config_id) return;
    const target = testEmail.trim();
    if (!target) {
      toast.error("请填写测试收件地址");
      return;
    }
    try {
      await testMut.mutateAsync({ appid: appId, config_id: form.config_id, test_email: target });
      toast.success("测试邮件已发送");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "测试失败");
    }
  }

  if (!appId) return <EmptyState title="请先选择应用" description="选择应用后可管理邮件配置。" />;

  return (
    <div className="space-y-5">
      {/* 标题栏 */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">邮件配置</h2>
          <p className="text-sm text-muted-foreground">SMTP 直连与 Zeabur Email 服务配置管理</p>
        </div>
        <Button size="sm" onClick={openCreate}>
          <Plus className="size-3.5" /> 新建配置
        </Button>
      </div>

      <Tabs defaultValue="configs">
        <TabsList>
          <TabsTrigger value="configs">渠道配置</TabsTrigger>
          <TabsTrigger value="deliveries">投递记录</TabsTrigger>
        </TabsList>

        <TabsContent value="configs" className="mt-4">
          {configs.length === 0 ? (
            <EmptyState title="暂无邮件配置" description="点击新建配置添加 SMTP 或 Zeabur Email 服务。" />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {configs.map((item) => {
                const provider = item.provider || "smtp";
                const endpoint = provider === "zeabur"
                  ? (item.zeabur?.baseUrl || "api.zeabur.com")
                  : `${item.smtp?.host || "—"}:${item.smtp?.port || "—"}`;
                const from = provider === "zeabur" ? item.zeabur?.fromAddress : item.smtp?.fromAddress;
                return (
                  <button
                    key={item.id}
                    type="button"
                    className="flex flex-col gap-2 rounded-xl border p-4 text-left transition-colors hover:border-primary/30 hover:bg-muted/30"
                    onClick={() => openEdit(item)}
                  >
                    <div className="flex items-center justify-between w-full">
                      <div className="flex items-center gap-2 min-w-0">
                        <Mail className="size-4 shrink-0 text-muted-foreground" />
                        <span className="text-sm font-semibold truncate">{item.name || `配置 ${item.id}`}</span>
                      </div>
                      <div className="flex gap-1.5 shrink-0">
                        {item.isDefault && <Badge variant="outline" className="text-[10px]">默认</Badge>}
                        <Badge variant={item.enabled === false ? "warning" : "success"} className="text-[10px]">
                          {item.enabled === false ? "停用" : "启用"}
                        </Badge>
                      </div>
                    </div>
                    <Badge variant="info" className="w-fit text-[10px]">{PROVIDER_LABEL[provider] || provider}</Badge>
                    <div className="text-xs text-muted-foreground truncate">{endpoint}</div>
                    <div className="text-xs text-muted-foreground truncate">{from || "—"}</div>
                    <div className="text-[10px] text-muted-foreground">{fmtDate(item.updatedAt)}</div>
                  </button>
                );
              })}
            </div>
          )}
        </TabsContent>

        <TabsContent value="deliveries" className="mt-4">
          <EmailDeliveryTable appId={appId} />
        </TabsContent>
      </Tabs>

      {/* 编辑/创建 Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg p-0! gap-0!">
          <DialogHeader className="px-6 pt-6 pb-4 border-b">
            <DialogTitle>{isEditing ? "编辑邮件配置" : "新建邮件配置"}</DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSave} className="px-6 py-5 space-y-4 max-h-[70vh] overflow-y-auto">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="配置名称">
                <Input className="h-8 text-sm" placeholder="主邮箱" value={form.config_name} onChange={e => set("config_name", e.target.value)} required />
              </Field>
              <Field label="服务商">
                <Select value={form.provider} onValueChange={v => set("provider", v)}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="smtp">SMTP 直连</SelectItem>
                    <SelectItem value="zeabur">Zeabur Email</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            </div>

            <Separator />

            {isZeabur ? (
              <ZeaburFields appId={appId} form={form} set={set} isEditing={isEditing} />
            ) : (
              <SmtpFields form={form} set={set} isEditing={isEditing} />
            )}

            <Field label="说明">
              <Textarea className="text-sm" rows={2} placeholder="可选备注" value={form.description} onChange={e => set("description", e.target.value)} />
            </Field>

            <div className="grid gap-3 sm:grid-cols-2">
              <SwitchRow label="启用" checked={form.enabled} onCheckedChange={v => set("enabled", v)} />
              <SwitchRow label="设为默认" checked={form.is_default} onCheckedChange={v => set("is_default", v)} />
            </div>

            {/* 测试发送（仅编辑时） */}
            {isEditing && (
              <>
                <Separator />
                <div className="flex items-end gap-2">
                  <div className="flex-1 space-y-1.5">
                    <Label className="text-xs">测试收件地址</Label>
                    <Input className="h-8 text-sm" placeholder="you@example.com" value={testEmail} onChange={e => setTestEmail(e.target.value)} />
                  </div>
                  <Button type="button" size="sm" variant="outline" className="h-8" onClick={() => void handleTest()} disabled={testMut.isPending}>
                    <Send className="size-3.5" /> {testMut.isPending ? "发送中..." : "发送测试"}
                  </Button>
                </div>
              </>
            )}
          </form>

          <DialogFooter className="px-6 py-4 border-t">
            <div className="flex w-full items-center justify-between">
              <div>
                {isEditing && (
                  <Button type="button" size="sm" variant="destructive" onClick={() => void handleDelete()} disabled={deleteMut.isPending}>
                    <Trash2 className="size-3.5" /> 删除
                  </Button>
                )}
              </div>
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
                <Button type="submit" size="sm" onClick={(e) => { e.preventDefault(); void handleSave(e as unknown as FormEvent); }} disabled={createMut.isPending || updateMut.isPending}>
                  <Save className="size-3.5" /> {createMut.isPending || updateMut.isPending ? "保存中..." : "保存"}
                </Button>
              </div>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ── 字段组 ──

type FieldSetter = <K extends keyof EmailForm>(k: K, v: EmailForm[K]) => void;

function SmtpFields({ form, set, isEditing }: { form: EmailForm; set: FieldSetter; isEditing: boolean }) {
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="SMTP 主机">
          <Input className="h-8 text-sm font-mono" placeholder="smtp.example.com" value={form.smtp_host} onChange={e => set("smtp_host", e.target.value)} />
        </Field>
        <Field label="端口">
          <Select value={form.smtp_port} onValueChange={v => set("smtp_port", v)}>
            <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="25">25 (不加密)</SelectItem>
              <SelectItem value="465">465 (SSL)</SelectItem>
              <SelectItem value="587">587 (STARTTLS)</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label="用户名">
          <Input className="h-8 text-sm" placeholder="user@example.com" value={form.smtp_user} onChange={e => set("smtp_user", e.target.value)} />
        </Field>
        <Field label="密码">
          <Input className="h-8 text-sm" type="password" placeholder={isEditing ? "留空不修改" : "SMTP 密码"} value={form.smtp_password} onChange={e => set("smtp_password", e.target.value)} />
        </Field>
      </div>

      <Separator />

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="发件地址">
          <Input className="h-8 text-sm" placeholder="noreply@example.com" value={form.smtp_from} onChange={e => set("smtp_from", e.target.value)} />
        </Field>
        <Field label="发件名称">
          <Input className="h-8 text-sm" placeholder="Aegis System" value={form.smtp_from_name} onChange={e => set("smtp_from_name", e.target.value)} />
        </Field>
        <Field label="回信地址">
          <Input className="h-8 text-sm" placeholder="可选" value={form.smtp_reply_to} onChange={e => set("smtp_reply_to", e.target.value)} />
        </Field>
      </div>

      <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
        部署在 Zeabur / Linode 等封禁出站 SMTP 端口的平台时，直连 SMTP 会一律超时，请改用 Zeabur Email 服务商。
      </p>
    </>
  );
}

function ZeaburFields({
  appId,
  form,
  set,
  isEditing,
}: {
  appId: number;
  form: EmailForm;
  set: FieldSetter;
  isEditing: boolean;
}) {
  const origin = useOrigin();
  const [copied, setCopied] = useState(false);
  // 回调地址带上配置名，多套配置各自独立验签；配置名为空时后端回落到默认配置。
  const webhookPath = `/api/email/webhook/zeabur/${appId}${form.config_name ? `/${encodeURIComponent(form.config_name)}` : ""}`;
  const webhookURL = origin ? `${origin}${webhookPath}` : webhookPath;

  async function copyWebhook() {
    try {
      await navigator.clipboard.writeText(webhookURL);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("复制失败，请手动选择地址");
    }
  }

  return (
    <>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <Field label="API Key">
            <Input
              className="h-8 text-sm font-mono"
              type="password"
              placeholder={form.zeabur_api_key_set ? "已配置，留空不修改" : "在 Zeabur 控制台 Email → API Keys 创建"}
              value={form.zeabur_api_key}
              onChange={e => set("zeabur_api_key", e.target.value)}
            />
          </Field>
        </div>
        <Field label="发件地址">
          <Input className="h-8 text-sm" placeholder="noreply@yourdomain.com" value={form.zeabur_from} onChange={e => set("zeabur_from", e.target.value)} />
        </Field>
        <Field label="发件名称">
          <Input className="h-8 text-sm" placeholder="Aegis System" value={form.zeabur_from_name} onChange={e => set("zeabur_from_name", e.target.value)} />
        </Field>
        <Field label="回信地址">
          <Input className="h-8 text-sm" placeholder="可选" value={form.zeabur_reply_to} onChange={e => set("zeabur_reply_to", e.target.value)} />
        </Field>
        <Field label="API 地址">
          <Input className="h-8 text-sm font-mono" placeholder="https://api.zeabur.com/api/v1/zsend" value={form.zeabur_base_url} onChange={e => set("zeabur_base_url", e.target.value)} />
        </Field>
      </div>

      <p className="rounded-lg border border-dashed px-3 py-2 text-xs text-muted-foreground">
        发件域名需先在 Zeabur 控制台完成 DKIM / SPF / DMARC 验证。未验证域名每日仅 100 封，验证后 1000 封（UTC 00:00 重置）。
      </p>

      <Separator />

      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs">Webhook 回调地址</Label>
          <div className="flex gap-2">
            <Input className="h-8 text-sm font-mono" readOnly value={webhookURL} />
            <Button type="button" size="sm" variant="outline" className="h-8 shrink-0" onClick={() => void copyWebhook()}>
              {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            </Button>
          </div>
          <p className="text-[11px] text-muted-foreground">
            填入 Zeabur 控制台的 Webhook 管理，用于回填送达 / 退信 / 投诉状态。
            {!isEditing && " 配置保存后地址才会生效。"}
          </p>
        </div>

        <Field label="Webhook 签名密钥">
          <Input
            className="h-8 text-sm font-mono"
            type="password"
            placeholder={form.zeabur_webhook_secret_set ? "已配置，留空不修改" : "Zeabur 创建 Webhook 时生成的 Secret"}
            value={form.zeabur_webhook_secret}
            onChange={e => set("zeabur_webhook_secret", e.target.value)}
          />
        </Field>
        <p className="text-[11px] text-muted-foreground">
          未填写密钥时回调一律拒收 —— 无法验签的回执可被任意伪造。
        </p>
      </div>
    </>
  );
}

// ── 投递记录 ──

function EmailDeliveryTable({ appId }: { appId: number }) {
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const deliveriesQ = useAdminEmailDeliveriesQuery(appId, {
    status: status === "all" ? undefined : status,
    keyword: keyword.trim() || undefined,
    pageSize: 50,
  });

  const items: EmailDelivery[] = deliveriesQ.data?.items || [];

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="h-8 w-56 text-sm"
          placeholder="按收件地址或主题筛选"
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
        />
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger className="h-8 w-36 text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {Object.entries(DELIVERY_STATUS).map(([value, meta]) => (
              <SelectItem key={value} value={value}>{meta.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="button" size="sm" variant="outline" className="h-8" onClick={() => void deliveriesQ.refetch()} disabled={deliveriesQ.isFetching}>
          <RefreshCw className={`size-3.5 ${deliveriesQ.isFetching ? "animate-spin" : ""}`} /> 刷新
        </Button>
        <span className="text-xs text-muted-foreground">共 {deliveriesQ.data?.total ?? 0} 条</span>
      </div>

      {items.length === 0 ? (
        <EmptyState title="暂无投递记录" description="发出的邮件会在此留痕；Zeabur 通道的送达状态由 webhook 回填。" />
      ) : (
        <div className="overflow-x-auto rounded-xl border">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left font-medium">收件人</th>
                <th className="px-3 py-2 text-left font-medium">主题</th>
                <th className="px-3 py-2 text-left font-medium">用途</th>
                <th className="px-3 py-2 text-left font-medium">通道</th>
                <th className="px-3 py-2 text-left font-medium">状态</th>
                <th className="px-3 py-2 text-left font-medium">时间</th>
              </tr>
            </thead>
            <tbody>
              {items.map(item => {
                const meta = DELIVERY_STATUS[item.status] || { label: item.status, variant: "outline" as const };
                return (
                  <tr key={item.id} className="border-t align-top">
                    <td className="px-3 py-2 font-mono text-xs">{item.toAddress}</td>
                    <td className="px-3 py-2 max-w-[22rem] truncate" title={item.subject}>{item.subject || "—"}</td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">{item.purpose || "—"}</td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">{PROVIDER_LABEL[item.provider] || item.provider}</td>
                    <td className="px-3 py-2">
                      <Badge variant={meta.variant} className="text-[10px]">{meta.label}</Badge>
                      {item.errorMessage && (
                        <div className="mt-1 max-w-[20rem] text-[11px] text-muted-foreground" title={item.errorMessage}>
                          {item.errorMessage}
                        </div>
                      )}
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground whitespace-nowrap">
                      {fmtDate(item.deliveredAt || item.sentAt || item.createdAt)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ── 辅助 ──

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="space-y-1.5"><Label className="text-xs">{label}</Label>{children}</div>;
}

function SwitchRow({ label, checked, onCheckedChange }: { label: string; checked: boolean; onCheckedChange: (v: boolean) => void }) {
  return (
    <div className="flex items-center justify-between rounded-xl border px-4 py-3">
      <Label className="text-sm cursor-pointer">{label}</Label>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}
