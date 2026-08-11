package organization

import (
	"sort"
	"strconv"
	"strings"
)

// BuildDepartmentTree 把平铺部门列表组装成树。
//
// 按内部 ID 而非 UUID 组树：物化路径里存的是自增 ID，两边用同一把尺子
// 才能保证「路径里的父节点」和「树上的父节点」永远一致。
// 父节点不在列表中的节点（子树查询的场景）会作为根节点返回，不会被丢弃。
func BuildDepartmentTree(departments []Department) []DepartmentNode {
	present := make(map[int64]bool, len(departments))
	for _, d := range departments {
		present[d.ID] = true
	}

	childMap := make(map[int64][]Department, len(departments))
	var roots []Department
	for _, d := range departments {
		if d.ParentID == nil || !present[*d.ParentID] {
			roots = append(roots, d)
			continue
		}
		childMap[*d.ParentID] = append(childMap[*d.ParentID], d)
	}
	return buildDeptChildren(roots, childMap)
}

func buildDeptChildren(items []Department, childMap map[int64][]Department) []DepartmentNode {
	sortDepartments(items)
	result := make([]DepartmentNode, 0, len(items))
	for _, item := range items {
		node := DepartmentNode{Department: item, Children: []DepartmentNode{}}
		if children, ok := childMap[item.ID]; ok {
			node.Children = buildDeptChildren(children, childMap)
		}
		result = append(result, node)
	}
	return result
}

func sortDepartments(items []Department) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].ID < items[j].ID
	})
}

// BuildOrganizationTree 把平铺组织列表组装成层级树
func BuildOrganizationTree(orgs []Organization) []OrganizationNode {
	present := make(map[int64]bool, len(orgs))
	for _, o := range orgs {
		present[o.ID] = true
	}

	childMap := make(map[int64][]Organization, len(orgs))
	var roots []Organization
	for _, o := range orgs {
		if o.ParentID == nil || !present[*o.ParentID] {
			roots = append(roots, o)
			continue
		}
		childMap[*o.ParentID] = append(childMap[*o.ParentID], o)
	}
	return buildOrgChildren(roots, childMap)
}

func buildOrgChildren(items []Organization, childMap map[int64][]Organization) []OrganizationNode {
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	result := make([]OrganizationNode, 0, len(items))
	for _, item := range items {
		node := OrganizationNode{Organization: item, Children: []OrganizationNode{}}
		if children, ok := childMap[item.ID]; ok {
			node.Children = buildOrgChildren(children, childMap)
		}
		result = append(result, node)
	}
	return result
}

// MaterializePath 由父路径与自身 ID 生成物化路径，形如 /1/5/12/
func MaterializePath(parentPath string, id int64) string {
	if parentPath == "" {
		return "/" + strconv.FormatInt(id, 10) + "/"
	}
	return parentPath + strconv.FormatInt(id, 10) + "/"
}

// PathIDs 解析物化路径中的 ID 序列（自根到自身）
func PathIDs(path string) []int64 {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// IsDescendantPath 判断 childPath 是否位于 ancestorPath 的子树内（含自身）
func IsDescendantPath(ancestorPath, childPath string) bool {
	if ancestorPath == "" || childPath == "" {
		return false
	}
	return strings.HasPrefix(childPath, ancestorPath)
}

// FullDeptName 由祖先链拼出「技术中心 / 平台组」形式的全名
func FullDeptName(names []string) string {
	return strings.Join(names, " / ")
}

// FlattenDepartments 把部门树摊平回列表（深度优先，保持展示顺序）
func FlattenDepartments(nodes []DepartmentNode) []Department {
	var out []Department
	var walk func([]DepartmentNode)
	walk = func(items []DepartmentNode) {
		for _, n := range items {
			out = append(out, n.Department)
			walk(n.Children)
		}
	}
	walk(nodes)
	return out
}
