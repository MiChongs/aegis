package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ticketdomain "aegis/internal/domain/ticket"

	"github.com/jackc/pgx/v5"
)

// 工单数据访问层。
//
// 可见范围（Scope）在 SQL 层收敛，而不是查完再过滤：
//   - Scope.All            → 不加范围条件
//   - Scope.PersonalOnly   → 只保留「与我有关」（受理人 / 提单人 / 关注人 / 我所在组）
//   - 否则                 → appid IN (...) OR「与我有关」
//
// 这样即使某管理员只是某个处理组成员、没有任何 ticket:read 权限点，
// 也能查到指派给自己或本组的工单，而绝不会看到其它工单。

const ticketColumns = `t.id, t.ticket_no, t.appid,
	t.requester_type, t.requester_user_id, t.requester_admin_id, t.requester_name, t.requester_contact,
	t.category_id, t.title, t.status, t.priority, t.source,
	t.assignee_admin_id, t.group_id,
	t.sla_policy_id, t.first_response_due_at, t.resolve_due_at, t.first_responded_at,
	t.resolved_at, t.closed_at, t.sla_state,
	t.message_count, t.last_message_at, t.last_message_role, t.reopened_count,
	t.rating, t.rating_comment, t.rated_at,
	t.tags, COALESCE(t.metadata, '{}'::jsonb), t.locked,
	t.created_by_admin_id, t.created_at, t.updated_at,
	COALESCE(c.name, ''), COALESCE(g.name, ''), COALESCE(a.display_name, a.account, ''), COALESCE(ap.name, '')`

const ticketJoins = `FROM tickets t
	LEFT JOIN ticket_categories c ON c.id = t.category_id
	LEFT JOIN ticket_groups g ON g.id = t.group_id
	LEFT JOIN admin_accounts a ON a.id = t.assignee_admin_id
	LEFT JOIN apps ap ON ap.id = t.appid`

func scanTicket(row interface{ Scan(dest ...any) error }) (*ticketdomain.Ticket, error) {
	item := &ticketdomain.Ticket{}
	var (
		metadataRaw []byte
		tags        []string
	)
	err := row.Scan(
		&item.ID, &item.TicketNo, &item.AppID,
		&item.RequesterType, &item.RequesterUserID, &item.RequesterAdminID, &item.RequesterName, &item.RequesterContact,
		&item.CategoryID, &item.Title, &item.Status, &item.Priority, &item.Source,
		&item.AssigneeAdminID, &item.GroupID,
		&item.SLAPolicyID, &item.FirstResponseDueAt, &item.ResolveDueAt, &item.FirstRespondedAt,
		&item.ResolvedAt, &item.ClosedAt, &item.SLAState,
		&item.MessageCount, &item.LastMessageAt, &item.LastMessageRole, &item.ReopenedCount,
		&item.Rating, &item.RatingComment, &item.RatedAt,
		&tags, &metadataRaw, &item.Locked,
		&item.CreatedByAdminID, &item.CreatedAt, &item.UpdatedAt,
		&item.CategoryName, &item.GroupName, &item.AssigneeName, &item.AppName,
	)
	if err != nil {
		return nil, err
	}
	item.Tags = tags
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &item.Metadata)
	}
	return item, nil
}

// ticketScopeClause 生成范围条件；返回 "" 表示不限。
func ticketScopeClause(scope ticketdomain.Scope, args *[]any) string {
	if scope.All {
		return ""
	}
	personal := make([]string, 0, 4)
	*args = append(*args, scope.AdminID)
	adminPos := len(*args)
	personal = append(personal,
		fmt.Sprintf("t.assignee_admin_id = $%d", adminPos),
		fmt.Sprintf("t.requester_admin_id = $%d", adminPos),
		fmt.Sprintf("t.created_by_admin_id = $%d", adminPos),
		fmt.Sprintf("EXISTS (SELECT 1 FROM ticket_watchers w WHERE w.ticket_id = t.id AND w.admin_id = $%d)", adminPos),
	)
	if len(scope.GroupIDs) > 0 {
		*args = append(*args, scope.GroupIDs)
		personal = append(personal, fmt.Sprintf("t.group_id = ANY($%d)", len(*args)))
	}
	personalClause := "(" + strings.Join(personal, " OR ") + ")"

	if scope.PersonalOnly || len(scope.AppIDs) == 0 {
		return personalClause
	}
	*args = append(*args, scope.AppIDs)
	return fmt.Sprintf("(t.appid = ANY($%d) OR %s)", len(*args), personalClause)
}

// ticketFilterClauses 把查询条件翻译成 WHERE 片段。
func ticketFilterClauses(query ticketdomain.ListQuery, scope ticketdomain.Scope, args *[]any) []string {
	clauses := make([]string, 0, 12)
	if c := ticketScopeClause(scope, args); c != "" {
		clauses = append(clauses, c)
	}
	if query.AppID != nil {
		*args = append(*args, *query.AppID)
		clauses = append(clauses, fmt.Sprintf("t.appid = $%d", len(*args)))
	}
	if len(query.Statuses) > 0 {
		*args = append(*args, query.Statuses)
		clauses = append(clauses, fmt.Sprintf("t.status = ANY($%d)", len(*args)))
	} else if !query.IncludeClosed {
		*args = append(*args, []string{ticketdomain.StatusClosed, ticketdomain.StatusCancelled})
		clauses = append(clauses, fmt.Sprintf("t.status <> ALL($%d)", len(*args)))
	}
	if len(query.Priorities) > 0 {
		*args = append(*args, query.Priorities)
		clauses = append(clauses, fmt.Sprintf("t.priority = ANY($%d)", len(*args)))
	}
	if query.CategoryID != nil {
		*args = append(*args, *query.CategoryID)
		clauses = append(clauses, fmt.Sprintf("t.category_id = $%d", len(*args)))
	}
	if query.GroupID != nil {
		*args = append(*args, *query.GroupID)
		clauses = append(clauses, fmt.Sprintf("t.group_id = $%d", len(*args)))
	}
	if query.Unassigned {
		clauses = append(clauses, "t.assignee_admin_id IS NULL")
	} else if query.AssigneeID != nil {
		*args = append(*args, *query.AssigneeID)
		clauses = append(clauses, fmt.Sprintf("t.assignee_admin_id = $%d", len(*args)))
	}
	if query.RequesterID != nil {
		*args = append(*args, *query.RequesterID)
		clauses = append(clauses, fmt.Sprintf("(t.requester_user_id = $%d OR t.requester_admin_id = $%d)", len(*args), len(*args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		*args = append(*args, "%"+keyword+"%")
		pos := len(*args)
		clauses = append(clauses, fmt.Sprintf(
			"(t.title ILIKE $%d OR t.ticket_no ILIKE $%d OR t.requester_name ILIKE $%d "+
				"OR EXISTS (SELECT 1 FROM ticket_messages m WHERE m.ticket_id = t.id AND m.content ILIKE $%d))",
			pos, pos, pos, pos))
	}
	if len(query.Tags) > 0 {
		*args = append(*args, query.Tags)
		clauses = append(clauses, fmt.Sprintf("t.tags && $%d", len(*args)))
	}
	if state := strings.TrimSpace(query.SLAState); state != "" {
		*args = append(*args, state)
		clauses = append(clauses, fmt.Sprintf("t.sla_state = $%d", len(*args)))
	}
	if query.OverdueOnly {
		clauses = append(clauses, `((t.first_responded_at IS NULL AND t.first_response_due_at IS NOT NULL AND t.first_response_due_at < NOW())
			OR (t.resolved_at IS NULL AND t.resolve_due_at IS NOT NULL AND t.resolve_due_at < NOW()))`)
	}
	if query.CreatedFrom != nil {
		*args = append(*args, *query.CreatedFrom)
		clauses = append(clauses, fmt.Sprintf("t.created_at >= $%d", len(*args)))
	}
	if query.CreatedTo != nil {
		*args = append(*args, *query.CreatedTo)
		clauses = append(clauses, fmt.Sprintf("t.created_at <= $%d", len(*args)))
	}
	if query.Rated != nil {
		if *query.Rated {
			clauses = append(clauses, "t.rating IS NOT NULL")
		} else {
			clauses = append(clauses, "t.rating IS NULL")
		}
	}
	return clauses
}

func ticketOrderBy(query ticketdomain.ListQuery) string {
	dir := "DESC"
	if strings.EqualFold(strings.TrimSpace(query.SortDir), "asc") {
		dir = "ASC"
	}
	switch strings.TrimSpace(query.SortBy) {
	case "created":
		return "ORDER BY t.created_at " + dir + ", t.id " + dir
	case "priority":
		// 紧急在前；同级按更新时间
		return `ORDER BY CASE t.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'normal' THEN 2 ELSE 1 END ` + dir + `, t.updated_at DESC`
	case "due":
		return "ORDER BY COALESCE(t.first_response_due_at, t.resolve_due_at) " + dir + " NULLS LAST, t.id DESC"
	default:
		return "ORDER BY t.updated_at " + dir + ", t.id " + dir
	}
}

// ListTickets 分页查询工单（含总数）。
func (r *Repository) ListTickets(ctx context.Context, query ticketdomain.ListQuery, scope ticketdomain.Scope) ([]ticketdomain.Ticket, int64, error) {
	args := make([]any, 0, 16)
	clauses := ticketFilterClauses(query, scope, &args)
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int64
	countSQL := "SELECT COUNT(*) " + ticketJoins + where
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []ticketdomain.Ticket{}, 0, nil
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	args = append(args, limit, (page-1)*limit)
	listSQL := "SELECT " + ticketColumns + " " + ticketJoins + where + " " + ticketOrderBy(query) +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ticketdomain.Ticket, 0, limit)
	for rows.Next() {
		item, err := scanTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// GetTicketByID 单条查询（不做范围校验，由服务层判定）。
func (r *Repository) GetTicketByID(ctx context.Context, id int64) (*ticketdomain.Ticket, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+ticketColumns+" "+ticketJoins+" WHERE t.id = $1", id)
	item, err := scanTicket(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// GetTicketByNo 按工单号查询。
func (r *Repository) GetTicketByNo(ctx context.Context, ticketNo string) (*ticketdomain.Ticket, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+ticketColumns+" "+ticketJoins+" WHERE t.ticket_no = $1", strings.TrimSpace(ticketNo))
	item, err := scanTicket(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// CreateTicket 建单：工单 + 首条消息 + created 事件 + 附件归属，同一事务完成。
func (r *Repository) CreateTicket(ctx context.Context, cmd ticketdomain.CreateCommand, ticketNo string,
	firstResponseDue *time.Time, resolveDue *time.Time, slaPolicyID *int64) (*ticketdomain.Ticket, error) {

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	metadataJSON, _ := json.Marshal(orEmptyMap(cmd.Metadata))
	tags := cmd.Tags
	if tags == nil {
		tags = []string{}
	}

	var ticketID int64
	insertSQL := `INSERT INTO tickets (
		ticket_no, appid, requester_type, requester_user_id, requester_admin_id, requester_name, requester_contact,
		category_id, title, status, priority, source, assignee_admin_id, group_id,
		sla_policy_id, first_response_due_at, resolve_due_at,
		message_count, last_message_at, last_message_role,
		tags, metadata, created_by_admin_id, created_at, updated_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,1,NOW(),'requester',$18,$19,$20,NOW(),NOW())
	RETURNING id`
	if err := tx.QueryRow(ctx, insertSQL,
		ticketNo, cmd.AppID, cmd.RequesterType, cmd.RequesterUserID, cmd.RequesterAdminID, cmd.RequesterName, cmd.RequesterContact,
		cmd.CategoryID, cmd.Title, ticketdomain.StatusOpen, cmd.Priority, cmd.Source, cmd.AssigneeAdminID, cmd.GroupID,
		slaPolicyID, firstResponseDue, resolveDue,
		tags, metadataJSON, cmd.CreatedByAdminID,
	).Scan(&ticketID); err != nil {
		return nil, err
	}

	contentType := strings.TrimSpace(cmd.ContentType)
	if contentType == "" {
		contentType = "text"
	}
	var messageID int64
	msgSQL := `INSERT INTO ticket_messages (ticket_id, author_type, author_user_id, author_admin_id, author_name, internal, content, content_type)
		VALUES ($1, 'requester', $2, $3, $4, FALSE, $5, $6) RETURNING id`
	if err := tx.QueryRow(ctx, msgSQL, ticketID, cmd.RequesterUserID, cmd.RequesterAdminID, cmd.RequesterName, cmd.Content, contentType).Scan(&messageID); err != nil {
		return nil, err
	}

	if len(cmd.AttachmentIDs) > 0 {
		// 只回填「尚未归属」的附件，避免把别人工单的附件挂过来
		if _, err := tx.Exec(ctx,
			`UPDATE ticket_attachments SET ticket_id = $1, message_id = $2 WHERE id = ANY($3) AND ticket_id IS NULL`,
			ticketID, messageID, cmd.AttachmentIDs); err != nil {
			return nil, err
		}
	}

	actorType, actorID := requesterActor(cmd)
	if _, err := tx.Exec(ctx,
		`INSERT INTO ticket_events (ticket_id, event, actor_type, actor_id, actor_name, to_value, summary)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ticketID, ticketdomain.EventCreated, actorType, actorID, cmd.RequesterName, ticketdomain.StatusOpen,
		"提交工单："+cmd.Title); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return r.GetTicketByID(ctx, ticketID)
}

func requesterActor(cmd ticketdomain.CreateCommand) (string, *int64) {
	if cmd.RequesterType == ticketdomain.RequesterAdmin {
		return "admin", cmd.RequesterAdminID
	}
	return "user", cmd.RequesterUserID
}

func orEmptyMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

// UpdateTicketFields 局部更新工单字段。传入 nil 的字段保持不变。
func (r *Repository) UpdateTicketFields(ctx context.Context, id int64, cmd ticketdomain.UpdateCommand) error {
	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)
	if cmd.Title != nil {
		args = append(args, strings.TrimSpace(*cmd.Title))
		sets = append(sets, fmt.Sprintf("title = $%d", len(args)))
	}
	if cmd.CategoryID != nil {
		if *cmd.CategoryID <= 0 {
			sets = append(sets, "category_id = NULL")
		} else {
			args = append(args, *cmd.CategoryID)
			sets = append(sets, fmt.Sprintf("category_id = $%d", len(args)))
		}
	}
	if cmd.Priority != nil {
		args = append(args, *cmd.Priority)
		sets = append(sets, fmt.Sprintf("priority = $%d", len(args)))
	}
	if cmd.Tags != nil {
		tags := *cmd.Tags
		if tags == nil {
			tags = []string{}
		}
		args = append(args, tags)
		sets = append(sets, fmt.Sprintf("tags = $%d", len(args)))
	}
	if cmd.Locked != nil {
		args = append(args, *cmd.Locked)
		sets = append(sets, fmt.Sprintf("locked = $%d", len(args)))
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	sql := "UPDATE tickets SET " + strings.Join(sets, ", ") + fmt.Sprintf(" WHERE id = $%d", len(args))
	_, err := r.pool.Exec(ctx, sql, args...)
	return err
}

// AssignTicket 更新受理人 / 处理组。首次指派时若仍为 open 则推进到 processing。
//
// 参数必须显式加类型标注：`$2` 同时出现在赋值位（可推出 bigint）与 `$2 IS NOT NULL`
// （不提供任何类型信息）两处，Postgres 会直接报 42P08「无法确定参数类型」。
func (r *Repository) AssignTicket(ctx context.Context, id int64, assigneeID *int64, groupID *int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tickets
		SET assignee_admin_id = $2::bigint,
		    group_id = $3::bigint,
		    status = CASE WHEN status = 'open' AND $2::bigint IS NOT NULL THEN 'processing' ELSE status END,
		    updated_at = NOW()
		WHERE id = $1::bigint`, id, assigneeID, groupID)
	return err
}

// UpdateTicketStatus 状态流转，同时维护 resolved_at / closed_at / reopened_count。
//
// `$2::text` 的标注不可省：同一参数既赋值给 varchar 列（推导为 varchar），
// 又与裸字面量比较（推导为 text），两次推导冲突会让整条语句报 42P08。
func (r *Repository) UpdateTicketStatus(ctx context.Context, id int64, status string, reopen bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tickets
		SET status = $2::text,
		    resolved_at = CASE WHEN $2::text = 'resolved' THEN NOW() WHEN $3::boolean THEN NULL ELSE resolved_at END,
		    closed_at   = CASE WHEN $2::text IN ('closed','cancelled') THEN NOW() WHEN $3::boolean THEN NULL ELSE closed_at END,
		    reopened_count = reopened_count + CASE WHEN $3::boolean THEN 1 ELSE 0 END,
		    sla_state = CASE
		        WHEN $2::text IN ('resolved','closed') AND sla_state <> 'breached' THEN 'met'
		        WHEN $3::boolean THEN 'ontime'
		        ELSE sla_state END,
		    locked = CASE WHEN $2::text = 'closed' THEN TRUE WHEN $3::boolean THEN FALSE ELSE locked END,
		    updated_at = NOW()
		WHERE id = $1::bigint`, id, status, reopen)
	return err
}

// UpdateTicketSLAWindow 重开或改优先级后重算 SLA 时限。
func (r *Repository) UpdateTicketSLAWindow(ctx context.Context, id int64, policyID *int64, firstResponseDue, resolveDue *time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE tickets SET sla_policy_id = $2, first_response_due_at = $3, resolve_due_at = $4, updated_at = NOW() WHERE id = $1`,
		id, policyID, firstResponseDue, resolveDue)
	return err
}

// SetTicketSLAState 由 SLA 巡检器写入。
func (r *Repository) SetTicketSLAState(ctx context.Context, id int64, state string) error {
	_, err := r.pool.Exec(ctx, `UPDATE tickets SET sla_state = $2, updated_at = updated_at WHERE id = $1 AND sla_state <> $2`, id, state)
	return err
}

// SubmitTicketRating 提交满意度评价。仅在尚未评价时生效，返回是否写入。
func (r *Repository) SubmitTicketRating(ctx context.Context, id int64, rating int16, comment string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tickets SET rating = $2, rating_comment = $3, rated_at = NOW(), updated_at = NOW() WHERE id = $1 AND rating IS NULL`,
		id, rating, strings.TrimSpace(comment))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteTicket 物理删除（级联删除消息/事件/附件）。
func (r *Repository) DeleteTicket(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tickets WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────── 消息 ───────────────

const ticketMessageColumns = `id, ticket_id, author_type, author_user_id, author_admin_id, author_name,
	internal, content, content_type, COALESCE(metadata, '{}'::jsonb), edited_at, created_at`

func scanTicketMessage(row interface{ Scan(dest ...any) error }) (*ticketdomain.Message, error) {
	item := &ticketdomain.Message{}
	var metadataRaw []byte
	if err := row.Scan(
		&item.ID, &item.TicketID, &item.AuthorType, &item.AuthorUserID, &item.AuthorAdminID, &item.AuthorName,
		&item.Internal, &item.Content, &item.ContentType, &metadataRaw, &item.EditedAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &item.Metadata)
	}
	return item, nil
}

// ListTicketMessages 会话消息。includeInternal=false 时过滤内部备注。
func (r *Repository) ListTicketMessages(ctx context.Context, ticketID int64, includeInternal bool) ([]ticketdomain.Message, error) {
	sql := "SELECT " + ticketMessageColumns + " FROM ticket_messages WHERE ticket_id = $1"
	if !includeInternal {
		sql += " AND internal = FALSE"
	}
	sql += " ORDER BY id ASC"
	rows, err := r.pool.Query(ctx, sql, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Message, 0, 16)
	for rows.Next() {
		item, err := scanTicketMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// AddTicketMessageInput 追加消息的入参。
type AddTicketMessageInput struct {
	TicketID      int64
	AuthorType    string
	AuthorUserID  *int64
	AuthorAdminID *int64
	AuthorName    string
	Internal      bool
	Content       string
	ContentType   string
	Metadata      map[string]any
	AttachmentIDs []int64
	// NextStatus 非空时同事务切换状态
	NextStatus string
}

// AddTicketMessage 追加消息，同事务维护工单冗余字段：
// message_count / last_message_* / first_responded_at（首次对外 agent 回复）。
func (r *Repository) AddTicketMessage(ctx context.Context, input AddTicketMessageInput) (*ticketdomain.Message, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "text"
	}
	metadataJSON, _ := json.Marshal(orEmptyMap(input.Metadata))

	var messageID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO ticket_messages (ticket_id, author_type, author_user_id, author_admin_id, author_name, internal, content, content_type, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		input.TicketID, input.AuthorType, input.AuthorUserID, input.AuthorAdminID, input.AuthorName,
		input.Internal, input.Content, contentType, metadataJSON,
	).Scan(&messageID); err != nil {
		return nil, err
	}

	if len(input.AttachmentIDs) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE ticket_attachments SET ticket_id = $1, message_id = $2 WHERE id = ANY($3) AND (ticket_id IS NULL OR ticket_id = $1)`,
			input.TicketID, messageID, input.AttachmentIDs); err != nil {
			return nil, err
		}
	}

	// 内部备注不改变对外会话状态，也不计首响
	if !input.Internal {
		markFirstResponse := input.AuthorType == ticketdomain.AuthorAgent
		// 同 UpdateTicketStatus：$4 既与裸字面量比较又赋值给 varchar 列，
		// 不显式标注会触发 42P08（参数类型推导冲突）。
		if _, err := tx.Exec(ctx, `
			UPDATE tickets
			SET message_count = message_count + 1,
			    last_message_at = NOW(),
			    last_message_role = $2::text,
			    first_responded_at = CASE WHEN $3::boolean AND first_responded_at IS NULL THEN NOW() ELSE first_responded_at END,
			    status = CASE
			        WHEN $4::text <> '' THEN $4::text
			        WHEN status = 'open' AND $3::boolean THEN 'processing'
			        WHEN status = 'resolved' AND NOT $3::boolean THEN 'processing'
			        ELSE status END,
			    updated_at = NOW()
			WHERE id = $1::bigint`,
			input.TicketID, input.AuthorType, markFirstResponse, strings.TrimSpace(input.NextStatus)); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE tickets SET updated_at = NOW() WHERE id = $1`, input.TicketID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	row := r.pool.QueryRow(ctx, "SELECT "+ticketMessageColumns+" FROM ticket_messages WHERE id = $1", messageID)
	return scanTicketMessage(row)
}

// ─────────────── 附件 ───────────────

const ticketAttachmentColumns = `id, ticket_id, message_id, file_name, content_type, size_bytes, storage_ref,
	uploaded_by_type, uploaded_by_id, created_at`

func scanTicketAttachment(row interface{ Scan(dest ...any) error }) (*ticketdomain.Attachment, error) {
	item := &ticketdomain.Attachment{}
	if err := row.Scan(&item.ID, &item.TicketID, &item.MessageID, &item.FileName, &item.ContentType,
		&item.SizeBytes, &item.StorageRef, &item.UploadedByType, &item.UploadedByID, &item.CreatedAt); err != nil {
		return nil, err
	}
	return item, nil
}

// CreateTicketAttachment 落库一条附件。ticketID 传 0 表示"待关联"（提单表单先传附件再建单）。
func (r *Repository) CreateTicketAttachment(ctx context.Context, item ticketdomain.Attachment) (*ticketdomain.Attachment, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO ticket_attachments (ticket_id, message_id, file_name, content_type, size_bytes, storage_ref, uploaded_by_type, uploaded_by_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+ticketAttachmentColumns,
		item.TicketID, item.MessageID, item.FileName, item.ContentType, item.SizeBytes, item.StorageRef,
		item.UploadedByType, item.UploadedByID)
	return scanTicketAttachment(row)
}

// ListTicketAttachments 工单全部附件。
func (r *Repository) ListTicketAttachments(ctx context.Context, ticketID int64) ([]ticketdomain.Attachment, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+ticketAttachmentColumns+" FROM ticket_attachments WHERE ticket_id = $1 ORDER BY id ASC", ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Attachment, 0, 8)
	for rows.Next() {
		item, err := scanTicketAttachment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetTicketAttachment 单条附件（下载鉴权用）。
func (r *Repository) GetTicketAttachment(ctx context.Context, id int64) (*ticketdomain.Attachment, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+ticketAttachmentColumns+" FROM ticket_attachments WHERE id = $1", id)
	item, err := scanTicketAttachment(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// ─────────────── 时间线 ───────────────

// AddTicketEvent 追加时间线条目。
func (r *Repository) AddTicketEvent(ctx context.Context, item ticketdomain.Event) error {
	metadataJSON, _ := json.Marshal(orEmptyMap(item.Metadata))
	_, err := r.pool.Exec(ctx,
		`INSERT INTO ticket_events (ticket_id, event, actor_type, actor_id, actor_name, from_value, to_value, summary, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		item.TicketID, item.Event, item.ActorType, item.ActorID, item.ActorName,
		item.FromValue, item.ToValue, item.Summary, metadataJSON)
	return err
}

// ListTicketEvents 时间线。
func (r *Repository) ListTicketEvents(ctx context.Context, ticketID int64) ([]ticketdomain.Event, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, ticket_id, event, actor_type, actor_id, actor_name, from_value, to_value, summary, COALESCE(metadata, '{}'::jsonb), created_at
		 FROM ticket_events WHERE ticket_id = $1 ORDER BY id ASC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Event, 0, 16)
	for rows.Next() {
		var item ticketdomain.Event
		var metadataRaw []byte
		if err := rows.Scan(&item.ID, &item.TicketID, &item.Event, &item.ActorType, &item.ActorID, &item.ActorName,
			&item.FromValue, &item.ToValue, &item.Summary, &metadataRaw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadataRaw) > 0 {
			_ = json.Unmarshal(metadataRaw, &item.Metadata)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ─────────────── 关注人 ───────────────

// AddTicketWatcher 添加关注人（幂等）。
func (r *Repository) AddTicketWatcher(ctx context.Context, ticketID int64, adminID int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO ticket_watchers (ticket_id, admin_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, ticketID, adminID)
	return err
}

// RemoveTicketWatcher 取消关注。
func (r *Repository) RemoveTicketWatcher(ctx context.Context, ticketID int64, adminID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM ticket_watchers WHERE ticket_id = $1 AND admin_id = $2`, ticketID, adminID)
	return err
}

// ListTicketWatchers 关注人（带管理员账号信息）。
func (r *Repository) ListTicketWatchers(ctx context.Context, ticketID int64) ([]ticketdomain.Watcher, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.ticket_id, w.admin_id, a.account, COALESCE(a.display_name, ''), w.created_at
		FROM ticket_watchers w JOIN admin_accounts a ON a.id = w.admin_id
		WHERE w.ticket_id = $1 ORDER BY w.created_at ASC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Watcher, 0, 8)
	for rows.Next() {
		var item ticketdomain.Watcher
		if err := rows.Scan(&item.TicketID, &item.AdminID, &item.Account, &item.DisplayName, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListTicketNotifyTargets 汇总某工单需要通知的管理员：受理人 + 关注人 + 处理组成员。
func (r *Repository) ListTicketNotifyTargets(ctx context.Context, ticketID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT admin_id FROM (
			SELECT assignee_admin_id AS admin_id FROM tickets WHERE id = $1 AND assignee_admin_id IS NOT NULL
			UNION
			SELECT admin_id FROM ticket_watchers WHERE ticket_id = $1
			UNION
			SELECT m.admin_id FROM ticket_group_members m
			  JOIN tickets t ON t.group_id = m.group_id
			 WHERE t.id = $1
		) s WHERE admin_id IS NOT NULL`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, 8)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ─────────────── 分类 ───────────────

const ticketCategoryColumns = `id, appid, parent_id, key, name, description, default_priority,
	default_group_id, sla_policy_id, COALESCE(form_schema, '[]'::jsonb), user_submittable, sort, enabled, created_at, updated_at`

func scanTicketCategory(row interface{ Scan(dest ...any) error }) (*ticketdomain.Category, error) {
	item := &ticketdomain.Category{}
	var schemaRaw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.ParentID, &item.Key, &item.Name, &item.Description,
		&item.DefaultPriority, &item.DefaultGroupID, &item.SLAPolicyID, &schemaRaw,
		&item.UserSubmittable, &item.Sort, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.FormSchema = []ticketdomain.FormField{}
	if len(schemaRaw) > 0 {
		_ = json.Unmarshal(schemaRaw, &item.FormSchema)
	}
	return item, nil
}

// ListTicketCategories 列出分类。appID>0 时同时返回平台级（appid=0）分类。
func (r *Repository) ListTicketCategories(ctx context.Context, appID int64, onlyEnabled bool) ([]ticketdomain.Category, error) {
	sql := "SELECT " + ticketCategoryColumns + " FROM ticket_categories WHERE (appid = $1 OR appid = 0)"
	if onlyEnabled {
		sql += " AND enabled = TRUE"
	}
	sql += " ORDER BY appid ASC, sort ASC, id ASC"
	rows, err := r.pool.Query(ctx, sql, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Category, 0, 16)
	for rows.Next() {
		item, err := scanTicketCategory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetTicketCategory 单条。
func (r *Repository) GetTicketCategory(ctx context.Context, id int64) (*ticketdomain.Category, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+ticketCategoryColumns+" FROM ticket_categories WHERE id = $1", id)
	item, err := scanTicketCategory(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// UpsertTicketCategory 新建（ID=0）或更新。
func (r *Repository) UpsertTicketCategory(ctx context.Context, item ticketdomain.Category) (*ticketdomain.Category, error) {
	schemaJSON, _ := json.Marshal(orEmptyFields(item.FormSchema))
	if item.ID > 0 {
		row := r.pool.QueryRow(ctx, `
			UPDATE ticket_categories
			SET parent_id = $2, key = $3, name = $4, description = $5, default_priority = $6,
			    default_group_id = $7, sla_policy_id = $8, form_schema = $9,
			    user_submittable = $10, sort = $11, enabled = $12, updated_at = NOW()
			WHERE id = $1 RETURNING `+ticketCategoryColumns,
			item.ID, item.ParentID, item.Key, item.Name, item.Description, item.DefaultPriority,
			item.DefaultGroupID, item.SLAPolicyID, schemaJSON, item.UserSubmittable, item.Sort, item.Enabled)
		return scanTicketCategory(row)
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ticket_categories (appid, parent_id, key, name, description, default_priority,
			default_group_id, sla_policy_id, form_schema, user_submittable, sort, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+ticketCategoryColumns,
		item.AppID, item.ParentID, item.Key, item.Name, item.Description, item.DefaultPriority,
		item.DefaultGroupID, item.SLAPolicyID, schemaJSON, item.UserSubmittable, item.Sort, item.Enabled)
	return scanTicketCategory(row)
}

func orEmptyFields(in []ticketdomain.FormField) []ticketdomain.FormField {
	if in == nil {
		return []ticketdomain.FormField{}
	}
	return in
}

// DeleteTicketCategory 删除分类（工单的 category_id 置空）。
func (r *Repository) DeleteTicketCategory(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ticket_categories WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────── 处理组 ───────────────

// ListTicketGroups 处理组（含成员数与待办数）。
func (r *Repository) ListTicketGroups(ctx context.Context, appID int64, onlyEnabled bool) ([]ticketdomain.Group, error) {
	sql := `SELECT g.id, g.appid, g.key, g.name, g.description, g.assign_strategy, g.enabled, g.created_at, g.updated_at,
		(SELECT COUNT(*) FROM ticket_group_members m WHERE m.group_id = g.id),
		(SELECT COUNT(*) FROM tickets t WHERE t.group_id = g.id AND t.status NOT IN ('closed','cancelled'))
		FROM ticket_groups g WHERE (g.appid = $1 OR g.appid = 0)`
	if onlyEnabled {
		sql += " AND g.enabled = TRUE"
	}
	sql += " ORDER BY g.appid ASC, g.id ASC"
	rows, err := r.pool.Query(ctx, sql, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Group, 0, 8)
	for rows.Next() {
		var item ticketdomain.Group
		if err := rows.Scan(&item.ID, &item.AppID, &item.Key, &item.Name, &item.Description,
			&item.AssignStrategy, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
			&item.MemberCount, &item.OpenCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetTicketGroup 单条处理组。
func (r *Repository) GetTicketGroup(ctx context.Context, id int64) (*ticketdomain.Group, error) {
	var item ticketdomain.Group
	err := r.pool.QueryRow(ctx, `
		SELECT g.id, g.appid, g.key, g.name, g.description, g.assign_strategy, g.enabled, g.created_at, g.updated_at,
		(SELECT COUNT(*) FROM ticket_group_members m WHERE m.group_id = g.id),
		(SELECT COUNT(*) FROM tickets t WHERE t.group_id = g.id AND t.status NOT IN ('closed','cancelled'))
		FROM ticket_groups g WHERE g.id = $1`, id).Scan(
		&item.ID, &item.AppID, &item.Key, &item.Name, &item.Description,
		&item.AssignStrategy, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
		&item.MemberCount, &item.OpenCount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

// UpsertTicketGroup 新建或更新处理组。
func (r *Repository) UpsertTicketGroup(ctx context.Context, item ticketdomain.Group) (*ticketdomain.Group, error) {
	var id int64
	if item.ID > 0 {
		if err := r.pool.QueryRow(ctx, `
			UPDATE ticket_groups SET key = $2, name = $3, description = $4, assign_strategy = $5, enabled = $6, updated_at = NOW()
			WHERE id = $1 RETURNING id`,
			item.ID, item.Key, item.Name, item.Description, item.AssignStrategy, item.Enabled).Scan(&id); err != nil {
			return nil, err
		}
	} else {
		if err := r.pool.QueryRow(ctx, `
			INSERT INTO ticket_groups (appid, key, name, description, assign_strategy, enabled)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			item.AppID, item.Key, item.Name, item.Description, item.AssignStrategy, item.Enabled).Scan(&id); err != nil {
			return nil, err
		}
	}
	return r.GetTicketGroup(ctx, id)
}

// DeleteTicketGroup 删除处理组。
func (r *Repository) DeleteTicketGroup(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ticket_groups WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListTicketGroupMembers 组成员（带账号信息与当前待办量）。
func (r *Repository) ListTicketGroupMembers(ctx context.Context, groupID int64) ([]ticketdomain.GroupMember, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.group_id, m.admin_id, a.account, COALESCE(a.display_name, ''), COALESCE(a.avatar, ''), m.role, m.created_at,
		       (SELECT COUNT(*) FROM tickets t WHERE t.assignee_admin_id = m.admin_id AND t.status NOT IN ('closed','cancelled'))
		FROM ticket_group_members m JOIN admin_accounts a ON a.id = m.admin_id
		WHERE m.group_id = $1 ORDER BY m.created_at ASC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.GroupMember, 0, 8)
	for rows.Next() {
		var item ticketdomain.GroupMember
		if err := rows.Scan(&item.GroupID, &item.AdminID, &item.Account, &item.DisplayName, &item.Avatar,
			&item.Role, &item.CreatedAt, &item.OpenCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SetTicketGroupMembers 全量覆盖组成员。
func (r *Repository) SetTicketGroupMembers(ctx context.Context, groupID int64, members []ticketdomain.GroupMember) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `DELETE FROM ticket_group_members WHERE group_id = $1`, groupID); err != nil {
		return err
	}
	for _, m := range members {
		role := strings.TrimSpace(m.Role)
		if role != "leader" {
			role = "agent"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO ticket_group_members (group_id, admin_id, role) VALUES ($1,$2,$3) ON CONFLICT (group_id, admin_id) DO UPDATE SET role = EXCLUDED.role`,
			groupID, m.AdminID, role); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// ListTicketGroupIDsForAdmin 某管理员所属的处理组 ID（Scope 推导用）。
func (r *Repository) ListTicketGroupIDsForAdmin(ctx context.Context, adminID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT group_id FROM ticket_group_members WHERE admin_id = $1`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, 4)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// IsTicketGroupLeader 是否为组内负责人（负责人可处理组内全部工单）。
func (r *Repository) IsTicketGroupLeader(ctx context.Context, adminID int64, groupID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM ticket_group_members WHERE admin_id = $1 AND group_id = $2 AND role = 'leader')`,
		adminID, groupID).Scan(&exists)
	return exists, err
}

// PickTicketGroupAgent 按组策略挑选一名成员：
//   - round_robin：按游标轮询
//   - least_open：挑当前未结工单最少的人
//   - manual：不自动挑人
//
// 返回 nil 表示组为空或策略为 manual。
func (r *Repository) PickTicketGroupAgent(ctx context.Context, groupID int64) (*int64, error) {
	var strategy string
	var cursor int
	if err := r.pool.QueryRow(ctx, `SELECT assign_strategy, round_robin_cursor FROM ticket_groups WHERE id = $1`, groupID).
		Scan(&strategy, &cursor); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	switch strategy {
	case "least_open":
		var adminID int64
		err := r.pool.QueryRow(ctx, `
			SELECT m.admin_id FROM ticket_group_members m
			JOIN admin_accounts a ON a.id = m.admin_id AND a.status = 'active'
			WHERE m.group_id = $1
			ORDER BY (SELECT COUNT(*) FROM tickets t WHERE t.assignee_admin_id = m.admin_id AND t.status NOT IN ('closed','cancelled')) ASC,
			         m.admin_id ASC
			LIMIT 1`, groupID).Scan(&adminID)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}
		return &adminID, nil
	case "round_robin":
		var ids []int64
		rows, err := r.pool.Query(ctx, `
			SELECT m.admin_id FROM ticket_group_members m
			JOIN admin_accounts a ON a.id = m.admin_id AND a.status = 'active'
			WHERE m.group_id = $1 ORDER BY m.admin_id ASC`, groupID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		next := cursor % len(ids)
		_, _ = r.pool.Exec(ctx, `UPDATE ticket_groups SET round_robin_cursor = $2 WHERE id = $1`, groupID, (next+1)%len(ids))
		return &ids[next], nil
	default:
		return nil, nil
	}
}

// ─────────────── SLA 策略 ───────────────

const ticketSLAColumns = `id, appid, name, description, first_response_minutes, resolve_minutes, business_hours, warn_ratio, enabled, created_at, updated_at`

func scanTicketSLA(row interface{ Scan(dest ...any) error }) (*ticketdomain.SLAPolicy, error) {
	item := &ticketdomain.SLAPolicy{}
	var firstRaw, resolveRaw, hoursRaw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Description,
		&firstRaw, &resolveRaw, &hoursRaw, &item.WarnRatio, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.FirstResponseMinutes = map[string]int{}
	item.ResolveMinutes = map[string]int{}
	if len(firstRaw) > 0 {
		_ = json.Unmarshal(firstRaw, &item.FirstResponseMinutes)
	}
	if len(resolveRaw) > 0 {
		_ = json.Unmarshal(resolveRaw, &item.ResolveMinutes)
	}
	if len(hoursRaw) > 0 {
		var hours ticketdomain.BusinessHours
		if err := json.Unmarshal(hoursRaw, &hours); err == nil && strings.TrimSpace(hours.Start) != "" {
			item.BusinessHours = &hours
		}
	}
	return item, nil
}

// ListTicketSLAPolicies SLA 策略列表（含平台级）。
func (r *Repository) ListTicketSLAPolicies(ctx context.Context, appID int64) ([]ticketdomain.SLAPolicy, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+ticketSLAColumns+" FROM ticket_sla_policies WHERE (appid = $1 OR appid = 0) ORDER BY appid ASC, id ASC", appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.SLAPolicy, 0, 8)
	for rows.Next() {
		item, err := scanTicketSLA(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetTicketSLAPolicy 单条。
func (r *Repository) GetTicketSLAPolicy(ctx context.Context, id int64) (*ticketdomain.SLAPolicy, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+ticketSLAColumns+" FROM ticket_sla_policies WHERE id = $1", id)
	item, err := scanTicketSLA(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// UpsertTicketSLAPolicy 新建或更新。
func (r *Repository) UpsertTicketSLAPolicy(ctx context.Context, item ticketdomain.SLAPolicy) (*ticketdomain.SLAPolicy, error) {
	firstJSON, _ := json.Marshal(item.FirstResponseMinutes)
	resolveJSON, _ := json.Marshal(item.ResolveMinutes)
	var hoursJSON []byte
	if item.BusinessHours != nil {
		hoursJSON, _ = json.Marshal(item.BusinessHours)
	}
	if item.ID > 0 {
		row := r.pool.QueryRow(ctx, `
			UPDATE ticket_sla_policies SET name = $2, description = $3, first_response_minutes = $4,
			  resolve_minutes = $5, business_hours = $6, warn_ratio = $7, enabled = $8, updated_at = NOW()
			WHERE id = $1 RETURNING `+ticketSLAColumns,
			item.ID, item.Name, item.Description, firstJSON, resolveJSON, hoursJSON, item.WarnRatio, item.Enabled)
		return scanTicketSLA(row)
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ticket_sla_policies (appid, name, description, first_response_minutes, resolve_minutes, business_hours, warn_ratio, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+ticketSLAColumns,
		item.AppID, item.Name, item.Description, firstJSON, resolveJSON, hoursJSON, item.WarnRatio, item.Enabled)
	return scanTicketSLA(row)
}

// DeleteTicketSLAPolicy 删除策略。
func (r *Repository) DeleteTicketSLAPolicy(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ticket_sla_policies WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────── 快捷回复 ───────────────

const ticketQuickReplyColumns = `id, appid, title, content, category_id, owner_admin_id, usage_count, sort, enabled, created_at, updated_at`

// ListTicketQuickReplies 快捷回复：共享话术 + 自己的私人话术。
func (r *Repository) ListTicketQuickReplies(ctx context.Context, appID int64, adminID int64) ([]ticketdomain.QuickReply, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+ticketQuickReplyColumns+` FROM ticket_quick_replies
		 WHERE (appid = $1 OR appid = 0) AND enabled = TRUE AND (owner_admin_id IS NULL OR owner_admin_id = $2)
		 ORDER BY sort ASC, id ASC`, appID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.QuickReply, 0, 16)
	for rows.Next() {
		var item ticketdomain.QuickReply
		if err := rows.Scan(&item.ID, &item.AppID, &item.Title, &item.Content, &item.CategoryID,
			&item.OwnerAdminID, &item.UsageCount, &item.Sort, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpsertTicketQuickReply 新建或更新快捷回复。
func (r *Repository) UpsertTicketQuickReply(ctx context.Context, item ticketdomain.QuickReply) (*ticketdomain.QuickReply, error) {
	var row pgx.Row
	if item.ID > 0 {
		row = r.pool.QueryRow(ctx, `
			UPDATE ticket_quick_replies SET title = $2, content = $3, category_id = $4, owner_admin_id = $5,
			  sort = $6, enabled = $7, updated_at = NOW() WHERE id = $1 RETURNING `+ticketQuickReplyColumns,
			item.ID, item.Title, item.Content, item.CategoryID, item.OwnerAdminID, item.Sort, item.Enabled)
	} else {
		row = r.pool.QueryRow(ctx, `
			INSERT INTO ticket_quick_replies (appid, title, content, category_id, owner_admin_id, sort, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+ticketQuickReplyColumns,
			item.AppID, item.Title, item.Content, item.CategoryID, item.OwnerAdminID, item.Sort, item.Enabled)
	}
	var out ticketdomain.QuickReply
	if err := row.Scan(&out.ID, &out.AppID, &out.Title, &out.Content, &out.CategoryID,
		&out.OwnerAdminID, &out.UsageCount, &out.Sort, &out.Enabled, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTicketQuickReply 删除快捷回复。
func (r *Repository) DeleteTicketQuickReply(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ticket_quick_replies WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// IncrTicketQuickReplyUsage 使用计数 +1（用于排序热度）。
func (r *Repository) IncrTicketQuickReplyUsage(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE ticket_quick_replies SET usage_count = usage_count + 1 WHERE id = $1`, id)
	return err
}

// ─────────────── 统计 ───────────────

// TicketStats 概览统计（受 Scope 约束）。
func (r *Repository) TicketStats(ctx context.Context, appID *int64, scope ticketdomain.Scope) (*ticketdomain.Stats, error) {
	args := make([]any, 0, 6)
	clauses := make([]string, 0, 3)
	if c := ticketScopeClause(scope, &args); c != "" {
		clauses = append(clauses, c)
	}
	if appID != nil {
		args = append(args, *appID)
		clauses = append(clauses, fmt.Sprintf("t.appid = $%d", len(args)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, scope.AdminID)
	minePos := len(args)

	stats := &ticketdomain.Stats{ByPriority: map[string]int64{}}
	sql := `SELECT
		COUNT(*),
		COUNT(*) FILTER (WHERE t.status = 'open'),
		COUNT(*) FILTER (WHERE t.status = 'processing'),
		COUNT(*) FILTER (WHERE t.status IN ('pending_user','pending_third_party')),
		COUNT(*) FILTER (WHERE t.status = 'resolved'),
		COUNT(*) FILTER (WHERE t.status IN ('closed','cancelled')),
		COUNT(*) FILTER (WHERE t.assignee_admin_id IS NULL AND t.status NOT IN ('closed','cancelled')),
		COUNT(*) FILTER (WHERE t.assignee_admin_id = $` + fmt.Sprint(minePos) + ` AND t.status NOT IN ('closed','cancelled')),
		COUNT(*) FILTER (WHERE t.first_responded_at IS NULL AND t.first_response_due_at IS NOT NULL AND t.first_response_due_at < NOW() AND t.status NOT IN ('closed','cancelled')),
		COUNT(*) FILTER (WHERE t.resolved_at IS NULL AND t.resolve_due_at IS NOT NULL AND t.resolve_due_at < NOW() AND t.status NOT IN ('closed','cancelled')),
		COUNT(*) FILTER (WHERE t.created_at >= date_trunc('day', NOW())),
		COUNT(*) FILTER (WHERE t.resolved_at >= date_trunc('day', NOW())),
		COUNT(*) FILTER (WHERE t.priority = 'low'),
		COUNT(*) FILTER (WHERE t.priority = 'normal'),
		COUNT(*) FILTER (WHERE t.priority = 'high'),
		COUNT(*) FILTER (WHERE t.priority = 'urgent'),
		COALESCE(AVG(EXTRACT(EPOCH FROM (t.first_responded_at - t.created_at)) * 1000) FILTER (WHERE t.first_responded_at IS NOT NULL), 0),
		COALESCE(AVG(EXTRACT(EPOCH FROM (t.resolved_at - t.created_at)) * 1000) FILTER (WHERE t.resolved_at IS NOT NULL), 0),
		COALESCE(AVG(t.rating) FILTER (WHERE t.rating IS NOT NULL), 0),
		COUNT(*) FILTER (WHERE t.rating IS NOT NULL)
	FROM tickets t` + where

	var low, normal, high, urgent int64
	var avgFirst, avgResolve, avgRating float64
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(
		&stats.Total, &stats.Open, &stats.Processing, &stats.PendingUser, &stats.Resolved, &stats.Closed,
		&stats.Unassigned, &stats.MineAssigned, &stats.OverdueFirst, &stats.OverdueResolve,
		&stats.CreatedToday, &stats.ResolvedToday,
		&low, &normal, &high, &urgent,
		&avgFirst, &avgResolve, &avgRating, &stats.RatingCount,
	); err != nil {
		return nil, err
	}
	stats.ByPriority["low"] = low
	stats.ByPriority["normal"] = normal
	stats.ByPriority["high"] = high
	stats.ByPriority["urgent"] = urgent
	stats.AvgFirstRespMs = int64(avgFirst)
	stats.AvgResolveMs = int64(avgResolve)
	stats.AvgRating = avgRating

	// 分类分布单独查
	catArgs := make([]any, 0, 4)
	catClauses := make([]string, 0, 2)
	if c := ticketScopeClause(scope, &catArgs); c != "" {
		catClauses = append(catClauses, c)
	}
	if appID != nil {
		catArgs = append(catArgs, *appID)
		catClauses = append(catClauses, fmt.Sprintf("t.appid = $%d", len(catArgs)))
	}
	catWhere := ""
	if len(catClauses) > 0 {
		catWhere = " WHERE " + strings.Join(catClauses, " AND ")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT t.category_id, COALESCE(c.name, '未分类'), COUNT(*)
		FROM tickets t LEFT JOIN ticket_categories c ON c.id = t.category_id`+catWhere+`
		GROUP BY t.category_id, c.name ORDER BY COUNT(*) DESC LIMIT 20`, catArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats.ByCategory = make([]ticketdomain.CategoryStat, 0, 8)
	for rows.Next() {
		var item ticketdomain.CategoryStat
		if err := rows.Scan(&item.CategoryID, &item.CategoryName, &item.Count); err != nil {
			return nil, err
		}
		stats.ByCategory = append(stats.ByCategory, item)
	}
	return stats, rows.Err()
}

// TicketTrend 近 N 天创建 / 解决 / 关闭趋势。
func (r *Repository) TicketTrend(ctx context.Context, appID *int64, scope ticketdomain.Scope, days int) ([]ticketdomain.TrendPoint, error) {
	if days <= 0 || days > 180 {
		days = 30
	}
	args := make([]any, 0, 4)
	clauses := []string{fmt.Sprintf("t.created_at >= NOW() - INTERVAL '%d days'", days)}
	if c := ticketScopeClause(scope, &args); c != "" {
		clauses = append(clauses, c)
	}
	if appID != nil {
		args = append(args, *appID)
		clauses = append(clauses, fmt.Sprintf("t.appid = $%d", len(args)))
	}
	sql := `SELECT to_char(date_trunc('day', t.created_at), 'YYYY-MM-DD') AS d,
		COUNT(*),
		COUNT(*) FILTER (WHERE t.resolved_at IS NOT NULL),
		COUNT(*) FILTER (WHERE t.closed_at IS NOT NULL)
		FROM tickets t WHERE ` + strings.Join(clauses, " AND ") + `
		GROUP BY d ORDER BY d ASC`
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.TrendPoint, 0, days)
	for rows.Next() {
		var item ticketdomain.TrendPoint
		if err := rows.Scan(&item.Date, &item.Created, &item.Resolved, &item.Closed); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// TicketAgentStats 处理人绩效榜。
func (r *Repository) TicketAgentStats(ctx context.Context, appID *int64, scope ticketdomain.Scope, limit int) ([]ticketdomain.AgentStat, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := make([]any, 0, 4)
	clauses := []string{"t.assignee_admin_id IS NOT NULL"}
	if c := ticketScopeClause(scope, &args); c != "" {
		clauses = append(clauses, c)
	}
	if appID != nil {
		args = append(args, *appID)
		clauses = append(clauses, fmt.Sprintf("t.appid = $%d", len(args)))
	}
	args = append(args, limit)
	sql := `SELECT a.id, a.account, COALESCE(a.display_name, ''),
		COUNT(*),
		COUNT(*) FILTER (WHERE t.resolved_at IS NOT NULL),
		COUNT(*) FILTER (WHERE t.status NOT IN ('closed','cancelled')),
		COALESCE(AVG(EXTRACT(EPOCH FROM (t.first_responded_at - t.created_at)) * 1000) FILTER (WHERE t.first_responded_at IS NOT NULL), 0),
		COALESCE(AVG(EXTRACT(EPOCH FROM (t.resolved_at - t.created_at)) * 1000) FILTER (WHERE t.resolved_at IS NOT NULL), 0),
		COALESCE(AVG(t.rating) FILTER (WHERE t.rating IS NOT NULL), 0),
		COUNT(*) FILTER (WHERE t.sla_state = 'breached')
		FROM tickets t JOIN admin_accounts a ON a.id = t.assignee_admin_id
		WHERE ` + strings.Join(clauses, " AND ") + `
		GROUP BY a.id, a.account, a.display_name
		ORDER BY COUNT(*) DESC
		LIMIT $` + fmt.Sprint(len(args))
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.AgentStat, 0, limit)
	for rows.Next() {
		var item ticketdomain.AgentStat
		var avgFirst, avgResolve, avgRating float64
		if err := rows.Scan(&item.AdminID, &item.Account, &item.DisplayName,
			&item.Assigned, &item.Resolved, &item.Open, &avgFirst, &avgResolve, &avgRating, &item.Breached); err != nil {
			return nil, err
		}
		item.AvgFirstRespMs = int64(avgFirst)
		item.AvgResolveMs = int64(avgResolve)
		item.AvgRating = avgRating
		items = append(items, item)
	}
	return items, rows.Err()
}

// ─────────────── SLA 巡检 ───────────────

// SLAScanRow SLA 巡检返回的最小工单信息。
type SLAScanRow struct {
	ID                 int64
	TicketNo           string
	AppID              int64
	Title              string
	Priority           string
	Status             string
	SLAState           string
	AssigneeAdminID    *int64
	GroupID            *int64
	CategoryID         *int64
	FirstResponseDueAt *time.Time
	ResolveDueAt       *time.Time
	FirstRespondedAt   *time.Time
	WarnRatio          float64
	CreatedAt          time.Time
}

// ScanTicketsForSLA 拉取未终结且设置了时限的工单，供 SLA 巡检器判定预警/超时。
func (r *Repository) ScanTicketsForSLA(ctx context.Context, limit int) ([]SLAScanRow, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.ticket_no, t.appid, t.title, t.priority, t.status, t.sla_state,
		       t.assignee_admin_id, t.group_id, t.category_id,
		       t.first_response_due_at, t.resolve_due_at, t.first_responded_at,
		       COALESCE(p.warn_ratio, 0.8), t.created_at
		FROM tickets t LEFT JOIN ticket_sla_policies p ON p.id = t.sla_policy_id
		WHERE t.status NOT IN ('closed','cancelled','resolved')
		  AND (t.first_response_due_at IS NOT NULL OR t.resolve_due_at IS NOT NULL)
		  AND t.sla_state <> 'breached'
		ORDER BY COALESCE(t.first_response_due_at, t.resolve_due_at) ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SLAScanRow, 0, limit)
	for rows.Next() {
		var item SLAScanRow
		if err := rows.Scan(&item.ID, &item.TicketNo, &item.AppID, &item.Title, &item.Priority, &item.Status, &item.SLAState,
			&item.AssigneeAdminID, &item.GroupID, &item.CategoryID,
			&item.FirstResponseDueAt, &item.ResolveDueAt, &item.FirstRespondedAt, &item.WarnRatio, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// NextTicketSequence 取工单流水号。走 Postgres 序列而非 COUNT(*)：
// 序列是原子的，并发建单不会撞号，删除历史工单也不会重号。
func (r *Repository) NextTicketSequence(ctx context.Context) (int64, error) {
	var seq int64
	if err := r.pool.QueryRow(ctx, `SELECT nextval('ticket_no_seq')`).Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

// CountAdminOpenTickets 某管理员当前待办数量（工作台角标）。
func (r *Repository) CountAdminOpenTickets(ctx context.Context, adminID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tickets t
		WHERE t.status NOT IN ('closed','cancelled')
		  AND (t.assignee_admin_id = $1
		       OR (t.assignee_admin_id IS NULL AND t.group_id IN (SELECT group_id FROM ticket_group_members WHERE admin_id = $1)))`,
		adminID).Scan(&count)
	return count, err
}

// ListTicketsForExport 导出（不分页，受 Scope 约束）。
func (r *Repository) ListTicketsForExport(ctx context.Context, query ticketdomain.ListQuery, scope ticketdomain.Scope, limit int) ([]ticketdomain.Ticket, error) {
	if limit <= 0 || limit > 20000 {
		limit = 5000
	}
	args := make([]any, 0, 16)
	clauses := ticketFilterClauses(query, scope, &args)
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	sql := "SELECT " + ticketColumns + " " + ticketJoins + where + " " + ticketOrderBy(query) + fmt.Sprintf(" LIMIT $%d", len(args))
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ticketdomain.Ticket, 0, 256)
	for rows.Next() {
		item, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
