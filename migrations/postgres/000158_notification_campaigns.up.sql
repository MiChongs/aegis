-- +migrate Up
-- 站内信群发任务（docs/notification-center.md「群发任务」）。
--
-- 群发不再在请求里逐人投递：管理员发起后落一个任务，任意实例 SKIP LOCKED 认领，按用户 ID 升序
-- 每批一条语句把消息与回执一起写进去，进度（游标、计数）与这批写入同一事务提交——实例中途没了，
-- 心跳过期后别的实例从游标接着发，(campaign_id, user_id) 唯一索引保证一个人最多收到一条。
--
-- 本文件随每次启动整份重放，全部语句必须幂等。notifications 上的两个新索引在 000159 / 000160
-- 用 CONCURRENTLY 建（表可能很大，普通建索引会在构建期间挡住所有投递）。

CREATE TABLE IF NOT EXISTS notification_campaigns (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 幂等键：同一应用同一 request_id 只建一个任务（控制台连点、网络重试）
    request_id VARCHAR(64) NULL,
    -- 消息内容（与 notifications 同口径，发起时已按 Deliver 的规则校验与归一）
    type VARCHAR(64) NOT NULL,
    category VARCHAR(32) NOT NULL,
    title VARCHAR(200) NOT NULL,
    content TEXT NOT NULL,
    level VARCHAR(32) NOT NULL DEFAULT 'info',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    action JSONB NULL,
    image TEXT NULL,
    expires_at TIMESTAMPTZ NULL,
    -- notification.Audience：目标人群
    audience JSONB NOT NULL,
    -- 人群的中文摘要（列表上展示，不必解 audience）
    audience_label VARCHAR(255) NOT NULL DEFAULT '',
    -- scheduled 定时 / queued 排队 / running 发送中 / succeeded 完成 / failed 失败 / cancelled 已取消 /
    -- recalling 撤回中 / recalled 已撤回
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    scheduled_at TIMESTAMPTZ NULL,
    -- 开始发送时人群的人数（估计值：发送期间人群可能变化），开始前为 NULL
    total BIGINT NULL,
    -- 开始发送时人群里最大的用户 ID：只发给这一刻已经存在的人，发送期间新注册的不追加
    max_user_id BIGINT NULL,
    -- 已经扫过的最大用户 ID（下一批从它之后取）
    cursor BIGINT NOT NULL DEFAULT 0,
    -- 扫过的人数 / 真正写入的条数 / 已有（重复）跳过的人数
    processed BIGINT NOT NULL DEFAULT 0,
    delivered BIGINT NOT NULL DEFAULT 0,
    skipped BIGINT NOT NULL DEFAULT 0,
    -- 撤回删掉的条数
    recalled BIGINT NOT NULL DEFAULT 0,
    -- 执行它的实例与心跳；attempts 认领次数（反复被中断就判失败）
    owner VARCHAR(128) NOT NULL DEFAULT '',
    heartbeat_at TIMESTAMPTZ NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    operator VARCHAR(128) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    recalled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_notification_campaigns_request
    ON notification_campaigns (appid, request_id) WHERE request_id IS NOT NULL;
-- 管理端列表：按应用倒序
CREATE INDEX IF NOT EXISTS idx_notification_campaigns_app_time
    ON notification_campaigns (appid, id DESC);
-- 认领：只看还没结束的
CREATE INDEX IF NOT EXISTS idx_notification_campaigns_pending
    ON notification_campaigns (status, scheduled_at)
    WHERE status IN ('scheduled', 'queued', 'running', 'recalling');

-- 消息属于哪个群发任务；单条投递为空。不加外键：任务随应用级联删除，消息另有自己的清理路径，
-- 外键只会让每一批写入多一次检查
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS campaign_id BIGINT NULL;
