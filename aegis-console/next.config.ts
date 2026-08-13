import { networkInterfaces } from "node:os";
import { resolve } from "node:path";
import type { NextConfig } from "next";

// ── 项目根：就是本目录 ──
// pnpm-lock.yaml 与 pnpm-workspace.yaml 都在 aegis-console/ 下（见 pnpm-workspace.yaml
// 里的说明），所以这里不能往上指。曾经写成 `../../`（仓库之外的 userSystem/），
// 两个后果：Turbopack 的解析与追踪边界莫名其妙地覆盖了整个仓库同级目录；
// 而在容器里这个相对路径会算成 `/`，等于告诉 Turbopack "整个文件系统都是项目"。
const projectRoot = import.meta.dirname;

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
    root: projectRoot
  },
  // ── 容器镜像用的自包含产物 ──
  // 只在 Docker 构建时开（NEXT_OUTPUT=standalone）：它要对整棵依赖树做文件追踪，
  // 平时本地 build 用不上这份产物，白等这一段没有意义。
  //
  // 追踪是从应用代码出发的，因此**够不到客户端 IP 预载那两个脚本**（没有任何页面
  // 引用它们，它们由 node --import 装载），也够不到它们依赖的 ipaddr.js。
  // 漏掉的表现极其隐蔽：容器照常起、页面照常开，只是全站请求的客户端 IP 都变成
  // 控制台自己的地址，限流 / 封禁 / 地理风控 / 审计集体失准，功能上完全看不出来。
  ...(process.env.NEXT_OUTPUT === "standalone"
    ? {
        output: "standalone" as const,
        outputFileTracingRoot: projectRoot,
        outputFileTracingIncludes: {
          // 注意 ipaddr.js 那一条：outputFileTracingIncludes 只**复制**列出的文件，
          // 不会再去追它们自己的 import，所以依赖得手写。
          "/**": [
            "./scripts/forwarded-headers.mjs",
            "./scripts/forwarded-headers-preload.mjs",
            "./node_modules/ipaddr.js/**"
          ]
        }
      }
    : {}),
  // ── 类型检查交给 `tsc --noEmit`（TS 7），不在 build 里做 ──
  // build 期那一遍走 TS 6 的 JS API（见下方 useTypeScriptCli），而 TS 7 是 Go 重写的
  // 原生编译器：同一份代码实测差**接近一个数量级**，查的却是同一件事，留慢的那个没有意义。
  // 关掉不等于不检查：`pnpm build` 的第一步就是 `tsc --noEmit`，类型不过一样构建不出来，
  // 而且报错来得更早 —— 不用等 Turbopack 先编译完。
  typescript: {
    ignoreBuildErrors: true
  },
  experimental: {
    // 只列 Next 内置清单里没有的桶文件包（lucide-react / recharts / date-fns 等已内置）。
    // 这几个都是"导入一个成员要先解析整包"的重灾区：simple-icons 系两个包各有
    // 3000+ 图标，radix-ui 统一包重导出 32 个 primitive，@turf/turf 汇总上百个模块。
    optimizePackageImports: [
      "radix-ui",
      "@icons-pack/react-simple-icons",
      "simple-icons",
      "@turf/turf"
    ],
    // ── TypeScript 7 / 6 并存所必需（默认为 true，此处必须显式关闭） ──
    // 本项目按 TS 7 官方方案做 side-by-side：包名 `typescript` 别名到
    // @typescript/typescript6（提供 TS 6 的 JS API，供 typescript-eslint 使用），
    // 真正的 TS 7 由 @typescript/native 提供 `tsc` 二进制（pnpm typecheck 走它）。
    //
    // useTypeScriptCli=true 时 Next 走 CLI 模式，要找 `typescript/bin/tsc`，
    // 而 TS 6 兼容包只提供 `bin/tsc6`，会误报「typescript 未安装」并触发自动安装
    // ——把上面那条 alias 覆盖掉。上面的 ignoreBuildErrors 只是不做检查，
    // Next 仍会探测这个包，所以这一项照样不能删。
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
