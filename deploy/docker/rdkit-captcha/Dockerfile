# syntax=docker/dockerfile:1
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# RDKit Cairo 渲染器依赖的系统库（libXrender、libXext、libcairo 等）
#
# apt 的下载目录挂成 BuildKit cache：改一个包名不必把上百 MB 的 .deb 重下一遍。
# 前提是先删掉基础镜像里的 docker-clean —— 它会在每次 install 后清空 /var/cache/apt，
# 留着的话缓存永远是空的。两个目录都是 cache mount，不进镜像层，
# 因此也不再需要收尾的 `rm -rf /var/lib/apt/lists/*`。
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        libxrender1 libxext6 libcairo2 libpango-1.0-0 libpangocairo-1.0-0 \
        libglib2.0-0 libsm6 libice6 curl \
        fonts-liberation fonts-dejavu-core fontconfig \
    && fc-cache -f

# 同理：rdkit 的 wheel 有几十 MB，缓存住之后重建这一层不再走网络。
# 用 cache mount 而不是 --no-cache-dir，镜像体积同样不受影响（缓存不落层）。
# 版本刻意不钉：这是可选组件，跟随上游即可；要复现构建请自行加 requirements.txt。
RUN --mount=type=cache,target=/root/.cache/pip,sharing=locked \
    pip install rdkit flask gunicorn edge-tts Pillow cairosvg

WORKDIR /app
COPY deploy/docker/rdkit-captcha/server.py /app/server.py

EXPOSE 5000

# 生产环境用 gunicorn，开发可直接 flask run
CMD ["gunicorn", "--bind", "0.0.0.0:5000", "--workers", "2", "--timeout", "60", "server:app"]
