"""一次深度模型训练的完整流程（一条 music.job.deep.<app> 任务）。

标 running → 读数据 → 训练 SASRec → 评估（对比热度）→ 训练 LightGBM LambdaRank → 导出并自检数值对齐
→ 写产物与指标、标 succeeded → 达标自动发布。每阶段往 log 追加一行带时间的中文日志并刷新 heartbeat_at。
失败写进训练记录（status=failed、error），进程关停时抛 Cancelled（不标失败，由重投接着做）。
"""

from __future__ import annotations

import logging
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime

import torch

from . import export as exporter
from .config import AppConfig
from .data import SequenceBuilder, build_item_features
from .ranker import train_ranker
from .store import CH, PG
from .train import Cancelled, train

log = logging.getLogger("music_deep.pipeline")

SUBJECT_PUBLISHED = "music.deep.published"
MIN_USERS = 10
PREV_RATIO = 0.97
RANK_DAYS = 30

Publisher = Callable[[str, bytes], None]


@dataclass
class Outcome:
    status: str  # skipped | succeeded | failed
    run_id: int
    app_id: int = 0
    published: bool = False
    metrics: dict | None = None
    error: str = ""


class _Heartbeat:
    """后台每 20 秒刷新一次 heartbeat_at（一轮训练可能超过 10 分钟没有日志）。"""

    def __init__(self, pg: PG, run_id: int, every: float = 20.0) -> None:
        self.pg, self.run_id, self.every = pg, run_id, every
        self._stop = threading.Event()
        self._t = threading.Thread(target=self._loop, name=f"heartbeat-{run_id}", daemon=True)

    def _loop(self) -> None:
        while not self._stop.wait(self.every):
            try:
                self.pg.heartbeat(self.run_id)
            except Exception as e:  # noqa: BLE001
                log.warning("心跳写入失败：%s", e)

    def __enter__(self) -> _Heartbeat:
        self._t.start()
        return self

    def __exit__(self, *_exc: object) -> None:
        self._stop.set()
        self._t.join(timeout=5)


def run_job(
    run_id: int,
    *,
    pg: PG,
    ch: CH,
    publish: Publisher | None = None,
    threads: int = 4,
    cancel: threading.Event | None = None,
) -> Outcome:
    run = pg.get_run(run_id)
    if run is None:
        log.warning("训练记录 %d 不存在，跳过", run_id)
        return Outcome("skipped", run_id)
    if run.status not in ("queued", "running"):
        log.info("训练记录 %d 状态为 %s，跳过", run_id, run.status)
        return Outcome("skipped", run_id, run.app_id)
    cfg = AppConfig.from_json(pg.app_config(run.app_id))
    p = cfg.deep
    if not pg.start_run(run_id, p.to_json()):
        log.info("训练记录 %d 已被别的实例接手，跳过", run_id)
        return Outcome("skipped", run_id, run.app_id)

    torch.set_num_threads(max(1, threads))
    app_id = run.app_id
    stages: list[dict] = []

    def logf(msg: str) -> None:
        line = f"{datetime.now().strftime('%H:%M:%S')} {msg}"
        log.info("[run %d] %s", run_id, msg)
        try:
            pg.heartbeat(run_id, line)
        except Exception as e:  # noqa: BLE001
            log.warning("写训练日志失败：%s", e)

    def beat() -> None:
        try:
            pg.heartbeat(run_id)
        except Exception:  # noqa: BLE001
            pass

    def stage(name: str, start: float) -> None:
        stages.append({"name": name, "ms": int((time.monotonic() - start) * 1000)})

    try:
        with _Heartbeat(pg, run_id):
            return _run(run_id, app_id, cfg, pg, ch, publish, threads, cancel, logf, beat, stage, stages)
    except Cancelled:
        logf("进程关停，训练中断，等待重新调度")
        raise
    except Exception as e:  # noqa: BLE001
        log.exception("训练记录 %d 失败", run_id)
        msg = str(e) or e.__class__.__name__
        logf(f"失败：{msg}")
        try:
            pg.fail_run(run_id, stages, msg)
        except Exception:  # noqa: BLE001
            log.exception("标记失败也失败了")
            raise
        return Outcome("failed", run_id, app_id, error=msg)


def _run(run_id, app_id, cfg: AppConfig, pg: PG, ch: CH, publish, threads, cancel, logf, beat, stage, stages) -> Outcome:
    p = cfg.deep
    if not p.enabled:
        logf("应用配置里深度模型未开启，本次按手动任务照常训练")
    seed = (run_id * 2654435761 + 1) % (2**31 - 1)
    logf(f"开始训练：d={p.d}、{p.layers} 层、{p.heads} 头、maxLen={p.maxLen}、最多 {p.epochs} 轮，线程 {threads}")

    # ── 读数据 ──
    t0 = time.monotonic()
    tracks = pg.tracks(app_id)
    tags = pg.track_tags(app_id)
    feats = build_item_features(tracks, tags)
    logf(f"曲库 {feats.n} 首，有标签 {feats.tagged} 首，有语义向量 {feats.embedded} 首，单独编号的歌手 {feats.n_artists - 1} 位")
    if feats.n < 2:
        raise ValueError(f"数据不足：曲库至少需要 2 首作品（当前 {feats.n} 首）")
    from_ms = int((time.time() - cfg.windowDays * 86400) * 1000)
    builder = SequenceBuilder(feats.index)
    ch.stream_sequence_events(app_id, from_ms, builder.add)
    seqs = builder.finish()
    stage("load", t0)
    total_len = sum(len(s) for s in seqs.seqs)
    logf(f"近 {cfg.windowDays} 天正向行为 {seqs.events} 条，进入训练的听众 {len(seqs)} 人，序列总长 {total_len}")
    if len(seqs) < MIN_USERS:
        raise ValueError(f"数据不足：至少需要 {MIN_USERS} 位有 3 次以上正向行为的听众（当前 {len(seqs)} 人）")

    # ── 训练与评估 ──
    t0 = time.monotonic()
    res = train(feats, seqs, p, seed=seed, log=logf, beat=beat, cancel=cancel)
    stage("train", t0)
    metrics = {
        **res.metrics.to_json(),
        "baseline": res.baseline.to_json(),
        "testUsers": res.metrics.users,
        "epochs": res.best_epoch + res.refit_epochs,
        "trainMs": res.train_ms,
    }
    logf(f"留出评估（{res.metrics.users} 人，最优第 {res.best_epoch} 轮）：NDCG@20 {res.metrics.ndcg20:.4f}（热度 "
         f"{res.baseline.ndcg20:.4f}），Recall@20 {res.metrics.recall20:.4f}（热度 {res.baseline.recall20:.4f}）")

    # ── 精排 ──
    t0 = time.monotonic()
    ranker_text: str | None = None
    if not cfg.rankerEnabled:
        ranker_info = {"enabled": False, "reason": "应用配置里精排未开启"}
    else:
        try:
            samples = ch.rank_samples(app_id, int((time.time() - RANK_DAYS * 86400) * 1000))
            rr = train_ranker(samples, cfg.blend, min_samples=cfg.rankerMinSamples, threads=threads, seed=seed, log=logf)
            ranker_info, ranker_text = rr.info, rr.model_text
        except Exception as e:  # noqa: BLE001  精排失败不影响序列模型
            log.exception("精排训练失败")
            ranker_info = {"enabled": False, "reason": f"精排训练失败：{e}"}
    if not ranker_info.get("enabled"):
        logf(f"精排不导出：{ranker_info.get('reason', '')}")
    stage("ranker", t0)

    # ── 导出与数值自检 ──
    t0 = time.monotonic()
    ex = exporter.export(
        res.model, app_id=app_id, run_id=run_id, items=feats.items, item_count=res.item_count,
        heads=p.heads, layers=p.layers, ffn=p.ffn, metrics=metrics, ranker=ranker_info,
        real_seqs=seqs.seqs[:2], seed=seed,
    )
    stage("export", t0)
    logf(f"导出模型 {len(ex.model) / (1 << 20):.1f} MB，数值对齐 {ex.parity_summary['cases']} 例，最大误差 "
         f"{ex.parity_summary['maxError']:.2e}")
    if not ex.parity_summary["ok"]:
        raise RuntimeError(f"数值对齐自检未通过：最大误差 {ex.parity_summary['maxError']:.2e} 超过 {exporter.PARITY_TOLERANCE}")

    # ── 保存 ──
    t0 = time.monotonic()
    blobs = {"model": ex.model, "meta": exporter.dumps(ex.meta), "parity": exporter.dumps(ex.parity)}
    if ranker_text:
        blobs["ranker"] = ranker_text.encode("utf-8")
    save_stages = stages + [{"name": "save", "ms": 0}]
    pg.finish_run(run_id, blobs=blobs, metrics=metrics, ranker=ranker_info, parity=ex.parity_summary, stages=save_stages,
                  users=len(seqs), items=feats.n, sequences=res.windows)
    stage("save", t0)
    pg.set_stages(run_id, stages)
    size = sum(len(b) for b in blobs.values())
    logf(f"产物已保存（{size / (1 << 20):.1f} MB）")

    # ── 自动发布 ──
    published = False
    ndcg, base = res.metrics.ndcg20, res.baseline.ndcg20
    prev = pg.current_published(app_id, run_id)
    prev_ndcg = None
    if prev and isinstance(prev[1], dict) and isinstance(prev[1].get("ndcg20"), (int, float)):
        prev_ndcg = float(prev[1]["ndcg20"])
    if not cfg.autoPublish:
        logf("未开启自动发布，等待手动发布")
    elif ndcg <= 0:
        # 数据太少时模型与热度都可能一首没命中（0 ≥ 0 × minLift 也成立），这种版本不能上线
        logf(f"NDCG@20 为 {ndcg:.4f}，留出集上一首都没命中，不自动发布")
    elif ndcg < base * p.minLift:
        logf(f"NDCG@20 未达到发布门槛（热度 × {p.minLift:.2f} = {base * p.minLift:.4f}），保留为待发布")
    elif prev_ndcg is not None and ndcg < prev_ndcg * PREV_RATIO:
        logf(f"NDCG@20 低于当前发布版本 #{prev[0]}（{prev_ndcg:.4f}）× {PREV_RATIO}，保留为待发布")
    elif pg.publish_run(app_id, run_id):
        published = True
        if publish is not None:
            try:
                publish(SUBJECT_PUBLISHED, exporter.dumps({"app_id": app_id, "run_id": run_id}))
            except Exception as e:  # noqa: BLE001  通知丢了 music-rec 也会轮询已发布版本
                logf(f"发布通知发送失败：{e}")
        logf("已自动发布")
    else:
        logf("自动发布失败：记录状态已变化")
    try:
        pg.prune_blobs(app_id)
    except Exception as e:  # noqa: BLE001
        log.warning("清理旧产物失败：%s", e)
    return Outcome("succeeded", run_id, app_id, published=published, metrics=metrics)
