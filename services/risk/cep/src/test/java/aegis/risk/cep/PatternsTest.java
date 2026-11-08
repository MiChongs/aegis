package aegis.risk.cep;

import org.apache.flink.cep.CEP;
import org.apache.flink.streaming.api.datastream.DataStream;
import org.apache.flink.streaming.api.environment.StreamExecutionEnvironment;
import org.apache.flink.util.CloseableIterator;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

class PatternsTest {

    static RiskEvent ev(String id, String type, long user, String device, double amount) {
        RiskEvent e = new RiskEvent();
        e.id = id;
        e.type = type;
        e.appId = 1;
        e.userId = user;
        e.deviceId = device;
        e.amount = amount;
        e.ts = System.currentTimeMillis();
        return e;
    }

    static PatternConfig.Step step(String type, int times) {
        PatternConfig.Step s = new PatternConfig.Step();
        s.events = List.of(type);
        s.times = times;
        return s;
    }

    static List<Alert> run(PatternConfig cfg, List<RiskEvent> input) throws Exception {
        StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
        env.setParallelism(1);
        DataStream<RiskEvent> events = env.fromData(input.toArray(new RiskEvent[0]));
        DataStream<Alert> out = CEP.pattern(events.keyBy(e -> e.key(cfg.keyBy)), Patterns.build(cfg))
                .inProcessingTime()
                .process(new Alert.Select(cfg));
        List<Alert> got = new ArrayList<>();
        try (CloseableIterator<Alert> it = out.executeAndCollect()) {
            it.forEachRemaining(got::add);
        }
        return got;
    }

    @Test
    void failuresThenSuccess() throws Exception {
        PatternConfig cfg = new PatternConfig();
        cfg.key = "brute";
        cfg.name = "爆破后成功";
        cfg.keyBy = "user";
        cfg.withinSeconds = 600;
        cfg.steps = List.of(step("login.fail", 3), step("login.success", 1));
        List<Alert> alerts = run(cfg, List.of(
                ev("a1", "login.fail", 7, "d", 0), ev("a2", "login.fail", 7, "d", 0), ev("b1", "login.fail", 8, "d", 0),
                ev("a3", "login.fail", 7, "d", 0), ev("a4", "login.success", 7, "d", 0), ev("b2", "login.success", 8, "d", 0)));
        assertEquals(1, alerts.size());
        Alert a = alerts.get(0);
        assertEquals("7", a.value);
        assertEquals(List.of("a1", "a2", "a3", "a4"), a.eventIds);
        assertEquals(32, a.id.length());
    }

    @Test
    void distinctDevices() throws Exception {
        PatternConfig cfg = new PatternConfig();
        cfg.key = "hop";
        cfg.name = "注册后换设备";
        cfg.keyBy = "user";
        cfg.withinSeconds = 600;
        PatternConfig.Step logins = step("login.success", 3);
        logins.distinct = "device_id";
        cfg.steps = List.of(step("register.success", 1), logins);
        List<Alert> alerts = run(cfg, List.of(
                ev("r", "register.success", 9, "d1", 0), ev("l1", "login.success", 9, "d1", 0), ev("l2", "login.success", 9, "d1", 0),
                ev("l3", "login.success", 9, "d2", 0), ev("l4", "login.success", 9, "d3", 0)));
        assertEquals(1, alerts.size());
        assertEquals(List.of("r", "l1", "l3", "l4"), alerts.get(0).eventIds);
    }

    @Test
    void conditions() {
        PatternConfig.Step s = step("payment.paid", 1);
        PatternConfig.Condition c = new PatternConfig.Condition();
        c.field = "amount";
        c.op = "gte";
        c.value = 500;
        s.conditions = List.of(c);
        assertTrue(StepCondition.matches(s, ev("x", "payment.paid", 1, "d", 800)));
        assertFalse(StepCondition.matches(s, ev("x", "payment.paid", 1, "d", 100)));
        PatternConfig.Condition in = new PatternConfig.Condition();
        in.field = "device_id";
        in.op = "in";
        in.value = List.of("a", "b");
        assertTrue(StepCondition.check(in, "b"));
        assertFalse(StepCondition.check(in, "c"));
        RiskEvent shadow = ev("x", "payment.paid", 1, "d", 800);
        shadow.shadow = true;
        assertFalse(StepCondition.matches(s, shadow));
    }

    @Test
    void fingerprintStable() {
        PatternConfig a = new PatternConfig();
        a.key = "k";
        a.steps = List.of(step("x", 1));
        PatternConfig b = new PatternConfig();
        b.key = "k";
        b.steps = List.of(step("x", 1));
        assertEquals(a.fingerprint(), b.fingerprint());
        b.withinSeconds = 5;
        assertFalse(a.fingerprint().equals(b.fingerprint()));
        assertTrue(Map.of().isEmpty());
    }
}
