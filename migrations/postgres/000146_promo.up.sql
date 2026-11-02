-- +migrate Up
-- 推广活动（docs/promo.md）：一段时间内的邀请档位奖励与分享文章审核。
--
--   promo_campaigns    活动：标题、时间、状态，其余配置（档位、平台、奖励、次数）在 config
--   promo_tier_grants  邀请档位的领取记录，每人每档一次
--   promo_submissions  分享提交：平台、链接、审核结论与发放结果
--
-- 邀请人数不另存：按活动时间段数 invite_relations。
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS promo_campaigns (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    title VARCHAR(64) NOT NULL,
    subtitle VARCHAR(128) NOT NULL DEFAULT '',
    rules_text TEXT NOT NULL DEFAULT '',
    -- draft 草稿 / online 上线 / offline 下线
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    -- 形状由 internal/domain/promo 的 Config 决定
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by VARCHAR(128) NULL,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_promo_campaigns_status CHECK (status IN ('draft','online','offline')),
    CONSTRAINT ck_promo_campaigns_window CHECK (ends_at > starts_at)
);
CREATE INDEX IF NOT EXISTS idx_promo_campaigns_app ON promo_campaigns(appid, status, starts_at DESC);

CREATE TABLE IF NOT EXISTS promo_tier_grants (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    campaign_id BIGINT NOT NULL REFERENCES promo_campaigns(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 档位人数
    tier_count INTEGER NOT NULL,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_promo_tier_grants UNIQUE (campaign_id, user_id, tier_count)
);
CREATE INDEX IF NOT EXISTS idx_promo_tier_grants_user ON promo_tier_grants(appid, user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS promo_submissions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    campaign_id BIGINT NOT NULL REFERENCES promo_campaigns(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform VARCHAR(32) NOT NULL,
    url VARCHAR(1024) NOT NULL,
    -- 判重用：主机 + 路径（见 promo.ParseLink）
    url_key VARCHAR(1024) NOT NULL,
    note VARCHAR(400) NOT NULL DEFAULT '',
    -- pending 待审核 / approved 已通过 / rejected 已驳回
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reject_reason VARCHAR(400) NOT NULL DEFAULT '',
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    reviewed_by VARCHAR(128) NULL,
    reviewed_at TIMESTAMPTZ NULL,
    device_id VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_promo_submissions_status CHECK (status IN ('pending','approved','rejected'))
);
-- 同一篇文章在一期活动里只能有一条未驳回的提交（驳回之后可以重新提交）
CREATE UNIQUE INDEX IF NOT EXISTS uq_promo_submissions_url ON promo_submissions(campaign_id, url_key)
    WHERE status <> 'rejected';
CREATE INDEX IF NOT EXISTS idx_promo_submissions_user ON promo_submissions(campaign_id, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_promo_submissions_review ON promo_submissions(appid, status, created_at DESC);
