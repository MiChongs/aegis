"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  ChevronDown,
  Clipboard,
  ExternalLink,
  Loader2,
  Lock,
  MinusCircle,
  Plus,
  RotateCw,
  Save,
  ShieldCheck,
  Trash2,
  XCircle,
  Zap
} from "lucide-react";
import { toast } from "sonner";
import Link from "next/link";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { buildEnvSnippet, buildScenarios } from "@/lib/integration-snippets";
import {
  getAuthProtocol,
  listTransportKeys,
  revokeTransportKey,
  rotateSigningSecret,
  rotateTransportKey,
  runIntegrationSelfTest,
  updateAuthProtocol,
  type AuthProtocolPolicy,
  type RegistrationField,
  type SecurityLevel,
  type SelfTestResult
} from "@/lib/api/app-auth-protocol";
import { AppTransportEncryptionPanel } from "@/components/apps/app-transport-encryption-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { CodeSamples } from "@/components/developers/code-samples";
import { cn } from "@/lib/utils";

type Props = { appKey?: string | null };

function errorMessage(error: unknown) {
  return error instanceof ApiError
    ? error.message
    : error instanceof Error
      ? error.message
      : "操作失败";
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN") : "—";
}

/** 三档安全等级。文案只写"客户端要多做什么"，其余留给文档。 */
const LEVELS: { level: SecurityLevel; title: string; work: string; icon: typeof Zap }[] = [
  { level: "standard", title: "标准", work: "无需密钥，一次 fetch 即可接入", icon: Zap },
  { level: "signed", title: "签名", work: "每个请求加一个 HMAC 头", icon: ShieldCheck },
  { level: "sealed", title: "加密", work: "签名之上再叠端到端加密载荷", icon: Lock }
];

const IDENTIFIER_OPTIONS = [
  { value: "username", label: "用户名" },
  { value: "email", label: "邮箱" },
  { value: "phone", label: "手机号" }
];

// 登录与注册的可选集合刻意不同：第三方能否自动建号由每个渠道自己的
// allowRegister 决定（在「第三方登录」页配），这里再开一个开关会变成两处配同一件事。
const LOGIN_METHODS = [
  { value: "password", label: "密码" },
  { value: "sms", label: "短信" },
  { value: "oauth", label: "第三方" }
];

const REGISTER_METHODS = [
  { value: "password", label: "密码" },
  { value: "sms", label: "短信" }
];

const FIELD_TYPES = ["text", "password", "email", "phone", "number", "boolean"];

function CopyButton({ value, label = "复制" }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      size="sm"
      variant="ghost"
      className="h-7 px-2 text-xs"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1600);
        } catch {
          toast.error("剪贴板不可用，请手动选中复制");
        }
      }}
    >
      {copied ? <Check className="size-3.5" /> : <Clipboard className="size-3.5" />}
      {copied ? "已复制" : label}
    </Button>
  );
}

function CredentialRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b py-1.5 last:border-b-0">
      <span className="shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 items-center gap-1">
        <span className="truncate font-mono text-xs">{value}</span>
        <CopyButton value={value} label="" />
      </div>
    </div>
  );
}

/**
 * 接入信息 + 自检。
 *
 * 合成一张卡：接入方要拿的凭据，和"到底通没通"的答案，本来就该在同一屏。
 */
function IntegrationCard({
  appKey,
  baseUrl,
  policy,
  onSecretRotated
}: {
  appKey: string;
  baseUrl: string;
  policy: AuthProtocolPolicy;
  onSecretRotated: () => Promise<unknown>;
}) {
  const token = useAdminToken();
  const [rotating, setRotating] = useState(false);
  const [revealed, setRevealed] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<SelfTestResult | null>(null);
  const needsSecret = policy.securityLevel !== "standard";

  async function rotate() {
    if (!token) return;
    setRotating(true);
    try {
      const rotated = await rotateSigningSecret(token, appKey);
      setRevealed(rotated.appSecret);
      await onSecretRotated();
      toast.success("应用密钥已轮换", { description: "明文仅此一次可见，请立即保存" });
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setRotating(false);
    }
  }

  async function selfTest() {
    if (!token) return;
    setRunning(true);
    try {
      const outcome = await runIntegrationSelfTest(token, appKey, baseUrl);
      setResult(outcome);
      if (outcome.ok) toast.success("接入链路全部通过");
      else toast.error("自检发现问题，请查看失败步骤");
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setRunning(false);
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-3">
        <div>
          <CardTitle>接入信息</CardTitle>
          <CardDescription>把这几项填进接入方配置即可开始对接。</CardDescription>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <CopyButton
            value={buildEnvSnippet({ baseUrl, appKey, level: policy.securityLevel })}
            label="复制 .env"
          />
          <Button size="sm" variant="outline" disabled={running} onClick={() => void selfTest()}>
            {running ? <Loader2 className="size-3.5 animate-spin" /> : <CheckCircle2 className="size-3.5" />}
            运行自检
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <CredentialRow label="Base URL" value={baseUrl || "（未配置，使用当前站点）"} />
          <CredentialRow label="App Key" value={appKey} />
          {needsSecret ? (
            <div className="flex items-center justify-between gap-3 border-b py-1.5 last:border-b-0">
              <span className="shrink-0 text-xs text-muted-foreground">App Secret</span>
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate font-mono text-xs text-muted-foreground">
                  {policy.signingSecretSet ? policy.signingSecretHint : "尚未签发"}
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 px-2 text-xs"
                  disabled={rotating}
                  onClick={() => void rotate()}
                >
                  {rotating ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCw className="size-3.5" />}
                  轮换
                </Button>
              </div>
            </div>
          ) : null}
        </div>

        {revealed ? (
          <div className="space-y-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3">
            <p className="flex items-center gap-1.5 text-xs font-medium text-amber-600 dark:text-amber-400">
              <AlertTriangle className="size-3.5" />
              明文仅此一次可见，离开本页后无法再取回
            </p>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded bg-background px-2 py-1.5 font-mono text-xs">
                {revealed}
              </code>
              <CopyButton value={revealed} />
            </div>
          </div>
        ) : null}

        {result ? (
          <div className="space-y-1.5 rounded-lg border p-2.5">
            <div className="flex items-center gap-2">
              <Badge variant={result.ok ? "success" : "danger"} size="sm">
                {result.ok ? "全部通过" : "存在失败"}
              </Badge>
              <span className="text-xs text-muted-foreground">
                {result.securityLevel} · {result.durationMs}ms
              </span>
            </div>
            {result.steps.map((step) => (
              <div key={step.key} className="flex items-start gap-2 text-xs">
                {step.skipped ? (
                  <MinusCircle className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                ) : step.ok ? (
                  <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
                ) : (
                  <XCircle className="mt-0.5 size-3.5 shrink-0 text-destructive" />
                )}
                <div className="min-w-0">
                  <span className={cn(!step.ok && !step.skipped && "font-medium text-destructive")}>
                    {step.title}
                  </span>
                  {step.detail ? (
                    <span className="text-muted-foreground"> · {step.detail}</span>
                  ) : null}
                  {step.hint ? (
                    <p className="text-amber-600 dark:text-amber-400">建议：{step.hint}</p>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

/** 安全等级三选一，一行一档 */
function SecurityLevelCard({
  value,
  onChange
}: {
  value: SecurityLevel;
  onChange: (level: SecurityLevel) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>安全等级</CardTitle>
        <CardDescription>
          只改变请求的包装方式；三档共用同一批路径与 JSON 结构，换档不必改业务代码。
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-2 sm:grid-cols-3">
        {LEVELS.map((item) => {
          const Icon = item.icon;
          const active = item.level === value;
          return (
            <button
              key={item.level}
              type="button"
              onClick={() => onChange(item.level)}
              className={cn(
                "rounded-lg border p-3 text-left transition-colors",
                active ? "border-primary bg-primary/5" : "hover:bg-muted/50"
              )}
            >
              <div className="flex items-center gap-2 text-sm font-medium">
                <Icon className="size-4" />
                {item.title}
                {active ? <Check className="ml-auto size-3.5 text-primary" /> : null}
              </div>
              <p className="mt-1 text-xs text-muted-foreground">{item.work}</p>
            </button>
          );
        })}
      </CardContent>
    </Card>
  );
}

/** 注册字段可视化编辑器，取代裸 JSON textarea */
function RegistrationSchemaEditor({
  fields,
  onChange
}: {
  fields: RegistrationField[];
  onChange: (fields: RegistrationField[]) => void;
}) {
  // account / password / nickname 由后端映射到固定列，改名会让映射失效
  const reserved = new Set(["account", "password", "nickname"]);

  function update(index: number, patch: Partial<RegistrationField>) {
    onChange(fields.map((field, position) => (position === index ? { ...field, ...patch } : field)));
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label>注册字段</Label>
        <Button
          size="sm"
          variant="outline"
          onClick={() =>
            onChange([...fields, { name: "", type: "text", required: false, mutable: true }])
          }
        >
          <Plus className="size-3.5" />
          新增
        </Button>
      </div>
      <div className="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="min-w-32">字段名</TableHead>
              <TableHead className="min-w-28">类型</TableHead>
              <TableHead className="min-w-28">显示名</TableHead>
              <TableHead className="w-20 text-center">必填</TableHead>
              <TableHead className="w-28 text-center">客户端可写</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {fields.map((field, index) => {
              const isReserved = reserved.has(field.name);
              return (
                <TableRow key={`${field.name}-${index}`}>
                  <TableCell>
                    <Input
                      value={field.name}
                      disabled={isReserved}
                      placeholder="company"
                      className="h-8 font-mono text-xs"
                      onChange={(event) => update(index, { name: event.target.value })}
                    />
                  </TableCell>
                  <TableCell>
                    <Select value={field.type || "text"} onValueChange={(type) => update(index, { type })}>
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {FIELD_TYPES.map((type) => (
                          <SelectItem key={type} value={type}>
                            {type}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    <Input
                      value={field.label ?? ""}
                      placeholder="公司"
                      className="h-8 text-xs"
                      onChange={(event) => update(index, { label: event.target.value })}
                    />
                  </TableCell>
                  <TableCell className="text-center">
                    <Checkbox
                      checked={field.required}
                      onCheckedChange={(checked) => update(index, { required: checked === true })}
                    />
                  </TableCell>
                  <TableCell className="text-center">
                    <Checkbox
                      checked={field.mutable}
                      onCheckedChange={(checked) => update(index, { mutable: checked === true })}
                    />
                  </TableCell>
                  <TableCell>
                    {isReserved ? null : (
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`删除字段 ${field.name || index + 1}`}
                        onClick={() => onChange(fields.filter((_, position) => position !== index))}
                      >
                        <Trash2 className="size-3.5 text-destructive" />
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
      <p className="text-xs text-muted-foreground">
        account / password / nickname 是内置列，名称不可改；
        只有勾选「客户端可写」的自定义字段允许通过 profile 提交。
      </p>
    </div>
  );
}

function CheckboxGroup({
  label,
  hint,
  options,
  value,
  onChange
}: {
  label: string;
  hint?: string;
  options: { value: string; label: string }[];
  value: string[];
  onChange: (value: string[]) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <div className="flex flex-wrap gap-2">
        {options.map((option) => (
          <label
            key={option.value}
            className="flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-sm hover:bg-muted/50"
          >
            <Checkbox
              checked={value.includes(option.value)}
              onCheckedChange={(checked) =>
                onChange(
                  checked === true
                    ? [...value, option.value]
                    : value.filter((item) => item !== option.value)
                )
              }
            />
            {option.label}
          </label>
        ))}
      </div>
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function SwitchField({
  label,
  hint,
  checked,
  onChange
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-lg border p-3">
      <div className="min-w-0">
        <Label>{label}</Label>
        {hint ? <p className="mt-1 text-xs text-muted-foreground">{hint}</p> : null}
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  );
}

/**
 * 接入配置主体。
 *
 * 由父组件以 `key={appKey}` 挂载：切换应用时整体重挂载，本地草稿态随之重置，
 * 因此不需要用 effect 把服务端数据回写进 state。
 */
function IntegrationEditor({
  appKey,
  baseUrl,
  initial,
  onSaved
}: {
  appKey: string;
  baseUrl: string;
  initial: AuthProtocolPolicy;
  onSaved: () => Promise<unknown>;
}) {
  const token = useAdminToken();
  const [policy, setPolicy] = useState<AuthProtocolPolicy>(initial);
  const [saving, setSaving] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);

  const dirty = JSON.stringify(policy) !== JSON.stringify(initial);
  const smsOn = policy.loginMethods.includes("sms") || policy.registerMethods.includes("sms");
  const oauthOn = policy.loginMethods.includes("oauth");

  // 未启用的方式不该出现在示例里 —— 页面上不摆调了必 403 的代码
  const scenarios = buildScenarios({ appKey, baseUrl, level: policy.securityLevel }).filter(
    (scenario) =>
      (scenario.id !== "sms" || smsOn) && (scenario.id !== "oauth" || oauthOn)
  );

  async function save() {
    if (!token) return;
    setSaving(true);
    try {
      const updated = await updateAuthProtocol(token, appKey, {
        identifiers: policy.identifiers,
        loginMethods: policy.loginMethods,
        registerMethods: policy.registerMethods,
        registrationSchema: policy.registrationSchema,
        requireCaptcha: policy.requireCaptcha,
        autoLoginAfterRegister: policy.autoLoginAfterRegister,
        securityLevel: policy.securityLevel,
        allowLegacy: policy.allowLegacy
      });
      setPolicy(updated);
      await onSaved();
      toast.success("接入配置已保存");
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4">
      <IntegrationCard
        appKey={appKey}
        baseUrl={baseUrl}
        policy={policy}
        onSecretRotated={onSaved}
      />

      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div>
            <CardTitle>代码示例</CardTitle>
            <CardDescription>已填入真实 App Key 与服务地址，复制即可运行。</CardDescription>
          </div>
          <Button asChild size="sm" variant="outline">
            <Link href="/developers" target="_blank">
              完整文档 <ExternalLink className="size-3.5" />
            </Link>
          </Button>
        </CardHeader>
        <CardContent>
          <CodeSamples scenarios={scenarios} />
        </CardContent>
      </Card>

      <SecurityLevelCard
        value={policy.securityLevel}
        onChange={(securityLevel) => setPolicy({ ...policy, securityLevel })}
      />

      {policy.securityLevel === "sealed" ? <TransportKeysCard appKey={appKey} /> : null}

      <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
        <Card>
          <CollapsibleTrigger asChild>
            <CardHeader className="cursor-pointer flex-row items-center justify-between">
              <div>
                <CardTitle>认证策略</CardTitle>
                <CardDescription>登录标识、认证方式与注册字段。默认值适用于多数应用。</CardDescription>
              </div>
              <ChevronDown
                className={cn("size-4 shrink-0 transition-transform", advancedOpen && "rotate-180")}
              />
            </CardHeader>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <CardContent className="space-y-4">
              <CheckboxGroup
                label="登录标识"
                hint="允许用户拿哪些字段当账号；启用短信时必须包含手机号"
                options={IDENTIFIER_OPTIONS}
                value={policy.identifiers}
                onChange={(identifiers) => setPolicy({ ...policy, identifiers })}
              />
              <div className="grid gap-4 sm:grid-cols-2">
                <CheckboxGroup
                  label="登录方式"
                  hint="第三方的可用渠道在「第三方登录」页配置"
                  options={LOGIN_METHODS}
                  value={policy.loginMethods}
                  onChange={(loginMethods) => setPolicy({ ...policy, loginMethods })}
                />
                <CheckboxGroup
                  label="注册方式"
                  hint="第三方能否自动建号由各渠道自己的开关决定，不在这里配"
                  options={REGISTER_METHODS}
                  value={policy.registerMethods}
                  onChange={(registerMethods) => setPolicy({ ...policy, registerMethods })}
                />
              </div>
              {smsOn ? (
                <p className="rounded-lg border border-amber-500/35 bg-amber-500/5 p-3 text-xs text-amber-700 dark:text-amber-300">
                  短信认证依赖「验证码」页配置好的短信服务商，未配置时取码接口直接报错。
                </p>
              ) : null}

              <div className="grid gap-3 sm:grid-cols-2">
                <SwitchField
                  label="登录/注册要求验证码"
                  hint="开启后客户端必须先调 /captcha"
                  checked={policy.requireCaptcha}
                  onChange={(requireCaptcha) => setPolicy({ ...policy, requireCaptcha })}
                />
                <SwitchField
                  label="注册后自动登录"
                  hint="关闭后注册只返回用户标识，不签发令牌"
                  checked={policy.autoLoginAfterRegister}
                  onChange={(autoLoginAfterRegister) =>
                    setPolicy({ ...policy, autoLoginAfterRegister })
                  }
                />
                <SwitchField
                  label="保留旧 /api/auth/* 接口"
                  hint="与安全等级互不影响；全部迁移完成后可关闭"
                  checked={policy.allowLegacy}
                  onChange={(allowLegacy) => setPolicy({ ...policy, allowLegacy })}
                />
              </div>

              <RegistrationSchemaEditor
                fields={policy.registrationSchema}
                onChange={(registrationSchema) => setPolicy({ ...policy, registrationSchema })}
              />
            </CardContent>
          </CollapsibleContent>
        </Card>
      </Collapsible>

      {/*
        旧命名空间的传输加密。只在保留了 /api/auth/* 时才有意义 ——
        关掉 allowLegacy 后这段配置不作用于任何可达路径，摆在那里只会让人
        误以为它管的是上面的安全等级。它原先住在「策略配置」Tab，
        和登录策略混编，看不出这层关系。
      */}
      {policy.allowLegacy ? <AppTransportEncryptionPanel appKey={appKey} /> : null}

      <div className="sticky bottom-4 flex items-center gap-3 rounded-lg border bg-background/95 p-3 backdrop-blur">
        <Button onClick={() => void save()} disabled={saving || !dirty}>
          {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
          保存
        </Button>
        <p className="text-xs text-muted-foreground">
          {dirty ? "有未保存的改动" : "已与服务端一致"}
          {policy.securityLevel !== "standard" && !policy.signingSecretSet
            ? " · 保存后会自动签发一把应用密钥"
            : ""}
        </p>
      </div>
    </div>
  );
}

/** Transport 公钥列表与轮换；仅在加密档出现 */
function TransportKeysCard({ appKey }: { appKey: string }) {
  const token = useAdminToken();
  const keysQuery = useQuery({
    queryKey: ["app-transport-keys", token, appKey],
    queryFn: () => listTransportKeys(token as string, appKey),
    enabled: Boolean(token)
  });
  // 兜底成数组：列表渲染不该因为接口形状变动就把整个页签打崩
  const keys = Array.isArray(keysQuery.data) ? keysQuery.data : [];

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <div>
          <CardTitle>Transport 密钥</CardTitle>
          <CardDescription>
            X25519 公钥；轮换期间 active 与 retiring 并存，旧密钥最多保留 24 小时。
          </CardDescription>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={async () => {
            if (!token) return;
            try {
              await rotateTransportKey(token, appKey);
              await keysQuery.refetch();
              toast.success("传输密钥已轮换");
            } catch (error) {
              toast.error(errorMessage(error));
            }
          }}
        >
          <RotateCw className="size-4" />
          轮换
        </Button>
      </CardHeader>
      <CardContent>
        {keysQuery.isLoading ? (
          <div className="flex min-h-32 items-center justify-center">
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key ID</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>有效期至</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {keys.map((key) => (
                <TableRow key={key.keyId}>
                  <TableCell className="max-w-40 truncate font-mono text-xs">{key.keyId}</TableCell>
                  <TableCell>
                    <Badge variant={key.status === "active" ? "default" : "outline"}>
                      {key.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatTime(key.notAfter)}
                  </TableCell>
                  <TableCell className="text-right">
                    {key.status !== "revoked" ? (
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`撤销密钥 ${key.keyId}`}
                        onClick={async () => {
                          if (!token) return;
                          try {
                            await revokeTransportKey(token, appKey, key.keyId);
                            await keysQuery.refetch();
                            toast.success("密钥已撤销");
                          } catch (error) {
                            toast.error(errorMessage(error));
                          }
                        }}
                      >
                        <Trash2 className="size-4 text-destructive" />
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
              {!keys.length ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-10 text-center text-sm text-muted-foreground">
                    暂无传输密钥
                  </TableCell>
                </TableRow>
              ) : null}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * 应用接入台。
 *
 * 先回答接入方的问题（往哪发、怎么发、通没通），再把管理员才关心的策略折叠到下面。
 */
export function AppAuthProtocolPanel({ appKey }: Props) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const origin = useOrigin();
  // 显式配置了直连地址就用它，否则用控制台当前站点（同源反代场景）
  const baseUrl = appConfig.apiBaseUrl || origin || "";

  const policyQuery = useQuery({
    queryKey: ["app-auth-protocol", token, appKey],
    queryFn: () => getAuthProtocol(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });

  if (!appKey) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          请先选择应用。
        </CardContent>
      </Card>
    );
  }

  if (policyQuery.isLoading) {
    return (
      <div className="flex min-h-48 items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (policyQuery.isError || !policyQuery.data) {
    return (
      <Card>
        <CardContent className="py-12 text-center text-sm text-muted-foreground">
          接入配置加载失败：{errorMessage(policyQuery.error)}
        </CardContent>
      </Card>
    );
  }

  return (
    // key={appKey}：切换应用时重挂载，本地草稿态随之重置
    <IntegrationEditor
      key={appKey}
      appKey={appKey}
      baseUrl={baseUrl}
      initial={policyQuery.data}
      onSaved={() =>
        queryClient.invalidateQueries({ queryKey: ["app-auth-protocol", token, appKey] })
      }
    />
  );
}
