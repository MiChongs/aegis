// Package gifcaptcha 生成逐帧动画的图形验证码（GIF）。
//
// 输入一组参数，输出一段 GIF 与答案，不含业务逻辑。
// 纯 Go、无 cgo、无外部进程，字形来自内嵌字体（x/image/font/gofont）而不读系统字体目录，
// 因此 Windows / Linux / 容器 / ARM 上表现一致。
package gifcaptcha

import (
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand/v2"
	"strings"
	"time"
)

// Mode 字符集档位。
type Mode string

const (
	// ModeAlnum 大写字母 + 数字，剔除易混字符（0/O、1/I/L、2/Z、5/S、8/B）。默认档。
	ModeAlnum Mode = "alnum"
	// ModeAlpha 仅大写字母（同样剔除易混字符），适合答案要读出来的场景。
	ModeAlpha Mode = "alpha"
	// ModeDigit 仅数字，键盘友好，但可选空间最小。
	ModeDigit Mode = "digit"
)

// 字符集剔除易混字符（0/O、1/I/L、2/Z、5/S、8/B）
const (
	charsetAlnum = "ACDEFGHJKMNPQRTUVWXY34679"
	charsetAlpha = "ACDEFGHJKMNPQRTUVWXY"
	charsetDigit = "0123456789"
)

func (m Mode) charset() string {
	switch m {
	case ModeAlpha:
		return charsetAlpha
	case ModeDigit:
		return charsetDigit
	default:
		return charsetAlnum
	}
}

// normalizeMode 未知档位回落到 alnum，不报错（配置写错不该让登录页取不到验证码）
func normalizeMode(m Mode) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(string(m)))) {
	case ModeAlpha:
		return ModeAlpha
	case ModeDigit:
		return ModeDigit
	default:
		return ModeAlnum
	}
}

// 参数边界。宽 × 高 × 帧数决定渲染耗时与响应体大小，必须有上界。
const (
	MinLength = 3
	MaxLength = 8

	MinWidth  = 80
	MaxWidth  = 640
	MinHeight = 40
	MaxHeight = 240

	MinFrames = 4
	MaxFrames = 40

	MinFrameDelay = 20 * time.Millisecond
	MaxFrameDelay = time.Second

	// MaxPixelBudget 单次渲染的像素总量上限（宽 × 高 × 帧数），超出时减帧而不是报错
	MaxPixelBudget = 2_000_000
)

// 默认值：12 帧 × 90ms ≈ 1.08 秒一轮
const (
	DefaultLength     = 5
	DefaultWidth      = 240
	DefaultHeight     = 80
	DefaultFrames     = 12
	DefaultFrameDelay = 90 * time.Millisecond
	DefaultNoise      = 45
	DefaultWobble     = 55
)

// Options 渲染参数
type Options struct {
	Width      int           // 画布宽度（像素）
	Height     int           // 画布高度（像素）
	Length     int           // 字符数
	Frames     int           // 帧数
	FrameDelay time.Duration // 帧间隔
	Mode       Mode          // 字符集档位
	Noise      int           // 干扰强度 0-100：噪点数量、干扰线条数与粗细
	Wobble     int           // 运动幅度 0-100：字符位移/旋转/缩放与水波扭曲的振幅
}

// DefaultOptions 返回一份可直接使用的参数。
func DefaultOptions() Options {
	return Options{
		Width:      DefaultWidth,
		Height:     DefaultHeight,
		Length:     DefaultLength,
		Frames:     DefaultFrames,
		FrameDelay: DefaultFrameDelay,
		Mode:       ModeAlnum,
		Noise:      DefaultNoise,
		Wobble:     DefaultWobble,
	}
}

// Normalize 补默认值并把每一项夹进合法区间。幂等。
func (o Options) Normalize() Options {
	if o.Width <= 0 {
		o.Width = DefaultWidth
	}
	if o.Height <= 0 {
		o.Height = DefaultHeight
	}
	if o.Length <= 0 {
		o.Length = DefaultLength
	}
	if o.Frames <= 0 {
		o.Frames = DefaultFrames
	}
	if o.FrameDelay <= 0 {
		o.FrameDelay = DefaultFrameDelay
	}
	if o.Noise < 0 {
		o.Noise = DefaultNoise
	}
	if o.Wobble < 0 {
		o.Wobble = DefaultWobble
	}

	o.Width = clampInt(o.Width, MinWidth, MaxWidth)
	o.Height = clampInt(o.Height, MinHeight, MaxHeight)
	o.Length = clampInt(o.Length, MinLength, MaxLength)
	o.Frames = clampInt(o.Frames, MinFrames, MaxFrames)
	o.Noise = clampInt(o.Noise, 0, 100)
	o.Wobble = clampInt(o.Wobble, 0, 100)
	o.Mode = normalizeMode(o.Mode)

	if o.FrameDelay < MinFrameDelay {
		o.FrameDelay = MinFrameDelay
	}
	if o.FrameDelay > MaxFrameDelay {
		o.FrameDelay = MaxFrameDelay
	}

	// 像素预算：按每帧面积算出还能画几帧
	perFrame := o.Width * o.Height
	if perFrame > 0 && perFrame*o.Frames > MaxPixelBudget {
		o.Frames = clampInt(MaxPixelBudget/perFrame, MinFrames, o.Frames)
	}
	return o
}

// Result 一次生成的产物。
type Result struct {
	Answer   string        // 正确答案（大写，校验按大小写不敏感）
	Data     []byte        // GIF 字节
	MimeType string        // 恒为 image/gif
	Width    int           // 实际画布宽度（Normalize 夹取后的值）
	Height   int           // 实际画布高度
	Frames   int           // 实际帧数
	Delay    time.Duration // 实际帧间隔
}

// Duration 动画一轮的时长。
func (r *Result) Duration() time.Duration {
	if r == nil {
		return 0
	}
	return time.Duration(r.Frames) * r.Delay
}

// Generate 生成一个动态验证码。答案用 crypto/rand，画面随机数用 crypto 播种的 math/rand/v2。
func Generate(opts Options) (*Result, error) {
	opts = opts.Normalize()

	answer, err := randomAnswer(opts.Mode.charset(), opts.Length)
	if err != nil {
		return nil, err
	}
	rng, err := newRand()
	if err != nil {
		return nil, err
	}

	data, err := render(opts, answer, rng)
	if err != nil {
		return nil, err
	}
	return &Result{
		Answer:   answer,
		Data:     data,
		MimeType: "image/gif",
		Width:    opts.Width,
		Height:   opts.Height,
		Frames:   opts.Frames,
		Delay:    opts.FrameDelay,
	}, nil
}

// randomAnswer 从字符集等概率取 n 个字符（rand.Int 而不是取模，避免分布偏斜）
func randomAnswer(charset string, n int) (string, error) {
	if charset == "" || n <= 0 {
		return "", fmt.Errorf("gifcaptcha: 非法的答案参数")
	}
	max := big.NewInt(int64(len(charset)))
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("gifcaptcha: 生成答案失败: %w", err)
		}
		sb.WriteByte(charset[idx.Int64()])
	}
	return sb.String(), nil
}

// newRand 用 crypto/rand 播种 PCG。
func newRand() (*mrand.Rand, error) {
	seed := make([]byte, 16)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("gifcaptcha: 取随机种子失败: %w", err)
	}
	hi := uint64(0)
	lo := uint64(0)
	for i := 0; i < 8; i++ {
		hi = hi<<8 | uint64(seed[i])
		lo = lo<<8 | uint64(seed[8+i])
	}
	return mrand.New(mrand.NewPCG(hi, lo)), nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
