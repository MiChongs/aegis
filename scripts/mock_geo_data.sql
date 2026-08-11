-- ════════════════════════════════════════════════════════════════════════
--  Aegis 地理风控演示数据（四表一体）
--
--  覆盖前端面板：
--    · 安全 → 地理风控 → 热力分析   ← geo_stats_hourly（block/login）
--    · 安全 → 地理风控 → 围栏管理   ← geo_fences
--    · 安全 → 地理风控 → 轨迹回放   ← login_geo_events
--    · 安全 → 防火墙 → 拦截日志/攻击飞线图 ← firewall_logs
--
--  特性：
--    · 幂等可重跑——以 request_id='mock-%' / device_id='mock-%' / name='[MOCK]%'
--      为标记，重跑前自动清理；geo_stats_hourly 仅清理最近 31 天窗口
--    · 全部经 PostGIS 服务端 generate_series 生成，文件精简
--    · 依赖：已执行 migrate（含 000054 geo_analytics）、PostGIS 扩展就绪
--
--  导入：
--    psql postgresql://aegis:aegis@127.0.0.1:15432/aegis -f scripts/mock_geo_data.sql
--  或（容器内）：
--    docker exec -i aegis-postgres psql postgresql://aegis:aegis@127.0.0.1:5432/aegis \
--      < scripts/mock_geo_data.sql
-- ════════════════════════════════════════════════════════════════════════

\set ON_ERROR_STOP on
BEGIN;

-- ── 0. 幂等清理 ──────────────────────────────────────────────────────────
DELETE FROM firewall_logs   WHERE request_id LIKE 'mock-%';
DELETE FROM login_geo_events WHERE device_id LIKE 'mock-%';
DELETE FROM geo_fences      WHERE name LIKE '[MOCK]%';
DELETE FROM geo_stats_hourly WHERE bucket >= date_trunc('hour', NOW()) - INTERVAL '31 days';

-- 确保登录事件分区存在（覆盖近 3 个月）。
-- 容错：分区已存在、或其月份范围已被既有分区覆盖时会抛 overlap/duplicate，
-- 这里逐月吞掉异常继续——既有分区仍能正常承接插入，不应中断整个脚本。
DO $partitions$
DECLARE
  m INT;
BEGIN
  FOR m IN 0..2 LOOP
    BEGIN
      PERFORM ensure_login_geo_partition((date_trunc('month', NOW()) - (m || ' month')::interval)::date);
    EXCEPTION WHEN others THEN
      RAISE NOTICE '月份 -% 分区已存在或被覆盖，跳过（%）', m, SQLERRM;
    END;
  END LOOP;
END
$partitions$;

-- ════════════════════════════════════════════════════════════════════════
--  1. 地理围栏（geo_fences）—— 4 条，覆盖 deny / allow / review + 圆形/多边形
-- ════════════════════════════════════════════════════════════════════════
INSERT INTO geo_fences (app_id, name, mode, fence, center, radius_m, ban_mode, reason, enabled, expires_at, match_count, last_match_at, created_at, updated_at)
VALUES
  -- 多边形 deny：俄罗斯西部高风险区
  (NULL, '[MOCK] 高危地区拦截·俄西', 'deny',
   ST_Multi(ST_GeomFromText('POLYGON((28 54, 45 54, 45 62, 28 62, 28 54))', 4326))::geography,
   NULL, NULL, 'forbidden', '高频暴破来源区域', TRUE, NULL, 4127, NOW() - INTERVAL '12 minutes',
   NOW() - INTERVAL '20 days', NOW() - INTERVAL '12 minutes'),

  -- 圆形 allow：公司总部 50km 白名单（仅此范围内允许管理员登录）
  (NULL, '[MOCK] 总部白名单·北京', 'allow',
   NULL, ST_SetSRID(ST_MakePoint(116.4074, 39.9042), 4326)::geography, 50000, '', '仅允许总部网络访问管理后台', TRUE, NULL,
   18234, NOW() - INTERVAL '3 minutes', NOW() - INTERVAL '45 days', NOW() - INTERVAL '3 minutes'),

  -- 多边形 review：东南亚观察区（只记录不拦截）
  (NULL, '[MOCK] 观察区·东南亚', 'review',
   ST_Multi(ST_GeomFromText('POLYGON((95 -10, 130 -10, 130 25, 95 25, 95 -10))', 4326))::geography,
   NULL, NULL, '', '灰度评估：新增登录来源观察', TRUE, NULL, 932, NOW() - INTERVAL '1 hour',
   NOW() - INTERVAL '8 days', NOW() - INTERVAL '1 hour'),

  -- 圆形 deny（已禁用）：演示禁用态虚线渲染
  (NULL, '[MOCK] 临时封锁·已停用', 'deny',
   NULL, ST_SetSRID(ST_MakePoint(-74.0060, 40.7128), 4326)::geography, 80000, 'tarpit', '事件已结束，规则保留', FALSE, NULL,
   56, NOW() - INTERVAL '5 days', NOW() - INTERVAL '15 days', NOW() - INTERVAL '5 days');

-- ════════════════════════════════════════════════════════════════════════
--  2. 防火墙拦截日志（firewall_logs）—— 攻击飞线图 / 拦截日志面板
--     约 600 条，30 个全球攻击源，近 7 天分布，reason/severity 加权随机
-- ════════════════════════════════════════════════════════════════════════
WITH sources(ip, country, cc, region, city, isp, asn, tz, lat, lng, vol) AS (VALUES
  ('45.33.32.156','United States','US','California','Los Angeles','Linode','AS63949','America/Los_Angeles',34.0522,-118.2437,28),
  ('104.131.175.196','United States','US','New York','New York','DigitalOcean','AS14061','America/New_York',40.7128,-74.0060,24),
  ('23.94.128.11','United States','US','Illinois','Chicago','ColoCrossing','AS36352','America/Chicago',41.8781,-87.6298,18),
  ('198.51.100.42','United States','US','Texas','Dallas','AWS','AS16509','America/Chicago',32.7767,-96.7970,16),
  ('185.220.101.34','Russia','RU','Moscow','Moscow','DataLine','AS39134','Europe/Moscow',55.7558,37.6173,40),
  ('91.243.85.67','Russia','RU','Saint Petersburg','Saint Petersburg','Selectel','AS49505','Europe/Moscow',59.9343,30.3351,30),
  ('218.75.176.20','China','CN','Zhejiang','Hangzhou','China Telecom','AS4134','Asia/Shanghai',30.2741,120.1551,22),
  ('114.114.114.114','China','CN','Jiangsu','Nanjing','China Telecom','AS4134','Asia/Shanghai',32.0603,118.7969,14),
  ('36.99.136.210','China','CN','Beijing','Beijing','China Unicom','AS4837','Asia/Shanghai',39.9042,116.4074,20),
  ('136.243.44.11','Germany','DE','Saxony','Falkenstein','Hetzner','AS24940','Europe/Berlin',50.4779,12.3713,18),
  ('5.9.61.200','Germany','DE','Bavaria','Nuremberg','Hetzner','AS24940','Europe/Berlin',49.4521,11.0767,16),
  ('89.248.167.131','Netherlands','NL','North Holland','Amsterdam','DigitalOcean','AS14061','Europe/Amsterdam',52.3676,4.9041,20),
  ('177.71.208.100','Brazil','BR','Sao Paulo','Sao Paulo','Locaweb','AS27715','America/Sao_Paulo',-23.5505,-46.6333,18),
  ('153.126.203.4','Japan','JP','Tokyo','Tokyo','SAKURA Internet','AS9370','Asia/Tokyo',35.6762,139.6503,14),
  ('103.99.170.50','India','IN','Maharashtra','Mumbai','HostGator','AS133229','Asia/Kolkata',19.0760,72.8777,22),
  ('51.15.112.80','United Kingdom','GB','England','London','Scaleway','AS12876','Europe/London',51.5074,-0.1278,16),
  ('211.49.46.20','South Korea','KR','Seoul','Seoul','Korea Telecom','AS4766','Asia/Seoul',37.5665,126.9780,18),
  ('128.199.159.40','Singapore','SG','Singapore','Singapore','DigitalOcean','AS14061','Asia/Singapore',1.3521,103.8198,14),
  ('192.99.14.81','Canada','CA','Quebec','Montreal','OVH','AS16276','America/Toronto',45.5017,-73.5673,12),
  ('163.172.67.180','France','FR','Ile-de-France','Paris','Scaleway','AS12876','Europe/Paris',48.8566,2.3522,18),
  ('41.76.108.46','South Africa','ZA','Gauteng','Johannesburg','RSAWEB','AS37153','Africa/Johannesburg',-26.2041,28.0473,10),
  ('91.234.33.17','Ukraine','UA','Kyiv','Kyiv','Hetzner Ukraine','AS213230','Europe/Kyiv',50.4501,30.5234,24),
  ('103.22.200.7','Australia','AU','New South Wales','Sydney','Cloudflare','AS13335','Australia/Sydney',-33.8688,151.2093,10),
  ('187.174.252.10','Mexico','MX','CDMX','Mexico City','Telmex','AS8151','America/Mexico_City',19.4326,-99.1332,12),
  ('190.2.148.90','Argentina','AR','Buenos Aires','Buenos Aires','Telecom Argentina','AS22927','America/Argentina/Buenos_Aires',-34.6037,-58.3816,10),
  ('46.29.248.100','Poland','PL','Masovia','Warsaw','DigitalOcean','AS14061','Europe/Warsaw',52.2297,21.0122,14),
  ('89.46.100.50','Romania','RO','Bucharest','Bucharest','M247','AS9009','Europe/Bucharest',44.4268,26.1025,16),
  ('195.54.160.21','Iran','IR','Tehran','Tehran','RESPINA','AS197207','Asia/Tehran',35.6892,51.3890,18),
  ('14.225.17.9','Vietnam','VN','Hanoi','Hanoi','FPT Telecom','AS18403','Asia/Ho_Chi_Minh',21.0278,105.8342,16),
  ('105.112.20.7','Nigeria','NG','Lagos','Lagos','MTN','AS29465','Africa/Lagos',6.5244,3.3792,10)
),
reasons(reason, status, code, severity, waf_id, waf_act, waf_data, method, path, query, ua, wt) AS (VALUES
  ('blocked_signature',403,40395,'high',  NULL,    '',     '',                      'GET', '/.git/config',        '',                          'Mozilla/5.0',      25),
  ('blocked_path',     403,40395,'high',  NULL,    '',     '',                      'GET', '/.env',               '',                          'Mozilla/5.0',      15),
  ('blocked_user_agent',403,40394,'medium',NULL,   '',     '',                      'GET', '/wp-admin/',          '',                          'sqlmap/1.6',       16),
  ('rate_limited',     429,42900,'medium',NULL,    '',     '',                      'POST','/api/auth/login',     '',                          'Mozilla/5.0',      30),
  ('waf_blocked',      403,40396,'critical',942100,'block','SQL Injection Attack',  'POST','/api/search',         'id=1+union+select+1,2,3',   'Mozilla/5.0',       8),
  ('blocked_method',   501,50190,'low',   NULL,    '',     '',                      'TRACE','/api/app/public',    '',                          'curl/7.68',         4),
  ('banned_ip',        403,40397,'high',  NULL,    '',     '',                      'GET', '/api/user/info',      '',                          'Mozilla/5.0',       2)
)
INSERT INTO firewall_logs (
  request_id, ip, method, path, query_string, user_agent, headers,
  reason, http_status, response_code, waf_rule_id, waf_action, waf_data,
  country, country_code, region, city, isp, asn, timezone, latitude, longitude, severity, blocked_at
)
SELECT
  'mock-' || s.cc || '-' || g.n || '-' || floor(random()*1000000)::text,
  s.ip, r.method, r.path, r.query, r.ua, '{}',
  r.reason, r.status, r.code, r.waf_id, r.waf_act, r.waf_data,
  s.country, s.cc, s.region, s.city, s.isp, s.asn, s.tz,
  s.lat + (random()-0.5)*0.04, s.lng + (random()-0.5)*0.04,   -- 网格内轻微散布
  r.severity,
  -- 近期加权（power 偏向 0），集中在最近 ~40h 内，确保 24h 默认窗口地图够密
  NOW() - (power(random(), 2) * INTERVAL '40 hours')
FROM sources s
CROSS JOIN LATERAL generate_series(1, s.vol) g(n)
CROSS JOIN LATERAL (
  -- Efraimidis–Spirakis 加权随机：每行重新抽一种 reason
  SELECT * FROM reasons ORDER BY power(random(), 1.0 / wt) DESC LIMIT 1
) r;

-- 集中式限流暴破源：3 个 IP 近期密集 rate_limited（作为 TOP IPS 榜首与近期突发）。
-- 量级刻意控制在 72 行，避免占满日志接口 200 条的分页上限而淹没地图的地理多样性。
-- 注：自动封禁(rate_limit_abuse)由 Worker 在实时拦截事件上触发，历史 mock 行不会触发实时封禁，
--     该规则逻辑由单元测试覆盖（internal/service/ip_ban_auto_rules_test.go）。
INSERT INTO firewall_logs (
  request_id, ip, method, path, query_string, user_agent, headers,
  reason, http_status, response_code, waf_rule_id, waf_action, waf_data,
  country, country_code, region, city, isp, asn, timezone, latitude, longitude, severity, blocked_at
)
SELECT
  'mock-brute-' || b.ip || '-' || g.n,
  b.ip, 'POST', '/api/auth/login', '', 'python-requests/2.31', '{}',
  'rate_limited', 429, 42900, NULL, '', '',
  b.country, b.cc, b.region, b.city, b.isp, b.asn, b.tz, b.lat, b.lng, 'medium',
  NOW() - (random() * INTERVAL '25 minutes')
FROM (VALUES
  ('45.155.205.99','Russia','RU','Moscow','Moscow','Stark Industries','AS44477','Europe/Moscow',55.7558,37.6173),
  ('193.142.146.35','Netherlands','NL','North Holland','Amsterdam','IP Volume','AS202425','Europe/Amsterdam',52.3676,4.9041),
  ('167.94.138.60','United States','US','Michigan','Ann Arbor','Censys','AS398324','America/Detroit',42.2808,-83.7430)
) AS b(ip,country,cc,region,city,isp,asn,tz,lat,lng)
CROSS JOIN LATERAL generate_series(1, 24) g(n);

-- ════════════════════════════════════════════════════════════════════════
--  3. 地理统计小时汇总（geo_stats_hourly）—— 热力分析面板唯一数据源
--     近 30 天 × 城市 × kind(block/login)，按 geohash5 网格，近期加权
-- ════════════════════════════════════════════════════════════════════════
WITH cities(cc, city, lat, lng, block_base, login_base) AS (VALUES
  -- 攻击高发（block 权重高）
  ('RU','Moscow',           55.7558, 37.6173, 90, 12),
  ('CN','Hangzhou',         30.2741,120.1551, 70, 40),
  ('US','Los Angeles',      34.0522,-118.2437,65, 55),
  ('US','New York',         40.7128,-74.0060, 60, 70),
  ('NL','Amsterdam',        52.3676,  4.9041, 55, 20),
  ('DE','Falkenstein',      50.4779, 12.3713, 50, 10),
  ('UA','Kyiv',             50.4501, 30.5234, 45, 12),
  ('IN','Mumbai',           19.0760, 72.8777, 50, 60),
  ('BR','Sao Paulo',       -23.5505,-46.6333, 40, 45),
  ('IR','Tehran',           35.6892, 51.3890, 42,  8),
  ('VN','Hanoi',            21.0278,105.8342, 38, 18),
  -- 正常用户高发（login 权重高）
  ('CN','Beijing',          39.9042,116.4074, 30,120),
  ('CN','Shanghai',         31.2304,121.4737, 25,110),
  ('CN','Shenzhen',         22.5431,114.0579, 22, 95),
  ('JP','Tokyo',            35.6762,139.6503, 28, 80),
  ('KR','Seoul',            37.5665,126.9780, 24, 65),
  ('GB','London',           51.5074, -0.1278, 30, 50),
  ('SG','Singapore',         1.3521,103.8198, 20, 55),
  ('FR','Paris',            48.8566,  2.3522, 26, 48),
  ('AU','Sydney',          -33.8688,151.2093, 14, 40)
)
INSERT INTO geo_stats_hourly (bucket, kind, country_code, city, geohash5, lat, lng, cnt)
SELECT
  b.bucket,
  k.kind,
  c.cc,
  c.city,
  substring(ST_GeoHash(ST_SetSRID(ST_MakePoint(c.lng, c.lat), 4326), 5) for 8),
  c.lat,
  c.lng,
  GREATEST(1, round(
    (CASE WHEN k.kind = 'block' THEN c.block_base ELSE c.login_base END)
    * exp(-EXTRACT(EPOCH FROM (NOW() - b.bucket)) / 86400.0 / 18.0)   -- 18 天半衰，近期更密
    * (0.35 + random() * 1.1)                                         -- 随机波动
    * (0.6 + 0.4 * sin(EXTRACT(HOUR FROM b.bucket) / 24.0 * 2 * pi())) -- 昼夜节律
  ))::bigint
FROM cities c
CROSS JOIN generate_series(
  date_trunc('hour', NOW()) - INTERVAL '30 days',
  date_trunc('hour', NOW()),
  INTERVAL '1 hour'
) AS b(bucket)
CROSS JOIN (VALUES ('block'), ('login')) AS k(kind)
WHERE random() < 0.55   -- 网格稀疏化，避免每小时每城都满格
ON CONFLICT (bucket, kind, country_code, city, geohash5) DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════
--  4. 登录地理事件（login_geo_events）—— 轨迹回放面板
--     5 个演示用户，含 1 个「不可能旅行」异常序列
-- ════════════════════════════════════════════════════════════════════════
-- app_id 绑定到现存的第一个应用（无应用时回退 10000）
INSERT INTO login_geo_events (user_id, app_id, ip, country_code, country, region, city, asn, isp, geom, login_type, device_id, created_at)
SELECT
  e.user_id,
  COALESCE((SELECT min(id) FROM apps), 10000),
  e.ip::inet, e.cc, e.country, e.region, e.city, e.asn, e.isp,
  ST_SetSRID(ST_MakePoint(e.lng, e.lat), 4326)::geography,
  e.login_type, 'mock-dev-' || e.user_id,
  NOW() - (e.mins_ago * sc.scale * INTERVAL '1 minute')
FROM (VALUES
  -- 用户 90001：北京常驻用户，全部本地登录（无异常）
  (90001,'123.118.10.5','CN','China','Beijing','Beijing','AS4808','China Unicom',39.91,116.40,'password', 27000),
  (90001,'123.118.10.5','CN','China','Beijing','Beijing','AS4808','China Unicom',39.92,116.41,'password', 24000),
  (90001,'123.118.11.8','CN','China','Beijing','Beijing','AS4808','China Unicom',39.90,116.39,'password', 20000),
  (90001,'123.118.11.8','CN','China','Beijing','Beijing','AS4808','China Unicom',39.93,116.42,'oauth_wechat', 15000),
  (90001,'123.118.12.2','CN','China','Beijing','Beijing','AS4808','China Unicom',39.91,116.40,'password', 9000),
  (90001,'123.118.12.2','CN','China','Beijing','Beijing','AS4808','China Unicom',39.92,116.41,'password', 2000),
  -- 用户 90002：合理的商务出行 北京→上海→东京（时间充裕）
  (90002,'101.80.20.10','CN','China','Shanghai','Shanghai','AS4812','China Telecom',31.23,121.47,'password', 30000),
  (90002,'101.80.20.10','CN','China','Shanghai','Shanghai','AS4812','China Telecom',31.22,121.46,'password', 28800),
  (90002,'124.83.5.1','CN','China','Beijing','Beijing','AS4808','China Unicom',39.90,116.40,'password', 18000),
  (90002,'124.83.5.1','CN','China','Beijing','Beijing','AS4808','China Unicom',39.91,116.41,'password', 17000),
  (90002,'126.0.1.9','JP','Japan','Tokyo','Tokyo','AS2516','KDDI',35.68,139.69,'password', 6000),
  (90002,'126.0.1.9','JP','Japan','Tokyo','Tokyo','AS2516','KDDI',35.69,139.70,'oauth_google', 1500),
  -- 用户 90003：⚠ 不可能旅行——北京登录后 30 分钟纽约登录
  (90003,'123.115.8.20','CN','China','Beijing','Beijing','AS4808','China Unicom',39.90,116.40,'password', 5000),
  (90003,'123.115.8.20','CN','China','Beijing','Beijing','AS4808','China Unicom',39.91,116.41,'password', 4200),
  (90003,'74.125.0.5','US','United States','New York','New York','AS15169','Google',40.71,-74.00,'password', 4170),  -- 30 分钟后纽约
  (90003,'74.125.0.5','US','United States','New York','New York','AS15169','Google',40.72,-74.01,'password', 4100),
  (90003,'185.60.216.1','GB','United Kingdom','London','London','AS32934','Facebook',51.50,-0.12,'password', 4080),  -- 20 分钟后伦敦
  (90003,'123.115.8.20','CN','China','Beijing','Beijing','AS4808','China Unicom',39.90,116.40,'password', 300),
  -- 用户 90004：东南亚多城活动（落在 review 围栏内）
  (90004,'103.6.150.2','SG','Singapore','Singapore','Singapore','AS9506','Singtel',1.35,103.82,'password', 26000),
  (90004,'202.158.5.9','ID','Indonesia','Jakarta','Jakarta','AS7713','Telkom',-6.21,106.85,'password', 19000),
  (90004,'27.131.10.4','MY','Malaysia','Kuala Lumpur','Kuala Lumpur','AS4788','TM',3.14,101.69,'password', 12000),
  (90004,'171.96.1.7','TH','Thailand','Bangkok','Bangkok','AS3320','TOT',13.75,100.50,'oauth_google', 4000),
  -- 用户 90005：欧美跨洲（间隔合理）
  (90005,'51.140.0.3','GB','United Kingdom','London','London','AS8075','Microsoft',51.51,-0.13,'password', 28000),
  (90005,'13.107.0.9','US','United States','San Francisco','San Francisco','AS8075','Microsoft',37.77,-122.42,'password', 14000),
  (90005,'52.95.1.4','US','United States','Seattle','Seattle','AS16509','Amazon',47.61,-122.33,'password', 3000)
) AS e(user_id, ip, cc, country, region, city, asn, isp, lat, lng, login_type, mins_ago)
CROSS JOIN (
  -- 把整条时间轴等比压缩进「当月已过去时长 × 0.85」内：
  -- 确保最老的事件（mins_ago=30000）也落在当前月分区内，绕开历史分区空洞；
  -- 等比缩放保留事件相对顺序，并让「不可能旅行」的时间差更紧凑（更易触发标红）。
  SELECT LEAST(
    1.0,
    (EXTRACT(EPOCH FROM (NOW() - date_trunc('month', NOW()))) / 60.0 * 0.85) / 30000.0
  )::double precision AS scale
) sc;

COMMIT;

-- ── 摘要：导入结果 + 轨迹回放可查询的演示账号 ──────────────────────────────
SELECT 'firewall_logs (mock)'  AS table, count(*) AS rows FROM firewall_logs WHERE request_id LIKE 'mock-%'
UNION ALL SELECT 'geo_stats_hourly (30d)', count(*) FROM geo_stats_hourly WHERE bucket >= NOW() - INTERVAL '31 days'
UNION ALL SELECT 'login_geo_events (mock)', count(*) FROM login_geo_events WHERE device_id LIKE 'mock-%'
UNION ALL SELECT 'geo_fences (mock)',      count(*) FROM geo_fences WHERE name LIKE '[MOCK]%';

SELECT '轨迹回放演示：appId=' || COALESCE((SELECT min(id)::text FROM apps), '10000')
       || ' / userId ∈ {90001 正常, 90002 商务出行, 90003 ⚠不可能旅行, 90004 东南亚, 90005 欧美}' AS hint;
