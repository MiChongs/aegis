// 把 monaco-editor 的 AMD 产物同步到 public/monaco/vs。
//
// 为什么用「自托管 AMD 产物」而不是「打包 ESM 入口」：
//   1. @monaco-editor/react 默认从 jsDelivr CDN 加载 monaco —— 内网部署会直接白屏，
//      也不该让管理后台依赖公网；
//   2. Monaco 的语言服务跑在 Web Worker 里（TypeScript worker 就是完整的 tsserver）。
//      走 ESM 打包时需要把 worker 交给 bundler 处理，Turbopack 对
//      `new Worker(new URL("monaco-editor/esm/...", import.meta.url))` 这类裸包说明符
//      的解析并不稳定，worker 起不来就等于没有补全和诊断。
//      AMD 模式下 worker 由 monaco 自己按 vs 路径加载，与打包器完全解耦。
//
// 产物只在访问脚本编辑器时按需请求，不进主 bundle。

import fs from "node:fs";
import path from "node:path";

const rootDir = path.resolve(import.meta.dirname, "..");
const sourceDir = path.join(rootDir, "node_modules", "monaco-editor", "min", "vs");
const targetDir = path.join(rootDir, "public", "monaco", "vs");

if (!fs.existsSync(sourceDir)) {
  console.error(`monaco assets not found: ${sourceDir}\n请先执行 pnpm install`);
  process.exit(1);
}

// 全量替换，避免升级 monaco 后残留旧版本文件
fs.rmSync(targetDir, { recursive: true, force: true });
fs.mkdirSync(path.dirname(targetDir), { recursive: true });
fs.cpSync(sourceDir, targetDir, { recursive: true });

function countFiles(dir) {
  let total = 0;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    total += entry.isDirectory() ? countFiles(path.join(dir, entry.name)) : 1;
  }
  return total;
}

const version = JSON.parse(
  fs.readFileSync(path.join(rootDir, "node_modules", "monaco-editor", "package.json"), "utf8")
).version;

console.log(`monaco assets synced: v${version} files=${countFiles(targetDir)} → public/monaco/vs`);
