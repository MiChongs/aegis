"""入口：python -m music_deep（或 music-deep）。

  music-deep                 常驻消费 MUSIC_JOBS 的 music.job.deep.>
  music-deep run <run_id>    直接跑一条训练记录（排查用，不经过 NATS；达标时照常发布但不发通知）
"""

from __future__ import annotations

import asyncio
import logging
import sys

from .config import Settings
from .metrics import Metrics, serve


def main(argv: list[str] | None = None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s", stream=sys.stderr)
    log = logging.getLogger("music_deep")
    s = Settings.from_env()
    errs = s.validate()
    if argv[:1] == ["run"]:
        errs = [e for e in errs if "NATS" not in e]
    if errs:
        for e in errs:
            log.error("配置错误：%s", e)
        return 2

    if argv[:1] == ["run"]:
        from .pipeline import run_job
        from .store import CH, PG

        pg = PG(s.pg_dsn)
        pg.ensure_schema()
        out = run_job(int(argv[1]), pg=pg, ch=CH(s), threads=s.threads)
        log.info("结果：%s %s", out.status, out.error)
        return 0 if out.status != "failed" else 1

    from .worker import Worker

    metrics = Metrics()
    worker: Worker | None = None

    def ready() -> bool:
        return worker is not None and worker.ready()

    serve(s.metrics_host, s.metrics_port, metrics, ready)
    log.info("music-deep 启动：指标 %s:%d，线程 %d", s.metrics_host, s.metrics_port, s.threads)
    worker = Worker(s, metrics)
    asyncio.run(worker.run())
    log.info("music-deep 已退出")
    return 0


if __name__ == "__main__":
    sys.exit(main())
