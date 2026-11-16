"""LightGBM LambdaRank 精排（docs「精排（LightGBM LambdaRank）」）。

样本：rank_samples 里特征长度为 20 的，按 rec_id 分组；按时间留出最后 20% 的组评估 NDCG@10，
与「融合分」（前 5 个特征按 blend 加权求和）排序的 NDCG@10 比较，更好才导出。
"""

from __future__ import annotations

import math
from collections.abc import Callable, Iterable
from dataclasses import dataclass
from typing import Any

import lightgbm as lgb
import numpy as np

from .config import NUM_RANK_FEATURES, RANK_FEATURES

HOLDOUT = 0.2
MIN_GROUPS = 20


@dataclass
class RankSample:
    rec_id: str
    ts: int
    features: list[float]
    label: bool


@dataclass
class RankerResult:
    info: dict[str, Any]
    model_text: str | None


def ndcg_at(scores: np.ndarray, labels: np.ndarray, groups: list[tuple[int, int]], k: int = 10) -> float:
    """有正样本的组的平均 NDCG@k（同分按原顺序）。"""
    total, cnt = 0.0, 0
    for a, b in groups:
        y = labels[a:b]
        if y.sum() <= 0:
            continue
        order = np.argsort(-scores[a:b], kind="stable")[:k]
        dcg = sum(y[i] / math.log2(r + 2) for r, i in enumerate(order))
        ideal = np.sort(y)[::-1][:k]
        idcg = sum(v / math.log2(r + 2) for r, v in enumerate(ideal))
        total += dcg / idcg
        cnt += 1
    return total / cnt if cnt else 0.0


def _pack(groups: list[list[RankSample]]) -> tuple[np.ndarray, np.ndarray, list[int], list[tuple[int, int]]]:
    rows = [s for g in groups for s in g]
    x = np.asarray([s.features for s in rows], dtype=np.float64).reshape(len(rows), NUM_RANK_FEATURES)
    y = np.asarray([1.0 if s.label else 0.0 for s in rows], dtype=np.float64)
    sizes = [len(g) for g in groups]
    bounds, off = [], 0
    for n in sizes:
        bounds.append((off, off + n))
        off += n
    return x, y, sizes, bounds


def _lgb_v3(text: str) -> str:
    """LightGBM 4.x 的文本头是 version=v4，Go 的 leaves 只认 v2 / v3（树的文本格式没变，实测改头后分数完全一致）。"""
    lines = text.split("\n", 3)
    for i in range(min(3, len(lines))):
        if lines[i] == "version=v4":
            lines[i] = "version=v3"
    return "\n".join(lines)


def train_ranker(
    samples: Iterable[RankSample],
    blend: tuple[float, ...],
    *,
    min_samples: int = 2000,
    threads: int = 4,
    seed: int = 1,
    log: Callable[[str], None] | None = None,
) -> RankerResult:
    log = log or (lambda _m: None)
    by_rec: dict[str, list[RankSample]] = {}
    total = 0
    for s in samples:
        if len(s.features) != NUM_RANK_FEATURES:
            continue  # 第二期（16 个特征）的旧样本
        if not all(math.isfinite(v) for v in s.features):
            continue
        by_rec.setdefault(s.rec_id, []).append(s)
        total += 1
    if total < min_samples:
        return RankerResult({"enabled": False, "reason": f"第三期样本不足：{total} 条（至少 {min_samples} 条）", "samples": total}, None)
    # 只有一首的组没有可比较的对，去掉；组按最早的时间排序后留出最后 20%
    groups = [g for g in by_rec.values() if len(g) >= 2]
    for g in groups:
        g.sort(key=lambda s: s.ts)
    groups.sort(key=lambda g: (g[0].ts, g[0].rec_id))
    positives = sum(1 for g in groups for s in g if s.label)
    if len(groups) < MIN_GROUPS or positives == 0:
        return RankerResult({"enabled": False, "reason": f"可用的推荐组不足（{len(groups)} 组，正样本 {positives} 条）",
                             "samples": total, "groups": len(groups)}, None)
    cut = max(1, int(round(len(groups) * (1 - HOLDOUT))))
    cut = min(cut, len(groups) - 1)
    train_g, test_g = groups[:cut], groups[cut:]
    xtr, ytr, gtr, _ = _pack(train_g)
    xte, yte, gte, bte = _pack(test_g)
    if ytr.sum() == 0 or yte.sum() == 0:
        return RankerResult({"enabled": False, "reason": "训练集或留出集没有正样本", "samples": total, "groups": len(groups)}, None)

    params = {
        "objective": "lambdarank",
        "metric": "ndcg",
        "eval_at": [10],
        "num_leaves": 31,
        "learning_rate": 0.05,
        "num_threads": threads,
        "seed": seed,
        "deterministic": True,
        "verbosity": -1,
    }
    dtrain = lgb.Dataset(xtr, ytr, group=gtr, feature_name=list(RANK_FEATURES), free_raw_data=False)
    dvalid = lgb.Dataset(xte, yte, group=gte, reference=dtrain, free_raw_data=False)
    booster = lgb.train(params, dtrain, num_boost_round=300, valid_sets=[dvalid], valid_names=["holdout"],
                        callbacks=[lgb.early_stopping(30, verbose=False)])
    best_iter = booster.best_iteration or booster.current_iteration()
    pred = booster.predict(xte, num_iteration=best_iter, raw_score=True)
    w = np.zeros(NUM_RANK_FEATURES)
    w[:5] = np.asarray(blend[:5], dtype=np.float64)
    base = xte @ w
    ndcg = ndcg_at(pred, yte, bte)
    base_ndcg = ndcg_at(base, yte, bte)
    gains = booster.feature_importance(importance_type="gain", iteration=best_iter)
    importance = sorted(
        ({"name": RANK_FEATURES[i], "gain": round(float(g), 4)} for i, g in enumerate(gains)),
        key=lambda r: -r["gain"],
    )
    info: dict[str, Any] = {
        "ndcg10": round(ndcg, 6),
        "baselineNdcg10": round(base_ndcg, 6),
        "samples": total,
        "groups": len(groups),
        "importance": importance,
    }
    log(f"精排：样本 {total} 条、{len(groups)} 组，留出 NDCG@10 {ndcg:.4f}（融合分 {base_ndcg:.4f}），{best_iter} 棵树")
    if ndcg <= base_ndcg:
        info = {"enabled": False, "reason": f"留出 NDCG@10 {ndcg:.4f} 未超过融合分 {base_ndcg:.4f}", **info}
        return RankerResult(info, None)
    info = {"enabled": True, **info}
    return RankerResult(info, _lgb_v3(booster.model_to_string(num_iteration=best_iter)))
