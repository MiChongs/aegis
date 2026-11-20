/**
 * 应用用户站内信（消息中心）的分类、跳转页面与级别。
 *
 * 与后端 internal/domain/notification/category.go 一一对应（docs/notification-center.md），
 * 顺序即用户端的展示顺序。后端多出来的 key 在这里查不到时原样显示 key。
 */

export const USER_NOTIFICATION_CATEGORIES = [
  { value: "system", label: "系统通知" },
  { value: "announcement", label: "公告活动" },
  { value: "security", label: "账号安全" },
  { value: "membership", label: "会员与订单" },
  { value: "reward", label: "积分与奖励" },
  { value: "ticket", label: "工单与反馈" }
] as const;

export type UserNotificationCategory = (typeof USER_NOTIFICATION_CATEGORIES)[number]["value"];

/** 应用内页面（路由键白名单），客户端不认识的键不显示按钮。 */
export const USER_NOTIFICATION_ROUTES = [
  { value: "points", label: "积分中心" },
  { value: "points_store", label: "积分商城" },
  { value: "leaderboard", label: "排行榜" },
  { value: "lottery", label: "幸运抽奖" },
  { value: "card_keys", label: "卡密兑换" },
  { value: "signin", label: "签到" },
  { value: "membership", label: "会员" },
  { value: "wallet", label: "钱包" },
  { value: "security", label: "账号安全" },
  { value: "invite", label: "邀请" },
  { value: "cosmetics", label: "个性装扮" },
  { value: "tickets", label: "工单" },
  { value: "profile", label: "个人资料" }
] as const;

export const USER_NOTIFICATION_LEVELS = [
  { value: "info", label: "普通" },
  { value: "success", label: "成功" },
  { value: "warning", label: "提醒" },
  { value: "error", label: "重要" }
] as const;

export type UserNotificationAction = {
  type: "route" | "url";
  target: string;
  label?: string;
};

function labelOf(list: ReadonlyArray<{ value: string; label: string }>, value?: string | null) {
  if (!value) return "";
  return list.find((item) => item.value === value)?.label ?? value;
}

export function userNotificationCategoryLabel(value?: string | null) {
  return labelOf(USER_NOTIFICATION_CATEGORIES, value);
}

export function userNotificationRouteLabel(value?: string | null) {
  return labelOf(USER_NOTIFICATION_ROUTES, value);
}

export function userNotificationLevelLabel(value?: string | null) {
  // 旧数据里的 critical 与 error 同义
  return labelOf(USER_NOTIFICATION_LEVELS, value === "critical" ? "error" : value);
}

/** 动作的一句话说明：应用内页面给中文名，链接给地址。 */
export function describeUserNotificationAction(action?: UserNotificationAction | null) {
  if (!action?.target) return "";
  return action.type === "route" ? userNotificationRouteLabel(action.target) : action.target;
}

/** 是否为合法的 https 绝对地址（与后端校验一致：必须 https、有主机名、不带账号密码与空白）。 */
export function isHttpsUrl(value: string) {
  if (!value || /\s/.test(value)) return false;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && Boolean(url.hostname) && !url.username && !url.password;
  } catch {
    return false;
  }
}
