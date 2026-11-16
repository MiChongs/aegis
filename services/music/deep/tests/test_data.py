"""标签表、文字规则、序列构造、配置解析。"""

from __future__ import annotations

import numpy as np

from music_deep import tags
from music_deep.config import NUM_RANK_FEATURES, RANK_FEATURES, AppConfig, DeepParams, Settings
from music_deep.data import TrackInfo, TrackTags, build_item_features, build_sequences
from music_deep.text import artist_key, detect_lang, first_artist


def test_tag_table_sizes():
    assert len(tags.GENRES) == 32
    assert len(tags.MOODS) == 16
    assert len(tags.SCENES) == 14
    assert tags.ERAS[0] == "1960s" and tags.ERAS[-1] == "2020s"
    assert tags.VOCALS == ("male", "female", "group", "none")
    assert tags.FLAGS == ("live", "cover", "instrumental", "remix", "acoustic")
    assert tags.NUM_TAGS == 32 + 16 + 14 + 7 + 4 + 5
    assert len(set(tags.GENRES)) == 32 and len(set(tags.MOODS)) == 16 and len(set(tags.SCENES)) == 14


def test_tag_indices_drop_unknown():
    idx = tags.tag_indices(["pop", "nope", "pop"], ["sad"], [], "2000s", "female", ["instrumental", "x"])
    names = [tags.TAG_VOCAB[i] for i in idx]
    assert names == ["genre:pop", "mood:sad", "era:2000s", "vocal:female", "flag:instrumental"]
    assert tags.tag_indices(era="1950s", vocal="") == []


def test_text_rules():
    assert detect_lang("晴天", "周杰伦") == "zh"
    assert detect_lang("Shape of You", "Ed Sheeran") == "en"
    assert detect_lang("夜に駆ける", "YOASOBI") == "ja"
    assert detect_lang("Dynamite", "방탄소년단") == "ko"
    assert detect_lang("123", "") == "other"
    assert first_artist("周杰伦/费玉清") == "周杰伦"
    assert first_artist("A feat. B") == "A"
    assert artist_key("周杰倫 ") == artist_key("周杰伦")
    assert artist_key("Ｔａｙｌｏｒ Swift") == "taylorswift"


def test_item_features_artist_bucket_and_content():
    tracks = [
        TrackInfo("b", "歌1", "甲"), TrackInfo("a", "歌2", "甲/乙"), TrackInfo("c", "Song", "Solo"),
        TrackInfo("d", "歌4", "乙", lang="ja"),
    ]
    emb = [0.0] * 300
    emb[5] = 2.0
    f = build_item_features(tracks, {"a": TrackTags(genres=["pop"], moods=["sad"], embedding=emb)})
    assert f.items == ["a", "b", "c", "d"]
    # 「甲」出现在 2 首里单独编号，「Solo」「乙」只 1 首（a 的第一歌手是甲）→ 其他
    assert f.artist_idx[0] == f.artist_idx[1] != 0
    assert f.artist_idx[2] == 0 and f.artist_idx[3] == 0
    assert f.n_artists == 2
    assert list(f.lang_idx) == [0, 0, 1, 2]  # 表里有 lang 时用表里的
    assert f.tags[0].sum() == 2 and f.tags[1].sum() == 0
    assert f.text.shape == (4, 257)
    assert abs(f.text[0, 5] - 1.0) < 1e-6 and f.text[0, 256] == 1.0 and f.text[1, 256] == 0.0
    assert f.tagged == 1 and f.embedded == 1


def test_sequences_rules():
    index = {k: i for i, k in enumerate("abcdefg")}
    rows = [
        (1, "a", "play_end"), (1, "a", "like"), (1, "b", "play_end"), (1, "x", "play_end"),
        (1, "c", "play_end"), (1, "b", "play_end"), (1, "d", "share"), (1, "d", "dislike"),
        (2, "a", "play_end"), (2, "b", "play_end"),  # 只有 2 首，不进训练
        (3, "e", "play_end"), (3, "f", "dislike"), (3, "f", "play_end"), (3, "f", "undislike"), (3, "g", "like"),
    ]
    s = build_sequences(rows, index)
    assert s.user_ids == [1, 3]
    assert s.seqs[0].tolist() == [0, 1, 2, 1]  # a 连续两次只留一次；x 不在曲库；d 被不喜欢
    assert s.seqs[1].tolist() == [4, 5, 6]  # f 后来取消了不喜欢
    assert s.events == len(rows)


def test_config_parsing():
    s = Settings.from_env({
        "MUSIC_PG_DSN": "postgres://x", "MUSIC_CLICKHOUSE_ADDR": "ch1:9000,ch2:9000", "MUSIC_NATS_URL": "nats://n:4222",
        "MUSIC_DEEP_THREADS": "8", "MUSIC_DEEP_METRICS_ADDR": "127.0.0.1:18099",
    })
    assert (s.ch_host, s.ch_http_port, s.ch_db, s.threads) == ("ch1", 8123, "aegis_music", 8)
    assert (s.metrics_host, s.metrics_port) == ("127.0.0.1", 18099)
    assert s.validate() == []
    s2 = Settings.from_env({"MUSIC_CLICKHOUSE_ADDR": "ch:9000", "MUSIC_CLICKHOUSE_HTTP_ADDR": "127.0.0.1:18123"})
    assert (s2.ch_host, s2.ch_http_port, s2.metrics_port) == ("127.0.0.1", 18123, 8099)

    c = AppConfig.from_json({"windowDays": 30, "deep": {"d": 48, "heads": 4, "layers": 9, "minLift": 1.2},
                             "blend": {"als": 2, "seq": 1.2}, "autoPublish": False})
    assert c.windowDays == 30 and not c.autoPublish
    assert (c.deep.d, c.deep.heads, c.deep.layers, c.deep.minLift) == (48, 4, 4, 1.2)
    assert c.blend == (2.0, 0.8, 0.5, 0.3, 0.1)
    d = AppConfig.from_json(None)
    assert d.deep == DeepParams() and d.windowDays == 180
    p = DeepParams.from_config({"d": 96, "heads": 4})
    assert p.heads == 4
    p = DeepParams.from_config({"d": 48, "heads": 4})
    assert p.heads == 4  # 48 能被 4 整除
    p = DeepParams.from_config({"d": 50})
    assert p.d == 64


def test_rank_features():
    assert NUM_RANK_FEATURES == 20
    assert RANK_FEATURES[15] == "position" and RANK_FEATURES[16:] == ("seq", "tag_match", "semantic", "llm")
    assert np.unique(RANK_FEATURES).size == 20
