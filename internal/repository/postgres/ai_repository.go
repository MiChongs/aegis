package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	aidomain "aegis/internal/domain/ai"
)

// appid 在库里用 NULL 表示平台级，Go 侧用 0（aidomain.PlatformAppID）。
// 读取一律 COALESCE 回 0，写入一律经 nullableAppID —— 与邮件通道同一条映射约定。

const aiConfigColumns = `id, COALESCE(appid, 0), name, provider, enabled, is_default, shared, priority, description, settings, secrets, created_at, updated_at`

// aiScopeCondition 生成作用域过滤片段。
// 刻意分成 IS NULL 与等值两条而不是 COALESCE —— 后者让 appid 上的索引失效。
func aiScopeCondition(appID int64, args *[]any) string {
	if appID == aidomain.PlatformAppID {
		return "appid IS NULL"
	}
	*args = append(*args, appID)
	return fmt.Sprintf("appid = $%d", len(*args))
}

func nullableAIAppID(appID int64) any {
	if appID == aidomain.PlatformAppID {
		return nil
	}
	return appID
}

// ── 供应商配置 ──

func (r *Repository) ListAIProviderConfigs(ctx context.Context, appID int64) ([]aidomain.Config, error) {
	args := make([]any, 0, 1)
	where := aiScopeCondition(appID, &args)
	rows, err := r.pool.Query(ctx,
		`SELECT `+aiConfigColumns+` FROM ai_provider_configs WHERE `+where+` ORDER BY priority ASC, is_default DESC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.Config, 0, 4)
	for rows.Next() {
		item, err := scanAIConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAIProviderConfigByID(ctx context.Context, appID int64, id int64) (*aidomain.Config, error) {
	args := make([]any, 0, 2)
	where := aiScopeCondition(appID, &args)
	args = append(args, id)
	return scanAIConfig(r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM ai_provider_configs WHERE %s AND id = $%d LIMIT 1`, aiConfigColumns, where, len(args)), args...))
}

// ListEnabledAIProviderConfigs 供链路解析用：某作用域下启用的配置，按链路次序排。
func (r *Repository) ListEnabledAIProviderConfigs(ctx context.Context, appID int64) ([]aidomain.Config, error) {
	args := make([]any, 0, 1)
	where := aiScopeCondition(appID, &args)
	rows, err := r.pool.Query(ctx,
		`SELECT `+aiConfigColumns+` FROM ai_provider_configs WHERE `+where+` AND enabled = TRUE ORDER BY priority ASC, is_default DESC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.Config, 0, 4)
	for rows.Next() {
		item, err := scanAIConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListSharedPlatformAIConfigs 平台级**已共享**且启用的通道，应用无自有配置时的回落目标。
// shared 与 enabled 钉在查询里：平台管理员关掉共享的那一刻回落立即停止。
func (r *Repository) ListSharedPlatformAIConfigs(ctx context.Context) ([]aidomain.Config, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+aiConfigColumns+` FROM ai_provider_configs
WHERE appid IS NULL AND shared = TRUE AND enabled = TRUE
ORDER BY priority ASC, is_default DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.Config, 0, 2)
	for rows.Next() {
		item, err := scanAIConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertAIProviderConfig(ctx context.Context, item aidomain.Config) (*aidomain.Config, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if item.IsDefault {
		scopeArgs := make([]any, 0, 2)
		scopeWhere := aiScopeCondition(item.AppID, &scopeArgs)
		scopeArgs = append(scopeArgs, item.ID)
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`UPDATE ai_provider_configs SET is_default = FALSE, updated_at = NOW() WHERE %s AND id <> $%d`, scopeWhere, len(scopeArgs)),
			scopeArgs...); err != nil {
			return nil, err
		}
	}

	settingsJSON, err := json.Marshal(compactStringMap(item.Settings))
	if err != nil {
		return nil, err
	}
	secretsJSON, err := json.Marshal(compactStringMap(item.SecretsCipher))
	if err != nil {
		return nil, err
	}
	// shared 只在平台级配置上有意义，落库前夹一遍（库里的 CHECK 只是最后一道保险）。
	shared := item.Shared && item.IsPlatform()

	var saved *aidomain.Config
	if item.ID > 0 {
		saved, err = scanAIConfig(tx.QueryRow(ctx, `UPDATE ai_provider_configs SET
	name = $2, provider = $3, enabled = $4, is_default = $5, shared = $6,
	priority = $7, description = $8, settings = $9, secrets = $10, updated_at = NOW()
WHERE id = $1
RETURNING `+aiConfigColumns,
			item.ID, item.Name, item.Provider, item.Enabled, item.IsDefault, shared,
			item.Priority, item.Description, settingsJSON, secretsJSON))
	} else {
		saved, err = scanAIConfig(tx.QueryRow(ctx, `INSERT INTO ai_provider_configs
	(appid, name, provider, enabled, is_default, shared, priority, description, settings, secrets, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
RETURNING `+aiConfigColumns,
			nullableAIAppID(item.AppID), item.Name, item.Provider, item.Enabled, item.IsDefault, shared,
			item.Priority, item.Description, settingsJSON, secretsJSON))
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *Repository) DeleteAIProviderConfig(ctx context.Context, appID int64, id int64) (bool, error) {
	args := make([]any, 0, 2)
	where := aiScopeCondition(appID, &args)
	args = append(args, id)
	result, err := r.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM ai_provider_configs WHERE %s AND id = $%d`, where, len(args)), args...)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func scanAIConfig(row interface{ Scan(dest ...any) error }) (*aidomain.Config, error) {
	var item aidomain.Config
	var settingsBytes, secretsBytes []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Provider, &item.Enabled, &item.IsDefault,
		&item.Shared, &item.Priority, &item.Description, &settingsBytes, &secretsBytes,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Settings = map[string]string{}
	item.SecretsCipher = map[string]string{}
	item.Secrets = map[string]string{}
	_ = json.Unmarshal(settingsBytes, &item.Settings)
	_ = json.Unmarshal(secretsBytes, &item.SecretsCipher)
	return &item, nil
}

// ── 技能 ──

const aiSkillColumns = `id, COALESCE(appid, 0), key, name, description, content, enabled, created_at, updated_at`

func (r *Repository) ListAISkills(ctx context.Context, appID int64) ([]aidomain.Skill, error) {
	args := make([]any, 0, 1)
	where := aiScopeCondition(appID, &args)
	rows, err := r.pool.Query(ctx,
		`SELECT `+aiSkillColumns+` FROM ai_skills WHERE `+where+` ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.Skill, 0, 4)
	for rows.Next() {
		var item aidomain.Skill
		if err := rows.Scan(&item.ID, &item.AppID, &item.Key, &item.Name, &item.Description,
			&item.Content, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAISkillByID(ctx context.Context, appID int64, id int64) (*aidomain.Skill, error) {
	args := make([]any, 0, 2)
	where := aiScopeCondition(appID, &args)
	args = append(args, id)
	var item aidomain.Skill
	err := r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM ai_skills WHERE %s AND id = $%d LIMIT 1`, aiSkillColumns, where, len(args)), args...).
		Scan(&item.ID, &item.AppID, &item.Key, &item.Name, &item.Description,
			&item.Content, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func (r *Repository) UpsertAISkill(ctx context.Context, item aidomain.Skill) (*aidomain.Skill, error) {
	var saved aidomain.Skill
	var err error
	if item.ID > 0 {
		err = r.pool.QueryRow(ctx, `UPDATE ai_skills SET
	key = $2, name = $3, description = $4, content = $5, enabled = $6, updated_at = NOW()
WHERE id = $1
RETURNING `+aiSkillColumns,
			item.ID, item.Key, item.Name, item.Description, item.Content, item.Enabled).
			Scan(&saved.ID, &saved.AppID, &saved.Key, &saved.Name, &saved.Description,
				&saved.Content, &saved.Enabled, &saved.CreatedAt, &saved.UpdatedAt)
	} else {
		err = r.pool.QueryRow(ctx, `INSERT INTO ai_skills
	(appid, key, name, description, content, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
RETURNING `+aiSkillColumns,
			nullableAIAppID(item.AppID), item.Key, item.Name, item.Description, item.Content, item.Enabled).
			Scan(&saved.ID, &saved.AppID, &saved.Key, &saved.Name, &saved.Description,
				&saved.Content, &saved.Enabled, &saved.CreatedAt, &saved.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func (r *Repository) DeleteAISkill(ctx context.Context, appID int64, id int64) (bool, error) {
	args := make([]any, 0, 2)
	where := aiScopeCondition(appID, &args)
	args = append(args, id)
	result, err := r.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM ai_skills WHERE %s AND id = $%d`, where, len(args)), args...)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// ── MCP 服务器 ──

const aiMCPColumns = `id, COALESCE(appid, 0), name, url, enabled, description, headers_cipher, created_at, updated_at`

func (r *Repository) ListAIMCPServers(ctx context.Context, appID int64) ([]aidomain.MCPServer, error) {
	args := make([]any, 0, 1)
	where := aiScopeCondition(appID, &args)
	rows, err := r.pool.Query(ctx,
		`SELECT `+aiMCPColumns+` FROM ai_mcp_servers WHERE `+where+` ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.MCPServer, 0, 2)
	for rows.Next() {
		var item aidomain.MCPServer
		if err := rows.Scan(&item.ID, &item.AppID, &item.Name, &item.URL, &item.Enabled,
			&item.Description, &item.HeadersCipher, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAIMCPServerByID(ctx context.Context, appID int64, id int64) (*aidomain.MCPServer, error) {
	args := make([]any, 0, 2)
	where := aiScopeCondition(appID, &args)
	args = append(args, id)
	var item aidomain.MCPServer
	err := r.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM ai_mcp_servers WHERE %s AND id = $%d LIMIT 1`, aiMCPColumns, where, len(args)), args...).
		Scan(&item.ID, &item.AppID, &item.Name, &item.URL, &item.Enabled,
			&item.Description, &item.HeadersCipher, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func (r *Repository) UpsertAIMCPServer(ctx context.Context, item aidomain.MCPServer) (*aidomain.MCPServer, error) {
	var saved aidomain.MCPServer
	var err error
	if item.ID > 0 {
		err = r.pool.QueryRow(ctx, `UPDATE ai_mcp_servers SET
	name = $2, url = $3, enabled = $4, description = $5, headers_cipher = $6, updated_at = NOW()
WHERE id = $1
RETURNING `+aiMCPColumns,
			item.ID, item.Name, item.URL, item.Enabled, item.Description, item.HeadersCipher).
			Scan(&saved.ID, &saved.AppID, &saved.Name, &saved.URL, &saved.Enabled,
				&saved.Description, &saved.HeadersCipher, &saved.CreatedAt, &saved.UpdatedAt)
	} else {
		err = r.pool.QueryRow(ctx, `INSERT INTO ai_mcp_servers
	(appid, name, url, enabled, description, headers_cipher, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
RETURNING `+aiMCPColumns,
			nullableAIAppID(item.AppID), item.Name, item.URL, item.Enabled, item.Description, item.HeadersCipher).
			Scan(&saved.ID, &saved.AppID, &saved.Name, &saved.URL, &saved.Enabled,
				&saved.Description, &saved.HeadersCipher, &saved.CreatedAt, &saved.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func (r *Repository) DeleteAIMCPServer(ctx context.Context, appID int64, id int64) (bool, error) {
	args := make([]any, 0, 2)
	where := aiScopeCondition(appID, &args)
	args = append(args, id)
	result, err := r.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM ai_mcp_servers WHERE %s AND id = $%d`, where, len(args)), args...)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// ── Agent 会话 ──

const aiConversationColumns = `id, appid, admin_id, scene, ref, title, provider_config_id, model,
	input_tokens, output_tokens, compact_summary, compacted_before, compactions, created_at, updated_at`

func (r *Repository) CreateAIConversation(ctx context.Context, item aidomain.Conversation) (*aidomain.Conversation, error) {
	return scanAIConversation(r.pool.QueryRow(ctx, `INSERT INTO ai_conversations
	(appid, admin_id, scene, ref, title, provider_config_id, model, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
RETURNING `+aiConversationColumns,
		item.AppID, item.AdminID, item.Scene, item.Ref, item.Title, item.ProviderConfigID, item.Model))
}

func (r *Repository) GetAIConversation(ctx context.Context, appID int64, id int64) (*aidomain.Conversation, error) {
	return scanAIConversation(r.pool.QueryRow(ctx,
		`SELECT `+aiConversationColumns+` FROM ai_conversations WHERE appid = $1 AND id = $2 LIMIT 1`, appID, id))
}

func (r *Repository) ListAIConversations(ctx context.Context, query aidomain.ConversationQuery) ([]aidomain.Conversation, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `SELECT `+aiConversationColumns+` FROM ai_conversations
WHERE appid = $1 AND admin_id = $2 AND scene = $3 AND ref = $4
ORDER BY updated_at DESC LIMIT `+fmt.Sprint(limit),
		query.AppID, query.AdminID, query.Scene, query.Ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.Conversation, 0, limit)
	for rows.Next() {
		item, err := scanAIConversation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// TouchAIConversation 记录一轮对话后的状态：标题（首轮生成）、通道、型号与累计用量。
func (r *Repository) TouchAIConversation(ctx context.Context, id int64, title string,
	providerConfigID int64, model string, inputTokens, outputTokens int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE ai_conversations SET
	title = CASE WHEN title = '' AND $2 <> '' THEN $2 ELSE title END,
	provider_config_id = CASE WHEN $3 > 0 THEN $3 ELSE provider_config_id END,
	model = CASE WHEN $4 <> '' THEN $4 ELSE model END,
	input_tokens = input_tokens + $5,
	output_tokens = output_tokens + $6,
	updated_at = NOW()
WHERE id = $1`, id, title, providerConfigID, model, inputTokens, outputTokens)
	return err
}

// CompactAIConversation 记录一次压缩：滚动摘要 + 水位线。
func (r *Repository) CompactAIConversation(ctx context.Context, id int64, summary string, beforeMessageID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE ai_conversations SET
	compact_summary = $2, compacted_before = $3, compactions = compactions + 1, updated_at = NOW()
WHERE id = $1`, id, summary, beforeMessageID)
	return err
}

func (r *Repository) DeleteAIConversation(ctx context.Context, appID int64, id int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM ai_conversations WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func scanAIConversation(row interface{ Scan(dest ...any) error }) (*aidomain.Conversation, error) {
	var item aidomain.Conversation
	if err := row.Scan(&item.ID, &item.AppID, &item.AdminID, &item.Scene, &item.Ref, &item.Title,
		&item.ProviderConfigID, &item.Model, &item.InputTokens, &item.OutputTokens,
		&item.CompactSummary, &item.CompactedBefore, &item.Compactions,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

// ── Agent 消息 ──

func (r *Repository) AppendAIMessage(ctx context.Context, item aidomain.AgentMessage) (*aidomain.AgentMessage, error) {
	var usageJSON any
	if item.Usage != nil {
		encoded, err := json.Marshal(item.Usage)
		if err != nil {
			return nil, err
		}
		usageJSON = encoded
	}
	parts := item.Parts
	if len(parts) == 0 {
		parts = json.RawMessage("[]")
	}
	var saved aidomain.AgentMessage
	var usageBytes []byte
	err := r.pool.QueryRow(ctx, `INSERT INTO ai_messages (conversation_id, role, parts, usage, created_at)
VALUES ($1, $2, $3, $4, NOW())
RETURNING id, conversation_id, role, parts, usage, created_at`,
		item.ConversationID, item.Role, []byte(parts), usageJSON).
		Scan(&saved.ID, &saved.ConversationID, &saved.Role, &saved.Parts, &usageBytes, &saved.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(usageBytes) > 0 {
		var usage aidomain.Usage
		if json.Unmarshal(usageBytes, &usage) == nil {
			saved.Usage = &usage
		}
	}
	return &saved, nil
}

// ListAIMessages 取会话消息。afterID 为压缩水位线：0 取全量（界面回放），
// 非 0 只取水位线之后的（喂模型）。
func (r *Repository) ListAIMessages(ctx context.Context, conversationID int64, afterID int64) ([]aidomain.AgentMessage, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, conversation_id, role, parts, usage, created_at
FROM ai_messages WHERE conversation_id = $1 AND id > $2 ORDER BY id ASC`, conversationID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]aidomain.AgentMessage, 0, 16)
	for rows.Next() {
		var item aidomain.AgentMessage
		var usageBytes []byte
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role, &item.Parts, &usageBytes, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(usageBytes) > 0 {
			var usage aidomain.Usage
			if json.Unmarshal(usageBytes, &usage) == nil {
				item.Usage = &usage
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
