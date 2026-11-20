"use client";

import { useSyncExternalStore } from "react";

/**
 * 装扮编号 → 名称的进程内登记表。
 *
 * 积分商城、抽奖、邀请、卡密、物品发放的奖励摘要都是纯函数（`describeReward` 一类），拿不到 appKey，
 * 也不该为了一个名字各自再接一遍查询。装扮列表每次取回时把名字登记在这里（编号是全局自增主键，
 * 不同应用不会撞），摘要函数直接读；展示摘要的组件调一次 `useCosmeticNamesVersion()` 订阅，
 * 名字晚到时自动重绘。查不到时摘要退回「装扮 #编号」。
 */

const names = new Map<number, string>();
const listeners = new Set<() => void>();
let version = 0;

export function rememberCosmeticNames(items: Array<{ id: number; name: string }>) {
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

export function cosmeticNameOf(id?: number | null): string | undefined {
  return id ? names.get(id) : undefined;
}

/** 一项装扮奖励的说法：「鎏金光环 30 天」「鎏金光环 永久」 */
export function cosmeticRewardText(refId?: number | null, days?: number | null) {
  const name = cosmeticNameOf(refId) ?? (refId ? `装扮 #${refId}` : "未选择装扮");
  return `${name} ${cosmeticTermLabel(days ?? 0)}`;
}

export function cosmeticTermLabel(days: number) {
  if (days <= 0) return "永久";
  if (days % 365 === 0) return `${days / 365} 年`;
  if (days % 30 === 0) return `${days / 30} 个月`;
  return `${days} 天`;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** 订阅登记表：名字登记进来时让调用它的组件重绘 */
export function useCosmeticNamesVersion() {
  return useSyncExternalStore(
    subscribe,
    () => version,
    () => 0
  );
}
