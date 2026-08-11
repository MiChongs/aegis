"use client";

import { useCallback, useState } from "react";
import { CheckCircle2, Loader2, Save, TestTube, Trash2, X, XCircle } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type { PaymentProviderMeta } from "@/lib/api/types";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { PaymentBrandBadge } from "./payment-brand-icon";
import { PaymentCapabilityBadges, PaymentProviderFields, buildDefaultConfigData } from "./provider-fields";

type FormData = {
  paymentMethod: string;
  configName: string;
  enabled: boolean;
  isDefault: boolean;
  description: string;
  configData: Record<string, unknown>;
};

type TestResult = Record<string, unknown>;

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** 编辑态：已有配置；新建态为 null */
  editConfig?: Record<string, unknown> | null;
  /** 新建态：从渠道目录中选定的支付方式 */
  createMethod?: string | null;
  methods: PaymentProviderMeta[];
  appId?: number | null;
  onSave: (payload: Record<string, unknown>) => Promise<void>;
  onDelete?: () => Promise<void>;
  onTest?: () => Promise<TestResult | void>;
  isSaving?: boolean;
  isDeleting?: boolean;
  isTesting?: boolean;
};

/** 按「编辑既有配置」或「新建选定渠道」两种入口构造表单初值 */
function buildInitialForm(
  editConfig: Record<string, unknown> | null | undefined,
  createMethod: string | null | undefined,
  methods: PaymentProviderMeta[]
): FormData {
  if (editConfig) {
    return {
      paymentMethod: String(editConfig.payment_method ?? ""),
      configName: String(editConfig.config_name ?? ""),
      enabled: editConfig.enabled !== false,
      isDefault: !!editConfig.is_default,
      description: String(editConfig.description ?? ""),
      configData: (editConfig.config_data as Record<string, unknown>) || {}
    };
  }
  const method = createMethod ?? "";
  return {
    paymentMethod: method,
    configName: method ? `${method}-default` : "default",
    enabled: true,
    isDefault: false,
    description: "",
    // 用后端下发的字段默认值预填，减少必填项
    configData: buildDefaultConfigData(methods.find((m) => m.method === method))
  };
}

/**
 * 必填校验：返回第一个未填字段的显示名，全部填齐时返回 null。
 *
 * 表单上的 `*` 标记来自后端 schema，这里按同一份 schema 校验，
 * 避免「标了必填却能提交、由上游报错兜底」。后端当前没有 required 的高级项，
 * 因此不必为此展开折叠区；若将来出现，需同步让高级区自动展开。
 */
function findMissingField(form: FormData, meta?: PaymentProviderMeta | null): string | null {
  if (!form.configName.trim()) return "配置名称";
  for (const field of meta?.fields ?? []) {
    if (!field.required) continue;
    const value = form.configData[field.key];
    const empty =
      value === undefined ||
      value === null ||
      (typeof value === "string" && !value.trim()) ||
      (Array.isArray(value) && value.length === 0);
    if (empty) return field.label;
  }
  return null;
}

export function PaymentConfigForm({
  open,
  onOpenChange,
  editConfig,
  createMethod,
  methods,
  appId,
  onSave,
  onDelete,
  onTest,
  isSaving,
  isDeleting,
  isTesting
}: Props) {
  // 表单只在挂载时按 props 初始化一次。调用方每次打开表单都会换掉 key，
  // 因此这里不需要 useEffect 同步 props → state（那会触发级联渲染）。
  const [form, setForm] = useState<FormData>(() => buildInitialForm(editConfig, createMethod, methods));
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const isEdit = !!editConfig;

  const set = useCallback(<K extends keyof FormData>(key: K, value: FormData[K]) => {
    setForm((s) => ({ ...s, [key]: value }));
  }, []);

  const setConfigField = useCallback((key: string, value: unknown) => {
    setForm((s) => ({ ...s, configData: { ...s.configData, [key]: value } }));
  }, []);

  const meta = methods.find((m) => m.method === form.paymentMethod);

  async function handleSave() {
    const missing = findMissingField(form, meta);
    if (missing) {
      toast.error(`请先填写「${missing}」`);
      return;
    }
    const payload: Record<string, unknown> = {
      payment_method: form.paymentMethod,
      config_name: form.configName,
      enabled: form.enabled,
      is_default: form.isDefault,
      description: form.description,
      config_data: form.configData
    };
    if (appId) payload.appid = appId;
    if (editConfig?.id) payload.config_id = editConfig.id;
    try {
      await onSave(payload);
      toast.success(isEdit ? "配置已更新" : "配置已创建");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败");
    }
  }

  async function handleTest() {
    if (!onTest) return;
    try {
      const result = await onTest();
      setTestResult(result && typeof result === "object" ? result : { api_accessible: true });
    } catch (err) {
      setTestResult({ config_valid: false, error: err instanceof ApiError ? err.message : "连通测试失败" });
    }
  }

  async function handleDelete() {
    if (!onDelete) return;
    try {
      await onDelete();
      setConfirmDelete(false);
      toast.success("渠道配置已删除");
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  }

  const regions = meta?.regions?.length ? meta.regions.join(" / ") : "";
  const currencies = meta?.currencies?.length ? meta.currencies.slice(0, 6).join(" · ") : "";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      {/* 三段式：标题栏与操作条固定，只有中间表单区滚动 —— 长表单下「保存」不会被滚走 */}
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-2xl">
        <SheetHeader className="shrink-0 gap-0 border-b p-0">
          <div className="flex items-start gap-3 py-4 pr-12 pl-5">
            <PaymentBrandBadge
              slug={meta?.icon}
              brandColor={meta?.brandColor}
              name={meta?.name ?? form.paymentMethod}
              size="lg"
            />
            <div className="min-w-0 flex-1 space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <SheetTitle className="text-base">
                  {meta?.name ?? (isEdit ? "编辑支付渠道" : "接入支付渠道")}
                </SheetTitle>
                {meta?.categoryName && (
                  <Badge variant="outline" size="sm" className="font-normal">
                    {meta.categoryName}
                  </Badge>
                )}
              </div>
              <SheetDescription className="line-clamp-2 text-[11px] leading-relaxed">
                {meta?.description || (isEdit ? "调整该渠道的接入参数与启用状态" : "填写商户凭据后即可接入")}
              </SheetDescription>
            </div>
          </div>
        </SheetHeader>

        <ScrollArea className="min-h-0 flex-1">
          {/* 分栏按抽屉自身宽度切换（@container），不跟随视口 ——
              视口断点会在窄窗口里把 640px 宽的抽屉也排成两列，标签与提示全被挤到换行 */}
          <div className="@container/form space-y-5 px-5 py-4">
            {(regions || currencies || meta) && (
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg bg-muted/40 px-3 py-2.5 text-[11px] text-muted-foreground">
                {regions && (
                  <span>
                    <span className="text-muted-foreground/70">覆盖 </span>
                    {regions}
                  </span>
                )}
                {currencies && (
                  <span>
                    <span className="text-muted-foreground/70">币种 </span>
                    {currencies}
                  </span>
                )}
                {meta && <PaymentCapabilityBadges meta={meta} />}
              </div>
            )}

            {/* 基本设置 */}
            <Section title="基本设置">
              <div className="grid gap-3 @xl/form:grid-cols-2">
                <div className="space-y-1.5">
                  <Label className="text-xs">配置名称</Label>
                  <Input
                    className="h-8 text-sm"
                    placeholder="default"
                    value={form.configName}
                    onChange={(e) => set("configName", e.target.value)}
                  />
                  <p className="text-[11px] leading-relaxed text-muted-foreground">
                    同一渠道可建多套配置，下单时按名称选取
                  </p>
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">描述</Label>
                  <Input
                    className="h-8 text-sm"
                    placeholder="可选说明"
                    value={form.description}
                    onChange={(e) => set("description", e.target.value)}
                  />
                  <p className="text-[11px] leading-relaxed text-muted-foreground">仅在控制台展示，便于区分多套配置</p>
                </div>
              </div>
              <div className="grid gap-3 @xl/form:grid-cols-2">
                <ToggleField
                  label="启用该渠道"
                  hint="停用后用户端不再可见"
                  checked={form.enabled}
                  onChange={(v) => set("enabled", v)}
                />
                <ToggleField
                  label="设为默认配置"
                  hint="下单未指定配置名时使用"
                  checked={form.isDefault}
                  onChange={(v) => set("isDefault", v)}
                />
              </div>
            </Section>

            <Separator />

            {/* 渠道参数（由后端 schema 动态渲染） */}
            <Section title="渠道参数">
              <PaymentProviderFields meta={meta} data={form.configData} onChange={setConfigField} />
            </Section>
          </div>
        </ScrollArea>

        {/* 连通测试结果贴在操作条上方：测试按钮在固定区，结果若放进滚动区会看不见 */}
        {testResult && (
          <div className="@container/form max-h-52 shrink-0 overflow-y-auto border-t px-5 py-3">
            <TestResultPanel result={testResult} onDismiss={() => setTestResult(null)} />
          </div>
        )}

        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t px-5 py-3">
          {isEdit && onDelete && (
            <Button
              size="sm"
              variant="ghost"
              className="mr-auto h-8 gap-1 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive"
              disabled={isDeleting}
              onClick={() => setConfirmDelete(true)}
            >
              {isDeleting ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
              删除
            </Button>
          )}
          {onTest && (
            <Button size="sm" variant="outline" className="h-8 gap-1 text-xs" disabled={isTesting} onClick={handleTest}>
              {isTesting ? <Loader2 className="size-3 animate-spin" /> : <TestTube className="size-3" />}
              连通测试
            </Button>
          )}
          <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" className="h-8 gap-1 text-xs" disabled={isSaving} onClick={handleSave}>
            {isSaving ? <Loader2 className="size-3 animate-spin" /> : <Save className="size-3" />}
            {isSaving ? "保存中..." : "保存"}
          </Button>
        </div>

        <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>删除配置「{form.configName || "—"}」？</AlertDialogTitle>
              <AlertDialogDescription>
                删除后该配置立即从下单可选项中消失，已产生的订单与退款记录保留。商户凭据不可恢复，需要时请重新填写。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <AlertDialogAction disabled={isDeleting} onClick={handleDelete}>
                {isDeleting ? "删除中..." : "确认删除"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
  );
}

/**
 * 连通测试结果面板。
 *
 * 各渠道返回的字段不统一（config_valid / api_accessible / error 之外还有渠道特有信息，
 * 如 Square 的门店名、微信的证书下载结果），因此只把三个通用字段提为结论，
 * 其余原样列出，避免丢掉排障时最有用的上游报错原文。
 */
function TestResultPanel({ result, onDismiss }: { result: Record<string, unknown>; onDismiss: () => void }) {
  const configValid = result.config_valid !== false;
  const accessible = result.api_accessible === true;
  const error = typeof result.error === "string" ? result.error : "";
  const ok = configValid && accessible;
  const extras = Object.entries(result).filter(
    ([k]) => !["config_valid", "api_accessible", "error"].includes(k)
  );

  return (
    <div
      className={`space-y-2 rounded-xl border p-3 ${ok ? "border-emerald-500/30 bg-emerald-500/5" : "border-amber-500/30 bg-amber-500/5"}`}
    >
      <div className="flex items-center gap-2">
        {ok ? (
          <CheckCircle2 className="size-4 shrink-0 text-emerald-500" />
        ) : (
          <XCircle className="size-4 shrink-0 text-amber-500" />
        )}
        <span className="text-xs font-medium">
          {ok ? "连通正常，凭据有效" : configValid ? "配置格式有效，但上游不可达" : "配置校验未通过"}
        </span>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          className="ml-auto size-6 shrink-0"
          onClick={onDismiss}
          aria-label="关闭测试结果"
        >
          <X className="size-3" />
        </Button>
      </div>
      {error && (
        <p className="rounded-lg bg-background/70 px-2 py-1.5 font-mono text-[11px] leading-relaxed break-all">
          {error}
        </p>
      )}
      {extras.length > 0 && (
        <dl className="grid gap-x-4 gap-y-1 @xl/form:grid-cols-2">
          {extras.map(([key, value]) => (
            <div key={key} className="flex items-baseline gap-2 text-[11px]">
              <dt className="shrink-0 text-muted-foreground">{key}</dt>
              <dd className="min-w-0 truncate font-mono">{String(value)}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
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

function ToggleField({
  label,
  hint,
  checked,
  onChange
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/40">
      <Switch checked={checked} onCheckedChange={onChange} className="mt-0.5 shrink-0" />
      <span className="min-w-0 space-y-0.5">
        <span className="block text-xs">{label}</span>
        {hint && <span className="block text-[11px] leading-relaxed text-muted-foreground">{hint}</span>}
      </span>
    </label>
  );
}
