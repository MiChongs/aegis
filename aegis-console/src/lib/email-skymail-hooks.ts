"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  skymailAction,
  skymailQuery,
  type SkymailActions,
  type SkymailQueries
} from "@/lib/api/email-skymail";
import type { EmailScope } from "@/lib/email-hooks";

/**
 * Cloud Mail 邮局的查询与操作。
 *
 * 缓存键按「作用域 → 通道 → 资源 → 参数」分层：任何一次写操作都把这条通道下的全部查询作废 ——
 * 删一封信会同时改变收件箱、全部邮件与用户的收件计数，逐个点名作废总会漏掉一个。
 */

function scopeAppId(scope: EmailScope): number | null {
  return scope.kind === "platform" ? null : scope.appId;
}

function channelKey(scope: EmailScope, configId: number) {
  return ["email", "skymail", scope.kind === "platform" ? "platform" : `app-${scope.appId}`, configId] as const;
}

export function useSkymailQuery<R extends keyof SkymailQueries>(
  scope: EmailScope,
  configId: number | null,
  resource: R,
  params: SkymailQueries[R]["params"],
  options: { enabled?: boolean } = {}
) {
  const token = useAdminToken();
  return useQuery<SkymailQueries[R]["result"]>({
    queryKey: [...channelKey(scope, configId ?? 0), resource, params],
    queryFn: () => skymailQuery(token as string, scopeAppId(scope), configId as number, resource, params),
    enabled: Boolean(token) && Boolean(configId) && (options.enabled ?? true),
    retry: false,
    // 邮局里的数据别人也在改（新信、别的管理员），切回页面时刷新一次
    staleTime: 15_000
  });
}

/** 一个写操作：动作名与它的参数成对出现，传错参数在编译期就会报出来。 */
export type SkymailActionInput = {
  [A in keyof SkymailActions]: { action: A; params: SkymailActions[A] };
}[keyof SkymailActions];

export function useSkymailAction(scope: EmailScope, configId: number | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: SkymailActionInput) =>
      skymailAction(token as string, scopeAppId(scope), configId as number, input.action, input.params as never),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: channelKey(scope, configId ?? 0) });
    }
  });
}
