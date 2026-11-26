import { create } from "zustand";
import type { FirewallEventQuery, FirewallExclusionInput, FirewallSandboxInput } from "@/lib/api/firewall";

/**
 * 防火墙工作台的页签间跳转意图（不持久化）。
 *
 * 典型路径：事件详情 →「为这个误报建例外」→ 例外页签打开编辑器并预填；
 * 事件详情 →「在测试台复现」→ 测试台预填请求；总览点某个 IP / 规则组 → 事件页签带上筛选。
 * 目标页签挂载时用 `take*` 取走（取一次就清空），所以来回切页签不会重复弹编辑器。
 */
type FirewallIntentState = {
  exclusionDraft: FirewallExclusionInput | null;
  sandboxDraft: FirewallSandboxInput | null;
  eventsFilter: FirewallEventQuery | null;
  setExclusionDraft: (draft: FirewallExclusionInput | null) => void;
  setSandboxDraft: (draft: FirewallSandboxInput | null) => void;
  setEventsFilter: (filter: FirewallEventQuery | null) => void;
  takeExclusionDraft: () => FirewallExclusionInput | null;
  takeSandboxDraft: () => FirewallSandboxInput | null;
  takeEventsFilter: () => FirewallEventQuery | null;
};

export const useFirewallIntents = create<FirewallIntentState>((set, get) => ({
  exclusionDraft: null,
  sandboxDraft: null,
  eventsFilter: null,
  setExclusionDraft: (draft) => set({ exclusionDraft: draft }),
  setSandboxDraft: (draft) => set({ sandboxDraft: draft }),
  setEventsFilter: (filter) => set({ eventsFilter: filter }),
  takeExclusionDraft: () => {
    const draft = get().exclusionDraft;
    if (draft) set({ exclusionDraft: null });
    return draft;
  },
  takeSandboxDraft: () => {
    const draft = get().sandboxDraft;
    if (draft) set({ sandboxDraft: null });
    return draft;
  },
  takeEventsFilter: () => {
    const filter = get().eventsFilter;
    if (filter) set({ eventsFilter: null });
    return filter;
  }
}));
