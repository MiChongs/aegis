"""合成数据：两类口味的听众（A 类听 A 组歌，B 类听 B 组歌），外加一批两边都听的热门歌。

热度基线只会推热门歌；序列模型能学到「听过 A 组的人接着听 A 组」，NDCG 应明显胜过热度。
"""

from __future__ import annotations

import numpy as np

from music_deep.data import TrackInfo, TrackTags

GROUP = 90
POPULAR = 20


def catalog() -> tuple[list[TrackInfo], dict[str, TrackTags]]:
    tracks, tags = [], {}
    n = GROUP * 2 + POPULAR
    for i in range(n):
        wk = f"{i:016x}"
        if i < GROUP:
            artist, title, genres = f"歌手A{i // 10}", f"晴天{i}", ["pop"]
        elif i < GROUP * 2:
            artist, title, genres = f"Singer B{(i - GROUP) // 10}", f"Song {i}", ["rock"]
        else:
            artist, title, genres = f"热门{i}", f"热歌{i}", ["internet"]
        tracks.append(TrackInfo(work_key=wk, title=title, artist=artist, lang=""))
        if i % 3 == 0:  # 一部分有标签，一部分有语义向量
            emb = None
            if i % 6 == 0:
                v = np.zeros(256, dtype=np.float32)
                v[0 if i < GROUP else 1] = 1.0
                emb = v.tolist()
            tags[wk] = TrackTags(genres=genres, moods=["happy"], era="2010s", vocal="male", embedding=emb)
    return tracks, tags


def events(users: int = 400, seed: int = 3) -> list[tuple[int, str, str]]:
    """(user_id, work_key, type)，已按用户、时间排序。"""
    rng = np.random.default_rng(seed)
    rows: list[tuple[int, str, str]] = []
    pop_ids = np.arange(GROUP * 2, GROUP * 2 + POPULAR)
    pop_w = np.linspace(2.0, 1.0, POPULAR)
    pop_w /= pop_w.sum()
    for u in range(users):
        base = 0 if u % 2 == 0 else GROUP
        length = int(rng.integers(8, 30))
        cur = base + int(rng.integers(0, GROUP))
        for _ in range(length):
            r = rng.random()
            if r < 0.2:
                item = int(rng.choice(pop_ids, p=pop_w))
            else:
                # 组内顺着往后听（相邻几首），偶尔跳到组内别处
                cur = base + ((cur - base + int(rng.integers(1, 4))) % GROUP) if rng.random() < 0.7 else base + int(rng.integers(0, GROUP))
                item = cur
            rows.append((10_000 + u, f"{item:016x}", "play_end" if rng.random() < 0.8 else "like"))
    return rows
