package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
)

var recoveryCodeCleaner = regexp.MustCompile(`[^A-Z0-9]`)

func securityKeyMaterial(source string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(source)))
	return sum[:]
}

func encryptSecret(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decryptSecret(key []byte, ciphertext string) (string, error) {
	payload, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid ciphertext")
	}
	nonce := payload[:gcm.NonceSize()]
	data := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func normalizeRecoveryCode(code string) string {
	cleaned := strings.ToUpper(strings.TrimSpace(code))
	return recoveryCodeCleaner.ReplaceAllString(cleaned, "")
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

func maskSecret(secret string) string {
	if len(secret) <= 8 {
		return secret
	}
	return secret[:4] + strings.Repeat("*", len(secret)-8) + secret[len(secret)-4:]
}

func digitsFromInt(digits int) otp.Digits {
	switch digits {
	case 8:
		return otp.DigitsEight
	default:
		return otp.DigitsSix
	}
}

func credentialIDToString(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func credentialIDFromString(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
}

func passkeyUserHandle(appID int64, userID int64) []byte {
	return []byte(fmt.Sprintf("u:%d:%d", appID, userID))
}

func adminPasskeyUserHandle(adminID int64) []byte {
	return []byte(fmt.Sprintf("a:%d", adminID))
}

func formatRecoveryCode(raw string) string {
	var groups []string
	for len(raw) > 0 {
		take := 4
		if len(raw) < take {
			take = len(raw)
		}
		groups = append(groups, raw[:take])
		raw = raw[take:]
	}
	return strings.Join(groups, "-")
}

func generateRecoveryCodes(count int, length int) ([]string, error) {
	if count <= 0 {
		count = 10
	}
	if length <= 0 {
		length = 12
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codes := make([]string, 0, count)
	seen := make(map[string]struct{}, count)
	buffer := make([]byte, length)

	for len(codes) < count {
		random := make([]byte, length)
		if _, err := io.ReadFull(rand.Reader, random); err != nil {
			return nil, err
		}
		for i := range buffer {
			buffer[i] = alphabet[int(random[i])%len(alphabet)]
		}
		formatted := formatRecoveryCode(string(buffer))
		if _, ok := seen[formatted]; ok {
			continue
		}
		seen[formatted] = struct{}{}
		codes = append(codes, formatted)
	}

	return codes, nil
}

func recoveryCodeHint(code string) string {
	normalized := normalizeRecoveryCode(code)
	if len(normalized) <= 4 {
		return normalized
	}
	return "****" + normalized[len(normalized)-4:]
}

func buildJSONRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	body := bytes.TrimSpace(payload)
	if len(body) == 0 {
		return nil, fmt.Errorf("credential payload is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://aegis.local/_webauthn", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func newChallengeID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// ──────────────────────────────────────
// RSA / ECDH 密钥对生成
// ──────────────────────────────────────

func generateRSAKeyPair() (publicKeyPEM string, privateKeyPEM string, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})), nil
}

func generateECDHKeyPair() (publicKeyPEM string, privateKeyPEM string, err error) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		return "", "", err
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})), nil
}
