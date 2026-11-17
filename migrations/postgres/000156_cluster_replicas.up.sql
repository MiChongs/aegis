-- 多副本部署：API 可以在多台机器上跑 N 个副本、Worker 跑 1..N 个副本。
-- 这一版只补「状态不能再只留在某一个进程 / 某一块本地盘上」的几处。

-- 1. 支付凭证导出：原先落在 data/payment-bills 本地目录，多节点时在别的节点下载回 40475。
--    改存数据库：凭证是几十 KB 的 PDF、有效期最长七天（邮件链接），量不大；
--    存的是导出那一刻的那份文件，事后订单状态变了也不会让同一个链接下到不同的内容。
CREATE TABLE IF NOT EXISTS payment_receipt_exports (
    bill_id VARCHAR(64) PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 管理端代寄的凭证可能没有用户（订单未绑定用户），因此不加外键
    user_id BIGINT NOT NULL DEFAULT 0,
    order_no VARCHAR(128) NOT NULL DEFAULT '',
    transaction_no VARCHAR(64) NOT NULL DEFAULT '',
    file_name VARCHAR(255) NOT NULL,
    locale VARCHAR(32) NOT NULL DEFAULT '',
    content BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_payment_receipt_exports_expires ON payment_receipt_exports (expires_at);

-- 2. 封禁申诉的决策模型复审：认领时原子记下谁、什么时候认领的。
--    原先只靠 FOR UPDATE SKIP LOCKED，锁随语句结束就放了，别的副本下一分钟还能把同一条再认领走，
--    同一条申诉被并发送去大模型审两遍。认领超过租期（复审单次最长 3 分钟，租期取 5 分钟）视为认领者已经没了，可被重新认领。
ALTER TABLE ban_appeals ADD COLUMN IF NOT EXISTS review_claimed_at TIMESTAMPTZ NULL;
ALTER TABLE ban_appeals ADD COLUMN IF NOT EXISTS review_claimed_by VARCHAR(128) NOT NULL DEFAULT '';
-- 兜底认领也要覆盖「复审中但进程挂了」的申诉：原索引只覆盖 pending
CREATE INDEX IF NOT EXISTS idx_ban_appeals_unreviewed_open ON ban_appeals (created_at)
    WHERE status IN ('pending', 'reviewing') AND model_reviewed_at IS NULL;

-- 3. 存储迁移：执行者认领 + 心跳。同一个迁移任务只能有一个实例在跑；
--    持有者宕机后心跳过期，别的实例接手续跑。
ALTER TABLE storage_migrations ADD COLUMN IF NOT EXISTS owner VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE storage_migrations ADD COLUMN IF NOT EXISTS heartbeat_at TIMESTAMPTZ NULL;

-- 4. 流水号的节点槽位：每个进程启动时取一个号，模 100 作为流水号尾部 6 位里的前两位，
--    不同副本在同一秒内生成的流水号就落在互不相交的号段里。
CREATE SEQUENCE IF NOT EXISTS transaction_no_node_seq;
