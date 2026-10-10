-- +migrate Up
-- 广告服务策略：用户是否同意广告服务、同意与否决定的服务范围，以及开屏广告的展示记录。
--
-- 同意广告服务是账号上的一项事实，不是某台设备上的偏好：换一台设备登录，
-- 选择跟着账号走；在官网撤回同意，App 下次取策略时就按撤回处理。
-- 拒绝的用户只能使用应用配置的「基础服务」（basic_tools），其余功能需要先同意。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 策略：一个应用一行 ──
--
-- 没有这一行表示应用没配过：网关如实回答 configured=false，由客户端按自己内置的默认策略处理，
-- 而不是替它假设一份「不要求同意」—— 那样一部署后端，已经在执行的策略反而会失效。
CREATE TABLE IF NOT EXISTS app_ad_policies (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    -- 是否要求同意广告服务：开启后，拒绝或尚未选择的用户只能使用基础服务。
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 广告服务条款的版本。条款有实质变化时调高，所有人都要按新版本重新选择一次。
    consent_version INTEGER NOT NULL DEFAULT 1,
    policy_url VARCHAR(512) NOT NULL DEFAULT '',
    -- 有效会员不受同意要求约束，也不展示开屏广告。
    vip_exempt BOOLEAN NOT NULL DEFAULT TRUE,
    -- 基础服务：拒绝广告服务时仍可使用的功能标识（客户端的工具 ID）。
    basic_tools JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- 开屏广告
    splash_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    -- 平台上的开屏广告位；留空时客户端使用包里内置的广告位。
    splash_placement_id VARCHAR(64) NOT NULL DEFAULT '',
    -- 两次开屏之间至少间隔多久，0 表示不限。
    splash_min_interval_seconds INTEGER NOT NULL DEFAULT 0,
    -- 每人每天最多展示几次，0 表示不限。
    splash_daily_limit INTEGER NOT NULL DEFAULT 0,

    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_app_ad_policies_version CHECK (consent_version >= 1)
);

-- ── 用户当前的选择：一人一行 ──
CREATE TABLE IF NOT EXISTS user_ad_consents (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    accepted BOOLEAN NOT NULL,
    -- 用户是按哪个版本的条款做的选择；低于策略的当前版本即需要重新选择。
    version INTEGER NOT NULL DEFAULT 1,
    -- app App 内选择 / web 官网账户中心 / guest_sync 未登录时在本机做的选择，登录后同步上来
    source VARCHAR(16) NOT NULL DEFAULT 'app',
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_ad_consents_app_decided
    ON user_ad_consents(appid, decided_at DESC);

-- ── 选择的变更历史：只追加 ──
--
-- 同意与撤回都要留痕：用户问「我什么时候同意过」、监管问「凭什么给他展示广告」，
-- 回答都在这里。当前状态表只存最后一次，不够回答这两个问题。
CREATE TABLE IF NOT EXISTS user_ad_consent_logs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    accepted BOOLEAN NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    source VARCHAR(16) NOT NULL DEFAULT 'app',
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_ad_consent_logs_user
    ON user_ad_consent_logs(appid, user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_user_ad_consent_logs_app_time
    ON user_ad_consent_logs(appid, created_at DESC);

-- ── 开屏广告：一次开屏尝试一行 ──
--
-- 由客户端在这次开屏结束后上报（展示、点击、加载失败或超时）。登录用户的记录挂在账号上，
-- 未登录时只有设备标识。event_id 由客户端生成：离线攒下的记录重传时不会记两笔。
CREATE TABLE IF NOT EXISTS app_splash_ad_events (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    event_id VARCHAR(64) NOT NULL,
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    placement_id VARCHAR(64) NOT NULL DEFAULT '',
    -- shown 展示后关闭 / clicked 展示并点击 / failed 加载或展示失败 / timeout 加载超时
    status VARCHAR(16) NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_message VARCHAR(256) NOT NULL DEFAULT '',
    -- 从发起请求到广告就绪、从开始展示到关闭各用了多久（毫秒）
    load_ms INTEGER NOT NULL DEFAULT 0,
    shown_ms INTEGER NOT NULL DEFAULT 0,
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    -- 客户端发生的时间（已按服务器时间校正到合理范围），统计与频控都按它算。
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_app_splash_ad_events_event UNIQUE (appid, event_id)
);

CREATE INDEX IF NOT EXISTS idx_app_splash_ad_events_app_time
    ON app_splash_ad_events(appid, occurred_at DESC);
-- 频控只数真正展示过的：部分索引让「今天展示了几次、上次是什么时候」不必扫失败记录。
CREATE INDEX IF NOT EXISTS idx_app_splash_ad_events_user_shown
    ON app_splash_ad_events(appid, user_id, occurred_at DESC)
    WHERE status IN ('shown', 'clicked') AND user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_app_splash_ad_events_device_shown
    ON app_splash_ad_events(appid, device_id, occurred_at DESC)
    WHERE status IN ('shown', 'clicked') AND device_id <> '';
