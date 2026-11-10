function base64UrlToBytes(value: string) {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized + "=".repeat((4 - (normalized.length % 4 || 4)) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function bytesToBase64Url(value: ArrayBuffer | Uint8Array) {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value);
  let binary = "";
  bytes.forEach((item) => {
    binary += String.fromCharCode(item);
  });
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function mapCreationOptions(input: Record<string, unknown>): PublicKeyCredentialCreationOptions {
  const publicKey = structuredClone(input) as Record<string, any>;
  publicKey.challenge = base64UrlToBytes(String(publicKey.challenge));

  if (publicKey.user?.id) {
    publicKey.user.id = base64UrlToBytes(String(publicKey.user.id));
  }

  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map((item: Record<string, unknown>) => ({
      ...item,
      id: base64UrlToBytes(String(item.id))
    }));
  }

  return publicKey as unknown as PublicKeyCredentialCreationOptions;
}

function serializeRegistrationCredential(credential: PublicKeyCredential) {
  const response = credential.response as AuthenticatorAttestationResponse & {
    getPublicKeyAlgorithm?: () => number;
    getAuthenticatorData?: () => ArrayBuffer;
    getPublicKey?: () => ArrayBuffer | null;
  };
  return {
    id: credential.id,
    rawId: bytesToBase64Url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bytesToBase64Url(response.clientDataJSON),
      attestationObject: bytesToBase64Url(response.attestationObject),
      transports: typeof response.getTransports === "function" ? response.getTransports() : undefined,
      publicKeyAlgorithm: typeof response.getPublicKeyAlgorithm === "function"
        ? response.getPublicKeyAlgorithm()
        : undefined,
      authenticatorData: typeof response.getAuthenticatorData === "function"
        ? bytesToBase64Url(response.getAuthenticatorData())
        : undefined,
      publicKey: typeof response.getPublicKey === "function"
        ? (() => {
            const publicKey = response.getPublicKey();
            return publicKey ? bytesToBase64Url(publicKey) : undefined;
          })()
        : undefined
    },
    clientExtensionResults: credential.getClientExtensionResults()
  };
}

export function passkeyRegistrationSupported() {
  return typeof window !== "undefined" && typeof window.PublicKeyCredential !== "undefined" && !!navigator.credentials;
}

/**
 * WebAuthn 只在安全上下文里可用：HTTPS，或 localhost / 127.0.0.1 这类
 * 浏览器视为「潜在可信」的地址。走局域网 IP（http://192.168.x.x:3000）时
 * `navigator.credentials` 干脆不存在，报错却只说"不支持"——那会让人去查浏览器版本。
 */
export function passkeySecureContextIssue(): string | null {
  if (typeof window === "undefined") return null;
  if (window.isSecureContext) return null;
  return "Passkey 需在 HTTPS 或 localhost 下使用";
}

/**
 * 把 WebAuthn 抛出的 DOMException 翻成能照着做的中文。
 *
 * 原始文案是英文的规范术语（"registrable domain suffix"…），既不说是哪个值不对，
 * 也不说去哪里改。而 RP ID 不匹配恰恰是本项目最常撞上的一种：服务端配的
 * RP ID 跟不上访问域名时，每一次绑定都会停在这一句上。
 */
export function describePasskeyError(error: unknown): string {
  const host = typeof window !== "undefined" ? window.location.hostname : "";
  const name = error instanceof DOMException ? error.name : "";

  switch (name) {
    case "SecurityError":
      return `Passkey RP ID 与当前域名 ${host} 不匹配`;
    case "NotAllowedError":
      return "已取消或已超时";
    case "InvalidStateError":
      return "该设备已绑定过 Passkey";
    case "NotSupportedError":
      return "当前设备不支持该 Passkey 参数";
    case "AbortError":
      return "操作已中断";
    case "ConstraintError":
      return "认证器不满足要求";
    default:
      break;
  }

  if (error instanceof Error && error.message) return error.message;
  return "Passkey 操作失败";
}

export async function createPasskeyRegistrationCredential(options: Record<string, unknown>) {
  const insecure = passkeySecureContextIssue();
  if (insecure) {
    throw new Error(insecure);
  }
  if (!passkeyRegistrationSupported()) {
    throw new Error("当前环境不支持 Passkey");
  }

  // 后端返回 { publicKey: { challenge, rp, user, ... } }，提取 publicKey 部分
  const creationOptions = (options.publicKey as Record<string, unknown>) || options;

  const credentialAPI = PublicKeyCredential as typeof PublicKeyCredential & {
    parseCreationOptionsFromJSON?: (options: Record<string, unknown>) => PublicKeyCredentialCreationOptions;
  };
  const publicKey =
    typeof credentialAPI.parseCreationOptionsFromJSON === "function"
      ? credentialAPI.parseCreationOptionsFromJSON(creationOptions)
      : mapCreationOptions(creationOptions);

  let credential: PublicKeyCredential | null;
  try {
    credential = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
  } catch (cause) {
    throw new Error(describePasskeyError(cause), { cause });
  }

  if (!credential) {
    throw new Error("Passkey 创建失败");
  }

  const credentialWithJSON = credential as PublicKeyCredential & {
    toJSON?: () => Record<string, unknown>;
  };
  if (typeof credentialWithJSON.toJSON === "function") {
    return credentialWithJSON.toJSON();
  }

  return serializeRegistrationCredential(credential);
}

// ── 登录（断言） ──

function mapRequestOptions(input: Record<string, unknown>): PublicKeyCredentialRequestOptions {
  const publicKey = structuredClone(input) as Record<string, any>;
  publicKey.challenge = base64UrlToBytes(String(publicKey.challenge));
  if (Array.isArray(publicKey.allowCredentials)) {
    publicKey.allowCredentials = publicKey.allowCredentials.map((item: Record<string, unknown>) => ({
      ...item,
      id: base64UrlToBytes(String(item.id))
    }));
  }
  return publicKey as unknown as PublicKeyCredentialRequestOptions;
}

function serializeAssertionCredential(credential: PublicKeyCredential) {
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: bytesToBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: (credential as PublicKeyCredential & { authenticatorAttachment?: string | null })
      .authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: bytesToBase64Url(response.clientDataJSON),
      authenticatorData: bytesToBase64Url(response.authenticatorData),
      signature: bytesToBase64Url(response.signature),
      userHandle: response.userHandle ? bytesToBase64Url(response.userHandle) : undefined
    },
    clientExtensionResults: credential.getClientExtensionResults()
  };
}

/**
 * 浏览器是否支持「条件式 UI」：在账号输入框的自动填充建议里列出通行密钥
 * （输入框需带 `autocomplete="username webauthn"`）。
 */
export async function passkeyAutofillAvailable(): Promise<boolean> {
  if (!passkeyRegistrationSupported() || passkeySecureContextIssue()) return false;
  const api = PublicKeyCredential as typeof PublicKeyCredential & {
    isConditionalMediationAvailable?: () => Promise<boolean>;
  };
  if (typeof api.isConditionalMediationAvailable !== "function") return false;
  try {
    return await api.isConditionalMediationAvailable();
  } catch {
    return false;
  }
}

/**
 * 调起通行密钥验证，返回按 WebAuthn JSON 编码的断言（可直接交给服务端）。
 *
 * - `mediation: "conditional"` 不弹窗，挂在账号输入框的自动填充上，用户选了才返回；
 * - 不传时弹出系统的通行密钥选择框。
 * 同一时刻只能有一个挂起的请求：弹窗之前要先 abort 掉挂着的条件式请求。
 * WebAuthn 自己的异常（取消、超时、被中断）原样抛出，调用方用 `describePasskeyError` 翻译。
 */
export async function getPasskeyAssertion(
  options: Record<string, unknown>,
  init: { mediation?: "conditional"; signal?: AbortSignal } = {}
): Promise<Record<string, unknown>> {
  const insecure = passkeySecureContextIssue();
  if (insecure) throw new Error(insecure);
  if (!passkeyRegistrationSupported()) throw new Error("当前环境不支持通行密钥");

  // 后端返回 { publicKey: { challenge, rpId, userVerification, ... } }
  const requestOptions = (options.publicKey as Record<string, unknown>) || options;
  const api = PublicKeyCredential as typeof PublicKeyCredential & {
    parseRequestOptionsFromJSON?: (options: Record<string, unknown>) => PublicKeyCredentialRequestOptions;
  };
  const publicKey =
    typeof api.parseRequestOptionsFromJSON === "function"
      ? api.parseRequestOptionsFromJSON(requestOptions)
      : mapRequestOptions(requestOptions);

  const credential = (await navigator.credentials.get({
    publicKey,
    signal: init.signal,
    ...(init.mediation ? { mediation: init.mediation as CredentialMediationRequirement } : {})
  })) as PublicKeyCredential | null;
  if (!credential) throw new Error("通行密钥验证未完成");

  const withJSON = credential as PublicKeyCredential & { toJSON?: () => Record<string, unknown> };
  return typeof withJSON.toJSON === "function"
    ? (withJSON.toJSON() as unknown as Record<string, unknown>)
    : serializeAssertionCredential(credential);
}
