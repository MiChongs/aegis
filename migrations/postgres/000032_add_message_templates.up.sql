CREATE TABLE IF NOT EXISTS message_templates (
    id          BIGSERIAL    PRIMARY KEY,
    code        VARCHAR(64)  NOT NULL UNIQUE,
    name        VARCHAR(128) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    channel     VARCHAR(16)  NOT NULL DEFAULT 'email',
    subject     TEXT         NOT NULL DEFAULT '',
    body_html   TEXT         NOT NULL DEFAULT '',
    body_text   TEXT         NOT NULL DEFAULT '',
    variables   JSONB        NOT NULL DEFAULT '[]',
    is_builtin  BOOLEAN      NOT NULL DEFAULT FALSE,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_by  BIGINT       NULL REFERENCES admin_accounts(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT message_templates_channel_chk CHECK (channel IN ('email', 'sms', 'notification'))
);

-- 内置邮件模板种子
INSERT INTO message_templates (code, name, description, channel, subject, body_html, body_text, variables, is_builtin)
VALUES
('verification_code', '邮件验证码', '发送邮件验证码', 'email',
 '{{.AppName}} - 验证码',
 '<div style="font-family:-apple-system,BlinkMacSystemFont,''Segoe UI'',Roboto,sans-serif;max-width:520px;margin:0 auto;padding:32px;line-height:1.8"><h2 style="color:#18181b;margin:0 0 16px">{{.AppName}}</h2><p style="color:#52525b">用途：{{.Purpose}}</p><p style="color:#52525b">您的验证码是：</p><div style="font-size:36px;font-weight:700;letter-spacing:10px;color:#18181b;background:#f4f4f5;border-radius:12px;padding:20px;text-align:center;margin:16px 0">{{.Code}}</div><p style="color:#71717a;font-size:13px">验证码 {{.ExpireMinutes}} 分钟内有效，请勿泄露。</p></div>',
 '{{.AppName}} 验证码：{{.Code}}，{{.ExpireMinutes}} 分钟内有效。用途：{{.Purpose}}',
 '[{"key":"AppName","name":"应用名称","example":"MyApp"},{"key":"Code","name":"验证码","example":"123456"},{"key":"Purpose","name":"用途","example":"登录验证"},{"key":"ExpireMinutes","name":"有效分钟","example":"5"}]',
 TRUE),

('password_reset', '密码重置', '发送密码重置链接', 'email',
 '{{.AppName}} - 密码重置',
 '<div style="font-family:-apple-system,BlinkMacSystemFont,''Segoe UI'',Roboto,sans-serif;max-width:520px;margin:0 auto;padding:32px;line-height:1.8"><h2 style="color:#18181b;margin:0 0 16px">{{.AppName}}</h2><p style="color:#52525b">收到密码重置请求。</p><p style="color:#52525b">点击下方按钮重置密码：</p><div style="text-align:center;margin:24px 0"><a href="{{.ResetURL}}" style="display:inline-block;padding:12px 32px;background:#18181b;color:#fff;text-decoration:none;border-radius:8px;font-weight:600">重置密码</a></div><p style="color:#71717a;font-size:13px">链接 30 分钟内有效。如非本人操作，请忽略。</p></div>',
 '{{.AppName}} 密码重置链接：{{.ResetURL}}，30 分钟内有效。',
 '[{"key":"AppName","name":"应用名称","example":"MyApp"},{"key":"ResetURL","name":"重置链接","example":"https://example.com/reset?token=abc"},{"key":"UserName","name":"用户名","example":"张三"}]',
 TRUE),

('welcome', '欢迎邮件', '新用户欢迎', 'email',
 '欢迎加入 {{.AppName}}',
 '<div style="font-family:-apple-system,BlinkMacSystemFont,''Segoe UI'',Roboto,sans-serif;max-width:520px;margin:0 auto;padding:32px;line-height:1.8"><h2 style="color:#18181b;margin:0 0 16px">欢迎加入 {{.AppName}}</h2><p style="color:#52525b">{{.UserName}}，您好！</p><p style="color:#52525b">您的账号已完成初始化，现在可以开始使用了。</p></div>',
 '{{.UserName}}，欢迎加入 {{.AppName}}！您的账号已完成初始化。',
 '[{"key":"AppName","name":"应用名称","example":"MyApp"},{"key":"UserName","name":"用户名","example":"张三"}]',
 TRUE),

('sms_verification', '短信验证码', '发送短信验证码', 'sms',
 '',
 '',
 '您的验证码是 {{.Code}}，{{.ExpireMinutes}} 分钟内有效。',
 '[{"key":"Code","name":"验证码","example":"123456"},{"key":"ExpireMinutes","name":"有效分钟","example":"5"}]',
 TRUE)

ON CONFLICT (code) DO NOTHING;
