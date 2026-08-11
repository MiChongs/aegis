package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	admindomain "aegis/internal/domain/admin"
	orgdomain "aegis/internal/domain/organization"
	apperrors "aegis/pkg/errors"

	"github.com/xuri/excelize/v2"
)

// 组织架构的 Excel 导入导出。
//
// 导入永远是「先预检、再落库」两段式：DryRun 把全部问题一次性列给用户，
// 而不是导到第 37 行才失败并留下半个组织。真正落库时整批走一个事务级流程，
// 任一致命问题就整体放弃。

const (
	importSheetName = "组织架构"
	importMaxRows   = 5000
)

// importColumns 模板列定义，导入导出共用一份，保证导出的文件能直接改完再导回去
var importColumns = []struct {
	Header string
	Width  float64
	Hint   string
}{
	{"部门路径", 28, "多级用 / 分隔，如：技术中心/平台组。留空表示只加入组织不进部门"},
	{"部门代码", 16, "新建部门时使用；已存在的部门按路径匹配，此列可留空"},
	{"登录账号", 18, "必填，必须是平台已存在的管理员账号"},
	{"工号", 14, "选填"},
	{"职位", 18, "选填，如：高级工程师"},
	{"组织角色", 12, "owner / admin / manager / member / viewer，留空默认 member"},
	{"岗位代码", 16, "选填，必须是本组织已有的岗位代码"},
	{"是否负责人", 12, "是 / 否，留空默认否"},
	{"汇报给", 18, "选填，填上级的登录账号，必须与本人同部门"},
}

// ── 导出 ──

// ExportOrganization 导出组织架构为 Excel
func (s *OrganizationService) ExportOrganization(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]byte, string, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermExport)
	if err != nil {
		return nil, "", err
	}

	depts, err := s.pg.ListDepartments(ctx, oc.OrgID, "")
	if err != nil {
		return nil, "", err
	}
	deptFullName := map[string]string{}
	deptIDByUUID := map[string]int64{}
	for _, d := range depts {
		deptIDByUUID[d.UUID] = d.ID
	}
	ids := make([]int64, 0, len(depts))
	for _, d := range depts {
		ids = append(ids, d.ID)
	}
	fullNames, err := s.pg.DeptFullNames(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	for _, d := range depts {
		deptFullName[d.UUID] = fullNames[d.ID]
	}

	f := excelize.NewFile()
	defer f.Close()

	if err := writeImportSheet(f, oc.Org.Name); err != nil {
		return nil, "", err
	}

	// 全量成员（分页拉满，组织成员数远小于这个量级）
	members, err := s.pg.ListOrgMembers(ctx, oc.OrgID, orgdomain.MemberListQuery{Page: 1, Limit: 200})
	if err != nil {
		return nil, "", err
	}
	all := members.Items
	for page := 2; page <= members.TotalPages; page++ {
		next, err := s.pg.ListOrgMembers(ctx, oc.OrgID, orgdomain.MemberListQuery{Page: page, Limit: 200})
		if err != nil {
			return nil, "", err
		}
		all = append(all, next.Items...)
	}

	// 部门内属性（岗位 / 汇报线）按部门取一次，避免逐人查
	type deptDetail struct {
		position string
		leader   bool
		reportTo string
	}
	detailByKey := map[string]deptDetail{}
	positions, err := s.pg.ListPositions(ctx, oc.OrgID)
	if err != nil {
		return nil, "", err
	}
	posCodeByUUID := map[string]string{}
	for _, p := range positions {
		posCodeByUUID[p.UUID] = p.Code
	}
	accountByAdminID := map[int64]string{}
	for _, m := range all {
		accountByAdminID[m.AdminID] = m.Account
	}
	for _, d := range depts {
		dms, err := s.pg.ListDepartmentMembers(ctx, oc.OrgID, d.ID)
		if err != nil {
			return nil, "", err
		}
		for _, dm := range dms {
			key := d.UUID + "|" + fmt.Sprint(dm.AdminID)
			detail := deptDetail{position: posCodeByUUID[dm.PositionUUID], leader: dm.IsLeader}
			if dm.ReportingTo != nil {
				detail.reportTo = accountByAdminID[*dm.ReportingTo]
			}
			detailByKey[key] = detail
		}
	}

	row := 2
	for _, m := range all {
		if len(m.Departments) == 0 {
			// 没有部门归属的成员也要导出，否则导出再导入会把他们丢掉
			if err := writeMemberRow(f, row, "", "", m, deptDetail{}.position, false, ""); err != nil {
				return nil, "", err
			}
			row++
			continue
		}
		for _, dept := range m.Departments {
			detail := detailByKey[dept.UUID+"|"+fmt.Sprint(m.AdminID)]
			if err := writeMemberRow(f, row, deptFullName[dept.UUID], "", m,
				detail.position, detail.leader, detail.reportTo); err != nil {
				return nil, "", err
			}
			row++
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	filename := fmt.Sprintf("%s-组织架构.xlsx", oc.Org.Name)
	return buf.Bytes(), filename, nil
}

// ExportTemplate 导出空白导入模板（只有表头与填写说明）
func (s *OrganizationService) ExportTemplate(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]byte, string, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermImport)
	if err != nil {
		return nil, "", err
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := writeImportSheet(f, oc.Org.Name); err != nil {
		return nil, "", err
	}
	if err := writeHintSheet(f); err != nil {
		return nil, "", err
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "组织架构导入模板.xlsx", nil
}

func writeImportSheet(f *excelize.File, orgName string) error {
	index, err := f.NewSheet(importSheetName)
	if err != nil {
		return err
	}
	f.SetActiveSheet(index)
	_ = f.DeleteSheet("Sheet1")

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"4F46E5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return err
	}

	for i, col := range importColumns {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(importSheetName, cell, col.Header); err != nil {
			return err
		}
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		if err := f.SetColWidth(importSheetName, name, name, col.Width); err != nil {
			return err
		}
		// 表头批注承载填写说明，用户不用来回翻文档
		if err := f.AddComment(importSheetName, excelize.Comment{
			Cell: cell, Author: "Aegis",
			Paragraph: []excelize.RichTextRun{{Text: col.Hint}},
		}); err != nil {
			return err
		}
	}
	lastCol, err := excelize.ColumnNumberToName(len(importColumns))
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(importSheetName, "A1", lastCol+"1", headerStyle); err != nil {
		return err
	}
	if err := f.SetPanes(importSheetName, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	}); err != nil {
		return err
	}
	return f.SetSheetProps(importSheetName, &excelize.SheetPropsOptions{})
}

func writeHintSheet(f *excelize.File) error {
	const sheet = "填写说明"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}
	_ = f.SetColWidth(sheet, "A", "A", 20)
	_ = f.SetColWidth(sheet, "B", "B", 80)
	rows := [][]string{{"列名", "说明"}}
	for _, col := range importColumns {
		rows = append(rows, []string{col.Header, col.Hint})
	}
	rows = append(rows,
		[]string{"", ""},
		[]string{"导入规则", "部门路径中不存在的层级会自动创建；已存在的按名称匹配，不会重复建"},
		[]string{"", "登录账号必须是平台已有的管理员，导入不会创建新账号"},
		[]string{"", "同一人可以出现在多行，表示同时归属多个部门"},
		[]string{"", "建议先用「仅校验」跑一遍，确认无误后再正式导入"},
	)
	for i, row := range rows {
		for j, val := range row {
			cell, err := excelize.CoordinatesToCellName(j+1, i+1)
			if err != nil {
				return err
			}
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeMemberRow(f *excelize.File, row int, deptPath, deptCode string, m orgdomain.Member,
	positionCode string, isLeader bool, reportTo string) error {
	leader := "否"
	if isLeader {
		leader = "是"
	}
	values := []any{deptPath, deptCode, m.Account, m.EmployeeNo, m.Title, m.OrgRole, positionCode, leader, reportTo}
	for i, v := range values {
		cell, err := excelize.CoordinatesToCellName(i+1, row)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(importSheetName, cell, v); err != nil {
			return err
		}
	}
	return nil
}

// ── 导入 ──

// ImportOrganization 从 Excel 导入组织架构。
// dryRun 为 true 时只校验不落库，把全部问题一次性返回。
func (s *OrganizationService) ImportOrganization(ctx context.Context, access *admindomain.AccessContext, orgUUID string, data []byte, dryRun bool) (*orgdomain.ImportResult, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermImport)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}

	rows, err := parseImportRows(data)
	if err != nil {
		return nil, err
	}
	result := &orgdomain.ImportResult{DryRun: dryRun, TotalRows: len(rows), Issues: []orgdomain.ImportIssue{}}
	if len(rows) == 0 {
		result.Issues = append(result.Issues, orgdomain.ImportIssue{
			Message: "表格中没有可导入的数据行", Fatal: true,
		})
		return result, nil
	}

	// ── 预检：账号、岗位、角色、汇报关系 ──

	accounts := map[string]int64{}
	for _, row := range rows {
		accounts[row.Account] = 0
	}
	resolved, err := s.pg.ResolveAdminIDsByAccount(ctx, mapKeys(accounts))
	if err != nil {
		return nil, err
	}
	for account, id := range resolved {
		accounts[account] = id
	}

	positions, err := s.pg.ListPositions(ctx, oc.OrgID)
	if err != nil {
		return nil, err
	}
	posByCode := map[string]string{}
	for _, p := range positions {
		posByCode[p.Code] = p.UUID
	}

	seenAccounts := map[string]bool{}
	for _, row := range rows {
		if row.Account == "" {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "登录账号", Message: "登录账号不能为空", Fatal: true,
			})
			continue
		}
		if accounts[row.Account] == 0 {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "登录账号", Value: row.Account,
				Message: "平台中不存在该管理员账号", Fatal: true,
			})
		}
		if row.OrgRole != "" && !orgdomain.IsValidRole(row.OrgRole) {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "组织角色", Value: row.OrgRole,
				Message: "组织角色取值无效", Fatal: true,
			})
		}
		if row.OrgRole == orgdomain.RoleOwner {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "组织角色", Value: row.OrgRole,
				Message: "所有者只能通过转让产生，不能通过导入设置", Fatal: true,
			})
		}
		if row.Position != "" {
			if _, ok := posByCode[row.Position]; !ok {
				result.Issues = append(result.Issues, orgdomain.ImportIssue{
					RowNo: row.RowNo, Field: "岗位代码", Value: row.Position,
					Message: "本组织没有该岗位代码，请先创建岗位", Fatal: false,
				})
			}
		}
		if row.ReportTo != "" {
			if _, ok := accounts[row.ReportTo]; !ok || accounts[row.ReportTo] == 0 {
				result.Issues = append(result.Issues, orgdomain.ImportIssue{
					RowNo: row.RowNo, Field: "汇报给", Value: row.ReportTo,
					Message: "上级账号不在平台中", Fatal: false,
				})
			}
			if row.ReportTo == row.Account {
				result.Issues = append(result.Issues, orgdomain.ImportIssue{
					RowNo: row.RowNo, Field: "汇报给", Value: row.ReportTo,
					Message: "不能把自己设为自己的上级", Fatal: false,
				})
			}
		}
		if row.DeptPath == "" && !seenAccounts[row.Account] {
			seenAccounts[row.Account] = true
		}
	}

	// 配额在预检阶段就要算清楚，不能导到一半才发现超了
	if oc.Org.Quota.MemberLimit > 0 {
		existing, err := s.pg.CountOrgMembers(ctx, oc.OrgID)
		if err != nil {
			return nil, err
		}
		distinct := map[string]bool{}
		for _, row := range rows {
			distinct[row.Account] = true
		}
		if existing+len(distinct) > oc.Org.Quota.MemberLimit {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				Message: fmt.Sprintf("导入后成员数将达 %d 人，超过组织配额上限 %d",
					existing+len(distinct), oc.Org.Quota.MemberLimit),
				Fatal: true,
			})
		}
	}

	if hasFatal(result.Issues) || dryRun {
		if hasFatal(result.Issues) {
			result.Skipped = len(rows)
		}
		return result, nil
	}

	// ── 落库 ──

	deptCache := map[string]string{} // 路径 → 部门 UUID
	for _, row := range rows {
		adminID := accounts[row.Account]

		orgRole := row.OrgRole
		if orgRole == "" {
			orgRole = orgdomain.RoleMember
		}
		existing, err := s.pg.GetOrgMember(ctx, oc.OrgID, adminID)
		if err != nil {
			return nil, err
		}
		if _, err := s.pg.AddOrgMember(ctx, oc.OrgID, orgdomain.AddMemberInput{
			AdminID: adminID, OrgRole: orgRole, EmployeeNo: row.EmployeeNo, Title: row.Title,
		}, access.AdminID); err != nil {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "登录账号", Value: row.Account,
				Message: "加入组织失败：" + err.Error(),
			})
			continue
		}
		if existing == nil {
			result.MemberAdded++
		} else {
			result.MemberUpdate++
		}

		if row.DeptPath == "" {
			continue
		}
		deptUUID, created, err := s.ensureDeptPath(ctx, oc, row.DeptPath, row.DeptCode, deptCache)
		if err != nil {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "部门路径", Value: row.DeptPath,
				Message: "创建部门失败：" + err.Error(),
			})
			continue
		}
		result.DeptCreated += created

		if err := s.pg.AssignMemberDepartments(ctx, oc.OrgID, adminID,
			orgdomain.AssignDeptInput{DeptUUIDs: []string{deptUUID}}); err != nil {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "部门路径", Value: row.DeptPath,
				Message: "加入部门失败：" + err.Error(),
			})
			continue
		}

		deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
		if err != nil {
			continue
		}
		setInput := orgdomain.SetDeptMemberInput{}
		if row.IsLeader {
			leader := true
			setInput.IsLeader = &leader
		}
		if posUUID, ok := posByCode[row.Position]; ok && row.Position != "" {
			setInput.PositionUUID = &posUUID
		}
		if row.Title != "" {
			title := row.Title
			setInput.JobTitle = &title
		}
		if err := s.pg.SetDepartmentMember(ctx, oc.OrgID, deptID, adminID, setInput); err != nil {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Message: "设置部门属性失败：" + err.Error(),
			})
		}
	}

	// 汇报线放在第二轮 —— 第一轮跑完才能保证上级也已经在部门里了
	for _, row := range rows {
		if row.ReportTo == "" || row.DeptPath == "" {
			continue
		}
		supervisorID := accounts[row.ReportTo]
		adminID := accounts[row.Account]
		if supervisorID == 0 || adminID == 0 {
			continue
		}
		deptUUID, ok := deptCache[row.DeptPath]
		if !ok {
			continue
		}
		deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
		if err != nil {
			continue
		}
		if err := s.pg.SetDepartmentMember(ctx, oc.OrgID, deptID, adminID,
			orgdomain.SetDeptMemberInput{ReportingTo: &supervisorID}); err != nil {
			result.Issues = append(result.Issues, orgdomain.ImportIssue{
				RowNo: row.RowNo, Field: "汇报给", Value: row.ReportTo,
				Message: "设置汇报线失败：" + err.Error(),
			})
		}
	}

	s.recordActivity(ctx, oc.OrgID, access.AdminID, "org.import", "organization", oc.UUID,
		fmt.Sprintf("导入组织架构：新增 %d 人、更新 %d 人、新建 %d 个部门",
			result.MemberAdded, result.MemberUpdate, result.DeptCreated), nil)
	return result, nil
}

// ensureDeptPath 按「技术中心/平台组」逐级确保部门存在，返回叶子部门的 UUID
// 与本次新建的部门数。
func (s *OrganizationService) ensureDeptPath(ctx context.Context, oc *orgContext, path, leafCode string, cache map[string]string) (string, int, error) {
	if uuid, ok := cache[path]; ok {
		return uuid, 0, nil
	}
	segments := splitDeptPath(path)
	if len(segments) == 0 {
		return "", 0, apperrors.New(40083, http.StatusBadRequest, "部门路径为空")
	}

	depts, err := s.pg.ListDepartments(ctx, oc.OrgID, "")
	if err != nil {
		return "", 0, err
	}
	byParentName := map[string]orgdomain.Department{}
	for _, d := range depts {
		parentKey := ""
		if d.ParentUUID != "" {
			parentKey = d.ParentUUID
		}
		byParentName[parentKey+"|"+d.Name] = d
	}

	created := 0
	parentUUID := ""
	accumulated := ""
	for i, name := range segments {
		if accumulated == "" {
			accumulated = name
		} else {
			accumulated += "/" + name
		}
		if uuid, ok := cache[accumulated]; ok {
			parentUUID = uuid
			continue
		}
		if existing, ok := byParentName[parentUUID+"|"+name]; ok {
			parentUUID = existing.UUID
			cache[accumulated] = existing.UUID
			continue
		}
		code := slugifyDeptCode(name)
		if i == len(segments)-1 && leafCode != "" {
			code = leafCode
		}
		dept, err := s.pg.CreateDepartment(ctx, oc.OrgID, orgdomain.CreateDeptInput{
			ParentUUID: parentUUID, Name: name, Code: code,
		})
		if err != nil {
			return "", created, err
		}
		created++
		parentUUID = dept.UUID
		cache[accumulated] = dept.UUID
		byParentName[dept.ParentUUID+"|"+dept.Name] = *dept
	}
	cache[path] = parentUUID
	return parentUUID, created, nil
}

// ── 解析 ──

func parseImportRows(data []byte) ([]orgdomain.ImportRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, apperrors.New(40084, http.StatusBadRequest, "无法解析 Excel 文件："+err.Error())
	}
	defer f.Close()

	sheet := importSheetName
	if _, err := f.GetSheetIndex(sheet); err != nil || !sheetExists(f, sheet) {
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, apperrors.New(40085, http.StatusBadRequest, "Excel 文件中没有工作表")
		}
		sheet = sheets[0]
	}

	raw, err := f.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	if len(raw) <= 1 {
		return nil, nil
	}
	if len(raw)-1 > importMaxRows {
		return nil, apperrors.New(40086, http.StatusBadRequest,
			fmt.Sprintf("单次最多导入 %d 行，当前 %d 行", importMaxRows, len(raw)-1))
	}

	rows := make([]orgdomain.ImportRow, 0, len(raw)-1)
	for i, cells := range raw[1:] {
		row := orgdomain.ImportRow{RowNo: i + 2}
		row.DeptPath = strings.TrimSpace(cellAt(cells, 0))
		row.DeptCode = strings.TrimSpace(cellAt(cells, 1))
		row.Account = strings.TrimSpace(cellAt(cells, 2))
		row.EmployeeNo = strings.TrimSpace(cellAt(cells, 3))
		row.Title = strings.TrimSpace(cellAt(cells, 4))
		row.OrgRole = strings.ToLower(strings.TrimSpace(cellAt(cells, 5)))
		row.Position = strings.TrimSpace(cellAt(cells, 6))
		row.IsLeader = isTruthy(cellAt(cells, 7))
		row.ReportTo = strings.TrimSpace(cellAt(cells, 8))

		// 整行空白是表格末尾的残留，静默跳过而不是报一堆「账号为空」
		if row.DeptPath == "" && row.Account == "" && row.EmployeeNo == "" && row.Title == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func sheetExists(f *excelize.File, name string) bool {
	for _, s := range f.GetSheetList() {
		if s == name {
			return true
		}
	}
	return false
}

func cellAt(cells []string, idx int) string {
	if idx < len(cells) {
		return cells[idx]
	}
	return ""
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "是", "y", "yes", "true", "1", "√":
		return true
	}
	return false
}

func splitDeptPath(path string) []string {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' || r == '>' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// slugifyDeptCode 由部门名生成一个可用的代码。中文名生成不出有意义的 slug，
// 退化成带哈希的形式即可 —— 代码在这里只是唯一键，不需要人类可读。
func slugifyDeptCode(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = fmt.Sprintf("dept-%08x", hashString(name))
	}
	if len(slug) > 60 {
		slug = slug[:60]
	}
	return slug
}

func hashString(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func hasFatal(issues []orgdomain.ImportIssue) bool {
	for _, i := range issues {
		if i.Fatal {
			return true
		}
	}
	return false
}

func mapKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}
