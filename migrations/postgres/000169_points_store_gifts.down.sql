-- +migrate Down
-- 撤销过的兑换回到已到账（旧代码不认识 revoked）；赠送商品回到发给自己。
DROP TABLE IF EXISTS points_store_gift_policies;
DROP TABLE IF EXISTS points_store_gifts;

UPDATE points_store_redemptions SET status = 'completed' WHERE status = 'revoked';
ALTER TABLE points_store_redemptions DROP CONSTRAINT IF EXISTS ck_points_store_redemptions_status;
ALTER TABLE points_store_redemptions
    ADD CONSTRAINT ck_points_store_redemptions_status CHECK (status IN ('completed','pending','cancelled'));

ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_delivery;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS delivery;
