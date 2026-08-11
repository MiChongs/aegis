package egress

import "strings"

// suffixTrie 是按域名标签**倒序**组织的前缀树，用来回答
// 「这个 host 命中了哪条后缀规则」。
//
// 用 strings.HasSuffix 逐条比对有两个问题：一是规则上千条时每次连接都要线性扫，
// 二是 "google.com" 会错误命中 "notgoogle.com"。按标签建树同时解决了这两点：
// 匹配复杂度只与 host 的标签数（个位数）有关，且天然落在标签边界上。
type suffixTrie struct {
	root *trieNode
	size int
}

type trieNode struct {
	children map[string]*trieNode
	// terminal 表示「从根到这里」构成一条完整的后缀规则。
	terminal bool
}

func newSuffixTrie() *suffixTrie {
	return &suffixTrie{root: &trieNode{}}
}

// buildSuffixTrie 从已规范化的后缀列表构造；空列表返回 nil，调用方据此跳过匹配。
func buildSuffixTrie(suffixes []string) *suffixTrie {
	if len(suffixes) == 0 {
		return nil
	}
	t := newSuffixTrie()
	for _, s := range suffixes {
		t.insert(s)
	}
	if t.size == 0 {
		return nil
	}
	return t
}

func (t *suffixTrie) insert(suffix string) {
	labels := splitLabels(suffix)
	if len(labels) == 0 {
		return
	}
	node := t.root
	for i := len(labels) - 1; i >= 0; i-- {
		if node.children == nil {
			node.children = make(map[string]*trieNode, 2)
		}
		child, ok := node.children[labels[i]]
		if !ok {
			child = &trieNode{}
			node.children[labels[i]] = child
		}
		node = child
	}
	if !node.terminal {
		node.terminal = true
		t.size++
	}
}

// match 返回命中的后缀本身（便于向管理端解释「为什么走了这条规则」）。
// 多条后缀同时可命中时返回**最长**的那条（*.a.example.com 优先于 *.example.com）。
func (t *suffixTrie) match(host string) (string, bool) {
	if t == nil || host == "" {
		return "", false
	}
	labels := splitLabels(host)
	if len(labels) == 0 {
		return "", false
	}
	node := t.root
	best := -1
	for i := len(labels) - 1; i >= 0; i-- {
		child, ok := node.children[labels[i]]
		if !ok {
			break
		}
		node = child
		if node.terminal {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return strings.Join(labels[best:], "."), true
}

func (t *suffixTrie) contains(host string) bool {
	_, ok := t.match(host)
	return ok
}

func splitLabels(host string) []string {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return nil
	}
	parts := strings.Split(host, ".")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
