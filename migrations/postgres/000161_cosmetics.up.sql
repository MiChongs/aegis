-- +migrate Up
-- 个性装扮（docs/cosmetics.md）：头像框、资料卡背景、昵称样式、勋章。
--
-- 装扮（cosmetic_items）→ 持有（user_cosmetics，与 user_feature_unlocks 同构的账本行）→ 佩戴（user_cosmetic_equips）。
-- 能不能戴在佩戴时与读取时各判一次（cosmetic.Eligibility），佩戴行失效后不必清理，读出来就是空的。
--
-- 本文件随每次启动整份重放，全部语句必须幂等。

CREATE TABLE IF NOT EXISTS cosmetic_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    badge_slots INTEGER NOT NULL DEFAULT 3,
    updated_by VARCHAR(128) NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_cosmetic_settings_badges CHECK (badge_slots BETWEEN 1 AND 5)
);

-- 素材库：同一应用的装扮共用；reference 是 storage://{configID}/{objectKey}
CREATE TABLE IF NOT EXISTS cosmetic_assets (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    -- image / sprite
    kind VARCHAR(16) NOT NULL DEFAULT 'image',
    content_type VARCHAR(64) NOT NULL,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 VARCHAR(64) NOT NULL,
    reference TEXT NOT NULL,
    uploaded_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cosmetic_assets_app ON cosmetic_assets(appid, id DESC);
-- 同一应用同一份内容只存一次（重复上传返回已有的那条）
CREATE UNIQUE INDEX IF NOT EXISTS uq_cosmetic_assets_sha ON cosmetic_assets(appid, sha256);

CREATE TABLE IF NOT EXISTS cosmetic_items (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- avatar_frame / profile_card / name_style / badge
    slot VARCHAR(32) NOT NULL,
    code VARCHAR(48) NOT NULL,
    name VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    rarity VARCHAR(16) NOT NULL DEFAULT 'common',
    series VARCHAR(64) NOT NULL DEFAULT '',
    -- draft / active / archived
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    -- cosmetic.Unlock
    unlock JSONB NOT NULL DEFAULT '{"mode":"grant"}'::jsonb,
    -- 已发布的设计稿（下发给客户端）与设计器草稿
    design JSONB NULL,
    draft JSONB NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0,
    preview_ref TEXT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    hidden BOOLEAN NOT NULL DEFAULT FALSE,
    created_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ NULL,
    CONSTRAINT ck_cosmetic_items_slot CHECK (slot IN ('avatar_frame', 'profile_card', 'name_style', 'badge')),
    CONSTRAINT ck_cosmetic_items_status CHECK (status IN ('draft', 'active', 'archived')),
    CONSTRAINT ck_cosmetic_items_published CHECK (status <> 'active' OR (revision > 0 AND design IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cosmetic_items_code ON cosmetic_items(appid, code);
CREATE INDEX IF NOT EXISTS idx_cosmetic_items_app ON cosmetic_items(appid, slot, sort_order, id);

-- 素材被哪些装扮引用（草稿或已发布任一处引用即算；删素材前查它）
CREATE TABLE IF NOT EXISTS cosmetic_item_assets (
    cosmetic_id BIGINT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
    asset_id BIGINT NOT NULL REFERENCES cosmetic_assets(id) ON DELETE RESTRICT,
    PRIMARY KEY (cosmetic_id, asset_id)
);
CREATE INDEX IF NOT EXISTS idx_cosmetic_item_assets_asset ON cosmetic_item_assets(asset_id);

CREATE TABLE IF NOT EXISTS cosmetic_revisions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    cosmetic_id BIGINT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    design JSONB NOT NULL,
    note VARCHAR(256) NOT NULL DEFAULT '',
    published_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cosmetic_revisions ON cosmetic_revisions(cosmetic_id, revision);

CREATE TABLE IF NOT EXISTS user_cosmetics (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cosmetic_id BIGINT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
    -- admin_grant / points_store / lottery / card_key / item_grant / invite / leaderboard / promo
    source VARCHAR(32) NOT NULL,
    source_ref VARCHAR(64) NULL,
    duration_days INTEGER NOT NULL DEFAULT 0,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    revoked_by VARCHAR(128) NULL,
    revoke_reason VARCHAR(256) NULL,
    operator VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_user_cosmetics_window CHECK (ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT ck_user_cosmetics_days CHECK (duration_days >= 0)
);
CREATE INDEX IF NOT EXISTS idx_user_cosmetics_user ON user_cosmetics(appid, user_id, cosmetic_id);
CREATE INDEX IF NOT EXISTS idx_user_cosmetics_item ON user_cosmetics(cosmetic_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_cosmetics_app_time ON user_cosmetics(appid, created_at DESC);

CREATE TABLE IF NOT EXISTS user_cosmetic_equips (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slot VARCHAR(32) NOT NULL,
    -- 勋章戴在第几位（0 起）；其它槽位恒为 0
    position SMALLINT NOT NULL DEFAULT 0,
    cosmetic_id BIGINT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
    equipped_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id, slot, position),
    CONSTRAINT ck_user_cosmetic_equips_position CHECK (position BETWEEN 0 AND 4)
);
CREATE INDEX IF NOT EXISTS idx_user_cosmetic_equips_item ON user_cosmetic_equips(cosmetic_id);
