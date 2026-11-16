package contract

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"unicode"
)

// WorkKey 作品键：同一首歌在各音源里的不同 ID 合成一个作品。
// sha1(规范化歌名 | 规范化第一歌手) 的前 16 位十六进制。
func WorkKey(title, artist string) string {
	t := NormalizeTitle(title)
	a := NormalizeArtist(FirstArtist(artist))
	if t == "" {
		return ""
	}
	sum := sha1.Sum([]byte(t + "|" + a))
	return hex.EncodeToString(sum[:8])
}

// 括号里出现这些词时整段去掉（版本标记，不是歌名本身）。
var versionWords = []string{
	"live", "现场", "伴奏", "纯音乐", "inst", "instrumental", "翻唱", "cover", "remix", "混音", "dj",
	"版", "version", "ver.", "ver", "remaster", "重制", "demo", "钢琴", "piano", "acoustic", "原唱",
	"女声", "男声", "合唱", "official", "mv", "抖音", "片段", "铃声", "加速", "降调", "升调", "0.8x", "1.2x",
	"feat", "ft.", "with",
}

var bracketPairs = map[rune]rune{'(': ')', '（': '）', '[': ']', '【': '】', '<': '>', '《': '》', '{': '}', '「': '」'}

// NormalizeTitle 规范化歌名：全角转半角、繁转简、小写、去掉版本括号、去空白与标点。
func NormalizeTitle(s string) string {
	s = toHalfWidth(strings.TrimSpace(s))
	s = toSimplified(strings.ToLower(s))
	s = stripVersionBrackets(s)
	// 「歌名 - 某某版」这种破折号后缀
	for _, sep := range []string{" - ", " – ", " — "} {
		if i := strings.Index(s, sep); i > 0 && containsVersionWord(s[i+len(sep):]) {
			s = s[:i]
		}
	}
	return keepWordRunes(s)
}

// NormalizeArtist 规范化歌手名。
func NormalizeArtist(s string) string {
	return keepWordRunes(toSimplified(strings.ToLower(toHalfWidth(strings.TrimSpace(s)))))
}

// FirstArtist 多位歌手时取第一位。
func FirstArtist(s string) string {
	s = strings.TrimSpace(s)
	cut := len(s)
	for _, sep := range []string{"/", "、", ",", "，", "&", ";", "；", " feat.", " feat ", " ft.", " x ", " X ", "|"} {
		if i := strings.Index(s, sep); i > 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimSpace(s[:cut])
}

// SplitArtists 拆出全部歌手（画像统计用）。
func SplitArtists(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '、' || r == ',' || r == '，' || r == '&' || r == ';' || r == '；' || r == '|'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func stripVersionBrackets(s string) string {
	runes := []rune(s)
	var out []rune
	for i := 0; i < len(runes); i++ {
		close, ok := bracketPairs[runes[i]]
		if !ok {
			out = append(out, runes[i])
			continue
		}
		j := i + 1
		for j < len(runes) && runes[j] != close {
			j++
		}
		if j >= len(runes) {
			out = append(out, runes[i])
			continue
		}
		inner := string(runes[i+1 : j])
		if containsVersionWord(inner) {
			i = j
			continue
		}
		out = append(out, runes[i:j+1]...)
		i = j
	}
	return string(out)
}

func containsVersionWord(s string) bool {
	s = strings.ToLower(s)
	for _, w := range versionWords {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func keepWordRunes(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toHalfWidth(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0x3000:
			r = ' '
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		}
		b.WriteRune(r)
	}
	return b.String()
}

// 歌名、歌手名里常见的繁体字（够把「晴天 / 晴天」「愛你 / 爱你」这类合起来；不是完整的繁简转换）。
const tradChars = "愛們會來說時個為這東車長門開關聽聲樂歡風飛雲氣電話後與對無點過還發現頭將覺國嗎著麼裡處見從進動實學經問讓體讓們間當書場給變醫萬業兩戀歲憶憶淚夢戰離傷殺記憶歸紅綠藍銀鐘錯鬧緣陽陰燈龍鳳華夢讀寫遠邊選總聽輕寧靜願熱溫暖該請誰謝識語話調講論譜詞誰錄鍵響項順須頁領類顏願飄飛馬驕鳥麗齊龜灣廣廳歷壓厲參雙號嚮團園圓圖壞夠奪奮孫學寶實寵將專導屆層岡峽嶺幣幫廢彈彎徑從復徵忍態懷懶戲戶擁擊擇擔據擠擺攜敗敘數斷時晝暫曉書會杯條極構槍樓標樣橋機檢權歡歐殘毀氣決沒況洩淺淨渾湧滅滿漢潔潛濃濕灣災烏無煙煩熱燒營爐爭爺牆犧狀獨獲獻環現瑪產畫畢異當疊瘋療發盜盡監盤眾睜矯礙祕禮禱禪秘稱種穩窮竊競筆節範築簡籃米紀約紅純紙級紛細終組結絕給統絲經綠維綱網緊線練縣總績繞繪繼續纜罰羅義習聖聞聯職聲肅腦脫腳興舉舊艦艱藝節範薦藍藥蘇蘋處號蟲蠟衛衝補裝製複見規視親覺覽觀計訂訓記設許診詞詢試詩話該詳認誓誤說誰課請諾謀謎謝謠證識譯議護讀變讓豐貝負財貢貧貨販貪貫責貴費賀資賊賓賞賢賣賤質賴購贈贏趕趙跡踐蹤躍軌軟較載輔輝輩輪輸轉辦辭農這連週進遊運過達違遙適遲遷選遺還邊鄉鄰醜釋針鈔鈴銅銘鋼錢錦鍋鎖鏡鐵閃閉問閒間閱闊阻陣陸隊際隨險隱雖雞離難雪電霧靈靜響頂順須預頓頻顆題願顯風飢飯飲飽飾餐館饒驗騎騰驚體髮鬥鬱魚魯鮮鳴鴻鵬鷹鹽麥黃點齒"
const simpChars = "爱们会来说时个为这东车长门开关听声乐欢风飞云气电话后与对无点过还发现头将觉国吗着么里处见从进动实学经问让体让们间当书场给变医万业两恋岁忆忆泪梦战离伤杀记忆归红绿蓝银钟错闹缘阳阴灯龙凤华梦读写远边选总听轻宁静愿热温暖该请谁谢识语话调讲论谱词谁录键响项顺须页领类颜愿飘飞马骄鸟丽齐龟湾广厅历压厉参双号向团园圆图坏够夺奋孙学宝实宠将专导届层冈峡岭币帮废弹弯径从复征忍态怀懒戏户拥击择担据挤摆携败叙数断时昼暂晓书会杯条极构枪楼标样桥机检权欢欧残毁气决没况泄浅净浑涌灭满汉洁潜浓湿湾灾乌无烟烦热烧营炉争爷墙牺状独获献环现玛产画毕异当叠疯疗发盗尽监盘众睁矫碍秘礼祷禅秘称种稳穷窃竞笔节范筑简篮米纪约红纯纸级纷细终组结绝给统丝经绿维纲网紧线练县总绩绕绘继续缆罚罗义习圣闻联职声肃脑脱脚兴举旧舰艰艺节范荐蓝药苏苹处号虫蜡卫冲补装制复见规视亲觉览观计订训记设许诊词询试诗话该详认誓误说谁课请诺谋谜谢谣证识译议护读变让丰贝负财贡贫货贩贪贯责贵费贺资贼宾赏贤卖贱质赖购赠赢赶赵迹践踪跃轨软较载辅辉辈轮输转办辞农这连周进游运过达违遥适迟迁选遗还边乡邻丑释针钞铃铜铭钢钱锦锅锁镜铁闪闭问闲间阅阔阻阵陆队际随险隐虽鸡离难雪电雾灵静响顶顺须预顿频颗题愿显风饥饭饮饱饰餐馆饶验骑腾惊体发斗郁鱼鲁鲜鸣鸿鹏鹰盐麦黄点齿"

// 补充对照：繁简成对排列（繁、简、繁、简……），不怕两串错位。
const tradSimpPairs = "倫伦傑杰張张劉刘陳陈鄧邓楊杨顧顾蕭萧譚谭韓韩鄭郑葉叶嚴严優优價价傳传億亿偉伟備备兒儿凱凯劇剧創创勝胜區区協协卻却員员啟启喚唤單单嘆叹嘯啸圍围堅坚報报塊块塵尘墜坠壯壮夾夹奧奥婦妇媽妈嬌娇宮宫尋寻屬属嶼屿帳帐帶带師师幾几廟庙強强彥彦徹彻憂忧憐怜憑凭懸悬撲扑擬拟攤摊曆历曬晒濤涛瀟潇灑洒煉炼燦灿爾尔牽牵獅狮猶犹瑤瑶碩硕禍祸綿绵緩缓縮缩織织繩绳翹翘聰聪腸肠膽胆臉脸艷艳莊庄蓮莲蔣蒋薩萨蘭兰蝦虾蠻蛮襲袭覓觅訴诉詠咏誌志説说誕诞諧谐謊谎譽誉讚赞貓猫賽赛趨趋遞递邁迈釘钉鋒锋錶表鍾钟鏈链闖闯韋韦韻韵頌颂颱台餘余駐驻驅驱驟骤鬆松黨党齡龄戀恋夥伙"

var t2s = func() map[rune]rune {
	tr, sr := []rune(tradChars), []rune(simpChars)
	m := make(map[rune]rune, len(tr))
	for i := range tr {
		if i < len(sr) && tr[i] != sr[i] {
			m[tr[i]] = sr[i]
		}
	}
	pairs := []rune(tradSimpPairs)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}()

func toSimplified(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if v, ok := t2s[r]; ok {
			r = v
		}
		b.WriteRune(r)
	}
	return b.String()
}
