-- 完整钱包：余额分桶（充值余额 / 赠送余额）、余额兑换积分、充值档位（docs/wallet.md）。
--
-- 本文件随每次启动整份重放，全部语句必须幂等。回填只在第一次加列时执行一次：
-- 之后的拆分由账本实时维护，再按历史重放一遍反而会覆盖掉实时状态（退回、冻结、按桶调整）。

-- ── 1. 钱包分桶 ──
--
-- bonus_balance 赠送余额；充值余额（本金）= balance - bonus_balance，不单独存列。
-- frozen_bonus 冻结中属于赠送余额的部分（AI 网关暂扣时先冻赠送余额）。
DO $$
DECLARE
    w RECORD;
    t RECORD;
    v_cash NUMERIC(18,2);
    v_bonus NUMERIC(18,2);
    v_need NUMERIC(18,2);
    v_take NUMERIC(18,2);
    v_part NUMERIC(18,2);
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'user_wallets' AND column_name = 'bonus_balance') THEN
        RETURN;
    END IF;

    ALTER TABLE user_wallets
        ADD COLUMN bonus_balance NUMERIC(18,2) NOT NULL DEFAULT 0,
        ADD COLUMN frozen_bonus NUMERIC(18,2) NOT NULL DEFAULT 0,
        ADD COLUMN total_bonus NUMERIC(18,2) NOT NULL DEFAULT 0,
        ADD COLUMN total_exchanged NUMERIC(18,2) NOT NULL DEFAULT 0;
    ALTER TABLE wallet_transactions
        ADD COLUMN bonus_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
        ADD COLUMN restores_txn_no VARCHAR(40) NULL;

    -- 按历史流水重放一遍新规则：入账里只有充值与退款进本金，其余进赠送余额；
    -- 出账先扣赠送余额（充值退款冲正先扣本金）。最后夹到当前余额以内 ——
    -- AI 网关的冻结与放回不写流水，重放出来的数可能与当前余额差一笔在途暂扣。
    FOR w IN SELECT user_id, appid, balance FROM user_wallets LOOP
        v_cash := 0;
        v_bonus := 0;
        FOR t IN SELECT id, type, amount FROM wallet_transactions
                 WHERE user_id = w.user_id AND appid = w.appid ORDER BY id LOOP
            IF t.amount > 0 THEN
                IF t.type IN ('recharge', 'refund') THEN
                    v_cash := v_cash + t.amount;
                    v_part := 0;
                ELSE
                    v_bonus := v_bonus + t.amount;
                    v_part := t.amount;
                END IF;
            ELSE
                v_need := -t.amount;
                IF t.type = 'refund' THEN
                    v_take := LEAST(GREATEST(v_cash, 0), v_need);
                    v_take := v_need - v_take;
                ELSE
                    v_take := LEAST(GREATEST(v_bonus, 0), v_need);
                END IF;
                v_take := LEAST(v_take, GREATEST(v_bonus, 0));
                v_bonus := v_bonus - v_take;
                v_cash := v_cash - (v_need - v_take);
                v_part := -v_take;
            END IF;
            IF v_part <> 0 THEN
                UPDATE wallet_transactions SET bonus_amount = v_part WHERE id = t.id;
            END IF;
        END LOOP;
        UPDATE user_wallets
        SET bonus_balance = LEAST(GREATEST(v_bonus, 0), balance),
            total_bonus = COALESCE((SELECT SUM(bonus_amount) FROM wallet_transactions
                                    WHERE user_id = w.user_id AND appid = w.appid AND bonus_amount > 0), 0)
        WHERE user_id = w.user_id;
    END LOOP;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ck_user_wallets_bonus_balance') THEN
        ALTER TABLE user_wallets ADD CONSTRAINT ck_user_wallets_bonus_balance
            CHECK (bonus_balance >= 0 AND bonus_balance <= balance);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ck_user_wallets_frozen_bonus') THEN
        ALTER TABLE user_wallets ADD CONSTRAINT ck_user_wallets_frozen_bonus
            CHECK (frozen_bonus >= 0 AND frozen_bonus <= frozen);
    END IF;
END $$;

-- 每日兑换上限按「用户 + 类型 + 时间」求和
CREATE INDEX IF NOT EXISTS idx_wallet_transactions_user_type_time
    ON wallet_transactions(user_id, type, created_at DESC);
-- 退回时累计「这笔出账已经退回了多少赠送余额」
CREATE INDEX IF NOT EXISTS idx_wallet_transactions_restores
    ON wallet_transactions(restores_txn_no) WHERE restores_txn_no IS NOT NULL;

-- ── 2. 充值档位 ──
CREATE TABLE IF NOT EXISTS wallet_recharge_packages (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    title VARCHAR(60) NOT NULL,
    amount NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    bonus_amount NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (bonus_amount >= 0),
    bonus_integral BIGINT NOT NULL DEFAULT 0 CHECK (bonus_integral >= 0),
    badge VARCHAR(20) NULL,
    description VARCHAR(200) NULL,
    per_user_limit INTEGER NOT NULL DEFAULT 0 CHECK (per_user_limit >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wallet_recharge_packages_app
    ON wallet_recharge_packages(appid, is_active, sort_order, id);

-- 档位限购按「用户 + 档位」数已付款的充值订单
CREATE INDEX IF NOT EXISTS idx_payment_orders_wallet_package
    ON payment_orders(appid, user_id, ((metadata->>'walletPackageId')))
    WHERE metadata->>'walletPackageId' IS NOT NULL;
