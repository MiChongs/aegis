-- +migrate Up

-- ── 签到记录的 device_info 装不下一个 User-Agent ──
-- 这一列存的是 `c.Request.UserAgent()` 的原文，而 VARCHAR(128) 装不下任何一个
-- 现代客户端的 UA：Android WebView 的 UA 本身就有 150～180 字符，再加上应用自己的
-- 标识就过 200 了。于是签到直接以
--   ERROR: value too long for type character varying(128) (SQLSTATE 22001)
-- 失败 —— 报错发生在写库那一刻，用户看到的是「签到失败」，而积分与连签天数
-- 在同一个事务里一起回滚，看不出与 UA 有任何关系。
--
-- 这张表里其余同类字段（ip_address / location）都够用，只有它是按「设备型号」
-- 那种短串估的宽度，与实际写进去的东西对不上。
--
-- 选 512 而不是 TEXT：仓储层按同一个数字截断，schema 与代码里的上限是同一个，
-- 谁改都不会只改一半。真正超过 512 的 UA 是构造出来的，不是浏览器发的。
ALTER TABLE daily_signins ALTER COLUMN device_info TYPE VARCHAR(512);
