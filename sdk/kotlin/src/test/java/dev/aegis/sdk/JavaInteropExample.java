package dev.aegis.sdk;

import kotlinx.serialization.json.JsonElement;

import java.io.File;
import java.io.IOException;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Java 调用示例。
 *
 * 这个类**不会被执行**，它的作用是让编译器证明「Kotlin 写的 SDK 从 Java 用起来是顺的」：
 * 默认参数有没有生成重载（@JvmOverloads）、companion 上的工厂方法能不能静态调用
 * （@JvmStatic）、属性有没有生成 getter —— 这些只有真的用 Java 写一遍才发现得了，
 * 而不是等接入方来提 issue。
 */
public final class JavaInteropExample {

    private JavaInteropExample() {
    }

    public static void main(String[] args) throws IOException {
        // @JvmStatic 让它是静态调用，而不是 AegisClient.Companion.builder(...)
        AegisClient client = AegisClient.builder("https://api.example.com", "demo_app")
                .tokenStore(AegisTokenStore.Companion.inMemory())
                .build();

        // 先读配置：安全等级、登录方式、注册字段都在里面
        AegisConfig config = client.config(false);
        System.out.println("安全等级：" + config.getSecurity().getLevel());
        System.out.println("可用登录方式：" + config.getAuth().getLoginMethods());

        try {
            // @JvmOverloads 让 Kotlin 那边的默认参数在 Java 里变成重载，
            // 不必把 deviceId / captcha 一路传 null
            AegisSession session = client.getAuth().loginWithPassword("alice", "secret");

            if (session.getRequiresSecondFactor()) {
                SecondFactorChallenge challenge = session.getChallenge();
                // challenge 不为 null 时才有 challengeId
                session = client.getAuth().verifySecondFactor(challenge.getChallengeId(), "123456");
            }
            if (session.getPasswordChangeRequired()) {
                System.out.println("该账号被要求先改密码");
                return;
            }

            JsonElement profile = client.getMe().profile();
            System.out.println("资料：" + profile);

            Map<String, Object> update = new LinkedHashMap<>();
            update.put("nickname", "Alice");
            client.getMe().updateProfile(update);

            client.getMe().uploadAvatar(new File("avatar.png"));

            // 目录里没封的接口用通用调用；路径照 config.getOperations() 抄
            client.call("GET", "/points/overview", null, java.util.Collections.emptyMap(), true);
        } catch (AegisException error) {
            switch (error.getKind()) {
                case BUSINESS:
                    System.out.println("业务拒绝：" + error.getMessage());
                    break;
                case AUTH:
                    System.out.println("需要重新登录");
                    break;
                case TRANSPORT:
                    System.out.println("接入配置有误：" + error.getHint());
                    break;
                case NETWORK:
                    System.out.println("网络异常，可重试");
                    break;
            }
        }
    }
}
