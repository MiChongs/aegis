"use client";

import { useMemo, useState } from "react";
import { FileJson, Loader2, RotateCcw, Save, Sparkles, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { AppFunction, FunctionCatalog } from "@/lib/api/app-functions";
import { useDeleteFunctionMutation, useUpdateFunctionMutation } from "@/lib/function-hooks";
import { INPUT_SCHEMA_META } from "@/lib/monaco/json-schema-meta";
import { JsonEditor } from "@/components/functions/json-editor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import { CapabilityPicker, HighRiskNotice, errorMessage } from "./function-shared";

type Form = {
  description: string;
  status: AppFunction["status"];
  capabilities: string[];
  timeoutMs: number;
  maxRequestBytes: number;
  maxResponseBytes: number;
  maxConcurrency: number;
  rateLimitPerMin: number;
  config: string;
  inputSchema: string;
};

function seed(item: AppFunction): Form {
  return {
    description: item.description,
    status: item.status,
    capabilities: item.capabilities,
    timeoutMs: item.timeoutMs,
    maxRequestBytes: item.maxRequestBytes,
    maxResponseBytes: item.maxResponseBytes,
    maxConcurrency: item.maxConcurrency,
    rateLimitPerMin: item.rateLimitPerMin,
    config: JSON.stringify(item.config ?? {}, null, 2),
    inputSchema: JSON.stringify(item.inputSchema ?? {}, null, 2)
  };
}

/** 解析一段必须是 JSON 对象的文本；失败时抛出可直接展示的原因。 */
function parseJSONObject(text: string, label: string): Record<string, unknown> {
  const parsed = JSON.parse(text || "{}");
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`${label}顶层必须是 JSON 对象`);
  }
  return parsed as Record<string, unknown>;
}

/**
 * 函数设置。
 *
 * 重构前这一整页是不存在的：函数建出来之后，能力、超时、限额全部锁死，
 * 想加一项能力只能删掉重建 —— 而删掉会连同全部版本与调用审计一起消失。
 */
export function FunctionSettingsPanel({
  appKey,
  selected,
  catalog,
  onDeleted
}: {
  appKey: string;
  selected: AppFunction;
  catalog?: FunctionCatalog;
  onDeleted: () => void;
}) {
  const scope = `${appKey}:${selected.name}`;
  // 草稿按作用域绑定，不用 effect 同步：切换函数时上一个函数的
  // 未保存改动不会串过来（与配置面板同一条约束）。
  const [draft, setDraft] = useState<{ scope: string; value: Form } | null>(null);
  const form = draft?.scope === scope ? draft.value : seed(selected);
  const patch = <K extends keyof Form>(key: K, value: Form[K]) =>
    setDraft({ scope, value: { ...form, [key]: value } });

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [confirmName, setConfirmName] = useState("");
  const updateMutation = useUpdateFunctionMutation(appKey);
  const deleteMutation = useDeleteFunctionMutation(appKey);

  const limits = catalog?.limits;

  // 顶层字段数即「这份契约约束了什么」。解不开时按 0 处理 ——
  // 编辑到一半的 JSON 本来就解不开，那不该让徽标闪成红色。
  const schemaFieldCount = useMemo(() => {
    try {
      const parsed = JSON.parse(form.inputSchema || "{}");
      return Object.keys(parsed?.properties ?? {}).length;
    } catch {
      return 0;
    }
  }, [form.inputSchema]);

  async function save() {
    let config: Record<string, unknown>;
    let inputSchema: Record<string, unknown>;
    try {
      // 顶层不是对象时脚本里 aegis.config.xxx 恒为 undefined，
      // 而那种失败不会报错，只会让阈值静默变回默认值 —— 必须在这里拦住
      config = parseJSONObject(form.config, "函数配置");
      inputSchema = parseJSONObject(form.inputSchema, "入参契约");
    } catch (error) {
      toast.error(errorMessage(error));
      return;
    }
    try {
      await updateMutation.mutateAsync({
        name: selected.name,
        payload: {
          description: form.description,
          status: form.status,
          capabilities: form.capabilities,
          timeoutMs: form.timeoutMs,
          maxRequestBytes: form.maxRequestBytes,
          maxResponseBytes: form.maxResponseBytes,
          maxConcurrency: form.maxConcurrency,
          rateLimitPerMin: form.rateLimitPerMin,
          config,
          inputSchema
        }
      });
      setDraft(null);
      toast.success("设置已保存");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div>
            <CardTitle>基本信息</CardTitle>
            <CardDescription>
              状态是函数自己的开关；被停用时调用直接返回 40990，已发布的版本不受影响。
            </CardDescription>
          </div>
          <div className="flex shrink-0 gap-2">
            {draft?.scope === scope ? (
              <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
                <RotateCcw className="size-4" />
                重置
              </Button>
            ) : null}
            <Button size="sm" disabled={updateMutation.isPending} onClick={save}>
              {updateMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              保存
            </Button>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2 sm:col-span-2">
            <Label>说明</Label>
            <Input
              value={form.description}
              onChange={(event) => patch("description", event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label>状态</Label>
            <Select
              value={form.status}
              onValueChange={(value) => patch("status", value as AppFunction["status"])}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="draft">draft（未发布）</SelectItem>
                <SelectItem value="active">active（可被调用）</SelectItem>
                <SelectItem value="disabled">disabled（停用）</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>Runtime</Label>
            <Input value={selected.runtime} readOnly className="font-mono" />
            <p className="text-[11px] text-muted-foreground">
              运行时决定了版本产物的形态，建好之后不可更改。
            </p>
          </div>
        </CardContent>
      </Card>

      {selected.runtime === "script" ? (
        <Card>
          <CardHeader>
            <CardTitle>Capabilities</CardTitle>
            <CardDescription>
              改动保存后立即生效于下一次调用，不需要重新发布版本 ——
              但砍掉一项能力会让正在用它的脚本当场报错，请先确认脚本里没有用到。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <CapabilityPicker
              catalog={catalog?.capabilities ?? []}
              selected={form.capabilities}
              onChange={(next) => patch("capabilities", next)}
            />
            <HighRiskNotice catalog={catalog?.capabilities ?? []} selected={form.capabilities} />
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>运行闸门</CardTitle>
          <CardDescription>
            并发是「同时能跑几个」，按实例算，保护的是本进程；频次是「一分钟能跑几次」，
            计数落在数据库上，多实例部署下仍然准确。两者管的不是同一件事。
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <NumberField
            label="超时 (ms)"
            value={form.timeoutMs}
            min={10}
            max={limits?.maxTimeoutMs ?? 30000}
            onChange={(value) => patch("timeoutMs", value)}
            hint="超时会中断脚本，死循环也挡得住"
          />
          <NumberField
            label="并发上限"
            value={form.maxConcurrency}
            min={1}
            max={limits?.maxConcurrency ?? 64}
            onChange={(value) => patch("maxConcurrency", value)}
            hint="单实例同时执行数，超出返回 42990"
          />
          <NumberField
            label="频次上限 (次/分钟)"
            value={form.rateLimitPerMin}
            min={0}
            max={600000}
            onChange={(value) => patch("rateLimitPerMin", value)}
            hint="0 表示不限；超出返回 42991"
          />
          <NumberField
            label="请求上限 (字节)"
            value={form.maxRequestBytes}
            min={1}
            max={1048576}
            onChange={(value) => patch("maxRequestBytes", value)}
          />
          <NumberField
            label="响应上限 (字节)"
            value={form.maxResponseBytes}
            min={1}
            max={1048576}
            onChange={(value) => patch("maxResponseBytes", value)}
          />
        </CardContent>
      </Card>

      {selected.runtime === "script" ? (
        <Card>
          <CardHeader className="flex-row items-start justify-between gap-3">
            <div className="min-w-0">
              <CardTitle className="flex items-center gap-2">
                入参契约
                {schemaFieldCount ? (
                  <Badge variant="success" size="sm" className="gap-1">
                    <FileJson className="size-3" />
                    {schemaFieldCount} 个字段
                  </Badge>
                ) : (
                  <Badge variant="outline" size="sm">
                    未配置
                  </Badge>
                )}
              </CardTitle>
              <CardDescription>
                一份 JSON Schema，同时驱动三处：调用入口的前置校验、试跑输入框的补全与校验、
                以及编辑器里 <code className="font-mono">ctx.input</code> 的真实类型。
                <span className="mt-1 block">
                  不配的话，接入方少传一个字段的表现是脚本第三行抛 TypeError，
                  而调用方拿到的是一句 50290「应用函数执行失败」—— 既不说少了什么，
                  也不说是自己传错了。
                </span>
              </CardDescription>
            </div>
            {catalog?.inputSchemaTemplate && !schemaFieldCount ? (
              <Button
                size="sm"
                variant="outline"
                className="shrink-0"
                onClick={() => patch("inputSchema", catalog.inputSchemaTemplate)}
              >
                <Sparkles className="size-4" />
                从样例开始
              </Button>
            ) : null}
          </CardHeader>
          <CardContent className="space-y-2">
            {/* 元 schema 喂给 JSON 语言服务：关键字补全、取值枚举、
                悬浮说明。只登记服务端真的会处理的那一批关键字 ——
                一个能补全出来、却不起作用的关键字比补不出来更误导人。 */}
            <JsonEditor
              value={form.inputSchema}
              onChange={(next) => patch("inputSchema", next)}
              schema={INPUT_SCHEMA_META}
              height={260}
            />
            <p className="text-xs text-muted-foreground">
              留空（<code className="font-mono">{"{}"}</code>）表示不约束，与配它之前的行为一致。
              保存时会真的编译一遍 —— 一份编译不过的 schema 在调用时的表现是「校验永远抛错」
              或者更糟：「校验被跳过」。
            </p>
          </CardContent>
        </Card>
      ) : null}

      {selected.runtime === "script" ? (
        <Card>
          <CardHeader>
            <CardTitle>函数配置</CardTitle>
            <CardDescription>
              脚本里读作 <code className="font-mono">aegis.config</code>。改一个阈值不该需要发一个
              新版本 —— 版本不可变说的是逻辑，不是每日额度这种数字。永不下发给接入方。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            <JsonEditor
              value={form.config}
              onChange={(next) => patch("config", next)}
              height={180}
            />
            <p className="text-xs text-muted-foreground">
              顶层必须是 JSON 对象。例如{" "}
              <code className="font-mono">{`{"dailyQuota": 100, "endpoint": "https://…"}`}</code>
              。这里的键会出现在脚本编辑器 <code className="font-mono">aegis.config.</code>{" "}
              的补全里，并带上当前值。
            </p>
          </CardContent>
        </Card>
      ) : null}

      <Card className="border-destructive/40">
        <CardHeader>
          <CardTitle className="text-destructive">删除函数</CardTitle>
          <CardDescription>
            将连同全部版本与调用审计一起删除，不可恢复。只想临时停掉请把状态改成 disabled。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
            <Trash2 className="size-4" />
            删除 {selected.name}
          </Button>
        </CardContent>
      </Card>

      <Dialog
        open={deleteOpen}
        onOpenChange={(next) => {
          setDeleteOpen(next);
          if (!next) setConfirmName("");
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除函数</DialogTitle>
            <DialogDescription>
              请输入函数名 <code className="font-mono">{selected.name}</code> 以确认。
              删除后调用方会立即收到 40490，且历史调用记录一并消失。
            </DialogDescription>
          </DialogHeader>
          <Input
            value={confirmName}
            onChange={(event) => setConfirmName(event.target.value)}
            placeholder={selected.name}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={confirmName !== selected.name || deleteMutation.isPending}
              onClick={async () => {
                try {
                  await deleteMutation.mutateAsync(selected.name);
                  setDeleteOpen(false);
                  setConfirmName("");
                  onDeleted();
                  toast.success("函数已删除");
                } catch (error) {
                  toast.error(errorMessage(error));
                }
              }}
            >
              <Trash2 className="size-4" />
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function NumberField({
  label,
  value,
  min,
  max,
  hint,
  onChange
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  hint?: string;
  onChange: (value: number) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <Input
        type="number"
        min={min}
        max={max}
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
      />
      <p className="text-[11px] text-muted-foreground">
        {hint ? `${hint} · ` : ""}
        {min}–{max}
      </p>
    </div>
  );
}
