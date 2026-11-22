-- +migrate Down
DROP TABLE IF EXISTS cosmetic_workshop_parts;
ALTER TABLE cosmetic_settings DROP COLUMN IF EXISTS workshop;
DELETE FROM cosmetic_items WHERE origin = 'workshop';
DROP INDEX IF EXISTS idx_cosmetic_items_creator;
ALTER TABLE cosmetic_items DROP COLUMN IF EXISTS takedown_reason;
ALTER TABLE cosmetic_items DROP COLUMN IF EXISTS creator_user_id;
ALTER TABLE cosmetic_items DROP COLUMN IF EXISTS origin;
