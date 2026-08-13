-- +migrate Up
-- 应用级卡密系统。
--
-- 一张卡有两种形态，靠 kind 区分，但共用同一套生成、作废、核销与权益目录：
--
--   login   授权卡：卡**就是**登录凭证。首次使用自动建号并绑定，之后凭卡登录；
--           卡有授权期，到期即登不进去；一张卡能在几台设备上用由 max_devices 决定。
--   redeem  兑换卡：给已登录用户发权益（会员 / 积分 / 经验 / 余额 / 抽奖次数 / 设备位）。
--
-- 两种形态共用一张 card_keys 表而不是各建一张：生成、导出、作废、查询、
-- 核销记录这五件事逐字相同，分表意味着这五处逻辑各写两遍，
-- 而它们迟早会漂移成两套行为。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行
-- （与 000059 / 000079 同一约束：约束先 DROP IF EXISTS 再 ADD，否则第二次执行报 42710）。

-- ── 批次 ──
--
-- 卡密是批量生成的，而「这一批是什么卡、发什么、有效多久」只该记一次。
-- 单卡行上仍冗余了 kind / max_devices：核销与登录是热路径，
-- 为了取这两个值去 JOIN 批次表不划算，而它们在生成之后不会再变。
CREATE TABLE IF NOT EXISTS card_key_batches (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'redeem',
    remark TEXT NOT NULL DEFAULT '',

    -- 生成规则。只作留档：卡面已经生成完毕，改这里不会重排已有的卡，
    -- 但补发同一批次时要照着原样式生成，否则同一批卡长得不一样。
    code_prefix VARCHAR(16) NOT NULL DEFAULT '',
    segments SMALLINT NOT NULL DEFAULT 4,
    segment_length SMALLINT NOT NULL DEFAULT 4,

    -- 权益清单，元素形如 {"type":"vip_days","amount":30}。
    -- 取值受 internal/domain/cardkey 的权益目录约束，不在这里用 CHECK 表达：
    -- 目录会随功能增加而变长，而 CHECK 改不动已经写进库的行。
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- 授权卡：一张卡能在几台设备上使用。兑换卡忽略此列。
    max_devices INTEGER NOT NULL DEFAULT 1,

    -- 有效期模式：
    --   permanent            永不过期
    --   fixed_until          批次统一到期（生成时就写死 card_keys.expires_at）
    --   days_from_first_use  激活即计时（首次使用时才写 expires_at）
    -- 第三种是卡密的常态：卖出去到被使用之间的时间不该算进用户的授权期。
    validity_mode VARCHAR(24) NOT NULL DEFAULT 'permanent',
    validity_days INTEGER NOT NULL DEFAULT 0,
    valid_until TIMESTAMPTZ NULL,

    total INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_kind;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_kind CHECK (kind IN ('login', 'redeem'));

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_status;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_status CHECK (status IN ('active', 'disabled'));

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_validity_mode;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_validity_mode
    CHECK (validity_mode IN ('permanent', 'fixed_until', 'days_from_first_use'));

-- 选了「激活即计时」却没填天数，等于生成了一批立刻过期的卡，
-- 而那要等到第一个用户来兑换时才被发现。
ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_validity_days;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_validity_days
    CHECK (validity_mode <> 'days_from_first_use' OR validity_days > 0);

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_valid_until;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_valid_until
    CHECK (validity_mode <> 'fixed_until' OR valid_until IS NOT NULL);

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_max_devices;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_max_devices CHECK (max_devices BETWEEN 1 AND 64);

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_segments;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_segments
    CHECK (segments BETWEEN 1 AND 8 AND segment_length BETWEEN 3 AND 12);

CREATE INDEX IF NOT EXISTS idx_card_key_batches_app ON card_key_batches(appid, id DESC);

-- ── 单卡 ──
--
-- status 只有四档，且**过期不是其中之一**：过期是 expires_at 与当前时间比出来的结论，
-- 存成状态就需要一个定时任务去翻它，而那个任务停掉的表现是「过期卡还能用」。
--
--   unused    未使用
--   active    授权卡已激活（在授权期内可反复登录）
--   used      兑换卡已核销（终态）
--   disabled  已作废
CREATE TABLE IF NOT EXISTS card_keys (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    batch_id BIGINT NOT NULL REFERENCES card_key_batches(id) ON DELETE CASCADE,
    code VARCHAR(64) NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'redeem',
    status VARCHAR(16) NOT NULL DEFAULT 'unused',

    -- 授权卡：卡绑到哪个账号。首次登录时自动建号并写入；
    -- 同一个用户可以绑多张授权卡（用新卡续期），因此这里**不能**加唯一约束。
    bound_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    max_devices INTEGER NOT NULL DEFAULT 1,

    activated_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NULL,
    used_at TIMESTAMPTZ NULL,
    disabled_at TIMESTAMPTZ NULL,
    disabled_reason VARCHAR(256) NOT NULL DEFAULT '',
    remark VARCHAR(256) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- 一码一用的第一道保证。查询也走它：兑换与登录都是「拿着卡面文本找卡」。
    CONSTRAINT uq_card_keys_code UNIQUE (appid, code)
);

ALTER TABLE card_keys DROP CONSTRAINT IF EXISTS ck_card_keys_kind;
ALTER TABLE card_keys ADD CONSTRAINT ck_card_keys_kind CHECK (kind IN ('login', 'redeem'));

ALTER TABLE card_keys DROP CONSTRAINT IF EXISTS ck_card_keys_status;
ALTER TABLE card_keys
    ADD CONSTRAINT ck_card_keys_status CHECK (status IN ('unused', 'active', 'used', 'disabled'));

ALTER TABLE card_keys DROP CONSTRAINT IF EXISTS ck_card_keys_max_devices;
ALTER TABLE card_keys ADD CONSTRAINT ck_card_keys_max_devices CHECK (max_devices BETWEEN 1 AND 64);

CREATE INDEX IF NOT EXISTS idx_card_keys_batch ON card_keys(batch_id, id);
CREATE INDEX IF NOT EXISTS idx_card_keys_app_status ON card_keys(appid, status, id DESC);
CREATE INDEX IF NOT EXISTS idx_card_keys_bound_user
    ON card_keys(appid, bound_user_id) WHERE bound_user_id IS NOT NULL;

-- ── 卡与设备的绑定 ──
--
-- 「一卡三机」的载体。现有的任何一处都放不下这个语义：
-- 登录一致性基线是「一个用户绑一台设备」且只活在 Redis 里（会过期），
-- device_fingerprints 的 device_id 是全局唯一的（一行一设备，不是多对多）。
CREATE TABLE IF NOT EXISTS card_key_devices (
    id BIGSERIAL PRIMARY KEY,
    card_key_id BIGINT NOT NULL REFERENCES card_keys(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    device_id VARCHAR(128) NOT NULL,
    device_name VARCHAR(128) NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seen_count BIGINT NOT NULL DEFAULT 1,
    -- 同一台设备重复登录同一张卡不该占两个名额；
    -- 并发的两次首登也由它兜住（应用层的 COUNT 检查拦不住）。
    CONSTRAINT uq_card_key_devices UNIQUE (card_key_id, device_id)
);

CREATE INDEX IF NOT EXISTS idx_card_key_devices_card ON card_key_devices(card_key_id, last_seen_at DESC);

-- ── 核销流水 ──
--
-- rewards 是**核销那一刻的权益快照**。批次配置随时会改，而已经发出去的权益
-- 不该被追溯改写 —— 与 vip_transactions.features 同一条理由。
-- results 记的是实际发放结果（发了多少、落在哪笔流水上），排障时要靠它回答
-- 「用户说没到账」到底是没发还是发到别处了。
CREATE TABLE IF NOT EXISTS card_key_redemptions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    card_key_id BIGINT NOT NULL REFERENCES card_keys(id) ON DELETE CASCADE,
    batch_id BIGINT NULL,
    code VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- redeem 用户主动兑换 / login 授权卡首次激活时随带的权益 / admin 管理员代兑
    source VARCHAR(16) NOT NULL DEFAULT 'redeem',
    device_id VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    user_agent VARCHAR(256) NULL,
    operator VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 一码一用的第二道保证。条件 UPDATE 抢占已经拦住了并发，
    -- 这条约束兜住的是「抢占逻辑本身被改坏」——那种 bug 的表现是重复发钱。
    CONSTRAINT uq_card_key_redemptions_card UNIQUE (card_key_id)
);

CREATE INDEX IF NOT EXISTS idx_card_key_redemptions_app_time
    ON card_key_redemptions(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_card_key_redemptions_user
    ON card_key_redemptions(appid, user_id, created_at DESC);

-- ── 抽奖次数余额 ──
--
-- 抽奖此前没有「次数」这个概念：dailyLimit / totalLimit 是数历史行数出来的，
-- 没有任何地方存得下「这个人还剩几次」。卡密要发抽奖次数就必须先有这个账户。
--
-- 余额是**应用级**而不是活动级：卡密卖的是「抽奖机会」，不是「某个活动的机会」，
-- 后者会在活动下线时变成一堆无处可用的余额。
CREATE TABLE IF NOT EXISTS lottery_draw_grants (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    balance INTEGER NOT NULL DEFAULT 0,
    total_granted BIGINT NOT NULL DEFAULT 0,
    total_consumed BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_lottery_draw_grants UNIQUE (appid, user_id)
);

ALTER TABLE lottery_draw_grants DROP CONSTRAINT IF EXISTS ck_lottery_draw_grants_balance;
ALTER TABLE lottery_draw_grants
    ADD CONSTRAINT ck_lottery_draw_grants_balance CHECK (balance >= 0);
