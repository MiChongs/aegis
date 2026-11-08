// Package models 树模型文件：放对象存储（MinIO / S3），按内容摘要校验与缓存。
package models

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dmitryikh/leaves"
)

// MaxSize 单个模型文件上限
const MaxSize = 64 << 20

// Store 模型存储
type Store struct {
	s3     *s3.Client
	bucket string

	mu    sync.Mutex
	cache map[string]*leaves.Ensemble
}

// New 连对象存储
func New(cfg config.Config) *Store {
	scheme := "http://"
	if cfg.S3UseSSL {
		scheme = "https://"
	}
	cli := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(scheme + cfg.S3Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		UsePathStyle: true,
	})
	return &Store{s3: cli, bucket: cfg.S3Bucket, cache: map[string]*leaves.Ensemble{}}
}

// EnsureBucket 建桶（migrate）
func (s *Store) EnsureBucket(ctx context.Context) error {
	if _, err := s.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket}); err == nil {
		return nil
	}
	_, err := s.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket})
	if err != nil && !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") {
		return err
	}
	return nil
}

// Parse 解析模型文件，返回特征数
func Parse(format string, data []byte) (*leaves.Ensemble, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	switch format {
	case bundle.ModelLightGBM:
		if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			return leaves.LGEnsembleFromJSON(bytes.NewReader(data), true)
		}
		return leaves.LGEnsembleFromReader(r, true)
	case bundle.ModelXGBoost:
		return leaves.XGEnsembleFromReader(r, true)
	}
	return nil, fmt.Errorf("不支持的模型格式 %q", format)
}

// Checksum 内容摘要
func Checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Put 上传：键按摘要命名，同一文件重复上传是同一个对象
func (s *Store) Put(ctx context.Context, key string, data []byte) (objectKey, checksum string, err error) {
	checksum = Checksum(data)
	objectKey = "models/" + key + "/" + checksum[:16]
	_, err = s.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &objectKey, Body: bytes.NewReader(data)})
	return objectKey, checksum, err
}

// Load 实现 engine.ModelLoader
func (s *Store) Load(ctx context.Context, ref bundle.Model) (*leaves.Ensemble, error) {
	s.mu.Lock()
	if e, ok := s.cache[ref.Checksum]; ok {
		s.mu.Unlock()
		return e, nil
	}
	s.mu.Unlock()
	out, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &ref.ObjectKey})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	data, err := io.ReadAll(io.LimitReader(out.Body, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, errors.New("模型文件超过上限")
	}
	if ref.Checksum != "" && Checksum(data) != ref.Checksum {
		return nil, errors.New("模型文件摘要与发布包不一致")
	}
	e, err := Parse(ref.Format, data)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[ref.Checksum] = e
	s.mu.Unlock()
	return e, nil
}
