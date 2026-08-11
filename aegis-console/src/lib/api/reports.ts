import { apiRequest, buildQuery } from "./client";

type TimeSeriesPoint = { date: string; count: number; extra?: Record<string, unknown> };
type DimensionPoint = { label: string; count: number; percentage: number };
type FunnelStep = { step: string; count: number; rate: number };
type RetentionRow = { cohortDate: string; cohortSize: number; retained: number[]; rates: number[] };

export type RegistrationReport = { total: number; series: TimeSeriesPoint[] };
export type LoginReport = { totalSuccess: number; totalFailure: number; series: TimeSeriesPoint[] };
export type RetentionReport = { days: number[]; rows: RetentionRow[] };
export type ActiveReport = { dau: TimeSeriesPoint[]; wau?: TimeSeriesPoint[]; mau?: TimeSeriesPoint[] };
export type DeviceReport = { os: DimensionPoint[]; browser: DimensionPoint[]; platform: DimensionPoint[] };
export type RegionReport = { topIPs: DimensionPoint[] };
export type ChannelReport = { channels: DimensionPoint[] };
export type PaymentReport = { totalAmount: number; totalOrders: number; series: TimeSeriesPoint[]; methods: DimensionPoint[] };
export type NotificationReport = { totalSent: number; totalRead: number; series: TimeSeriesPoint[]; types: DimensionPoint[] };
export type RiskReport = { totalBlocked: number; series: TimeSeriesPoint[]; severity: DimensionPoint[]; topRules: DimensionPoint[] };
export type ActivityReport = { totalParticipants: number; totalDraws: number; winRate: number; prizes: DimensionPoint[] };
export type FunnelReport = { steps: FunnelStep[] };

type Params = { start: string; end: string; [k: string]: string | number | boolean | undefined };

const base = (appKey: string) => `/api/admin/apps/${appKey}/reports`;

export const getRegistrationReport = (token: string, appKey: string, p: Params) =>
  apiRequest<RegistrationReport>(`${base(appKey)}/registration${buildQuery(p)}`, { token });

export const getLoginReport = (token: string, appKey: string, p: Params) =>
  apiRequest<LoginReport>(`${base(appKey)}/login${buildQuery(p)}`, { token });

export const getRetentionReport = (token: string, appKey: string, p: Params) =>
  apiRequest<RetentionReport>(`${base(appKey)}/retention${buildQuery(p)}`, { token });

export const getActiveReport = (token: string, appKey: string, p: Params) =>
  apiRequest<ActiveReport>(`${base(appKey)}/active${buildQuery(p)}`, { token });

export const getDeviceReport = (token: string, appKey: string, p: Params) =>
  apiRequest<DeviceReport>(`${base(appKey)}/device${buildQuery(p)}`, { token });

export const getRegionReport = (token: string, appKey: string, p: Params) =>
  apiRequest<RegionReport>(`${base(appKey)}/region${buildQuery(p)}`, { token });

export const getChannelReport = (token: string, appKey: string, p: Params) =>
  apiRequest<ChannelReport>(`${base(appKey)}/channel${buildQuery(p)}`, { token });

export const getPaymentReport = (token: string, appKey: string, p: Params) =>
  apiRequest<PaymentReport>(`${base(appKey)}/payment${buildQuery(p)}`, { token });

export const getNotificationReport = (token: string, appKey: string, p: Params) =>
  apiRequest<NotificationReport>(`${base(appKey)}/notification${buildQuery(p)}`, { token });

export const getRiskReport = (token: string, appKey: string, p: Params) =>
  apiRequest<RiskReport>(`${base(appKey)}/risk${buildQuery(p)}`, { token });

export const getActivityReport = (token: string, appKey: string, p: Params) =>
  apiRequest<ActivityReport>(`${base(appKey)}/activity${buildQuery(p)}`, { token });

export const getFunnelReport = (token: string, appKey: string, p: Params) =>
  apiRequest<FunnelReport>(`${base(appKey)}/funnel${buildQuery(p)}`, { token });

export function getReportExportUrl(appKey: string, type: string, start: string, end: string) {
  return `${base(appKey)}/export?type=${type}&start=${encodeURIComponent(start)}&end=${encodeURIComponent(end)}&format=csv`;
}
