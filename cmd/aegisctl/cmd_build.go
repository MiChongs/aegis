package main

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// cmd_build.go：在 manager 上检出指定提交，用 buildx 构建并推送镜像。

var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var refPattern = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,200}$`)

// localGitHead 本机仓库的 HEAD
func localGitHead(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("读不到本机仓库的 HEAD（用 --ref / --tag 指定）：%w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// TagForCommit 镜像标签：sha-<7 位短哈希>（与 CI 一致）
func TagForCommit(sha string) string {
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return "sha-" + sha
}

// BuildCommand 一个镜像的 buildx 命令
func BuildCommand(im ImageSpec, registry, tag string, env map[string]string, extraTags []string) string {
	args := []string{"docker", "buildx", "build", "--push", "--progress=plain", "-f", im.Dockerfile,
		"-t", registry + "/" + im.Name + ":" + tag}
	for _, t := range extraTags {
		args = append(args, "-t", registry+"/"+im.Name+":"+t)
	}
	keys := make([]string, 0, len(im.BuildArgs)+len(im.FixedArgs))
	vals := map[string]string{}
	for arg, envKey := range im.BuildArgs {
		if v := env[envKey]; v != "" {
			keys = append(keys, arg)
			vals[arg] = v
		}
	}
	for arg, v := range im.FixedArgs {
		keys = append(keys, arg)
		vals[arg] = strings.ReplaceAll(v, "$TAG", tag)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--build-arg", k+"="+vals[k])
	}
	args = append(args, "--label", "org.opencontainers.image.revision="+tag)
	args = append(args, im.Context)
	return ShellJoin(args...)
}

func (a *App) cmdBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	ref := fs.String("ref", "", "要构建的提交或分支（默认本机仓库的 HEAD，须已推送）")
	tag := fs.String("tag", "", "镜像标签（默认 sha-<7 位短哈希>）")
	extra := fs.String("also-tag", "", "额外的标签，逗号分隔（如 latest）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	images, err := ResolveImages(pos)
	if err != nil {
		return err
	}
	if *ref == "" {
		repo, err := a.repo()
		if err != nil {
			return err
		}
		if *ref, err = localGitHead(repo); err != nil {
			return err
		}
	}
	if !refPattern.MatchString(*ref) {
		return usagef("--ref 不合法：%q", *ref)
	}
	var extraTags []string
	for _, t := range splitList(*extra) {
		if !tagPattern.MatchString(t) {
			return usagef("标签不合法：%q", t)
		}
		extraTags = append(extraTags, t)
	}
	envFile, _, err := a.remoteEnv(ctx)
	if err != nil {
		return err
	}
	env := envFile.Map()
	registry := strings.TrimSuffix(env["AEGIS_REGISTRY"], "/")
	if registry == "" {
		return fmt.Errorf("cluster.env 里没有 AEGIS_REGISTRY")
	}
	repoURL := firstNonEmpty(env["AEGIS_REPO_URL"], "https://github.com/MiChongs/aegis.git")

	a.step("在 %s 上检出 %s", a.manager(), *ref)
	checkout := fmt.Sprintf(`src=%[1]s
if [ ! -d "$src/.git" ]; then
  install -d -m 755 "$(dirname "$src")"
  git clone --filter=blob:none %[2]s "$src" >&2
fi
git -C "$src" remote set-url origin %[2]s
git -C "$src" fetch --prune --tags --force origin '+refs/heads/*:refs/remotes/origin/*' >&2
git -C "$src" checkout --force --detach %[3]s >&2 2>/dev/null || git -C "$src" checkout --force --detach origin/%[3]s >&2
git -C "$src" clean -fdq -e node_modules >&2
git -C "$src" rev-parse HEAD
`, remoteSrcDir, ShellQuote(repoURL), ShellQuote(*ref))
	out, err := a.capture(ctx, checkout)
	if err != nil {
		return fmt.Errorf("%w\n（提交要先推到 %s；私有仓库要给 manager 配拉取凭据）", err, repoURL)
	}
	sha := strings.TrimSpace(lastLine(out))
	if *tag == "" {
		*tag = TagForCommit(sha)
	}
	if !tagPattern.MatchString(*tag) {
		return usagef("标签不合法：%q", *tag)
	}
	a.infof("  提交 %s，标签 %s", sha, *tag)

	for _, im := range images {
		a.step("构建 %s/%s:%s", registry, im.Name, *tag)
		script := "cd " + ShellQuote(remoteSrcDir) + "\nexport DOCKER_BUILDKIT=1\n" + BuildCommand(im, registry, *tag, env, extraTags) + "\n"
		if err := a.ex.Run(ctx, Remote{Host: a.manager(), Script: script, Stdout: a.out, Stderr: a.out}); err != nil {
			return fmt.Errorf("构建 %s 失败：%w（推送失败时检查 manager 上的 docker login %s）", im.Name, err, registry)
		}
	}
	a.infof("%s 已推送 %d 个镜像，标签 %s。发布：aegisctl deploy --tag %s", a.pal.Green("完成。"), len(images), *tag, *tag)
	return nil
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}
