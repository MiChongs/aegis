// Package egress 是平台唯一的出海（出站）代理网关。
//
// 它解决一个问题：部署在境内的服务需要访问境外 API（Stripe / Google / GitHub /
// Apple / 各云存储），而这些访问的可达性、线路、鉴权方式各不相同。与其让每个
// service 各自 new 一个 http.Client 再各自处理代理，不如把「这条出站流量该怎么
// 出去」收敛成一份**按目标域名后缀匹配的路由表**。
//
// 三个核心概念：
//
//	Endpoint 端点  —— 一条真实的出海线路（协议 + 地址 + 凭据），可经 Via 串成链
//	Rule     规则  —— 匹配条件（域名后缀为主）→ 动作（proxy / direct / reject）
//	Gateway  网关  —— 持有端点与规则的快照，对外提供 Transport / Client / DialContext
//
// 典型用法（业务侧只认这一行）：
//
//	client := egress.NewClient(egress.Profile{Name: "payment.stripe", Timeout: 15 * time.Second})
//
// NewClient 绑定的是**全局默认网关**，因此配置热重载后无需重建客户端；未装配网关
// 时自动退化为直连，单元测试与 CLI 场景不需要任何初始化。
//
// 需要裸 TCP（SMTP / LDAP / 自定义协议）时用 DialContext：
//
//	conn, err := egress.DialContext(ctx, "tcp", "smtp.gmail.com:465")
//
// 设计上本包不依赖 aegis 的任何内部包，可整包复制到别的项目复用。
package egress
