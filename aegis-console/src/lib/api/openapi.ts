import { apiRequest } from "./client";

/**
 * OpenAPI 3.1 规范的最小可用子集。
 *
 * 后端 `BuildOpenAPISpec()` 由 Gin 路由树实时生成，schema 全部内联展开
 * （`components.schemas` 为空、正文中不出现 `$ref`），因此这里不需要
 * 引用解析器；若将来后端改为输出 `$ref`，需在渲染层补 resolve。
 */
export type OpenAPISchema = {
  type?: string;
  format?: string;
  description?: string;
  example?: unknown;
  enum?: unknown[];
  required?: string[];
  items?: OpenAPISchema;
  properties?: Record<string, OpenAPISchema>;
  additionalProperties?: OpenAPISchema | boolean;
  nullable?: boolean;
  $ref?: string;
};

export type OpenAPIParameter = {
  name: string;
  in: "path" | "query" | "header" | "cookie";
  required?: boolean;
  deprecated?: boolean;
  description?: string;
  schema?: OpenAPISchema;
  example?: unknown;
};

export type OpenAPIMediaType = {
  schema?: OpenAPISchema;
  example?: unknown;
};

export type OpenAPIRequestBody = {
  required?: boolean;
  description?: string;
  content?: Record<string, OpenAPIMediaType>;
};

export type OpenAPIResponse = {
  description?: string;
  content?: Record<string, OpenAPIMediaType>;
};

export type OpenAPIOperation = {
  operationId?: string;
  summary?: string;
  description?: string;
  tags?: string[];
  deprecated?: boolean;
  parameters?: OpenAPIParameter[];
  requestBody?: OpenAPIRequestBody;
  responses?: Record<string, OpenAPIResponse>;
  security?: Array<Record<string, string[]>>;
};

export type OpenAPISecurityScheme = {
  type: string;
  description?: string;
  scheme?: string;
  bearerFormat?: string;
  in?: string;
  name?: string;
};

export type OpenAPISpec = {
  openapi?: string;
  info?: { title?: string; version?: string; description?: string };
  servers?: Array<{ url: string; description?: string }>;
  tags?: Array<{ name: string; description?: string }>;
  paths?: Record<string, Record<string, OpenAPIOperation>>;
  components?: {
    schemas?: Record<string, OpenAPISchema>;
    securitySchemes?: Record<string, OpenAPISecurityScheme>;
  };
};

/** OpenAPI 中真正代表操作的键；`paths` 下还可能出现 `parameters` 等非方法键 */
export const HTTP_METHODS = ["get", "post", "put", "patch", "delete", "head", "options"] as const;

export type HttpMethod = (typeof HTTP_METHODS)[number];

/** 摊平后的单个操作，附带来源 path/method，便于列表渲染与检索 */
export type FlatOperation = OpenAPIOperation & {
  path: string;
  method: Uppercase<HttpMethod>;
  /** 稳定且可用于锚点的唯一键 */
  key: string;
  /** 归属分组（取 tags[0]，缺省为「未分组」） */
  tag: string;
};

export const UNTAGGED = "未分组";

/** 把 `paths` 摊平成按 path→method 排列的操作列表 */
export function flattenOperations(spec: OpenAPISpec | null): FlatOperation[] {
  if (!spec?.paths) return [];
  const result: FlatOperation[] = [];
  for (const [path, methods] of Object.entries(spec.paths)) {
    if (!methods || typeof methods !== "object") continue;
    for (const method of HTTP_METHODS) {
      const operation = methods[method];
      if (!operation) continue;
      const upper = method.toUpperCase() as Uppercase<HttpMethod>;
      result.push({
        ...operation,
        path,
        method: upper,
        key: `${upper} ${path}`,
        tag: operation.tags?.[0]?.trim() || UNTAGGED
      });
    }
  }
  return result.sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method));
}

/** 按 tag 分组，分组内保持 path 顺序；分组按操作数降序 */
export function groupByTag(operations: FlatOperation[]) {
  const groups = new Map<string, FlatOperation[]>();
  for (const operation of operations) {
    const list = groups.get(operation.tag);
    if (list) list.push(operation);
    else groups.set(operation.tag, [operation]);
  }
  return [...groups.entries()]
    .map(([name, items]) => ({ name, items }))
    .sort((a, b) => b.items.length - a.items.length || a.name.localeCompare(b.name));
}

/**
 * 分组名 → slug，与后端旧 `/docs/tags/:slug` 的规则一致。
 * 后端把这些旧链接 302 到 `/developers/api?tag=<slug>`，因此这里要能反查回来。
 */
export function slugifyTag(name: string): string {
  return name.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

/** 按分组原名或其 slug 解析 `?tag=` 参数，命中不了返回空串 */
export function resolveTagName(candidate: string, tagNames: string[]): string {
  const value = candidate.trim();
  if (!value) return "";
  const exact = tagNames.find((name) => name === value);
  if (exact) return exact;
  const slug = slugifyTag(value);
  return tagNames.find((name) => slugifyTag(name) === slug) || "";
}

/** 把内联 schema 渲染成一行类型提示，例如 `object{account, password}` */
export function schemaHint(schema?: OpenAPISchema): string {
  if (!schema) return "—";
  if (schema.$ref) return schema.$ref.split("/").pop() || "object";
  if (schema.enum?.length) return schema.enum.map((item) => JSON.stringify(item)).join(" | ");
  const base = schema.format ? `${schema.type || "string"}<${schema.format}>` : schema.type || "any";
  if (schema.type === "array") return `${schemaHint(schema.items)}[]`;
  if (schema.type === "object" && schema.properties) {
    const keys = Object.keys(schema.properties);
    const head = keys.slice(0, 4).join(", ");
    return `object{${head}${keys.length > 4 ? `, …+${keys.length - 4}` : ""}}`;
  }
  return base;
}

/** 依据 schema 造一份可直接粘进请求体的示例 JSON */
export function sampleFromSchema(schema?: OpenAPISchema, depth = 0): unknown {
  if (!schema || depth > 4) return null;
  if (typeof schema.example !== "undefined") return schema.example;
  if (schema.enum?.length) return schema.enum[0];
  switch (schema.type) {
    case "array":
      return [sampleFromSchema(schema.items, depth + 1)];
    case "object": {
      const output: Record<string, unknown> = {};
      for (const [name, child] of Object.entries(schema.properties || {})) {
        output[name] = sampleFromSchema(child, depth + 1);
      }
      return output;
    }
    case "integer":
    case "number":
      return 0;
    case "boolean":
      return false;
    case "string":
      if (schema.format === "date-time") return "2026-01-01T00:00:00Z";
      return "";
    default:
      return null;
  }
}

/** 为单个操作生成可执行的 curl 命令 */
export function buildCurl(operation: FlatOperation, baseUrl: string): string {
  const origin = baseUrl.replace(/\/$/, "");
  let path = operation.path;
  const query: string[] = [];

  for (const parameter of operation.parameters || []) {
    if (parameter.in === "path") {
      path = path.replace(`{${parameter.name}}`, `<${parameter.name}>`);
    } else if (parameter.in === "query" && parameter.required) {
      query.push(`${parameter.name}=<${parameter.name}>`);
    }
  }

  const url = `${origin}${path}${query.length ? `?${query.join("&")}` : ""}`;
  const lines = [`curl -X ${operation.method} "${url}"`];

  const schemes = operation.security?.flatMap((entry) => Object.keys(entry)) || [];
  if (schemes.includes("adminBearerAuth") || schemes.includes("bearerAuth")) {
    lines.push(`  -H "Authorization: Bearer $TOKEN"`);
  }
  if (schemes.includes("xAdminToken")) {
    lines.push(`  -H "X-Admin-Token: $ADMIN_TOKEN"`);
  }

  for (const parameter of operation.parameters || []) {
    if (parameter.in === "header") {
      lines.push(`  -H "${parameter.name}: <${parameter.name}>"`);
    }
  }

  const jsonBody = operation.requestBody?.content?.["application/json"];
  if (jsonBody) {
    lines.push(`  -H "Content-Type: application/json"`);
    const sample = JSON.stringify(sampleFromSchema(jsonBody.schema) ?? {}, null, 2);
    lines.push(`  -d '${sample}'`);
  }

  return lines.join(" \\\n");
}

export function getOpenAPISpec() {
  return apiRequest<OpenAPISpec>("/openapi.json");
}
