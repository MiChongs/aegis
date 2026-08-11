"use client";

import { useState } from "react";
import { Code2, Eye, FileText, Loader2, Mail, MessageSquare, Plus, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import { previewMessageTemplate } from "@/lib/api/admin";
import {
  useMessageTemplatesQuery,
  useCreateMessageTemplateMutation,
  useUpdateMessageTemplateMutation,
  useDeleteMessageTemplateMutation,
} from "@/lib/admin-hooks";
import { useAuthStore } from "@/lib/auth-store";
import type { MessageTemplate, RenderResult, TemplateVariable } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";

const channelLabel: Record<string, string> = { email: "邮件", sms: "短信", notification: "通知" };
const channelIcon: Record<string, typeof Mail> = { email: Mail, sms: MessageSquare, notification: FileText };

export default function TemplatesPage() {
  const templatesQuery = useMessageTemplatesQuery();
  const deleteMutation = useDeleteMessageTemplateMutation();
  const [editing, setEditing] = useState<MessageTemplate | null>(null);
  const [creating, setCreating] = useState(false);

  if (templatesQuery.isLoading) return <LoadingState title="加载模板" description="" />;
  const templates = templatesQuery.data || [];

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="消息模板" />
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">管理邮件、短信模板，支持变量插入和实时预览</p>
        <Button size="sm" onClick={() => setCreating(true)}><Plus className="size-3.5" />创建模板</Button>
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {templates.map((t) => {
          const Icon = channelIcon[t.channel] || FileText;
          return (
            <div key={t.code} className="rounded-xl border bg-card p-4 space-y-3 cursor-pointer hover:border-primary/30 transition-colors" onClick={() => setEditing(t)}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Icon className="size-4 text-muted-foreground" />
                  <span className="text-sm font-semibold">{t.name}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <Badge variant={t.isBuiltin ? "secondary" : "outline"} className="text-[9px]">{t.isBuiltin ? "内置" : "自定义"}</Badge>
                  <Badge variant="outline" className="text-[9px]">{channelLabel[t.channel]}</Badge>
                </div>
              </div>
              <p className="text-xs text-muted-foreground line-clamp-2">{t.description || t.subject || "无描述"}</p>
              <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                <span className="font-mono">{t.code}</span>
                <span>{t.variables?.length || 0} 个变量</span>
              </div>
            </div>
          );
        })}
      </div>

      {/* 编辑 Sheet */}
      <Sheet open={!!editing} onOpenChange={(o) => { if (!o) setEditing(null); }}>
        <SheetContent className="w-[640px] sm:max-w-2xl overflow-y-auto">
          {editing && <TemplateEditor template={editing} onClose={() => setEditing(null)} />}
        </SheetContent>
      </Sheet>

      {/* 创建 Sheet */}
      <Sheet open={creating} onOpenChange={setCreating}>
        <SheetContent className="w-[640px] sm:max-w-2xl overflow-y-auto">
          <TemplateCreator onClose={() => setCreating(false)} />
        </SheetContent>
      </Sheet>
    </div>
  );
}

function TemplateEditor({ template, onClose }: { template: MessageTemplate; onClose: () => void }) {
  const updateMutation = useUpdateMessageTemplateMutation();
  const token = useAuthStore((s) => s.accessToken);
  const [subject, setSubject] = useState(template.subject);
  const [bodyHtml, setBodyHtml] = useState(template.bodyHtml);
  const [bodyText, setBodyText] = useState(template.bodyText);
  const [enabled, setEnabled] = useState(template.enabled);
  const [preview, setPreview] = useState<RenderResult | null>(null);
  const [previewData, setPreviewData] = useState<Record<string, string>>(() => {
    const d: Record<string, string> = {};
    for (const v of template.variables || []) d[v.key] = v.example || "";
    return d;
  });

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({ code: template.code, subject, bodyHtml, bodyText, enabled });
      toast.success("模板已保存");
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "保存失败");
    }
  };

  const handlePreview = async () => {
    if (!token) return;
    try {
      const result = await previewMessageTemplate(token, template.code, previewData);
      setPreview(result);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "预览失败");
    }
  };

  return (
    <div className="space-y-4 pt-4">
      <SheetHeader>
        <SheetTitle className="flex items-center gap-2">
          {template.name}
          <Badge variant={template.isBuiltin ? "secondary" : "outline"} className="text-[9px]">{template.isBuiltin ? "内置" : "自定义"}</Badge>
        </SheetTitle>
      </SheetHeader>

      <Tabs defaultValue="edit">
        <TabsList>
          <TabsTrigger value="edit"><Code2 className="size-3.5" />编辑</TabsTrigger>
          <TabsTrigger value="preview"><Eye className="size-3.5" />预览</TabsTrigger>
        </TabsList>

        <TabsContent value="edit" className="space-y-4">
          <div className="flex items-center justify-between">
            <Label className="text-xs">启用</Label>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </div>

          {template.channel === "email" && (
            <>
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label className="text-xs">邮件主题</Label>
                  <VariableButtons variables={template.variables} onInsert={(v) => setSubject((s) => s + `{{.${v}}}`)} />
                </div>
                <Input value={subject} onChange={(e) => setSubject(e.target.value)} className="font-mono text-xs" />
              </div>
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label className="text-xs">HTML 正文</Label>
                  <VariableButtons variables={template.variables} onInsert={(v) => setBodyHtml((s) => s + `{{.${v}}}`)} />
                </div>
                <Textarea value={bodyHtml} onChange={(e) => setBodyHtml(e.target.value)} rows={12} className="font-mono text-xs" />
              </div>
            </>
          )}

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label className="text-xs">{template.channel === "email" ? "纯文本正文（备用）" : "正文"}</Label>
              <VariableButtons variables={template.variables} onInsert={(v) => setBodyText((s) => s + `{{.${v}}}`)} />
            </div>
            <Textarea value={bodyText} onChange={(e) => setBodyText(e.target.value)} rows={4} className="font-mono text-xs" />
          </div>

          <Button className="w-full" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </TabsContent>

        <TabsContent value="preview" className="space-y-4">
          <div className="space-y-2">
            <Label className="text-xs font-semibold">变量值</Label>
            <div className="grid gap-2 grid-cols-2">
              {(template.variables || []).map((v) => (
                <div key={v.key} className="space-y-1">
                  <Label className="text-[10px] text-muted-foreground">{v.name} ({v.key})</Label>
                  <Input value={previewData[v.key] || ""} onChange={(e) => setPreviewData((d) => ({ ...d, [v.key]: e.target.value }))} className="h-7 text-xs" placeholder={v.example} />
                </div>
              ))}
            </div>
            <Button variant="outline" size="sm" onClick={handlePreview}><Eye className="size-3.5" />预览渲染</Button>
          </div>

          {preview && (
            <div className="space-y-3">
              <Separator />
              {preview.subject && (
                <div><Label className="text-[10px] text-muted-foreground">主题</Label><p className="text-sm font-medium">{preview.subject}</p></div>
              )}
              {preview.bodyHtml && (
                <div className="space-y-1">
                  <Label className="text-[10px] text-muted-foreground">HTML 渲染</Label>
                  <div className="rounded-lg border bg-white p-4" dangerouslySetInnerHTML={{ __html: preview.bodyHtml }} />
                </div>
              )}
              {preview.bodyText && (
                <div><Label className="text-[10px] text-muted-foreground">纯文本</Label><pre className="text-xs bg-muted/50 rounded-lg p-3 whitespace-pre-wrap">{preview.bodyText}</pre></div>
              )}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

function TemplateCreator({ onClose }: { onClose: () => void }) {
  const createMutation = useCreateMessageTemplateMutation();
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [channel, setChannel] = useState("email");
  const [subject, setSubject] = useState("");
  const [bodyHtml, setBodyHtml] = useState("");
  const [bodyText, setBodyText] = useState("");

  const handleCreate = async () => {
    if (!code || !name) { toast.error("请填写代码和名称"); return; }
    try {
      await createMutation.mutateAsync({ code, name, channel, subject, bodyHtml, bodyText, variables: [] });
      toast.success("模板已创建");
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "创建失败");
    }
  };

  return (
    <div className="space-y-4 pt-4">
      <SheetHeader><SheetTitle>创建消息模板</SheetTitle></SheetHeader>
      <div className="grid gap-3 grid-cols-2">
        <div className="space-y-1.5"><Label className="text-xs">模板代码</Label><Input value={code} onChange={(e) => setCode(e.target.value.replace(/[^a-z0-9_]/g, ""))} placeholder="custom_my_template" className="font-mono" /></div>
        <div className="space-y-1.5"><Label className="text-xs">名称</Label><Input value={name} onChange={(e) => setName(e.target.value)} /></div>
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">渠道</Label>
        <Select value={channel} onValueChange={setChannel}>
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="email">邮件</SelectItem>
            <SelectItem value="sms">短信</SelectItem>
            <SelectItem value="notification">通知</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {channel === "email" && (
        <>
          <div className="space-y-1.5"><Label className="text-xs">邮件主题</Label><Input value={subject} onChange={(e) => setSubject(e.target.value)} className="font-mono text-xs" /></div>
          <div className="space-y-1.5"><Label className="text-xs">HTML 正文</Label><Textarea value={bodyHtml} onChange={(e) => setBodyHtml(e.target.value)} rows={8} className="font-mono text-xs" /></div>
        </>
      )}
      <div className="space-y-1.5"><Label className="text-xs">纯文本正文</Label><Textarea value={bodyText} onChange={(e) => setBodyText(e.target.value)} rows={4} className="font-mono text-xs" /></div>
      <Button className="w-full" onClick={handleCreate} disabled={createMutation.isPending}>
        {createMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null} 创建
      </Button>
    </div>
  );
}

function VariableButtons({ variables, onInsert }: { variables?: TemplateVariable[]; onInsert: (key: string) => void }) {
  if (!variables?.length) return null;
  return (
    <div className="flex items-center gap-1 flex-wrap">
      {variables.map((v) => (
        <button key={v.key} type="button" onClick={() => onInsert(v.key)}
          className="rounded border px-1.5 py-0.5 text-[9px] font-mono text-muted-foreground hover:bg-muted/50 transition-colors">
          {v.key}
        </button>
      ))}
    </div>
  );
}
