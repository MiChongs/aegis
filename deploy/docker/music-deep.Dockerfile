# syntax=docker/dockerfile:1
#
# 音乐个性化第三期：深度模型训练服务 music-deep（services/music/deep，docs/music-personalization-3.md「三、深度模型」）。
# Python 3.12 + PyTorch（CPU 版）+ LightGBM，依赖锁在 services/music/deep/uv.lock：
#   torch 来自 https://download.pytorch.org/whl/cpu（不带 CUDA），其余来自清华 PyPI 镜像（锁文件里记着地址）。
#
#   docker build -f deploy/docker/music-deep.Dockerfile -t aegis-music-deep .
#
FROM python:3.12-slim AS build
ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never UV_PROJECT_ENVIRONMENT=/opt/venv
RUN pip install --no-cache-dir -i ${PIP_INDEX_URL} "uv>=0.8,<0.13"
WORKDIR /src
# 先只装依赖（层缓存：改代码不重新下载 torch）
COPY services/music/deep/pyproject.toml services/music/deep/uv.lock services/music/deep/.python-version ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-install-project
COPY services/music/deep/README.md ./
COPY services/music/deep/src ./src
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-editable

FROM python:3.12-slim
# LightGBM 的轮子依赖系统的 OpenMP 运行库
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgomp1 tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin music
COPY --from=build /opt/venv /opt/venv
ENV PATH=/opt/venv/bin:$PATH PYTHONUNBUFFERED=1 MUSIC_DEEP_THREADS=4
USER music
# /healthz、/readyz、/metrics（只在内网）
EXPOSE 8099
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=10 \
    CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8099/readyz', timeout=3)"]
ENTRYPOINT ["music-deep"]
