-- Mock firewall log data for UI testing
-- 40+ records across 15+ countries, all severity levels, all reason types

INSERT INTO firewall_logs (
  request_id, ip, method, path, query_string, user_agent, headers,
  reason, http_status, response_code,
  waf_rule_id, waf_action, waf_data,
  country, country_code, region, city, isp, asn, timezone,
  latitude, longitude, severity, blocked_at
) VALUES
-- === United States (5 IPs) ===
('mock-us-01','45.33.32.156','GET','/.git/config','','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','United States','US','California','Los Angeles','Linode','AS63949','America/Los_Angeles',34.0522,-118.2437,'high',NOW() - interval '2 minutes'),
('mock-us-02','45.33.32.156','GET','/.env','','Mozilla/5.0','{}','blocked_path',403,40395,NULL,'','','United States','US','California','Los Angeles','Linode','AS63949','America/Los_Angeles',34.0522,-118.2437,'high',NOW() - interval '5 minutes'),
('mock-us-03','104.131.175.196','POST','/api/auth/login','q=union+select+1,2,3','sqlmap/1.6','{}','blocked_user_agent',403,40394,NULL,'','','United States','US','New York','New York','DigitalOcean','AS14061','America/New_York',40.7128,-74.0060,'medium',NOW() - interval '8 minutes'),
('mock-us-04','104.131.175.196','GET','/api/user/info','id=1 OR 1=1','sqlmap/1.6','{}','blocked_user_agent',403,40394,NULL,'','','United States','US','New York','New York','DigitalOcean','AS14061','America/New_York',40.7128,-74.0060,'medium',NOW() - interval '10 minutes'),
('mock-us-05','198.51.100.42','GET','/wp-admin/','','nikto/2.1','{}','blocked_user_agent',403,40394,NULL,'','','United States','US','Texas','Dallas','AWS','AS16509','America/Chicago',32.7767,-96.7970,'medium',NOW() - interval '12 minutes'),
('mock-us-06','23.94.128.11','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','United States','US','Illinois','Chicago','ColoCrossing','AS36352','America/Chicago',41.8781,-87.6298,'medium',NOW() - interval '1 minute'),
('mock-us-07','23.94.128.11','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','United States','US','Illinois','Chicago','ColoCrossing','AS36352','America/Chicago',41.8781,-87.6298,'medium',NOW() - interval '2 minutes'),
('mock-us-08','23.94.128.11','GET','/.git/HEAD','','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','United States','US','Illinois','Chicago','ColoCrossing','AS36352','America/Chicago',41.8781,-87.6298,'high',NOW() - interval '3 minutes'),
('mock-us-09','67.205.167.33','GET','/api/search','benchmark(10000000,md5(1))','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','United States','US','California','San Francisco','DigitalOcean','AS14061','America/Los_Angeles',37.7749,-122.4194,'high',NOW() - interval '55 minutes'),

-- === Russia (2 IPs) ===
('mock-ru-01','185.220.101.34','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','Russia','RU','Moscow','Moscow','DataLine','AS39134','Europe/Moscow',55.7558,37.6173,'medium',NOW() - interval '1 minute'),
('mock-ru-02','185.220.101.34','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','Russia','RU','Moscow','Moscow','DataLine','AS39134','Europe/Moscow',55.7558,37.6173,'medium',NOW() - interval '3 minutes'),
('mock-ru-03','185.220.101.34','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','Russia','RU','Moscow','Moscow','DataLine','AS39134','Europe/Moscow',55.7558,37.6173,'medium',NOW() - interval '4 minutes'),
('mock-ru-04','91.243.85.67','GET','/etc/passwd','','curl/7.68','{}','blocked_signature',403,40395,NULL,'','','Russia','RU','Saint Petersburg','Saint Petersburg','Selectel','AS49505','Europe/Moscow',59.9343,30.3351,'high',NOW() - interval '15 minutes'),
('mock-ru-05','91.243.85.67','GET','/proc/self/environ','','curl/7.68','{}','blocked_signature',403,40395,NULL,'','','Russia','RU','Saint Petersburg','Saint Petersburg','Selectel','AS49505','Europe/Moscow',59.9343,30.3351,'high',NOW() - interval '16 minutes'),

-- === China (3 IPs) ===
('mock-cn-01','218.75.176.20','GET','/api/search','q=xss','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','China','CN','Zhejiang','Hangzhou','China Telecom','AS4134','Asia/Shanghai',30.2741,120.1551,'high',NOW() - interval '6 minutes'),
('mock-cn-02','218.75.176.20','POST','/api/auth/login','','Mozilla/5.0','{}','waf_blocked',403,40396,941100,'block','XSS Attack Detected','China','CN','Zhejiang','Hangzhou','China Telecom','AS4134','Asia/Shanghai',30.2741,120.1551,'critical',NOW() - interval '7 minutes'),
('mock-cn-03','114.114.114.114','TRACE','/api/app/public','','curl/7.81','{}','blocked_method',501,50190,NULL,'','','China','CN','Jiangsu','Nanjing','China Telecom','AS4134','Asia/Shanghai',32.0603,118.7969,'low',NOW() - interval '20 minutes'),
('mock-cn-04','36.99.136.210','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','China','CN','Beijing','Beijing','China Unicom','AS4837','Asia/Shanghai',39.9042,116.4074,'medium',NOW() - interval '58 minutes'),

-- === Germany (2 IPs) ===
('mock-de-01','136.243.44.11','GET','/.git/HEAD','','acunetix-scanner','{}','blocked_user_agent',403,40394,NULL,'','','Germany','DE','Saxony','Falkenstein','Hetzner','AS24940','Europe/Berlin',50.4779,12.3713,'medium',NOW() - interval '9 minutes'),
('mock-de-02','136.243.44.11','POST','/api/auth/login','','acunetix-scanner','{}','blocked_user_agent',403,40394,NULL,'','','Germany','DE','Saxony','Falkenstein','Hetzner','AS24940','Europe/Berlin',50.4779,12.3713,'medium',NOW() - interval '11 minutes'),
('mock-de-03','5.9.61.200','GET','/vendor/phpunit','','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','Germany','DE','Bavaria','Nuremberg','Hetzner','AS24940','Europe/Berlin',49.4521,11.0767,'high',NOW() - interval '18 minutes'),

-- === Brazil ===
('mock-br-01','177.71.208.100','POST','/api/auth/login','','Mozilla/5.0','{}','waf_blocked',403,40396,942100,'block','SQL Injection Attack','Brazil','BR','Sao Paulo','Sao Paulo','Locaweb','AS27715','America/Sao_Paulo',-23.5505,-46.6333,'critical',NOW() - interval '14 minutes'),
('mock-br-02','177.71.208.100','POST','/api/auth/login','id=sleep(5)','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','Brazil','BR','Sao Paulo','Sao Paulo','Locaweb','AS27715','America/Sao_Paulo',-23.5505,-46.6333,'high',NOW() - interval '19 minutes'),

-- === Japan ===
('mock-jp-01','153.126.203.4','GET','/api/admin/users','','nessus/10.0','{}','blocked_user_agent',403,40394,NULL,'','','Japan','JP','Tokyo','Tokyo','SAKURA Internet','AS9370','Asia/Tokyo',35.6762,139.6503,'medium',NOW() - interval '22 minutes'),
('mock-jp-02','153.126.203.4','GET','/.env.local','','nessus/10.0','{}','blocked_path',403,40395,NULL,'','','Japan','JP','Tokyo','Tokyo','SAKURA Internet','AS9370','Asia/Tokyo',35.6762,139.6503,'high',NOW() - interval '23 minutes'),

-- === India ===
('mock-in-01','103.99.170.50','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','India','IN','Maharashtra','Mumbai','HostGator','AS133229','Asia/Kolkata',19.0760,72.8777,'medium',NOW() - interval '25 minutes'),
('mock-in-02','103.99.170.50','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','India','IN','Maharashtra','Mumbai','HostGator','AS133229','Asia/Kolkata',19.0760,72.8777,'medium',NOW() - interval '26 minutes'),

-- === United Kingdom ===
('mock-gb-01','51.15.112.80','GET','/api/search','benchmark(10000000,md5(1))','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','United Kingdom','GB','England','London','Scaleway','AS12876','Europe/London',51.5074,-0.1278,'high',NOW() - interval '28 minutes'),

-- === South Korea ===
('mock-kr-01','211.49.46.20','POST','/api/auth/login','','Mozilla/5.0','{}','waf_blocked',403,40396,949110,'block','Inbound Anomaly Score Exceeded','South Korea','KR','Seoul','Seoul','Korea Telecom','AS4766','Asia/Seoul',37.5665,126.9780,'critical',NOW() - interval '30 minutes'),

-- === Netherlands ===
('mock-nl-01','89.248.167.131','GET','/.git/config','','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','Netherlands','NL','North Holland','Amsterdam','DigitalOcean','AS14061','Europe/Amsterdam',52.3676,4.9041,'high',NOW() - interval '32 minutes'),
('mock-nl-02','89.248.167.131','GET','/.env.production','','Mozilla/5.0','{}','blocked_path',403,40395,NULL,'','','Netherlands','NL','North Holland','Amsterdam','DigitalOcean','AS14061','Europe/Amsterdam',52.3676,4.9041,'high',NOW() - interval '33 minutes'),

-- === Australia ===
('mock-au-01','103.22.200.7','CONNECT','/proxy','','Mozilla/5.0','{}','blocked_method',501,50190,NULL,'','','Australia','AU','New South Wales','Sydney','Cloudflare','AS13335','Australia/Sydney',-33.8688,151.2093,'low',NOW() - interval '35 minutes'),

-- === Singapore ===
('mock-sg-01','128.199.159.40','POST','/api/auth/login','','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','Singapore','SG','Singapore','Singapore','DigitalOcean','AS14061','Asia/Singapore',1.3521,103.8198,'high',NOW() - interval '37 minutes'),

-- === Canada ===
('mock-ca-01','192.99.14.81','GET','/api/admin/system','','nikto/2.1.6','{}','blocked_user_agent',403,40394,NULL,'','','Canada','CA','Quebec','Montreal','OVH','AS16276','America/Toronto',45.5017,-73.5673,'medium',NOW() - interval '40 minutes'),

-- === France ===
('mock-fr-01','163.172.67.180','GET','/wp-admin/install.php','','Mozilla/5.0','{}','blocked_path',403,40395,NULL,'','','France','FR','Ile-de-France','Paris','Scaleway','AS12876','Europe/Paris',48.8566,2.3522,'high',NOW() - interval '42 minutes'),
('mock-fr-02','163.172.67.180','POST','/api/auth/login','','Mozilla/5.0','{}','waf_blocked',403,40396,941160,'block','NoScript XSS InjectionChecker','France','FR','Ile-de-France','Paris','Scaleway','AS12876','Europe/Paris',48.8566,2.3522,'critical',NOW() - interval '43 minutes'),

-- === South Africa ===
('mock-za-01','41.76.108.46','GET','/api/search','load_file(/etc/shadow)','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','South Africa','ZA','Gauteng','Johannesburg','RSAWEB','AS37153','Africa/Johannesburg',-26.2041,28.0473,'high',NOW() - interval '45 minutes'),

-- === Ukraine ===
('mock-ua-01','91.234.33.17','GET','/.env.backup','','dirsearch/0.4','{}','blocked_path',403,40395,NULL,'','','Ukraine','UA','Kyiv','Kyiv','Hetzner Ukraine','AS213230','Europe/Kyiv',50.4501,30.5234,'high',NOW() - interval '48 minutes'),

-- === Argentina ===
('mock-ar-01','190.2.148.90','POST','/api/auth/login','','Mozilla/5.0','{}','rate_limited',429,42900,NULL,'','','Argentina','AR','Buenos Aires','Buenos Aires','Telecom Argentina','AS22927','America/Argentina/Buenos_Aires',-34.6037,-58.3816,'medium',NOW() - interval '50 minutes'),

-- === Thailand ===
('mock-th-01','171.97.32.15','GET','/api/admin/users','information_schema','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','Thailand','TH','Bangkok','Bangkok','True Internet','AS17552','Asia/Bangkok',13.7563,100.5018,'high',NOW() - interval '52 minutes'),

-- === Local/Private IPs (no GeoIP) ===
('mock-lo-01','127.0.0.1','GET','/.git/config','','curl/8.0','{}','blocked_signature',403,40395,NULL,'','','','','','','','','',NULL,NULL,'high',NOW() - interval '2 minutes'),
('mock-lo-02','::1','GET','/.env','','curl/8.0','{}','blocked_path',403,40395,NULL,'','','','','','','','','',NULL,NULL,'high',NOW() - interval '3 minutes'),
('mock-lo-03','192.168.1.100','POST','/api/auth/login','','sqlmap/1.7','{}','blocked_user_agent',403,40394,NULL,'','','','','','','','','',NULL,NULL,'medium',NOW() - interval '5 minutes'),
('mock-lo-04','10.0.0.5','GET','/proc/self/environ','','Mozilla/5.0','{}','blocked_signature',403,40395,NULL,'','','','','','','','','',NULL,NULL,'high',NOW() - interval '8 minutes'),
('mock-lo-05','172.16.0.1','TRACE','/api/app/public','','curl/7.88','{}','blocked_method',501,50190,NULL,'','','','','','','','','',NULL,NULL,'low',NOW() - interval '13 minutes');
