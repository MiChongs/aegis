CREATE TABLE IF NOT EXISTS app_email_configs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'smtp',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    description TEXT,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, name)
);

CREATE INDEX IF NOT EXISTS idx_app_email_configs_appid ON app_email_configs(appid);
CREATE INDEX IF NOT EXISTS idx_app_email_configs_default ON app_email_configs(appid, is_default);

CREATE TABLE IF NOT EXISTS payment_configs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    payment_method VARCHAR(32) NOT NULL,
    config_name VARCHAR(100) NOT NULL,
    config_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (appid, payment_method, config_name)
);

CREATE INDEX IF NOT EXISTS idx_payment_configs_appid_method ON payment_configs(appid, payment_method);
CREATE INDEX IF NOT EXISTS idx_payment_configs_default ON payment_configs(appid, payment_method, is_default);

CREATE TABLE IF NOT EXISTS payment_orders (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    config_id BIGINT NOT NULL REFERENCES payment_configs(id) ON DELETE RESTRICT,
    order_no VARCHAR(64) NOT NULL UNIQUE,
    provider_order_no VARCHAR(128),
    subject VARCHAR(255) NOT NULL,
    body TEXT,
    amount NUMERIC(18,2) NOT NULL,
    payment_method VARCHAR(32) NOT NULL,
    provider_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    notify_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    client_ip VARCHAR(64),
    notify_url TEXT,
    return_url TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_callback JSONB NOT NULL DEFAULT '{}'::jsonb,
    paid_at TIMESTAMPTZ,
    expire_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_orders_appid_user ON payment_orders(appid, user_id);
CREATE INDEX IF NOT EXISTS idx_payment_orders_status ON payment_orders(appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_orders_provider_order ON payment_orders(provider_order_no);

CREATE TABLE IF NOT EXISTS payment_callback_logs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    order_id BIGINT REFERENCES payment_orders(id) ON DELETE SET NULL,
    payment_method VARCHAR(32) NOT NULL,
    callback_method VARCHAR(16) NOT NULL,
    client_ip VARCHAR(64),
    callback_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    verification_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_callback_logs_appid ON payment_callback_logs(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_callback_logs_order ON payment_callback_logs(order_id);

CREATE TABLE IF NOT EXISTS workflows (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    category VARCHAR(50),
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    version INT NOT NULL DEFAULT 1,
    definition JSONB NOT NULL DEFAULT '{}'::jsonb,
    trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ui_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    permissions JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflows_appid_status ON workflows(appid, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflows_appid_category ON workflows(appid, category);

CREATE TABLE IF NOT EXISTS workflow_instances (
    id BIGSERIAL PRIMARY KEY,
    workflow_id BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    instance_name VARCHAR(200),
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    priority INT NOT NULL DEFAULT 5,
    started_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    current_node_id VARCHAR(100),
    input_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_instances_workflow ON workflow_instances(workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_appid_status ON workflow_instances(appid, status, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow_tasks (
    id BIGSERIAL PRIMARY KEY,
    workflow_id BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    instance_id BIGINT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    node_id VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(32) NOT NULL DEFAULT 'user_task',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    priority INT NOT NULL DEFAULT 5,
    assigned_to BIGINT REFERENCES users(id) ON DELETE SET NULL,
    input_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    form_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    comment TEXT,
    due_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_tasks_instance ON workflow_tasks(instance_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_tasks_assignee ON workflow_tasks(appid, assigned_to, status, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow_logs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    workflow_id BIGINT REFERENCES workflows(id) ON DELETE CASCADE,
    instance_id BIGINT REFERENCES workflow_instances(id) ON DELETE CASCADE,
    task_id BIGINT REFERENCES workflow_tasks(id) ON DELETE CASCADE,
    level VARCHAR(16) NOT NULL DEFAULT 'info',
    event VARCHAR(64) NOT NULL,
    message TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_logs_appid_created ON workflow_logs(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_logs_instance ON workflow_logs(instance_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow_templates (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    category VARCHAR(50),
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    definition JSONB NOT NULL DEFAULT '{}'::jsonb,
    trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ui_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_templates_appid ON workflow_templates(appid, category, updated_at DESC);
