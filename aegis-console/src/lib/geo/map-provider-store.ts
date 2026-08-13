"use client";

// 底图供应商偏好：全站一份，跨面板与跨标签页同步
//
// 用 `useSyncExternalStore` 而不是「useState 初值读 localStorage」：
// 后者在 SSR 与首帧客户端之间会给出两个不同的值（水合告警），且五个地图面板
// 各持一份 state，在同一页切换供应商时另一张图不会跟着变。
// 与 lib/use-client-value.ts、门户凭据是同一条约束。

import { useCallback, useMemo, useSyncExternalStore } from "react";

import {
  AUTO_PREFERENCE_VALUE,
  detectMapLocale,
  effectiveLang,
  resolveProvider,
  type MapLang,
  type MapLocale,
  type MapProvider,
  type MapProviderPreference
} from "@/lib/geo/map-providers";

const STORAGE_KEY = "aegis:geo-map:provider";
/** 旧版只有「简图 / 街道」两档，键名与取值都不同，读到就地迁移一次 */
const LEGACY_KEY = "aegis:geo-map:tiles";
const LEGACY_VALUES: Record<string, string> = { vector: "local", raster: "carto" };

const listeners = new Set<() => void>();
let cached: MapProviderPreference | null = null;
let storageBound = false;

function readPreference(): MapProviderPreference {
  if (typeof window === "undefined") return AUTO_PREFERENCE_VALUE;
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) return saved;
    const legacy = localStorage.getItem(LEGACY_KEY);
    const migrated = legacy ? LEGACY_VALUES[legacy] : undefined;
    if (migrated) {
      localStorage.setItem(STORAGE_KEY, migrated);
      localStorage.removeItem(LEGACY_KEY);
      return migrated;
    }
  } catch {
    // 隐私模式下 localStorage 不可读，走自动档
  }
  return AUTO_PREFERENCE_VALUE;
}

function emit() {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (!storageBound && typeof window !== "undefined") {
    storageBound = true;
    window.addEventListener("storage", (e) => {
      if (e.key !== null && e.key !== STORAGE_KEY) return;
      cached = null;
      emit();
    });
  }
  return () => {
    listeners.delete(listener);
  };
}

function getSnapshot(): MapProviderPreference {
  if (cached === null) cached = readPreference();
  return cached;
}

function getServerSnapshot(): MapProviderPreference {
  return AUTO_PREFERENCE_VALUE;
}

export function setMapProviderPreference(next: MapProviderPreference) {
  if (cached === next) return;
  cached = next;
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // 隐私模式下写不进去，本次会话内仍然生效
  }
  emit();
}

// ── 浏览器语言：一次会话内不会变，订阅函数永不触发 ──

const noopSubscribe = () => () => {};
/** 服务端快照必须是稳定引用，否则 useSyncExternalStore 会判定为无限更新 */
const SERVER_LOCALE: MapLocale = { lang: "en", region: "global", tag: "" };
let cachedLocale: MapLocale | null = null;

function getLocaleSnapshot(): MapLocale {
  if (!cachedLocale) cachedLocale = detectMapLocale();
  return cachedLocale;
}

export function useMapLocale(): MapLocale {
  return useSyncExternalStore(noopSubscribe, getLocaleSnapshot, () => SERVER_LOCALE);
}

export type MapProviderSelection = {
  /** 原始偏好：`auto` 或供应商 id */
  pref: MapProviderPreference;
  /** 是否处于自动档 */
  auto: boolean;
  /** 实际生效的供应商 */
  provider: MapProvider;
  /** 浏览器语言推导结果 */
  locale: MapLocale;
  /** 该供应商在此语言下实际给出的注记语言 */
  lang: MapLang;
  select: (next: MapProviderPreference) => void;
};

export function useMapProvider(): MapProviderSelection {
  const pref = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  const locale = useMapLocale();

  const provider = useMemo(() => resolveProvider(pref, locale.region), [pref, locale.region]);
  const lang = effectiveLang(provider, locale.lang);
  const select = useCallback((next: MapProviderPreference) => setMapProviderPreference(next), []);

  return { pref, auto: pref === AUTO_PREFERENCE_VALUE, provider, locale, lang, select };
}
