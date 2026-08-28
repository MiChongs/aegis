"use client";

import { useMemo, useState } from "react";
import {
  AlertCircle,
  Bot,
  Check,
  ChevronDown,
  Copy,
  Download,
  ExternalLink,
  Eye,
  FileCode2,
  Globe,
  KeyRound,
  UserRound
} from "lucide-react";
import { toast } from "sonner";
import type { AppFunction } from "@/lib/api/app-functions";
import { appConfig } from "@/lib/env";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import {
  AI_PROVIDERS,
  AUTH_HEADER,
  INVOKE_ERROR_CODES,
  SAMPLE_LANGUAGES,
  buildCodeSample,
  buildFunctionAIPrompt,
  buildFunctionMarkdown,
  buildProviderUrl,
  contractUrl,
  discoveryUrl,
  invokeUrl,
  type DocsAuthMode,
  type SampleLanguage
} from "./function-docs-content";

const BASE_URL_STORAGE_KEY = "aegis-function-docs-base-url";

function defaultBaseUrl() {
  if (typeof window === "undefined") return "";
  const stored = window.localStorage.getItem(BASE_URL_STORAGE_KEY);
  if (stored) return stored;
  return appConfig.apiBaseUrl || window.location.origin;
}

async function copyText(text: string, message = "已复制") {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(message);
    return true;
  } catch {
    toast.error("复制失败，请手动选择复制");
    return false;
  }
}

function prettyJson(raw: string | undefined, fallback = "{}") {
  if (!raw?.trim()) return fallback;
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw.trim();
  }
}

/**
 * 接入文档面板。
 *
 * 这一页的读者不是函数作者，而是**拿着密钥要调通它的人**（或替他干活的 AI）。
 * 因此内容组织是「照抄即可用」：端点 → 鉴权 → 请求 → 示例 → 响应 → 错误码 → 限额，
 * 全部由当前函数的真实契约生成，改一次入参 schema 这里立即跟着变。
 */
export function FunctionDocsPanel({
  appKey,
  selected
}: {
  appKey: string;
  selected: AppFunction;
}) {
  const [baseUrl, setBaseUrl] = useState(defaultBaseUrl);
  const [authMode, setAuthMode] = useState<DocsAuthMode>("user");
  const [language, setLanguage] = useState<SampleLanguage>("curl");

  const normalizedBase = baseUrl.trim().replace(/\/+$/, "") || "https://your-aegis-host";
  const sampleInput = useMemo(
    () => prettyJson(selected.inputSample),
    [selected.inputSample]
  );

  const params = useMemo(
    () => ({
      baseUrl: normalizedBase,
      appKey,
      functionName: selected.name,
      sampleInput,
      authMode
    }),
    [normalizedBase, appKey, selected.name, sampleInput, authMode]
  );

  const schemaText = useMemo(() => {
    const schema = selected.inputSchema;
    if (!schema || !Object.keys(schema).length) return "";
    return JSON.stringify(schema, null, 2);
  }, [selected.inputSchema]);

  const code = useMemo(() => buildCodeSample(language, params), [language, params]);
  const markdown = useMemo(() => buildFunctionMarkdown(selected, params), [selected, params]);

  const callable = selected.status === "active" && Boolean(selected.activeVersion);

  function updateBaseUrl(value: string) {
    setBaseUrl(value);
    try {
      window.localStorage.setItem(BASE_URL_STORAGE_KEY, value.trim());
    } catch {
      // 隐私模式下 localStorage 不可写：地址仍生效，只是不记住
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0 flex-1 space-y-1.5">
          <Label htmlFor="docs-base-url" className="text-xs">
            服务地址
          </Label>
          <div className="relative max-w-md">
            <Globe className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="docs-base-url"
              className="h-9 pl-8 font-mono text-xs"
              value={baseUrl}
              onChange={(event) => updateBaseUrl(event.target.value)}
              placeholder="https://your-aegis-host"
            />
          </div>
        </div>
        <AICopyButton markdown={markdown} fileName={`${appKey}-${selected.name}.md`} />
      </div>

      {!callable ? (
        <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm">
          <AlertCircle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <span>
            该函数当前不可被调用（调用返回 40990）
            {!selected.activeVersion ? "：尚未激活版本。" : "：函数未启用。"}
            以下文档基于当前契约生成，激活后即可按此接入。
          </span>
        </div>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>调用端点</CardTitle>
          <CardDescription>调用恒定落在当前激活版本（{selected.activeVersion || "—"}）。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <EndpointRow method="POST" url={invokeUrl(params)} label="调用" />
          <EndpointRow method="GET" url={discoveryUrl(params)} label="发现可调用函数" />
          <EndpointRow method="GET" url={contractUrl(params)} label="本函数契约" />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>鉴权</CardTitle>
          <CardDescription>二选一；契约发现接口使用同一套凭据。</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 lg:grid-cols-2">
          <AuthModeCard
            active={authMode === "user"}
            onSelect={() => setAuthMode("user")}
            icon={<UserRound className="size-4" />}
            title="用户令牌"
            header={`Authorization: Bearer <accessToken>`}
            description="网页、桌面与移动客户端使用。令牌必须属于当前应用的已登录用户。"
          />
          <AuthModeCard
            active={authMode === "key"}
            onSelect={() => setAuthMode("key")}
            icon={<KeyRound className="size-4" />}
            title="函数密钥"
            header={`X-Aegis-Function-Key: afk_...`}
            description="接入方服务端使用，在「设置」页创建。严禁打进客户端安装包。"
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>请求体</CardTitle>
          <CardDescription>
            eventId 为幂等键：成功调用重复提交返回既有结果；失败重试须更换 eventId。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <CodeBlock
            code={`{\n  "eventId": "<UUID v4，可选>",\n  "input": ${sampleInput.split("\n").join("\n  ")}\n}`}
          />
          {schemaText ? (
            <details className="group rounded-lg border">
              <summary className="cursor-pointer select-none px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground">
                入参契约（JSON Schema）—— 不满足返回 40109
              </summary>
              <div className="border-t p-2">
                <CodeBlock code={schemaText} />
              </div>
            </details>
          ) : (
            <p className="text-xs text-muted-foreground">该函数未配置入参契约，input 不受约束。</p>
          )}
          {selected.inputTypes?.trim() ? (
            <details className="group rounded-lg border">
              <summary className="cursor-pointer select-none px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground">
                入参 TypeScript 类型
              </summary>
              <div className="border-t p-2">
                <CodeBlock code={selected.inputTypes.trim()} />
              </div>
            </details>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2">
              <FileCode2 className="size-4" />
              调用示例
            </CardTitle>
            <CardDescription>示例随上方鉴权方式与服务地址实时生成。</CardDescription>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          <Tabs value={language} onValueChange={(value) => setLanguage(value as SampleLanguage)}>
            <TabsList className="h-auto flex-wrap">
              {SAMPLE_LANGUAGES.map((item) => (
                <TabsTrigger key={item.key} value={item.key} className="text-xs">
                  {item.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <CodeBlock code={code} maxHeight="max-h-96" />
          {authMode === "key" ? (
            <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
              <KeyRound className="mt-0.5 size-3.5 shrink-0" />
              函数密钥仅可存放于服务端环境变量或密钥管理服务，泄露后请立即在「设置」页撤销。
            </p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>响应</CardTitle>
          <CardDescription>
            output 为函数返回值；effects 是服务端实际执行的写操作流水，可用于对账。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <CodeBlock
            code={[
              `{`,
              `  "code": 200,`,
              `  "message": "函数调用成功",`,
              `  "data": {`,
              `    "eventId": "9f6f3a2e-8f8c-4e57-b7a1-2d3c4e5f6a7b",`,
              `    "version": "${selected.activeVersion || "1.0.0"}",`,
              `    "output": {},`,
              `    "effects": [{ "type": "points.write", "arguments": {} }]`,
              `  }`,
              `}`
            ].join("\n")}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>错误码</CardTitle>
          <CardDescription>失败响应为 {"{ code, message }"}，按下表处理。</CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-20">code</TableHead>
                <TableHead className="w-16">HTTP</TableHead>
                <TableHead>含义</TableHead>
                <TableHead>处理建议</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {INVOKE_ERROR_CODES.map((item) => (
                <TableRow key={item.code + item.message}>
                  <TableCell className="font-mono text-xs">{item.code}</TableCell>
                  <TableCell className="font-mono text-xs">{item.http}</TableCell>
                  <TableCell className="text-xs">{item.message}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{item.hint}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>限额</CardTitle>
          <CardDescription>由管理端配置，超出时按错误码退避重试。</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <LimitTile label="单次执行超时" value={`${selected.timeoutMs} ms`} />
          <LimitTile label="请求体上限" value={`${selected.maxRequestBytes} B`} hint="超出返回 40089" />
          <LimitTile
            label="每分钟调用上限"
            value={selected.rateLimitPerMin > 0 ? `${selected.rateLimitPerMin} 次` : "不限"}
            hint="超出返回 42991"
          />
          <LimitTile label="单实例并发上限" value={String(selected.maxConcurrency)} hint="超出返回 42990" />
        </CardContent>
      </Card>

      {selected.runtime === "http" ? (
        <Card>
          <CardHeader>
            <CardTitle>端点验签（http 运行时）</CardTitle>
            <CardDescription>
              Aegis 对转发到你端点的每个请求做 Ed25519 签名；你的响应也必须签名。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-xs">
            <CodeBlock
              code={[
                `# 获取 Aegis 请求签名公钥（免鉴权）`,
                `GET ${normalizedBase}/api/functions/signing-key`,
                ``,
                `# Aegis → 你的端点，请求头：`,
                `X-Aegis-Event-ID / X-Aegis-Timestamp / X-Aegis-Content-SHA256 / X-Aegis-Signature`,
                `# 请求签名串：timestamp + "\\n" + eventId + "\\n" + contentSha256`,
                ``,
                `# 你的端点 → Aegis，响应头：`,
                `X-Aegis-Response-Signature`,
                `# 响应签名串：eventId + "\\n" + sha256(responseBody)，Ed25519 + base64url（无填充）`
              ].join("\n")}
            />
            <p className="text-muted-foreground">
              响应验签公钥在创建版本时写入 responsePublicKey。仅允许 HTTPS，禁止重定向与内网地址。
            </p>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

/**
 * 「复制 Markdown」拆分按钮 + AI 菜单。
 *
 * 主按钮一步到位（最高频动作），菜单里是低频入口：AI 提示词、
 * 各家对话站点、预览与下载。跳转前先把提示词放进剪贴板 ——
 * 部分站点不支持 URL 携带内容，粘贴是唯一兜底。
 */
function AICopyButton({ markdown, fileName }: { markdown: string; fileName: string }) {
  const [previewOpen, setPreviewOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  async function copyMarkdown() {
    if (await copyText(markdown, "Markdown 已复制，可直接粘贴给 AI")) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    }
  }

  async function openProvider(provider: (typeof AI_PROVIDERS)[number]) {
    const prompt = buildFunctionAIPrompt(markdown);
    await copyText(prompt, "提示词已复制；若对话框为空，请直接粘贴");
    window.open(buildProviderUrl(provider, prompt), "_blank", "noopener,noreferrer");
  }

  function downloadMarkdown() {
    const blob = new Blob([markdown], { type: "text/markdown;charset=utf-8" });
    const href = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.download = fileName;
    anchor.click();
    URL.revokeObjectURL(href);
  }

  return (
    <>
      <ButtonGroup>
        <Button variant="outline" size="sm" onClick={copyMarkdown}>
          {copied ? <Check className="size-4 text-emerald-600" /> : <Copy className="size-4" />}
          复制 Markdown
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="px-1.5" aria-label="更多 AI 操作">
              <ChevronDown className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel className="flex items-center gap-1.5 text-xs">
              <Bot className="size-3.5" />
              交给 AI
            </DropdownMenuLabel>
            <DropdownMenuItem
              onClick={() =>
                copyText(buildFunctionAIPrompt(markdown), "AI 提示词已复制")
              }
            >
              <Copy className="size-4" />
              复制 AI 提示词
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {AI_PROVIDERS.map((provider) => (
              <DropdownMenuItem key={provider.key} onClick={() => openProvider(provider)}>
                <ExternalLink className="size-4" />
                {provider.label}
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => setPreviewOpen(true)}>
              <Eye className="size-4" />
              查看 Markdown
            </DropdownMenuItem>
            <DropdownMenuItem onClick={downloadMarkdown}>
              <Download className="size-4" />
              下载 .md 文件
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </ButtonGroup>

      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className="flex max-h-[80vh] flex-col sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">{fileName}</DialogTitle>
            <DialogDescription>
              由当前契约实时生成，与页面内容同源。
            </DialogDescription>
          </DialogHeader>
          <pre className="min-h-0 flex-1 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed">
            {markdown}
          </pre>
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" onClick={downloadMarkdown}>
              <Download className="size-4" />
              下载
            </Button>
            <Button size="sm" onClick={copyMarkdown}>
              <Copy className="size-4" />
              复制全文
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

const METHOD_TONE: Record<string, string> = {
  GET: "text-emerald-600 dark:text-emerald-400",
  POST: "text-sky-600 dark:text-sky-400"
};

function EndpointRow({ method, url, label }: { method: string; url: string; label: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border px-2.5 py-2">
      <span className={cn("w-11 shrink-0 font-mono text-xs font-semibold", METHOD_TONE[method])}>
        {method}
      </span>
      <code className="min-w-0 flex-1 truncate font-mono text-xs" title={url}>
        {url}
      </code>
      <Badge variant="outline" size="sm" className="hidden shrink-0 sm:inline-flex">
        {label}
      </Badge>
      <Button
        size="icon-sm"
        variant="ghost"
        className="shrink-0"
        aria-label={`复制${label}地址`}
        onClick={() => copyText(url)}
      >
        <Copy className="size-3.5" />
      </Button>
    </div>
  );
}

function AuthModeCard({
  active,
  onSelect,
  icon,
  title,
  header,
  description
}: {
  active: boolean;
  onSelect: () => void;
  icon: React.ReactNode;
  title: string;
  header: string;
  description: string;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "rounded-lg border p-3 text-left transition-colors",
        active ? "border-primary/60 bg-muted/50" : "hover:bg-muted/40"
      )}
    >
      <span className="flex items-center gap-2 text-sm font-medium">
        {icon}
        {title}
        {active ? (
          <Badge variant="secondary" size="sm" className="ml-auto">
            示例使用中
          </Badge>
        ) : null}
      </span>
      <code className="mt-2 block truncate rounded bg-muted/60 px-2 py-1 font-mono text-[11px]">
        {header}
      </code>
      <span className="mt-1.5 block text-xs leading-snug text-muted-foreground">{description}</span>
    </button>
  );
}

function CodeBlock({ code, maxHeight = "max-h-72" }: { code: string; maxHeight?: string }) {
  return (
    <div className="group relative">
      <pre
        className={cn(
          "overflow-auto rounded-lg border bg-muted/40 p-3 pr-10 font-mono text-[11px] leading-relaxed",
          maxHeight
        )}
      >
        {code}
      </pre>
      <Button
        size="icon-sm"
        variant="ghost"
        className="absolute right-1.5 top-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        aria-label="复制代码"
        onClick={() => copyText(code)}
      >
        <Copy className="size-3.5" />
      </Button>
    </div>
  );
}

function LimitTile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-lg">{value}</p>
      {hint ? <p className="mt-0.5 text-[11px] text-muted-foreground">{hint}</p> : null}
    </div>
  );
}
