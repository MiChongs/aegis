package egressgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"aegis/pkg/egress/mihomo"
)

// SecretKept 管理端读到的密钥占位：原样提交表示保持原值，提交空串表示清空。
const SecretKept = "__AEGIS_SECRET_KEPT__"

// gatewaySecretKeys 网关线路里的密钥字段（与 mihomo 的字段名合在一起按任意层级匹配）。
var gatewaySecretKeys = []string{"password", "privateKeyPem", "passphrase", "clientKeyPem"}

func secretKeySet() map[string]bool {
	out := map[string]bool{}
	for _, key := range gatewaySecretKeys {
		out[key] = true
	}
	for _, key := range mihomo.SecretKeys {
		out[key] = true
	}
	return out
}

var secretKeys = secretKeySet()

// isSecret 判断某个节点里的某个字段是不是密钥。订阅地址通常带令牌，也按密钥处理。
func isSecret(nodeType string, path []string) bool {
	if len(path) == 0 {
		return false
	}
	if nodeType == NodeProvider && len(path) == 2 && path[0] == "provider" && path[1] == "url" {
		return true
	}
	if nodeType == NodeRules || nodeType == NodeCore || nodeType == NodeRule || nodeType == NodeGateway {
		return false
	}
	return secretKeys[path[len(path)-1]]
}

// SecretFunc 处理一个密钥值；返回值替换原值。
type SecretFunc func(node Node, path string, value string) (string, error)

// WalkSecrets 遍历所有节点里的密钥字段（非空字符串才会回调）。
func WalkSecrets(graph Graph, fn SecretFunc) (Graph, error) {
	out := Graph{Nodes: make([]Node, len(graph.Nodes)), Edges: append([]Edge(nil), graph.Edges...), Viewport: graph.Viewport}
	for i, node := range graph.Nodes {
		out.Nodes[i] = node
		if len(node.Data) == 0 {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(node.Data))
		decoder.UseNumber()
		var data any
		if err := decoder.Decode(&data); err != nil {
			continue
		}
		changed := false
		var walk func(value any, path []string) (any, error)
		walk = func(value any, path []string) (any, error) {
			switch typed := value.(type) {
			case map[string]any:
				for key, item := range typed {
					next, err := walk(item, append(path, key))
					if err != nil {
						return nil, err
					}
					typed[key] = next
				}
				return typed, nil
			case []any:
				for index, item := range typed {
					next, err := walk(item, append(path, "["+strconv.Itoa(index)+"]"))
					if err != nil {
						return nil, err
					}
					typed[index] = next
				}
				return typed, nil
			case string:
				if typed == "" || !isSecret(node.Type, keyPath(path)) {
					return typed, nil
				}
				replaced, err := fn(node, strings.Join(path, "."), typed)
				if err != nil {
					return nil, err
				}
				if replaced != typed {
					changed = true
				}
				return replaced, nil
			default:
				return value, nil
			}
		}
		next, err := walk(data, nil)
		if err != nil {
			return Graph{}, err
		}
		if changed {
			encoded, err := json.Marshal(next)
			if err != nil {
				return Graph{}, err
			}
			out.Nodes[i].Data = encoded
		}
	}
	return out, nil
}

// keyPath 去掉数组下标，只留字段名（peers.[0].private-key → peers.private-key）。
func keyPath(path []string) []string {
	out := make([]string, 0, len(path))
	for _, item := range path {
		if !strings.HasPrefix(item, "[") {
			out = append(out, item)
		}
	}
	return out
}

// Mask 把所有密钥替换成占位，供管理端读取。
func Mask(graph Graph) Graph {
	masked, err := WalkSecrets(graph, func(Node, string, string) (string, error) { return SecretKept, nil })
	if err != nil {
		return graph
	}
	return masked
}

// Restore 把提交里的占位换回原值：先按节点 ID 与字段路径找，找不到再按「类型 + 名称」找
// （复制出来的节点 ID 变了，但通常是同名节点的副本）。找不到的返回问题清单。
func Restore(next, previous Graph) (Graph, []Issue) {
	byID := map[string]string{}
	byName := map[string]string{}
	_, _ = WalkSecrets(previous, func(node Node, path, value string) (string, error) {
		byID[node.ID+"\x00"+path] = value
		if name := NodeName(node); name != "" {
			byName[node.Type+"\x00"+name+"\x00"+path] = value
		}
		return value, nil
	})
	issues := []Issue{}
	restored, _ := WalkSecrets(next, func(node Node, path, value string) (string, error) {
		if value != SecretKept {
			return value, nil
		}
		if original, ok := byID[node.ID+"\x00"+path]; ok {
			return original, nil
		}
		if original, ok := byName[node.Type+"\x00"+NodeName(node)+"\x00"+path]; ok {
			return original, nil
		}
		issues = append(issues, Issue{Level: issueError, NodeID: node.ID, Message: fmt.Sprintf("「%s」的 %s 需要重新填写", displayName(node), path)})
		return "", nil
	})
	return restored, issues
}

// NodeName 节点的显示名（规则名、线路名、核心里的名字）。
func NodeName(node Node) string {
	var probe struct {
		Name  string         `json:"name"`
		Proxy map[string]any `json:"proxy"`
		Group map[string]any `json:"group"`
	}
	if len(node.Data) > 0 {
		_ = json.Unmarshal(node.Data, &probe)
	}
	switch {
	case probe.Name != "":
		return probe.Name
	case probe.Proxy != nil:
		return stringOf(probe.Proxy, "name")
	case probe.Group != nil:
		return stringOf(probe.Group, "name")
	default:
		return ""
	}
}

func displayName(node Node) string {
	if name := NodeName(node); name != "" {
		return name
	}
	if spec, ok := specOf(node.Type); ok {
		return spec.Label
	}
	return node.ID
}
