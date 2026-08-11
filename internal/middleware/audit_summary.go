package middleware

import (
	"fmt"
	"net/http"
	"strings"

	systemdomain "aegis/internal/domain/system"
)

// 本文件提供审计日志的人类可读化与结构化：
//   - action key：machine-readable 动作键（如 admin.user.freeze / app.role_application.statistics）
//   - category：业务域粗分类（auth / admin / user / app / rbac / content / storage / workflow / release /
//              settings / security / audit / monitor / email / payment / points / signin / lottery / api）
//   - severity：基于业务域 + 方法 + 路径末段（读/写）估算等级（info / low / medium / high / critical）
//   - summary：一句话中文摘要（如"查询统计 角色申请"、"冻结 用户 42（失败 403）"）
//
// 关键设计：单一 actionVerbRegistry 取代分散在 actionVerb / verbKey / isActionVerbKey /
// readLikeLastSegments 的多份定义，保证机器 verb、中文动词、读/写语义三者永不漂移。

// actionMeta 单个"动作段"的元数据
//
//	Key  : machine verb，snake_case，入 action key（如 "statistics" / "batch_review"）
//	Verb : 展示给人类的中文动词短语（如 "查询统计" / "批量审批"）
//	Read : true 表示语义上为只读；严重度判定会下调到 info
type actionMeta struct {
	Key  string
	Verb string
	Read bool
}

// actionVerbRegistry 动作段 → 元数据 的唯一真源
//
//	Key 以规范化形式（小写 + snake_case）存，查询前用 normalizeActionKey 处理输入段
//	包括两类：
//	  1. 写动作：freeze / approve / revoke / rotate / import / upload 等
//	  2. 读动作（POST body-based query）：statistics / list / search / summary 等
var actionVerbRegistry = map[string]actionMeta{
	// ── 生命周期类写动作 ──
	"freeze":       {Key: "freeze", Verb: "冻结"},
	"unfreeze":     {Key: "unfreeze", Verb: "解冻"},
	"disable":      {Key: "disable", Verb: "禁用"},
	"deactivate":   {Key: "deactivate", Verb: "禁用"},
	"enable":       {Key: "enable", Verb: "启用"},
	"activate":     {Key: "activate", Verb: "启用"},
	"lock":         {Key: "lock", Verb: "锁定"},
	"unlock":       {Key: "unlock", Verb: "解锁"},
	"pause":        {Key: "pause", Verb: "暂停"},
	"resume":       {Key: "resume", Verb: "恢复"},
	"start":        {Key: "start", Verb: "启动"},
	"stop":         {Key: "stop", Verb: "停止"},
	"cancel":       {Key: "cancel", Verb: "取消"},
	"archive":      {Key: "archive", Verb: "归档"},
	"unarchive":    {Key: "unarchive", Verb: "取消归档"},
	"restore":      {Key: "restore", Verb: "恢复"},
	"discard":      {Key: "discard", Verb: "丢弃"},

	// ── 审批流 ──
	"approve":      {Key: "approve", Verb: "审批通过"},
	"reject":       {Key: "reject", Verb: "驳回"},
	"review":       {Key: "review", Verb: "审批"},
	"batch_review": {Key: "batch_review", Verb: "批量审批"},
	"submit":       {Key: "submit", Verb: "提交"},
	"withdraw":     {Key: "withdraw", Verb: "撤回"},
	"resubmit":     {Key: "resubmit", Verb: "重新提交"},

	// ── 状态处理 ──
	"resolve":      {Key: "resolve", Verb: "处理"},
	"acknowledge":  {Key: "acknowledge", Verb: "确认"},
	"ack":          {Key: "ack", Verb: "确认"},
	"dismiss":      {Key: "dismiss", Verb: "忽略"},
	"mute":         {Key: "mute", Verb: "静音"},
	"unmute":       {Key: "unmute", Verb: "取消静音"},
	"ignore":       {Key: "ignore", Verb: "忽略"},

	// ── 认证 & 密钥 ──
	"login":          {Key: "login", Verb: "登录"},
	"logout":         {Key: "logout", Verb: "登出"},
	"refresh":        {Key: "refresh", Verb: "刷新"},
	"verify":         {Key: "verify", Verb: "验证"},
	"verify_mfa":     {Key: "verify_mfa", Verb: "验证 MFA"},
	"reset_password": {Key: "reset_password", Verb: "重置密码"},
	"change_password": {Key: "change_password", Verb: "修改密码"},
	"revoke":         {Key: "revoke", Verb: "吊销"},
	"regenerate_key": {Key: "regenerate_key", Verb: "重新生成密钥"},
	"rotate_key":     {Key: "rotate_key", Verb: "轮换密钥"},
	"rotate":         {Key: "rotate", Verb: "轮换"},
	"enroll":         {Key: "enroll", Verb: "启用"},
	"unenroll":       {Key: "unenroll", Verb: "解除绑定"},
	"register":       {Key: "register", Verb: "注册"},
	"unregister":     {Key: "unregister", Verb: "注销"},

	// ── 数据流 ──
	"import":   {Key: "import", Verb: "导入"},
	"export":   {Key: "export", Verb: "导出"},
	"upload":   {Key: "upload", Verb: "上传"},
	"download": {Key: "download", Verb: "下载"},
	"migrate":  {Key: "migrate", Verb: "迁移"},
	"backup":   {Key: "backup", Verb: "备份"},
	"sync":     {Key: "sync", Verb: "同步"},
	"clone":    {Key: "clone", Verb: "克隆"},
	"duplicate": {Key: "duplicate", Verb: "复制"},
	"copy":     {Key: "copy", Verb: "复制"},
	"move":     {Key: "move", Verb: "移动"},

	// ── 清理 / 重建 ──
	"clear":      {Key: "clear", Verb: "清理"},
	"flush":      {Key: "flush", Verb: "清理"},
	"purge":      {Key: "purge", Verb: "清理"},
	"reset":      {Key: "reset", Verb: "重置"},
	"rebuild":    {Key: "rebuild", Verb: "重建"},
	"reload":     {Key: "reload", Verb: "重载"},
	"recompile":  {Key: "recompile", Verb: "重新编译"},
	"recalculate": {Key: "recalculate", Verb: "重新计算"},

	// ── 发布 / 分发 ──
	"publish":     {Key: "publish", Verb: "发布"},
	"unpublish":   {Key: "unpublish", Verb: "取消发布"},
	"release":     {Key: "release", Verb: "发布"},
	"send":        {Key: "send", Verb: "发送"},
	"broadcast":   {Key: "broadcast", Verb: "广播"},
	"push":        {Key: "push", Verb: "推送"},
	"dispatch":    {Key: "dispatch", Verb: "派发"},
	"deliver":     {Key: "deliver", Verb: "投递"},
	"trigger":     {Key: "trigger", Verb: "触发"},
	"execute":     {Key: "execute", Verb: "执行"},
	"run":         {Key: "run", Verb: "运行"},
	"schedule":    {Key: "schedule", Verb: "调度"},
	"unschedule":  {Key: "unschedule", Verb: "取消调度"},
	"invoke":      {Key: "invoke", Verb: "调用"},

	// ── 分配 / 邀请 ──
	"assign":      {Key: "assign", Verb: "分配"},
	"unassign":    {Key: "unassign", Verb: "取消分配"},
	"grant":       {Key: "grant", Verb: "授权"},
	"deny":        {Key: "deny", Verb: "拒绝"},
	"invite":      {Key: "invite", Verb: "邀请"},
	"kick":        {Key: "kick", Verb: "移除"},
	"ban":         {Key: "ban", Verb: "封禁"},
	"unban":       {Key: "unban", Verb: "解封"},
	"link":        {Key: "link", Verb: "关联"},
	"unlink":      {Key: "unlink", Verb: "解除关联"},
	"bind":        {Key: "bind", Verb: "绑定"},
	"unbind":      {Key: "unbind", Verb: "解绑"},
	"join":        {Key: "join", Verb: "加入"},
	"leave":       {Key: "leave", Verb: "退出"},
	"subscribe":   {Key: "subscribe", Verb: "订阅"},
	"unsubscribe": {Key: "unsubscribe", Verb: "退订"},
	"follow":      {Key: "follow", Verb: "关注"},
	"unfollow":    {Key: "unfollow", Verb: "取消关注"},

	// ── 状态标记 ──
	"pin":        {Key: "pin", Verb: "置顶"},
	"unpin":      {Key: "unpin", Verb: "取消置顶"},
	"favorite":   {Key: "favorite", Verb: "收藏"},
	"unfavorite": {Key: "unfavorite", Verb: "取消收藏"},
	"star":       {Key: "star", Verb: "加星"},
	"unstar":     {Key: "unstar", Verb: "取消加星"},
	"flag":       {Key: "flag", Verb: "标记"},
	"unflag":     {Key: "unflag", Verb: "取消标记"},
	"tag":        {Key: "tag", Verb: "打标签"},
	"untag":      {Key: "untag", Verb: "取消标签"},

	// ── 校验 / 测试 ──
	"test":     {Key: "test", Verb: "测试"},
	"probe":    {Key: "probe", Verb: "探测"},
	"ping":     {Key: "ping", Verb: "探测"},
	"validate": {Key: "validate", Verb: "校验"},
	"apply":    {Key: "apply", Verb: "应用"},
	"simulate": {Key: "simulate", Verb: "模拟"},
	"dry_run":  {Key: "dry_run", Verb: "预演"},

	// ── 批量 ──
	"bulk_delete":   {Key: "bulk_delete", Verb: "批量删除"},
	"bulk_update":   {Key: "bulk_update", Verb: "批量更新"},
	"bulk_create":   {Key: "bulk_create", Verb: "批量创建"},
	"batch":         {Key: "batch", Verb: "批量操作"},
	"batch_delete":  {Key: "batch_delete", Verb: "批量删除"},
	"batch_update":  {Key: "batch_update", Verb: "批量更新"},
	"batch_create":  {Key: "batch_create", Verb: "批量创建"},
	"batch_revoke":  {Key: "batch_revoke", Verb: "批量吊销"},
	"delete_by_filter": {Key: "delete_by_filter", Verb: "按条件删除"},

	// ── 分享 / 公开 ──
	"share":   {Key: "share", Verb: "分享"},
	"unshare": {Key: "unshare", Verb: "取消分享"},

	// ─────────────────────────────────────────────
	// ── 以下为读动作（Read: true）—— severity 下调 ──
	// ─────────────────────────────────────────────
	"statistics":     {Key: "statistics", Verb: "查询统计", Read: true},
	"stats":          {Key: "statistics", Verb: "查询统计", Read: true},
	"summary":        {Key: "summary", Verb: "查询摘要", Read: true},
	"aggregate":      {Key: "aggregate", Verb: "聚合查询", Read: true},
	"list":           {Key: "list", Verb: "查询列表", Read: true},
	"query":          {Key: "query", Verb: "查询", Read: true},
	"lookup":         {Key: "lookup", Verb: "查询", Read: true},
	"search":         {Key: "search", Verb: "搜索", Read: true},
	"filter":         {Key: "filter", Verb: "筛选", Read: true},
	"history":        {Key: "history", Verb: "查询历史", Read: true},
	"report":         {Key: "report", Verb: "生成报表", Read: true},
	"metrics":        {Key: "metrics", Verb: "查询指标", Read: true},
	"snapshot":       {Key: "snapshot", Verb: "查询快照", Read: true},
	"dashboard":      {Key: "dashboard", Verb: "查看仪表盘", Read: true},
	"overview":       {Key: "overview", Verb: "查看概览", Read: true},
	"preview":        {Key: "preview", Verb: "预览", Read: true},
	"count":          {Key: "count", Verb: "计数查询", Read: true},
	"graph":          {Key: "graph", Verb: "查询关系图", Read: true},
	"matrix":         {Key: "matrix", Verb: "查询矩阵", Read: true},
	"impact_preview": {Key: "impact_preview", Verb: "预览影响", Read: true},
	"fetch":          {Key: "fetch", Verb: "获取", Read: true},
	"retrieve":       {Key: "retrieve", Verb: "获取", Read: true},
	"batch_get":      {Key: "batch_get", Verb: "批量查询", Read: true},
	"detail":         {Key: "detail", Verb: "查询详情", Read: true},
	"detect":         {Key: "detect", Verb: "检测", Read: true},
	"check":          {Key: "check", Verb: "检查", Read: true},
	"trend":          {Key: "trend", Verb: "查询趋势", Read: true},
	"ranking":        {Key: "ranking", Verb: "查询排名", Read: true},
	"top":            {Key: "top", Verb: "查询 Top 列表", Read: true},
	"leaderboard":    {Key: "leaderboard", Verb: "查询排行榜", Read: true},
	"feed":           {Key: "feed", Verb: "查询动态", Read: true},
	"tree":           {Key: "tree", Verb: "查询树形结构", Read: true},
	"options":        {Key: "options", Verb: "查询可选项", Read: true},
	"schema":         {Key: "schema", Verb: "查询模式", Read: true},
	"fields":         {Key: "fields", Verb: "查询字段", Read: true},
	"columns":        {Key: "columns", Verb: "查询字段", Read: true},
	"structure":      {Key: "structure", Verb: "查询结构", Read: true},
	"exists":         {Key: "exists", Verb: "检查是否存在", Read: true},
	"diff":           {Key: "diff", Verb: "对比差异", Read: true},
	"compare":        {Key: "compare", Verb: "对比", Read: true},
	"explain":        {Key: "explain", Verb: "解释", Read: true},
	"breakdown":      {Key: "breakdown", Verb: "查询分项", Read: true},
	"distribution":   {Key: "distribution", Verb: "查询分布", Read: true},
	"analyze":        {Key: "analyze", Verb: "分析", Read: true},
	"regions":        {Key: "regions", Verb: "查询地域分布", Read: true},
	"auth_sources":   {Key: "auth_sources", Verb: "查询认证来源", Read: true},
	"user_trend":     {Key: "user_trend", Verb: "查询用户趋势", Read: true},
	"status":         {Key: "status", Verb: "查询状态", Read: true},
	"health":         {Key: "health", Verb: "查询健康", Read: true},
	"available":      {Key: "available", Verb: "查询可用性", Read: true},
	"reachable":      {Key: "reachable", Verb: "查询连通性", Read: true},
	"catalog":        {Key: "catalog", Verb: "查询目录", Read: true},
	"suggestions":    {Key: "suggestions", Verb: "查询建议", Read: true},
	"autocomplete":   {Key: "autocomplete", Verb: "自动补全查询", Read: true},

	// 日志 / 事件 / 活动 / 链路 / 追踪类（常被设计成 POST + 复杂筛选 body）
	"logs":         {Key: "logs", Verb: "查询日志", Read: true},
	"log":          {Key: "log", Verb: "查询日志", Read: true},
	"events":       {Key: "events", Verb: "查询事件", Read: true},
	"event":        {Key: "event", Verb: "查询事件", Read: true},
	"activity":     {Key: "activity", Verb: "查询活动", Read: true},
	"activities":   {Key: "activities", Verb: "查询活动", Read: true},
	"timeline":     {Key: "timeline", Verb: "查询时间线", Read: true},
	"audit_trail":  {Key: "audit_trail", Verb: "查询审计链路", Read: true},
	"trace":        {Key: "trace", Verb: "查询链路", Read: true},
	"traces":       {Key: "traces", Verb: "查询链路", Read: true},
	"span":         {Key: "span", Verb: "查询 Span", Read: true},
	"spans":        {Key: "spans", Verb: "查询 Span", Read: true},
	"records":      {Key: "records", Verb: "查询记录", Read: true},
	"record":       {Key: "record", Verb: "查询记录", Read: true},
	"entries":      {Key: "entries", Verb: "查询条目", Read: true},
	"steps":        {Key: "steps", Verb: "查询步骤", Read: true},
	"nodes":        {Key: "nodes", Verb: "查询节点", Read: true},
	"runs":         {Key: "runs", Verb: "查询运行实例", Read: true},
	"executions":   {Key: "executions", Verb: "查询执行记录", Read: true},
	"sessions_active": {Key: "sessions_active", Verb: "查询活跃会话", Read: true},
	"online":       {Key: "online", Verb: "查询在线状态", Read: true},
	"pending":      {Key: "pending", Verb: "查询待处理", Read: true},
	"outbound":     {Key: "outbound", Verb: "查询出站", Read: true},
	"inbound":      {Key: "inbound", Verb: "查询入站", Read: true},
	"usage":        {Key: "usage", Verb: "查询使用情况", Read: true},
	"quota":        {Key: "quota", Verb: "查询配额", Read: true},
}

// resourceLabelMap 资源 key → 中文 label
//
//	summary 生成时，把机器 key 翻译成友好中文名
//	支持 "<domain>.<name>" 复合 key 的完整匹配 + 最后一段回退匹配
var resourceLabelMap = map[string]string{
	// ── 账号体系 ──
	"admins": "管理员", "admin": "管理员",
	"users": "用户", "user": "用户",
	"user-master": "用户主档", "user_master": "用户主档",
	"usermaster": "用户主档",

	// ── 应用 ──
	"apps": "应用", "app": "应用",
	"app.role-application": "角色申请", "app.role_application": "角色申请",
	"role-application": "角色申请", "role_application": "角色申请",
	"role-applications": "角色申请", "role_applications": "角色申请",

	// ── RBAC ──
	"roles": "角色", "role": "角色",
	"permissions": "权限", "permission": "权限",
	"assignments": "角色分配", "assignment": "角色分配",
	"policies": "策略", "policy": "策略",
	"custom-roles": "自定义角色", "custom_roles": "自定义角色",

	// ── 内容 ──
	"notifications": "通知", "notification": "通知",
	"announcements": "公告", "announcement": "公告",
	"templates": "模板", "template": "模板",
	"banners": "横幅", "banner": "横幅",
	"platform-banners": "平台横幅", "platform_banners": "平台横幅",

	// ── 存储 ──
	"storage":          "存储",
	"buckets":          "存储桶",
	"bucket":           "存储桶",
	"storage-config":   "存储配置",
	"storage_config":   "存储配置",
	"storage-resource": "存储资源",
	"storage_resource": "存储资源",
	"storage-resources": "存储资源",

	// ── 工作流 ──
	"workflows": "工作流", "workflow": "工作流",
	"approval":  "审批", "approvals": "审批",
	"tasks":     "任务", "task": "任务",
	"jobs":      "作业", "job": "作业",

	// ── 发布 ──
	"versions": "版本", "version": "版本",
	"releases": "发布", "release": "发布",

	// ── 平台设置 ──
	"settings":       "平台设置",
	"configuration":  "配置",
	"config":         "配置",
	"branding":       "品牌",
	"organizations":  "组织", "organization": "组织",
	"platform":       "平台",

	// ── 安全 ──
	"security":    "安全",
	"firewall":    "防火墙",
	"risk":        "风险",
	"risk-rules":  "风控规则",
	"risk_rules":  "风控规则",
	"captcha":     "验证码",
	"captcha-config": "验证码配置",
	"captcha_config": "验证码配置",
	"encryption":  "加密配置",
	"mfa":         "多因子",
	"ip-ban":      "IP 封禁", "ip_ban": "IP 封禁",
	"geo-ban":     "地理封禁", "geo_ban": "地理封禁",
	"webhook":     "Webhook", "webhooks": "Webhook",
	"api-key":     "API 密钥", "api_key": "API 密钥",

	// ── 审计 ──
	"audit":       "审计",
	"audit-logs":  "审计日志", "audit_logs": "审计日志",
	"audits":      "审计",
	"login-audits":   "登录审计", "login_audits": "登录审计",
	"session-audits": "会话审计", "session_audits": "会话审计",

	// ── 监控 ──
	"monitor":   "监控",
	"monitors":  "监控",
	"runtime":   "运行时",
	"system":    "系统",
	"crashlog":  "崩溃日志", "crash-log": "崩溃日志", "crash_log": "崩溃日志",
	"crashlogs": "崩溃日志",
	"memory":    "内存",
	"gc":        "GC",
	"dashboards": "仪表盘",
	"reports":   "报表",

	// ── 认证相关 ──
	"auth":     "认证",
	"login":    "登录",
	"logout":   "登出",
	"password": "密码",
	"oauth":    "OAuth",
	"oidc":     "OIDC",
	"ldap":     "LDAP",
	"saml":     "SAML",
	"sso":      "SSO",
	"sessions": "会话", "session": "会话",
	"tokens":   "令牌", "token": "令牌",

	// ── 业务功能 ──
	"email":         "邮件",
	"email-config":  "邮件配置", "email_config": "邮件配置",
	"payment":       "支付",
	"payment-config": "支付配置", "payment_config": "支付配置",
	"points":        "积分",
	"signin":        "签到",
	"auto-sign":     "自动签到", "auto_sign": "自动签到",
	"signin-reward": "签到奖励", "signin_reward": "签到奖励",
	"password-policy": "密码策略", "password_policy": "密码策略",
	"lottery":       "抽奖",
	"plugins":       "插件", "plugin": "插件",
	"realtime":      "实时",
	"websocket":     "WebSocket", "ws": "WebSocket",
	"device":        "设备",
	"device-marketing": "设备画像", "device_marketing": "设备画像",

	// ── 读动作段（作为资源末段回退标签时的友好翻译）──
	"statistics": "统计", "stats": "统计",
	"summary": "摘要", "metrics": "指标",
	"history": "历史", "overview": "概览",
	"snapshot": "快照", "impact-preview": "影响预览",

	// ── 其它 ──
	"import": "导入", "export": "导出",
	"freeze": "冻结", "unfreeze": "解冻",
	"disable": "禁用", "enable": "启用",
	"reset-password": "重置密码", "reset_password": "重置密码",
	"revoke": "吊销", "regenerate-key": "重新生成密钥",
	"tags":       "标签", "tag": "标签",
	"categories": "分类", "category": "分类",
	"feedback":   "反馈",
	"invitations": "邀请", "invitation": "邀请",
	"files":      "文件", "file": "文件",
	"resources":  "资源", "resource": "资源",
	"locations":  "地理位置", "location": "地理位置",
}

// categoryMap 路径段 → category 的扁平映射（classifyAuditCategory 查表用）
var categoryMap = map[string]string{
	"auth": "auth", "login": "auth", "logout": "auth", "mfa": "auth",
	"oauth": "auth", "oidc": "auth", "ldap": "auth", "saml": "auth", "sso": "auth",

	"admins": "admin", "admin": "admin",
	"users": "user", "user": "user", "user-master": "user", "usermaster": "user",

	"apps": "app", "app": "app",

	"roles": "rbac", "role": "rbac",
	"assignments": "rbac", "permissions": "rbac", "custom-roles": "rbac",

	"notifications": "content", "notification": "content",
	"announcements": "content", "announcement": "content",
	"templates": "content", "template": "content",
	"banners": "content", "banner": "content",

	"storage": "storage", "buckets": "storage", "bucket": "storage",
	"storage-resource": "storage", "storage-resources": "storage",
	"storage-config": "storage",

	"workflows": "workflow", "workflow": "workflow",
	"approval": "workflow", "approvals": "workflow",

	"versions": "release", "version": "release", "releases": "release",

	"settings": "settings", "configuration": "settings",
	"branding": "settings", "organizations": "settings", "organization": "settings",
	"platform": "settings",

	"security": "security", "firewall": "security",
	"risk": "security", "captcha": "security", "mfa-config": "security",
	"ip-ban": "security", "geo-ban": "security", "webhook": "security",

	"audit-logs": "audit", "audit": "audit", "audits": "audit",

	"monitor": "monitor", "runtime": "monitor", "system": "monitor",
	"crashlog": "monitor", "crash-log": "monitor",
	"memory": "monitor", "gc": "monitor", "reports": "monitor",

	"email": "email", "payment": "payment", "points": "points",
	"signin": "signin", "lottery": "lottery", "auto-sign": "signin",

	"plugins": "plugin", "plugin": "plugin",
	"realtime": "realtime", "websocket": "realtime", "ws": "realtime",
}

// actionVerb 根据 HTTP Method + path 尾段推断人类可读的中文动词
func actionVerb(method, lastSeg string) string {
	if meta, ok := lookupActionMeta(lastSeg); ok {
		return meta.Verb
	}
	// 回落到 HTTP method
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return "查询"
	case http.MethodPost:
		return "创建"
	case http.MethodPut:
		return "更新"
	case http.MethodPatch:
		return "修改"
	case http.MethodDelete:
		return "删除"
	case http.MethodHead, http.MethodOptions:
		return "查询"
	default:
		return "操作"
	}
}

// verbKey 机器可读动词 key（入 action 字符串）
func verbKey(method, lastSeg string) string {
	if meta, ok := lookupActionMeta(lastSeg); ok {
		return meta.Key
	}
	switch strings.ToUpper(method) {
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	case http.MethodGet:
		return "read"
	case http.MethodHead:
		return "head"
	case http.MethodOptions:
		return "options"
	}
	return "action"
}

// isActionVerbKey 末段本身是否已经是动作动词（拼 action key 时不重复）
func isActionVerbKey(s string) bool {
	_, ok := lookupActionMeta(s)
	return ok
}

// isReadLikeSegment 末段是否为"读类" POST（严重度降级判定）
func isReadLikeSegment(s string) bool {
	meta, ok := lookupActionMeta(s)
	return ok && meta.Read
}

// lookupActionMeta 统一查询入口，输入允许 kebab/snake/camel 混合
func lookupActionMeta(seg string) (actionMeta, bool) {
	key := normalizeActionKey(seg)
	meta, ok := actionVerbRegistry[key]
	return meta, ok
}

// normalizeActionKey 规范化动作段：小写 + kebab→snake
func normalizeActionKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))
}

// lastSegment 取路径的最后一段（跳过 `:param` 占位符）
func lastSegment(route string) string {
	segments := strings.Split(strings.Trim(route, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		s := strings.TrimSpace(segments[i])
		if s == "" || strings.HasPrefix(s, ":") {
			continue
		}
		return s
	}
	return ""
}

// ClassifyAuditCategoryForRoute 外部可调用版本（handler 侧记录审计时复用同一套分类）
func ClassifyAuditCategoryForRoute(route string) string {
	return classifyAuditCategory(route)
}

// InferAuditSeverityFor 外部可调用版本
//
//	相较内部 InferAuditSeverity 多一个 route 参数：用来识别"读类 POST"
//	（如 /statistics / /list / /search），避免把本质是查询的 POST 记为
//	和真正写操作同等级的 severity。
func InferAuditSeverityFor(category, method, route string, statusCode int, isSuperAdmin bool) string {
	return InferAuditSeverity(category, method, route, statusCode, isSuperAdmin)
}

// classifyAuditCategory 从路由模板推断业务域
//
//	例：
//	  /api/admin/users/:id                  → user
//	  /api/admin/settings/firewall          → settings
//	  /api/admin/system/monitor/runtime     → monitor
//	  /api/admin/app/role-application/list  → app
func classifyAuditCategory(route string) string {
	trimmed := strings.TrimPrefix(route, "/api/")
	trimmed = strings.TrimPrefix(trimmed, "admin/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "api"
	}
	segments := strings.Split(trimmed, "/")
	for _, s := range segments {
		if s == "" || strings.HasPrefix(s, ":") {
			continue
		}
		lower := strings.ToLower(s)
		if cat, ok := categoryMap[lower]; ok {
			return cat
		}
		// 首个非 :param 段兜底
		return lower
	}
	return "api"
}

// inferAuditAction 生成形如 app.role_application.statistics 的机器 action key
//
// 规则：category + middle segments + (resource | verb)
//
//	例：
//	  GET  /api/admin/users/:id                                          → user.read
//	  POST /api/admin/users/:id/freeze                                   → user.freeze
//	  POST /api/admin/app/role-application/statistics                    → app.role_application.statistics
//	  PUT  /api/admin/apps/:appkey/password-policy                       → app.password_policy.update
//	  POST /api/admin/app/role-application/batch-review                  → app.role_application.batch_review
//	  POST /api/admin/system/monitor/runtime/crashlog/:id/resolve        → monitor.monitor.runtime.crashlog.resolve
func inferAuditAction(method, route, category string) string {
	last := lastSegment(route)
	verb := verbKey(method, last)

	parts := make([]string, 0, 6)
	if category != "" && category != "api" {
		parts = append(parts, category)
	}
	for _, seg := range middleSegments(route, category, last) {
		parts = append(parts, seg)
	}
	if last != "" && !isActionVerbKey(last) && !stringSliceContains([]string{verb}, last) {
		normalized := normalizeActionKey(last)
		// 语义去重：路径最后一段与 category 的单/复数同义时（user vs users，role vs roles）
		// 不再作为资源段追加，避免 "user.users.delete" 这类双重命名
		if !equivalentEntity(normalized, category) {
			parts = append(parts, normalized)
		}
	}
	parts = append(parts, verb)
	return strings.Join(dedupeAdjacentStrings(parts), ".")
}

// equivalentEntity 判断两个英文实体词是否语义同一（覆盖单/复数、y↔ies 规则）
//
//	"user"     == "users"
//	"admin"    == "admins"
//	"app"      == "apps"
//	"category" == "categories"
//	"box"      == "boxes"
func equivalentEntity(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	// +s / +es
	if a+"s" == b || a+"es" == b {
		return true
	}
	if b+"s" == a || b+"es" == a {
		return true
	}
	// y ↔ ies（categories/category）
	if strings.HasSuffix(a, "ies") && strings.HasSuffix(b, "y") && a[:len(a)-3] == b[:len(b)-1] {
		return true
	}
	if strings.HasSuffix(b, "ies") && strings.HasSuffix(a, "y") && b[:len(b)-3] == a[:len(a)-1] {
		return true
	}
	return false
}

// middleSegments 提取 route 中 category 与 last 之间的"中间路径段"
//
//	规范化：去 :param、snake_case、空段去除；
//	与 category（lowercase 后）相同的首段会被跳过，避免 "app.apps" 类冗余。
func middleSegments(route, category, last string) []string {
	trimmed := strings.Trim(strings.TrimPrefix(strings.TrimPrefix(route, "/api/"), "admin/"), "/")
	if trimmed == "" {
		return nil
	}
	segments := strings.Split(trimmed, "/")
	clean := make([]string, 0, len(segments))
	for _, s := range segments {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, ":") {
			continue
		}
		clean = append(clean, s)
	}
	if len(clean) <= 1 {
		return nil
	}
	middle := clean[1:]
	if last != "" && strings.EqualFold(middle[len(middle)-1], last) {
		middle = middle[:len(middle)-1]
	}
	out := make([]string, 0, len(middle))
	lowerCategory := strings.ToLower(category)
	for _, s := range middle {
		normalized := normalizeActionKey(s)
		if normalized == "" {
			continue
		}
		// 去重：中间段与 category 语义同一（含单/复数）时跳过
		if equivalentEntity(normalized, lowerCategory) {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func dedupeAdjacentStrings(arr []string) []string {
	if len(arr) <= 1 {
		return arr
	}
	out := make([]string, 0, len(arr))
	for i, s := range arr {
		if i > 0 && arr[i-1] == s {
			continue
		}
		out = append(out, s)
	}
	return out
}

func stringSliceContains(arr []string, item string) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

// resourceLabel 把 resource key 翻译成中文 label
//
//	逐级回退：完整匹配（含 ".") → 末段匹配 → 原样返回
func resourceLabel(resource string) string {
	if resource == "" {
		return ""
	}
	if label, ok := resourceLabelMap[resource]; ok {
		return label
	}
	// 尝试 snake 形式
	snake := strings.ReplaceAll(resource, "-", "_")
	if label, ok := resourceLabelMap[snake]; ok {
		return label
	}
	// 末段匹配
	segments := strings.Split(resource, ".")
	last := segments[len(segments)-1]
	if label, ok := resourceLabelMap[last]; ok {
		return label
	}
	if lastSnake := strings.ReplaceAll(last, "-", "_"); lastSnake != last {
		if label, ok := resourceLabelMap[lastSnake]; ok {
			return label
		}
	}
	return resource
}

// BuildAuditSummary 生成一句话中文操作摘要
//
//	格式：<动词> <资源标签>[ <短 ID>][（失败 <status>）]
//
//	示例：
//	  "查询统计 角色申请"
//	  "冻结 用户 42"
//	  "更新 应用（失败 403）"
//	  "批量审批 角色申请 8 条"
func BuildAuditSummary(method, route, resource, resourceID string, statusCode int) string {
	last := lastSegment(route)
	verb := actionVerb(method, last)
	label := resourceLabel(resource)
	if label == "" {
		label = resourceLabel(last)
	}

	builder := strings.Builder{}
	builder.WriteString(verb)
	if label != "" && label != verb {
		builder.WriteString(" ")
		builder.WriteString(label)
	}
	if rid := shortResourceID(resourceID); rid != "" && rid != route {
		builder.WriteString(" ")
		builder.WriteString(rid)
	}
	if statusCode >= 400 {
		builder.WriteString(fmt.Sprintf("（失败 %d）", statusCode))
	}
	return builder.String()
}

// shortResourceID 把形如 `id=42` / `appid=7,userid=10` 的资源 ID 压缩为友好表达
//
//	id=42                → 42
//	appid=7              → appid=7
//	id=42,appid=7        → 42 · appid=7
func shortResourceID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, ",") {
		if strings.Contains(s, "=") {
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 && (parts[0] == "id" || parts[0] == "ID") {
				return strings.TrimSpace(parts[1])
			}
		}
		return s
	}
	// 多键合并：`id=42,appid=7` → `42 · appid=7`
	chunks := strings.Split(s, ",")
	out := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		if kv := strings.SplitN(chunk, "=", 2); len(kv) == 2 {
			if kv[0] == "id" || kv[0] == "ID" {
				out = append(out, strings.TrimSpace(kv[1]))
				continue
			}
		}
		out = append(out, chunk)
	}
	return strings.Join(out, " · ")
}

// InferAuditSeverity 根据业务域 + method + route 估算严重度
//
// 顺序：
//  1. 5xx / 401 / 403 → High（失败异常）
//  2. 读类 POST（/statistics、/list、/search 等）及 GET/HEAD/OPTIONS → Info
//  3. 按业务域 + 写方法精细判定
//
// 业务域覆盖：auth / admin / rbac / settings / security / user / app /
// content / storage / workflow / release / email / payment / points /
// signin / lottery / audit / monitor / plugin / realtime / api
func InferAuditSeverity(category, method, route string, statusCode int, isSuperAdmin bool) string {
	upper := strings.ToUpper(method)

	// 失败响应统一升级
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return systemdomain.AuditSeverityHigh
	}
	if statusCode >= 500 {
		return systemdomain.AuditSeverityHigh
	}

	// 纯读方法 / 读类 POST 一律 Info
	if upper == http.MethodGet || upper == http.MethodHead || upper == http.MethodOptions {
		return systemdomain.AuditSeverityInfo
	}
	if upper == http.MethodPost && isReadLikeSegment(lastSegment(route)) {
		return systemdomain.AuditSeverityInfo
	}

	switch category {
	case "auth":
		// 认证写操作（登录、刷新 token、MFA 验证等）——登录失败已在上面被 High 捕获
		if upper == http.MethodPost {
			return systemdomain.AuditSeverityMedium
		}
		return systemdomain.AuditSeverityInfo

	case "admin", "rbac":
		// 管理员增删改 / RBAC 变更：删除最高，写入区分超管
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityCritical
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if isSuperAdmin {
				return systemdomain.AuditSeverityHigh
			}
			return systemdomain.AuditSeverityMedium
		}
		return systemdomain.AuditSeverityInfo

	case "settings", "security":
		// 平台 / 安全设置：大刀阔斧的配置变更，同样需要高级别
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityCritical
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if isSuperAdmin {
				return systemdomain.AuditSeverityHigh
			}
			return systemdomain.AuditSeverityMedium
		}
		return systemdomain.AuditSeverityInfo

	case "user", "app":
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityHigh
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityMedium
		}

	case "content":
		// Banner / 公告 / 通知 / 模板：写入 low~medium
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityMedium
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityLow
		}

	case "storage":
		// 对象存储：上传 low，删除 medium/high（可能牵涉敏感数据）
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityHigh
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityLow
		}

	case "workflow":
		// 工作流触发 / 审批：medium；定义变更 high
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityHigh
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityMedium
		}

	case "release":
		// 版本发布：变更和发布 high，仅变更 low
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityHigh
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			if last := lastSegment(route); last == "publish" || last == "release" {
				return systemdomain.AuditSeverityHigh
			}
			return systemdomain.AuditSeverityMedium
		}

	case "monitor":
		// 监控：读占绝大多数（上面已拦截），剩下的写（resolve/ack/clear）统一 low
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityMedium
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityLow
		}
		return systemdomain.AuditSeverityInfo

	case "audit":
		// 审计本身：导出可能敏感，其它为 info
		if upper == http.MethodGet || upper == http.MethodHead {
			return systemdomain.AuditSeverityInfo
		}
		if last := lastSegment(route); last == "export" || last == "download" {
			return systemdomain.AuditSeverityLow
		}
		return systemdomain.AuditSeverityInfo

	case "plugin":
		// 插件启用 / 禁用 / 升级：medium~high
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityHigh
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityMedium
		}

	case "realtime":
		// WebSocket 管理：广播 low，断连 medium
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityMedium
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityLow
		}
		return systemdomain.AuditSeverityInfo

	case "email", "payment":
		// 邮件发送 / 支付创建：medium；配置变更 high
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityHigh
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityMedium
		}

	case "points", "signin", "lottery":
		// 运营功能：写入 low；删除 medium
		switch upper {
		case http.MethodDelete:
			return systemdomain.AuditSeverityMedium
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return systemdomain.AuditSeverityLow
		}
	}

	// 兜底
	switch upper {
	case http.MethodDelete:
		return systemdomain.AuditSeverityHigh
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return systemdomain.AuditSeverityLow
	}
	return systemdomain.AuditSeverityInfo
}
