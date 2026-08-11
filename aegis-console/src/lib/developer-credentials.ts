"use client";

import { useSyncExternalStore } from "react";
import { EMPTY_CREDENTIALS, type Credentials } from "@/lib/api/openapi-request";

const STORAGE_KEY = "aegis.developers.credentials";

/**
 * 接口文档调试台的凭据存储。
 *
 * 与 `use-client-value.ts` 同一套路：用 `useSyncExternalStore` 而不是
 * `useEffect` + `setState` 读 localStorage，后者会触发级联渲染，
 * 也过不了 `react-hooks/set-state-in-effect`。
 *
 * 这是公开门户，存进来的是调试用的短期令牌，不做加密；
 * 页面上已提示不要填生产环境的长期凭据。
 */

let cached: Credentials | null = null;
const listeners = new Set<() => void>();

function readSnapshot(): Credentials {
  if (cached) return cached;
  let loaded: Credentials = EMPTY_CREDENTIALS;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) loaded = { ...EMPTY_CREDENTIALS, ...JSON.parse(raw) };
  } catch {
    // 隐私模式或内容损坏时退回空凭据，不影响文档浏览
  }
  cached = loaded;
  return loaded;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** 写入并广播。getSnapshot 依赖 cached 的引用稳定性，因此这里必须整体替换。 */
export function updateCredentials(patch: Partial<Credentials>) {
  cached = { ...readSnapshot(), ...patch };
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(cached));
  } catch {
    // 写入失败时内存态仍然生效，本次会话可以继续调试
  }
  for (const listener of listeners) listener();
}

export function useCredentials(): Credentials {
  return useSyncExternalStore(subscribe, readSnapshot, () => EMPTY_CREDENTIALS);
}
