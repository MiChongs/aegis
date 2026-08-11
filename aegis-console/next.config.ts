import { networkInterfaces } from "node:os";
import { resolve } from "node:path";
import type { NextConfig } from "next";

// pnpm workspace root (where pnpm-lock.yaml lives)
const workspaceRoot = resolve(import.meta.dirname, "../..");

// ── 开发环境：收集本机所有 IPv4 网卡地址作为允许的 dev origin ──
// Next.js 16 的 allowedDevOrigins 只接受精确 host（不支持 CIDR / 通配）。
// 启动 `next dev` 时自动枚举 RFC1918 内网地址，无需手动维护。
// 额外来源可通过 NEXT_DEV_ALLOWED_ORIGINS 环境变量追加（逗号分隔）。
function collectLanHosts(): string[] {
  const hosts = new Set<string>(["localhost", "127.0.0.1"]);
  try {
    const ifaces = networkInterfaces();
    for (const list of Object.values(ifaces)) {
      if (!list) continue;
      for (const info of list) {
        if (info.family === "IPv4" && !info.internal) {
          hosts.add(info.address);
        }
      }
    }
  } catch {
    // 某些受限环境下 networkInterfaces 可能抛错，忽略即可
  }
  const extra = process.env.NEXT_DEV_ALLOWED_ORIGINS?.split(",").map((s) => s.trim()).filter(Boolean) ?? [];
  for (const h of extra) hosts.add(h);
  return [...hosts];
}

// ── 后端地址：仅 Next.js 服务端进程使用，绝不通过 NEXT_PUBLIC_* 暴露给浏览器 ──
// 浏览器只看到相对路径 /api/* —— 由 Next.js server 反代到真正的 Go 后端。
// 好处：
//   1. 局域网访问无需切 API 地址，浏览器天然同源
//   2. 不暴露后端内网 host/端口
//   3. 规避 CORS/WebSocket 跨域/HTTPS 混合内容等麻烦
const backendOrigin = (process.env.AEGIS_API_BACKEND || "http://127.0.0.1:8088").replace(/\/$/, "");

const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // 仅 `next dev` 读取；next build / next start 忽略该字段。
  allowedDevOrigins: collectLanHosts(),
  turbopack: {
    root: workspaceRoot
  },
  experimental: {
    optimizePackageImports: ["lucide-react"],
    // ── TypeScript 7 / 6 并存所必需（默认为 true，此处必须显式关闭） ──
    // 本项目按 TS 7 官方方案做 side-by-side：包名 `typescript` 别名到
    // @typescript/typescript6（提供 TS 6 的 JS API，供 typescript-eslint 使用），
    // 真正的 TS 7 由 @typescript/native 提供 `tsc` 二进制（pnpm typecheck 走它）。
    //
    // useTypeScriptCli=true 时 Next 走 CLI 模式，要找 `typescript/bin/tsc`，
    // 而 TS 6 兼容包只提供 `bin/tsc6`，会误报「typescript 未安装」并触发自动安装。
    // 关闭后 Next 改用 API 模式（`typescript/lib/typescript.js`），该文件存在，
    // build 期类型检查由 TS 6 API 完成；TS 7 的权威校验由 `pnpm typecheck` 承担。
    useTypeScriptCli: false
  },
  // ── next/image 配置 ──
  // 管理员可粘贴任意外链 / 同源代理（/api/storage/proxy/*）作为 Banner 图片，
  // 因此白名单放得较宽：允许所有 HTTP / HTTPS 主机名；同源相对路径天然放行。
  // 若将来要收紧，改为具体 hostname 即可。
  images: {
    remotePatterns: [
      { protocol: "https", hostname: "**" },
      { protocol: "http", hostname: "**" }
    ],
    // 允许的 quality 级别（Next.js 16 起强制声明；默认只允许 75）
    qualities: [60, 75, 78, 85, 90],
    formats: ["image/avif", "image/webp"],
    minimumCacheTTL: 60,
    dangerouslyAllowSVG: true,
    contentDispositionType: "inline",
    contentSecurityPolicy: "default-src 'self'; script-src 'none'; sandbox;"
  },
  // 同源反向代理：浏览器请求 /api/* 由 Next.js 转发到 AEGIS_API_BACKEND/api/*。
  // WebSocket 升级（/api/ws）会随 HTTP upgrade 一并透传。
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${backendOrigin}/api/:path*` },
      // 开发者门户 /developers/api 直接消费后端实时生成的 OpenAPI 规范，
      // 必须同源代理，否则浏览器会打到 Next.js 自身并拿到 404。
      { source: "/openapi.json", destination: `${backendOrigin}/openapi.json` },
      { source: "/healthz", destination: `${backendOrigin}/healthz` },
      { source: "/readyz", destination: `${backendOrigin}/readyz` }
    ];
  }
};

export default nextConfig;
