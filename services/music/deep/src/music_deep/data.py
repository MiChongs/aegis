"""训练数据：曲库 → 物品特征，行为事件 → 用户序列（docs「三、深度模型 / 数据」）。"""

from __future__ import annotations

from collections.abc import Iterable
from dataclasses import dataclass, field

import numpy as np

from .config import EMBED_DIM
from .tags import NUM_TAGS, tag_indices
from .text import LANG_INDEX, LANGS, artist_key, detect_lang

# 正向行为（play_end 的「听完或 ≥ 30 秒」在 SQL 里已经筛过）
POSITIVE_TYPES = frozenset({"play_end", "like", "playlist_add", "download", "share", "search_click"})
MIN_SEQ_LEN = 3


@dataclass
class TrackInfo:
    work_key: str
    title: str = ""
    artist: str = ""
    lang: str = ""


@dataclass
class TrackTags:
    genres: list[str] = field(default_factory=list)
    moods: list[str] = field(default_factory=list)
    scenes: list[str] = field(default_factory=list)
    era: str = ""
    vocal: str = ""
    flags: list[str] = field(default_factory=list)
    embedding: list[float] | None = None


@dataclass
class ItemFeatures:
    items: list[str]  # 按 work_key 排序，下标即物品编号
    index: dict[str, int]
    artist_idx: np.ndarray  # [N]，0 = 「其他」
    n_artists: int
    lang_idx: np.ndarray  # [N]
    tags: np.ndarray  # [N, NUM_TAGS] 多热
    text: np.ndarray  # [N, EMBED_DIM + 1]：语义向量 + has_embedding
    tagged: int = 0
    embedded: int = 0

    @property
    def n(self) -> int:
        return len(self.items)


def build_item_features(tracks: Iterable[TrackInfo], tags: dict[str, TrackTags] | None = None) -> ItemFeatures:
    tags = tags or {}
    rows = sorted({t.work_key: t for t in tracks if t.work_key}.values(), key=lambda t: t.work_key)
    items = [t.work_key for t in rows]
    n = len(items)
    # 歌手：第一歌手规范化名在 ≥ 2 首歌里出现才单独编号，否则归「其他」（0）
    keys = [artist_key(t.artist) for t in rows]
    counts: dict[str, int] = {}
    for k in keys:
        if k:
            counts[k] = counts.get(k, 0) + 1
    vocab = {k: i + 1 for i, k in enumerate(sorted(k for k, c in counts.items() if c >= 2))}
    artist_idx = np.array([vocab.get(k, 0) for k in keys], dtype=np.int64)
    lang_idx = np.array(
        [LANG_INDEX.get(t.lang if t.lang in LANG_INDEX else detect_lang(t.title, t.artist), len(LANGS) - 1) for t in rows],
        dtype=np.int64,
    )
    multi = np.zeros((n, NUM_TAGS), dtype=np.float32)
    text = np.zeros((n, EMBED_DIM + 1), dtype=np.float32)
    tagged = embedded = 0
    for i, wk in enumerate(items):
        tg = tags.get(wk)
        if tg is None:
            continue
        idx = tag_indices(tg.genres, tg.moods, tg.scenes, tg.era, tg.vocal, tg.flags)
        if idx:
            multi[i, idx] = 1.0
            tagged += 1
        if tg.embedding:
            v = np.asarray(tg.embedding[:EMBED_DIM], dtype=np.float32)
            if v.size and np.isfinite(v).all() and np.any(v != 0):
                norm = float(np.linalg.norm(v))
                text[i, : v.size] = v / norm if norm > 0 else v
                text[i, EMBED_DIM] = 1.0
                embedded += 1
    return ItemFeatures(
        items=items,
        index={k: i for i, k in enumerate(items)},
        artist_idx=artist_idx,
        n_artists=len(vocab) + 1,
        lang_idx=lang_idx,
        tags=multi,
        text=text,
        tagged=tagged,
        embedded=embedded,
    )


@dataclass
class Sequences:
    user_ids: list[int]
    seqs: list[np.ndarray]  # 每人一条，物品下标，旧 → 新
    events: int = 0  # 读到的事件数

    def __len__(self) -> int:
        return len(self.seqs)


class SequenceBuilder:
    """按 (user, ts) 顺序喂事件，得到每个人的正向序列。

    规则：同一作品连续出现只留一次；dislike 的作品（最后状态仍是不喜欢）从这个人的序列里删掉；
    不在曲库里的作品跳过；长度 < 3 的人不进训练。
    """

    def __init__(self, index: dict[str, int]) -> None:
        self.index = index
        self.events = 0
        self._users: list[int] = []
        self._seqs: list[np.ndarray] = []
        self._cur_user: int | None = None
        self._cur: list[int] = []
        self._disliked: dict[int, bool] = {}

    def add(self, user_id: int, work_key: str, etype: str) -> None:
        self.events += 1
        if user_id != self._cur_user:
            self._flush()
            self._cur_user = user_id
        idx = self.index.get(work_key)
        if idx is None:
            return
        if etype == "dislike":
            self._disliked[idx] = True
        elif etype == "undislike":
            self._disliked[idx] = False
        elif etype in POSITIVE_TYPES:
            self._cur.append(idx)

    def _flush(self) -> None:
        if self._cur_user is not None and self._cur:
            out: list[int] = []
            for i in self._cur:
                if self._disliked.get(i):
                    continue
                if out and out[-1] == i:
                    continue
                out.append(i)
            if len(out) >= MIN_SEQ_LEN:
                self._users.append(self._cur_user)
                self._seqs.append(np.asarray(out, dtype=np.int64))
        self._cur = []
        self._disliked = {}

    def finish(self) -> Sequences:
        self._flush()
        self._cur_user = None
        return Sequences(user_ids=self._users, seqs=self._seqs, events=self.events)


def build_sequences(rows: Iterable[tuple[int, str, str]], index: dict[str, int]) -> Sequences:
    """rows：(user_id, work_key, type)，已按 (user_id, ts) 排序。"""
    b = SequenceBuilder(index)
    for uid, wk, typ in rows:
        b.add(int(uid), wk, typ)
    return b.finish()


def user_counts(seqs: Iterable[np.ndarray], n_items: int) -> np.ndarray:
    """每个物品被多少人交互过。"""
    cnt = np.zeros(n_items, dtype=np.int64)
    for s in seqs:
        cnt[np.unique(s)] += 1
    return cnt
