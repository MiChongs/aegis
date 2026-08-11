"use client";

import { Suspense, useEffect, useMemo, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  Activity, BarChart3, Bell, CreditCard, Download, Filter, Globe2,
  Laptop, LogIn, Monitor, Repeat, ShieldAlert, Smartphone, Trophy, UserPlus, Users
} from "lucide-react";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Cell,
  Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis
} from "recharts";
import { SectionHeading } from "@/components/ui/section-heading";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/data-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  useAdminAppsQuery,
  useRegistrationReportQuery, useLoginReportQuery, useRetentionReportQuery,
  useActiveReportQuery, useDeviceReportQuery, useRegionReportQuery, useChannelReportQuery,
  usePaymentReportQuery, useNotificationReportQuery, useRiskReportQuery,
  useActivityReportQuery, useFunnelReportQuery,
} from "@/lib/admin-hooks";
import { getReportExportUrl } from "@/lib/api/reports";
import { useAuthStore } from "@/lib/auth-store";
import { cn } from "@/lib/utils";

const COLORS = ["#6366f1", "#06b6d4", "#f59e0b", "#ef4444", "#22c55e", "#8b5cf6", "#ec4899", "#14b8a6"];
const fmt = (n: number) => new Intl.NumberFormat("zh-CN").format(n);
const fmtAmt = (n: number) => `¥${(n / 100).toFixed(2)}`;

function daysAgo(d: number): string {
  const dt = new Date();
  dt.setDate(dt.getDate() - d);
  return dt.toISOString();
}

function ReportsPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = searchParams.get("tab") || "registration";
  const appsQuery = useAdminAppsQuery();
  const apps = appsQuery.data || [];
  const [appKey, setAppKey] = useState("");
  const [range, setRange] = useState("30");
  const [customStart, setCustomStart] = useState("");
  const [customEnd, setCustomEnd] = useState("");

  useEffect(() => { if (apps.length > 0 && !appKey) setAppKey(apps[0].appKey || ""); }, [apps, appKey]);

  const start = range === "custom" ? (customStart ? new Date(customStart).toISOString() : "") : daysAgo(parseInt(range));
  const end = range === "custom" ? (customEnd ? new Date(customEnd).toISOString() : "") : new Date().toISOString();
  const ready = Boolean(appKey && start && end);

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="报表分析中心" />

      {/* 全局筛选栏 */}
      <div className="flex items-center gap-3 flex-wrap">
        <Select value={appKey} onValueChange={setAppKey}>
          <SelectTrigger className="h-8 w-44 text-xs"><SelectValue placeholder="选择应用" /></SelectTrigger>
          <SelectContent>{apps.map((a) => <SelectItem key={a.appKey} value={a.appKey || String(a.id)}>{a.name}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={range} onValueChange={setRange}>
          <SelectTrigger className="h-8 w-28 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="7">近 7 天</SelectItem>
            <SelectItem value="30">近 30 天</SelectItem>
            <SelectItem value="90">近 90 天</SelectItem>
            <SelectItem value="custom">自定义</SelectItem>
          </SelectContent>
        </Select>
        {range === "custom" && (<>
          <Input type="date" value={customStart} onChange={(e) => setCustomStart(e.target.value)} className="h-8 w-36 text-xs" />
          <span className="text-xs text-muted-foreground">至</span>
          <Input type="date" value={customEnd} onChange={(e) => setCustomEnd(e.target.value)} className="h-8 w-36 text-xs" />
        </>)}
      </div>

      <Tabs value={tab} onValueChange={(v) => router.replace(`/reports?tab=${v}`, { scroll: false })}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="registration"><UserPlus className="size-3.5" />注册</TabsTrigger>
          <TabsTrigger value="login"><LogIn className="size-3.5" />登录</TabsTrigger>
          <TabsTrigger value="retention"><Repeat className="size-3.5" />留存</TabsTrigger>
          <TabsTrigger value="active"><Activity className="size-3.5" />活跃</TabsTrigger>
          <TabsTrigger value="device"><Smartphone className="size-3.5" />设备</TabsTrigger>
          <TabsTrigger value="region"><Globe2 className="size-3.5" />地区</TabsTrigger>
          <TabsTrigger value="channel"><Monitor className="size-3.5" />渠道</TabsTrigger>
          <TabsTrigger value="payment"><CreditCard className="size-3.5" />支付</TabsTrigger>
          <TabsTrigger value="notification"><Bell className="size-3.5" />通知</TabsTrigger>
          <TabsTrigger value="risk"><ShieldAlert className="size-3.5" />风控</TabsTrigger>
          <TabsTrigger value="activity"><Trophy className="size-3.5" />活动</TabsTrigger>
          <TabsTrigger value="funnel"><Filter className="size-3.5" />漏斗</TabsTrigger>
          <TabsTrigger value="export"><Download className="size-3.5" />导出</TabsTrigger>
        </TabsList>

        <div className="mt-4">
          {!ready ? <EmptyState title="请选择应用和日期范围" description="" /> : (<>
            <TabsContent value="registration"><RegistrationTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="login"><LoginTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="retention"><RetentionTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="active"><ActiveTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="device"><DeviceTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="region"><RegionTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="channel"><ChannelTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="payment"><PaymentTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="notification"><NotificationTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="risk"><RiskTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="activity"><ActivityTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="funnel"><FunnelTab appKey={appKey} start={start} end={end} /></TabsContent>
            <TabsContent value="export"><ExportTab appKey={appKey} start={start} end={end} /></TabsContent>
          </>)}
        </div>
      </Tabs>
    </div>
  );
}

export default function ReportsPage() {
  return <Suspense><ReportsPageInner /></Suspense>;
}

type TP = { appKey: string; start: string; end: string };

/* ── 注册报表 ── */
function RegistrationTab({ appKey, start, end }: TP) {
  const q = useRegistrationReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <div className="space-y-4">
      <StatCards items={[{ label: "注册总数", value: fmt(d.total) }]} />
      <ChartCard title="日注册趋势">
        <ResponsiveContainer width="100%" height={280}>
          <AreaChart data={d.series}><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis dataKey="date" tick={{ fontSize: 10 }} /><YAxis tick={{ fontSize: 10 }} /><Tooltip /><Area type="monotone" dataKey="count" stroke="#6366f1" fill="#6366f1" fillOpacity={0.15} /></AreaChart>
        </ResponsiveContainer>
      </ChartCard>
    </div>
  );
}

/* ── 登录报表 ── */
function LoginTab({ appKey, start, end }: TP) {
  const q = useLoginReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  const series = d.series.map((p) => ({ date: p.date, success: Number(p.extra?.success || 0), failure: Number(p.extra?.failure || 0) }));
  return (
    <div className="space-y-4">
      <StatCards items={[{ label: "成功登录", value: fmt(d.totalSuccess) }, { label: "失败登录", value: fmt(d.totalFailure) }]} />
      <ChartCard title="日登录趋势">
        <ResponsiveContainer width="100%" height={280}>
          <AreaChart data={series}><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis dataKey="date" tick={{ fontSize: 10 }} /><YAxis tick={{ fontSize: 10 }} /><Tooltip />
            <Area type="monotone" dataKey="success" stroke="#22c55e" fill="#22c55e" fillOpacity={0.15} />
            <Area type="monotone" dataKey="failure" stroke="#ef4444" fill="#ef4444" fillOpacity={0.1} />
          </AreaChart>
        </ResponsiveContainer>
      </ChartCard>
    </div>
  );
}

/* ── 留存报表 ── */
function RetentionTab({ appKey, start, end }: TP) {
  const q = useRetentionReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  const days = d.days || [1, 3, 7, 14, 30];
  return (
    <ChartCard title="留存矩阵">
      {d.rows.length === 0 ? <p className="text-sm text-muted-foreground text-center py-8">暂无留存数据</p> : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead><tr className="border-b"><th className="text-left py-1.5 px-2">注册日期</th><th className="text-right py-1.5 px-2">注册数</th>
              {days.map((d) => <th key={d} className="text-right py-1.5 px-2">Day{d}</th>)}
            </tr></thead>
            <tbody>{d.rows.map((row) => (
              <tr key={row.cohortDate} className="border-b"><td className="py-1.5 px-2 font-mono">{row.cohortDate}</td><td className="text-right py-1.5 px-2">{row.cohortSize}</td>
                {(row.rates || []).map((r, i) => <td key={i} className="text-right py-1.5 px-2" style={{ backgroundColor: `rgba(99,102,241,${Math.min(r / 100, 1) * 0.4})` }}>{r.toFixed(1)}%</td>)}
              </tr>
            ))}</tbody>
          </table>
        </div>
      )}
    </ChartCard>
  );
}

/* ── 活跃报表 ── */
function ActiveTab({ appKey, start, end }: TP) {
  const q = useActiveReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <ChartCard title="日活跃用户（DAU）">
      <ResponsiveContainer width="100%" height={280}>
        <AreaChart data={d.dau}><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis dataKey="date" tick={{ fontSize: 10 }} /><YAxis tick={{ fontSize: 10 }} /><Tooltip /><Area type="monotone" dataKey="count" stroke="#06b6d4" fill="#06b6d4" fillOpacity={0.15} /></AreaChart>
      </ResponsiveContainer>
    </ChartCard>
  );
}

/* ── 设备报表 ── */
function DeviceTab({ appKey, start, end }: TP) {
  const q = useDeviceReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <PieCard title="操作系统" data={d.os} />
      <PieCard title="浏览器" data={d.browser} />
      <PieCard title="平台" data={d.platform} />
    </div>
  );
}

/* ── 地区报表 ── */
function RegionTab({ appKey, start, end }: TP) {
  const q = useRegionReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <ChartCard title="登录 IP 分布 TOP 20">
      <ResponsiveContainer width="100%" height={Math.max(280, (d.topIPs?.length || 0) * 28)}>
        <BarChart data={(d.topIPs || []).slice(0, 20)} layout="vertical"><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis type="number" tick={{ fontSize: 10 }} /><YAxis type="category" dataKey="label" width={120} tick={{ fontSize: 9 }} /><Tooltip /><Bar dataKey="count" fill="#6366f1" radius={[0, 4, 4, 0]} /></BarChart>
      </ResponsiveContainer>
    </ChartCard>
  );
}

/* ── 渠道报表 ── */
function ChannelTab({ appKey, start, end }: TP) {
  const q = useChannelReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return <PieCard title="登录渠道分布" data={d.channels || []} large />;
}

/* ── 支付报表 ── */
function PaymentTab({ appKey, start, end }: TP) {
  const q = usePaymentReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  const series = d.series.map((p) => ({ date: p.date, amount: Number(p.extra?.amount || 0) / 100, orders: Number(p.extra?.orders || 0) }));
  return (
    <div className="space-y-4">
      <StatCards items={[{ label: "总营收", value: fmtAmt(d.totalAmount) }, { label: "总订单", value: fmt(d.totalOrders) }]} />
      <ChartCard title="日营收趋势">
        <ResponsiveContainer width="100%" height={280}>
          <AreaChart data={series}><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis dataKey="date" tick={{ fontSize: 10 }} /><YAxis tick={{ fontSize: 10 }} /><Tooltip /><Area type="monotone" dataKey="amount" stroke="#f59e0b" fill="#f59e0b" fillOpacity={0.15} /></AreaChart>
        </ResponsiveContainer>
      </ChartCard>
      <PieCard title="支付方式分布" data={d.methods} />
    </div>
  );
}

/* ── 通知报表 ── */
function NotificationTab({ appKey, start, end }: TP) {
  const q = useNotificationReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <div className="space-y-4">
      <StatCards items={[{ label: "总发送", value: fmt(d.totalSent) }, { label: "已读", value: fmt(d.totalRead) }, { label: "已读率", value: d.totalSent ? `${((d.totalRead / d.totalSent) * 100).toFixed(1)}%` : "0%" }]} />
      <ChartCard title="日发送趋势">
        <ResponsiveContainer width="100%" height={280}>
          <AreaChart data={d.series}><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis dataKey="date" tick={{ fontSize: 10 }} /><YAxis tick={{ fontSize: 10 }} /><Tooltip /><Area type="monotone" dataKey="count" stroke="#8b5cf6" fill="#8b5cf6" fillOpacity={0.15} /></AreaChart>
        </ResponsiveContainer>
      </ChartCard>
      <PieCard title="通知类型分布" data={d.types} />
    </div>
  );
}

/* ── 风控报表 ── */
function RiskTab({ appKey, start, end }: TP) {
  const q = useRiskReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <div className="space-y-4">
      <StatCards items={[{ label: "总拦截", value: fmt(d.totalBlocked) }]} />
      <ChartCard title="日拦截趋势">
        <ResponsiveContainer width="100%" height={280}>
          <AreaChart data={d.series}><CartesianGrid strokeDasharray="3 3" opacity={0.15} /><XAxis dataKey="date" tick={{ fontSize: 10 }} /><YAxis tick={{ fontSize: 10 }} /><Tooltip /><Area type="monotone" dataKey="count" stroke="#ef4444" fill="#ef4444" fillOpacity={0.15} /></AreaChart>
        </ResponsiveContainer>
      </ChartCard>
      <div className="grid gap-4 lg:grid-cols-2">
        <PieCard title="严重等级分布" data={d.severity} />
        <ChartCard title="TOP 拦截规则">
          <div className="space-y-1.5">{(d.topRules || []).slice(0, 10).map((r, i) => (
            <div key={i} className="flex items-center gap-2 text-xs"><span className="w-6 text-right text-muted-foreground">{i + 1}</span><span className="flex-1 truncate font-mono">{r.label}</span><span className="font-semibold">{r.count}</span></div>
          ))}</div>
        </ChartCard>
      </div>
    </div>
  );
}

/* ── 活动报表 ── */
function ActivityTab({ appKey, start, end }: TP) {
  const q = useActivityReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  return (
    <div className="space-y-4">
      <StatCards items={[{ label: "参与人数", value: fmt(d.totalParticipants) }, { label: "抽奖次数", value: fmt(d.totalDraws) }, { label: "中奖率", value: `${d.winRate.toFixed(1)}%` }]} />
      <PieCard title="奖品消耗分布" data={d.prizes} />
    </div>
  );
}

/* ── 转化漏斗 ── */
function FunnelTab({ appKey, start, end }: TP) {
  const q = useFunnelReportQuery(appKey, start, end);
  const d = q.data;
  if (!d) return <Loading />;
  const steps = d.steps || [];
  const maxCount = steps[0]?.count || 1;
  return (
    <ChartCard title="用户转化漏斗">
      <div className="space-y-3 py-4">
        {steps.map((s, i) => (
          <div key={s.step} className="flex items-center gap-3">
            <span className="w-20 text-xs text-right text-muted-foreground shrink-0">{s.step}</span>
            <div className="flex-1 h-10 rounded-lg bg-muted/30 overflow-hidden relative">
              <div className="h-full rounded-lg transition-all" style={{ width: `${(s.count / maxCount) * 100}%`, backgroundColor: COLORS[i % COLORS.length] + "40" }} />
              <div className="absolute inset-0 flex items-center px-3 text-xs font-semibold">{fmt(s.count)}</div>
            </div>
            <span className="w-16 text-xs text-right font-mono shrink-0">{s.rate.toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </ChartCard>
  );
}

/* ── 自定义导出 ── */
function ExportTab({ appKey, start, end }: TP) {
  const token = useAuthStore((s) => s.accessToken);
  const types = [
    { value: "registration", label: "注册报表" }, { value: "login", label: "登录报表" },
    { value: "active", label: "活跃报表" }, { value: "payment", label: "支付报表" },
    { value: "notification", label: "通知报表" }, { value: "risk", label: "风控报表" },
    { value: "channel", label: "渠道报表" }, { value: "region", label: "地区报表" },
    { value: "device", label: "设备报表" }, { value: "funnel", label: "转化漏斗" },
  ];
  const [exportType, setExportType] = useState("registration");

  const handleExport = () => {
    const url = getReportExportUrl(appKey, exportType, start, end);
    window.open(`${url}&token=${token}`, "_blank");
  };

  return (
    <Card><CardContent className="p-6 space-y-4">
      <h3 className="text-sm font-semibold flex items-center gap-2"><Download className="size-4" />自定义筛选导出</h3>
      <div className="flex items-end gap-3 flex-wrap">
        <div className="space-y-1.5"><Label className="text-xs">报表类型</Label>
          <Select value={exportType} onValueChange={setExportType}><SelectTrigger className="h-8 w-40 text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>{types.map((t) => <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <Button size="sm" onClick={handleExport}><Download className="size-3.5" />导出 CSV</Button>
      </div>
      <p className="text-xs text-muted-foreground">导出将使用当前选择的应用和日期范围。</p>
    </CardContent></Card>
  );
}

/* ── 共享组件 ── */

function Loading() { return <div className="py-16 text-center text-sm text-muted-foreground">加载报表数据...</div>; }

function StatCards({ items }: { items: Array<{ label: string; value: string }> }) {
  return (
    <div className={cn("grid gap-3", items.length <= 2 ? "grid-cols-2" : "grid-cols-2 lg:grid-cols-" + Math.min(items.length, 4))}>
      {items.map((i) => (
        <div key={i.label} className="rounded-xl border bg-card px-4 py-3">
          <div className="text-[10px] font-medium uppercase tracking-widest text-muted-foreground">{i.label}</div>
          <div className="text-xl font-semibold font-mono mt-0.5">{i.value}</div>
        </div>
      ))}
    </div>
  );
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card><CardContent className="p-4 space-y-3">
      <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-widest">{title}</h4>
      {children}
    </CardContent></Card>
  );
}

function PieCard({ title, data, large }: { title: string; data: Array<{ label: string; count: number; percentage: number }>; large?: boolean }) {
  return (
    <Card><CardContent className="p-4 space-y-3">
      <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-widest">{title}</h4>
      {data.length === 0 ? <p className="text-xs text-muted-foreground text-center py-6">暂无数据</p> : (
        <div className={cn("flex items-center gap-4", large ? "flex-col lg:flex-row" : "")}>
          <ResponsiveContainer width={large ? 200 : 140} height={large ? 200 : 140}>
            <PieChart><Pie data={data} dataKey="count" nameKey="label" cx="50%" cy="50%" innerRadius={large ? 50 : 35} outerRadius={large ? 80 : 55} paddingAngle={2}>
              {data.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
            </Pie><Tooltip formatter={(v) => fmt(Number(v))} /></PieChart>
          </ResponsiveContainer>
          <div className="flex-1 space-y-1">
            {data.map((d, i) => (
              <div key={d.label} className="flex items-center gap-2 text-xs">
                <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: COLORS[i % COLORS.length] }} />
                <span className="truncate flex-1">{d.label}</span>
                <span className="font-mono text-muted-foreground">{d.percentage.toFixed(1)}%</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </CardContent></Card>
  );
}
