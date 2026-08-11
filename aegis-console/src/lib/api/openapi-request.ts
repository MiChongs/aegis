import type { CodeLanguageId } from "@/lib/code-languages";
import {
  sampleFromSchema,
  type FlatOperation,
  type OpenAPIParameter,
  type OpenAPISecurityScheme
} from "./openapi";

/**
 * 把一个 OpenAPI 操作变成「可以真的发出去的请求」。
 *
 * 文档页的调试面板与代码示例都由这里驱动：面板里改了什么，
 * 生成的 curl / Python / Go 就是什么，两者不会各说各话。
 */

/** 调试面板的表单值。全部用字符串保存，发送前才按 schema 还原类型。 */
export type RequestValues = {
  path: Record<string, string>;
  query: Record<string, string>;
  header: Record<string, string>;
  body: string;
};

/** 认证凭据。跨接口共用一份，存在 localStorage 里免得每次重填。 */
export type Credentials = {
  /** 用户令牌，对应 bearerAuth */
  userToken: string;
  /** 管理员令牌，对应 adminBearerAuth */
  adminToken: string;
  /** 管理员静态令牌，对应 xAdminToken 头 */
  adminApiToken: string;
};

export const EMPTY_CREDENTIALS: Credentials = {
  userToken: "",
  adminToken: "",
  adminApiToken: ""
};

export const CREDENTIAL_FIELDS: {
  key: keyof Credentials;
  scheme: string;
  label: string;
  hint: string;
}[] = [
  {
    key: "userToken",
    scheme: "bearerAuth",
    label: "用户令牌",
    hint: "登录接口返回的 accessToken，放在 Authorization 头"
  },
  {
    key: "adminToken",
    scheme: "adminBearerAuth",
    label: "管理员令牌",
    hint: "管理员登录返回的 accessToken，放在 Authorization 头"
  },
  {
    key: "adminApiToken",
    scheme: "xAdminToken",
    label: "管理员静态令牌",
    hint: "ADMIN_API_TOKEN 环境变量的值，放在 X-Admin-Token 头"
  }
];

/** 操作声明的安全方案名，去重后按声明顺序返回 */
export function securitySchemeNames(operation: FlatOperation): string[] {
  return [...new Set(operation.security?.flatMap((entry) => Object.keys(entry)) || [])];
}

function parametersIn(operation: FlatOperation, location: OpenAPIParameter["in"]) {
  return (operation.parameters || []).filter((parameter) => parameter.in === location);
}

/** 表单初值：优先用参数自带的 example，其次按 schema 造样例 */
export function initialValues(operation: FlatOperation): RequestValues {
  const seed = (parameter: OpenAPIParameter) => {
    if (typeof parameter.example !== "undefined") return String(parameter.example);
    const sample = sampleFromSchema(parameter.schema);
    return sample === null || typeof sample === "object" ? "" : String(sample);
  };

  const values: RequestValues = { path: {}, query: {}, header: {}, body: "" };
  for (const parameter of parametersIn(operation, "path")) {
    values.path[parameter.name] = seed(parameter);
  }
  for (const parameter of parametersIn(operation, "query")) {
    // 非必填的 query 留空，避免一发就带上一串无意义的默认值
    values.query[parameter.name] = parameter.required ? seed(parameter) : "";
  }
  for (const parameter of parametersIn(operation, "header")) {
    values.header[parameter.name] = parameter.required ? seed(parameter) : "";
  }

  const json = operation.requestBody?.content?.["application/json"];
  if (json) {
    const sample = json.example ?? sampleFromSchema(json.schema) ?? {};
    values.body = JSON.stringify(sample, null, 2);
  }
  return values;
}

export type BuiltRequest = {
  method: string;
  /** 已替换路径参数、拼好 query 的完整地址 */
  url: string;
  /** 相对路径部分，用于展示与签名 */
  pathWithQuery: string;
  headers: Record<string, string>;
  body?: string;
  /** 路径参数没填完时列出缺失项，调用方据此禁用发送按钮 */
  missingPath: string[];
};

export function buildRequest(
  operation: FlatOperation,
  baseUrl: string,
  values: RequestValues,
  credentials: Credentials
): BuiltRequest {
  const missingPath: string[] = [];
  let path = operation.path;

  for (const parameter of parametersIn(operation, "path")) {
    const raw = (values.path[parameter.name] ?? "").trim();
    if (!raw) {
      missingPath.push(parameter.name);
      path = path.replace(`{${parameter.name}}`, `{${parameter.name}}`);
      continue;
    }
    path = path.replace(`{${parameter.name}}`, encodeURIComponent(raw));
  }

  const search = new URLSearchParams();
  for (const parameter of parametersIn(operation, "query")) {
    const raw = (values.query[parameter.name] ?? "").trim();
    if (raw) search.set(parameter.name, raw);
  }
  const queryString = search.toString();
  const pathWithQuery = `${path}${queryString ? `?${queryString}` : ""}`;

  const headers: Record<string, string> = {};
  for (const parameter of parametersIn(operation, "header")) {
    const raw = (values.header[parameter.name] ?? "").trim();
    if (raw) headers[parameter.name] = raw;
  }

  // 认证头按操作声明的方案自动注入；同时声明 bearer 与 adminBearer 时，
  // 以填了值的那个为准，两个都填则管理员优先（这类接口本就是管理端的）。
  const schemes = securitySchemeNames(operation);
  if (schemes.includes("adminBearerAuth") && credentials.adminToken.trim()) {
    headers.Authorization = `Bearer ${credentials.adminToken.trim()}`;
  } else if (schemes.includes("bearerAuth") && credentials.userToken.trim()) {
    headers.Authorization = `Bearer ${credentials.userToken.trim()}`;
  }
  if (schemes.includes("xAdminToken") && credentials.adminApiToken.trim() && !headers.Authorization) {
    headers["X-Admin-Token"] = credentials.adminApiToken.trim();
  }

  const hasBody =
    Boolean(operation.requestBody?.content?.["application/json"]) &&
    operation.method !== "GET" &&
    operation.method !== "HEAD" &&
    values.body.trim() !== "";
  if (hasBody) headers["Content-Type"] = "application/json";

  return {
    method: operation.method,
    url: `${baseUrl.replace(/\/$/, "")}${pathWithQuery}`,
    pathWithQuery,
    headers,
    body: hasBody ? values.body : undefined,
    missingPath
  };
}

// ─────────────────────────────────────────────────────────────────────
// 代码示例：由上面这份 BuiltRequest 生成，面板改什么代码就变什么
// ─────────────────────────────────────────────────────────────────────

export const SAMPLE_LANGUAGES: CodeLanguageId[] = [
  "curl",
  "typescript",
  "python",
  "go",
  "java",
  "php",
  "csharp"
];

/** 令牌一律替换成占位符，避免复制出去的示例里带着真实凭据 */
function maskedHeaders(headers: Record<string, string>): Record<string, string> {
  const output: Record<string, string> = {};
  for (const [name, value] of Object.entries(headers)) {
    if (name === "Authorization") output[name] = "Bearer $TOKEN";
    else if (name === "X-Admin-Token") output[name] = "$ADMIN_TOKEN";
    else output[name] = value;
  }
  return output;
}

function indentBody(body: string, indent: string): string {
  return body
    .split("\n")
    .map((line, index) => (index === 0 ? line : indent + line))
    .join("\n");
}

export function buildCodeSample(
  language: CodeLanguageId,
  request: BuiltRequest
): string {
  const headers = maskedHeaders(request.headers);
  const { url, method, body } = request;

  switch (language) {
    case "typescript": {
      const lines = [
        `const res = await fetch("${url}", {`,
        `  method: "${method}",`,
        `  headers: ${JSON.stringify(headers, null, 2).replace(/\n/g, "\n  ")}${body ? "," : ""}`
      ];
      if (body) lines.push(`  body: JSON.stringify(${indentBody(body, "  ")})`);
      lines.push("});", "", "const envelope = await res.json();", "console.log(envelope);");
      return lines.join("\n");
    }

    case "python": {
      const lines = ["import requests", "", `url = "${url}"`];
      lines.push(`headers = ${pythonDict(headers)}`);
      if (body) lines.push(`payload = ${indentBody(body, "")}`);
      lines.push(
        "",
        `res = requests.request("${method}", url, headers=headers${body ? ", json=payload" : ""}, timeout=10)`,
        "print(res.status_code, res.json())"
      );
      return lines.join("\n");
    }

    case "go": {
      const lines = [
        `req, _ := http.NewRequest(${JSON.stringify(method)}, ${JSON.stringify(url)}, ${
          body ? "bytes.NewReader(payload)" : "nil"
        })`
      ];
      for (const [name, value] of Object.entries(headers)) {
        lines.push(`req.Header.Set(${JSON.stringify(name)}, ${JSON.stringify(value)})`);
      }
      lines.push(
        "",
        "resp, err := http.DefaultClient.Do(req)",
        "if err != nil {",
        "    return err",
        "}",
        "defer resp.Body.Close()"
      );
      if (body) {
        lines.unshift(`payload := []byte(\`${body}\`)`, "");
      }
      return lines.join("\n");
    }

    case "java": {
      const lines = ["HttpRequest.Builder builder = HttpRequest.newBuilder()", `        .uri(URI.create("${url}"))`];
      for (const [name, value] of Object.entries(headers)) {
        lines.push(`        .header("${name}", "${value}")`);
      }
      lines.push(
        body
          ? `        .method("${method}", HttpRequest.BodyPublishers.ofString(payload));`
          : `        .method("${method}", HttpRequest.BodyPublishers.noBody());`,
        "",
        "HttpResponse<String> response = HttpClient.newHttpClient()",
        "        .send(builder.build(), HttpResponse.BodyHandlers.ofString());",
        "System.out.println(response.statusCode() + \" \" + response.body());"
      );
      if (body) {
        lines.unshift(`String payload = """\n${body}\n""";`, "");
      }
      return lines.join("\n");
    }

    case "php": {
      const lines = ["<?php", `$ch = curl_init("${url}");`, "curl_setopt_array($ch, ["];
      lines.push(`    CURLOPT_CUSTOMREQUEST => "${method}",`);
      lines.push(
        `    CURLOPT_HTTPHEADER => [${Object.entries(headers)
          .map(([name, value]) => `'${name}: ${value}'`)
          .join(", ")}],`
      );
      if (body) lines.push(`    CURLOPT_POSTFIELDS => <<<'JSON'\n${body}\nJSON,`);
      lines.push("    CURLOPT_RETURNTRANSFER => true,", "]);", "", "$response = curl_exec($ch);", "curl_close($ch);", "echo $response;");
      return lines.join("\n");
    }

    case "csharp": {
      const lines = ["using var http = new HttpClient();", `var request = new HttpRequestMessage(new HttpMethod("${method}"), "${url}");`];
      for (const [name, value] of Object.entries(headers)) {
        if (name === "Content-Type") continue;
        lines.push(`request.Headers.TryAddWithoutValidation("${name}", "${value}");`);
      }
      if (body) {
        lines.push("", `request.Content = new StringContent(@"${body.replace(/"/g, '""')}", Encoding.UTF8, "application/json");`);
      }
      lines.push(
        "",
        "var response = await http.SendAsync(request);",
        "Console.WriteLine((int)response.StatusCode);",
        "Console.WriteLine(await response.Content.ReadAsStringAsync());"
      );
      return lines.join("\n");
    }

    case "curl":
    default: {
      const lines = [`curl -X ${method} "${url}"`];
      for (const [name, value] of Object.entries(headers)) {
        lines.push(`  -H "${name}: ${value}"`);
      }
      if (body) lines.push(`  -d '${body}'`);
      return lines.join(" \\\n");
    }
  }
}

function pythonDict(headers: Record<string, string>): string {
  const entries = Object.entries(headers);
  if (!entries.length) return "{}";
  return `{\n${entries.map(([name, value]) => `    "${name}": "${value}",`).join("\n")}\n}`;
}

/** 认证方案的中文说明，用于详情页的「认证」一行 */
export function describeScheme(name: string, scheme?: OpenAPISecurityScheme): string {
  const field = CREDENTIAL_FIELDS.find((item) => item.scheme === name);
  if (field) return field.label;
  if (scheme?.type === "apiKey" && scheme.name) return `${scheme.in} 参数 ${scheme.name}`;
  if (scheme?.type === "http" && scheme.scheme) return `HTTP ${scheme.scheme}`;
  return name;
}
