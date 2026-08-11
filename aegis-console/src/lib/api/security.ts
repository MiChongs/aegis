import { apiRequest } from "./client";
import type {
  AdminSecurityStatus,
  PasskeyRegistrationOptionsResult,
  PasskeySummary,
  RecoveryCodeIssueResult,
  RecoveryCodeSummary,
  TOTPEnrollment
} from "./types";

export function getAdminSecurityStatus(token: string) {
  return apiRequest<AdminSecurityStatus>("/api/admin/profile/security", { token });
}

export function beginAdminTOTPEnrollment(token: string) {
  return apiRequest<TOTPEnrollment>("/api/admin/profile/two-factor/enroll", {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}

export function enableAdminTOTP(
  token: string,
  payload: {
    enrollmentId: string;
    code: string;
  }
) {
  return apiRequest<{
    twoFactor: AdminSecurityStatus["twoFactor"];
    recoveryCodes: RecoveryCodeIssueResult;
  }>("/api/admin/profile/two-factor/enable", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function disableAdminTOTP(
  token: string,
  payload: {
    code?: string;
    recoveryCode?: string;
  }
) {
  return apiRequest<AdminSecurityStatus["twoFactor"]>("/api/admin/profile/two-factor/disable", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminRecoveryCodes(token: string) {
  return apiRequest<RecoveryCodeSummary>("/api/admin/profile/two-factor/recovery-codes", { token });
}

export function generateAdminRecoveryCodes(
  token: string,
  payload: {
    code?: string;
    recoveryCode?: string;
  }
) {
  return apiRequest<RecoveryCodeIssueResult>("/api/admin/profile/two-factor/recovery-codes", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function regenerateAdminRecoveryCodes(
  token: string,
  payload: {
    code?: string;
    recoveryCode?: string;
  }
) {
  return apiRequest<RecoveryCodeIssueResult>("/api/admin/profile/two-factor/recovery-codes/regenerate", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAdminPasskeys(token: string) {
  return apiRequest<PasskeySummary>("/api/admin/profile/passkey", { token });
}

export function beginAdminPasskeyRegistration(token: string) {
  return apiRequest<PasskeyRegistrationOptionsResult>("/api/admin/profile/passkey/register/options", {
    method: "POST",
    token,
    body: JSON.stringify({})
  });
}

export function finishAdminPasskeyRegistration(
  token: string,
  payload: {
    challengeId: string;
    // 浏览器 WebAuthn API 返回的凭证对象，原样 JSON 序列化后透传给后端。
    // 不能用 Record<string, unknown>：标准接口（RegistrationResponseJSON 等）没有索引签名，
    // TypeScript 7 会拒绝该赋值。
    credential: object;
    credentialName?: string;
  }
) {
  return apiRequest<NonNullable<PasskeySummary["items"]>[number]>("/api/admin/profile/passkey/register", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminPasskey(token: string, credentialId: string) {
  return apiRequest<{ deleted: boolean }>(`/api/admin/profile/passkey/${encodeURIComponent(credentialId)}`, {
    method: "DELETE",
    token
  });
}
