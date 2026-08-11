-- 回滚到 VARCHAR(256)；若现有数据超长会失败，需先清理或截断
ALTER TABLE device_marketing_names
    ALTER COLUMN identifier     TYPE VARCHAR(256),
    ALTER COLUMN marketing_name TYPE VARCHAR(256);
