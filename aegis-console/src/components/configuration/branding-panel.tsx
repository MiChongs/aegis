"use client";

import { useEffect, useState } from "react";
import { Globe, Image as ImageIcon, Loader2, Palette, RotateCcw, Save, Type } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { useAdminSystemSettingsQuery, useUpdateAdminSystemSettingsMutation } from "@/lib/admin-hooks";
import type { BrandingSettings } from "@/lib/api/types";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/data-state";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { ImageLightbox } from "@/components/ui/image-lightbox";

type Draft = {
  platformName: string;
  consoleName: string;
  logoURL: string;
  logoDarkURL: string;
  faviconURL: string;
  primaryColor: string;
  primaryColorDark: string;
  accentColor: string;
  loginBgURL: string;
  loginBgColor: string;
  footerText: string;
  customCSS: string;
};

function seedDraft(b?: BrandingSettings): Draft {
  return {
    platformName: b?.platformName ?? "Aegis",
    consoleName: b?.consoleName ?? "控制台",
    logoURL: b?.logoURL ?? "",
    logoDarkURL: b?.logoDarkURL ?? "",
    faviconURL: b?.faviconURL ?? "",
    primaryColor: b?.primaryColor ?? "",
    primaryColorDark: b?.primaryColorDark ?? "",
    accentColor: b?.accentColor ?? "",
    loginBgURL: b?.loginBgURL ?? "",
    loginBgColor: b?.loginBgColor ?? "",
    footerText: b?.footerText ?? "",
    customCSS: b?.customCSS ?? "",
  };
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs font-medium">{label}</Label>
      {children}
      {hint && <p className="text-[10px] text-muted-foreground">{hint}</p>}
    </div>
  );
}

function ColorRow({ label, hint, value, onChange }: { label: string; hint?: string; value: string; onChange: (v: string) => void }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs font-medium">{label}</Label>
      <div className="flex items-center gap-2">
        <input type="color" value={value || "#18181b"} onChange={(e) => onChange(e.target.value)}
          className="size-9 rounded-md border cursor-pointer p-0.5" />
        <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder="#18181b" className="font-mono flex-1" />
        {value && (
          <Button variant="ghost" size="sm" className="h-8 px-2 text-xs text-muted-foreground" onClick={() => onChange("")}>
            清除
          </Button>
        )}
      </div>
      {hint && <p className="text-[10px] text-muted-foreground">{hint}</p>}
    </div>
  );
}

export function BrandingPanel() {
  const settingsQuery = useAdminSystemSettingsQuery();
  const updateMutation = useUpdateAdminSystemSettingsMutation();
  const branding = settingsQuery.data?.branding;
  const [draft, setDraft] = useState<Draft>(() => seedDraft(branding));

  useEffect(() => {
    if (branding) setDraft(seedDraft(branding));
  }, [branding]);

  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({ branding: draft as never });
      toast.success("品牌配置已保存，刷新页面后完全生效");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "保存失败");
    }
  };

  if (settingsQuery.isLoading) return <LoadingState title="加载中" description="" />;

  return (
    <div className="space-y-5">
      {/* 状态栏 */}
      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatusCard label="平台名称" value={draft.platformName || "Aegis"} />
        <StatusCard label="主题色" value={draft.primaryColor || "默认"} color={draft.primaryColor} />
        <StatusCard label="Logo" value={draft.logoURL ? "已配置" : "默认"} />
        <StatusCard label="数据来源" value={branding?.source === "database" ? "数据库" : "未配置"} />
      </div>

      <Separator />

      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">品牌自定义</h3>
          <p className="text-xs text-muted-foreground">自定义控制台外观、Logo、主题色</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setDraft(seedDraft(branding))} disabled={updateMutation.isPending}>
            <RotateCcw className="size-3.5" /> 重置
          </Button>
          <Button size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      <Accordion type="multiple" defaultValue={["identity"]} className="space-y-2">
        {/* 品牌标识 */}
        <AccordionItem value="identity" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Type className="size-4 text-muted-foreground" /><span className="text-sm font-medium">品牌标识</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <Row label="平台名称" hint="侧边栏和登录页显示"><Input value={draft.platformName} onChange={(e) => patch("platformName", e.target.value)} /></Row>
              <Row label="控制台名称" hint="侧边栏副标题"><Input value={draft.consoleName} onChange={(e) => patch("consoleName", e.target.value)} /></Row>
            </div>
            <Row label="Logo URL" hint="支持 SVG/PNG，建议正方形">
              <Input value={draft.logoURL} onChange={(e) => patch("logoURL", e.target.value)} placeholder="https://example.com/logo.svg" />
            </Row>
            {(draft.logoURL || draft.logoDarkURL || draft.faviconURL) && (
              <div className="grid gap-4 lg:grid-cols-3">
                <ImageLightbox
                  src={draft.logoURL}
                  alt="平台 Logo"
                  caption="Logo 预览"
                  hint="支持 SVG / PNG，点击可放大、缩放与下载"
                  frameClassName="min-h-36"
                  imageClassName="max-h-20"
                />
                <ImageLightbox
                  src={draft.logoDarkURL || draft.logoURL}
                  alt="深色模式 Logo"
                  caption="深色模式 Logo"
                  hint="默认回退到主 Logo"
                  frameClassName="min-h-36 bg-background"
                  imageClassName="max-h-20"
                />
                <ImageLightbox
                  src={draft.faviconURL}
                  alt="Favicon"
                  caption="Favicon 预览"
                  hint="建议使用清晰的小尺寸图标"
                  frameClassName="min-h-36"
                  imageClassName="max-h-14"
                  emptyLabel="未配置 Favicon"
                />
              </div>
            )}
            <Row label="深色模式 Logo" hint="可选，留空使用上方 Logo"><Input value={draft.logoDarkURL} onChange={(e) => patch("logoDarkURL", e.target.value)} /></Row>
            <Row label="Favicon URL"><Input value={draft.faviconURL} onChange={(e) => patch("faviconURL", e.target.value)} placeholder="https://example.com/favicon.ico" /></Row>
          </AccordionContent>
        </AccordionItem>

        {/* 主题色 */}
        <AccordionItem value="colors" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Palette className="size-4 text-muted-foreground" /><span className="text-sm font-medium">主题色</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <ColorRow label="主题色" hint="影响按钮、链接、选中状态等" value={draft.primaryColor} onChange={(v) => patch("primaryColor", v)} />
            <ColorRow label="深色模式主题色" hint="可选，留空自动适配" value={draft.primaryColorDark} onChange={(v) => patch("primaryColorDark", v)} />
            <ColorRow label="强调色" hint="可选，用于特殊高亮" value={draft.accentColor} onChange={(v) => patch("accentColor", v)} />
            {draft.primaryColor && (
              <div className="rounded-lg border p-3 space-y-2">
                <p className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">预览</p>
                <div className="flex items-center gap-3">
                  <div className="rounded-lg px-4 py-2 text-sm font-medium text-white" style={{ backgroundColor: draft.primaryColor }}>按钮样式</div>
                  <span className="text-sm font-medium" style={{ color: draft.primaryColor }}>链接颜色</span>
                  <div className="size-6 rounded-full border-2" style={{ borderColor: draft.primaryColor, backgroundColor: draft.primaryColor + "20" }} />
                </div>
              </div>
            )}
          </AccordionContent>
        </AccordionItem>

        {/* 登录页 */}
        <AccordionItem value="login" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><ImageIcon className="size-4 text-muted-foreground" /><span className="text-sm font-medium">登录页</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <Row label="背景图 URL" hint="登录页全屏背景图"><Input value={draft.loginBgURL} onChange={(e) => patch("loginBgURL", e.target.value)} placeholder="https://example.com/bg.jpg" /></Row>
            <ColorRow label="背景色" hint="无背景图时使用" value={draft.loginBgColor} onChange={(v) => patch("loginBgColor", v)} />
            {draft.loginBgURL && (
              <ImageLightbox
                src={draft.loginBgURL}
                alt="登录页背景图"
                caption="登录页背景图预览"
                hint="支持大图预览，方便检查裁切和清晰度"
                frameClassName="min-h-56 bg-card p-2"
                imageClassName="h-56 w-full rounded-2xl object-cover"
              />
            )}
          </AccordionContent>
        </AccordionItem>

        {/* 页脚 */}
        <AccordionItem value="footer" className="rounded-xl border overflow-hidden border-b-0">
          <AccordionTrigger className="hover:no-underline py-3 px-4">
            <div className="flex items-center gap-2"><Globe className="size-4 text-muted-foreground" /><span className="text-sm font-medium">页脚与高级</span></div>
          </AccordionTrigger>
          <AccordionContent className="px-4 pb-4 space-y-4">
            <Row label="页脚文本"><Input value={draft.footerText} onChange={(e) => patch("footerText", e.target.value)} placeholder="Powered by Aegis" /></Row>
            <Row label="自定义 CSS" hint="注入到页面 <style> 标签，覆盖全局样式">
              <Textarea value={draft.customCSS} onChange={(e) => patch("customCSS", e.target.value)} rows={6} className="font-mono text-xs" placeholder=".my-class { color: red; }" />
            </Row>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

function StatusCard({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div className="rounded-xl border bg-card px-4 py-3 space-y-1">
      <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{label}</div>
      <div className="flex items-center gap-2">
        {color && <div className="size-4 rounded-full border" style={{ backgroundColor: color }} />}
        <span className="text-sm font-semibold truncate">{value}</span>
      </div>
    </div>
  );
}



