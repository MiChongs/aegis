"use client";

import { useEffect, useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { useAdminSystemSettingsQuery, useUpdateAdminSystemSettingsMutation } from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import { Button } from "@/components/ui/button";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { SurfaceCard } from "@/components/ui/surface-card";
import { Switch } from "@/components/ui/switch";

type Draft = {
  enabled: boolean;
  type: string;
  requireForLogin: boolean;
  requireForRegister: boolean;
  audioLang: string;
};

const defaultDraft: Draft = {
  enabled: false,
  type: "image",
  requireForLogin: false,
  requireForRegister: false,
  audioLang: "zh"
};

const captchaTypeOptions = [
  { value: "image", label: "图形字符" },
  { value: "math", label: "算术" },
  { value: "digit", label: "纯数字" },
  { value: "dynamic", label: "动态图片 (GIF)" },
  { value: "audio", label: "音频 (WAV)" },
  { value: "chiral", label: "手性碳 (点选)" }
];

export function AdminCaptchaConfigPanel() {
  const operator = useAuthStore((s) => s.operator);
  const settingsQuery = useAdminSystemSettingsQuery(Boolean(operator?.isSuperAdmin));
  const updateMutation = useUpdateAdminSystemSettingsMutation();
  const [draft, setDraft] = useState<Draft>(defaultDraft);
  const [synced, setSynced] = useState(false);

  useEffect(() => {
    const cfg = settingsQuery.data?.adminCaptcha;
    if (cfg && !synced) {
      setDraft({
        enabled: cfg.enabled ?? false,
        type: cfg.type || "image",
        requireForLogin: cfg.requireForLogin ?? false,
        requireForRegister: cfg.requireForRegister ?? false,
        audioLang: cfg.audioLang || "zh"
      });
      setSynced(true);
    }
  }, [settingsQuery.data, synced]);

  async function handleSave() {
    try {
      await updateMutation.mutateAsync({
        adminCaptcha: {
          enabled: draft.enabled,
          type: draft.type,
          requireForLogin: draft.requireForLogin,
          requireForRegister: draft.requireForRegister,
          audioLang: draft.audioLang
        }
      });
      toast.success("管理员验证码配置已保存");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "保存失败");
    }
  }

  if (!operator?.isSuperAdmin) {
    return <EmptyState title="无访问权限" description="仅超级管理员可配置管理员验证码。" />;
  }

  if (settingsQuery.isLoading) {
    return <LoadingState title="加载配置" />;
  }

  return (
    <SurfaceCard>
      <div className="space-y-5">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold text-foreground">管理员验证码</h2>
          <p className="text-sm text-muted-foreground">全局配置，适用于管理员登录和注册页面。</p>
        </div>

        <div className="space-y-4">
          <Row label="启用验证码">
            <Switch checked={draft.enabled} onCheckedChange={(v) => setDraft((s) => ({ ...s, enabled: v }))} />
          </Row>

          <Row label="验证码类型">
            <Select value={draft.type} onValueChange={(v) => setDraft((s) => ({ ...s, type: v }))}>
              <SelectTrigger className="h-8 w-40 text-sm"><SelectValue /></SelectTrigger>
              <SelectContent>
                {captchaTypeOptions.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
              </SelectContent>
            </Select>
          </Row>

          <Row label="登录需验证码">
            <Switch checked={draft.requireForLogin} onCheckedChange={(v) => setDraft((s) => ({ ...s, requireForLogin: v }))} />
          </Row>

          <Row label="注册需验证码">
            <Switch checked={draft.requireForRegister} onCheckedChange={(v) => setDraft((s) => ({ ...s, requireForRegister: v }))} />
          </Row>

          <Row label="音频语言">
            <Select value={draft.audioLang} onValueChange={(v) => setDraft((s) => ({ ...s, audioLang: v }))}>
              <SelectTrigger className="h-8 w-40 text-sm"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="zh">中文</SelectItem>
                <SelectItem value="en">English</SelectItem>
                <SelectItem value="ja">日本語</SelectItem>
                <SelectItem value="ko">한국어</SelectItem>
              </SelectContent>
            </Select>
          </Row>
        </div>

        <Button className="h-9 gap-1.5" disabled={updateMutation.isPending} onClick={handleSave}>
          {updateMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
          {updateMutation.isPending ? "保存中..." : "保存"}
        </Button>
      </div>
    </SurfaceCard>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between rounded-xl border px-4 py-3">
      <Label className="text-sm">{label}</Label>
      {children}
    </div>
  );
}
