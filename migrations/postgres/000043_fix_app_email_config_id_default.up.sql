CREATE SEQUENCE IF NOT EXISTS app_email_configs_id_seq;

ALTER TABLE app_email_configs
    ALTER COLUMN id SET DEFAULT nextval('app_email_configs_id_seq');

ALTER SEQUENCE app_email_configs_id_seq OWNED BY app_email_configs.id;

SELECT setval(
    'app_email_configs_id_seq',
    GREATEST(COALESCE((SELECT MAX(id) FROM app_email_configs), 0), 1),
    (SELECT COUNT(*) > 0 FROM app_email_configs)
);
