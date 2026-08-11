import { appConfig } from "@/lib/env";

type ApiEnvelope<T> = {
  code?: number;
  message?: string;
  requestId?: string;
  data?: T;
};

type RequestOptions = RequestInit & {
  token?: string | null;
};

type QueryValue = string | number | boolean | null | undefined;

export class ApiError extends Error {
  code?: number;
  requestId?: string;
  status?: number;

  constructor(message: string, options?: { code?: number; requestId?: string; status?: number }) {
    super(message);
    this.name = "ApiError";
    this.code = options?.code;
    this.requestId = options?.requestId;
    this.status = options?.status;
  }
}

function joinUrl(baseUrl: string, path: string) {
  if (/^https?:\/\//.test(path)) {
    return path;
  }
  return `${baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
}

/** 把相对路径拼成完整 API 地址。二进制下载不能走 apiRequest，需要自己 fetch。 */
export function joinApiUrl(path: string) {
  return joinUrl(appConfig.apiBaseUrl, path);
}

export function buildQuery(params: Record<string, QueryValue>) {
  const search = new URLSearchParams();

  Object.entries(params).forEach(([key, value]) => {
    if (value === null || typeof value === "undefined" || value === "") {
      return;
    }
    search.set(key, String(value));
  });

  const result = search.toString();
  return result ? `?${result}` : "";
}

async function parsePayload<T>(response: Response) {
  const contentType = response.headers.get("content-type") || "";
  if (contentType.includes("application/json")) {
    return (await response.json()) as ApiEnvelope<T> | T;
  }
  return (await response.text()) as T;
}

export async function apiRequest<T>(path: string, init: RequestOptions = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const isFormData = typeof FormData !== "undefined" && init.body instanceof FormData;

  if (init.body && !isFormData && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  headers.set("Accept", "application/json");

  if (init.token) {
    headers.set("Authorization", `Bearer ${init.token}`);
    headers.set("X-Admin-Token", init.token);
  }

  const response = await fetch(joinUrl(appConfig.apiBaseUrl, path), {
    ...init,
    headers,
    cache: "no-store"
  });

  const payload = await parsePayload<T>(response);

  if (!response.ok) {
    if (typeof payload === "object" && payload !== null && "message" in payload) {
      const envelope = payload as ApiEnvelope<T>;
      throw new ApiError(envelope.message || "请求失败", {
        code: envelope.code,
        requestId: envelope.requestId,
        status: response.status
      });
    }

    throw new ApiError("请求失败", { status: response.status });
  }

  if (typeof payload === "object" && payload !== null) {
    const envelope = payload as ApiEnvelope<T>;
    if (typeof envelope.code === "number" && envelope.code >= 40000) {
      throw new ApiError(envelope.message || "请求失败", {
        code: envelope.code,
        requestId: envelope.requestId,
        status: response.status
      });
    }

    if (typeof envelope.data !== "undefined") {
      return envelope.data;
    }
  }

  return payload as T;
}
