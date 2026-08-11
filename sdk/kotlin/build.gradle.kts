plugins {
    kotlin("jvm") version "2.1.0"
    kotlin("plugin.serialization") version "2.1.0"
    `java-library`
}

group = "dev.aegis"
version = "1.0.0"

repositories {
    mavenCentral()
}

// 纯 JVM 库，不依赖 Android SDK：
// 同一份产物既给 Android 客户端用（standard 档），也给 Java/Kotlin 服务端用
// （signed 档，appSecret 只能放服务端）。加 Android 依赖会把后一类用户挡在门外。
dependencies {
    api("com.squareup.okhttp3:okhttp:4.12.0")
    api("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
    // X25519 / HKDF / ChaCha20-Poly1305。纯 Java 实现，不需要 NDK，
    // 也不受 Android API 33 才有 XDH 的限制。
    implementation("org.bouncycastle:bcprov-jdk18on:1.79")

    testImplementation(kotlin("test"))
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}

java {
    // Android 与服务端的最大公约数：8 能覆盖所有还在维护的 minSdk。
    sourceCompatibility = JavaVersion.VERSION_1_8
    targetCompatibility = JavaVersion.VERSION_1_8
    withSourcesJar()
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_1_8)
        freeCompilerArgs.add("-Xjvm-default=all")
    }
}

tasks.test {
    useJUnitPlatform()
    testLogging { showStandardStreams = true }
}
