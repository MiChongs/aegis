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
  return `当前地址 ${window.location.origin} 不是安全上下文，浏览器不会提供 Passkey。请改用 HTTPS，或从 http://localhost / http://127.0.0.1 打开控制台。`;
}

/**
 * 把 WebAuthn 抛出的 DOMException 翻成能照着做的中文。
 *
 * 原始文案是英文的规范术语（"registrable domain suffix"…），既不说是哪个值不对，
 * 也不说去哪里改。而 RP ID 不匹配恰恰是本项目最常撞上的一种：服务端配的
 * RP ID 跟不上访问域名时，每一次绑定都会停在这一句上。
 */
export function describePasskeyError(error: unknown): string {
  const origin = typeof window !== "undefined" ? window.location.origin : "";
  const host = typeof window !== "undefined" ? window.location.hostname : "";
  const name = error instanceof DOMException ? error.name : "";

  switch (name) {
    case "SecurityError":
      return `Passkey 的 RP ID 与当前访问地址不匹配（当前域名 ${host}）。请到「平台配置 · 系统安全 · Passkey」把 RP ID 留空（留空即跟随访问域名）或填成 ${host}，并确认允许来源里有 ${origin}。`;
    case "NotAllowedError":
      return "已取消，或等待超时。请重新点击并在弹窗里完成指纹 / 面容 / PIN 验证。";
    case "InvalidStateError":
      return "这台设备上已经为该账号绑定过 Passkey，不能重复绑定。";
    case "NotSupportedError":
      return "当前设备或浏览器不支持所要求的 Passkey 参数。";
    case "AbortError":
      return "操作已被中断。";
    case "ConstraintError":
      return "当前认证器不满足要求（多为要求可发现凭据或用户验证但设备不支持）。";
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
