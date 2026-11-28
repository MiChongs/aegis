-- +migrate Up
-- 删号性能：users（及其级联子表）被删除时，Postgres 的外键触发器按 `WHERE <列> = $1` 逐行查子表。
-- 这些列此前只出现在 (appid, user_id, …) 之类复合索引的第二列，PG17 用不上，每删一个账号就要把子表整表扫一遍。
-- 000179_fk_index_* 给每个这样的外键列补一条以它开头的索引（可空列只索引非空行）。
-- CONCURRENTLY：不挡线上写入；本文件只能有这一条语句（多条语句会合成隐式事务）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ai_gateway_keys_user_id_fk ON ai_gateway_keys (user_id);
