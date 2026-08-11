DROP INDEX IF EXISTS idx_app_governance_appeals_app;
DROP INDEX IF EXISTS idx_app_governance_appeals_status;
DROP INDEX IF EXISTS uq_app_governance_appeals_pending;
DROP TABLE IF EXISTS app_governance_appeals;

DROP INDEX IF EXISTS idx_app_governance_actions_operator;
DROP INDEX IF EXISTS idx_app_governance_actions_action;
DROP INDEX IF EXISTS idx_app_governance_actions_created;
DROP INDEX IF EXISTS idx_app_governance_actions_app;
DROP TABLE IF EXISTS app_governance_actions;

DROP INDEX IF EXISTS idx_app_governance_states_state;
DROP INDEX IF EXISTS idx_app_governance_states_due;
DROP TABLE IF EXISTS app_governance_states;
