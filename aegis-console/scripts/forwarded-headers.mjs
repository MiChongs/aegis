// 客户端 IP 透传：把「本进程亲眼看到的直连对端」追加到 X-Forwarded-For。
//
// ── 为什么需要它 ────────────────────────────────────────────────────
//
// 浏览器只和 Next.js 说话：`/api/*` 由 `next.config.ts` 的 rewrites 反代到 Go 后端
// （见那里「同源反向代理」一段）。而 Next 内置的那个代理
// （next/dist/server/lib/router-utils/proxy-request.js）**不追加转发头** ——
// 它只手工塞了一个 `x-forwarded-host`，httpxy 的 `xfwd` 选项并没有打开。
//
// 于是后端收到的请求里，缺的正是「控制台这一跳看到的对端是谁」这条事实：
//
//   没有入口反代时（本机开发 / 自建单机）
//     转发链为空 → 后端只能退回直连对端 → 全站用户的 IP 都是控制台自己
//   有入口反代时（Zeabur / K8s / Nginx）
//     转发链最右端是入口写的，控制台无法为它背书 —— 而客户端自己也能写这个头，
//     后端因此分不清「链是入口写的」还是「链是客户端伪造的」
//
// 限流、封禁、地理风控、审计全部建立在后端算出来的那个地址上，判错不报错，
// 只会让这几样一起静默失效。判定规则见 docs/client-ip.md。
//
// ── 它只做一件事 ────────────────────────────────────────────────────
//
// 在 HTTP 服务器边界，把本进程 socket 上的直连对端**追加**到 X-Forwarded-For 末尾。
// 这正是 nginx `proxy_add_x_forwarded_for` 与 http-proxy `xfwd: true` 的语义。
//
// **刻意不在这里判断谁可信。** 受信网段只有一处配置入口（后端的 `TRUSTED_PROXIES`），
// 控制台再配一份，「到底谁说了算」就没有答案了。控制台负责如实追加一条事实，
// 判定权留在后端 —— 那边已经有完整的受信集合、平台探测与排障头。
//
// **追加而不是覆盖**，因为这样对两种拓扑同时成立：
//
//   客户端伪造了 XFF          → 伪造条目留在链的左边，后端从右往左先遇到我们写的这条
//   入口反代已经写了真实 IP   → 我们追加的是入口的内网地址，后端按受信网段跳过它，
//                               继续往左走，停在入口写下的那个真实客户端上
//
// ── 为什么是 monkey patch，而不是中间件 ─────────────────────────────
//
// 对端地址只存在于 socket 上。Next 的 middleware / proxy.ts 与 Route Handler
// 拿到的都是 Web `Request`，看不到 socket（`NextRequest.ip` 在 Next 15 已移除），
// 也就没有任何东西可以如实追加。而那个 `http.Server` 由 Next 自己创建并持有
// （`next start` 走 startServer，`next dev` 还要再 fork 一层），外部拿不到实例。
//
// 所以只剩一条路：在 Next 创建服务器之前替换掉 `http.createServer`。
// 装载方式见 forwarded-headers-preload.mjs 与 package.json 的 dev / start 脚本。

import http from "node:http";
import https from "node:https";
import { isIP } from "node:net";

// 打在请求对象上的幂等标记。同一个请求只应被追加一次 —— 重复追加会在链上
// 造出两条相同条目，让「本服务前面有几层代理」这个判断凭空多一跳。
const STAMPED = Symbol.for("aegis.forwardedHeaders.stamped");
// 打在被改写过的 createServer / server 上，避免重复安装（预载可能被载入多次）。
const PATCHED = Symbol.for("aegis.forwardedHeaders.patched");

const PRELOAD_URL = new URL("./forwarded-headers-preload.mjs", import.meta.url).href;

let announced = false;

/**
 * 归一化一个 socket 对端地址，拿不到合法地址时返回空串。
 *
 * 两处必须处理，否则后端会把它当成坏值丢掉：
 *   - `::ffff:1.2.3.4`：双栈监听下 IPv4 连接的常见形态，摘掉前缀还原成 IPv4
 *   - `fe80::1%eth0`：IPv6 zone 只对本机有意义，跨进程传出去没有含义
 */
export function normalizePeerAddress(raw) {
  if (typeof raw !== "string") return "";

  let value = raw.trim();
  if (!value) return "";

  // Unix domain socket / 命名管道没有对端地址，remoteAddress 会是空或非 IP。
  const zone = value.lastIndexOf("%");
  if (zone > 0) value = value.slice(0, zone);

  if (/^::ffff:/i.test(value) && isIP(value.slice(7)) === 4) {
    value = value.slice(7);
  }

  return isIP(value) ? value : "";
}

/** 取头值：Node 只有 set-cookie 是数组，其余同名头已按 ", " 合并，这里仍防一手。 */
function headerValue(value) {
  if (Array.isArray(value)) return value.join(", ");
  return typeof value === "string" ? value : "";
}

/**
 * RFC 7239 的 `for=` 结点标识：IPv6 必须加方括号并整体加引号，IPv4 裸写。
 * 写错的话下游解析器会把 `for=::1` 里的冒号当成参数分隔符。
 */
function forwardedNode(peer) {
  return isIP(peer) === 6 ? `for="[${peer}]"` : `for=${peer}`;
}

/**
 * 给一个 Node 请求对象补齐转发头。返回是否真的追加了对端地址。
 *
 * `x-forwarded-proto` / `-host` / `-port` 一律**只在缺失时**补：它们描述的是
 * 「客户端最初访问的是什么」，链路上第一个知道答案的是最外层入口，
 * 而不是我们。覆盖它会把 https 改写成 http，后端拼出来的回跳地址随之出错。
 */
export function stampForwardedHeaders(req) {
  if (!req || typeof req !== "object" || req[STAMPED]) return false;

  const headers = req.headers;
  if (!headers || typeof headers !== "object") return false;

  req[STAMPED] = true;

  const socket = req.socket ?? req.connection ?? null;
  const encrypted = Boolean(socket?.encrypted);

  if (!headerValue(headers["x-forwarded-proto"])) {
    headers["x-forwarded-proto"] = encrypted ? "https" : "http";
  }
  if (!headerValue(headers["x-forwarded-host"]) && headerValue(headers.host)) {
    headers["x-forwarded-host"] = headerValue(headers.host);
  }
  if (!headerValue(headers["x-forwarded-port"]) && socket?.localPort) {
    headers["x-forwarded-port"] = String(socket.localPort);
  }

  const peer = normalizePeerAddress(socket?.remoteAddress);
  if (!peer) return false;

  const chain = headerValue(headers["x-forwarded-for"]).trim().replace(/,\s*$/, "");
  headers["x-forwarded-for"] = chain ? `${chain}, ${peer}` : peer;

  // RFC 7239 的 Forwarded 只在上游已经用了它时才续写：它和 XFF 是两条独立的链，
  // 凭空造一条半截的出来，只会让配了 CLIENT_IP_LIST_HEADER=Forwarded 的后端
  // 读到一条「只有代理、没有客户端」的链，然后退回直连对端。
  const forwarded = headerValue(headers.forwarded).trim().replace(/,\s*$/, "");
  if (forwarded) {
    headers.forwarded = `${forwarded}, ${forwardedNode(peer)}`;
  }

  return true;
}

/**
 * 在一个已存在的 http.Server 上安装追加逻辑。
 *
 * 挂在 `emit` 上而不是 `prependListener('upgrade', …)`：给一个本来没有 upgrade
 * 监听器的 server 装上监听器，Node 就不再自动销毁升级请求的连接了 —— 那会把
 * 「没人处理的 WebSocket 握手」从「立刻断开」变成「一直挂着」。
 * 改写 emit 不改变任何监听器集合，只是抢在分发之前落一次笔。
 */
export function installOnServer(server) {
  if (!server || typeof server.emit !== "function" || server[PATCHED]) return server;
  server[PATCHED] = true;

  const emit = server.emit;
  server.emit = function patchedEmit(event, ...args) {
    if (event === "request" || event === "upgrade") {
      stampForwardedHeaders(args[0]);
    }
    return emit.call(this, event, ...args);
  };

  announce();
  return server;
}

/**
 * 替换 http / https 的 createServer，让此后创建的每个服务器都带上追加逻辑。
 * 幂等，返回本次是否真的改写了什么。
 */
export function install() {
  return [http, https].map(patchModule).some(Boolean);
}

function patchModule(mod) {
  const original = mod?.createServer;
  if (typeof original !== "function" || original[PATCHED]) return false;

  const patched = function createServer(...args) {
    return installOnServer(original.apply(this, args));
  };
  patched[PATCHED] = true;
  mod.createServer = patched;
  return true;
}

/**
 * 只在第一个服务器装好时打一行。
 *
 * 这一行是**部署自检的唯一线索**：托管平台若绕过 package.json 的 dev / start
 * 脚本直接跑 `next start`（Zeabur 的 serverless 档就会这样），预载不会执行，
 * 而少了一条转发链条目这件事在功能上完全看不出来 —— 日志里没有这行，就是没生效。
 */
function announce() {
  if (announced) return;
  announced = true;
  console.log("▲ 客户端 IP 透传已启用：反代到后端的请求会追加本进程看到的直连对端（X-Forwarded-For）");
}

/**
 * 给子进程的环境变量补上预载参数。
 *
 * `next dev` 会 fork 一层真正的服务器进程（next/dist/cli/next-dev.js 里的
 * `fork(startServerPath, { execArgv, env })`），且 execArgv 是它自己按
 * NODE_OPTIONS 重新拼的 —— 命令行上的 `--import` 只有先落进 NODE_OPTIONS
 * 才能传到那一层。用 `=` 形式是必须的：分隔符写成空格时，Next 的
 * NODE_OPTIONS 解析虽然能兜住，但没有理由去赌它。
 */
export function withForwardedPreload(env = process.env) {
  const flag = `--import=${PRELOAD_URL}`;
  const current = typeof env.NODE_OPTIONS === "string" ? env.NODE_OPTIONS : "";
  if (current.includes(PRELOAD_URL)) return { ...env };
  return { ...env, NODE_OPTIONS: current ? `${current} ${flag}` : flag };
}

/** 预载模块的绝对 file URL，供启动脚本拼 `--import=`。 */
export const preloadUrl = PRELOAD_URL;
