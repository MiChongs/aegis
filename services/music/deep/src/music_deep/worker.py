"""NATS JetStream 消费者：MUSIC_JOBS 流的 music.job.deep.>，持久拉取消费者 music-deep（工作队列，一条任务一个实例）。

训练跑在线程里，事件循环这边每 20 秒 in_progress() 续期，避免 AckWait 到期被别的实例重复领走。
训练失败（已写进训练记录）照样 ack；进程关停中断的 nak，由别的实例或重启后的自己接着做。
"""

from __future__ import annotations

import asyncio
import json
import logging
import signal
import threading
import time

import nats
from nats.errors import TimeoutError as NatsTimeout
from nats.js.api import AckPolicy, ConsumerConfig

from .config import Settings
from .metrics import Metrics
from .pipeline import run_job
from .store import CH, PG
from .train import Cancelled

log = logging.getLogger("music_deep.worker")

STREAM = "MUSIC_JOBS"
SUBJECT = "music.job.deep.>"
DURABLE = "music-deep"
ACK_WAIT = 60.0
PROGRESS_EVERY = 20.0


class Worker:
    def __init__(self, s: Settings, metrics: Metrics) -> None:
        self.s = s
        self.metrics = metrics
        self.pg = PG(s.pg_dsn)
        self.ch: CH | None = None
        self.nc: nats.NATS | None = None
        self.stop = asyncio.Event()
        self.cancel = threading.Event()
        self.loop: asyncio.AbstractEventLoop | None = None

    def ready(self) -> bool:
        return self.metrics.consumer_ready and self.nc is not None and self.nc.is_connected

    async def run(self) -> None:
        self.loop = asyncio.get_running_loop()
        for sig in (signal.SIGTERM, signal.SIGINT):
            try:
                self.loop.add_signal_handler(sig, self._shutdown)
            except NotImplementedError:  # pragma: no cover
                pass
        await asyncio.to_thread(self._init_stores)
        self.nc = await nats.connect(
            self.s.nats_url, name="music-deep", max_reconnect_attempts=-1, reconnect_time_wait=2,
            error_cb=self._on_error, disconnected_cb=self._on_disconnect, reconnected_cb=self._on_reconnect,
        )
        js = self.nc.jetstream()
        sub = None
        while not self.stop.is_set() and sub is None:
            try:
                sub = await js.pull_subscribe(
                    SUBJECT, durable=DURABLE, stream=STREAM,
                    config=ConsumerConfig(
                        durable_name=DURABLE, ack_policy=AckPolicy.EXPLICIT, ack_wait=ACK_WAIT,
                        max_deliver=5, max_ack_pending=64, filter_subject=SUBJECT,
                    ),
                )
            except Exception as e:  # noqa: BLE001  流还没建（Go 端负责建流）或主题不在流里
                log.warning("创建消费者失败（%s），30 秒后重试；MUSIC_JOBS 流需包含主题 %s", e, SUBJECT)
                await self._sleep(30)
        if sub is None:
            await self.nc.drain()
            return
        self.metrics.consumer_ready = True
        log.info("开始消费 %s / %s（消费者 %s）", STREAM, SUBJECT, DURABLE)
        while not self.stop.is_set():
            try:
                msgs = await sub.fetch(1, timeout=5)
            except NatsTimeout:
                continue
            except Exception as e:  # noqa: BLE001
                if self.stop.is_set():
                    break
                log.warning("拉取任务失败：%s", e)
                await self._sleep(1)
                continue
            for msg in msgs:
                await self._handle(msg)
        self.metrics.consumer_ready = False
        try:
            await self.nc.drain()
        except Exception:  # noqa: BLE001
            pass

    def _init_stores(self) -> None:
        self.pg.ensure_schema()
        self.ch = CH(self.s)

    async def _sleep(self, sec: float) -> None:
        try:
            await asyncio.wait_for(self.stop.wait(), timeout=sec)
        except TimeoutError:
            pass

    def _shutdown(self) -> None:
        log.info("收到退出信号，停止接新任务")
        self.stop.set()
        self.cancel.set()

    async def _on_error(self, e: Exception) -> None:
        log.warning("NATS 错误：%s", e)

    async def _on_disconnect(self) -> None:
        log.warning("NATS 连接断开")

    async def _on_reconnect(self) -> None:
        log.info("NATS 已重连")

    def publish_threadsafe(self, subject: str, data: bytes) -> None:
        """供训练线程调用：在事件循环里发普通 NATS 消息并 flush。"""
        assert self.loop is not None and self.nc is not None
        nc = self.nc

        async def go() -> None:
            await nc.publish(subject, data)
            await nc.flush(timeout=5)

        asyncio.run_coroutine_threadsafe(go(), self.loop).result(timeout=10)

    async def _handle(self, msg) -> None:  # noqa: ANN001
        try:
            job = json.loads(msg.data)
            run_id = int(job["run_id"])
            if run_id <= 0:
                raise ValueError("run_id 必须为正")
        except Exception as e:  # noqa: BLE001  毒消息：不重投
            log.warning("任务消息无效（%s）：%r", e, msg.data[:200])
            self.metrics.job("invalid")
            await msg.term()
            return
        log.info("领到任务：run %d，app %s", run_id, job.get("app_id"))

        async def keepalive() -> None:
            while True:
                await asyncio.sleep(PROGRESS_EVERY)
                try:
                    await msg.in_progress()
                except Exception as e:  # noqa: BLE001
                    log.warning("续期失败：%s", e)

        ka = asyncio.create_task(keepalive())
        self.metrics.running = 1
        started = time.monotonic()
        try:
            assert self.ch is not None
            out = await asyncio.to_thread(
                run_job, run_id, pg=self.pg, ch=self.ch, publish=self.publish_threadsafe,
                threads=self.s.threads, cancel=self.cancel,
            )
        except Cancelled:
            log.info("run %d 因关停中断，退回队列", run_id)
            self.metrics.job("cancelled")
            await msg.nak()
            return
        except Exception:  # noqa: BLE001  连失败都没能写进记录（如 PG 不可用）：稍后重投
            log.exception("run %d 处理异常，30 秒后重投", run_id)
            self.metrics.job("error")
            await msg.nak(delay=30)
            return
        finally:
            ka.cancel()
            self.metrics.running = 0
        self.metrics.job(out.status, time.monotonic() - started if out.status != "skipped" else None, out.published)
        log.info("run %d 结束：%s%s", run_id, out.status, "（已发布）" if out.published else "")
        await msg.ack()
