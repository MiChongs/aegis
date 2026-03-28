package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"aegis/internal/config"
	"aegis/internal/db"
	pgrepo "aegis/internal/repository/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type mockProvinceCatalog struct {
	Province string
	Cities   []string
}

type mockUserRecord struct {
	Account     string
	Nickname    string
	Email       string
	Integral    int64
	Experience  int64
	VIPExpireAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Extra       map[string]any
}

type mockUserSeedOptions struct {
	AppID        int64
	CountPerCity int
	BatchSize    int
	Password     string
	Seed         int64
}

var chinaMockUserCatalog = []mockProvinceCatalog{
	{Province: "北京市", Cities: []string{"东城区", "西城区", "朝阳区", "海淀区", "丰台区", "通州区"}},
	{Province: "天津市", Cities: []string{"和平区", "河西区", "南开区", "滨海新区", "武清区", "西青区"}},
	{Province: "上海市", Cities: []string{"黄浦区", "徐汇区", "浦东新区", "静安区", "闵行区", "嘉定区"}},
	{Province: "重庆市", Cities: []string{"渝中区", "江北区", "南岸区", "渝北区", "九龙坡区", "沙坪坝区"}},
	{Province: "河北省", Cities: []string{"石家庄市", "唐山市", "保定市", "邯郸市", "廊坊市", "沧州市"}},
	{Province: "山西省", Cities: []string{"太原市", "大同市", "长治市", "晋中市", "临汾市", "运城市"}},
	{Province: "辽宁省", Cities: []string{"沈阳市", "大连市", "鞍山市", "锦州市", "营口市", "葫芦岛市"}},
	{Province: "吉林省", Cities: []string{"长春市", "吉林市", "四平市", "延边州", "松原市", "白城市"}},
	{Province: "黑龙江省", Cities: []string{"哈尔滨市", "齐齐哈尔市", "牡丹江市", "佳木斯市", "大庆市", "绥化市"}},
	{Province: "江苏省", Cities: []string{"南京市", "苏州市", "无锡市", "常州市", "南通市", "徐州市", "盐城市"}},
	{Province: "浙江省", Cities: []string{"杭州市", "宁波市", "温州市", "嘉兴市", "绍兴市", "金华市", "台州市"}},
	{Province: "安徽省", Cities: []string{"合肥市", "芜湖市", "蚌埠市", "阜阳市", "安庆市", "滁州市"}},
	{Province: "福建省", Cities: []string{"福州市", "厦门市", "泉州市", "漳州市", "莆田市", "龙岩市"}},
	{Province: "江西省", Cities: []string{"南昌市", "赣州市", "九江市", "上饶市", "宜春市", "吉安市"}},
	{Province: "山东省", Cities: []string{"济南市", "青岛市", "烟台市", "潍坊市", "临沂市", "济宁市", "淄博市"}},
	{Province: "河南省", Cities: []string{"郑州市", "洛阳市", "开封市", "南阳市", "新乡市", "商丘市", "周口市"}},
	{Province: "湖北省", Cities: []string{"武汉市", "襄阳市", "宜昌市", "黄石市", "荆州市", "孝感市"}},
	{Province: "湖南省", Cities: []string{"长沙市", "株洲市", "湘潭市", "衡阳市", "岳阳市", "常德市", "郴州市"}},
	{Province: "广东省", Cities: []string{"广州市", "深圳市", "佛山市", "东莞市", "珠海市", "中山市", "汕头市", "湛江市"}},
	{Province: "海南省", Cities: []string{"海口市", "三亚市", "儋州市", "琼海市", "文昌市", "万宁市"}},
	{Province: "四川省", Cities: []string{"成都市", "绵阳市", "德阳市", "宜宾市", "南充市", "泸州市", "乐山市"}},
	{Province: "贵州省", Cities: []string{"贵阳市", "遵义市", "六盘水市", "安顺市", "毕节市", "黔南州"}},
	{Province: "云南省", Cities: []string{"昆明市", "曲靖市", "玉溪市", "大理州", "红河州", "昭通市", "普洱市"}},
	{Province: "陕西省", Cities: []string{"西安市", "咸阳市", "宝鸡市", "渭南市", "榆林市", "汉中市"}},
	{Province: "甘肃省", Cities: []string{"兰州市", "天水市", "酒泉市", "庆阳市", "武威市", "张掖市"}},
	{Province: "青海省", Cities: []string{"西宁市", "海东市", "海西州", "海南州", "黄南州", "玉树州"}},
	{Province: "台湾省", Cities: []string{"台北市", "新北市", "桃园市", "台中市", "台南市", "高雄市"}},
	{Province: "内蒙古自治区", Cities: []string{"呼和浩特市", "包头市", "赤峰市", "鄂尔多斯市", "通辽市", "呼伦贝尔市"}},
	{Province: "广西壮族自治区", Cities: []string{"南宁市", "柳州市", "桂林市", "北海市", "玉林市", "百色市"}},
	{Province: "西藏自治区", Cities: []string{"拉萨市", "日喀则市", "林芝市", "昌都市", "那曲市", "山南市"}},
	{Province: "宁夏回族自治区", Cities: []string{"银川市", "石嘴山市", "吴忠市", "固原市", "中卫市"}},
	{Province: "新疆维吾尔自治区", Cities: []string{"乌鲁木齐市", "克拉玛依市", "喀什地区", "伊犁州", "昌吉州", "库尔勒市"}},
	{Province: "香港特别行政区", Cities: []string{"中西区", "湾仔区", "观塘区", "沙田区", "荃湾区"}},
	{Province: "澳门特别行政区", Cities: []string{"花地玛堂区", "圣安多尼堂区", "大堂区", "望德堂区", "嘉模堂区"}},
}

var mockSurnames = []string{"赵", "钱", "孙", "李", "周", "吴", "郑", "王", "冯", "陈", "褚", "卫", "蒋", "沈", "韩", "杨", "朱", "秦", "尤", "许", "何", "吕", "施", "张", "孔", "曹", "严", "华", "金", "魏", "陶", "姜"}
var mockGivenNameChars = []string{"安", "北", "晨", "成", "川", "达", "东", "帆", "峰", "光", "浩", "恒", "宏", "嘉", "杰", "景", "凯", "坤", "林", "铭", "宁", "鹏", "琦", "锐", "森", "涛", "伟", "翔", "星", "阳", "一", "宇", "远", "泽", "正", "知", "梓", "子", "文", "欣", "雅", "雨", "悦", "可", "依", "诺", "清", "岚", "宁", "妍", "诗", "思", "珂", "菲", "芷", "瑶"}
var mockISPs = []string{"中国移动", "中国联通", "中国电信", "中国广电"}

func RunGenerateMockUsers(ctx context.Context, args []string) error {
	opts, err := parseMockUserSeedOptions(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := pgrepo.New(pool)
	appID := opts.AppID
	if appID == 0 {
		apps, err := repo.ListApps(ctx)
		if err != nil {
			return err
		}
		if len(apps) == 0 {
			return fmt.Errorf("no apps found, create an app before generating mock users")
		}
		appID = apps[0].ID
	}
	app, err := repo.GetAppByID(ctx, appID)
	if err != nil {
		return err
	}
	if app == nil {
		return fmt.Errorf("app %d not found", appID)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(opts.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(opts.Seed))
	records := buildMockUserRecords(appID, opts.CountPerCity, rng)
	inserted, skipped, err := insertMockUserRecords(ctx, pool, appID, string(passwordHash), opts.BatchSize, records)
	if err != nil {
		return err
	}

	fmt.Printf("mock users generated: appid=%d app=%s provinces=%d cities=%d requested=%d inserted=%d skipped=%d password=%s seed=%d\n",
		appID,
		app.Name,
		len(chinaMockUserCatalog),
		countMockCities(),
		len(records),
		inserted,
		skipped,
		opts.Password,
		opts.Seed,
	)
	return nil
}

func parseMockUserSeedOptions(args []string) (mockUserSeedOptions, error) {
	opts := mockUserSeedOptions{
		CountPerCity: 24,
		BatchSize:    500,
		Password:     "Aegis@123456",
		Seed:         time.Now().UnixNano(),
	}

	fs := flag.NewFlagSet("mock-users", flag.ContinueOnError)
	fs.Int64Var(&opts.AppID, "appid", 0, "目标应用 ID，默认取第一个应用")
	fs.IntVar(&opts.CountPerCity, "count-per-city", opts.CountPerCity, "每个地区生成的用户数")
	fs.IntVar(&opts.BatchSize, "batch-size", opts.BatchSize, "单批次写入数量")
	fs.StringVar(&opts.Password, "password", opts.Password, "模拟用户默认密码")
	fs.Int64Var(&opts.Seed, "seed", opts.Seed, "随机种子")
	if err := fs.Parse(args); err != nil {
		return mockUserSeedOptions{}, err
	}

	remaining := fs.Args()
	if len(remaining) > 0 && opts.AppID == 0 {
		var appID int64
		_, err := fmt.Sscanf(remaining[0], "%d", &appID)
		if err == nil && appID > 0 {
			opts.AppID = appID
		}
	}
	if len(remaining) > 1 {
		var count int
		_, err := fmt.Sscanf(remaining[1], "%d", &count)
		if err == nil && count > 0 {
			opts.CountPerCity = count
		}
	}
	if opts.CountPerCity <= 0 {
		return mockUserSeedOptions{}, fmt.Errorf("count-per-city must be > 0")
	}
	if opts.BatchSize <= 0 {
		return mockUserSeedOptions{}, fmt.Errorf("batch-size must be > 0")
	}
	return opts, nil
}

func buildMockUserRecords(appID int64, countPerCity int, rng *rand.Rand) []mockUserRecord {
	total := countMockCities() * countPerCity
	records := make([]mockUserRecord, 0, total)
	now := time.Now().UTC()

	provinceIndex := 0
	for _, province := range chinaMockUserCatalog {
		cityIndex := 0
		for _, city := range province.Cities {
			for i := 0; i < countPerCity; i++ {
				index := i + 1
				account := fmt.Sprintf("mock%d%02d%02d%03d", appID, provinceIndex+1, cityIndex+1, index)
				createdAt := now.Add(-time.Duration(rng.Intn(24*540)) * time.Hour)
				updatedAt := createdAt.Add(time.Duration(rng.Intn(24*90)) * time.Hour)
				if updatedAt.After(now) {
					updatedAt = now.Add(-time.Duration(rng.Intn(48)) * time.Hour)
				}
				experience := int64(50 + rng.Intn(18000))
				integral := int64(20 + rng.Intn(6000))
				var vipExpireAt *time.Time
				if rng.Intn(100) < 18 {
					value := now.Add(time.Duration(24*(30+rng.Intn(540))) * time.Hour)
					vipExpireAt = &value
				}

				registerIP := fmt.Sprintf("%d.%d.%d.%d", 36+(provinceIndex%180), 10+(cityIndex%180), 1+rng.Intn(200), 2+rng.Intn(250))
				nickname := buildMockNickname(rng)
				email := account + "@mock.aegis.local"
				records = append(records, mockUserRecord{
					Account:     account,
					Nickname:    nickname,
					Email:       email,
					Integral:    integral,
					Experience:  experience,
					VIPExpireAt: vipExpireAt,
					CreatedAt:   createdAt,
					UpdatedAt:   updatedAt,
					Extra: map[string]any{
						"register_ip":       registerIP,
						"register_time":     createdAt.Format(time.RFC3339),
						"register_province": province.Province,
						"register_city":     city,
						"register_isp":      mockISPs[rng.Intn(len(mockISPs))],
						"markcode":          fmt.Sprintf("mock-%02d-%02d", provinceIndex+1, cityIndex+1),
						"source":            "mock_seed",
					},
				})
			}
			cityIndex++
		}
		provinceIndex++
	}
	return records
}

func buildMockNickname(rng *rand.Rand) string {
	surname := mockSurnames[rng.Intn(len(mockSurnames))]
	first := mockGivenNameChars[rng.Intn(len(mockGivenNameChars))]
	second := mockGivenNameChars[rng.Intn(len(mockGivenNameChars))]
	if rng.Intn(100) < 35 {
		return surname + first
	}
	return surname + first + second
}

func countMockCities() int {
	total := 0
	for _, province := range chinaMockUserCatalog {
		total += len(province.Cities)
	}
	return total
}

func insertMockUserRecords(ctx context.Context, pool *pgxpool.Pool, appID int64, passwordHash string, batchSize int, records []mockUserRecord) (int, int, error) {
	insertedTotal := 0
	skippedTotal := 0

	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}

		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return insertedTotal, skippedTotal, err
		}

		insertedIDs, skipped, err := insertMockUserChunk(ctx, tx, appID, passwordHash, records[start:end])
		if err != nil {
			_ = tx.Rollback(ctx)
			return insertedTotal, skippedTotal, err
		}
		if err := tx.Commit(ctx); err != nil {
			return insertedTotal, skippedTotal, err
		}

		insertedTotal += len(insertedIDs)
		skippedTotal += skipped
	}

	return insertedTotal, skippedTotal, nil
}

func insertMockUserChunk(ctx context.Context, tx pgx.Tx, appID int64, passwordHash string, chunk []mockUserRecord) ([]int64, int, error) {
	if len(chunk) == 0 {
		return nil, 0, nil
	}

	var usersSQL strings.Builder
	usersSQL.WriteString(`INSERT INTO users (appid, account, password_hash, enabled, integral, experience, vip_expire_at, created_at, updated_at) VALUES `)

	args := make([]any, 0, len(chunk)*9)
	for i, item := range chunk {
		if i > 0 {
			usersSQL.WriteString(",")
		}
		base := i * 8
		usersSQL.WriteString(fmt.Sprintf("($%d,$%d,$%d,TRUE,$%d,$%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8))
		args = append(args, appID, item.Account, passwordHash, item.Integral, item.Experience, item.VIPExpireAt, item.CreatedAt, item.UpdatedAt)
	}
	usersSQL.WriteString(` ON CONFLICT (appid, account) DO NOTHING RETURNING id, account`)

	rows, err := tx.Query(ctx, usersSQL.String(), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	accountToID := make(map[string]int64, len(chunk))
	insertedIDs := make([]int64, 0, len(chunk))
	for rows.Next() {
		var (
			id      int64
			account string
		)
		if err := rows.Scan(&id, &account); err != nil {
			return nil, 0, err
		}
		accountToID[account] = id
		insertedIDs = append(insertedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	skipped := len(chunk) - len(insertedIDs)

	if len(insertedIDs) == 0 {
		return nil, skipped, nil
	}

	var profilesSQL strings.Builder
	profilesSQL.WriteString(`INSERT INTO user_profiles (user_id, nickname, email, extra, updated_at) VALUES `)
	profileArgs := make([]any, 0, len(insertedIDs)*5)
	profileIndex := 0
	for _, item := range chunk {
		userID, ok := accountToID[item.Account]
		if !ok {
			continue
		}
		if profileIndex > 0 {
			profilesSQL.WriteString(",")
		}
		extraJSON, _ := json.Marshal(item.Extra)
		base := profileIndex * 5
		profilesSQL.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4, base+5))
		profileArgs = append(profileArgs, userID, item.Nickname, item.Email, extraJSON, item.UpdatedAt)
		profileIndex++
	}
	profilesSQL.WriteString(` ON CONFLICT (user_id) DO UPDATE SET nickname = EXCLUDED.nickname, email = EXCLUDED.email, extra = EXCLUDED.extra, updated_at = EXCLUDED.updated_at`)
	if _, err := tx.Exec(ctx, profilesSQL.String(), profileArgs...); err != nil {
		return nil, 0, err
	}

	if err := insertMockUserLevels(ctx, tx, insertedIDs); err != nil {
		return nil, 0, err
	}
	return insertedIDs, skipped, nil
}

func insertMockUserLevels(ctx context.Context, tx pgx.Tx, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO user_level_records (
    user_id,
    appid,
    current_level,
    current_experience,
    total_experience,
    next_level_experience,
    level_progress,
    highest_level,
    level_up_count,
    last_level_up_at,
    created_at,
    updated_at
)
SELECT
    u.id,
    u.appid,
    COALESCE(curr.level, 1) AS current_level,
    GREATEST(u.experience - COALESCE(curr.experience_required, 0), 0) AS current_experience,
    u.experience AS total_experience,
    CASE
        WHEN nxt.experience_required IS NULL THEN NULL
        ELSE GREATEST(nxt.experience_required - u.experience, 0)
    END AS next_level_experience,
    CASE
        WHEN nxt.experience_required IS NULL THEN 100.00
        WHEN nxt.experience_required = COALESCE(curr.experience_required, 0) THEN 0.00
        ELSE ROUND(
            LEAST(
                100.00,
                GREATEST(
                    0.00,
                    ((u.experience - COALESCE(curr.experience_required, 0))::numeric / NULLIF((nxt.experience_required - COALESCE(curr.experience_required, 0)), 0)::numeric) * 100.00
                )
            ),
            2
        )
    END AS level_progress,
    COALESCE(curr.level, 1) AS highest_level,
    0 AS level_up_count,
    NULL AS last_level_up_at,
    u.created_at,
    u.updated_at
FROM users u
LEFT JOIN LATERAL (
    SELECT level, experience_required
    FROM user_levels
    WHERE is_active = TRUE AND experience_required <= u.experience
    ORDER BY level DESC
    LIMIT 1
) curr ON TRUE
LEFT JOIN LATERAL (
    SELECT level, experience_required
    FROM user_levels
    WHERE is_active = TRUE AND experience_required > u.experience
    ORDER BY level ASC
    LIMIT 1
) nxt ON TRUE
WHERE u.id = ANY($1)
ON CONFLICT (user_id, appid) DO NOTHING`, userIDs)
	return err
}
