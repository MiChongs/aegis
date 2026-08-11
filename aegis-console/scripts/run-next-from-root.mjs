import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const consoleRoot = resolve(scriptDir, "..");
const nextBin = join(consoleRoot, "node_modules", "next", "dist", "bin", "next");
const command = process.argv[2];
const extraArgs = process.argv.slice(3);

if (!command) {
  console.error("缺少 Next.js 子命令，例如 dev、build、start。");
  process.exit(1);
}

if (!existsSync(nextBin)) {
  console.error(`未找到 Next.js 可执行文件: ${nextBin}`);
  console.error("请先在 aegis-console 根目录安装依赖。");
  process.exit(1);
}

const child = spawn(process.execPath, [nextBin, command, ...extraArgs], {
  cwd: consoleRoot,
  env: process.env,
  stdio: "inherit"
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }

  process.exit(code ?? 0);
});

