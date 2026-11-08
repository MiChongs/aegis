# syntax=docker/dockerfile:1
#
# 风控行为序列作业（Flink CEP，services/risk/cep）。application 模式：jobmanager 用
#   standalone-job --job-classname aegis.risk.cep.RiskCepJob
# 直接带着作业启动，taskmanager 用同一个镜像。
#
#   docker build -f deploy/docker/risk-cep.Dockerfile -t aegis-risk-cep .
#
# 两个构建参数都是可选的：
#   MAVEN_MIRROR  Maven Central 的镜像地址（mirrorOf central），留空直连
#   MAVEN_OPTS    传给 mvn 的 JVM 参数。HK 服务器到 Maven Central 的 IPv4 连接会被重置、IPv6 正常，
#                 生产编排传 -Djava.net.preferIPv6Addresses=true（Java 默认优先 IPv4）
#
FROM maven:3-eclipse-temurin-21 AS build
WORKDIR /src
ARG MAVEN_MIRROR=
ARG MAVEN_OPTS=
ENV MAVEN_OPTS=${MAVEN_OPTS}
RUN if [ -n "$MAVEN_MIRROR" ]; then \
      printf '<settings><mirrors><mirror><id>central-mirror</id><mirrorOf>central</mirrorOf><url>%s</url></mirror></mirrors></settings>\n' "$MAVEN_MIRROR"; \
    else \
      printf '<settings/>\n'; \
    fi > /src/mvn-settings.xml
COPY services/risk/cep/pom.xml ./
RUN --mount=type=cache,target=/root/.m2,sharing=locked mvn -s /src/mvn-settings.xml -q -B dependency:go-offline -DexcludeScope=test || true
COPY services/risk/cep/src ./src
# 单测在 MiniCluster 上跑模式编译（几秒），构建时一起跑
RUN --mount=type=cache,target=/root/.m2,sharing=locked mvn -s /src/mvn-settings.xml -q -B package

FROM flink:2.3.0-java21
# 检查点与保存点目录：命名卷第一次挂载时沿用镜像里这个目录的属主，flink 用户才写得进去
USER root
RUN mkdir -p /flink-data/checkpoints /flink-data/savepoints && chown -R flink:flink /flink-data
USER flink
COPY --from=build /src/target/risk-cep-1.0.0.jar /opt/flink/usrlib/risk-cep.jar
