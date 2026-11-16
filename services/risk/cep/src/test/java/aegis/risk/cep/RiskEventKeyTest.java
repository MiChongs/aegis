package aegis.risk.cep;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

class RiskEventKeyTest {

    static RiskEvent ev(long user, String account) {
        RiskEvent e = new RiskEvent();
        e.appId = 7;
        e.userId = user;
        e.account = account;
        return e;
    }

    @Test
    void accountKeyMergesBoundLoginNames() {
        // 同一个账号用登录名、邮箱、手机号：都归成 #ID
        assertEquals("7:#42", ev(42, "Alice").key("account"));
        assertEquals("7:#42", ev(42, "alice@example.com").key("account"));
        assertEquals("7:#42", ev(42, "13800000000").key("account"));
        // 没有账号 ID：小写登录名；以 # 开头的补一个 #，撞不上 #ID
        assertEquals("7:bob", ev(0, "Bob").key("account"));
        assertEquals("7:##42", ev(0, "#42").key("account"));
        assertEquals("", ev(0, "").key("account"));
    }
}
