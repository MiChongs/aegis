"use client";

import { useState, type ReactNode } from "react";
import { ArrowLeft, ArrowRight, Check, Copy, Link2, Lock, LockOpen } from "lucide-react";
import { ApiConsole } from "@/components/developers/api-console";
import { CodeBlock } from "@/components/developers/code-block";
import { MethodBadge, PathText } from "@/components/developers/method-badge";
import { SchemaView } from "@/components/developers/schema-view";
import {
  sampleFromSchema,
  schemaHint,
  type FlatOperation,
  type OpenAPIParameter,
  type OpenAPISecurityScheme
} from "@/lib/api/openapi";
import { describeScheme, securitySchemeNames, type Credentials } from "@/lib/api/openapi-request";
import { cn } from "@/lib/utils";

const LOCATION_LABELS: Record<OpenAPIParameter["in"], string> = {
  path: "路径",
  query: "查询",
  header: "请求头",
  cookie: "Cookie"
};

function statusDot(status: string) {
  if (status.startsWith("2")) return "bg-emerald-500";
  if (status.startsWith("3")) return "bg-sky-500";
  if (status.startsWith("4")) return "bg-amber-500";
  if (status.startsWith("5")) return "bg-red-500";
  return "bg-muted-foreground";
}

function useCopy() {
  const [copied, setCopied] = useState(false);
  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // 剪贴板不可用（非安全上下文）时静默降级
    }
  }
  return { copied, copy };
}

function Section({
  title,
  aside,
  children
}: {
  title: string;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-3 border-b pb-2">
        <h3 className="text-[13.5px] font-semibold">{title}</h3>
        {aside ? <div className="ml-auto flex items-center gap-2">{aside}</div> : null}
      </div>
      {children}
    </section>
  );
}

function RequiredMark() {
  return (
    <span className="rounded bg-amber-500/12 px-1.5 py-px text-[10.5px] font-medium text-amber-700 dark:text-amber-300">
      必填
    </span>
  );
}

function ParameterList({ parameters }: { parameters: OpenAPIParameter[] }) {
  return (
    <ul className="divide-y rounded-xl border">
      {parameters.map((parameter) => (
        <li key={`${parameter.in}-${parameter.name}`} className="space-y-1 px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <code className="font-mono text-[13px] font-medium">{parameter.name}</code>
            <span className="font-mono text-[11.5px] text-muted-foreground">
              {schemaHint(parameter.schema)}
            </span>
            <span className="rounded border px-1.5 py-px text-[10.5px] text-muted-foreground">
              {LOCATION_LABELS[parameter.in]}
            </span>
            {parameter.required ? <RequiredMark /> : null}
            {parameter.deprecated ? (
              <span className="text-[10.5px] text-muted-foreground line-through">已废弃</span>
            ) : null}
          </div>
          {parameter.description ? (
            <p className="text-[12.5px] leading-relaxed text-muted-foreground">
              {parameter.description}
            </p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function ResponseSection({ operation }: { operation: FlatOperation }) {
  const responses = Object.entries(operation.responses || {}).sort(([a], [b]) => a.localeCompare(b));
  const [active, setActive] = useState(responses[0]?.[0] ?? "");
  const current = responses.find(([status]) => status === active) ?? responses[0];
  if (!current) return null;

  const [status, response] = current;
  const media = response.content?.["application/json"] || Object.values(response.content || {})[0];
  const sample = media?.example ?? (media?.schema ? sampleFromSchema(media.schema) : undefined);

  return (
    <Section title="响应">
      <div className="flex flex-wrap gap-1.5" role="tablist" aria-label="响应状态码">
        {responses.map(([code]) => (
          <button
            key={code}
            type="button"
            role="tab"
            aria-selected={code === status}
            onClick={() => setActive(code)}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 font-mono text-[12px] transition-colors",
              code === status
                ? "border-foreground/20 bg-muted font-semibold text-foreground"
                : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
            )}
          >
            <span className={cn("size-1.5 rounded-full", statusDot(code))} />
            {code}
          </button>
        ))}
      </div>
      {response.description ? (
        <p className="text-[13px] text-muted-foreground">{response.description}</p>
      ) : null}
      {media?.schema ? <SchemaView schema={media.schema} /> : null}
      {typeof sample !== "undefined" && sample !== null ? (
        <CodeBlock
          language="json"
          title="示例"
          code={JSON.stringify(sample, null, 2)}
          maxHeight={280}
        />
      ) : null}
    </Section>
  );
}

function SiblingLink({
  operation,
  direction,
  onSelect
}: {
  operation?: FlatOperation;
  direction: "prev" | "next";
  onSelect: (operation: FlatOperation) => void;
}) {
  if (!operation) return <div className="max-sm:hidden" />;
  const prev = direction === "prev";
  return (
    <button
      type="button"
      onClick={() => onSelect(operation)}
      className={cn(
        "group flex min-w-0 flex-col gap-1 rounded-xl border px-4 py-3 transition-colors hover:border-foreground/20 hover:bg-muted/40",
        prev ? "items-start text-left" : "items-end text-right"
      )}
    >
      <span className="inline-flex items-center gap-1 text-[11.5px] text-muted-foreground">
        {prev ? <ArrowLeft className="size-3.5 transition-transform group-hover:-translate-x-0.5" /> : null}
        {prev ? "上一个" : "下一个"}
        {prev ? null : <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />}
      </span>
      <span className="w-full truncate text-[13px] font-medium">
        {operation.summary || operation.path}
      </span>
    </button>
  );
}

/** 单个接口：说明、参数、请求体、响应，以及右侧常驻的调试台 */
export function OperationDetail({
  operation,
  baseUrl,
  credentials,
  securitySchemes,
  previous,
  next,
  onSelect
}: {
  operation: FlatOperation;
  baseUrl: string;
  credentials: Credentials;
  securitySchemes: Record<string, OpenAPISecurityScheme>;
  previous?: FlatOperation;
  next?: FlatOperation;
  onSelect: (operation: FlatOperation) => void;
}) {
  const schemes = securitySchemeNames(operation);
  const parameters = operation.parameters || [];
  const jsonBody = operation.requestBody?.content?.["application/json"];
  const pathCopy = useCopy();
  const linkCopy = useCopy();

  return (
    <article className="min-w-0 gap-8 2xl:grid 2xl:grid-cols-[minmax(0,1fr)_minmax(0,520px)]">
      <div className="min-w-0 space-y-8">
        <header className="space-y-4">
          <div className="flex items-center gap-2 text-[12.5px] text-muted-foreground">
            <span>{operation.tag}</span>
            {operation.operationId ? (
              <>
                <span className="text-muted-foreground/50">/</span>
                <span className="truncate font-mono">{operation.operationId}</span>
              </>
            ) : null}
          </div>

          <div className="flex flex-wrap items-start gap-3">
            <h2 className="min-w-0 flex-1 text-2xl font-semibold tracking-tight">
              {operation.summary || operation.path}
            </h2>
            <button
              type="button"
              onClick={() => {
                const url = new URL(window.location.href);
                url.searchParams.set("op", operation.key);
                url.searchParams.set("tag", operation.tag);
                void linkCopy.copy(url.toString());
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              {linkCopy.copied ? <Check className="size-3.5 text-emerald-500" /> : <Link2 className="size-3.5" />}
              {linkCopy.copied ? "已复制" : "复制链接"}
            </button>
          </div>

          <div className="flex items-center gap-3 rounded-xl border bg-muted/40 py-2 pr-2 pl-2.5">
            <MethodBadge method={operation.method} size="md" />
            <PathText path={operation.path} className="min-w-0 flex-1 text-[13.5px] break-all" />
            <button
              type="button"
              onClick={() => void pathCopy.copy(operation.path)}
              className="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
              aria-label="复制路径"
            >
              {pathCopy.copied ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
            </button>
          </div>

          <div className="flex flex-wrap items-center gap-2 text-[12px]">
            {schemes.length ? (
              schemes.map((name) => (
                <span
                  key={name}
                  className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-muted-foreground"
                >
                  <Lock className="size-3.5" />
                  {describeScheme(name, securitySchemes[name])}
                </span>
              ))
            ) : (
              <span className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-muted-foreground">
                <LockOpen className="size-3.5" />
                无需认证
              </span>
            )}
            {operation.deprecated ? (
              <span className="rounded-full bg-amber-500/12 px-2.5 py-1 text-amber-700 dark:text-amber-300">
                已废弃
              </span>
            ) : null}
          </div>

          {operation.description ? (
            <p className="max-w-3xl text-[14px] leading-7 text-muted-foreground">{operation.description}</p>
          ) : null}
        </header>

        {parameters.length ? (
          <Section title="参数">
            <ParameterList parameters={parameters} />
          </Section>
        ) : null}

        {jsonBody ? (
          <Section
            title="请求体"
            aside={
              <>
                {operation.requestBody?.required ? <RequiredMark /> : null}
                <span className="font-mono text-[11px] text-muted-foreground">application/json</span>
              </>
            }
          >
            {operation.requestBody?.description ? (
              <p className="text-[13px] text-muted-foreground">{operation.requestBody.description}</p>
            ) : null}
            <SchemaView schema={jsonBody.schema} />
          </Section>
        ) : null}

        <ResponseSection operation={operation} />

        {previous || next ? (
          <nav className="grid gap-3 border-t pt-6 sm:grid-cols-2" aria-label="相邻接口">
            <SiblingLink operation={previous} direction="prev" onSelect={onSelect} />
            <SiblingLink operation={next} direction="next" onSelect={onSelect} />
          </nav>
        ) : null}
      </div>

      <aside className="mt-10 min-w-0 2xl:mt-0" aria-label="调试">
        <div className="2xl:sticky 2xl:top-20 2xl:max-h-[calc(100vh-6rem)] 2xl:overflow-y-auto 2xl:overscroll-contain 2xl:pb-2">
          <ApiConsole operation={operation} baseUrl={baseUrl} credentials={credentials} />
        </div>
      </aside>
    </article>
  );
}
