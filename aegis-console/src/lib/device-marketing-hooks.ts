"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  createDeviceMarketingName,
  deleteDeviceMarketingName,
  getDeviceMarketingName,
  listDeviceManufacturers,
  listDeviceMarketingNames,
  lookupDeviceMarketingName,
  reseedDeviceMarketingNames,
  updateDeviceMarketingName,
  type DeviceMarketingCreateInput,
  type DeviceMarketingQuery,
  type DeviceMarketingUpdateInput,
  type DevicePlatform,
} from "@/lib/api/device-marketing";

const ROOT = ["device-marketing"] as const;

// ── 查询 ──────────────────────────────────────────

export function useDeviceMarketingListQuery(params: DeviceMarketingQuery) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...ROOT, "list", token, params],
    queryFn: () => listDeviceMarketingNames(token as string, params),
    enabled: Boolean(token),
    staleTime: 30_000,
  });
}

export function useDeviceMarketingDetailQuery(id: number | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...ROOT, "detail", token, id],
    queryFn: () => getDeviceMarketingName(token as string, id as number),
    enabled: Boolean(token && id && id > 0),
  });
}

export function useDeviceManufacturersQuery(platform?: DevicePlatform | "") {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...ROOT, "manufacturers", token, platform ?? ""],
    queryFn: () => listDeviceManufacturers(token as string, platform),
    enabled: Boolean(token),
    staleTime: 60_000,
  });
}

export function useDeviceMarketingLookupQuery(
  platform?: DevicePlatform | null,
  identifier?: string | null,
  options: { enabled?: boolean } = {},
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...ROOT, "lookup", token, platform, identifier],
    queryFn: () =>
      lookupDeviceMarketingName(token as string, platform as DevicePlatform, identifier as string),
    enabled: Boolean(token && platform && identifier && (options.enabled ?? true)),
  });
}

// ── 变更 ──────────────────────────────────────────

export function useCreateDeviceMarketingMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: DeviceMarketingCreateInput) =>
      createDeviceMarketingName(token as string, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: ROOT }),
  });
}

export function useUpdateDeviceMarketingMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, payload }: { id: number; payload: DeviceMarketingUpdateInput }) =>
      updateDeviceMarketingName(token as string, id, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: ROOT }),
  });
}

export function useDeleteDeviceMarketingMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteDeviceMarketingName(token as string, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ROOT }),
  });
}

export function useReseedDeviceMarketingMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => reseedDeviceMarketingNames(token as string),
    onSuccess: () => qc.invalidateQueries({ queryKey: ROOT }),
  });
}
