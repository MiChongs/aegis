package mihomo

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"aegis/pkg/egress"
)

// 核心从官方发布页安装：https://github.com/MetaCubeX/mihomo/releases。
// 发布页给每个附件附带 sha256 摘要，下载后逐字节校验，对不上就丢弃。
const releaseRepo = "MetaCubeX/mihomo"

// InstallResult 一次安装的结果。
type InstallResult struct {
	Version string `json:"version"`
	Asset   string `json:"asset"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Source  string `json:"source"`
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// assetName 当前平台对应的发布附件名。amd64 选 compatible 版本：不要求 AVX2，老机器也能跑。
func assetName(goos, goarch, tag string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64", "darwin/amd64":
		return fmt.Sprintf("mihomo-%s-amd64-compatible-%s.gz", goos, tag), nil
	case "linux/arm64", "darwin/arm64", "linux/386", "linux/riscv64", "linux/loong64", "freebsd/amd64", "freebsd/arm64":
		return fmt.Sprintf("mihomo-%s-%s-%s.gz", goos, goarch, tag), nil
	case "linux/arm":
		return fmt.Sprintf("mihomo-linux-armv7-%s.gz", tag), nil
	case "windows/amd64":
		return fmt.Sprintf("mihomo-windows-amd64-compatible-%s.zip", tag), nil
	case "windows/arm64", "windows/386":
		return fmt.Sprintf("mihomo-windows-%s-%s.zip", goarch, tag), nil
	default:
		return "", fmt.Errorf("没有适用于 %s/%s 的 mihomo 发布包", goos, goarch)
	}
}

// Install 下载并安装核心。version 为空或 latest 时取最新正式版。
// 已在跑的核心不会被替换掉正在执行的文件：新文件落盘后需要 Restart 才生效。
func (c *Core) Install(ctx context.Context, version string) (InstallResult, error) {
	c.mu.Lock()
	if c.installing {
		c.mu.Unlock()
		return InstallResult{}, errors.New("核心正在安装中")
	}
	c.installing = true
	previous := c.status
	if c.status != StatusRunning {
		c.status = StatusInstalling
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.installing = false
		if c.status == StatusInstalling {
			c.status = previous
			if previous == StatusMissing && binaryExists(c.BinaryPath()) {
				c.status = StatusStopped
			}
		}
		c.mu.Unlock()
	}()

	installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result, err := c.install(installCtx, version)
	if err != nil {
		c.logs.add("aegis", "error", "安装核心失败: "+err.Error())
		return result, err
	}
	c.logs.add("aegis", "info", fmt.Sprintf("已安装核心 %s（%s，sha256 %s…）", result.Version, result.Asset, result.SHA256[:12]))
	return result, nil
}

func (c *Core) install(ctx context.Context, version string) (InstallResult, error) {
	version = strings.TrimSpace(version)
	endpoint := "https://api.github.com/repos/" + releaseRepo + "/releases/latest"
	if version != "" && version != "latest" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		endpoint = "https://api.github.com/repos/" + releaseRepo + "/releases/tags/" + version
	}
	body, _, err := c.fetch(ctx, endpoint, 4<<20)
	if err != nil {
		return InstallResult{}, fmt.Errorf("查询发布信息失败: %w", err)
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil || rel.TagName == "" {
		return InstallResult{}, fmt.Errorf("发布信息格式无法识别")
	}
	name, err := assetName(runtime.GOOS, runtime.GOARCH, rel.TagName)
	if err != nil {
		return InstallResult{}, err
	}
	var asset *releaseAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return InstallResult{}, fmt.Errorf("发布 %s 里没有附件 %s", rel.TagName, name)
	}
	expected := strings.TrimPrefix(strings.ToLower(asset.Digest), "sha256:")
	if len(expected) != 64 {
		return InstallResult{}, fmt.Errorf("附件 %s 没有 sha256 摘要，拒绝安装", name)
	}

	archive, downloadSource, err := c.fetch(ctx, asset.URL, 128<<20)
	if err != nil {
		return InstallResult{}, fmt.Errorf("下载 %s 失败: %w", name, err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != expected {
		return InstallResult{}, fmt.Errorf("%s 校验失败（期望 %s，实际 %s）", name, expected[:12], got[:12])
	}
	binary, err := extractBinary(name, archive)
	if err != nil {
		return InstallResult{}, err
	}

	target := c.BinaryPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return InstallResult{}, err
	}
	tmp := target + ".download"
	if err := os.WriteFile(tmp, binary, 0o755); err != nil {
		return InstallResult{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkCtx, tmp, "-v").CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(output)), "mihomo") {
		_ = os.Remove(tmp)
		return InstallResult{}, fmt.Errorf("下载的核心无法在本机运行: %s", strings.TrimSpace(string(output)))
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return InstallResult{}, err
	}
	return InstallResult{
		Version: rel.TagName,
		Asset:   name,
		Path:    target,
		SHA256:  expected,
		Size:    int64(len(archive)),
		Source:  downloadSource,
	}, nil
}

// fetch 先走镜像前缀，再回落到经出海网关的直连。
func (c *Core) fetch(ctx context.Context, rawURL string, limit int64) ([]byte, string, error) {
	type attempt struct {
		url    string
		client *http.Client
		label  string
	}
	attempts := make([]attempt, 0, 2)
	if mirror := strings.TrimSpace(c.opts.Mirror); mirror != "" {
		attempts = append(attempts, attempt{
			url:    strings.TrimRight(mirror, "/") + "/" + rawURL,
			client: &http.Client{Timeout: 4 * time.Minute, Transport: &http.Transport{Proxy: nil}},
			label:  "mirror",
		})
	}
	attempts = append(attempts, attempt{
		url:    rawURL,
		client: egress.NewClient(egress.Profile{Name: "egress.mihomo.install", Timeout: 4 * time.Minute}),
		label:  "github",
	})
	var errs []string
	for _, item := range attempts {
		data, err := download(ctx, item.client, item.url, limit)
		if err == nil {
			return data, item.label, nil
		}
		errs = append(errs, item.label+": "+err.Error())
	}
	return nil, "", errors.New(strings.Join(errs, "；"))
}

func download(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "aegis-egress")
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("响应超过 %d MB", limit>>20)
	}
	return data, nil
}

func extractBinary(name string, archive []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(name, ".gz"):
		reader, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, fmt.Errorf("解压 %s 失败: %w", name, err)
		}
		defer reader.Close()
		return io.ReadAll(io.LimitReader(reader, 256<<20))
	case strings.HasSuffix(name, ".zip"):
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("解压 %s 失败: %w", name, err)
		}
		for _, file := range reader.File {
			if strings.HasSuffix(strings.ToLower(file.Name), ".exe") {
				rc, err := file.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, 256<<20))
			}
		}
		return nil, fmt.Errorf("%s 里没有可执行文件", name)
	default:
		return nil, fmt.Errorf("不认识的附件格式: %s", name)
	}
}
