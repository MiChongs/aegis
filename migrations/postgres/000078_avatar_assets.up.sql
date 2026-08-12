-- +migrate Up
-- 头像资产表。
--
-- 在此之前，「用户的头像是什么」这件事的全部信息就是 user_profiles.avatar /
-- admin_accounts.avatar 里的一个字符串。自定义上传时那个字符串是
-- `storage://{configID}/{objectKey}` 引用，读资料时现签一个 30 分钟的代理票据
-- 换成可访问地址。这套做法有两个后果，都表现为「头像过一阵子就没了」：
--
--   1. 服务端交给客户端的是一个**会过期**的地址，而客户端理所当然地把它存了下来
--      （localStorage / Room / 邮件正文 / CDN）。票据一过期，那份副本就是死链。
--   2. 有客户端会把读回来的整份资料原样 PUT 回来 —— 于是那个临时地址被
--      **写回了数据库**，覆盖掉唯一那份 storage:// 引用。这一步之后头像
--      不是过期，是**永久丢失**：库里再也没有任何线索指向那个对象。
--
-- 这张表把第 2 种情况变成可恢复的：引用被写坏了，这里还留着 owner → 对象的
-- 对应关系，解析时可以自愈回去。同时它承载多尺寸变体与 blurhash 这些
-- 「一个字符串装不下」的元数据。
--
-- 落库引用仍然是 storage:// 那个字符串（存量数据零迁移即可继续解析），
-- 这张表是它的**元数据与历史**，不是替代品。

CREATE TABLE IF NOT EXISTS avatar_assets (
    id BIGSERIAL PRIMARY KEY,
    -- 主体：平台里有两套互不相通的主键空间，撞号会让 appid=1 的用户 42
    -- 拿到管理员 42 的头像。owner_app_id 对管理员恒为 0。
    owner_type VARCHAR(16) NOT NULL,
    owner_app_id BIGINT NOT NULL DEFAULT 0,
    owner_id BIGINT NOT NULL,

    config_id BIGINT NOT NULL,
    -- base_key 归一化原图的对象键；落库引用指向的就是它
    base_key TEXT NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT 'image/jpeg',
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    -- checksum 归一化后原图的 sha256，同时是版本号与 ETag 的来源
    checksum VARCHAR(64) NOT NULL DEFAULT '',
    blurhash VARCHAR(64) NOT NULL DEFAULT '',
    dominant_color VARCHAR(16) NOT NULL DEFAULT '',
    animated BOOLEAN NOT NULL DEFAULT FALSE,
    -- variants 各尺寸档的对象键与体积；结构见 internal/domain/avatar.Variant
    variants JSONB NOT NULL DEFAULT '[]'::jsonb,
    file_name TEXT NOT NULL DEFAULT '',
    source VARCHAR(16) NOT NULL DEFAULT 'upload',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    replaced_at TIMESTAMPTZ NULL,

    CONSTRAINT ck_avatar_assets_owner_type CHECK (owner_type IN ('user','admin')),
    CONSTRAINT ck_avatar_assets_status CHECK (status IN ('active','replaced','deleted'))
);

-- 一个主体至多一条 active。用应用层「先把旧的置为 replaced 再插新的」保证是不够的：
-- 两次并发上传会各自看到零条 active 然后各插一条，之后「当前头像是哪张」
-- 就只能靠 ORDER BY 的偶然顺序决定。
CREATE UNIQUE INDEX IF NOT EXISTS uq_avatar_assets_active
    ON avatar_assets(owner_type, owner_app_id, owner_id)
    WHERE status = 'active';

-- 历史查询与自愈都按主体 + 时间倒序取
CREATE INDEX IF NOT EXISTS idx_avatar_assets_owner
    ON avatar_assets(owner_type, owner_app_id, owner_id, created_at DESC);

-- 清理已删除存储配置的遗留资产时按 config 扫
CREATE INDEX IF NOT EXISTS idx_avatar_assets_config
    ON avatar_assets(config_id);
