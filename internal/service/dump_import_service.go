package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/taskpool"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// DumpImportService 从旧用户系统（Node.js + MySQL）的 mysqldump 文件直接导入用户。
//
// 特性：
//   - 直接流式解析 .sql 文件，无需启动临时 MySQL 实例
//   - 导入时强制指定目标应用（--appid），旧 appid 仅作过滤与备查
//   - 不保留旧密码（哈希不兼容），统一设置新密码（bcrypt），默认强制首登改密
//   - 用户 ID 重新分配（Postgres 序列），旧 id/appid 记入 profile.extra 备查
//   - 同应用账号冲突跳过（幂等，可重复执行）
//   - 保留 QQ / 微信 OAuth 绑定，三方登录无缝衔接
type DumpImportService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

func NewDumpImportService(log *zap.Logger, pg *pgrepo.Repository) *DumpImportService {
	return &DumpImportService{log: log, pg: pg}
}

// DumpImportOptions 导入参数
type DumpImportOptions struct {
	FilePath string
	// TargetAppID 导入到哪个应用（必填）
	TargetAppID int64
	// UnifiedPassword 统一密码（必填；旧密码哈希不兼容，不保留）
	UnifiedPassword string
	// Table dump 中的用户表名，默认 "user"
	Table string
	// SourceAppID 仅导入 dump 中该 appid 的行；0 = 全部
	SourceAppID int64
	// RequirePasswordChange 首次登录强制改密，默认 true
	RequirePasswordChange bool
	// DryRun 只解析统计不写库
	DryRun bool
	// Limit 最多导入条数；0 = 不限
	Limit int
	// Concurrency 写库并发，默认 4
	Concurrency int
}

// DumpImportResult 导入统计
type DumpImportResult struct {
	Parsed          int      `json:"parsed"`          // dump 中解析到的总行数
	Filtered        int      `json:"filtered"`        // 因 --source-appid 过滤掉的行数
	Imported        int      `json:"imported"`        // 成功导入
	Skipped         int      `json:"skipped"`         // 账号已存在跳过
	Failed          int      `json:"failed"`          // 失败
	SkippedAccounts []string `json:"skippedAccounts"` // 跳过的账号（最多保留 200 条）
}

// ImportDump 执行导入
func (s *DumpImportService) ImportDump(ctx context.Context, opts DumpImportOptions) (*DumpImportResult, error) {
	if opts.TargetAppID <= 0 {
		return nil, fmt.Errorf("必须通过 --appid 指定目标应用")
	}
	if strings.TrimSpace(opts.UnifiedPassword) == "" {
		return nil, fmt.Errorf("必须通过 --password 指定统一密码（旧密码哈希不兼容，不会保留）")
	}
	if opts.Table == "" {
		opts.Table = "user"
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}

	file, err := os.Open(opts.FilePath)
	if err != nil {
		return nil, fmt.Errorf("打开 dump 文件失败: %w", err)
	}
	defer file.Close()

	// 统一密码只哈希一次，所有导入用户复用同一 bcrypt 哈希
	unifiedHash, err := bcrypt.GenerateFromPassword([]byte(opts.UnifiedPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	result := &DumpImportResult{}
	var mu sync.Mutex

	// 流式解析 → 分批并发写库
	const batchSize = 256
	batch := make([]dumpRow, 0, batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		rows := batch
		batch = make([]dumpRow, 0, batchSize)
		return taskpool.Dispatch(ctx, opts.Concurrency, rows, func(taskCtx context.Context, row dumpRow) {
			err := s.importRow(taskCtx, row, opts, string(unifiedHash))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				result.Imported++
			case errors.Is(err, errImportRowSkipped):
				result.Skipped++
				if len(result.SkippedAccounts) < 200 {
					result.SkippedAccounts = append(result.SkippedAccounts, dumpColStr(row, "account"))
				}
			default:
				result.Failed++
				s.log.Error("导入用户失败",
					zap.String("account", dumpColStr(row, "account")),
					zap.Int64("legacy_id", dumpColInt64(row, "id")),
					zap.Error(err),
				)
			}
		})
	}

	count, parseErr := streamDumpTableRows(bufio.NewReaderSize(file, 1<<20), opts.Table, func(row dumpRow) error {
		result.Parsed++
		if opts.SourceAppID > 0 && dumpColInt64(row, "appid") != opts.SourceAppID {
			result.Filtered++
			return nil
		}
		if opts.Limit > 0 && result.Parsed-result.Filtered > opts.Limit {
			return errDumpStop
		}
		if opts.DryRun {
			result.Imported++ // dry-run 下表示「将导入」
			return nil
		}
		batch = append(batch, row)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
			if result.Parsed%2048 == 0 {
				s.log.Info("导入进度",
					zap.Int("parsed", result.Parsed),
					zap.Int("imported", result.Imported),
					zap.Int("skipped", result.Skipped),
					zap.Int("failed", result.Failed),
				)
			}
		}
		return nil
	})
	if parseErr != nil && !errors.Is(parseErr, errDumpStop) {
		return result, fmt.Errorf("解析 dump 失败（已解析 %d 行）: %w", count, parseErr)
	}
	if !opts.DryRun {
		if err := flush(); err != nil {
			return result, err
		}
	}
	return result, nil
}

var errImportRowSkipped = errors.New("account already exists, skipped")

// importRow 单行导入：新 ID + 统一密码 + 资料 + 安全状态 + OAuth 绑定
func (s *DumpImportService) importRow(ctx context.Context, row dumpRow, opts DumpImportOptions, unifiedHash string) error {
	legacyID := dumpColInt64(row, "id")
	legacyAppID := dumpColInt64(row, "appid")
	account := strings.TrimSpace(dumpColStr(row, "account"))
	if account == "" {
		if legacyID <= 0 {
			return fmt.Errorf("行缺少 account 与 id，无法生成账号")
		}
		account = fmt.Sprintf("legacy_%d", legacyID)
	}

	now := time.Now().UTC()
	requireChange := opts.RequirePasswordChange
	security := userdomain.ProfileSecurityState{
		PasswordChangedAt:      &now,
		PasswordChangeRequired: &requireChange,
	}

	profile := userdomain.Profile{
		Nickname:            dumpColStr(row, "name"),
		Avatar:              dumpColStr(row, "avatar"),
		Email:               dumpColStr(row, "email"),
		Phone:               dumpColStr(row, "phone"),
		Role:                defaultIfEmpty(dumpColStr(row, "role"), "user"),
		MarkCode:            dumpColStr(row, "markcode"),
		CustomID:            dumpColStr(row, "customId"),
		CustomIDCount:       int64ToIntPtr(dumpColInt64(row, "customIdCount")),
		ParentInviteAccount: dumpColStr(row, "parent_invite_account"),
		RegisterIP:          dumpColStr(row, "register_ip"),
		RegisterISP:         dumpColStr(row, "register_isp"),
		RegisterProvince:    dumpColStr(row, "register_province"),
		RegisterCity:        dumpColStr(row, "register_city"),
		RegisterTime:        dumpColTime(row, "register_time"),
		DisabledReason:      dumpColStr(row, "reason"),
		Extra: map[string]any{
			"importSource": "nodejs_mysqldump",
			"legacyId":     legacyID,
			"legacyAppid":  legacyAppID,
			"importedAt":   now.Format(time.RFC3339),
		},
	}

	// 邀请码：优先沿用旧码，被占用则随机重生成
	preferredInvite := strings.TrimSpace(dumpColStr(row, "invite_code"))
	inviteCode, err := s.resolveInviteCode(ctx, preferredInvite)
	if err != nil {
		return err
	}
	profile.InviteCode = inviteCode

	// 事务创建（新 ID 自动分配）；邀请码/自定义 ID 撞唯一索引时重试
	var created *userdomain.User
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		created, err = s.pg.CreateUserWithProfile(ctx, opts.TargetAppID, account, unifiedHash, profile, security)
		if err == nil {
			break
		}
		if errors.Is(err, pgrepo.ErrAccountAlreadyExists) {
			return fmt.Errorf("%w: appid=%d account=%s", errImportRowSkipped, opts.TargetAppID, account)
		}
		if pgrepo.IsUniqueViolation(err) {
			// 邀请码或 customId 撞库：换随机邀请码、放弃 customId 后重试
			profile.InviteCode, err = s.resolveInviteCode(ctx, "")
			if err != nil {
				return err
			}
			profile.CustomID = ""
			continue
		}
		return err
	}
	if created == nil {
		return fmt.Errorf("创建用户重试超限: account=%s", account)
	}

	// 回填积分 / 经验 / VIP / 启用状态 / 原注册时间（按新 ID upsert）
	enabled := dumpColBool(row, "enabled", true)
	user := userdomain.User{
		ID:              created.ID,
		AppID:           opts.TargetAppID,
		Account:         account,
		PasswordHash:    unifiedHash,
		Integral:        dumpColInt64(row, "integral"),
		Experience:      dumpColInt64(row, "experience"),
		Enabled:         enabled,
		DisabledEndTime: dumpColTime(row, "disabledEndTime"),
		VIPExpireAt:     normalizeLegacyVIPTime(dumpColInt64(row, "vip_time")),
		CreatedAt:       zeroOrNow(dumpColTimeValue(row, "created_at")),
		UpdatedAt:       now,
	}
	if err := s.pg.UpsertImportedUser(ctx, user); err != nil {
		return fmt.Errorf("回填用户属性失败: %w", err)
	}

	// 保留三方登录绑定（绑定到新 appid + 新用户 ID）
	if openQQ := dumpColStr(row, "open_qq"); openQQ != "" {
		if err := s.pg.UpsertOAuthBinding(ctx, opts.TargetAppID, created.ID, authdomain.ProviderProfile{
			Provider:       "qq",
			ProviderUserID: openQQ,
			RawProfile:     map[string]any{"source": "nodejs_mysqldump"},
		}); err != nil {
			return err
		}
	}
	if openWechat := dumpColStr(row, "open_wechat"); openWechat != "" {
		if err := s.pg.UpsertOAuthBinding(ctx, opts.TargetAppID, created.ID, authdomain.ProviderProfile{
			Provider:       "wechat",
			ProviderUserID: openWechat,
			RawProfile:     map[string]any{"source": "nodejs_mysqldump"},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *DumpImportService) resolveInviteCode(ctx context.Context, preferred string) (string, error) {
	if preferred != "" {
		exists, err := s.pg.HasInviteCode(ctx, preferred)
		if err != nil {
			return "", err
		}
		if !exists {
			return preferred, nil
		}
	}
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		code, err := randomInviteCode(inviteCodeLength)
		if err != nil {
			return "", err
		}
		exists, err := s.pg.HasInviteCode(ctx, code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("生成邀请码超过最大重试次数")
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// ──────────────────────────────────────
// dump 行取值助手
// ──────────────────────────────────────

func dumpColStr(row dumpRow, column string) string {
	if v, ok := row[column]; ok && v != nil {
		return *v
	}
	return ""
}

func dumpColInt64(row dumpRow, column string) int64 {
	raw := strings.TrimSpace(dumpColStr(row, column))
	if raw == "" {
		return 0
	}
	if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
		return int64(parsed)
	}
	return 0
}

func dumpColBool(row dumpRow, column string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(dumpColStr(row, column)))
	switch raw {
	case "":
		return fallback
	case "1", "true", "t", "yes":
		return true
	case "0", "false", "f", "no":
		return false
	default:
		return fallback
	}
}

// dumpColTime 解析时间列；非法/零值返回 nil
func dumpColTime(row dumpRow, column string) *time.Time {
	raw := strings.TrimSpace(dumpColStr(row, column))
	if raw == "" || strings.HasPrefix(raw, "0000-00-00") {
		return nil
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
		time.RFC3339,
	} {
		if parsed, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	// 兼容 Unix 时间戳
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
		parsed := time.Unix(seconds, 0).UTC()
		if parsed.Year() >= 2000 && parsed.Year() <= 2100 {
			return &parsed
		}
	}
	return nil
}

func dumpColTimeValue(row dumpRow, column string) time.Time {
	if t := dumpColTime(row, column); t != nil {
		return *t
	}
	return time.Time{}
}

// ──────────────────────────────────────
// mysqldump 流式解析器
// ──────────────────────────────────────

// dumpRow 一行数据：列名 → 原始值（nil 表示 SQL NULL）
type dumpRow map[string]*string

var errDumpStop = errors.New("dump scan stopped")

// streamDumpTableRows 流式解析 mysqldump，对目标表的每一行回调 fn。
// 支持：扩展 INSERT（单语句多行）、显式列清单、反斜杠转义、” 转义、NULL、
// 条件注释 /*!...*/、行注释 --，列顺序取自 CREATE TABLE（无 CREATE TABLE 时需 INSERT 自带列清单）。
// 返回成功回调的行数。fn 返回 errDumpStop 可提前终止。
func streamDumpTableRows(reader *bufio.Reader, table string, fn func(row dumpRow) error) (int, error) {
	var columns []string
	rows := 0

	for {
		stmt, err := readDumpStatement(reader)
		if stmt != "" {
			trimmed := strings.TrimSpace(stmt)
			upper := strings.ToUpper(trimmed)
			switch {
			case strings.HasPrefix(upper, "CREATE TABLE"):
				name, cols := parseCreateTable(trimmed)
				if name == table {
					columns = cols
				}
			case strings.HasPrefix(upper, "INSERT INTO"):
				name, insertCols, valuesPart, ok := splitInsert(trimmed)
				if ok && name == table {
					cols := columns
					if len(insertCols) > 0 {
						cols = insertCols
					}
					if len(cols) == 0 {
						return rows, fmt.Errorf("未找到表 %q 的列定义（dump 缺少 CREATE TABLE 且 INSERT 无列清单）", table)
					}
					if err := parseInsertValues(valuesPart, cols, func(row dumpRow) error {
						rows++
						return fn(row)
					}); err != nil {
						return rows, err
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return rows, nil
			}
			return rows, err
		}
	}
}

// readDumpStatement 读取一条以顶层 ';' 结尾的语句（忽略字符串/反引号/注释内的分号）
func readDumpStatement(reader *bufio.Reader) (string, error) {
	var sb strings.Builder
	const (
		stNormal = iota
		stSingle
		stDouble
		stBacktick
		stLineComment
		stBlockComment
	)
	state := stNormal
	atLineStart := true

	for {
		ch, _, err := reader.ReadRune()
		if err != nil {
			return sb.String(), err
		}
		switch state {
		case stNormal:
			// 语句间的 "-- 注释" 行
			if atLineStart && ch == '-' {
				if next, _ := reader.Peek(1); len(next) == 1 && next[0] == '-' && strings.TrimSpace(sb.String()) == "" {
					state = stLineComment
					continue
				}
			}
			atLineStart = ch == '\n'
			switch ch {
			case '\'':
				state = stSingle
			case '"':
				state = stDouble
			case '`':
				state = stBacktick
			case '/':
				if next, _ := reader.Peek(1); len(next) == 1 && next[0] == '*' && strings.TrimSpace(sb.String()) == "" {
					// 语句开头的块注释（含 mysqldump 条件注释）整段吞掉
					_, _, _ = reader.ReadRune()
					state = stBlockComment
					continue
				}
			case ';':
				return sb.String(), nil
			}
			sb.WriteRune(ch)
		case stSingle, stDouble:
			sb.WriteRune(ch)
			if ch == '\\' {
				if next, _, err2 := reader.ReadRune(); err2 == nil {
					sb.WriteRune(next)
				} else {
					return sb.String(), err2
				}
				continue
			}
			if (state == stSingle && ch == '\'') || (state == stDouble && ch == '"') {
				state = stNormal
			}
		case stBacktick:
			sb.WriteRune(ch)
			if ch == '`' {
				state = stNormal
			}
		case stLineComment:
			if ch == '\n' {
				state = stNormal
				atLineStart = true
			}
		case stBlockComment:
			if ch == '*' {
				if next, _ := reader.Peek(1); len(next) == 1 && next[0] == '/' {
					_, _, _ = reader.ReadRune()
					// 条件注释后通常跟 ';'，顺带吞掉
					if after, _ := reader.Peek(1); len(after) == 1 && after[0] == ';' {
						_, _, _ = reader.ReadRune()
					}
					state = stNormal
				}
			}
		}
	}
}

// parseCreateTable 从 CREATE TABLE 语句提取表名与按序列名
func parseCreateTable(stmt string) (string, []string) {
	name := extractIdentifierAfter(stmt, "TABLE")
	open := strings.Index(stmt, "(")
	if open < 0 {
		return name, nil
	}
	body := stmt[open+1:]
	var columns []string
	depth := 0
	lineStart := true
	expectColumn := true
	var token strings.Builder
	inBacktick := false
	inString := false
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inString {
			// DEFAULT/COMMENT 字符串：处理 \' 与 '' 转义，忽略其中的括号/逗号
			if ch == '\\' && i+1 < len(body) {
				i++
				continue
			}
			if ch == '\'' {
				if i+1 < len(body) && body[i+1] == '\'' {
					i++
					continue
				}
				inString = false
			}
			continue
		}
		if inBacktick {
			if ch == '`' {
				inBacktick = false
				if expectColumn {
					columns = append(columns, token.String())
					expectColumn = false
				}
				token.Reset()
			} else {
				token.WriteByte(ch)
			}
			continue
		}
		if ch == '\'' {
			inString = true
			lineStart = false
			continue
		}
		switch ch {
		case '`':
			if lineStart && depth == 0 {
				inBacktick = true
			}
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return name, columns
			}
			depth--
		case ',':
			if depth == 0 {
				expectColumn = true
				lineStart = true
				continue
			}
		}
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' && ch != '`' {
			lineStart = false
		}
		if ch == '\n' {
			lineStart = true
		}
	}
	return name, columns
}

// splitInsert 拆出 INSERT 的表名、可选列清单与 VALUES 之后的部分
func splitInsert(stmt string) (table string, columns []string, valuesPart string, ok bool) {
	table = extractIdentifierAfter(stmt, "INTO")
	upper := strings.ToUpper(stmt)
	valuesIdx := strings.Index(upper, "VALUES")
	if table == "" || valuesIdx < 0 {
		return "", nil, "", false
	}
	head := stmt[:valuesIdx]
	if open := strings.Index(head, "("); open >= 0 {
		listPart := head[open+1:]
		if close := strings.LastIndex(listPart, ")"); close >= 0 {
			for _, raw := range strings.Split(listPart[:close], ",") {
				col := strings.Trim(strings.TrimSpace(raw), "`\"")
				if col != "" {
					columns = append(columns, col)
				}
			}
		}
	}
	return table, columns, stmt[valuesIdx+len("VALUES"):], true
}

// extractIdentifierAfter 提取关键字后的标识符（支持反引号/裸标识符，去掉库名前缀）
func extractIdentifierAfter(stmt, keyword string) string {
	upper := strings.ToUpper(stmt)
	idx := strings.Index(upper, keyword)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(stmt[idx+len(keyword):])
	// 跳过 IF NOT EXISTS
	if strings.HasPrefix(strings.ToUpper(rest), "IF NOT EXISTS") {
		rest = strings.TrimSpace(rest[len("IF NOT EXISTS"):])
	}
	var name string
	if strings.HasPrefix(rest, "`") {
		end := strings.Index(rest[1:], "`")
		if end < 0 {
			return ""
		}
		name = rest[1 : 1+end]
		// 库名前缀 `db`.`table`
		after := rest[2+end:]
		if strings.HasPrefix(after, ".`") {
			if end2 := strings.Index(after[2:], "`"); end2 >= 0 {
				name = after[2 : 2+end2]
			}
		}
	} else {
		fields := strings.FieldsFunc(rest, func(r rune) bool {
			return r == ' ' || r == '(' || r == '\t' || r == '\n' || r == '\r'
		})
		if len(fields) == 0 {
			return ""
		}
		name = fields[0]
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
	}
	return name
}

// parseInsertValues 解析 VALUES 后的 (v1,v2,...),(...) 序列
func parseInsertValues(s string, columns []string, fn func(row dumpRow) error) error {
	i := 0
	n := len(s)
	for i < n {
		// 找下一个元组开头
		for i < n && s[i] != '(' {
			i++
		}
		if i >= n {
			return nil
		}
		i++ // 跳过 '('
		values := make([]*string, 0, len(columns))
		for {
			// 跳过空白
			for i < n && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				i++
			}
			if i >= n {
				return fmt.Errorf("INSERT 值列表意外结束")
			}
			switch {
			case s[i] == '\'' || s[i] == '"':
				quote := s[i]
				i++
				var sb strings.Builder
				closed := false
				for i < n {
					ch := s[i]
					if ch == '\\' && i+1 < n {
						sb.WriteByte(decodeMySQLEscape(s[i+1]))
						i += 2
						continue
					}
					if ch == quote {
						// '' / "" 转义
						if i+1 < n && s[i+1] == quote {
							sb.WriteByte(quote)
							i += 2
							continue
						}
						i++
						closed = true
						break
					}
					sb.WriteByte(ch)
					i++
				}
				if !closed {
					return fmt.Errorf("字符串字面量未闭合")
				}
				value := sb.String()
				values = append(values, &value)
			default:
				// 裸字面量：NULL / 数字 / 0x.. / 函数等，读到顶层 ',' 或 ')'
				start := i
				for i < n && s[i] != ',' && s[i] != ')' {
					i++
				}
				raw := strings.TrimSpace(s[start:i])
				if strings.EqualFold(raw, "NULL") {
					values = append(values, nil)
				} else {
					value := raw
					values = append(values, &value)
				}
			}
			// 跳过空白
			for i < n && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				i++
			}
			if i >= n {
				return fmt.Errorf("INSERT 元组未闭合")
			}
			if s[i] == ',' {
				i++
				continue
			}
			if s[i] == ')' {
				i++
				break
			}
			return fmt.Errorf("INSERT 元组中出现意外字符 %q", s[i])
		}
		if len(values) != len(columns) {
			return fmt.Errorf("行值数量 %d 与列数 %d 不一致", len(values), len(columns))
		}
		row := make(dumpRow, len(columns))
		for idx, col := range columns {
			row[col] = values[idx]
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	return nil
}

// decodeMySQLEscape MySQL 反斜杠转义解码
func decodeMySQLEscape(ch byte) byte {
	switch ch {
	case '0':
		return 0x00
	case 'b':
		return '\b'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'Z':
		return 0x1a
	default:
		// \' \" \\ \% \_ 等保持原字符
		return ch
	}
}
