// 基础配置：默认走 Next.js 同源反向代理（见 next.config.ts 的 rewrites）
//
// 默认行为：
//   - apiBaseUrl = ""（空串 = 相对路径，走当前 host）
//   - wsUrl 基于 window.location，自动 http→ws / https→wss
//
// 显式直连（绕过代理）：
//   设 NEXT_PUBLIC_AEGIS_API_BASE_URL=https://api.example.com
//   / NEXT_PUBLIC_AEGIS_WS_URL=wss://api.example.com/api/ws
//   —— 通常仅在需要前后端彻底分离部署的场景下使用。

const rawApiBase = process.env.NEXT_PUBLIC_AEGIS_API_BASE_URL?.replace(/\/$/, "") ?? "";
const rawWsBase = process.env.NEXT_PUBLIC_AEGIS_WS_URL?.replace(/\/$/, "") ?? "";

function resolveApiBaseUrl(): string {
  // 显式配置了直连地址就直接用（交给使用方自行承担 CORS / 混合内容责任）
  return rawApiBase;
}

function resolveWsUrl(): string {
  if (rawWsBase) return rawWsBase;
  // 未显式配置时，从当前页面派生同源 WS
  if (typeof window !== "undefined") {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${proto}//${window.location.host}/api/ws`;
  }
  // SSR fallback（不会真正被 WebSocket 客户端使用）
  return "ws://127.0.0.1/api/ws";
}

export const appConfig = {
  consoleName: process.env.NEXT_PUBLIC_AEGIS_CONSOLE_NAME || "Aegis Console",
  platformName: process.env.NEXT_PUBLIC_AEGIS_PLATFORM_NAME || "Aegis",
  environment: process.env.NEXT_PUBLIC_AEGIS_ENVIRONMENT || "Development",
  get apiBaseUrl() {
    return resolveApiBaseUrl();
  },
  get wsUrl() {
    return resolveWsUrl();
  }
};
