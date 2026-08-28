/**
 * 应用用户列表的**查询状态**：单一形状 + URL 编解码。
 *
 * 整页只有这一个状态对象，筛选、排序、分页、请求参数、导出参数、
 * 保存的视图全部由它派生。旧版把 keyword / status / page / limit 拆成四个
 * useState 再逐个同步进 URL（`updateListUrl` 里五个 if），加一个筛选条件
 * 要动四处 —— 这就是那十个后端已支持的过滤字段一个都没接上来的原因。
 */

export type SortField =
  | "createdAt"
  | "updatedAt"
  | "id"
  | "account"
  | "integral"
  | "experience"
  | "vipExpireAt"
  | "registerTime"
  | "nickname"
  | "email";

export type UserQueryState = {
  appKey: string | null;
  keyword: string;
  status: "all" | "enabled" | "disabled";
  /** 精确字段：走等值/前缀匹配，与 keyword 的全字段模糊搜不是一回事 */
  account: string;
  nickname: string;
  email: string;
  phone: string;
  inviteCode: string;
  registerIp: string;
  markcode: string;
  customId: string;
  /** YYYY-MM-DD */
  createdFrom: string;
  createdTo: string;
  sort: SortField;
  order: "asc" | "desc";
  page: number;
  limit: number;
};

export const DEFAULT_QUERY: UserQueryState = {
  appKey: null,
  keyword: "",
  status: "all",
  account: "",
  nickname: "",
  email: "",
  phone: "",
  inviteCode: "",
  registerIp: "",
  markcode: "",
  customId: "",
  createdFrom: "",
  createdTo: "",
  sort: "createdAt",
  order: "desc",
  page: 1,
  limit: 20
};

/** 精确字段的登记表：驱动「更多条件」表单与「已筛」胶囊，两处不再各抄一份。 */
export const PRECISE_FIELDS = [
  { key: "account", label: "账号", placeholder: "精确/前缀匹配" },
  { key: "nickname", label: "昵称", placeholder: "精确/前缀匹配" },
  { key: "email", label: "邮箱", placeholder: "user@example.com" },
  { key: "phone", label: "手机", placeholder: "13800000000" },
  { key: "inviteCode", label: "邀请码", placeholder: "查这个码拉了谁" },
  { key: "registerIp", label: "注册 IP", placeholder: "找同源批量注册" },
  { key: "markcode", label: "标识码", placeholder: "设备/机器标识码" },
  { key: "customId", label: "自定义 ID", placeholder: "用户自定义 ID" }
] as const satisfies ReadonlyArray<{
  key: keyof UserQueryState;
  label: string;
  placeholder: string;
}>;

export const SORT_LABELS: Record<SortField, string> = {
  createdAt: "创建时间",
  updatedAt: "更新时间",
  id: "用户 ID",
  account: "账号",
  integral: "积分",
  experience: "经验",
  vipExpireAt: "会员到期",
  registerTime: "注册时间",
  nickname: "昵称",
  email: "邮箱"
};

const SORT_FIELDS = new Set(Object.keys(SORT_LABELS));

/** 单页条数档位。200 / 500 靠虚拟滚动承载，后端上限也是 500。 */
export const PAGE_SIZES = [20, 50, 100, 200, 500];

export function parseQuery(params: URLSearchParams): UserQueryState {
  const num = (key: string, fallback: number) => {
    const raw = Number(params.get(key));
    return Number.isFinite(raw) && raw > 0 ? raw : fallback;
  };
  const text = (key: string) => params.get(key)?.trim() ?? "";
  const rawStatus = params.get("status");
  const rawSort = params.get("sort");

  return {
    appKey: params.get("app") || null,
    keyword: text("keyword"),
    status: rawStatus === "enabled" || rawStatus === "disabled" ? rawStatus : "all",
    account: text("account"),
    nickname: text("nickname"),
    email: text("email"),
    phone: text("phone"),
    inviteCode: text("inviteCode"),
    registerIp: text("registerIp"),
    markcode: text("markcode"),
    customId: text("customId"),
    createdFrom: text("createdFrom"),
    createdTo: text("createdTo"),
    sort: rawSort && SORT_FIELDS.has(rawSort) ? (rawSort as SortField) : "createdAt",
    order: params.get("order") === "asc" ? "asc" : "desc",
    page: num("page", 1),
    limit: num("limit", 20)
  };
}

/**
 * 写回 URL。只写非默认值 —— 否则一个没动过任何筛选的页面地址栏里会挂十几个参数，
 * 分享出去的链接看不出到底筛了什么。
 */
export function serializeQuery(state: UserQueryState, extra?: Record<string, string>) {
  const params = new URLSearchParams();
  if (state.appKey) params.set("app", state.appKey);

  const put = (key: string, value: string) => {
    if (value.trim()) params.set(key, value.trim());
  };
  put("keyword", state.keyword);
  if (state.status !== "all") params.set("status", state.status);
  for (const field of PRECISE_FIELDS) put(field.key, state[field.key] as string);
  put("createdFrom", state.createdFrom);
  put("createdTo", state.createdTo);
  if (state.sort !== DEFAULT_QUERY.sort) params.set("sort", state.sort);
  if (state.order !== DEFAULT_QUERY.order) params.set("order", state.order);
  if (state.page > 1) params.set("page", String(state.page));
  if (state.limit !== DEFAULT_QUERY.limit) params.set("limit", String(state.limit));

  for (const [key, value] of Object.entries(extra ?? {})) params.set(key, value);
  const query = params.toString();
  return query ? `/app-users?${query}` : "/app-users";
}

/** 转成 API 参数。空串一律省略，不要发 `?account=` —— 那会被后端当成有条件。 */
export function toListParams(state: UserQueryState) {
  const trim = (value: string) => (value.trim() ? value.trim() : undefined);
  return {
    keyword: trim(state.keyword),
    account: trim(state.account),
    nickname: trim(state.nickname),
    email: trim(state.email),
    phone: trim(state.phone),
    inviteCode: trim(state.inviteCode),
    registerIp: trim(state.registerIp),
    markcode: trim(state.markcode),
    customId: trim(state.customId),
    enabled: state.status === "all" ? undefined : state.status === "enabled",
    createdFrom: trim(state.createdFrom),
    createdTo: trim(state.createdTo),
    sort: state.sort,
    order: state.order,
    page: state.page,
    limit: state.limit
  };
}

export type ActiveFilter = { key: keyof UserQueryState; label: string; value: string };

/** 当前生效的筛选条件，用于「已筛」胶囊。分页与排序不算筛选，不进这里。 */
export function activeFilters(state: UserQueryState): ActiveFilter[] {
  const list: ActiveFilter[] = [];
  if (state.keyword.trim()) list.push({ key: "keyword", label: "搜索", value: state.keyword.trim() });
  if (state.status !== "all") {
    list.push({ key: "status", label: "状态", value: state.status === "enabled" ? "启用" : "受限" });
  }
  for (const field of PRECISE_FIELDS) {
    const value = (state[field.key] as string).trim();
    if (value) list.push({ key: field.key, label: field.label, value });
  }
  if (state.createdFrom || state.createdTo) {
    list.push({
      key: "createdFrom",
      label: "注册于",
      value: `${state.createdFrom || "不限"} ~ ${state.createdTo || "至今"}`
    });
  }
  return list;
}

/** 清掉某一条筛选。日期是一对，必须成对清。 */
export function clearFilter(state: UserQueryState, key: keyof UserQueryState): UserQueryState {
  if (key === "createdFrom" || key === "createdTo") {
    return { ...state, createdFrom: "", createdTo: "", page: 1 };
  }
  if (key === "status") return { ...state, status: "all", page: 1 };
  return { ...state, [key]: "", page: 1 };
}

export function clearAllFilters(state: UserQueryState): UserQueryState {
  return {
    ...DEFAULT_QUERY,
    appKey: state.appKey,
    sort: state.sort,
    order: state.order,
    limit: state.limit
  };
}

// ── 日期工具 ──────────────────────────

export function toDateInput(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function parseDateInput(value: string): Date | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined;
  const date = new Date(`${value}T00:00:00`);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

export const DATE_PRESETS = [
  { key: "today", label: "今天", days: 0 },
  { key: "7d", label: "近 7 天", days: 6 },
  { key: "30d", label: "近 30 天", days: 29 },
  { key: "90d", label: "近 90 天", days: 89 }
] as const;

export function presetRange(days: number) {
  const to = new Date();
  const from = new Date();
  from.setDate(from.getDate() - days);
  return { createdFrom: toDateInput(from), createdTo: toDateInput(to) };
}
