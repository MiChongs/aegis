// `--import` 预载入口：在 Next.js 创建 HTTP 服务器之前改写 http.createServer。
//
// 单独一个文件而不是让 forwarded-headers.mjs 自己带副作用：那个文件同时被测试
// 与启动脚本引入，import 即改写全局的话，「谁装的、什么时候装的」就说不清了。
//
// 装载点有两处，都指向这个文件：
//   pnpm dev   → scripts/dev-with-friendly-proxy.mjs 把 --import 塞进 NODE_OPTIONS
//                （next dev 会 fork 出真正的服务器进程，只有 NODE_OPTIONS 传得过去）
//   pnpm start → package.json 里直接 --import（next start 不 fork，命令行即可）

import { install } from "./forwarded-headers.mjs";

install();
