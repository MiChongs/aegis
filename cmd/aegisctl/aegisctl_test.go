package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
)

// ─────────────────────────── env 文件 ───────────────────────────

const sampleTmpl = `# 头注释
AEGIS_REGISTRY=ghcr.io/michongs
#@ required
AEGIS_PG_USER=aegis
#@ secret 16
AEGIS_PG_PASSWORD=
#@ hex 8
RISK_INTERNAL_TOKEN=
#@ copy AEGIS_PG_PASSWORD
TEMPORAL_DB_PASSWORD=
AEGIS_API_REPLICAS=2
`

func TestParseEnvRoundTrip(t *testing.T) {
	in := "# 注释\n\nA=1\n  B = x\nC=has=equals and spaces \nD=\"quoted\"\nnot a kv\n"
	f, problems := ParseEnv(in)
	if len(problems) != 2 { // "  B = x" 键名带空格、"not a kv" 没有等号
		t.Fatalf("problems = %v", problems)
	}
	if v := f.Value("C"); v != "has=equals and spaces " {
		t.Fatalf("C = %q", v)
	}
	if f.String() != in {
		t.Fatalf("写回不一致：\n%q\n%q", f.String(), in)
	}
	if got := QuotedValueKeys(f); len(got) != 1 || got[0] != "D" {
		t.Fatalf("QuotedValueKeys = %v", got)
	}
	f.Set("A", "2")
	f.Set("E", "new")
	if !f.Unset("D") || f.Unset("D") {
		t.Fatal("Unset 结果不对")
	}
	if f.Value("A") != "2" || f.Value("E") != "new" {
		t.Fatal("Set 没生效")
	}
	if strings.Contains(f.String(), "quoted") {
		t.Fatal("Unset 后还在")
	}
}

func TestParseEnvDirectives(t *testing.T) {
	f, _ := ParseEnv(sampleTmpl)
	if d := f.DirectivesOf("AEGIS_PG_PASSWORD"); len(d) != 1 || d[0] != "secret 16" {
		t.Fatalf("directives = %v", d)
	}
	req := f.RequiredKeys()
	want := []string{"AEGIS_PG_USER", "AEGIS_PG_PASSWORD", "RISK_INTERNAL_TOKEN", "TEMPORAL_DB_PASSWORD"}
	if strings.Join(req, ",") != strings.Join(want, ",") {
		t.Fatalf("RequiredKeys = %v", req)
	}
}

func TestMergeFresh(t *testing.T) {
	tmpl, _ := ParseEnv(sampleTmpl)
	imported := map[string]string{"AEGIS_PG_USER": "zeabur", "GEOIP_UAPIS_KEY": "k1", "APP_ENV": "production"}
	out, rep, err := MergeClusterEnv(tmpl, nil, imported, []string{"APP_ENV", "GEOIP_UAPIS_KEY"}, rand.Reader, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Value("AEGIS_PG_USER") != "zeabur" {
		t.Fatal("导入值应当盖过模板默认值")
	}
	pw := out.Value("AEGIS_PG_PASSWORD")
	if len(pw) != 16 || strings.Trim(pw, alnum) != "" {
		t.Fatalf("生成的密码不对：%q", pw)
	}
	if tok := out.Value("RISK_INTERNAL_TOKEN"); len(tok) != 16 {
		t.Fatalf("hex 8 字节应是 16 个字符：%q", tok)
	}
	if out.Value("TEMPORAL_DB_PASSWORD") != pw {
		t.Fatal("copy 没生效")
	}
	if out.Value("AEGIS_REGISTRY") != "ghcr.io/michongs" {
		t.Fatal("模板默认值丢了")
	}
	if strings.Join(rep.Extra, ",") != "APP_ENV,GEOIP_UAPIS_KEY" {
		t.Fatalf("Extra = %v", rep.Extra)
	}
	if !strings.Contains(out.String(), "# 头注释") {
		t.Fatal("新建时应保留模板注释")
	}
	if len(rep.Generated) != 2 || len(rep.Copied) != 1 || len(rep.Imported) != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestMergeExisting(t *testing.T) {
	tmpl, _ := ParseEnv(sampleTmpl)
	existing, _ := ParseEnv("AEGIS_PG_USER=keep\nAEGIS_PG_PASSWORD=\nAEGIS_API_REPLICAS=5\nCUSTOM=1\n")
	imported := map[string]string{"AEGIS_PG_USER": "ignored", "AEGIS_PG_PASSWORD": "fromprod", "CUSTOM": "x"}
	out, rep, err := MergeClusterEnv(tmpl, existing, imported, []string{"CUSTOM"}, rand.Reader, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Value("AEGIS_PG_USER") != "keep" || out.Value("AEGIS_API_REPLICAS") != "5" || out.Value("CUSTOM") != "1" {
		t.Fatal("线上已有的非空值不能被覆盖")
	}
	if out.Value("AEGIS_PG_PASSWORD") != "fromprod" {
		t.Fatal("线上为空的键应取导入值")
	}
	if out.Value("TEMPORAL_DB_PASSWORD") != "fromprod" {
		t.Fatal("补上的 copy 键应取同文件的值")
	}
	if !strings.Contains(strings.Join(rep.Added, ","), "AEGIS_REGISTRY") || len(rep.Extra) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if !strings.HasPrefix(out.String(), "AEGIS_PG_USER=keep\n") {
		t.Fatal("原有行序应保持")
	}
	// 再合并一次不应有变化
	again, _, _ := MergeClusterEnv(tmpl, out, nil, nil, rand.Reader, "")
	if again.String() != out.String() {
		t.Fatal("合并不是幂等的")
	}
}

func TestMaskingNeverLeaks(t *testing.T) {
	secret := "SuperSecretValue123"
	for _, k := range []string{"AEGIS_PG_PASSWORD", "JWT_SECRET", "RISK_INTERNAL_TOKEN", "SESSION_TOKEN_KEY", "LEGACY_MYSQL_DSN", "HYDRA_PAIRWISE_SALT"} {
		if got := DisplayValue(k, secret, false); strings.Contains(got, secret) || strings.Contains(got, "Secret") {
			t.Fatalf("%s 泄露：%s", k, got)
		}
	}
	if got := DisplayValue("SOME_URL", "nats://u:"+secret+"@h:4222", false); strings.Contains(got, secret) {
		t.Fatalf("带凭据的 URL 泄露：%s", got)
	}
	if DisplayValue("AEGIS_API_REPLICAS", "2", false) != "2" {
		t.Fatal("非敏感值应原样显示")
	}
	if DisplayValue("JWT_SECRET", secret, true) != secret {
		t.Fatal("--reveal 应显示明文")
	}
	changes := DiffEnv(map[string]string{"A": secret, "B": "1"}, map[string]string{"A": secret + "x", "C": "2"})
	if len(changes) != 3 {
		t.Fatalf("changes = %+v", changes)
	}
	for _, c := range changes {
		if strings.Contains(c.From+c.To, "Secret") || (c.From != "" && len(c.From) != 8) {
			t.Fatalf("差异里不应有明文：%+v", c)
		}
	}
}

// ─────────────────────────── 插值与栈文件 ───────────────────────────

func TestInterpolate(t *testing.T) {
	env := map[string]string{"A": "a$b", "EMPTY": ""}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	cases := []struct{ in, escaped, plain string }{
		{"x: ${A}", "x: a$$b", "x: a$b"},
		{"x: $A/y", "x: a$$b/y", "x: a$b/y"},
		{"x: $$HOME", "x: $$HOME", "x: $HOME"},
		{"x: ${EMPTY:-d}", "x: d", "x: d"},
		{"x: ${EMPTY-d}", "x: ", "x: "},
		{"x: ${NOPE-d}", "x: d", "x: d"},
		{"x: '{{.Task.Slot}}'", "x: '{{.Task.Slot}}'", "x: '{{.Task.Slot}}'"},
		{"# ${MISSING:?不渲染注释}", "# ${MISSING:?不渲染注释}", "# ${MISSING:?不渲染注释}"},
	}
	for _, c := range cases {
		got, errs := Interpolate(c.in, lookup, true)
		if len(errs) > 0 || got != c.escaped {
			t.Errorf("escape %q → %q %v，期望 %q", c.in, got, errs, c.escaped)
		}
		got, _ = Interpolate(c.in, lookup, false)
		if got != c.plain {
			t.Errorf("plain %q → %q，期望 %q", c.in, got, c.plain)
		}
	}
	_, errs := Interpolate("a: ${MISSING:?要填}\nb: ${EMPTY:?}\nc: ${EMPTY?}\nd: $ x\n", lookup, true)
	if len(errs) != 3 {
		t.Fatalf("errs = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "第 1 行") || !strings.Contains(errs[0].Error(), "MISSING：要填") {
		t.Fatalf("错误信息 = %v", errs[0])
	}
}

func TestReferencedVars(t *testing.T) {
	refs := ReferencedVars("a: ${X:?}\nb: ${Y:-1}\n# ${Z:?}\nc: $${NOT}\nd: ${X}\n")
	got := fmt.Sprint(refs)
	if got != "[{X true } {Y false 1}]" {
		t.Fatalf("refs = %s", got)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, stackRelPath)); err != nil {
		t.Skip("不在仓库里运行")
	}
	return root
}

func fakeClusterEnv(t *testing.T, root string) map[string]string {
	t.Helper()
	text, err := readRepoFile(root, envTmplRelPath)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, problems := ParseEnv(text)
	if len(problems) > 0 {
		t.Fatalf("cluster.env.example 有问题：%v", problems)
	}
	out, _, err := MergeClusterEnv(tmpl, nil, nil, nil, rand.Reader, "")
	if err != nil {
		t.Fatal(err)
	}
	return out.Map()
}

func allTags(tag string) map[string]string {
	vars := map[string]string{migrateTagVar: tag}
	for _, im := range Images {
		vars[im.TagVar] = tag
	}
	return vars
}

func TestRealStackRenders(t *testing.T) {
	root := repoRoot(t)
	stack, err := readRepoFile(root, stackRelPath)
	if err != nil {
		t.Fatal(err)
	}
	env := fakeClusterEnv(t, root)
	names, err := StackServices(stack)
	if err != nil {
		t.Fatal(err)
	}
	// 目录清单与栈文件对得上：每个服务要么在某个镜像里，要么是第三方镜像；分组里的服务都在栈里
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	for g, list := range serviceGroups {
		for _, s := range list {
			if !known[s] {
				t.Errorf("分组 %s 里的 %s 不在栈文件里", g, s)
			}
		}
	}
	for _, im := range Images {
		for _, s := range im.Services {
			if !known[s] {
				t.Errorf("镜像 %s 的服务 %s 不在栈文件里", im.Name, s)
			}
		}
	}
	for s := range scalable {
		if !known[s] {
			t.Errorf("可扩缩的 %s 不在栈文件里", s)
		}
	}

	out, err := renderStack(stack, env, allTags("sha-1234567"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") && strings.Contains(strings.ReplaceAll(l, "$$", ""), "${") {
			t.Fatalf("第 %d 行还有未展开的变量：%s", i+1, l)
		}
	}
	for _, want := range []string{
		"ghcr.io/michongs/aegis-server:sha-1234567",
		`name: "aegis-api-data-{{.Task.Slot}}"`,
		`AEGIS_NODE_ID: "{{.Node.Hostname}}-api-{{.Task.Slot}}"`,
		`AEGIS_NODE_ID: "{{.Node.Hostname}}-worker-{{.Task.Slot}}"`,
		"wget -q -O /dev/null http://127.0.0.1:8089/readyz",
		"$$REDIS_PASSWORD", // docker 转义保留
		"mode: replicated-job",
		"order: start-first",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果里没有 %q", want)
		}
	}
	// 部署时交给 docker 的是未渲染的原文（docker 先解析 YAML 再插值），原文必须是合法 YAML
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(stack), &doc); err != nil {
		t.Fatalf("栈文件不是合法 YAML：%v", err)
	}
	services := doc["services"].(map[string]any)
	if len(services) != len(names) {
		t.Fatalf("YAML 里 %d 个服务，切分得到 %d 个", len(services), len(names))
	}
	for name, raw := range services {
		svc := raw.(map[string]any)
		for _, banned := range []string{"depends_on", "container_name", "build", "shm_size", "security_opt", "restart"} {
			if _, ok := svc[banned]; ok {
				t.Errorf("%s 用了 Swarm 不支持的 %s", name, banned)
			}
		}
		if _, ok := svc["deploy"]; !ok {
			t.Errorf("%s 没有 deploy 段", name)
		}
	}

	// 缺必填变量时报错
	delete(env, "AEGIS_PG_PASSWORD")
	if _, err := renderStack(stack, env, allTags("t"), nil); err == nil || !strings.Contains(err.Error(), "AEGIS_PG_PASSWORD") {
		t.Fatalf("应当报缺 AEGIS_PG_PASSWORD：%v", err)
	}
}

func TestSelectServices(t *testing.T) {
	root := repoRoot(t)
	stack, _ := readRepoFile(root, stackRelPath)
	sub, err := SelectServices(stack, map[string]bool{"aegis-migrate": true})
	if err != nil {
		t.Fatal(err)
	}
	names, _ := StackServices(sub)
	if len(names) != 1 || names[0] != "aegis-migrate" {
		t.Fatalf("names = %v", names)
	}
	if !strings.Contains(sub, "x-aegis-env: &aegis-env") || !strings.Contains(sub, "\nnetworks:\n") || !strings.Contains(sub, "aegis-worker-data:") {
		t.Fatal("子集要保留锚点与 networks / volumes")
	}
	env := fakeClusterEnv(t, root)
	if _, err := renderStack(stack, env, map[string]string{migrateTagVar: "t"}, map[string]bool{"aegis-migrate": true}); err != nil {
		t.Fatalf("只渲染迁移任务时不应要求其他镜像标签：%v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(sub), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectServices(stack, map[string]bool{"nope": true}); err == nil {
		t.Fatal("没有选中任何服务时应报错")
	}
}

func TestExpandServices(t *testing.T) {
	known := []string{"aegis-api", "aegis-worker", "aegis-migrate", "aegis-console", "postgres"}
	set, err := ExpandServices("aegis-api,aegis-console", known)
	if err != nil {
		t.Fatal(err)
	}
	if !set["aegis-migrate"] || !set["aegis-console"] || set["aegis-worker"] {
		t.Fatalf("set = %v", set)
	}
	if _, err := ExpandServices("nope", known); err == nil {
		t.Fatal("未知服务应报错")
	}
	set, _ = ExpandServices("app", known)
	if len(set) != 3 {
		t.Fatalf("app 组 = %v", set)
	}
}

// ─────────────────────────── Traefik ───────────────────────────

func TestRenderTraefik(t *testing.T) {
	root := repoRoot(t)
	tmpl, err := readRepoFile(root, traefikRelPath)
	if err != nil {
		t.Fatal(err)
	}
	env := fakeClusterEnv(t, root)
	out, err := RenderTraefik(tmpl, env)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		HTTP struct {
			Routers map[string]struct {
				Rule        string   `yaml:"rule"`
				EntryPoints []string `yaml:"entryPoints"`
				Service     string   `yaml:"service"`
				Priority    int      `yaml:"priority"`
				Middlewares []string `yaml:"middlewares"`
				TLS         *struct {
					CertResolver string `yaml:"certResolver"`
				} `yaml:"tls"`
			} `yaml:"routers"`
			Services map[string]struct {
				LoadBalancer struct {
					Servers []struct {
						URL string `yaml:"url"`
					} `yaml:"servers"`
				} `yaml:"loadBalancer"`
			} `yaml:"services"`
		} `yaml:"http"`
		EntryPoints any `yaml:"entryPoints"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("不是合法 YAML：%v\n%s", err, out)
	}
	if doc.EntryPoints != nil {
		t.Fatal("不能写 entryPoints")
	}
	api := doc.HTTP.Routers["aegis-api"]
	if api.Rule != "Host(`aegis.karpov.cn`) || Host(`system.karpov.cn`) || Host(`aegis-origin.karpov.cn`)" {
		t.Fatalf("rule = %q", api.Rule)
	}
	if api.TLS == nil || api.TLS.CertResolver != "letsencrypt" || api.Priority != 1000 || api.EntryPoints[0] != "websecure" {
		t.Fatalf("api 路由 = %+v", api)
	}
	if web := doc.HTTP.Routers["aegis-api-web"]; len(web.Middlewares) != 1 || web.Middlewares[0] != "redirect-to-https@file" {
		t.Fatalf("明文路由 = %+v", web)
	}
	if u := doc.HTTP.Services["aegis-api"].LoadBalancer.Servers[0].URL; u != "http://aegis_aegis-api:8088" {
		t.Fatalf("后端 = %s", u)
	}
	// 一起听挂在 API 域名的 /listen-together 下（与现网 Dokploy 一致），优先级要压过 API 的整域名路由
	lt := doc.HTTP.Routers["aegis-listen-together-path"]
	if !strings.Contains(lt.Rule, "PathPrefix(`/listen-together`)") || !strings.Contains(lt.Rule, "Host(`system.karpov.cn`)") ||
		lt.Priority <= api.Priority || lt.Service != "aegis-listen-together-path" || len(lt.Middlewares) != 1 || lt.Middlewares[0] != "aegis-listen-together-strip" {
		t.Fatalf("一起听路径路由 = %+v", lt)
	}
	if !strings.Contains(out, "prefixes: [\"/listen-together\"]") {
		t.Fatalf("缺少去前缀中间件：\n%s", out)
	}
	if u := doc.HTTP.Services["aegis-listen-together-path"].LoadBalancer.Servers[0].URL; u != "http://aegis_listen-together:8090" {
		t.Fatalf("一起听后端 = %s", u)
	}
	if u := doc.HTTP.Services["aegis-console"].LoadBalancer.Servers[0].URL; u != "http://aegis_aegis-console:3000" {
		t.Fatalf("控制台后端 = %s", u)
	}
	if _, ok := doc.HTTP.Routers["aegis-listen-together"]; ok {
		t.Fatal("没配域名的组不应生成路由")
	}

	env["AEGIS_TRAEFIK_HTTP_MODE"] = "off"
	env["AEGIS_TRAEFIK_CERT_RESOLVER"] = ""
	env["AEGIS_TEMPORAL_UI_DOMAINS"] = "temporal.example.com"
	env["AEGIS_TEMPORAL_UI_MIDDLEWARES"] = "auth@file"
	out, err = RenderTraefik(tmpl, env)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("不是合法 YAML：%v\n%s", err, out)
	}
	if _, ok := doc.HTTP.Routers["aegis-api-web"]; ok {
		t.Fatal("off 时不应有明文路由")
	}
	if r := doc.HTTP.Routers["aegis-temporal-ui"]; len(r.Middlewares) != 1 || r.Middlewares[0] != "auth@file" {
		t.Fatalf("temporal-ui 路由 = %+v", r)
	}

	env["AEGIS_API_DOMAINS"] = "bad domain`.com"
	if _, err := RenderTraefik(tmpl, env); err == nil {
		t.Fatal("非法域名应报错")
	}
}

// ─────────────────────────── docker 输出解析 ───────────────────────────

const sampleServiceLS = `{"ID":"abc","Image":"ghcr.io/michongs/aegis-server:sha-1111111","Mode":"replicated","Name":"aegis_aegis-api","Ports":"","Replicas":"2/2 (max 3 per node)"}
{"ID":"def","Image":"ghcr.io/michongs/aegis-server:sha-1111111","Mode":"replicated job","Name":"aegis_aegis-migrate","Ports":"","Replicas":"0/1 (1/1 completed)"}
{"ID":"ghi","Image":"bryanlin16899/postgres-vector-postgis:17.4@sha256:deadbeef","Mode":"replicated","Name":"aegis_postgres","Ports":"","Replicas":"0/1"}

`

func TestParseServiceLS(t *testing.T) {
	rows, err := ParseJSONLines[ServiceRow](sampleServiceLS)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %v %v", rows, err)
	}
	r := ParseReplicas(rows[0].Replicas)
	if !r.Valid || r.Running != 2 || r.Desired != 2 || r.Job {
		t.Fatalf("replicas = %+v", r)
	}
	j := ParseReplicas(rows[1].Replicas)
	if !j.Job || j.Completed != 1 || j.Total != 1 || j.Running != 0 {
		t.Fatalf("job = %+v", j)
	}
	if repo, tag := SplitImage(rows[2].Image); repo != "bryanlin16899/postgres-vector-postgis" || tag != "17.4" {
		t.Fatalf("SplitImage = %s %s", repo, tag)
	}
	if _, tag := SplitImage("localhost:5000/aegis-server"); tag != "latest" {
		t.Fatalf("带端口的仓库 tag = %s", tag)
	}
	if ShortService("aegis", rows[0].Name) != "aegis-api" {
		t.Fatal("ShortService")
	}
	if _, err := ParseJSONLines[ServiceRow]("{bad"); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}

func TestParseStateAndTags(t *testing.T) {
	out := "@@NOW\n1700000000\n@@LS\n" + sampleServiceLS + "@@INSPECT\n" +
		`{"name":"aegis_aegis-api","image":"ghcr.io/michongs/aegis-server:sha-1111111@sha256:aa","labels":{},"mode":{"Replicated":{"Replicas":2}},"update":{"State":"completed","StartedAt":"2026-11-17T01:00:00Z","CompletedAt":"2026-11-17T01:01:00Z","Message":"update completed"},"job":null,"updatedAt":"2026-11-17T01:00:00Z"}` + "\n" +
		`{"name":"aegis_aegis-migrate","image":"ghcr.io/michongs/aegis-server:sha-0000000","labels":{},"mode":{"ReplicatedJob":{"MaxConcurrent":1,"TotalCompletions":1}},"update":null,"job":{"JobIteration":{"Index":42},"LastExecution":"2026-11-17T01:00:00Z"},"updatedAt":"2026-11-17T01:00:00Z"}` + "\n"
	st, err := parseState("aegis", out)
	if err != nil {
		t.Fatal(err)
	}
	if st.now.Unix() != 1700000000 || len(st.rows) != 3 || len(st.infos) != 2 {
		t.Fatalf("state = %+v", st)
	}
	if !st.infos["aegis-migrate"].IsJob() || jobIteration(st, "aegis-migrate") != 42 || jobIteration(st, "nope") != -1 {
		t.Fatal("job 解析不对")
	}
	tags := CurrentTags(st)
	if tags["AEGIS_TAG"] != "sha-1111111" || tags[migrateTagVar] != "sha-0000000" {
		t.Fatalf("tags = %v", tags)
	}
}

func TestPlanTagsUsesPinnedTagsOnFirstDeploy(t *testing.T) {
	// 第一次部署：栈不存在，没改动的镜像用 cluster.env 钉住的标签
	plan, err := PlanTags(map[string]bool{"music-rec": true, "aegis-api": true}, "new", map[string]string{},
		map[string]string{"AEGIS_MUSIC_TAG": "dokploy-1"}, func(n string) bool { return n == "aegis-server" })
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tags["AEGIS_MUSIC_TAG"] != "dokploy-1" || plan.Tags["AEGIS_TAG"] != "new" {
		t.Fatalf("plan = %+v", plan.Tags)
	}
}

func TestPlanTags(t *testing.T) {
	sel := map[string]bool{"aegis-api": true, "aegis-console": true}
	current := map[string]string{"AEGIS_TAG": "old", "AEGIS_CONSOLE_TAG": "c-old", "AEGIS_RISK_TAG": "r-old"}
	plan, err := PlanTags(sel, "new", current, nil, func(n string) bool { return n == "aegis-server" })
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tags["AEGIS_TAG"] != "new" || plan.Tags[migrateTagVar] != "new" {
		t.Fatalf("server = %v", plan.Tags)
	}
	if plan.Tags["AEGIS_CONSOLE_TAG"] != "c-old" || len(plan.Fallback) != 1 {
		t.Fatalf("控制台没有新标签时应沿用现网：%v %v", plan.Tags, plan.Fallback)
	}
	if plan.Tags["AEGIS_RISK_TAG"] != "r-old" || plan.Tags["AEGIS_MUSIC_TAG"] != "new" {
		t.Fatalf("未选中的镜像：%v", plan.Tags)
	}
	if _, err := PlanTags(map[string]bool{"music-rec": true}, "new", current, nil, func(string) bool { return false }); err == nil {
		t.Fatal("没有新标签也没有现网版本时应报错")
	}
}

func TestEvaluateConvergence(t *testing.T) {
	since := time.Date(2026, 11, 17, 1, 0, 0, 0, time.UTC)
	mk := func(state string, started time.Time) ServiceInfo {
		var info ServiceInfo
		info.Name = "aegis_aegis-api"
		info.Update = &struct {
			State       string    `json:"State"`
			StartedAt   time.Time `json:"StartedAt"`
			CompletedAt time.Time `json:"CompletedAt"`
			Message     string    `json:"Message"`
		}{State: state, StartedAt: started}
		return info
	}
	ok := ParseReplicas("2/2")
	part := ParseReplicas("1/2")
	if c := EvaluateConvergence(mk("updating", since.Add(time.Second)), ok, since); c.Done {
		t.Fatal("updating 不算完成")
	}
	if c := EvaluateConvergence(mk("completed", since.Add(time.Second)), part, since); c.Done {
		t.Fatal("副本不齐不算完成")
	}
	if c := EvaluateConvergence(mk("completed", since.Add(time.Second)), ok, since); !c.Done || c.Failed {
		t.Fatal("应当完成")
	}
	if c := EvaluateConvergence(mk("rollback_completed", since.Add(time.Second)), ok, since); !c.Failed {
		t.Fatal("自动回滚算失败")
	}
	if c := EvaluateConvergence(mk("paused", since.Add(-time.Hour)), ok, since); c.Failed || !c.Done {
		t.Fatal("本次之前的暂停状态不算本次失败")
	}
	if c := EvaluateConvergence(ServiceInfo{}, ok, since); !c.Done {
		t.Fatal("从没更新过、副本齐全算完成")
	}
}

func TestEvaluateJob(t *testing.T) {
	mk := func(iter int64, state string, sec int) TaskInfo {
		t := TaskInfo{State: state, Timestamp: time.Unix(int64(sec), 0), Err: "exit " + state}
		t.JobIteration = &struct {
			Index int64 `json:"Index"`
		}{iter}
		return t
	}
	tasks := []TaskInfo{mk(1, "complete", 1), mk(2, "failed", 2), mk(2, "running", 3)}
	if o := EvaluateJob(tasks, 2, 3); o.Done || o.Failed != 1 || o.Running != 1 {
		t.Fatalf("o = %+v", o)
	}
	tasks = append(tasks, mk(2, "failed", 4), mk(2, "failed", 5))
	tasks[2].State = "shutdown"
	if o := EvaluateJob(tasks, 2, 3); !o.Done || o.Succeeded || o.Failed != 3 || o.LastError != "exit failed" {
		t.Fatalf("o = %+v", o)
	}
	if o := EvaluateJob(tasks, 1, 3); !o.Succeeded {
		t.Fatal("上一轮成功")
	}
	parsed, err := ParseJSONLines[TaskInfo](`{"id":"t1","service":"s","node":"n","slot":1,"desired":"complete","state":"complete","message":"finished","err":"","ts":"2026-11-17T01:00:00Z","container":"c","job":{"Index":7}}`)
	if err != nil || parsed[0].JobIteration.Index != 7 {
		t.Fatalf("parsed = %+v %v", parsed, err)
	}
}

func TestNodesAndTasks(t *testing.T) {
	out := "@@SELF\nn1\n@@NODES\n" +
		`{"Availability":"Active","EngineVersion":"28.0","Hostname":"hk-1","ID":"n1","ManagerStatus":"Leader","Self":true,"Status":"Ready"}` + "\n" +
		`{"Availability":"Drain","EngineVersion":"28.0","Hostname":"hk-2","ID":"n2","ManagerStatus":"","Self":"false","Status":"Ready"}` + "\n" +
		"@@INSPECT\n" +
		`{"ID":"n1","Spec":{"Labels":{"aegis.db":"true","aegis.app":"true","other":"x"},"Role":"manager","Availability":"active"},"Description":{"Hostname":"hk-1"},"Status":{"State":"ready","Addr":"10.0.0.1"},"ManagerStatus":{"Leader":true,"Addr":"10.0.0.1:2377"}}` + "\n" +
		`{"ID":"n2","Spec":{"Labels":{"aegis.app":"true","aegis.db":"true"},"Role":"worker","Availability":"drain"},"Description":{"Hostname":"hk-2"},"Status":{"State":"ready","Addr":"10.0.0.2"}}` + "\n"
	ns, err := parseNodes(out)
	if err != nil {
		t.Fatal(err)
	}
	if ns.selfHostname() != "hk-1" || ns.byName("hk-2").ID != "n2" || len(ns.rows) != 2 {
		t.Fatalf("nodes = %+v", ns)
	}
	if l := AegisLabels(ns.infos[0].Spec.Labels); strings.Join(l, ",") != "aegis.app=true,aegis.db=true" {
		t.Fatalf("labels = %v", l)
	}
	cs := CheckLabels(ns.infos)
	var fails, warns int
	for _, c := range cs {
		switch c.Level {
		case "fail":
			fails++
		case "warn":
			warns++
		}
	}
	if fails != 3 || warns != 1 { // storage / risk / music 没有节点，db 在两个节点上
		t.Fatalf("checks = %+v", cs)
	}
	tasks, _ := ParseJSONLines[TaskRow](`{"ID":"t1","Name":"aegis_aegis-api.1","Node":"hk-1","DesiredState":"Running","CurrentState":"Running 2 hours ago"}
{"ID":"t2","Name":"aegis_aegis-api.2","Node":"hk-2","DesiredState":"Running","CurrentState":"Running 1 hour ago"}
{"ID":"t3","Name":"aegis_postgres.1","Node":"hk-1","DesiredState":"Running","CurrentState":"Preparing 3 seconds ago"}`)
	per := TasksPerNode(tasks)
	if per["hk-1"] != 1 || per["hk-2"] != 1 {
		t.Fatalf("per = %v", per)
	}
	dist := ServicesPerNode("aegis", tasks)
	if dist["aegis-api"]["hk-2"] != 1 || len(dist["postgres"]) != 0 {
		t.Fatalf("dist = %v", dist)
	}
	table := renderNodeTable(ns, per, Palette{})
	if !strings.Contains(table, "hk-1 *") || !strings.Contains(table, "manager（leader）") {
		t.Fatalf("表格：\n%s", table)
	}
}

func TestStatusRender(t *testing.T) {
	st, _ := parseState("aegis", "@@LS\n"+sampleServiceLS+"@@INSPECT\n"+
		`{"name":"aegis_aegis-api","image":"r/aegis-server:sha-1","mode":{"Replicated":{"Replicas":2}},"update":{"State":"updating","StartedAt":"2026-11-17T01:00:00Z"}}`+"\n"+
		`{"name":"aegis_postgres","image":"pg:17","mode":{"Replicated":{"Replicas":1}}}`+"\n")
	m := StatusModel{Stack: "aegis", State: st, At: time.Now(), Probes: ParseHealthProbes("api 10.0.0.5 200\napi 10.0.0.6 503\nworker 10.0.0.7 200\n")}
	out := RenderStatus(m, Palette{})
	for _, want := range []string{"aegis-api", "2/2", "sha-1", "更新中", "postgres", "0/1", "api      1/2 就绪", "worker   1/1 就绪", "api 10.0.0.6 返回 503"} {
		if !strings.Contains(out, want) {
			t.Errorf("总览里没有 %q：\n%s", want, out)
		}
	}
	colored := RenderStatus(m, Palette{On: true})
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("开着颜色时应着色")
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatal("关着颜色时不应有控制码")
	}
}

func TestSections(t *testing.T) {
	s := Sections("ignored\n@@A\nx\ny\n@@B\n@@C\nz\n")
	if s["A"] != "x\ny\n" || s["B"] != "" || s["C"] != "z\n" {
		t.Fatalf("sections = %q", s)
	}
}

// ─────────────────────────── 节点、标签、参数 ───────────────────────────

func TestLabelsAndJoin(t *testing.T) {
	add, rm, err := ParseLabels([]string{"aegis.app=true,aegis.db", "aegis.risk-"})
	if err != nil {
		t.Fatal(err)
	}
	if add["aegis.db"] != "true" || len(rm) != 1 || rm[0] != "aegis.risk" {
		t.Fatalf("add=%v rm=%v", add, rm)
	}
	if got := strings.Join(LabelArgs(add, rm), " "); got != "--label-add aegis.app=true --label-add aegis.db=true --label-rm aegis.risk" {
		t.Fatalf("args = %s", got)
	}
	if _, _, err := ParseLabels([]string{"bad key=1"}); err == nil {
		t.Fatal("非法标签名应报错")
	}
	if _, _, err := ParseLabels([]string{"k=a'b"}); err == nil {
		t.Fatal("标签值含引号应报错")
	}
	s := JoinScript("10.0.0.1:2377", "192.168.1.5")
	if !strings.HasPrefix(s, "read -r tok\n") || !strings.Contains(s, `--token "$tok"`) || !strings.Contains(s, "--advertise-addr 192.168.1.5 10.0.0.1:2377") {
		t.Fatalf("join 脚本：%s", s)
	}
	if strings.Contains(JoinScript("10.0.0.1:2377", ""), "advertise") {
		t.Fatal("没有通告地址时不加参数")
	}
	if JoinAddr("10.0.0.1") != "10.0.0.1:2377" || JoinAddr("fd00::1") != "[fd00::1]:2377" {
		t.Fatal("JoinAddr")
	}
}

func TestArgs(t *testing.T) {
	g, rest, err := extractGlobal([]string{"node", "join", "--manager=m1", "host", "--role", "worker", "--no-color", "--", "--manager", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if g.manager != "m1" || !g.noColor || strings.Join(rest, " ") != "node join host --role worker -- --manager x" {
		t.Fatalf("g=%+v rest=%v", g, rest)
	}
	fs := newFlagSet("t")
	role := fs.String("role", "", "")
	pos, err := parseFlags(fs, []string{"host", "--role", "manager", "extra"})
	if err != nil || *role != "manager" || strings.Join(pos, ",") != "host,extra" {
		t.Fatalf("pos=%v role=%s err=%v", pos, *role, err)
	}
	pos, err = parseFlags(newFlagSet("exec"), []string{"aegis-api", "--", "ls", "-la"})
	if err != nil || strings.Join(pos, " ") != "aegis-api -- ls -la" {
		t.Fatalf("exec pos=%v err=%v", pos, err)
	}
	vals, stdin, err := ParseAssignments([]string{"A=1", "B=x=y", "C", "-"})
	if err != nil || vals["B"] != "x=y" || len(stdin) != 1 || stdin[0] != "C" {
		t.Fatalf("vals=%v stdin=%v err=%v", vals, stdin, err)
	}
	if _, _, err := ParseAssignments([]string{"bad"}); err == nil {
		t.Fatal("没有等号应报错")
	}
	sc, err := ParseScale([]string{"aegis-api=4"})
	if err != nil || sc["aegis-api"] != 4 {
		t.Fatal("ParseScale")
	}
	if _, err := ParseScale([]string{"postgres=2"}); err == nil {
		t.Fatal("有状态服务不能扩缩")
	}
	if ShellQuote("a b'c") != `'a b'\''c'` || ShellQuote("plain-1.2") != "plain-1.2" || ShellQuote("") != "''" {
		t.Fatal("ShellQuote")
	}
}

func TestImport(t *testing.T) {
	services := []runningUnit{
		{Name: "aegis-api-a9k2vs", Env: []string{
			"POSTGRES_DSN=postgres://zeabur:pgpw@aegis-postgres:5432/zeabur_db?sslmode=disable&pool_max_conns=40",
			"NATS_URL=nats://zeabur:natspw@aegis-nats:4222", "REDIS_ADDR=aegis-redis:6379", "REDIS_PASSWORD=rpw",
			"JWT_SECRET=jwt", "TRUSTED_PROXIES=infra,95.82.8.0/23", "RISK_INTERNAL_TOKEN=rt",
		}, Mounts: []mountInfo{{Type: "bind", Source: "/srv/aegis/aegis-api/data", Target: "/app/data"}}},
		{Name: "listen-together-x1", Env: []string{"LT_INTERNAL_TOKEN=lt", "LT_ROOM_CAPACITY=50", "LT_REDIS_URL=redis://x"}},
		{Name: "aegis_aegis-api", Env: []string{"JWT_SECRET=from-new-stack"}},
	}
	alias := func(a string) map[string]struct {
		Aliases []string `json:"Aliases"`
	} {
		return map[string]struct {
			Aliases []string `json:"Aliases"`
		}{"dokploy-network": {Aliases: []string{a}}}
	}
	containers := []runningUnit{
		{Name: "/pg", Image: "bryanlin16899/postgres-vector-postgis:17.4", Env: []string{"POSTGRES_USER=other"}, Networks: alias("aegis-postgres"),
			Mounts: []mountInfo{{Type: "bind", Source: "/srv/aegis/pgdata", Destination: "/var/lib/postgresql/data"}}},
		{Name: "/redis", Image: "redis/redis-stack-server:7.2.0-v10", Env: []string{"REDIS_ARGS=--requirepass rpw2 --appendonly no"}, Networks: alias("aegis-redis"),
			Mounts: []mountInfo{{Type: "bind", Source: "/srv/aegis/redis", Destination: "/data"}}},
		{Name: "/nats", Image: "nats:2", Networks: alias("aegis-nats"), Mounts: []mountInfo{
			{Type: "bind", Source: "/srv/aegis/nats/nats-server.conf", Destination: "/etc/nats/nats-server.conf"},
			{Type: "bind", Source: "/srv/aegis/nats/jetstream", Destination: "/data/js"}}},
		{Name: "/temporal", Image: "temporalio/auto-setup:1.29.7", Env: []string{"POSTGRES_USER=zeabur", "POSTGRES_PWD=pgpw", "POSTGRES_SEEDS=aegis-postgres"}, Networks: alias("aegis-temporal")},
		{Name: "/risk-admin", Env: []string{"RISK_S3_ACCESS_KEY=ak", "RISK_S3_SECRET_KEY=sk", "RISK_NEBULA_PASSWORD=nb", "RISK_PG_DSN=postgres://riskuser:x@aegis-postgres:5432/aegis_risk"},
			Labels: map[string]string{"com.docker.compose.service": "risk-admin"}},
		{Name: "/risk-etcd", Networks: alias("risk-etcd"), Mounts: []mountInfo{{Type: "bind", Source: "/srv/aegis/risk/etcd", Destination: "/etcd-data"}}},
	}
	res := ClassifyImport(services, containers, ImportOptions{Stack: "aegis"})
	v := res.Values
	checks := map[string]string{
		"AEGIS_PG_USER": "zeabur", "AEGIS_PG_PASSWORD": "pgpw", "AEGIS_PG_DB": "zeabur_db",
		"AEGIS_PG_DSN_PARAMS": "sslmode=disable&pool_max_conns=40", "AEGIS_PG_DIR": "/srv/aegis/pgdata",
		"AEGIS_NATS_USER": "zeabur", "AEGIS_NATS_PASSWORD": "natspw", "AEGIS_NATS_CONF": "/srv/aegis/nats/nats-server.conf",
		"AEGIS_NATS_DATA": "/srv/aegis/nats/jetstream", "AEGIS_REDIS_PASSWORD": "rpw", "AEGIS_REDIS_EXTRA_ARGS": "--appendonly no",
		"AEGIS_REDIS_IMAGE": "redis/redis-stack-server:7.2.0-v10", "TEMPORAL_DB_PASSWORD": "pgpw",
		"RISK_RUSTFS_ACCESS_KEY": "ak", "RISK_NEBULA_PASSWORD": "nb", "AEGIS_RISK_DIR": "/srv/aegis/risk",
		"JWT_SECRET": "jwt", "TRUSTED_PROXIES": "infra,95.82.8.0/23", "LISTEN_TOGETHER_INTERNAL_TOKEN": "lt", "LT_ROOM_CAPACITY": "50",
	}
	for k, want := range checks {
		if v[k] != want {
			t.Errorf("%s = %q，期望 %q", k, v[k], want)
		}
	}
	for _, k := range []string{"POSTGRES_DSN", "REDIS_ADDR", "NATS_URL", "LT_REDIS_URL"} {
		if _, ok := v[k]; ok {
			t.Errorf("栈覆盖的 %s 不应导入", k)
		}
	}
	notes := strings.Join(res.Notes, "\n")
	// NATS 的挂载点改为直接沿用现网（AEGIS_NATS_DATA_TARGET），不再提示改配置文件
	if v["AEGIS_NATS_DATA_TARGET"] != "/data/js" {
		t.Errorf("AEGIS_NATS_DATA_TARGET = %q，应沿用现网挂载点 /data/js", v["AEGIS_NATS_DATA_TARGET"])
	}
	for _, want := range []string{"/srv/aegis/aegis-api/data", "riskuser"} {
		if !strings.Contains(notes, want) {
			t.Errorf("提示里没有 %q：%s", want, notes)
		}
	}
}

func TestCatalogAndScripts(t *testing.T) {
	ims, err := ResolveImages([]string{"api", "console", "aegis-console"})
	if err != nil || len(ims) != 2 {
		t.Fatalf("ims = %v %v", ims, err)
	}
	if _, err := ResolveImages([]string{"nope"}); err == nil {
		t.Fatal("未知镜像应报错")
	}
	cmd := BuildCommand(Images[2], "ghcr.io/michongs", "sha-abc1234", map[string]string{"AEGIS_BUILD_GOPROXY": "https://goproxy.cn,direct"}, []string{"latest"})
	for _, want := range []string{"-f deploy/docker/listen-together.Dockerfile", "-t ghcr.io/michongs/aegis-listen-together:sha-abc1234", "-t ghcr.io/michongs/aegis-listen-together:latest", "--build-arg GOPROXY=https://goproxy.cn,direct", "--build-arg VERSION=sha-abc1234", "--push"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("构建命令里没有 %q：%s", want, cmd)
		}
	}
	if TagForCommit("0123456789abcdef") != "sha-0123456" {
		t.Fatal("TagForCommit")
	}
	dirs := DirsForLabel("aegis.risk", map[string]string{"AEGIS_RISK_DIR": "/data/risk"})
	script := PrepareDirsScript(dirs)
	if !strings.Contains(script, "chown -R 9999:9999 /data/risk/flink") || !strings.Contains(script, "chown -R 10001:10001 /data/risk/spool/decision-1") {
		t.Fatalf("目录脚本：%s", script)
	}
	if strings.Contains(PrepareDirsScript(DirsForLabel("aegis.db", nil)), "chown") {
		t.Fatal("已有的数据库目录不该改属主")
	}
	if !strings.Contains(NatsConfig("u", "p"), `{ user: "u", password: "p" }`) {
		t.Fatal("NATS 配置")
	}
	p := PruneBackupsScript("/srv/aegis/backups", 7)
	if !strings.Contains(p, `[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-`) || !strings.Contains(p, `-gt 7`) {
		t.Fatalf("清理脚本：%s", p)
	}
	for path, ok := range map[string]bool{"/srv/aegis/backups": true, "/": false, "/srv": false, "relative/x": false, "/srv/aegis/../x": false} {
		if validBackupBase(path) != ok {
			t.Errorf("validBackupBase(%q) 应为 %v", path, ok)
		}
	}
	w := WriteFileScript("/etc/aegis/x", []byte("secret"), "600")
	if strings.Contains(w, "secret") || !strings.Contains(w, "mv -f /etc/aegis/x.aegisctl-tmp /etc/aegis/x") {
		t.Fatalf("写文件脚本：%s", w)
	}
	if df := ParseDF("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/sda1 100 91 9 91% /\n"); df["/"] != 91 {
		t.Fatalf("df = %v", df)
	}
	if c := ConnBudget(map[string]string{"POSTGRES_MAX_CONNS": "40"}, 200); c.Level != "warn" {
		t.Fatalf("6×40+40 > 200 应提醒：%+v", c)
	}
	if c := ConnBudget(map[string]string{"POSTGRES_MAX_CONNS": "20"}, 200); c.Level != "ok" {
		t.Fatalf("budget = %+v", c)
	}
	if displayWidth("中文ab") != 6 || displayWidth("\x1b[31mok\x1b[0m") != 2 {
		t.Fatal("displayWidth")
	}
}

// ─────────────────────────── 假执行器：远端流程 ───────────────────────────

type fakeExec struct {
	mu      sync.Mutex
	scripts []Remote
	handler func(r Remote, stdin string) (string, error)
}

func (f *fakeExec) Run(_ context.Context, r Remote) error {
	stdin := ""
	if r.Stdin != nil {
		b, _ := io.ReadAll(r.Stdin)
		stdin = string(b)
	}
	f.mu.Lock()
	f.scripts = append(f.scripts, r)
	f.mu.Unlock()
	out, err := f.handler(r, stdin)
	if r.Stdout != nil {
		io.WriteString(r.Stdout, out)
	}
	return err
}

func newTestApp(t *testing.T, ex Executor) (*App, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &App{cfg: cfg, ex: ex, out: &out, errOut: &out, now: time.Now}, &out
}

func TestConfigSetNeverPrintsSecret(t *testing.T) {
	remote := "JWT_SECRET=old\nAEGIS_API_REPLICAS=2\n"
	var written string
	ex := &fakeExec{handler: func(r Remote, _ string) (string, error) {
		switch {
		case strings.Contains(r.Script, "echo present"):
			return "present\n" + remote, nil
		case strings.Contains(r.Script, "base64 -d"):
			written = r.Script
			return "", nil
		}
		return "", nil
	}}
	app, out := newTestApp(t, ex)
	if err := app.configSet(context.Background(), []string{"JWT_SECRET=TopSecretNewValue", "AEGIS_API_REPLICAS=3"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "TopSecretNewValue") {
		t.Fatalf("输出里有明文：%s", out.String())
	}
	if !strings.Contains(out.String(), "要 aegisctl deploy 后才对运行中的服务生效") {
		t.Fatal("应提示需要 deploy")
	}
	if !strings.Contains(written, "env-backups/cluster.env.") || strings.Contains(written, "TopSecretNewValue") {
		t.Fatal("写入脚本应先备份，且内容只以 base64 出现")
	}
	for _, r := range ex.scripts {
		if r.Stdin != nil && strings.Contains(r.Script, "TopSecret") {
			t.Fatal("密钥不能出现在 bash -c 的参数里")
		}
	}
}

func TestRunMigrationForcesRerunAndFails(t *testing.T) {
	stateCalls := 0
	forced := false
	state := func(iter int) string {
		return fmt.Sprintf("@@NOW\n1\n@@LS\n@@INSPECT\n"+`{"name":"aegis_aegis-migrate","image":"r/aegis-server:t","mode":{"ReplicatedJob":{}},"job":{"JobIteration":{"Index":%d}}}`+"\n", iter)
	}
	task := func(state string, n int) string {
		return fmt.Sprintf(`{"id":"t%d","state":%q,"err":"exit status 1","ts":"2026-11-17T01:00:0%dZ","job":{"Index":9}}`+"\n", n, state, n)
	}
	ex := &fakeExec{handler: func(r Remote, _ string) (string, error) {
		switch {
		case strings.Contains(r.Script, "@@INSPECT"):
			stateCalls++
			if forced {
				return state(9), nil
			}
			return state(5), nil
		case strings.Contains(r.Script, "update --force"):
			forced = true
			return "", nil
		case strings.Contains(r.Script, "docker service ps -q"):
			return task("failed", 1) + task("failed", 2) + task("failed", 3) + task("failed", 4) + task("failed", 5) + task("failed", 6), nil
		case strings.Contains(r.Script, "service logs"):
			return "migration 000154 failed\n", nil
		}
		return "", nil
	}}
	app, out := newTestApp(t, ex)
	err := app.runMigration(context.Background(), func() error { return nil }, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "迁移失败") || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("err = %v", err)
	}
	if !forced {
		t.Fatal("spec 没变时应 --force 重跑")
	}
	if !strings.Contains(out.String(), "migration 000154 failed") {
		t.Fatalf("应打印迁移日志：%s", out.String())
	}
}

// DeployScript 在真实的 bash 里跑一遍：值原样导出（不求值），标签变量覆盖，docker 从标准输入拿到栈文件
func TestDeployScriptExportsLiterally(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("没有 bash")
	}
	dir := t.TempDir()
	envPath := filepath.Join(dir, "cluster.env")
	content := "# 注释\n\n  LT_ALLOWED_ORIGINS=*\nTRICKY=a $(touch " + filepath.Join(dir, "pwned") + ") `id` \"q\" ' #x\nAEGIS_TAG=from-file\nDOCKER_HOST=tcp://evil:2375\nbad key=1\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "docker")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nenv > \"$OUT/env\"\ncat > \"$OUT/stdin\"\necho \"$@\" > \"$OUT/args\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := DeployScript("aegis", envPath, map[string]string{"AEGIS_TAG": "sha-1234567"})
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "OUT=" + dir}
	cmd.Stdin = strings.NewReader("services: {}\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("cluster.env 的值被 shell 求值了")
	}
	envOut, _ := os.ReadFile(filepath.Join(dir, "env"))
	got := string(envOut)
	for _, want := range []string{"LT_ALLOWED_ORIGINS=*\n", "TRICKY=a $(touch ", "AEGIS_TAG=sha-1234567\n", "AEGIS_ENV_FILE=" + envPath} {
		if !strings.Contains(got, want) {
			t.Errorf("环境里没有 %q", want)
		}
	}
	if strings.Contains(got, "DOCKER_HOST") {
		t.Error("DOCKER_* 不应导出")
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if string(stdin) != "services: {}\n" || !strings.Contains(string(args), "stack deploy --with-registry-auth --prune=false") {
		t.Fatalf("stdin=%q args=%q", stdin, args)
	}
}
