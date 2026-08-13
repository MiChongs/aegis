-- ── 平台级邮件通道 ──
--
-- 此前邮件配置只有应用级一档：appid 是 NOT NULL 且外键指向 apps(id)。
-- 于是管理端自己的信（管理员通知、平台告警）无处可发 —— NotifyHub 的
-- 平台级 email 渠道（notify_channels.appid = 0）走到 EmailService 时，
-- 会在「查 appid=0 这个应用」那一步拿到 40410「无法找到该应用」。
--
-- 作用域用 **appid IS NULL** 表达而不是沿用 notify_channels 的 appid = 0：
-- 0 那种写法要求列上没有外键，而这里的外键正是「删掉应用时把它的邮件配置
-- 一并带走」的唯一保证。NULL 能同时满足外键与平台级两件事。
-- Go 侧仍以 0 表示平台级（emaildomain.PlatformAppID），0 ↔ NULL 的映射
-- 收在仓储层一处。

ALTER TABLE app_email_configs ALTER COLUMN appid DROP NOT NULL;

-- shared：允许应用在**自己没有任何可用配置**时回落到这条平台通道。
-- 只对平台级配置有意义，因此加约束钉住 —— 应用级配置上出现一个
-- 打开的 shared 会让人以为「这个应用的通道被共享给别人了」。
ALTER TABLE app_email_configs
    ADD COLUMN IF NOT EXISTS shared BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE app_email_configs
    DROP CONSTRAINT IF EXISTS ck_app_email_configs_shared;
ALTER TABLE app_email_configs
    ADD CONSTRAINT ck_app_email_configs_shared
    CHECK (shared = FALSE OR appid IS NULL);

-- 原来的 UNIQUE (appid, name) 在 appid 为 NULL 时形同虚设：
-- Postgres 认为两个 NULL 互不相等，于是能建出两条同名的平台级配置，
-- 而按名字取配置的接口会随机命中其中一条。
ALTER TABLE app_email_configs DROP CONSTRAINT IF EXISTS app_email_configs_appid_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_app_email_configs_scope_name
    ON app_email_configs (COALESCE(appid, 0), name);

-- 每个作用域至多一条默认配置。此前没有这条约束，两条 is_default 同时为真时
-- 取默认配置只能靠 ORDER BY 的偶然顺序，而那正是「改了配置没生效」的常见来源。
--
-- 建索引之前必须先把存量的重复默认降级：约束是新加的，而库里可能已经有
-- 两条 is_default 的行（早期版本、或有人直接改过库）。不先清理的话，
-- 迁移会在那些部署上直接失败，而失败信息只会给出一个索引名。
-- 保留的是每个作用域里 id 最小的那条 —— 与既有取默认的排序
-- （ORDER BY is_default DESC, id ASC）一致，因此这次降级不改变任何实际行为。
UPDATE app_email_configs AS target
SET is_default = FALSE, updated_at = NOW()
WHERE is_default
  AND id <> (
    SELECT MIN(peer.id) FROM app_email_configs AS peer
    WHERE peer.is_default AND COALESCE(peer.appid, 0) = COALESCE(target.appid, 0)
  );

CREATE UNIQUE INDEX IF NOT EXISTS uq_app_email_configs_scope_default
    ON app_email_configs (COALESCE(appid, 0))
    WHERE is_default;

-- 平台级配置的检索路径与应用级不同（WHERE appid IS NULL），单独给一条部分索引。
CREATE INDEX IF NOT EXISTS idx_app_email_configs_platform
    ON app_email_configs (id)
    WHERE appid IS NULL;

-- 共享通道的解析在每个「应用自己没配」的发信请求上都会走一次，给它一条窄索引。
CREATE INDEX IF NOT EXISTS idx_app_email_configs_shared
    ON app_email_configs (is_default DESC, id)
    WHERE appid IS NULL AND shared AND enabled;

-- ── 投递留痕同样要能记平台级的信 ──
ALTER TABLE email_deliveries ALTER COLUMN appid DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_email_deliveries_platform_created
    ON email_deliveries (created_at DESC)
    WHERE appid IS NULL;

-- provider 列原本按 VARCHAR(32) 够用，但服务商从 2 档扩到 9 档后
-- 值仍然都在 8 个字符以内，不动。这里只补一条按用途的检索索引：
-- 凭证补发的频次限制按 (appid, purpose, to_address) 统计，
-- 此前只能落到 (appid, to_address) 那条索引上再过滤。
CREATE INDEX IF NOT EXISTS idx_email_deliveries_purpose
    ON email_deliveries (appid, purpose, created_at DESC);
