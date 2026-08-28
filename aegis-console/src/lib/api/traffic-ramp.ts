import { apiRequest, buildQuery } from "./client";

/**
 * 突发流量爬坡管理端 API。
 *
 * 类型对齐后端 `systemdomain.TrafficRampSettingsView` / `TrafficRampSettingsPatch` /
 * `TrafficRampStats`。配置逐字段 patch（与防火墙一致，全是标量开关）；
 * 统计是运行期内存态，读取不打库，适合短间隔轮询。
 */

export type TrafficRampState = "stable" | "ramping" | "saturated" | "cooldown";

export interface TrafficRampSettings {
  enabled: boolean;
  baselineRps: number;
  maxRps: number;
  rampStepPct: number;
  rampIntervalMs: number;
  cooldownSeconds: number;
  queueSize: number;
  queueTimeoutMs: number;
  maxConcurrent: number;
  exemptPathPrefixes: string[] | null;
  exemptAdmin: boolean;
  retryAfterSeconds: number;
  source: string;
  reloadVersion: number;
  reloadedAt: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
}

export interface TrafficRampSettingsPatch {
  enabled?: boolean;
  baselineRps?: number;
  maxRps?: number;
  rampStepPct?: number;
  rampIntervalMs?: number;
  cooldownSeconds?: number;
  queueSize?: number;
  queueTimeoutMs?: number;
  maxConcurrent?: number;
  exemptPathPrefixes?: string[];
  exemptAdmin?: boolean;
  retryAfterSeconds?: number;
}

export interface TrafficRampSeriesPoint {
  ts: number; // unix 秒
  arrivals: number;
  admitted: number;
  queued: number;
  rejected: number;
  limit: number;
}

export interface TrafficRampStats {
  enabled: boolean;
  state: TrafficRampState;
  currentLimit: number;
  baselineRps: number;
  maxRps: number;
  inflight: number;
  queueDepth: number;
  queueCapacity: number;
  totalArrivals: number;
  totalAdmitted: number;
  totalQueuedAdmitted: number;
  totalRejectedRate: number;
  totalRejectedTimeout: number;
  totalRejectedLoad: number;
  totalExempt: number;
  rampEvents: number;
  peakArrivalRps: number;
  peakLimit: number;
  lastBurstAt?: string | null;
  statsSince: string;
  series: TrafficRampSeriesPoint[];
}

const BASE = "/api/admin/system/traffic-ramp";

export function getTrafficRampSettings(token: string) {
  return apiRequest<TrafficRampSettings>(BASE, { token });
}

export function updateTrafficRampSettings(token: string, payload: TrafficRampSettingsPatch) {
  return apiRequest<TrafficRampSettings>(BASE, { token, method: "PUT", body: JSON.stringify(payload) });
}

export function getTrafficRampStats(token: string, seconds: number) {
  return apiRequest<TrafficRampStats>(`${BASE}/stats${buildQuery({ seconds })}`, { token });
}

export function resetTrafficRampStats(token: string) {
  return apiRequest<{ ok: boolean }>(`${BASE}/reset-stats`, { token, method: "POST" });
}
