"""标签表（docs/music-personalization-3.md「标签表」）。

键固定，与 Go 的 services/music/contract/tags.go 各一份，内容必须一致；改这里也要改那边。
深度模型的标签多热向量按 TAG_VOCAB 的顺序排列（genres、moods、scenes、era、vocal、flags）。
"""

from __future__ import annotations

from collections.abc import Iterable

# 曲风（最多 3 个，共 32）
GENRES: tuple[str, ...] = (
    "pop", "rock", "folk", "electronic", "hiphop", "rnb", "jazz", "classical",
    "light", "guofeng", "soundtrack", "acg", "game", "children", "metal", "punk",
    "indie", "country", "blues", "reggae", "latin", "dance", "postrock", "instrumental",
    "chinese_opera", "world", "ethnic", "religious", "internet", "city_pop", "ambient", "lofi",
)

# 情绪（最多 3 个，共 16）
MOODS: tuple[str, ...] = (
    "happy", "sad", "calm", "energetic", "romantic", "lonely", "nostalgic", "healing",
    "angry", "dreamy", "epic", "chill", "sweet", "dark", "hopeful", "tense",
)

# 场景（最多 3 个，共 14）
SCENES: tuple[str, ...] = (
    "workout", "study", "sleep", "commute", "party", "driving", "work",
    "relax", "rainy", "night", "morning", "travel", "gaming", "wedding",
)

# 年代（或空）
ERAS: tuple[str, ...] = ("1960s", "1970s", "1980s", "1990s", "2000s", "2010s", "2020s")

# 人声（或空）
VOCALS: tuple[str, ...] = ("male", "female", "group", "none")

# 标记（子集）
FLAGS: tuple[str, ...] = ("live", "cover", "instrumental", "remix", "acoustic")

# 多热向量的键表：「分组:键」，同名键在不同分组里各占一位（如 genres:instrumental 与 flags:instrumental）
TAG_VOCAB: tuple[str, ...] = (
    tuple(f"genre:{k}" for k in GENRES)
    + tuple(f"mood:{k}" for k in MOODS)
    + tuple(f"scene:{k}" for k in SCENES)
    + tuple(f"era:{k}" for k in ERAS)
    + tuple(f"vocal:{k}" for k in VOCALS)
    + tuple(f"flag:{k}" for k in FLAGS)
)
TAG_INDEX: dict[str, int] = {k: i for i, k in enumerate(TAG_VOCAB)}
NUM_TAGS = len(TAG_VOCAB)


def tag_indices(
    genres: Iterable[str] | None = None,
    moods: Iterable[str] | None = None,
    scenes: Iterable[str] | None = None,
    era: str | None = None,
    vocal: str | None = None,
    flags: Iterable[str] | None = None,
) -> list[int]:
    """一首歌的标签 → 多热下标（不在表里的键丢弃，去重，保持顺序）。"""
    out: list[int] = []
    seen: set[int] = set()

    def add(group: str, key: str | None) -> None:
        if not key:
            return
        idx = TAG_INDEX.get(f"{group}:{key.strip().lower()}")
        if idx is not None and idx not in seen:
            seen.add(idx)
            out.append(idx)

    for g in genres or ():
        add("genre", g)
    for m in moods or ():
        add("mood", m)
    for s in scenes or ():
        add("scene", s)
    add("era", era)
    add("vocal", vocal)
    for f in flags or ():
        add("flag", f)
    return out
