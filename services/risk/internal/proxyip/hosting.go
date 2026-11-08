package proxyip

import "strings"

// SharedUsers 一个 IP 被这么多（及以上）个不同用户用过，就当作共享出口（运营商级 NAT、校园网、公司出口……）
const SharedUsers = 5

// hostingKeywords 运营商名里出现这些词，基本是机房 / 云主机 / 代理服务的出口，而不是家庭或手机网络
var hostingKeywords = []string{
	"oracle", "amazon", "aws", "google cloud", "google llc", "microsoft", "azure", "alibaba", "aliyun", "tencent cloud",
	"huawei cloud", "huawei clouds", "linode", "akamai", "digitalocean", "digital ocean", "vultr", "choopa", "ovh",
	"hetzner", "cloudflare", "zenlayer", "m247", "leaseweb", "hytron", "termrat", "cloudinnovation", "cloud innovation",
	"dmit", "it7 networks", "contabo", "hostinger", "scaleway", "kamatera", "datacamp", "cdn77", "psychz", "multacom",
	"gthost", "kaopu", "ucloud", "kingsoft cloud", "byteplus", "volcengine", "bandwagon", "racknerd", "colocrossing",
	"hostwinds", "ionos", "hosting", "datacenter", "data center", "vps", "server",
	"阿里云", "腾讯云", "华为云", "金山云", "火山引擎", "数据中心", "机房", "云计算", "云服务", "idc",
}

// hostingASNs 已知的云主机 / 机房 ASN
var hostingASNs = map[int64]bool{
	31898: true, 16509: true, 14618: true, 15169: true, 396982: true, 8075: true, 45102: true, 37963: true,
	132203: true, 45090: true, 136907: true, 63949: true, 20940: true, 16625: true, 14061: true, 20473: true,
	16276: true, 24940: true, 13335: true, 21859: true, 9009: true, 60781: true, 28753: true, 151407: true,
	154648: true, 906: true, 25820: true, 51167: true, 47583: true, 12876: true, 36007: true, 212238: true,
	60068: true, 40676: true, 35916: true, 63023: true, 138915: true, 135377: true, 59019: true, 55990: true,
	36352: true, 8100: true, 62240: true,
}

// IsHosting 运营商名或 ASN 看起来是机房 / 云主机 / 代理服务
func IsHosting(isp string, asn int64) bool {
	if asn > 0 && hostingASNs[asn] {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(isp))
	if s == "" {
		return false
	}
	for _, k := range hostingKeywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
