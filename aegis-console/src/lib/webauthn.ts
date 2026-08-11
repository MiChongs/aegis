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

export async function createPasskeyRegistrationCredential(options: Record<string, unknown>) {
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

  const credential = (await navigator.credentials.create({
    publicKey
  })) as PublicKeyCredential | null;

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
