package clientip

import (
	"os"
	"strings"
)

// Platform 一个托管平台的「客户端 IP 档案」。
//
// 探测依据是**平台自己注入的环境变量**，而不是请求里的任何东西 ——
// 这一点是安全前提：`FLY_APP_NAME` / `ZEABUR_SERVICE_ID` 只有平台的运行时能写进来，
// 攻击者构造不出。据此补充受信网段或选定单值头，不会给伪造留口子。
type Platform struct {
	Key  string // 机器可读标识（横幅、日志、调试头里出现的就是它）
	Name string // 中文名
	// Header 该平台注入的单值头。平台自己写、且会覆盖客户端同名头时才填。
	Header string
	// Hops 该平台恒定追加的跳数。只有确知跳数固定时才填，否则留 0 走受信网段判定。
	Hops int
	// TrustedPresets 该平台需要额外信任的网段预设。
	TrustedPresets []string
	// TrustPeer 该平台的直连对端是否**按构造**就是它自己的入口。
	//
	// 托管平台把全部入站流量终结在自己的边缘，业务容器收不到来自公网的直连 ——
	// 这不是配置出来的，是平台的定义。因此对端只可能是平台入口，信任它安全。
	//
	// 这条存在的理由是：入口到容器这一跳**并不总是走内网**。云上的 LB、部分 PaaS
	// 的入口回源时持有公网地址，落不进 infra 预设，于是转发头被整个丢掉、
	// 全站客户端 IP 收敛成入口那一个。而地址还会漂移，写死也撑不了多久。
	//
	// 自建 Kubernetes 与 ECS 刻意**不设**：hostNetwork / NodePort / 公网任务
	// 都能让容器被直连，那时对端就是客户端本人，信任它等于交出伪造权。
	TrustPeer bool
	// Env 探测变量，任意一个非空即判定命中。
	Env []string
}

// platformProfiles 探测顺序即优先级：**先具体、后笼统**。
//
// Kubernetes 必须排在最后 —— 上面几乎每一家 PaaS 底层都是 K8s，也都会注入
// `KUBERNETES_SERVICE_HOST`。它排在前面的话，所有平台都会被认成「Kubernetes」，
// 那些平台特有的头与网段就再也用不上了。
var platformProfiles = []Platform{
	{
		Key:       "fly",
		TrustPeer: true,
		Name:      "Fly.io",
		// Fly 的边缘代理会覆写这个头，客户端写进来的同名头到不了应用。
		Header: "Fly-Client-IP",
		Env:    []string{"FLY_APP_NAME", "FLY_ALLOC_ID", "FLY_MACHINE_ID"},
	},
	{
		Key:       "appengine",
		TrustPeer: true,
		Name:      "Google App Engine",
		Header:    "X-Appengine-User-Ip",
		Env:       []string{"GAE_ENV", "GAE_SERVICE", "GAE_INSTANCE"},
	},
	{
		Key:       "cloudrun",
		TrustPeer: true,
		Name:      "Google Cloud Run",
		// 直挂 run.app 时转发链只有客户端一条；挂在外部 HTTPS 负载均衡后面会多一跳，
		// 而那一跳是**公网**地址，不把 GCP LB 段算作受信就会停在它上面。
		TrustedPresets: []string{PresetGCPLoadBalancer},
		Env:            []string{"K_SERVICE", "K_REVISION", "K_CONFIGURATION"},
	},
	{
		Key:       "netlify",
		TrustPeer: true,
		Name:      "Netlify",
		Header:    "X-Nf-Client-Connection-Ip",
		Env:       []string{"NETLIFY"},
	},
	{
		Key:       "zeabur",
		TrustPeer: true,
		Name:      "Zeabur",
		// 两跳都不在 infra 里，缺一条就取不到真实客户端：
		// 入口回源到业务容器时**持有公网地址**（靠 TrustPeer 覆盖），
		// 而平台分发的 *.zeabur.app 域名前面还挂着 CDN，那一跳同样是公网地址 ——
		// 不信任它的话「从右往左找第一个不受信条目」会停在 CDN 边缘上，
		// 结果是全站用户的 IP 收敛成一小撮机房地址。
		TrustedPresets: []string{PresetCloudflare},
		Env:            []string{"ZEABUR_SERVICE_ID", "ZEABUR_PROJECT_ID", "ZEABUR_ENVIRONMENT_ID", "ZEABUR"},
	},
	{
		Key:            "railway",
		TrustPeer:      true,
		Name:           "Railway",
		TrustedPresets: []string{PresetCloudflare},
		Env:            []string{"RAILWAY_ENVIRONMENT", "RAILWAY_SERVICE_ID", "RAILWAY_PROJECT_ID"},
	},
	{
		Key:            "render",
		TrustPeer:      true,
		Name:           "Render",
		TrustedPresets: []string{PresetCloudflare},
		Env:            []string{"RENDER", "RENDER_SERVICE_ID"},
	},
	{
		Key:            "koyeb",
		TrustPeer:      true,
		Name:           "Koyeb",
		TrustedPresets: []string{PresetCloudflare},
		Env:            []string{"KOYEB_APP_NAME", "KOYEB_SERVICE_ID"},
	},
	{
		Key:       "heroku",
		TrustPeer: true,
		Name:      "Heroku",
		// Heroku 路由层把「连到路由的那个地址」追加到转发链末尾，
		// 因此受信网段判定天然给出正确结果，不需要额外档案。
		Env: []string{"DYNO"},
	},
	{
		Key:       "vercel",
		TrustPeer: true,
		Name:      "Vercel",
		Env:       []string{"VERCEL", "VERCEL_ENV"},
	},
	{
		Key:       "azure-app-service",
		TrustPeer: true,
		Name:      "Azure App Service",
		// 这家的转发链条目带端口（1.2.3.4:56789），端口由解析库统一剥掉。
		Env: []string{"WEBSITE_SITE_NAME", "WEBSITE_INSTANCE_ID"},
	},
	{
		Key:  "aws-ecs",
		Name: "AWS ECS / App Runner",
		Env:  []string{"ECS_CONTAINER_METADATA_URI_V4", "ECS_CONTAINER_METADATA_URI", "AWS_EXECUTION_ENV"},
	},
	{
		Key:  "kubernetes",
		Name: "Kubernetes",
		Env:  []string{"KUBERNETES_SERVICE_HOST"},
	},
}

// DetectPlatform 从环境变量判断当前跑在哪个托管平台上。
// getenv 为 nil 时读进程环境；测试注入自己的实现。
// 没命中时返回零值 Platform（Key 为空），调用方据此不做任何补充。
func DetectPlatform(getenv func(string) string) Platform {
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, profile := range platformProfiles {
		for _, key := range profile.Env {
			if strings.TrimSpace(getenv(key)) != "" {
				return profile
			}
		}
	}
	return Platform{}
}

// KnownPlatforms 全部内置平台档案，供文档与自检展示。
func KnownPlatforms() []Platform {
	return append([]Platform(nil), platformProfiles...)
}
