package middleware

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/secure-io/sio-go"
)

const (
	appEncryptionHeaderEnabled          = "X-Aegis-Encrypted"
	appEncryptionHeaderAppID            = "X-Aegis-Appid"
	appEncryptionHeaderNonce            = "X-Aegis-Nonce"
	appEncryptionHeaderAlgorithm        = "X-Aegis-Algorithm"
	appEncryptionHeaderPlainContentType = "X-Aegis-Plain-Content-Type"
	appEncryptionQueryPayload           = "_payload"
	appEncryptionAlgorithm              = "XChaCha20Poly1305"
	appEncryptionPlaintextMessage       = "当前应用已开启加密通信"
	appEncryptionSniffBodyLimit         = 2 << 20
)

type appEncryptionAppService interface {
	GetApp(ctx context.Context, appID int64) (*appdomain.App, error)
	ResolveTransportEncryption(app *appdomain.App) appdomain.TransportEncryptionPolicy
}

func AppEncryption(appService appEncryptionAppService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if appService == nil || !shouldApplyAppEncryption(c.Request.URL.Path) {
			c.Next()
			return
		}

		appID, ok := resolveAppID(c)
		if !ok || appID <= 0 {
			c.Next()
			return
		}

		appItem, err := appService.GetApp(c.Request.Context(), appID)
		if err != nil || appItem == nil {
			c.Next()
			return
		}

		policy := appService.ResolveTransportEncryption(appItem)
		if !policy.Enabled {
			c.Next()
			return
		}
		if strings.TrimSpace(policy.Secret) == "" {
			rejectPlaintextRequest(c)
			return
		}
		if !isEncryptedRequest(c.Request) {
			rejectPlaintextRequest(c)
			return
		}

		cryptoContext, err := newAppEncryptionContext(policy.Secret, appID, c.Request.Method, c.Request.URL.Path)
		if err != nil {
			rejectPlaintextRequest(c)
			return
		}

		if err := decryptIncomingRequest(c, cryptoContext); err != nil {
			rejectPlaintextRequest(c)
			return
		}

		var encryptedWriter *appEncryptionResponseWriter
		if policy.ResponseEncryption && c.Request.Method != http.MethodHead {
			encryptedWriter = newAppEncryptionResponseWriter(c.Writer)
			c.Writer = encryptedWriter
		}

		c.Next()

		if encryptedWriter != nil {
			if err := encryptedWriter.FlushEncrypted(cryptoContext); err != nil {
				underlying := encryptedWriter.ResponseWriter
				resetHeaders(underlying.Header())
				underlying.Header().Set("Content-Type", "application/json; charset=utf-8")
				underlying.Header().Set("Cache-Control", "no-store")
				underlying.WriteHeader(http.StatusInternalServerError)
				_, _ = underlying.Write([]byte(`{"code":50000,"message":"服务暂时不可用"}`))
			}
		}
	}
}

func shouldApplyAppEncryption(path string) bool {
	switch {
	case path == "/healthz", path == "/readyz":
		return false
	case strings.HasPrefix(path, "/api/admin"):
		return false
	case strings.HasPrefix(path, "/api/public/pay"):
		return false
	case strings.HasPrefix(path, "/api/storage/proxy/"):
		return false
	case strings.HasPrefix(path, "/api/auth"):
		return true
	case strings.HasPrefix(path, "/api/user"):
		return true
	case strings.HasPrefix(path, "/api/user-settings"):
		return true
	case strings.HasPrefix(path, "/api/points"):
		return true
	case strings.HasPrefix(path, "/api/notifications"):
		return true
	case strings.HasPrefix(path, "/api/email"):
		return true
	case strings.HasPrefix(path, "/api/pay"):
		return true
	case strings.HasPrefix(path, "/api/storage"):
		return true
	case path == "/api/app/public":
		return true
	default:
		return false
	}
}

func resolveAppID(c *gin.Context) (int64, bool) {
	if appID, ok := parseInt64(strings.TrimSpace(c.GetHeader(appEncryptionHeaderAppID))); ok {
		return appID, true
	}
	if appID, ok := parseJWTAppID(c.GetHeader("Authorization")); ok {
		return appID, true
	}
	if appID, ok := parseInt64(strings.TrimSpace(c.Query("appid"))); ok {
		return appID, true
	}
	if appID, ok := sniffPlainAppIDFromBody(c.Request); ok {
		return appID, true
	}
	return 0, false
}

func parseJWTAppID(header string) (int64, bool) {
	token := bearerToken(header)
	if token == "" {
		return 0, false
	}
	claims := jwt.MapClaims{}
	if _, _, err := new(jwt.Parser).ParseUnverified(token, claims); err != nil {
		return 0, false
	}
	value, ok := claims["appid"]
	if !ok {
		return 0, false
	}
	return parseAnyInt64(value)
}

func sniffPlainAppIDFromBody(req *http.Request) (int64, bool) {
	if req == nil || req.Body == nil {
		return 0, false
	}
	if req.ContentLength > appEncryptionSniffBodyLimit {
		return 0, false
	}

	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return 0, false
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return 0, false
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		return 0, false
	}

	if strings.Contains(contentType, "application/json") {
		payload := map[string]any{}
		if err := json.Unmarshal(body, &payload); err != nil {
			return 0, false
		}
		value, ok := payload["appid"]
		if !ok {
			return 0, false
		}
		return parseAnyInt64(value)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return 0, false
	}
	return parseInt64(strings.TrimSpace(values.Get("appid")))
}

func isEncryptedRequest(req *http.Request) bool {
	if req == nil {
		return false
	}
	value := strings.TrimSpace(strings.ToLower(req.Header.Get(appEncryptionHeaderEnabled)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

type appEncryptionContext struct {
	stream *sio.Stream
	appID  int64
	method string
	path   string
}

func newAppEncryptionContext(secret string, appID int64, method string, path string) (*appEncryptionContext, error) {
	key := deriveAppEncryptionKey(secret, appID)
	stream, err := sio.XChaCha20Poly1305.Stream(key)
	if err != nil {
		return nil, err
	}
	return &appEncryptionContext{
		stream: stream,
		appID:  appID,
		method: strings.ToUpper(method),
		path:   path,
	}, nil
}

func deriveAppEncryptionKey(secret string, appID int64) []byte {
	material := decodeSecretMaterial(secret)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%x", appID, material)))
	return sum[:]
}

func decodeSecretMaterial(secret string) []byte {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return nil
	}
	decoders := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, decoder := range decoders {
		if decoded, err := decoder.DecodeString(trimmed); err == nil && len(decoded) > 0 {
			return decoded
		}
	}
	if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) > 0 {
		return decoded
	}
	return []byte(trimmed)
}

func decryptIncomingRequest(c *gin.Context, cryptoContext *appEncryptionContext) error {
	if c.Request == nil {
		return errors.New("missing request")
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodHead {
		return decryptEncryptedQuery(c.Request, cryptoContext)
	}
	return decryptEncryptedBody(c.Request, cryptoContext)
}

func decryptEncryptedQuery(req *http.Request, cryptoContext *appEncryptionContext) error {
	payload := strings.TrimSpace(req.URL.Query().Get(appEncryptionQueryPayload))
	if payload == "" {
		req.URL.RawQuery = ""
		return nil
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return err
	}
	nonce, err := decodeNonce(req.Header.Get(appEncryptionHeaderNonce), cryptoContext.stream.NonceSize())
	if err != nil {
		return err
	}
	plaintext, err := decryptBytes(cryptoContext.stream, ciphertext, nonce, cryptoContext.associatedData("request-query"))
	if err != nil {
		return err
	}
	req.URL.RawQuery = string(plaintext)
	return nil
}

func decryptEncryptedBody(req *http.Request, cryptoContext *appEncryptionContext) error {
	if req.Body == nil {
		return nil
	}
	if req.ContentLength == 0 && strings.TrimSpace(req.Header.Get(appEncryptionHeaderNonce)) == "" {
		return nil
	}
	nonce, err := decodeNonce(req.Header.Get(appEncryptionHeaderNonce), cryptoContext.stream.NonceSize())
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "aegis-decrypted-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanup := func(closeFile bool) {
		if closeFile {
			_ = tempFile.Close()
		}
		_ = os.Remove(tempPath)
	}

	writer := cryptoContext.stream.DecryptWriter(tempFile, nonce, cryptoContext.associatedData("request-body"))
	if _, err := io.Copy(writer, req.Body); err != nil {
		_ = writer.Close()
		cleanup(true)
		return err
	}
	if err := writer.Close(); err != nil {
		cleanup(true)
		return err
	}
	plainFile, err := os.Open(tempPath)
	if err != nil {
		cleanup(false)
		return err
	}

	if plainType := strings.TrimSpace(req.Header.Get(appEncryptionHeaderPlainContentType)); plainType != "" {
		req.Header.Set("Content-Type", plainType)
	}
	req.Header.Del("Content-Length")
	req.ContentLength = -1
	req.Body = &temporaryBodyFile{File: plainFile, path: tempPath}
	return nil
}

func decodeNonce(value string, expectedSize int) ([]byte, error) {
	nonce, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(nonce) != expectedSize {
		return nil, fmt.Errorf("invalid nonce length")
	}
	return nonce, nil
}

func decryptBytes(stream *sio.Stream, ciphertext []byte, nonce []byte, associatedData []byte) ([]byte, error) {
	var plaintext bytes.Buffer
	writer := stream.DecryptWriter(&plaintext, nonce, associatedData)
	if _, err := writer.Write(ciphertext); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return plaintext.Bytes(), nil
}

func encryptBytes(stream *sio.Stream, plaintext []byte, nonce []byte, associatedData []byte) ([]byte, error) {
	var ciphertext bytes.Buffer
	writer := stream.EncryptWriter(&ciphertext, nonce, associatedData)
	if _, err := writer.Write(plaintext); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return ciphertext.Bytes(), nil
}

func (c *appEncryptionContext) associatedData(scope string) []byte {
	return []byte(fmt.Sprintf("appid=%d|method=%s|path=%s|scope=%s", c.appID, c.method, c.path, scope))
}

type temporaryBodyFile struct {
	*os.File
	path string
}

func (f *temporaryBodyFile) Close() error {
	if f.File == nil {
		return nil
	}
	err := f.File.Close()
	_ = os.Remove(f.path)
	return err
}

type appEncryptionResponseWriter struct {
	gin.ResponseWriter
	header  http.Header
	body    bytes.Buffer
	status  int
	written bool
}

func newAppEncryptionResponseWriter(writer gin.ResponseWriter) *appEncryptionResponseWriter {
	return &appEncryptionResponseWriter{
		ResponseWriter: writer,
		header:         make(http.Header),
		status:         http.StatusOK,
	}
}

func (w *appEncryptionResponseWriter) Header() http.Header {
	return w.header
}

func (w *appEncryptionResponseWriter) WriteHeader(statusCode int) {
	if w.written {
		return
	}
	w.status = statusCode
}

func (w *appEncryptionResponseWriter) WriteHeaderNow() {
	w.written = true
}

func (w *appEncryptionResponseWriter) Write(data []byte) (int, error) {
	w.written = true
	return w.body.Write(data)
}

func (w *appEncryptionResponseWriter) WriteString(value string) (int, error) {
	w.written = true
	return w.body.WriteString(value)
}

func (w *appEncryptionResponseWriter) Status() int {
	return w.status
}

func (w *appEncryptionResponseWriter) Size() int {
	return w.body.Len()
}

func (w *appEncryptionResponseWriter) Written() bool {
	return w.written
}

func (w *appEncryptionResponseWriter) FlushEncrypted(cryptoContext *appEncryptionContext) error {
	underlying := w.ResponseWriter
	if !w.written && w.body.Len() == 0 {
		copyHeaders(underlying.Header(), w.header)
		underlying.WriteHeader(w.status)
		return nil
	}

	nonce := make([]byte, cryptoContext.stream.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext, err := encryptBytes(cryptoContext.stream, w.body.Bytes(), nonce, cryptoContext.associatedData("response-body"))
	if err != nil {
		return err
	}

	resetHeaders(underlying.Header())
	copyHeaders(underlying.Header(), w.header)
	underlying.Header().Del("Content-Length")
	if plainType := strings.TrimSpace(w.header.Get("Content-Type")); plainType != "" {
		underlying.Header().Set(appEncryptionHeaderPlainContentType, plainType)
	}
	underlying.Header().Set("Content-Type", "application/octet-stream")
	underlying.Header().Set(appEncryptionHeaderEnabled, "1")
	underlying.Header().Set(appEncryptionHeaderAlgorithm, appEncryptionAlgorithm)
	underlying.Header().Set(appEncryptionHeaderNonce, base64.RawURLEncoding.EncodeToString(nonce))
	underlying.Header().Set("Cache-Control", "no-store")
	underlying.WriteHeader(w.status)
	_, err = underlying.Write(ciphertext)
	return err
}

func copyHeaders(target http.Header, source http.Header) {
	for key, values := range source {
		copied := make([]string, len(values))
		copy(copied, values)
		target[key] = copied
	}
}

func resetHeaders(header http.Header) {
	for key := range header {
		header.Del(key)
	}
}

func rejectPlaintextRequest(c *gin.Context) {
	response.Error(c, http.StatusBadRequest, 40061, appEncryptionPlaintextMessage)
	c.Abort()
}

func parseInt64(value string) (int64, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseAnyInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		return parseInt64(typed)
	default:
		return 0, false
	}
}
