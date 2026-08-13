/**
 * 脚本代码片段。
 *
 * 成员补全（来自后端下发的 .d.ts）解决的是「有哪些 API」，片段解决的是
 * 「这段该怎么写」—— 而这里几种写法一旦写错，**都不会报错**：
 *
 *   - 先查后写不加锁：单实例下靠运气也对，多副本部署必然重复发放
 *   - 日切用 dayKey() 而不是 dayKeyIn()：默认走 UTC，比东八区晚八小时切
 *   - 签名串自己拼：JS 对象遍历顺序对数字键与字符串键规则不同，签名会时对时不对
 *   - 金额用 number 相加：0.1 + 0.2 落到账上就是对不平的那一分
 *
 * 片段不进后端目录：它们是**编辑体验**，不是运行时契约 —— 与 .d.ts 那份
 * 「补全里有什么、运行时就绑定什么」的约束不是一回事，混在一起会让
 * 「改一句注释」变成一次后端发版。
 */
export type AegisSnippet = {
  /** 触发词，一律 aegis 前缀，输 aeg 就能列全 */
  label: string;
  summary: string;
  detail: string;
  /** Monaco 片段语法：${1:占位} */
  body: string;
};

export const AEGIS_SNIPPETS: AegisSnippet[] = [
  {
    label: "aegis-handle",
    summary: "函数骨架",
    detail: "handle 入口 + 调用者判定。沙箱每次调用都是全新运行时，没有跨请求状态。",
    body: [
      "/** @param {AegisContext} ctx */",
      "function handle(ctx) {",
      "  const me = aegis.user.get();",
      '  aegis.assert(me, "需要用户身份调用", 40100);',
      "",
      "  ${1:// 逻辑写在这里}",
      "",
      "  return { ok: true, at: aegis.time.iso() };",
      "}"
    ].join("\n")
  },
  {
    label: "aegis-quota",
    summary: "每日额度（按时区日切）",
    detail:
      "incr 是数据库层面的原子操作，并发调用不会读到同一个旧值。" +
      "日切用 dayKeyIn 指定时区 —— 默认的 dayKey 走 UTC，比东八区晚八小时。",
    body: [
      'const quota = Number(aegis.config.${1:dailyQuota} || ${2:100});',
      'const used = aegis.kv.user.incr("${3:quota}:" + aegis.time.dayKeyIn("Asia/Shanghai"), 1, 86400);',
      'if (used > quota) aegis.fail("今日额度已用尽", 42901);'
    ].join("\n")
  },
  {
    label: "aegis-lock",
    summary: "加锁的临界区",
    detail:
      "「先查后写」在多实例下挡不住并发：两个请求会同时读到「还没领过」。" +
      "run 在锁内执行，无论正常返回还是抛错都会释放。",
    body: [
      'return aegis.lock.run("${1:claim}:" + me.id, function () {',
      "  ${2:// 判重与发放都放在这里}",
      "  return { ok: true };",
      "}, 10);"
    ].join("\n")
  },
  {
    label: "aegis-vip-gate",
    summary: "会员闸门（按功能标识）",
    detail:
      "判功能标识而不是套餐名 —— 后者是运营随时会改的展示文案。" +
      "试用会员要不要放行由脚本自己决定，服务端只如实告诉你他是哪一档。",
    body: [
      "const me = aegis.user.get();",
      'aegis.assert(me && !me.banned, "账号不可用", 40311);',
      'if (!me.vip) aegis.fail("该功能仅限会员", 40310);',
      'if (me.vipTrial) aegis.fail("该功能不对试用会员开放", 40310);',
      'if (!me.vipFeatures.includes("${1:export}")) aegis.fail("当前会员不含该功能", 40310);'
    ].join("\n")
  },
  {
    label: "aegis-sign",
    summary: "服务端签名下发",
    detail:
      "密钥只存在 KV 里，客户端拿到的只有一次性签名 —— 即便把客户端整个反编译，" +
      "也造不出第二个合法签名。",
    body: [
      'const secret = String(aegis.kv.get("${1:signing-secret}") || "");',
      'aegis.assert(secret, "服务端尚未配置签名密钥", 50001);',
      "const nonce = aegis.crypto.randomHex(16);",
      "const issuedAt = aegis.time.unix();",
      'const signature = aegis.crypto.hmacSha256(secret, [me.id, nonce, issuedAt].join("\\n"));'
    ].join("\n")
  },
  {
    label: "aegis-verify-sign",
    summary: "校验第三方回调签名",
    detail:
      "queryStringify 按键名字典序拼接，顺序稳定；timingSafeEqual 定长比较，" +
      "不会因为提前返回而泄露前缀信息。",
    body: [
      'const secret = String(aegis.kv.get("${1:webhook-secret}") || "");',
      "const expected = aegis.crypto.hmacSha256(secret, aegis.encoding.queryStringify({",
      "  ${2:orderNo}: ctx.input.${2:orderNo}, timestamp: ctx.input.timestamp",
      "}));",
      "if (!aegis.crypto.timingSafeEqual(expected, String(ctx.input.sign))) {",
      '  aegis.fail("签名校验失败", 40103);',
      "}"
    ].join("\n")
  },
  {
    label: "aegis-schema",
    summary: "入参 schema 校验",
    detail: "一次返回全部错误，而不是遇到第一个就停 —— 免得作者改一个再跑一次。",
    body: [
      "const shape = aegis.validate.schema({",
      '  type: "object",',
      '  required: ["${1:orderNo}"],',
      '  properties: { ${1:orderNo}: { type: "string", minLength: 1 } }',
      "}, ctx.input);",
      'if (!shape.valid) aegis.fail("参数不合法：" + shape.errors.join("; "), 40001);'
    ].join("\n")
  },
  {
    label: "aegis-money",
    summary: "金额运算（定点小数）",
    detail:
      "JS 的 number 是双精度浮点，0.1 + 0.2 得到 0.30000000000000004。" +
      "钱的加减乘除一律走 aegis.decimal，字符串进、字符串出。",
    body: [
      'const total = aegis.decimal.mul("${1:9.90}", String(ctx.input.count || 1));',
      'const discounted = aegis.decimal.round(aegis.decimal.mul(total, "0.8"), 2);',
      'if (aegis.decimal.cmp(discounted, "0") <= 0) aegis.fail("金额无效", 40001);'
    ].join("\n")
  },
  {
    label: "aegis-fetch",
    summary: "调用上游接口",
    detail:
      "第三方 API Key 留在服务端，客户端只能通过这个函数间接使用它 —— " +
      "额度、频次、审计全部由平台说了算。",
    body: [
      'const apiKey = String(aegis.kv.get("${1:upstream-api-key}") || "");',
      'aegis.assert(apiKey, "服务端尚未配置上游密钥", 50001);',
      'const response = aegis.fetch("${2:https://api.example.com/v1/query}", {',
      '  method: "POST",',
      '  headers: { Authorization: "Bearer " + apiKey },',
      "  body: { query: ctx.input.query }",
      "});",
      'if (!response.ok) aegis.fail("上游服务暂不可用", 50201);'
    ].join("\n")
  },
  {
    label: "aegis-jwt",
    summary: "签发短期令牌",
    detail: "令牌里写的是服务端算出来的权益，接入方的服务只需验签，不必再问一次。",
    body: [
      'const token = aegis.crypto.jwtSign(String(aegis.kv.get("${1:jwt-secret}")), {',
      "  sub: String(me.id),",
      "  aud: ctx.appKey,",
      "  vip: me.vip",
      '}, { alg: "HS256", expiresIn: ${2:900} });'
    ].join("\n")
  }
];
