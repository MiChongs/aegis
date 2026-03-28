package middleware

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"strings"

	"github.com/secure-io/sio-go"
)

// ──────────────────────────────────────
// 算法接口
// ──────────────────────────────────────

// CryptoStream 统一的加密流接口
type CryptoStream interface {
	NonceSize() int
	EncryptWriter(w io.Writer, nonce, aad []byte) io.WriteCloser
	DecryptWriter(w io.Writer, nonce, aad []byte) io.WriteCloser
}

// ──────────────────────────────────────
// XChaCha20Poly1305（封装 sio-go）
// ──────────────────────────────────────

type xchachaStream struct{ inner *sio.Stream }

func newXChaCha20Stream(key []byte) (CryptoStream, error) {
	s, err := sio.XChaCha20Poly1305.Stream(key)
	if err != nil {
		return nil, err
	}
	return &xchachaStream{inner: s}, nil
}
func (s *xchachaStream) NonceSize() int { return s.inner.NonceSize() }
func (s *xchachaStream) EncryptWriter(w io.Writer, nonce, aad []byte) io.WriteCloser {
	return s.inner.EncryptWriter(w, nonce, aad)
}
func (s *xchachaStream) DecryptWriter(w io.Writer, nonce, aad []byte) io.WriteCloser {
	return s.inner.DecryptWriter(w, nonce, aad)
}

// ──────────────────────────────────────
// AES-256-GCM（封装 sio-go）
// ──────────────────────────────────────

type aesGCMStream struct{ inner *sio.Stream }

func newAES256GCMStream(key []byte) (CryptoStream, error) {
	if len(key) < 32 {
		h := sha256.Sum256(key)
		key = h[:]
	} else {
		key = key[:32]
	}
	s, err := sio.AES_256_GCM.Stream(key)
	if err != nil {
		return nil, err
	}
	return &aesGCMStream{inner: s}, nil
}
func (s *aesGCMStream) NonceSize() int { return s.inner.NonceSize() }
func (s *aesGCMStream) EncryptWriter(w io.Writer, nonce, aad []byte) io.WriteCloser {
	return s.inner.EncryptWriter(w, nonce, aad)
}
func (s *aesGCMStream) DecryptWriter(w io.Writer, nonce, aad []byte) io.WriteCloser {
	return s.inner.DecryptWriter(w, nonce, aad)
}

// ──────────────────────────────────────
// 算法常量与注册表
// ──────────────────────────────────────

const (
	AlgoXChaCha20         = "XChaCha20Poly1305"
	AlgoAES256GCM         = "AES-256-GCM"
	AlgoHybridRSAXChaCha  = "hybrid-rsa-xchacha20"
	AlgoHybridRSAAES256   = "hybrid-rsa-aes256gcm"
	AlgoHybridECDHXChaCha = "hybrid-ecdh-xchacha20"
	AlgoHybridECDHAES256  = "hybrid-ecdh-aes256gcm"
)

const appEncryptionHeaderKey = "X-Aegis-Key"

// NewCryptoStream 根据算法名称创建加密流
func NewCryptoStream(algorithm string, key []byte) (CryptoStream, error) {
	switch strings.TrimSpace(algorithm) {
	case AlgoXChaCha20, "":
		return newXChaCha20Stream(key)
	case AlgoAES256GCM:
		return newAES256GCMStream(key)
	default:
		return nil, fmt.Errorf("不支持的加密算法: %s", algorithm)
	}
}

// IsHybridAlgorithm 判断是否为混合加密
func IsHybridAlgorithm(algorithm string) bool {
	return strings.HasPrefix(algorithm, "hybrid-")
}

// HybridSymmetricAlgorithm 从混合算法名中提取对称算法
func HybridSymmetricAlgorithm(hybrid string) string {
	switch hybrid {
	case AlgoHybridRSAXChaCha, AlgoHybridECDHXChaCha:
		return AlgoXChaCha20
	case AlgoHybridRSAAES256, AlgoHybridECDHAES256:
		return AlgoAES256GCM
	default:
		return AlgoXChaCha20
	}
}

// IsSupportedAlgorithm 检查算法是否受支持
func IsSupportedAlgorithm(algorithm string) bool {
	switch algorithm {
	case AlgoXChaCha20, AlgoAES256GCM,
		AlgoHybridRSAXChaCha, AlgoHybridRSAAES256,
		AlgoHybridECDHXChaCha, AlgoHybridECDHAES256:
		return true
	default:
		return false
	}
}

// AllSupportedAlgorithms 返回所有支持的算法列表
func AllSupportedAlgorithms() []string {
	return []string{
		AlgoXChaCha20, AlgoAES256GCM,
		AlgoHybridRSAXChaCha, AlgoHybridRSAAES256,
		AlgoHybridECDHXChaCha, AlgoHybridECDHAES256,
	}
}

// ──────────────────────────────────────
// RSA 解密会话密钥（RSA-OAEP-SHA256）
// ──────────────────────────────────────

func RSADecryptSessionKey(privateKeyPEM string, encryptedKey []byte) ([]byte, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("无效的 RSA 私钥")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("密钥类型不是 RSA")
	}
	return rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaKey, encryptedKey, nil)
}

// ──────────────────────────────────────
// ECDH 密钥协商
// ──────────────────────────────────────

func ECDHDeriveSessionKey(serverPrivateKeyPEM string, clientPublicKeyBytes []byte) ([]byte, error) {
	block, _ := pem.Decode([]byte(serverPrivateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("无效的 ECDH 私钥")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecdsaKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("密钥类型不是 ECDSA")
	}
	serverPriv, err := ecdsaKey.ECDH()
	if err != nil {
		return nil, err
	}

	clientPub, err := ecdh.P256().NewPublicKey(clientPublicKeyBytes)
	if err != nil {
		pubKey, parseErr := x509.ParsePKIXPublicKey(clientPublicKeyBytes)
		if parseErr != nil {
			return nil, fmt.Errorf("解析客户端公钥失败: %w", err)
		}
		ecdsaPub, ok := pubKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("客户端公钥类型不是 ECDSA")
		}
		clientPub, err = ecdsaPub.ECDH()
		if err != nil {
			return nil, err
		}
	}

	shared, err := serverPriv.ECDH(clientPub)
	if err != nil {
		return nil, err
	}
	derived := sha256.Sum256(shared)
	return derived[:], nil
}

// ──────────────────────────────────────
// AES-GCM 简单加解密（非流式，用于小数据如会话密钥）
// ──────────────────────────────────────

func AESGCMEncrypt(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, nil), nil
}
