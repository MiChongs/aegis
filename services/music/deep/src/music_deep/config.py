"""环境变量与应用配置（深度模型部分）。"""

from __future__ import annotations

import os
from dataclasses import asdict, dataclass, field
from typing import Any

# 第三期精排特征（20 个，顺序固定，与 Go model.RankFeatures 一致）
RANK_FEATURES: tuple[str, ...] = (
    "als", "itemcf", "artist", "trending", "explore",
    "foldin", "pop", "complete", "skip", "artist_aff",
    "recent", "fatigue", "lang_match", "fresh", "pref_artist",
    "position",
    "seq", "tag_match", "semantic", "llm",
)
NUM_RANK_FEATURES = len(RANK_FEATURES)

# 语义向量维度（模型输入固定 256；表里的向量长度不同时截断或补零）
EMBED_DIM = 256


@dataclass
class Settings:
    """进程配置（MUSIC_* 环境变量）。"""

    pg_dsn: str = ""
    ch_host: str = ""
    ch_http_port: int = 8123
    ch_user: str = "default"
    ch_password: str = ""
    ch_db: str = "aegis_music"
    nats_url: str = ""
    threads: int = 4
    metrics_host: str = "0.0.0.0"
    metrics_port: int = 8099

    @classmethod
    def from_env(cls, env: dict[str, str] | None = None) -> Settings:
        e = dict(os.environ if env is None else env)
        s = cls()
        s.pg_dsn = e.get("MUSIC_PG_DSN", "").strip()
        # MUSIC_CLICKHOUSE_ADDR 是 Go 用的原生协议地址（host:9000，可逗号分隔多个）；
        # clickhouse-connect 走 HTTP：优先 MUSIC_CLICKHOUSE_HTTP_ADDR，没有就取原生地址的主机 + 8123
        http_addr = e.get("MUSIC_CLICKHOUSE_HTTP_ADDR", "").strip()
        native = e.get("MUSIC_CLICKHOUSE_ADDR", "").split(",")[0].strip()
        if http_addr:
            s.ch_host, port = _split_host_port(http_addr, 8123)
            s.ch_http_port = port
        elif native:
            s.ch_host, _ = _split_host_port(native, 9000)
            s.ch_http_port = 8123
        s.ch_user = e.get("MUSIC_CLICKHOUSE_USER", "").strip() or "default"
        s.ch_password = e.get("MUSIC_CLICKHOUSE_PASSWORD", "")
        s.ch_db = e.get("MUSIC_CLICKHOUSE_DB", "").strip() or "aegis_music"
        s.nats_url = e.get("MUSIC_NATS_URL", "").strip()
        try:
            s.threads = max(1, int(e.get("MUSIC_DEEP_THREADS", "4") or 4))
        except ValueError:
            s.threads = 4
        host, port = _split_host_port(e.get("MUSIC_DEEP_METRICS_ADDR", "").strip() or ":8099", 8099)
        s.metrics_host, s.metrics_port = host or "0.0.0.0", port
        return s

    def validate(self) -> list[str]:
        errs = []
        if not self.pg_dsn:
            errs.append("MUSIC_PG_DSN 未配置")
        if not self.ch_host:
            errs.append("MUSIC_CLICKHOUSE_ADDR（或 MUSIC_CLICKHOUSE_HTTP_ADDR）未配置")
        if not self.nats_url:
            errs.append("MUSIC_NATS_URL 未配置")
        return errs


def _split_host_port(addr: str, default_port: int) -> tuple[str, int]:
    addr = addr.strip()
    for prefix in ("http://", "https://", "tcp://", "clickhouse://"):
        if addr.startswith(prefix):
            addr = addr[len(prefix):]
    addr = addr.split("/")[0]
    if addr.startswith("["):  # [::1]:8123
        host, _, rest = addr[1:].partition("]")
        port = rest.lstrip(":")
        return host, int(port) if port else default_port
    if addr.count(":") == 1:
        host, port = addr.split(":")
        return host, int(port) if port else default_port
    return addr, default_port


@dataclass
class DeepParams:
    """应用配置里的 deep（docs 第五节：范围由 Go 的 PUT /config 校验，这里再夹一次防脏数据）。"""

    enabled: bool = True
    d: int = 64
    layers: int = 2
    heads: int = 2
    maxLen: int = 50
    epochs: int = 20
    minItemCount: int = 3
    minLift: float = 1.0
    # 以下不在应用配置里，固定值（写进 params 方便排查）
    ffn: int = 256
    dropout: float = 0.2
    lr: float = 1e-3
    weightDecay: float = 0.01
    batchSize: int = 128
    negatives: int = 512
    patience: int = 3

    @classmethod
    def from_config(cls, raw: dict[str, Any] | None) -> DeepParams:
        p = cls()
        if not isinstance(raw, dict):
            return p
        for k in ("enabled",):
            if isinstance(raw.get(k), bool):
                setattr(p, k, raw[k])
        for k in ("d", "layers", "heads", "maxLen", "epochs", "minItemCount"):
            v = raw.get(k)
            if isinstance(v, (int, float)) and not isinstance(v, bool):
                setattr(p, k, int(v))
        v = raw.get("minLift")
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            p.minLift = float(v)
        p.clamp()
        return p

    def clamp(self) -> None:
        if self.d not in (32, 48, 64, 96, 128):
            self.d = 64
        self.layers = min(max(self.layers, 1), 4)
        if self.heads not in (1, 2, 4) or self.d % self.heads != 0:
            self.heads = 2 if self.d % 2 == 0 else 1
        self.maxLen = min(max(self.maxLen, 10), 200)
        self.epochs = min(max(self.epochs, 1), 100)
        self.minItemCount = min(max(self.minItemCount, 1), 50)
        self.minLift = min(max(self.minLift, 0.0), 3.0)

    def to_json(self) -> dict[str, Any]:
        return asdict(self)


@dataclass
class AppConfig:
    """训练用到的应用配置片段（music_configs.config；缺的字段取默认值，同 Go appcfg.Default）。"""

    windowDays: int = 180
    autoPublish: bool = True
    deep: DeepParams = field(default_factory=DeepParams)
    # blend 前 5 个通道权重（als、itemcf、artist、trending、explore），精排基线「融合分」用
    blend: tuple[float, float, float, float, float] = (1.0, 0.8, 0.5, 0.3, 0.1)
    rankerEnabled: bool = True
    rankerMinSamples: int = 2000

    @classmethod
    def from_json(cls, raw: dict[str, Any] | None) -> AppConfig:
        c = cls()
        if not isinstance(raw, dict):
            return c
        v = raw.get("windowDays")
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            c.windowDays = min(max(int(v), 7), 730)
        if isinstance(raw.get("autoPublish"), bool):
            c.autoPublish = raw["autoPublish"]
        c.deep = DeepParams.from_config(raw.get("deep"))
        b = raw.get("blend")
        if isinstance(b, dict):
            keys = ("als", "itemcf", "artist", "trending", "explore")
            vals = []
            for k, dv in zip(keys, c.blend, strict=True):
                x = b.get(k)
                vals.append(float(x) if isinstance(x, (int, float)) and not isinstance(x, bool) else dv)
            c.blend = tuple(vals)  # type: ignore[assignment]
        r = raw.get("ranker")
        if isinstance(r, dict):
            if isinstance(r.get("enabled"), bool):
                c.rankerEnabled = r["enabled"]
            ms = r.get("minSamples")
            if isinstance(ms, (int, float)) and not isinstance(ms, bool):
                c.rankerMinSamples = min(max(int(ms), 200), 1_000_000)
        return c
