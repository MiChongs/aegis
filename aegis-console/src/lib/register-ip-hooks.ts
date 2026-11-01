"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  addRegDeviceLists,
  addRegIPLists,
  deleteRegDeviceLists,
  inspectRegDevice,
  listRegDeviceLists,
  updateRegDeviceList,
  deleteRegIPLists,
  getRegIPOverview,
  getRegIPPolicy,
  inspectRegIP,
  listRegIPEvents,
  listRegIPLists,
  listRegIPPenalties,
  listRegIPRegistrations,
  releaseRegIP,
  saveRegIPPolicy,
  simulateRegIP,
  updateRegIPList,
  type RegIPEventParams,
  type RegIPListInput,
  type RegIPListKind,
  type RegIPListParams,
  type RegIPPolicy,
  type RegIPRegistrationParams,
  type RegIPReleaseInput,
  type RegIPSimulateInput
} from "@/lib/api/register-ip";
import { useAdminToken } from "@/lib/admin-hooks";
import { browserTimeZone } from "@/lib/datetime";

/**
 * 注册 IP 控制 hooks。
 *
 * 改策略、改名单、解除限制都会同时改变概览、IP 查询、惩罚期与记录，所以写完整组失效，
 * 不在组件里逐个点名。
 */

const ROOT = "admin-register-ip";

function useInvalidateRegIP() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: [ROOT] });
}

export function useRegIPOverviewQuery(appKey: string | null | undefined, days: number) {
  const token = useAdminToken();
  const tz = browserTimeZone();
  return useQuery({
    queryKey: [ROOT, "overview", token, appKey, days, tz],
    queryFn: () => getRegIPOverview(token as string, appKey as string, days, tz),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    // 突增状态与惩罚期是「此刻」的，概览开着时每 30 秒刷一次
    refetchInterval: 30_000
  });
}

export function useRegIPPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "policy", token, appKey],
    queryFn: () => getRegIPPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 保存后先用返回值写回缓存，编辑器立即跟上；其余视图随整组失效刷新 */
export function useSaveRegIPPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: (policy: RegIPPolicy) => saveRegIPPolicy(token as string, appKey as string, policy),
    onSuccess: async (view) => {
      queryClient.setQueryData([ROOT, "policy", token, appKey], view);
      await invalidate();
    }
  });
}

export function useSimulateRegIPMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: RegIPSimulateInput) => simulateRegIP(token as string, appKey as string, input)
  });
}

export function useRegIPInspectQuery(appKey: string | null | undefined, ip: string | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "inspect", token, appKey, ip],
    queryFn: () => inspectRegIP(token as string, appKey as string, ip as string),
    enabled: Boolean(token && appKey && ip),
    retry: false
  });
}

export function useReleaseRegIPMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: (input: RegIPReleaseInput) => releaseRegIP(token as string, appKey as string, input),
    onSuccess: () => invalidate()
  });
}

export function useRegIPPenaltiesQuery(appKey: string | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "penalties", token, appKey],
    queryFn: () => listRegIPPenalties(token as string, appKey as string, 200),
    enabled: Boolean(token && appKey),
    refetchInterval: 30_000
  });
}

export function useRegIPEventsQuery(appKey: string | null | undefined, params: RegIPEventParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "events", token, appKey, params],
    queryFn: () => listRegIPEvents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useRegIPRegistrationsQuery(appKey: string | null | undefined, params: RegIPRegistrationParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "registrations", token, appKey, params],
    queryFn: () => listRegIPRegistrations(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useRegIPListsQuery(appKey: string | null | undefined, params: RegIPListParams, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "lists", token, appKey, params],
    queryFn: () => listRegIPLists(token as string, appKey as string, params),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: keepPreviousData
  });
}

export function useAddRegIPListsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: (input: RegIPListInput) => addRegIPLists(token as string, appKey as string, input),
    onSuccess: () => invalidate()
  });
}

export function useUpdateRegIPListMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: ({
      id,
      input
    }: {
      id: number;
      input: { kind: RegIPListKind; factor?: number; note?: string; expiresAt?: string | null };
    }) => updateRegIPList(token as string, appKey as string, id, input),
    onSuccess: () => invalidate()
  });
}

export function useDeleteRegIPListsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: (ids: number[]) => deleteRegIPLists(token as string, appKey as string, ids),
    onSuccess: () => invalidate()
  });
}

export function useRegDeviceInspectQuery(appKey: string | null | undefined, device: string | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "device-inspect", token, appKey, device],
    queryFn: () => inspectRegDevice(token as string, appKey as string, device as string),
    enabled: Boolean(token && appKey && device),
    retry: false
  });
}

export function useRegDeviceListsQuery(appKey: string | null | undefined, params: RegIPListParams, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ROOT, "device-lists", token, appKey, params],
    queryFn: () => listRegDeviceLists(token as string, appKey as string, params),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: keepPreviousData
  });
}

export function useAddRegDeviceListsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: (input: RegIPListInput) => addRegDeviceLists(token as string, appKey as string, input),
    onSuccess: () => invalidate()
  });
}

export function useUpdateRegDeviceListMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: ({
      id,
      input
    }: {
      id: number;
      input: { kind: RegIPListKind; factor?: number; note?: string; expiresAt?: string | null };
    }) => updateRegDeviceList(token as string, appKey as string, id, input),
    onSuccess: () => invalidate()
  });
}

export function useDeleteRegDeviceListsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRegIP();
  return useMutation({
    mutationFn: (ids: number[]) => deleteRegDeviceLists(token as string, appKey as string, ids),
    onSuccess: () => invalidate()
  });
}
