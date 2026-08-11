import { create } from "zustand";
import { persist } from "zustand/middleware";

export type OperatorProfile = {
  id?: string | number;
  account?: string;
  displayName?: string;
  avatar?: string;
  avatarVersion?: number; // 时间戳，用于破缓存
  role?: string;
  appId?: string | number;
  isSuperAdmin?: boolean;
  authSource?: string;
  assignments?: Array<{
    roleKey?: string;
    appid?: number | null;
    appName?: string;
  }>;
};

/** 为头像 URL 追加版本参数以破缓存 */
export function avatarUrl(src?: string | null, version?: number): string {
  if (!src) return "";
  if (!version) return src;
  const sep = src.includes("?") ? "&" : "?";
  return `${src}${sep}v=${version}`;
}

type AuthState = {
  hydrated: boolean;
  accessToken: string | null;
  refreshToken: string | null;
  operator: OperatorProfile | null;
  setHydrated: (hydrated: boolean) => void;
  setSession: (payload: {
    accessToken: string;
    refreshToken?: string | null;
    operator?: OperatorProfile | null;
  }) => void;
  patchOperator: (payload: Partial<OperatorProfile>) => void;
  clearSession: () => void;
};

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      hydrated: false,
      accessToken: null,
      refreshToken: null,
      operator: null,
      setHydrated: (hydrated) => set({ hydrated }),
      setSession: ({ accessToken, refreshToken, operator }) =>
        set({
          hydrated: true,
          accessToken,
          refreshToken: refreshToken ?? null,
          operator: operator ?? null
        }),
      patchOperator: (payload) =>
        set((state) => ({
          operator: state.operator ? { ...state.operator, ...payload } : state.operator
        })),
      clearSession: () =>
        set({
          hydrated: true,
          accessToken: null,
          refreshToken: null,
          operator: null
        })
    }),
    {
      name: "aegis-console-auth",
      onRehydrateStorage: () => (state) => {
        state?.setHydrated(true);
      }
    }
  )
);
