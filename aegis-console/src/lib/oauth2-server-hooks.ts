"use client";

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  createOAuth2Client,
  deleteOAuth2Client,
  getOAuth2Overview,
  listOAuth2Clients,
  listUserOAuth2Grants,
  revokeUserOAuth2Grants,
  rotateOAuth2ClientSecret,
  updateOAuth2Client,
  type OAuth2ClientPayload
} from "@/lib/api/oauth2-server";

/**
 * 客户端的增删改会同时影响列表与概览里的客户端数，统一失效整棵子树，
 * 免得操作者看到「新建成功了，计数却没变」。
 */
function invalidateClients(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ["oauth2-server"] });
}

export function useOAuth2OverviewQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["oauth2-server", "overview", token, appKey],
    queryFn: () => getOAuth2Overview(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useOAuth2ClientsQuery(appKey?: string | null, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["oauth2-server", "clients", token, appKey],
    queryFn: () => listOAuth2Clients(token as string, appKey as string),
    enabled: Boolean(token && appKey && enabled)
  });
}

/** 新建与编辑共用：带 clientId 即编辑 */
export function useSaveOAuth2ClientMutation(appKey?: string | null) {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ clientId, payload }: { clientId?: string; payload: OAuth2ClientPayload }) =>
      clientId
        ? updateOAuth2Client(token as string, appKey as string, clientId, payload)
        : createOAuth2Client(token as string, appKey as string, payload),
    onSuccess: () => invalidateClients(qc)
  });
}

export function useRotateOAuth2SecretMutation(appKey?: string | null) {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (clientId: string) => rotateOAuth2ClientSecret(token as string, appKey as string, clientId),
    onSuccess: () => invalidateClients(qc)
  });
}

export function useDeleteOAuth2ClientMutation(appKey?: string | null) {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (clientId: string) => deleteOAuth2Client(token as string, appKey as string, clientId),
    onSuccess: () => invalidateClients(qc)
  });
}

export function useUserOAuth2GrantsQuery(appKey?: string | null, userId?: number | null, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["oauth2-server", "grants", token, appKey, userId],
    queryFn: () => listUserOAuth2Grants(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId && enabled)
  });
}

export function useRevokeUserOAuth2GrantsMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (clientId?: string) =>
      revokeUserOAuth2Grants(token as string, appKey as string, userId as number, clientId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["oauth2-server", "grants"] })
  });
}
