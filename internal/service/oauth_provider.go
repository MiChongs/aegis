package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	apperrors "aegis/pkg/errors"
)

type OAuthProvider struct {
	cfg config.OAuthProviderConfig
}

func NewOAuthProvider(cfg config.OAuthProviderConfig) *OAuthProvider {
	return &OAuthProvider{cfg: cfg}
}

func (p *OAuthProvider) AuthURL(state string) string {
	params := url.Values{}
	switch p.cfg.Name {
	case "wechat":
		params.Set("appid", p.cfg.ClientID)
		params.Set("scope", strings.Join(p.cfg.Scopes, ","))
		params.Set("response_type", "code")
		params.Set("redirect_uri", p.cfg.RedirectURL)
		return p.cfg.AuthURL + "?" + params.Encode() + "#wechat_redirect"
	case "qq":
		params.Set("client_id", p.cfg.ClientID)
		params.Set("redirect_uri", p.cfg.RedirectURL)
		params.Set("response_type", "code")
		params.Set("state", state)
		if len(p.cfg.Scopes) > 0 {
			params.Set("scope", strings.Join(p.cfg.Scopes, ","))
		}
	default:
		params.Set("client_id", p.cfg.ClientID)
		params.Set("redirect_uri", p.cfg.RedirectURL)
		params.Set("response_type", "code")
		params.Set("state", state)
		if len(p.cfg.Scopes) > 0 {
			params.Set("scope", strings.Join(p.cfg.Scopes, " "))
		}
	}
	return p.cfg.AuthURL + "?" + params.Encode()
}

func (p *OAuthProvider) ExchangeCode(ctx context.Context, client *http.Client, code string) (authdomain.ProviderProfile, error) {
	if p.cfg.ClientID == "" || p.cfg.ClientSecret == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40010, http.StatusBadRequest, "OAuth2 提供商未配置密钥")
	}
	if strings.TrimSpace(code) == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40011, http.StatusBadRequest, "OAuth2 授权码不能为空")
	}
	if p.cfg.RedirectURL == "" {
		return authdomain.ProviderProfile{}, apperrors.New(40012, http.StatusBadRequest, "OAuth2 回调地址未配置")
	}
	tokenResp, err := p.exchangeToken(ctx, client, code)
	if err != nil {
		return authdomain.ProviderProfile{}, err
	}
	accessToken, _ := tokenResp["access_token"].(string)
	if accessToken == "" {
		return authdomain.ProviderProfile{}, apperrors.New(50201, http.StatusBadGateway, "OAuth2 access_token 获取失败")
	}
	return p.fetchProfile(ctx, client, tokenResp)
}

func (p *OAuthProvider) exchangeToken(ctx context.Context, client *http.Client, code string) (map[string]any, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	method := http.MethodPost
	requestURL := p.cfg.TokenURL
	if p.cfg.Name == "wechat" || p.cfg.Name == "qq" {
		method = http.MethodGet
	}
	if p.cfg.Name == "wechat" {
		form = url.Values{}
		form.Set("appid", p.cfg.ClientID)
		form.Set("secret", p.cfg.ClientSecret)
		form.Set("code", code)
		form.Set("grant_type", "authorization_code")
	}
	if p.cfg.Name == "qq" {
		form = url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", p.cfg.ClientID)
		form.Set("client_secret", p.cfg.ClientSecret)
		form.Set("code", code)
		form.Set("redirect_uri", p.cfg.RedirectURL)
	}

	var requestBody io.Reader
	if method == http.MethodGet {
		if strings.Contains(requestURL, "?") {
			requestURL += "&" + form.Encode()
		} else {
			requestURL += "?" + form.Encode()
		}
	} else {
		requestBody = strings.NewReader(form.Encode())
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL, requestBody)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("Accept", "application/json")
	if p.cfg.Name == "github" {
		request.Header.Set("Accept", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("oauth token exchange http %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	result := map[string]any{}
	if json.Unmarshal(responseBody, &result) == nil {
		if errValue, ok := result["error"]; ok && errValue != nil && fmt.Sprint(errValue) != "" {
			return nil, fmt.Errorf("oauth token exchange failed: %v", errValue)
		}
		return result, nil
	}
	values, err := url.ParseQuery(string(responseBody))
	if err != nil {
		return nil, fmt.Errorf("parse token response failed: %w", err)
	}
	for key, vals := range values {
		if len(vals) > 0 {
			result[key] = vals[0]
		}
	}
	if errValue, ok := result["error"]; ok && errValue != nil && fmt.Sprint(errValue) != "" {
		return nil, fmt.Errorf("oauth token exchange failed: %v", errValue)
	}
	return result, nil
}

func (p *OAuthProvider) fetchProfile(ctx context.Context, client *http.Client, tokenResp map[string]any) (authdomain.ProviderProfile, error) {
	accessToken, _ := tokenResp["access_token"].(string)
	refreshToken, _ := tokenResp["refresh_token"].(string)
	result := authdomain.ProviderProfile{
		Provider: p.cfg.Name,
		Tokens: map[string]string{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
		},
	}

	switch p.cfg.Name {
	case "qq":
		return p.fetchQQProfile(ctx, client, result)
	case "wechat":
		openid, _ := tokenResp["openid"].(string)
		unionid, _ := tokenResp["unionid"].(string)
		return p.fetchWechatProfile(ctx, client, result, openid, unionid)
	case "weibo":
		uid, _ := tokenResp["uid"].(string)
		return p.fetchWeiboProfile(ctx, client, result, uid)
	case "github":
		return p.fetchGitHubProfile(ctx, client, result)
	default:
		return p.fetchGenericProfile(ctx, client, result)
	}
}

func (p *OAuthProvider) fetchGenericProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	request.Header.Set("Authorization", "Bearer "+profile.Tokens["access_token"])
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("oauth profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	if errValue, ok := raw["error"]; ok && errValue != nil && fmt.Sprint(errValue) != "" {
		return profile, fmt.Errorf("oauth profile fetch failed: %v", errValue)
	}
	profile.RawProfile = raw
	profile.ProviderUserID = firstString(raw, "sub", "id", "login", "openid", "uid")
	profile.Nickname = firstString(raw, "name", "login", "preferred_username", "nickname", "screen_name")
	profile.Avatar = firstString(raw, "avatar_url", "picture", "profile_image_url", "avatar")
	profile.Email = firstString(raw, "email")
	if profile.ProviderUserID == "" {
		return profile, apperrors.New(50202, http.StatusBadGateway, "OAuth2 用户信息缺少唯一标识")
	}
	return profile, nil
}

func (p *OAuthProvider) fetchQQProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.qq.com/oauth2.0/me?access_token="+url.QueryEscape(profile.Tokens["access_token"]), nil)
	if err != nil {
		return profile, err
	}
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("qq openid fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	text := string(body)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return profile, apperrors.New(50203, http.StatusBadGateway, "QQ openid 获取失败")
	}
	openResp := map[string]any{}
	if err := json.Unmarshal([]byte(text[start:end+1]), &openResp); err != nil {
		return profile, err
	}
	if ret := firstString(openResp, "ret"); ret != "" && ret != "0" {
		return profile, apperrors.New(50203, http.StatusBadGateway, "QQ openid 获取失败")
	}
	openid := firstString(openResp, "openid")
	if openid == "" {
		return profile, apperrors.New(50203, http.StatusBadGateway, "QQ openid 获取失败")
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	query := request.URL.Query()
	query.Set("access_token", profile.Tokens["access_token"])
	query.Set("oauth_consumer_key", p.cfg.ClientID)
	query.Set("openid", openid)
	request.URL.RawQuery = query.Encode()
	response, err = client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("qq profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	if ret := firstString(raw, "ret"); ret != "" && ret != "0" {
		return profile, apperrors.New(50203, http.StatusBadGateway, "QQ 用户信息获取失败")
	}
	profile.ProviderUserID = openid
	profile.Nickname = firstString(raw, "nickname")
	profile.Avatar = firstString(raw, "figureurl_qq_2", "figureurl_2", "figureurl_qq_1")
	profile.RawProfile = raw
	return profile, nil
}

func (p *OAuthProvider) fetchGitHubProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	profile, err := p.fetchGenericProfile(ctx, client, profile)
	if err != nil {
		return profile, err
	}
	if profile.Email != "" {
		return profile, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return profile, err
	}
	request.Header.Set("Authorization", "Bearer "+profile.Tokens["access_token"])
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("github email fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var emails []map[string]any
	if err := json.Unmarshal(body, &emails); err != nil {
		return profile, nil
	}
	for _, item := range emails {
		email := firstString(item, "email")
		if email == "" {
			continue
		}
		primary, _ := item["primary"].(bool)
		verified, _ := item["verified"].(bool)
		if primary || verified {
			profile.Email = email
			return profile, nil
		}
	}
	if len(emails) > 0 {
		profile.Email = firstString(emails[0], "email")
	}
	return profile, nil
}

func (p *OAuthProvider) fetchWechatProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, openid, unionid string) (authdomain.ProviderProfile, error) {
	if openid == "" {
		return profile, apperrors.New(50204, http.StatusBadGateway, "微信 openid 获取失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	query := request.URL.Query()
	query.Set("access_token", profile.Tokens["access_token"])
	query.Set("openid", openid)
	query.Set("lang", "zh_CN")
	request.URL.RawQuery = query.Encode()
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("wechat profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	if errCode := firstString(raw, "errcode"); errCode != "" && errCode != "0" {
		return profile, apperrors.New(50204, http.StatusBadGateway, "微信用户信息获取失败")
	}
	profile.ProviderUserID = openid
	profile.UnionID = unionid
	profile.Nickname = firstString(raw, "nickname")
	profile.Avatar = firstString(raw, "headimgurl")
	profile.RawProfile = raw
	return profile, nil
}

func (p *OAuthProvider) fetchWeiboProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile, uid string) (authdomain.ProviderProfile, error) {
	if uid == "" {
		return profile, apperrors.New(50205, http.StatusBadGateway, "微博 uid 获取失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	query := request.URL.Query()
	query.Set("access_token", profile.Tokens["access_token"])
	query.Set("uid", uid)
	request.URL.RawQuery = query.Encode()
	response, err := client.Do(request)
	if err != nil {
		return profile, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return profile, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return profile, fmt.Errorf("weibo profile fetch http %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return profile, err
	}
	profile.ProviderUserID = uid
	profile.Nickname = firstString(raw, "screen_name", "name")
	profile.Avatar = firstString(raw, "avatar_large", "profile_image_url")
	profile.RawProfile = raw
	return profile, nil
}

func firstString(source map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := source[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		case float64:
			return fmt.Sprintf("%.0f", typed)
		case int:
			return strconv.Itoa(typed)
		case int64:
			return strconv.FormatInt(typed, 10)
		case json.Number:
			return typed.String()
		}
	}
	return ""
}
