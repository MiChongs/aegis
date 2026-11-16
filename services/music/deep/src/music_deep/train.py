"""SASRec 训练与离线评估。

- 损失：批内目标 + 按热度采样的负例，sampled softmax，logQ 修正（logits − log q）。
- 评估：每人最后一个正向行为为测试目标，前面为输入；在全部物品上打分（排除输入里出现过的），
  算 Recall@20、NDCG@20、HitRate@20，与热度基线对比；早停看这个留出集的 NDCG@20。
- 导出模型：从最优轮接着用全部数据（含每人最后一个）再训几轮。
"""

from __future__ import annotations

import copy
import math
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass

import numpy as np
import torch
from torch.nn import functional as F

from .config import DeepParams
from .data import ItemFeatures, Sequences, user_counts
from .model import SASRec

K = 20
# 一个人最多取最近的这么多个窗口（每窗 maxLen 个目标），重度用户不至于淹没其他人
MAX_WINDOWS_PER_USER = 10
MAX_EVAL_USERS = 10000
MIN_TEST_USERS = 10


class Cancelled(Exception):
    """进程在关停：训练中途放弃（不标失败，由重投接着做）。"""


@dataclass
class EvalResult:
    recall20: float
    ndcg20: float
    hit_rate20: float
    users: int

    def to_json(self) -> dict[str, float]:
        return {"recall20": round(self.recall20, 6), "ndcg20": round(self.ndcg20, 6), "hitRate20": round(self.hit_rate20, 6)}


@dataclass
class TrainResult:
    model: SASRec
    metrics: EvalResult
    baseline: EvalResult
    best_epoch: int
    refit_epochs: int
    train_ms: int
    item_count: np.ndarray
    history: list[dict]
    windows: int = 0  # 全量再训的窗口数（训练序列数）


def make_windows(seqs: list[np.ndarray], max_len: int) -> tuple[np.ndarray, np.ndarray]:
    """序列 → 训练窗口：输入 [W, maxLen]、目标 [W, maxLen]（右对齐，左侧填 −1）。

    一条长度 T 的序列给出 T−1 个 (前缀 → 下一个) 目标；超过 maxLen 的从尾部往前切成多个窗口。
    """
    inputs, targets = [], []
    for s in seqs:
        t = len(s)
        if t < 2:
            continue
        end, made = t, 0
        while end >= 2 and made < MAX_WINDOWS_PER_USER:
            start = max(0, end - 1 - max_len)
            inp = s[start : end - 1]
            tgt = s[start + 1 : end]
            pad = max_len - len(inp)
            inputs.append(np.concatenate([np.full(pad, -1, dtype=np.int64), inp]))
            targets.append(np.concatenate([np.full(pad, -1, dtype=np.int64), tgt]))
            made += 1
            end = start + 1
            if start == 0:
                break
    if not inputs:
        return np.zeros((0, max_len), dtype=np.int64), np.zeros((0, max_len), dtype=np.int64)
    return np.stack(inputs), np.stack(targets)


def popularity(seqs: list[np.ndarray], n_items: int) -> np.ndarray:
    cnt = np.zeros(n_items, dtype=np.float64)
    for s in seqs:
        np.add.at(cnt, s, 1.0)
    return cnt


def _left_pad(seqs: list[np.ndarray], max_len: int) -> tuple[torch.Tensor, torch.Tensor]:
    arr = np.full((len(seqs), max_len), -1, dtype=np.int64)
    for i, s in enumerate(seqs):
        s = s[-max_len:]
        arr[i, max_len - len(s) :] = s
    t = torch.as_tensor(arr)
    return t, t >= 0


def _rank_metrics(scores: torch.Tensor, inputs: list[np.ndarray], targets: np.ndarray) -> tuple[float, float]:
    """scores [B, N]（会被就地改写）；返回本批 NDCG@20 之和、命中数之和。"""
    for r, s in enumerate(inputs):
        scores[r, torch.as_tensor(s)] = float("-inf")
    top = torch.topk(scores, k=min(K, scores.shape[1]), dim=1).indices.numpy()
    ndcg = hits = 0.0
    for r in range(len(inputs)):
        pos = np.nonzero(top[r] == targets[r])[0]
        if pos.size:
            hits += 1.0
            ndcg += 1.0 / math.log2(pos[0] + 2)
    return ndcg, hits


def _eval_split(seqs: list[np.ndarray], seed: int) -> tuple[list[np.ndarray], np.ndarray]:
    idx = np.arange(len(seqs))
    if len(idx) > MAX_EVAL_USERS:
        idx = np.sort(np.random.default_rng(seed).choice(idx, MAX_EVAL_USERS, replace=False))
    return [seqs[i][:-1] for i in idx], np.array([seqs[i][-1] for i in idx], dtype=np.int64)


@torch.no_grad()
def evaluate(model: SASRec, inputs: list[np.ndarray], targets: np.ndarray, batch: int = 256) -> EvalResult:
    model.eval()
    table = model.all_item_repr()
    ndcg = hits = 0.0
    for s in range(0, len(inputs), batch):
        chunk = inputs[s : s + batch]
        seq, valid = _left_pad(chunk, model.max_len)
        u = model.encode(seq, valid, table)[:, -1]
        scores = u @ table.T
        n, h = _rank_metrics(scores, chunk, targets[s : s + batch])
        ndcg += n
        hits += h
    users = max(len(inputs), 1)
    return EvalResult(hits / users, ndcg / users, hits / users, len(inputs))


def evaluate_popularity(pop: np.ndarray, inputs: list[np.ndarray], targets: np.ndarray, batch: int = 256) -> EvalResult:
    # 同分按下标先后（加一个极小的递减量，topk 结果确定）
    base = torch.as_tensor(pop - np.arange(len(pop)) * 1e-9, dtype=torch.float64)
    ndcg = hits = 0.0
    for s in range(0, len(inputs), batch):
        chunk = inputs[s : s + batch]
        scores = base.unsqueeze(0).repeat(len(chunk), 1)
        n, h = _rank_metrics(scores, chunk, targets[s : s + batch])
        ndcg += n
        hits += h
    users = max(len(inputs), 1)
    return EvalResult(hits / users, ndcg / users, hits / users, len(inputs))


class _Trainer:
    def __init__(self, model: SASRec, p: DeepParams, sample_q: np.ndarray, seed: int, cancel: threading.Event | None) -> None:
        self.model, self.p, self.cancel = model, p, cancel
        q = sample_q / sample_q.sum()
        self.q = torch.as_tensor(q, dtype=torch.float32)
        self.log_q = torch.log(self.q)
        self.gen = torch.Generator().manual_seed(seed)
        self.rng = np.random.default_rng(seed)

    def optimizer(self, lr: float) -> torch.optim.Optimizer:
        return torch.optim.AdamW(self.model.parameters(), lr=lr, weight_decay=self.p.weightDecay)

    def epoch(self, opt: torch.optim.Optimizer, inputs: np.ndarray, targets: np.ndarray, beat: Callable[[], None] | None) -> float:
        self.model.train()
        order = self.rng.permutation(len(inputs))
        # 数据少时减小批大小，保证每轮至少约 32 步（否则 20 轮也只有几十次更新，学不出东西）
        bs = min(self.p.batchSize, max(16, len(inputs) // 32))
        total, batches = 0.0, 0
        last_beat = time.monotonic()
        n_items = self.model.n_items
        for s in range(0, len(order), bs):
            if self.cancel is not None and self.cancel.is_set():
                raise Cancelled()
            sel = order[s : s + bs]
            seq = torch.as_tensor(inputs[sel])
            tgt = torch.as_tensor(targets[sel])
            valid = seq >= 0
            h = self.model.encode(seq, valid)
            mask = tgt >= 0
            u = h[mask]
            y = tgt[mask]
            if y.numel() == 0:
                continue
            neg = torch.multinomial(self.q, min(self.p.negatives, n_items), replacement=True, generator=self.gen)
            cand = torch.unique(torch.cat([y, neg]))
            e = self.model.item_repr(cand)
            logits = u @ e.T - self.log_q[cand].unsqueeze(0)
            label = torch.searchsorted(cand, y)
            loss = F.cross_entropy(logits, label)
            opt.zero_grad(set_to_none=True)
            loss.backward()
            torch.nn.utils.clip_grad_norm_(self.model.parameters(), 5.0)
            opt.step()
            total += float(loss.detach())
            batches += 1
            if beat is not None and time.monotonic() - last_beat > 15:
                beat()
                last_beat = time.monotonic()
        return total / max(batches, 1)


def train(
    feats: ItemFeatures,
    seqs: Sequences,
    p: DeepParams,
    *,
    seed: int = 1,
    log: Callable[[str], None] | None = None,
    beat: Callable[[], None] | None = None,
    cancel: threading.Event | None = None,
) -> TrainResult:
    """完整训练：留出评估 + 早停 → 全量再训 → 导出用的模型。"""
    log = log or (lambda _m: None)
    torch.manual_seed(seed)
    started = time.monotonic()
    n = feats.n
    all_seqs = seqs.seqs
    eval_inputs, eval_targets = _eval_split(all_seqs, seed)
    if len(eval_inputs) < MIN_TEST_USERS:
        raise ValueError(f"可评估的听众不足 {MIN_TEST_USERS} 人（当前 {len(eval_inputs)} 人）")
    train_seqs = [s[:-1] for s in all_seqs]

    model = SASRec(
        artist_idx=feats.artist_idx, n_artists=feats.n_artists, lang_idx=feats.lang_idx, n_langs=5,
        tags=feats.tags, text=feats.text, d=p.d, heads=p.heads, layers=p.layers, ffn=p.ffn,
        max_len=p.maxLen, dropout=p.dropout,
    )
    model.set_has_id(user_counts(train_seqs, n) >= p.minItemCount)

    pop_train = popularity(train_seqs, n)
    baseline = evaluate_popularity(pop_train, eval_inputs, eval_targets)
    log(f"热度基线（{baseline.users} 人）：NDCG@20 {baseline.ndcg20:.4f}，Recall@20 {baseline.recall20:.4f}")

    inputs, targets = make_windows(train_seqs, p.maxLen)
    log(f"训练窗口 {len(inputs)} 个，目标 {int((targets >= 0).sum())} 个")
    trainer = _Trainer(model, p, (pop_train + 1.0) ** 0.75, seed, cancel)
    opt = trainer.optimizer(p.lr)
    best: EvalResult | None = None
    best_state = None
    best_epoch = 0
    history: list[dict] = []
    stale = 0
    for ep in range(1, p.epochs + 1):
        t0 = time.monotonic()
        loss = trainer.epoch(opt, inputs, targets, beat)
        res = evaluate(model, eval_inputs, eval_targets)
        history.append({"epoch": ep, "loss": round(loss, 5), "ndcg20": round(res.ndcg20, 5), "recall20": round(res.recall20, 5)})
        log(f"第 {ep} 轮：损失 {loss:.4f}，NDCG@20 {res.ndcg20:.4f}，Recall@20 {res.recall20:.4f}，耗时 {time.monotonic() - t0:.1f} 秒")
        if best is None or res.ndcg20 > best.ndcg20:
            best, best_epoch, stale = res, ep, 0
            best_state = copy.deepcopy(model.state_dict())
        else:
            stale += 1
            if stale >= p.patience:
                log(f"连续 {p.patience} 轮没有提升，早停（最优第 {best_epoch} 轮）")
                break
        if beat is not None:
            beat()
    assert best is not None and best_state is not None
    model.load_state_dict(best_state)

    # 全量再训：加上每人最后一个行为，从最优轮接着训几轮（学习率减半）
    refit_epochs = min(3, max(1, best_epoch // 4))
    model.set_has_id(user_counts(all_seqs, n) >= p.minItemCount)
    full_in, full_tgt = make_windows(all_seqs, p.maxLen)
    trainer_full = _Trainer(model, p, (popularity(all_seqs, n) + 1.0) ** 0.75, seed + 1, cancel)
    opt_full = trainer_full.optimizer(p.lr * 0.5)
    for ep in range(1, refit_epochs + 1):
        loss = trainer_full.epoch(opt_full, full_in, full_tgt, beat)
        log(f"全量再训第 {ep}/{refit_epochs} 轮：损失 {loss:.4f}")
    model.eval()
    return TrainResult(
        model=model, metrics=best, baseline=baseline, best_epoch=best_epoch, refit_epochs=refit_epochs,
        train_ms=int((time.monotonic() - started) * 1000), item_count=user_counts(all_seqs, n), history=history,
        windows=len(full_in),
    )
