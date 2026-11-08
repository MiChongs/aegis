package aegis.risk.cep;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.io.Serializable;

/**
 * 风控事件（与 Go 侧 events.Event 的扁平字段一致）。公开字段 + 无参构造，走 Flink 的 POJO 序列化。
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public class RiskEvent implements Serializable {
    public String id = "";
    public String type = "";
    public String scene = "";
    public long ts;
    @JsonProperty("app_id") public long appId;
    @JsonProperty("user_id") public long userId;
    public String account = "";
    public String ip = "";
    @JsonProperty("ip_subnet") public String ipSubnet = "";
    @JsonProperty("device_id") public String deviceId = "";
    public String country = "";
    public String province = "";
    public String city = "";
    public String isp = "";
    public long asn;
    public String phone = "";
    @JsonProperty("phone_prefix") public String phonePrefix = "";
    @JsonProperty("email_domain") public String emailDomain = "";
    public double amount;
    @JsonProperty("target_user_id") public long targetUserId;
    public String action = "";
    public String level = "";
    public int score;
    public boolean shadow;

    public RiskEvent() {}

    /** 条件里按字段名取值（与 Go 侧 stream.Columns 同名）；数值返回 Double、字符串返回 String */
    public Object field(String name) {
        return switch (name) {
            case "id" -> id;
            case "type" -> type;
            case "scene" -> scene;
            case "ts" -> (double) ts;
            case "app_id" -> (double) appId;
            case "user_id" -> (double) userId;
            case "account" -> account;
            case "ip" -> ip;
            case "ip_subnet" -> ipSubnet;
            case "device_id" -> deviceId;
            case "country" -> country;
            case "province" -> province;
            case "city" -> city;
            case "isp" -> isp;
            case "asn" -> (double) asn;
            case "phone" -> phone;
            case "phone_prefix" -> phonePrefix;
            case "email_domain" -> emailDomain;
            case "amount" -> amount;
            case "target_user_id" -> (double) targetUserId;
            case "action" -> action;
            case "level" -> level;
            case "score" -> (double) score;
            case "shadow" -> shadow ? "true" : "false";
            default -> null;
        };
    }

    /** 按模式的分组主体取键；空串表示这个事件不参与 */
    public String key(String keyBy) {
        String v = switch (keyBy) {
            case "user" -> userId > 0 ? Long.toString(userId) : "";
            case "device" -> deviceId == null ? "" : deviceId;
            case "ip" -> ip == null ? "" : ip;
            case "account" -> account == null ? "" : account.toLowerCase();
            default -> "";
        };
        return v.isEmpty() ? "" : appId + ":" + v;
    }

    private static String nz(String s) { return s == null ? "" : s; }

    /** 告警详情里的事件摘要 */
    public java.util.Map<String, Object> summary() {
        java.util.Map<String, Object> m = new java.util.LinkedHashMap<>();
        m.put("id", nz(id));
        m.put("type", nz(type));
        if (!nz(scene).isEmpty()) m.put("scene", scene);
        m.put("ts", ts);
        if (userId > 0) m.put("user_id", userId);
        if (!nz(deviceId).isEmpty()) m.put("device_id", deviceId);
        if (!nz(ip).isEmpty()) m.put("ip", ip);
        if (!nz(action).isEmpty()) m.put("action", action);
        if (amount != 0) m.put("amount", amount);
        return m;
    }
}
