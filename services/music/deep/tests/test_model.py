"""训练效果（胜过热度）、导出的键与形状、独立 numpy 实现与 PyTorch 的数值对齐。"""

from __future__ import annotations

import json
import math

import numpy as np
import pytest
import torch
from safetensors.numpy import load as st_load

import synth
from music_deep import export as exporter
from music_deep.config import RANK_FEATURES, DeepParams
from music_deep.data import build_item_features, build_sequences
from music_deep.train import make_windows, train


@pytest.fixture(scope="module")
def trained():
    torch.set_num_threads(4)
    tracks, tags = synth.catalog()
    feats = build_item_features(tracks, tags)
    seqs = build_sequences(synth.events(), feats.index)
    p = DeepParams(d=32, heads=2, layers=2, maxLen=20, epochs=15, minItemCount=3, ffn=64, patience=3)
    logs: list[str] = []
    res = train(feats, seqs, p, seed=5, log=logs.append)
    return feats, seqs, p, res, logs


def test_beats_popularity(trained):
    _feats, _seqs, _p, res, logs = trained
    assert res.metrics.users >= 100
    assert res.metrics.ndcg20 > res.baseline.ndcg20 * 2, logs
    assert res.metrics.recall20 > res.baseline.recall20, logs
    assert res.metrics.recall20 == res.metrics.hit_rate20
    assert any("第 1 轮" in line for line in logs)


def test_windows_right_aligned():
    s = [np.arange(10, dtype=np.int64)]
    inp, tgt = make_windows(s, 4)
    # 9 个目标切成 4 + 4 + 1，右对齐，左侧 −1
    assert inp.tolist() == [[5, 6, 7, 8], [1, 2, 3, 4], [-1, -1, -1, 0]]
    assert tgt.tolist() == [[6, 7, 8, 9], [2, 3, 4, 5], [-1, -1, -1, 1]]


# ── 独立的 numpy 实现：直接照 docs/music-personalization-3.md「导出」的前向写，不复用包里的 reference ──

def _ln(x, w, b, eps):
    mu = x.mean(-1, keepdims=True)
    var = np.mean((x - mu) ** 2, axis=-1, keepdims=True)
    return (x - mu) / np.sqrt(var + eps) * w + b


def _gelu(x):
    return np.array([0.5 * v * (1 + math.erf(v / math.sqrt(2))) for v in x.ravel()]).reshape(x.shape)


def _forward(t, meta, seq):
    d, heads, max_len, eps = meta["d"], meta["heads"], meta["maxLen"], meta["eps"]
    s = seq[-max_len:]
    n = len(s)
    w = {k: v.astype(np.float64) for k, v in t.items()}
    x = np.stack([w["item_repr"][s[i]] + w["pos_emb"][max_len - n + i] for i in range(n)])
    dh = d // heads
    for b in range(meta["layers"]):
        g = lambda k: w[f"blocks.{b}.{k}"]  # noqa: E731
        h = _ln(x, g("ln1.weight"), g("ln1.bias"), eps)
        q, k, v = (h @ g(f"attn.w{c}") + g(f"attn.b{c}") for c in "qkv")
        ctx = np.zeros_like(x)
        for hd in range(heads):
            c = slice(hd * dh, (hd + 1) * dh)
            for ti in range(n):
                logits = np.array([q[ti, c] @ k[j, c] / math.sqrt(dh) if j <= ti else -np.inf for j in range(n)])
                a = np.exp(logits - logits.max())
                a /= a.sum()
                ctx[ti, c] = a @ v[:, c]
        x = x + ctx @ g("attn.wo") + g("attn.bo")
        h2 = _ln(x, g("ln2.weight"), g("ln2.bias"), eps)
        x = x + _gelu(h2 @ g("ffn.w1") + g("ffn.b1")) @ g("ffn.w2") + g("ffn.b2")
    return _ln(x[n - 1], w["final_ln.weight"], w["final_ln.bias"], eps)


@pytest.fixture(scope="module")
def exported(trained):
    feats, seqs, p, res, _ = trained
    ranker = {"enabled": False, "reason": "测试"}
    ex = exporter.export(
        res.model, app_id=10000, run_id=12, items=feats.items, item_count=res.item_count, heads=p.heads,
        layers=p.layers, ffn=p.ffn, metrics={"ndcg20": res.metrics.ndcg20}, ranker=ranker, real_seqs=seqs.seqs[:2],
    )
    return feats, p, res, ex


def test_export_keys_and_shapes(exported):
    feats, p, _res, ex = exported
    t = st_load(ex.model)
    n, d, f, L = feats.n, p.d, p.ffn, p.maxLen
    want = {"item_repr": (n, d), "pos_emb": (L, d), "final_ln.weight": (d,), "final_ln.bias": (d,)}
    for b in range(p.layers):
        pre = f"blocks.{b}."
        want |= {pre + "ln1.weight": (d,), pre + "ln1.bias": (d,), pre + "ln2.weight": (d,), pre + "ln2.bias": (d,),
                 pre + "ffn.w1": (d, f), pre + "ffn.b1": (f,), pre + "ffn.w2": (f, d), pre + "ffn.b2": (d,)}
        for c in "qkvo":
            want[pre + f"attn.w{c}"] = (d, d)
            want[pre + f"attn.b{c}"] = (d,)
    assert set(t) == set(want)
    for k, shp in want.items():
        assert t[k].shape == shp, k
        assert t[k].dtype == np.float32, k
    # 小端 float32：safetensors 头里的 dtype 为 F32
    header_len = int.from_bytes(ex.model[:8], "little")
    header = json.loads(ex.model[8 : 8 + header_len])
    assert all(v["dtype"] == "F32" for k, v in header.items() if k != "__metadata__")


def test_linear_layout_in_out(exported):
    _feats, _p, res, ex = exported
    t = st_load(ex.model)
    blk = res.model.blocks[0]
    x = torch.randn(3, res.model.d)
    with torch.no_grad():
        want = blk.w1(x).numpy()
    got = x.numpy() @ t["blocks.0.ffn.w1"] + t["blocks.0.ffn.b1"]
    assert np.allclose(got, want, atol=1e-5)
    assert np.allclose(t["item_repr"], res.model.all_item_repr().numpy())


def test_meta_and_parity(exported):
    feats, p, res, ex = exported
    meta = json.loads(exporter.dumps(ex.meta))
    assert meta["format"] == 1 and meta["appId"] == 10000 and meta["runId"] == 12
    assert (meta["d"], meta["heads"], meta["layers"], meta["ffn"], meta["maxLen"]) == (p.d, p.heads, p.layers, p.ffn, p.maxLen)
    assert meta["eps"] == 1e-5
    assert meta["items"] == feats.items and len(meta["itemCount"]) == feats.n
    assert meta["rankerFeatures"] == list(RANK_FEATURES) and len(meta["rankerFeatures"]) == 20
    assert meta["ranker"] == {"enabled": False, "reason": "测试"}
    assert isinstance(meta["trainedAt"], int)

    parity = json.loads(exporter.dumps(ex.parity))
    assert len(parity["cases"]) >= 5
    assert any(len(c["seq"]) > p.maxLen for c in parity["cases"])  # 有一例测截断
    assert ex.parity_summary["ok"] and ex.parity_summary["maxError"] < 1e-4
    t = st_load(ex.model)
    worst = 0.0
    for c in parity["cases"]:
        u = _forward(t, meta, c["seq"])
        assert len(c["user"]) == p.d
        worst = max(worst, float(np.max(np.abs(u - np.asarray(c["user"])))))
        assert len(c["top"]) == 5
        scores = t["item_repr"].astype(np.float64) @ u
        top = np.argsort(-scores)[:5]
        for (idx, sc), j in zip(c["top"], top, strict=True):
            assert abs(scores[idx] - sc) < 1e-3
            assert abs(scores[j] - scores[idx]) < 1e-3  # 同分时顺序可能不同，但分数必须对得上
    assert worst < 1e-4, worst
