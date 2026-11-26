package dev.aegis.sdk

import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/** 反馈详情 / 列表项里的 `reward`：有效奖励解出来，没有或为 null 时得到 null，多出的字段不影响。 */
class AegisFeedbackRewardTest {

    private val api = AegisClient.builder("http://127.0.0.1:1", "demo_app").build().engagement

    @Test
    fun `reward is decoded from feedback detail`() {
        val detail = Json.parseToJsonElement(
            """{"id":"417","ticketNo":"FB-417","reward":{"points":50,"reason":"定位到闪退原因","grantedAt":"2026-11-26T08:00:00Z","extra":1}}""",
        )
        assertEquals(AegisFeedbackReward(50, "定位到闪退原因", "2026-11-26T08:00:00Z"), api.feedbackRewardOf(detail))
    }

    @Test
    fun `missing or null reward yields null`() {
        assertNull(api.feedbackRewardOf(Json.parseToJsonElement("""{"id":"1"}""")))
        assertNull(api.feedbackRewardOf(Json.parseToJsonElement("""{"id":"1","reward":null}""")))
        assertNull(api.feedbackRewardOf(Json.parseToJsonElement("""[]""")))
    }
}
