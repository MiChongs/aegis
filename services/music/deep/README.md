# music-deep：音乐个性化第三期深度模型训练服务

契约见 [docs/music-personalization-3.md](../../../docs/music-personalization-3.md)「三、深度模型」（导出格式是 Go 推理的唯一依据）。

- 消费 NATS JetStream 流 `MUSIC_JOBS` 的 `music.job.deep.>`（持久拉取消费者 `music-deep`，工作队列，一条任务一个实例），
  消息 `{"run_id": 12, "app_id": 10000}`。流由 Go 端（`contract.EnsureStreams`）建，这里不建流。
- 训练记录 `music_deep_runs` 只接 `queued`，或 `running` 但心跳超过 10 分钟的；训练中每 20 秒 `in_progress()` 续期并刷新 `heartbeat_at`。
- 流程：读 PG 曲库（`music_tracks`）、标签（`music_track_tags`，没有表也能跑，内容特征为零）、应用配置（`deep` / `blend` / `windowDays` / `ranker`）
  与 ClickHouse 行为序列、`rank_samples` → 训练 SASRec（留出最后一个行为评估、早停，再用全部数据训几轮）→ 对比热度 →
  训练 LightGBM LambdaRank（只用 20 个特征的样本）→ 导出 `model`（safetensors）/ `meta` / `parity` / `ranker` 到 `music_deep_blobs` →
  `succeeded` → 达标自动发布（`published_at=now()`，NATS 发 `music.deep.published`）。
- 每阶段往 `log` 追加带时间的中文日志；失败写 `status=failed`、`error` 并 ack；进程关停时中断训练并 nak（由重投接着做）。
- 导出后自检：用导出的 numpy 权重按契约公式（`reference.py`）重算前向，与 PyTorch eval 输出比，最大误差写进 `parity` 列
  （`{"cases", "maxError", "ok"}`），超过 1e-3 视为失败。

代码：`src/music_deep/`

| 文件 | 内容 |
|---|---|
| `tags.py` | 标签表（与 Go `contract/tags.go` 一致）与多热键表 |
| `text.py` | 语种判定、歌手名规范化（照 Go `contract` 复写） |
| `data.py` | 曲库 → 物品特征，事件 → 用户序列 |
| `model.py` | SASRec（pre-LN、因果注意力、物品表示融合 ID / 歌手 / 语种 / 标签 / 语义向量） |
| `train.py` | sampled softmax + logQ 训练、留出评估、热度基线 |
| `export.py` / `reference.py` | 导出与契约公式的 numpy 前向 |
| `ranker.py` | LightGBM LambdaRank（文本头改写为 `version=v3`，Go 的 leaves 只认 v2 / v3） |
| `store.py` | PG / ClickHouse 读写（启动时执行与 Go 相同的 `CREATE TABLE IF NOT EXISTS`） |
| `pipeline.py` | 一次训练的完整流程 |
| `worker.py` / `metrics.py` / `__main__.py` | NATS 消费者、`/healthz` `/readyz` `/metrics`、入口 |

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `MUSIC_PG_DSN` | — | PostgreSQL（库 `aegis_music`） |
| `MUSIC_CLICKHOUSE_ADDR` | — | Go 用的原生协议地址 `host:9000`；这里取它的主机 |
| `MUSIC_CLICKHOUSE_HTTP_ADDR` | 同主机 `:8123` | clickhouse-connect 走 HTTP；本机 docker 的风控 ClickHouse 是 `127.0.0.1:18123` |
| `MUSIC_CLICKHOUSE_USER` / `_PASSWORD` / `_DB` | `default` / — / `aegis_music` | |
| `MUSIC_NATS_URL` | — | NATS（JetStream） |
| `MUSIC_DEEP_THREADS` | `4` | PyTorch 与 LightGBM 线程数 |
| `MUSIC_DEEP_METRICS_ADDR` | `:8099` | `/healthz`、`/readyz`（PG、NATS、消费者就绪）、`/metrics` |

## 运行

```bash
cd services/music/deep
uv sync                         # Python 3.12；torch 取 CPU 版（download.pytorch.org/whl/cpu），其余走清华镜像
uv run music-deep               # 常驻消费
uv run music-deep run 12        # 直接跑一条训练记录（排查用，不经过 NATS；达标照常发布但不发通知）
```

镜像：`docker build -f deploy/docker/music-deep.Dockerfile -t aegis-music-deep .`（compose 的 music profile 与
`deploy/dokploy/music-stack.yml` 里的 `music-deep` 服务，内存上限 6g）。

## 测试

```bash
uv run pytest -q                # 单元测试：标签表、序列规则、合成两类口味数据训练后 NDCG 胜过热度、
                                # 导出键与形状、独立 numpy 前向对齐 PyTorch、LightGBM 分组训练与导出
```

本机集成测试（`MUSIC_IT=1` 才跑；本机 docker 的 aegis-postgres 15432、aegis-risk-clickhouse 原生 19000 / HTTP 18123、aegis-nats）：

```bash
set -a; . <放连接串的 env 文件>; set +a
MUSIC_IT=1 MUSIC_CLICKHOUSE_HTTP_ADDR=127.0.0.1:18123 uv run pytest tests/test_it.py -v -s
```

它用一个新的测试应用（980000000 段）写曲库、事件与精排样本，建 queued 训练记录，直接调训练函数跑完并检查产物、状态与发布；
再用临时流起一次消费者走 NATS；最后删掉全部测试数据（PG 记录、ClickHouse 轻量删除、临时流）。
