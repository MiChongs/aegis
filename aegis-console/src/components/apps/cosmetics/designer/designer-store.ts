"use client";

import { createContext, useContext } from "react";
import { createStore, useStore, type StoreApi } from "zustand";
import type { CosmeticSlot, Design, TrackProperty } from "@/lib/api/cosmetics";
import { designSignature } from "./design-ops";

/**
 * 设计器状态（每打开一件装扮建一个 store，不跨件共享）。
 *
 * 撤销栈存整份设计稿的快照（设计稿是不可变更新，快照之间共享没改的图层，内存很省）：
 * - `update(fn, key)`：一次编辑；同一个 key 在 700ms 内连续改（打字、拖数值）合成一步；
 * - `begin()` / `live(fn)` / `end()`：画布上的拖动——按下时记快照，拖动中只改不入栈，松手时入栈一步。
 * 锁定、选中、时间轴时刻都不进撤销栈。
 */

export type KeyframeRef = { layerId: string; property: TrackProperty; index: number };

export type DesignerState = {
  slot: CosmeticSlot;
  design: Design;
  past: Design[];
  future: Design[];
  gesture: Design | null;
  lastKey: string | null;
  lastAt: number;
  selected: string | null;
  keyframe: KeyframeRef | null;
  locked: Record<string, boolean>;
  /** 自己的时钟（毫秒），设计稿时刻 = clock mod duration */
  clock: number;
  playing: boolean;
  savedSignature: string;

  update: (fn: (design: Design) => Design, key?: string) => void;
  begin: () => void;
  live: (fn: (design: Design) => Design) => void;
  end: () => void;
  undo: () => void;
  redo: () => void;
  select: (id: string | null) => void;
  selectKeyframe: (ref: KeyframeRef | null) => void;
  toggleLock: (id: string) => void;
  setClock: (clock: number) => void;
  setPlaying: (playing: boolean) => void;
  markSaved: (design: Design) => void;
  /** 换掉整份设计稿（放回历史版本）：作为一步可撤销的编辑 */
  replace: (design: Design) => void;
};

const HISTORY = 120;

export function createDesignerStore(slot: CosmeticSlot, initial: Design): StoreApi<DesignerState> {
  return createStore<DesignerState>((set, get) => ({
    slot,
    design: initial,
    past: [],
    future: [],
    gesture: null,
    lastKey: null,
    lastAt: 0,
    selected: null,
    keyframe: null,
    locked: {},
    clock: 0,
    playing: initial.duration > 0,
    savedSignature: designSignature(initial),

    update: (fn, key) => {
      const state = get();
      const next = fn(state.design);
      if (next === state.design) return;
      const now = Date.now();
      const merge = key !== undefined && key === state.lastKey && now - state.lastAt < 700;
      set({
        design: next,
        past: merge ? state.past : [...state.past, state.design].slice(-HISTORY),
        future: [],
        lastKey: key ?? null,
        lastAt: now
      });
    },
    begin: () => set({ gesture: get().design, lastKey: null }),
    live: (fn) => {
      const next = fn(get().design);
      if (next !== get().design) set({ design: next });
    },
    end: () => {
      const { gesture, design, past } = get();
      if (!gesture) return;
      set({
        gesture: null,
        past: gesture !== design ? [...past, gesture].slice(-HISTORY) : past,
        future: gesture !== design ? [] : get().future
      });
    },
    undo: () => {
      const { past, design, future } = get();
      if (!past.length) return;
      const previous = past[past.length - 1];
      set({ design: previous, past: past.slice(0, -1), future: [design, ...future].slice(0, HISTORY), lastKey: null, keyframe: null });
    },
    redo: () => {
      const { past, design, future } = get();
      if (!future.length) return;
      const [next, ...rest] = future;
      set({ design: next, past: [...past, design].slice(-HISTORY), future: rest, lastKey: null, keyframe: null });
    },
    select: (id) => set({ selected: id, keyframe: get().keyframe?.layerId === id ? get().keyframe : null }),
    selectKeyframe: (ref) => set({ keyframe: ref, selected: ref ? ref.layerId : get().selected }),
    toggleLock: (id) => set({ locked: { ...get().locked, [id]: !get().locked[id] } }),
    setClock: (clock) => set({ clock }),
    setPlaying: (playing) => set({ playing }),
    markSaved: (design) => set({ savedSignature: designSignature(design) }),
    replace: (design) => {
      const state = get();
      set({ design, past: [...state.past, state.design].slice(-HISTORY), future: [], selected: null, keyframe: null, lastKey: null });
    }
  }));
}

export const DesignerContext = createContext<StoreApi<DesignerState> | null>(null);

export function useDesignerApi() {
  const store = useContext(DesignerContext);
  if (!store) throw new Error("DesignerContext missing");
  return store;
}

export function useDesigner<T>(selector: (state: DesignerState) => T): T {
  return useStore(useDesignerApi(), selector);
}

/** 设计稿当前时刻 */
export function designTime(state: Pick<DesignerState, "clock" | "design">) {
  const duration = state.design.duration;
  return duration > 0 ? ((state.clock % duration) + duration) % duration : 0;
}
