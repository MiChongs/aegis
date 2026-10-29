package httptransport

// 出海代理网关管理端 DTO。
//
// 配置本体（端点 / 规则）直接复用 systemdomain.EgressSettingsUpdate：
// 它已经是按 JSON 契约设计的整份路由表，在这里再抄一遍 30 个同名字段
// 只会制造两处必须同步修改的地方。下面只定义确实需要在传输层补默认值的入参。

// AdminEgressTestRequest 出海连通性自测。
type AdminEgressTestRequest struct {
	// URL 留空时用健康检查的探测地址。
	URL string `json:"url"`
	// Endpoint 指定端点则绕过规则直连该端点，用于单条线路排障。
	Endpoint string `json:"endpoint"`
	// Profile 模拟调用方标识，验证 profile 维度的规则是否按预期生效。
	Profile   string `json:"profile"`
	TimeoutMs int    `json:"timeoutMs"`
	// Network 为 udp 时做 UDP 自测：URL 填 DNS 服务器 host:port（留空用 UDP 探测服务器），查询 Name。
	Network string `json:"network" binding:"omitempty,oneof=tcp udp"`
	Name    string `json:"name"`
}

// AdminEgressExplainRequest 「这个域名会怎么出去」。
type AdminEgressExplainRequest struct {
	Host    string `json:"host" binding:"required"`
	Port    int    `json:"port"`
	Scheme  string `json:"scheme"`
	Profile string `json:"profile"`
	// Network tcp（默认）或 udp。
	Network string `json:"network" binding:"omitempty,oneof=tcp udp"`
}
