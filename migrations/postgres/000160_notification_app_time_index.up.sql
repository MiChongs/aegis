-- +migrate Up
-- 管理端站内信列表按应用倒序翻页（WHERE appid = ? ORDER BY created_at DESC, id DESC）：群发之后表里一个应用
-- 动辄几十万行，没有这条索引每翻一页都要把整个应用的消息排一遍序。
-- CONCURRENTLY：本文件只能有这一条语句。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_notifications_app_time ON notifications (appid, created_at DESC, id DESC);
