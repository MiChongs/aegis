// Package avatar 头像领域类型。
//
// 头像这件事有一个和别的资源都不一样的性质：**它的地址会被别人存起来**。
// 客户端把它塞进 localStorage、Android 塞进 Room、邮件正文里嵌成 <img>、
// 甚至有客户端把读回来的整份资料原样 PUT 回来。因此「当场可用」是不够的，
// 地址必须**永久有效**。这里的类型就是围绕这一条组织的：
//
//	Reference —— 落库的那个字符串，只有服务端能产生（storage:// 引用）
//	Asset     —— 一次上传产出的全部变体与元数据，同时是历史与自愈的依据
//	View      —— 出网的形状，url 恒为永久地址，恒不为空
package avatar

import "time"

// 主体类型。平台里有两套互不相通的主键空间（应用用户 / 管理员），
// 头像的归属必须跟着这条线走，否则 appid=1 的用户 42 会和管理员 42 撞在一起。
const (
	OwnerUser  = "user"
	OwnerAdmin = "admin"
)

// 资产状态。replaced 保留是为了历史与自愈 —— 一个人误传了头像想换回上一张，
// 以及库里那一列被写坏时还有地方把引用找回来。
const (
	StatusActive   = "active"
	StatusReplaced = "replaced"
	StatusDeleted  = "deleted"
)

// 来源。区分「用户自己传的」与「第三方登录带过来的」，
// 后者在渠道解绑时才有依据决定要不要一起清掉。
const (
	SourceUpload   = "upload"
	SourceImport   = "import"
	SourceMigrated = "migrated"
)

// 标准变体边长。**这组值不能随便改**：它已经落进 avatar_assets.variants，
// 存量资产不会因为改了常量就多出一个尺寸，请求一个不存在的变体只会回落到最近的一档。
// 新增尺寸是安全的（老资产回落），删除尺寸不是。
var StandardSizes = []int{64, 128, 256, 512}

// DefaultRenderSize 不带尺寸参数时给哪一档。
// 256 是控制台头像（40px @3x）与客户端资料页（96px @2x）都够用的一档；
// 给 512 会让每个列表页多下载三四倍的字节，给 128 则在高分屏上糊。
const DefaultRenderSize = 256

// MaxRenderSize 允许请求的最大边长，同时是归一化后原图的上限。
const MaxRenderSize = 512

// Owner 头像归属的主体。
type Owner struct {
	Type string `json:"type"`
	// AppID 应用用户的租户标识；管理员恒为 0。
	AppID int64 `json:"appid"`
	ID    int64 `json:"id"`
}

// Valid 主体是否可用于寻址。
func (o Owner) Valid() bool {
	if o.ID <= 0 {
		return false
	}
	switch o.Type {
	case OwnerUser:
		return o.AppID > 0
	case OwnerAdmin:
		return true
	default:
		return false
	}
}

// Variant 一个尺寸档的产物。
type Variant struct {
	// Size 边长像素；0 表示「归一化后的原图」（长边不超过 MaxRenderSize）。
	Size        int    `json:"size"`
	Key         string `json:"key"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`
}

// Asset 一次头像上传的完整产物。
type Asset struct {
	ID       int64 `json:"id"`
	Owner    Owner `json:"owner"`
	ConfigID int64 `json:"configId"`
	// BaseKey 归一化原图的对象键，也是落库引用指向的那一个。
	BaseKey     string `json:"baseKey"`
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bytes       int64  `json:"bytes"`
	// Checksum 归一化后原图的 sha256，同时充当版本号与 ETag 的来源。
	// 用处理后的字节而不是上传字节：两张 EXIF 不同、像素相同的照片
	// 应该得到同一个版本，否则客户端会为了同一张脸重新下载一遍。
	Checksum string `json:"checksum"`
	// Blurhash 供客户端在图片到达前渲染占位色块（BlurHash 规范，21~30 字符）。
	Blurhash string `json:"blurhash,omitempty"`
	// DominantColor 形如 #RRGGBB，用于骨架屏与聊天气泡描边。
	DominantColor string `json:"dominantColor,omitempty"`
	// Animated 原图是多帧 GIF。此时 BaseKey 指向保留下来的原始动图。
	Animated   bool       `json:"animated"`
	Variants   []Variant  `json:"variants"`
	Source     string     `json:"source"`
	Status     string     `json:"status"`
	FileName   string     `json:"fileName,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReplacedAt *time.Time `json:"replacedAt,omitempty"`
}

// VariantFor 取最接近请求尺寸的变体：**只向上取**，取不到才退回最大的一档。
// 向下取会把 64px 的图拉到 256px 显示，糊得比多下几 KB 明显得多。
func (a *Asset) VariantFor(size int) *Variant {
	if a == nil || len(a.Variants) == 0 {
		return nil
	}
	var best *Variant
	for i := range a.Variants {
		v := &a.Variants[i]
		if v.Size <= 0 {
			continue
		}
		if v.Size >= size && (best == nil || v.Size < best.Size) {
			best = v
		}
	}
	if best != nil {
		return best
	}
	for i := range a.Variants {
		v := &a.Variants[i]
		if v.Size > 0 && (best == nil || v.Size > best.Size) {
			best = v
		}
	}
	return best
}

// 出网视图里的 kind：客户端据此决定要不要显示「更换头像」之外的
// 「移除头像」入口 —— 默认头像没什么可移除的。
const (
	KindCustom   = "custom"   // 用户自己上传的
	KindExternal = "external" // 第三方地址（OAuth 带过来的等）
	KindDefault  = "default"  // 服务端生成的默认头像
)

// View 出网的头像描述。**URL 恒不为空** —— 没有自定义头像时它指向
// 服务端生成的默认头像。让这个字段可能为空，等于把「该显示什么」
// 这件事推给每一个客户端各写一遍。
type View struct {
	URL           string `json:"url"`
	Kind          string `json:"kind"`
	Version       string `json:"version,omitempty"`
	Blurhash      string `json:"blurhash,omitempty"`
	DominantColor string `json:"dominantColor,omitempty"`
	Animated      bool   `json:"animated,omitempty"`
	// Sizes 可直接请求的尺寸档，省得客户端猜。
	Sizes []int `json:"sizes,omitempty"`
}

// UploadOptions 上传时可选的处理参数。
type UploadOptions struct {
	// Crop 客户端裁剪框（原图像素坐标）。四项全为 0 表示不裁剪，由服务端居中取方。
	Crop *CropRect
}

// CropRect 裁剪框，坐标基于**EXIF 纠正之后**的图像。
// 基于原始像素会让所有竖拍照片裁错位置，而客户端预览时看到的正是纠正后的图。
type CropRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Empty 裁剪框是否等于「没传」。
func (c *CropRect) Empty() bool {
	return c == nil || c.Width <= 0 || c.Height <= 0
}
