"""本机集成测试（MUSIC_IT=1 才跑）：真 PostgreSQL + ClickHouse（+ NATS）。

往一个新的测试应用（980000000 段）写曲库、行为事件、精排样本，建 queued 训练记录，
直接调训练函数跑完并检查产物与状态；再起一次消费者走一遍 NATS（临时流，结束删掉）。最后删掉全部测试数据。

  set -a; . <env 文件>; set +a
  MUSIC_IT=1 MUSIC_CLICKHOUSE_HTTP_ADDR=127.0.0.1:18123 uv run pytest tests/test_it.py -v -s
"""

from __future__ import annotations

import asyncio
import json
import os
import random
import time
from datetime import UTC, datetime, timedelta

import numpy as np
import pytest
from psycopg.types.json import Jsonb
from safetensors.numpy import load as st_load

import synth
from music_deep.config import RANK_FEATURES, Settings
from music_deep.export import expected_shapes
from music_deep.pipeline import SUBJECT_PUBLISHED, run_job
from music_deep.store import CH, PG

pytestmark = pytest.mark.skipif(os.environ.get("MUSIC_IT") != "1", reason="设置 MUSIC_IT=1 才跑本机集成测试")

NEW_TRACKS = 15  # 曲库里有、但没人听过的新歌
CFG = {"windowDays": 60, "deep": {"d": 32, "layers": 2, "heads": 2, "maxLen": 20, "epochs": 15, "minItemCount": 3, "minLift": 1.0},
       "ranker": {"enabled": True, "minSamples": 500}}


def _ch_events(app_id: int, now: datetime) -> list[list]:
    rows = []
    t = now - timedelta(days=20)
    n = 0
    prev_user = None
    for uid, wk, typ in synth.events(users=300, seed=11):
        if uid != prev_user:
            t = now - timedelta(days=20, minutes=random.randint(0, 600))
            prev_user = uid
        t += timedelta(minutes=4)
        n += 1
        played = 200_000 if typ == "play_end" else 0
        rows.append([app_id, uid, f"it-{n}", typ, t, t, wk, played, 240_000, "completed" if typ == "play_end" else "", "", ""])
        if n % 9 == 0:  # 早跳：不算正向行为，不进序列
            n += 1
            rows.append([app_id, uid, f"it-{n}", "play_end", t, t, wk, 5_000, 240_000, "skipped", "", ""])
    return rows


def _rank_rows(app_id: int, now: datetime, catalog_keys: list[str]) -> tuple[list[list], list[list]]:
    rng = np.random.default_rng(4)
    samples, events = [], []
    n = 0
    for g in range(150):
        rec = f"itrec-{app_id}-{g}"
        ts = now - timedelta(days=10) + timedelta(minutes=g * 5)
        events.append([app_id, 777, f"imp-{g}", "impression", ts, ts, "", 0, 0, "", rec, "home"])
        for k in range(10):
            x = rng.random(20).astype(np.float32)
            wk = catalog_keys[(g * 10 + k) % len(catalog_keys)]
            samples.append([app_id, 777, rec, "home", k, wk, 1, x.tolist(), ts])
            if x[16] * 0.7 + x[17] * 0.3 > 0.7:
                n += 1
                events.append([app_id, 777, f"lab-{n}", "like", ts + timedelta(seconds=30), ts, wk, 0, 0, "", rec, "home"])
        # 第二期旧样本（16 个特征）不应进训练
        samples.append([app_id, 777, rec, "home", 10, f"old{g:013x}", 1, [0.1] * 16, ts])
    return samples, events


@pytest.fixture(scope="module")
def env():
    s = Settings.from_env()
    assert not s.validate(), s.validate()
    pg, ch = PG(s.pg_dsn), CH(s)
    pg.ensure_schema()
    app_id = 980_000_000 + random.randint(0, 999_999)
    now = datetime.now(UTC)
    tracks, _tags = synth.catalog()
    extra = [synth.TrackInfo(work_key=f"ffff{i:012x}", title=f"新歌{i}", artist="新人") for i in range(NEW_TRACKS)]
    tracks = tracks + extra
    inserted_tags = False
    try:
        with pg.conn() as c:
            with c.cursor() as cur:
                cur.executemany(
                    """INSERT INTO music_tracks (app_id, work_key, source, song_id, title, artist, album, cover, duration_ms,
first_seen, last_seen, lang) VALUES (%s,%s,'wy',%s,%s,%s,'','',240000,now(),now(),'')""",
                    [(app_id, t.work_key, str(i), t.title, t.artist) for i, t in enumerate(tracks)],
                )
            c.execute("INSERT INTO music_configs (app_id, config) VALUES (%s, %s)", (app_id, Jsonb(CFG)))
            if c.execute("SELECT to_regclass('music_track_tags') IS NOT NULL").fetchone()[0]:
                try:
                    c.execute(
                        """INSERT INTO music_track_tags (app_id, work_key, genres, moods, scenes, era, energy, vocal, flags,
confidence, embedding, model, tagged_at) VALUES (%s, %s, '{pop}', '{happy}', '{}', '2010s', 0.5, 'male', '{}', 0.9, NULL, 'it', now())""",
                        (app_id, tracks[0].work_key),
                    )
                    inserted_tags = True
                except Exception as e:  # noqa: BLE001  表结构以 Go 为准，对不上就不写标签
                    print("跳过写标签：", e)
        cols = ["app_id", "user_id", "event_id", "type", "ts", "received_at", "work_key", "played_ms", "duration_ms", "reason",
                "rec_id", "scene"]
        ch.client.insert("events", _ch_events(app_id, now), column_names=cols)
        samples, rank_events = _rank_rows(app_id, now, [t.work_key for t in tracks[:-NEW_TRACKS]])
        ch.client.insert("events", rank_events, column_names=cols)
        ch.client.insert("rank_samples", samples, column_names=["app_id", "user_id", "rec_id", "scene", "position", "work_key",
                                                                "model_version", "features", "ts"])
        yield s, pg, ch, app_id, tracks
    finally:
        with pg.conn() as c:
            c.execute("DELETE FROM music_deep_runs WHERE app_id=%s", (app_id,))
            c.execute("DELETE FROM music_tracks WHERE app_id=%s", (app_id,))
            c.execute("DELETE FROM music_configs WHERE app_id=%s", (app_id,))
            if inserted_tags:
                c.execute("DELETE FROM music_track_tags WHERE app_id=%s", (app_id,))
        for table in ("events", "rank_samples"):
            ch.client.command(f"DELETE FROM {table} WHERE app_id = {int(app_id)}")


def _new_run(pg: PG, app_id: int, note: str) -> int:
    with pg.conn() as c:
        return int(c.execute(
            "INSERT INTO music_deep_runs (app_id, status, trigger, note, operator) VALUES (%s, 'queued', 'manual', %s, 'it') RETURNING id",
            (app_id, note),
        ).fetchone()[0])


def _row(pg: PG, run_id: int) -> dict:
    with pg.conn() as c:
        cur = c.execute("SELECT * FROM music_deep_runs WHERE id=%s", (run_id,))
        names = [d.name for d in cur.description]
        return dict(zip(names, cur.fetchone(), strict=True))


def _blobs(pg: PG, run_id: int) -> dict[str, bytes]:
    with pg.conn() as c:
        return {k: bytes(v) for k, v in c.execute("SELECT kind, data FROM music_deep_blobs WHERE run_id=%s", (run_id,)).fetchall()}


def test_run_job_end_to_end(env):
    s, pg, ch, app_id, tracks = env
    run_id = _new_run(pg, app_id, "集成测试")
    sent: list[tuple[str, bytes]] = []
    out = run_job(run_id, pg=pg, ch=ch, publish=lambda subj, data: sent.append((subj, data)), threads=s.threads)
    row = _row(pg, run_id)
    print("\n日志：\n" + "\n".join(row["log"]))
    assert out.status == "succeeded", row["error"]
    assert row["status"] == "succeeded" and row["error"] == ""
    assert row["started_at"] and row["finished_at"] and row["heartbeat_at"]
    m = row["metrics"]
    print("指标：", json.dumps(m, ensure_ascii=False), "\n精排：", json.dumps({k: v for k, v in row["ranker"].items() if k != "importance"},
                                                                            ensure_ascii=False))
    assert m["ndcg20"] > m["baseline"]["ndcg20"]
    # 300 位合成听众 + 精排样本那位（user 777，收藏过很多首）
    assert m["testUsers"] == 301 and m["epochs"] >= 1 and m["trainMs"] > 0
    assert row["users"] == 301 and row["items"] == len(tracks) and row["sequences"] >= 301
    assert row["params"]["d"] == 32 and row["params"]["maxLen"] == 20
    assert row["parity"]["ok"] and row["parity"]["cases"] >= 5 and row["parity"]["maxError"] < 1e-3
    assert [st["name"] for st in row["stages"]] == ["load", "train", "ranker", "export", "save"]
    assert any("留出评估" in line for line in row["log"])
    rk = row["ranker"]
    assert rk["samples"] == 1500  # 16 个特征的旧样本被排除
    assert rk["groups"] == 150

    blobs = _blobs(pg, run_id)
    assert {"model", "meta", "parity"} <= set(blobs)
    assert ("ranker" in blobs) == rk["enabled"]
    if rk["enabled"]:
        assert "version=v3" in blobs["ranker"].decode()[:200]
    meta = json.loads(blobs["meta"])
    assert meta["appId"] == app_id and meta["runId"] == run_id
    assert meta["items"] == sorted(t.work_key for t in tracks)
    assert meta["itemCount"][meta["items"].index(f"ffff{0:012x}")] == 0  # 新歌没人听过，也在物品表里
    assert meta["rankerFeatures"] == list(RANK_FEATURES)
    assert meta["ranker"] == rk
    t = st_load(blobs["model"])
    want = expected_shapes(len(tracks), 32, 2, meta["ffn"], 20)
    assert {k: v.shape for k, v in t.items()} == want
    parity = json.loads(blobs["parity"])
    assert len(parity["cases"]) == row["parity"]["cases"]

    # 第一版且胜过热度：自动发布 + 通知
    assert out.published and row["published_at"] is not None
    assert sent and sent[0][0] == SUBJECT_PUBLISHED and json.loads(sent[0][1]) == {"app_id": app_id, "run_id": run_id}

    # 已结束的记录再投一次：跳过，不改状态
    again = run_job(run_id, pg=pg, ch=ch, threads=s.threads)
    assert again.status == "skipped"
    assert _row(pg, run_id)["status"] == "succeeded"


def test_worker_over_nats(env, monkeypatch):
    """起一次消费者：临时流 + 临时主题，投一条任务，等训练完成与发布通知。"""
    import nats
    from nats.js.api import RetentionPolicy

    from music_deep import worker as wmod
    from music_deep.metrics import Metrics

    s, pg, _ch, app_id, _tracks = env
    if not s.nats_url:
        pytest.skip("没有 MUSIC_NATS_URL")
    tag = f"{int(time.time())}{random.randint(0, 999)}"
    stream, subject = f"MUSIC_DEEP_IT_{tag}", f"music.itdeep.{tag}.>"
    monkeypatch.setattr(wmod, "STREAM", stream)
    monkeypatch.setattr(wmod, "SUBJECT", subject)
    monkeypatch.setattr(wmod, "DURABLE", f"music-deep-it-{tag}")
    run_id = _new_run(pg, app_id, "集成测试（NATS）")

    async def main() -> tuple[dict, list]:
        nc = await nats.connect(s.nats_url)
        js = nc.jetstream()
        await js.add_stream(name=stream, subjects=[subject], retention=RetentionPolicy.WORK_QUEUE)
        got: list = []

        async def on_published(m) -> None:  # noqa: ANN001
            got.append(json.loads(m.data))

        sub = await nc.subscribe(SUBJECT_PUBLISHED, cb=on_published)
        w = wmod.Worker(s, Metrics())
        task = asyncio.create_task(w.run())
        try:
            await js.publish(subject.replace(">", str(app_id)), json.dumps({"run_id": run_id, "app_id": app_id}).encode())
            deadline = time.monotonic() + 300
            row = {}
            while time.monotonic() < deadline:
                await asyncio.sleep(1)
                row = await asyncio.to_thread(_row, pg, run_id)
                if row["status"] in ("succeeded", "failed") and w.metrics.running == 0:
                    break
            await asyncio.sleep(1)
            assert w.ready()
            assert "music_deep_jobs_total" in w.metrics.render()
            return row, got
        finally:
            w._shutdown()
            await asyncio.wait_for(task, timeout=30)
            await sub.unsubscribe()
            await js.delete_stream(stream)
            await nc.close()

    row, got = asyncio.run(main())
    assert row["status"] == "succeeded", row["error"]
    # 上一版刚发布：这一版 NDCG 不低于上一版 × 0.97 才发布；两种结果都合法，但发布了就必须有通知
    if row["published_at"] is not None:
        assert {"app_id": app_id, "run_id": run_id} in got
