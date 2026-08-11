-- +migrate Up
-- 平台级 Banner — 面向管理员（超级管理员专属管理）的全局横幅
-- 与应用级 banners 表的差异：
--   1. 无 appid 外键，天然全局
--   2. 增加 image_url 专列（强视觉导向）
--   3. type 限定在枚举白名单（info/notice/maintenance/release/security）
--   4. created_by 记录创建管理员 ID（审计用，不加外键避免管理员删除级联影响）
CREATE TABLE IF NOT EXISTS platform_banners (
    id BIGSERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL,
    click_url TEXT NOT NULL DEFAULT '',
    type VARCHAR(32) NOT NULL DEFAULT 'info',
    position INTEGER NOT NULL DEFAULT 0,
    status BOOLEAN NOT NULL DEFAULT TRUE,
    start_time TIMESTAMPTZ NULL,
    end_time TIMESTAMPTZ NULL,
    created_by BIGINT NULL,
    view_count BIGINT NOT NULL DEFAULT 0,
    click_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 展示态常用条件索引：status=true 的按 position 排序
CREATE INDEX IF NOT EXISTS idx_platform_banners_active
    ON platform_banners(position ASC)
    WHERE status = TRUE;

-- 时间窗口过滤索引
CREATE INDEX IF NOT EXISTS idx_platform_banners_window
    ON platform_banners(start_time, end_time);
