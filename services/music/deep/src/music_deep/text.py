"""文字规则：语种判定、歌手名规范化（照 Go 的 services/music/contract/lang.go、workkey.go 复写）。

深度模型只用它们给物品分桶（歌手、语种），Go 推理用的是导出的 item_repr，不依赖这里，
所以个别冷僻字符与 Go 判得不同也只影响训练特征，不影响数值对齐。
"""

from __future__ import annotations

import unicodedata

from ._zhconv import SIMPCHARS, TRADCHARS, TRADSIMPPAIRS

LANGS: tuple[str, ...] = ("zh", "en", "ja", "ko", "other")
LANG_INDEX: dict[str, int] = {k: i for i, k in enumerate(LANGS)}


def _t2s() -> dict[str, str]:
    m: dict[str, str] = {}
    for i, t in enumerate(TRADCHARS):
        if i < len(SIMPCHARS) and t != SIMPCHARS[i]:
            m[t] = SIMPCHARS[i]
    for i in range(0, len(TRADSIMPPAIRS) - 1, 2):
        m[TRADSIMPPAIRS[i]] = TRADSIMPPAIRS[i + 1]
    return m


_T2S = _t2s()

# Unicode 文字（script）范围：Python 标准库没有 script 属性，按 Go unicode 表的主要区段列出
_KANA = (
    (0x3041, 0x3096), (0x309D, 0x309F), (0x30A1, 0x30FA), (0x30FD, 0x30FF), (0x31F0, 0x31FF),
    (0x32D0, 0x32FE), (0x3300, 0x3357), (0xFF66, 0xFF6F), (0xFF71, 0xFF9D), (0x1B000, 0x1B122),
)
_HANGUL = (
    (0x1100, 0x11FF), (0x302E, 0x302F), (0x3131, 0x318E), (0x3200, 0x321E), (0x3260, 0x327E),
    (0xA960, 0xA97C), (0xAC00, 0xD7A3), (0xD7B0, 0xD7C6), (0xD7CB, 0xD7FB), (0xFFA0, 0xFFDC),
)
_HAN = (
    (0x2E80, 0x2E99), (0x2E9B, 0x2EF3), (0x2F00, 0x2FD5), (0x3005, 0x3005), (0x3007, 0x3007),
    (0x3021, 0x3029), (0x3038, 0x303B), (0x3400, 0x4DBF), (0x4E00, 0x9FFF), (0xF900, 0xFA6D),
    (0xFA70, 0xFAD9), (0x20000, 0x2FA1F), (0x30000, 0x323AF),
)


def _in(cp: int, ranges: tuple[tuple[int, int], ...]) -> bool:
    return any(a <= cp <= b for a, b in ranges)


def _is_letter(ch: str) -> bool:
    return unicodedata.category(ch).startswith("L")


def _is_number(ch: str) -> bool:
    return unicodedata.category(ch).startswith("N")


def detect_lang(title: str, artist: str) -> str:
    """按歌名与歌手的文字判定语种：假名 → ja，谚文 → ko，汉字 → zh，拉丁字母占多数 → en，其余 other。"""
    han = kana = hangul = latin = other = 0
    for s in (title or "", artist or ""):
        for ch in s:
            cp = ord(ch)
            if _in(cp, _KANA):
                kana += 1
            elif _in(cp, _HANGUL):
                hangul += 1
            elif _in(cp, _HAN):
                han += 1
            elif cp < 0x250 and _is_letter(ch):
                latin += 1
            elif _is_letter(ch):
                other += 1
    if kana > 0:
        return "ja"
    if hangul > 0:
        return "ko"
    if han > 0:
        return "zh"
    if latin > 0 and latin >= other:
        return "en"
    return "other"


def _half_width(s: str) -> str:
    out = []
    for ch in s:
        cp = ord(ch)
        if cp == 0x3000:
            ch = " "
        elif 0xFF01 <= cp <= 0xFF5E:
            ch = chr(cp - 0xFEE0)
        out.append(ch)
    return "".join(out)


def _simplified(s: str) -> str:
    return "".join(_T2S.get(ch, ch) for ch in s)


def _keep_word_runes(s: str) -> str:
    return "".join(ch for ch in s if _is_letter(ch) or _is_number(ch))


_ARTIST_SEPS = ("/", "、", ",", "，", "&", ";", "；", " feat.", " feat ", " ft.", " x ", " X ", "|")


def first_artist(s: str) -> str:
    """多位歌手时取第一位（分隔符与 Go FirstArtist 相同）。"""
    s = (s or "").strip()
    cut = len(s)
    for sep in _ARTIST_SEPS:
        i = s.find(sep)
        if 0 < i < cut:
            cut = i
    return s[:cut].strip()


def normalize_artist(s: str) -> str:
    """规范化歌手名：全角转半角、小写、繁转简、只留字母数字。"""
    return _keep_word_runes(_simplified(_half_width((s or "").strip()).lower()))


def artist_key(artist: str) -> str:
    """第一歌手的规范化名（歌手分桶的键）。"""
    return normalize_artist(first_artist(artist))
