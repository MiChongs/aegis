"use client";

import { useMemo, useState, type ReactNode } from "react";
import { Eye, EyeOff, Loader2, Plus, RotateCcw, Save, Send, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import type {
  AdminTestSMSResult,
  CaptchaAppConfig,
  CaptchaSMSTemplateConfig
} from "@/lib/api/captcha";
import {
  useAdminCaptchaConfigQuery,
  useTestAdminCaptchaSMSMutation,
  useUpdateAdminCaptchaConfigMutation
} from "@/lib/admin-hooks";
import { DynamicCaptchaDesigner, normalizeDynamicConfig } from "@/components/captcha/dynamic-captcha-designer";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

type Props = { appKey?: string | null };

type CaptchaToggleKey =
  | "imageEnabled"
  | "mathEnabled"
  | "digitEnabled"
  | "dynamicEnabled"
  | "audioEnabled"
  | "chiralEnabled"
  | "smsEnabled";

const captchaToggleOptions: Array<{ key: CaptchaToggleKey; type: string; label: string }> = [
  { key: "imageEnabled", type: "image", label: "图形字符" },
  { key: "mathEnabled", type: "math", label: "算术" },
  { key: "digitEnabled", type: "digit", label: "纯数字" },
  { key: "dynamicEnabled", type: "dynamic", label: "动态图片" },
  { key: "audioEnabled", type: "audio", label: "音频" },
  { key: "chiralEnabled", type: "chiral", label: "手性碳" },
  { key: "smsEnabled", type: "sms", label: "短信" }
];

const smsProviders = [
  { value: "aliyun", label: "阿里云" },
  { value: "tencent", label: "腾讯云" }
] as const;

const testPurposeOptions = [
  { value: "login", label: "login" },
  { value: "register", label: "register" },
  { value: "reset_password", label: "reset_password" },
  { value: "bind_phone", label: "bind_phone" },
  { value: "verify_identity", label: "verify_identity" },
  { value: "change_email", label: "change_email" },
  { value: "change_phone", label: "change_phone" },
  { value: "admin_login", label: "admin_login" },
  { value: "test", label: "test" },
  { value: "custom", label: "custom" }
] as const;

function defaultTemplate(): CaptchaSMSTemplateConfig {
  return {
    purpose: "login",
    name: "",
    enabled: true,
    signName: "",
    templateId: "",
    codeParamKey: ""
  };
}

function defaultConfig(): CaptchaAppConfig {
  return {
    imageEnabled: true,
    mathEnabled: true,
    digitEnabled: false,
    dynamicEnabled: false,
    audioEnabled: false,
    chiralEnabled: false,
    smsEnabled: false,
    defaultType: "image",
    requireForLogin: true,
    requireForRegister: true,
    dynamic: normalizeDynamicConfig(),
    sms: {
      provider: "aliyun",
      accessKey: "",
      secretKey: "",
      region: "cn-hangzhou",
      signName: "",
      templateId: "",
      codeParamKey: "",
      sdkAppId: "",
      templates: []
    },
    antiFlood: {
      requireCaptcha: true,
      ipHourlyLimit: 5,
      ipDailyLimit: 20,
      phoneDailyLimit: 10,
      globalPhoneDailyLimit: 15,
      sendIntervalSeconds: 60
    }
  };
}

function normalizeTemplate(template?: Partial<CaptchaSMSTemplateConfig>): CaptchaSMSTemplateConfig {
  return {
    ...defaultTemplate(),
    ...template,
    purpose: String(template?.purpose ?? "login"),
    name: String(template?.name ?? ""),
    signName: String(template?.signName ?? ""),
    templateId: String(template?.templateId ?? ""),
    codeParamKey: String(template?.codeParamKey ?? "")
  };
}

function normalizeConfig(config?: Partial<CaptchaAppConfig> | null): CaptchaAppConfig {
  const fallback = defaultConfig();
  return {
    ...fallback,
    ...config,
    defaultType: String(config?.defaultType ?? fallback.defaultType),
    dynamic: normalizeDynamicConfig(config?.dynamic),
    sms: {
      ...fallback.sms,
      ...config?.sms,
      provider: String(config?.sms?.provider ?? fallback.sms.provider),
      accessKey: String(config?.sms?.accessKey ?? ""),
      secretKey: String(config?.sms?.secretKey ?? ""),
      region: String(config?.sms?.region ?? fallback.sms.region),
      signName: String(config?.sms?.signName ?? ""),
      templateId: String(config?.sms?.templateId ?? ""),
      codeParamKey: String(config?.sms?.codeParamKey ?? ""),
      sdkAppId: String(config?.sms?.sdkAppId ?? ""),
      templates: (config?.sms?.templates ?? []).map((item) => normalizeTemplate(item))
    },
    antiFlood: {
      ...fallback.antiFlood,
      ...config?.antiFlood
    }
  };
}

function prettyJSON(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function templateTitle(template: CaptchaSMSTemplateConfig, index: number) {
  if (template.name.trim()) return template.name.trim();
  if (template.purpose.trim()) return template.purpose.trim();
  return `模板 ${index + 1}`;
}

export function AppCaptchaPanel({ appKey }: Props) {
  const configQuery = useAdminCaptchaConfigQuery(appKey);
  const updateMutation = useUpdateAdminCaptchaConfigMutation(appKey);
  const testMutation = useTestAdminCaptchaSMSMutation(appKey);

  const serverConfig = useMemo(() => normalizeConfig(configQuery.data), [configQuery.data]);
  const [draft, setDraft] = useState<CaptchaAppConfig | null>(null);
  const [draftAppKey, setDraftAppKey] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [showSecrets, setShowSecrets] = useState(false);
  const [testPhone, setTestPhone] = useState("");
  const [testPurpose, setTestPurpose] = useState<string>("login");
  const [customTestPurpose, setCustomTestPurpose] = useState("");
  const [testResult, setTestResult] = useState<{ appKey: string; data: AdminTestSMSResult } | null>(null);

  const currentDraft = dirty && draft && draftAppKey === appKey ? draft : serverConfig;
  const currentTestResult = testResult && testResult.appKey === appKey ? testResult.data : null;

  const defaultTypeOptions = useMemo(
    () => captchaToggleOptions.filter((option) => option.type !== "sms" && currentDraft[option.key]),
    [currentDraft]
  );

  const effectiveTestPurpose = testPurpose === "custom" ? customTestPurpose.trim() : testPurpose;

  // 统一基于 prev state 的 updater，避免 React 18 batching + 闭包 stale 导致的字段丢失
  // 关键点：新增的 dynamic/audio/chiral 三个开关如果还沿用老的 `markChanged({...currentDraft,...})`
  // 在同一 tick 内连续修改会用到相同闭包、后写覆盖前写，表现就是"开关看似切换但保存无效"。
  function applyDraft(patcher: (prev: CaptchaAppConfig) => CaptchaAppConfig) {
    setDraft((prev) => {
      const base = prev && draftAppKey === appKey ? prev : serverConfig;
      return patcher(base);
    });
    setDraftAppKey(appKey ?? null);
    setDirty(true);
  }

  function patchDraft<K extends keyof CaptchaAppConfig>(key: K, value: CaptchaAppConfig[K]) {
    applyDraft((prev) => ({ ...prev, [key]: value }));
  }

  function patchSMS<K extends keyof CaptchaAppConfig["sms"]>(key: K, value: CaptchaAppConfig["sms"][K]) {
    applyDraft((prev) => ({
      ...prev,
      sms: {
        ...prev.sms,
        [key]: value
      }
    }));
  }

  function patchAntiFlood<K extends keyof CaptchaAppConfig["antiFlood"]>(
    key: K,
    value: CaptchaAppConfig["antiFlood"][K]
  ) {
    applyDraft((prev) => ({
      ...prev,
      antiFlood: {
        ...prev.antiFlood,
        [key]: value
      }
    }));
  }

  function patchTemplate(index: number, patch: Partial<CaptchaSMSTemplateConfig>) {
    applyDraft((prev) => ({
      ...prev,
      sms: {
        ...prev.sms,
        templates: prev.sms.templates.map((item, itemIndex) =>
          itemIndex === index ? { ...item, ...patch } : item
        )
      }
    }));
  }

  function addTemplate() {
    applyDraft((prev) => ({
      ...prev,
      sms: { ...prev.sms, templates: [...prev.sms.templates, defaultTemplate()] }
    }));
  }

  function removeTemplate(index: number) {
    applyDraft((prev) => ({
      ...prev,
      sms: {
        ...prev.sms,
        templates: prev.sms.templates.filter((_, itemIndex) => itemIndex !== index)
      }
    }));
  }

  function resetDraft() {
    setDraft(null);
    setDraftAppKey(appKey ?? null);
    setDirty(false);
  }

  async function handleSave() {
    try {
      await updateMutation.mutateAsync(currentDraft);
      setDraft(null);
      setDraftAppKey(appKey ?? null);
      setDirty(false);
      toast.success("验证码配置已保存");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "保存失败");
    }
  }

  async function handleTestSMS() {
    if (!appKey) return;
    if (!testPhone.trim()) {
      toast.error("请输入测试手机号");
      return;
    }
    if (!effectiveTestPurpose) {
      toast.error("请输入测试用途");
      return;
    }
    try {
      const result = await testMutation.mutateAsync({
        phone: testPhone.trim(),
        purpose: effectiveTestPurpose
      });
      setTestResult(appKey ? { appKey, data: result } : null);
      toast.success("测试短信已发送");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "测试发送失败");
    }
  }

  if (!appKey) {
    return (
      <div className="rounded-2xl border p-6">
        <h3 className="text-sm font-semibold">应用验证码配置</h3>
        <p className="mt-2 text-sm text-muted-foreground">请先选择应用。</p>
      </div>
    );
  }

  if (configQuery.isLoading && !configQuery.data && !(dirty && draftAppKey === appKey && draft)) {
    return (
      <div className="space-y-4">
        {Array.from({ length: 5 }).map((_, index) => (
          <Skeleton key={index} className="h-28 w-full rounded-2xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 rounded-2xl border p-5 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1">
          <h3 className="text-base font-semibold">应用验证码与短信配置</h3>
          <p className="text-sm text-muted-foreground">对接后端验证码配置、短信模板和测试发送接口。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" variant="outline" size="sm" onClick={resetDraft} disabled={!dirty && !configQuery.isFetching}>
            <RotateCcw className="size-3.5" />
            重置
          </Button>
          <Button type="button" size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            保存
          </Button>
        </div>
      </div>

      <PanelSection title="验证码能力">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {captchaToggleOptions.map((option) => (
            <SwitchField
              key={option.key}
              label={option.label}
              checked={currentDraft[option.key]}
              onCheckedChange={(checked) => patchDraft(option.key, checked)}
            />
          ))}
        </div>
        <div className="grid gap-3 md:max-w-sm">
          <Field label="默认验证码类型">
            <Select
              value={
                defaultTypeOptions.some((option) => option.type === currentDraft.defaultType)
                  ? currentDraft.defaultType
                  : defaultTypeOptions[0]?.type ?? "image"
              }
              onValueChange={(value) => patchDraft("defaultType", value)}
            >
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {defaultTypeOptions.map((option) => (
                  <SelectItem key={option.type} value={option.type}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
      </PanelSection>

      <PanelSection title="动态验证码外观">
        <DynamicCaptchaDesigner
          scope="app"
          appKey={appKey}
          value={currentDraft.dynamic}
          onChange={(next) => patchDraft("dynamic", next)}
          enabled={currentDraft.dynamicEnabled}
          disabledHint="「动态图片」当前未启用，这里调的外观不会出现在登录页上。"
        />
      </PanelSection>

      <PanelSection title="场景策略">
        <p className="text-xs text-muted-foreground">
          默认登录 / 注册都要求验证码（需先在"验证码能力"中至少启用一种类型）。
          可按需关闭某一场景，比如只在注册时要求验证码、登录跳过。
        </p>
        <div className="grid gap-3 md:grid-cols-2">
          <SwitchField
            label="登录时要求验证码"
            checked={currentDraft.requireForLogin}
            onCheckedChange={(checked) => patchDraft("requireForLogin", checked)}
          />
          <SwitchField
            label="注册时要求验证码"
            checked={currentDraft.requireForRegister}
            onCheckedChange={(checked) => patchDraft("requireForRegister", checked)}
          />
        </div>
      </PanelSection>

      <PanelSection
        title="短信服务商"
        action={
          <div className="flex items-center gap-3">
            <Label className="text-sm text-muted-foreground">启用短信验证码</Label>
            <Switch checked={currentDraft.smsEnabled} onCheckedChange={(checked) => patchDraft("smsEnabled", checked)} />
          </div>
        }
      >
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <Field label="服务商">
            <Select value={currentDraft.sms.provider} onValueChange={(value) => patchSMS("provider", value)}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {smsProviders.map((provider) => (
                  <SelectItem key={provider.value} value={provider.value}>
                    {provider.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="Region">
            <Input
              className="h-9"
              value={currentDraft.sms.region}
              placeholder="cn-hangzhou"
              onChange={(event) => patchSMS("region", event.target.value)}
            />
          </Field>
          {currentDraft.sms.provider === "tencent" ? (
            <Field label="SDK AppID">
              <Input
                className="h-9"
                value={currentDraft.sms.sdkAppId}
                placeholder="1400000000"
                onChange={(event) => patchSMS("sdkAppId", event.target.value)}
              />
            </Field>
          ) : (
            <Field label="参数名">
              <Input
                className="h-9"
                value={currentDraft.sms.codeParamKey}
                placeholder="code"
                onChange={(event) => patchSMS("codeParamKey", event.target.value)}
              />
            </Field>
          )}
          <Field label="AccessKey">
            <div className="flex items-center gap-2">
              <Input
                className="h-9 font-mono"
                type={showSecrets ? "text" : "password"}
                value={currentDraft.sms.accessKey}
                onChange={(event) => patchSMS("accessKey", event.target.value)}
              />
              <Button
                type="button"
                size="icon"
                variant="outline"
                className="size-9 shrink-0"
                onClick={() => setShowSecrets((current) => !current)}
              >
                {showSecrets ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
              </Button>
            </div>
          </Field>
          <Field label="SecretKey">
            <Input
              className="h-9 font-mono"
              type={showSecrets ? "text" : "password"}
              value={currentDraft.sms.secretKey}
              onChange={(event) => patchSMS("secretKey", event.target.value)}
            />
          </Field>
        </div>

        <Separator />

        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <Field label="默认签名">
            <Input
              className="h-9"
              value={currentDraft.sms.signName}
              placeholder="Aegis"
              onChange={(event) => patchSMS("signName", event.target.value)}
            />
          </Field>
          <Field label="默认模板 ID">
            <Input
              className="h-9"
              value={currentDraft.sms.templateId}
              placeholder="SMS_123456789"
              onChange={(event) => patchSMS("templateId", event.target.value)}
            />
          </Field>
          <Field label="默认参数名">
            <Input
              className="h-9"
              value={currentDraft.sms.codeParamKey}
              placeholder="code"
              onChange={(event) => patchSMS("codeParamKey", event.target.value)}
            />
          </Field>
        </div>
      </PanelSection>

      <PanelSection
        title="用途模板"
        action={
          <Button type="button" variant="outline" size="sm" onClick={addTemplate}>
            <Plus className="size-3.5" />
            新增模板
          </Button>
        }
      >
        {currentDraft.sms.templates.length === 0 ? (
          <div className="rounded-2xl border border-dashed px-4 py-8 text-sm text-muted-foreground">
            未配置用途模板时，短信发送会回退到默认签名和默认模板。
          </div>
        ) : (
          <ScrollArea className="max-h-[36rem] rounded-2xl border">
            <Accordion type="multiple" className="divide-y">
              {currentDraft.sms.templates.map((template, index) => (
                <AccordionItem key={`${template.purpose}-${index}`} value={`template-${index}`} className="border-b-0 px-4">
                  <AccordionTrigger className="hover:no-underline">
                    <div className="flex min-w-0 flex-1 items-center justify-between gap-4 pr-3">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">{templateTitle(template, index)}</div>
                        <div className="truncate text-xs text-muted-foreground">{template.purpose || "未设置用途"}</div>
                      </div>
                      <div className="shrink-0 text-xs text-muted-foreground">
                        {template.enabled ? "已启用" : "已停用"}
                      </div>
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="space-y-4">
                    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                      <Field label="用途">
                        <Input
                          className="h-9"
                          value={template.purpose}
                          placeholder="login"
                          onChange={(event) => patchTemplate(index, { purpose: event.target.value })}
                        />
                      </Field>
                      <Field label="名称">
                        <Input
                          className="h-9"
                          value={template.name}
                          placeholder="登录短信"
                          onChange={(event) => patchTemplate(index, { name: event.target.value })}
                        />
                      </Field>
                      <SwitchField
                        label="启用"
                        checked={template.enabled}
                        onCheckedChange={(checked) => patchTemplate(index, { enabled: checked })}
                      />
                      <Field label="签名">
                        <Input
                          className="h-9"
                          value={template.signName}
                          placeholder="为空时使用默认签名"
                          onChange={(event) => patchTemplate(index, { signName: event.target.value })}
                        />
                      </Field>
                      <Field label="模板 ID">
                        <Input
                          className="h-9"
                          value={template.templateId}
                          placeholder="SMS_123456789"
                          onChange={(event) => patchTemplate(index, { templateId: event.target.value })}
                        />
                      </Field>
                      <Field label="参数名">
                        <Input
                          className="h-9"
                          value={template.codeParamKey}
                          placeholder="code"
                          onChange={(event) => patchTemplate(index, { codeParamKey: event.target.value })}
                        />
                      </Field>
                    </div>
                    <div className="flex justify-end">
                      <Button type="button" variant="outline" size="sm" onClick={() => removeTemplate(index)}>
                        <Trash2 className="size-3.5" />
                        删除模板
                      </Button>
                    </div>
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </ScrollArea>
        )}
      </PanelSection>

      <PanelSection title="测试短信">
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Field label="手机号">
            <Input
              className="h-9"
              value={testPhone}
              placeholder="13800138000"
              onChange={(event) => setTestPhone(event.target.value)}
            />
          </Field>
          <Field label="用途">
            <Select value={testPurpose} onValueChange={setTestPurpose}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {testPurposeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {testPurpose === "custom" ? (
            <Field label="自定义用途">
              <Input
                className="h-9"
                value={customTestPurpose}
                placeholder="change_profile"
                onChange={(event) => setCustomTestPurpose(event.target.value)}
              />
            </Field>
          ) : (
            <Field label="当前 purpose">
              <Input className="h-9" value={effectiveTestPurpose} readOnly />
            </Field>
          )}
          <div className="flex items-end">
            <Button
              type="button"
              className="h-9 w-full"
              onClick={handleTestSMS}
              disabled={testMutation.isPending || !testPhone.trim() || !effectiveTestPurpose}
            >
              {testMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}
              发送测试短信
            </Button>
          </div>
        </div>

        {currentTestResult ? (
          <div className="space-y-4 rounded-2xl border p-4">
            <div className="grid gap-4 md:grid-cols-3">
              <ReadField label="实际 purpose" value={currentTestResult.purpose} />
              <ReadField label="实际签名" value={currentTestResult.signName} />
              <ReadField label="实际模板 ID" value={currentTestResult.templateId} />
            </div>
            <Field label="返回数据">
              <Textarea readOnly rows={8} value={prettyJSON(currentTestResult.result)} />
            </Field>
          </div>
        ) : null}
      </PanelSection>

      <PanelSection title="发送限制">
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <SwitchField
            label="发送前要求图形验证码"
            checked={currentDraft.antiFlood.requireCaptcha}
            onCheckedChange={(checked) => patchAntiFlood("requireCaptcha", checked)}
          />
          <NumberField
            label="同 IP 小时限额"
            value={currentDraft.antiFlood.ipHourlyLimit}
            onChange={(value) => patchAntiFlood("ipHourlyLimit", value)}
          />
          <NumberField
            label="同 IP 日限额"
            value={currentDraft.antiFlood.ipDailyLimit}
            onChange={(value) => patchAntiFlood("ipDailyLimit", value)}
          />
          <NumberField
            label="同号码日限额"
            value={currentDraft.antiFlood.phoneDailyLimit}
            onChange={(value) => patchAntiFlood("phoneDailyLimit", value)}
          />
          <NumberField
            label="全局号码日限额"
            value={currentDraft.antiFlood.globalPhoneDailyLimit}
            onChange={(value) => patchAntiFlood("globalPhoneDailyLimit", value)}
          />
          <NumberField
            label="发送间隔（秒）"
            value={currentDraft.antiFlood.sendIntervalSeconds}
            onChange={(value) => patchAntiFlood("sendIntervalSeconds", value)}
          />
        </div>
      </PanelSection>
    </div>
  );
}

function PanelSection({
  title,
  children,
  action
}: {
  title: string;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <section className="space-y-4 rounded-2xl border p-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h4 className="text-sm font-semibold">{title}</h4>
        {action}
      </div>
      {children}
    </section>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {children}
    </div>
  );
}

function ReadField({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-1.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="rounded-xl border px-3 py-2 text-sm">{value || "-"}</div>
    </div>
  );
}

function SwitchField({
  label,
  checked,
  onCheckedChange
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex min-h-16 items-center justify-between rounded-2xl border px-4 py-3">
      <Label className="text-sm">{label}</Label>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function NumberField({
  label,
  value,
  onChange
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <Field label={label}>
      <Input
        type="number"
        className="h-9"
        value={String(value ?? 0)}
        onChange={(event) => onChange(Number(event.target.value) || 0)}
      />
    </Field>
  );
}
