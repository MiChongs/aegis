package service

import (
	"bytes"
	"crypto/sha256"
	"image/png"
	"math"
	"strings"
	"sync"
	"unicode"

	"github.com/fogleman/gg"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/mozillazg/go-pinyin"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
)

// 默认头像的生成。
//
// 「没上传头像的人显示什么」以前的答案是：拼一个 weavatar.com 的地址交给客户端。
// 那意味着三件事 —— 每个用户的邮箱哈希被送到第三方、内网部署的实例上头像
// 一律加载失败、以及那个服务哪天改了域名平台这边毫无察觉。而当用户既没传头像
// 也没有邮箱/手机号时，连那个地址都拼不出来，avatar 字段直接是空字符串，
// 于是每个客户端各自决定画什么（有的画灰块、有的什么都不画、有的报错）。
//
// 现在服务端自己画。确定性生成：同一个人任何时候画出来都一样，
// 不需要存储、可以整块缓存，也不会有任何数据出网。
const (
	// AvatarStyleIdenticon 由标识哈希生成的左右对称几何图案。
	// 对中文名友好（不依赖任何字形），且天然人人不同。
	AvatarStyleIdenticon = "identicon"
	// AvatarStyleInitials 首字母单字图。中文名取拼音首字母 ——
	// 内嵌字体只有拉丁字形，直接画汉字会得到一个"豆腐块"。
	AvatarStyleInitials = "initials"
	// AvatarStyleGravatar 回到老做法：跳转到 WeAvatar/Gravatar。
	// 保留它是因为有些部署确实想要用户在别处维护的那张头像。
	AvatarStyleGravatar = "gravatar"
	// AvatarStyleNone 不生成，头像地址留空（由客户端自己兜底）。
	AvatarStyleNone = "none"
)

// avatarDefaultSeedNamespace 让种子不至于和别处的哈希撞上，
// 也保证换了这个常量就能整体换一批配色（升级时不要动它，除非真想全站换脸）。
const avatarDefaultSeedNamespace = "aegis.avatar.default\x00"

var (
	avatarFontOnce sync.Once
	avatarFont     *opentype.Font
	avatarFontErr  error
)

// avatarIdentityRequest 一次默认头像渲染的输入。
type avatarIdentityRequest struct {
	// Seed 决定配色与图案的稳定标识。用主体标识而不是昵称：
	// 改个昵称就换一张脸，用户会以为账号出了问题。
	Seed string
	// Label 取首字母用的展示名（昵称 / 账号 / 邮箱）。
	Label string
	Style string
	Size  int
}

// renderDefaultAvatar 画一张默认头像，返回 PNG 字节。
//
// PNG 而不是 JPEG：这类图是大色块 + 硬边几何，JPEG 会在边缘留下振铃，
// 而 PNG 在这种内容上反而更小。
func renderDefaultAvatar(req avatarIdentityRequest) ([]byte, string, error) {
	size := req.Size
	if size <= 0 {
		size = 256
	}
	if size > 512 {
		size = 512
	}
	digest := sha256.Sum256([]byte(avatarDefaultSeedNamespace + strings.TrimSpace(req.Seed)))

	ctx := gg.NewContext(size, size)
	drawAvatarBackground(ctx, digest, size)

	if req.Style == AvatarStyleInitials {
		if label := avatarInitial(req.Label); label != "" {
			drawAvatarInitial(ctx, label, size)
		} else {
			// 取不到可画的首字母（纯符号昵称、空账号）时回落到几何图案，
			// 而不是画一个问号 —— 问号看起来像是加载失败。
			drawAvatarIdenticon(ctx, digest, size)
		}
	} else {
		drawAvatarIdenticon(ctx, digest, size)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, ctx.Image()); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/png", nil
}

// drawAvatarBackground 由哈希派生一对同色系的颜色画对角渐变。
//
// 在 HCL 空间取色而不是直接摆弄 RGB：RGB 里等距的两个值，人眼看到的
// 明度差可能相去甚远，随机出来的配色会有一部分深得看不见白色文字。
// 固定 C/L、只让 H 随哈希转，就能保证**每一种**结果的对比度都够。
func drawAvatarBackground(ctx *gg.Context, digest [32]byte, size int) {
	hue := float64(uint16(digest[0])<<8|uint16(digest[1])) / 65535.0 * 360.0
	base := colorful.Hcl(hue, 0.52, 0.58).Clamped()
	// 副色沿色相转 28°，得到的是"同一块布的两种光照"而不是两种颜色
	accent := colorful.Hcl(math.Mod(hue+28, 360), 0.55, 0.48).Clamped()

	grad := gg.NewLinearGradient(0, 0, float64(size), float64(size))
	grad.AddColorStop(0, base)
	grad.AddColorStop(1, accent)
	ctx.SetFillStyle(grad)
	ctx.DrawRectangle(0, 0, float64(size), float64(size))
	ctx.Fill()
}

// drawAvatarIdenticon 画 5×5 的左右对称图案。
//
// 对称是这类图案好看的关键：随机撒点看起来像噪声，镜像之后大脑会
// 把它读成一个"图形"。只需要决定左边 3 列（15 格），右边镜像过去。
func drawAvatarIdenticon(ctx *gg.Context, digest [32]byte, size int) {
	const grid = 5
	cell := float64(size) / float64(grid+2) // 四周各留一格的边距
	origin := (float64(size) - cell*float64(grid)) / 2

	ctx.SetRGBA(1, 1, 1, 0.92)
	for col := 0; col < (grid+1)/2; col++ {
		for row := 0; row < grid; row++ {
			if digest[col*grid+row+2]&0x01 == 0 {
				continue
			}
			for _, x := range []int{col, grid - 1 - col} {
				ctx.DrawRoundedRectangle(
					origin+float64(x)*cell, origin+float64(row)*cell,
					cell, cell, cell*0.22)
				ctx.Fill()
			}
		}
	}
}

// drawAvatarInitial 在中央画一个字。
func drawAvatarInitial(ctx *gg.Context, label string, size int) {
	face := avatarRenderFace(float64(size) * 0.42)
	if face == nil {
		return
	}
	defer func() { _ = face.Close() }()
	ctx.SetFontFace(face)
	ctx.SetRGBA(1, 1, 1, 0.95)
	// 以 (0.5, 0.5) 为锚点，让字形的**视觉中心**落在圆心上；
	// 按基线定位会让所有字母整体偏下三分之一个字高。
	ctx.DrawStringAnchored(label, float64(size)/2, float64(size)/2, 0.5, 0.5)
}

func avatarRenderFace(size float64) font.Face {
	avatarFontOnce.Do(func() {
		avatarFont, avatarFontErr = opentype.Parse(gobold.TTF)
	})
	if avatarFontErr != nil {
		return nil
	}
	face, err := opentype.NewFace(avatarFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil
	}
	return face
}

// avatarInitial 取一个可以画出来的首字母。
//
// 三条规则，按顺序：
//
//	拉丁字母 / 数字  → 原样大写
//	Han（汉字）      → 拼音首字母（内嵌字体没有中日韩字形，直接画是豆腐块）
//	其他             → 空，交由调用方回落到几何图案
//
// 日文汉字在 Unicode 里同属 Han，因此也会走拼音那条分支（日 → R）。
// 没有语言标注时这是可接受的取舍：一个读音不对的字母仍然是个字母，
// 而豆腐块看起来就是"这个系统坏了"。假名与西里尔等落到第三条，回落几何图案。
//
// 拼音这一步用的是平台里已有的 go-pinyin。把「张三」画成 Z 不是最理想的
// （最理想是画「张」），但那需要在运行环境里找到一份中日韩字体，
// 而那件事在容器里十有八九是找不到的 —— 画不出来的方案不能当默认方案。
func avatarInitial(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	// 邮箱取 @ 之前那段，否则所有 Gmail 用户都会得到同一个字母
	if idx := strings.Index(label, "@"); idx > 0 {
		label = label[:idx]
	}
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
			return string(unicode.ToUpper(r))
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return string(r)
		case unicode.Is(unicode.Han, r):
			args := pinyin.NewArgs()
			args.Style = pinyin.FirstLetter
			if result := pinyin.Pinyin(string(r), args); len(result) > 0 && len(result[0]) > 0 {
				return strings.ToUpper(result[0][0])
			}
			return ""
		}
	}
	return ""
}

// normalizeAvatarDefaultStyle 把配置值收敛到已知的四档。
// 未知值一律当 identicon：拼错一个字母就让全站没有默认头像，
// 代价与收益完全不成比例。
func normalizeAvatarDefaultStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AvatarStyleInitials:
		return AvatarStyleInitials
	case AvatarStyleGravatar:
		return AvatarStyleGravatar
	case AvatarStyleNone:
		return AvatarStyleNone
	default:
		return AvatarStyleIdenticon
	}
}
