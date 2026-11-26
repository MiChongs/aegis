-- +migrate Down
-- 额度批次与订单一并删除（余额已扣的订单无法自动退回，回滚前先在控制台处理）。
-- 扣费来源里的 pack 去掉，旧代码不认识它；integral 不自动加回。
UPDATE ai_gateway_settings
SET settings = jsonb_set(settings, '{sources}', (
        SELECT COALESCE(jsonb_agg(to_jsonb(e.value) ORDER BY e.ord), '[]'::jsonb)
        FROM jsonb_array_elements_text(settings -> 'sources') WITH ORDINALITY AS e(value, ord)
        WHERE e.value <> 'pack'))
WHERE jsonb_typeof(settings -> 'sources') = 'array' AND settings -> 'sources' ? 'pack';

DELETE FROM ai_gateway_credit_ledger WHERE kind IN ('purchase', 'welcome', 'expire', 'revoke', 'order_refund') OR bucket = 'pack';
ALTER TABLE ai_gateway_credit_ledger DROP CONSTRAINT IF EXISTS ck_ai_gateway_credit_ledger_bucket;
ALTER TABLE ai_gateway_credit_ledger DROP CONSTRAINT IF EXISTS ck_ai_gateway_credit_ledger_kind;
ALTER TABLE ai_gateway_credit_ledger ADD CONSTRAINT ck_ai_gateway_credit_ledger_kind CHECK (kind IN ('grant', 'consume', 'refund', 'adjust'));
ALTER TABLE ai_gateway_credit_ledger DROP COLUMN IF EXISTS total_after;
ALTER TABLE ai_gateway_credit_ledger DROP COLUMN IF EXISTS lot_id;
ALTER TABLE ai_gateway_credit_ledger DROP COLUMN IF EXISTS bucket;
ALTER TABLE ai_gateway_accounts DROP COLUMN IF EXISTS welcome_claimed_at;

DROP TABLE IF EXISTS ai_gateway_package_orders;
DROP TABLE IF EXISTS ai_gateway_credit_lots;
DROP TABLE IF EXISTS ai_gateway_packages;
