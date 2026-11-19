package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// config.go：本机配置 ~/.config/aegisctl/config.json 与远端约定路径。

const (
	defaultManager   = "hk-paas"
	defaultStack     = "aegis"
	remoteConfDir    = "/etc/aegis"
	remoteEnvFile    = "/etc/aegis/cluster.env"
	remoteHistory    = "/etc/aegis/deploy-history.jsonl"
	remoteLockDir    = "/etc/aegis/deploy.lock"
	remoteTraefikBak = "/etc/aegis/traefik-backups"
	remoteSrcDir     = "/srv/aegis/src"
	defaultProbe     = "curlimages/curl:latest"
	stackRelPath     = "deploy/swarm/aegis-stack.yml"
	envTmplRelPath   = "deploy/swarm/cluster.env.example"
	traefikRelPath   = "deploy/swarm/traefik/aegis.yml.tmpl"
	internalNetwork  = "aegis-internal"
	publicNetwork    = "dokploy-network"
)

// Config 本机配置
type Config struct {
	// Manager 能 ssh 上去的 Swarm manager（~/.ssh/config 里的主机名）
	Manager string `json:"manager"`
	// Sudo 远端命令前加 sudo -n（ssh 用户不是 root 时）
	Sudo bool `json:"sudo,omitempty"`
	// SSHArgs 额外的 ssh 参数
	SSHArgs []string `json:"sshArgs,omitempty"`
	// Nodes 节点主机名 → ssh 目标（node join 时记录；exec、backup、doctor 跨节点时用）
	Nodes map[string]string `json:"nodes,omitempty"`
	// RepoDir 本机仓库根目录（栈文件、模板从这里读）；留空时从当前目录向上找
	RepoDir string `json:"repoDir,omitempty"`
	// ProbeImage status 探活用的镜像（要有 sh、nslookup、curl）
	ProbeImage string `json:"probeImage,omitempty"`
	// Stack 栈名（Traefik 后端地址里写死了 aegis_，改名要同时改模板）
	Stack string `json:"stack,omitempty"`

	path string
}

// ConfigDir 本机配置目录
func ConfigDir() (string, error) {
	if d := os.Getenv("AEGISCTL_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "aegisctl"), nil
}

// LoadConfig 读配置；文件不存在时用默认值
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		dir, err := ConfigDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "config.json")
	}
	c := &Config{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("%s 不是合法的 JSON：%w", path, err)
		}
	}
	if c.Manager == "" {
		c.Manager = defaultManager
	}
	if c.Stack == "" {
		c.Stack = defaultStack
	}
	if c.ProbeImage == "" {
		c.ProbeImage = defaultProbe
	}
	if c.Nodes == nil {
		c.Nodes = map[string]string{}
	}
	return c, nil
}

// Save 写回（600）
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(data, '\n'), 0o600)
}

// LocalEnvBackup 本机备份 cluster.env 的路径
func (c *Config) LocalEnvBackup() string {
	return filepath.Join(filepath.Dir(c.path), "cluster.env")
}

// HostForNode 节点主机名对应的 ssh 目标：记录过的优先；是 manager 自己时用 manager；否则直接用主机名
func (c *Config) HostForNode(node, managerHostname string) string {
	if h, ok := c.Nodes[node]; ok && h != "" {
		return h
	}
	if node == managerHostname || node == "" {
		return c.Manager
	}
	return node
}

// FindRepo 找仓库根目录：配置 → 当前目录向上找 deploy/swarm/aegis-stack.yml
func FindRepo(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(filepath.Join(configured, stackRelPath)); err != nil {
			return "", fmt.Errorf("配置的仓库目录 %s 里没有 %s", configured, stackRelPath)
		}
		return configured, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, stackRelPath)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("找不到 Aegis 仓库（需要 %s）：在仓库目录里运行，或用 --repo 指定、写进 config.json 的 repoDir", stackRelPath)
		}
		dir = parent
	}
}

// readRepoFile 读仓库里的文件。换行统一成 LF：Windows 上 core.autocrlf 检出的是 CRLF，
// 原样传到 manager 会让栈文件、Traefik 路由带上回车（路由文件每次都被判成有变化而重写）
func readRepoFile(repo, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n"), nil
}

// validLabelKey 节点标签键
func validLabelKey(k string) bool {
	if k == "" || len(k) > 128 {
		return false
	}
	for _, c := range k {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".-_", c)) {
			return false
		}
	}
	return true
}
