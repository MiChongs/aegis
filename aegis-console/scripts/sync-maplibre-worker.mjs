// 把 maplibre-gl 的 Web Worker 同步到 public/maplibre。
//
// maplibre-gl 6.x 的 ESM 产物会在运行时按 `import.meta.url` 推算 worker 地址
// （主文件旁边的 maplibre-gl-worker.mjs）。这个地址是拼出来的字符串，打包器识别不到，
// 不会把 worker 一起产出；而主文件被 Turbopack 打进 /_next/static/chunks/ 之后，
// 旁边根本没有那个文件 —— 结果是控制台报「Worker failed to load」，矢量瓦片与
// GeoJSON 解析都跑不起来。
//
// 所以把 worker 自托管到 public/，再由 maplibre-map.tsx 用 setWorkerUrl 指过去。
// worker 文件是自包含的（maplibre-gl-shared.mjs 为空），只需拷这一个。

import fs from "node:fs";
import path from "node:path";

const rootDir = path.resolve(import.meta.dirname, "..");
const packageDir = path.join(rootDir, "node_modules", "maplibre-gl");
const source = path.join(packageDir, "dist", "maplibre-gl-worker.mjs");
const targetDir = path.join(rootDir, "public", "maplibre");
const target = path.join(targetDir, "maplibre-gl-worker.mjs");

if (!fs.existsSync(source)) {
  console.error(`maplibre worker not found: ${source}\n请先执行 bun install`);
  process.exit(1);
}

const version = JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8")).version;
const sourceBytes = fs.readFileSync(source);

// 内容相同就不写，免得每次 dev / build 都改动工作区
if (fs.existsSync(target) && fs.readFileSync(target).equals(sourceBytes)) {
  console.log(`maplibre worker up to date: v${version}`);
  process.exit(0);
}

fs.mkdirSync(targetDir, { recursive: true });
fs.writeFileSync(target, sourceBytes);
console.log(`maplibre worker synced: v${version} → public/maplibre/maplibre-gl-worker.mjs`);
