# Aegis 主数据库镜像：pgvector + PostGIS
#
# 基底 pgvector/pgvector:pg17 为 Debian 系官方 postgres 衍生镜像，
# 在其上叠加 PostGIS 3（地理风控 / 分析能力依赖，见 migrations/postgres/000054）。
FROM pgvector/pgvector:pg17

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        postgresql-17-postgis-3 \
        postgresql-17-postgis-3-scripts && \
    rm -rf /var/lib/apt/lists/*
