package aegis.risk.cep;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.io.Serializable;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;

/** 与 Go 侧 bundle.CEPPattern 一一对应（risk-admin /internal/cep/patterns 下发） */
@JsonIgnoreProperties(ignoreUnknown = true)
public class PatternConfig implements Serializable {
    public String key = "";
    public String name = "";
    public boolean enabled = true;
    public String keyBy = "user";
    public List<Long> appIds = new ArrayList<>();
    public List<Step> steps = new ArrayList<>();
    public int withinSeconds = 600;
    public String severity = "medium";

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class Step implements Serializable {
        public String name = "";
        public List<String> events = new ArrayList<>();
        public List<String> scenes = new ArrayList<>();
        public List<String> actions = new ArrayList<>();
        public List<Condition> conditions = new ArrayList<>();
        public int times = 1;
        public boolean contiguous;
        public String distinct = "";
        public boolean absent;
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class Condition implements Serializable {
        public String field = "";
        public String op = "eq";
        public Object value;
    }

    /** 下发列表 */
    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class Envelope {
        public int code;
        public String message;
        public Data data;
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    public static class Data {
        public String version = "";
        public List<PatternConfig> patterns = new ArrayList<>();
    }

    /** 定义摘要：算子 UID 用它，定义没变的模式在换版本时能从保存点恢复部分匹配 */
    public String fingerprint() {
        try {
            String raw = Json.MAPPER.writeValueAsString(this);
            byte[] sum = MessageDigest.getInstance("SHA-256").digest(raw.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(sum, 0, 8);
        } catch (Exception e) {
            throw new IllegalStateException(e);
        }
    }

    public boolean appliesTo(long appId) {
        return appIds == null || appIds.isEmpty() || appIds.contains(appId);
    }
}
