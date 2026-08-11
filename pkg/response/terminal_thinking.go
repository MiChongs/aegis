package response

// ─────────────────────────────────────────────────────────────────────
// 共享品牌 logo —— Aegis "扫描盾牌" 动画
//
// 设计语言：
//   - 一个 Aegis 品牌盾牌 SVG（Lucide shield-check 几何），不再出现两个
//     独立符号（之前 ✻ + 盲文旋转并排展示，观感像两个 logo）。
//   - 盾牌轮廓常亮用 --brand 颜色；顶上再叠一条相同路径，用短 dasharray
//     做周期性 stroke-dashoffset 扫描，像"护盾正在巡检"——呼应 Aegis
//     (身份与访问护盾) 与 "terminal 思考中" 两层意涵。
//   - 中央对钩 ✓ 呼吸脉冲，表示"守卫就绪"。
//   - "Aegis" 文字仍做 shimmer 渐变。
//   - 纯 CSS keyframes + SVG，无 JS。支持 prefers-reduced-motion 降级。
// ─────────────────────────────────────────────────────────────────────

// BrandLogoCSS 与扫描盾动画相关的全部样式。
const BrandLogoCSS = `
  .brand-logo {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    font-family: "Anthropic Serif", Georgia, serif;
    font-size: 22px;
    font-weight: 500;
    letter-spacing: -0.01em;
    color: var(--text-primary, #141413);
    text-decoration: none;
    line-height: 1;
  }
  .brand-logo .mark {
    position: relative;
    display: inline-flex;
    width: 26px;
    height: 26px;
    color: var(--brand, #c96442);
    flex-shrink: 0;
  }
  .brand-logo .mark svg {
    width: 100%;
    height: 100%;
    overflow: visible;
    display: block;
  }
  .brand-logo .mark .shield-base {
    stroke: var(--brand, #c96442);
    opacity: 0.35;
    fill: none;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .brand-logo .mark .shield-scan {
    stroke: var(--brand, #c96442);
    fill: none;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 14 52;
    animation: brand-scan 2.4s linear infinite;
    filter: drop-shadow(0 0 2px var(--brand, #c96442));
  }
  @keyframes brand-scan {
    0%   { stroke-dashoffset: 66; }
    100% { stroke-dashoffset: 0; }
  }
  .brand-logo .mark .shield-check {
    stroke: var(--brand, #c96442);
    fill: none;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
    transform-origin: 12px 12px;
    animation: brand-check 2.4s ease-in-out infinite;
  }
  @keyframes brand-check {
    0%, 100% { opacity: 0.55; transform: scale(0.92); }
    50%      { opacity: 1.00; transform: scale(1.04); }
  }
  .brand-logo .wordmark {
    background: linear-gradient(
      90deg,
      var(--text-tertiary, #87867f) 0%,
      var(--text-primary, #141413) 45%,
      var(--text-tertiary, #87867f) 100%
    );
    background-size: 250% 100%;
    -webkit-background-clip: text;
            background-clip: text;
    color: transparent;
    animation: brand-shimmer 3.6s linear infinite;
  }
  @keyframes brand-shimmer {
    0%   { background-position: 200% 0; }
    100% { background-position: -100% 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    .brand-logo .shield-scan,
    .brand-logo .shield-check,
    .brand-logo .wordmark { animation: none; }
    .brand-logo .shield-scan { stroke-dasharray: none; }
    .brand-logo .shield-check { opacity: 1; transform: none; }
    .brand-logo .wordmark {
      color: var(--text-primary, #141413);
      background: none;
      -webkit-background-clip: border-box;
              background-clip: border-box;
    }
  }
`

// BrandLogoHTML 左上角动画 logo 的 HTML 片段。
//
// 一个盾牌 SVG + "Aegis" 文字，视觉上仅一个品牌 mark + 一个 wordmark。
// 盾牌路径 d="M12 2 L20 5 V12 C20 17 16 20.5 12 22 C8 20.5 4 17 4 12 V5 Z" ——
// Lucide shield 轮廓；内部 ✓ 路径为 `m8.5 12 2.5 2.5 4.5-4.5`。
const BrandLogoHTML = `<a class="brand-logo" href="/" aria-label="Aegis — Multi-tenant identity platform">
  <span class="mark" aria-hidden="true">
    <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
      <path class="shield-base" d="M12 2 L20 5 V12 C20 17 16 20.5 12 22 C8 20.5 4 17 4 12 V5 Z"/>
      <path class="shield-scan" d="M12 2 L20 5 V12 C20 17 16 20.5 12 22 C8 20.5 4 17 4 12 V5 Z"/>
      <path class="shield-check" d="m8.5 12 2.5 2.5 4.5-4.5"/>
    </svg>
  </span>
  <span class="wordmark">Aegis</span>
</a>`
