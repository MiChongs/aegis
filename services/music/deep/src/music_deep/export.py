"""导出：safetensors（键与形状见 docs「导出」）、meta JSON、数值对齐用例 parity JSON。"""

from __future__ import annotations

import json
import time
from dataclasses import dataclass
from typing import Any

import numpy as np
import torch
from safetensors.numpy import save as st_save

from . import reference
from .config import RANK_FEATURES
from .model import LN_EPS, SASRec

PARITY_TOLERANCE = 1e-3


def model_tensors(model: SASRec) -> dict[str, np.ndarray]:
    """PyTorch 模块 → 契约键名的 float32 数组（线性层一律转成 [in, out]）。"""
    model.eval()

    def f32(t: torch.Tensor) -> np.ndarray:
        return np.ascontiguousarray(t.detach().cpu().numpy().astype("<f4"))

    out: dict[str, np.ndarray] = {
        "item_repr": f32(model.all_item_repr()),
        "pos_emb": f32(model.pos_emb.weight),
    }
    for b, blk in enumerate(model.blocks):
        p = f"blocks.{b}."
        out[p + "ln1.weight"] = f32(blk.ln1.weight)
        out[p + "ln1.bias"] = f32(blk.ln1.bias)
        for name, lin in (("q", blk.wq), ("k", blk.wk), ("v", blk.wv), ("o", blk.wo)):
            out[p + f"attn.w{name}"] = f32(lin.weight.T)
            out[p + f"attn.b{name}"] = f32(lin.bias)
        out[p + "ln2.weight"] = f32(blk.ln2.weight)
        out[p + "ln2.bias"] = f32(blk.ln2.bias)
        out[p + "ffn.w1"] = f32(blk.w1.weight.T)
        out[p + "ffn.b1"] = f32(blk.w1.bias)
        out[p + "ffn.w2"] = f32(blk.w2.weight.T)
        out[p + "ffn.b2"] = f32(blk.w2.bias)
    out["final_ln.weight"] = f32(model.final_ln.weight)
    out["final_ln.bias"] = f32(model.final_ln.bias)
    return out


def expected_shapes(n: int, d: int, layers: int, ffn: int, max_len: int) -> dict[str, tuple[int, ...]]:
    """契约里的键与形状（测试与自检共用）。"""
    shapes: dict[str, tuple[int, ...]] = {"item_repr": (n, d), "pos_emb": (max_len, d)}
    for b in range(layers):
        p = f"blocks.{b}."
        shapes[p + "ln1.weight"] = (d,)
        shapes[p + "ln1.bias"] = (d,)
        for x in ("q", "k", "v", "o"):
            shapes[p + f"attn.w{x}"] = (d, d)
            shapes[p + f"attn.b{x}"] = (d,)
        shapes[p + "ln2.weight"] = (d,)
        shapes[p + "ln2.bias"] = (d,)
        shapes[p + "ffn.w1"] = (d, ffn)
        shapes[p + "ffn.b1"] = (ffn,)
        shapes[p + "ffn.w2"] = (ffn, d)
        shapes[p + "ffn.b2"] = (d,)
    shapes["final_ln.weight"] = (d,)
    shapes["final_ln.bias"] = (d,)
    return shapes


def parity_sequences(n_items: int, max_len: int, real: list[np.ndarray], seed: int) -> list[list[int]]:
    """对齐用例的输入：长度 1、2、3、7、maxLen、maxLen+5（测截断）的随机序列，再加至多 2 条真实序列。"""
    rng = np.random.default_rng(seed)
    out: list[list[int]] = []
    for length in (1, 2, 3, 7, max_len, max_len + 5):
        out.append([int(x) for x in rng.integers(0, n_items, size=length)])
    for s in real[:2]:
        out.append([int(x) for x in s[-max_len:]])
    return out


@dataclass
class Exported:
    model: bytes
    meta: dict[str, Any]
    parity: dict[str, Any]
    parity_summary: dict[str, Any]
    tensors: dict[str, np.ndarray]


def export(
    model: SASRec,
    *,
    app_id: int,
    run_id: int,
    items: list[str],
    item_count: np.ndarray,
    heads: int,
    layers: int,
    ffn: int,
    metrics: dict[str, Any],
    ranker: dict[str, Any],
    real_seqs: list[np.ndarray],
    seed: int = 7,
) -> Exported:
    tensors = model_tensors(model)
    shapes = expected_shapes(len(items), model.d, layers, ffn, model.max_len)
    if set(tensors) != set(shapes):
        raise RuntimeError(f"导出的键与契约不一致：多了 {set(tensors) - set(shapes)}，少了 {set(shapes) - set(tensors)}")
    for k, shp in shapes.items():
        if tensors[k].shape != shp:
            raise RuntimeError(f"导出张量 {k} 的形状 {tensors[k].shape} 与契约 {shp} 不一致")
    blob = st_save(tensors)

    # 数值对齐：PyTorch eval 输出 vs 按契约公式的 numpy 前向（float64）
    table = torch.as_tensor(tensors["item_repr"])
    ref_t = {k: v.astype(np.float64) for k, v in tensors.items()}
    cases, max_err = [], 0.0
    for seq in parity_sequences(len(items), model.max_len, real_seqs, seed):
        u = model.user_vector(seq, table)
        scores = u @ table.T
        top = torch.topk(scores, k=min(5, len(items)))
        u_ref = reference.user_vector(ref_t, seq, heads=heads, layers=layers, max_len=model.max_len, eps=LN_EPS)
        max_err = max(max_err, float(np.max(np.abs(u_ref - u.double().numpy()))))
        cases.append({
            "seq": seq,
            "user": [float(x) for x in u.tolist()],
            "top": [[int(i), float(s)] for i, s in zip(top.indices.tolist(), top.values.tolist(), strict=True)],
        })
    parity = {"cases": cases}
    summary = {"cases": len(cases), "maxError": max_err, "ok": bool(max_err <= PARITY_TOLERANCE)}

    meta = {
        "format": 1,
        "appId": app_id,
        "runId": run_id,
        "d": model.d,
        "heads": heads,
        "layers": layers,
        "ffn": ffn,
        "maxLen": model.max_len,
        "eps": LN_EPS,
        "items": list(items),
        "itemCount": [int(x) for x in item_count],
        "trainedAt": int(time.time() * 1000),
        "metrics": metrics,
        "rankerFeatures": list(RANK_FEATURES),
        "ranker": ranker,
    }
    return Exported(model=blob, meta=meta, parity=parity, parity_summary=summary, tensors=tensors)


def dumps(obj: Any) -> bytes:
    return json.dumps(obj, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
