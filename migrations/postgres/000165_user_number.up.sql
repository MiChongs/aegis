-- +migrate Up
-- 用户号码（docs/user-number.md）。
--
-- users.public_id（000112）是 16 位、全站唯一、终生不变的对外身份：OAuth2 sub、头像与广告令牌、
-- 一起听、客户端本地数据都挂在它上面，所以它不能改。这一版另加一层「号码」：
--   * 5–12 位纯数字，按应用唯一（不同应用可以有同一个号）；
--   * 可以更换：改号卡、靓号券、摇号、管理员指定（权益与流水见下面几张表）；
--   * 只用于展示与查找，任何令牌、签名、第三方关联都不挂在它上面，换号不让任何东西失效。
--
-- 发号：BEFORE INSERT 触发器（列默认值拿不到 NEW.appid）按应用的默认位数发一个「普通号」——
-- aegis_user_number_is_plain 是 Go 侧规律目录（internal/domain/usernumber/patterns.go）在默认位数下的
-- 超集过滤，放行的号在 Go 侧一定判普通（TestPlainFilterIsSuperset 钉住），靓号不会被白送出去。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS user_number_settings (
    appid      BIGINT PRIMARY KEY,
    config     JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 占用：一个号同一时刻最多一条。reserved 后台保留（只能由后台指定），frozen 换下来的旧号
-- （冻结期内只有原主人能换回，注销用户的号 user_id 为空、谁都拿不到），candidate 摇号候选（锁定期内只给摇出它的人）。
-- expires_at 为空表示不过期；过期的行不必有人清，判定时按 expires_at 过滤，再次占用时覆盖。
CREATE TABLE IF NOT EXISTS user_number_holds (
    appid      BIGINT NOT NULL,
    number     BIGINT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('reserved', 'frozen', 'candidate')),
    tier       SMALLINT NOT NULL DEFAULT 0,
    user_id    BIGINT NULL,
    draw_id    BIGINT NULL,
    note       TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NULL,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, number)
);
CREATE INDEX IF NOT EXISTS idx_user_number_holds_kind ON user_number_holds(appid, kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_number_holds_draw ON user_number_holds(draw_id) WHERE draw_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_number_history (
    id          BIGSERIAL PRIMARY KEY,
    appid       BIGINT NOT NULL,
    user_id     BIGINT NOT NULL,
    old_number  BIGINT NULL,
    new_number  BIGINT NOT NULL,
    tier        SMALLINT NOT NULL DEFAULT 0,
    source      TEXT NOT NULL,
    right_kind  TEXT NOT NULL DEFAULT '',
    right_level SMALLINT NOT NULL DEFAULT 0,
    draw_id     BIGINT NULL,
    operator    TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_number_history_user ON user_number_history(appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_number_history_app ON user_number_history(appid, created_at DESC);

-- 权益账户：改号卡（change, level 0）、靓号券（ticket, level 1–4）、摇号次数（draw, level 0）。
CREATE TABLE IF NOT EXISTS user_number_rights (
    appid      BIGINT NOT NULL,
    user_id    BIGINT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('change', 'ticket', 'draw')),
    level      SMALLINT NOT NULL DEFAULT 0,
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    earned     BIGINT NOT NULL DEFAULT 0,
    used       BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id, kind, level),
    CHECK ((kind = 'ticket' AND level BETWEEN 1 AND 4) OR (kind <> 'ticket' AND level = 0))
);

CREATE TABLE IF NOT EXISTS user_number_ledger (
    id            BIGSERIAL PRIMARY KEY,
    appid         BIGINT NOT NULL,
    user_id       BIGINT NOT NULL,
    kind          TEXT NOT NULL,
    level         SMALLINT NOT NULL DEFAULT 0,
    delta         BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    origin        TEXT NOT NULL,
    ref           TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL DEFAULT '',
    operator      TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_number_ledger_user ON user_number_ledger(appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_number_ledger_app ON user_number_ledger(appid, created_at DESC);

CREATE TABLE IF NOT EXISTS user_number_draws (
    id         BIGSERIAL PRIMARY KEY,
    appid      BIGINT NOT NULL,
    user_id    BIGINT NOT NULL,
    candidates JSONB NOT NULL DEFAULT '[]'::jsonb,
    luck       DOUBLE PRECISION NOT NULL DEFAULT 1,
    status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'picked', 'discarded', 'expired')),
    picked     BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    closed_at  TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_user_number_draws_user ON user_number_draws(appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_number_draws_app ON user_number_draws(appid, created_at DESC);
-- 一个人同一时刻最多一张打开的摇号单
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_number_draws_open ON user_number_draws(appid, user_id) WHERE status = 'open';

ALTER TABLE users ADD COLUMN IF NOT EXISTS number BIGINT NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS number_tier SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS number_changed_at TIMESTAMPTZ NULL;

-- 唯一索引先于回填：发号函数的查重要靠它走索引
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_app_number ON users(appid, number);

-- 默认位数下的「普通号」过滤（Go 侧规律目录的超集，改规律时两边一起改）：
-- 三连同号、AABB / ABAB 形（不论 A、B 是否相同）、四位顺子、吉利尾号、年份尾号、回文
CREATE OR REPLACE FUNCTION aegis_user_number_is_plain(n BIGINT) RETURNS BOOLEAN AS $$
DECLARE
    s TEXT := n::text;
    tail INT;
BEGIN
    IF s ~ '(\d)\1\1' OR s ~ '(\d)\1(\d)\2' OR s ~ '(\d)(\d)\1\2' THEN
        RETURN FALSE;
    END IF;
    IF s ~ '(0123|1234|2345|3456|4567|5678|6789|9876|8765|7654|6543|5432|4321|3210)' THEN
        RETURN FALSE;
    END IF;
    IF s ~ '(520|1314|168|518)$' THEN
        RETURN FALSE;
    END IF;
    tail := right(s, 4)::int;
    IF tail BETWEEN 1950 AND 2039 THEN
        RETURN FALSE;
    END IF;
    RETURN s <> reverse(s);
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- 按应用的默认位数（user_number_settings.config.defaultLength，8–12，缺省 9）发一个空闲的普通号。
-- 随机源同 000112 用 gen_random_uuid()；查重看本应用正在用的号与仍生效的占用。
CREATE OR REPLACE FUNCTION aegis_new_user_number(p_appid BIGINT) RETURNS BIGINT AS $$
DECLARE
    len INT;
    lo BIGINT;
    span BIGINT;
    raw BIGINT;
    candidate BIGINT;
BEGIN
    SELECT (config->>'defaultLength')::int INTO len FROM user_number_settings WHERE appid = p_appid;
    len := LEAST(GREATEST(COALESCE(len, 9), 8), 12);
    lo := (10::bigint ^ (len - 1))::bigint;
    span := 9 * lo;
    FOR attempt IN 1..64 LOOP
        raw := ('x' || encode(substring(uuid_send(gen_random_uuid()) FROM 9 FOR 8), 'hex'))::bit(64)::bigint;
        candidate := lo + ((raw % span) + span) % span;
        IF aegis_user_number_is_plain(candidate)
           AND NOT EXISTS (SELECT 1 FROM users WHERE appid = p_appid AND number = candidate)
           AND NOT EXISTS (SELECT 1 FROM user_number_holds h
                           WHERE h.appid = p_appid AND h.number = candidate
                             AND (h.expires_at IS NULL OR h.expires_at > NOW())) THEN
            RETURN candidate;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'aegis_new_user_number: no free number for app % after 64 attempts', p_appid;
END;
$$ LANGUAGE plpgsql VOLATILE;

-- 回填存量用户：逐行发号，查重才看得到前面刚发出的号（同 000112）。
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT id, appid FROM users WHERE number IS NULL ORDER BY id LOOP
        UPDATE users SET number = aegis_new_user_number(r.appid), number_tier = 0 WHERE id = r.id;
    END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION aegis_users_number_assign() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.number IS NULL THEN
        NEW.number := aegis_new_user_number(NEW.appid);
        NEW.number_tier := 0;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_users_number_assign ON users;
CREATE TRIGGER trg_users_number_assign
    BEFORE INSERT ON users
    FOR EACH ROW EXECUTE FUNCTION aegis_users_number_assign();

-- 删号（注销、管理员删除）：号码按应用的冻结天数冻结、不挂原主人，冻结期内谁都拿不到，
-- 免得注销后立刻有人顶着同一个号冒充。
CREATE OR REPLACE FUNCTION aegis_users_number_release() RETURNS TRIGGER AS $$
DECLARE
    days INT;
BEGIN
    IF OLD.number IS NULL THEN
        RETURN OLD;
    END IF;
    SELECT (config->>'freezeDays')::int INTO days FROM user_number_settings WHERE appid = OLD.appid;
    days := COALESCE(days, 90);
    IF days > 0 THEN
        INSERT INTO user_number_holds (appid, number, kind, tier, user_id, note, expires_at, created_by)
        VALUES (OLD.appid, OLD.number, 'frozen', OLD.number_tier, NULL, '账号已删除', NOW() + make_interval(days => days), 'system')
        ON CONFLICT (appid, number) DO UPDATE
            SET kind = 'frozen', tier = EXCLUDED.tier, user_id = NULL, draw_id = NULL, note = EXCLUDED.note,
                expires_at = EXCLUDED.expires_at, created_by = EXCLUDED.created_by, created_at = NOW()
            WHERE user_number_holds.kind <> 'reserved';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_users_number_release ON users;
CREATE TRIGGER trg_users_number_release
    AFTER DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION aegis_users_number_release();

ALTER TABLE users ALTER COLUMN number SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ck_users_number_range') THEN
        ALTER TABLE users ADD CONSTRAINT ck_users_number_range
            CHECK (number BETWEEN 10000 AND 999999999999);
    END IF;
END;
$$;
