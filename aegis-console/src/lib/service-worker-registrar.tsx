"use client";

import { useEffect } from "react";
import { Workbox } from "workbox-window";

/**
 * Service Worker 注册器
 *
 * 窗口侧用 Google 官方 `workbox-window`（npm 依赖，随包打进 bundle，不走 CDN）
 * 处理 SW 生命周期：
 *   - 自动注册 /sw.js
 *   - 旧 SW 等待中新版激活时发 SKIP_WAITING 消息
 *   - 完成安装 / 激活 / 错误事件自动打点，便于 devtools 观察
 *
 * Worker 侧（public/sw.js）是零依赖手写实现，**不 importScripts 任何外部脚本** ——
 * 早先那版从 workbox CDN 拉运行时，境内网络下随机 TLS 失败会让整个 SW
 * 求值阶段抛错（"ServiceWorker script evaluation failed"）、注册直接失败。
 *
 * **生产 & 开发都启用** —— public/sw.js 只拦截图片 / 头像 / 代理下载；
 * 完全不碰 `/_next/**`（HMR WebSocket / 模块）、API JSON 等路径，
 * 因此不会干扰 Turbopack 的热更新。localhost 在所有现代浏览器下都允许 SW 注册。
 */
export function ServiceWorkerRegistrar() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!("serviceWorker" in navigator)) return;

    const wb = new Workbox("/sw.js", { scope: "/" });

    wb.addEventListener("installed", (event) => {
      if (event.isUpdate) {
        console.info("[sw] 新版本已安装（等待激活）");
      } else {
        console.info("[sw] 首次安装成功，图片将走 Cache Storage CacheFirst 策略");
      }
    });

    wb.addEventListener("waiting", () => {
      // 新版 SW 在等待：发消息让它立即接管，避免用户看到两个版本
      wb.messageSkipWaiting();
    });

    wb.addEventListener("controlling", () => {
      console.info("[sw] 新版本已接管");
    });

    wb.addEventListener("redundant", () => {
      console.warn("[sw] Service Worker 已作废");
    });

    wb.register().catch((err) => {
      console.error("[sw] 注册失败：", err);
    });
  }, []);

  return null;
}
