-- +migrate Up
-- 自定义工坊（docs/cosmetics.md「自定义工坊」）：用户用官方零件拼自己的头像框、资料卡、昵称样式、勋章。
--
-- 作品就是 cosmetic_items 里 origin = 'workshop' 的一行（与官方装扮同一套佩戴、绘制与素材引用），
-- creator_user_id 是作者；零件是运营从素材库里挑出来允许用户使用的图片与序列帧。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

ALTER TABLE cosmetic_items ADD COLUMN IF NOT EXISTS origin VARCHAR(16) NOT NULL DEFAULT 'official';
ALTER TABLE cosmetic_items ADD COLUMN IF NOT EXISTS creator_user_id BIGINT NULL REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE cosmetic_items ADD COLUMN IF NOT EXISTS takedown_reason VARCHAR(128) NULL;
CREATE INDEX IF NOT EXISTS idx_cosmetic_items_creator
    ON cosmetic_items(appid, creator_user_id, id) WHERE origin = 'workshop';

-- 工坊设置：{enabled, freeQuota, vipQuota, tierQuotas}
ALTER TABLE cosmetic_settings ADD COLUMN IF NOT EXISTS workshop JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS cosmetic_workshop_parts (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 零件还在时素材不能删
    asset_id BIGINT NOT NULL REFERENCES cosmetic_assets(id) ON DELETE RESTRICT,
    name VARCHAR(64) NOT NULL,
    category VARCHAR(32) NOT NULL DEFAULT '',
    -- 可用的槽位：avatar_frame / profile_card / badge
    slots TEXT[] NOT NULL DEFAULT '{}',
    columns_ INTEGER NOT NULL DEFAULT 0,
    rows_ INTEGER NOT NULL DEFAULT 0,
    frame_count INTEGER NOT NULL DEFAULT 0,
    fps DOUBLE PRECISION NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cosmetic_workshop_parts_asset ON cosmetic_workshop_parts(appid, asset_id);
CREATE INDEX IF NOT EXISTS idx_cosmetic_workshop_parts_app ON cosmetic_workshop_parts(appid, sort_order, id);
