package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	oauthdomain "aegis/internal/domain/oauth"
	apperrors "aegis/pkg/errors"
)

// OAuthProvider 单个第三方登录渠道的协议适配器。
//
// 配置既可能来自平台级 .env，也可能来自应用级 app_oauth_providers；
// 协议差异统一由 cfg.Kind 决定（留空时回落到 cfg.Name，保持旧配置行为）。
type OAuthProvider struct {
	cfg config.OAuthProviderConfig
}

func NewOAuthProvider(cfg config.OAuthProviderConfig) *OAuthProvider {
	return &OAuthProvider{cfg: cfg}
}

// kind 返回协议适配器类型；未显式指定时按渠道名推断，
// 使自定义 slug（如 my-github）也能复用内置适配器。
func (p *OAuthProvider) kind() string {
	kind := strings.ToLower(strings.TrimSpace(p.cfg.Kind))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(p.cfg.Name))
	}
	switch kind {
	case oauthdomain.KindQQ, oauthdomain.KindWechat, oauthdomain.KindWeibo,
		oauthdomain.KindGitHub, oauthdomain.KindMicrosoft:
		return kind
	default:
		return oauthdomain.KindGeneric
	}
}

func (p *OAuthProvider) tokenAuthStyle() string {
	switch strings.ToLower(strings.TrimSpace(p.cfg.TokenAuthStyle)) {
	case oauthdomain.TokenAuthParams:
		return oauthdomain.TokenAuthParams
	case oauthdomain.TokenAuthBasic:
		return oauthdomain.TokenAuthBasic
	default:
		return oauthdomain.TokenAuthAuto
	}
}

func (p *OAuthProvider) userInfoAuthStyle() string {
	if strings.EqualFold(strings.TrimSpace(p.cfg.UserInfoAuthStyle), oauthdomain.UserInfoAuthQuery) {
		return oauthdomain.UserInfoAuthQuery
	}
	return oauthdomain.UserInfoAuthHeader
}

func (p *OAuthProvider) AuthURL(state string) string {
	params := url.Values{}
	switch p.kind() {
	case oauthdomain.KindWechat:
		params.Set("appid", p.cfg.ClientID)
		params.Set("scope", strings.Join(p.cfg.Scopes, ","))
		params.Set("response_type", "code")
		params.Set("redirect_uri", p.cfg.RedirectURL)
		params.Set("state", state)
		p.applyExtraAuthParams(params)
		return p.cfg.AuthURL + "?" + params.Encode() + "#wechat_redirect"
	case oauthdomain.KindQQ:
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
	p.applyExtraAuthParams(params)
	return p.cfg.AuthURL + "?" + params.Encode()
}

// applyExtraAuthParams 追加管理端自定义的授权参数（如 prompt=consent、access_type=offline）。
// 协议必需参数（client_id / redirect_uri / response_type / state / scope）不允许被覆盖。
func (p *OAuthProvider) applyExtraAuthParams(params url.Values) {
	if len(p.cfg.ExtraAuthParams) == 0 {
		return
	}
	reserved := map[string]bool{
		"client_id": true, "appid": true, "redirect_uri": true,
		"response_type": true, "state": true, "scope": true,
	}
	keys := make([]string, 0, len(p.cfg.ExtraAuthParams))
	for key := range p.cfg.ExtraAuthParams {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		normalized := strings.TrimSpace(key)
		if normalized == "" || reserved[strings.ToLower(normalized)] {
			continue
		}
		params.Set(normalized, p.cfg.ExtraAuthParams[key])
	}
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

// exchangeToken 换取 access_token。
//
// tokenAuthStyle=auto 时先按表单参数提交（绝大多数渠道支持），
// 仅在返回 invalid_client / 401 时用 HTTP Basic 重试一次 ——
// 覆盖那些严格要求 Basic 的实现（如 LINUX DO Connect），无需人工切换。
func (p *OAuthProvider) exchangeToken(ctx context.Context, client *http.Client, code string) (map[string]any, error) {
	style := p.tokenAuthStyle()
	kind := p.kind()
	// QQ / 微信走各自的私有参数格式，不适用 Basic
	if kind == oauthdomain.KindQQ || kind == oauthdomain.KindWechat {
		style = oauthdomain.TokenAuthParams
	}
	result, err := p.requestToken(ctx, client, code, style == oauthdomain.TokenAuthBasic)
	if err != nil && style == oauthdomain.TokenAuthAuto && isClientAuthError(err) {
		return p.requestToken(ctx, client, code, true)
	}
	return result, err
}

func (p *OAuthProvider) requestToken(ctx context.Context, client *http.Client, code string, basicAuth bool) (map[string]any, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	method := http.MethodPost
	requestURL := p.cfg.TokenURL
	kind := p.kind()
	if kind == oauthdomain.KindWechat || kind == oauthdomain.KindQQ {
		method = http.MethodGet
	}
	if kind == oauthdomain.KindWechat {
		form = url.Values{}
		form.Set("appid", p.cfg.ClientID)
		form.Set("secret", p.cfg.ClientSecret)
		form.Set("code", code)
		form.Set("grant_type", "authorization_code")
	}
	if kind == oauthdomain.KindQQ {
		form = url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("client_id", p.cfg.ClientID)
		form.Set("client_secret", p.cfg.ClientSecret)
		form.Set("code", code)
		form.Set("redirect_uri", p.cfg.RedirectURL)
		form.Set("fmt", "json")
	}
	if basicAuth {
		// Basic 模式下凭据只走 Authorization 头，表单里不再重复携带 secret
		form.Del("client_secret")
		form.Del("client_id")
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
	if basicAuth {
		credential := base64.StdEncoding.EncodeToString([]byte(p.cfg.ClientID + ":" + p.cfg.ClientSecret))
		request.Header.Set("Authorization", "Basic "+credential)
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

// isClientAuthError 判断错误是否为"客户端凭据认证方式不被接受"，用于 auto 模式的 Basic 重试。
func isClientAuthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid_client") ||
		strings.Contains(message, "unauthorized_client") ||
		strings.Contains(message, "http 401") ||
		strings.Contains(message, "http 403")
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

	switch p.kind() {
	case oauthdomain.KindQQ:
		return p.fetchQQProfile(ctx, client, result)
	case oauthdomain.KindWechat:
		openid, _ := tokenResp["openid"].(string)
		unionid, _ := tokenResp["unionid"].(string)
		return p.fetchWechatProfile(ctx, client, result, openid, unionid)
	case oauthdomain.KindWeibo:
		uid := firstString(tokenResp, "uid")
		return p.fetchWeiboProfile(ctx, client, result, uid)
	case oauthdomain.KindGitHub:
		return p.fetchGitHubProfile(ctx, client, result)
	default:
		return p.fetchGenericProfile(ctx, client, result)
	}
}

func (p *OAuthProvider) fetchGenericProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	if strings.TrimSpace(p.cfg.UserInfoURL) == "" {
		return profile, apperrors.New(40012, http.StatusBadRequest, "OAuth2 用户信息地址未配置")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return profile, err
	}
	if p.userInfoAuthStyle() == oauthdomain.UserInfoAuthQuery {
		query := request.URL.Query()
		query.Set("access_token", profile.Tokens["access_token"])
		request.URL.RawQuery = query.Encode()
	} else {
		request.Header.Set("Authorization", "Bearer "+profile.Tokens["access_token"])
	}
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
	profile.ProviderUserID = p.mappedString(raw, oauthdomain.MappingID, "sub", "id", "login", "openid", "uid", "user_id")
	profile.Nickname = p.mappedString(raw, oauthdomain.MappingNickname, "name", "login", "preferred_username", "nickname", "screen_name", "username")
	profile.Avatar = p.mappedString(raw, oauthdomain.MappingAvatar, "avatar_url", "picture", "profile_image_url", "avatar", "avatar_template")
	profile.Email = p.mappedString(raw, oauthdomain.MappingEmail, "email")
	profile.UnionID = p.mappedString(raw, oauthdomain.MappingUnionID, "unionid", "union_id")
	if profile.ProviderUserID == "" {
		return profile, apperrors.New(50202, http.StatusBadGateway, "OAuth2 用户信息缺少唯一标识")
	}
	return profile, nil
}

// mappedString 先按管理端配置的字段映射（支持 data.user.id 这类点号路径）取值，
// 未配置映射时回落到内置候选键，兼容既有渠道。
func (p *OAuthProvider) mappedString(raw map[string]any, mappingKey string, fallbackKeys ...string) string {
	if path := strings.TrimSpace(p.cfg.ProfileMapping[mappingKey]); path != "" {
		if value := lookupProfilePath(raw, path); value != "" {
			return value
		}
	}
	return firstString(raw, fallbackKeys...)
}

// lookupProfilePath 按点号路径在嵌套 JSON 中取标量值。
func lookupProfilePath(raw map[string]any, path string) string {
	segments := strings.Split(path, ".")
	var current any = raw
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return ""
		}
		node, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = node[segment]
		if !ok || current == nil {
			return ""
		}
	}
	return scalarToString(current)
}

func (p *OAuthProvider) fetchQQProfile(ctx context.Context, client *http.Client, profile authdomain.ProviderProfile) (authdomain.ProviderProfile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.qq.com/oauth2.0/me?fmt=json&access_token="+url.QueryEscape(profile.Tokens["access_token"]), nil)
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
	profile.UnionID = firstString(openResp, "unionid")
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
	if profile.UnionID == "" {
		profile.UnionID = firstString(raw, "unionid")
	}
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
		if text := scalarToString(value); text != "" {
			return text
		}
	}
	return ""
}

func scalarToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	}
	return ""
}
