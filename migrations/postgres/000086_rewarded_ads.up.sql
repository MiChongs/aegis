-- +migrate Up
-- 应用级激励广告（看完一段激励视频，按场景发会员 / 积分 / 经验 / 抽奖次数）。
--
-- 一次「看完」要两方各说一句才算数：
--
--   广告平台的服务端回调   证明这次观看真的发生了（平台按 sha256(securityKey:transId) 签名）
--   客户端的上报           说明这次观看是为哪个场景、哪个账号看的（走网关、带用户令牌）
--
-- 两句话靠同一个 trans_id 对上。哪一句先到都可以：先到的落一行 pending，
-- 后到的把它补齐并在同一事务里发奖。只信客户端等于谁都能伪造一次观看；
-- 只信服务端回调则不知道这次是为哪个场景看的，也无法拒绝拿别人的观看来领奖。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 配置：一个应用一行 ──
--
-- 场景放 JSONB 而不是单独建表：场景只在保存配置时整体替换，从不被单独引用或计数
-- （观看记录里存的是场景 key 的快照，不是外键），拆表只会多出一套同步逻辑。
CREATE TABLE IF NOT EXISTS app_rewarded_ad_configs (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 广告平台。目前只有 huijing（灰鲸），留出这一列是因为回调签名与参数名随平台而变。
    provider VARCHAR(32) NOT NULL DEFAULT 'huijing',
    -- 平台侧的应用 ID，只作核对与展示：客户端初始化 SDK 时用的是包里打进去的那一份。
    provider_app_id VARCHAR(64) NOT NULL DEFAULT '',
    -- 回调验签用的 Security Key，AES-GCM 密文（派生自 SECURITY_MASTER_KEY），永不回传明文。
    security_key_cipher TEXT NOT NULL DEFAULT '',
    security_key_hint VARCHAR(32) NOT NULL DEFAULT '',
    -- dual 双方确认（默认）/ server 仅服务端回调 / client 仅客户端上报（不安全，联调用）
    verify_mode VARCHAR(16) NOT NULL DEFAULT 'dual',
    -- 每人每天所有场景合计可领几次，0 表示不限。
    daily_limit INTEGER NOT NULL DEFAULT 0,
    scenes JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── 观看记录：一次观看一行，以平台的 trans_id 为准 ──
CREATE TABLE IF NOT EXISTS app_rewarded_ad_views (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 由先到的一方写入；后到的一方必须与之一致，否则记为 user_mismatch。
    user_id BIGINT NULL,
    trans_id VARCHAR(128) NOT NULL,
    scene VARCHAR(64) NOT NULL DEFAULT '',
    placement_id VARCHAR(64) NOT NULL DEFAULT '',
    -- pending 等另一方 / granted 已发放 / rejected 未发放（原因见 reason）
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reason VARCHAR(64) NOT NULL DEFAULT '',

    -- 服务端回调带来的事实
    server_verified_at TIMESTAMPTZ NULL,
    reward_name VARCHAR(128) NOT NULL DEFAULT '',
    reward_amount INTEGER NOT NULL DEFAULT 0,
    network_id VARCHAR(32) NOT NULL DEFAULT '',
    extra TEXT NOT NULL DEFAULT '',

    -- 客户端上报带来的事实
    client_reported_at TIMESTAMPTZ NULL,
    -- SDK 自己的奖励校验结论（HJRewardVerify.isReward），只在 client 模式下作数。
    client_verified BOOLEAN NULL,
    client_error VARCHAR(128) NOT NULL DEFAULT '',
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',

    -- 发放结果：每项权益实际发成了什么（账本流水号），排障时回答「到底到没到账」。
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,
    granted_at TIMESTAMPTZ NULL,
    operator VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- 一次观看只发一次奖的最后一道保证。平台重试回调、客户端重复上报都落在这一行上。
    CONSTRAINT uq_app_rewarded_ad_views_trans UNIQUE (appid, trans_id)
);

CREATE INDEX IF NOT EXISTS idx_app_rewarded_ad_views_app_time
    ON app_rewarded_ad_views(appid, created_at DESC);
-- 日限额与冷却只数已发放的行：部分索引让这两条判定不必扫 pending / rejected。
CREATE INDEX IF NOT EXISTS idx_app_rewarded_ad_views_user_granted
    ON app_rewarded_ad_views(appid, user_id, granted_at DESC)
    WHERE status = 'granted';
