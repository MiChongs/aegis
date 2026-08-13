"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api-client";
import {
  createAppEmailConfig,
  createPlatformEmailConfig,
  deleteAppEmailConfig,
  deletePlatformEmailConfig,
  getAppEmailChannel,
  getAppEmailConfigs,
  getAppEmailDeliveries,
  getAppEmailStats,
  getEmailProviderCatalog,
  getPlatformEmailChannel,
  getPlatformEmailConfigs,
  getPlatformEmailDeliveries,
  getPlatformEmailStats,
  testAppEmailConfig,
  testPlatformEmailConfig,
  updateAppEmailConfig,
  updatePlatformEmailConfig,
  type PlatformEmailConfigPayload
} from "@/lib/api/email";
import type {
  EmailChannelResolution,
  EmailConfig,
  EmailDeliveryPage,
  EmailDeliveryStats,
  EmailProviderCatalog
} from "@/lib/api/types";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 邮件通道的 React Query hooks。
 *
 * 作用域用 `EmailScope` 表达而不是「有没有 appId」：平台级的 appId 是 0，
 * 而 0 在 JS 里是 falsy —— 用 `appId ? ... : ...` 判断会把平台级静默当成
 * 「还没选应用」，表现是面板永远显示「请先选择应用」。
 */
export type EmailScope = { kind: "platform" } | { kind: "app"; appId: number };

export const PLATFORM_EMAIL_SCOPE: EmailScope = { kind: "platform" };

/** 作用域的查询键片段。两种作用域的缓存必须分开，否则切页面会读到对方的数据。 */
function scopeKey(scope: EmailScope) {
  return scope.kind === "platform" ? "platform" : `app-${scope.appId}`;
}

function isReady(scope: EmailScope) {
  return scope.kind === "platform" || scope.appId > 0;
}

// ── 服务商目录 ──

/**
 * 服务商目录。静态自述，缓存整场会话 —— 它只会随后端发版变化，
 * 而每打开一次面板重拉一遍是纯浪费。
 */
export function useEmailProviderCatalogQuery() {
  const token = useAdminToken();
  return useQuery<EmailProviderCatalog>({
    queryKey: ["email", "providers"],
    queryFn: () => getEmailProviderCatalog(token as string),
    enabled: Boolean(token),
    staleTime: Infinity
  });
}

// ── 配置 ──

export function useEmailConfigsQuery(scope: EmailScope) {
  const token = useAdminToken();
  return useQuery<EmailConfig[]>({
    queryKey: ["email", "configs", scopeKey(scope)],
    queryFn: () =>
      scope.kind === "platform"
        ? getPlatformEmailConfigs(token as string)
        : getAppEmailConfigs(token as string, scope.appId),
    enabled: Boolean(token) && isReady(scope)
  });
}

/**
 * 当前生效的通道。
 *
 * 后端在「一条通道都没有」时返回 404，这里把它收敛成 `null` 而不是错误：
 * 「没配」是这个页面最正常的初始状态，把它渲染成一条红色报错会让人以为出了故障。
 */
export function useEmailChannelQuery(scope: EmailScope) {
  const token = useAdminToken();
  return useQuery<EmailChannelResolution | null>({
    queryKey: ["email", "channel", scopeKey(scope)],
    queryFn: async () => {
      try {
        return scope.kind === "platform"
          ? await getPlatformEmailChannel(token as string)
          : await getAppEmailChannel(token as string, scope.appId);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
          return null;
        }
        throw error;
      }
    },
    enabled: Boolean(token) && isReady(scope),
    retry: false
  });
}

export function useEmailStatsQuery(scope: EmailScope) {
  const token = useAdminToken();
  return useQuery<EmailDeliveryStats>({
    queryKey: ["email", "stats", scopeKey(scope)],
    queryFn: () =>
      scope.kind === "platform"
        ? getPlatformEmailStats(token as string)
        : getAppEmailStats(token as string, scope.appId),
    enabled: Boolean(token) && isReady(scope)
  });
}

export type EmailDeliveryFilters = {
  status?: string;
  provider?: string;
  purpose?: string;
  keyword?: string;
  page?: number;
  pageSize?: number;
};

export function useEmailDeliveriesQuery(scope: EmailScope, filters: EmailDeliveryFilters = {}) {
  const token = useAdminToken();
  return useQuery<EmailDeliveryPage>({
    queryKey: ["email", "deliveries", scopeKey(scope), filters],
    queryFn: () =>
      scope.kind === "platform"
        ? getPlatformEmailDeliveries(token as string, filters)
        : getAppEmailDeliveries(token as string, { appid: scope.appId, ...filters }),
    enabled: Boolean(token) && isReady(scope)
  });
}

// ── 写操作 ──

/**
 * 失效关系集中在这里：一次保存会同时影响配置列表、当前通道、以及
 * （改了默认或共享开关时）应用侧看到的继承结论。散在各组件里必然漏掉其中几张。
 */
function useEmailInvalidator(scope: EmailScope) {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: ["email", "configs", scopeKey(scope)] });
    void queryClient.invalidateQueries({ queryKey: ["email", "channel"] });
    void queryClient.invalidateQueries({ queryKey: ["email", "stats", scopeKey(scope)] });
  };
}

export function useSaveEmailConfigMutation(scope: EmailScope) {
  const token = useAdminToken();
  const invalidate = useEmailInvalidator(scope);
  return useMutation({
    mutationFn: ({ configId, payload }: { configId?: number; payload: PlatformEmailConfigPayload }) => {
      if (scope.kind === "platform") {
        return configId
          ? updatePlatformEmailConfig(token as string, configId, payload)
          : createPlatformEmailConfig(token as string, payload);
      }
      const appPayload = { ...payload, appid: scope.appId, config_id: configId };
      return configId
        ? updateAppEmailConfig(token as string, appPayload)
        : createAppEmailConfig(token as string, appPayload);
    },
    onSuccess: invalidate
  });
}

export function useDeleteEmailConfigMutation(scope: EmailScope) {
  const token = useAdminToken();
  const invalidate = useEmailInvalidator(scope);
  return useMutation({
    mutationFn: (configId: number) =>
      scope.kind === "platform"
        ? deletePlatformEmailConfig(token as string, configId)
        : deleteAppEmailConfig(token as string, { appid: scope.appId, config_id: configId }),
    onSuccess: invalidate
  });
}

export function useTestEmailConfigMutation(scope: EmailScope) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ configId, testEmail }: { configId: number; testEmail: string }) =>
      scope.kind === "platform"
        ? testPlatformEmailConfig(token as string, configId, testEmail)
        : testAppEmailConfig(token as string, {
            appid: scope.appId,
            config_id: configId,
            test_email: testEmail
          }),
    // 试发也会留痕，成功与否都要刷新投递记录 —— 那份记录正是判断
    // 「到底发出去没有」的唯一依据，而失败的试发恰恰最需要看。
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ["email", "deliveries", scopeKey(scope)] });
      void queryClient.invalidateQueries({ queryKey: ["email", "stats", scopeKey(scope)] });
    }
  });
}
