import type { AppFunction } from "@/lib/api/app-functions";

/**
 * 接入文档的内容生成器：多语言调用示例、完整 Markdown、AI 提示词。
 *
 * 全部是纯函数，不碰 React —— 同一份数据要喂三个消费方（面板渲染、
 * 剪贴板、AI 对话链接），任何一处自己拼一遍都会与另外两处漂移。
 */

export type DocsAuthMode = "user" | "key";

export type DocsParams = {
  baseUrl: string;
  appKey: string;
  functionName: string;
  /** 已经格式化好的示例 input JSON（多行） */
  sampleInput: string;
  authMode: DocsAuthMode;
};

export const AUTH_HEADER: Record<DocsAuthMode, { name: string; placeholder: string }> = {
  user: { name: "Authorization", placeholder: "Bearer YOUR_ACCESS_TOKEN" },
  key: { name: "X-Aegis-Function-Key", placeholder: "YOUR_FUNCTION_KEY" }
};

export function invokeUrl(params: Pick<DocsParams, "baseUrl" | "appKey" | "functionName">) {
  return `${params.baseUrl}/api/apps/${params.appKey}/functions/${params.functionName}/invoke`;
}

export function contractUrl(params: Pick<DocsParams, "baseUrl" | "appKey" | "functionName">) {
  return `${params.baseUrl}/api/apps/${params.appKey}/functions/${params.functionName}`;
}

export function discoveryUrl(params: Pick<DocsParams, "baseUrl" | "appKey">) {
  return `${params.baseUrl}/api/apps/${params.appKey}/functions`;
}

/** 调用端可能收到的全部错误码。顺序即排查顺序：先鉴权，再入参，再状态，再限额。 */
export const INVOKE_ERROR_CODES: Array<{
  code: string;
  http: string;
  message: string;
  hint: string;
}> = [
  { code: "40100", http: "401", message: "缺少用户令牌或函数密钥", hint: "补齐鉴权头" },
  { code: "40190", http: "401", message: "函数密钥无效", hint: "确认密钥未撤销、未拼错" },
  { code: "40390", http: "403", message: "令牌不属于当前应用", hint: "用户令牌与 appKey 必须同属一个应用" },
  { code: "40088", http: "400", message: "eventId 必须是 UUID", hint: "使用标准 UUID v4" },
  { code: "40089", http: "400", message: "输入不是有效 JSON 或超过大小限制", hint: "检查 input 体积与格式" },
  { code: "40109", http: "400", message: "函数入参不符合契约", hint: "错误信息会逐条列出缺失/类型不符的字段" },
  { code: "40490", http: "404", message: "应用函数不存在", hint: "核对函数名与 appKey" },
  { code: "40990", http: "409", message: "应用函数尚未激活", hint: "等待管理端发布并激活版本" },
  { code: "40991", http: "409", message: "相同 eventId 的调用已经失败", hint: "更换新的 eventId 重试" },
  { code: "40993", http: "409", message: "相同 eventId 的调用正在执行", hint: "稍后以同一 eventId 重试可取回结果" },
  { code: "42990", http: "429", message: "函数并发调用已达到限制", hint: "退避后重试" },
  { code: "42991", http: "429", message: "函数调用频次超过每分钟限制", hint: "退避后重试，或联系管理员调高限额" },
  { code: "50290", http: "502", message: "应用函数执行失败", hint: "服务端执行异常，携带 eventId 联系管理员" },
  { code: "自定义", http: "403", message: "业务错误（函数主动返回）", hint: "code 与 message 由函数逻辑定义，按业务语义处理" }
];

export const SAMPLE_LANGUAGES = [
  { key: "curl", label: "cURL" },
  { key: "javascript", label: "JavaScript" },
  { key: "node", label: "Node.js" },
  { key: "python", label: "Python" },
  { key: "go", label: "Go" },
  { key: "java", label: "Java" },
  { key: "csharp", label: "C#" },
  { key: "php", label: "PHP" }
] as const;

export type SampleLanguage = (typeof SAMPLE_LANGUAGES)[number]["key"];

/** 把多行 JSON 按目标缩进重排，嵌进各语言的代码里不至于错位。 */
function indentBlock(text: string, indent: string, skipFirstLine = true) {
  return text
    .split("\n")
    .map((line, index) => (skipFirstLine && index === 0 ? line : indent + line))
    .join("\n");
}

function compactJson(text: string) {
  try {
    return JSON.stringify(JSON.parse(text));
  } catch {
    return text.trim();
  }
}

export function buildCodeSample(language: SampleLanguage, params: DocsParams): string {
  const url = invokeUrl(params);
  const auth = AUTH_HEADER[params.authMode];
  const sample = params.sampleInput.trim() || "{}";

  switch (language) {
    case "curl":
      return [
        `# eventId 是幂等键：重试时复用同一个值可取回既有结果`,
        `curl -X POST '${url}' \\`,
        `  -H 'Content-Type: application/json' \\`,
        `  -H '${auth.name}: ${auth.placeholder}' \\`,
        `  -d '{`,
        `    "eventId": "'"$(uuidgen)"'",`,
        `    "input": ${indentBlock(sample, "    ")}`,
        `  }'`
      ].join("\n");

    case "javascript":
      return [
        `const response = await fetch(`,
        `  "${url}",`,
        `  {`,
        `    method: "POST",`,
        `    headers: {`,
        `      "Content-Type": "application/json",`,
        `      "${auth.name}": "${auth.placeholder}"`,
        `    },`,
        `    body: JSON.stringify({`,
        `      // 幂等键：重试时复用同一个值可取回既有结果`,
        `      eventId: crypto.randomUUID(),`,
        `      input: ${indentBlock(sample, "      ")}`,
        `    })`,
        `  }`,
        `);`,
        ``,
        `const payload = await response.json();`,
        `if (!response.ok || payload.code >= 40000) {`,
        "  throw new Error(`${payload.code}: ${payload.message}`);",
        `}`,
        `// payload.data = { eventId, version, output, effects }`,
        `console.log(payload.data.output);`
      ].join("\n");

    case "node":
      return [
        `// Node.js 18+（内置 fetch 与 crypto.randomUUID）`,
        `import { randomUUID } from "node:crypto";`,
        ``,
        `async function invoke(input) {`,
        `  const response = await fetch(`,
        `    "${url}",`,
        `    {`,
        `      method: "POST",`,
        `      headers: {`,
        `        "Content-Type": "application/json",`,
        `        "${auth.name}": "${auth.placeholder}"`,
        `      },`,
        `      // 幂等键：重试时复用同一个值可取回既有结果`,
        `      body: JSON.stringify({ eventId: randomUUID(), input })`,
        `    }`,
        `  );`,
        `  const payload = await response.json();`,
        `  if (!response.ok || payload.code >= 40000) {`,
        "    throw new Error(`${payload.code}: ${payload.message}`);",
        `  }`,
        `  return payload.data; // { eventId, version, output, effects }`,
        `}`,
        ``,
        `const result = await invoke(${indentBlock(sample, "")});`,
        `console.log(result.output);`
      ].join("\n");

    case "python":
      return [
        `import uuid`,
        ``,
        `import requests`,
        ``,
        ``,
        `def invoke(input_data: dict) -> dict:`,
        `    response = requests.post(`,
        `        "${url}",`,
        `        headers={"${auth.name}": "${auth.placeholder}"},`,
        `        json={`,
        `            # 幂等键：重试时复用同一个值可取回既有结果`,
        `            "eventId": str(uuid.uuid4()),`,
        `            "input": input_data,`,
        `        },`,
        `        timeout=10,`,
        `    )`,
        `    payload = response.json()`,
        `    if payload.get("code", 0) >= 40000:`,
        `        raise RuntimeError(f"{payload['code']}: {payload['message']}")`,
        `    return payload["data"]  # {eventId, version, output, effects}`,
        ``,
        ``,
        `result = invoke(${indentBlock(sample, "")})`,
        `print(result["output"])`
      ].join("\n");

    case "go":
      return [
        `package main`,
        ``,
        `import (`,
        `\t"encoding/json"`,
        `\t"fmt"`,
        `\t"net/http"`,
        `\t"strings"`,
        ``,
        `\t"github.com/google/uuid"`,
        `)`,
        ``,
        `type envelope struct {`,
        `\tCode    int             \u0060json:"code"\u0060`,
        `\tMessage string          \u0060json:"message"\u0060`,
        `\tData    json.RawMessage \u0060json:"data"\u0060`,
        `}`,
        ``,
        `func main() {`,
        `\tinput := \u0060${compactJson(sample)}\u0060`,
        `\t// eventId 是幂等键：重试时复用同一个值可取回既有结果`,
        `\tbody := fmt.Sprintf(\u0060{"eventId":%q,"input":%s}\u0060, uuid.NewString(), input)`,
        ``,
        `\treq, _ := http.NewRequest("POST",`,
        `\t\t"${url}",`,
        `\t\tstrings.NewReader(body))`,
        `\treq.Header.Set("Content-Type", "application/json")`,
        `\treq.Header.Set("${auth.name}", "${auth.placeholder}")`,
        ``,
        `\tresp, err := http.DefaultClient.Do(req)`,
        `\tif err != nil {`,
        `\t\tpanic(err)`,
        `\t}`,
        `\tdefer resp.Body.Close()`,
        ``,
        `\tvar result envelope`,
        `\tjson.NewDecoder(resp.Body).Decode(&result)`,
        `\tif result.Code >= 40000 {`,
        `\t\tpanic(fmt.Sprintf("%d: %s", result.Code, result.Message))`,
        `\t}`,
        `\tfmt.Println(string(result.Data)) // { eventId, version, output, effects }`,
        `}`
      ].join("\n");

    case "java":
      return [
        `// Java 11+ HttpClient`,
        `import java.net.URI;`,
        `import java.net.http.HttpClient;`,
        `import java.net.http.HttpRequest;`,
        `import java.net.http.HttpResponse;`,
        `import java.util.UUID;`,
        ``,
        `public class InvokeExample {`,
        `    public static void main(String[] args) throws Exception {`,
        `        String input = """`,
        `            ${indentBlock(sample, "            ")}`,
        `            """;`,
        `        // eventId 是幂等键：重试时复用同一个值可取回既有结果`,
        `        String body = "{\\"eventId\\":\\"" + UUID.randomUUID() + "\\",\\"input\\":" + input + "}";`,
        ``,
        `        HttpRequest request = HttpRequest.newBuilder()`,
        `            .uri(URI.create("${url}"))`,
        `            .header("Content-Type", "application/json")`,
        `            .header("${auth.name}", "${auth.placeholder}")`,
        `            .POST(HttpRequest.BodyPublishers.ofString(body))`,
        `            .build();`,
        ``,
        `        HttpResponse<String> response = HttpClient.newHttpClient()`,
        `            .send(request, HttpResponse.BodyHandlers.ofString());`,
        `        // 响应结构：{"code":200,"message":"…","data":{eventId,version,output,effects}}`,
        `        System.out.println(response.body());`,
        `    }`,
        `}`
      ].join("\n");

    case "csharp":
      return [
        `// .NET 6+`,
        `using System.Text;`,
        ``,
        `var http = new HttpClient();`,
        `http.DefaultRequestHeaders.Add("${auth.name}", "${auth.placeholder}");`,
        ``,
        `var input = """`,
        `    ${indentBlock(sample, "    ")}`,
        `    """;`,
        `// eventId 是幂等键：重试时复用同一个值可取回既有结果`,
        `var body = $$"""{"eventId":"{{Guid.NewGuid()}}","input":{{input}}}""";`,
        ``,
        `using var content = new StringContent(body, Encoding.UTF8, "application/json");`,
        `var response = await http.PostAsync(`,
        `    "${url}",`,
        `    content);`,
        ``,
        `// 响应结构：{"code":200,"message":"…","data":{eventId,version,output,effects}}`,
        `Console.WriteLine(await response.Content.ReadAsStringAsync());`
      ].join("\n");

    case "php":
      return [
        `<?php`,
        ``,
        `function uuidv4(): string`,
        `{`,
        `    $bytes = random_bytes(16);`,
        `    $bytes[6] = chr((ord($bytes[6]) & 0x0f) | 0x40);`,
        `    $bytes[8] = chr((ord($bytes[8]) & 0x3f) | 0x80);`,
        `    return vsprintf('%s%s-%s-%s-%s-%s%s%s', str_split(bin2hex($bytes), 4));`,
        `}`,
        ``,
        `$input = json_decode(<<<'JSON'`,
        `${sample}`,
        `JSON, true);`,
        ``,
        `$body = [`,
        `    'eventId' => uuidv4(), // 幂等键：重试时复用同一个值可取回既有结果`,
        `    'input' => $input,`,
        `];`,
        ``,
        `$ch = curl_init('${url}');`,
        `curl_setopt_array($ch, [`,
        `    CURLOPT_POST => true,`,
        `    CURLOPT_RETURNTRANSFER => true,`,
        `    CURLOPT_HTTPHEADER => [`,
        `        'Content-Type: application/json',`,
        `        '${auth.name}: ${auth.placeholder}',`,
        `    ],`,
        `    CURLOPT_POSTFIELDS => json_encode($body),`,
        `]);`,
        `$payload = json_decode(curl_exec($ch), true);`,
        `curl_close($ch);`,
        ``,
        `if (($payload['code'] ?? 0) >= 40000) {`,
        `    throw new RuntimeException($payload['code'] . ': ' . $payload['message']);`,
        `}`,
        `// $payload['data'] = [eventId, version, output, effects]`,
        `var_dump($payload['data']['output']);`
      ].join("\n");
  }
}

const MARKDOWN_LANGUAGE_TAGS: Record<SampleLanguage, string> = {
  curl: "bash",
  javascript: "javascript",
  node: "javascript",
  python: "python",
  go: "go",
  java: "java",
  csharp: "csharp",
  php: "php"
};

const RUNTIME_NOTES: Record<string, string> = {
  script: "script（逻辑在 Aegis 服务端执行，客户端不可见、不可复现）",
  wasm: "wasm（纯计算沙箱，无平台数据访问）",
  http: "http（Aegis 签名后转发至接入方自建 HTTPS 端点）"
};

function prettySchema(schema: Record<string, unknown>) {
  if (!schema || !Object.keys(schema).length) return "";
  return JSON.stringify(schema, null, 2);
}

/**
 * 生成完整的接入 Markdown 文档。
 *
 * 这份文档是给「函数作者之外的人」的：接入工程师直接照抄，
 * AI 拿它当唯一上下文回答问题 —— 因此端点、鉴权、契约、错误码、
 * 限额、幂等语义必须一次给全，不能依赖「控制台上还有另一页」。
 */
export function buildFunctionMarkdown(fn: AppFunction, params: DocsParams): string {
  const lines: string[] = [];
  const schema = prettySchema(fn.inputSchema);
  const sample = params.sampleInput.trim() || "{}";
  const url = invokeUrl(params);

  lines.push(`# 远程函数接入文档：${params.appKey} / ${fn.name}`);
  lines.push("");
  if (fn.description) {
    lines.push(fn.description);
    lines.push("");
  }
  lines.push(`- 运行时：${RUNTIME_NOTES[fn.runtime] ?? fn.runtime}`);
  lines.push(`- 当前版本：${fn.activeVersion || "（未激活，暂不可调用）"}`);
  lines.push(`- 基础地址：${params.baseUrl}`);
  lines.push("");

  lines.push(`## 调用端点`);
  lines.push("");
  lines.push("```");
  lines.push(`POST ${url}`);
  lines.push("```");
  lines.push("");
  lines.push(`调用方无需也无法指定版本 —— 请求恒定落在当前激活版本上。`);
  lines.push("");

  lines.push(`## 鉴权（二选一）`);
  lines.push("");
  lines.push(`| 方式 | 请求头 | 适用场景 |`);
  lines.push(`|---|---|---|`);
  lines.push(
    `| 用户令牌 | \`Authorization: Bearer <accessToken>\` | 网页 / 桌面 / 移动客户端；令牌必须属于当前应用的已登录用户 |`
  );
  lines.push(
    `| 函数密钥 | \`X-Aegis-Function-Key: afk_...\` | 接入方服务端；**严禁**打进网页、桌面或移动端安装包 |`
  );
  lines.push("");

  lines.push(`## 请求体`);
  lines.push("");
  lines.push("```json");
  lines.push(`{`);
  lines.push(`  "eventId": "<UUID v4，可选>",`);
  lines.push(`  "input": { }`);
  lines.push(`}`);
  lines.push("```");
  lines.push("");
  lines.push(
    `- \`eventId\`：幂等键。同一 eventId 的**成功**调用重复提交会直接返回既有结果，不会二次执行副作用；` +
      `失败后的重试必须换新的 eventId（复用会返回 40991）。不传则由服务端生成。`
  );
  lines.push(
    `- \`input\`：业务入参，上限 ${fn.maxRequestBytes} 字节${schema ? "，须满足下方入参契约（不满足返回 40109，错误信息逐条列出问题字段）" : ""}。`
  );
  lines.push("");

  if (schema) {
    lines.push(`### 入参契约（JSON Schema）`);
    lines.push("");
    lines.push("```json");
    lines.push(schema);
    lines.push("```");
    lines.push("");
  }

  if (fn.inputTypes?.trim()) {
    lines.push(`### 入参 TypeScript 类型`);
    lines.push("");
    lines.push("```typescript");
    lines.push(fn.inputTypes.trim());
    lines.push("```");
    lines.push("");
  }

  lines.push(`### 示例 input`);
  lines.push("");
  lines.push("```json");
  lines.push(sample);
  lines.push("```");
  lines.push("");

  lines.push(`## 响应`);
  lines.push("");
  lines.push(`成功（HTTP 200）：`);
  lines.push("");
  lines.push("```json");
  lines.push(`{`);
  lines.push(`  "code": 200,`);
  lines.push(`  "message": "函数调用成功",`);
  lines.push(`  "data": {`);
  lines.push(`    "eventId": "9f6f3a2e-8f8c-4e57-b7a1-2d3c4e5f6a7b",`);
  lines.push(`    "version": "${fn.activeVersion || "1.0.0"}",`);
  lines.push(`    "output": {},`);
  lines.push(`    "effects": [{ "type": "points.write", "arguments": {} }]`);
  lines.push(`  }`);
  lines.push(`}`);
  lines.push("```");
  lines.push("");
  lines.push(`- \`output\`：函数的返回值，结构由函数逻辑定义。`);
  lines.push(`- \`effects\`：本次调用在服务端实际执行的写操作流水（加积分、改会员等），可用于对账。`);
  lines.push(`- 响应体上限 ${fn.maxResponseBytes} 字节。`);
  lines.push("");
  lines.push(`失败（HTTP 4xx / 5xx）：\`{ "code": <错误码>, "message": "<原因>" }\``);
  lines.push("");

  lines.push(`## 错误码`);
  lines.push("");
  lines.push(`| code | HTTP | 含义 | 处理建议 |`);
  lines.push(`|---|---|---|---|`);
  for (const item of INVOKE_ERROR_CODES) {
    lines.push(`| ${item.code} | ${item.http} | ${item.message} | ${item.hint} |`);
  }
  lines.push("");

  lines.push(`## 限额`);
  lines.push("");
  lines.push(`| 项 | 值 | 超出表现 |`);
  lines.push(`|---|---|---|`);
  lines.push(`| 单次执行超时 | ${fn.timeoutMs} ms | 调用失败（50290） |`);
  lines.push(`| 请求体上限 | ${fn.maxRequestBytes} B | 40089 |`);
  lines.push(`| 响应体上限 | ${fn.maxResponseBytes} B | 执行失败 |`);
  lines.push(
    `| 每分钟调用上限 | ${fn.rateLimitPerMin > 0 ? `${fn.rateLimitPerMin} 次` : "不限"} | 42991，退避后重试 |`
  );
  lines.push(`| 单实例并发上限 | ${fn.maxConcurrency} | 42990，退避后重试 |`);
  lines.push("");

  lines.push(`## 契约发现`);
  lines.push("");
  lines.push(`鉴权与调用相同（用户令牌或函数密钥）：`);
  lines.push("");
  lines.push("```");
  lines.push(`GET ${discoveryUrl(params)}`);
  lines.push(`GET ${contractUrl(params)}`);
  lines.push("```");
  lines.push("");
  lines.push(
    `列表只含可调用（已启用且有激活版本）的函数；单查返回名称、说明、当前版本、` +
      `入参契约（JSON Schema 与 TypeScript 声明）、示例 input 与限额。`
  );
  lines.push("");

  lines.push(`## 代码示例`);
  lines.push("");
  const markdownSampleLanguages: SampleLanguage[] = ["curl", "javascript", "python", "go"];
  for (const language of markdownSampleLanguages) {
    const meta = SAMPLE_LANGUAGES.find((item) => item.key === language)!;
    lines.push(`### ${meta.label}`);
    lines.push("");
    lines.push("```" + MARKDOWN_LANGUAGE_TAGS[language]);
    lines.push(buildCodeSample(language, params));
    lines.push("```");
    lines.push("");
  }

  return lines.join("\n").trimEnd() + "\n";
}

/** 包一层指令再交给 AI：只给裸文档的话，模型不知道自己该扮演什么角色。 */
export function buildFunctionAIPrompt(markdown: string) {
  return [
    "你是接入工程师的助手。以下是 Aegis 平台一个远程函数的完整接入文档（Markdown）。",
    "请通读后基于它回答问题、编写调用代码或排查报错；文档未覆盖的行为请明确说明「文档未说明」，不要编造。",
    "",
    "<接入文档>",
    markdown.trim(),
    "</接入文档>"
  ].join("\n");
}

/**
 * AI 对话入口。supportsQuery 为 true 的站点可以把提示词直接带进输入框；
 * 其余站点只能打开首页，提示词走剪贴板。
 */
export const AI_PROVIDERS: Array<{
  key: string;
  label: string;
  url: string;
  supportsQuery: boolean;
}> = [
  { key: "chatgpt", label: "在 ChatGPT 中提问", url: "https://chatgpt.com/", supportsQuery: true },
  { key: "claude", label: "在 Claude 中提问", url: "https://claude.ai/new", supportsQuery: true },
  { key: "doubao", label: "在豆包中提问", url: "https://www.doubao.com/chat/", supportsQuery: false },
  { key: "qwen", label: "在通义千问中提问", url: "https://www.tongyi.com/", supportsQuery: false }
];

/** 超过这个长度就不塞进 URL：过长的 query 会被对话站点截断甚至拒绝。 */
const MAX_QUERY_PROMPT_LENGTH = 6000;

export function buildProviderUrl(provider: (typeof AI_PROVIDERS)[number], prompt: string) {
  if (!provider.supportsQuery || prompt.length > MAX_QUERY_PROMPT_LENGTH) {
    return provider.url;
  }
  const joiner = provider.url.includes("?") ? "&" : "?";
  return `${provider.url}${joiner}q=${encodeURIComponent(prompt)}`;
}
