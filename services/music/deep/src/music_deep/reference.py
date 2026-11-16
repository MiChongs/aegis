"""照契约公式写的 numpy 前向（与 Go 推理一一对应），导出后自检数值对齐用。

只依赖导出的张量（[in, out] 线性层）与 meta 里的超参，不碰 PyTorch 模块。
"""

from __future__ import annotations

import math

import numpy as np

_erf = np.vectorize(math.erf, otypes=[np.float64])


def layer_norm(x: np.ndarray, w: np.ndarray, b: np.ndarray, eps: float) -> np.ndarray:
    mean = x.mean(axis=-1, keepdims=True)
    var = ((x - mean) ** 2).mean(axis=-1, keepdims=True)  # 总体方差
    return (x - mean) / np.sqrt(var + eps) * w + b


def gelu(x: np.ndarray) -> np.ndarray:
    return 0.5 * x * (1.0 + _erf(x / math.sqrt(2.0)))


def user_vector(t: dict[str, np.ndarray], seq: list[int], *, heads: int, layers: int, max_len: int, eps: float) -> np.ndarray:
    """输入序列（物品下标，旧 → 新）→ u（float64）。"""
    g = {k: np.asarray(v, dtype=np.float64) for k, v in t.items()}
    s = list(seq)[-max_len:]
    n = len(s)
    d = g["item_repr"].shape[1]
    dh = d // heads
    x = g["item_repr"][s] + g["pos_emb"][max_len - n : max_len]
    mask = np.triu(np.full((n, n), -np.inf), k=1)  # j > t 为 −∞
    for b in range(layers):
        p = f"blocks.{b}."
        h = layer_norm(x, g[p + "ln1.weight"], g[p + "ln1.bias"], eps)
        q = h @ g[p + "attn.wq"] + g[p + "attn.bq"]
        k = h @ g[p + "attn.wk"] + g[p + "attn.bk"]
        v = h @ g[p + "attn.wv"] + g[p + "attn.bv"]
        outs = []
        for hd in range(heads):
            sl = slice(hd * dh, (hd + 1) * dh)
            a = q[:, sl] @ k[:, sl].T / math.sqrt(dh) + mask
            a = np.exp(a - a.max(axis=1, keepdims=True))
            a = a / a.sum(axis=1, keepdims=True)
            outs.append(a @ v[:, sl])
        x = x + np.concatenate(outs, axis=1) @ g[p + "attn.wo"] + g[p + "attn.bo"]
        h2 = layer_norm(x, g[p + "ln2.weight"], g[p + "ln2.bias"], eps)
        x = x + gelu(h2 @ g[p + "ffn.w1"] + g[p + "ffn.b1"]) @ g[p + "ffn.w2"] + g[p + "ffn.b2"]
    return layer_norm(x[n - 1], g["final_ln.weight"], g["final_ln.bias"], eps)
