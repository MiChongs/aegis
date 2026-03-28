package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	captchadomain "aegis/internal/domain/captcha"

	redislib "github.com/redis/go-redis/v9"
)

// CaptchaRepository 验证码 Redis 存储层
type CaptchaRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewCaptchaRepository 创建验证码仓储
func NewCaptchaRepository(client *redislib.Client, keyPrefix string) *CaptchaRepository {
	return &CaptchaRepository{client: client, keyPrefix: keyPrefix}
}

// ────────────────────── 图形验证码存取 ──────────────────────

// SetCaptcha 存储图形验证码记录
func (r *CaptchaRepository) SetCaptcha(ctx context.Context, captchaID string, record captchadomain.CaptchaRecord, ttl time.Duration) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.captchaKey(captchaID), data, ttl).Err()
}

// GetCaptcha 获取图形验证码记录
func (r *CaptchaRepository) GetCaptcha(ctx context.Context, captchaID string) (*captchadomain.CaptchaRecord, error) {
	value, err := r.client.Get(ctx, r.captchaKey(captchaID)).Result()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var record captchadomain.CaptchaRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// DeleteCaptcha 删除图形验证码记录
func (r *CaptchaRepository) DeleteCaptcha(ctx context.Context, captchaID string) error {
	return r.client.Del(ctx, r.captchaKey(captchaID)).Err()
}

// IncrementCaptchaAttempts 增加验证尝试次数，返回更新后的次数
func (r *CaptchaRepository) IncrementCaptchaAttempts(ctx context.Context, captchaID string) (int, error) {
	record, err := r.GetCaptcha(ctx, captchaID)
	if err != nil || record == nil {
		return 0, err
	}
	record.Attempts++
	ttl, err := r.client.TTL(ctx, r.captchaKey(captchaID)).Result()
	if err != nil || ttl <= 0 {
		return record.Attempts, err
	}
	data, _ := json.Marshal(record)
	_ = r.client.Set(ctx, r.captchaKey(captchaID), data, ttl).Err()
	return record.Attempts, nil
}

// ────────────────────── 短信验证码存取 ──────────────────────

// SetSMSCode 存储短信验证码
func (r *CaptchaRepository) SetSMSCode(ctx context.Context, appID int64, phone string, purpose captchadomain.Purpose, record captchadomain.SMSRecord, ttl time.Duration) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.smsCodeKey(appID, phone, purpose), data, ttl).Err()
}

// GetSMSCode 获取短信验证码
func (r *CaptchaRepository) GetSMSCode(ctx context.Context, appID int64, phone string, purpose captchadomain.Purpose) (*captchadomain.SMSRecord, error) {
	value, err := r.client.Get(ctx, r.smsCodeKey(appID, phone, purpose)).Result()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var record captchadomain.SMSRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// DeleteSMSCode 删除短信验证码
func (r *CaptchaRepository) DeleteSMSCode(ctx context.Context, appID int64, phone string, purpose captchadomain.Purpose) error {
	return r.client.Del(ctx, r.smsCodeKey(appID, phone, purpose)).Err()
}

// IncrementSMSAttempts 增加短信验证尝试次数
func (r *CaptchaRepository) IncrementSMSAttempts(ctx context.Context, appID int64, phone string, purpose captchadomain.Purpose) (int, error) {
	record, err := r.GetSMSCode(ctx, appID, phone, purpose)
	if err != nil || record == nil {
		return 0, err
	}
	record.Attempts++
	ttl, err := r.client.TTL(ctx, r.smsCodeKey(appID, phone, purpose)).Result()
	if err != nil || ttl <= 0 {
		return record.Attempts, err
	}
	data, _ := json.Marshal(record)
	_ = r.client.Set(ctx, r.smsCodeKey(appID, phone, purpose), data, ttl).Err()
	return record.Attempts, nil
}

// ────────────────────── 短信发送频率限制 ──────────────────────

// CheckSMSSendInterval 检查短信发送间隔（防刷）
func (r *CaptchaRepository) CheckSMSSendInterval(ctx context.Context, appID int64, phone string) (bool, error) {
	exists, err := r.client.Exists(ctx, r.smsSendLockKey(appID, phone)).Result()
	if err != nil {
		return false, err
	}
	return exists == 0, nil // true = 可以发送
}

// SetSMSSendLock 设置短信发送锁（间隔控制）
func (r *CaptchaRepository) SetSMSSendLock(ctx context.Context, appID int64, phone string, interval time.Duration) error {
	return r.client.Set(ctx, r.smsSendLockKey(appID, phone), "1", interval).Err()
}

// GetSMSDailyCount 获取每日发送次数
func (r *CaptchaRepository) GetSMSDailyCount(ctx context.Context, appID int64, phone string) (int64, error) {
	count, err := r.client.Get(ctx, r.smsDailyCountKey(appID, phone)).Int64()
	if err != nil {
		if err == redislib.Nil {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

// IncrementSMSDailyCount 递增每日发送次数
func (r *CaptchaRepository) IncrementSMSDailyCount(ctx context.Context, appID int64, phone string) error {
	key := r.smsDailyCountKey(appID, phone)
	pipe := r.client.TxPipeline()
	pipe.Incr(ctx, key)
	// 设置过期时间到当天结束（UTC+8 北京时间）
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// ────────────────────── IP 维度限流（防短信轰炸） ──────────────────────

// GetIPHourlyCount 获取 IP 本小时发送次数
func (r *CaptchaRepository) GetIPHourlyCount(ctx context.Context, ip string) (int64, error) {
	count, err := r.client.Get(ctx, r.smsIPHourlyKey(ip)).Int64()
	if err != nil {
		if err == redislib.Nil {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

// IncrementIPHourlyCount 递增 IP 小时发送计数
func (r *CaptchaRepository) IncrementIPHourlyCount(ctx context.Context, ip string) error {
	key := r.smsIPHourlyKey(ip)
	pipe := r.client.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// GetIPDailyCount 获取 IP 当日发送次数
func (r *CaptchaRepository) GetIPDailyCount(ctx context.Context, ip string) (int64, error) {
	count, err := r.client.Get(ctx, r.smsIPDailyKey(ip)).Int64()
	if err != nil {
		if err == redislib.Nil {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

// IncrementIPDailyCount 递增 IP 每日发送计数
func (r *CaptchaRepository) IncrementIPDailyCount(ctx context.Context, ip string) error {
	key := r.smsIPDailyKey(ip)
	pipe := r.client.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// ────────────────────── 全局手机号限流（跨 AppID） ──────────────────────

// GetGlobalPhoneDailyCount 获取手机号当日全局发送次数（跨所有 AppID）
func (r *CaptchaRepository) GetGlobalPhoneDailyCount(ctx context.Context, phone string) (int64, error) {
	count, err := r.client.Get(ctx, r.smsGlobalPhoneDailyKey(phone)).Int64()
	if err != nil {
		if err == redislib.Nil {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

// IncrementGlobalPhoneDailyCount 递增手机号全局每日发送计数
func (r *CaptchaRepository) IncrementGlobalPhoneDailyCount(ctx context.Context, phone string) error {
	key := r.smsGlobalPhoneDailyKey(phone)
	pipe := r.client.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// ────────────────────── Key 格式 ──────────────────────

func (r *CaptchaRepository) captchaKey(captchaID string) string {
	return fmt.Sprintf("%s:captcha:image:%s", r.keyPrefix, captchaID)
}

func (r *CaptchaRepository) smsCodeKey(appID int64, phone string, purpose captchadomain.Purpose) string {
	return fmt.Sprintf("%s:captcha:sms:%d:%s:%s", r.keyPrefix, appID, purpose, phone)
}

func (r *CaptchaRepository) smsSendLockKey(appID int64, phone string) string {
	return fmt.Sprintf("%s:captcha:sms:lock:%d:%s", r.keyPrefix, appID, phone)
}

func (r *CaptchaRepository) smsDailyCountKey(appID int64, phone string) string {
	return fmt.Sprintf("%s:captcha:sms:daily:%d:%s", r.keyPrefix, appID, phone)
}

func (r *CaptchaRepository) smsIPHourlyKey(ip string) string {
	return fmt.Sprintf("%s:captcha:sms:ip:hourly:%s", r.keyPrefix, ip)
}

func (r *CaptchaRepository) smsIPDailyKey(ip string) string {
	return fmt.Sprintf("%s:captcha:sms:ip:daily:%s", r.keyPrefix, ip)
}

func (r *CaptchaRepository) smsGlobalPhoneDailyKey(phone string) string {
	return fmt.Sprintf("%s:captcha:sms:global:daily:%s", r.keyPrefix, phone)
}
