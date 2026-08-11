package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	appdomain "aegis/internal/domain/app"
	authprotocol "aegis/internal/domain/authprotocol"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

const (
	gwHeaderAppKey           = "X-Aegis-App-Key"
	gwHeaderSignature        = "X-Aegis-Signature"
	gwHeaderTimestamp        = "X-Aegis-Timestamp"
	gwHeaderNonce            = "X-Aegis-Nonce"
	gwHeaderProtocol         = "X-Aegis-Protocol"
	gwHeaderKeyID            = "X-Aegis-Key-Id"
	gwHeaderClientKey        = "X-Aegis-Client-Key"
	gwHeaderResponseNonce    = "X-Aegis-Response-Nonce"
	gwHeaderPlainContentType = "X-Aegis-Plain-Content-Type"

	gatewayPathPrefix = "/api/v1/apps/"
)

type appGatewayService interface {
	ResolveAppAndPolicy(context.Context, string) (*appdomain.App, *authprotocol.Policy, error)
	VerifySignature(context.Context, authprotocol.SignatureMetadata) error
	OpenRequest(context.Context, authprotocol.RequestMetadata, []byte) ([]byte, *authprotocol.CryptoContext, error)
	SealResponse(*authprotocol.CryptoContext, int, []byte) ([]byte, string, error)
}

// AppGateway 是 /api/v1/apps/{appKey}/* 的统一入口闸门。
//
// 三档安全等级是**累加**的，且只影响请求怎么包装，不影响路径与 JSON 结构：
//
//	standard —— 直通。HTTPS 之外不再要求任何东西。
//	signed   —— 校验 HMAC-SHA256 请求签名（防篡改 + 防重放 + 证明持有 appSecret）。
//	sealed   —— 在 signed 之上再解开 Transport v2 端到端加密载荷，并加密响应。
//
// sealed 之所以仍要签名：AEAD 只证明"这段密文没被改过"，任何人都能用公开的服务端
// 公钥造出合法密文。签名才证明"调用方确实持有 appSecret"。
func AppGateway(service appGatewayService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil || !strings.HasPrefix(c.Request.URL.Path, gatewayPathPrefix) {
			c.Next()
			return
		}
		appKey := appKeyFromGatewayPath(c.Request.URL.Path)
		if appKey == "" {
			response.Error(c, http.StatusBadRequest, 40079, "缺少 appKey")
			c.Abort()
			return
		}
		// 两条路径必须免包装放行：
		//   /config              —— 客户端得先知道自己该用哪一档，否则陷入
		//                           "要读配置得先按配置包装"的死锁；
		//   /auth/oauth/callback —— 由第三方平台重定向浏览器发起，客户端没有
		//                           任何机会给它签名或加密。
		// 两者都不接受任何请求体，回跳的越权与 CSRF 由 state 校验兜住。
		if isGatewayUnwrappedPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		app, policy, err := service.ResolveAppAndPolicy(c.Request.Context(), appKey)
		if err != nil {
			writeAppGatewayError(c, err)
			c.Abort()
			return
		}
		if headerApp := strings.TrimSpace(c.GetHeader(gwHeaderAppKey)); headerApp != "" && headerApp != appKey {
			response.Error(c, http.StatusBadRequest, 40084, "Header AppKey 与路由不一致")
			c.Abort()
			return
		}

		if policy.SecurityLevel == authprotocol.LevelStandard {
			c.Set("aegis.app_id", app.ID)
			c.Set("aegis.app_key", app.AppKey)
			c.Next()
			return
		}

		limit := gatewayBodyLimit(c.Request)
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
		if err != nil || int64(len(body)) > limit {
			response.Error(c, http.StatusBadRequest, 40078, "请求体读取失败或超过限制")
			c.Abort()
			return
		}

		// 签名覆盖实际发出的字节 **以及原样 query string**：sealed 档签的就是那串密文，
		// 客户端因此不需要关心"先签还是先加密"以外的任何顺序细节。
		// query 参与签名之后，`?page=1` 才改不成 `?page=999`（v2 签名，见 splitSignatureVersion）。
		if err := service.VerifySignature(c.Request.Context(), authprotocol.SignatureMetadata{
			AppKey: appKey, Signature: c.GetHeader(gwHeaderSignature),
			Timestamp: c.GetHeader(gwHeaderTimestamp), Nonce: c.GetHeader(gwHeaderNonce),
			Method: c.Request.Method, Path: c.Request.URL.Path,
			Query: c.Request.URL.RawQuery, Body: body,
		}); err != nil {
			writeAppGatewayError(c, err)
			c.Abort()
			return
		}

		if policy.SecurityLevel != authprotocol.LevelSealed {
			// signed 档只验签，不碰载荷：Content-Type 必须原样保留，
			// 否则 multipart 上传会被当成 JSON 解析。
			restoreRequestBody(c, body, c.Request.Header.Get("Content-Type"))
			c.Set("aegis.app_id", app.ID)
			c.Set("aegis.app_key", app.AppKey)
			c.Next()
			return
		}

		if strings.TrimSpace(c.GetHeader(gwHeaderProtocol)) != authprotocol.TransportV2 {
			response.Error(c, http.StatusUpgradeRequired, 42670, "该应用要求使用 Aegis Transport v2 加密载荷")
			c.Abort()
			return
		}
		ciphertext, ok := sealedCiphertext(c, body)
		if !ok {
			return
		}
		// 时间戳与 Nonce 在签名校验中已经查过一次；这里 OpenRequest 用的是
		// 独立的 Redis 作用域（sealed:<keyId>），因此不会自己撞自己的防重放。
		plainContentType := strings.TrimSpace(c.GetHeader(gwHeaderPlainContentType))
		plaintext, cryptoContext, err := service.OpenRequest(c.Request.Context(), authprotocol.RequestMetadata{
			AppKey: appKey, KeyID: c.GetHeader(gwHeaderKeyID),
			ClientPublicKey: c.GetHeader(gwHeaderClientKey), Timestamp: c.GetHeader(gwHeaderTimestamp),
			Nonce: c.GetHeader(gwHeaderNonce), Method: c.Request.Method, Path: c.Request.URL.Path,
			PlainContentType: plainContentType,
		}, ciphertext)
		if err != nil {
			writeAppGatewayError(c, err)
			c.Abort()
			return
		}
		if authprotocol.BodylessMethod(c.Request.Method) {
			// 无请求体的方法，明文就是真正的 query string。
			c.Request.URL.RawQuery = string(plaintext)
			restoreRequestBody(c, nil, "")
		} else {
			if plainContentType == "" {
				plainContentType = "application/json"
			}
			restoreRequestBody(c, plaintext, plainContentType)
		}
		c.Set("aegis.app_id", cryptoContext.AppID)
		c.Set("aegis.app_key", cryptoContext.AppKey)

		writer := newAppEncryptionResponseWriter(c.Writer)
		c.Writer = writer
		c.Next()

		sealed, responseNonce, err := service.SealResponse(cryptoContext, writer.status, writer.body.Bytes())
		underlying := writer.ResponseWriter
		if err != nil {
			resetHeaders(underlying.Header())
			c.Writer = underlying
			response.Error(c, http.StatusInternalServerError, 50070, "响应加密失败")
			return
		}
		resetHeaders(underlying.Header())
		copyHeaders(underlying.Header(), writer.header)
		underlying.Header().Del("Content-Length")
		// 原始 Content-Type 随响应带回：下载类接口在 sealed 档下拿到的是一段密文，
		// 客户端解开之后得知道它到底是 JSON 还是图片。
		if plainType := strings.TrimSpace(writer.header.Get("Content-Type")); plainType != "" {
			underlying.Header().Set(gwHeaderPlainContentType, plainType)
		}
		underlying.Header().Set("Content-Type", "application/octet-stream")
		underlying.Header().Set(gwHeaderProtocol, authprotocol.TransportV2)
		underlying.Header().Set(gwHeaderKeyID, cryptoContext.KeyID)
		underlying.Header().Set(gwHeaderResponseNonce, responseNonce)
		underlying.Header().Set("Cache-Control", "no-store")
		underlying.WriteHeader(writer.status)
		_, _ = underlying.Write(sealed)
	}
}

// sealedCiphertext 取出本次请求的密文。
//
// 密文的位置取决于方法有没有请求体：GET / DELETE / HEAD 走 `?_payload=`，
// 其余走 body。之所以不让 GET 带 body —— HTTP 允许，但 OkHttp、URLSession
// 与浏览器 fetch 全都拒绝构造这种请求，恰好就是 Android / iOS / Web 三端。
//
// 没有 query 要传时，`_payload` 是**空串的密文**（AEAD 对空明文照样产出 16 字节 tag），
// 于是「有没有参数」不构成分支，客户端一套代码走到底。
func sealedCiphertext(c *gin.Context, body []byte) ([]byte, bool) {
	if authprotocol.BodylessMethod(c.Request.Method) {
		// 用标准库读，不走 gin 的 queryCache —— 稍后要把 RawQuery 整个换掉。
		payload := strings.TrimSpace(c.Request.URL.Query().Get(authprotocol.SealedPayloadParam))
		if payload == "" {
			response.Error(c, http.StatusBadRequest, 40078,
				"缺少加密载荷：无请求体的方法需把密文放在 "+authprotocol.SealedPayloadParam+" 查询参数里")
			c.Abort()
			return nil, false
		}
		return []byte(payload), true
	}
	if len(body) == 0 {
		response.Error(c, http.StatusBadRequest, 40078, "加密载荷为空")
		c.Abort()
		return nil, false
	}
	return body, true
}

// gatewayBodyLimit 上传类请求放宽到 MaxUploadBytes，其余按 MaxRequestBytes。
// 一刀切成 1 MiB 会让「换头像」在 signed 档以一个和图片无关的错误失败。
func gatewayBodyLimit(request *http.Request) int64 {
	contentType := strings.ToLower(request.Header.Get("Content-Type"))
	if strings.Contains(contentType, "multipart/form-data") {
		return authprotocol.MaxUploadBytes
	}
	// sealed 档的上传是一整段密文（octet-stream），Content-Type 看不出是不是上传，
	// 由客户端用 X-Aegis-Plain-Content-Type 声明原始类型。
	plainType := strings.ToLower(request.Header.Get(gwHeaderPlainContentType))
	if strings.Contains(plainType, "multipart/form-data") {
		return authprotocol.MaxUploadBytes
	}
	return authprotocol.MaxRequestBytes
}

// restoreRequestBody 把拆包后的字节放回请求。
//
// contentType 为空表示不改动原有的头；传空 body 时一并清掉 Content-Type，
// 让下游把它当作真正的无体请求。
func restoreRequestBody(c *gin.Context, body []byte, contentType string) {
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	switch {
	case len(body) == 0:
		c.Request.Header.Del("Content-Type")
	case strings.TrimSpace(contentType) != "":
		c.Request.Header.Set("Content-Type", contentType)
	}
}

func writeAppGatewayError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		response.Error(c, http.StatusServiceUnavailable, 50370, "接入网关暂不可用")
		return
	}
	switch appErr.HTTPStatus {
	case http.StatusNotFound:
		response.Error(c, http.StatusNotFound, appErr.Code, "应用不存在或已停用")
	case http.StatusUnauthorized:
		// 签名类失败原样透出：接入调试期最需要知道到底是格式错还是算错了。
		response.Error(c, http.StatusUnauthorized, appErr.Code, appErr.Message)
	case http.StatusConflict:
		response.Error(c, http.StatusConflict, appErr.Code, "请求已被使用")
	case http.StatusServiceUnavailable:
		response.Error(c, http.StatusServiceUnavailable, appErr.Code, appErr.Message)
	default:
		response.Error(c, http.StatusBadRequest, appErr.Code, appErr.Message)
	}
}

// gatewayUnwrappedSuffixes 见 AppGateway 中的说明：这两条路径在任何安全等级下
// 都以明文放行，且都不接受请求体。新增条目前请确认它确实无法被客户端包装。
var gatewayUnwrappedSuffixes = []string{"/config", "/auth/oauth/callback"}

func isGatewayUnwrappedPath(path string) bool {
	for _, suffix := range gatewayUnwrappedSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func appKeyFromGatewayPath(path string) string {
	if !strings.HasPrefix(path, gatewayPathPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, gatewayPathPrefix)
	if index := strings.IndexByte(rest, '/'); index >= 0 {
		rest = rest[:index]
	}
	return strings.TrimSpace(rest)
}
