"""SASRec（pre-LN Transformer，因果注意力），结构与 docs/music-personalization-3.md「模型」「导出」逐项对应。

物品表示 e_i = W_id[i]·m_i + W_artist[a_i] + W_lang[l_i] + mean_{t∈tags(i)} W_tag[t] + P·text_i，
其中 text_i = [语义向量 256 维, has_embedding]（没有向量时前 256 维为 0、标志位为 0）。
导出时把 e_i 全部算好存成 item_repr，Go 只做序列前向与点积。
"""

from __future__ import annotations

import math

import numpy as np
import torch
from torch import nn
from torch.nn import functional as F

LN_EPS = 1e-5


class Block(nn.Module):
    """一个 pre-LN Transformer 块：x = x + Attn(LN1(x))；x = x + FFN(LN2(x))。"""

    def __init__(self, d: int, heads: int, ffn: int, dropout: float) -> None:
        super().__init__()
        self.d, self.heads, self.dh = d, heads, d // heads
        self.ln1 = nn.LayerNorm(d, eps=LN_EPS)
        self.wq = nn.Linear(d, d)
        self.wk = nn.Linear(d, d)
        self.wv = nn.Linear(d, d)
        self.wo = nn.Linear(d, d)
        self.ln2 = nn.LayerNorm(d, eps=LN_EPS)
        self.w1 = nn.Linear(d, ffn)
        self.w2 = nn.Linear(ffn, d)
        self.drop = nn.Dropout(dropout)
        self.attn_drop = nn.Dropout(dropout)

    def forward(self, x: torch.Tensor, allowed: torch.Tensor) -> torch.Tensor:
        """x [B, L, d]；allowed [B, 1, L, L] 布尔，True 表示 t 可以看 j。"""
        b, length, _ = x.shape
        h = self.ln1(x)

        def split(t: torch.Tensor) -> torch.Tensor:
            return t.view(b, length, self.heads, self.dh).transpose(1, 2)

        q, k, v = split(self.wq(h)), split(self.wk(h)), split(self.wv(h))
        scores = q @ k.transpose(-1, -2) / math.sqrt(self.dh)
        scores = scores.masked_fill(~allowed, float("-inf"))
        att = self.attn_drop(torch.softmax(scores, dim=-1))
        ctx = (att @ v).transpose(1, 2).reshape(b, length, self.d)
        x = x + self.drop(self.wo(ctx))
        h2 = self.ln2(x)
        x = x + self.drop(self.w2(F.gelu(self.w1(h2))))  # F.gelu 默认即精确版 0.5·x·(1+erf(x/√2))
        return x


class SASRec(nn.Module):
    def __init__(
        self,
        *,
        artist_idx: np.ndarray,
        n_artists: int,
        lang_idx: np.ndarray,
        n_langs: int,
        tags: np.ndarray,
        text: np.ndarray,
        d: int,
        heads: int,
        layers: int,
        ffn: int,
        max_len: int,
        dropout: float,
    ) -> None:
        super().__init__()
        n = len(artist_idx)
        self.n_items, self.d, self.max_len = n, d, max_len
        self.id_emb = nn.Embedding(n, d)
        self.artist_emb = nn.Embedding(max(n_artists, 1), d)
        self.lang_emb = nn.Embedding(max(n_langs, 1), d)
        self.tag_emb = nn.Parameter(torch.empty(tags.shape[1], d))
        self.text_proj = nn.Linear(text.shape[1], d, bias=False)
        self.pos_emb = nn.Embedding(max_len, d)
        self.emb_drop = nn.Dropout(dropout)
        self.blocks = nn.ModuleList(Block(d, heads, ffn, dropout) for _ in range(layers))
        self.final_ln = nn.LayerNorm(d, eps=LN_EPS)

        # 物品特征（不训练）：标签多热按行除以标签数 → 与 W_tag 相乘即为均值
        cnt = tags.sum(axis=1, keepdims=True)
        tag_mean = np.divide(tags, cnt, out=np.zeros_like(tags, dtype=np.float32), where=cnt > 0)
        self.register_buffer("artist", torch.as_tensor(artist_idx, dtype=torch.long), persistent=False)
        self.register_buffer("lang", torch.as_tensor(lang_idx, dtype=torch.long), persistent=False)
        self.register_buffer("tag_mean", torch.as_tensor(tag_mean, dtype=torch.float32), persistent=False)
        self.register_buffer("text", torch.as_tensor(text, dtype=torch.float32), persistent=False)
        self.register_buffer("has_id", torch.zeros(n, dtype=torch.float32), persistent=False)
        self._init()

    def _init(self) -> None:
        for emb in (self.id_emb, self.artist_emb, self.lang_emb, self.pos_emb):
            nn.init.normal_(emb.weight, std=0.05)
        nn.init.normal_(self.tag_emb, std=0.05)
        nn.init.normal_(self.text_proj.weight, std=0.02)
        for blk in self.blocks:
            for lin in (blk.wq, blk.wk, blk.wv, blk.wo, blk.w1, blk.w2):
                nn.init.xavier_uniform_(lin.weight)
                nn.init.zeros_(lin.bias)

    def set_has_id(self, mask: np.ndarray) -> None:
        """m_i：被 ≥ minItemCount 人交互过的物品才用独立 ID 向量。"""
        self.has_id.copy_(torch.as_tensor(mask, dtype=torch.float32))

    def item_repr(self, idx: torch.Tensor) -> torch.Tensor:
        """任意形状的物品下标 → [..., d] 的 e_i。"""
        e = self.id_emb(idx) * self.has_id[idx].unsqueeze(-1)
        e = e + self.artist_emb(self.artist[idx]) + self.lang_emb(self.lang[idx])
        e = e + self.tag_mean[idx] @ self.tag_emb
        e = e + self.text_proj(self.text[idx])
        return e

    @torch.no_grad()
    def all_item_repr(self, chunk: int = 8192) -> torch.Tensor:
        out = []
        for s in range(0, self.n_items, chunk):
            out.append(self.item_repr(torch.arange(s, min(s + chunk, self.n_items))))
        return torch.cat(out, dim=0)

    def encode(self, seq: torch.Tensor, valid: torch.Tensor, item_table: torch.Tensor | None = None) -> torch.Tensor:
        """序列 [B, L]（右对齐，左侧填充，valid 标出真实位置）→ final_ln 后的隐状态 [B, L, d]。

        位置编码用 pos_emb[maxLen − L + t]：L = maxLen 时与 Go 的「右对齐、最后一个用 pos_emb[maxLen−1]」一致；
        数值对齐用例里直接传不填充的长度 n 序列，正是 Go 的写法。
        """
        b, length = seq.shape
        safe = seq.clamp(min=0)
        x = item_table[safe] if item_table is not None else self.item_repr(safe)
        pos = torch.arange(self.max_len - length, self.max_len, device=seq.device)
        x = x + self.pos_emb(pos).unsqueeze(0)
        x = self.emb_drop(x) * valid.unsqueeze(-1)
        # 因果 + 只看真实位置；填充位置只看自己（避免整行 −∞ 出 NaN，它们的输出不参与任何计算）
        causal = torch.tril(torch.ones(length, length, dtype=torch.bool, device=seq.device))
        allowed = causal.unsqueeze(0) & valid.unsqueeze(1)
        allowed = allowed | torch.eye(length, dtype=torch.bool, device=seq.device).unsqueeze(0)
        allowed = allowed.unsqueeze(1)
        for blk in self.blocks:
            x = blk(x, allowed)
        return self.final_ln(x)

    @torch.no_grad()
    def user_vector(self, seq: list[int], item_table: torch.Tensor | None = None) -> torch.Tensor:
        """Go 的推理写法：只取最后 maxLen 个、不填充，u = final_ln(x_{n−1})。"""
        s = list(seq)[-self.max_len:]
        t = torch.as_tensor([s], dtype=torch.long)
        h = self.encode(t, torch.ones_like(t, dtype=torch.bool), item_table)
        return h[0, -1]
