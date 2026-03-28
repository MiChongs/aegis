package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	mathrand "math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	captchadomain "aegis/internal/domain/captcha"
	apperrors "aegis/pkg/errors"

	"github.com/fogleman/gg"
	gojson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"go.uber.org/zap"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// ────────────────────── 数据结构 ──────────────────────

// chiralAtom 分子中的原子
type chiralAtom struct {
	Symbol string  // C, O, N, F, Cl, Br, H, CH3, C2H5, COOH, SH, NH2, NO2, OH
	X, Y   float64 // 2D 渲染坐标
	Bonds  []int   // 连接的原子索引
}

// chiralMolecule 分子图
type chiralMolecule struct {
	Atoms []chiralAtom
}

// ChiralClickPoint 用户点击坐标
type ChiralClickPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// chiralTarget 手性碳验证目标
type chiralTarget struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Tolerance float64 `json:"t"`
}

type chiralRenderStyle struct {
	LabelFontSize float64
	BondWidth     float64
	CarbonRadius  float64
	LabelPadX     float64
	LabelPadY     float64
}

var (
	chiralFontOnce sync.Once
	chiralFont     *opentype.Font
	chiralFontErr  error
)

// ────────────────────── 取代基定义 ──────────────────────

// substituent 取代基（作为单原子/基团附加到主链碳上）
type substituent struct {
	Symbol string // 显示符号
	Weight int    // 选择权重（越大越常被选中）
}

var substituentPool = []substituent{
	{Symbol: "H", Weight: 3},
	{Symbol: "OH", Weight: 5},
	{Symbol: "NH2", Weight: 3},
	{Symbol: "F", Weight: 4},
	{Symbol: "Cl", Weight: 5},
	{Symbol: "Br", Weight: 4},
	{Symbol: "CH3", Weight: 6},
	{Symbol: "C2H5", Weight: 3},
	{Symbol: "COOH", Weight: 2},
	{Symbol: "SH", Weight: 2},
	{Symbol: "NO2", Weight: 1},
}

// ────────────────────── 分子生成 ──────────────────────

// generateRandomMolecule 生成随机有机分子（保证至少 1 个手性碳）
func generateRandomMolecule(canvasW, canvasH int) (*chiralMolecule, []int) {
	rng := mathrand.New(mathrand.NewSource(time.Now().UnixNano() + cryptoSeed()))

	for attempt := 0; attempt < 50; attempt++ {
		chainLen := 3 + rng.Intn(4) // 3-6 碳主链
		mol := &chiralMolecule{
			Atoms: make([]chiralAtom, 0, chainLen*3),
		}

		// 1. 生成主链碳原子（逻辑坐标中的锯齿形布局，后续统一缩放到画布）
		spacing := 170.0
		startX := -spacing * float64(chainLen-1) / 2
		centerY := 0.0
		zigzagY := 72.0

		for i := 0; i < chainLen; i++ {
			x := startX + float64(i)*spacing
			y := centerY
			if i%2 == 0 {
				y -= zigzagY
			} else {
				y += zigzagY
			}
			atom := chiralAtom{Symbol: "C", X: x, Y: y}
			mol.Atoms = append(mol.Atoms, atom)
		}

		// 主链键
		for i := 0; i < chainLen-1; i++ {
			mol.Atoms[i].Bonds = append(mol.Atoms[i].Bonds, i+1)
			mol.Atoms[i+1].Bonds = append(mol.Atoms[i+1].Bonds, i)
		}

		// 2. 给每个主链碳挂取代基
		for i := 0; i < chainLen; i++ {
			existingBonds := len(mol.Atoms[i].Bonds) // 主链连接数
			freeSlots := 4 - existingBonds           // 碳最多4键

			// 收集已挂的取代基符号（用于去重）
			usedSymbols := make(map[string]bool)
			// 主链邻居的"签名"已经不同（位置不同）

			for s := 0; s < freeSlots; s++ {
				sub := pickSubstituent(rng, usedSymbols)
				usedSymbols[sub.Symbol] = true
				subIdx := len(mol.Atoms)
				subAtom := chiralAtom{Symbol: sub.Symbol}

				// 布局：在逻辑坐标中向外延伸，后续统一拟合到画布
				baseX := mol.Atoms[i].X
				baseY := mol.Atoms[i].Y
				primaryAngle := -math.Pi / 2
				secondaryAngle := math.Pi / 2
				if i%2 == 1 {
					primaryAngle, secondaryAngle = secondaryAngle, primaryAngle
				}
				angle := 0.0
				dist := 138.0
				switch {
				case s == 0:
					angle = primaryAngle
				case s == 1:
					angle = secondaryAngle
				case s == 2:
					angle = primaryAngle - math.Pi/4
					dist = 152.0
				default:
					angle = secondaryAngle + math.Pi/4
					dist = 152.0
				}
				subAtom.X = baseX + dist*math.Cos(angle)
				subAtom.Y = baseY + dist*math.Sin(angle)

				mol.Atoms = append(mol.Atoms, subAtom)
				mol.Atoms[i].Bonds = append(mol.Atoms[i].Bonds, subIdx)
				mol.Atoms[subIdx].Bonds = append(mol.Atoms[subIdx].Bonds, i)
			}
		}

		// 3. 检测手性碳
		chiralIndices := detectChiralCarbons(mol, chainLen)
		if len(chiralIndices) > 0 {
			return mol, chiralIndices
		}

		// 无手性碳：尝试强制修改一个中间碳的取代基
		if chainLen >= 3 {
			targetC := 1 + rng.Intn(chainLen-2) // 选中间碳
			forceChiral(mol, targetC, rng)
			chiralIndices = detectChiralCarbons(mol, chainLen)
			if len(chiralIndices) > 0 {
				return mol, chiralIndices
			}
		}
	}

	// 兜底：生成一个简单的已知手性分子（2-溴丁烷）
	return generateFallbackMolecule(canvasW, canvasH)
}

// pickSubstituent 从池中加权随机选取一个取代基（尽量选不同的）
func pickSubstituent(rng *mathrand.Rand, used map[string]bool) substituent {
	// 优先选未使用的
	available := make([]substituent, 0, len(substituentPool))
	totalWeight := 0
	for _, s := range substituentPool {
		if !used[s.Symbol] {
			available = append(available, s)
			totalWeight += s.Weight
		}
	}
	if len(available) == 0 {
		available = substituentPool
		totalWeight = 0
		for _, s := range available {
			totalWeight += s.Weight
		}
	}

	r := rng.Intn(totalWeight)
	cumulative := 0
	for _, s := range available {
		cumulative += s.Weight
		if r < cumulative {
			return s
		}
	}
	return available[len(available)-1]
}

// forceChiral 强制修改一个碳的取代基使其变为手性碳
func forceChiral(mol *chiralMolecule, carbonIdx int, rng *mathrand.Rand) {
	atom := &mol.Atoms[carbonIdx]
	// 找到所有取代基（非主链碳的邻居）
	subIndices := []int{}
	for _, ni := range atom.Bonds {
		if mol.Atoms[ni].Symbol != "C" {
			subIndices = append(subIndices, ni)
		}
	}
	if len(subIndices) < 2 {
		return
	}

	// 确保所有取代基符号不同
	usedSymbols := make(map[string]bool)
	for _, ni := range atom.Bonds {
		usedSymbols[mol.Atoms[ni].Symbol] = true
	}

	// 找到重复的取代基并替换
	seen := make(map[string]bool)
	for _, si := range subIndices {
		sym := mol.Atoms[si].Symbol
		if seen[sym] {
			// 替换为一个未使用的
			for _, s := range substituentPool {
				if !usedSymbols[s.Symbol] {
					mol.Atoms[si].Symbol = s.Symbol
					usedSymbols[s.Symbol] = true
					break
				}
			}
		}
		seen[sym] = true
	}
}

// generateFallbackMolecule 兜底分子：2-溴丁烷 (CH3-CHBr-CH2-CH3)
func generateFallbackMolecule(canvasW, canvasH int) (*chiralMolecule, []int) {
	_ = canvasW
	_ = canvasH

	cx := 0.0
	cy := 0.0
	sp := 180.0

	mol := &chiralMolecule{}
	// 主链碳 0-3
	mol.Atoms = append(mol.Atoms, chiralAtom{Symbol: "C", X: cx - sp*1.5, Y: cy - 80})
	mol.Atoms = append(mol.Atoms, chiralAtom{Symbol: "C", X: cx - sp*0.5, Y: cy + 80})
	mol.Atoms = append(mol.Atoms, chiralAtom{Symbol: "C", X: cx + sp*0.5, Y: cy - 80})
	mol.Atoms = append(mol.Atoms, chiralAtom{Symbol: "C", X: cx + sp*1.5, Y: cy + 80})

	// 主链键
	for i := 0; i < 3; i++ {
		mol.Atoms[i].Bonds = append(mol.Atoms[i].Bonds, i+1)
		mol.Atoms[i+1].Bonds = append(mol.Atoms[i+1].Bonds, i)
	}

	// C0: CH3 → 取代基 H, H, H（终端甲基）
	addSub(mol, 0, "H", -110, -90)
	addSub(mol, 0, "H", -115, -10)
	addSub(mol, 0, "H", 0, -130)

	// C1: CHBr → 取代基 Br, H（手性碳）
	addSub(mol, 1, "Br", 0, 150)
	addSub(mol, 1, "H", -100, 120)

	// C2: CH2 → 取代基 H, H
	addSub(mol, 2, "H", 0, -150)
	addSub(mol, 2, "H", 95, -120)

	// C3: CH3 → 取代基 H, H, H
	addSub(mol, 3, "H", 110, 0)
	addSub(mol, 3, "H", 110, 90)
	addSub(mol, 3, "H", 0, 145)

	return mol, []int{1} // C1 是手性碳
}

func addSub(mol *chiralMolecule, parentIdx int, symbol string, dx, dy float64) {
	idx := len(mol.Atoms)
	mol.Atoms = append(mol.Atoms, chiralAtom{
		Symbol: symbol,
		X:      mol.Atoms[parentIdx].X + dx,
		Y:      mol.Atoms[parentIdx].Y + dy,
		Bonds:  []int{parentIdx},
	})
	mol.Atoms[parentIdx].Bonds = append(mol.Atoms[parentIdx].Bonds, idx)
}

// ────────────────────── 手性碳检测（Morgan 算法） ──────────────────────

// detectChiralCarbons 检测分子中所有手性碳（前 chainLen 个原子为主链碳）
func detectChiralCarbons(mol *chiralMolecule, chainLen int) []int {
	var result []int
	for i := 0; i < chainLen; i++ {
		atom := &mol.Atoms[i]
		if atom.Symbol != "C" {
			continue
		}
		if len(atom.Bonds) != 4 {
			continue
		}

		// 计算每个邻居的子树签名
		signatures := make([]string, 4)
		for j, ni := range atom.Bonds {
			signatures[j] = computeSubtreeSignature(mol, ni, i, 4)
		}

		// 检查 4 个签名是否互不相同
		if allDistinct(signatures) {
			result = append(result, i)
		}
	}
	return result
}

// computeSubtreeSignature 计算从 startIdx 出发（排除 excludeIdx）的子树签名
func computeSubtreeSignature(mol *chiralMolecule, startIdx, excludeIdx, maxDepth int) string {
	type bfsNode struct {
		idx   int
		depth int
	}

	visited := map[int]bool{excludeIdx: true}
	queue := []bfsNode{{idx: startIdx, depth: 0}}
	visited[startIdx] = true

	var parts []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		parts = append(parts, fmt.Sprintf("%d:%s", node.depth, mol.Atoms[node.idx].Symbol))

		if node.depth >= maxDepth {
			continue
		}
		for _, ni := range mol.Atoms[node.idx].Bonds {
			if !visited[ni] {
				visited[ni] = true
				queue = append(queue, bfsNode{idx: ni, depth: node.depth + 1})
			}
		}
	}

	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func allDistinct(items []string) bool {
	seen := make(map[string]bool, len(items))
	for _, s := range items {
		if seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}

// ────────────────────── 2D 渲染 ──────────────────────

func cloneMolecule(mol *chiralMolecule) *chiralMolecule {
	clone := &chiralMolecule{Atoms: make([]chiralAtom, len(mol.Atoms))}
	for i, atom := range mol.Atoms {
		bonds := make([]int, len(atom.Bonds))
		copy(bonds, atom.Bonds)
		clone.Atoms[i] = chiralAtom{
			Symbol: atom.Symbol,
			X:      atom.X,
			Y:      atom.Y,
			Bonds:  bonds,
		}
	}
	return clone
}

func atomLayoutPadding(symbol string) (float64, float64) {
	switch symbol {
	case "H":
		return 0, 0
	case "C":
		return 28, 28
	default:
		return 48 + float64(len(symbol))*16, 38
	}
}

func fitMoleculeToCanvas(mol *chiralMolecule, width, height int) *chiralMolecule {
	fitted := cloneMolecule(mol)
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64

	for _, atom := range fitted.Atoms {
		if atom.Symbol == "H" {
			continue
		}
		padX, padY := atomLayoutPadding(atom.Symbol)
		minX = math.Min(minX, atom.X-padX)
		maxX = math.Max(maxX, atom.X+padX)
		minY = math.Min(minY, atom.Y-padY)
		maxY = math.Max(maxY, atom.Y+padY)
	}

	if minX == math.MaxFloat64 || minY == math.MaxFloat64 {
		return fitted
	}

	layoutW := math.Max(maxX-minX, 1)
	layoutH := math.Max(maxY-minY, 1)
	targetW := float64(width) * 0.78
	targetH := float64(height) * 0.68
	scale := math.Min(targetW/layoutW, targetH/layoutH)
	centerX := (minX + maxX) / 2
	centerY := (minY + maxY) / 2
	destX := float64(width) / 2
	destY := float64(height) / 2

	for i := range fitted.Atoms {
		fitted.Atoms[i].X = (fitted.Atoms[i].X-centerX)*scale + destX
		fitted.Atoms[i].Y = (fitted.Atoms[i].Y-centerY)*scale + destY
	}

	return fitted
}

func chiralRenderFace(size float64) font.Face {
	chiralFontOnce.Do(func() {
		chiralFont, chiralFontErr = opentype.Parse(goregular.TTF)
	})
	if chiralFontErr != nil {
		return basicfont.Face7x13
	}
	face, err := opentype.NewFace(chiralFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return basicfont.Face7x13
	}
	return face
}

func chiralRenderPalette(symbol string) color.Color {
	switch symbol {
	case "O", "OH", "COOH":
		return color.RGBA{R: 204, G: 35, B: 35, A: 255}
	case "N", "NH2", "NO2":
		return color.RGBA{R: 27, G: 74, B: 178, A: 255}
	case "F", "Cl", "Br":
		return color.RGBA{R: 35, G: 135, B: 55, A: 255}
	case "S", "SH":
		return color.RGBA{R: 178, G: 136, B: 19, A: 255}
	default:
		return color.RGBA{R: 42, G: 46, B: 54, A: 255}
	}
}

func renderMolecule(mol *chiralMolecule, width, height int) ([]byte, *chiralMolecule) {
	fitted := fitMoleculeToCanvas(mol, width, height)
	supersample := 2
	hiW, hiH := width*supersample, height*supersample
	style := chiralRenderStyle{
		LabelFontSize: math.Max(24, math.Min(34, float64(min(width, height))/34)),
		BondWidth:     math.Max(2.8, float64(min(width, height))/280),
		CarbonRadius:  math.Max(5.5, float64(min(width, height))/155),
		LabelPadX:     math.Max(8, float64(min(width, height))/90),
		LabelPadY:     math.Max(5, float64(min(width, height))/140),
	}

	dc := gg.NewContext(hiW, hiH)
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	dc.Scale(float64(supersample), float64(supersample))
	dc.SetLineCapRound()
	dc.SetLineJoinRound()

	labelFace := chiralRenderFace(style.LabelFontSize)
	dc.SetFontFace(labelFace)

	bondColor := color.RGBA{R: 56, G: 61, B: 70, A: 255}
	drawnBonds := make(map[[2]int]bool)
	for i, atom := range fitted.Atoms {
		for _, j := range atom.Bonds {
			key := [2]int{min(i, j), max(i, j)}
			if drawnBonds[key] {
				continue
			}
			drawnBonds[key] = true

			a1 := fitted.Atoms[i]
			a2 := fitted.Atoms[j]
			if a1.Symbol == "H" || a2.Symbol == "H" {
				continue
			}

			dc.SetColor(bondColor)
			dc.SetLineWidth(style.BondWidth)
			dc.DrawLine(a1.X, a1.Y, a2.X, a2.Y)
			dc.Stroke()
		}
	}

	for _, atom := range fitted.Atoms {
		if atom.Symbol == "H" {
			continue
		}

		if atom.Symbol == "C" {
			dc.SetRGB255(49, 54, 63)
			dc.DrawCircle(atom.X, atom.Y, style.CarbonRadius)
			dc.Fill()

			dc.SetRGB255(255, 255, 255)
			dc.SetLineWidth(math.Max(1.1, style.BondWidth*0.32))
			dc.DrawCircle(atom.X, atom.Y, style.CarbonRadius+1.2)
			dc.Stroke()
			continue
		}

		labelColor := chiralRenderPalette(atom.Symbol)
		labelW, labelH := dc.MeasureString(atom.Symbol)
		boxW := labelW + style.LabelPadX*2
		boxH := labelH + style.LabelPadY*2

		dc.SetRGBA255(255, 255, 255, 248)
		dc.DrawRoundedRectangle(atom.X-boxW/2, atom.Y-boxH/2, boxW, boxH, boxH*0.34)
		dc.Fill()

		dc.SetRGBA255(220, 224, 231, 240)
		dc.SetLineWidth(math.Max(0.7, style.BondWidth*0.18))
		dc.DrawRoundedRectangle(atom.X-boxW/2, atom.Y-boxH/2, boxW, boxH, boxH*0.34)
		dc.Stroke()

		dc.SetColor(labelColor)
		dc.DrawStringAnchored(atom.Symbol, atom.X, atom.Y, 0.5, 0.5)
	}

	hiImg := dc.Image()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(img, img.Bounds(), hiImg, hiImg.Bounds(), xdraw.Src, nil)

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes(), fitted
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func cryptoSeed() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24
}

// ────────────────────── 服务集成 ──────────────────────

// rdkitChiralResult Python 脚本输出结构
type rdkitChiralResult struct {
	Image       string         `json:"image"`
	Targets     []chiralTarget `json:"targets"`
	Hint        string         `json:"hint"`
	SMILES      string         `json:"smiles"`
	ChiralCount int            `json:"chiralCount"`
	Error       string         `json:"error,omitempty"`
}

// GenerateChiralCaptcha 生成手性碳点选验证码（调用 RDKit Python 子进程）
func (s *CaptchaService) GenerateChiralCaptcha(ctx context.Context, req captchadomain.GenerateRequest) (*captchadomain.GenerateResult, error) {
	if !s.cfg.Captcha.Enabled {
		return nil, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}

	const canvasW, canvasH = 1600, 1200

	// 调用 Python RDKit 脚本生成分子图
	rdkitResult, err := s.callRDKitService(ctx, canvasW, canvasH)
	if err != nil {
		s.log.Error("RDKit 脚本调用失败，降级为 Go 原生生成", zap.Error(err))
		return s.generateChiralFallback(ctx, req, canvasW, canvasH)
	}

	if rdkitResult.Error != "" || len(rdkitResult.Targets) == 0 {
		s.log.Warn("RDKit 脚本返回错误，降级为 Go 原生生成", zap.String("error", rdkitResult.Error))
		return s.generateChiralFallback(ctx, req, canvasW, canvasH)
	}

	b64 := "data:image/png;base64," + rdkitResult.Image

	answerJSON, _ := gojson.Marshal(rdkitResult.Targets)
	captchaID := newCaptchaID()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)
	record := captchadomain.CaptchaRecord{
		Answer:    string(answerJSON),
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}

	hint := rdkitResult.Hint
	if hint == "" {
		hint = "请找出并点击分子中所有的手性碳原子"
	}

	return &captchadomain.GenerateResult{
		CaptchaID:     captchaID,
		ImageData:     b64,
		MimeType:      "image/png",
		ClickRequired: true,
		ImageWidth:    canvasW,
		ImageHeight:   canvasH,
		Hint:          hint,
		ChiralCount:   encodeChiralCount(len(rdkitResult.Targets)),
		ExpiresAt:     expiresAt.Unix(),
	}, nil
}

// callRDKitService 调用 RDKit HTTP 微服务
func (s *CaptchaService) callRDKitService(ctx context.Context, width, height int) (*rdkitChiralResult, error) {
	url := strings.TrimRight(s.cfg.RDKitCaptchaURL, "/") + fmt.Sprintf("/generate?width=%d&height=%d", width, height)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rdkit service request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rdkit service read failed: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("rdkit service returned %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	var result rdkitChiralResult
	if err := gojson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("rdkit service parse failed: %w", err)
	}
	return &result, nil
}

// generateChiralFallback Go 原生降级生成（当 RDKit 不可用时）
func (s *CaptchaService) generateChiralFallback(ctx context.Context, req captchadomain.GenerateRequest, canvasW, canvasH int) (*captchadomain.GenerateResult, error) {
	mol, chiralIndices := generateRandomMolecule(canvasW, canvasH)
	pngData, renderedMol := renderMolecule(mol, canvasW, canvasH)
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)

	targets := make([]chiralTarget, 0, len(chiralIndices))
	for _, idx := range chiralIndices {
		atom := renderedMol.Atoms[idx]
		targets = append(targets, chiralTarget{
			X:         atom.X / float64(canvasW),
			Y:         atom.Y / float64(canvasH),
			Tolerance: 0.055,
		})
	}

	answerJSON, _ := gojson.Marshal(targets)
	captchaID := newCaptchaID()
	expiresAt := time.Now().Add(s.cfg.Captcha.TTL)
	record := captchadomain.CaptchaRecord{
		Answer:    string(answerJSON),
		Purpose:   req.Purpose,
		Scope:     req.Scope,
		AppID:     req.AppID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.SetCaptcha(ctx, captchaID, record, s.cfg.Captcha.TTL); err != nil {
		return nil, apperrors.New(50011, http.StatusInternalServerError, "存储验证码失败")
	}

	return &captchadomain.GenerateResult{
		CaptchaID:     captchaID,
		ImageData:     b64,
		MimeType:      "image/png",
		ClickRequired: true,
		ImageWidth:    canvasW,
		ImageHeight:   canvasH,
		Hint:          "请找出并点击分子中所有的手性碳原子",
		ChiralCount:   encodeChiralCount(len(targets)),
		ExpiresAt:     expiresAt.Unix(),
	}, nil
}

// encodeChiralCount 混淆手性碳数量：随机盐 + XOR + base64，防止前端直接读取明文数字
func encodeChiralCount(count int) string {
	salt := make([]byte, 4)
	_, _ = rand.Read(salt)
	val := make([]byte, 4)
	binary.LittleEndian.PutUint32(val, uint32(count)^binary.LittleEndian.Uint32(salt))
	return base64.RawURLEncoding.EncodeToString(append(salt, val...))
}

// VerifyClick 验证坐标点选（手性碳验证码）— 需要点中所有手性碳
func (s *CaptchaService) VerifyClick(ctx context.Context, captchaID string, clicks []ChiralClickPoint) (bool, error) {
	if !s.cfg.Captcha.Enabled {
		return false, apperrors.New(40310, http.StatusForbidden, "验证码服务未启用")
	}

	record, err := s.repo.GetCaptcha(ctx, captchaID)
	if err != nil {
		return false, err
	}
	if record == nil {
		return false, apperrors.New(40011, http.StatusBadRequest, "验证码不存在或已过期")
	}

	if record.Attempts >= 5 {
		_ = s.repo.DeleteCaptcha(ctx, captchaID)
		return false, apperrors.New(42900, http.StatusTooManyRequests, "验证码尝试次数过多，请刷新重试")
	}

	var targets []chiralTarget
	if err := gojson.Unmarshal([]byte(record.Answer), &targets); err != nil {
		return false, apperrors.New(50012, http.StatusInternalServerError, "验证码数据异常")
	}

	// 检查每个目标是否被至少一个点击命中
	if len(clicks) < len(targets) {
		_, _ = s.repo.IncrementCaptchaAttempts(ctx, captchaID)
		return false, nil // 点击数不够
	}

	matched := make([]bool, len(targets))
	for _, click := range clicks {
		for i, t := range targets {
			if matched[i] {
				continue
			}
			dx := click.X - t.X
			dy := click.Y - t.Y
			if dx*dx+dy*dy <= t.Tolerance*t.Tolerance {
				matched[i] = true
				break
			}
		}
	}

	allMatched := true
	for _, m := range matched {
		if !m {
			allMatched = false
			break
		}
	}

	if allMatched {
		// 不立即删除，而是标记为已验证（Answer 改为 "VERIFIED"），设短 TTL 30 秒
		// 后续 AdminLogin/Register 提交时通过 Verify 检查此标记
		verifiedRecord := captchadomain.CaptchaRecord{
			Answer:    "VERIFIED",
			Purpose:   record.Purpose,
			Scope:     record.Scope,
			AppID:     record.AppID,
			CreatedAt: record.CreatedAt,
		}
		_ = s.repo.SetCaptcha(ctx, captchaID, verifiedRecord, 30*time.Second)
		return true, nil
	}

	// 未命中：记录尝试次数
	_, _ = s.repo.IncrementCaptchaAttempts(ctx, captchaID)
	return false, nil
}

func newCaptchaID() string {
	return fmt.Sprintf("chiral_%s", uuid.New().String())
}
