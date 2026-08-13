import { create } from "zustand";
import { persist } from "zustand/middleware";

/**
 * 脚本工作台的本地状态：未发布草稿、试跑用例、编辑器偏好。
 *
 * 三样都刻意**不落服务端**，理由各不相同：
 *
 * - **草稿**：它是「还没想好要不要发」的东西。落到服务端就要回答「谁能看见、
 *   两个人同时改怎么办、什么时候清理」这一串问题，而它的实际寿命通常是几分钟。
 *   但完全不存也不行 —— 误刷新一次就把半小时的改动清零，那是这个界面上
 *   最常见也最没必要的一次损失。
 * - **试跑用例**：它是本人的调试素材（「会员用户」「过期用户」「缺参数」），
 *   不是团队资产。每个人关心的边界不一样，共享只会让列表越来越长。
 * - **编辑器偏好**：字号与换行是个人习惯，本来就该跟着人走而不是跟着函数走。
 *
 * 草稿与用例按 `appKey:functionName` 分键：切换函数时上一个函数的内容
 * 不会串过来，也就不需要一个 effect 去同步（与配置面板同一条约束）。
 */

export type ScriptTestCase = {
  id: string;
  name: string;
  /** input 的 JSON 文本，原样保存 —— 存对象会把作者写的注释与格式弄丢 */
  input: string;
  /** 以哪个用户的身份跑，空串表示以当前管理员身份 */
  asUserId: string;
};

export type ScriptDraft = {
  source: string;
  /** 最后一次改动时间，用来在界面上说清「这份草稿是什么时候的」 */
  updatedAt: number;
};

type FunctionWorkbenchState = {
  drafts: Record<string, ScriptDraft>;
  testCases: Record<string, ScriptTestCase[]>;
  editor: {
    fontSize: number;
    wordWrap: boolean;
    minimap: boolean;
    /**
     * 呼吸光标 / 平滑插入符 / 惯性滚动。
     *
     * 与系统的 `prefers-reduced-motion` 是两件事：那条偏好是无障碍诉求
     * （系统说了算，应用不能盖过），这个开关管的是性能 ——
     * 低端机上平滑插入符会掉帧，而掉帧的光标比不动的光标更难看。
     */
    smoothAnimation: boolean;
  };
  saveDraft: (scope: string, source: string) => void;
  dropDraft: (scope: string) => void;
  addTestCase: (scope: string, testCase: ScriptTestCase) => void;
  removeTestCase: (scope: string, id: string) => void;
  setEditorOption: <K extends keyof FunctionWorkbenchState["editor"]>(
    key: K,
    value: FunctionWorkbenchState["editor"][K]
  ) => void;
};

/** 每个函数最多留 12 条用例：再多就该整理了，而不是继续堆 */
const MAX_TEST_CASES = 12;

export const useFunctionWorkbenchStore = create<FunctionWorkbenchState>()(
  persist(
    (set) => ({
      drafts: {},
      testCases: {},
      editor: { fontSize: 13, wordWrap: false, minimap: false, smoothAnimation: true },

      saveDraft: (scope, source) =>
        set((state) => ({
          drafts: { ...state.drafts, [scope]: { source, updatedAt: Date.now() } }
        })),

      dropDraft: (scope) =>
        set((state) => {
          if (!(scope in state.drafts)) return state;
          const next = { ...state.drafts };
          delete next[scope];
          return { drafts: next };
        }),

      addTestCase: (scope, testCase) =>
        set((state) => {
          const existing = state.testCases[scope] ?? [];
          // 同名覆盖而不是追加：作者调整一条用例时用的就是同一个名字，
          // 追加会让列表里出现两条只有内容不同的「会员用户」。
          const merged = [testCase, ...existing.filter((item) => item.name !== testCase.name)];
          return { testCases: { ...state.testCases, [scope]: merged.slice(0, MAX_TEST_CASES) } };
        }),

      removeTestCase: (scope, id) =>
        set((state) => ({
          testCases: {
            ...state.testCases,
            [scope]: (state.testCases[scope] ?? []).filter((item) => item.id !== id)
          }
        })),

      setEditorOption: (key, value) =>
        set((state) => ({ editor: { ...state.editor, [key]: value } }))
    }),
    {
      name: "aegis-console-function-workbench",
      // v2 加了 smoothAnimation。老状态里没有这个键，而 persist 是整体
      // 覆盖 initialState 的 —— 不合并的话老用户会拿到 undefined，
      // 于是动效开关既不是开也不是关，界面上那个勾显示成未选中却仍在动。
      version: 2,
      merge: (persisted, current) => {
        const saved = (persisted ?? {}) as Partial<FunctionWorkbenchState>;
        return { ...current, ...saved, editor: { ...current.editor, ...saved.editor } };
      }
    }
  )
);
