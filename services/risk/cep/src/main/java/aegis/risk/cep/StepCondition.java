package aegis.risk.cep;

import org.apache.flink.cep.pattern.conditions.IterativeCondition;

import java.util.ArrayList;
import java.util.List;
import java.util.Objects;

/**
 * 一步的匹配条件：事件类型、场景、处置、字段条件；distinct 要求这一步里已匹配的事件在该字段上各不相同。
 */
public class StepCondition extends IterativeCondition<RiskEvent> {
    private final PatternConfig.Step step;
    private final String stepName;

    public StepCondition(PatternConfig.Step step, String stepName) {
        this.step = step;
        this.stepName = stepName;
    }

    @Override
    public boolean filter(RiskEvent e, Context<RiskEvent> ctx) throws Exception {
        if (!matches(step, e)) {
            return false;
        }
        if (step.distinct != null && !step.distinct.isEmpty() && step.times > 1) {
            Object v = e.field(step.distinct);
            if (v == null || "".equals(v)) {
                return false;
            }
            for (RiskEvent prev : ctx.getEventsForPattern(stepName)) {
                if (Objects.equals(prev.field(step.distinct), v)) {
                    return false;
                }
            }
        }
        return true;
    }

    /** 不依赖上下文的部分（单测直接用） */
    static boolean matches(PatternConfig.Step step, RiskEvent e) {
        if (e.shadow) {
            return false;
        }
        if (!step.events.isEmpty() && !step.events.contains(e.type)) {
            return false;
        }
        if (step.scenes != null && !step.scenes.isEmpty() && !step.scenes.contains(e.scene)) {
            return false;
        }
        if (step.actions != null && !step.actions.isEmpty() && !step.actions.contains(e.action)) {
            return false;
        }
        if (step.conditions != null) {
            for (PatternConfig.Condition c : step.conditions) {
                if (!check(c, e.field(c.field))) {
                    return false;
                }
            }
        }
        return true;
    }

    static boolean check(PatternConfig.Condition c, Object actual) {
        if (actual == null) {
            return false;
        }
        switch (c.op) {
            case "in":
            case "not_in": {
                boolean found = false;
                for (Object item : items(c.value)) {
                    if (equal(actual, item)) {
                        found = true;
                        break;
                    }
                }
                return c.op.equals("in") == found;
            }
            case "eq":
                return equal(actual, c.value);
            case "ne":
                return !equal(actual, c.value);
            default: {
                Double a = number(actual), b = number(c.value);
                if (a == null || b == null) {
                    return false;
                }
                return switch (c.op) {
                    case "gt" -> a > b;
                    case "gte" -> a >= b;
                    case "lt" -> a < b;
                    case "lte" -> a <= b;
                    default -> false;
                };
            }
        }
    }

    private static List<Object> items(Object v) {
        List<Object> out = new ArrayList<>();
        if (v instanceof List<?> l) {
            out.addAll(l);
        } else if (v != null) {
            for (String s : v.toString().split(",")) {
                out.add(s.trim());
            }
        }
        return out;
    }

    private static boolean equal(Object actual, Object expected) {
        if (expected == null) {
            return false;
        }
        if (actual instanceof Double d) {
            Double e = number(expected);
            return e != null && Double.compare(d, e) == 0;
        }
        return actual.toString().equals(expected.toString());
    }

    private static Double number(Object v) {
        if (v instanceof Number n) {
            return n.doubleValue();
        }
        if (v == null) {
            return null;
        }
        try {
            return Double.parseDouble(v.toString());
        } catch (NumberFormatException e) {
            return null;
        }
    }
}
