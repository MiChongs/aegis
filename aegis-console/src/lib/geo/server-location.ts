// 服务端端点位置：地图上代表「我们自己」的那个点。
//
// 两处在用，且必须是同一个值：
//   攻击飞线图   所有攻击弧线汇聚的目标
//   用户活动地图 内网 / 回环来源的落点（这类地址的位置就是服务端自己）
// 拆成两份配置的话，同一台机器会在两张图上出现在两个地方，
// 而管理员无从判断哪张图说得对。
//
// 用 useSyncExternalStore 而不是 useEffect + setState：后者既触发级联渲染、
// 也过不了 react-hooks/set-state-in-effect（与 use-client-value.ts 同一约束）。

import { useSyncExternalStore } from "react";

export type ServerLocation = { name: string; lat: number; lng: number };

const KEY = "aegis:map:server-location";
/** 旧键：这份配置原本只属于攻击飞线图。读取时兼容，不再写入。 */
const LEGACY_KEY = "aegis:attack-map:server-location";

export const DEFAULT_SERVER_LOCATION: ServerLocation = { name: "服务器", lat: 39.9, lng: 116.4 };

function parse(raw: string | null): ServerLocation | null {
  if (!raw) return null;
  try {
    const p = JSON.parse(raw) as Partial<ServerLocation>;
    if (typeof p?.lat !== "number" || typeof p?.lng !== "number") return null;
    if (!Number.isFinite(p.lat) || !Number.isFinite(p.lng)) return null;
    if (p.lat < -90 || p.lat > 90 || p.lng < -180 || p.lng > 180) return null;
    return { name: (p.name || "").trim() || DEFAULT_SERVER_LOCATION.name, lat: p.lat, lng: p.lng };
  } catch {
    return null;
  }
}

function read(): ServerLocation {
  if (typeof window === "undefined") return DEFAULT_SERVER_LOCATION;
  try {
    return parse(localStorage.getItem(KEY)) ?? parse(localStorage.getItem(LEGACY_KEY)) ?? DEFAULT_SERVER_LOCATION;
  } catch {
    // 隐私模式下 localStorage 不可读
    return DEFAULT_SERVER_LOCATION;
  }
}

// 快照必须是稳定引用：useSyncExternalStore 每次渲染都会调 getSnapshot，
// 每次返回新对象会被判定为「外部状态变了」而无限重渲染。
let snapshot: ServerLocation | null = null;
const listeners = new Set<() => void>();

function emit() {
  snapshot = null;
  for (const fn of listeners) fn();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  const onStorage = (e: StorageEvent) => {
    if (e.key === KEY || e.key === LEGACY_KEY) emit();
  };
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", onStorage);
  };
}

function getSnapshot(): ServerLocation {
  if (!snapshot) snapshot = read();
  return snapshot;
}

/** SSR 快照恒为默认值：localStorage 在服务端不存在，水合后 React 会自动补一次渲染 */
function getServerSnapshot(): ServerLocation {
  return DEFAULT_SERVER_LOCATION;
}

export function saveServerLocation(next: ServerLocation) {
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // 隐私模式下写不进去，本次会话内仍生效
  }
  emit();
}

export function useServerLocation(): ServerLocation {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
