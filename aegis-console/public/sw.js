/**
 * Aegis Service Worker —— 零依赖，不引用任何外部脚本
 *
 * 职责：**最极致速度的图片缓存** —— Service Worker 工作在网络栈之上，
 *   请求根本不经过主线程就能从 Cache Storage API 命中，比 IndexedDB 还快一级。
 *
 * 为什么不再用 Workbox（不要改回去）：
 *   旧版本第一行是 importScripts("https://storage.googleapis.com/workbox-cdn/…/workbox-sw.js")。
 *   workbox-sw 拿到的 `self.workbox` 是个惰性 Proxy —— 读 `.core` / `.routing` 时才会再
 *   importScripts 各个 workbox-*.prod.js。境内网络下这些子请求会随机 TLS 失败，
 *   异常抛在 SW 顶层求值阶段，浏览器只报一句
 *   "ServiceWorker script evaluation failed"，注册整体失败、缓存一点没生效。
 *   而 `if (self.workbox) … else 打日志` 那条兜底分支根本轮不到执行：
 *   workbox 对象是存在的，炸的是读它属性的那一刻。
 *   本文件真正用到的只有 CacheFirst + 条数/时效上限，手写反而更短，
 *   也与项目「出网依赖必须可控」的一贯做法一致（自托管 Monaco、出海代理网关同理）。
 *
 * 生产 & 开发环境都启用。为了不扰动 Turbopack HMR，本 SW:
 *   - 仅拦截 image destination / /api/storage/proxy/* / /api/avatar/* / 字体
 *   - **不匹配 HMR 通道、RSC 流、模块 chunk**（它们都不是 image/font destination）
 *   - **完全不匹配 API JSON**（/api 下除了 storage/proxy 和 avatar 都放行）
 *
 * 缓存层级：
 *   L1 浏览器 memory cache      —— SW 命中后仍可走此层
 *   L2 Cache Storage API（本文件） —— CacheFirst，持久到磁盘、跨重启保留
 *   L3 网络                     —— 只有未命中才走到这里
 */

// 入库时间戳写在响应头上（Cache Storage 本身不记录写入时间）。
// 跨域 no-cors 的 opaque 响应读不到也改不了头，只能退化成「只受条数上限约束」。
const CACHED_AT = "x-aegis-cached-at";

const BUCKETS = [
  {
    // ── 图片（含 banner、头像、content image）：CacheFirst，30 天 ──
    //   匹配条件非常严格 —— 只接 image destination 或我们清楚的代理/头像路径
    cacheName: "aegis-images",
    maxEntries: 300,
    maxAgeMs: 30 * 24 * 60 * 60 * 1000,
    match: (request, url) =>
      request.destination === "image" ||
      url.pathname.startsWith("/api/storage/proxy/") ||
      url.pathname.startsWith("/api/avatar/")
  },
  {
    // ── 字体：CacheFirst，1 年 ──
    //   字体文件极稳定，哈希指纹已在 URL 里，永远缓存一年
    cacheName: "aegis-fonts",
    maxEntries: 30,
    maxAgeMs: 365 * 24 * 60 * 60 * 1000,
    match: (request) => request.destination === "font"
  }
];

// 立即激活：新版 SW 跳过 waiting，不让旧版拖着
self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  // Range 请求（拖动进度条等）交回网络：CacheFirst 会拿整份响应去答复 206，语义不对
  if (request.headers.has("range")) return;

  let url;
  try {
    url = new URL(request.url);
  } catch {
    return;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return;

  const bucket = BUCKETS.find((item) => item.match(request, url));
  if (!bucket) return;

  event.respondWith(cacheFirst(event, bucket));
});

async function cacheFirst(event, bucket) {
  const cache = await caches.open(bucket.cacheName);
  const cached = await cache.match(event.request);
  if (cached && !isStale(cached, bucket.maxAgeMs)) return cached;

  try {
    const response = await fetch(event.request);
    // 200 = 正常同源 / CORS 响应；0 = 跨域 no-cors 的 opaque 响应（外链图片走这条）
    if (response && (response.status === 200 || response.status === 0)) {
      event.waitUntil(store(cache, event.request, response.clone(), bucket));
    }
    return response;
  } catch (error) {
    // 断网时宁可给一份过期内容，也好过让 <img> 直接碎掉
    if (cached) return cached;
    throw error;
  }
}

async function store(cache, request, response, bucket) {
  try {
    await cache.put(request, await stamp(response));
    await trim(cache, bucket.maxEntries);
  } catch (error) {
    if (error && error.name === "QuotaExceededError") {
      // 配额耗尽：整桶丢掉重来，等价于 Workbox 的 purgeOnQuotaError
      await caches.delete(bucket.cacheName);
      return;
    }
    console.warn("[sw] 写入缓存失败：", error);
  }
}

/** 给响应补一个入库时间戳；opaque 响应改不了头，原样返回。 */
async function stamp(response) {
  if (response.type === "opaque" || response.type === "opaqueredirect") return response;
  const headers = new Headers(response.headers);
  headers.set(CACHED_AT, String(Date.now()));
  return new Response(await response.blob(), {
    status: response.status,
    statusText: response.statusText,
    headers
  });
}

function isStale(response, maxAgeMs) {
  const stamped = Number(response.headers.get(CACHED_AT));
  const cachedAt =
    Number.isFinite(stamped) && stamped > 0 ? stamped : Date.parse(response.headers.get("date") || "");
  // opaque 响应拿不到任何时间信息 —— 交给 maxEntries 淘汰
  if (!Number.isFinite(cachedAt)) return false;
  return Date.now() - cachedAt > maxAgeMs;
}

/** Cache Storage 的 keys() 按插入顺序返回，超出上限就从最早的开始删。 */
async function trim(cache, maxEntries) {
  const keys = await cache.keys();
  if (keys.length <= maxEntries) return;
  await Promise.all(keys.slice(0, keys.length - maxEntries).map((key) => cache.delete(key)));
}

// 监听前端发来的手动指令（workbox-window 的 messageSkipWaiting 发的就是 SKIP_WAITING）
self.addEventListener("message", (event) => {
  const type = event.data && event.data.type;
  if (type === "SKIP_WAITING") {
    self.skipWaiting();
    return;
  }
  if (type === "CLEAR_IMAGE_CACHE") {
    event.waitUntil(caches.delete("aegis-images"));
  }
});

console.info("[sw] Aegis SW 就绪：image / avatar / storage-proxy / font 走 CacheFirst");
