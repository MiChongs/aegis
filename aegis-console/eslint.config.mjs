import nextVitals from "eslint-config-next/core-web-vitals";

export default [
  {
    // public/monaco/vs 是 scripts/sync-monaco-assets.mjs 从 node_modules 拷来的
    // Monaco 官方压缩产物，不属于本项目源码，不参与 lint。
    ignores: ["public/monaco/**", "public/geo/**"]
  },
  ...nextVitals
];
