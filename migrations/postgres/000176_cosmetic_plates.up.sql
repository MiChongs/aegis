-- +migrate Up
-- 铭牌（docs/cosmetics.md「铭牌」）：号码铭牌 id_plate、会员铭牌 vip_plate 两个新槽位。
-- 设计稿多一个 plate（JSONB 内，不用改列）；佩戴表 user_cosmetic_equips 与工坊零件表的槽位没有约束，不用动。
-- 迁移器每次启动都会重跑全部 *.up.sql，因此先删后建，可重复执行。

ALTER TABLE cosmetic_items DROP CONSTRAINT IF EXISTS ck_cosmetic_items_slot;
ALTER TABLE cosmetic_items ADD CONSTRAINT ck_cosmetic_items_slot
    CHECK (slot IN ('avatar_frame', 'profile_card', 'name_style', 'badge', 'id_plate', 'vip_plate'));
