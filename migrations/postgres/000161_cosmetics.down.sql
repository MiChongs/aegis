-- +migrate Down
DROP TABLE IF EXISTS user_cosmetic_equips;
DROP TABLE IF EXISTS user_cosmetics;
DROP TABLE IF EXISTS cosmetic_revisions;
DROP TABLE IF EXISTS cosmetic_item_assets;
DROP TABLE IF EXISTS cosmetic_items;
DROP TABLE IF EXISTS cosmetic_assets;
DROP TABLE IF EXISTS cosmetic_settings;
