-- +migrate Up
-- 会员档位的补充权益标签（docs/vip-tiers.md「权益标签」）：只用于会员页展示，与功能标识无关。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
ALTER TABLE vip_tiers ADD COLUMN IF NOT EXISTS perks JSONB NOT NULL DEFAULT '[]'::jsonb;
