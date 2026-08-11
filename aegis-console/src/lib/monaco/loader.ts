"use client";

import { loader } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";

// ─────────────────────────────────────────────────────────────────────
//  Monaco 的唯一加载入口 —— 自托管，不走公网 CDN
//
//  为什么不能只调 loader.config({ paths })：
//    @monaco-editor/loader 只认第一次生效的配置，且 Next 会把它复制进多个
//    chunk，谁先调用 loader.init() 就替所有人决定 monaco 从哪来。实测下来
//    配置常常输掉这场竞争，静默回退到它内置的默认值
//    (cdn.jsdelivr.net/npm/monaco-editor@0.55.1/min/vs)。
//    这个失效极难发现 —— 旧代码里手写的 CDN 常量恰好与该默认值一字不差。
//
//  更要命的是那份 CDN 构建里 `monaco.languages.typescript` 根本不存在，
//  也就没有 TypeScript 语言服务：补全、诊断、悬浮文档全部失效。
//
//  所以这里改为「自己用 monaco 自带的 AMD loader 从 /monaco/vs 加载，
//  再把拿到的实例交给 @monaco-editor/react」。传入实例后它不会再去碰 CDN，
//  加载时序也就与模块求值顺序无关了。
//
//  产物由 scripts/sync-monaco-assets.mjs 从 node_modules/monaco-editor 同步
//  （predev / prebuild 自动执行），版本与 package.json 严格一致。
// ─────────────────────────────────────────────────────────────────────

const MONACO_VS_PATH = "/monaco/vs";

type AmdRequire = {
  (modules: string[], onLoad: () => void, onError?: (error: unknown) => void): void;
  config(options: Record<string, unknown>): void;
};

let monacoPromise: Promise<typeof MonacoNS> | null = null;

/**
 * 加载自托管 Monaco，返回 monaco 命名空间。幂等，可在任意处调用。
 *
 * 组件应当在这个 Promise resolve 之后再渲染 `<Editor>`：
 * 那时 `loader.config({ monaco })` 已生效，Editor 内部的 init 会直接复用实例。
 */
export function loadMonaco(): Promise<typeof MonacoNS> {
  if (monacoPromise) return monacoPromise;
  if (typeof window === "undefined") {
    return Promise.reject(new Error("Monaco 只能在浏览器端加载"));
  }

  const bootstrapped = (window as unknown as { __aegisMonaco?: Promise<typeof MonacoNS> }).__aegisMonaco;
  if (bootstrapped) {
    // 常规路径：root layout 的 beforeInteractive 脚本已经在加载本地 monaco
    monacoPromise = bootstrapped;
    void monacoPromise
      .then((monaco) => loader.config({ monaco }))
      .catch(() => {
        monacoPromise = null;
      });
    return monacoPromise;
  }

  // 兜底：引导脚本因故未执行时自行加载，行为与其一致
  monacoPromise = new Promise<typeof MonacoNS>((resolve, reject) => {
    const globalScope = window as unknown as { require?: AmdRequire; monaco?: typeof MonacoNS };

    const boot = () => {
      const amdRequire = globalScope.require;
      if (!amdRequire?.config) {
        reject(new Error("Monaco AMD loader 未就绪"));
        return;
      }
      // 同 layout.tsx：不要配置 "vs/nls"，否则 editor.main 永远不会 resolve
      amdRequire.config({ paths: { vs: MONACO_VS_PATH } });
      amdRequire(
        ["vs/editor/editor.main"],
        () => {
          if (globalScope.monaco) resolve(globalScope.monaco);
          else reject(new Error("Monaco 加载完成但未挂载到 window"));
        },
        (error) => reject(error instanceof Error ? error : new Error(String(error)))
      );
    };

    // loader.js 会声明全局变量，重复注入会抛 "_amdLoaderGlobal has already been declared"
    if (globalScope.require?.config) {
      boot();
      return;
    }
    const script = document.createElement("script");
    script.src = `${MONACO_VS_PATH}/loader.js`;
    script.async = true;
    script.onload = boot;
    script.onerror = () => reject(new Error("加载 /monaco/vs/loader.js 失败"));
    document.head.appendChild(script);
  });

  // 把实例交给 @monaco-editor/react —— 之后它不会再发起任何 CDN 请求
  void monacoPromise
    .then((monaco) => {
      loader.config({ monaco });
    })
    .catch(() => {
      // 允许后续重试
      monacoPromise = null;
    });

  return monacoPromise;
}

/** 预热：首访即开始下载，真正打开编辑器时已经就位。失败静默，组件各自回退占位。 */
export function ensureMonacoLoader() {
  if (typeof window === "undefined") return;
  void loadMonaco().catch(() => {});
}
