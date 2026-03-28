-- 增强渠道表：新增多维度管理字段
ALTER TABLE app_version_channels
  ADD COLUMN IF NOT EXISTS priority     INT          NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS color        VARCHAR(32)  NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS level        VARCHAR(32)  NOT NULL DEFAULT 'stable',
  ADD COLUMN IF NOT EXISTS rollout_pct  INT          NOT NULL DEFAULT 100,
  ADD COLUMN IF NOT EXISTS platforms    TEXT[]        NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS min_version_code BIGINT   NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS max_version_code BIGINT   NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS rules        JSONB        NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN app_version_channels.priority         IS '排序优先级，数值越大越优先';
COMMENT ON COLUMN app_version_channels.color            IS '渠道标签颜色（hex），用于 UI 展示';
COMMENT ON COLUMN app_version_channels.level            IS '渠道级别：stable / beta / alpha / canary / nightly';
COMMENT ON COLUMN app_version_channels.rollout_pct      IS '灰度放量百分比（0-100）';
COMMENT ON COLUMN app_version_channels.platforms        IS '限定平台列表（空=全平台）';
COMMENT ON COLUMN app_version_channels.min_version_code IS '最低适用版本码（0=不限）';
COMMENT ON COLUMN app_version_channels.max_version_code IS '最高适用版本码（0=不限）';
COMMENT ON COLUMN app_version_channels.rules            IS '灰度分发规则 JSON 数组，结构 [{field, op, value}]';
