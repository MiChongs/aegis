/**
 * 登录页 · 无限斜线滚动背景
 *
 * 纯 CSS 实现（样式在 `globals.css` 的 `.login-stripes*`），没有 canvas / WebGL / JS 动画，
 * 因此这是一个服务端组件，登录页不再为背景加载 three.js。
 *
 * 无缝原理：条纹画成「竖向」的 repeating-linear-gradient，倾斜由外层 rotate 完成；
 * 动画只沿 X 平移**恰好一个 pattern 周期**，首尾像素完全重合，看不出接缝。
 *
 * 三条带的周期 / 速度 / 明度各不相同（26px·34s → 92px·19s → 228px·11s），
 * 叠在一起产生视差纵深；再加一条超长周期的柔光带缓慢扫过，避免画面死板。
 *
 * 倾角由 `--login-stripe-angle` 控制（默认 -45deg），配色走
 * `--login-stripe-*` 变量，深浅两套主题各一份。
 */
export function LoginBackground() {
  return (
    <div aria-hidden className="login-stripes pointer-events-none select-none">
      <div className="login-stripes__tilt">
        <div className="login-stripes__band login-stripes__band--fine" />
        <div className="login-stripes__band login-stripes__band--mid" />
        <div className="login-stripes__band login-stripes__band--bold" />
        <div className="login-stripes__sheen" />
      </div>
      <div className="login-stripes__vignette" />
    </div>
  );
}
