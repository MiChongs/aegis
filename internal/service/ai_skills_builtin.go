package service

import (
	aidomain "aegis/internal/domain/ai"
)

// 内置技能：随二进制发布的提示词包。
//
// 与数据库里的自定义技能对 Agent 完全同构（启用即注入系统提示词），
// 区别只在来源：这几份讲的是平台自身的领域知识，跟着代码版本走才不会过时 ——
// 落库的话，平台升级后旧库里的技能还在教模型用已经改名的 API。
//
// 内容刻意只讲「方法论与坑」，不抄 SDK 签名：签名的唯一事实源是能力目录，
// Agent 要看签名会调 get_sdk_reference 工具，那份是按当前版本现生成的。

func builtinSkills() []aidomain.Skill {
	return []aidomain.Skill{
		{
			Key:  "builtin:function-authoring",
			Name: "远程函数写作方法论",
			Description: "写 Aegis 远程函数的结构约定、迭代流程与常见坑",
			Builtin: true, Enabled: true,
			Content: `Aegis 远程函数是跑在服务端沙箱里的 JavaScript（ES2020，无 Node/浏览器 API）：

## 脚本结构
- 入口是顶层代码，最后一个表达式或 return 的值就是函数输出（必须可 JSON 序列化）。
- 全局对象：ctx（本次调用元数据：ctx.input 入参、ctx.caller 调用者、ctx.dryRun 是否试跑）、
  aegis（平台 SDK，只有函数**已声明**的能力会被绑定，未声明的命名空间是 undefined）、
  console（log/info/warn/error，日志会回传给作者）。
- 业务失败用 aegis.fail(code, message) 主动抛出，调用方会拿到这个码；
  不要用 throw new Error 表达业务判定 —— 那会被当成脚本崩溃。
- 可调阈值放进函数配置（aegis.config 读取），改配置不用发新版本。

## 迭代流程（务必遵守）
1. 改代码 → stage_source 放进编辑器（作者看得见，也是发布的正文来源）；
2. analyze_draft 静态检查：能力缺失、未知成员、语法错误都会带行号指出；
3. test_draft 试跑：读是真的、写只记不做（effects 里标 simulated），失败会给
   错误位置、日志与已发生的副作用清单；
4. 反复 2-3 直到通过，需要发布时再用 publish_version。
不要跳过试跑直接宣布完成 —— 「看起来对」和「跑得通」是两回事。

## 常见坑
- 能力是「声明即授权」：用了没声明的能力，脚本里那个对象直接是 undefined。
  发现缺能力时用 update_function_settings 补声明，或提醒作者在设置里勾选。
- ctx.input 的形状由入参契约（JSON Schema）约束；改输入形状要同步改契约，
  否则真实调用会在入口就被 40109 拦下。
- 单次执行有 SDK 配额（调用次数 / 写操作数 / 出网次数），循环里逐条调 SDK
  很快会撞上限 —— 想想能不能一次查出来。
- KV 是唯一的服务端持久状态：user 作用域按调用者隔离，app 作用域全应用共享。`,
		},
		{
			Key:  "builtin:aegis-effects",
			Name: "副作用与试跑语义",
			Description: "effects 流水、dry-run 行为与写操作配额",
			Builtin: true, Enabled: true,
			Content: `## 副作用（effects）
- aegis.* 里的每个写操作（加积分、发通知、写 KV、封禁……）都会记一条 effect，
  最终落进调用审计 —— 这是「函数动过什么」的唯一凭证。
- 试跑（ctx.dryRun = true）时写操作**只记录不执行**，effect 上带 simulated: true。
  读操作永远是真的：试跑读到的用户、积分、KV 都是线上数据。
- 因此试跑结果里的 effects 列表就是「这段脚本想做什么」的完整预演，
  给作者解释脚本行为时直接引用它。

## 配额
- 写操作次数、总调用次数、出网 fetch 次数各有单次上限（get_capability_catalog
  可查当前值）。超限会中断执行 —— 批量场景要么合并操作，要么让调用方分批。

## 与外部系统交互
- http.fetch 能力出网有大小与次数限制；试跑时它**真的会发请求**，
  对不可逆的外部动作要在脚本里判 ctx.dryRun 自行跳过。`,
		},
		{
			Key:  "builtin:input-contract",
			Name: "入参契约（JSON Schema）",
			Description: "一份 Schema 同时驱动调用校验、试跑补全与编辑器类型",
			Builtin: true, Enabled: true,
			Content: `## 入参契约
- 函数的 inputSchema 是一份 JSON Schema（draft 2020-12 的常用子集：
  type / properties / required / items / enum / minimum / maximum /
  minLength / maxLength / pattern / additionalProperties / description / default）。
- {} 表示不约束。一旦声明，真实调用与试跑都会先校验：不合规的请求在入口
  就被拒绝并逐条说明哪里不对，脚本里因此**不必再写防御性判空**。
- 同一份声明还驱动编辑器里 ctx.input 的 TypeScript 类型与试跑输入框的补全 ——
  改了输入形状务必同步改契约（update_function_settings 的 inputSchema 字段）。
- 给每个字段写 description：那是作者与接入方都会看到的接口文档。`,
		},
		{
			Key:  "builtin:function-debugging",
			Name: "函数排错清单",
			Description: "按信号定位远程函数的线上问题",
			Builtin: true, Enabled: true,
			Content: `## 排错顺序
1. get_invocation_stats 先看全貌：成功率、P95、错误 Top —— 「偶发还是全挂」
   决定后面看什么。
2. get_invocations 按 status=error 捞失败调用：错误消息里带抛错位置。
3. 复现：用失败调用的 input 跑 test_draft（必要时 asUserId 指定同一个调用者身份）。
4. 日志里的 elapsedMs 是相对执行起点的毫秒数，两条日志的间隔就是中间那段代码
   的耗时 —— 「哪一步慢」靠它看出来。
5. 改完先 analyze_draft 再 test_draft，通过后 stage_source 交给作者确认。

## 常见错误对照
- 「应用函数尚未激活」：函数 status 不是 active 或没有激活版本 —— 发布并激活。
- 40109 入参校验失败：调用方的 input 不满足契约，看错误里逐条说明。
- 频率限制：rateLimitPerMin 撞顶，看统计里的调用量再决定调阈值还是让调用方退避。
- 某个 aegis 命名空间 undefined：能力没声明，补声明而不是绕过。`,
		},
	}
}
