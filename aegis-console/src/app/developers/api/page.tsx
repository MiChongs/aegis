"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Check, KeyRound, Link2, Loader2, Lock, Search } from "lucide-react";
import { CodeBlock } from "@/components/developers/code-block";
import { ApiConsole } from "@/components/developers/api-console";
import { SchemaView } from "@/components/developers/schema-view";
import {
  flattenOperations,
  getOpenAPISpec,
  groupByTag,
  resolveTagName,
  type FlatOperation,
  type OpenAPISecurityScheme,
  type OpenAPISpec
} from "@/lib/api/openapi";
import {
  CREDENTIAL_FIELDS,
  describeScheme,
  securitySchemeNames,
  type Credentials
} from "@/lib/api/openapi-request";
import { useCredentials, updateCredentials } from "@/lib/developer-credentials";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const METHOD_STYLES: Record<string, string> = {
  GET: "bg-sky-500/12 text-sky-700 dark:text-sky-300",
  POST: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
  PUT: "bg-amber-500/14 text-amber-700 dark:text-amber-300",
  PATCH: "bg-violet-500/12 text-violet-700 dark:text-violet-300",
  DELETE: "bg-red-500/12 text-red-700 dark:text-red-300"
};

function MethodBadge({ method, className }: { method: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex w-[52px] shrink-0 justify-center rounded px-1.5 py-0.5 font-mono text-[10px] font-bold tracking-wide",
        METHOD_STYLES[method] || "bg-muted text-muted-foreground",
        className
      )}
    >
      {method}
    </span>
  );
}

function CopyLinkButton({ operationKey }: { operationKey: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      size="sm"
      variant="ghost"
      className="h-7 px-2 text-xs"
      onClick={async () => {
        const url = new URL(window.location.href);
        url.searchParams.set("op", operationKey);
        try {
          await navigator.clipboard.writeText(url.toString());
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1600);
        } catch {
          // 剪贴板不可用时静默降级
        }
      }}
    >
      {copied ? <Check className="size-3.5" /> : <Link2 className="size-3.5" />}
      {copied ? "已复制" : "复制链接"}
    </Button>
  );
}

/** 单个接口的完整页面：说明、认证、参数、结构、调试台、代码示例 */
function OperationDetail({
  operation,
  baseUrl,
  credentials,
  securitySchemes
}: {
  operation: FlatOperation;
  baseUrl: string;
  credentials: Credentials;
  securitySchemes: Record<string, OpenAPISecurityScheme>;
}) {
  const schemes = securitySchemeNames(operation);
  const jsonBody = operation.requestBody?.content?.["application/json"];
  const responses = Object.entries(operation.responses || {}).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div className="min-w-0 space-y-5">
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <MethodBadge method={operation.method} />
          <code className="min-w-0 break-all font-mono text-[13.5px]">{operation.path}</code>
          {operation.deprecated ? (
            <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300">
              已废弃
            </span>
          ) : null}
          <CopyLinkButton operationKey={operation.key} />
        </div>
        {operation.summary ? (
          <h2 className="mt-2 text-lg font-semibold tracking-tight">{operation.summary}</h2>
        ) : null}
        {operation.description ? (
          <p className="mt-1.5 text-[13px] leading-relaxed text-muted-foreground">
            {operation.description}
          </p>
        ) : null}
        <div className="mt-2 flex flex-wrap items-center gap-2 text-[12.5px] text-muted-foreground">
          {operation.operationId ? (
            <span className="font-mono">{operation.operationId}</span>
          ) : null}
          {schemes.length ? (
            <span className="inline-flex items-center gap-1.5">
              <Lock className="size-3.5" />
              {schemes.map((name) => describeScheme(name, securitySchemes[name])).join(" 或 ")}
            </span>
          ) : (
            <span>无需认证</span>
          )}
        </div>
      </div>

      {/* 宽屏时左文档右调试，两边各自滚动；窄屏顺序堆叠 */}
      <div className="gap-6 xl:grid xl:grid-cols-2">
        <div className="min-w-0 space-y-5">
          {jsonBody ? (
            <section className="space-y-2">
              <h3 className="text-[13px] font-medium">
                请求体结构
                {operation.requestBody?.required ? (
                  <span className="ml-2 text-[11px] font-normal text-amber-600 dark:text-amber-400">
                    必填
                  </span>
                ) : null}
              </h3>
              <SchemaView schema={jsonBody.schema} />
            </section>
          ) : null}

          {responses.length ? (
            <section className="space-y-2">
              <h3 className="text-[13px] font-medium">响应</h3>
              {responses.map(([status, response]) => {
                const media =
                  response.content?.["application/json"] || Object.values(response.content || {})[0];
                return (
                  <div key={status} className="space-y-1.5">
                    <p className="flex items-baseline gap-2 text-[12.5px]">
                      <code className="font-mono font-medium">{status}</code>
                      <span className="text-muted-foreground">{response.description || ""}</span>
                    </p>
                    {media?.schema ? <SchemaView schema={media.schema} /> : null}
                  </div>
                );
              })}
            </section>
          ) : null}
        </div>

        <div className="mt-5 min-w-0 xl:mt-0">
          <ApiConsole operation={operation} baseUrl={baseUrl} credentials={credentials} />
        </div>
      </div>
    </div>
  );
}

function ApiReferenceInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const tagParam = searchParams.get("tag") || "";
  const opParam = searchParams.get("op") || "";
  const [keyword, setKeyword] = useState("");
  const credentials = useCredentials();
  const [credentialsOpen, setCredentialsOpen] = useState(false);

  // 同源部署时 apiBaseUrl 为空串，回落到当前页面 origin（服务端渲染阶段为空）
  const pageOrigin = useOrigin();
  const [baseUrl, setBaseUrl] = useState("");
  const effectiveBase = baseUrl || appConfig.apiBaseUrl || pageOrigin;

  const specQuery = useQuery<OpenAPISpec>({
    queryKey: ["openapi-spec"],
    queryFn: getOpenAPISpec,
    staleTime: 5 * 60_000
  });

  const operations = useMemo(() => flattenOperations(specQuery.data ?? null), [specQuery.data]);
  const groups = useMemo(() => groupByTag(operations), [operations]);
  const securitySchemes = specQuery.data?.components?.securitySchemes || {};

  // tagParam 可能来自后端 /docs/tags/:slug 的 302，形如 admin-system，需按 slug 反查
  const activeTag = useMemo(() => {
    const names = groups.map((group) => group.name);
    return resolveTagName(tagParam, names) || names[0] || "";
  }, [tagParam, groups]);

  const normalizedKeyword = keyword.trim().toLowerCase();

  // 有关键词时跨全部分组检索，否则只列当前分组
  const listed = useMemo(() => {
    if (!normalizedKeyword) {
      return operations.filter((operation) => operation.tag === activeTag);
    }
    return operations.filter((operation) =>
      `${operation.method} ${operation.path} ${operation.summary || ""} ${operation.operationId || ""} ${operation.tag}`
        .toLowerCase()
        .includes(normalizedKeyword)
    );
  }, [operations, activeTag, normalizedKeyword]);

  // ?op= 指向的接口优先；它可能不在当前分组里（分享链接的场景）
  const selected = useMemo(() => {
    const byParam = operations.find((operation) => operation.key === opParam);
    if (byParam) return byParam;
    return listed[0] ?? null;
  }, [operations, opParam, listed]);

  function selectTag(name: string) {
    const params = new URLSearchParams();
    params.set("tag", name);
    setKeyword("");
    router.replace(`/developers/api?${params.toString()}`, { scroll: false });
  }

  function selectOperation(operation: FlatOperation) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("op", operation.key);
    params.set("tag", operation.tag);
    router.replace(`/developers/api?${params.toString()}`, { scroll: false });
  }

  if (specQuery.isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> 正在加载 OpenAPI 规范
      </div>
    );
  }

  if (specQuery.isError) {
    return (
      <div className="mx-auto max-w-lg px-4 py-20 text-center">
        <AlertCircle className="mx-auto size-8 text-destructive" />
        <h2 className="mt-3 text-lg font-semibold">无法加载接口文档</h2>
        <p className="mt-1.5 text-sm text-muted-foreground">
          拉取 <code className="font-mono">/openapi.json</code> 失败，请确认后端服务可达。
        </p>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1600px] px-4 py-8 md:px-6">
      <div className="flex flex-col gap-3 border-b pb-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
            {specQuery.data?.info?.title || "OpenAPI"}
          </p>
          <h1 className="mt-1.5 text-2xl font-semibold tracking-tight">接口文档</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            版本 {specQuery.data?.info?.version || "—"} · {operations.length} 个端点 ·{" "}
            {groups.length} 个分组 · 由后端路由实时生成
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative w-full sm:w-72">
            <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              placeholder="搜索路径、方法、摘要或分组"
            />
          </div>
          <Button
            variant={credentialsOpen ? "default" : "outline"}
            size="sm"
            onClick={() => setCredentialsOpen((value) => !value)}
          >
            <KeyRound className="size-4" />
            调试凭据
          </Button>
        </div>
      </div>

      {credentialsOpen ? (
        <div className="mt-4 space-y-3 rounded-xl border bg-muted/20 p-4">
          <p className="text-[13px] text-muted-foreground">
            填入后会按每个接口声明的认证方式自动附加到请求头，并保存在本浏览器的
            localStorage 中。这里只用于调试，请勿填入生产环境的长期凭据。
          </p>
          <div className="grid gap-3 lg:grid-cols-2">
            <div>
              <label className="text-[12.5px] font-medium" htmlFor="cred-base">
                Base URL
              </label>
              <p className="mt-0.5 text-xs text-muted-foreground">
                留空则使用当前站点（经同源代理转发到后端），跨域时需要目标服务允许本页来源。
              </p>
              <Input
                id="cred-base"
                value={baseUrl}
                placeholder={appConfig.apiBaseUrl || pageOrigin || "https://api.example.com"}
                className="mt-1.5 font-mono text-xs"
                onChange={(event) => setBaseUrl(event.target.value.trim())}
              />
            </div>
            {CREDENTIAL_FIELDS.map((field) => (
              <div key={field.key}>
                <label className="text-[12.5px] font-medium" htmlFor={`cred-${field.key}`}>
                  {field.label}
                </label>
                <p className="mt-0.5 text-xs text-muted-foreground">{field.hint}</p>
                <Input
                  id={`cred-${field.key}`}
                  type="password"
                  value={credentials[field.key]}
                  className="mt-1.5 font-mono text-xs"
                  onChange={(event) => updateCredentials({ [field.key]: event.target.value })}
                />
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <div className="mt-6 gap-6 xl:grid xl:grid-cols-[300px_minmax(0,1fr)]">
        <nav className="mb-6 xl:mb-0" aria-label="接口列表">
          <div className="sticky top-20 flex max-h-[calc(100vh-6rem)] flex-col gap-3">
            {!normalizedKeyword ? (
              <select
                aria-label="接口分组"
                value={activeTag}
                onChange={(event) => selectTag(event.target.value)}
                className="h-9 w-full rounded-md border bg-background px-2.5 text-sm"
              >
                {groups.map((group) => (
                  <option key={group.name} value={group.name}>
                    {group.name}（{group.items.length}）
                  </option>
                ))}
              </select>
            ) : (
              <p className="text-[12.5px] text-muted-foreground">
                检索到 {listed.length} 个端点
              </p>
            )}

            <ul className="min-h-0 flex-1 space-y-0.5 overflow-y-auto pr-1">
              {listed.map((operation) => {
                const active = operation.key === selected?.key;
                return (
                  <li key={operation.key}>
                    <button
                      type="button"
                      onClick={() => selectOperation(operation)}
                      className={cn(
                        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors",
                        active ? "bg-muted" : "hover:bg-muted/60"
                      )}
                    >
                      <MethodBadge method={operation.method} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-mono text-[11.5px]">
                          {operation.path}
                        </span>
                        {operation.summary ? (
                          <span className="block truncate text-[11.5px] text-muted-foreground">
                            {operation.summary}
                          </span>
                        ) : null}
                      </span>
                    </button>
                  </li>
                );
              })}
              {!listed.length ? (
                <li className="px-2 py-8 text-center text-sm text-muted-foreground">
                  没有匹配的端点
                </li>
              ) : null}
            </ul>
          </div>
        </nav>

        {selected ? (
          <OperationDetail
            // key 保证切换接口时调试面板的表单与响应完全重置
            key={selected.key}
            operation={selected}
            baseUrl={effectiveBase}
            credentials={credentials}
            securitySchemes={securitySchemes}
          />
        ) : (
          <div className="rounded-lg border py-20 text-center text-sm text-muted-foreground">
            从左侧选择一个接口
          </div>
        )}
      </div>
    </div>
  );
}

export default function ApiReferencePage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-[60vh] items-center justify-center">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </div>
      }
    >
      <ApiReferenceInner />
    </Suspense>
  );
}
