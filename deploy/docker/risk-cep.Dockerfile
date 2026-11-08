# syntax=docker/dockerfile:1
#
# 风控行为序列作业（Flink CEP，services/risk/cep）。application 模式：jobmanager 用
#   standalone-job --job-classname aegis.risk.cep.RiskCepJob
# 直接带着作业启动，taskmanager 用同一个镜像。
#
#   docker build -f deploy/docker/risk-cep.Dockerfile -t aegis-risk-cep .
#
FROM maven:3-eclipse-temurin-21 AS build
WORKDIR /src
COPY services/risk/cep/pom.xml ./
RUN --mount=type=cache,target=/root/.m2,sharing=locked mvn -q -B dependency:go-offline -DexcludeScope=test || true
COPY services/risk/cep/src ./src
# 单测在 MiniCluster 上跑模式编译（几秒），构建时一起跑
RUN --mount=type=cache,target=/root/.m2,sharing=locked mvn -q -B package

FROM flink:2.3.0-java21
# 检查点与保存点目录：命名卷第一次挂载时沿用镜像里这个目录的属主，flink 用户才写得进去
USER root
RUN mkdir -p /flink-data/checkpoints /flink-data/savepoints && chown -R flink:flink /flink-data
USER flink
COPY --from=build /src/target/risk-cep-1.0.0.jar /opt/flink/usrlib/risk-cep.jar
