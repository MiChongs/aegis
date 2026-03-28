FROM python:3.12-slim

# RDKit Cairo 渲染器依赖的系统库（libXrender、libXext、libcairo 等）
RUN apt-get update && apt-get install -y --no-install-recommends \
    libxrender1 libxext6 libcairo2 libpango-1.0-0 libpangocairo-1.0-0 \
    libglib2.0-0 libsm6 libice6 curl \
    fonts-liberation fonts-dejavu-core fontconfig \
    && fc-cache -fv \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir rdkit flask gunicorn edge-tts Pillow cairosvg

WORKDIR /app
COPY deploy/docker/rdkit-captcha/server.py /app/server.py

EXPOSE 5000

# 生产环境用 gunicorn，开发可直接 flask run
CMD ["gunicorn", "--bind", "0.0.0.0:5000", "--workers", "2", "--timeout", "60", "server:app"]
