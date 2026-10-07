/**
 * 管理员用户名与密码规则的前端镜像。
 *
 * 唯一事实源在后端 internal/service/admin_account_self_service.go，这里只用来
 * 在提交前给出即时提示；两边措辞保持一致，被服务端拒绝时错误原样显示。
 * 改规则时两处一起改。
 */

/** 字母开头，3–32 位，只含字母、数字、下划线、点、连字符 */
export const ADMIN_ACCOUNT_PATTERN = /^[A-Za-z][A-Za-z0-9_.-]{2,31}$/;

const RESERVED_ADMIN_ACCOUNTS = new Set([
  "admin", "administrator", "root", "system", "sysadmin",
  "superadmin", "super_admin", "aegis", "support", "security",
  "official", "operator", "service", "anonymous", "null", "undefined"
]);

export const ADMIN_ACCOUNT_RULE_TEXT = "以字母开头，3 到 32 位，可包含字母、数字、下划线、点和连字符";

/** 返回错误说明；合法时返回 null */
export function adminAccountNameError(value: string): string | null {
  const account = value.trim();
  if (!account) return "请输入用户名";
  if (!ADMIN_ACCOUNT_PATTERN.test(account)) {
    return "用户名须以字母开头，长度 3 到 32 位，只能包含字母、数字、下划线、点和连字符";
  }
  if (RESERVED_ADMIN_ACCOUNTS.has(account.toLowerCase())) return "该用户名为系统保留，不能使用";
  return null;
}

/** 返回错误说明；合法时返回 null。account 用于「密码不能包含用户名」 */
export function adminPasswordError(password: string, account: string): string | null {
  const length = password.trim().length;
  if (length < 8) return "密码长度不能少于 8 位";
  if (length > 72) return "密码长度不能超过 72 位";
  if (!/\p{L}/u.test(password) || !/\p{N}/u.test(password)) return "密码须同时包含字母和数字";
  const name = account.trim().toLowerCase();
  if (name.length >= 3 && password.toLowerCase().includes(name)) return "密码不能包含用户名";
  return null;
}

/**
 * 密码强度：只按长度与字符多样性画进度，不冒充最终判定。
 * 真正的裁决在服务端，被拒时错误会原样显示。
 */
export function passwordScore(value: string): { percent: number; label: string; tone: string } {
  if (!value) return { percent: 0, label: "", tone: "" };

  const variety =
    Number(/[a-z]/.test(value)) +
    Number(/[A-Z]/.test(value)) +
    Number(/\d/.test(value)) +
    Number(/[^\w\s]/.test(value));
  const lengthScore = Math.min(value.length / 16, 1);
  const percent = Math.round(Math.min(lengthScore * 0.6 + (variety / 4) * 0.4, 1) * 100);

  if (value.length < 8) return { percent: Math.max(percent, 8), label: "太短", tone: "text-destructive" };
  if (percent < 45) return { percent, label: "偏弱", tone: "text-amber-600 dark:text-amber-400" };
  if (percent < 75) return { percent, label: "一般", tone: "text-amber-600 dark:text-amber-400" };
  return { percent, label: "较强", tone: "text-emerald-600 dark:text-emerald-400" };
}
