"""LightGBM LambdaRank：按 rec_id 分组训练、胜过融合分才导出、文本格式能被 Go leaves 读。"""

from __future__ import annotations

import lightgbm as lgb
import numpy as np

from music_deep.config import RANK_FEATURES
from music_deep.ranker import RankSample, ndcg_at, train_ranker


def _samples(groups: int = 400, size: int = 12, seed: int = 1, n_feat: int = 20) -> list[RankSample]:
    rng = np.random.default_rng(seed)
    out = []
    for g in range(groups):
        for k in range(size):
            x = rng.random(n_feat)
            # 真实偏好由 seq（16）与 tag_match（17）决定，融合分用的前 5 个特征只是噪声
            label = x[16] * 0.7 + x[17] * 0.3 + rng.normal(0, 0.1) > 0.75 if n_feat == 20 else False
            out.append(RankSample(rec_id=f"rec{g}", ts=1_700_000_000_000 + g * 1000 + k, features=x.tolist(), label=bool(label)))
    return out


def test_ranker_beats_blend_and_exports():
    logs: list[str] = []
    r = train_ranker(_samples(), (1.0, 0.8, 0.5, 0.3, 0.1), min_samples=2000, threads=2, log=logs.append)
    info = r.info
    assert info["enabled"], info
    assert info["ndcg10"] > info["baselineNdcg10"]
    assert info["samples"] == 400 * 12 and info["groups"] == 400
    names = [x["name"] for x in info["importance"]]
    assert sorted(names) == sorted(RANK_FEATURES)
    assert names[0] in ("seq", "tag_match")
    assert r.model_text is not None
    head = r.model_text.splitlines()[:3]
    assert "version=v3" in head  # Go 的 leaves 只认 v2 / v3
    assert "objective=lambdarank" in r.model_text
    # 改了版本头后 LightGBM 自己仍能读回，分数一致
    b = lgb.Booster(model_str=r.model_text)
    x = np.random.default_rng(9).random((5, 20))
    assert b.num_feature() == 20
    assert np.isfinite(b.predict(x, raw_score=True)).all()


def test_ranker_requires_20_features():
    old = _samples(groups=300, n_feat=16)
    r = train_ranker(old, (1.0, 0.8, 0.5, 0.3, 0.1), min_samples=200)
    assert not r.info["enabled"]
    assert "样本不足" in r.info["reason"] and r.info["samples"] == 0
    assert r.model_text is None


def test_ranker_not_better_than_blend():
    rng = np.random.default_rng(2)
    out = []
    for g in range(300):
        for k in range(10):
            x = rng.random(20)
            out.append(RankSample(f"r{g}", g * 100 + k, x.tolist(), bool(x[0] > 0.8)))  # 融合分第一项就是答案
    r = train_ranker(out, (1.0, 0.0, 0.0, 0.0, 0.0), min_samples=200, threads=2)
    assert not r.info["enabled"]
    assert "未超过融合分" in r.info["reason"]
    assert r.model_text is None


def test_ndcg_at():
    scores = np.array([3.0, 2.0, 1.0, 0.5, 0.2])
    labels = np.array([0.0, 1.0, 0.0, 0.0, 0.0])
    assert abs(ndcg_at(scores, labels, [(0, 5)]) - 1 / np.log2(3)) < 1e-9
    assert ndcg_at(scores, np.zeros(5), [(0, 5)]) == 0.0
