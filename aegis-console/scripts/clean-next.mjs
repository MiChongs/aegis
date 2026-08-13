// 清掉上一次的构建产物。默认**保留 `.next/cache`**，`--all` 连它一起删。
//
// 那个目录装的是 Turbopack 的文件系统缓存（Next 16 起默认开启，本项目约 280MB）
// 与 TypeScript 的 .tsbuildinfo。predev / prebuild 每次都连它一起删的代价是
// 每次都从零编译：实测 `next build` 的编译阶段 2.7s → 10.8s，dev 首屏同理。
// 而它们要的只是"产物是干净的" —— 会残留旧内容的是 manifest / server / static /
// dev 这些目录，缓存本身带版本标记，Next 自己会判失效。
//
// `pnpm clean` 是另一回事：手动清理的场景恰恰是怀疑缓存坏了，所以它走 --all。

import { existsSync, readdirSync, rmSync } from "node:fs";
import { join } from "node:path";

const purgeCache = process.argv.includes("--all");
const distDir = join(process.cwd(), ".next");

if (existsSync(distDir)) {
  for (const entry of readdirSync(distDir)) {
    if (entry === "cache" && !purgeCache) {
      continue;
    }
    rmSync(join(distDir, entry), {
      force: true,
      recursive: true,
      maxRetries: 3,
      retryDelay: 200
    });
  }
}
