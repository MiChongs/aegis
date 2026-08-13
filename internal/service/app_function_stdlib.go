package service

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/big"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/clbanning/mxj/v2"
	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/require"
	gojaurl "github.com/dop251/goja_nodejs/url"
	"github.com/goccy/go-yaml"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/inbucket/html2text"
	"github.com/microcosm-cc/bluemonday"
	"github.com/mileusna/useragent"
	"github.com/mozillazg/go-pinyin"
	"github.com/pquerna/otp/totp"
	"github.com/robfig/cron/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/sha3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// 脚本标准库 —— 免声明即可用的那一批。
//
// 判据只有一条：**纯计算，不碰平台数据、不出网、不产生副作用**。
// 满足这条的东西不该要一次能力声明 —— 声明的意义是「授权访问某样东西」，
// 而这里没有任何东西可授权。
//
// 不给的代价不是「作者少了个便利」，而是他会在脚本里**自己手写一份**：
// 手写的 HMAC 拼串顺序不稳定（签名时对时不对）、手写的日切按本地时区
// （跨时区就错一天）、手写的金额用 JS 的 number（0.1+0.2 落在钱上就是对不上账）。
// 这三种错误都不会报错，只会在某个时刻悄悄给出错误结果。
const (
	// maxStdlibInputBytes 单次调用传给标准库函数的字符串上限。
	// 没有它的话，一次 gzip 或一次模板渲染就能把整个进程的内存吃掉，
	// 而脚本自身的超时闸门管不着已经分配出去的那块内存。
	maxStdlibInputBytes = 1 << 20
	// maxBcryptCost bcrypt 的代价上限。14 已经是每次约 1 秒，
	// 再往上单次调用必然超时，且会占着一个并发槽位。
	maxBcryptCost = 14
	// maxPBKDF2Iterations 同理：迭代次数由脚本指定，不封顶等于给了一个 CPU 炸弹。
	maxPBKDF2Iterations = 1 << 20
)

// sandboxModules 是共享的 goja_nodejs 模块注册表。
//
// 加载器恒定拒绝：core module（buffer / url）不经过它，因此 Buffer 与 URL
// 照常可用，而 `require("./任意文件")` 读不到宿主文件系统 —— 默认加载器
// 是**直接读磁盘**的，沿用它等于给脚本开了一个文件读取入口。
// 注册表本身设计为可跨 runtime 共享（内部有锁与编译缓存），因此只建一次。
var sandboxModules = require.NewRegistry(require.WithLoader(func(string) ([]byte, error) {
	return nil, require.ModuleFileDoesNotExistError
}))

// scriptPrelude 在宿主原语之上补齐几个标准全局。
//
// 写成一个立即可调用的工厂函数而不是直接赋值给全局：这样宿主原语
// （__utf8Encode 之流）只作为参数存在于闭包里，脚本在全局上找不到它们，
// 也就不可能绕过封装直接调。
const scriptPrelude = `(function (utf8Encode, utf8Decode, b64Encode, b64Decode) {
  function TextEncoder() {}
  TextEncoder.prototype.encode = function (input) {
    return new Uint8Array(utf8Encode(input === undefined ? "" : String(input)));
  };
  Object.defineProperty(TextEncoder.prototype, "encoding", { get: function () { return "utf-8"; } });

  function TextDecoder(label) { this._encoding = (label || "utf-8").toLowerCase(); }
  TextDecoder.prototype.decode = function (input) {
    return input === undefined ? "" : utf8Decode(input);
  };
  Object.defineProperty(TextDecoder.prototype, "encoding", { get: function () { return this._encoding; } });

  globalThis.TextEncoder = TextEncoder;
  globalThis.TextDecoder = TextDecoder;
  globalThis.btoa = function (input) { return b64Encode(String(input)); };
  globalThis.atob = function (input) { return b64Decode(String(input)); };
})`

var (
	// scriptSanitizer 与公告正文用的是同一套白名单取向：放行排版标签与 class，
	// 拒绝 style 与事件属性。脚本产出的 HTML 最终会进邮件与 WebView，
	// 让每个作者自己想白名单，漏一个就是一次存储型 XSS。
	scriptSanitizer = newScriptSanitizer()

	slugSeparatorPattern = regexp.MustCompile(`[^a-z0-9]+`)
	emailPattern         = regexp.MustCompile(`^[^@\s]+@[^@\s.]+(\.[^@\s.]+)+$`)
	uuidPattern          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	chinaPhonePattern    = regexp.MustCompile(`^1[3-9]\d{9}$`)

	// cronParser 与平台其它定时任务同一套语法（五段，不含秒），
	// 否则脚本算出来的「下一次触发」与调度器实际触发的时刻会差一整个字段。
	cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	// schemaErrorPrinter jsonschema 的错误文案要一个 printer 才能渲染，
	// 传 nil 会在库内部空指针 —— 它自己也是拿一个包级默认 printer 调的。
	schemaErrorPrinter = message.NewPrinter(language.English)

	// preludeProgram 只编译一次：它每个 runtime 都要跑一遍，
	// 而 goja 的编译在这条热路径上比执行本身还贵。
	preludeProgram = goja.MustCompile("aegis-prelude.js", scriptPrelude, true)
)

func newScriptSanitizer() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.AllowAttrs("class").Globally()
	policy.RequireNoFollowOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return policy
}

// installSandboxGlobals 装上沙箱里那批标准全局：Buffer / URL / URLSearchParams /
// TextEncoder / TextDecoder / atob / btoa。
//
// 它们全部是**纯内存类型**，没有任何 I/O —— 「沙箱里没有 Node」与
// 「沙箱里连一个字节缓冲类型都没有」是两回事，后者纯粹是缺东西：
// 接第三方接口时二进制处理是常态，没有 Buffer 的话作者只能用字符串
// 硬拼字节，那才是真正容易出错的做法。
//
// require 全局在装完之后被删掉：模块系统只是 buffer / url 的装载途径，
// 它本身不该留在脚本可见的世界里（脚本也确实没有第二个模块可加载）。
func installSandboxGlobals(vm *goja.Runtime) error {
	sandboxModules.Enable(vm)
	buffer.Enable(vm)
	gojaurl.Enable(vm)
	if err := vm.GlobalObject().Delete("require"); err != nil {
		return fmt.Errorf("移除 require 全局失败: %w", err)
	}

	factory, err := vm.RunProgram(preludeProgram)
	if err != nil {
		return fmt.Errorf("装载沙箱标准全局失败: %w", err)
	}
	install, ok := goja.AssertFunction(factory)
	if !ok {
		return fmt.Errorf("沙箱标准全局的装载入口不是函数")
	}
	_, err = install(goja.Undefined(),
		vm.ToValue(func(input string) goja.ArrayBuffer {
			return vm.NewArrayBuffer([]byte(input))
		}),
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(string(buffer.Bytes(vm, call.Argument(0))))
		}),
		vm.ToValue(func(input string) string {
			return base64.StdEncoding.EncodeToString([]byte(toLatin1(input)))
		}),
		vm.ToValue(func(call goja.FunctionCall) goja.Value {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(call.Argument(0).String()))
			if err != nil {
				panic(vm.NewTypeError("atob: 不是合法的 base64"))
			}
			return vm.ToValue(fromLatin1(decoded))
		}),
	)
	return err
}

// toLatin1 / fromLatin1 复刻浏览器 btoa/atob 的「二进制串」语义：
// 每个字符必须落在 0..255。直接按 UTF-8 编码会让 btoa("中") 得出
// 与浏览器不同的结果，而那种差异只会在对接方验签失败时才被发现。
func toLatin1(input string) string {
	out := make([]byte, 0, len(input))
	for _, r := range input {
		if r > 0xFF {
			panic("btoa: 字符超出 Latin-1 范围，请先用 TextEncoder 编码")
		}
		out = append(out, byte(r))
	}
	return string(out)
}

func fromLatin1(data []byte) string {
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes)
}

// ── aegis.crypto 扩展 ───────────────────────────────────────────────

// extendCrypto 在基础摘要之上补齐「接第三方接口真正会用到」的那一批。
//
// 每一项都对应一类作者原本只能手写的东西：对称加密（手写必错）、
// JWT（手写的 base64url 与 padding 十有八九不对）、TOTP（时间窗口容忍
// 写错就是「有时能登有时不能」）、bcrypt / PBKDF2 / HKDF（自己拿 sha256
// 循环一万次不是 PBKDF2）。
func (s *ScriptSDK) extendCrypto(vm *goja.Runtime, object *goja.Object) {
	_ = object.Set("sha3", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		sum := sha3.Sum256([]byte(call.Argument(0).String()))
		return vm.ToValue(hex.EncodeToString(sum[:]))
	})
	_ = object.Set("crc32", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(call.Argument(0).String()))))
	})
	_ = object.Set("hmacMd5", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		writer := hmac.New(md5.New, []byte(call.Argument(0).String()))
		writer.Write([]byte(call.Argument(1).String()))
		return vm.ToValue(hex.EncodeToString(writer.Sum(nil)))
	})
	_ = object.Set("hmacSha1", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		writer := hmac.New(sha1.New, []byte(call.Argument(0).String()))
		writer.Write([]byte(call.Argument(1).String()))
		return vm.ToValue(hex.EncodeToString(writer.Sum(nil)))
	})
	_ = object.Set("randomBytes", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		size := int(call.Argument(0).ToInteger())
		if size <= 0 || size > 64 {
			size = 32
		}
		payload := make([]byte, size)
		if _, err := rand.Read(payload); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(base64.StdEncoding.EncodeToString(payload))
	})

	_ = object.Set("aesEncrypt", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		plaintext := requireStdlibString(vm, call.Argument(1))
		gcm := s.aeadFromKey(vm, call.Argument(0).String())
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			throw(vm, err)
		}
		sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
		return vm.ToValue(base64.StdEncoding.EncodeToString(sealed))
	})
	_ = object.Set("aesDecrypt", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(call.Argument(1).String()))
		if err != nil {
			throw(vm, fmt.Errorf("密文不是合法 base64: %w", err))
		}
		gcm := s.aeadFromKey(vm, call.Argument(0).String())
		if len(raw) < gcm.NonceSize() {
			panic(vm.ToValue("密文长度不足"))
		}
		plaintext, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
		if err != nil {
			// 认证失败一律报错，绝不返回「尽力而为」的半截明文：
			// GCM 的价值就在于改过一个比特就解不开。
			throw(vm, fmt.Errorf("解密失败：密钥不对或密文被改过"))
		}
		return vm.ToValue(string(plaintext))
	})

	_ = object.Set("jwtSign", s.bindJWTSign(vm))
	_ = object.Set("jwtVerify", s.bindJWTVerify(vm))

	_ = object.Set("totpVerify", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(totp.Validate(
			strings.TrimSpace(call.Argument(1).String()),
			strings.TrimSpace(call.Argument(0).String())))
	})

	_ = object.Set("bcryptHash", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		cost := int(call.Argument(1).ToInteger())
		if cost < bcrypt.MinCost || cost > maxBcryptCost {
			cost = bcrypt.DefaultCost
		}
		// bcrypt 只哈希前 72 字节，超长直接报错而不是静默截断 ——
		// 静默截断会让「前 72 字节相同的两个口令可以互相验过」。
		password := call.Argument(0).String()
		if len(password) > 72 {
			panic(vm.ToValue("bcrypt 最多接受 72 字节，请先做一次摘要"))
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(password), cost)
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(string(hashed))
	})
	_ = object.Set("bcryptVerify", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		err := bcrypt.CompareHashAndPassword(
			[]byte(call.Argument(0).String()), []byte(call.Argument(1).String()))
		return vm.ToValue(err == nil)
	})

	_ = object.Set("pbkdf2", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		iterations := int(call.Argument(2).ToInteger())
		if iterations <= 0 {
			iterations = 100000
		}
		if iterations > maxPBKDF2Iterations {
			panic(vm.ToValue(fmt.Sprintf("迭代次数不能超过 %d", maxPBKDF2Iterations)))
		}
		length := int(call.Argument(3).ToInteger())
		if length <= 0 || length > 128 {
			length = 32
		}
		derived := pbkdf2.Key([]byte(call.Argument(0).String()),
			[]byte(call.Argument(1).String()), iterations, length, sha256.New)
		return vm.ToValue(hex.EncodeToString(derived))
	})
	_ = object.Set("hkdf", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		length := int(call.Argument(3).ToInteger())
		if length <= 0 || length > 128 {
			length = 32
		}
		reader := hkdf.New(sha256.New, []byte(call.Argument(0).String()),
			[]byte(call.Argument(1).String()), []byte(call.Argument(2).String()))
		derived := make([]byte, length)
		if _, err := io.ReadFull(reader, derived); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(hex.EncodeToString(derived))
	})
}

// aeadFromKey 把任意长度的密钥收敛成 AES-256 的 32 字节。
//
// 要求作者恰好给 32 字节是不现实的（他手上的密钥来自第三方控制台），
// 而截断或补零会让两个不同的密钥派生出同一把 —— 过一次 SHA-256 是
// 唯一既不挑长度又不产生碰撞的做法。
func (s *ScriptSDK) aeadFromKey(vm *goja.Runtime, key string) cipher.AEAD {
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		throw(vm, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		throw(vm, err)
	}
	return gcm
}

func (s *ScriptSDK) bindJWTSign(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	methods := map[string]jwt.SigningMethod{
		"HS256": jwt.SigningMethodHS256,
		"HS384": jwt.SigningMethodHS384,
		"HS512": jwt.SigningMethodHS512,
	}
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		secret := call.Argument(0).String()
		if strings.TrimSpace(secret) == "" {
			panic(vm.ToValue("JWT 密钥不能为空"))
		}
		claims := jwt.MapClaims{}
		if exported, ok := call.Argument(1).Export().(map[string]any); ok {
			for key, value := range exported {
				claims[key] = value
			}
		}
		method := jwt.SigningMethod(jwt.SigningMethodHS256)
		expiresIn := int64(0)
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			if options, ok := call.Argument(2).Export().(map[string]any); ok {
				if name, ok := options["alg"].(string); ok {
					resolved, supported := methods[strings.ToUpper(name)]
					if !supported {
						// 只支持 HMAC 家族：非对称签名要求脚本持有私钥，
						// 而 KV 里存私钥比存共享密钥危险得多，值得单独设计。
						panic(vm.ToValue("仅支持 HS256 / HS384 / HS512"))
					}
					method = resolved
				}
				expiresIn = toInt64(options["expiresIn"])
			}
		}
		now := time.Now()
		if _, exists := claims["iat"]; !exists {
			claims["iat"] = now.Unix()
		}
		if expiresIn > 0 {
			claims["exp"] = now.Add(time.Duration(expiresIn) * time.Second).Unix()
		}
		signed, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(signed)
	}
}

func (s *ScriptSDK) bindJWTVerify(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		secret := []byte(call.Argument(0).String())
		claims := jwt.MapClaims{}
		// 显式限定算法族：不限定的话攻击者把 alg 改成 none 就能伪造令牌，
		// 这是 JWT 最经典的一个坑，而库默认不替我们挡。
		_, err := jwt.ParseWithClaims(strings.TrimSpace(call.Argument(1).String()), claims,
			func(*jwt.Token) (any, error) { return secret, nil },
			jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}))
		if err != nil {
			// 校验失败是**正常分支**（令牌过期是最常见的情形），
			// 抛错会逼每个作者写一圈 try/catch，而沙箱里的 catch 拿不到结构化原因。
			return vm.ToValue(map[string]any{"valid": false, "error": err.Error()})
		}
		return vm.ToValue(map[string]any{"valid": true, "claims": map[string]any(claims)})
	}
}

// ── aegis.text ──────────────────────────────────────────────────────

func (s *ScriptSDK) bindText(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()

	_ = object.Set("template", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		source := requireStdlibString(vm, call.Argument(0))
		parsed, err := template.New("script").Option("missingkey=zero").Parse(source)
		if err != nil {
			throw(vm, fmt.Errorf("模板语法错误: %w", err))
		}
		data, _ := call.Argument(1).Export().(map[string]any)
		var rendered bytes.Buffer
		if err := parsed.Execute(&rendered, data); err != nil {
			throw(vm, fmt.Errorf("模板渲染失败: %w", err))
		}
		return vm.ToValue(rendered.String())
	})

	_ = object.Set("slugify", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		lower := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		return vm.ToValue(strings.Trim(slugSeparatorPattern.ReplaceAllString(lower, "-"), "-"))
	})

	_ = object.Set("pinyin", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		args := pinyin.NewArgs()
		separator := " "
		switch call.Argument(1).String() {
		case "tone":
			args.Style = pinyin.Tone
		case "initials":
			args.Style = pinyin.FirstLetter
			separator = ""
		}
		segments := pinyin.Pinyin(call.Argument(0).String(), args)
		parts := make([]string, 0, len(segments))
		for _, segment := range segments {
			if len(segment) > 0 {
				parts = append(parts, segment[0])
			}
		}
		return vm.ToValue(strings.Join(parts, separator))
	})

	// 复用平台既有的遮罩口径：同一个邮箱在资料页、审计日志与脚本产出里
	// 必须遮成同一个样子，各写一份的结果是同一条记录看起来像两个人。
	_ = object.Set("maskEmail", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(maskEmail(call.Argument(0).String()))
	})
	_ = object.Set("maskPhone", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(maskPhoneValue(call.Argument(0).String()))
	})

	_ = object.Set("truncate", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		// 按 rune 截断：按字节切会把一个汉字劈成两半，
		// 而那半个字节在 JSON 里是一个替换字符，接收端看到的是乱码。
		runes := []rune(call.Argument(0).String())
		limit := int(call.Argument(1).ToInteger())
		if limit <= 0 || len(runes) <= limit {
			return vm.ToValue(string(runes))
		}
		ellipsis := "…"
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			ellipsis = call.Argument(2).String()
		}
		return vm.ToValue(string(runes[:limit]) + ellipsis)
	})

	_ = object.Set("stripHtml", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		text, err := html2text.FromString(requireStdlibString(vm, call.Argument(0)),
			html2text.Options{OmitLinks: true})
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(strings.TrimSpace(text))
	})
	_ = object.Set("sanitizeHtml", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(scriptSanitizer.Sanitize(requireStdlibString(vm, call.Argument(0))))
	})
	_ = object.Set("escapeHtml", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		replacer := strings.NewReplacer(
			"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
		return vm.ToValue(replacer.Replace(call.Argument(0).String()))
	})
	_ = object.Set("length", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(len([]rune(call.Argument(0).String())))
	})

	return object
}

// ── aegis.encoding ──────────────────────────────────────────────────

func (s *ScriptSDK) bindEncoding(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()

	_ = object.Set("yamlParse", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		var decoded any
		if err := yaml.Unmarshal([]byte(requireStdlibString(vm, call.Argument(0))), &decoded); err != nil {
			throw(vm, fmt.Errorf("YAML 解析失败: %w", err))
		}
		return vm.ToValue(decoded)
	})
	_ = object.Set("yamlStringify", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		encoded, err := yaml.Marshal(call.Argument(0).Export())
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(string(encoded))
	})

	_ = object.Set("csvParse", s.bindCSVParse(vm))
	_ = object.Set("csvStringify", s.bindCSVStringify(vm))

	_ = object.Set("xmlToJson", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		decoded, err := mxj.NewMapXml([]byte(requireStdlibString(vm, call.Argument(0))))
		if err != nil {
			throw(vm, fmt.Errorf("XML 解析失败: %w", err))
		}
		return vm.ToValue(map[string]any(decoded))
	})
	_ = object.Set("jsonToXml", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		payload, ok := call.Argument(0).Export().(map[string]any)
		if !ok {
			panic(vm.ToValue("jsonToXml 只接受对象"))
		}
		root := "xml"
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			root = call.Argument(1).String()
		}
		encoded, err := mxj.Map(payload).Xml(root)
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(string(encoded))
	})

	_ = object.Set("queryParse", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		values, err := url.ParseQuery(strings.TrimPrefix(call.Argument(0).String(), "?"))
		if err != nil {
			throw(vm, fmt.Errorf("query 解析失败: %w", err))
		}
		out := make(map[string]any, len(values))
		for key, list := range values {
			if len(list) == 1 {
				out[key] = list[0]
				continue
			}
			out[key] = list
		}
		return vm.ToValue(out)
	})
	_ = object.Set("queryStringify", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		payload, ok := call.Argument(0).Export().(map[string]any)
		if !ok {
			panic(vm.ToValue("queryStringify 只接受对象"))
		}
		// 键按字典序输出：签名串的顺序必须稳定，而 JS 对象的
		// 遍历顺序对数字键与字符串键规则不同，靠它排序迟早出事。
		keys := make([]string, 0, len(payload))
		for key := range payload {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+scriptScalarString(payload[key]))
		}
		return vm.ToValue(strings.Join(parts, "&"))
	})

	_ = object.Set("urlEncode", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(url.QueryEscape(call.Argument(0).String()))
	})
	_ = object.Set("urlDecode", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		decoded, err := url.QueryUnescape(call.Argument(0).String())
		if err != nil {
			throw(vm, fmt.Errorf("URL 解码失败: %w", err))
		}
		return vm.ToValue(decoded)
	})

	_ = object.Set("gzip", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write([]byte(requireStdlibString(vm, call.Argument(0)))); err != nil {
			throw(vm, err)
		}
		if err := writer.Close(); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(base64.StdEncoding.EncodeToString(compressed.Bytes()))
	})
	_ = object.Set("gunzip", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(call.Argument(0).String()))
		if err != nil {
			throw(vm, fmt.Errorf("不是合法 base64: %w", err))
		}
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			throw(vm, fmt.Errorf("不是合法的 gzip 数据: %w", err))
		}
		defer reader.Close()
		// 限长读取：压缩比可以做到上千倍，不限长的话一个几 KB 的输入
		// 就能解出几百兆（zip bomb），而那块内存在超时之前已经分配出去了。
		plain, err := io.ReadAll(io.LimitReader(reader, maxStdlibInputBytes+1))
		if err != nil {
			throw(vm, err)
		}
		if len(plain) > maxStdlibInputBytes {
			panic(vm.ToValue(fmt.Sprintf("解压结果超过 %d KB 上限", maxStdlibInputBytes>>10)))
		}
		return vm.ToValue(string(plain))
	})

	return object
}

func (s *ScriptSDK) bindCSVParse(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		reader := csv.NewReader(strings.NewReader(requireStdlibString(vm, call.Argument(0))))
		reader.FieldsPerRecord = -1
		header := true
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			if options, ok := call.Argument(1).Export().(map[string]any); ok {
				if value, ok := options["header"].(bool); ok {
					header = value
				}
				if value, ok := options["delimiter"].(string); ok && len(value) > 0 {
					reader.Comma = []rune(value)[0]
				}
			}
		}
		records, err := reader.ReadAll()
		if err != nil {
			throw(vm, fmt.Errorf("CSV 解析失败: %w", err))
		}
		if !header {
			rows := make([]any, 0, len(records))
			for _, record := range records {
				rows = append(rows, toAnySlice(record))
			}
			return vm.ToValue(rows)
		}
		if len(records) == 0 {
			return vm.ToValue([]any{})
		}
		columns := records[0]
		rows := make([]any, 0, len(records)-1)
		for _, record := range records[1:] {
			row := make(map[string]any, len(columns))
			for index, column := range columns {
				if index < len(record) {
					row[column] = record[index]
					continue
				}
				row[column] = ""
			}
			rows = append(rows, row)
		}
		return vm.ToValue(rows)
	}
}

func (s *ScriptSDK) bindCSVStringify(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		rows, ok := call.Argument(0).Export().([]any)
		if !ok {
			panic(vm.ToValue("csvStringify 只接受数组"))
		}
		withHeader := true
		var columns []string
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			if options, ok := call.Argument(1).Export().(map[string]any); ok {
				if value, ok := options["header"].(bool); ok {
					withHeader = value
				}
				if value, ok := options["columns"].([]any); ok {
					for _, item := range value {
						columns = append(columns, scriptScalarString(item))
					}
				}
			}
		}
		if len(columns) == 0 {
			columns = deriveCSVColumns(rows)
		}
		var out bytes.Buffer
		writer := csv.NewWriter(&out)
		if withHeader && len(columns) > 0 {
			if err := writer.Write(columns); err != nil {
				throw(vm, err)
			}
		}
		for _, row := range rows {
			record := make([]string, 0, len(columns))
			switch typed := row.(type) {
			case map[string]any:
				for _, column := range columns {
					record = append(record, scriptScalarString(typed[column]))
				}
			case []any:
				for _, cell := range typed {
					record = append(record, scriptScalarString(cell))
				}
			default:
				record = append(record, scriptScalarString(row))
			}
			if err := writer.Write(record); err != nil {
				throw(vm, err)
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			throw(vm, err)
		}
		return vm.ToValue(out.String())
	}
}

// deriveCSVColumns 从第一行对象推列名，并按字典序固定顺序。
// 靠 map 遍历顺序会让同一份数据每次导出的列序都不同，下游对不上。
func deriveCSVColumns(rows []any) []string {
	for _, row := range rows {
		typed, ok := row.(map[string]any)
		if !ok {
			continue
		}
		columns := make([]string, 0, len(typed))
		for key := range typed {
			columns = append(columns, key)
		}
		sort.Strings(columns)
		return columns
	}
	return nil
}

// ── aegis.decimal ───────────────────────────────────────────────────

// bindDecimal 定点小数。
//
// 脚本里算钱是常态（分成、折扣、按比例退款），而 JS 只有双精度浮点：
// `0.1 + 0.2 !== 0.3` 在展示上无伤大雅，落到账上就是对不平的那一分。
// 服务端本来就用 shopspring/decimal，把同一套算术交给脚本是唯一
// 能让两侧算出同一个数的做法。
func (s *ScriptSDK) bindDecimal(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()

	parse := func(value goja.Value) decimal.Decimal {
		parsed, err := decimal.NewFromString(strings.TrimSpace(value.String()))
		if err != nil {
			throw(vm, fmt.Errorf("金额格式无效: %w", err))
		}
		return parsed
	}
	binary := func(operate func(a, b decimal.Decimal) decimal.Decimal) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			return vm.ToValue(operate(parse(call.Argument(0)), parse(call.Argument(1))).String())
		}
	}

	_ = object.Set("add", binary(func(a, b decimal.Decimal) decimal.Decimal { return a.Add(b) }))
	_ = object.Set("sub", binary(func(a, b decimal.Decimal) decimal.Decimal { return a.Sub(b) }))
	_ = object.Set("mul", binary(func(a, b decimal.Decimal) decimal.Decimal { return a.Mul(b) }))
	_ = object.Set("div", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		divisor := parse(call.Argument(1))
		if divisor.IsZero() {
			panic(vm.ToValue("除数不能为 0"))
		}
		scale := int32(8)
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			scale = int32(call.Argument(2).ToInteger())
		}
		return vm.ToValue(parse(call.Argument(0)).DivRound(divisor, scale).String())
	})
	_ = object.Set("cmp", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(parse(call.Argument(0)).Cmp(parse(call.Argument(1))))
	})
	_ = object.Set("round", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(parse(call.Argument(0)).Round(int32(call.Argument(1).ToInteger())).String())
	})
	_ = object.Set("abs", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(parse(call.Argument(0)).Abs().String())
	})
	_ = object.Set("isZero", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(parse(call.Argument(0)).IsZero())
	})
	_ = object.Set("format", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		scale := int32(2)
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			scale = int32(call.Argument(1).ToInteger())
		}
		return vm.ToValue(formatThousands(parse(call.Argument(0)).StringFixed(scale)))
	})

	return object
}

func formatThousands(value string) string {
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	integer, fraction, _ := strings.Cut(value, ".")
	var grouped strings.Builder
	for index, digit := range integer {
		if index > 0 && (len(integer)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	out := grouped.String()
	if fraction != "" {
		out += "." + fraction
	}
	if negative {
		return "-" + out
	}
	return out
}

// ── aegis.json ──────────────────────────────────────────────────────

func (s *ScriptSDK) bindJSONUtil(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()

	// 路径取值走 gjson：脚本里写 `a && a.b && a.b[0] && a.b[0].c` 这种
	// 防空链条既长又容易漏一环，而漏掉那一环的表现是一次 TypeError。
	resolve := func(value goja.Value, path string) gjson.Result {
		var raw string
		if text, ok := value.Export().(string); ok && json.Valid([]byte(text)) {
			raw = text
		} else {
			encoded, err := json.Marshal(value.Export())
			if err != nil {
				throw(vm, err)
			}
			raw = string(encoded)
		}
		return gjson.Get(raw, path)
	}

	_ = object.Set("get", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		result := resolve(call.Argument(0), call.Argument(1).String())
		if !result.Exists() {
			return goja.Undefined()
		}
		return vm.ToValue(result.Value())
	})
	_ = object.Set("exists", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		return vm.ToValue(resolve(call.Argument(0), call.Argument(1).String()).Exists())
	})
	_ = object.Set("pretty", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		encoded, err := json.MarshalIndent(call.Argument(0).Export(), "", "  ")
		if err != nil {
			throw(vm, err)
		}
		return vm.ToValue(string(encoded))
	})
	_ = object.Set("parse", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		var decoded any
		if err := json.Unmarshal([]byte(call.Argument(0).String()), &decoded); err != nil {
			// 解析失败给 fallback 而不是抛错：外部数据解不开是常态，
			// 而沙箱里 try/catch 得到的只是一句字符串。
			return call.Argument(1)
		}
		return vm.ToValue(decoded)
	})

	return object
}

// ── aegis.validate ──────────────────────────────────────────────────

func (s *ScriptSDK) bindValidate(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()

	_ = object.Set("schema", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		definition, ok := call.Argument(0).Export().(map[string]any)
		if !ok {
			panic(vm.ToValue("schema 必须是对象"))
		}
		errors := validateAgainstSchema(definition, call.Argument(1).Export())
		if errors == nil {
			errors = []string{}
		}
		return vm.ToValue(map[string]any{"valid": len(errors) == 0, "errors": errors})
	})

	text := func(check func(string) bool) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			s.budget(vm, false)
			return vm.ToValue(check(strings.TrimSpace(call.Argument(0).String())))
		}
	}
	_ = object.Set("email", text(func(value string) bool { return emailPattern.MatchString(value) }))
	_ = object.Set("url", text(func(value string) bool {
		parsed, err := url.Parse(value)
		return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
	}))
	_ = object.Set("ip", text(func(value string) bool { return net.ParseIP(value) != nil }))
	_ = object.Set("uuid", text(func(value string) bool { return uuidPattern.MatchString(value) }))
	_ = object.Set("phone", text(func(value string) bool { return chinaPhonePattern.MatchString(value) }))
	_ = object.Set("json", text(func(value string) bool { return json.Valid([]byte(value)) }))

	return object
}

// compileInputSchema 只编译不校验，用于保存入参契约时的前置检查。
//
// 与 validateAgainstSchema 共用同一套编译路径（同一个库、同一份 resource URI），
// 因此「保存时说能用」与「调用时真的能用」不可能不一致。
func compileInputSchema(definition map[string]any) error {
	_, err := buildSchema(definition)
	return err
}

func buildSchema(definition map[string]any) (*jsonschema.Schema, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, err
	}
	parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	// 每次新建 compiler 且不装 URLLoader：默认加载器会按 $ref 里的 URL
	// 发起网络请求，而 schema 是接入方写的 —— 那就是一个 SSRF 入口。
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaResourceURI, parsed); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaResourceURI)
}

// schemaResourceURI 编译时给 schema 的固定标识。用 aegis:// 而不是 http(s)://
// 是为了让任何试图联网解析的路径当场失败，而不是悄悄发出去一个请求。
const schemaResourceURI = "aegis://function/schema.json"

// validateAgainstSchema 返回**全部**校验错误而不是第一条。
//
// 只回第一条会让作者陷入「改一个、再跑一次、又冒出一个」的循环，
// 而这些错误本来在一次校验里就全都知道了。
func validateAgainstSchema(definition map[string]any, value any) []string {
	schema, err := buildSchema(definition)
	if err != nil {
		return []string{"schema 不可用：" + err.Error()}
	}
	// 走一次 JSON 往返：goja 导出的是 map[string]any / []any / int64，
	// 而校验器要的是 encoding/json 的形状（数字一律 float64）。
	normalized, err := roundTripJSON(value)
	if err != nil {
		return []string{"待校验的值无法序列化：" + err.Error()}
	}
	if err := schema.Validate(normalized); err != nil {
		var failure *jsonschema.ValidationError
		if errors.As(err, &failure) {
			return flattenSchemaErrors(failure)
		}
		return []string{err.Error()}
	}
	return nil
}

func roundTripJSON(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
}

func flattenSchemaErrors(failure *jsonschema.ValidationError) []string {
	if failure == nil {
		return nil
	}
	if len(failure.Causes) == 0 {
		return []string{describeSchemaFailure(failure)}
	}
	var out []string
	for _, cause := range failure.Causes {
		out = append(out, flattenSchemaErrors(cause)...)
	}
	return out
}

// describeSchemaFailure 把一条校验失败翻成一句人话。
//
// 库自带的 LocalizedString 只有英文，拼进中文错误里读起来是
// 「回调报文不合法：/: missing properties 'orderNo'」—— 半中半英，
// 开头那个 `/` 还是根路径的 JSON Pointer，对读的人毫无意义。
// 而这句话会一路传到接入方的日志里，是他排查时唯一的线索。
//
// 只翻最常见的那十几种；认不出的仍旧回落到库的英文原文，
// 那比编一句不准确的中文强。
func describeSchemaFailure(failure *jsonschema.ValidationError) string {
	where := describeSchemaLocation(failure.InstanceLocation)
	detail := failure.ErrorKind.LocalizedString(schemaErrorPrinter)

	switch typed := failure.ErrorKind.(type) {
	case *kind.Required:
		// 根对象缺字段是最常见的一条，单独措辞：说「缺少必填字段 x」
		// 比「/ 处缺少属性 x」自然得多
		return fmt.Sprintf("%s缺少必填字段 %s", wherePrefix(where), quoteList(typed.Missing))
	case *kind.AdditionalProperties:
		return fmt.Sprintf("%s不接受额外字段 %s", wherePrefix(where), quoteList(typed.Properties))
	case *kind.Type:
		return fmt.Sprintf("%s类型应为 %s，实际是 %s",
			wherePrefix(where), strings.Join(typed.Want, " 或 "), typed.Got)
	case *kind.Enum:
		return fmt.Sprintf("%s取值必须是 %s 之一，实际是 %s",
			wherePrefix(where), quoteAnyList(typed.Want), formatSchemaValue(typed.Got))
	case *kind.Const:
		return fmt.Sprintf("%s取值必须是 %s", wherePrefix(where), formatSchemaValue(typed.Want))
	case *kind.MinLength:
		return fmt.Sprintf("%s长度至少 %d，实际 %d", wherePrefix(where), typed.Want, typed.Got)
	case *kind.MaxLength:
		return fmt.Sprintf("%s长度最多 %d，实际 %d", wherePrefix(where), typed.Want, typed.Got)
	case *kind.Minimum:
		return fmt.Sprintf("%s不能小于 %s，实际 %s",
			wherePrefix(where), formatRat(typed.Want), formatRat(typed.Got))
	case *kind.Maximum:
		return fmt.Sprintf("%s不能大于 %s，实际 %s",
			wherePrefix(where), formatRat(typed.Want), formatRat(typed.Got))
	case *kind.ExclusiveMinimum:
		return fmt.Sprintf("%s必须大于 %s，实际 %s",
			wherePrefix(where), formatRat(typed.Want), formatRat(typed.Got))
	case *kind.ExclusiveMaximum:
		return fmt.Sprintf("%s必须小于 %s，实际 %s",
			wherePrefix(where), formatRat(typed.Want), formatRat(typed.Got))
	case *kind.MinItems:
		return fmt.Sprintf("%s至少 %d 个元素，实际 %d", wherePrefix(where), typed.Want, typed.Got)
	case *kind.MaxItems:
		return fmt.Sprintf("%s最多 %d 个元素，实际 %d", wherePrefix(where), typed.Want, typed.Got)
	case *kind.Pattern:
		return fmt.Sprintf("%s不匹配格式 %s", wherePrefix(where), typed.Want)
	case *kind.Format:
		return fmt.Sprintf("%s不是合法的 %s", wherePrefix(where), typed.Want)
	}
	if where == "" {
		return detail
	}
	return fmt.Sprintf("%s%s", wherePrefix(where), detail)
}

// describeSchemaLocation 把 JSON Pointer 段拼成 `coupon.code` / `tags[0]` 这种
// 脚本里真正会写出来的路径。根对象返回空串。
func describeSchemaLocation(location []string) string {
	var builder strings.Builder
	for _, segment := range location {
		// 纯数字段是数组下标
		if index, err := strconv.Atoi(segment); err == nil {
			fmt.Fprintf(&builder, "[%d]", index)
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('.')
		}
		builder.WriteString(segment)
	}
	return builder.String()
}

func wherePrefix(where string) string {
	if where == "" {
		return ""
	}
	return where + " "
}

func quoteList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return strings.Join(quoted, "、")
}

func quoteAnyList(values []any) string {
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, formatSchemaValue(value))
	}
	return strings.Join(rendered, "、")
}

func formatSchemaValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

// formatRat 边界值是 *big.Rat。直接 String() 得到的是 "3/2" 这种分数写法，
// 而 schema 里写的是 1.5 —— 报错里出现一个作者从没写过的形式只会让人困惑。
func formatRat(value *big.Rat) string {
	if value == nil {
		return "?"
	}
	if value.IsInt() {
		return value.Num().String()
	}
	return strings.TrimRight(strings.TrimRight(value.FloatString(6), "0"), ".")
}

// ── aegis.ua ────────────────────────────────────────────────────────

func (s *ScriptSDK) bindUserAgent(vm *goja.Runtime) *goja.Object {
	object := vm.NewObject()
	_ = object.Set("parse", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		parsed := useragent.Parse(call.Argument(0).String())
		// kind 是**分类**（脚本要判的几乎都是它），device 是机型字符串
		// （常为空）。只给其中一个都会让作者拿另一个当它用。
		kind := "desktop"
		switch {
		case parsed.Bot:
			kind = "bot"
		case parsed.Tablet:
			kind = "tablet"
		case parsed.Mobile:
			kind = "mobile"
		}
		return vm.ToValue(map[string]any{
			"name": parsed.Name, "version": parsed.Version,
			"os": parsed.OS, "osVersion": parsed.OSVersion,
			"device": parsed.Device, "kind": kind,
			"mobile": parsed.Mobile, "tablet": parsed.Tablet,
			"desktop": parsed.Desktop, "bot": parsed.Bot,
		})
	})
	return object
}

// ── aegis.time 扩展 ─────────────────────────────────────────────────

// extendTime 补齐「按时区日切」「格式化」「cron 下一跳」这几件事。
//
// 尤其是 dayKeyIn：默认的 dayKey 走 UTC，而国内接入方要的日切几乎都是
// 东八区的零点。让作者自己 +8 小时再取日期，会在夏令时与跨年那两天出错，
// 而那两天的错误谁也不会在测试里发现。
func (s *ScriptSDK) extendTime(vm *goja.Runtime, object *goja.Object) {
	_ = object.Set("weekKey", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		moment := time.Now().UTC().AddDate(0, 0, int(call.Argument(0).ToInteger())*7)
		year, week := moment.ISOWeek()
		return vm.ToValue(fmt.Sprintf("%04d-W%02d", year, week))
	})
	_ = object.Set("dayKeyIn", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		location := s.loadLocation(vm, call.Argument(0).String())
		moment := time.Now().In(location).AddDate(0, 0, int(call.Argument(1).ToInteger()))
		return vm.ToValue(moment.Format("2006-01-02"))
	})
	_ = object.Set("format", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		moment := time.Now()
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			moment = time.UnixMilli(call.Argument(1).ToInteger())
		}
		location := time.UTC
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
			location = s.loadLocation(vm, call.Argument(2).String())
		}
		return vm.ToValue(moment.In(location).Format(call.Argument(0).String()))
	})
	_ = object.Set("parse", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		value := strings.TrimSpace(call.Argument(0).String())
		layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02", time.RFC1123}
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			layouts = []string{call.Argument(1).String()}
		}
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, value); err == nil {
				return vm.ToValue(parsed.UnixMilli())
			}
		}
		// 解析不出来返回 0 而不是抛错：时间字符串来自外部输入，
		// 解不开是常态，抛错会逼作者给每一次解析套 try/catch。
		return vm.ToValue(0)
	})
	_ = object.Set("add", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		moment := time.UnixMilli(call.Argument(0).ToInteger())
		amount := int(call.Argument(1).ToInteger())
		switch call.Argument(2).String() {
		case "minute":
			moment = moment.Add(time.Duration(amount) * time.Minute)
		case "hour":
			moment = moment.Add(time.Duration(amount) * time.Hour)
		case "day":
			moment = moment.AddDate(0, 0, amount)
		case "month":
			moment = moment.AddDate(0, amount, 0)
		case "year":
			moment = moment.AddDate(amount, 0, 0)
		default:
			moment = moment.Add(time.Duration(amount) * time.Second)
		}
		return vm.ToValue(moment.UnixMilli())
	})
	_ = object.Set("diff", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		delta := time.UnixMilli(call.Argument(1).ToInteger()).
			Sub(time.UnixMilli(call.Argument(0).ToInteger()))
		switch call.Argument(2).String() {
		case "minute":
			return vm.ToValue(int64(delta.Minutes()))
		case "hour":
			return vm.ToValue(int64(delta.Hours()))
		case "day":
			return vm.ToValue(int64(delta.Hours() / 24))
		default:
			return vm.ToValue(int64(delta.Seconds()))
		}
	})
	_ = object.Set("startOfDay", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		moment := time.Now()
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Argument(0)) {
			moment = time.UnixMilli(call.Argument(0).ToInteger())
		}
		location := time.UTC
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			location = s.loadLocation(vm, call.Argument(1).String())
		}
		local := moment.In(location)
		return vm.ToValue(time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UnixMilli())
	})
	_ = object.Set("cronNext", func(call goja.FunctionCall) goja.Value {
		s.budget(vm, false)
		schedule, err := cronParser.Parse(strings.TrimSpace(call.Argument(0).String()))
		if err != nil {
			throw(vm, fmt.Errorf("cron 表达式非法: %w", err))
		}
		after := time.Now()
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			after = time.UnixMilli(call.Argument(1).ToInteger())
		}
		return vm.ToValue(schedule.Next(after).UnixMilli())
	})
}

// loadLocation 时区名解析。给错名字直接报错而不是悄悄回落到 UTC ——
// 回落的表现是「配了 Asia/Shanghai，日切却在早上八点」。
func (s *ScriptSDK) loadLocation(vm *goja.Runtime, name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		throw(vm, fmt.Errorf("无法识别的时区 %q（请用 IANA 名称，如 Asia/Shanghai）", name))
	}
	return location
}

// ── 共用小工具 ──────────────────────────────────────────────────────

// requireStdlibString 给标准库入参加一道长度闸门。
//
// 这些函数的入参可以来自 aegis.fetch 拿回的响应体，而那条链路上的
// 上限是 256KB —— 拼几次就能撑到几兆，模板渲染与 XML 解析在那个量级上
// 会先吃满内存再超时。
func requireStdlibString(vm *goja.Runtime, value goja.Value) string {
	text := value.String()
	if len(text) > maxStdlibInputBytes {
		panic(vm.ToValue(fmt.Sprintf("入参超过 %d KB 上限", maxStdlibInputBytes>>10)))
	}
	return text
}

func scriptScalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		// 整数值不要输出成 "1e+06"：签名串里那个写法与对方算出来的不一样
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}

func toAnySlice(values []string) []any {
	out := make([]any, len(values))
	for index, value := range values {
		out[index] = value
	}
	return out
}

func toInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	}
	return 0
}

// scriptUUID 供 lock 之类的能力生成令牌，与 aegis.crypto.uuid 同源。
func scriptUUID() string { return uuid.NewString() }
