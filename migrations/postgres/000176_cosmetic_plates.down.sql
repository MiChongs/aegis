-- +migrate Down
-- 收回铭牌槽位：先删掉铭牌装扮（持有、佩戴、版本、素材引用随外键级联），再把约束收窄回四个槽位。
DELETE FROM user_cosmetic_equips WHERE slot IN ('id_plate', 'vip_plate');
DELETE FROM cosmetic_items WHERE slot IN ('id_plate', 'vip_plate');

ALTER TABLE cosmetic_items DROP CONSTRAINT IF EXISTS ck_cosmetic_items_slot;
ALTER TABLE cosmetic_items ADD CONSTRAINT ck_cosmetic_items_slot
    CHECK (slot IN ('avatar_frame', 'profile_card', 'name_style', 'badge'));
