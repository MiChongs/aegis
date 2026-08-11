"use client";

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Activity, Check, Code2, Cpu, Loader2, Plus, Power, PowerOff,
  Puzzle, RefreshCw, Search, Trash2, X
} from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@/lib/auth-store";
import { ApiError } from "@/lib/api/client";
import {
  listPlugins, createPlugin, deletePlugin, enablePlugin, disablePlugin,
  getHookRegistry, listHookExecutions,
  type Plugin, type HookRegistryView, type HookExecution
} from "@/lib/api/plugins";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState, EmptyState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import dynamic from "next/dynamic";
import type { Monaco } from "@monaco-editor/react";
import { loadMonaco } from "@/lib/monaco/loader";

// 不在模块作用域预热：RoutePrefetcher 会预取本路由并执行模块体，
// 那会在自托管配置生效前把 Monaco 拉起来（详见 @/lib/monaco/loader）。
const MonacoEditor = dynamic(() => import("@monaco-editor/react").then(m => m.default), { ssr: false });

// Expr 语言注册（语法高亮 + 自动补全）
let exprLangRegistered = false;
function registerExprLanguage(monaco: Monaco) {
  if (exprLangRegistered) return;
  exprLangRegistered = true;

  monaco.languages.register({ id: "expr" });

  // 语法高亮
  monaco.languages.setMonarchTokensProvider("expr", {
    keywords: ["true", "false", "nil", "in", "not", "and", "or", "matches", "contains", "startsWith", "endsWith", "let"],
    builtins: ["allow", "deny", "log", "now", "hour", "minute", "weekday", "date", "datetime", "matchCIDR", "hasRole", "setHeader", "getData", "setData", "len", "upper", "lower", "trim", "split", "join", "int", "float", "string", "all", "any", "one", "none", "map", "filter", "count"],
    operators: ["==", "!=", "<", ">", "<=", ">=", "&&", "||", "!", "+", "-", "*", "/", "%", "?:", "??", ".."],
    symbols: /[=><!~?:&|+\-*/^%]+/,
    tokenizer: {
      root: [
        [/\/\/.*$/, "comment"],
        [/\/\*/, "comment", "@comment"],
        [/"([^"\\]|\\.)*"/, "string"],
        [/'([^'\\]|\\.)*'/, "string"],
        [/`([^`\\]|\\.)*`/, "string"],
        [/\d+(\.\d+)?/, "number"],
        [/[a-zA-Z_]\w*/, {
          cases: {
            "@keywords": "keyword",
            "@builtins": "type.identifier",
            "@default": "identifier",
          },
        }],
        [/[{}()[\]]/, "@brackets"],
        [/@symbols/, {
          cases: {
            "@operators": "operator",
            "@default": "",
          },
        }],
        [/[;,.]/, "delimiter"],
      ],
      comment: [
        [/[^/*]+/, "comment"],
        [/\*\//, "comment", "@pop"],
        [/[/*]/, "comment"],
      ],
    },
  });

  // 自动补全
  monaco.languages.registerCompletionItemProvider("expr", {
    provideCompletionItems: (_model: unknown, position: { lineNumber: number; column: number }) => {
      const range = {
        startLineNumber: position.lineNumber, startColumn: position.column,
        endLineNumber: position.lineNumber, endColumn: position.column,
      };
      const suggestions = [
        { label: "allow", kind: monaco.languages.CompletionItemKind.Function, insertText: "allow()", detail: "允许请求继续", range },
        { label: "deny", kind: monaco.languages.CompletionItemKind.Function, insertText: 'deny("${1:原因}")', insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet, detail: "拒绝请求", range },
        { label: "matchCIDR", kind: monaco.languages.CompletionItemKind.Function, insertText: 'matchCIDR(${1:ip}, "${2:10.0.0.0/8}")', insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet, detail: "IP 范围检查", range },
        { label: "now", kind: monaco.languages.CompletionItemKind.Function, insertText: "now()", detail: "当前时间 (time.Time)", range },
        { label: "hour", kind: monaco.languages.CompletionItemKind.Function, insertText: "hour()", detail: "当前小时 (0-23)", range },
        { label: "minute", kind: monaco.languages.CompletionItemKind.Function, insertText: "minute()", detail: "当前分钟 (0-59)", range },
        { label: "weekday", kind: monaco.languages.CompletionItemKind.Function, insertText: "weekday()", detail: "星期几 (0=日 1=一 ... 6=六)", range },
        { label: "date", kind: monaco.languages.CompletionItemKind.Function, insertText: "date()", detail: "当前日期 MM-DD", range },
        { label: "datetime", kind: monaco.languages.CompletionItemKind.Function, insertText: "datetime()", detail: "当前时间 YYYY-MM-DD HH:MM:SS", range },
        { label: "hasRole", kind: monaco.languages.CompletionItemKind.Function, insertText: 'hasRole(${1:roles}, "${2:admin}")', insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet, detail: "角色检查", range },
        { label: "ip", kind: monaco.languages.CompletionItemKind.Variable, insertText: "ip", detail: "客户端 IP 地址", range },
        { label: "userAgent", kind: monaco.languages.CompletionItemKind.Variable, insertText: "userAgent", detail: "User-Agent 头", range },
        { label: "hookName", kind: monaco.languages.CompletionItemKind.Variable, insertText: "hookName", detail: "当前钩子名称", range },
        { label: "account", kind: monaco.languages.CompletionItemKind.Variable, insertText: "account", detail: "账号", range },
      ];
      return { suggestions };
    },
  });

  // Expr 主题（适配深色模式）
  monaco.editor.defineTheme("expr-dark", {
    base: "vs-dark",
    inherit: true,
    rules: [
      { token: "type.identifier", foreground: "4EC9B0", fontStyle: "bold" },
      { token: "keyword", foreground: "569CD6" },
      { token: "string", foreground: "CE9178" },
      { token: "number", foreground: "B5CEA8" },
      { token: "comment", foreground: "6A9955" },
      { token: "operator", foreground: "D4D4D4" },
    ],
    colors: { "editor.background": "#1E1E2E" },
  });

  monaco.editor.defineTheme("expr-light", {
    base: "vs",
    inherit: true,
    rules: [
      { token: "type.identifier", foreground: "267F99", fontStyle: "bold" },
      { token: "keyword", foreground: "0000FF" },
      { token: "string", foreground: "A31515" },
      { token: "number", foreground: "098658" },
      { token: "comment", foreground: "008000" },
    ],
    colors: {},
  });
}

// ── Expr 脚本模板大全 ──

const EXPR_TEMPLATES = [
  {
    name: "空白模板",
    category: "基础",
    description: "插件骨架，包含完整注释说明",
    code: `// ═══════════════════════════════════════════════════
// Aegis 插件模板 — 空白骨架
// ═══════════════════════════════════════════════════
//
// 可用变量（由钩子注入）：
//   account   — 当前操作的账号名
//   ip        — 客户端 IP 地址
//   userAgent — 客户端 User-Agent
//   hookName  — 当前触发的钩子名称
//   appId     — 应用 ID（仅用户侧钩子）
//   userId    — 用户 ID（仅用户侧钩子）
//   adminId   — 管理员 ID（仅管理员侧钩子）
//
// 可用函数：
//   allow()              — 允许请求继续
//   deny("原因")         — 拒绝请求并返回原因
//   log("level", "msg")  — 记录日志 (info/warn/error)
//   matchCIDR(ip, cidr)  — IP 是否在 CIDR 范围内
//   hour()               — 当前小时 (0-23)
//   minute()             — 当前分钟 (0-59)
//   weekday()            — 星期几 (0=日 1=一 ... 6=六)
//   date()               — 当前日期 "MM-DD"
//   datetime()           — 完整时间戳
//   now()                — Go time.Time 对象
//
// 返回值：
//   allow()  或  deny("原因")
// ═══════════════════════════════════════════════════

allow()`,
  },
  {
    name: "IP 白名单（多网段）",
    category: "安全",
    description: "仅允许内网 + 指定公网 IP 段访问，其余全部拒绝",
    code: `// ═══════════════════════════════════════════════════
// IP 白名单策略 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// 允许的网段：
//   - 10.0.0.0/8       RFC1918 A 类内网
//   - 172.16.0.0/12    RFC1918 B 类内网
//   - 192.168.0.0/16   RFC1918 C 类内网
//   - 100.64.0.0/10    CGNAT 共享地址
//   - 127.0.0.0/8      本地回环
//   - 自定义公网段（按需添加）

matchCIDR(ip, "10.0.0.0/8")
  || matchCIDR(ip, "172.16.0.0/12")
  || matchCIDR(ip, "192.168.0.0/16")
  || matchCIDR(ip, "100.64.0.0/10")
  || matchCIDR(ip, "127.0.0.0/8")
  // || matchCIDR(ip, "203.0.113.0/24")  // 取消注释添加公网段
  ? allow()
  : deny("IP " + ip + " 不在允许的访问范围内，请联系管理员")`,
  },
  {
    name: "IP 黑名单（动态封禁）",
    category: "安全",
    description: "拦截指定 IP/网段，支持多条规则",
    code: `// ═══════════════════════════════════════════════════
// IP 黑名单策略 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// 封禁的 IP/网段列表（按需修改）

matchCIDR(ip, "203.0.113.0/24")     // 示例：某 IDC 段
  || matchCIDR(ip, "198.51.100.0/24") // 示例：已知攻击源
  || ip == "45.33.32.156"              // 示例：单个 IP
  ? deny("您的 IP（" + ip + "）已被安全策略封禁")
  : allow()`,
  },
  {
    name: "User-Agent 安全过滤",
    category: "安全",
    description: "拦截已知攻击工具、爬虫和扫描器的 UA",
    code: `// ═══════════════════════════════════════════════════
// User-Agent 安全过滤 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// 拦截已知恶意工具的 User-Agent 特征

userAgent contains "sqlmap"      // SQL 注入工具
  || userAgent contains "nmap"     // 端口扫描器
  || userAgent contains "nikto"    // Web 漏洞扫描器
  || userAgent contains "masscan"  // 大规模端口扫描
  || userAgent contains "dirbuster" // 目录爆破
  || userAgent contains "gobuster"  // 目录爆破
  || userAgent contains "nuclei"    // 漏洞扫描框架
  || userAgent contains "zgrab"     // 网络扫描
  || userAgent contains "python-requests" // 脚本请求（可选）
  ? deny("检测到自动化工具请求，访问已被拒绝")
  : allow()`,
  },
  {
    name: "禁止特定账号登录",
    category: "安全",
    description: "黑名单账号列表，拦截测试/临时/通用账号",
    code: `// ═══════════════════════════════════════════════════
// 账号黑名单 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// 禁止登录的账号列表

account == "test" || account == "demo" || account == "guest"
  || account == "admin" || account == "root" || account == "administrator"
  || account == "temp" || account == "tmp" || account == "debug"
  || account startsWith "test_"
  || account startsWith "tmp_"
  ? deny("账号 " + account + " 已被安全策略禁止登录")
  : allow()`,
  },
  {
    name: "工作时间访问控制",
    category: "访问控制",
    description: "工作日 9:00-18:00 + 周六上午半天",
    code: `// ═══════════════════════════════════════════════════
// 工作时间策略 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// weekday(): 0=周日 1=周一 ... 5=周五 6=周六
// hour():    0-23 小时

// 周一至周五 9:00-18:00
(weekday() >= 1 && weekday() <= 5 && hour() >= 9 && hour() < 18)
  // 周六 9:00-12:00（可选加班半天）
  || (weekday() == 6 && hour() >= 9 && hour() < 12)
  ? allow()
  : deny("当前时段（" + datetime() + "）不在允许的工作时间范围内")`,
  },
  {
    name: "节假日黑名单",
    category: "访问控制",
    description: "中国法定节假日禁止登录（2025-2026）",
    code: `// ═══════════════════════════════════════════════════
// 节假日策略 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// date() 返回 "MM-DD" 格式
// 中国 2025-2026 法定节假日（根据实际公告修改）

// 元旦
date() == "01-01"
  // 春节（约 2 月初，每年不同）
  || date() == "01-28" || date() == "01-29" || date() == "01-30"
  || date() == "01-31" || date() == "02-01" || date() == "02-02" || date() == "02-03"
  // 清明节
  || date() == "04-04" || date() == "04-05" || date() == "04-06"
  // 劳动节
  || date() == "05-01" || date() == "05-02" || date() == "05-03"
  || date() == "05-04" || date() == "05-05"
  // 端午节
  || date() == "05-31" || date() == "06-01" || date() == "06-02"
  // 中秋节 + 国庆节
  || date() == "10-01" || date() == "10-02" || date() == "10-03"
  || date() == "10-04" || date() == "10-05" || date() == "10-06"
  || date() == "10-07" || date() == "10-08"
  ? deny("今天（" + date() + "）为法定节假日，系统暂停登录")
  : allow()`,
  },
  {
    name: "夜间禁止登录",
    category: "访问控制",
    description: "0:00-6:00 深夜禁止所有登录操作",
    code: `// ═══════════════════════════════════════════════════
// 夜间封锁策略 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// 禁止凌晨 0:00-6:00 的登录行为（防止异常时段操作）

hour() >= 0 && hour() < 6
  ? deny("安全策略：凌晨 0:00-6:00 禁止登录操作，当前时间 " + datetime())
  : allow()`,
  },
  {
    name: "邮箱域名白名单",
    category: "访问控制",
    description: "仅允许指定公司邮箱后缀登录",
    code: `// ═══════════════════════════════════════════════════
// 邮箱域名策略 — 绑定到 auth.onPreLogin (before)
// ═══════════════════════════════════════════════════
// 仅允许以下公司邮箱域名登录

account endsWith "@company.com"
  || account endsWith "@corp.cn"
  || account endsWith "@internal.org"
  ? allow()
  : deny("仅允许公司邮箱地址（@company.com / @corp.cn）登录")`,
  },
  {
    name: "综合登录安全策略",
    category: "安全",
    description: "IP + 时间 + UA + 账号的多维度组合检查",
    code: `// ═══════════════════════════════════════════════════
// 综合登录安全策略 — 绑定到 auth.onPreLogin (before)
// 多维度组合检查，任一维度命中即拒绝
// ═══════════════════════════════════════════════════

// 维度 1：IP 黑名单
matchCIDR(ip, "0.0.0.0/8")  // 保留地址
  ? deny("IP 地址无效")

// 维度 2：UA 检查（空 UA 可疑）
  : userAgent == ""
  ? deny("缺少 User-Agent 头，请使用正常浏览器访问")

// 维度 3：夜间封锁
  : hour() >= 1 && hour() < 5
  ? deny("凌晨维护时段（1:00-5:00）禁止登录")

// 维度 4：账号格式检查
  : len(account) < 3
  ? deny("账号长度不能少于 3 个字符")

// 全部通过
  : allow()`,
  },
  {
    name: "登录失败详细告警",
    category: "审计",
    description: "登录失败时记录完整上下文（绑定到 auth.onLoginFailed）",
    code: `// ═══════════════════════════════════════════════════
// 登录失败告警 — 绑定到 auth.onLoginFailed (after)
// ═══════════════════════════════════════════════════
// 记录完整的失败上下文，用于安全分析

log("warn", "[LOGIN_FAILED] account=" + account + " ip=" + ip + " ua=" + userAgent + " time=" + datetime())

// after 钩子始终返回 allow（不影响已完成的业务流程）
allow()`,
  },
  {
    name: "用户注册通知",
    category: "审计",
    description: "新用户注册后记录详细日志（绑定到 user.onRegistered）",
    code: `// ═══════════════════════════════════════════════════
// 注册审计 — 绑定到 user.onRegistered (after)
// ═══════════════════════════════════════════════════

log("info", "[USER_REGISTERED] account=" + account + " ip=" + ip + " time=" + datetime())

allow()`,
  },
  {
    name: "管理员操作审计",
    category: "审计",
    description: "记录管理员创建/状态变更/权限修改",
    code: `// ═══════════════════════════════════════════════════
// 管理员操作审计 — 绑定到 admin.onCreated / admin.onStatusChanged / admin.onAccessUpdated (after)
// ═══════════════════════════════════════════════════

log("info", "[ADMIN_AUDIT] hook=" + hookName + " adminId=" + string(adminId) + " time=" + datetime())

allow()`,
  },
  {
    name: "应用生命周期追踪",
    category: "审计",
    description: "追踪应用创建/更新/删除事件",
    code: `// ═══════════════════════════════════════════════════
// 应用生命周期 — 绑定到 app.onCreated / app.onUpdated / app.onDeleted (after)
// ═══════════════════════════════════════════════════

log("info", "[APP_LIFECYCLE] hook=" + hookName + " appId=" + string(appId) + " time=" + datetime())

allow()`,
  },
  {
    name: "系统启动通知",
    category: "监控",
    description: "系统启动完成后记录日志（绑定到 system.onStartup）",
    code: `// ═══════════════════════════════════════════════════
// 启动通知 — 绑定到 system.onStartup (after)
// ═══════════════════════════════════════════════════

log("info", "[SYSTEM_STARTUP] Aegis 插件系统已启动 time=" + datetime())

allow()`,
  },
  {
    name: "设置变更告警",
    category: "监控",
    description: "平台设置修改后记录告警（绑定到 system.onSettingsUpdated）",
    code: `// ═══════════════════════════════════════════════════
// 设置变更告警 — 绑定到 system.onSettingsUpdated (after)
// ═══════════════════════════════════════════════════

log("warn", "[SETTINGS_CHANGED] 平台设置已被修改 time=" + datetime())

allow()`,
  },
];

const templateCategories = [...new Set(EXPR_TEMPLATES.map((t) => t.category))];

// 钩子点定义（与后端 plugin_registry.go 对齐）
function GetAllHookDefinitions() {
  return [
    { name: "auth.onPreLogin", domain: "auth", phase: "both", description: "登录前检查（可拒绝）" },
    { name: "auth.onPasswordVerified", domain: "auth", phase: "after", description: "密码验证后" },
    { name: "auth.onMFACreated", domain: "auth", phase: "after", description: "MFA 挑战创建后" },
    { name: "auth.onMFAVerified", domain: "auth", phase: "after", description: "MFA 验证成功后" },
    { name: "auth.onSessionIssued", domain: "auth", phase: "after", description: "会话颁发后" },
    { name: "auth.onLoginFailed", domain: "auth", phase: "after", description: "登录失败后" },
    { name: "user.onRegistered", domain: "user", phase: "after", description: "用户注册完成后" },
    { name: "user.onProfileUpdated", domain: "user", phase: "after", description: "用户资料更新后" },
    { name: "user.onDeleted", domain: "user", phase: "after", description: "用户删除后" },
    { name: "user.onRoleChanged", domain: "user", phase: "after", description: "用户角色变更后" },
    { name: "user.onBanned", domain: "user", phase: "after", description: "用户封禁后" },
    { name: "app.onCreated", domain: "app", phase: "after", description: "应用创建后" },
    { name: "app.onUpdated", domain: "app", phase: "after", description: "应用更新后" },
    { name: "app.onDeleted", domain: "app", phase: "after", description: "应用删除后" },
    { name: "admin.onCreated", domain: "admin", phase: "after", description: "管理员创建后" },
    { name: "admin.onStatusChanged", domain: "admin", phase: "after", description: "管理员状态变更后" },
    { name: "admin.onAccessUpdated", domain: "admin", phase: "after", description: "管理员权限更新后" },
    { name: "payment.onCreated", domain: "payment", phase: "after", description: "支付订单创建后" },
    { name: "payment.onCompleted", domain: "payment", phase: "after", description: "支付完成后" },
    { name: "payment.onRefunded", domain: "payment", phase: "after", description: "退款完成后" },
    { name: "notification.onCreated", domain: "notification", phase: "after", description: "通知创建后" },
    { name: "notification.onSent", domain: "notification", phase: "after", description: "通知发送后" },
    { name: "storage.onFileUploaded", domain: "storage", phase: "after", description: "文件上传后" },
    { name: "storage.onFileDeleted", domain: "storage", phase: "after", description: "文件删除后" },
    { name: "system.onSettingsUpdated", domain: "system", phase: "after", description: "系统设置更新后" },
    { name: "system.onStartup", domain: "system", phase: "after", description: "系统启动后" },
  ];
}

const statusVariant = (s: string) => s === "enabled" ? "success" as const : s === "error" ? "danger" as const : "outline" as const;
const statusLabel = (s: string) => s === "enabled" ? "已启用" : s === "error" ? "错误" : "已禁用";
const typeLabel = (t: string) => t === "expr" ? "Expr 表达式" : "WASM 模块";

const domainLabels: Record<string, string> = {
  auth: "认证", user: "用户", app: "应用", admin: "管理员",
  payment: "支付", notification: "通知", storage: "存储", system: "系统",
};

export default function PluginsPage() {
  const token = useAuthStore((s) => s.accessToken) || "";
  const qc = useQueryClient();
  const [keyword, setKeyword] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [typeFilter, setTypeFilter] = useState("all");
  const [page, setPage] = useState(1);

  const pluginsQuery = useQuery({
    queryKey: ["plugins", keyword, statusFilter, typeFilter, page],
    queryFn: () => listPlugins(token, {
      keyword: keyword || undefined,
      status: statusFilter === "all" ? undefined : statusFilter,
      type: typeFilter === "all" ? undefined : typeFilter,
      page, limit: 20,
    }),
    enabled: !!token,
  });

  const registryQuery = useQuery({
    queryKey: ["hook-registry"],
    queryFn: () => getHookRegistry(token),
    enabled: !!token,
  });

  const execQuery = useQuery({
    queryKey: ["hook-executions"],
    queryFn: () => listHookExecutions(token, { page: 1, limit: 20 }),
    enabled: !!token,
  });

  const toggleMutation = useMutation({
    mutationFn: async ({ id, enabled }: { id: number; enabled: boolean }) => {
      if (enabled) await disablePlugin(token, id);
      else await enablePlugin(token, id);
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["plugins"] }); toast.success("操作成功"); },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "操作失败"),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deletePlugin(token, id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["plugins"] }); toast.success("插件已删除"); },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : "删除失败"),
  });

  const data = pluginsQuery.data;
  const registry = registryQuery.data;
  const executions = execQuery.data;

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="插件系统" />

      <Tabs defaultValue="plugins">
        <TabsList>
          <TabsTrigger value="plugins"><Puzzle className="size-4" />插件管理</TabsTrigger>
          <TabsTrigger value="hooks"><Activity className="size-4" />钩子注册表</TabsTrigger>
          <TabsTrigger value="logs"><Code2 className="size-4" />执行日志</TabsTrigger>
        </TabsList>

        {/* ── 插件管理 ── */}
        <TabsContent value="plugins" className="space-y-4">
          <div className="flex items-center gap-2 flex-wrap">
            <div className="relative flex-1 min-w-48">
              <Search className="absolute left-2.5 top-2.5 size-3.5 text-muted-foreground" />
              <Input placeholder="搜索插件..." value={keyword} onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
                className="pl-8 h-9 text-xs" />
            </div>
            <Select value={statusFilter} onValueChange={(v) => { setStatusFilter(v); setPage(1); }}>
              <SelectTrigger className="w-28 h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="enabled">已启用</SelectItem>
                <SelectItem value="disabled">已禁用</SelectItem>
                <SelectItem value="error">错误</SelectItem>
              </SelectContent>
            </Select>
            <Select value={typeFilter} onValueChange={(v) => { setTypeFilter(v); setPage(1); }}>
              <SelectTrigger className="w-28 h-9 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部类型</SelectItem>
                <SelectItem value="expr">Expr</SelectItem>
                <SelectItem value="wasm">WASM</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="outline" size="sm" onClick={() => pluginsQuery.refetch()}><RefreshCw className="size-3.5" /></Button>
            <PluginCreateSheet token={token} onCreated={() => qc.invalidateQueries({ queryKey: ["plugins"] })} />
          </div>

          {pluginsQuery.isLoading ? <LoadingState title="加载中" description="" /> : !data?.items.length ? <EmptyState title="暂无插件" description="点击右上角新建按钮创建第一个插件" /> : (
            <div className="space-y-2">
              {data.items.map((p) => (
                <Card key={p.id}>
                  <CardContent className="flex items-center gap-4 p-4">
                    <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/50">
                      {p.type === "expr" ? <Code2 className="size-4 text-muted-foreground" /> : <Cpu className="size-4 text-muted-foreground" />}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-semibold">{p.displayName}</span>
                        <Badge variant={statusVariant(p.status)} className="text-[9px]">{statusLabel(p.status)}</Badge>
                        <Badge variant="outline" className="text-[9px]">{typeLabel(p.type)}</Badge>
                        <Badge variant="outline" className="text-[9px]">{p.hooks.length} 钩子</Badge>
                      </div>
                      <p className="text-xs text-muted-foreground truncate">{p.description || p.name}</p>
                      {p.errorMessage && <p className="text-[10px] text-red-500 truncate">{p.errorMessage}</p>}
                    </div>
                    <div className="flex items-center gap-1.5 shrink-0">
                      <Button variant="ghost" size="sm" className="h-7 w-7 p-0"
                        onClick={() => toggleMutation.mutate({ id: p.id, enabled: p.status === "enabled" })}>
                        {p.status === "enabled" ? <PowerOff className="size-3.5" /> : <Power className="size-3.5" />}
                      </Button>
                      <Button variant="ghost" size="sm" className="h-7 w-7 p-0 text-red-500 hover:text-red-600"
                        onClick={() => { if (confirm("确认删除?")) deleteMutation.mutate(p.id); }}>
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              ))}
              {data.totalPages > 1 && (
                <div className="flex justify-center gap-2 pt-2">
                  <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
                  <span className="text-xs text-muted-foreground self-center">{page} / {data.totalPages}</span>
                  <Button variant="outline" size="sm" disabled={page >= data.totalPages} onClick={() => setPage(page + 1)}>下一页</Button>
                </div>
              )}
            </div>
          )}
        </TabsContent>

        {/* ── 钩子注册表 ── */}
        <TabsContent value="hooks" className="space-y-4">
          {!registry ? <LoadingState title="加载中" description="" /> : (
            <Accordion type="multiple" className="space-y-2">
              {Object.entries(
                registry.hooks.reduce((acc, h) => {
                  (acc[h.domain] = acc[h.domain] || []).push(h);
                  return acc;
                }, {} as Record<string, typeof registry.hooks>)
              ).map(([domain, hooks]) => (
                <AccordionItem key={domain} value={domain} className="rounded-xl border overflow-hidden border-b-0">
                  <AccordionTrigger className="hover:no-underline py-3 px-4">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-semibold">{domainLabels[domain] || domain}</span>
                      <Badge variant="outline" className="text-[10px]">{hooks.length} 个钩子</Badge>
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="px-4 pb-4">
                    <div className="space-y-2">
                      {hooks.map((h) => {
                        const binds = registry.bindings[h.name] || [];
                        return (
                          <div key={h.name} className="flex items-center justify-between rounded-lg border bg-muted/30 px-3 py-2">
                            <div>
                              <div className="text-xs font-medium font-mono">{h.name}</div>
                              <div className="text-[10px] text-muted-foreground">{h.description}</div>
                            </div>
                            <div className="flex items-center gap-1.5">
                              <Badge variant={h.phase === "both" ? "warning" : "outline"} className="text-[9px]">{h.phase}</Badge>
                              {binds.length > 0 && <Badge variant="success" className="text-[9px]">{binds.length} 插件</Badge>}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          )}
        </TabsContent>

        {/* ── 执行日志 ── */}
        <TabsContent value="logs" className="space-y-4">
          {!executions ? <LoadingState title="加载中" description="" /> : !executions.items.length ? <EmptyState title="暂无执行日志" description="启用插件并触发钩子后将在此显示" /> : (
            <div className="space-y-1.5">
              {executions.items.map((e) => (
                <div key={e.id} className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2 text-xs">
                  <Badge variant={e.status === "success" ? "success" : "danger"} className="text-[9px] shrink-0">{e.status}</Badge>
                  <span className="font-mono text-muted-foreground shrink-0 w-20 truncate">{e.pluginName}</span>
                  <span className="font-mono truncate flex-1">{e.hookName}</span>
                  <span className="text-muted-foreground shrink-0">{e.durationMs.toFixed(2)}ms</span>
                  <span className="text-muted-foreground shrink-0 w-36 text-right">{new Date(e.createdAt).toLocaleString("zh-CN")}</span>
                </div>
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── 创建插件 Sheet ──

function PluginCreateSheet({ token, onCreated }: { token: string; onCreated: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [type, setType] = useState<"expr" | "wasm">("expr");
  const [script, setScript] = useState(EXPR_TEMPLATES[0].code);
  const [templateOpen, setTemplateOpen] = useState(false);
  const [priority, setPriority] = useState(100);
  const [hooks, setHooks] = useState<Array<{ hookName: string; phase: "before" | "after" }>>([]);
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (!name.trim() || !displayName.trim()) {
      toast.error("名称不能为空");
      return;
    }
    setSaving(true);
    try {
      await createPlugin(token, {
        name: name.trim(), displayName: displayName.trim(), description,
        type, exprScript: type === "expr" ? script : undefined, priority,
        hooks: hooks.map((h) => ({ ...h, priority })),
      });
      toast.success("插件已创建");
      setOpen(false);
      onCreated();
      setName(""); setDisplayName(""); setDescription(""); setScript(EXPR_TEMPLATES[0].code); setHooks([]);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "创建失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button size="sm"><Plus className="size-3.5" /> 新建插件</Button>
      </SheetTrigger>
      <SheetContent className="sm:max-w-2xl overflow-y-auto">
        <SheetHeader><SheetTitle>新建插件</SheetTitle></SheetHeader>
        <div className="space-y-4 mt-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">标识符</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-plugin" className="h-8 text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">显示名称</Label>
              <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="我的插件" className="h-8 text-xs" />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs">描述</Label>
            <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="插件描述" className="h-8 text-xs" />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">类型</Label>
              <Select value={type} onValueChange={(v) => setType(v as "expr" | "wasm")}>
                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="expr">Expr 表达式</SelectItem>
                  <SelectItem value="wasm">WASM 模块</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">优先级（越小越先）</Label>
              <Input type="number" value={priority} onChange={(e) => setPriority(Number(e.target.value))} className="h-8 text-xs" />
            </div>
          </div>
          {type === "expr" && (
            <div className="space-y-3">
              {/* 模板选择器 */}
              <div className="space-y-1.5">
                <Label className="text-xs">脚本模板</Label>
                <div className="flex flex-wrap gap-1.5">
                  {templateCategories.map((cat) => (
                    <div key={cat} className="flex items-center gap-1">
                      <span className="text-[9px] text-muted-foreground font-medium uppercase">{cat}</span>
                      {EXPR_TEMPLATES.filter((t) => t.category === cat).map((t) => (
                        <Button key={t.name} variant={script === t.code ? "default" : "outline"}
                          size="sm" className="h-6 text-[10px] px-2"
                          onClick={() => { setScript(t.code); setTemplateOpen(false); }}>
                          {t.name}
                        </Button>
                      ))}
                    </div>
                  ))}
                </div>
              </div>
              <ExprScriptEditor value={script} onChange={setScript} />
            </div>
          )}
          {/* 钩子绑定 */}
          <div className="space-y-2">
            <Label className="text-xs">绑定钩子（至少选择一个）</Label>
            <Accordion type="multiple" className="space-y-1">
              {Object.entries(
                GetAllHookDefinitions().reduce((acc, h) => {
                  (acc[h.domain] = acc[h.domain] || []).push(h);
                  return acc;
                }, {} as Record<string, Array<{ name: string; domain: string; phase: string; description: string }>>)
              ).map(([domain, domainHooks]) => (
                <AccordionItem key={domain} value={`hook-${domain}`} className="rounded-lg border overflow-hidden border-b-0">
                  <AccordionTrigger className="hover:no-underline py-2 px-3 text-xs">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{domainLabels[domain] || domain}</span>
                      <Badge variant="outline" className="text-[9px]">{domainHooks.length}</Badge>
                      {hooks.some((h) => domainHooks.some((dh) => dh.name === h.hookName)) && (
                        <Badge variant="success" className="text-[9px]">
                          {hooks.filter((h) => domainHooks.some((dh) => dh.name === h.hookName)).length} 已选
                        </Badge>
                      )}
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="px-3 pb-2">
                    <div className="space-y-1">
                      {domainHooks.map((dh) => {
                        const bound = hooks.find((h) => h.hookName === dh.name);
                        return (
                          <div key={dh.name} className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50">
                            <div className="flex items-center gap-2 min-w-0">
                              <Switch
                                checked={!!bound}
                                onCheckedChange={(checked) => {
                                  if (checked) {
                                    setHooks((prev) => [...prev, { hookName: dh.name, phase: dh.phase === "both" ? "before" : (dh.phase as "before" | "after") }]);
                                  } else {
                                    setHooks((prev) => prev.filter((h) => h.hookName !== dh.name));
                                  }
                                }}
                              />
                              <div className="min-w-0">
                                <div className="text-[11px] font-mono truncate">{dh.name}</div>
                                <div className="text-[9px] text-muted-foreground">{dh.description}</div>
                              </div>
                            </div>
                            {bound && dh.phase === "both" && (
                              <Select value={bound.phase} onValueChange={(v) => {
                                setHooks((prev) => prev.map((h) => h.hookName === dh.name ? { ...h, phase: v as "before" | "after" } : h));
                              }}>
                                <SelectTrigger className="h-6 w-20 text-[10px]"><SelectValue /></SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="before">before</SelectItem>
                                  <SelectItem value="after">after</SelectItem>
                                </SelectContent>
                              </Select>
                            )}
                            {bound && dh.phase !== "both" && (
                              <Badge variant="outline" className="text-[9px] shrink-0">{dh.phase}</Badge>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
            {hooks.length > 0 && (
              <p className="text-[10px] text-muted-foreground">已选 {hooks.length} 个钩子：{hooks.map((h) => h.hookName).join("、")}</p>
            )}
          </div>

          <Button className="w-full" onClick={handleSave} disabled={saving || hooks.length === 0}>
            {saving ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
            创建插件{hooks.length === 0 ? "（请选择钩子）" : ""}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ── Expr 脚本编辑器（Monaco + 自定义语法高亮 + 自动补全） ──

function ExprScriptEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [isDark, setIsDark] = useState(false);
  // 等自托管 Monaco 就绪再渲染，避免 @monaco-editor/react 自行 init 回落到公网 CDN
  const [monacoReady, setMonacoReady] = useState(false);

  useState(() => {
    setIsDark(document.documentElement.classList.contains("dark"));
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains("dark"));
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
  });

  useEffect(() => {
    let alive = true;
    void loadMonaco().then(() => {
      if (alive) setMonacoReady(true);
    });
    return () => {
      alive = false;
    };
  }, []);

  if (!monacoReady) {
    return (
      <div className="space-y-1.5">
        <Label className="text-xs">Expr 脚本</Label>
        <div className="flex h-[300px] items-center justify-center rounded-md border bg-muted/20">
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      <Label className="text-xs">Expr 脚本</Label>
      <div className="rounded-md border overflow-hidden">
        <MonacoEditor
          height="300px"
          language="expr"
          theme={isDark ? "expr-dark" : "expr-light"}
          value={value}
          onChange={(v) => onChange(v || "")}
          beforeMount={registerExprLanguage}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Menlo, monospace",
            fontLigatures: true,
            lineNumbers: "on",
            scrollBeyondLastLine: false,
            wordWrap: "on",
            tabSize: 2,
            automaticLayout: true,
            padding: { top: 8, bottom: 8 },
            suggestOnTriggerCharacters: true,
            quickSuggestions: true,
            bracketPairColorization: { enabled: true },
            guides: { bracketPairs: true, indentation: true },
            renderLineHighlight: "line",
            cursorBlinking: "smooth",
            cursorSmoothCaretAnimation: "on",
            smoothScrolling: true,
            contextmenu: false,
            folding: false,
          }}
        />
      </div>
      <p className="text-[10px] text-muted-foreground">
        内置函数：<code className="text-[9px] bg-muted px-1 rounded">allow()</code> <code className="text-[9px] bg-muted px-1 rounded">deny(&quot;原因&quot;)</code> <code className="text-[9px] bg-muted px-1 rounded">matchCIDR(ip, cidr)</code> <code className="text-[9px] bg-muted px-1 rounded">hour()</code> <code className="text-[9px] bg-muted px-1 rounded">weekday()</code> <code className="text-[9px] bg-muted px-1 rounded">date()</code> <code className="text-[9px] bg-muted px-1 rounded">now()</code>
        &nbsp;·&nbsp;变量：<code className="text-[9px] bg-muted px-1 rounded">ip</code> <code className="text-[9px] bg-muted px-1 rounded">userAgent</code> <code className="text-[9px] bg-muted px-1 rounded">hookName</code> <code className="text-[9px] bg-muted px-1 rounded">account</code>
      </p>
    </div>
  );
}
