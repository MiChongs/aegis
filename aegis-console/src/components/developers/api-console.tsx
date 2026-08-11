"use client";

import { useMemo, useState } from "react";
import { Loader2, Play, RotateCcw } from "lucide-react";
import { CodeBlock } from "@/components/developers/code-block";
import { CodeLanguageIcon, codeLanguage } from "@/lib/code-languages";
import type { FlatOperation, OpenAPIParameter } from "@/lib/api/openapi";
import {
  SAMPLE_LANGUAGES,
  buildCodeSample,
  buildRequest,
  initialValues,
  securitySchemeNames,
  type Credentials,
  type RequestValues
} from "@/lib/api/openapi-request";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

type ResponseState = {
  status: number;
  statusText: string;
  durationMs: number;
  sizeBytes: number;
  headers: [string, string][];
  body: string;
  isJson: boolean;
};

function formatBytes(size: number) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(2)} MB`;
}

function statusTone(status: number) {
  if (status >= 500) return "bg-red-500/15 text-red-700 dark:text-red-300";
  if (status >= 400) return "bg-amber-500/15 text-amber-700 dark:text-amber-300";
  if (status >= 200 && status < 300) return "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300";
  return "bg-muted text-muted-foreground";
}

function ParamInputs({
  title,
  parameters,
  values,
  onChange
}: {
  title: string;
  parameters: OpenAPIParameter[];
  values: Record<string, string>;
  onChange: (name: string, value: string) => void;
}) {
  if (!parameters.length) return null;
  return (
    <div className="space-y-2">
      <p className="text-[13px] font-medium">{title}</p>
      <div className="space-y-2">
        {parameters.map((parameter) => (
          <div key={parameter.name} className="grid gap-1.5 sm:grid-cols-[160px_minmax(0,1fr)] sm:items-center">
            <label
              className="flex items-baseline gap-1.5 text-[12.5px]"
              htmlFor={`param-${parameter.in}-${parameter.name}`}
            >
              <code className="font-mono">{parameter.name}</code>
              {parameter.required ? (
                <span className="text-[11px] text-amber-600 dark:text-amber-400">必填</span>
              ) : null}
            </label>
            <Input
              id={`param-${parameter.in}-${parameter.name}`}
              value={values[parameter.name] ?? ""}
              placeholder={parameter.description || parameter.schema?.type || ""}
              className="h-8 font-mono text-xs"
              onChange={(event) => onChange(parameter.name, event.target.value)}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * 接口调试台。
 *
 * 面板里填的值同时驱动三件事：真实请求、代码示例、以及上方展示的完整 URL。
 * 三者永远同源，不会出现「示例长这样、实际发的又是另一回事」。
 *
 * 请求由浏览器直接发出。默认 baseUrl 与控制台同源（Next 的 /api 反向代理），
 * 因此不涉及跨域；填了外部地址时若被 CORS 拦下，会在下方原样提示。
 */
export function ApiConsole({
  operation,
  baseUrl,
  credentials
}: {
  operation: FlatOperation;
  baseUrl: string;
  credentials: Credentials;
}) {
  const [values, setValues] = useState<RequestValues>(() => initialValues(operation));
  const [language, setLanguage] = useState(SAMPLE_LANGUAGES[0]);
  const [sending, setSending] = useState(false);
  const [response, setResponse] = useState<ResponseState | null>(null);
  const [error, setError] = useState("");

  // 切换接口由父组件的 key 触发整体重挂载，这里不需要再用 effect 同步 operation：
  // 表单初值、响应与错误都随新实例重新初始化。
  const request = useMemo(
    () => buildRequest(operation, baseUrl, values, credentials),
    [operation, baseUrl, values, credentials]
  );

  const pathParams = (operation.parameters || []).filter((item) => item.in === "path");
  const queryParams = (operation.parameters || []).filter((item) => item.in === "query");
  const headerParams = (operation.parameters || []).filter((item) => item.in === "header");
  const jsonBody = operation.requestBody?.content?.["application/json"];
  const schemes = securitySchemeNames(operation);
  const missingCredential =
    schemes.length > 0 && !request.headers.Authorization && !request.headers["X-Admin-Token"];

  function patch(location: "path" | "query" | "header", name: string, value: string) {
    setValues((current) => ({ ...current, [location]: { ...current[location], [name]: value } }));
  }

  async function send() {
    setSending(true);
    setError("");
    setResponse(null);
    const startedAt = performance.now();
    try {
      const result = await fetch(request.url, {
        method: request.method,
        headers: request.headers,
        body: request.body
      });
      const text = await result.text();
      const contentType = result.headers.get("content-type") || "";
      const isJson = contentType.includes("json");
      setResponse({
        status: result.status,
        statusText: result.statusText,
        durationMs: Math.round(performance.now() - startedAt),
        sizeBytes: new Blob([text]).size,
        headers: [...result.headers.entries()],
        body: isJson ? prettyJson(text) : text,
        isJson
      });
    } catch (cause) {
      // fetch 只在网络层失败时抛错，最常见的是跨域被拦
      setError(
        cause instanceof Error
          ? `${cause.message}（若 Base URL 指向其他域名，请确认该域已允许当前来源跨域）`
          : "请求失败"
      );
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="space-y-3 rounded-lg border p-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[13px] font-medium">调试</span>
          <code className="min-w-0 flex-1 truncate rounded bg-muted/60 px-2 py-1 font-mono text-[11.5px]">
            {request.method} {request.pathWithQuery}
          </code>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 px-2 text-xs"
            onClick={() => setValues(initialValues(operation))}
          >
            <RotateCcw className="size-3.5" />
            重置
          </Button>
          <Button size="sm" disabled={sending || request.missingPath.length > 0} onClick={() => void send()}>
            {sending ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
            发送
          </Button>
        </div>

        {request.missingPath.length ? (
          <p className="text-[12.5px] text-amber-600 dark:text-amber-400">
            请先填写路径参数：{request.missingPath.join("、")}
          </p>
        ) : null}
        {missingCredential ? (
          <p className="text-[12.5px] text-amber-600 dark:text-amber-400">
            该接口需要认证，请在页面顶部填入对应令牌，否则会返回 401。
          </p>
        ) : null}

        <ParamInputs
          title="路径参数"
          parameters={pathParams}
          values={values.path}
          onChange={(name, value) => patch("path", name, value)}
        />
        <ParamInputs
          title="查询参数"
          parameters={queryParams}
          values={values.query}
          onChange={(name, value) => patch("query", name, value)}
        />
        <ParamInputs
          title="请求头"
          parameters={headerParams}
          values={values.header}
          onChange={(name, value) => patch("header", name, value)}
        />

        {jsonBody ? (
          <div className="space-y-1.5">
            <p className="text-[13px] font-medium">
              请求体
              {operation.requestBody?.required ? (
                <span className="ml-2 text-[11px] font-normal text-amber-600 dark:text-amber-400">
                  必填
                </span>
              ) : null}
            </p>
            <Textarea
              value={values.body}
              spellCheck={false}
              className="min-h-40 font-mono text-xs"
              onChange={(event) => setValues((current) => ({ ...current, body: event.target.value }))}
            />
          </div>
        ) : null}
      </div>

      {error ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-[12.5px] text-destructive">
          {error}
        </div>
      ) : null}

      {response ? (
        <div className="space-y-2 rounded-lg border p-3">
          <div className="flex flex-wrap items-center gap-2 text-[12.5px]">
            <span
              className={cn(
                "rounded px-2 py-0.5 font-mono text-[11.5px] font-medium",
                statusTone(response.status)
              )}
            >
              {response.status} {response.statusText}
            </span>
            <span className="text-muted-foreground">{response.durationMs} ms</span>
            <span className="text-muted-foreground">{formatBytes(response.sizeBytes)}</span>
          </div>
          <CodeBlock
            language={response.isJson ? "json" : "text"}
            title="响应体"
            code={response.body || "（空）"}
            maxHeight={360}
          />
          <details className="text-[12.5px]">
            <summary className="cursor-pointer text-muted-foreground">响应头</summary>
            <div className="mt-1.5 overflow-x-auto rounded-lg border bg-background">
              <table className="w-full text-[12px]">
                <tbody className="divide-y">
                  {response.headers.map(([name, value]) => (
                    <tr key={name}>
                      <td className="whitespace-nowrap px-3 py-1 font-mono text-muted-foreground">
                        {name}
                      </td>
                      <td className="px-3 py-1 font-mono break-all">{value}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        </div>
      ) : null}

      <div className="space-y-2">
        <div className="flex flex-wrap gap-1.5">
          {SAMPLE_LANGUAGES.map((id) => (
            <button
              key={id}
              type="button"
              onClick={() => setLanguage(id)}
              aria-pressed={id === language}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs transition-colors",
                id === language
                  ? "border-primary/40 bg-primary/10 font-medium text-foreground"
                  : "text-muted-foreground hover:bg-muted"
              )}
            >
              <CodeLanguageIcon id={id} className="size-3.5" />
              {codeLanguage(id).label}
            </button>
          ))}
        </div>
        <CodeBlock
          language={language}
          title="按当前参数生成"
          code={buildCodeSample(language, request)}
          maxHeight={320}
        />
      </div>
    </div>
  );
}

function prettyJson(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}
