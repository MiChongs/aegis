-- +migrate Down

-- 先截断再收窄：回滚时库里已经有超过 128 字符的 UA，直接 ALTER 会以同一个
-- 22001 失败，把回滚本身卡住。
UPDATE daily_signins SET device_info = LEFT(device_info, 128) WHERE LENGTH(device_info) > 128;
ALTER TABLE daily_signins ALTER COLUMN device_info TYPE VARCHAR(128);
