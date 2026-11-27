-- +migrate Down
DROP TABLE IF EXISTS payment_order_promotions;
DROP TABLE IF EXISTS coupon_promotions;
DROP TABLE IF EXISTS user_coupons;
DROP TABLE IF EXISTS coupon_templates;
