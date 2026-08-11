"use client";

import { useCallback, useEffect, useState } from "react";
import { Loader2, Save, TestTube, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { ProviderFields, providerOptions } from "./provider-fields";

type FormData = {
  provider: string;
  configName: string;
  accessMode: string;
  enabled: boolean;
  isDefault: boolean;
  proxyDownload: boolean;
  baseUrl: string;
  rootPath: string;
  description: string;
  configData: Record<string, unknown>;
};

const defaultForm: FormData = {
  provider: "s3",
  configName: "default",
  accessMode: "public",
  enabled: true,
  isDefault: false,
  proxyDownload: true,
  baseUrl: "",
  rootPath: "",
  description: "",
  configData: {}
};

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  editConfig?: Record<string, unknown> | null; // 编辑模式时传入已有配置
  scope: "global" | "app";
  appId?: number | null;
  onSave: (payload: Record<string, unknown>) => Promise<void>;
  onDelete?: () => Promise<void>;
  onTest?: () => Promise<void>;
  isSaving?: boolean;
  isDeleting?: boolean;
  isTesting?: boolean;
};

export function StorageConfigForm({ open, onOpenChange, editConfig, scope, appId, onSave, onDelete, onTest, isSaving, isDeleting, isTesting }: Props) {
  const [form, setForm] = useState<FormData>(defaultForm);
  const isEdit = !!editConfig;

  useEffect(() => {
    if (!open) return;
    if (editConfig) {
      setForm({
        provider: String(editConfig.provider ?? "s3"),
        configName: String(editConfig.config_name ?? ""),
        accessMode: String(editConfig.access_mode ?? "public"),
        enabled: editConfig.enabled !== false,
        isDefault: !!editConfig.is_default,
        proxyDownload: editConfig.proxy_download !== false,
        baseUrl: String(editConfig.base_url ?? ""),
        rootPath: String(editConfig.root_path ?? ""),
        description: String(editConfig.description ?? ""),
        configData: (editConfig.config_data as Record<string, unknown>) || {}
      });
    } else {
      setForm(defaultForm);
    }
  }, [open, editConfig]);

  const set = <K extends keyof FormData>(key: K, value: FormData[K]) => setForm((s) => ({ ...s, [key]: value }));
  const setConfigField = useCallback((key: string, value: unknown) => {
    setForm((s) => ({ ...s, configData: { ...s.configData, [key]: value } }));
  }, []);

  async function handleSave() {
    const payload: Record<string, unknown> = {
      provider: form.provider,
      config_name: form.configName,
      access_mode: form.accessMode,
      enabled: form.enabled,
      is_default: form.isDefault,
      proxy_download: form.proxyDownload,
      base_url: form.baseUrl,
      root_path: form.rootPath,
      description: form.description,
      config_data: form.configData
    };
    if (scope === "app" && appId) payload.appid = appId;
    if (editConfig?.id) payload.id = editConfig.id;
    try {
      await onSave(payload);
      toast.success(isEdit ? "配置已更新" : "配置已创建");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[92vw] max-w-xl overflow-y-auto">
        <SheetHeader>
          <SheetTitle className="text-sm">{isEdit ? "编辑存储配置" : "新建存储配置"}</SheetTitle>
        </SheetHeader>

        <div className="space-y-5 pt-3">
          {/* 通用字段 */}
          <Section title="基本设置">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label className="text-xs">存储提供商</Label>
                <Select value={form.provider} onValueChange={(v) => { set("provider", v); set("configData", {}); }} disabled={isEdit}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {providerOptions.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label className="text-xs">配置名称</Label>
                <Input className="h-8 text-sm" placeholder="default" value={form.configName} onChange={(e) => set("configName", e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">访问模式</Label>
                <Select value={form.accessMode} onValueChange={(v) => set("accessMode", v)}>
                  <SelectTrigger className="h-8 text-sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="public">公开</SelectItem>
                    <SelectItem value="private">私有</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label className="text-xs">基础 URL</Label>
                <Input className="h-8 text-sm" placeholder="https://cdn.example.com" value={form.baseUrl} onChange={(e) => set("baseUrl", e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">根路径</Label>
                <Input className="h-8 text-sm" placeholder="uploads/" value={form.rootPath} onChange={(e) => set("rootPath", e.target.value)} />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">描述</Label>
                <Input className="h-8 text-sm" placeholder="可选" value={form.description} onChange={(e) => set("description", e.target.value)} />
              </div>
            </div>
            <div className="flex flex-wrap gap-3">
              <ToggleField label="启用" checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <ToggleField label="默认配置" checked={form.isDefault} onChange={(v) => set("isDefault", v)} />
              <ToggleField label="代理下载" checked={form.proxyDownload} onChange={(v) => set("proxyDownload", v)} />
            </div>
          </Section>

          <Separator />

          {/* 提供商专属字段 */}
          <Section title={`${providerOptions.find(o => o.value === form.provider)?.label || form.provider} 配置`}>
            <ProviderFields provider={form.provider} data={form.configData} onChange={setConfigField} />
          </Section>

          <Separator />

          {/* 操作按钮 */}
          <div className="flex flex-wrap gap-2">
            <Button size="sm" className="h-8 gap-1 text-xs" disabled={isSaving} onClick={handleSave}>
              {isSaving ? <Loader2 className="size-3 animate-spin" /> : <Save className="size-3" />}
              {isSaving ? "保存中..." : "保存"}
            </Button>
            {onTest && (
              <Button size="sm" variant="outline" className="h-8 gap-1 text-xs" disabled={isTesting} onClick={async () => { try { await onTest(); toast.success("连通测试成功"); } catch (err) { toast.error(err instanceof ApiError ? err.message : "连通测试失败"); } }}>
                {isTesting ? <Loader2 className="size-3 animate-spin" /> : <TestTube className="size-3" />}
                测试
              </Button>
            )}
            {isEdit && onDelete && (
              <Button size="sm" variant="ghost" className="h-8 gap-1 text-xs text-destructive hover:text-destructive" disabled={isDeleting} onClick={async () => { try { await onDelete(); toast.success("配置已删除"); onOpenChange(false); } catch (err) { toast.error(err instanceof ApiError ? err.message : "删除失败"); } }}>
                {isDeleting ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
                删除
              </Button>
            )}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">{title}</h4>
      {children}
    </div>
  );
}

function ToggleField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2">
      <Checkbox checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      <span className="text-xs">{label}</span>
    </label>
  );
}
