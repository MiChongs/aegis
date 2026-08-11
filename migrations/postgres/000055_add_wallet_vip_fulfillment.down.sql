-- +migrate Down
DROP INDEX IF EXISTS idx_payment_orders_unfulfilled;
ALTER TABLE payment_orders DROP COLUMN IF EXISTS fulfilled_at;
ALTER TABLE payment_orders DROP COLUMN IF EXISTS fulfillment_status;

DROP INDEX IF EXISTS idx_vip_transactions_order;
DROP INDEX IF EXISTS idx_vip_transactions_user_time;
DROP TABLE IF EXISTS vip_transactions;

DROP INDEX IF EXISTS idx_vip_plans_app_active;
DROP TABLE IF EXISTS vip_plans;

DROP INDEX IF EXISTS idx_wallet_transactions_order;
DROP INDEX IF EXISTS idx_wallet_transactions_user_time;
DROP INDEX IF EXISTS uq_wallet_transactions_idem;
DROP TABLE IF EXISTS wallet_transactions;

DROP INDEX IF EXISTS idx_user_wallets_app;
DROP TABLE IF EXISTS user_wallets;
