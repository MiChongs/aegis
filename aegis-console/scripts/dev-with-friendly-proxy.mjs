// 包装 `next dev`：捕获并美化 Next.js rewrites 代理在后端不可达时的原始报错。
//
// Next 16 原始输出（多行 + 堆栈）形如：
//   Failed to proxy http://127.0.0.1:8088/api/ws?token=... Error: connect ECONNREFUSED 127.0.0.1:8088
//       at <unknown> (...)
//   {
//     errno: -4078,
//     code: 'ECONNREFUSED',
//     ...
//   }
//
// 压成一行中文提示，并按 (host + path + errCode) 去重（窗口 5s），避免 WS 自动重连刷屏。
// 其它所有日志保持原样透传。

import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { existsSync } from "node:fs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const consoleRoot = resolve(scriptDir, "..");
const nextBin = join(consoleRoot, "node_modules", "next", "dist", "bin", "next");

if (!existsSync(nextBin)) {
  console.error(`未找到 Next.js 可执行文件: ${nextBin}`);
  console.error("请先在 aegis-console 根目录执行 pnpm install。");
  process.exit(1);
}

const nextArgs = ["dev", ...process.argv.slice(2)];

const child = spawn(process.execPath, [nextBin, ...nextArgs], {
  cwd: consoleRoot,
  env: process.env,
  stdio: ["inherit", "pipe", "pipe"]
});

// 一个可流式解析的行缓冲：既要按行处理（便于匹配），又要保留原分隔符透传。
function createLineStream(target /* process.stdout | process.stderr */, onLine) {
  let buf = "";
  return (chunk) => {
    buf += chunk.toString("utf8");
    let idx;
    while ((idx = buf.indexOf("\n")) !== -1) {
      const line = buf.slice(0, idx);
      buf = buf.slice(idx + 1);
      onLine(line, target);
    }
    // 尾部未换行的片段留在 buf；进程退出时再 flush
    void 0;
  };
}

// ── 代理错误块状态机 ────────────────────────────────────────────────
// 一旦读到 "Failed to proxy ..."，吞掉后续的堆栈/属性行，
// 直到遇到一行 "}"（错误对象结束）或一行空白/普通日志。

let collecting = false;
let pending = ""; // 缓存首行用于解析
const dedup = new Map(); // key -> 最近一次打印的 ms
const WINDOW_MS = 5000;

function isErrorDetailLine(line) {
  if (line.trim() === "") return false;
  if (line.trim() === "}") return true;
  if (/^\s+at\s/.test(line)) return true;
  if (/^\s+(errno|code|syscall|address|port|hostname):/.test(line)) return true;
  if (/^\s*\{/.test(line)) return true;
  return false;
}

function emitFriendly(raw, target) {
  // raw 形如: "Failed to proxy http://127.0.0.1:8088/api/ws?token=... Error: connect ECONNREFUSED 127.0.0.1:8088"
  const m = /Failed to proxy\s+(\S+)\s+Error:\s+(.+?)(?:\s+([\d.]+:\d+))?\s*$/.exec(raw);
  if (!m) {
    target.write(raw + "\n"); // 不匹配就原样输出
    return;
  }
  const url = m[1];
  const errMsg = m[2];
  const addr = m[3] || "";

  let pathPart = url;
  try {
    const u = new URL(url);
    pathPart = u.pathname + (u.search ? "?…" : "");
  } catch { /* 原样使用 */ }

  const codeMatch = /(ECONNREFUSED|ETIMEDOUT|ECONNRESET|ENOTFOUND|EHOSTUNREACH)/.exec(errMsg);
  const code = codeMatch ? codeMatch[1] : errMsg;

  const key = `${addr}|${pathPart.split("?")[0]}|${code}`;
  const now = Date.now();
  const last = dedup.get(key) ?? 0;
  if (now - last < WINDOW_MS) return; // 静默重复
  dedup.set(key, now);

  const hint = code === "ECONNREFUSED"
    ? "Go 后端似乎未启动，请运行：go run ./cmd/server（默认 :8088）。恢复后浏览器会自动重连。"
    : code === "ETIMEDOUT" || code === "EHOSTUNREACH"
      ? "后端地址可达性异常，检查 AEGIS_API_BACKEND / 网络 / 防火墙。"
      : code === "ENOTFOUND"
        ? "AEGIS_API_BACKEND 指向的主机名无法解析，确认地址或 DNS。"
        : "后端连接异常，稍后再试。";

  target.write(`⚠ 代理到后端失败 · ${addr || "upstream"} · ${pathPart} · ${code}\n  ${hint}\n`);
}

function handleLine(line, target) {
  if (collecting) {
    if (isErrorDetailLine(line)) return; // 吞掉
    collecting = false;
    // 错误块结束，flush 首行友好版，并继续处理当前行
    if (pending) {
      emitFriendly(pending, target);
      pending = "";
    }
    // 当前行若又是一个新的代理错误，下面会再次进入 collecting
  }

  if (/^Failed to proxy\s/.test(line)) {
    collecting = true;
    pending = line;
    return;
  }

  target.write(line + "\n");
}

const onStdout = createLineStream(process.stdout, handleLine);
const onStderr = createLineStream(process.stderr, handleLine);

child.stdout.on("data", onStdout);
child.stderr.on("data", onStderr);

// 子进程结束时如还在收集中，flush 一次
function finalize() {
  if (collecting && pending) {
    emitFriendly(pending, process.stderr);
    pending = "";
    collecting = false;
  }
}

for (const sig of ["SIGINT", "SIGTERM", "SIGHUP", "SIGBREAK"]) {
  process.on(sig, () => {
    if (!child.killed) child.kill(sig);
  });
}

child.on("exit", (code, signal) => {
  finalize();
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 0);
});
