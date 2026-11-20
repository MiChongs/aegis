"use client";

import { useState } from "react";
import {
  AtSign,
  Check,
  Clipboard,
  Fingerprint,
  Hash,
  KeyRound,
  Link2,
  Lock,
  MessageSquare,
  Phone,
  ShieldCheck,
  Ticket,
  User,
  Zap,
  type LucideIcon
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api/client";
import type { AuthProtocolPolicy, SecurityLevel } from "@/lib/api/app-auth-protocol";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** 三档安全等级。说明只写「客户端要多做什么」，细节在文档里。 */
export const LEVELS: { level: SecurityLevel; title: string; requirement: string; icon: LucideIcon }[] = [
  { level: "standard", title: "标准", requirement: "无需密钥", icon: Zap },
  { level: "signed", title: "签名", requirement: "HMAC 请求签名", icon: ShieldCheck },
  { level: "sealed", title: "加密", requirement: "签名与端到端加密", icon: Lock }
];

export function levelMeta(level: SecurityLevel) {
  return LEVELS.find((item) => item.level === level) ?? LEVELS[0];
}

export type MethodOption = { value: string; label: string; icon: LucideIcon };

// 密码登录时账号框按这里勾选的标识认人（后端 resolveLoginCandidates）：账号名永远先认且始终可用，
// 其后依次是 16 位对外 ID、邮箱、手机号。邮箱或手机号挂在多个账号上时由密码区分。
// 用户自己设的用户名不许纯数字、不含 @，所以新账号不会和另外几种标识撞在一起（docs/account-setup.md）。
// 两层身份（docs/user-number.md）：16 位对外 ID 终生不变，是这里的「id」标识；用户在 App 里看到的是
// 5–12 位、可以更换的号码（「number」标识，只在本应用里找）。勾了「id」的应用同样认号码。
// 号码与 11 位手机号、老系统导入的纯数字账号可能撞上，几类候选合在一起由密码决定。
export const IDENTIFIERS: MethodOption[] = [
  { value: "username", label: "用户名", icon: User },
  { value: "email", label: "邮箱", icon: AtSign },
  { value: "phone", label: "手机号", icon: Phone },
  { value: "number", label: "号码", icon: Hash },
  { value: "id", label: "16 位对外 ID", icon: Fingerprint }
];

// 登录与注册的可选集合刻意不同：第三方能否自动建号由每个渠道自己的
// allowRegister 决定（在「第三方登录」区块配），这里再开一个开关会变成两处配同一件事。
// cardkey 不在注册方式里：卡是运营发出去的，发出去就意味着允许它建号，
// 再加一个注册开关会造出「卡有效但登不进去」这种没人解释得清的状态。
export const LOGIN_METHODS: MethodOption[] = [
  { value: "password", label: "密码", icon: KeyRound },
  { value: "sms", label: "短信", icon: MessageSquare },
  { value: "oauth", label: "第三方", icon: Link2 },
  { value: "cardkey", label: "卡密", icon: Ticket }
];

export const REGISTER_METHODS: MethodOption[] = [
  { value: "password", label: "密码", icon: KeyRound },
  { value: "sms", label: "短信", icon: MessageSquare }
];

export const FIELD_TYPES = [
  { value: "text", label: "文本" },
  { value: "password", label: "密码" },
  { value: "email", label: "邮箱" },
  { value: "phone", label: "手机号" },
  { value: "number", label: "数字" },
  { value: "boolean", label: "是否" }
];

/** account / password / nickname 由后端映射到固定列，改名或删除会让映射失效 */
export const RESERVED_FIELDS = new Set(["account", "password", "nickname"]);

/** 管理端可写的那部分策略。比较「改没改」只看这些：密钥轮换会刷新提示位，不该被当成未保存的修改。 */
export type PolicyDraft = Pick<
  AuthProtocolPolicy,
  | "identifiers"
  | "loginMethods"
  | "registerMethods"
  | "registrationSchema"
  | "requireCaptcha"
  | "autoLoginAfterRegister"
  | "securityLevel"
  | "allowLegacy"
>;

export function draftOf(policy: AuthProtocolPolicy): PolicyDraft {
  return {
    identifiers: [...policy.identifiers],
    loginMethods: [...policy.loginMethods],
    registerMethods: [...policy.registerMethods],
    registrationSchema: policy.registrationSchema.map((field) => ({ ...field })),
    requireCaptcha: policy.requireCaptcha,
    autoLoginAfterRegister: policy.autoLoginAfterRegister,
    securityLevel: policy.securityLevel,
    allowLegacy: policy.allowLegacy
  };
}

/** 集合类字段按集合比较：勾选顺序不同不算修改 */
export function sameDraft(a: PolicyDraft, b: PolicyDraft) {
  const sameSet = (x: string[], y: string[]) => x.length === y.length && x.every((item) => y.includes(item));
  return (
    sameSet(a.identifiers, b.identifiers) &&
    sameSet(a.loginMethods, b.loginMethods) &&
    sameSet(a.registerMethods, b.registerMethods) &&
    JSON.stringify(a.registrationSchema) === JSON.stringify(b.registrationSchema) &&
    a.requireCaptcha === b.requireCaptcha &&
    a.autoLoginAfterRegister === b.autoLoginAfterRegister &&
    a.securityLevel === b.securityLevel &&
    a.allowLegacy === b.allowLegacy
  );
}

/** 加载期间喂给骨架屏的占位策略：按真实形状渲染，骨架照着它量。 */
export const PLACEHOLDER_POLICY: AuthProtocolPolicy = {
  appId: 0,
  protocolVersion: "v1",
  identifiers: ["username"],
  loginMethods: ["password", "sms"],
  registerMethods: ["password"],
  registrationSchema: [
    { name: "account", type: "text", required: true, mutable: true, label: "账号" },
    { name: "password", type: "password", required: true, mutable: true, label: "密码" }
  ],
  requireCaptcha: false,
  autoLoginAfterRegister: true,
  securityLevel: "signed",
  allowLegacy: false,
  signingSecretSet: true,
  signingSecretHint: "sk_••••••••",
  createdAt: "",
  updatedAt: ""
};

export function errorMessage(error: unknown, fallback = "操作失败") {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return fallback;
}

export function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "—";
}

export function methodLabels(values: string[], options: MethodOption[]) {
  return options.filter((option) => values.includes(option.value)).map((option) => option.label);
}

export function CopyButton({ value, label, className }: { value: string; label?: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      size="sm"
      variant="ghost"
      className={cn("h-7 gap-1 px-2 text-xs", className)}
      aria-label={label ? undefined : "复制"}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1600);
        } catch {
          toast.error("剪贴板不可用");
        }
      }}
    >
      {copied ? <Check className="size-3.5 text-emerald-600 dark:text-emerald-400" /> : <Clipboard className="size-3.5" />}
      {label ? (copied ? "已复制" : label) : null}
    </Button>
  );
}

/**
 * 一个可勾选的方式：图标 + 名称，选中时与 SwitchRow 同一种强调（翠绿底、内描边、打勾）。
 * 整块可点，比一排小复选框更容易点准，也一眼看得出开了哪几种。
 */
export function MethodToggle({
  option,
  checked,
  onToggle,
  disabled
}: {
  option: MethodOption;
  checked: boolean;
  onToggle: () => void;
  disabled?: boolean;
}) {
  const Icon = option.icon;
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      disabled={disabled}
      onClick={onToggle}
      className={cn(
        "flex items-center gap-2.5 rounded-xl bg-muted px-3 py-2.5 text-left transition-colors duration-150",
        "hover:bg-muted-foreground/10 disabled:pointer-events-none disabled:opacity-50",
        checked && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30 hover:bg-emerald-500/15"
      )}
    >
      <span
        className={cn(
          "grid size-8 shrink-0 place-items-center rounded-lg bg-background text-muted-foreground",
          checked && "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
        )}
      >
        <Icon className="size-4" />
      </span>
      <span className="min-w-0 flex-1 truncate text-sm font-medium">{option.label}</span>
      <span
        className={cn(
          "grid size-4 shrink-0 place-items-center rounded-full border border-muted-foreground/30",
          checked && "border-emerald-500 bg-emerald-500 text-white"
        )}
      >
        {checked ? <Check className="size-3" strokeWidth={3} /> : null}
      </span>
    </button>
  );
}

/** 切换一个集合里的某项 */
export function toggleValue(values: string[], value: string) {
  return values.includes(value) ? values.filter((item) => item !== value) : [...values, value];
}

/** 一行状态提示：琥珀色是「需要处理」，灰色是「知道一下」。右侧可挂一个跳转。 */
export function InlineNotice({
  tone = "muted",
  icon: Icon,
  children,
  action
}: {
  tone?: "muted" | "warning";
  icon: LucideIcon;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex flex-wrap items-center gap-2 rounded-lg px-3 py-2 text-xs",
        tone === "warning"
          ? "border border-amber-200 bg-amber-50/60 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200"
          : "bg-muted/60 text-muted-foreground"
      )}
    >
      <Icon className="size-3.5 shrink-0" />
      <span className="min-w-0 flex-1">{children}</span>
      {action}
    </div>
  );
}
