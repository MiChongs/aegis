package service

import "strings"

// 中文语境弱口令补充词表。
//
// # 为什么非补不可
//
// zxcvbn 自带六张词表：泄露口令榜（rockyou 系）、英文维基常用词、英美男女名、
// 英美姓氏、影视台词。**没有一张覆盖中文语境。** 于是在中国用户身上会出现
// 系统性误判 —— 下面这些在历次中文库泄露里排进前列的口令，
// 在纯 zxcvbn 下会被当成"无规律随机串"给出高分：
//
//	woaini1314   → 拆不出任何字典词，按 10 位暴力算 ≈ 10^10 猜测 → 70 分
//	zhangwei     → 英文词表里没有，同样按暴力算
//	5201314      → 纯数字会被 sequence/repeat 挡一点，但"我爱你一生一世"这层语义丢失
//
// 补上之后它们会被识别成字典命中，分数落到 10 分以下 —— 这才是攻击者眼里的真实难度。
//
// # 词表怎么来的
//
// 按公开的中文口令泄露事件（2011 年 CSDN / 天涯 / 人人等批次）分析报告里反复出现的
// 高频项整理，覆盖四类：拼音词与称谓、数字谐音梗、常见姓氏拼音、国内品牌与地名。
// **顺序即权重** —— zxcvbn 的 buildRankedDict 按下标定 rank，越靠前算作越常见、
// 猜中所需次数越少，所以这份表是按大致频次降序排的，不是字母序。
//
// 这是数据不是算法：需要扩充时往对应分组里加词即可，不必改任何判定逻辑。
var chineseWeakPasswords = []string{
	// —— 数字谐音与纪念日（中文库里占比最高的一类） ——
	"5201314", "1314520", "521521", "7758521", "5211314", "1314521", "520520",
	"1314", "520", "521", "1234520", "5201314520", "13141314",
	"123321", "112233", "121212", "111222", "123123123", "168168", "1688",
	"888888888", "66666666", "99999999", "77777777", "1qaz2wsx3edc",

	// —— 拼音表白 / 称谓 ——
	"woaini", "woaini1314", "woaini520", "woaini521", "woaimama", "woaita",
	"aini", "aini1314", "woaiwo", "xihuanni", "xiangni", "baobei", "baobao",
	"laopo", "laogong", "meimei", "gege", "jiejie", "didi", "baby", "bb",
	"qinaide", "xiaobao", "xiaoke", "xiaoxiao", "yangyang", "tiantian",

	// —— 常用拼音词 ——
	"mima", "nihao", "nihaoma", "xiexie", "zaijian", "duibuqi", "meiguanxi",
	"zhongguo", "zhonghua", "womenzou", "yongyuan", "jiayou", "fadacai",
	"shengri", "kuaile", "xingfu", "pingan", "jiankang", "facai", "hongbao",
	"chuangqi", "chuanqi", "menghuan", "youxi", "wangyou", "youxian",
	"tongxue", "laoshi", "xuexiao", "gongsi", "shangban", "gongzuo",

	// —— 高频姓氏 + 名字拼音（含带数字后缀的常见形态） ——
	"zhangwei", "wangwei", "lina", "liwei", "wangfang", "liwang", "zhangwei123",
	"zhangsan", "lisi", "wangwu", "zhaoliu", "xiaoming", "xiaohong", "xiaogang",
	"zhang", "wang", "liu", "chen", "yang", "huang", "zhao", "zhou", "xu",
	"sun", "ma", "zhu", "hu", "guo", "lin", "gao", "luo", "zheng", "liang",
	"xie", "song", "tang", "han", "cao", "deng", "feng", "peng", "cheng",
	"jiang", "shen", "lu", "ding", "yao", "lan", "yuan", "pan", "du", "dai",

	// —— 地名 ——
	"beijing", "shanghai", "guangzhou", "shenzhen", "hangzhou", "nanjing",
	"chengdu", "wuhan", "xian", "chongqing", "tianjin", "suzhou", "qingdao",
	"changsha", "zhengzhou", "shenyang", "dalian", "xiamen", "kunming",
	"harbin", "jinan", "hefei", "fuzhou", "nanchang", "guiyang", "lanzhou",

	// —— 国内平台与品牌 ——
	"taobao", "alipay", "zhifubao", "weixin", "wechat", "tencent", "qqqq",
	"qq123456", "qq5201314", "baidu", "aliyun", "xiaomi", "huawei", "meituan",
	"jingdong", "sina", "weibo", "netease", "wangyi", "youku", "bilibili",
	"douyin", "kuaishou", "pinduoduo", "zhihu", "douban", "tieba", "kaixin",
	"renren", "csdn", "tianya", "duowan", "17173", "4399", "7k7k",

	"woaini123",
}

// supplementalWeakPasswords 与语种无关的常见字母数字混排补充。
//
// 与上表分开只为**标签准确**：把 "abcd1234" 报成「中文常见弱口令」会让管理员困惑。
// 两张表在喂给 zxcvbn 时会合并成同一份外部词表，只在结果归因时才区分。
var supplementalWeakPasswords = []string{
	"a123456", "a123456789", "aa123456", "abc123456", "abcd1234", "abc12345",
	"123456a", "123456aa", "123456abc", "asd123456", "zxc123456", "qwe123456",
	"as123456", "q123456", "z123456", "w123456", "s123456",
	"admin888", "admin123456", "root123456", "test123456", "user123456",
	"password1", "passw0rd", "p@ssw0rd", "iloveyou1314",
}

// supplementalDictionary 两张补充表合并后的词表，按频次序拼接
// （zxcvbn 的 buildRankedDict 按下标定 rank，越靠前越"常见"）。
// 包级变量而非每次现拼：注册与改密链路每次调用都要用它。
var supplementalDictionary = append(append([]string{}, chineseWeakPasswords...), supplementalWeakPasswords...)

// chineseWeakPasswordSet / supplementalWeakPasswordSet 命中反查表。
// key 为小写 —— zxcvbn 的字典匹配大小写不敏感，MatchedWord 回报的是
// 词表里的原样（两表均全小写）。
var (
	chineseWeakPasswordSet      = buildWordSet(chineseWeakPasswords)
	supplementalWeakPasswordSet = buildWordSet(supplementalWeakPasswords)
)

func buildWordSet(words []string) map[string]struct{} {
	set := make(map[string]struct{}, len(words))
	for _, word := range words {
		set[word] = struct{}{}
	}
	return set
}

// chineseWeakPasswordDictionary 返回补充词表，供拼进 zxcvbn 的 userInputs。
func chineseWeakPasswordDictionary() []string {
	return supplementalDictionary
}

// classifySupplementalWord 判断一次 user_inputs 字典命中来自哪张补充表。
// 都不是则说明命中的是调用方传入的账号 / 昵称等上下文，返回空串。
func classifySupplementalWord(matchedWord string) string {
	word := strings.ToLower(matchedWord)
	if _, ok := chineseWeakPasswordSet[word]; ok {
		return chineseDictionaryLabel
	}
	if _, ok := supplementalWeakPasswordSet[word]; ok {
		return labelCommonWeak
	}
	return ""
}
