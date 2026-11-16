"""存储访问：PostgreSQL（训练记录、产物、曲库、标签、应用配置）与 ClickHouse（行为序列、精排样本）。

PG 每次操作开一个短连接（autocommit）：训练线程、心跳线程、HTTP 探针各用各的，无需连接池与锁。
"""

from __future__ import annotations

import json
import logging
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Any

import clickhouse_connect
import psycopg
from psycopg.types.json import Jsonb

from .config import NUM_RANK_FEATURES, Settings
from .data import TrackInfo, TrackTags
from .ranker import RankSample

log = logging.getLogger("music_deep.store")

# 与 Go（music-trainer）建表语句一致，两边谁先启动谁建
DEEP_SCHEMA = """
CREATE TABLE IF NOT EXISTS music_deep_runs (
  id BIGSERIAL PRIMARY KEY, app_id BIGINT NOT NULL, status TEXT NOT NULL, trigger TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
  operator TEXT NOT NULL DEFAULT '', params JSONB, metrics JSONB, ranker JSONB, parity JSONB, stages JSONB NOT NULL DEFAULT '[]',
  log JSONB NOT NULL DEFAULT '[]', users INT NOT NULL DEFAULT 0, items INT NOT NULL DEFAULT 0, sequences INT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
  published_at TIMESTAMPTZ, heartbeat_at TIMESTAMPTZ);
CREATE INDEX IF NOT EXISTS music_deep_runs_app ON music_deep_runs (app_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS music_deep_runs_active ON music_deep_runs (app_id) WHERE status IN ('queued','running');
CREATE TABLE IF NOT EXISTS music_deep_blobs (run_id BIGINT NOT NULL REFERENCES music_deep_runs (id) ON DELETE CASCADE,
  kind TEXT NOT NULL, data BYTEA NOT NULL, PRIMARY KEY (run_id, kind));
"""

# Go 的 store.migrate 用同一把咨询锁串行建表，这里也拿它，避免并发 CREATE 撞唯一约束
MIGRATE_LOCK = 778801
STALE_AFTER = "10 minutes"
KEEP_BLOBS = 5


@dataclass
class Run:
    id: int
    app_id: int
    status: str
    heartbeat_stale: bool


class PG:
    def __init__(self, dsn: str) -> None:
        self.dsn = dsn

    @contextmanager
    def conn(self) -> Iterator[psycopg.Connection]:
        with psycopg.connect(self.dsn, autocommit=True, connect_timeout=10) as c:
            yield c

    def ping(self) -> bool:
        try:
            with self.conn() as c:
                c.execute("SELECT 1")
            return True
        except Exception:  # noqa: BLE001
            return False

    def ensure_schema(self) -> None:
        with self.conn() as c:
            c.execute("SELECT pg_advisory_lock(%s)", (MIGRATE_LOCK,))
            try:
                c.execute(DEEP_SCHEMA)
            finally:
                c.execute("SELECT pg_advisory_unlock(%s)", (MIGRATE_LOCK,))

    # ── 训练记录 ──

    def get_run(self, run_id: int) -> Run | None:
        with self.conn() as c:
            row = c.execute(
                f"SELECT id, app_id, status, COALESCE(heartbeat_at < now() - interval '{STALE_AFTER}', true) "
                "FROM music_deep_runs WHERE id=%s",
                (run_id,),
            ).fetchone()
        return Run(*row) if row else None

    def start_run(self, run_id: int, params: dict[str, Any]) -> bool:
        """只接 queued，或 running 但心跳超过 10 分钟的（上一个实例死了）。"""
        with self.conn() as c:
            cur = c.execute(
                f"""UPDATE music_deep_runs SET status='running', started_at=now(), heartbeat_at=now(), params=%s,
  stages='[]', log='[]', error='', metrics=NULL, ranker=NULL, parity=NULL, finished_at=NULL
WHERE id=%s AND (status='queued' OR (status='running' AND (heartbeat_at IS NULL OR heartbeat_at < now() - interval '{STALE_AFTER}')))""",
                (Jsonb(params), run_id),
            )
            return cur.rowcount == 1

    def heartbeat(self, run_id: int, line: str = "") -> None:
        with self.conn() as c:
            if line:
                c.execute(
                    "UPDATE music_deep_runs SET heartbeat_at=now(), log = log || to_jsonb(%s::text) WHERE id=%s",
                    (line, run_id),
                )
            else:
                c.execute("UPDATE music_deep_runs SET heartbeat_at=now() WHERE id=%s", (run_id,))

    def fail_run(self, run_id: int, stages: list[dict], msg: str) -> None:
        with self.conn() as c:
            c.execute(
                "UPDATE music_deep_runs SET status='failed', finished_at=now(), heartbeat_at=now(), stages=%s, error=%s WHERE id=%s",
                (Jsonb(stages), msg.strip()[:4000], run_id),
            )

    def finish_run(
        self,
        run_id: int,
        *,
        blobs: dict[str, bytes],
        metrics: dict,
        ranker: dict,
        parity: dict,
        stages: list[dict],
        users: int,
        items: int,
        sequences: int,
    ) -> None:
        with psycopg.connect(self.dsn, connect_timeout=10) as c, c.transaction():
            c.execute("DELETE FROM music_deep_blobs WHERE run_id=%s", (run_id,))
            with c.cursor() as cur:
                cur.executemany(
                    "INSERT INTO music_deep_blobs (run_id, kind, data) VALUES (%s, %s, %s)",
                    [(run_id, k, v) for k, v in blobs.items()],
                )
            c.execute(
                """UPDATE music_deep_runs SET status='succeeded', finished_at=now(), heartbeat_at=now(), metrics=%s, ranker=%s,
  parity=%s, stages=%s, users=%s, items=%s, sequences=%s, error='' WHERE id=%s""",
                (Jsonb(metrics), Jsonb(ranker), Jsonb(parity), Jsonb(stages), users, items, sequences, run_id),
            )

    def set_stages(self, run_id: int, stages: list[dict]) -> None:
        with self.conn() as c:
            c.execute("UPDATE music_deep_runs SET stages=%s WHERE id=%s", (Jsonb(stages), run_id))

    def current_published(self, app_id: int, exclude: int) -> tuple[int, dict | None] | None:
        """当前发布的深度模型（最近 published_at）。"""
        with self.conn() as c:
            row = c.execute(
                """SELECT id, metrics FROM music_deep_runs WHERE app_id=%s AND id<>%s AND published_at IS NOT NULL
ORDER BY published_at DESC LIMIT 1""",
                (app_id, exclude),
            ).fetchone()
        return (int(row[0]), row[1]) if row else None

    def publish_run(self, app_id: int, run_id: int) -> bool:
        with self.conn() as c:
            cur = c.execute(
                """UPDATE music_deep_runs SET published_at=now() WHERE app_id=%s AND id=%s AND status='succeeded'
  AND EXISTS (SELECT 1 FROM music_deep_blobs b WHERE b.run_id=%s AND b.kind='model')""",
                (app_id, run_id, run_id),
            )
            return cur.rowcount == 1

    def prune_blobs(self, app_id: int, keep: int = KEEP_BLOBS) -> None:
        """只留最近 keep 个成功版本与当前发布版本的产物（记录本身保留）。"""
        with self.conn() as c:
            c.execute(
                """DELETE FROM music_deep_blobs b USING music_deep_runs r
WHERE b.run_id = r.id AND r.app_id = %(app)s
  AND r.id NOT IN (SELECT id FROM music_deep_runs WHERE app_id=%(app)s AND status='succeeded' ORDER BY id DESC LIMIT %(keep)s)
  AND r.id NOT IN (SELECT id FROM music_deep_runs WHERE app_id=%(app)s AND published_at IS NOT NULL ORDER BY published_at DESC LIMIT 1)""",
                {"app": app_id, "keep": keep},
            )

    # ── 曲库、标签、配置 ──

    def app_config(self, app_id: int) -> dict | None:
        with self.conn() as c:
            row = c.execute("SELECT config FROM music_configs WHERE app_id=%s", (app_id,)).fetchone()
        if not row:
            return None
        raw = row[0]
        return json.loads(raw) if isinstance(raw, (str, bytes)) else raw

    def tracks(self, app_id: int) -> list[TrackInfo]:
        with self.conn() as c:
            rows = c.execute(
                'SELECT work_key, title, artist, lang FROM music_tracks WHERE app_id=%s ORDER BY work_key COLLATE "C"',
                (app_id,),
            ).fetchall()
        return [TrackInfo(work_key=r[0], title=r[1] or "", artist=r[2] or "", lang=r[3] or "") for r in rows]

    def track_tags(self, app_id: int) -> dict[str, TrackTags]:
        """music_track_tags 由 music-trainer 的 enricher 建；还没有表或列不全时返回空（内容特征为零）。"""
        with self.conn() as c:
            exists = c.execute("SELECT to_regclass('music_track_tags') IS NOT NULL").fetchone()[0]
            if not exists:
                return {}
            try:
                rows = c.execute(
                    """SELECT work_key, genres, moods, scenes, era, vocal, flags, embedding
FROM music_track_tags WHERE app_id=%s""",
                    (app_id,),
                ).fetchall()
            except psycopg.errors.UndefinedColumn as e:
                log.warning("music_track_tags 列不全，忽略标签：%s", e)
                return {}
        out: dict[str, TrackTags] = {}
        for wk, genres, moods, scenes, era, vocal, flags, emb in rows:
            out[wk] = TrackTags(
                genres=list(genres or []), moods=list(moods or []), scenes=list(scenes or []),
                era=era or "", vocal=vocal or "", flags=list(flags or []),
                embedding=[float(x) for x in emb] if emb else None,
            )
        return out


# 正向行为 + dislike / undislike（用于从序列里删掉不喜欢的歌）；play_end 要听完或 ≥ 30 秒
_SEQ_SQL = """
SELECT user_id, work_key, type
FROM events FINAL
WHERE app_id = {app:UInt64} AND ts >= fromUnixTimestamp64Milli({from_ms:Int64}) AND work_key != ''
  AND (type IN ('like', 'playlist_add', 'download', 'share', 'search_click', 'dislike', 'undislike')
       OR (type = 'play_end' AND (reason = 'completed' OR played_ms >= 30000
                                  OR (duration_ms > 0 AND played_ms >= duration_ms * 0.8))))
ORDER BY user_id, ts, event_id
"""

# 与 Go store.RankSamples 同一套标签规则，多取 rec_id 用来分组，且只要第三期（20 个特征）的样本
_SKIP = ("(type = 'play_end' AND reason IN ('skipped', 'switched') AND played_ms < 30000 "
         "AND (duration_ms = 0 OR played_ms < duration_ms * 0.3))")
_LABEL = ("(countIf(type IN ('like','playlist_add','download','share') OR (type = 'play_end' AND played_ms >= 30000)) > 0 "
          f"OR (countIf(type IN ('click','play_start','search_click')) > 0 AND countIf({_SKIP}) = 0))")
_RANK_SQL = f"""
SELECT s.rec_id, toUnixTimestamp64Milli(s.ts), s.features, toUInt8(ifNull(l.label, 0))
FROM (SELECT rec_id, work_key, features, ts FROM rank_samples FINAL
      WHERE app_id = {{app:UInt64}} AND ts >= fromUnixTimestamp64Milli({{from_ms:Int64}}) AND length(features) = {NUM_RANK_FEATURES}) AS s
INNER JOIN (SELECT DISTINCT rec_id FROM events WHERE app_id = {{app:UInt64}} AND ts >= fromUnixTimestamp64Milli({{from_ms:Int64}})
            AND type = 'impression') AS imp ON imp.rec_id = s.rec_id
LEFT JOIN (
  SELECT rec_id, work_key, toUInt8({_LABEL}) AS label
  FROM events FINAL WHERE app_id = {{app:UInt64}} AND ts >= fromUnixTimestamp64Milli({{from_ms:Int64}}) AND rec_id != '' AND work_key != ''
  GROUP BY rec_id, work_key) AS l ON l.rec_id = s.rec_id AND l.work_key = s.work_key
ORDER BY s.ts DESC LIMIT {{limit:UInt64}}
"""


class CH:
    def __init__(self, s: Settings) -> None:
        self.client = clickhouse_connect.get_client(
            host=s.ch_host, port=s.ch_http_port, username=s.ch_user, password=s.ch_password, database=s.ch_db,
            connect_timeout=10, send_receive_timeout=600, settings={"max_execution_time": 600},
        )

    def ping(self) -> bool:
        try:
            return bool(self.client.ping())
        except Exception:  # noqa: BLE001
            return False

    def stream_sequence_events(self, app_id: int, from_ms: int, fn: Callable[[int, str, str], None]) -> None:
        with self.client.query_row_block_stream(_SEQ_SQL, parameters={"app": app_id, "from_ms": from_ms}) as stream:
            for block in stream:
                for uid, wk, typ in block:
                    fn(int(uid), wk, typ)

    def rank_samples(self, app_id: int, from_ms: int, limit: int = 1_000_000) -> list[RankSample]:
        out: list[RankSample] = []
        with self.client.query_row_block_stream(_RANK_SQL, parameters={"app": app_id, "from_ms": from_ms, "limit": limit}) as stream:
            for block in stream:
                for rec_id, ts, feats, label in block:
                    out.append(RankSample(rec_id=rec_id, ts=int(ts), features=[float(x) for x in feats], label=bool(label)))
        return out
