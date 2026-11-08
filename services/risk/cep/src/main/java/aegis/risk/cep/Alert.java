package aegis.risk.cep;

import com.fasterxml.jackson.annotation.JsonProperty;
import io.nats.client.impl.Headers;
import io.synadia.flink.message.SinkConverter;
import io.synadia.flink.message.SinkMessage;
import org.apache.flink.cep.functions.PatternProcessFunction;
import org.apache.flink.util.Collector;

import java.io.Serializable;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** 告警（与 Go 侧 analytics.Alert 同形） */
public class Alert implements Serializable {
    public String id;
    public long ts;
    @JsonProperty("app_id") public long appId;
    public String pattern;
    @JsonProperty("pattern_name") public String patternName;
    public String entity;
    public String value;
    @JsonProperty("user_id") public long userId;
    @JsonProperty("device_id") public String deviceId = "";
    public String ip = "";
    public String severity;
    @JsonProperty("event_ids") public List<String> eventIds = new ArrayList<>();
    public Map<String, Object> detail = new LinkedHashMap<>();

    public Alert() {}

    /** 命中 → 告警。ID 由模式与命中的事件确定：同一次命中重放（故障恢复）得到同一个 ID，JetStream 按它去重 */
    public static class Select extends PatternProcessFunction<RiskEvent, Alert> {
        private final PatternConfig cfg;

        public Select(PatternConfig cfg) {
            this.cfg = cfg;
        }

        @Override
        public void processMatch(Map<String, List<RiskEvent>> match, Context ctx, Collector<Alert> out) throws Exception {
            Alert a = new Alert();
            a.pattern = cfg.key;
            a.patternName = cfg.name;
            a.entity = cfg.keyBy;
            a.severity = cfg.severity;
            a.ts = System.currentTimeMillis();
            RiskEvent last = null;
            Map<String, Object> steps = new LinkedHashMap<>();
            for (int i = 0; i < cfg.steps.size(); i++) {
                List<RiskEvent> evs = match.get(Patterns.stepName(i));
                if (evs == null) {
                    continue;
                }
                List<Object> summaries = new ArrayList<>();
                for (RiskEvent e : evs) {
                    a.eventIds.add(e.id);
                    summaries.add(e.summary());
                    last = e;
                }
                String label = cfg.steps.get(i).name == null || cfg.steps.get(i).name.isEmpty() ? Patterns.stepName(i) : cfg.steps.get(i).name;
                steps.put(label, summaries);
            }
            if (last == null) {
                return;
            }
            a.appId = last.appId;
            a.userId = last.userId;
            a.deviceId = last.deviceId == null ? "" : last.deviceId;
            a.ip = last.ip == null ? "" : last.ip;
            String key = last.key(cfg.keyBy);
            a.value = key.substring(key.indexOf(':') + 1);
            a.detail.put("steps", steps);
            a.detail.put("withinSeconds", cfg.withinSeconds);
            byte[] sum = MessageDigest.getInstance("SHA-256").digest((cfg.key + "|" + String.join(",", a.eventIds)).getBytes(StandardCharsets.UTF_8));
            a.id = HexFormat.of().formatHex(sum, 0, 16);
            out.collect(a);
        }
    }

    /** JetStream Sink 的消息转换：带 Nats-Msg-Id */
    public static class Converter implements SinkConverter<Alert> {
        @Override
        public SinkMessage convert(Alert a) {
            try {
                Headers h = new Headers();
                h.put("Nats-Msg-Id", a.id);
                return new SinkMessage(Json.MAPPER.writeValueAsBytes(a), h);
            } catch (Exception e) {
                throw new IllegalStateException(e);
            }
        }
    }
}
