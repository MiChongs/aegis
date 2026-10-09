-- +migrate Up
-- 头像字节的数据库副本。
--
-- 头像的对外地址早就是永久的（/api/avatars/{token}，见 000078），但地址背后的字节
-- 只存在对象存储里。对象存储一旦拿不到这几个字节，头像就退回默认图 —— 而这种情况
-- 比想象中常见：
--
--   1. 没配存储时服务会自建一份本地存储（data/storage）。在容器里跑、又没给 data
--      挂卷的部署（Zeabur、docker run 不带 -v），每次重新部署都会把目录清空，
--      所有自定义头像一起消失。
--   2. 外部存储短暂不可用（网络抖动、熔断打开、凭据轮换）时，那一刻的请求取不到图。
--
-- 头像很小（归一化后不超过 512px，整套变体通常不到 150KB），而它是用户最在意、
-- 丢了最显眼的那一张图，值得在库里留一份：上传时与资产记录同一个事务写入，
-- 取图时先读这里，读不到再去对象存储，从对象存储读到的顺手补进来。
--
-- size 与 avatar_assets.variants 的 size 同义：0 是归一化后的原图，其余是变体边长。
-- 只给每个主体最近几张头像留副本（历史里能「换回上一张」的那几张），
-- 更早的由 ReplaceAvatarAsset 在同一个事务里清掉，不会无限增长。

CREATE TABLE IF NOT EXISTS avatar_asset_blobs (
    asset_id BIGINT NOT NULL REFERENCES avatar_assets(id) ON DELETE CASCADE,
    size INTEGER NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT 'image/jpeg',
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (asset_id, size),
    CONSTRAINT ck_avatar_asset_blobs_size CHECK (size >= 0)
);
