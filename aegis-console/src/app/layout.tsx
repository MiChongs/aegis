import type { Metadata } from "next";
import NextTopLoader from "nextjs-toploader";
import { Toaster } from "@/components/ui/sonner";
import { IntroOverlay } from "@/components/intro-overlay";
import { Providers } from "@/lib/providers";
import "@fontsource/oxanium/700.css";
import "yet-another-react-lightbox/styles.css";
import "./globals.css";

export const metadata: Metadata = {
  title: "Aegis Console",
  description: "Aegis 统一控制台前端"
};

// ── 自托管 Monaco 引导 ────────────────────────────────────────────────
// 必须早于任何 React 代码：@monaco-editor/loader 会在自己的 init() 里注入
// 公网 CDN 的 loader.js，而 AMD 一旦缓存了 vs/editor/editor.main，之后再改
// 路径也没用（还会因重复声明 _amdLoaderGlobal 直接报错）。更关键的是那份
// CDN 构建不含 monaco.languages.typescript，脚本编辑器的补全 / 诊断会整体失效。
//
// 这里在 <head> 解析阶段就用本地 loader 建好 window.require 并开始加载
// editor.main，结果挂到 window.__aegisMonaco 上供 @/lib/monaco/loader 复用。
// loader.js 与 editor.main 都是异步下载，不阻塞首屏。
//
// 两条不能改回去的写法：
//  1. **不要用 next/script 的 beforeInteractive**。它在 app router 下只渲染一段
//     `(self.__next_s=…).push([src])`，真正的 <script src> 是 Next 运行时后来
//     createElement 追加到 body 的 —— 动态插入的外链脚本默认 async，紧随其后的
//     内联引导会先跑，那时 window.require 还不存在，引导直接空转。
//     而且这两段 <script> 作为 <html> 的直接子节点属于非法 HTML，
//     React 19 会报 "In HTML, <script> cannot be a child of <html>" 并水合失败。
//  2. **不要配置 "vs/nls" 汉化**。monaco 0.56 的 nls/lang/*.js 是普通脚本
//     （只给 globalThis._VSCODE_NLS_MESSAGES 赋值，没有 define()），而
//     nls.messages-loader 会把它当 AMD 模块 require —— 于是永远挂起，连带
//     vs/editor/editor.main 一起不 resolve，TypeScript 语言服务就没了。
const MONACO_BOOT = `
(function () {
  var VS = "/monaco/vs";
  if (window.__aegisMonaco) return;
  var booting = new Promise(function (resolve, reject) {
    var script = document.createElement("script");
    script.src = VS + "/loader.js";
    script.async = true;
    script.onload = function () {
      var amd = window.require;
      if (!amd || !amd.config) { reject(new Error("Monaco AMD loader 未就绪")); return; }
      amd.config({ paths: { vs: VS } });
      amd(["vs/editor/editor.main"], function () { resolve(window.monaco); }, reject);
    };
    script.onerror = function () { reject(new Error("加载 " + VS + "/loader.js 失败")); };
    document.head.appendChild(script);
  });
  // 失败即撤下句柄：@/lib/monaco/loader 的兜底路径会自己重试；
  // 这里同时消化掉 rejection，避免变成一条 unhandledrejection。
  booting.catch(function () {
    if (window.__aegisMonaco === booting) { delete window.__aegisMonaco; }
  });
  window.__aegisMonaco = booting;
})();
`;

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN" data-scroll-behavior="smooth" suppressHydrationWarning>
      <head>
        <script id="aegis-monaco-boot" dangerouslySetInnerHTML={{ __html: MONACO_BOOT }} />
      </head>
      <body className="min-h-screen bg-background text-foreground">
        {/* 顶部进度条 — 原生 Zinc 单色调设计（Radix 风）
            - color 只作 fallback；真正的动画渐变、主题联动、层级提升由 globals.css 的 #nprogress 覆盖完成
            - zIndex 显式提到 2147483000，确保浮动在 Tooltip(9999)/IntroOverlay(950) 等之上 */}
        <NextTopLoader
          color="var(--foreground)"
          initialPosition={0.08}
          crawlSpeed={240}
          height={2}
          crawl
          showSpinner={false}
          easing="cubic-bezier(0.22, 1, 0.36, 1)"
          speed={420}
          shadow={false}
          zIndex={2147483000}
        />
        {/* IntroOverlay 挂在 Providers 之外、最先渲染，保证首屏立刻覆盖，
            不被 ThemeProvider / QueryClientProvider 的水合时序影响。 */}
        <IntroOverlay />
        <Providers>
          {children}
          <Toaster />
        </Providers>
      </body>
    </html>
  );
}
