# 内置护栏策略：每个场景都在场景策略之后过这一层，控制台只读。
#
# input.proposed 是前面各步（名单 / 规则 / 决策流 / 场景策略）提出的处置，
# 这里只做与业务无关、但永远该成立的修正。
package aegis.risk.guard

import rego.v1

decision := d if {
	input.lists.block
	d := object.union(input.proposed, {"action": "block", "reasons": array.concat(input.proposed.reasons, ["命中黑名单"]), "decided_by": "list"})
} else := d if {
	input.lists.allow
	d := object.union(input.proposed, {"action": "pass", "reasons": ["命中白名单，放行"], "decided_by": "list"})
} else := d if {
	input.proposed.action == "captcha"
	input.request.captcha_passed
	d := object.union(input.proposed, {"action": "pass", "reasons": array.concat(input.proposed.reasons, ["已通过图形验证码"])})
} else := d if {
	input.proposed.action == "captcha"
	not input.scene.captchaSupported
	d := object.union(input.proposed, {"action": "review", "reasons": array.concat(input.proposed.reasons, ["场景接不了验证码，转人工复核"])})
} else := d if {
	input.proposed.action == "captcha"
	not input.request.captcha_available
	d := object.union(input.proposed, {"action": "review", "reasons": array.concat(input.proposed.reasons, ["应用没有配置图形验证码，转人工复核"])})
} else := d if {
	input.proposed.action == "ban"
	not input.scene.enforcement.autoBanAllowed
	d := object.union(input.proposed, {"action": "block", "reasons": array.concat(input.proposed.reasons, ["场景不允许自动封禁，只拦截"])})
} else := input.proposed
