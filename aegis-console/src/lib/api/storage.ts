import { apiRequest } from "./client";
import type { StorageConfig, StorageConfigDetail } from "./types";

export function getGlobalStorageConfigs(token: string, provider?: string) {
  return apiRequest<StorageConfig[]>("/api/admin/platform/storage-config/list", {
    method: "POST",
    token,
    body: JSON.stringify(provider ? { provider } : {})
  });
}

export function getAppStorageConfigs(token: string, appid: number, provider?: string) {
  return apiRequest<StorageConfig[]>("/api/admin/app/storage-config/list", {
    method: "POST",
    token,
    body: JSON.stringify({
      appid,
      provider: provider || undefined
    })
  });
}

export function getGlobalStorageConfigDetail(token: string, configId: number) {
  return apiRequest<StorageConfigDetail>("/api/admin/platform/storage-config/detail", {
    method: "POST",
    token,
    body: JSON.stringify({
      configId,
      config_id: configId
    })
  });
}

export function getAppStorageConfigDetail(token: string, appid: number, configId: number) {
  return apiRequest<StorageConfigDetail>("/api/admin/app/storage-config/detail", {
    method: "POST",
    token,
    body: JSON.stringify({
      appid,
      configId,
      config_id: configId
    })
  });
}

export function createGlobalStorageConfig(
  token: string,
  payload: {
    provider: string;
    config_name: string;
    access_mode: string;
    enabled?: boolean;
    is_default?: boolean;
    proxy_download?: boolean;
    base_url?: string;
    root_path?: string;
    description?: string;
    config_data?: Record<string, unknown>;
  }
) {
  return apiRequest<StorageConfigDetail>("/api/admin/platform/storage-config/create", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateGlobalStorageConfig(
  token: string,
  payload: {
    config_id: number;
    provider?: string;
    config_name?: string;
    access_mode?: string;
    enabled?: boolean;
    is_default?: boolean;
    proxy_download?: boolean;
    base_url?: string;
    root_path?: string;
    description?: string;
    config_data?: Record<string, unknown>;
  }
) {
  return apiRequest<StorageConfigDetail>("/api/admin/platform/storage-config/update", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteGlobalStorageConfig(token: string, configId: number) {
  return apiRequest<null>("/api/admin/platform/storage-config/delete", {
    method: "POST",
    token,
    body: JSON.stringify({ config_id: configId })
  });
}

export function testGlobalStorageConfig(token: string, configId: number) {
  return apiRequest<Record<string, unknown>>("/api/admin/platform/storage-config/test", {
    method: "POST",
    token,
    body: JSON.stringify({ config_id: configId })
  });
}

export function createAppStorageConfig(
  token: string,
  payload: {
    appid: number;
    provider: string;
    config_name: string;
    access_mode: string;
    enabled?: boolean;
    is_default?: boolean;
    proxy_download?: boolean;
    base_url?: string;
    root_path?: string;
    description?: string;
    config_data?: Record<string, unknown>;
  }
) {
  return apiRequest<StorageConfigDetail>("/api/admin/app/storage-config/create", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updateAppStorageConfig(
  token: string,
  payload: {
    appid: number;
    config_id: number;
    provider?: string;
    config_name?: string;
    access_mode?: string;
    enabled?: boolean;
    is_default?: boolean;
    proxy_download?: boolean;
    base_url?: string;
    root_path?: string;
    description?: string;
    config_data?: Record<string, unknown>;
  }
) {
  return apiRequest<StorageConfigDetail>("/api/admin/app/storage-config/update", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAppStorageConfig(token: string, appid: number, configId: number) {
  return apiRequest<null>("/api/admin/app/storage-config/delete", {
    method: "POST",
    token,
    body: JSON.stringify({ appid, config_id: configId })
  });
}

export function testAppStorageConfig(token: string, appid: number, configId: number) {
  return apiRequest<Record<string, unknown>>("/api/admin/app/storage-config/test", {
    method: "POST",
    token,
    body: JSON.stringify({ appid, config_id: configId })
  });
}
