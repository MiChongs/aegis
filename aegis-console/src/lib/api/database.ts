import { apiRequest, buildQuery } from "./client";

/**
 * 数据库生命周期与泄漏监控（仅超级管理员）。
 *
 * 读接口分两档：
 *   - snapshot / leak / slow-queries 走服务端内存缓存，零数据库往返，可高频轮询；
 *   - refresh / sessions / maintenance 会真正查库，应按需触发。
 */

export type PoolMetrics = {
  maxConns: number;
  totalConns: number;
  acquiredConns: number;
  idleConns: number;
  constructingConns: number;
  acquireCount: number;
  acquireDurationMs: number;
  avgAcquireWaitMs: number;
  emptyAcquireCount: number;
  canceledAcquireCount: number;
  newConnsCount: number;
  maxLifetimeDestroyCount: number;
  maxIdleDestroyCount: number;
  usagePct: number;
};

/** 进程内观测装置的统计 */
export type InstrumentStats = {
  acquireTotal: number;
  releaseTotal: number;
  checkedOut: number;
  queryTotal: number;
  queryErrors: number;
  slowQueries: number;
  verySlowQueries: number;
  inFlightQueries: number;
  connectTotal: number;
  connectErrors: number;
  avgQueryMs: number;
  maxQueryMs: number;
  leakSuspectCount: number;
  oldestCheckoutMs: number;
  oldestInFlightMs: number;
  trackAcquireStack: boolean;
};

/** 服务端（PostgreSQL）视角的聚合指标 */
export type ServerMetrics = {
  maxConnections: number;
  totalBackends: number;
  activeBackends: number;
  idleBackends: number;
  idleInTxCount: number;
  idleInTxAborted: number;
  waitingBackends: number;
  connUsagePct: number;
  oldestXactSeconds: number;
  oldestQuerySeconds: number;
  oldestIdleInTxSeconds: number;
  oldestSnapshotSeconds: number;
  xactCommit: number;
  xactRollback: number;
  deadlocks: number;
  tempFiles: number;
  tempBytes: number;
  blksHit: number;
  blksRead: number;
  cacheHitRatio: number;
  databaseBytes: number;
  conflictCount: number;
  rolledBackPct: number;
  blockedByLocks: number;
  inactiveReplicationSlots: number;
  replicationRetainedBytes: number;
  preparedXacts: number;
  oldestPreparedXactSeconds: number;
  deadTuples: number;
  tablesNeedingVacuum: number;
  collectErrors?: string[];
};

export type RedisPoolMetrics = {
  hits: number;
  misses: number;
  timeouts: number;
  totalConns: number;
  idleConns: number;
  staleConns: number;
};

export type DatabaseMetrics = {
  timestamp: string;
  healthy: boolean;
  pingMs: number;
  pool: PoolMetrics;
  instrument: InstrumentStats;
  server: ServerMetrics;
  redis?: RedisPoolMetrics;
};

export type LeakKind =
  | "connection"
  | "transaction"
  | "snapshot"
  | "wal"
  | "prepared_xact"
  | "storage"
  | "pool"
  | "trend";

export type LeakSeverity = "info" | "warning" | "critical";

export type DBLeakFinding = {
  kind: LeakKind;
  severity: LeakSeverity;
  title: string;
  detail: string;
  count?: number;
  evidence?: string[];
  advice?: string;
  detectedAt: string;
};

export type LeakIndicator = {
  name: string;
  trending: string;
  samples: number;
  consecutiveUp: number;
  latestValue: number;
  deltaPercent: number;
  suspectedLeak: boolean;
  alertMessage?: string;
  lastCheckedAt: string;
};

export type DBLeakReport = {
  checkedAt: string;
  suspicious: boolean;
  critical: number;
  warning: number;
  summary: string;
  findings: DBLeakFinding[];
  trends: LeakIndicator[];
};

export type SlowQuerySample = {
  sql: string;
  durationMs: number;
  rows: number;
  err?: string;
  pid?: number;
  occurredAt: string;
};

export type InFlightQuery = {
  sql: string;
  elapsedMs: number;
  startedAt: string;
  pid?: number;
};

export type ConnLeakSuspect = {
  pid: number;
  heldMs: number;
  since: string;
  stack?: string;
  reported: boolean;
};

export type DatabaseAlert = {
  level: string;
  message: string;
  metric: string;
  value: number;
  threshold: number;
  at: string;
};

export type DatabaseLifecycleView = {
  maxConns: number;
  minConns: number;
  maxConnLifetime: string;
  maxConnLifetimeJitter: string;
  maxConnIdleTime: string;
  healthCheckPeriod: string;
  connectTimeout: string;
  sessionSettings: Record<string, string>;
  monitorInterval: string;
  slowQueryThreshold: string;
  connHoldThreshold: string;
  idleInTxThreshold: string;
  longQueryThreshold: string;
  trackAcquireStack: boolean;
  autoTerminateIdleInTx: boolean;
  autoTerminateAfter: string;
  drainTimeout: string;
  warmupOnStart: boolean;
};

export type DatabaseSnapshot = {
  timestamp: string;
  metrics?: DatabaseMetrics;
  leak?: DBLeakReport;
  alerts: DatabaseAlert[];
  slowQueries: SlowQuerySample[];
  inFlight: InFlightQuery[];
  leakSuspects: ConnLeakSuspect[];
  lifecycle: DatabaseLifecycleView;
};

export type DBSession = {
  pid: number;
  user?: string;
  applicationName?: string;
  clientAddr?: string;
  state?: string;
  waitEventType?: string;
  waitEvent?: string;
  backendType?: string;
  query?: string;
  backendStart?: string;
  xactStart?: string;
  queryStart?: string;
  stateChange?: string;
  xactSeconds: number;
  querySeconds: number;
  stateSeconds: number;
  blockedBy?: number[];
  isSelf: boolean;
};

export type BloatedTable = {
  schema: string;
  table: string;
  liveTuples: number;
  deadTuples: number;
  deadRatio: number;
  totalBytes: number;
  lastAutoVacuum?: string;
  lastAutoAnalyze?: string;
};

export type UnusedIndex = {
  schema: string;
  table: string;
  index: string;
  scans: number;
  bytes: number;
};

export type DatabaseMaintenance = {
  bloatedTables: BloatedTable[];
  unusedIndexes: UnusedIndex[];
};

const base = "/api/admin/system/database";

export function getDatabaseSnapshot(token: string) {
  return apiRequest<DatabaseSnapshot>(`${base}/snapshot`, { token });
}

export function refreshDatabaseSnapshot(token: string) {
  return apiRequest<DatabaseSnapshot>(`${base}/refresh`, { method: "POST", token });
}

export function getDatabaseHistory(token: string, range?: string) {
  return apiRequest<{ items: DatabaseMetrics[] }>(`${base}/history${buildQuery({ range })}`, { token });
}

export function getDatabaseLeak(token: string) {
  return apiRequest<{
    leak?: DBLeakReport;
    leakSuspects: ConnLeakSuspect[];
    inFlight: InFlightQuery[];
  }>(`${base}/leak`, { token });
}

export function getDatabaseSlowQueries(token: string) {
  return apiRequest<{ items: SlowQuerySample[] }>(`${base}/slow-queries`, { token });
}

export function getDatabaseSessions(
  token: string,
  params?: { onlyProblematic?: boolean; limit?: number }
) {
  return apiRequest<{ items: DBSession[] }>(`${base}/sessions${buildQuery({ ...params })}`, { token });
}

/** 终止与取消返回同构结果，便于调用方用同一个 mutation 承载两种操作 */
export type SessionActionResult = { pid: number; terminated?: boolean; canceled?: boolean };

export function terminateDatabaseSession(token: string, pid: number) {
  return apiRequest<SessionActionResult>(`${base}/sessions/${pid}/terminate`, {
    method: "POST",
    token
  });
}

export function cancelDatabaseSession(token: string, pid: number) {
  return apiRequest<SessionActionResult>(`${base}/sessions/${pid}/cancel`, {
    method: "POST",
    token
  });
}

export function getDatabaseMaintenance(token: string, limit?: number) {
  return apiRequest<DatabaseMaintenance>(`${base}/maintenance${buildQuery({ limit })}`, { token });
}

export function warmupDatabasePool(token: string) {
  return apiRequest<DatabaseSnapshot>(`${base}/warmup`, { method: "POST", token });
}
