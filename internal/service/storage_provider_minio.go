package service

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioProvider struct {
	httpClient *http.Client
}

func newMinIOProvider(httpClient *http.Client) storageProvider {
	return &minioProvider{httpClient: httpClient}
}

func (p *minioProvider) Name() string { return storagedomain.ProviderMinIO }

func (p *minioProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	created, err := p.ensureBucket(ctx, client, raw)
	if err != nil {
		return nil, err
	}
	count := 0
	for item := range client.ListObjects(ctx, raw.Bucket, minio.ListObjectsOptions{Recursive: false, MaxKeys: 1}) {
		if item.Err != nil {
			return nil, item.Err
		}
		count++
	}
	return map[string]any{
		"bucket":                raw.Bucket,
		"endpoint":              raw.Endpoint,
		"region":                raw.Region,
		"sampleObjectCount":     count,
		"bucketAutoCreated":     created,
		"autoCreateBucket":      raw.AutoCreateBucket,
		"presignedURLSupported": true,
	}, nil
}

func (p *minioProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := p.ensureBucket(ctx, client, raw); err != nil {
		return nil, err
	}

	size := input.ContentLength
	if size <= 0 {
		size = -1
	}

	info, err := client.PutObject(ctx, raw.Bucket, input.ObjectKey, input.Content, size, minio.PutObjectOptions{
		ContentType:  strings.TrimSpace(input.ContentType),
		CacheControl: strings.TrimSpace(input.CacheControl),
		UserMetadata: input.Metadata,
	})
	if err != nil {
		return nil, err
	}

	return &storagedomain.StoredObject{
		Bucket:      raw.Bucket,
		Key:         input.ObjectKey,
		FileName:    input.FileName,
		Size:        info.Size,
		ContentType: strings.TrimSpace(input.ContentType),
		ETag:        strings.Trim(info.ETag, "\""),
		URL:         composeMinIOURL(cfg, raw, input.ObjectKey),
	}, nil
}

func (p *minioProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	client, raw, err := p.client(cfg)
	if err != nil {
		return nil, err
	}
	object, err := client.GetObject(ctx, raw.Bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	info, err := object.Stat()
	if err != nil {
		closeSilently(object)
		return nil, err
	}

	reader := &storagedomain.ObjectReader{
		Body:         object,
		Size:         info.Size,
		ContentType:  info.ContentType,
		FileName:     objectKey,
		ETag:         strings.Trim(info.ETag, "\""),
		CacheControl: info.Metadata.Get("Cache-Control"),
	}
	if !info.LastModified.IsZero() {
		lastModified := info.LastModified
		reader.LastModified = &lastModified
	}
	if disposition := info.Metadata.Get("Content-Disposition"); disposition != "" {
		reader.FileName = disposition
	}
	return reader, nil
}

func (p *minioProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	raw, err := decodeMinIOConfig(cfg.ConfigData)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey), nil
	}

	client, _, err := p.client(cfg)
	if err != nil {
		return "", err
	}
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	link, err := client.PresignedGetObject(ctx, raw.Bucket, objectKey, expiresIn, nil)
	if err != nil {
		return composeMinIOURL(cfg, raw, objectKey), nil
	}
	return link.String(), nil
}

func (p *minioProvider) client(cfg *storagedomain.Config) (*minio.Client, *storagedomain.MinIOConfig, error) {
	raw, err := decodeMinIOConfig(cfg.ConfigData)
	if err != nil {
		return nil, nil, err
	}
	endpoint, secure, err := normalizeMinIOEndpoint(raw.Endpoint, raw.UseSSL)
	if err != nil {
		return nil, nil, err
	}
	options := &minio.Options{
		Creds:  credentials.NewStaticV4(raw.AccessKeyID, raw.SecretAccessKey, raw.SessionToken),
		Secure: secure,
		Region: strings.TrimSpace(raw.Region),
	}
	if p.httpClient != nil {
		options.Transport = p.httpClient.Transport
	}
	client, err := minio.New(endpoint, options)
	if err != nil {
		return nil, nil, err
	}
	return client, raw, nil
}

func (p *minioProvider) ensureBucket(ctx context.Context, client *minio.Client, raw *storagedomain.MinIOConfig) (bool, error) {
	exists, err := client.BucketExists(ctx, raw.Bucket)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if !raw.AutoCreateBucket {
		return false, fmt.Errorf("minio bucket %s does not exist", raw.Bucket)
	}
	if err := client.MakeBucket(ctx, raw.Bucket, minio.MakeBucketOptions{Region: strings.TrimSpace(raw.Region)}); err != nil {
		exists, existsErr := client.BucketExists(ctx, raw.Bucket)
		if existsErr == nil && exists {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func composeMinIOURL(cfg *storagedomain.Config, raw *storagedomain.MinIOConfig, objectKey string) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return composeStaticObjectURL(cfg.BaseURL, objectKey)
	}
	endpoint, secure, err := normalizeMinIOEndpoint(raw.Endpoint, raw.UseSSL)
	if err != nil {
		return composeStaticObjectURL(raw.Endpoint, raw.Bucket+"/"+strings.TrimLeft(strings.TrimSpace(objectKey), "/"))
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + endpoint + "/" + raw.Bucket + "/" + encodeObjectPath(objectKey)
}

func normalizeMinIOEndpoint(endpoint string, useSSL bool) (string, bool, error) {
	value := strings.TrimSpace(endpoint)
	if value == "" {
		return "", false, fmt.Errorf("minio endpoint is required")
	}
	secure := useSSL
	if strings.Contains(value, "://") {
		parsed, err := neturl.Parse(value)
		if err != nil {
			return "", false, err
		}
		if strings.TrimSpace(parsed.Host) == "" {
			return "", false, fmt.Errorf("minio endpoint host is required")
		}
		value = parsed.Host
		if strings.EqualFold(parsed.Scheme, "https") {
			secure = true
		} else if strings.EqualFold(parsed.Scheme, "http") {
			secure = false
		}
	}
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	return value, secure, nil
}

func firstHeaderValue(headers map[string][]string, key string) string {
	if len(headers) == 0 {
		return ""
	}
	values := headers[http.CanonicalHeaderKey(key)]
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func parseMinIOObjectFilename(objectKey string, info minio.ObjectInfo) string {
	disposition := firstHeaderValue(info.Metadata, "Content-Disposition")
	if disposition == "" {
		return objectKey
	}
	return disposition
}

func minioObjectReaderFileName(objectKey string, info minio.ObjectInfo) string {
	fileName := parseMinIOObjectFilename(objectKey, info)
	if strings.TrimSpace(fileName) != "" {
		return fileName
	}
	if index := strings.LastIndex(strings.TrimRight(objectKey, "/"), "/"); index >= 0 {
		return objectKey[index+1:]
	}
	return strings.TrimSpace(objectKey)
}

func minioObjectCacheControl(info minio.ObjectInfo) string {
	value := firstHeaderValue(info.Metadata, "Cache-Control")
	if value != "" {
		return value
	}
	if value = firstHeaderValue(info.Metadata, "X-Amz-Meta-Cache-Control"); value != "" {
		return value
	}
	return ""
}

func minioObjectSize(info minio.ObjectInfo) int64 {
	if info.Size > 0 {
		return info.Size
	}
	if value := firstHeaderValue(info.Metadata, "Content-Length"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}
