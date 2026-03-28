package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type src struct {
	ip, country, cc, region, city, isp, asn, tz string
	lat, lng                                     float64
}

type rDef struct {
	reason   string
	status   int
	code     int
	severity string
	wafID    *int
	wafAct   string
	wafMsg   string
}

func ip(v int) *int { return &v }

// 120+ 全球攻击源，覆盖所有大洲主要国家
var sources = []src{
	// ── 北美（12）──
	{"45.33.32.156", "United States", "US", "California", "Los Angeles", "Linode", "AS63949", "America/Los_Angeles", 34.0522, -118.2437},
	{"104.131.175.196", "United States", "US", "New York", "New York", "DigitalOcean", "AS14061", "America/New_York", 40.7128, -74.006},
	{"23.94.128.11", "United States", "US", "Illinois", "Chicago", "ColoCrossing", "AS36352", "America/Chicago", 41.8781, -87.6298},
	{"67.205.167.33", "United States", "US", "California", "San Francisco", "DigitalOcean", "AS14061", "America/Los_Angeles", 37.7749, -122.4194},
	{"157.245.100.5", "United States", "US", "Virginia", "Ashburn", "DigitalOcean", "AS14061", "America/New_York", 39.0438, -77.4874},
	{"198.51.100.42", "United States", "US", "Texas", "Dallas", "AWS", "AS16509", "America/Chicago", 32.7767, -96.797},
	{"142.93.120.18", "United States", "US", "Oregon", "Portland", "DigitalOcean", "AS14061", "America/Los_Angeles", 45.5152, -122.6784},
	{"192.99.14.81", "Canada", "CA", "Quebec", "Montreal", "OVH", "AS16276", "America/Toronto", 45.5017, -73.5673},
	{"198.27.80.50", "Canada", "CA", "Ontario", "Toronto", "OVH", "AS16276", "America/Toronto", 43.6532, -79.3832},
	{"187.174.252.10", "Mexico", "MX", "CDMX", "Mexico City", "Telmex", "AS8151", "America/Mexico_City", 19.4326, -99.1332},
	{"190.2.148.90", "Argentina", "AR", "Buenos Aires", "Buenos Aires", "Telecom Argentina", "AS22927", "America/Argentina/Buenos_Aires", -34.6037, -58.3816},
	{"177.71.208.100", "Brazil", "BR", "Sao Paulo", "Sao Paulo", "Locaweb", "AS27715", "America/Sao_Paulo", -23.5505, -46.6333},
	// ── 欧洲（25）──
	{"51.15.112.80", "United Kingdom", "GB", "England", "London", "Scaleway", "AS12876", "Europe/London", 51.5074, -0.1278},
	{"81.2.69.144", "United Kingdom", "GB", "England", "Manchester", "BT", "AS2856", "Europe/London", 53.4808, -2.2426},
	{"163.172.67.180", "France", "FR", "Ile-de-France", "Paris", "Scaleway", "AS12876", "Europe/Paris", 48.8566, 2.3522},
	{"51.158.48.100", "France", "FR", "Provence", "Marseille", "Scaleway", "AS12876", "Europe/Paris", 43.2965, 5.3698},
	{"136.243.44.11", "Germany", "DE", "Saxony", "Falkenstein", "Hetzner", "AS24940", "Europe/Berlin", 50.4779, 12.3713},
	{"5.9.61.200", "Germany", "DE", "Bavaria", "Nuremberg", "Hetzner", "AS24940", "Europe/Berlin", 49.4521, 11.0767},
	{"78.46.91.130", "Germany", "DE", "Bavaria", "Munich", "Hetzner", "AS24940", "Europe/Berlin", 48.1351, 11.582},
	{"89.248.167.131", "Netherlands", "NL", "North Holland", "Amsterdam", "DigitalOcean", "AS14061", "Europe/Amsterdam", 52.3676, 4.9041},
	{"185.107.47.215", "Netherlands", "NL", "South Holland", "Rotterdam", "FDCServers", "AS174", "Europe/Amsterdam", 51.9225, 4.4792},
	{"46.29.248.100", "Poland", "PL", "Masovia", "Warsaw", "DigitalOcean", "AS14061", "Europe/Warsaw", 52.2297, 21.0122},
	{"89.46.100.50", "Romania", "RO", "Bucharest", "Bucharest", "M247", "AS9009", "Europe/Bucharest", 44.4268, 26.1025},
	{"91.234.33.17", "Ukraine", "UA", "Kyiv", "Kyiv", "Hetzner Ukraine", "AS213230", "Europe/Kyiv", 50.4501, 30.5234},
	{"176.36.20.8", "Ukraine", "UA", "Kharkiv", "Kharkiv", "Triolan", "AS13188", "Europe/Kyiv", 49.9935, 36.2304},
	{"185.220.101.34", "Russia", "RU", "Moscow", "Moscow", "DataLine", "AS39134", "Europe/Moscow", 55.7558, 37.6173},
	{"91.243.85.67", "Russia", "RU", "Saint Petersburg", "Saint Petersburg", "Selectel", "AS49505", "Europe/Moscow", 59.9343, 30.3351},
	{"195.54.160.10", "Russia", "RU", "Novosibirsk", "Novosibirsk", "Rostelecom", "AS12389", "Asia/Novosibirsk", 55.0084, 82.9357},
	{"88.255.216.10", "Turkey", "TR", "Istanbul", "Istanbul", "Turk Telekom", "AS9121", "Europe/Istanbul", 41.0082, 28.9784},
	{"193.239.86.5", "Spain", "ES", "Madrid", "Madrid", "Telefonica", "AS3352", "Europe/Madrid", 40.4168, -3.7038},
	{"151.100.179.10", "Italy", "IT", "Lazio", "Rome", "Fastweb", "AS12874", "Europe/Rome", 41.9028, 12.4964},
	{"94.23.156.80", "Portugal", "PT", "Lisbon", "Lisbon", "OVH", "AS16276", "Europe/Lisbon", 38.7223, -9.1393},
	{"46.166.186.20", "Sweden", "SE", "Stockholm", "Stockholm", "Bahnhof", "AS8473", "Europe/Stockholm", 59.3293, 18.0686},
	{"91.207.7.10", "Finland", "FI", "Uusimaa", "Helsinki", "Hetzner", "AS24940", "Europe/Helsinki", 60.1699, 24.9384},
	{"80.67.167.81", "Switzerland", "CH", "Zurich", "Zurich", "Init7", "AS13030", "Europe/Zurich", 47.3769, 8.5417},
	{"193.176.84.10", "Austria", "AT", "Vienna", "Vienna", "A1 Telekom", "AS1901", "Europe/Vienna", 48.2082, 16.3738},
	{"31.220.0.50", "Czech Republic", "CZ", "Prague", "Prague", "Contabo", "AS51167", "Europe/Prague", 50.0755, 14.4378},
	{"5.2.72.100", "Hungary", "HU", "Budapest", "Budapest", "Digi", "AS20845", "Europe/Budapest", 47.4979, 19.0402},
	{"213.171.215.10", "Greece", "GR", "Attica", "Athens", "OTE", "AS6799", "Europe/Athens", 37.9838, 23.7275},
	// ── 亚洲（30）──
	{"218.75.176.20", "China", "CN", "Zhejiang", "Hangzhou", "China Telecom", "AS4134", "Asia/Shanghai", 30.2741, 120.1551},
	{"114.114.114.114", "China", "CN", "Jiangsu", "Nanjing", "China Telecom", "AS4134", "Asia/Shanghai", 32.0603, 118.7969},
	{"36.99.136.210", "China", "CN", "Beijing", "Beijing", "China Unicom", "AS4837", "Asia/Shanghai", 39.9042, 116.4074},
	{"120.232.0.18", "China", "CN", "Guangdong", "Guangzhou", "China Mobile", "AS9808", "Asia/Shanghai", 23.1291, 113.2644},
	{"61.135.169.125", "China", "CN", "Beijing", "Beijing", "China Unicom", "AS4808", "Asia/Shanghai", 39.9289, 116.3883},
	{"180.101.49.12", "China", "CN", "Jiangsu", "Suzhou", "China Telecom", "AS4134", "Asia/Shanghai", 31.2990, 120.5853},
	{"153.126.203.4", "Japan", "JP", "Tokyo", "Tokyo", "SAKURA Internet", "AS9370", "Asia/Tokyo", 35.6762, 139.6503},
	{"133.242.175.10", "Japan", "JP", "Osaka", "Osaka", "SAKURA Internet", "AS9370", "Asia/Tokyo", 34.6937, 135.5023},
	{"211.49.46.20", "South Korea", "KR", "Seoul", "Seoul", "Korea Telecom", "AS4766", "Asia/Seoul", 37.5665, 126.978},
	{"175.45.176.3", "South Korea", "KR", "Busan", "Busan", "SK Broadband", "AS9318", "Asia/Seoul", 35.1796, 129.0756},
	{"103.99.170.50", "India", "IN", "Maharashtra", "Mumbai", "HostGator", "AS133229", "Asia/Kolkata", 19.076, 72.8777},
	{"49.36.128.20", "India", "IN", "Delhi", "New Delhi", "Reliance Jio", "AS55836", "Asia/Kolkata", 28.6139, 77.209},
	{"103.69.97.5", "India", "IN", "Karnataka", "Bangalore", "ACT Fibernet", "AS45609", "Asia/Kolkata", 12.9716, 77.5946},
	{"128.199.159.40", "Singapore", "SG", "Singapore", "Singapore", "DigitalOcean", "AS14061", "Asia/Singapore", 1.3521, 103.8198},
	{"13.228.0.15", "Singapore", "SG", "Singapore", "Singapore", "AWS", "AS16509", "Asia/Singapore", 1.3521, 103.8198},
	{"171.97.32.15", "Thailand", "TH", "Bangkok", "Bangkok", "True Internet", "AS17552", "Asia/Bangkok", 13.7563, 100.5018},
	{"36.90.48.100", "Indonesia", "ID", "Jakarta", "Jakarta", "Telkom Indonesia", "AS17974", "Asia/Jakarta", -6.2088, 106.8456},
	{"113.160.92.5", "Vietnam", "VN", "Hanoi", "Hanoi", "VNPT", "AS45899", "Asia/Ho_Chi_Minh", 21.0285, 105.8542},
	{"175.176.32.10", "Philippines", "PH", "Metro Manila", "Manila", "PLDT", "AS9299", "Asia/Manila", 14.5995, 120.9842},
	{"103.216.220.5", "Bangladesh", "BD", "Dhaka", "Dhaka", "Link3 Technologies", "AS24389", "Asia/Dhaka", 23.8103, 90.4125},
	{"5.160.139.20", "Iran", "IR", "Tehran", "Tehran", "SHATEL", "AS31549", "Asia/Tehran", 35.6892, 51.389},
	{"176.221.68.10", "Iraq", "IQ", "Baghdad", "Baghdad", "EarthLink", "AS203214", "Asia/Baghdad", 33.3152, 44.3661},
	{"37.148.217.20", "Saudi Arabia", "SA", "Riyadh", "Riyadh", "STC", "AS25019", "Asia/Riyadh", 24.7136, 46.6753},
	{"94.182.190.10", "UAE", "AE", "Dubai", "Dubai", "Etisalat", "AS8966", "Asia/Dubai", 25.2048, 55.2708},
	{"185.117.75.5", "Israel", "IL", "Tel Aviv", "Tel Aviv", "Bezeq", "AS8551", "Asia/Jerusalem", 32.0853, 34.7818},
	{"103.28.250.10", "Pakistan", "PK", "Punjab", "Lahore", "StormFiber", "AS45773", "Asia/Karachi", 31.5204, 74.3587},
	{"202.166.207.5", "Nepal", "NP", "Bagmati", "Kathmandu", "Worldlink", "AS17501", "Asia/Kathmandu", 27.7172, 85.324},
	{"103.146.56.10", "Myanmar", "MM", "Yangon", "Yangon", "Frontiir", "AS136255", "Asia/Yangon", 16.8661, 96.1951},
	{"103.91.64.5", "Cambodia", "KH", "Phnom Penh", "Phnom Penh", "EZECOM", "AS18013", "Asia/Phnom_Penh", 11.5564, 104.9282},
	{"202.46.32.10", "Sri Lanka", "LK", "Western", "Colombo", "SLT", "AS9329", "Asia/Colombo", 6.9271, 79.8612},
	// ── 非洲（15）──
	{"41.76.108.46", "South Africa", "ZA", "Gauteng", "Johannesburg", "RSAWEB", "AS37153", "Africa/Johannesburg", -26.2041, 28.0473},
	{"105.112.0.10", "Nigeria", "NG", "Lagos", "Lagos", "Airtel Nigeria", "AS36873", "Africa/Lagos", 6.5244, 3.3792},
	{"196.216.2.5", "Kenya", "KE", "Nairobi", "Nairobi", "Safaricom", "AS33771", "Africa/Nairobi", -1.2921, 36.8219},
	{"197.210.0.10", "Egypt", "EG", "Cairo", "Cairo", "Vodafone Egypt", "AS36935", "Africa/Cairo", 30.0444, 31.2357},
	{"41.188.0.5", "Morocco", "MA", "Casablanca", "Casablanca", "Maroc Telecom", "AS6713", "Africa/Casablanca", 33.5731, -7.5898},
	{"105.235.0.10", "Algeria", "DZ", "Algiers", "Algiers", "Algerie Telecom", "AS36947", "Africa/Algiers", 36.7538, 3.0588},
	{"41.216.0.5", "Tunisia", "TN", "Tunis", "Tunis", "Tunisie Telecom", "AS2609", "Africa/Tunis", 36.8065, 10.1815},
	{"196.1.0.10", "Ghana", "GH", "Greater Accra", "Accra", "Vodafone Ghana", "AS29614", "Africa/Accra", 5.6037, -0.187},
	{"196.28.0.5", "Tanzania", "TZ", "Dar es Salaam", "Dar es Salaam", "TTCL", "AS33765", "Africa/Dar_es_Salaam", -6.7924, 39.2083},
	{"105.16.0.10", "Ethiopia", "ET", "Addis Ababa", "Addis Ababa", "Ethio Telecom", "AS24757", "Africa/Addis_Ababa", 9.025, 38.7469},
	{"41.223.0.5", "Senegal", "SN", "Dakar", "Dakar", "Sonatel", "AS8346", "Africa/Dakar", 14.7167, -17.4677},
	{"154.72.0.10", "Cameroon", "CM", "Centre", "Yaounde", "MTN Cameroon", "AS36912", "Africa/Douala", 3.848, 11.5021},
	{"41.78.0.5", "Uganda", "UG", "Central", "Kampala", "MTN Uganda", "AS20294", "Africa/Kampala", 0.3476, 32.5825},
	{"102.68.0.10", "Mozambique", "MZ", "Maputo", "Maputo", "Vodacom Mozambique", "AS37342", "Africa/Maputo", -25.9692, 32.5732},
	{"196.43.0.5", "Zimbabwe", "ZW", "Harare", "Harare", "TelOne", "AS37204", "Africa/Harare", -17.8252, 31.0335},
	// ── 大洋洲（4）──
	{"103.22.200.7", "Australia", "AU", "New South Wales", "Sydney", "Cloudflare", "AS13335", "Australia/Sydney", -33.8688, 151.2093},
	{"103.4.16.10", "Australia", "AU", "Victoria", "Melbourne", "DigitalOcean", "AS14061", "Australia/Melbourne", -37.8136, 144.9631},
	{"210.48.76.5", "New Zealand", "NZ", "Auckland", "Auckland", "Spark NZ", "AS4771", "Pacific/Auckland", -36.8485, 174.7633},
	{"202.0.0.10", "Fiji", "FJ", "Central", "Suva", "Telecom Fiji", "AS18200", "Pacific/Fiji", -18.1416, 178.4419},
	// ── 南美补充（6）──
	{"200.160.2.10", "Brazil", "BR", "Rio de Janeiro", "Rio de Janeiro", "NIC.br", "AS22548", "America/Sao_Paulo", -22.9068, -43.1729},
	{"181.41.206.10", "Colombia", "CO", "Bogota", "Bogota", "Telmex Colombia", "AS14080", "America/Bogota", 4.711, -74.0721},
	{"200.48.0.5", "Peru", "PE", "Lima", "Lima", "Telefonica del Peru", "AS6147", "America/Lima", -12.0464, -77.0428},
	{"190.131.0.10", "Chile", "CL", "Santiago", "Santiago", "VTR", "AS22047", "America/Santiago", -33.4489, -70.6693},
	{"186.2.0.5", "Ecuador", "EC", "Pichincha", "Quito", "CNT", "AS27947", "America/Guayaquil", -0.1807, -78.4678},
	{"200.35.0.10", "Venezuela", "VE", "Caracas", "Caracas", "CANTV", "AS8048", "America/Caracas", 10.4806, -66.9036},
	// ── 中美/加勒比（4）──
	{"200.52.0.5", "Costa Rica", "CR", "San Jose", "San Jose", "ICE", "AS11830", "America/Costa_Rica", 9.9281, -84.0907},
	{"168.243.0.10", "Panama", "PA", "Panama", "Panama City", "Cable & Wireless", "AS11556", "America/Panama", 8.9824, -79.5199},
	{"201.220.0.5", "Guatemala", "GT", "Guatemala", "Guatemala City", "Tigo Guatemala", "AS14754", "America/Guatemala", 14.6349, -90.5069},
	{"196.3.0.10", "Jamaica", "JM", "Kingston", "Kingston", "FLOW Jamaica", "AS15146", "America/Jamaica", 18.0179, -76.8099},
	// ── 中亚/高加索（3）──
	{"46.34.128.10", "Kazakhstan", "KZ", "Almaty", "Almaty", "Kazakhtelecom", "AS9198", "Asia/Almaty", 43.2551, 76.9126},
	{"31.148.0.5", "Georgia", "GE", "Tbilisi", "Tbilisi", "Magticom", "AS16010", "Asia/Tbilisi", 41.7151, 44.8271},
	{"37.252.0.10", "Azerbaijan", "AZ", "Baku", "Baku", "Delta Telecom", "AS29049", "Asia/Baku", 40.4093, 49.8671},
	// ── 本地 IP（无坐标）──
	{"127.0.0.1", "", "", "", "", "", "", "", 0, 0},
	{"::1", "", "", "", "", "", "", "", 0, 0},
	{"192.168.1.100", "", "", "", "", "", "", "", 0, 0},
	{"10.0.0.5", "", "", "", "", "", "", "", 0, 0},
	{"172.16.0.1", "", "", "", "", "", "", "", 0, 0},
	{"192.168.0.50", "", "", "", "", "", "", "", 0, 0},
	{"10.10.10.10", "", "", "", "", "", "", "", 0, 0},
	{"fd00::1", "", "", "", "", "", "", "", 0, 0},
}

var paths = []string{
	"/.git/config", "/.git/HEAD", "/.git/objects", "/.env", "/.env.local", "/.env.production", "/.env.backup",
	"/wp-admin/", "/wp-admin/install.php", "/wp-login.php", "/wp-content/uploads",
	"/vendor/phpunit", "/etc/passwd", "/proc/self/environ",
	"/api/auth/login", "/api/auth/register", "/api/auth/reset-password", "/api/auth/2fa/verify",
	"/api/user/info", "/api/user/settings", "/api/admin/users", "/api/admin/system", "/api/admin/system/settings",
	"/api/search", "/api/app/public", "/api/email/send-code",
	"/xmlrpc.php", "/administrator/", "/phpmyadmin/", "/cgi-bin/",
}

var reasons = []rDef{
	{"blocked_signature", 403, 40395, "high", nil, "", ""},
	{"blocked_signature", 403, 40395, "high", nil, "", ""},
	{"blocked_signature", 403, 40395, "high", nil, "", ""},
	{"blocked_path", 403, 40395, "high", nil, "", ""},
	{"blocked_path", 403, 40395, "high", nil, "", ""},
	{"blocked_user_agent", 403, 40394, "medium", nil, "", ""},
	{"blocked_user_agent", 403, 40394, "medium", nil, "", ""},
	{"rate_limited", 429, 42900, "medium", nil, "", ""},
	{"rate_limited", 429, 42900, "medium", nil, "", ""},
	{"rate_limited", 429, 42900, "medium", nil, "", ""},
	{"blocked_method", 501, 50190, "low", nil, "", ""},
	{"blocked_cidr", 403, 40391, "medium", nil, "", ""},
	{"path_too_long", 403, 40392, "low", nil, "", ""},
	{"query_too_long", 403, 40393, "low", nil, "", ""},
	{"waf_blocked", 403, 40396, "critical", ip(941100), "block", "XSS Attack Detected"},
	{"waf_blocked", 403, 40396, "critical", ip(942100), "block", "SQL Injection Attack"},
	{"waf_blocked", 403, 40396, "critical", ip(949110), "block", "Inbound Anomaly Score Exceeded"},
	{"waf_blocked", 403, 40396, "critical", ip(941160), "block", "NoScript XSS InjectionChecker"},
	{"waf_blocked", 403, 40396, "critical", ip(932100), "block", "Remote Command Execution"},
	{"banned_ip", 403, 40397, "high", nil, "", ""},
}

var uas = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
	"curl/7.68", "curl/8.0", "curl/7.81", "curl/7.88",
	"sqlmap/1.6", "sqlmap/1.7", "sqlmap/1.8",
	"nikto/2.1", "nikto/2.1.6",
	"nessus/10.0", "nessus/10.4",
	"acunetix-scanner", "dirsearch/0.4", "gobuster/3.5", "ffuf/2.0",
	"python-requests/2.28", "python-requests/2.31",
	"Go-http-client/1.1", "Go-http-client/2.0",
	"Wget/1.21", "masscan/1.3",
}

var qs = []string{
	"", "", "", "", "", "", "", // 70% 无参数
	"id=1", "q=test", "page=1", "limit=100",
	"q=union+select+1,2,3", "q=<script>alert(1)</script>",
	"file=../../etc/passwd", "cmd=sleep(5)",
	"id=1 OR 1=1", "search=benchmark(10000000,md5(1))",
	"url=javascript:alert(1)", "callback=eval(atob(payload))",
}

func main() {
	const totalRows = 5000

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://aegis:aegis@127.0.0.1:5432/aegis?sslmode=disable"
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Println("connect error:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	// 先清除旧 mock 数据
	tag, _ := conn.Exec(ctx, "DELETE FROM firewall_logs WHERE request_id LIKE 'mock-%'")
	fmt.Printf("cleaned %d old mock rows\n", tag.RowsAffected())

	rng := rand.New(rand.NewSource(42))
	now := time.Now().UTC()

	// 分批插入（每批 500 行）
	const batchSize = 500
	inserted := int64(0)

	for batch := 0; batch < totalRows; batch += batchSize {
		end := batch + batchSize
		if end > totalRows {
			end = totalRows
		}
		count := end - batch

		var sb strings.Builder
		sb.WriteString(`INSERT INTO firewall_logs (
			request_id, ip, method, path, query_string, user_agent, headers,
			reason, http_status, response_code,
			waf_rule_id, waf_action, waf_data,
			country, country_code, region, city, isp, asn, timezone,
			latitude, longitude, severity, blocked_at
		) VALUES `)

		methods := []string{"GET", "GET", "GET", "GET", "POST", "POST", "POST", "PUT", "DELETE", "TRACE", "CONNECT"}

		for i := 0; i < count; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			idx := batch + i
			s := sources[rng.Intn(len(sources))]
			r := reasons[rng.Intn(len(reasons))]
			p := paths[rng.Intn(len(paths))]
			m := methods[rng.Intn(len(methods))]
			ua := uas[rng.Intn(len(uas))]
			q := qs[rng.Intn(len(qs))]

			// 时间：最近 72 小时随机分布，近期密度更高
			hoursAgo := rng.Float64() * rng.Float64() * 72 // 平方分布，近期更密集
			blockedAt := now.Add(-time.Duration(hoursAgo*3600) * time.Second)

			isLocal := s.lat == 0 && s.lng == 0
			latStr, lngStr := "NULL", "NULL"
			if !isLocal {
				latStr = fmt.Sprintf("%.4f", s.lat+(rng.Float64()*0.1-0.05))
				lngStr = fmt.Sprintf("%.4f", s.lng+(rng.Float64()*0.1-0.05))
			}

			wafStr := "NULL"
			if r.wafID != nil {
				wafStr = fmt.Sprintf("%d", *r.wafID)
			}

			// 转义单引号
			escUA := strings.ReplaceAll(ua, "'", "''")
			q = strings.ReplaceAll(q, "'", "''")

			sb.WriteString(fmt.Sprintf(
				`('mock-%d','%s','%s','%s','%s','%s','{}','%s',%d,%d,%s,'%s','%s','%s','%s','%s','%s','%s','%s','%s',%s,%s,'%s','%s')`,
				idx, s.ip, m, p, q, escUA,
				r.reason, r.status, r.code,
				wafStr, r.wafAct, r.wafMsg,
				s.country, s.cc, s.region, s.city, s.isp, s.asn, s.tz,
				latStr, lngStr, r.severity,
				blockedAt.Format("2006-01-02 15:04:05-07:00"),
			))
		}
		sb.WriteString(";")

		t, err := conn.Exec(ctx, sb.String())
		if err != nil {
			fmt.Printf("batch %d exec error: %v\n", batch/batchSize, err)
			os.Exit(1)
		}
		inserted += t.RowsAffected()
		fmt.Printf("batch %d/%d: +%d rows\n", batch/batchSize+1, (totalRows+batchSize-1)/batchSize, t.RowsAffected())
	}

	fmt.Printf("\ntotal inserted: %d mock firewall log rows\n", inserted)
	fmt.Printf("sources: %d IPs across %d+ countries\n", len(sources), 50)
}
