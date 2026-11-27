"use client";

import { useSyncExternalStore } from "react";

/**
 * 券编号 → 名称的进程内登记表（与 cosmetic-names 同一个做法）。
 *
 * 积分商城、抽奖、邀请、卡密、物品发放的奖励摘要是纯函数，拿不到 appKey；券列表每次取回时把名字登记在这里
 * （编号是全局自增主键，不同应用不会撞），摘要函数直接读。展示摘要的组件调一次 `useCouponNamesVersion()` 订阅，
 * 名字晚到时自动重绘；查不到时摘要退回「优惠券 #编号」。
 */

const names = new Map<number, string>();
const listeners = new Set<() => void>();
let version = 0;

export function rememberCouponNames(items: Array<{ id: number; name: string }>) {
  let changed = false;
  for (const item of items) {
    if (names.get(item.id) !== item.name) {
      names.set(item.id, item.name);
      changed = true;
    }
  }
  if (changed) {
    version += 1;
    for (const listener of listeners) listener();
  }
}

export function couponNameOf(id?: number | null): string | undefined {
  return id ? names.get(id) : undefined;
}

/** 一项券奖励的说法：「新人立减券 2 张」 */
export function couponRewardText(refId?: number | null, count?: number | null) {
  const name = couponNameOf(refId) ?? (refId ? `优惠券 #${refId}` : "未选择优惠券");
  return `${name} ${Math.max(1, count ?? 1)} 张`;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useCouponNamesVersion() {
  return useSyncExternalStore(
    subscribe,
    () => version,
    () => 0
  );
}
