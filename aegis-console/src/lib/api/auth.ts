import { apiRequest } from "./client";
import type { AdminLoginResult, AdminSession } from "./types";

export type AdminLoginResponse = {
  accessToken?: string;
  refreshToken: null;
  requiresSecondFactor?: boolean;
  challenge?: { challengeId: string; methods: string[]; expiresAt: string };
  operator?: {
    id?: number;
    account?: string;
    displayName?: string;
    avatar?: string;
    role?: string;
    isSuperAdmin?: boolean;
    authSource?: string;
    assignments?: AdminLoginResult["assignments"];
  };
};

export async function loginAsAdmin(payload: { account: string; password: string; captchaId?: string; captchaAnswer?: string }): Promise<AdminLoginResponse> {
  const result = await apiRequest<AdminLoginResult>("/api/admin/auth/login", {
    method: "POST",
    body: JSON.stringify(payload)
  });

  if (result.requiresSecondFactor) {
    return {
      refreshToken: null,
      requiresSecondFactor: true,
      challenge: result.challenge,
    };
  }

  return {
    accessToken: result.accessToken,
    refreshToken: null,
    operator: {
      id: result.admin?.id,
      account: result.admin?.account || payload.account,
      displayName: result.admin?.displayName || payload.account,
      avatar: result.admin?.avatar,
      role: result.admin?.isSuperAdmin ? "super-admin" : "admin",
      isSuperAdmin: result.admin?.isSuperAdmin,
      authSource: result.admin?.authSource,
      assignments: result.assignments
    }
  };
}

export async function verifyAdminMFA(payload: { challengeId: string; code?: string; recoveryCode?: string }): Promise<AdminLoginResponse> {
  const result = await apiRequest<AdminLoginResult>("/api/admin/auth/verify-mfa", {
    method: "POST",
    body: JSON.stringify(payload)
  });

  return {
    accessToken: result.accessToken,
    refreshToken: null,
    operator: {
      id: result.admin?.id,
      account: result.admin?.account,
      displayName: result.admin?.displayName,
      avatar: result.admin?.avatar,
      role: result.admin?.isSuperAdmin ? "super-admin" : "admin",
      isSuperAdmin: result.admin?.isSuperAdmin,
      authSource: result.admin?.authSource,
      assignments: result.assignments
    }
  };
}

export async function registerAdmin(payload: { account: string; password: string; displayName?: string; email?: string; captchaId?: string; captchaAnswer?: string }) {
  const result = await apiRequest<AdminLoginResult>("/api/admin/auth/register", {
    method: "POST",
    body: JSON.stringify(payload)
  });

  return {
    accessToken: result.accessToken,
    refreshToken: null,
    operator: {
      id: result.admin?.id,
      account: result.admin?.account || payload.account,
      displayName: result.admin?.displayName || payload.account,
      avatar: result.admin?.avatar,
      role: result.admin?.isSuperAdmin ? "super-admin" : "admin",
      isSuperAdmin: result.admin?.isSuperAdmin,
      authSource: result.admin?.authSource,
      assignments: result.assignments
    }
  };
}

export function getAdminMe(token: string) {
  return apiRequest<AdminSession>("/api/admin/auth/me", { token });
}

export function logoutAdmin(token: string) {
  return apiRequest<{ logout: boolean }>("/api/admin/auth/logout", {
    method: "POST",
    token
  });
}
