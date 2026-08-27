"use client";

import { useState } from "react";
import { Loader2, Plus } from "lucide-react";
import { toast } from "sonner";
import type { AppFunctionRuntime, FunctionCatalog } from "@/lib/api/app-functions";
import { useCreateFunctionMutation } from "@/lib/function-hooks";
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
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { CapabilityPicker, HighRiskNotice, errorMessage } from "./function-shared";

const RUNTIME_HINT: Record<AppFunctionRuntime, string> = {
  script: "在控制台编写逻辑，运行于 Aegis 进程内，可读写平台数据，适用于自定义 API。",
  wasm: "纯计算沙箱，无法访问平台数据，适用于确定性算法。",
  http: "转发至自建 HTTPS 端点，需实现 Ed25519 双向签名。"
};

/**
 * 创建远程函数。
 *
 * 模板选择放在这里而不是「新建版本」时：模板自带它需要的能力，
 * 建函数时就把 capabilities 勾好，作者才不会在写完脚本之后
 * 才发现少声明一项 —— 而那时编辑器已经把那些调用标红一路了。
 */
export function CreateFunctionDialog({
  open,
  onOpenChange,
  appKey,
  catalog,
  onCreated
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  appKey: string;
  catalog?: FunctionCatalog;
  onCreated: (name: string, template?: string) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [runtime, setRuntime] = useState<AppFunctionRuntime>("script");
  const [templateKey, setTemplateKey] = useState("starter");
  const [capabilities, setCapabilities] = useState<string[]>(["user.read"]);
  const createMutation = useCreateFunctionMutation(appKey);

  const templates = catalog?.templates ?? [];
  const capabilityCatalog = catalog?.capabilities ?? [];

  function applyTemplate(key: string) {
    setTemplateKey(key);
    const template = templates.find((item) => item.key === key);
    if (template) setCapabilities(template.capabilities);
  }

  function reset() {
    setName("");
    setDescription("");
    setRuntime("script");
    setTemplateKey("starter");
    setCapabilities(["user.read"]);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>创建远程函数</DialogTitle>
          <DialogDescription>
            函数归属当前应用。创建后在「脚本」页编写逻辑，发布并激活版本后即可被调用。
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="-mx-6 max-h-[60vh] px-6">
          <div className="grid gap-4 pb-1">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>函数名</Label>
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="sync-user-profile"
                />
                <p className="text-[11px] text-muted-foreground">
                  小写字母开头，可含数字与 <code className="font-mono">. _ -</code>
                </p>
              </div>
              <div className="space-y-2">
                <Label>说明</Label>
                <Input
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  placeholder="函数用途简述"
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label>运行时</Label>
              <Select value={runtime} onValueChange={(value) => setRuntime(value as AppFunctionRuntime)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="script">服务端脚本（JavaScript）</SelectItem>
                  <SelectItem value="wasm">WASM 沙箱（纯计算）</SelectItem>
                  <SelectItem value="http">HTTP 端点（自建服务）</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{RUNTIME_HINT[runtime]}</p>
            </div>

            {runtime === "script" && templates.length ? (
              <div className="space-y-2">
                <Label>起始模板</Label>
                <div className="grid gap-2 sm:grid-cols-2">
                  {templates.map((template) => (
                    <button
                      type="button"
                      key={template.key}
                      onClick={() => applyTemplate(template.key)}
                      className={cn(
                        "rounded-lg border p-2.5 text-left text-xs transition-colors",
                        templateKey === template.key
                          ? "border-primary/60 bg-muted/50"
                          : "hover:bg-muted/40"
                      )}
                    >
                      <span className="font-medium">{template.title}</span>
                      <span className="mt-0.5 block leading-snug text-muted-foreground">
                        {template.summary}
                      </span>
                    </button>
                  ))}
                </div>
                <p className="text-[11px] text-muted-foreground">
                  选择模板将自动勾选所需能力，脚本正文在创建后预填。
                </p>
              </div>
            ) : null}

            {runtime === "script" ? (
              <div className="space-y-2">
                <Label>能力声明</Label>
                <CapabilityPicker
                  catalog={capabilityCatalog}
                  selected={capabilities}
                  onChange={setCapabilities}
                />
                <HighRiskNotice catalog={capabilityCatalog} selected={capabilities} />
              </div>
            ) : (
              <p className="rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
                {runtime === "wasm"
                  ? "WASM 运行时无法访问平台数据，无需声明能力。"
                  : "HTTP 端点由外部服务实现，平台仅负责转发与双向签名，无需声明能力。"}
              </p>
            )}
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button
            disabled={createMutation.isPending || !name.trim()}
            onClick={async () => {
              try {
                // 模板自带的入参契约一并写入：形状校验交给平台在调用入口做，
                // 而不是让每个作者在脚本里手写一遍
                const template = templates.find((item) => item.key === templateKey);
                const created = await createMutation.mutateAsync({
                  name: name.trim(),
                  description,
                  runtime,
                  capabilities: runtime === "script" ? capabilities : [],
                  inputSchema:
                    runtime === "script" && template?.inputSchema
                      ? (JSON.parse(template.inputSchema) as Record<string, unknown>)
                      : undefined
                });
                onCreated(created.name, runtime === "script" ? templateKey : undefined);
                onOpenChange(false);
                reset();
                toast.success("函数已创建");
              } catch (error) {
                toast.error(errorMessage(error));
              }
            }}
          >
            {createMutation.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
