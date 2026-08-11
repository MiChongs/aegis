"use client";

import { CopyPlus, Play, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { WorkflowTemplate } from "@/lib/api-client";
import { toText } from "./workflow-helpers";

type StartDialogProps = {
  open: boolean;
  loading?: boolean;
  form: { instanceName: string; priority: string; inputText: string };
  onOpenChange: (open: boolean) => void;
  onChange: (next: { instanceName: string; priority: string; inputText: string }) => void;
  onSubmit: () => void;
};

export function WorkflowStartDialog({
  open,
  loading,
  form,
  onOpenChange,
  onChange,
  onSubmit
}: StartDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>启动工作流</DialogTitle>
          <DialogDescription>创建新的工作流实例，并传入启动参数。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="space-y-2">
            <Label htmlFor="instance-name">实例名称</Label>
            <Input
              id="instance-name"
              value={form.instanceName}
              onChange={(event) => onChange({ ...form, instanceName: event.target.value })}
              placeholder="可选"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="instance-priority">优先级</Label>
            <Input
              id="instance-priority"
              value={form.priority}
              onChange={(event) => onChange({ ...form, priority: event.target.value })}
              placeholder="默认 5"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="instance-input">启动输入 JSON</Label>
            <Textarea
              id="instance-input"
              className="min-h-[180px] font-mono text-xs leading-6"
              value={form.inputText}
              onChange={(event) => onChange({ ...form, inputText: event.target.value })}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={onSubmit} disabled={loading}>
            <Play className="size-4" />
            立即启动
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type SaveTemplateDialogProps = {
  open: boolean;
  loading?: boolean;
  form: { templateName: string; templateDescription: string; category: string; isPublic: string };
  onOpenChange: (open: boolean) => void;
  onChange: (next: { templateName: string; templateDescription: string; category: string; isPublic: string }) => void;
  onSubmit: () => void;
};

export function WorkflowSaveTemplateDialog({
  open,
  loading,
  form,
  onOpenChange,
  onChange,
  onSubmit
}: SaveTemplateDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>保存为模板</DialogTitle>
          <DialogDescription>将当前工作流定义保存为可复用模板。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="space-y-2">
            <Label htmlFor="template-name">模板名称</Label>
            <Input
              id="template-name"
              value={form.templateName}
              onChange={(event) => onChange({ ...form, templateName: event.target.value })}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="template-category">模板分类</Label>
            <Input
              id="template-category"
              value={form.category}
              onChange={(event) => onChange({ ...form, category: event.target.value })}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="template-description">模板说明</Label>
            <Textarea
              id="template-description"
              className="min-h-[120px]"
              value={form.templateDescription}
              onChange={(event) => onChange({ ...form, templateDescription: event.target.value })}
            />
          </div>
          <div className="space-y-2">
            <Label>可见性</Label>
            <Select value={form.isPublic} onValueChange={(value) => onChange({ ...form, isPublic: value })}>
              <SelectTrigger>
                <SelectValue placeholder="选择可见性" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="private">私有</SelectItem>
                <SelectItem value="public">公开</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={onSubmit} disabled={loading}>
            <Save className="size-4" />
            保存模板
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type FromTemplateDialogProps = {
  open: boolean;
  loading?: boolean;
  templates: WorkflowTemplate[];
  form: { templateId: string; name: string; description: string };
  onOpenChange: (open: boolean) => void;
  onChange: (next: { templateId: string; name: string; description: string }) => void;
  onSubmit: () => void;
};

export function WorkflowFromTemplateDialog({
  open,
  loading,
  templates,
  form,
  onOpenChange,
  onChange,
  onSubmit
}: FromTemplateDialogProps) {
  const selectedTemplate = templates.find((item) => String(item.id) === form.templateId) || null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>从模板创建工作流</DialogTitle>
          <DialogDescription>选择已有模板，快速生成新的工作流定义。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="space-y-2">
            <Label>模板</Label>
            <Select
              value={form.templateId || "placeholder-template"}
              onValueChange={(value) => onChange({ ...form, templateId: value === "placeholder-template" ? "" : value })}
            >
              <SelectTrigger>
                <SelectValue placeholder="选择模板" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="placeholder-template">选择模板</SelectItem>
                {templates.map((item) => (
                  <SelectItem key={item.id} value={String(item.id)}>
                    {toText(item.name, `模板 ${item.id}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {selectedTemplate ? (
            <div className="rounded-2xl border border-border/80 bg-muted/40 p-3 text-sm text-muted-foreground">
              {toText(selectedTemplate.description, "模板未填写说明。")}
            </div>
          ) : null}
          <div className="space-y-2">
            <Label htmlFor="workflow-name-from-template">工作流名称</Label>
            <Input
              id="workflow-name-from-template"
              value={form.name}
              onChange={(event) => onChange({ ...form, name: event.target.value })}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="workflow-description-from-template">工作流说明</Label>
            <Textarea
              id="workflow-description-from-template"
              className="min-h-[120px]"
              value={form.description}
              onChange={(event) => onChange({ ...form, description: event.target.value })}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={onSubmit} disabled={loading}>
            <CopyPlus className="size-4" />
            创建工作流
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
