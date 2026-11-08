"use client";

/**
 * 活动：这个账号最近发生了什么。
 *
 *   顶部指标   近 30 天登录、失败、不同 IP、不同设备
 *   每日分布   近 30 天每天的成功 / 失败 / 拦截，以及最近一次成功与失败
 *   活动地图   同一批数据的空间投影
 *   登录记录   结果筛选、关键词、时间区间、详情抽屉、导出
 *   会话事件   令牌的签发、刷新、撤销轨迹，同样可筛选与导出
 *   风控评估   引擎给这个账号打的分
 *
 * 指标、每日分布与地图共用同一份不带筛选的「最近 100 条登录」样本（同一个查询键，只请求一次），
 * 不随下方表格的筛选变化。样本覆盖不到 30 天时界面会写明。
 *
 * 登录记录与会话事件的结果 / 事件类型、关键词、时间区间都在服务端筛选（lib/user-activity-hooks），
 * 导出按同样的参数逐页拉取。
 */

import { useMemo, useRef, useState, type ComponentType, type ReactNode } from "react";
import Link from "next/link";
import {
  ArrowUpRight,
  Download,
  FileClock,
  Globe2,
  KeyRound,
  KeySquare,
  Loader2,
  LogIn,
  LogOut,
  Map as MapIcon,
  MonitorSmartphone,
  Network,
  Radar,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  ShieldX,
  TimerOff
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CountryFlag } from "@/components/ui/country-flag";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useNow } from "@/components/users/ban-form";
import { useAdminToken, useAdminUserSessionsQuery } from "@/lib/admin-hooks";
import { useAdminUserLoginAuditsQuery } from "@/lib/app-user-hooks";
import type { AdminAppUserDetail, UserLoginAuditRecord, UserSessionAuditRecord } from "@/lib/api/types";
import { formatDateTime, formatRelative, formatShortDateTime, formatWith, localDateKey } from "@/lib/datetime";
import {
  listUserLoginAudits,
  listUserSessionAudits,
  useUserLoginAuditListQuery,
  useUserSessionAuditListQuery
} from "@/lib/user-activity-hooks";
import { cn } from "@/lib/utils";
import { DeviceInfoCard, DeviceInfoChip } from "../device-info-chip";
import { UserActivityMap } from "./user-activity-map";
import {
  FilterBar,
  FilterSelect,
  MetricCard,
  Pager,
  RecordSheet,
  RecordTable,
  ResetButton,
  SearchField,
  Section,
  StateBlock,
  TimeRangePicker,
  ToneIcon,
  downloadCsv,
  errorMessage,
  collectPages,
  rangeToParams,
  useDebouncedValue,
  type Column,
  type CsvColumn,
  type InfoItem,
  type TimeRange,
  type Tone
} from "./kit";
import { LOGIN_STATUS_LABEL, SESSION_EVENT_LABEL, describeUserAgent, textValue } from "./user-detail-shared";

/** 与 UserActivityMap 的样本量一致：同一个查询键命中同一份缓存 */
const LOGIN_SAMPLE = 100;
const STRIP_DAYS = 30;
const EXPORT_MAX = 5000;
const ALL_TIME: TimeRange = { preset: "all" };

// ── 文案映射 ──────────────────────────

const LOGIN_TYPE_LABEL: Record<string, string> = {
  password: "密码登录",
  register: "注册",
  refresh: "令牌续期",
  sms: "短信验证码",
  email: "邮箱验证码",
  email_code: "邮箱验证码",
  oauth: "第三方登录",
  oauth2: "第三方登录",
  native_oauth: "第三方登录",
  qrcode: "扫码登录",
  qr: "扫码登录",
  scan: "扫码登录",
  cardkey: "卡密登录",
  passkey: "通行密钥",
  webauthn: "通行密钥",
  guest: "游客登录",
  device: "设备登录"
};

const PROVIDER_LABEL: Record<string, string> = {
  password: "密码",
  sms: "短信",
  email: "邮箱",
  cardkey: "卡密",
  qq: "QQ",
  wechat: "微信",
  weixin: "微信",
  github: "GitHub",
  google: "Google",
  apple: "Apple",
  microsoft: "Microsoft",
  gitee: "Gitee",
  dingtalk: "钉钉",
  feishu: "飞书",
  alipay: "支付宝"
};

const FAILURE_REASON_LABEL: Record<string, string> = {
  invalid_password: "密码错误",
  invalid_code: "验证码错误",
  user_disabled: "账号已停用",
  user_banned: "账号被封禁",
  rate_limited: "尝试过于频繁",
  risk_blocked: "风控拦截",
  captcha_failed: "人机验证未通过"
};

const LOGIN_STATUS_OPTIONS = Object.entries(LOGIN_STATUS_LABEL).map(([value, label]) => ({ value, label }));
const EVENT_OPTIONS = Object.entries(SESSION_EVENT_LABEL).map(([value, label]) => ({ value, label }));

function loginTypeLabel(item: UserLoginAuditRecord) {
  const type = LOGIN_TYPE_LABEL[item.loginType] ?? textValue(item.loginType, "登录");
  const provider = item.provider ? (PROVIDER_LABEL[item.provider.toLowerCase()] ?? item.provider) : "";
  if (!provider || type.includes(provider) || provider === "密码") return type;
  return `${type}（${provider}）`;
}

function loginLocation(item: Pick<UserLoginAuditRecord, "isPrivate" | "city" | "region" | "country" | "location">) {
  if (item.isPrivate) return "内网地址";
  const parts: string[] = [];
  for (const part of [item.country, item.region, item.city]) {
    if (part && parts[parts.length - 1] !== part) parts.push(part);
  }
  return parts.length ? parts.join(" ") : textValue(item.location, "");
}

function failureReason(item: UserLoginAuditRecord) {
  const reason = item.metadata?.reason;
  if (typeof reason !== "string" || !reason) return "";
  return FAILURE_REASON_LABEL[reason] ?? reason;
}

function loginDevice(item: UserLoginAuditRecord) {
  return item.deviceInfo?.name || describeUserAgent(item.userAgent).label;
}

function ts(value?: string | null) {
  const t = value ? new Date(value).getTime() : NaN;
  return Number.isNaN(t) ? 0 : t;
}

function statusVariant(status: string): "success" | "warning" | "danger" | "info" {
  if (status === "success") return "success";
  if (status === "blocked") return "danger";
  if (status === "challenge") return "info";
  return "warning";
}

function stamp() {
  return localDateKey().replace(/-/g, "");
}

// ── 页签 ──────────────────────────

export function UserActivityTab({ appKey, userId, user }: { appKey: string; userId: number; user: AdminAppUserDetail }) {
  // 指标卡点「失败」时把登录记录切到失败并滚过去
  const [loginStatus, setLoginStatus] = useState("");
  const loginsRef = useRef<HTMLDivElement>(null);

  return (
    <TooltipProvider delayDuration={150}>
      <div className="flex flex-col gap-5">
        <ActivityOverview
          appKey={appKey}
          userId={userId}
          onShowFailures={() => {
            setLoginStatus("failed");
            loginsRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
          }}
        />

        <Section title="活动地图" description="活跃会话与最近 100 条登录的地理分布，相邻两次活动按时间连线" icon={MapIcon} tone="info">
          <UserActivityMap appKey={appKey} userId={userId} />
        </Section>

        <div ref={loginsRef} className="scroll-mt-4">
          <LoginRecords appKey={appKey} userId={userId} status={loginStatus} onStatusChange={setLoginStatus} />
        </div>

        <SessionEvents appKey={appKey} userId={userId} />

      </div>
    </TooltipProvider>
  );
}

// ── 指标与每日分布 ──────────────────────────

type DayBucket = { key: string; date: Date; success: number; failed: number; blocked: number };

function ActivityOverview({ appKey, userId, onShowFailures }: { appKey: string; userId: number; onShowFailures: () => void }) {
  const loginsQuery = useAdminUserLoginAuditsQuery(appKey, userId, { limit: LOGIN_SAMPLE });
  const sessionsQuery = useAdminUserSessionsQuery(appKey, userId);
  const loading = loginsQuery.isLoading || sessionsQuery.isLoading;
  // 每分钟换一次：跨过零点时「近 30 天」的格子跟着挪
  const now = useNow(60_000);

  const summary = useMemo(() => {
    const logins = loginsQuery.data?.items ?? [];
    const sessions = sessionsQuery.data?.items ?? [];
    const since = now - STRIP_DAYS * 86_400_000;

    const recent = logins.filter((l) => ts(l.createdAt) >= since);
    const success = recent.filter((l) => l.status === "success").length;
    const failed = recent.filter((l) => l.status === "failed").length;
    const blocked = recent.filter((l) => l.status === "blocked").length;
    const challenge = recent.filter((l) => l.status === "challenge").length;

    const ips = new Set<string>();
    const devices = new Set<string>();
    const places = new Map<string, number>();
    for (const l of recent) {
      if (l.loginIp) ips.add(l.loginIp);
      devices.add(l.deviceId || loginDevice(l));
      const place = loginLocation(l);
      if (place) places.set(place, (places.get(place) ?? 0) + 1);
    }
    const topPlace = [...places.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];

    const days: DayBucket[] = [];
    const index = new Map<string, DayBucket>();
    for (let i = STRIP_DAYS - 1; i >= 0; i--) {
      const date = new Date(now - i * 86_400_000);
      const bucket: DayBucket = { key: localDateKey(date), date, success: 0, failed: 0, blocked: 0 };
      days.push(bucket);
      index.set(bucket.key, bucket);
    }
    for (const l of recent) {
      const bucket = index.get(localDateKey(l.createdAt));
      if (!bucket) continue;
      if (l.status === "success") bucket.success++;
      else if (l.status === "blocked") bucket.blocked++;
      else if (l.status === "failed") bucket.failed++;
    }
    const oldest = logins[logins.length - 1];
    // 样本是最近 100 条：若第 100 条仍在 30 天内，说明这 30 天里还有更早的记录没取到
    const truncated = (loginsQuery.data?.total ?? 0) > logins.length && Boolean(oldest) && ts(oldest?.createdAt) >= since;

    return {
      attempts: recent.length,
      success,
      failed,
      blocked,
      challenge,
      ips: ips.size,
      devices: devices.size,
      places: places.size,
      topPlace,
      sessions: sessions.length,
      sessionDevices: new Set(sessions.map((s) => s.deviceId || s.userAgent).filter(Boolean)).size,
      lastSuccess: logins.find((l) => l.status === "success"),
      lastFailure: logins.find((l) => l.status === "failed" || l.status === "blocked"),
      days,
      truncated
    };
  }, [loginsQuery.data, sessionsQuery.data, now]);

  const failures = summary.failed + summary.blocked;
  const dailyTotals = summary.days.map((d) => d.success + d.failed + d.blocked);
  const dailyFailures = summary.days.map((d) => d.failed + d.blocked);

  return (
    <>
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <MetricCard
          label={`近 ${STRIP_DAYS} 天登录`}
          icon={LogIn}
          tone="primary"
          loading={loading}
          value={summary.success.toLocaleString("zh-CN")}
          sub={`成功次数，共 ${summary.attempts} 次尝试`}
          spark={dailyTotals}
        />
        <MetricCard
          label="失败与拦截"
          icon={failures ? ShieldAlert : ShieldCheck}
          tone={summary.blocked ? "danger" : summary.failed ? "warning" : "success"}
          loading={loading}
          value={failures.toLocaleString("zh-CN")}
          sub={failures ? `失败 ${summary.failed}，拦截 ${summary.blocked}` : "没有失败或被拦截的登录"}
          spark={failures ? dailyFailures : undefined}
          onClick={failures ? onShowFailures : undefined}
        />
        <MetricCard
          label="不同 IP"
          icon={Network}
          tone="info"
          loading={loading}
          value={summary.ips.toLocaleString("zh-CN")}
          sub={summary.places ? `${summary.places} 个地点，常用 ${summary.topPlace}` : "未解析出地点"}
        />
        <MetricCard
          label="不同设备"
          icon={MonitorSmartphone}
          tone="violet"
          loading={loading}
          value={summary.devices.toLocaleString("zh-CN")}
          sub={summary.sessions ? `活跃会话 ${summary.sessions} 个，${summary.sessionDevices} 台设备` : "当前没有活跃会话"}
        />
      </div>

      <Section
        title={`近 ${STRIP_DAYS} 天每日登录`}
        description={
          <>
            统计口径：最近 {LOGIN_SAMPLE} 条登录记录中落在近 {STRIP_DAYS} 天内的部分；设备按设备标识区分，无标识时按设备名或 UA
            {summary.truncated ? "。样本未覆盖完整 30 天，实际次数更多" : ""}
          </>
        }
        icon={FileClock}
        tone="primary"
        actions={
          <span className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
            <Legend className="bg-blue-600 dark:bg-blue-500" label="成功" />
            <Legend className="bg-amber-500" label="失败" />
            <Legend className="bg-red-500" label="被拦截" />
          </span>
        }
      >
        {loginsQuery.isError ? (
          <StateBlock compact kind="error" description={errorMessage(loginsQuery.error)} onRetry={() => void loginsQuery.refetch()} />
        ) : (
          <div className="flex flex-col gap-4">
            {loading ? <Skeleton className="h-14 w-full" /> : <DayStrip days={summary.days} />}
            <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
              <RecentFact
                loading={loading}
                icon={ShieldCheck}
                tone="success"
                label="最近成功登录"
                empty="没有成功的登录记录"
                at={summary.lastSuccess?.createdAt}
                detail={summary.lastSuccess ? [loginLocation(summary.lastSuccess), loginTypeLabel(summary.lastSuccess), summary.lastSuccess.loginIp].filter(Boolean).join("，") : ""}
              />
              <RecentFact
                loading={loading}
                icon={ShieldAlert}
                tone={summary.lastFailure?.status === "blocked" ? "danger" : "warning"}
                label="最近失败或拦截"
                empty="没有失败或被拦截的登录"
                at={summary.lastFailure?.createdAt}
                detail={
                  summary.lastFailure
                    ? [LOGIN_STATUS_LABEL[summary.lastFailure.status] ?? summary.lastFailure.status, failureReason(summary.lastFailure), summary.lastFailure.loginIp]
                        .filter(Boolean)
                        .join("，")
                    : ""
                }
              />
            </div>
          </div>
        )}
      </Section>
    </>
  );
}

function RecentFact({
  loading,
  icon,
  tone,
  label,
  empty,
  at,
  detail
}: {
  loading: boolean;
  icon: ComponentType<{ className?: string }>;
  tone: Tone;
  label: string;
  empty: string;
  at?: string;
  detail: string;
}) {
  return (
    <div className="flex min-w-0 items-start gap-3">
      <ToneIcon icon={icon} tone={at ? tone : "neutral"} size="sm" />
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-xs text-muted-foreground">{label}</span>
        {loading ? (
          <Skeleton className="mt-1 h-4 w-40" />
        ) : at ? (
          <>
            <span className="text-sm" title={formatDateTime(at)}>
              {formatRelative(at)}
              <span className="ml-2 text-xs text-muted-foreground tabular-nums">{formatDateTime(at)}</span>
            </span>
            {detail ? <span className="truncate text-xs text-muted-foreground" title={detail}>{detail}</span> : null}
          </>
        ) : (
          <span className="text-sm text-muted-foreground">{empty}</span>
        )}
      </div>
    </div>
  );
}

function Legend({ className, label }: { className: string; label: string }) {
  return (
    <span className="flex items-center gap-1">
      <span className={cn("size-2 rounded-sm", className)} />
      {label}
    </span>
  );
}

/** 每天一根柱：成功在下、失败与拦截叠在上，柱高按这 30 天里最多的一天归一。 */
function DayStrip({ days }: { days: DayBucket[] }) {
  const peak = Math.max(1, ...days.map((d) => d.success + d.failed + d.blocked));
  return (
    <div className="flex h-14 items-end gap-[3px]">
      {days.map((day) => {
        const total = day.success + day.failed + day.blocked;
        return (
          <Tooltip key={day.key}>
            <TooltipTrigger asChild>
              <div className="flex h-full min-w-0 flex-1 cursor-default flex-col justify-end rounded-sm hover:bg-muted/60">
                {total === 0 ? (
                  <div className="h-[3px] rounded-full bg-muted" />
                ) : (
                  <div className="flex flex-col gap-px overflow-hidden rounded-[3px]" style={{ height: `${Math.max(12, (total / peak) * 100)}%` }}>
                    {day.blocked ? <div className="bg-red-500" style={{ flexGrow: day.blocked }} /> : null}
                    {day.failed ? <div className="bg-amber-500" style={{ flexGrow: day.failed }} /> : null}
                    {day.success ? <div className="bg-blue-600 dark:bg-blue-500" style={{ flexGrow: day.success }} /> : null}
                  </div>
                )}
              </div>
            </TooltipTrigger>
            <TooltipContent side="top" className="text-xs">
              <div className="font-medium">{formatWith(day.date, { month: "numeric", day: "numeric", weekday: "short" })}</div>
              {total === 0 ? (
                <div className="text-muted-foreground">没有登录</div>
              ) : (
                <div className="tabular-nums">
                  成功 {day.success}，失败 {day.failed}，拦截 {day.blocked}
                </div>
              )}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}

// ── 登录记录 ──────────────────────────

const LOGIN_CSV: Array<CsvColumn<UserLoginAuditRecord>> = [
  { header: "时间", value: (r) => formatDateTime(r.createdAt) },
  { header: "结果", value: (r) => LOGIN_STATUS_LABEL[r.status] ?? r.status },
  { header: "失败原因", value: (r) => failureReason(r) },
  { header: "方式", value: (r) => loginTypeLabel(r) },
  { header: "渠道", value: (r) => r.provider },
  { header: "IP", value: (r) => r.loginIp },
  { header: "位置", value: (r) => loginLocation(r) },
  { header: "运营商", value: (r) => r.isp },
  { header: "设备", value: (r) => loginDevice(r) },
  { header: "设备标识", value: (r) => r.deviceId },
  { header: "令牌", value: (r) => r.tokenJti },
  { header: "User-Agent", value: (r) => r.userAgent },
  { header: "记录 ID", value: (r) => r.id }
];

function LoginLocationCell({ item }: { item: UserLoginAuditRecord }) {
  const location = loginLocation(item);
  return (
    <span className="inline-flex max-w-[200px] min-w-0 items-center gap-1.5 text-xs">
      {item.countryCode && !item.isPrivate ? (
        <CountryFlag code={item.countryCode} size={14} className="shrink-0 rounded-[2px]" />
      ) : (
        <Globe2 className="size-3.5 shrink-0 text-muted-foreground" />
      )}
      <span className="truncate">{location || "未知地点"}</span>
    </span>
  );
}

function LoginRecords({
  appKey,
  userId,
  status,
  onStatusChange
}: {
  appKey: string;
  userId: number;
  status: string;
  onStatusChange: (status: string) => void;
}) {
  const token = useAdminToken();
  const [keyword, setKeyword] = useState("");
  const [range, setRange] = useState<TimeRange>(ALL_TIME);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [detail, setDetail] = useState<UserLoginAuditRecord | null>(null);
  const [exporting, setExporting] = useState(false);
  const debounced = useDebouncedValue(keyword.trim().toLowerCase());
  // 筛选变化时回到第一页：在 setter 里一起做，避免 effect 级联渲染
  const resetPage = () => setPage(1);

  const filter = useMemo(() => ({ status: status || undefined, keyword: debounced || undefined, ...rangeToParams(range) }), [status, debounced, range]);
  const query = useUserLoginAuditListQuery(appKey, userId, { ...filter, page, limit });
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const filtersActive = Boolean(keyword) || Boolean(status) || range.preset !== "all";

  async function handleExport() {
    setExporting(true);
    try {
      const { rows: list, total: all, truncated } = await collectPages(
        async (p, l) => {
          const data = await listUserLoginAudits(token as string, appKey, userId, { ...filter, page: p, limit: l });
          return { items: data.items ?? [], total: data.total };
        },
        { limit: 100, maxRows: EXPORT_MAX }
      );
      if (truncated) toast.warning(`符合条件的记录共 ${all} 条，只导出最近 ${EXPORT_MAX} 条`);
      if (!list.length) {
        toast.info("没有可导出的记录");
        return;
      }
      downloadCsv(`user-${userId}-logins-${stamp()}`, LOGIN_CSV, list);
      toast.success(`已导出 ${list.length} 条登录记录`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  }

  const columns: Array<Column<UserLoginAuditRecord>> = [
    {
      id: "time",
      header: "时间",
      cell: (r) => (
        <span className="text-xs whitespace-nowrap tabular-nums" title={formatDateTime(r.createdAt)}>
          {formatShortDateTime(r.createdAt)}
        </span>
      )
    },
    {
      id: "status",
      header: "结果",
      cell: (r) => {
        const reason = r.status !== "success" ? failureReason(r) : "";
        return (
          <span className="flex flex-col items-start gap-0.5">
            <Badge variant={statusVariant(r.status)} size="sm">
              {LOGIN_STATUS_LABEL[r.status] ?? r.status}
            </Badge>
            {reason ? <span className="max-w-[140px] truncate text-xs text-muted-foreground">{reason}</span> : null}
          </span>
        );
      }
    },
    { id: "type", header: "方式", cell: (r) => <span className="text-sm whitespace-nowrap">{loginTypeLabel(r)}</span> },
    { id: "location", header: "位置", cell: (r) => <LoginLocationCell item={r} /> },
    {
      id: "ip",
      header: "IP",
      hideBelow: "md",
      cell: (r) => <span className="font-mono text-xs text-muted-foreground">{r.loginIp || "—"}</span>
    },
    {
      id: "device",
      header: "设备",
      hideBelow: "lg",
      cell: (r) => (
        <div className="max-w-[220px] min-w-0" onClick={(event) => event.stopPropagation()}>
          <DeviceInfoChip info={r.deviceInfo} fallback={describeUserAgent(r.userAgent).label} />
        </div>
      )
    }
  ];

  const detailFields: InfoItem[] | undefined = detail
    ? [
        { label: "时间", value: formatDateTime(detail.createdAt), hint: formatRelative(detail.createdAt) },
        { label: "方式", value: loginTypeLabel(detail) },
        { label: "登录类型", value: detail.loginType, mono: true },
        { label: "渠道", value: detail.provider, mono: true },
        { label: "来源 IP", value: detail.loginIp, mono: true, copy: detail.loginIp },
        { label: "归属地", value: loginLocation(detail) },
        { label: "运营商", value: detail.isp },
        { label: "设备", value: loginDevice(detail) },
        { label: "设备标识", value: detail.deviceId, mono: true, copy: detail.deviceId, wide: true },
        { label: "令牌", value: detail.tokenJti, mono: true, copy: detail.tokenJti, wide: true },
        { label: "User-Agent", value: detail.userAgent, mono: true, copy: detail.userAgent, wide: true }
      ]
    : undefined;

  return (
    <>
      <Section
        title="登录记录"
        description="每一次登录尝试，含成功、失败、被拦截与需二次验证"
        icon={LogIn}
        tone="primary"
        flush
        actions={
          <>
            <Button size="sm" variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>
              <RefreshCw className={query.isFetching ? "animate-spin" : undefined} />
              刷新
            </Button>
            <Button size="sm" variant="outline" disabled={exporting || !total} onClick={() => void handleExport()}>
              {exporting ? <Loader2 className="animate-spin" /> : <Download />}
              导出 CSV
            </Button>
          </>
        }
        toolbar={
          <FilterBar>
            <SearchField
              value={keyword}
              onChange={(value) => {
                setKeyword(value);
                resetPage();
              }}
              placeholder="IP、设备标识、UA、登录方式"
            />
            <FilterSelect
              label="结果"
              value={status}
              options={LOGIN_STATUS_OPTIONS}
              onChange={(value) => {
                onStatusChange(value);
                resetPage();
              }}
            />
            <TimeRangePicker
              value={range}
              onChange={(value) => {
                setRange(value);
                resetPage();
              }}
            />
            <ResetButton
              visible={filtersActive}
              onClick={() => {
                setKeyword("");
                setRange(ALL_TIME);
                onStatusChange("");
                resetPage();
              }}
            />
          </FilterBar>
        }
        footer={
          <Pager
            page={page}
            limit={limit}
            total={total}
            onPage={setPage}
            onLimit={(next) => {
              setLimit(next);
              resetPage();
            }}
          />
        }
      >
        <RecordTable
          columns={columns}
          rows={rows}
          rowKey={(r) => r.id}
          loading={query.isLoading}
          fetching={query.isFetching}
          error={query.error}
          onRetry={() => void query.refetch()}
          onRowClick={setDetail}
          minWidth={760}
          empty={
            <StateBlock
              icon={LogIn}
              title={filtersActive ? "没有符合条件的登录" : "暂无登录记录"}
              description={filtersActive ? "调整筛选条件后再试" : undefined}
            />
          }
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => !open && setDetail(null)}
        title={detail ? loginTypeLabel(detail) : ""}
        description={detail ? `记录 #${detail.id}` : undefined}
        hero={
          detail ? (
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={statusVariant(detail.status)}>{LOGIN_STATUS_LABEL[detail.status] ?? detail.status}</Badge>
                {failureReason(detail) ? <span className="text-sm text-muted-foreground">{failureReason(detail)}</span> : null}
              </div>
              {detail.deviceInfo ? (
                <div className="overflow-hidden rounded-xl border">
                  <DeviceInfoCard info={detail.deviceInfo} />
                </div>
              ) : null}
            </div>
          ) : undefined
        }
        fields={detailFields}
        raw={detail}
      />
    </>
  );
}

// ── 会话事件 ──────────────────────────

type EventMeta = { icon: ComponentType<{ className?: string }>; tone: Tone };

const EVENT_META: Record<string, EventMeta> = {
  issued: { icon: KeyRound, tone: "success" },
  refreshed: { icon: RefreshCw, tone: "info" },
  logout: { icon: LogOut, tone: "neutral" },
  expired: { icon: TimerOff, tone: "neutral" },
  replaced: { icon: RefreshCw, tone: "neutral" },
  password_changed: { icon: KeySquare, tone: "warning" },
  password_reset: { icon: KeySquare, tone: "warning" }
};

function eventMeta(type: string): EventMeta {
  return EVENT_META[type] ?? (type.startsWith("revoked") ? { icon: ShieldX, tone: "danger" } : { icon: FileClock, tone: "neutral" });
}

/** 元数据里值得直接显示的几项 */
const EVENT_HIGHLIGHTS: Array<{ key: string; label: string; mono?: boolean }> = [
  { key: "ip", label: "IP", mono: true },
  { key: "provider", label: "方式" },
  { key: "device", label: "设备" },
  { key: "reason", label: "原因" },
  { key: "operator", label: "操作人" }
];

function metaText(event: UserSessionAuditRecord, key: string) {
  const value = event.metadata?.[key];
  if (typeof value === "string") return value.trim();
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function eventLabel(type: string) {
  return SESSION_EVENT_LABEL[type] ?? type;
}

const EVENT_CSV: Array<CsvColumn<UserSessionAuditRecord>> = [
  { header: "时间", value: (r) => formatDateTime(r.createdAt) },
  { header: "事件", value: (r) => eventLabel(r.eventType) },
  { header: "事件类型", value: (r) => r.eventType },
  { header: "令牌", value: (r) => r.tokenJti },
  ...EVENT_HIGHLIGHTS.map(({ key, label }) => ({ header: label, value: (r: UserSessionAuditRecord) => metaText(r, key) })),
  {
    header: "元数据",
    value: (r) => {
      try {
        return r.metadata && Object.keys(r.metadata).length ? JSON.stringify(r.metadata) : "";
      } catch {
        return "";
      }
    }
  },
  { header: "记录 ID", value: (r) => r.id }
];

function SessionEvents({ appKey, userId }: { appKey: string; userId: number }) {
  const token = useAdminToken();
  const [keyword, setKeyword] = useState("");
  const [eventType, setEventType] = useState("");
  const [range, setRange] = useState<TimeRange>(ALL_TIME);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [detail, setDetail] = useState<UserSessionAuditRecord | null>(null);
  const [exporting, setExporting] = useState(false);
  const debounced = useDebouncedValue(keyword.trim().toLowerCase());
  const filter = useMemo(() => ({ eventType: eventType || undefined, keyword: debounced || undefined, ...rangeToParams(range) }), [eventType, debounced, range]);
  const query = useUserSessionAuditListQuery(appKey, userId, { ...filter, page, limit });
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? 0;
  const filtersActive = Boolean(keyword) || Boolean(eventType) || range.preset !== "all";

  async function handleExport() {
    setExporting(true);
    try {
      const { rows: list, total: all, truncated } = await collectPages(
        async (p, l) => {
          const data = await listUserSessionAudits(token as string, appKey, userId, { ...filter, page: p, limit: l });
          return { items: data.items ?? [], total: data.total };
        },
        { limit: 100, maxRows: EXPORT_MAX }
      );
      if (truncated) toast.warning(`符合条件的事件共 ${all} 条，只导出最近 ${EXPORT_MAX} 条`);
      if (!list.length) {
        toast.info("没有可导出的事件");
        return;
      }
      downloadCsv(`user-${userId}-session-events-${stamp()}`, EVENT_CSV, list);
      toast.success(`已导出 ${list.length} 条会话事件`);
    } catch (error) {
      toast.error(errorMessage(error, "导出失败"));
    } finally {
      setExporting(false);
    }
  }

  const columns: Array<Column<UserSessionAuditRecord>> = [
    {
      id: "time",
      header: "时间",
      cell: (r) => (
        <span className="text-xs whitespace-nowrap tabular-nums" title={formatDateTime(r.createdAt)}>
          {formatShortDateTime(r.createdAt)}
        </span>
      )
    },
    {
      id: "event",
      header: "事件",
      cell: (r) => {
        const meta = eventMeta(r.eventType);
        return (
          <span className="inline-flex items-center gap-2 whitespace-nowrap">
            <ToneIcon icon={meta.icon} tone={meta.tone} size="sm" />
            <span className="text-sm">{eventLabel(r.eventType)}</span>
          </span>
        );
      }
    },
    {
      id: "token",
      header: "令牌",
      hideBelow: "sm",
      cell: (r) =>
        r.tokenJti ? (
          <span className="font-mono text-xs text-muted-foreground" title={r.tokenJti}>
            {r.tokenJti.slice(0, 12)}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        )
    },
    {
      id: "ip",
      header: "IP",
      hideBelow: "md",
      cell: (r) => <span className="font-mono text-xs text-muted-foreground">{metaText(r, "ip") || "—"}</span>
    },
    {
      id: "info",
      header: "说明",
      hideBelow: "lg",
      cell: (r) => {
        const parts = EVENT_HIGHLIGHTS.filter((h) => h.key !== "ip")
          .map((h) => {
            const value = metaText(r, h.key);
            return value ? `${h.label} ${value}` : "";
          })
          .filter(Boolean);
        return parts.length ? (
          <span className="block max-w-[320px] truncate text-xs text-muted-foreground" title={parts.join("，")}>
            {parts.join("，")}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        );
      }
    }
  ];

  const detailMeta = detail ? eventMeta(detail.eventType) : null;

  return (
    <>
      <Section
        title="会话事件"
        description="令牌的签发、刷新、下线与过期轨迹"
        icon={FileClock}
        tone="info"
        flush
        actions={
          <>
            <Button size="sm" variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>
              <RefreshCw className={query.isFetching ? "animate-spin" : undefined} />
              刷新
            </Button>
            <Button size="sm" variant="outline" disabled={exporting || !total} onClick={() => void handleExport()}>
              {exporting ? <Loader2 className="animate-spin" /> : <Download />}
              导出 CSV
            </Button>
          </>
        }
        toolbar={
          <FilterBar>
            <SearchField
              value={keyword}
              onChange={(value) => {
                setKeyword(value);
                setPage(1);
              }}
              placeholder="令牌 ID、IP、原因等"
            />
            <FilterSelect
              label="事件"
              value={eventType}
              options={EVENT_OPTIONS}
              allLabel="全部事件"
              onChange={(value) => {
                setEventType(value);
                setPage(1);
              }}
            />
            <TimeRangePicker
              value={range}
              onChange={(value) => {
                setRange(value);
                setPage(1);
              }}
            />
            <ResetButton
              visible={filtersActive}
              onClick={() => {
                setKeyword("");
                setEventType("");
                setRange(ALL_TIME);
                setPage(1);
              }}
            />
          </FilterBar>
        }
        footer={
          <Pager
            page={page}
            limit={limit}
            total={total}
            onPage={setPage}
            onLimit={(next) => {
              setLimit(next);
              setPage(1);
            }}
          />
        }
      >
        <RecordTable
          columns={columns}
          rows={rows}
          rowKey={(r) => r.id}
          loading={query.isLoading}
          fetching={query.isFetching}
          error={query.error}
          onRetry={() => void query.refetch()}
          onRowClick={setDetail}
          minWidth={680}
          empty={
            <StateBlock
              icon={FileClock}
              title={filtersActive ? "没有符合条件的事件" : "暂无会话事件"}
              description={filtersActive ? "调整筛选条件后再试" : undefined}
            />
          }
        />
      </Section>

      <RecordSheet
        open={Boolean(detail)}
        onOpenChange={(open) => !open && setDetail(null)}
        title={detail ? eventLabel(detail.eventType) : ""}
        description={detail ? `记录 #${detail.id}` : undefined}
        hero={
          detail && detailMeta ? (
            <div className="flex items-center gap-3 rounded-xl border px-4 py-3">
              <ToneIcon icon={detailMeta.icon} tone={detailMeta.tone} size="lg" />
              <div className="flex min-w-0 flex-col">
                <span className="text-sm font-medium">{eventLabel(detail.eventType)}</span>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {formatDateTime(detail.createdAt)}，{formatRelative(detail.createdAt)}
                </span>
              </div>
            </div>
          ) : undefined
        }
        fields={
          detail
            ? [
                { label: "事件类型", value: detail.eventType, mono: true },
                { label: "时间", value: formatDateTime(detail.createdAt) },
                { label: "令牌", value: detail.tokenJti, mono: true, copy: detail.tokenJti, wide: true },
                ...EVENT_HIGHLIGHTS.map(({ key, label, mono }) => ({
                  label,
                  value: metaText(detail, key) || undefined,
                  mono,
                  copy: mono ? metaText(detail, key) || undefined : undefined
                }))
              ]
            : undefined
        }
        raw={detail}
      />
    </>
  );
}

// ── 风控评估 ──────────────────────────

